package gin

import "testing"

func TestName(t *testing.T) {
	if got := (&Adapter{}).Name(); got != "gin" {
		t.Errorf("Name() = %q", got)
	}
}
