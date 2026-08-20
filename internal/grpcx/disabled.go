//go:build !grpclive

package grpcx

import (
	"errors"

	"github.com/gorilla/websocket"
)

// Live gRPC calling is behind the grpclive build tag because of what it costs
// to install: grpcurl pulls grpc-go, protoreflect, go-spiffe and the envoy
// control-plane protos — the better part of a hundred megabytes for a feature
// that only the console's "try it" button on a gRPC method uses. Scanning
// .proto sources and writing grpc.json need none of it and are always built.
//
//	go install github.com/bakhod1r/spector/cmd/spector@latest              # light
//	go install -tags grpclive github.com/bakhod1r/spector/cmd/spector@latest
var errDisabled = errors.New("spector was built without live gRPC support; rebuild with -tags grpclive")

// Invoke reports that this build cannot make the call. The error reaches the
// console as the response body, so the button says what to do rather than
// failing silently.
func Invoke(protoDir string, req Request) (string, error) {
	return "", errDisabled
}

// Stream reports the same for the bidirectional console socket.
func Stream(protoDir string, conn *websocket.Conn) error {
	return errDisabled
}
