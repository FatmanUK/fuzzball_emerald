package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// unlinkDenied is _do_unlink's own refusal (set.c:149), which is
// neither match_controlled's nor the "Permission denied." this server
// used to answer with.
const unlinkDenied = "Permission denied. " +
	"(You don't control the exit or its link)"

// setupUnlink builds the shape controls_link exists for: an exit
// somebody else owns, hanging in a room somebody else owns, pointing
// at a room the test player owns.
//
// The player is de-wizarded, because a wizard passes controls() and
// so never reaches controls_link at all — which is exactly why no
// golden case can cover any of this. The oracle drives #1.
func setupUnlink(t *testing.T, h *harness) {
	t.Helper()
	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		other := w.Create("Stranger", ref.TypePlayer,
			ref.Nothing)
		other.Owner = other.Ref

		// A hall the stranger owns, with the player standing
		// in it so the exit is reachable by name.
		hall := w.Create("Hall", ref.TypeRoom, other.Ref)
		if err := w.MoveTo(hall.Ref,
			ref.GlobalEnvironment); err != nil {
			t.Error(err)
		}
		if err := w.MoveTo(h.wizRef(), hall.Ref); err != nil {
			t.Error(err)
		}

		// A room the *player* owns, which the exit points at.
		mine := w.Create("Mine", ref.TypeRoom, h.wizRef())
		if err := w.MoveTo(mine.Ref,
			ref.GlobalEnvironment); err != nil {
			t.Error(err)
		}

		d := w.Create("door", ref.TypeExit, other.Ref)
		d.Dest = []ref.Ref{mine.Ref}
		if err := w.MoveTo(d.Ref, hall.Ref); err != nil {
			t.Error(err)
		}

		_ = d
		w.Get(h.wizRef()).Flags &^= ref.Wizard
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()
}

// TestUnlinkAcceptsTheDestinationsOwner is the first half of
// controls_link: the loop over an exit's destinations returns on the
// first one the player controls, so owning what an exit points at is
// enough to unlink it.
//
// Emerald refused this, which left the owner of a room powerless over
// an exit somebody else had aimed at it.
func TestUnlinkAcceptsTheDestinationsOwner(t *testing.T) {
	h := newHarness(t)
	h.login()
	setupUnlink(t, h)

	h.send("@unlink door")
	got := h.out()
	if !strings.Contains(got, "Unlinked.") {
		t.Errorf("the destination's owner should unlink:\n%s",
			got)
	}
	if strings.Contains(got, "Permission denied") {
		t.Errorf("controls_link was not consulted:\n%s", got)
	}
}

// TestUnlinkAcceptsTheRoomsOwner is the second half: when no
// destination is controlled, controls_link falls back to comparing
// `who` against the owner of the exit's *location*.
//
// That comparison is raw ownership rather than controls(), which is
// upstream's — db.c:1891 writes `who == OWNER(LOCATION(what))`.
func TestUnlinkAcceptsTheRoomsOwner(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		other := w.Create("Stranger", ref.TypePlayer,
			ref.Nothing)
		other.Owner = other.Ref

		// This time the hall is the player's and the
		// destination is not, so only the location clause can
		// let them through.
		hall := w.Create("Hall", ref.TypeRoom, h.wizRef())
		if err := w.MoveTo(hall.Ref,
			ref.GlobalEnvironment); err != nil {
			t.Error(err)
		}
		if err := w.MoveTo(h.wizRef(), hall.Ref); err != nil {
			t.Error(err)
		}
		theirs := w.Create("Vault", ref.TypeRoom, other.Ref)
		if err := w.MoveTo(theirs.Ref,
			ref.GlobalEnvironment); err != nil {
			t.Error(err)
		}
		d := w.Create("door", ref.TypeExit, other.Ref)
		d.Dest = []ref.Ref{theirs.Ref}
		if err := w.MoveTo(d.Ref, hall.Ref); err != nil {
			t.Error(err)
		}
		w.Get(h.wizRef()).Flags &^= ref.Wizard
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("@unlink door")
	got := h.out()
	if !strings.Contains(got, "Unlinked.") {
		t.Errorf("the room's owner should unlink:\n%s", got)
	}
}

