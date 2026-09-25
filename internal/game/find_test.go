package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestSearchRefusalsMatchUpstream covers the two control refusals the
// golden harness cannot reach: its player is #1, who controls
// everything in the fixture. They are different sentences upstream
// and are not interchangeable.
func TestSearchRefusalsMatchUpstream(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	var theirs ref.Ref
	err := h.engine.Do(ctx, func(w *world.World) {
		other := w.Create("Stranger", ref.TypePlayer, ref.Nothing)
		other.Owner = other.Ref
		here := w.Get(h.wizRef()).Location
		o := w.Create("locket", ref.TypeThing, other.Ref)
		o.Home = here
		if err := w.MoveTo(o.Ref, here); err != nil {
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
	_ = theirs

	for _, tc := range []struct{ send, want string }{
		{"@contents locket", "Permission denied. (You can't get " +
			"the contents of something you don't control)"},
		{"@entrances locket", "Permission denied. (You can't list " +
			"entrances of objects you don't control)"},
	} {
		h.send(tc.send)
		if got := h.out(); !strings.Contains(got, tc.want) {
			t.Errorf("%q said:\n%s\nwant %q",
				tc.send, got, tc.want)
		}
	}
}

// TestFindChargesLookupCost is the half of do_find nothing read
// before: lookup_cost is a @tune parameter this server had never
// consulted, and a player who cannot pay gets no results at all.
func TestFindChargesLookupCost(t *testing.T) {
	h := newHarness(t)
	h.login()

	// A mortal, since a wizard pays for nothing — but a
	// builder, since @find is BUILDERONLY at the dispatch site.
	who, mortal := connectAs(t, h, "Pauper", false)

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		w.Get(who).Flags |= ref.Builder
		if err := w.Tune.SetString("lookup_cost",
			"10"); err != nil {
			t.Error(err)
		}
		w.SetProp(who, propValue,
			props.Value{Type: props.Int, Num: 25})
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	// Two searches are affordable and the third is not.
	for i := 1; i <= 2; i++ {
		if got := sendAs(t, h, mortal, "@find"); !strings.Contains(
			got, "objects found.") {
			t.Errorf("search %d was refused:\n%s", i, got)
		}
	}
	got := sendAs(t, h, mortal, "@find")
	if !strings.Contains(got, "You don't have enough pennies.") {
		t.Errorf("a broke player was not refused:\n%s", got)
	}
	if strings.Contains(got, "objects found.") {
		t.Errorf("a refused search still listed results:\n%s", got)
	}

	// And 5 pennies are left, not 15 — the failed attempt is
	// free.
	err = h.engine.Do(ctx, func(w *world.World) {
		if n := valueOf(w, who); n != 5 {
			t.Errorf("the player has %d pennies, want 5", n)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestFindHasNoResultCap pins the invented limit that is gone. The
// old cmdFind stopped at 200 objects, so a large world answered with
// part of the truth and said so in a count that looked right.
func TestFindHasNoResultCap(t *testing.T) {
	h := newHarness(t)
	h.login()

	const n = 250
	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		for i := 0; i < n; i++ {
			o := w.Create("cog", ref.TypeThing, h.wizRef())
			o.Home = here
			if err := w.MoveTo(o.Ref, here); err != nil {
				t.Error(err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	// The mode comes after a *second* "=": the first separates
	// the name from the flags, and init_checkflags splits what is
	// left again. "@find cog=count" would read "count" as five
	// flag letters and find nothing.
	h.send("@find cog==count")
	if got := h.out(); !strings.Contains(got,
		sprintf("%d objects found.", n)) {
		t.Errorf("@find did not report all %d:\n%s", n, got)
	}
}

// TestOwnedFindsAnotherPlayersThings checks the wizard-only half of
// do_owned, and that a mortal naming somebody else silently gets
// their own things rather than a refusal — which is upstream's own
// reading of the argument.
func TestOwnedFindsAnotherPlayersThings(t *testing.T) {
	h := newHarness(t)
	h.login()

	who, mortal := connectAs(t, h, "Owner", false)
	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		w.Get(who).Flags |= ref.Builder
		here := w.Get(h.wizRef()).Location
		o := w.Create("theirthing", ref.TypeThing, who)
		o.Home = here
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("@owned Owner")
	if got := h.out(); !strings.Contains(got, "theirthing") {
		t.Errorf("a wizard could not list another's things:\n%s", got)
	}

	// The mortal naming the wizard gets their own, not the
	// wizard's.
	got := sendAs(t, h, mortal, "@owned Wizard")
	if !strings.Contains(got, "theirthing") {
		t.Errorf("a mortal did not get their own things:\n%s", got)
	}
	if strings.Contains(got, "The Study") {
		t.Errorf("a mortal listed the wizard's things:\n%s", got)
	}
}
