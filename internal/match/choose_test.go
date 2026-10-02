package match

import (
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// chooseFixture is a room holding two objects with the *same exact
// name* and different types, which is the only situation choose_thing
// is ever asked about: a partial match never reaches it.
type chooseFixture struct {
	w      *world.World
	room   ref.Ref
	player ref.Ref
	thing  ref.Ref
	exit   ref.Ref
}

func newChooseFixture(t *testing.T) *chooseFixture {
	t.Helper()
	w := world.New()
	room := w.Create("Room", ref.TypeRoom, ref.God)
	room.Dropto = ref.Nothing
	p := w.Create("Igor", ref.TypePlayer, ref.Nothing)
	p.Owner = p.Ref
	if err := w.MoveTo(p.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}

	thing := w.Create("box", ref.TypeThing, p.Ref)
	if err := w.MoveTo(thing.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}
	exit := w.Create("box", ref.TypeExit, p.Ref)
	exit.Dest = []ref.Ref{room.Ref}
	if err := w.MoveTo(exit.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}

	return &chooseFixture{
		w: w, room: room.Ref, player: p.Ref,
		thing: thing.Ref, exit: exit.Ref,
	}
}

// TestChoosePrefersTheAskedForType covers tie-break two, and the
// fixture is narrower than it looks because choose_thing is:
// candidates have to arrive through the **same** search.
//
// A thing and an exit of one name do not reach it. match_all_exits
// assigns a priority level, and an exit at a level above
// md->match_level overwrites exact_match outright (match.c:632)
// without consulting choose_thing at all — so the exit wins
// whatever type was asked for, in Emerald and upstream alike.
//
// What does reach it is two objects in one container, which means two
// of the four types that live in one: here a thing and a program both
// called "wand" in the player's hands.
func TestChoosePrefersTheAskedForType(t *testing.T) {
	f := newChooseFixture(t)

	wandThing := f.w.Create("wand", ref.TypeThing, f.player)
	if err := f.w.MoveTo(wandThing.Ref, f.player); err != nil {
		t.Fatal(err)
	}
	wandProg := f.w.Create("wand", ref.TypeProgram, f.player)
	if err := f.w.MoveTo(wandProg.Ref, f.player); err != nil {
		t.Fatal(err)
	}

	// Run each enough times that the coin toss would show if the
	// preference were not deciding.
	for i := 0; i < 30; i++ {
		if got := New(f.w, f.player, "wand").
			PreferType(ref.TypeThing).Possession().
			Result(); got != wandThing.Ref {
			t.Fatalf("preferring a thing got %v, want %v",
				got, wandThing.Ref)
		}
		if got := New(f.w, f.player, "wand").
			PreferType(ref.TypeProgram).Possession().
			Result(); got != wandProg.Ref {
			t.Fatalf("preferring a program: %v, want %v",
				got, wandProg.Ref)
		}
	}

	// A preference for something neither of them is leaves the
	// tie to the later rules, so an answer still comes back.
	got := New(f.w, f.player, "wand").
		PreferType(ref.TypeRoom).Possession().Result()
	if got != wandThing.Ref && got != wandProg.Ref {
		t.Errorf("preferring a room got %v, want one of "+
			"%v or %v", got, wandThing.Ref, wandProg.Ref)
	}
}

// TestExitPriorityBeatsTheTypePreference pins the reason the test
// above cannot use an exit: an exit above the current match level
// overwrites exact_match directly, so choose_thing is never asked.
func TestExitPriorityBeatsTheTypePreference(t *testing.T) {
	f := newChooseFixture(t)
	got := New(f.w, f.player, "box").
		PreferType(ref.TypeThing).Neighbor().Exits().Result()
	if got != f.exit {
		t.Errorf("= %v, want the exit %v despite the "+
			"preference", got, f.exit)
	}
}

// TestChooseChecksKeys covers tie-break three: given two exact
// matches and no type to separate them, an object the searcher can
// actually use beats one locked against them.
//
// Upstream sets this at three call sites and no more — do_move's
// direction and both of do_get's matches — so it is off unless a
// command asked for it.
func TestChooseChecksKeys(t *testing.T) {
	f := newChooseFixture(t)

	// A second thing of the same name, so the tie is between two
	// objects of one type and the preferred type cannot decide.
	other := f.w.Create("box", ref.TypeThing, f.player)
	if err := f.w.MoveTo(other.Ref, f.room); err != nil {
		t.Fatal(err)
	}

	// Pretend the first box is locked and the second is not.
	usable := func(r ref.Ref) bool { return r != f.thing }

	for i := 0; i < 20; i++ {
		got := New(f.w, f.player, "box").
			PreferType(ref.TypeThing).
			Usable(usable).
			Neighbor().Result()
		if got != other.Ref {
			t.Fatalf("got the locked box %v, want %v",
				got, other.Ref)
		}
	}

	// With neither usable the test falls through rather than
	// refusing, so an answer still comes back.
	none := func(ref.Ref) bool { return false }
	got := New(f.w, f.player, "box").
		PreferType(ref.TypeThing).Usable(none).
		Neighbor().Result()
	if got != f.thing && got != other.Ref {
		t.Errorf("with neither usable got %v", got)
	}
}

// TestChooseByEnvironmentDistance covers tie-break four. Two
// same-named things, one in the room and one two levels out, and the
// nearer wins.
func TestChooseByEnvironmentDistance(t *testing.T) {
	w := world.New()
	outer := w.Create("Outer", ref.TypeRoom, ref.God)
	outer.Dropto = ref.Nothing
	inner := w.Create("Inner", ref.TypeRoom, ref.God)
	inner.Dropto = ref.Nothing
	if err := w.MoveTo(inner.Ref, outer.Ref); err != nil {
		t.Fatal(err)
	}
	p := w.Create("Igor", ref.TypePlayer, ref.Nothing)
	p.Owner = p.Ref
	if err := w.MoveTo(p.Ref, inner.Ref); err != nil {
		t.Fatal(err)
	}

	// Two exits of the same name, one in the room the player is
	// in and one a level out, both reachable by the environment
	// walk match_all_exits does.
	near := w.Create("north", ref.TypeExit, p.Ref)
	near.Dest = []ref.Ref{outer.Ref}
	if err := w.MoveTo(near.Ref, inner.Ref); err != nil {
		t.Fatal(err)
	}
	far := w.Create("north", ref.TypeExit, p.Ref)
	far.Dest = []ref.Ref{inner.Ref}
	if err := w.MoveTo(far.Ref, outer.Ref); err != nil {
		t.Fatal(err)
	}

	// Run it enough times that the coin toss would show.
	for i := 0; i < 30; i++ {
		if got := New(w, p.Ref, "north").
			PreferType(ref.TypeExit).Exits().
			Result(); got != near.Ref {
			t.Fatalf("got the far exit %v, want near "+
				"%v", got, near.Ref)
		}
	}
}

// TestChooseTossesACoin pins the last resort, which is the one thing
// in the matcher no golden case can compare: two indistinguishable
// exact matches give an arbitrary answer.
//
// It is reproduced rather than settled deterministically because
// making it stable would invent a behaviour programs could come to
// rely on — and because "last one wins", which is what Emerald used
// to do, is a fifth answer upstream never gives.
func TestChooseTossesACoin(t *testing.T) {
	f := newChooseFixture(t)
	other := f.w.Create("box", ref.TypeThing, f.player)
	if err := f.w.MoveTo(other.Ref, f.room); err != nil {
		t.Fatal(err)
	}

	seen := map[ref.Ref]int{}
	for i := 0; i < 200; i++ {
		got := New(f.w, f.player, "box").
			PreferType(ref.TypeThing).Neighbor().Result()
		seen[got]++
	}
	// Both boxes are in the same room at the same distance with
	// no lock, so nothing but the coin separates them. 200 tosses
	// landing entirely on one side would be a 2^-199 accident.
	if len(seen) != 2 {
		t.Errorf("200 matches gave %d answers: %v",
			len(seen), seen)
	}
}

// TestChooseIgnoresNothing covers the first tie-break, which is the
// only one that fires on an ordinary single match: every addExact
// goes through chooseThing, so the common case is a comparison
// against ref.Nothing.
func TestChooseIgnoresNothing(t *testing.T) {
	f := newChooseFixture(t)

	got := New(f.w, f.player, "me").Me().Result()
	if got != f.player {
		t.Errorf("me = %v, want %v", got, f.player)
	}
	// A virtual ref has no type, no lock and no place in the
	// environment, and must survive a tie against one.
	if got := New(f.w, f.player, "home").Home().
		Result(); got != ref.Home {
		t.Errorf("home = %v, want HOME", got)
	}
	if got := New(f.w, f.player, "nil").Nil().
		Result(); got != ref.Nil {
		t.Errorf("nil = %v, want NIL", got)
	}
}

// TestEnvDistance pins the two things about env_distance that are not
// obvious, and the numbers below were taken from the C compiled and
// run over this exact graph rather than from reading it. They are not
// what the name suggests.
//
// It measures to the target's **parent**, so a thing sitting in the
// room you are standing in is at distance zero. And when the searcher
// is not under that parent at all, the walk runs off the top of the
// world and counts hops to #0 — so from a *sibling* branch the
// answer grows with how deep the searcher is rather than with how far
// the target is, and from #0 itself the answer is 1 because the very
// first hop reaches nothing.
func TestEnvDistance(t *testing.T) {
	w := world.New()
	// #0 → a → b, with a thing in each.
	root := w.Create("Root", ref.TypeRoom, ref.God)
	root.Dropto = ref.Nothing
	a := w.Create("A", ref.TypeRoom, ref.God)
	a.Dropto = ref.Nothing
	b := w.Create("B", ref.TypeRoom, ref.God)
	b.Dropto = ref.Nothing
	if err := w.MoveTo(a.Ref, root.Ref); err != nil {
		t.Fatal(err)
	}
	if err := w.MoveTo(b.Ref, a.Ref); err != nil {
		t.Fatal(err)
	}
	inB := w.Create("thing", ref.TypeThing, ref.God)
	if err := w.MoveTo(inB.Ref, b.Ref); err != nil {
		t.Fatal(err)
	}

	// From the containing room: zero, because the measurement is
	// to the thing's parent and that parent is B.
	if got := w.EnvDistance(b.Ref, inB.Ref); got != 0 {
		t.Errorf("from the containing room = %d, want 0", got)
	}
	// From one level out: **two**, not one. A is not under B, so
	// the walk never reaches B and counts to the top instead —
	// A to #0 is one hop, and the loop counts one more before
	// testing.
	if got := w.EnvDistance(a.Ref, inB.Ref); got != 2 {
		t.Errorf("from one level out = %d, want 2", got)
	}
	// From two levels out: **one**, which is less than from one
	// level out. root is #0, so the first hop reaches nothing and
	// the walk stops immediately.
	if got := w.EnvDistance(root.Ref, inB.Ref); got != 1 {
		t.Errorf("from #0 = %d, want 1", got)
	}
}
