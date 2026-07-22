package gqlgenx

import (
	"testing"
	"time"

	"github.com/user/specter/internal/core"
)

// walk pulls every schema reachable from a root into the used set, so the
// emitted document carries exactly the types the API touches.
func TestWalkFollowsEveryReferenceKind(t *testing.T) {
	all := map[string]*core.Schema{
		"Direct": {Type: "object"},
		"InList": {Type: "object"},
		"InMap":  {Type: "object"},
		"InProp": {Type: "object"},
	}
	root := &core.Schema{
		Type: "object",
		Properties: map[string]*core.Schema{
			"direct": {Ref: refPrefix + "Direct"},
			"list":   {Type: "array", Items: &core.Schema{Ref: refPrefix + "InList"}},
			"m":      {Type: "object", AdditionalProperties: &core.Schema{Ref: refPrefix + "InMap"}},
			"nested": {Type: "object", Properties: map[string]*core.Schema{
				"deep": {Ref: refPrefix + "InProp"},
			}},
		},
	}

	used := map[string]bool{}
	walk(root, all, used)

	for _, want := range []string{"Direct", "InList", "InMap", "InProp"} {
		if !used[want] {
			t.Errorf("%s was not reached", want)
		}
	}
}

func TestWalkNilSchema(t *testing.T) {
	used := map[string]bool{}
	walk(nil, map[string]*core.Schema{}, used)
	if len(used) != 0 {
		t.Errorf("used = %v, want empty", used)
	}
}

// A reference to a type that is not in the registry is simply not collected;
// it must not panic or invent an entry.
func TestWalkUnknownRefIsNotCollected(t *testing.T) {
	used := map[string]bool{}
	walk(&core.Schema{Ref: refPrefix + "Missing"}, map[string]*core.Schema{}, used)
	if used["Missing"] {
		t.Error("an unknown ref was marked used")
	}
}

// Types that reference each other must terminate.
func TestWalkHandlesCycles(t *testing.T) {
	node := &core.Schema{Type: "object", Properties: map[string]*core.Schema{
		"self": {Ref: refPrefix + "Node"},
	}}
	all := map[string]*core.Schema{"Node": node}

	done := make(chan map[string]bool, 1)
	go func() {
		used := map[string]bool{}
		walk(node, all, used)
		done <- used
	}()

	select {
	case used := <-done:
		if !used["Node"] {
			t.Error("Node was not collected")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("walk did not terminate on a cyclic schema")
	}
}
