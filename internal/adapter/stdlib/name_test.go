package stdlib

import "testing"

func TestName(t *testing.T) {
	if got := (&Adapter{}).Name(); got != "stdlib" {
		t.Errorf("Name() = %q", got)
	}
}
