package ascii

import "testing"

// SMatch had no test at all, which is how it came to be a plain
// alternation where upstream's `{a|b}` is a word pattern and to have
// no `[a-z]` classes whatever. The exhaustive table is
// `internal/golden/smatch_test.go`, where the compiled C decides
// every answer through MUF `SMATCH`; this is the same rules without
// the oracle, so `go test ./internal/ascii` still means something.
func TestSMatch(t *testing.T) {
	cases := []struct {
		s, p string
		want bool
	}{
		// '*' and '?'.
		{"dog", "d*g", true},
		{"dg", "d*g", true},
		{"dogs", "d*g", false},
		{"dg", "d?g", false},
		{"abcd", "*?d", true},
		{"d", "*?d", false},

		// A set is a whole word, anchored at the start of the
		// subject or just after a space.
		{"gold", "{gold|silver}", true},
		{"goldfish", "{gold|silver}*", false},
		{"gold fish", "{gold|silver}*", true},
		{"foop", "{foo}p", false},
		{"pfoo", "*{foo}", false},
		{"p foo", "*{foo}", true},
		{"a gold ring", "*{gold}*", true},
		{"a golden ring", "*{gold}*", false},
		// ...and a set after a star that is not at the start
		// of the pattern is only tried after a *later* space,
		// so this matches nothing at all.
		{"say gold now", "say *{gold}*", false},

		// The anchor is on the subject: a set reached with
		// characters already consumed and no space before it
		// matches nothing.
		{"say gold", "say {gold}", true},
		{"xgold", "x{gold}", false},

		// A leading '^' inverts a set.
		{"gold", "{^gold|silver}", false},
		{"bronze", "{^gold|silver}", true},

		// Character classes, ranges, and the two dashes that
		// are literals because of where the C reads them.
		{"Mr.", "M[rs].", true},
		{"Mx.", "M[rs].", false},
		{"Mb", "M[a-z]", true},
		{"M0", "M[a-z]", false},
		{"0", "[^a-z]", true},
		{"q", "[^a-z]", false},
		{"a-b", "a[-]b", true},
		{"azb", "a[z-]b", true},
		{"a-b", "a[z-]b", true},
		{"xyzq", "*[q]", true},
		{"xyzq", "*[a-c]", false},

		// An unterminated class or set matches nothing, not
		// even itself.
		{"a[bc", "a[bc", false},
		{"gold", "{gold", false},

		// Escapes, including a trailing backslash.
		{"a*b", `a\*b`, true},
		{"axb", `a\*b`, false},
		{"ab", `ab\`, false},
		{"a|b", `{a\|b}`, true},
		{"a}b", `{a\}b}`, true},

		// Folding, and the empty pattern.
		{"DOG", "d*g", true},
		{"", "", true},
		{"x", "", false},
		{"", "x", false},
	}
	for _, c := range cases {
		if got := SMatch(c.s, c.p); got != c.want {
			t.Errorf("SMatch(%q, %q) = %v, want %v",
				c.s, c.p, got, c.want)
		}
	}
}
