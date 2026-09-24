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

// commentMode selects how "( ... )" comments are parsed. It is
// upstream's force_comment, which starts from the muf_comments_strict
// parameter and is changed by $pragma.
type commentMode int

const (
	// commentLoose parses nested, and retries flat from the same
	// place when that fails. This is the default, and the reason
	// old code containing an unbalanced '(' inside a comment
	// still compiles.
	commentLoose commentMode = iota
	// commentStrict parses flat only, ending at the first ')'.
	commentStrict
	// commentRecurse parses nested only, and reports why when
	// that fails instead of quietly falling back.
	commentRecurse
)

// Comment parse outcomes, which are do_new_comment's return codes.
// Only commentRecurse tells them apart; the other two modes treat any
// non-zero code the same way.
const (
	commentOK = iota
	commentUnterminated
	commentExpected
	commentTooDeep
)

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

	// comments is how "( ... )" is parsed. $pragma writes to it
	// mid-compile, so it lives here rather than being fixed at
	// construction.
	comments commentMode
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

// skipComment consumes a "( ... )" comment, as the mode directs.
//
// Comments nest, up to seven deep, and may span lines. In the default
// loose mode a failed nested parse — unterminated, or too deep —
// is retried from the same place as a flat comment ending at the
// first ')'. Old code relies on that: a comment containing an
// unbalanced '(' only compiles because of the fallback.
//
// The line reported is the comment's opening one rather than
// upstream's, which is wherever the parse gave up; upstream makes up
// for that by appending "Comment starting at line N.", which says the
// same thing the other way round.
func (l *lexer) skipComment() error {
	startLine, startCol := l.line, l.col

	if l.comments == commentStrict {
		if l.skipFlatComment() {
			return nil
		}
		return &Error{Line: startLine + 1,
			Msg: "Unterminated comment."}
	}

	switch code := l.skipNestedComment(0); {
	case code == commentOK:
		return nil
	case l.comments == commentRecurse:
		return &Error{Line: startLine + 1,
			Msg: commentErrorText(code)}
	}

	l.line, l.col = startLine, startCol
	if l.skipFlatComment() {
		return nil
	}
	return &Error{Line: startLine + 1,
		Msg: "Unterminated comment."}
}

// commentErrorText is what do_abort_compile is given for each of
// do_new_comment's failure codes.
func commentErrorText(code int) string {
	switch code {
	case commentExpected:
		return "Expected comment."
	case commentTooDeep:
		return "Comments nested too deep " +
			"(more than 7 levels)."
	}
	return "Unterminated comment."
}

// skipNestedComment consumes a comment whose parentheses balance,
// returning one of the comment outcome codes. The position is
// undefined unless it returns commentOK.
func (l *lexer) skipNestedComment(depth int) int {
	if l.atEnd() || l.col >= len(l.cur()) ||
		l.cur()[l.col] != beginComment {
		return commentExpected
	}
	if depth >= maxCommentDepth {
		return commentTooDeep
	}
	l.col++ // past the opening paren
	for {
		if l.atEnd() {
			return commentUnterminated
		}
		line := l.cur()
		if l.col >= len(line) {
			l.advance()
			continue
		}
		switch line[l.col] {
		case endComment:
			l.col++
			return commentOK
		case beginComment:
			code := l.skipNestedComment(depth + 1)
			if code != commentOK {
				return code
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
	return strings.TrimSpace(l.restOfLineRaw())
}

// restOfLineRaw is restOfLine without the trim, for the messages that
// quote what they are discarding exactly as upstream does.
func (l *lexer) restOfLineRaw() string {
	if l.atEnd() {
		return ""
	}
	rest := l.cur()[l.col:]
	l.col = len(l.cur())
	return rest
}

// skipSpaceOnLine moves past whitespace without crossing a line
// boundary. Upstream's skip_whitespace works on one line's buffer, so
// a directive that checks for an argument after it finds none when
// the argument would be on the next line.
func (l *lexer) skipSpaceOnLine() {
	line := l.cur()
	for l.col < len(line) && isSpace(line[l.col]) {
		l.col++
	}
}

// moreOnLine reports whether anything but whitespace is left on the
// current line, consuming the whitespace.
func (l *lexer) moreOnLine() bool {
	if l.atEnd() {
		return false
	}
	l.skipSpaceOnLine()
	return l.col < len(l.cur())
}
