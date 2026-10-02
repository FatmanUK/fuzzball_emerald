package ansi

// The output filters, which are queue_ansi's job (interface.c:673).
//
// There are **two** and they are different functions rather than one
// with a flag, which is the thing to know about this file. Strip
// removes colour outright, for a player who has not asked for it.
// Sanitize keeps colour and makes it well-formed, for a player who
// has: it passes only SGR sequences, completes one that was left
// unterminated, and appends a reset so a line cannot leak its colour
// into the next.
//
// Neither is internal/muf's ansiPattern, and that is deliberate. That
// regexp is ANSI_STRIP's golden-tested contract and is narrower than
// either of these: it insists on a '[' and a terminating letter, so
// it leaves "ESC[31" and "ESCX" alone where Strip removes both —
// and it swallows the terminating letter of "ESC[31X" where Strip
// keeps the X. Sharing one implementation between the primitive and
// the output path would make one of them wrong.

import "strings"

// Escape is ESCAPE_CHAR (game.h:60).
const Escape = '\x1b'

// sanitizeLimit is BUFFER_LEN - 5, the number of input bytes
// strip_bad_ansi will look at before it stops (fbstrings.c:945, with
// BUFFER_LEN = MAX_COMMAND_LEN * 4 = 8192). A longer line is
// truncated, which is observable and is reproduced.
const sanitizeLimit = 8192 - 5

// Strip is fbstrings.c:886's strip_ansi: remove every escape
// sequence, leaving the text a client without colour should see.
//
// Three shapes are recognised, and the third is the one that catches
// people out:
//
//   - ESC at the very end of the string is dropped on its own.
//   - ESC followed by anything but '[' drops **both** characters.
//   - ESC '[' then digits and semicolons drops all of it, plus a
//     trailing 'm'. Terminated by anything else, that character is
//     *kept* — so "ESC[31X" leaves "X" behind.
func Strip(s string) string {
	if !strings.ContainsRune(s, Escape) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != Escape {
			b.WriteByte(s[i])
			i++
			continue
		}
		i++
		if i >= len(s) {
			// A lone ESC at the end of the string.
			break
		}
		if s[i] != '[' {
			i++
			continue
		}
		i++
		for i < len(s) && isParam(s[i]) {
			i++
		}
		if i < len(s) && s[i] == 'm' {
			i++
		}
	}
	return b.String()
}

// Sanitize is fbstrings.c:940's strip_bad_ansi: keep SGR colour and
// make it well-formed.
//
// What it passes through is narrower than what Strip removes. An ESC
// that does not begin a '['-sequence is dropped exactly as Strip
// drops it; a '['-sequence is copied, and if it was terminated by
// something other than 'm' an 'm' is inserted **and the terminator is
// copied too** — so "ESC[31X" becomes "ESC[31mX". Once any sequence
// has been seen the result ends with a reset, inserted *before* a
// trailing CRLF if there is one.
//
// Two upstream oddities are reproduced. A sequence truncated at the
// end of the input — "ESC[31" with nothing after it — gets its
// 'm' and then **loses the reset**: upstream writes the input's own
// NUL terminator into the middle of the buffer and appends the reset
// after it, where strlen never reaches. And the scan stops after
// sanitizeLimit bytes, so a very long coloured line is cut.
func Sanitize(s string) string {
	if !strings.ContainsRune(s, Escape) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + len(Reset))

	seen := false
	truncated := false
	i, budget := 0, sanitizeLimit
	for i < len(s) && budget > 0 {
		budget--
		if s[i] != Escape {
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+1 >= len(s) {
			// A lone ESC at the end, dropped.
			i++
			continue
		}
		if s[i+1] != '[' {
			i += 2
			continue
		}
		seen = true
		b.WriteString("\x1b[")
		i += 2
		for i < len(s) && isParam(s[i]) {
			b.WriteByte(s[i])
			i++
		}
		if i >= len(s) {
			// Truncated at the end of the input: the 'm'
			// goes on, and upstream's embedded NUL eats
			// the reset that would have followed.
			b.WriteByte('m')
			truncated = true
			break
		}
		if s[i] != 'm' {
			b.WriteByte('m')
		}
		b.WriteByte(s[i])
		i++
	}

	out := b.String()
	if !seen || truncated {
		return out
	}
	// The reset goes before a trailing CRLF rather than after it.
	// Emerald's descriptors carry a line without its terminator,
	// so this branch is normally inert and is kept because the
	// text is not always the server's own.
	if rest, ok := strings.CutSuffix(out, "\r\n"); ok {
		return rest + Reset + "\r\n"
	}
	return out + Reset
}

// isParam reports whether a byte may appear between "ESC[" and a
// sequence's terminator: digits and semicolons, which is what makes
// these the SGR subset rather than every escape sequence.
func isParam(c byte) bool {
	return c >= '0' && c <= '9' || c == ';'
}
