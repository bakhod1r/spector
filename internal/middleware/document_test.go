package middleware_test

import (
	"strconv"
	"strings"
	"testing"

	ginadapter "github.com/user/specter/internal/adapter/gin"
	"github.com/user/specter/internal/core"
	"github.com/user/specter/internal/gen"

	"github.com/user/specter"
)

// buildDoc runs the fixture all the way to an OpenAPI document, which is where
// the middleware findings have to arrive to be worth anything.
func buildDoc(t *testing.T) *core.Document {
	t.Helper()
	routes, schemas, err := (&ginadapter.Adapter{}).Scan("testdata/app")
	if err != nil {
		t.Fatal(err)
	}
	return gen.Build("t", "1", routes, schemas)
}

func operation(t *testing.T, doc *core.Document, path, method string) *core.Operation {
	t.Helper()
	op := doc.Paths[path][method]
	if op == nil {
		t.Fatalf("no operation %s %s", method, path)
	}
	return op
}

// headerParams resolves the operation's header parameters, following the
// $refs into components. Shared headers are defined once and referenced, so a
// test that only looked at the inline entries would see nothing.
func headerParams(t *testing.T, doc *core.Document, op *core.Operation) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, p := range op.Parameters {
		resolved := p
		if p.Ref != "" {
			key := strings.TrimPrefix(p.Ref, "#/components/parameters/")
			def, ok := doc.Components.Parameters[key]
			if !ok {
				t.Errorf("parameter $ref %q does not resolve", p.Ref)
				continue
			}
			resolved = *def
		}
		if resolved.In == "header" {
			out[resolved.Name] = resolved.Required
		}
	}
	return out
}

// The headers a guard reads are required on every request it guards. A client
// author has no other way to learn this: the handler never mentions them.
func TestGuardHeadersBecomeRequiredParameters(t *testing.T) {
	doc := buildDoc(t)
	params := headerParams(t, doc, operation(t, doc, "/v1/orders", "get"))

	for _, want := range []string{"X-Platform", "X-Sign", "X-Time-Unix", "X-Request-ID"} {
		required, present := params[want]
		if !present {
			t.Errorf("%s is not documented as a parameter; params: %v", want, params)
			continue
		}
		if !required {
			t.Errorf("%s is documented as optional, but the guard rejects requests without it", want)
		}
	}
}

// A route with no guard must not inherit another route's requirements.
func TestUnguardedRouteHasNoInventedHeaders(t *testing.T) {
	doc := buildDoc(t)
	if params := headerParams(t, doc, operation(t, doc, "/health", "get")); len(params) != 0 {
		t.Errorf("= %v, want none", params)
	}
}

// The statuses a guard aborts with are statuses the endpoint really returns,
// even though its handler contains no such code.
func TestGuardStatusesBecomeResponses(t *testing.T) {
	op := operation(t, buildDoc(t), "/v1/orders", "get")
	for _, code := range []int{488, 498, 409, 500} {
		if _, ok := op.Responses[strconv.Itoa(code)]; !ok {
			t.Errorf("%d is missing from the documented responses %v", code, codes(op))
		}
	}
}

// A non-standard code is exactly the case worth explaining: nothing else in the
// document would tell a client that 488 exists or where it comes from.
func TestNonStandardStatusNamesItsSource(t *testing.T) {
	resp := operation(t, buildDoc(t), "/v1/orders", "get").Responses["488"]
	if resp == nil {
		t.Fatal("488 is not documented")
	}
	if resp.Description == "" || resp.Description == "Response" {
		t.Errorf("description = %q; a client cannot tell what 488 means", resp.Description)
	}
}

// The success response the handler produces must survive alongside them.
func TestHandlerResponsesAreNotReplaced(t *testing.T) {
	op := operation(t, buildDoc(t), "/v1/orders", "get")
	if _, ok := op.Responses["200"]; !ok {
		t.Errorf("the handler's own 200 was lost: %v", codes(op))
	}
}

// Authentication middleware makes the operation carry a security requirement,
// which is the whole point: without it every protected route documents as
// public.
func TestGuardedOperationCarriesSecurity(t *testing.T) {
	op := operation(t, buildDoc(t), "/profile", "get")
	if len(op.Security) == 0 {
		t.Fatal("an authenticated route documents no security requirement")
	}
	if len(op.Security[0]) == 0 {
		t.Error("the security requirement names no scheme")
	}
}

