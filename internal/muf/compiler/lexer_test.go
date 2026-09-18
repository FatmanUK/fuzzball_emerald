package compiler

import (
	"strings"
	"testing"
)

// lexAll returns every token's text, or the error that stopped lexing.
func lexAll(t *testing.T, src string) ([]string, error) {
	t.Helper()
	l := newLexer(src)
	var out []string
	for {
		tok, ok, err := l.next()
		if err != nil {
			return out, err
		}
		if !ok {
			return out, nil
		}
		out = append(out, tok.text)
	}
}

func mustLex(t *testing.T, src string) []string {
	t.Helper()
	got, err := lexAll(t, src)
	if err != nil {
		t.Fatalf("lexing %q: %v", src, err)
	}
	return got
}

func TestLexWords(t *testing.T) {
	got := mustLex(t, "me @ 1 + .tell")
	want := []string{"me", "@", "1", "+", ".tell"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLexAcrossLines(t *testing.T) {
	got := mustLex(t, ": main\n  \"hi\" .tell\n;\n")
	want := []string{":", "main", "hi", ".tell", ";"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLexLineNumbers(t *testing.T) {
	l := newLexer("a\n\nb\n  c")
	want := []int{1, 3, 4}
	for i, wantLine := range want {
		tok, ok, err := l.next()
		if err != nil || !ok {
			t.Fatalf("token %d: %v", i, err)
		}
		if tok.line != wantLine {
			t.Errorf("token %q is on line %d, want %d", tok.text, tok.line, wantLine)
		}
	}
}

func TestLexStrings(t *testing.T) {
	got := mustLex(t, `"hello world" "" "with \"quotes\""`)
	want := []string{"hello world", "", `with "quotes"`}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLexStringEscapes(t *testing.T) {
	// \r is a carriage return, which MUCK uses as the in-string line
	// separator; \[ is the ANSI escape; anything else is itself.
	got := mustLex(t, `"a\rb" "\[[0m" "\q"`)
	if got[0] != "a\rb" {
		t.Errorf("\\r = %q, want a carriage return", got[0])
	}
	if got[1] != "\x1b[0m" {
		t.Errorf("\\[ = %q, want an escape character", got[1])
	}
	if got[2] != "q" {
		t.Errorf("\\q = %q, want q", got[2])
	}
}

func TestStringsAreMarked(t *testing.T) {
	// A quoted literal that looks like a number is still a string.
	l := newLexer(`"123" 123`)
	first, _, _ := l.next()
	second, _, _ := l.next()
	if !first.isString {
		t.Error("a quoted literal should be marked as a string")
	}
	if second.isString {
		t.Error("a bare number should not be marked as a string")
	}
}

func TestUnterminatedStringIsAnError(t *testing.T) {
	// A string may not span lines.
	if _, err := lexAll(t, "\"no end\nnext line"); err == nil {
		t.Error("an unterminated string should be an error")
	}
}

func TestLexComments(t *testing.T) {
	got := mustLex(t, "a ( this is ignored ) b")
	if strings.Join(got, "|") != "a|b" {
		t.Errorf("got %v, want [a b]", got)
	}
}

func TestCommentsSpanLines(t *testing.T) {
	got := mustLex(t, "a (\n  still a comment\n) b")
	if strings.Join(got, "|") != "a|b" {
		t.Errorf("got %v, want [a b]", got)
	}
}

func TestCommentsNest(t *testing.T) {
	got := mustLex(t, "a ( outer ( inner ) still outer ) b")
	if strings.Join(got, "|") != "a|b" {
		t.Errorf("got %v, want [a b]", got)
	}
}

// TestUnbalancedCommentFallsBackToFlat covers the behaviour old MUF depends
// on: a comment containing an unmatched '(' does not nest cleanly, and
// upstream retries it as a flat comment ending at the first ')'.
func TestUnbalancedCommentFallsBackToFlat(t *testing.T) {
	got := mustLex(t, "a ( a smiley :-( in a comment ) b")
	if strings.Join(got, "|") != "a|b" {
		t.Errorf("got %v, want [a b]: the flat fallback should have handled it", got)
	}
}

func TestCommentNestingIsBounded(t *testing.T) {
	// Eight levels exceeds the limit, so the flat fallback takes over and
	// the comment ends at the first ')'.
	src := "a " + strings.Repeat("(", 8) + " x " + strings.Repeat(")", 8) + " b"
	got := mustLex(t, src)
	// The flat fallback stops at the first ')', so the trailing parens
	// become tokens. What matters is that lexing terminates and does not
	// error.
	if len(got) == 0 || got[0] != "a" {
		t.Errorf("got %v, want it to start with a", got)
	}
}

func TestUnterminatedCommentIsAnError(t *testing.T) {
	if _, err := lexAll(t, "a ( never closed"); err == nil {
		t.Error("an unterminated comment should be an error")
	}
}

func TestCarriageReturnsAreNormalised(t *testing.T) {
	// Source written on another platform must lex identically.
	for _, src := range []string{"a\nb", "a\r\nb", "a\rb"} {
		got := mustLex(t, src)
		if strings.Join(got, "|") != "a|b" {
			t.Errorf("%q lexed to %v", src, got)
		}
	}
}
