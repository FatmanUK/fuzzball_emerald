package match

import (
	"strconv"
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

func TestStringMatchOnWordBoundaries(t *testing.T) {
	cases := []struct {
		src, sub string
		want     bool
	}{
		{"a rusty key", "rusty", true},
		{"a rusty key", "key", true},
		{"a rusty key", "a", true},
		{"a rusty key", "RUSTY", true}, // case-insensitive
		{"a rusty key", "rust", true},  // prefix of a word
		{"a rusty key", "usty", false}, // not at a word boundary
		{"a rusty key", "keys", false}, // longer than the word
		{"a rusty key", "", false},
		{"", "x", false},
	}
	for _, c := range cases {
		if got := StringMatch(c.src, c.sub); got != c.want {
			t.Errorf("StringMatch(%q, %q) = %v, want %v", c.src, c.sub, got, c.want)
		}
	}
}

// fixture builds a small world: a room holding a player, two things and an
// exit, with the player carrying one thing.
type fixture struct {
	w                          *world.World
	room, player, key, box, ex ref.Ref
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	w := world.New()
	w.SetClock(func() time.Time { return time.Unix(1_700_000_000, 0).UTC() })

	room := w.Create("The Study", ref.TypeRoom, ref.God)
	player := w.Create("Igor", ref.TypePlayer, ref.God)
	key := w.Create("a rusty key", ref.TypeThing, player.Ref)
	box := w.Create("a wooden box", ref.TypeThing, player.Ref)
	ex := w.Create("north;n;nor", ref.TypeExit, player.Ref)

	for _, r := range []ref.Ref{player.Ref, box.Ref, ex.Ref} {
		if err := w.MoveTo(r, room.Ref); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.MoveTo(key.Ref, player.Ref); err != nil {
		t.Fatal(err)
	}
	return &fixture{w: w, room: room.Ref, player: player.Ref,
		key: key.Ref, box: box.Ref, ex: ex.Ref}
}

func TestMeAndHere(t *testing.T) {
	f := newFixture(t)
	if got := New(f.w, f.player, "me").Everything().Result(); got != f.player {
		t.Errorf("me = %v, want %v", got, f.player)
	}
	if got := New(f.w, f.player, "ME").Everything().Result(); got != f.player {
		t.Errorf("ME should match me, got %v", got)
	}
	if got := New(f.w, f.player, "here").Everything().Result(); got != f.room {
		t.Errorf("here = %v, want %v", got, f.room)
	}
	if got := New(f.w, f.player, "home").Home().Result(); got != ref.Home {
		t.Errorf("home = %v, want #-3", got)
	}
	if got := New(f.w, f.player, "nil").Nil().Result(); got != ref.Nil {
		t.Errorf("nil = %v, want #-4", got)
	}
}

func TestMatchPossessionAndNeighbor(t *testing.T) {
	f := newFixture(t)
	// The key is carried, the box is in the room; both should be reachable.
	if got := New(f.w, f.player, "rusty").Everything().Result(); got != f.key {
		t.Errorf("rusty = %v, want the carried key %v", got, f.key)
	}
	if got := New(f.w, f.player, "wooden").Everything().Result(); got != f.box {
		t.Errorf("wooden = %v, want the box in the room %v", got, f.box)
	}
	if got := New(f.w, f.player, "nonesuch").Everything().Result(); got != ref.Nothing {
		t.Errorf("nonesuch = %v, want #-1", got)
	}
}

func TestExactNameBeatsPartial(t *testing.T) {
	f := newFixture(t)
	w := f.w
	// Two things whose names share a prefix; the exact one must win rather
	// than the pair being called ambiguous.
	sword := w.Create("sword", ref.TypeThing, f.player)
	if err := w.MoveTo(sword.Ref, f.room); err != nil {
		t.Fatal(err)
	}
	fancy := w.Create("sword of destiny", ref.TypeThing, f.player)
	if err := w.MoveTo(fancy.Ref, f.room); err != nil {
		t.Fatal(err)
	}

	if got := New(w, f.player, "sword").Everything().Result(); got != sword.Ref {
		t.Errorf("sword = %v, want the exact match %v", got, sword.Ref)
	}
	if got := New(w, f.player, "destiny").Everything().Result(); got != fancy.Ref {
		t.Errorf("destiny = %v, want %v", got, fancy.Ref)
	}
}

func TestAmbiguousMatch(t *testing.T) {
	f := newFixture(t)
	w := f.w
	for _, name := range []string{"red ball", "red cube"} {
		o := w.Create(name, ref.TypeThing, f.player)
		if err := w.MoveTo(o.Ref, f.room); err != nil {
			t.Fatal(err)
		}
	}
	if got := New(w, f.player, "red").Everything().Result(); got != ref.Ambiguous {
		t.Errorf("red = %v, want #-2", got)
	}
}

func TestExitAliases(t *testing.T) {
	f := newFixture(t)
	for _, alias := range []string{"north", "n", "nor", "NORTH", "N"} {
		if got := New(f.w, f.player, alias).Everything().Result(); got != f.ex {
			t.Errorf("%q = %v, want the exit %v", alias, got, f.ex)
		}
	}
	// A partial alias is not an exit match; exits match whole aliases.
	if got := New(f.w, f.player, "nort").Everything().Result(); got == f.ex {
		t.Error("a partial alias should not match an exit")
	}
}

func TestExitsFoundThroughEnvironment(t *testing.T) {
	f := newFixture(t)
	w := f.w
	// A global exit attached to the parent room must be reachable from a
	// child room, which is how $-commands and global exits work.
	outer := w.Create("Global Environment", ref.TypeRoom, ref.God)
	if err := w.MoveTo(f.room, outer.Ref); err != nil {
		t.Fatal(err)
	}
	global := w.Create("shout", ref.TypeExit, ref.God)
	if err := w.MoveTo(global.Ref, outer.Ref); err != nil {
		t.Fatal(err)
	}

	if got := New(w, f.player, "shout").Everything().Result(); got != global.Ref {
		t.Errorf("shout = %v, want the environment exit %v", got, global.Ref)
	}
	// The nearer exit still works.
	if got := New(w, f.player, "north").Everything().Result(); got != f.ex {
		t.Errorf("north = %v, want the local exit %v", got, f.ex)
	}
}

func TestExitPriorityBeatsProximity(t *testing.T) {
	f := newFixture(t)
	w := f.w
	outer := w.Create("Global Environment", ref.TypeRoom, ref.God)
	if err := w.MoveTo(f.room, outer.Ref); err != nil {
		t.Fatal(err)
	}

	// Two exits with the same name: one nearby at default priority, one
	// further out with mucker bits set, which raise its priority. The
	// higher-priority exit wins even though it is further away.
	near := w.Create("teleport", ref.TypeExit, ref.God)
	if err := w.MoveTo(near.Ref, f.room); err != nil {
		t.Fatal(err)
	}
	far := w.Create("teleport", ref.TypeExit, ref.God)
	far.Flags = far.Flags.SetMLevel(3)
	if err := w.MoveTo(far.Ref, outer.Ref); err != nil {
		t.Fatal(err)
	}

	if got := New(w, f.player, "teleport").Everything().Result(); got != far.Ref {
		t.Errorf("teleport = %v, want the higher-priority exit %v", got, far.Ref)
	}
}

func TestPriority(t *testing.T) {
	cases := []struct {
		flags ref.Flags
		want  int
	}{
		{0, 1},                         // the default
		{ref.Abode, 0},                 // binds more weakly
		{ref.Flags(0).SetMLevel(1), 2}, // mucker bits raise it
		{ref.Flags(0).SetMLevel(2), 3},
		{ref.Flags(0).SetMLevel(3), 4},
	}
	for _, c := range cases {
		if got := priority(c.flags); got != c.want {
			t.Errorf("priority(%#x) = %d, want %d", uint32(c.flags), got, c.want)
		}
	}
}

func TestAbsoluteRefRequiresControl(t *testing.T) {
	f := newFixture(t)
	w := f.w

	// The player owns the key, so may name it directly.
	if got := New(w, f.player, f.key.String()).Everything().Result(); got != f.key {
		t.Errorf("%v = %v, want the key", f.key, got)
	}

	// Something owned by someone else is not nameable by dbref.
	other := w.Create("Someone Else", ref.TypePlayer, ref.God)
	secret := w.Create("a secret", ref.TypeThing, other.Ref)
	if err := w.MoveTo(secret.Ref, f.room); err != nil {
		t.Fatal(err)
	}
	if got := New(w, f.player, secret.Ref.String()).Absolute().Result(); got != ref.Nothing {
		t.Errorf("%v = %v, want #-1 for an object the player does not control",
			secret.Ref, got)
	}

	// A wizard may name anything.
	wiz := w.Create("Wizard", ref.TypePlayer, ref.God)
	wiz.Flags |= ref.Wizard
	if got := New(w, wiz.Ref, secret.Ref.String()).Absolute().Result(); got != secret.Ref {
		t.Errorf("a wizard should reach %v, got %v", secret.Ref, got)
	}
}

func TestPlayerMatch(t *testing.T) {
	f := newFixture(t)
	if got := New(f.w, f.player, "Igor").Player().Result(); got != f.player {
		t.Errorf("Igor = %v, want %v", got, f.player)
	}
	// A leading '*' forces a player match.
	if got := New(f.w, f.player, "*igor").Player().Result(); got != f.player {
		t.Errorf("*igor = %v, want %v", got, f.player)
	}
	if got := New(f.w, f.player, "Nobody").Player().Result(); got != ref.Nothing {
		t.Errorf("Nobody = %v, want #-1", got)
	}
}

func TestMatchingSurvivesAnEnvironmentCycle(t *testing.T) {
	f := newFixture(t)
	// A room contained by itself would loop a naive walk forever.
	f.w.Get(f.room).Location = f.room

	done := make(chan ref.Ref, 1)
	go func() { done <- New(f.w, f.player, "north").Exits().Result() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("matching hung on a cyclic environment tree")
	}
}

// TestRegisteredResolvesThroughTheEnvironment checks that "$name" finds a
// registration on an ancestor, which is how a world names its libraries.
func TestRegisteredResolvesThroughTheEnvironment(t *testing.T) {
	w := world.New()
	root := w.Create("Root", ref.TypeRoom, ref.God)
	room := w.Create("Room", ref.TypeRoom, ref.God)
	who := w.Create("Someone", ref.TypePlayer, ref.God)
	lib := w.Create("lib-strings", ref.TypeProgram, ref.God)
	if err := w.MoveTo(room.Ref, root.Ref); err != nil {
		t.Fatal(err)
	}
	if err := w.MoveTo(who.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		value props.Value
	}{
		{"a dbref", props.Value{Type: props.Ref, Ref: lib.Ref}},
		{"an integer", props.Value{Type: props.Int, Num: int64(lib.Ref)}},
		{"a string", props.Value{Type: props.String, Str: lib.Ref.String()}},
		{"a string with no #", props.Value{Type: props.String,
			Str: strconv.Itoa(int(lib.Ref))}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w.SetProp(root.Ref, "_reg/lib-strings", tc.value)
			got := New(w, who.Ref, "$lib-strings").Registered().Result()
			if got != lib.Ref {
				t.Errorf("$lib-strings resolved to %v, want %v", got, lib.Ref)
			}
		})
	}

	// A name that is not registered anywhere finds nothing.
	if got := New(w, who.Ref, "$nosuch").Registered().Result(); got != ref.Nothing {
		t.Errorf("an unregistered name resolved to %v", got)
	}
	// A plain name is not a registration.
	if got := New(w, who.Ref, "lib-strings").Registered().Result(); got != ref.Nothing {
		t.Errorf("a bare name was treated as a registration: %v", got)
	}
}
