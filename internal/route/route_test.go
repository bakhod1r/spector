package route

import (
	"testing"

	"github.com/user/specter/internal/core"
)

func doc(paths ...[2]string) *core.Document {
	d := core.NewDocument("t", "1")
	for _, p := range paths {
		method, path := p[0], p[1]
		d.AddOperation(path, method, core.NewOperation(method+path))
	}
	return d
}

func TestMatchFindsTheOperation(t *testing.T) {
	rs := Compile(doc([2]string{"get", "/users/{id}"}))
	rt, params, ok := Match(rs, "GET", "/users/42")
	if !ok {
		t.Fatal("no match")
	}
	if rt.Path != "/users/{id}" {
		t.Errorf("path = %q, want the documented template", rt.Path)
	}
	if params["id"] != "42" {
		t.Errorf("params = %v, want id 42", params)
	}
}

// The method is part of the identity: POST /users and GET /users are different
// operations with different documents.
func TestMethodMustAgree(t *testing.T) {
	rs := Compile(doc([2]string{"get", "/users"}))
	if _, _, ok := Match(rs, "POST", "/users"); ok {
		t.Error("a POST matched an operation documented only for GET")
	}
}

// A literal segment beats a parameter, and it has to do so on every run — map
// iteration order cannot be allowed to decide which operation a request is.
func TestLiteralPathBeatsAParameterEveryTime(t *testing.T) {
	d := doc([2]string{"get", "/users/{id}"}, [2]string{"get", "/users/me"})
	for i := 0; i < 50; i++ {
		rt, _, ok := Match(Compile(d), "GET", "/users/me")
		if !ok || rt.Path != "/users/me" {
			t.Fatalf("run %d matched %q, want /users/me", i, rt.Path)
		}
	}
}

// Two documents with the same operations must compile to the same order, or
// anything built on top of the result churns between runs.
func TestCompileOrderIsStable(t *testing.T) {
	d := doc(
		[2]string{"get", "/a/b/c"}, [2]string{"get", "/a"}, [2]string{"post", "/a"},
		[2]string{"get", "/a/{id}"}, [2]string{"get", "/a/b"},
	)
	first := Compile(d)
	for i := 0; i < 20; i++ {
		next := Compile(d)
		for j := range first {
			if first[j].Path != next[j].Path || first[j].Method != next[j].Method {
				t.Fatalf("order changed: %s %s then %s %s",
					first[j].Method, first[j].Path, next[j].Method, next[j].Path)
			}
		}
	}
	// Shallower paths first, so the length branch of the sort is exercised.
	for i := 1; i < len(first); i++ {
		if len(first[i-1].segments) > len(first[i].segments) {
			t.Fatalf("not sorted by depth: %v then %v", first[i-1].segments, first[i].segments)
		}
	}
}

// A path with the wrong number of segments is a different path, however much
// of it lines up.
func TestSegmentCountMustAgree(t *testing.T) {
	rs := Compile(doc([2]string{"get", "/users/{id}"}))
	if _, _, ok := Match(rs, "GET", "/users/42/orders"); ok {
		t.Error("a longer path matched a shorter template")
	}
	if _, _, ok := Match(rs, "GET", "/users"); ok {
		t.Error("a shorter path matched a longer template")
	}
}

// A trailing or doubled slash does not change what a path means.
func TestSlashesAreNormalised(t *testing.T) {
	rs := Compile(doc([2]string{"get", "/users"}))
	for _, path := range []string{"/users", "/users/", "//users"} {
		if _, _, ok := Match(rs, "GET", path); !ok {
			t.Errorf("%q did not match", path)
		}
	}
}

func TestNilDocumentCompilesToNothing(t *testing.T) {
	if got := Compile(nil); len(got) != 0 {
		t.Errorf("= %v, want nothing", got)
	}
	if _, _, ok := Match(nil, "GET", "/x"); ok {
		t.Error("matched against an empty table")
	}
}

func TestIsParam(t *testing.T) {
	for seg, want := range map[string]bool{"{id}": true, "id": false, "{id": false, "id}": false} {
		if got := IsParam(seg); got != want {
			t.Errorf("IsParam(%q) = %v, want %v", seg, got, want)
		}
	}
}