func TestPublicOperationCarriesNoSecurity(t *testing.T) {
	if op := operation(t, buildDoc(t), "/health", "get"); len(op.Security) != 0 {
		t.Errorf("a public route claims security: %v", op.Security)
	}
}

// The chain itself is carried as an extension, so the console can show what
// runs in front of a handler even when it implies nothing about the contract.
func TestChainIsCarriedOnTheOperation(t *testing.T) {
	if op := operation(t, buildDoc(t), "/health", "get"); len(op.Middleware) == 0 {
		t.Error("no middleware recorded on an operation that has two")
	}
}

func codes(op *core.Operation) []string {
	out := make([]string, 0, len(op.Responses))
	for c := range op.Responses {
		out = append(out, c)
	}
	return out
}

// A security requirement naming a scheme the document never defines is not
// merely untidy: it makes the document invalid, and every consumer rejects it.
func TestEverySecuritySchemeReferencedIsDefined(t *testing.T) {
	cfg := specter.Config{Dir: "testdata/app", Title: "t", Version: "1"}
	doc, err := specter.Generate(cfg)
	if err != nil {
		t.Fatal(err)
	}

	used := 0
	for path, methods := range doc.Paths {
		for method, op := range methods {
			for _, req := range op.Security {
				for name := range req {
					used++
					if _, defined := doc.Components.SecuritySchemes[name]; !defined {
						t.Errorf("%s %s requires scheme %q, which components.securitySchemes does not define",
							method, path, name)
					}
				}
			}
		}
	}
	if used == 0 {
		t.Fatal("no security requirements were produced; the fixture is not exercising this")
	}
}

// A scheme stated in Config is better evidence than one guessed from a name.
func TestDeclaredSchemeWinsOverTheInferredOne(t *testing.T) {
	declared := specter.SecurityScheme{Type: "apiKey", Name: "X-Company-Token", In: "header"}
	doc, err := specter.Generate(specter.Config{
		Dir: "testdata/app", Title: "t", Version: "1",
		Security: map[string]specter.SecurityScheme{"bearerAuth": declared},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := doc.Components.SecuritySchemes["bearerAuth"]
	if got == nil {
		t.Fatal("bearerAuth is missing")
	}
	if got.Name != "X-Company-Token" {
		t.Errorf("= %+v, want the declared definition to win", *got)
	}
}

// The same guard on many routes is one requirement, not one per route. Defining
// it once in components is what says so — and it keeps a document with a
// hundred guarded endpoints from repeating four identical parameters a hundred
// times.
func TestSharedHeadersAreDefinedOnceAndReferenced(t *testing.T) {
	doc := buildDoc(t)
	op := operation(t, doc, "/v1/orders", "get")

	refs := 0
	for _, p := range op.Parameters {
		if p.Ref == "" {
			continue
		}
		refs++
		if p.Name != "" || p.In != "" || p.Schema != nil {
			t.Errorf("a $ref entry also carries inline fields: %+v", p)
		}
	}
	if refs == 0 {
		t.Fatal("no shared parameters were referenced")
	}
	if len(doc.Components.Parameters) == 0 {
		t.Fatal("components.parameters is empty")
	}
	for key, def := range doc.Components.Parameters {
		if def.Name == "" || def.In != "header" || !def.Required {
			t.Errorf("%s is not a usable header definition: %+v", key, *def)
		}
		// The definition should say where the requirement comes from; "you must
		// send X-Sign" is less useful than knowing which guard demands it.
		if def.Description == "" {
			t.Errorf("%s does not say which middleware requires it", key)
		}
	}
}

// Every $ref in the finished document has to resolve, or consumers reject it.
func TestNoDanglingParameterRefs(t *testing.T) {
	doc := buildDoc(t)
	for path, methods := range doc.Paths {
		for method, op := range methods {
			for _, p := range op.Parameters {
				if p.Ref == "" {
					continue
				}
				key := strings.TrimPrefix(p.Ref, "#/components/parameters/")
				if _, ok := doc.Components.Parameters[key]; !ok {
					t.Errorf("%s %s references undefined parameter %q", method, path, p.Ref)
				}
			}
		}
	}
}
