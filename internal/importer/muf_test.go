package importer

import (
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

func TestParseMacros(t *testing.T) {
	const in = "no?\n\"{0|n*}\" smatch\n1\n" +
		"yes?\n\"{1|y*}\" smatch\n1\n"

	got, err := ParseMacros(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d macros, want 2", len(got))
	}
	if got[0] != (Macro{Name: "no?", Definition: `"{0|n*}" smatch`, Owner: ref.God}) {
		t.Errorf("first macro = %+v", got[0])
	}
	if got[1].Name != "yes?" {
		t.Errorf("second macro = %+v", got[1])
	}
}

func TestParseMacrosRejectsTruncatedEntries(t *testing.T) {
	for _, in := range []string{
		"name\n",            // no definition
		"name\ndef\n",       // no owner
		"name\ndef\nnope\n", // owner is not a dbref
	} {
		if _, err := ParseMacros(strings.NewReader(in)); err == nil {
			t.Errorf("ParseMacros(%q) should have failed", in)
		}
	}
}

// TestParseShippedMacros reads the macro table Fuzzball ships.
func TestParseShippedMacros(t *testing.T) {
	f := openFixture(t, "../../testdata/starterdb/muf/macros")
	defer f.Close()

	got, err := ParseMacros(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("read %d macros, want 3", len(got))
	}
	for _, m := range got {
		if m.Name == "" || m.Definition == "" {
			t.Errorf("macro %+v is incomplete", m)
		}
	}
}

func TestLoadProgramDir(t *testing.T) {
	got, err := LoadProgramDir("../../testdata/starterdb/muf")
	if err != nil {
		t.Skipf("fixture unavailable: %v", err)
	}
	if len(got) != 61 {
		t.Errorf("read %d programs, want 61", len(got))
	}
	for _, p := range got {
		if p.Source == "" {
			t.Errorf("%v has empty source", p.Ref)
		}
		if !p.Ref.Ok() {
			t.Errorf("%v is not a usable dbref", p.Ref)
		}
	}
	// The macros file sits in the same directory and must not be mistaken
	// for a program.
	for _, p := range got {
		if strings.Contains(p.Source, "smatch\n1\n") && len(p.Source) < 200 {
			t.Errorf("%v looks like the macro table, not a program", p.Ref)
		}
	}
}
