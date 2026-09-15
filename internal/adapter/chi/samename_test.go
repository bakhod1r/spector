package chi

import (
	"reflect"
	"testing"
)

// A Go service names its handlers after what they do, so ProductHandler.Get and
// OrderHandler.Get sit in the same package as a matter of course. Resolving the
// registration by the method's bare name documented one endpoint from the
// other's body: the wrong summary, the wrong response schema, the wrong status
// codes — and, because the lookup ranged a map, a different wrong answer on
// each run.
func TestSameMethodNameOnTwoTypes(t *testing.T) {
	routes, _, _, err := (&Adapter{}).Scan("testdata/samename")
	if err != nil {
		t.Fatal(err)
	}
	m := routeMap(routes)

	for _, tc := range []struct {
		key     string
		summary string
		resp    string
	}{
		{"get /v1/products/{productID}", "Returns one product.", "Product"},
		{"get /v1/orders/{orderID}", "Returns one order.", "Order"},
	} {
		r, ok := m[tc.key]
		if !ok {
			t.Fatalf("%s not scanned; got %v", tc.key, keysOf(m))
		}
		if r.Summary != tc.summary {
			t.Errorf("%s: summary = %q, want %q", tc.key, r.Summary, tc.summary)
		}
		if r.ResponseType != tc.resp {
			t.Errorf("%s: response = %q, want %q", tc.key, r.ResponseType, tc.resp)
		}
	}
}

// The path keeps the adapter's spelling here; normalisation happens once for
// every adapter in the spector package.
//
// The envelope a list endpoint returns is the whole payload a client receives.
// Following a bare `Get` out of r.URL.Query().Get into a project method named
// Get attributed that method's response to the list, so the document promised
// the item type where the API sends a page.
func TestListKeepsItsEnvelope(t *testing.T) {
	routes, _, _, err := (&Adapter{}).Scan("testdata/samename")
	if err != nil {
		t.Fatal(err)
	}
	list, ok := routeMap(routes)["get /v1/products/"]
	if !ok {
		t.Fatalf("list route not scanned; got %v", keysOf(routeMap(routes)))
	}
	if list.ResponseType != "ProductPage" {
		t.Errorf("response = %q, want ProductPage", list.ResponseType)
	}
	if got := list.QueryTypes["page"]; got != "integer" {
		t.Errorf("page type = %q, want integer (the handler runs strconv.Atoi over it)", got)
	}
}

// A scan is run in CI and its output committed, so the same tree has to produce
// the same routes every time. Two lookups used to range a map and return
// whichever entry came first.
func TestScanIsDeterministic(t *testing.T) {
	first, _, _, err := (&Adapter{}).Scan("testdata/samename")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		got, _, _, err := (&Adapter{}).Scan("testdata/samename")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(first, got) {
			t.Fatalf("run %d differs from the first:\n first = %+v\n got   = %+v", i, first, got)
		}
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
