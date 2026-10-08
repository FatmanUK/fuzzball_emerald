package match

import (
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// `compatible_priorities` (`match.c:588`) promotes a default-priority
// exit from 1 to 2, and it had **no reader anywhere** in this server
// — one of the 69 `@tune` parameters nothing consulted, and it
// defaults on.
//
// Without it an exit on a THING beats a plain exit on the room,
// because both are PLevel 1 and a strictly higher level overwrites
// the earlier stage's match. With it both reach 2 and the equal-level
// tie goes to the stage that found one first, which is the room.
// §2.3.3 of the manual is what found this.
//
// Its third branch is the one no transcript can reach: the promotion
// is **withheld** from an exit on a thing whose owner does not
// control where the searcher is standing, so somebody else's puppet
// cannot outrank the room you are in. The oracle's player owns every
// object in its world, so this is a unit test.
func TestExitLevelPromotion(t *testing.T) {
	w := world.New()
	room := w.Create("Room", ref.TypeRoom, ref.God)
	room.Dropto = ref.Nothing
	owner := w.Create("Igor", ref.TypePlayer, ref.Nothing)
	owner.Owner = owner.Ref
	stranger := w.Create("Nadia", ref.TypePlayer, ref.Nothing)
	stranger.Owner = stranger.Ref
	if err := w.MoveTo(owner.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}

	// The room's own exit, and one hanging on a thing.
	onRoom := w.Create("bank", ref.TypeExit, owner.Ref)
	onRoom.Dest = []ref.Ref{room.Ref}
	if err := w.MoveTo(onRoom.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}
	till := w.Create("till", ref.TypeThing, owner.Ref)
	if err := w.MoveTo(till.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}
	onThing := w.Create("bank", ref.TypeExit, owner.Ref)
	onThing.Dest = []ref.Ref{room.Ref}
	if err := w.MoveTo(onThing.Ref, till.Ref); err != nil {
		t.Fatal(err)
	}

	level := func(from ref.Ref, e ref.Ref) int {
		m := New(w, owner.Ref, "bank").Around(from)
		return m.exitLevel(e, w.Get(e))
	}

	// The room's exit is promoted: its location is not a thing,
	// so the second disjunct answers before ownership is asked.
	if got := level(owner.Ref, onRoom.Ref); got != 2 {
		t.Errorf("an exit on a room: want level 2, got %d",
			got)
	}

	// The thing's exit is promoted too, because its owner owns
	// the room the searcher is in.
	if got := level(owner.Ref, onThing.Ref); got != 2 {
		t.Errorf("an owned thing's exit: want level 2, "+
			"got %d", got)
	}

	// Now move the thing into a room its owner does not control.
	elsewhere := w.Create("Elsewhere", ref.TypeRoom,
		stranger.Ref)
	elsewhere.Dropto = ref.Nothing
	if err := w.MoveTo(till.Ref, elsewhere.Ref); err != nil {
		t.Fatal(err)
	}
	if err := w.MoveTo(stranger.Ref, elsewhere.Ref); err != nil {
		t.Fatal(err)
	}
	// Searching from the stranger, who is standing in a room the
	// exit's owner does not control: no promotion.
	m := New(w, stranger.Ref, "bank")
	if got := m.exitLevel(onThing.Ref,
		w.Get(onThing.Ref)); got != 1 {
		t.Errorf("a stranger's room: want level 1, got %d",
			got)
	}

	// With the parameter off nothing is promoted, whoever asks.
	if err := w.Tune.SetString("compatible_priorities",
		"no"); err != nil {
		t.Fatal(err)
	}
	for _, e := range []ref.Ref{onRoom.Ref, onThing.Ref} {
		if got := level(owner.Ref, e); got != 1 {
			t.Errorf("with the parameter off: want "+
				"level 1, got %d", got)
		}
	}

	// A mucker bit makes the level explicit, so the promotion
	// never applies to it in the first place.
	if err := w.Tune.SetString("compatible_priorities",
		"yes"); err != nil {
		t.Fatal(err)
	}
	w.Get(onThing.Ref).Flags |= ref.SMucker
	if got := level(owner.Ref, onThing.Ref); got != 2 {
		t.Errorf("M1 is PLevel 2 outright: got %d", got)
	}
	w.Get(onThing.Ref).Flags |= ref.Mucker
	if got := level(owner.Ref, onThing.Ref); got != 4 {
		t.Errorf("M3 is PLevel 4: got %d", got)
	}
}
