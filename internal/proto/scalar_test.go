package proto

import "testing"

// Every proto scalar must map to a JSON Schema type; anything unrecognised is
// treated as a message reference.
func TestScalarSchema(t *testing.T) {
	cases := []struct {
		proto  string
		typ    string
		format string
		ref    string
	}{
		{proto: "string", typ: "string"},
		{proto: "bool", typ: "boolean"},
		{proto: "bytes", typ: "string", format: "byte"},
		{proto: "double", typ: "number"},
		{proto: "float", typ: "number"},
		{proto: "int32", typ: "integer"},
		{proto: "int64", typ: "integer"},
		{proto: "uint32", typ: "integer"},
		{proto: "uint64", typ: "integer"},
		{proto: "sint32", typ: "integer"},
		{proto: "sint64", typ: "integer"},
		{proto: "fixed32", typ: "integer"},
		{proto: "fixed64", typ: "integer"},
		{proto: "sfixed32", typ: "integer"},
		{proto: "sfixed64", typ: "integer"},
		{proto: "User", ref: "#/components/schemas/User"},
		{proto: "google.protobuf.Timestamp", ref: "#/components/schemas/google.protobuf.Timestamp"},
	}

	for _, tc := range cases {
		t.Run(tc.proto, func(t *testing.T) {
			got := scalarSchema(tc.proto)
			if got.Type != tc.typ {
				t.Errorf("type = %q, want %q", got.Type, tc.typ)
			}
			if got.Format != tc.format {
				t.Errorf("format = %q, want %q", got.Format, tc.format)
			}
			if got.Ref != tc.ref {
				t.Errorf("ref = %q, want %q", got.Ref, tc.ref)
			}
		})
	}
}
