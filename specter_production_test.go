package specter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Production mode hides the scanned application's source from the document:
// every operation's Source is cleared, so the emitted JSON carries no
// x-specter-source (no file paths, no line numbers) and the console's
// "View source" button — gated on that extension — never renders.
func TestGenerateProductionStripsSource(t *testing.T) {
	// Control: without Production, at least one operation carries a Source, so
	// the test below is meaningful (the field is populated to begin with).
	dev, err := Generate(Config{Dir: "examples/shop", Title: "Shop", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !anySource(dev) {
		t.Fatal("no operation has a Source without Production; test cannot prove stripping")
	}

	prod, err := Generate(Config{Dir: "examples/shop", Title: "Shop", Version: "1.0.0", Production: true})
	if err != nil {
		t.Fatal(err)
	}
	if anySource(prod) {
		t.Error("Production document still has an operation Source")
	}
	blob, err := json.Marshal(prod)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "x-specter-source") {
		t.Error("Production JSON still contains x-specter-source")
	}
}

func anySource(d *Document) bool {
	for _, methods := range d.Paths {
		for _, op := range methods {
			if op.Source != nil {
				return true
			}
		}
	}
	return false
}

// The source endpoint is refused in Production even for a hand-crafted request,
// so a caller cannot retrieve a file once the button is gone.
func TestHandlerProductionSource404(t *testing.T) {
	prod := Handler(Config{Dir: "examples/shop", Title: "Shop", Version: "1.0.0", Production: true})
	rec := httptest.NewRecorder()
	prod.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/source?file=main.go&line=1", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("Production source endpoint = %d, want 404", rec.Code)
	}

	// Without Production the endpoint still serves a snippet (regression guard).
	dev := Handler(Config{Dir: "examples/shop", Title: "Shop", Version: "1.0.0"})
	rec = httptest.NewRecorder()
	dev.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/source?file=main.go&line=1", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("dev source endpoint = %d, want 200", rec.Code)
	}
}
