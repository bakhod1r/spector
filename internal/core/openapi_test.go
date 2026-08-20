package core

import "testing"

// A status the scanner could not read is OpenAPI's "default" response, not an
// invented 200.
func TestSetResponseFilesUnknownStatusAsDefault(t *testing.T) {
	op := NewOperation("op")
	op.SetResponse(0, NewResponse("Response"))
	op.SetResponse(201, NewResponse("Created"))

	if _, ok := op.Responses["default"]; !ok {
		t.Errorf("responses = %v, want a default entry", op.Responses)
	}
	if _, ok := op.Responses["201"]; !ok {
		t.Errorf("responses = %v, want the numbered entry too", op.Responses)
	}
}
