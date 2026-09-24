package importer

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// openFixture opens a test fixture, skipping the test if it is
// unavailable.
func openFixture(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("fixture unavailable: %v", err)
	}
	return f
}

func parseFile(t *testing.T, path string) (*world.World, *Report) {
	t.Helper()
	f := openFixture(t, path)
	defer f.Close()

	w := world.New()
	rep, err := Parse(f, w)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return w, rep
}

// TestMinimalDatabase reads the two-object database Fuzzball ships,
// which is small enough to assert on completely.
func TestMinimalDatabase(t *testing.T) {
	w, rep := parseFile(t, "../../testdata/minimal.db")

	if rep.Objects != 2 {
		t.Fatalf("read %d objects, want 2", rep.Objects)
	}
	if w.Len() != 2 {
		t.Fatalf("world holds %d objects, want 2", w.Len())
	}
	if w.Top() != 2 {
		t.Errorf("Top() = %v, want #2", w.Top())
	}

	// #0, Room Zero.
	room := w.Get(ref.GlobalEnvironment)
	if room == nil {
		t.Fatal("#0 is missing")
	}
	if room.Name != "Room Zero" {
		t.Errorf("#0 name = %q", room.Name)
	}
	if room.Type() != ref.TypeRoom {
		t.Errorf("#0 type = %v, want room", room.Type())
	}
	if room.Location != ref.Nothing {
		t.Errorf("#0 location = %v, want #-1", room.Location)
	}
	if room.Contents != ref.God {
		t.Errorf("#0 contents = %v, want #1", room.Contents)
	}
	if room.Dropto != ref.Nothing {
		t.Errorf("#0 drop-to = %v, want #-1", room.Dropto)
	}
	if room.Owner != ref.God {
		t.Errorf("#0 owner = %v, want #1", room.Owner)
	}
	if v, ok := room.Props.Get("_/de"); !ok ||
		v.Str != "You are in Room Zero. It's very dark here." {
		t.Errorf("#0 description = %+v", v)
	}

	// #1, One: a wizard with mucker level 3.
	one := w.Get(ref.God)
	if one == nil {
		t.Fatal("#1 is missing")
	}
	if one.Name != "One" {
		t.Errorf("#1 name = %q", one.Name)
	}
	if one.Type() != ref.TypePlayer {
		t.Errorf("#1 type = %v, want player", one.Type())
	}
	if !one.Flags.IsWizard() {
		t.Error("#1 should be a wizard")
	}
	if got := one.Flags.RawMLevel(); got != 3 {
		t.Errorf("#1 mucker level = %d, want 3", got)
	}
	// A player owns itself; the dump does not store an owner for
	// one.
	if one.Owner != ref.God {
		t.Errorf("#1 owner = %v, want itself", one.Owner)
	}
	if one.Location != ref.GlobalEnvironment {
		t.Errorf("#1 location = %v, want #0", one.Location)
	}
	if one.Home != ref.GlobalEnvironment {
		t.Errorf("#1 home = %v, want #0", one.Home)
	}
	if one.UseCount != 2 {
		t.Errorf("#1 use count = %d, want 2", one.UseCount)
	}
	if one.Created.Unix() != 1435685445 {
		t.Errorf("#1 created = %v", one.Created)
	}
	if one.Modified.Unix() != 1435685445 {
		t.Errorf("#1 modified = %v", one.Modified)
	}
	if one.LastUsed.Unix() != 1551890386 {
		t.Errorf("#1 last used = %v", one.LastUsed)
	}

	// The password survives in a form the login path can still
	// verify.
	if !password.Verify(one.PasswordHash, "potrzebie").OK {
		t.Error("#1's documented starter password should verify after import")
	}

	// The player index must be built as objects arrive.
	if got, ok := w.PlayerNamed("one"); !ok || got != ref.God {
		t.Errorf("PlayerNamed(one) = %v, %v", got, ok)
	}
}

