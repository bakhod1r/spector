package export

import (
	"encoding/json"
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

func realtimeDoc() *core.Document {
	return &core.Document{
		Info:    core.Info{Title: "Chat API", Version: "2.0.0"},
		Servers: []core.Server{{URL: "https://api.example.com", Description: "prod"}},
		Paths: map[string]map[string]*core.Operation{
			"/ws/chat": {"get": {
				Summary:  "Chat socket",
				Realtime: "websocket",
				Responses: map[string]*core.Response{
					"200": {Content: jsonBody(&core.Schema{Ref: "#/components/schemas/Message"})},
				},
			}},
			"/events": {"get": {
				Summary:  "Event stream",
				Realtime: "sse",
			}},
			// An ordinary REST operation is not a channel.
			"/users": {"get": {Responses: map[string]*core.Response{"200": {}}}},
		},
		Components: core.Components{
			Schemas: map[string]*core.Schema{
				"Message": obj(map[string]*core.Schema{"text": {Type: "string"}}),
			},
		},
	}
}

func TestAsyncAPIFullDocument(t *testing.T) {
	data, err := AsyncAPI(realtimeDoc())
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	if spec["asyncapi"] != "2.6.0" {
		t.Errorf("version = %v", spec["asyncapi"])
	}
	info := spec["info"].(map[string]any)
	if info["title"] != "Chat API" || info["version"] != "2.0.0" {
		t.Errorf("info = %v", info)
	}

	channels := spec["channels"].(map[string]any)
	// Only the two realtime endpoints become channels; /users does not.
	if len(channels) != 2 {
		t.Fatalf("channels = %d, want 2: %v", len(channels), channels)
	}
	ws, ok := channels["/ws/chat"].(map[string]any)
	if !ok {
		t.Fatalf("missing /ws/chat channel: %v", channels)
	}
	// A WebSocket is bidirectional: both subscribe and publish are described.
	if _, ok := ws["subscribe"]; !ok {
		t.Errorf("ws missing subscribe: %v", ws)
	}
	if _, ok := ws["publish"]; !ok {
		t.Errorf("ws missing publish: %v", ws)
	}
	if b := ws["bindings"].(map[string]any); b["ws"] == nil {
		t.Errorf("ws binding missing: %v", ws)
	}

	sse := channels["/events"].(map[string]any)
	// SSE is server-to-client only: subscribe, no publish.
	if _, ok := sse["subscribe"]; !ok {
		t.Errorf("sse missing subscribe: %v", sse)
	}
	if _, ok := sse["publish"]; ok {
		t.Errorf("sse should not publish: %v", sse)
	}

	// The message schema is carried into components so the $ref resolves.
	comps := spec["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)
	if schemas["Message"] == nil {
		t.Errorf("Message schema not carried: %v", schemas)
	}

	// The server protocol is inferred from the scheme.
	servers := spec["servers"].(map[string]any)
	prod := servers["prod"].(map[string]any)
	if prod["protocol"] != "wss" {
		t.Errorf("protocol = %v, want wss", prod["protocol"])
	}
}

// A document with no realtime endpoints produces a valid spec with no channels
// rather than an error.
func TestAsyncAPINoRealtime(t *testing.T) {
	data, err := AsyncAPI(&core.Document{
		Info:  core.Info{Title: "Plain"},
		Paths: map[string]map[string]*core.Operation{"/x": {"get": {}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	if ch := spec["channels"].(map[string]any); len(ch) != 0 {
		t.Errorf("channels = %v, want none", ch)
	}
}

func TestAsyncAPIDeterministic(t *testing.T) {
	for i := 0; i < 5; i++ {
		a, _ := AsyncAPI(realtimeDoc())
		b, _ := AsyncAPI(realtimeDoc())
		if string(a) != string(b) {
			t.Fatal("AsyncAPI output is not stable")
		}
	}
}
