package ref

import "testing"

func TestRefString(t *testing.T) {
	cases := map[Ref]string{
		Nothing:           "#-1",
		Ambiguous:         "#-2",
		Home:              "#-3",
		Nil:               "#-4",
		GlobalEnvironment: "#0", God: "#1", Ref(2546): "#2546",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("Ref(%d).String() = %q, want %q", int(in), got, want)
		}
	}
}

func TestParse(t *testing.T) {
	for _, in := range []string{"#12", "12", " #12 "} {
		got, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		if got != 12 {
			t.Errorf("Parse(%q) = %v, want #12", in, got)
		}
	}
	if _, err := Parse("#frog"); err == nil {
		t.Error("Parse(\"#frog\") should fail")
	}
}

func TestMLevel(t *testing.T) {
	cases := []struct {
		flags    Flags
		raw, eff int
	}{
		{0, 0, 0},
		{SMucker, 1, 1},
		{Mucker, 2, 2},
		{Mucker | SMucker, 3, 3},
		// A wizard with any mucker bit is level 4, but the
		// raw level is still just the bits.
		{Wizard | SMucker, 1, MLevWizard},
		{Wizard | Mucker | SMucker, 3, MLevWizard},
		// A wizard with no mucker bits gets no mucker level
		// at all.
		{Wizard, 0, 0},
	}
	for _, c := range cases {
		if got := c.flags.RawMLevel(); got != c.raw {
			t.Errorf("Flags(%#x).RawMLevel() = %d, want %d", uint32(c.flags), got, c.raw)
		}
		if got := c.flags.MLevel(); got != c.eff {
			t.Errorf("Flags(%#x).MLevel() = %d, want %d", uint32(c.flags), got, c.eff)
		}
	}
}

func TestSetMLevelRoundTrips(t *testing.T) {
	for lvl := 0; lvl <= 3; lvl++ {
		f := Flags(0).SetMLevel(lvl)
		if got := f.RawMLevel(); got != lvl {
			t.Errorf("SetMLevel(%d) then RawMLevel() = %d", lvl, got)
		}
	}
	// Setting a level must clear whatever was there before.
	f := (Mucker | SMucker).SetMLevel(0)
	if f.RawMLevel() != 0 {
		t.Errorf("SetMLevel(0) left mucker bits: %#x", uint32(f))
	}
}

func TestWizardRespectsQuell(t *testing.T) {
	if !(Wizard).IsWizard() {
		t.Error("a plain wizard should be a wizard")
	}
	if (Wizard | Quell).IsWizard() {
		t.Error("a quelled wizard should not have wizard powers")
	}
	if !(Wizard | Quell).IsTrueWizard() {
		t.Error("a quelled wizard is still a true wizard")
	}
}

func TestTypeRoundTrips(t *testing.T) {
	for _, ty := range []ObjType{TypeRoom, TypeThing, TypeExit, TypePlayer, TypeProgram, TypeGarbage} {
		f := (Wizard | Dark).WithType(ty)
		if got := f.Type(); got != ty {
			t.Errorf("WithType(%v).Type() = %v", ty, got)
		}
		if f&(Wizard|Dark) != Wizard|Dark {
			t.Errorf("WithType(%v) clobbered unrelated flags: %#x", ty, uint32(f))
		}
	}
}

func TestUnparse(t *testing.T) {
	cases := []struct {
		flags Flags
		want  string
	}{
		{Flags(TypeRoom), "R"},
		{Flags(TypeThing), ""},
		{Flags(TypeExit), "E"},
		{Flags(TypePlayer), "P"},
		{Flags(TypeProgram), "F"},
		{Flags(TypeGarbage), "G"},
		{Flags(TypePlayer) | Wizard | Mucker | SMucker, "PWM3"},
		{Flags(TypeRoom) | Dark | JumpOK | Abode, "RDJA"},
		// Internal flags never show up.
		{Flags(TypeThing) | ObjectChanged | Listener, ""},
	}
	for _, c := range cases {
		if got := c.flags.Unparse(); got != c.want {
			t.Errorf("Flags(%#x).Unparse() = %q, want %q", uint32(c.flags), got, c.want)
		}
	}
}

func TestDumpMaskCoversInternalFlags(t *testing.T) {
	for _, f := range []Flags{Interactive, ObjectChanged, Listener, ReadMode, SaneBit} {
		if DumpMask&f == 0 {
			t.Errorf("internal flag %#x missing from DumpMask", uint32(f))
		}
	}
}

// TestSpecialRefsMatchFuzzball pins the reserved dbrefs to the values
// in Fuzzball's include/db.h. They appear in every legacy dump, so
// changing one would silently corrupt an imported world: #0 is the
// room every other room ultimately parents to, and the negatives are
// sentinels rather than objects.
func TestSpecialRefsMatchFuzzball(t *testing.T) {
	cases := []struct {
		name string
		got  Ref
		want int32
	}{
		{"GLOBAL_ENVIRONMENT", GlobalEnvironment, 0},
		{"GOD", God, 1},
		{"NOTHING", Nothing, -1},
		{"AMBIGUOUS", Ambiguous, -2},
		{"HOME", Home, -3},
		{"NIL", Nil, -4},
	}
	for _, c := range cases {
		if int32(c.got) != c.want {
			t.Errorf("%s = %d, want %d", c.name, int32(c.got), c.want)
		}
	}

	// #0 is a real object, not a sentinel. Ok() must agree, or
	// the world root would be treated as absent.
	if !GlobalEnvironment.Ok() {
		t.Error("#0 is the global environment, a real object")
	}
	if !God.Ok() {
		t.Error("#1 is a real object")
	}
	for _, r := range []Ref{Nothing, Ambiguous, Home, Nil} {
		if r.Ok() {
			t.Errorf("%v is a sentinel, not an object", r)
		}
	}

	// Go's zero value for a Ref is #0, the global environment,
	// not Nothing. Any struct holding refs must therefore
	// initialise them explicitly; forgetting to attaches objects
	// to the world root. This is exactly what went wrong in the
	// importer for programs, whose exit list a dump does not
	// store.
	var zero Ref
	if zero != GlobalEnvironment {
		t.Fatal("the zero value is expected to be #0; this test is the warning that it is")
	}
	if zero == Nothing {
		t.Error("the zero value must not be Nothing")
	}
}