func TestMinimalDatabaseParameters(t *testing.T) {
	_, rep := parseFile(t, "../../testdata/minimal.db")

	// 169 parameters: 8 dropped, 2 explicitly set
	// (default_room_parent and player_start), the rest marked as
	// defaults.
	if rep.ParamsDropped != 8 {
		t.Errorf("dropped %d parameters, want 8", rep.ParamsDropped)
	}
	if rep.ParamsSet != 2 {
		t.Errorf("set %d parameters, want 2", rep.ParamsSet)
	}
	if got := rep.ParamsSet + rep.ParamsReset + rep.ParamsDropped; got != 169 {
		t.Errorf("accounted for %d parameters, want 169", got)
	}
	for _, warn := range rep.Warnings {
		if strings.Contains(warn, "unknown parameter") {
			t.Errorf("unexpected unknown parameter: %s", warn)
		}
		if strings.Contains(warn, "default") &&
			strings.Contains(warn, "differs") {
			t.Errorf("our default has drifted from Fuzzball's: %s", warn)
		}
	}
}

// TestStarterDatabase reads the full starter world, which exercises
// every object type, every property type and a blessed property.
func TestStarterDatabase(t *testing.T) {
	w, rep := parseFile(t, "../../testdata/starterdb/starterdb.db")

	if rep.Objects < 100 {
		t.Fatalf("read only %d objects; the starter world is larger", rep.Objects)
	}
	if rep.Properties == 0 {
		t.Fatal("read no properties")
	}
	// The shipped starter database contains exactly one corrupt
	// property: _prefs/ws/doing was set with an embedded newline,
	// so its value spills onto a following line that is not a
	// property at all. Upstream cannot read it either and skips
	// it with a warning to the wizards. Pin the count, so a
	// regression that breaks other properties still fails.
	var propWarnings []string
	for _, warn := range rep.Warnings {
		if strings.Contains(warn, "property") {
			propWarnings = append(propWarnings, warn)
		}
	}
	if len(propWarnings) != 1 {
		t.Errorf("%d properties failed to parse, want the 1 known-corrupt line: %v",
			len(propWarnings), propWarnings)
	} else if !strings.Contains(propWarnings[0], "Doing") {
		t.Errorf("unexpected property warning: %s", propWarnings[0])
	}

	// Every object type should be represented.
	counts := map[ref.ObjType]int{}
	w.Each(func(o *world.Object) bool {
		counts[o.Type()]++
		return true
	})
	for _, ty := range []ref.ObjType{
		ref.TypeRoom, ref.TypeThing, ref.TypeExit, ref.TypePlayer, ref.TypeProgram,
	} {
		if counts[ty] == 0 {
			t.Errorf("no objects of type %v were read", ty)
		}
	}
	t.Logf("object types: %v", counts)

	// The README documents two players.
	for _, name := range []string{"One", "Keeper"} {
		if _, ok := w.PlayerNamed(name); !ok {
			t.Errorf("player %q is missing", name)
		}
	}

	// Every property type should have been seen at least once.
	seen := map[props.Type]int{}
	blessed := 0
	w.Each(func(o *world.Object) bool {
		for _, e := range o.Props.All() {
			seen[e.Value.Type]++
			if e.Value.Blessed {
				blessed++
			}
		}
		return true
	})
	for _, ty := range []props.Type{props.String, props.Int, props.Lock, props.Ref} {
		if seen[ty] == 0 {
			t.Errorf("no %v properties were read", ty)
		}
	}
	if blessed == 0 {
		t.Error("the starter world has a blessed property; none was read")
	}
	t.Logf("property types: %v, blessed: %d", seen, blessed)
}

