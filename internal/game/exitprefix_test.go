package game

import "testing"

// TestDetailMatchesAgreesWithExitPrefix is exit_prefix's truth table,
// produced by compiling fbstrings.c:120 and running it rather than by
// reading it — which is what the ANSI filters and env_distance both
// needed, and for the same reason: several of these answers are not
// what the code looks like it does.
//
// The three that matter most, because they are what a
// reimplementation gets wrong:
//
//   - "feh" does **not** match "fe". Despite the name, this is an
//     exact alias test and not a prefix test at all.
//   - " feh" matches nothing, while "feh " matches "feh".
//     Whitespace after an alias is skipped; whitespace before one is
//     skipped only when a delimiter has just been consumed.
//   - "a;;b" matches the empty string and "feh;" does not, because
//     the outer loop stops at the end of the name and so never
//     reaches a trailing empty alias.
func TestDetailMatchesAgreesWithExitPrefix(t *testing.T) {
	for _, tc := range []struct {
		name, typed string
		want        bool
	}{
		{"feh", "feh", true},
		{"feh", "fe", false},
		{"feh", "FEH", true},
		{"Feh", "feh", true},
		{"feh", "fehx", false},
		{"feh", "", false},
		{"", "feh", false},
		{"", "", false},
		{"feh;foo", "feh", true},
		{"feh;foo", "foo", true},
		{"feh;foo", "fo", false},
		{"feh; foo", "foo", true},
		{"feh ;foo", "feh", true},
		{"feh  ", "feh", true},
		{" feh", "feh", false},
		{"feh", "feh ", false},
		{"a;b;c", "a", true},
		{"a;b;c", "b", true},
		{"a;b;c", "c", true},
		{"a;b;c", "d", false},
		{"a;;b", "b", true},
		{"a;;b", "", true},
		{"feh;", "", false},
		{";feh", "feh", true},
		{"feh;;", "", true},
		{"a b", "a b", true},
		{"a b", "a", false},
		{"\tfeh", "feh", false},
		{"feh\t", "feh", true},
		{"north;n", "N", true},
		{"north;n", "nor", false},
	} {
		got := detailMatches(tc.name, tc.typed)
		if got != tc.want {
			t.Errorf("detailMatches(%q, %q) = %v, "+
				"want %v", tc.name, tc.typed, got,
				tc.want)
		}
	}
}
