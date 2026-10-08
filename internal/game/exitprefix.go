package game

// detailMatches is fbstrings.c:120's exit_prefix, whose name is the
// single most misleading thing in that file: **it is not a prefix
// test.** It walks the ';'-separated aliases of name and asks whether
// typed equals one of them whole, folded.
//
// `look.c:411` is its only caller, matching what a player typed
// against each name in the `_details` propdir.
//
// It is written as a direct port of the pointer walk rather than as a
// split-and-compare, because the whitespace rules fall out of where
// the C happens to leave its cursor and are not what anybody would
// write deliberately:
//
//   - whitespace *after* an alias is skipped, so "feh ;foo" matches
//     "feh";
//   - whitespace *before* an alias is skipped only when a delimiter
//     has just been consumed — so "feh; foo" matches "foo", but
//     " feh" matches nothing at all;
//   - a trailing space in what the player typed defeats the match,
//     which in practice never happens because arg1 arrives trimmed;
//   - an empty alias matches an empty typed string, so "a;;b" does
//     and "feh;" does not, the trailing one never being reached.
//
// internal/match's matchAlias is deliberately not reused. It splits
// an argument off at a space and compares against the whole typed
// line, because an exit may take one; sharing a single function
// between the two would make one of them wrong, for the same reason
// the two ANSI filters are kept apart.
//
// The expectations in exitprefix_test.go were produced by compiling
// exit_prefix and running it, not by reading it.
func detailMatches(name, typed string) bool {
	s := 0
	for s < len(name) {
		p := 0
		for s < len(name) && p < len(typed) &&
			lowerASCII(name[s]) == lowerASCII(typed[p]) {
			s++
			p++
		}
		s = skipSpace(name, s)
		if p == len(typed) &&
			(s == len(name) || name[s] == exitDelimiter) {
			return true
		}
		for s < len(name) && name[s] != exitDelimiter {
			s++
		}
		if s < len(name) {
			s++
		}
		s = skipSpace(name, s)
	}
	return false
}

// skipSpace advances past C isspace characters, which in the default
// locale are these six and nothing else.
func skipSpace(s string, i int) int {
	for i < len(s) {
		switch s[i] {
		case ' ', '\t', '\n', '\v', '\f', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

// lowerASCII is tolower for one byte, A–Z only — upstream folds
// no further, so "Ä" and "ä" are distinct alias names.
func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
