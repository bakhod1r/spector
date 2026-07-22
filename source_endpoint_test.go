package specter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// get issues a request against the console handler configured to scan the
// repository root, which is where this test file lives.
func getSource(t *testing.T, cfg Config, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	Handler(cfg).ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func TestSourceEndpointReturnsTheHandlerCode(t *testing.T) {
	w := getSource(t, Config{Dir: "."}, "/source?file=specter.go&line=1")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\n%s", w.Code, w.Body.String())
	}

	var snip struct {
		File  string   `json:"file"`
		Start int      `json:"start"`
		Line  int      `json:"line"`
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &snip); err != nil {
		t.Fatal(err)
	}
	if snip.File != "specter.go" || snip.Start != 1 {
		t.Errorf("snippet = %+v, want specter.go from line 1", snip)
	}
	if len(snip.Lines) == 0 || !strings.Contains(snip.Lines[0], "package specter") {
		t.Errorf("first line = %q, want the package clause", snip.Lines[0])
	}
}

// The operations in the document carry the positions this endpoint is asked
// for, so a position the generator emits must be one the endpoint will serve.
// If these two ever disagree, every "view source" link in the console breaks.
func TestEverySourceInTheDocumentIsServable(t *testing.T) {
	cfg := Config{Dir: "examples/shop", Title: "t", Version: "1"}
	doc, err := Generate(cfg)
	if err != nil {
		t.Fatal(err)
	}

	checked := 0
	for path, methods := range doc.Paths {
		for method, op := range methods {
			if op.Source == nil {
				t.Errorf("%s %s carries no source", method, path)
				continue
			}
			w := getSource(t, cfg, "/source?file="+op.Source.File+"&line="+itoa(op.Source.Line))
			if w.Code != http.StatusOK {
				t.Errorf("%s %s -> %s:%d is not servable: status %d",
					method, path, op.Source.File, op.Source.Line, w.Code)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no operations checked; the fixture is not producing routes")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// The endpoint reads files on behalf of a request, so the HTTP layer is checked
// separately from the package: a containment bug here would be a file
// disclosure, whatever internal/source does correctly.
func TestSourceEndpointRefusesEscapes(t *testing.T) {
	cfg := Config{Dir: "examples/shop"}
	for name, q := range map[string]string{
		"parent traversal": "file=../../specter.go&line=1",
		"deep traversal":   "file=../../../../etc/passwd&line=1",
		"absolute":         "file=/etc/passwd&line=1",
		"non-Go":           "file=../../go.mod&line=1",
		"no file":          "line=1",
		"no params":        "",
	} {
		t.Run(name, func(t *testing.T) {
			if code := getSource(t, cfg, "/source?"+q).Code; code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", code)
			}
		})
	}
}

// The error must not say which guess was closer; that turns 404s into a probe
// for what exists outside the tree.
func TestSourceEndpointDoesNotLeakTheReason(t *testing.T) {
	body := getSource(t, Config{Dir: "examples/shop"}, "/source?file=../../specter.go&line=1").Body.String()
	for _, leak := range []string{"outside", "scanned", "no such file", "directory", ".."} {
		if strings.Contains(strings.ToLower(body), leak) {
			t.Errorf("response mentions %q: %s", leak, body)
		}
	}
}

// The console is gated as a whole; the source endpoint is part of it and must
// not be an unauthenticated way to read the server's code.
func TestSourceEndpointIsGated(t *testing.T) {
	cfg := Config{Dir: ".", AccessKey: "k"}

	if code := getSource(t, cfg, "/source?file=specter.go&line=1").Code; code != http.StatusNotFound {
		t.Errorf("without a key: status = %d, want 404", code)
	}
	if code := getSource(t, cfg, "/source?file=specter.go&line=1&key=k").Code; code != http.StatusOK {
		t.Errorf("with the key: status = %d, want 200", code)
	}
}
