// Package compiler turns MUF source into a runnable program.
package compiler

import (
	"fmt"
	"strings"
)

// Lexical characters, from the top of src/compile.c.
const (
	beginComment   = '('
	endComment     = ')'
	beginString    = '"'
	endString      = '"'
	beginDirective = '$'
	beginMacro     = '.'
	beginEscape    = '\\'
	// escapeChar is what "\[" produces: ASCII ESC, used for ANSI
	// sequences.
	escapeChar = 27
)

// token is one lexed word.
type token struct {
	text string
	line int
	// isString records that the token came from a quoted literal,
	// so a string whose contents look like a number is still a
	// string.
	isString bool
}

// lexer walks MUF source a token at a time.
//
// MUF is line-oriented: a string literal may not span lines, and a
// comment may. Tokens are whitespace-separated except for strings and
// comments, which have their own rules.
type lexer struct {
	lines []string
	// line is the index of the line being read, and col the byte
	// offset in it.
	line int
	col  int
}

func newLexer(src string) *lexer {
	// Normalise line endings so a file written on another
	// platform lexes the same way.
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	return &lexer{lines: strings.Split(src, "\n")}
}

// atEnd reports whether every line has been consumed.
func (l *lexer) atEnd() bool { return l.line >= len(l.lines) }

// lineNumber is the one-based line the lexer is on, for error
// messages.
func (l *lexer) lineNumber() int { return l.line + 1 }

// cur returns the line being read.
func (l *lexer) cur() string {
	if l.atEnd() {
		return ""
	}
	return l.lines[l.line]
}

// advance moves to the start of the next line.
func (l *lexer) advance() {
	l.line++
	l.col = 0
}

// skipSpace moves past whitespace, advancing lines as needed.
func (l *lexer) skipSpace() {
	for !l.atEnd() {
		line := l.cur()
		for l.col < len(line) && isSpace(line[l.col]) {
			l.col++
		}
		if l.col < len(line) {
			return
		}
		l.advance()
	}
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\f' || c == '\v'
}

// next returns the next token. The bool reports whether one was
// available.
func (l *lexer) next() (token, bool, error) {
	for {
		l.skipSpace()
		if l.atEnd() {
			return token{}, false, nil
		}
		line := l.cur()
		c := line[l.col]

		switch c {
		case beginComment:
			if err := l.skipComment(); err != nil {
				return token{}, false, err
			}
			continue
		case beginString:
			return l.lexString()
		}

		start := l.col
		startLine := l.lineNumber()
		for l.col < len(line) && !isSpace(line[l.col]) {
			l.col++
		}
		return token{text: line[start:l.col], line: startLine}, true, nil
	}
}

// maxCommentDepth is how far comments may nest, from do_new_comment.
const maxCommentDepth = 7

// skipComment consumes a "( ... )" comment.
//
// Comments nest, up to seven deep, and may span lines. When a nested
// parse fails — unterminated, or too deep — upstream retries from
// the same place treating the comment as flat, ending at the first
// ')'. Old code relies on that: a comment containing an unbalanced
// '(' only compiles because of the fallback.
func (l *lexer) skipComment() error {
	startLine, startCol := l.line, l.col

	if l.skipNestedComment(0) {
		return nil
	}

	l.line, l.col = startLine, startCol
	if l.skipFlatComment() {
		return nil
	}
	return fmt.Errorf("line %d: unterminated comment", startLine+1)
}

// skipNestedComment consumes a comment whose parentheses balance. It
// reports whether it succeeded, leaving the position undefined if
// not.
func (l *lexer) skipNestedComment(depth int) bool {
	if depth >= maxCommentDepth {
		return false
	}
	l.col++ // past the opening paren
	for {
		if l.atEnd() {
			return false
		}
		line := l.cur()
		if l.col >= len(line) {
			l.advance()
			continue
		}
		switch line[l.col] {
		case endComment:
			l.col++
			return true
		case beginComment:
			if !l.skipNestedComment(depth + 1) {
				return false
			}
		default:
			l.col++
		}
	}
}

// skipFlatComment consumes everything up to the first ')', ignoring
// nesting.
func (l *lexer) skipFlatComment() bool {
	l.col++ // past the opening paren
	for {
		if l.atEnd() {
			return false
		}
		line := l.cur()
		for l.col < len(line) {
			if line[l.col] == endComment {
				l.col++
				return true
			}
			l.col++
		}
		l.advance()
	}
}

// lexString consumes a quoted literal. Escapes are "\r" for a
// carriage return, "\[" for the ANSI escape character, and "\x" for a
// literal x.
func (l *lexer) lexString() (token, bool, error) {
	line := l.cur()
	startLine := l.lineNumber()
	l.col++ // the opening quote

	var b strings.Builder
	for l.col < len(line) {
		c := line[l.col]
		switch {
		case c == endString:
			l.col++
			return token{text: b.String(), line: startLine, isString: true}, true, nil
		case c == beginEscape && l.col+1 < len(line):
			l.col++
			switch line[l.col] {
			case 'r':
				// A carriage return, not a newline:
				// MUCK uses CR as the line separator
				// inside strings, and notify is what
				// turns it into real output lines.
				b.WriteByte('\r')
			case '[':
				b.WriteByte(escapeChar)
			default:
				b.WriteByte(line[l.col])
			}
			l.col++
		default:
			b.WriteByte(c)
			l.col++
		}
	}
	// A string may not span lines, so running off the end is an
	// error rather than a continuation.
	return token{}, false, fmt.Errorf("line %d: unterminated string", startLine)
}

// restOfLine returns what is left of the current line and consumes
// it. A few directives take their argument that way rather than as a
// token.
func (l *lexer) restOfLine() string {
	if l.atEnd() {
		return ""
	}
	rest := l.cur()[l.col:]
	l.col = len(l.cur())
	return strings.TrimSpace(rest)
}
