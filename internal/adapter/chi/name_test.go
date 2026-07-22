package chi

import "testing"

func TestName(t *testing.T) {
	if got := (&Adapter{}).Name(); got != "chi" {
		t.Errorf("Name() = %q", got)
	}
}
