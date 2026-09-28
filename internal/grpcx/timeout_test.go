//go:build grpclive

package grpcx

import (
	"math"
	"testing"
	"time"
)

func TestTimeoutIsClamped(t *testing.T) {
	for _, sec := range []int{600, math.MaxInt64 / 2, math.MaxInt} {
		if d := timeoutOf(Request{TimeoutSec: sec}); d != maxTimeout {
			t.Errorf("timeoutOf(%d) = %v, want %v", sec, d, maxTimeout)
		}
	}
	if d := timeoutOf(Request{TimeoutSec: 3}); d != 3*time.Second {
		t.Errorf("timeoutOf(3) = %v", d)
	}
}
