package ascii

import "strings"

// SMatch implements MUCK pattern matching, which is not a regular expression:
// '*' matches any run, '?' any single character, '{a|b}' any alternative, and
// everything is case-insensitive.
func SMatch(s, pattern string) bool {
	return smatchAt(Fold(s), Fold(pattern))
}

func smatchAt(s, p string) bool {
	for len(p) > 0 {
		switch p[0] {
		case '*':
			// Collapse a run of stars, then try every split.
			for len(p) > 0 && p[0] == '*' {
				p = p[1:]
			}
			if p == "" {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if smatchAt(s[i:], p) {
					return true
				}
			}
			return false
		case '?':
			if s == "" {
				return false
			}
			s, p = s[1:], p[1:]
		case '{':
			end := strings.IndexByte(p, '}')
			if end < 0 {
				// An unmatched brace is a literal.
				if s == "" || s[0] != '{' {
					return false
				}
				s, p = s[1:], p[1:]
				continue
			}
			for _, alt := range strings.Split(p[1:end], "|") {
				if strings.HasPrefix(s, alt) && smatchAt(s[len(alt):], p[end+1:]) {
					return true
				}
			}
			return false
		case '\\':
			if len(p) > 1 {
				p = p[1:]
			}
			fallthrough
		default:
			if s == "" || s[0] != p[0] {
				return false
			}
			s, p = s[1:], p[1:]
		}
	}
	return s == ""
}
