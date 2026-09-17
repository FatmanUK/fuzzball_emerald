// Package ascii provides the case-insensitive string handling Fuzzball uses
// throughout, which is strcasecmp in the C locale.
//
// This is deliberately not Go's Unicode-aware folding. strcasecmp maps only
// A-Z, so "Ä" and "ä" are distinct property names and distinct player names
// upstream. Using strings.ToLower or strings.EqualFold here would silently
// merge names that a real MUCK keeps apart.
package ascii

// Fold lowercases the ASCII letters in s and leaves every other byte alone.
func Fold(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if b == nil {
				b = []byte(s)
			}
			b[i] = c + ('a' - 'A')
		}
	}
	if b == nil {
		return s
	}
	return string(b)
}

// EqualFold reports whether a and b match ignoring ASCII case.
func EqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if lower(a[i]) != lower(b[i]) {
			return false
		}
	}
	return true
}

// Compare orders two strings ignoring ASCII case, returning a negative number,
// zero or a positive number as strcasecmp does.
func Compare(a, b string) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		x, y := lower(a[i]), lower(b[i])
		if x != y {
			return int(x) - int(y)
		}
	}
	return len(a) - len(b)
}

// HasPrefix reports whether s starts with prefix, ignoring ASCII case.
func HasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && EqualFold(s[:len(prefix)], prefix)
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
