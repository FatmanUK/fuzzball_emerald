package props

import "testing"

// TestCheckIsPerSegment pins `Prop_Check`'s shape: the sigil counts
// at the start of the path or of any segment after a '/', and nowhere
// else.
func TestCheckIsPerSegment(t *testing.T) {
	for _, tc := range []struct {
		path string
		c    byte
		want bool
	}{
		{"@x", '@', true},
		{"_stuff/@x", '@', true},
		{"a/b/@c", '@', true},
		{"x@y", '@', false},   // not at a segment start
		{"a/x@y", '@', false}, // nor after one
		{"", '@', false},
		{"@", '@', true},
		{"a/@", '@', true},
		{"a/", '@', false}, // a trailing slash starts nothing
	} {
		if got := Check(tc.path, tc.c); got != tc.want {
			t.Errorf("Check(%q, %q) = %v, want %v",
				tc.path, string(tc.c), got, tc.want)
		}
	}
}

// TestTheFiveSigils checks each predicate reads its own character,
// and that ReadOnly reads two.
func TestTheFiveSigils(t *testing.T) {
	for _, tc := range []struct {
		path                                string
		ro, priv, hidden, seeOnly, isSystem bool
	}{
		{"_x", true, false, false, false, false},
		{"%x", true, false, false, false, false},
		{".x", false, true, false, false, false},
		{"@x", false, false, true, false, false},
		{"~x", false, false, false, true, false},
		{"plain", false, false, false, false, false},

		// A system property is hidden as well, because it
		// begins '@' — the two are not exclusive.
		{"@__sys__/x", false, false, true, false, true},

		// And one sigil deep in the path still counts.
		{"a/_b", true, false, false, false, false},
	} {
		if got := IsReadOnly(tc.path); got != tc.ro {
			t.Errorf("IsReadOnly(%q) = %v", tc.path, got)
		}
		if got := IsPrivate(tc.path); got != tc.priv {
			t.Errorf("IsPrivate(%q) = %v", tc.path, got)
		}
		if got := IsHidden(tc.path); got != tc.hidden {
			t.Errorf("IsHidden(%q) = %v", tc.path, got)
		}
		if got := IsSeeOnly(tc.path); got != tc.seeOnly {
			t.Errorf("IsSeeOnly(%q) = %v", tc.path, got)
		}
		if got := IsSystem(tc.path); got != tc.isSystem {
			t.Errorf("IsSystem(%q) = %v", tc.path, got)
		}
	}
}

// TestHasPropPrefixIsCaseSensitive is the divergence the
// consolidation fixed.
//
// `is_prop_prefix` (`fbstrings.c:1152`) compares **bytes** and trims
// a leading '/' from both sides. internal/game's own copy used a
// case-insensitive prefix test and did no trimming, so "@__SYS__/x"
// was a system property to it and not to upstream, and "/@__sys__/x"
// was one to upstream and not to it. Both are now upstream's, and the
// golden case compares the pair.
func TestHasPropPrefixIsCaseSensitive(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"@__sys__", true},
		{"@__sys__/x", true},
		{"/@__sys__/x", true}, // a leading slash is trimmed
		{"//@__sys__", true},  // and repeated ones
		{"@__SYS__/x", false}, // bytes, not folded
		{"@__Sys__", false},
		{"@__sys__x", false}, // must end or hit a '/'
		{"@__sys", false},
		{"", false},
		{"other/@__sys__", false},
	} {
		if got := IsSystem(tc.path); got != tc.want {
			t.Errorf("IsSystem(%q) = %v, want %v",
				tc.path, got, tc.want)
		}
	}
}
