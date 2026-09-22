package muf

import (
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// fmtTestHost answers the one question the '%D' directive asks.
type fmtTestHost struct {
	Host
}

func (h *fmtTestHost) Valid(r ref.Ref) bool  { return r == ref.Ref(7) }
func (h *fmtTestHost) Name(r ref.Ref) string { return "Rusty Key" }

func fmtOne(t *testing.T, format string, args ...Value) string {
	t.Helper()
	i := 0
	d := fmtDialect{arg: func(string, byte) (Value, error) {
		v := args[i]
		i++
		return v, nil
	}}
	got, err := formatWith(&fmtTestHost{}, format, d)
	if err != nil {
		t.Fatalf("formatWith(%q) failed: %v", format, err)
	}
	return got
}

func TestFormatDirectives(t *testing.T) {
	tests := []struct {
		format string
		args   []Value
		want   string
	}{
		{"%i", []Value{Int(42)}, "42"},
		{"%5i", []Value{Int(42)}, "   42"},
		{"%-5i|", []Value{Int(42)}, "42   |"},
		{"%05i", []Value{Int(42)}, "00042"},
		{"%+i", []Value{Int(42)}, "+42"},
		{"% i", []Value{Int(42)}, " 42"},

		{"%s", []Value{Str("abc")}, "abc"},
		{"%5s", []Value{Str("abc")}, "  abc"},
		{"%-5s|", []Value{Str("abc")}, "abc  |"},
		{"%.2s", []Value{Str("abcdef")}, "ab"},
		// C ignores the zero flag on a string; Go would pad with zeros.
		{"%05s", []Value{Str("abc")}, "  abc"},

		// A dbref prints as "#123" under %d and as its name under %D.
		{"%d", []Value{Obj(ref.Ref(7))}, "#7"},
		{"%D", []Value{Obj(ref.Ref(7))}, "Rusty Key"},

		{"%f", []Value{Float(1.5)}, "1.500000"},
		{"%.2f", []Value{Float(1.5)}, "1.50"},
		{"%g", []Value{Float(1.5)}, "1.5"},

		// '~' picks the verb from the value's own type.
		{"%~", []Value{Int(3)}, "3"},
		{"%~", []Value{Str("hi")}, "hi"},
		{"%~", []Value{Obj(ref.Ref(7))}, "#7"},
		// '?' names the type instead of printing the value.
		{"%?", []Value{Float(1)}, "FLOAT"},

		{"100%% done", nil, "100% done"},
		// The width counts visible characters, so an ANSI escape does not
		// eat into the padding.
		{"%6s|", []Value{Str("\x1b[1mab\x1b[0m")}, "    \x1b[1mab\x1b[0m|"},
	}
	for _, tt := range tests {
		if got := fmtOne(t, tt.format, tt.args...); got != tt.want {
			t.Errorf("format %q = %q, want %q", tt.format, got, tt.want)
		}
	}
}

func TestFormatRejectsWrongTypes(t *testing.T) {
	for _, tt := range []struct {
		format string
		arg    Value
	}{
		{"%i", Str("nope")},
		{"%s", Int(1)},
		{"%d", Str("nope")},
		{"%f", Int(1)},
		{"%q", Int(1)},
	} {
		d := fmtDialect{arg: func(string, byte) (Value, error) { return tt.arg, nil }}
		if _, err := formatWith(&fmtTestHost{}, tt.format, d); err == nil {
			t.Errorf("format %q with %v: expected an error", tt.format, tt.arg)
		}
	}
}

func TestRowDialectFillsMissingFieldsByVerb(t *testing.T) {
	row := NewDict()
	row.Set(Str("name"), Str("Rusty"))

	got, err := formatWith(nil, "%[name]s/%[count]i/%[missing]s", rowDialect(row))
	if err != nil {
		t.Fatalf("formatWith failed: %v", err)
	}
	if want := "Rusty/0/"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRowDialectRequiresAField(t *testing.T) {
	if _, err := formatWith(nil, "%s", rowDialect(NewDict())); err == nil {
		t.Error("a directive with no [field] should be refused")
	}
}
