package gin

import "testing"

// scanWrapped documents a project shaped like a real one: routes registered in
// a subpackage, groups handed to registration functions as arguments, handlers
// that reach the framework only through helpers, and a test file that builds
// its own router.
func scanWrapped(t *testing.T) map[string]struct {
	req, resp string
	statuses  []int
} {
	t.Helper()
	a := &Adapter{}
	routes, schemas, _, err := a.Scan("testdata/wrapped")
	if err != nil {
		t.Fatal(err)
	}
	if schemas["LoginReq"] == nil || schemas["TokenResp"] == nil || schemas["CreatedResp"] == nil {
		t.Fatalf("missing schemas: %v", schemas)
	}
	out := map[string]struct {
		req, resp string
		statuses  []int
	}{}
	for _, r := range routes {
		var statuses []int
		for _, resp := range r.Responses {
			statuses = append(statuses, resp.Status)
		}
		out[r.Method+" "+r.Path] = struct {
			req, resp string
			statuses  []int
		}{r.RequestType, r.ResponseType, statuses}
	}
	return out
}

// A group passed straight into a function — registerMFA(private.Group("/auth/mfa")) —
// never becomes a variable, and every route inside used to be documented at its
// bare path.
func TestScanResolvesGroupPassedAsArgument(t *testing.T) {
	m := scanWrapped(t)
	for _, want := range []string{
		"post /api/v1/auth/mfa/enroll",
		"get /api/v1/auth/mfa/dynamic",
		"post /api/v1/admin/billing/invoices",
		"get /api/v1/admin/billing/list",
		"get /api/v1/admin/billing/refunds/pending",
	} {
		if _, ok := m[want]; !ok {
			t.Errorf("missing %s; got %v", want, keys(m))
		}
	}
}

// Routes registered a directory below the scanned root are part of the API. A
// single-directory scan reported none of them and left the caller to guess
// which package to point at.
func TestScanIsRecursive(t *testing.T) {
	if _, ok := scanWrapped(t)["get /health"]; !ok {
		t.Error("missing GET /health from the subpackage")
	}
}

// A router built by a test is not the API, and documenting it also collides
// with the real routes: the duplicate costs the real operation its name.
func TestScanSkipsTestFiles(t *testing.T) {
	if _, ok := scanWrapped(t)["get /"]; ok {
		t.Error("documented GET / from routes_test.go")
	}
}

// Handlers that bind and respond through package helpers must document the same
// as handlers that call gin directly; otherwise a project with an httpx-style
// wrapper gets paths and nothing else.
func TestScanFollowsHandlerHelpers(t *testing.T) {
	m := scanWrapped(t)

	enroll := m["post /api/v1/auth/mfa/enroll"]
	if enroll.req != "LoginReq" {
		t.Errorf("enroll request = %q, want LoginReq through bindJSON", enroll.req)
	}
	if enroll.resp != "TokenResp" {
		t.Errorf("enroll response = %q, want TokenResp through ok()", enroll.resp)
	}

	// The status belongs to the helper that wrote it: created() answers 201,
	// and reporting 200 here is the failure that made the document lie.
	invoice := m["post /api/v1/admin/billing/invoices"]
	if len(invoice.statuses) != 1 || invoice.statuses[0] != 201 {
		t.Errorf("invoice statuses = %v, want [201]", invoice.statuses)
	}
	if invoice.resp != "CreatedResp" {
		t.Errorf("invoice response = %q, want CreatedResp", invoice.resp)
	}
}

// A code the source states through a value the scanner cannot read is reported
// as unknown (0, which the document renders as "default") rather than guessed.
func TestScanDoesNotInventAStatus(t *testing.T) {
	dyn := scanWrapped(t)["get /api/v1/auth/mfa/dynamic"]
	if len(dyn.statuses) != 1 || dyn.statuses[0] != 0 {
		t.Errorf("dynamic statuses = %v, want [0]", dyn.statuses)
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// c.DefaultQuery("limit", "20") states a server behaviour a client has no other
// way to learn, so the fallback travels into the parameter schema.
func TestScanRecordsQueryDefault(t *testing.T) {
	a := &Adapter{}
	routes, _, _, err := a.Scan("testdata/wrapped")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range routes {
		if r.Method+" "+r.Path != "get /api/v1/admin/billing/list" {
			continue
		}
		if r.QueryDefaults["limit"] != "20" {
			t.Errorf("query defaults = %v, want limit=20", r.QueryDefaults)
		}
		return
	}
	t.Fatal("route not found")
}
