package grpcx

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/fullstorydev/grpcurl"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/jhump/protoreflect/desc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type inFrame struct {
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data,omitempty"`
	Request *Request        `json:"request,omitempty"`
}

type outFrame struct {
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data,omitempty"`
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// wsHandler turns grpcurl invocation callbacks into outbound frames.
type wsHandler struct {
	format grpcurl.Formatter
	send   func(outFrame)
}

func (h *wsHandler) OnResolveMethod(*desc.MethodDescriptor) {}
func (h *wsHandler) OnSendHeaders(metadata.MD)              {}
func (h *wsHandler) OnReceiveHeaders(metadata.MD)           {}
func (h *wsHandler) OnReceiveResponse(m proto.Message) {
	s, err := h.format(m)
	if err != nil {
		h.send(outFrame{Type: "error", Error: err.Error()})
		return
	}
	h.send(outFrame{Type: "message", Data: json.RawMessage(s)})
}
func (h *wsHandler) OnReceiveTrailers(st *status.Status, _ metadata.MD) {
	h.send(outFrame{Type: "status", Code: st.Code().String(), Message: st.Message()})
}

// Stream drives one RPC over a WebSocket connection. It expects an init frame
// carrying the Request, then send/halfClose/cancel frames.
func Stream(protoDir string, conn *websocket.Conn) error {
	defer conn.Close()

	// First frame must be init.
	var initFrame inFrame
	if err := conn.ReadJSON(&initFrame); err != nil {
		return err
	}
	if initFrame.Type != "init" || initFrame.Request == nil {
		conn.WriteJSON(outFrame{Type: "error", Error: "first frame must be init with a request"})
		conn.WriteJSON(outFrame{Type: "end"})
		return nil
	}
	req := *initFrame.Request
	if req.Target == "" {
		conn.WriteJSON(outFrame{Type: "error", Error: "target is required"})
		conn.WriteJSON(outFrame{Type: "end"})
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeoutOf(req))
	defer cancel()

	cc, err := grpc.NewClient(req.Target, grpc.WithTransportCredentials(dialCreds(req)))
	if err != nil {
		conn.WriteJSON(outFrame{Type: "error", Error: err.Error()})
		conn.WriteJSON(outFrame{Type: "end"})
		return nil
	}
	defer cc.Close()

	source, err := descriptorSource(ctx, protoDir, cc)
	if err != nil {
		conn.WriteJSON(outFrame{Type: "error", Error: err.Error()})
		conn.WriteJSON(outFrame{Type: "end"})
		return nil
	}

	// The pipe feeds grpcurl's JSON request parser: each "send" frame writes one
	// JSON message; "halfClose" closes the writer so the parser sees io.EOF.
	pr, pw := io.Pipe()
	rf, formatter, err := grpcurl.RequestParserAndFormatter(
		grpcurl.FormatJSON, source, pr, grpcurl.FormatOptions{EmitJSONDefaultFields: true})
	if err != nil {
		conn.WriteJSON(outFrame{Type: "error", Error: err.Error()})
		conn.WriteJSON(outFrame{Type: "end"})
		return nil
	}

	// One writer goroutine owns all socket writes (gorilla forbids concurrent
	// writers). Frames are funnelled through sendCh.
	sendCh := make(chan outFrame, 16)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for f := range sendCh {
			if err := conn.WriteJSON(f); err != nil {
				return
			}
		}
	}()
	send := func(f outFrame) {
		defer func() { recover() }() // sendCh may be closed on teardown
		sendCh <- f
	}

	var headers []string
	for k, v := range req.Headers {
		headers = append(headers, k+": "+v)
	}
	symbol := strings.Replace(req.Symbol, "/", ".", 1)
	handler := &wsHandler{format: formatter, send: send}

	// Invoke runs in its own goroutine; it blocks pulling requests via rf.Next.
	invokeDone := make(chan error, 1)
	go func() {
		invokeDone <- grpcurl.InvokeRPC(ctx, source, cc, symbol, headers, handler, rf.Next)
	}()

	send(outFrame{Type: "open"})

	// Read loop: translate client frames into pipe writes / control actions.
	readErr := make(chan struct{})
	go func() {
		defer close(readErr)
		for {
			var f inFrame
			if err := conn.ReadJSON(&f); err != nil {
				cancel()
				return
			}
			switch f.Type {
			case "send":
				pw.Write(f.Data)
				pw.Write([]byte("\n"))
			case "halfClose":
				pw.Close()
			case "cancel":
				cancel()
				return
			}
		}
	}()

	// Wait for the invocation to finish (EOF, status, error, cancel, timeout).
	ierr := <-invokeDone
	pw.Close() // unblock any pending parser read
	if ierr != nil && ierr != io.EOF {
		send(outFrame{Type: "error", Error: ierr.Error()})
	}
	send(outFrame{Type: "end"})
	close(sendCh)
	<-writerDone
	return nil
}