// TestUnlinkRefusesEverybodyElse checks the refusal still happens,
// and in upstream's words: controlling neither the exit, nor any
// destination, nor the room it hangs in.
func TestUnlinkRefusesEverybodyElse(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		other := w.Create("Stranger", ref.TypePlayer,
			ref.Nothing)
		other.Owner = other.Ref

		hall := w.Create("Hall", ref.TypeRoom, other.Ref)
		if err := w.MoveTo(hall.Ref,
			ref.GlobalEnvironment); err != nil {
			t.Error(err)
		}
		if err := w.MoveTo(h.wizRef(), hall.Ref); err != nil {
			t.Error(err)
		}
		theirs := w.Create("Vault", ref.TypeRoom, other.Ref)
		if err := w.MoveTo(theirs.Ref,
			ref.GlobalEnvironment); err != nil {
			t.Error(err)
		}
		d := w.Create("door", ref.TypeExit, other.Ref)
		d.Dest = []ref.Ref{theirs.Ref}
		if err := w.MoveTo(d.Ref, hall.Ref); err != nil {
			t.Error(err)
		}
		w.Get(h.wizRef()).Flags &^= ref.Wizard
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("@unlink door")
	got := h.out()
	if !strings.Contains(got, unlinkDenied) {
		t.Errorf("want %q, got:\n%s", unlinkDenied, got)
	}
	// The old wording, and match_controlled's, are both wrong
	// here: this command has a refusal of its own.
	if strings.Contains(got, "what was matched") {
		t.Errorf("claims match_controlled:\n%s", got)
	}
}

// TestUnlinkStillWorksForTheOwner is the control: controls() is the
// first of the two tests and must still let the exit's own owner
// through.
func TestUnlinkStillWorksForTheOwner(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		d := w.Create("door", ref.TypeExit, h.wizRef())
		d.Dest = []ref.Ref{here}
		if err := w.MoveTo(d.Ref, here); err != nil {
			t.Error(err)
		}
		w.Get(h.wizRef()).Flags &^= ref.Wizard
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("@unlink door")
	got := h.out()
	if !strings.Contains(got, "Unlinked.") {
		t.Errorf("the owner should unlink:\n%s", got)
	}
}

// TestChownSeizesAnExitByItsLink covers the other caller of
// controls_link, which had the same fault for a different reason.
//
// do_chown (set.c:435) asks
//
//	Typeof(thing) != TYPE_EXIT ||
//	    (ndest && !controls_link(player, thing))
//
// so an exit with destinations is claimable by whoever controls its
// link. Emerald called canLink there — upstream's can_link, which
// is a different function — and in this branch, reached only when
// the exit *has* destinations, canLink collapses to plain controls.
// So the destination-owner case was lost here too.
func TestChownSeizesAnExitByItsLink(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		other := w.Create("Stranger", ref.TypePlayer,
			ref.Nothing)
		other.Owner = other.Ref

		hall := w.Create("Hall", ref.TypeRoom, other.Ref)
		if err := w.MoveTo(hall.Ref,
			ref.GlobalEnvironment); err != nil {
			t.Error(err)
		}
		if err := w.MoveTo(h.wizRef(), hall.Ref); err != nil {
			t.Error(err)
		}
		mine := w.Create("Mine", ref.TypeRoom, h.wizRef())
		if err := w.MoveTo(mine.Ref,
			ref.GlobalEnvironment); err != nil {
			t.Error(err)
		}
		d := w.Create("door", ref.TypeExit, other.Ref)
		d.Dest = []ref.Ref{mine.Ref}
		if err := w.MoveTo(d.Ref, hall.Ref); err != nil {
			t.Error(err)
		}

		// Seizing an exit costs exit_cost, so the claimant
		// needs the money; and a wizard would pass controls()
		// and never reach controls_link.
		w.SetProp(h.wizRef(), propValue, props.Value{
			Type: props.Int, Num: 1000,
		})
		w.Get(h.wizRef()).Flags &^= ref.Wizard
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("@chown door")
	got := h.out()
	if strings.Contains(got, "can't take possession") {
		t.Errorf("controls_link was not consulted:\n%s", got)
	}
	if !strings.Contains(got, "Owner changed") {
		t.Errorf("want the ownership change, got:\n%s", got)
	}
}