// TestStarterDatabaseChainsAreConsistent checks that the containment
// chains a real dump carries survive the import intact.
func TestStarterDatabaseChainsAreConsistent(t *testing.T) {
	w, _ := parseFile(t, "../../testdata/starterdb/starterdb.db")

	if n := w.RepairChains(); n != 0 {
		t.Errorf("%d containers in the shipped starter world needed repair; "+
			"either the dump is inconsistent or the importer mangled it", n)
	}
}

func TestParsePropLine(t *testing.T) {
	cases := []struct {
		line    string
		name    string
		want    props.Value
		wantErr bool
	}{
		{"_/de:2:A scroll", "_/de", props.Value{Type: props.String, Str: "A scroll"}, false},
		{"@/value:3:1", "@/value", props.Value{Type: props.Int, Num: 1}, false},
		{"_/lok:4:#0&!#0", "_/lok", props.Value{Type: props.Lock, Str: "#0&!#0"}, false},
		// Dumps store dbrefs as bare integers, with no
		// leading #.
		{"~/prog:5:68", "~/prog", props.Value{Type: props.Ref, Ref: ref.Ref(68)}, false},
		{"w:6:1.5", "w", props.Value{Type: props.Float, Float: 1.5}, false},
		// 4098 is string plus the blessed bit.
		{"_/sc:4098:{null}", "_/sc", props.Value{Type: props.String, Str: "{null}", Blessed: true}, false},
		// A value may contain colons; only the first two
		// split.
		{"p:2:a:b:c", "p", props.Value{Type: props.String, Str: "a:b:c"}, false},
		// An empty string value is legal in the file even
		// though storing it would unset the property.
		{"p:2:", "p", props.Value{Type: props.String, Str: ""}, false},

		{"noflags", "", props.Value{}, true},
		{"p:notanumber:v", "", props.Value{}, true},
		{"p:3:notanint", "", props.Value{}, true},
		{"p:5:notaref", "", props.Value{}, true},
		{"p:1:x", "", props.Value{}, true}, // type 1 is not a property type
	}
	for _, c := range cases {
		name, got, err := parseProp(c.line)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseProp(%q) should have failed", c.line)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseProp(%q): %v", c.line, err)
			continue
		}
		if name != c.name {
			t.Errorf("parseProp(%q) name = %q, want %q", c.line, name, c.name)
		}
		if got != c.want {
			t.Errorf("parseProp(%q) = %+v, want %+v", c.line, got, c.want)
		}
	}
}

