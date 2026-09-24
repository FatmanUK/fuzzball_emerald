package ascii

import "testing"

// TestAlphanumCompare checks against values taken from the C itself
// rather than from what the ordering ought to be: several of these
// are quirks of upstream's own zero-backtracking, and callers compare
// the result against zero in both directions, so the sign has to
// match and not merely the order.
func TestAlphanumCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"item2", "item10", -8},
		{"item10", "item2", 8},
		{"a", "b", -1},
		{"abc", "abc", 0},
		{"007", "7", -7},
		{"x1y", "x1z", -1},
		{"", "a", -97},
		{"a", "", 97},
		{"File9", "file10", -1},
		{"v1.2", "v1.10", -8},
		{"123456789", "12345678", 57},
		{"0", "00", -48},
		{"a0b", "a00b", 50},
	}
	for _, tt := range tests {
		if got := AlphanumCompare(tt.a, tt.b); got != tt.want {
			t.Errorf("AlphanumCompare(%q, %q) = %d, want %d",
				tt.a, tt.b, got, tt.want)
		}
	}
}
