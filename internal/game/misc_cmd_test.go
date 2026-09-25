package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestScoreUsesTheSingular checks the half of do_score the golden
// case cannot reach: the fixture's #1 never happens to hold exactly
// one penny, and the singular is a separate @tune parameter rather
// than the plural with the "s" taken off.
func TestScoreUsesTheSingular(t *testing.T) {
	h := newHarness(t)
	h.login()

	for _, tc := range []struct {
		value int64
		want  string
	}{
		{1, "You have 1 penny."},
		{2, "You have 2 pennies."},
		{0, "You have 0 pennies."},
	} {
		value := tc.value
		err := h.engine.Do(context.Background(),
			func(w *world.World) {
				w.SetProp(h.wizRef(), propValue,
					props.Value{Type: props.Int,
						Num: value})
			})
		if err != nil {
			t.Fatal(err)
		}
		h.out()

		h.send("score")
		if got := h.out(); !strings.Contains(got, tc.want) {
			t.Errorf("score with %d said:\n%s\nwant %q",
				tc.value, got, tc.want)
		}
	}
}

// TestUncompileEmptiesTheCache checks what @uncompile is for. The
// message is easy to get right and says nothing about whether
// anything was dropped, so the cache is inspected directly.
func TestUncompileEmptiesTheCache(t *testing.T) {
	h := newHarness(t)
	h.login()

	name := h.installProgram(t, "warmer",
		`: main me @ "ran" notify ;`)
	h.send(name)
	if got := h.out(); !strings.Contains(got, "ran") {
		t.Fatalf("the program did not run:\n%s", got)
	}

	var before, after int
	err := h.engine.Do(context.Background(),
		func(w *world.World) { before = len(h.s.programs) })
	if err != nil {
		t.Fatal(err)
	}
	if before == 0 {
		t.Fatal("running a program left nothing in the cache")
	}

	h.send("@uncompile")
	if got := h.out(); !strings.Contains(got,
		"All programs decompiled.") {
		t.Errorf("@uncompile said:\n%s", got)
	}
	err = h.engine.Do(context.Background(),
		func(w *world.World) { after = len(h.s.programs) })
	if err != nil {
		t.Fatal(err)
	}
	if after != 0 {
		t.Errorf("the cache still holds %d programs", after)
	}

	// And the program still runs, being recompiled on demand.
	h.send(name)
	if got := h.out(); !strings.Contains(got, "ran") {
		t.Errorf("the program stopped running after "+
			"@uncompile:\n%s", got)
	}
}

// TestTraceWalksTheEnvironment covers the shape golden's fixture
// cannot show: its room is #0, which is its own top, so nothing there
// has a chain longer than one link.
func TestTraceWalksTheEnvironment(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	var inner ref.Ref
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		mid := w.Create("Middle", ref.TypeRoom, h.wizRef())
		if err := w.MoveTo(mid.Ref, here); err != nil {
			t.Error(err)
		}
		in := w.Create("Inner", ref.TypeRoom, h.wizRef())
		if err := w.MoveTo(in.Ref, mid.Ref); err != nil {
			t.Error(err)
		}
		inner = in.Ref
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("@trace " + inner.String())
	got := h.out()
	for _, want := range []string{"Inner", "Middle", "The Study",
		"***End of List***"} {
		if !strings.Contains(got, want) {
			t.Errorf("@trace did not show %q:\n%s", want, got)
		}
	}

	// A depth stops the walk early, and zero means no limit.
	h.send("@trace " + inner.String() + "=2")
	got = h.out()
	if !strings.Contains(got, "Middle") {
		t.Errorf("depth 2 stopped too early:\n%s", got)
	}
	if strings.Contains(got, "The Study") {
		t.Errorf("depth 2 walked too far:\n%s", got)
	}

	h.send("@trace " + inner.String() + "=0")
	if got := h.out(); !strings.Contains(got, "The Study") {
		t.Errorf("depth 0 was treated as a limit:\n%s", got)
	}
}

// TestWallReachesEverybody checks the one thing about @wall that
// matters and that a single-connection golden run cannot show: it
// goes to every connected descriptor, not to the shouter's room.
func TestWallReachesEverybody(t *testing.T) {
	h := newHarness(t)
	h.login()

	// A second player with a live connection of their own, moved
	// out of the shouter's room so that hearing it cannot be room
	// speech reaching them by accident.
	who, other := connectAs(t, h, "Bystander", false)
	err := h.engine.Do(context.Background(), func(w *world.World) {
		far := w.Create("Far Room", ref.TypeRoom, h.wizRef())
		if err := w.MoveTo(who, far.Ref); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()
	drainDescriptor(other)

	h.send("@wall the roof is on fire")
	if got := h.out(); !strings.Contains(got,
		`Wizard shouts, "the roof is on fire"`) {
		t.Errorf("the shouter did not hear it:\n%s", got)
	}
	if got := drainDescriptor(other); !strings.Contains(got,
		`Wizard shouts, "the roof is on fire"`) {
		t.Errorf("the other connection did not hear it:\n%s", got)
	}
}
