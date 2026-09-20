package muf

import (
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

func TestStrEncryptDecryptRoundTrips(t *testing.T) {
	// Upstream's own scramble table only maps letters and a couple of
	// control characters onto a fixed 97-character "visible ASCII" range —
	// its own doc comment says as much — so only printable text, what
	// STRENCRYPT is actually used for, is expected to round-trip.
	for _, s := range []string{"hello world", "", "a", "The quick brown fox."} {
		enc := strEncrypt(s, "mykey")
		got := strDecrypt(enc, "mykey")
		if got != s {
			t.Errorf("round trip of %q = %q, want original back", s, got)
		}
	}
}

func TestStrDecryptRejectsShortOrMalformedInput(t *testing.T) {
	if got := strDecrypt("", "k"); got != "" {
		t.Errorf("strDecrypt(\"\") = %q, want \"\"", got)
	}
	if got := strDecrypt("x", "k"); got != "" {
		t.Errorf("strDecrypt of a 1-byte string = %q, want \"\"", got)
	}
}

func TestParseSTOD(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"#5", 5},
		{"5", 5},
		{"+5", 5},
		{"-5", -5},
		{"nonsense", -1},
		{"5abc", -1},
		{"", 0},
		{"  #7  ", 7},
	}
	for _, tt := range tests {
		if got := parseSTOD(tt.in); int(got) != tt.want {
			t.Errorf("parseSTOD(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// pronounTestHost stubs just enough of Host for pronounSub: TuneGet (the
// gender_prop name) and GetProp (the gender value itself).
type pronounTestHost struct {
	Host
	gender string
}

func (h *pronounTestHost) TuneGet(string) (string, bool) { return "sex", true }
func (h *pronounTestHost) GetProp(ref.Ref, string) (props.Value, bool) {
	if h.gender == "" {
		return props.Value{}, false
	}
	return props.Value{Type: props.String, Str: h.gender}, true
}

func TestPronounSubUsesDefaultTableByGender(t *testing.T) {
	// No gender set -> unassigned (index 0), every pronoun empty.
	got := pronounSub(&pronounTestHost{}, testPlayer, "%s likes %p stuff.")
	if want := " likes  stuff."; got != want {
		t.Errorf("unassigned gender: got %q, want %q", got, want)
	}

	// A capitalised directive capitalises the substitution's first letter —
	// upstream's own "isupper(prn[1])" rule — a lowercase one leaves it be.
	got = pronounSub(&pronounTestHost{gender: "male"}, testPlayer, "%S saw %o.")
	if want := "He saw him."; got != want {
		t.Errorf("male gender: got %q, want %q", got, want)
	}

	// %% is a literal percent, and an unrecognised directive passes through.
	got = pronounSub(&pronounTestHost{}, testPlayer, "100%% %q")
	if want := "100% %q"; got != want {
		t.Errorf("literal/unknown directives: got %q, want %q", got, want)
	}
}
