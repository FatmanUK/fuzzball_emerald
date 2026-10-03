package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// do_link's three refusals, each naming what it tested. Note the
// lower-case "you" in all three, which is upstream's and differs from
// match_controlled's capitalised wording.
const (
	linkNoRelink = "Permission denied. " +
		"(you don't control the exit to relink)"
	linkNoThing = "Permission denied. (you don't control the " +
		"thing, or you can't link to dest)"
	linkNoRoom = "Permission denied. (you don't control the " +
		"room, or can't link to the dropto)"
)

// linkWorld builds a stranger, a room they own, a room the player
// owns, and leaves the player standing somewhere they can see both.
// The player keeps BUILDER but loses WIZARD, since a wizard controls
// everything and pays for nothing.
func linkWorld(t *testing.T, h *harness,
	build func(w *world.World, other, mine, theirs ref.Ref)) {

	t.Helper()
	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		other := w.Create("Stranger", ref.TypePlayer,
			ref.Nothing)
		other.Owner = other.Ref

		mine := w.Create("Mine", ref.TypeRoom, h.wizRef())
		theirs := w.Create("Theirs", ref.TypeRoom, other.Ref)
		for _, r := range []ref.Ref{mine.Ref, theirs.Ref} {
			if err := w.MoveTo(r,
				ref.GlobalEnvironment); err != nil {
				t.Error(err)
			}
		}
		// Enough money that a cost refusal is never what is
		// being measured.
		w.SetProp(h.wizRef(), propValue, props.Value{
			Type: props.Int, Num: 10000,
		})
		w.Get(h.wizRef()).Flags &^= ref.Wizard
		w.Get(h.wizRef()).Flags |= ref.Builder

		build(w, other.Ref, mine.Ref, theirs.Ref)
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()
}

