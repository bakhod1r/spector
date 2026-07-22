package realtime

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/user/specter/internal/core"
)

func load(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, "testdata/app", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	funcs := map[string]*ast.FuncDecl{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				if fd, ok := decl.(*ast.FuncDecl); ok {
					funcs[fd.Name.Name] = fd
				}
			}
		}
	}
	return funcs
}

func detectIn(t *testing.T, name string) string {
	t.Helper()
	funcs := load(t)
	fd := funcs[name]
	if fd == nil {
		t.Fatalf("no func %q in the fixture", name)
	}
	return Detect(fd, funcs)
}

// Every WebSocket library spells the upgrade differently, and a project picks
// one. Recognising only gorilla's would silently mis-document the others.
func TestDetectsEveryUpgradeSpelling(t *testing.T) {
	for _, name := range []string{"gorillaWS", "acceptWS", "gobwasWS"} {
		t.Run(name, func(t *testing.T) {
			if got := detectIn(t, name); got != WebSocket {
				t.Errorf("= %q, want websocket", got)
			}
		})
	}
}

// The method name is the signal, not the receiver's type: wrapping the upgrader
// in your own type is common and must not hide the endpoint.
func TestDetectsWrappedUpgrader(t *testing.T) {
	if got := detectIn(t, "wrappedWS"); got != WebSocket {
		t.Errorf("= %q, want websocket", got)
	}
}

func TestDetectsSSEByContentType(t *testing.T) {
	if got := detectIn(t, "sseByHeader"); got != SSE {
		t.Errorf("= %q, want sse", got)
	}
}

func TestDetectsSSEByFrameworkHelper(t *testing.T) {
	if got := detectIn(t, "sseByHelper"); got != SSE {
		t.Errorf("= %q, want sse", got)
	}
}

// The upgrade is often behind a shared helper rather than in the handler.
func TestFollowsDelegation(t *testing.T) {
	if got := detectIn(t, "delegatingWS"); got != WebSocket {
		t.Errorf("= %q, want websocket", got)
	}
}

// ---- the false positives that matter ----

// Almost every handler in a program is an ordinary one, so a rule that fires
// loosely would mislabel the whole document.
func TestOrdinaryHandlerIsNotRealtime(t *testing.T) {
	if got := detectIn(t, "listUsers"); got != "" {
		t.Errorf("= %q, want no kind", got)
	}
}

// net.Listener.Accept takes no arguments; websocket.Accept takes a writer and a
// request. Matching on the name alone would confuse them.
func TestListenerAcceptIsNotAWebSocket(t *testing.T) {
	if got := detectIn(t, "notASocket"); got != "" {
		t.Errorf("= %q, want no kind", got)
	}
}

func TestUnrelatedUpgradeMethodIsNotAWebSocket(t *testing.T) {
	if got := detectIn(t, "notAnUpgrade"); got != "" {
		t.Errorf("= %q, want no kind", got)
	}
}

// The content type has to be written as a header, not merely mentioned.
func TestMentioningTheContentTypeIsNotSSE(t *testing.T) {
	if got := detectIn(t, "mentionsOnly"); got != "" {
		t.Errorf("= %q, want no kind", got)
	}
}

func TestNilInputs(t *testing.T) {
	if got := Detect(nil, nil); got != "" {
		t.Errorf("= %q, want empty", got)
	}
	if got := Detect(&ast.FuncDecl{Name: ast.NewIdent("x")}, nil); got != "" {
		t.Errorf("bodyless decl = %q, want empty", got)
	}
}

// ---- Collect ----

func TestCollectPicksOutRealtimeRoutes(t *testing.T) {
	got := Collect([]core.Route{
		{Method: "get", Path: "/users", HandlerName: "listUsers"},
		{Method: "get", Path: "/ws", HandlerName: "wsHandler", Realtime: WebSocket},
		{Method: "get", Path: "/events", HandlerName: "sseHandler", Realtime: SSE},
	})
	if len(got) != 2 {
		t.Fatalf("got %d endpoints, want 2: %+v", len(got), got)
	}
	if got[0].Kind != WebSocket || got[0].Path != "/ws" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Kind != SSE || got[1].Path != "/events" {
		t.Errorf("second = %+v", got[1])
	}
	if got[0].Method != "GET" {
		t.Errorf("method = %q, want upper case", got[0].Method)
	}
}

func TestCollectOnOrdinaryRoutes(t *testing.T) {
	if got := Collect([]core.Route{{Method: "get", Path: "/users"}}); len(got) != 0 {
		t.Errorf("= %+v, want nothing", got)
	}
}

// Every kind the detector can return needs a sentence, or the console shows a
// realtime endpoint with no explanation of why its response body is missing.
func TestEveryKindHasADescription(t *testing.T) {
	for _, kind := range []string{WebSocket, SSE} {
		if Describe(kind) == "" {
			t.Errorf("no description for %q", kind)
		}
	}
	if Describe("") != "" {
		t.Error("an ordinary handler must have no description")
	}
}
