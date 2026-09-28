package sdk

import (
	"strings"
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

// A literal % in a path is data, not a format verb.
func TestGoClientPathWithPercent(t *testing.T) {
	d := core.NewDocument("t", "1")
	d.AddOperation("/rate/100%/{id}", "get", &core.Operation{
		Parameters: []core.Parameter{{Name: "id", In: "path", Required: true, Schema: &core.Schema{Type: "string"}}},
		Responses:  map[string]*core.Response{"200": {}},
	})
	files, err := Generate(d, Options{Lang: "go"})
	if err != nil {
		t.Fatal(err)
	}
	src := string(files[0].Data)
	if !strings.Contains(src, `"/rate/100%%/%s"`) {
		t.Fatalf("path not escaped for Sprintf:\n%s", src)
	}
}

func TestGoParamNameIsAnIdentifier(t *testing.T) {
	for in, want := range map[string]string{"0": "p0", "type": "typeParam", "func": "funcParam", "id": "id"} {
		if got := goParamName(in); got != want {
			t.Errorf("goParamName(%q) = %q, want %q", in, got, want)
		}
	}
}