func TestParseFloatAcceptsFuzzballSpellings(t *testing.T) {
	cases := map[string]float64{"1.5": 1.5, "-2.25": -2.25, "1e10": 1e10}
	for in, want := range cases {
		got, err := parseFloat(in)
		if err != nil || got != want {
			t.Errorf("parseFloat(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"Inf", "inf", "+INF", "Infinity"} {
		got, err := parseFloat(in)
		if err != nil || got <= 0 {
			t.Errorf("parseFloat(%q) = %v, %v; want +Inf", in, got, err)
		}
	}
	for _, in := range []string{"-Inf", "-inf", "-INF"} {
		got, err := parseFloat(in)
		if err != nil || got >= 0 {
			t.Errorf("parseFloat(%q) = %v, %v; want -Inf", in, got, err)
		}
	}
	if _, err := parseFloat("banana"); err == nil {
		t.Error("parseFloat(banana) should fail")
	}
}

func TestRejectsWrongFormat(t *testing.T) {
	_, err := Parse(strings.NewReader("***Foxen8 TinyMUCK DUMP Format***\n"), world.New())
	if err == nil ||
		!strings.Contains(err.Error(), "unsupported dump format") {
		t.Errorf("err = %v, want a complaint about the format", err)
	}
	if _, err := Parse(strings.NewReader(""), world.New()); err == nil {
		t.Error("an empty file should be rejected")
	}
}

func TestRejectsTruncatedDump(t *testing.T) {
	dump := VersionString + "\n1\n0\n0\n#0\nRoom\n-1\n-1\n-1\n0\n0\n0\n0\n0\n-1\n-1\n1\n"
	// No ***END OF DUMP*** line.
	if _, err := Parse(strings.NewReader(dump), world.New()); err == nil {
		t.Error("a dump with no end marker should be rejected")
	}
}

func TestObjectWithNoProperties(t *testing.T) {
	// When an object has no properties, the type-specific fields
	// follow the timestamps directly, with no *Props* block. This
	// is the branch upstream distinguishes by peeking one
	// character.
	dump := VersionString + "\n1\n0\n0\n" +
		"#0\nRoom Zero\n-1\n-1\n-1\n0\n100\n200\n3\n400\n" +
		"-1\n-1\n1\n" + // drop-to, exits, owner
		endOfDump + "\n"

	w := world.New()
	rep, err := Parse(strings.NewReader(dump), w)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Objects != 1 || rep.Properties != 0 {
		t.Fatalf("read %d objects and %d properties, want 1 and 0",
			rep.Objects, rep.Properties)
	}
	o := w.Get(ref.GlobalEnvironment)
	if o.Name != "Room Zero" || o.Owner != ref.God ||
		o.Dropto != ref.Nothing {
		t.Errorf("object = %+v", o)
	}
	if o.UseCount != 3 || o.Created.Unix() != 100 ||
		o.LastUsed.Unix() != 200 {
		t.Errorf("timestamps or use count wrong: %+v", o)
	}
}

// TestInvalidUTF8PropertyIsSanitized checks that a property whose
// bytes are not valid UTF-8 — seen in a real Dreamtrack dump, where
// a debug library had stored a raw struct in a string property — is
// sanitized rather than aborting the whole import or being written to
// Postgres as-is, which a TEXT column refuses.
func TestInvalidUTF8PropertyIsSanitized(t *testing.T) {
	bad := "\xe0\x8f\x1e\x0c\xff\x7f"
	dump := VersionString + "\n1\n0\n0\n" +
		"#0\nRoom Zero\n-1\n-1\n-1\n0\n100\n200\n3\n400\n" +
		"*Props*\n" +
		".debug/lasterr:2:" + bad + "\n" +
		"*End*\n" +
		"-1\n-1\n1\n" + endOfDump + "\n"

	w := world.New()
	rep, err := Parse(strings.NewReader(dump), w)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Properties != 1 {
		t.Fatalf("read %d properties, want 1", rep.Properties)
	}
	found := false
	for _, warn := range rep.Warnings {
		if strings.Contains(warn, "not valid UTF-8") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want one mentioning invalid UTF-8", rep.Warnings)
	}

	v, ok := w.Get(ref.GlobalEnvironment).Props.Get(".debug/lasterr")
	if !ok {
		t.Fatal("the property should still be set, just sanitized")
	}
	if !utf8.ValidString(v.Str) {
		t.Errorf("stored value %q is still not valid UTF-8", v.Str)
	}
}

func TestDumpMaskFlagsAreCleared(t *testing.T) {
	// A dump should never carry live-state flags, but if it does
	// they must not survive the import.
	flags := uint32(ref.TypeRoom) | uint32(ref.Interactive) | uint32(ref.ObjectChanged) |
		uint32(ref.Listener) | uint32(ref.ReadMode) | uint32(ref.SaneBit) | uint32(ref.Dark)
	dump := VersionString + "\n1\n0\n0\n" +
		"#0\nRoom\n-1\n-1\n-1\n" + strconv.FormatUint(uint64(flags), 10) + "\n0\n0\n0\n0\n" +
		"-1\n-1\n1\n" + endOfDump + "\n"

	w := world.New()
	if _, err := Parse(strings.NewReader(dump), w); err != nil {
		t.Fatal(err)
	}
	got := w.Get(ref.GlobalEnvironment).Flags
	if got&ref.DumpMask != 0 {
		t.Errorf("flags = %#x, live-state bits should have been cleared", uint32(got))
	}
	if got&ref.Dark == 0 {
		t.Error("clearing live-state flags removed a real one")
	}
}

// TestUnstoredLinksDefaultToNothing guards the importer against Go's
// zero value leaking in as #0.
//
// A dump stores `exits` only for things, players and rooms, and an
// owner only for some types. Any field the record does not carry must
// end up as NOTHING, because the zero value for a ref is #0 — the
// global environment — and defaulting to it silently attaches
// objects to the world root.
func TestUnstoredLinksDefaultToNothing(t *testing.T) {
	// A program: the dump gives it an owner and nothing else.
	dump := VersionString + "\n1\n0\n0\n" +
		"#7\nlib-props\n2\n-1\n-1\n" + strconv.Itoa(int(ref.TypeProgram)) +
		"\n100\n200\n0\n300\n" +
		"2\n" + // owner, in the no-properties slot
		endOfDump + "\n"

	w := world.New()
	if _, err := Parse(strings.NewReader(dump), w); err != nil {
		t.Fatal(err)
	}
	o := w.Get(ref.Ref(7))
	if o == nil {
		t.Fatal("#7 is missing")
	}
	if o.Owner != ref.Ref(2) {
		t.Errorf("owner = %v, want #2", o.Owner)
	}
	for _, f := range []struct {
		name string
		got  ref.Ref
	}{
		{"exits", o.Exits},
		{"home", o.Home},
		{"drop-to", o.Dropto},
	} {
		if f.got != ref.Nothing {
			t.Errorf("a program's %s = %v, want #-1; the dump does not store it, "+
				"so it must not default to the global environment",
				f.name, f.got)
		}
	}
}

// TestGlobalEnvironmentSurvivesImport checks that #0 comes back as
// the world root it is in the shipped databases, rather than being
// confused with a sentinel.
func TestGlobalEnvironmentSurvivesImport(t *testing.T) {
	for _, path := range []string{
		"../../testdata/minimal.db",
		"../../testdata/starterdb/starterdb.db",
	} {
		w, _ := parseFile(t, path)
		root := w.Get(ref.GlobalEnvironment)
		if root == nil {
			t.Errorf("%s: #0 is missing", path)
			continue
		}
		if root.Type() != ref.TypeRoom {
			t.Errorf("%s: #0 is a %v, want a room", path, root.Type())
		}
		if root.Name != "Room Zero" {
			t.Errorf("%s: #0 is named %q, want Room Zero", path, root.Name)
		}
		// The world root sits in no container; that is what
		// makes it the root, and it must not be confused with
		// #0 meaning "unset".
		if root.Location != ref.Nothing {
			t.Errorf("%s: #0 location = %v, want #-1", path, root.Location)
		}
		// New rooms parent to default_room_parent, which is
		// #0 in the minimal world and the Null Environment
		// Room (#109) in the starter one. Either way it must
		// be a real room whose own containment chain reaches
		// #0, since that is what makes #0 the root of the
		// world rather than just another object.
		parent := w.Tune.Ref("default_room_parent")
		po := w.Get(parent)
		if po == nil {
			t.Errorf("%s: default_room_parent %v does not exist", path, parent)
			continue
		}
		if po.Type() != ref.TypeRoom {
			t.Errorf("%s: default_room_parent %v is a %v, want a room",
				path, parent, po.Type())
		}
		if !reachesRoot(w, parent) {
			t.Errorf("%s: default_room_parent %v does not sit under #0",
				path, parent)
		}
	}
}

// reachesRoot reports whether r is #0 or is contained, at any depth,
// by #0.
func reachesRoot(w *world.World, r ref.Ref) bool {
	for i := 0; i < w.Len()+1; i++ {
		if r == ref.GlobalEnvironment {
			return true
		}
		o := w.Get(r)
		if o == nil || o.Location == ref.Nothing {
			return false
		}
		r = o.Location
	}
	return false
}
