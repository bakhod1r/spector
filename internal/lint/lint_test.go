package lint

import (
	"strings"
	"testing"

	ginadapter "github.com/user/specter/internal/adapter/gin"
	"github.com/user/specter/internal/core"
)

func fixture(t *testing.T) []Finding {
	t.Helper()
	routes, _, err := (&ginadapter.Adapter{}).Scan("testdata/app")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := Analyze("testdata/app", routes)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func ofKind(fs []Finding, kind string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func mentions(fs []Finding, substr string) bool {
	for _, f := range fs {
		if strings.Contains(f.Message, substr) {
			return true
		}
	}
	return false
}

// A handler that is written but never registered is the common case: someone
// removed a registration or renamed a function, and the endpoint quietly
// stopped existing.
func TestReportsOrphanHandler(t *testing.T) {
	got := ofKind(fixture(t), OrphanHandler)
	if !mentions(got, "deleteUser") {
		t.Errorf("deleteUser not reported; got %v", got)
	}
}

// The check has to stay quiet about ordinary functions, or it gets switched
// off. A helper is not a handler, and neither is a function that takes a
// context but returns something gin never looks at.
func TestDoesNotReportNonHandlers(t *testing.T) {
	got := ofKind(fixture(t), OrphanHandler)
	for _, name := range []string{"helper", "alsoNotAHandler", "router"} {
		if mentions(got, name) {
			t.Errorf("%s was reported as an orphan handler: %v", name, got)
		}
	}
}

// Registered handlers are not orphans, however many routes point at them.
func TestRegisteredHandlersAreNotReported(t *testing.T) {
	got := ofKind(fixture(t), OrphanHandler)
	for _, name := range []string{"listUsers", "getUser", "currentUser"} {
		if mentions(got, name) {
			t.Errorf("%s is registered but was reported: %v", name, got)
		}
	}
}

func TestReportsDuplicateRoute(t *testing.T) {
	got := ofKind(fixture(t), DuplicateRoute)
	if len(got) != 1 {
		t.Fatalf("got %d duplicate findings, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "/users") {
		t.Errorf("message does not name the path: %s", got[0].Message)
	}
}

// Citing the same file:line as "and also here" reads as a bug in the tool.
// Route.Source is the handler declaration, so two registrations of one handler
// share a position, and the message must not pretend otherwise.
func TestDuplicateMessageDoesNotCiteOneLocationTwice(t *testing.T) {
	got := ofKind(fixture(t), DuplicateRoute)
	f := got[0]
	if f.Source == nil {
		t.Fatal("no source on the finding")
	}
	here := at(f.Source)
	if strings.Count(f.String(), here) > 1 {
		t.Errorf("the same location appears twice: %s", f.String())
	}
}

func TestReportsShadowedRoute(t *testing.T) {
	got := ofKind(fixture(t), ShadowedRoute)
	if len(got) != 1 {
		t.Fatalf("got %d shadow findings, want 1: %v", len(got), got)
	}
	msg := got[0].Message
	if !strings.Contains(msg, "/users/me") || !strings.Contains(msg, "/users/{id}") {
		t.Errorf("message names the wrong routes: %s", msg)
	}
}

// ---- unit checks on the matching rules ----

func TestCovers(t *testing.T) {
	cases := []struct {
		pattern, literal string
		want             bool
	}{
		{"/users/{id}", "/users/me", true},
		{"/a/{x}/b", "/a/1/b", true},
		{"/users/{id}", "/users/me/orders", false}, // different depth
		{"/users/{id}", "/orders/me", false},       // different prefix
		{"/users/me", "/users/me", false},          // no parameter: not a shadow
		{"/", "/", false},
	}
	for _, tc := range cases {
		if got := covers(tc.pattern, tc.literal); got != tc.want {
			t.Errorf("covers(%q, %q) = %v, want %v", tc.pattern, tc.literal, got, tc.want)
		}
	}
}

// Order is what decides a shadow. The same two routes registered the other way
// round are not a problem.
func TestShadowOnlyWhenTheParameterComesFirst(t *testing.T) {
	literalFirst := []core.Route{
		{Method: "get", Path: "/users/me"},
		{Method: "get", Path: "/users/{id}"},
	}
	if got := shadowed(literalFirst); len(got) != 0 {
		t.Errorf("literal registered first is fine, got %v", got)
	}

	paramFirst := []core.Route{
		{Method: "get", Path: "/users/{id}"},
		{Method: "get", Path: "/users/me"},
	}
	if got := shadowed(paramFirst); len(got) != 1 {
		t.Errorf("got %d findings, want 1: %v", len(got), got)
	}
}

// A different method is a different route and cannot shadow.
func TestShadowRespectsMethod(t *testing.T) {
	got := shadowed([]core.Route{
		{Method: "get", Path: "/users/{id}"},
		{Method: "post", Path: "/users/me"},
	})
	if len(got) != 0 {
		t.Errorf("got %v, want nothing across methods", got)
	}
}

func TestNoFindingsOnCleanRoutes(t *testing.T) {
	clean := []core.Route{
		{Method: "get", Path: "/users"},
		{Method: "post", Path: "/users"},
		{Method: "get", Path: "/users/{id}"},
	}
	if got := duplicates(clean); len(got) != 0 {
		t.Errorf("duplicates: %v", got)
	}
	if got := shadowed(clean); len(got) != 0 {
		t.Errorf("shadowed: %v", got)
	}
}

// CI output has to be stable or every run looks like a change.
func TestResultsAreSorted(t *testing.T) {
	fs := fixture(t)
	for i := 1; i < len(fs); i++ {
		if fs[i-1].Kind > fs[i].Kind {
			t.Fatalf("not sorted by kind: %v", fs)
		}
	}
	for i := 0; i < 3; i++ {
		again := fixture(t)
		if len(again) != len(fs) {
			t.Fatalf("run %d gave %d findings, first gave %d", i, len(again), len(fs))
		}
		for j := range again {
			if again[j].String() != fs[j].String() {
				t.Fatalf("run %d differs at %d:\n %s\n %s", i, j, again[j], fs[j])
			}
		}
	}
}

// A finding is only actionable if it says where to look.
func TestEveryFindingHasALocation(t *testing.T) {
	for _, f := range fixture(t) {
		if f.Source == nil || f.Source.File == "" || f.Source.Line <= 0 {
			t.Errorf("no usable location: %+v", f)
		}
	}
}

func TestAnalyzeOnMissingDir(t *testing.T) {
	if _, err := Analyze("testdata/nope", nil); err == nil {
		t.Error("a missing directory must be an error")
	}
}

// The example the project ships must be clean, or the check has no credibility.
func TestShopExampleIsClean(t *testing.T) {
	routes, _, err := (&ginadapter.Adapter{}).Scan("../../examples/shop")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := Analyze("../../examples/shop", routes)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("examples/shop has %d findings:\n%v", len(fs), fs)
	}
}
