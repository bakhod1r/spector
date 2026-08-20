package export

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/bakhod1r/spector/internal/core"
)

// AsyncAPI renders the document's realtime endpoints as an AsyncAPI 2.6
// document. The REST document already marks WebSocket and SSE handlers
// (x-spector-realtime); those become channels, keyed by path, that OpenAPI has
// no vocabulary for. A WebSocket is bidirectional, so it carries both a
// subscribe and a publish operation; SSE streams server-to-client only, so it
// carries subscribe alone. Ordinary REST operations are left to the OpenAPI
// document.
//
// The output is a map marshalled to JSON: json.Marshal sorts map keys, so the
// document is byte-identical across runs despite the map iteration in the
// document model.
func AsyncAPI(doc *core.Document) ([]byte, error) {
	channels := map[string]any{}
	for _, path := range sortedPaths(doc) {
		for _, method := range sortedMethods(doc.Paths[path]) {
			op := doc.Paths[path][method]
			switch op.Realtime {
			case "websocket":
				channels[path] = wsChannel(op)
			case "sse":
				channels[path] = sseChannel(op)
			}
		}
	}

	spec := map[string]any{
		"asyncapi": "2.6.0",
		"info": map[string]any{
			"title":   doc.Info.Title,
			"version": versionOr(doc.Info.Version),
		},
		"channels": channels,
	}
	if servers := asyncServers(doc); len(servers) > 0 {
		spec["servers"] = servers
	}
	// Carry the schemas so payload $refs resolve; the $ref path is the same in
	// both specs (#/components/schemas/X), so nothing needs rewriting.
	if len(doc.Components.Schemas) > 0 {
		spec["components"] = map[string]any{"schemas": doc.Components.Schemas}
	}
	return json.MarshalIndent(spec, "", "  ")
}

func versionOr(v string) string {
	if v == "" {
		return "0.0.0"
	}
	return v
}

func wsChannel(op *core.Operation) map[string]any {
	msg := message(op)
	return map[string]any{
		"description": op.Summary,
		"subscribe":   map[string]any{"message": msg},
		"publish":     map[string]any{"message": msg},
		"bindings":    map[string]any{"ws": map[string]any{}},
	}
}

func sseChannel(op *core.Operation) map[string]any {
	return map[string]any{
		"description": op.Summary,
		"subscribe":   map[string]any{"message": message(op)},
	}
}

// message builds the AsyncAPI message for an operation, taking the payload from
// the first documented JSON response body. With no body the payload is left an
// open object rather than invented.
func message(op *core.Operation) map[string]any {
	if schema := firstJSONResponseSchema(op); schema != nil {
		return map[string]any{"payload": schema}
	}
	return map[string]any{"payload": map[string]any{"type": "object"}}
}

func firstJSONResponseSchema(op *core.Operation) *core.Schema {
	codes := make([]string, 0, len(op.Responses))
	for code := range op.Responses {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		resp := op.Responses[code]
		if resp == nil {
			continue
		}
		if media, ok := resp.Content["application/json"]; ok && media.Schema != nil {
			return media.Schema
		}
	}
	return nil
}

// asyncServers maps the REST servers to AsyncAPI servers, inferring a realtime
// protocol from the URL scheme: an https base is reached over wss, http over
// ws. Names come from the description when set, else server1, server2, …
func asyncServers(doc *core.Document) map[string]any {
	out := map[string]any{}
	for i, s := range doc.Servers {
		name := s.Description
		if name == "" {
			name = fmt.Sprintf("server%d", i+1)
		}
		out[name] = map[string]any{
			"url":      s.URL,
			"protocol": protocolFor(s.URL),
		}
	}
	return out
}

func protocolFor(url string) string {
	switch {
	case strings.HasPrefix(url, "wss://"):
		return "wss"
	case strings.HasPrefix(url, "ws://"):
		return "ws"
	case strings.HasPrefix(url, "https://"):
		return "wss"
	default:
		return "ws"
	}
}
