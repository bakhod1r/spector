package conform

import "testing"

// allOf pointing back at itself must not recurse until the stack overflows;
// that is a fatal error recover cannot catch, so one bad document would kill
// the proxy.
func TestAllOfCycleTerminates(t *testing.T) {
	components := map[string]*Schema{}
	a := &Schema{Type: "object", AllOf: []*Schema{{Ref: "#/components/schemas/A"}},
		Properties: map[string]*Schema{"id": {Type: "integer"}}}
	components["A"] = a
	value := map[string]any{"id": 1.0, "extra": true}
	_ = Check(components, a, value, "$")
	_ = Undocumented(components, a, value, "$")
}

func TestIntegerMustBeWhole(t *testing.T) {
	if got := Check(nil, &Schema{Type: "integer"}, 1.5, "$"); len(got) != 1 {
		t.Fatalf("Check(integer, 1.5) = %v, want one finding", got)
	}
	if got := Check(nil, &Schema{Type: "integer"}, 2.0, "$"); len(got) != 0 {
		t.Fatalf("Check(integer, 2) = %v, want none", got)
	}
}
