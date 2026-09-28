package conform

import (
	"encoding/json"
	"testing"
)

// FuzzCheck runs the checker over arbitrary schemas and values, the shape of
// traffic a proxy sees. It must never panic or recurse without end.
func FuzzCheck(f *testing.F) {
	f.Add(`{"type":"object","required":["a"],"properties":{"a":{"type":"integer"}}}`, `{"a":1.5}`)
	f.Add(`{"allOf":[{"$ref":"#/components/schemas/A"}]}`, `{"x":[1,2,{"y":null}]}`)
	f.Add(`{"type":"array","items":{"enum":[1,"a"]}}`, `[1,"b",null]`)
	f.Fuzz(func(t *testing.T, schemaJSON, valueJSON string) {
		var s Schema
		if json.Unmarshal([]byte(schemaJSON), &s) != nil {
			return
		}
		var v any
		if json.Unmarshal([]byte(valueJSON), &v) != nil {
			return
		}
		components := map[string]*Schema{"A": &s}
		_ = Check(components, &s, v, "$")
		_ = Undocumented(components, &s, v, "$")
	})
}
