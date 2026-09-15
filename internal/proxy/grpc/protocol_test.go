package grpcproxy

import (
	"testing"
	"time"
)

func TestTimeoutRoundTrip(t *testing.T) {
	for _, d := range []time.Duration{1, time.Microsecond, 123 * time.Millisecond, 30 * time.Second, time.Duration(1<<63 - 1)} {
		encoded := TimeoutHeader(d)
		decoded, ok := Timeout(encoded)
		if !ok || decoded < d {
			t.Fatalf("%s => %s => %s, valid=%t", d, encoded, decoded, ok)
		}
	}
	for _, bad := range []string{"1", "+1S", "-1S", "999999999n", "3x", "1.2S"} {
		if _, ok := Timeout(bad); ok {
			t.Fatalf("accepted %q", bad)
		}
	}
}
