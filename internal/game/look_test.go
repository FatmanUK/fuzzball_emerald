package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestLookAtAThingListsContains covers the one contents heading the
// golden harness cannot reach: no implemented command puts a thing
// inside a thing, so the container is built here instead.
//
// A room heads its listing "Contents:", a player "Carrying:" and a
// thing "Contains:". Emerald used "Contents:" for all three.
func TestLookAtAThingListsContains(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		box := w.Create("box", ref.TypeThing, h.wizRef())
		box.Home = here
		if err := w.MoveTo(box.Ref, here); err != nil {
			t.Error(err)
		}
		coin := w.Create("coin", ref.TypeThing, h.wizRef())
		coin.Home = here
		if err := w.MoveTo(coin.Ref, box.Ref); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("look box")
	got := h.out()
	if !strings.Contains(got, "Contains:") {
		t.Errorf("a thing's contents were not headed Contains:\n%s",
			got)
	}
	if !strings.Contains(got, "coin") {
		t.Errorf("the contents were not listed:\n%s", got)
	}
	// And no name line, which is look_room's alone.
	if strings.Contains(got, "box(#") {
		t.Errorf("looking at a thing printed its name line:\n%s", got)
	}

	// HAVEN keeps them to itself, and the description still
	// shows.
	err = h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		for _, r := range w.Contents(here) {
			if nameOf(w, r) == "box" {
				w.Get(r).Flags |= ref.Haven
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("look box")
	got = h.out()
	if strings.Contains(got, "Contains:") ||
		strings.Contains(got, "coin") {
		t.Errorf("a HAVEN thing showed its contents:\n%s", got)
	}
	if !strings.Contains(got, "You see nothing special.") {
		t.Errorf("a HAVEN thing lost its description:\n%s", got)
	}
}

// TestLookRefusalsMatchUpstream covers do_look_at's three per-type
// permission messages. Each is a different sentence upstream, naming
// the test it failed, and they are not interchangeable.
//
// lookAt is called directly rather than through "look", because the
// matcher will not cooperate: match_absolute and match_player are
// wizard-only, and every other way a mortal can name an object —
// carrying it, standing beside it — is one of the conditions these
// refusals test for. Upstream's own refusals are reachable only
// through a dbref, so driving the command would test the matcher
// rather than the branch.
func TestLookRefusalsMatchUpstream(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	var elsewhere, stranger, theirs ref.Ref
	err := h.engine.Do(ctx, func(w *world.World) {
		// A room the player is neither in nor able to link
		// to, so neither LINK_OK nor ABODE — and owned by
		// someone else, since the harness's wizard is #1 and
		// so controls anything God owns.
		far := w.Create("Far Room", ref.TypeRoom, ref.Nothing)
		far.Flags &^= ref.LinkOK | ref.Abode
		elsewhere = far.Ref

		other := w.Create("Stranger", ref.TypePlayer, ref.Nothing)
		other.Owner = other.Ref
		other.Home = far.Ref
		far.Owner = other.Ref
		if err := w.MoveTo(other.Ref, far.Ref); err != nil {
			t.Error(err)
		}
		stranger = other.Ref

		o := w.Create("locket", ref.TypeThing, other.Ref)
		o.Home = far.Ref
		if err := w.MoveTo(o.Ref, far.Ref); err != nil {
			t.Error(err)
		}
		theirs = o.Ref

		// Stop being a wizard, so control is really tested.
		w.Get(h.wizRef()).Flags &^= ref.Wizard
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	for _, tc := range []struct {
		target ref.Ref
		want   string
	}{
		{elsewhere, "Permission denied. (you're not where you " +
			"want to look, and can't link to it)"},
		{stranger, "Permission denied. (Your location isn't the " +
			"same as what you're looking at)"},
		{theirs, "Permission denied. (You're not in the same room " +
			"as or carrying the object)"},
	} {
		target := tc.target
		err := h.engine.Do(ctx, func(w *world.World) {
			h.s.lookAt(w, h.d.ID, h.wizRef(), target)
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := h.out(); !strings.Contains(got, tc.want) {
			t.Errorf("looking at %s said:\n%s\nwant %q",
				target, got, tc.want)
		}
	}
}

// TestLookRoomWithNoDescriptionSaysNothing is the asymmetry between
// look_room and look_simple: a room with no description prints
// nothing at all, where anything else gets the nothing-special
// message.
func TestLookRoomWithNoDescriptionSaysNothing(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		w.Get(here).Props.Delete(propDesc)
		w.Modified(here)
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("look")
	if got := h.out(); strings.Contains(got,
		"You see nothing special.") {
		t.Errorf("a room with no description used look_simple's "+
			"message:\n%s", got)
	}
}

// TestLookRoomShowsItsSuccessMessage checks that look_room runs
// can_doit, which is what makes a room's @succ show on a plain look.
func TestLookRoomShowsItsSuccessMessage(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		w.SetProp(here, propSucc, props.Value{Type: props.String,
			Str: "The air here is warm."})
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("look")
	if got := h.out(); !strings.Contains(got,
		"The air here is warm.") {
		t.Errorf("a room's @succ did not show on look:\n%s", got)
	}
}
