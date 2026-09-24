package mcp

import "strings"

// The token readers below each return the token, the rest of the
// line, and whether anything was read. They are upstream's, character
// for character, because a client's idea of what is a valid
// identifier has to match the server's or messages are silently
// dropped.

// readIdent reads a key or package name: a letter or underscore, then
// letters, digits, underscores and hyphens.
func readIdent(in string) (tok, rest string, ok bool) {
	if in == "" || !isIdentStart(in[0]) {
		return "", in, false
	}
	i := 1
	for i < len(in) && isIdentChar(in[i]) {
		i++
	}
	return in[:i], in[i:], true
}

func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9' || c == '-'
}

// readUnquoted reads a bare value: printable characters other than
// the four that mean something to the parser.
func readUnquoted(in string) (tok, rest string, ok bool) {
	i := 0
	for i < len(in) && isSimpleChar(in[i]) {
		i++
	}
	if i == 0 {
		return "", in, false
	}
	return in[:i], in[i:], true
}

// isSimpleChar reports whether a byte may appear in an unquoted
// value.
//
// The test is upstream's isprint, which is ASCII-only: a byte above
// 127 is not printable to it, so a value carrying UTF-8 has to be
// quoted. That is what clients do, so the restriction costs nothing
// and keeping it means a message parses the same on both servers.
func isSimpleChar(c byte) bool {
	switch c {
	case '*', ':', '\\', '"', ' ':
		return false
	}
	return c > 0x20 && c < 0x7f
}

// readQuoted reads a "..." value, in which a backslash escapes the
// next character.
func readQuoted(in string) (tok, rest string, ok bool) {
	if in == "" || in[0] != '"' {
		return "", in, false
	}
	var b strings.Builder
	i := 1
	for i < len(in) {
		switch in[i] {
		case '\\':
			i++
			if i < len(in) {
				b.WriteByte(in[i])
				i++
			}
		case '"':
			return b.String(), in[i+1:], true
		default:
			b.WriteByte(in[i])
			i++
		}
	}
	// No closing quote: the value is not a value.
	return "", in, false
}

// skipSpace requires at least one space and consumes every following
// one.
func skipSpace(in string) (rest string, ok bool) {
	if in == "" || !isSpace(in[0]) {
		return in, false
	}
	i := 0
	for i < len(in) && isSpace(in[i]) {
		i++
	}
	return in[i:], true
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}