// TestLinkSeizesAnUnlinkedExit is the divergence this commit exists
// for. do_link tests controls() **only when the exit already points
// somewhere** (create.c:161), so an unlinked exit is linkable by
// anybody — a builder pays link_cost plus exit_cost, the old owner
// is paid exit_cost, and the exit changes hands.
//
// Emerald went through resolveControlled and refused before any of
// that could run, so an abandoned exit could only ever be relinked by
// its owner.
func TestLinkSeizesAnUnlinkedExit(t *testing.T) {
	h := newHarness(t)
	h.login()

	var door ref.Ref
	linkWorld(t, h, func(w *world.World, other, mine,
		theirs ref.Ref) {

		here := w.Get(h.wizRef()).Location
		d := w.Create("door", ref.TypeExit, other)
		// No destinations: this is what makes it seizable.
		d.Dest = nil
		if err := w.MoveTo(d.Ref, here); err != nil {
			t.Error(err)
		}
		door = d.Ref
	})

	h.send("@link door=Mine")
	got := h.out()
	if strings.Contains(got, "Permission denied") {
		t.Errorf("an unlinked exit should be seizable:\n%s",
			got)
	}
	if !strings.Contains(got, "Linked to") {
		t.Errorf("want the link, got:\n%s", got)
	}
	// Upstream warns that claiming unlinked exits is going away.
	if !strings.Contains(got, "Claiming unlinked exits") {
		t.Errorf("want the deprecation notice:\n%s", got)
	}
	// And the exit changed hands, which happens before the
	// destination is even resolved.
	ctx := context.Background()
	if err := h.engine.Do(ctx, func(w *world.World) {
		if o := w.Get(door); o.Owner != h.wizRef() {
			t.Errorf("owner = %v, want %v", o.Owner,
				h.wizRef())
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestLinkRefusesRelinkingSomebodyElses is the other side of the same
// test: once an exit has a destination, controls() decides, and the
// refusal is do_link's own wording rather than match_controlled's.
func TestLinkRefusesRelinkingSomebodyElses(t *testing.T) {
	h := newHarness(t)
	h.login()

	linkWorld(t, h, func(w *world.World, other, mine,
		theirs ref.Ref) {

		here := w.Get(h.wizRef()).Location
		d := w.Create("door", ref.TypeExit, other)
		d.Dest = []ref.Ref{theirs}
		if err := w.MoveTo(d.Ref, here); err != nil {
			t.Error(err)
		}
	})

	h.send("@link door=Mine")
	got := h.out()
	if !strings.Contains(got, linkNoRelink) {
		t.Errorf("want %q, got:\n%s", linkNoRelink, got)
	}
	if strings.Contains(got, "what was matched") {
		t.Errorf("claims match_controlled:\n%s", got)
	}
}

// TestLinkTreatsNilAsRelinkable pins the test upstream makes inside
// the controls() branch: dest[0] != NIL. An exit parked at NIL is not
// "already linked", so its owner may relink it, where one pointing at
// a real room may not.
func TestLinkTreatsNilAsRelinkable(t *testing.T) {
	h := newHarness(t)
	h.login()

	linkWorld(t, h, func(w *world.World, other, mine,
		theirs ref.Ref) {

		here := w.Get(h.wizRef()).Location
		nilExit := w.Create("nildoor", ref.TypeExit,
			h.wizRef())
		nilExit.Dest = []ref.Ref{ref.Nil}
		if err := w.MoveTo(nilExit.Ref, here); err != nil {
			t.Error(err)
		}
		realExit := w.Create("realdoor", ref.TypeExit,
			h.wizRef())
		realExit.Dest = []ref.Ref{mine}
		if err := w.MoveTo(realExit.Ref, here); err != nil {
			t.Error(err)
		}
	})

	h.send("@link nildoor=Mine")
	if got := h.out(); !strings.Contains(got, "Linked to") {
		t.Errorf("a NIL exit should relink:\n%s", got)
	}
	h.send("@link realdoor=Mine")
	got := h.out()
	if !strings.Contains(got, "That exit is already linked.") {
		t.Errorf("want the already-linked refusal:\n%s", got)
	}
}

// TestLinkRefusesSeizingWithoutBuilder covers the gate between the
// permission test and the money: seizing needs the BUILDER bit, and
// says so in its own words.
func TestLinkRefusesSeizingWithoutBuilder(t *testing.T) {
	h := newHarness(t)
	h.login()

	linkWorld(t, h, func(w *world.World, other, mine,
		theirs ref.Ref) {

		here := w.Get(h.wizRef()).Location
		d := w.Create("door", ref.TypeExit, other)
		d.Dest = nil
		if err := w.MoveTo(d.Ref, here); err != nil {
			t.Error(err)
		}
		w.Get(h.wizRef()).Flags &^= ref.Builder
	})

	h.send("@link door=Mine")
	got := h.out()
	const want = "Only authorized builders may seize exits."
	if !strings.Contains(got, want) {
		t.Errorf("want %q, got:\n%s", want, got)
	}
}

// TestLinkHomeAndDroptoNameTheirOwnTests checks the two non-exit
// branches, which do test controls() themselves — and each names
// both halves of what it asked, in wording of its own.
func TestLinkHomeAndDroptoNameTheirOwnTests(t *testing.T) {
	h := newHarness(t)
	h.login()

	linkWorld(t, h, func(w *world.World, other, mine,
		theirs ref.Ref) {

		here := w.Get(h.wizRef()).Location
		// Somebody else's thing, standing where the player
		// can name it.
		o := w.Create("locket", ref.TypeThing, other)
		o.Home = here
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Error(err)
		}
	})

	h.send("@link locket=Mine")
	if got := h.out(); !strings.Contains(got, linkNoThing) {
		t.Errorf("want %q, got:\n%s", linkNoThing, got)
	}
	// A room the player does not own, named by dbref so the
	// narrow destination search is not what refuses.
	h.send("@link Theirs=Mine")
	if got := h.out(); !strings.Contains(got, linkNoRoom) {
		t.Errorf("want %q, got:\n%s", linkNoRoom, got)
	}
}

// TestLinkWithNoDestinationSaysSo covers the invented message this
// commit removes. "Link it to what?" is nowhere in upstream; an exit
// with an empty destination string is charged for, transferred, and
// then told "No destinations linked." — because _link_exit's loop
// never runs and so never matches anything.
func TestLinkWithNoDestinationSaysSo(t *testing.T) {
	h := newHarness(t)
	h.login()

	linkWorld(t, h, func(w *world.World, other, mine,
		theirs ref.Ref) {

		here := w.Get(h.wizRef()).Location
		d := w.Create("door", ref.TypeExit, h.wizRef())
		d.Dest = nil
		if err := w.MoveTo(d.Ref, here); err != nil {
			t.Error(err)
		}
	})

	h.send("@link door=")
	got := h.out()
	if !strings.Contains(got, "No destinations linked.") {
		t.Errorf("want the no-destinations answer:\n%s", got)
	}
	if strings.Contains(got, "Link it to what") {
		t.Errorf("the invented message is back:\n%s", got)
	}
}
