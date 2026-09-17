package ascii

import "testing"

func TestFoldIsASCIIOnly(t *testing.T) {
	if got := Fold("MiXeD"); got != "mixed" {
		t.Errorf("Fold(MiXeD) = %q", got)
	}
	// Non-ASCII must survive untouched; strcasecmp does not fold it.
	for _, s := range []string{"Ä", "É", "Ω", "日本"} {
		if got := Fold(s); got != s {
			t.Errorf("Fold(%q) = %q, want it unchanged", s, got)
		}
	}
}

func TestEqualFold(t *testing.T) {
	if !EqualFold("Foo", "fOO") {
		t.Error("Foo and fOO should match")
	}
	if EqualFold("Ä", "ä") {
		t.Error("non-ASCII case must not be folded")
	}
	if EqualFold("abc", "ab") {
		t.Error("different lengths should not match")
	}
}

func TestCompareOrdersCaseInsensitively(t *testing.T) {
	cases := []struct {
		a, b string
		want int // sign
	}{
		{"apple", "Banana", -1},
		{"Banana", "apple", 1},
		{"same", "SAME", 0},
		{"ab", "abc", -1},
		{"abc", "ab", 1},
	}
	for _, c := range cases {
		got := Compare(c.a, c.b)
		if (got < 0) != (c.want < 0) || (got > 0) != (c.want > 0) {
			t.Errorf("Compare(%q, %q) = %d, want sign %d", c.a, c.b, got, c.want)
		}
	}
}

func TestHasPrefix(t *testing.T) {
	if !HasPrefix("@DESCRIBE", "@desc") {
		t.Error("@DESCRIBE should have prefix @desc")
	}
	if HasPrefix("@de", "@describe") {
		t.Error("a shorter string cannot have a longer prefix")
	}
}
