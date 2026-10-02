package ansi

import (
	"strings"
	"testing"
)

// The expectations here were not derived from reading the C: they
// were produced by compiling strip_ansi and strip_bad_ansi out of
// fbstrings.c and running them over these twelve inputs. Several are
// not what the code reads like it does.
func TestStripAndSanitize(t *testing.T) {
	const e = "\x1b"
	for _, tc := range []struct {
		in, strip, sanitize string
		why                 string
	}{
		{in: "plain", strip: "plain", sanitize: "plain"},

		{in: e + "[31mred" + e + "[0m",
			strip: "red",
			sanitize: e + "[31mred" + e + "[0m" +
				e + "[0m",
			why: "the reset is appended always, so " +
				"text ending in one gets two"},

		{in: "a" + e + "[1;32mb",
			strip:    "ab",
			sanitize: "a" + e + "[1;32mb" + e + "[0m"},

		{in: "a" + e + "[31",
			strip: "a",
			// Truncated at the end of the input: the 'm'
			// goes on and the reset is lost, because
			// upstream writes the input's NUL into the
			// middle of the buffer.
			sanitize: "a" + e + "[31m",
			why:      "a truncated run loses the reset"},

		{in: "a" + e + "X b",
			strip:    "a b",
			sanitize: "a b",
			why: "ESC then anything but '[' drops " +
				"both, in both filters"},

		{in: "a" + e, strip: "a", sanitize: "a",
			why: "a lone ESC at the end drops alone"},

		{in: "a" + e + "[31Xb",
			strip: "aXb",
			// The 'm' is inserted *and* the terminator is
			// copied, so the X survives in both.
			sanitize: "a" + e + "[31mXb" + e + "[0m",
			why: "a sequence ended by something other " +
				"than 'm' keeps that character"},

		{in: "a" + e + "[Xb",
			strip:    "aXb",
			sanitize: "a" + e + "[mXb" + e + "[0m"},

		{in: "a" + e + "[mb",
			strip:    "ab",
			sanitize: "a" + e + "[mb" + e + "[0m"},

		{in: e + "[31mred\r\n",
			strip:    "red\r\n",
			sanitize: e + "[31mred" + e + "[0m\r\n",
			why:      "the reset precedes a CRLF"},

		{in: e + "[31", strip: "", sanitize: e + "[31m"},

		{in: "no colour\r\n", strip: "no colour\r\n",
			sanitize: "no colour\r\n",
			why:      "no sequence seen, so no reset"},
	} {
		if got := Strip(tc.in); got != tc.strip {
			t.Errorf("Strip(%q) = %q, want %q%s",
				tc.in, got, tc.strip, because(tc.why))
		}
		if got := Sanitize(tc.in); got != tc.sanitize {
			t.Errorf("Sanitize(%q) = %q, want %q%s",
				tc.in, got, tc.sanitize,
				because(tc.why))
		}
	}
}

func because(why string) string {
	if why == "" {
		return ""
	}
	return " (" + why + ")"
}

// TestSanitizeTruncatesALongLine covers the BUFFER_LEN - 5 bound,
// which is a real limit rather than an implementation detail: a
// coloured line past it is cut.
func TestSanitizeTruncatesALongLine(t *testing.T) {
	long := "\x1b[31m" + strings.Repeat("x", sanitizeLimit+100)
	got := Sanitize(long)
	// Five bytes of sequence plus the bytes the budget allowed,
	// which the sequence itself consumed one of per character.
	if len(got) >= len(long) {
		t.Errorf("a %d-byte line came back %d bytes; "+
			"no limit applied", len(long), len(got))
	}
	if !strings.HasSuffix(got, Reset) {
		t.Error("a truncated line lost its reset")
	}

	// Strip has no such limit — its own comment warns the
	// caller to size the buffer instead.
	if got := Strip(long); len(got) != sanitizeLimit+100 {
		t.Errorf("Strip truncated: %d bytes, want %d",
			len(got), sanitizeLimit+100)
	}
}

// TestNoEscapeIsUntouched pins the fast path, which matters because
// every line of ordinary output goes through it.
func TestNoEscapeIsUntouched(t *testing.T) {
	for _, s := range []string{
		"", "plain text", "a[31mb", "100%",
	} {
		if got := Strip(s); got != s {
			t.Errorf("Strip(%q) = %q", s, got)
		}
		if got := Sanitize(s); got != s {
			t.Errorf("Sanitize(%q) = %q", s, got)
		}
	}
}
