package route

import (
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

func FuzzMatch(f *testing.F) {
	d := core.NewDocument("t", "1")
	d.AddOperation("/users/{id}", "get", core.NewOperation("getUser"))
	d.AddOperation("/users/{id}/posts/{post}", "get", core.NewOperation("getPost"))
	routes := Compile(d)
	for _, s := range []string{"/users/1", "//", "/users/%2F/posts/x", "", "/users/1/posts/"} {
		f.Add("GET", s)
	}
	f.Fuzz(func(t *testing.T, method, path string) {
		rt, _, ok := Match(routes, method, path)
		if ok && rt.Op == nil {
			t.Fatalf("matched with no operation: %+v", rt)
		}
	})
}
