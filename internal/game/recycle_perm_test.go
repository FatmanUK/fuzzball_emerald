package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// do_recycle's refusals. The outer one is controls()'s, and the four
// below it are each a different sentence for the same condition.
const (
	recNoControl = "Permission denied. (You don't " +
		"control what you want to recycle)"
	recNoRoom = "Permission denied. (You don't control " +
		"the room you want to recycle)"
	recNoThing = "Permission denied. (You can't recycle " +
		"a thing you don't control)"
	recNoExit = "Permission denied. (You may not recycle an " +
		"exit you don't own)"
	recNoProg = "Permission denied. (You can't recycle a " +
		"program you don't own)"
)

// TestRecycleIsStricterThanControls is the divergence this commit
// exists for, and it is the one that let this server do *more* than
// upstream rather than less.
//
// do_recycle checks controls() and then, per type, demands
// `OWNER(thing) == OWNER(player)` as well (create.c:875 onwards). A
// wizard passes controls() for everything and so used to recycle
// anything; upstream refuses them whatever they are unless they
// actually own it, with a different sentence per type.
//
// The player here stays a **wizard**, which is the whole point: the
// outer gate must pass so the inner one is what refuses.
func TestRecycleIsStricterThanControls(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		other := w.Create("Stranger", ref.TypePlayer,
			ref.Nothing)
		other.Owner = other.Ref
		here := w.Get(h.wizRef()).Location

		room := w.Create("Theirs", ref.TypeRoom, other.Ref)
		if err := w.MoveTo(room.Ref,
			ref.GlobalEnvironment); err != nil {
			t.Error(err)
		}
		thing := w.Create("locket", ref.TypeThing, other.Ref)
		thing.Home = here
		if err := w.MoveTo(thing.Ref, here); err != nil {
			t.Error(err)
		}
		exit := w.Create("door", ref.TypeExit, other.Ref)
		exit.Dest = []ref.Ref{here}
		if err := w.MoveTo(exit.Ref, here); err != nil {
			t.Error(err)
		}
		prog := w.Create("spell", ref.TypeProgram, other.Ref)
		if err := w.MoveTo(prog.Ref, here); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	for _, tc := range []struct{ cmd, want string }{
		{"@recycle Theirs", recNoRoom},
		{"@recycle locket", recNoThing},
		{"@recycle door", recNoExit},
		{"@recycle spell", recNoProg},
	} {
		h.send(tc.cmd)
		got := h.out()
		if !strings.Contains(got, tc.want) {
			t.Errorf("%q said:\n%s\nwant %q", tc.cmd, got,
				tc.want)
		}
		// A wizard passes controls(), so the *outer* refusal
		// must not be what fired — that would mean the
		// per-type rule never ran.
		if strings.Contains(got, recNoControl) {
			t.Errorf("%q used the outer refusal:\n%s",
				tc.cmd, got)
		}
		if strings.Contains(got, "Thank you for recycling") {
			t.Errorf("%q recycled somebody else's "+
				"object:\n%s", tc.cmd, got)
		}
	}
}

// TestRecycleRefusesAMortalOnTheOuterGate is the other half:
// controls() still comes first, and a mortal who controls nothing
// gets its wording rather than a per-type one.
func TestRecycleRefusesAMortalOnTheOuterGate(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		other := w.Create("Stranger", ref.TypePlayer,
			ref.Nothing)
		other.Owner = other.Ref
		here := w.Get(h.wizRef()).Location
		thing := w.Create("locket", ref.TypeThing, other.Ref)
		thing.Home = here
		if err := w.MoveTo(thing.Ref, here); err != nil {
			t.Error(err)
		}
		w.Get(h.wizRef()).Flags &^= ref.Wizard
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("@recycle locket")
	got := h.out()
	if !strings.Contains(got, recNoControl) {
		t.Errorf("want %q, got:\n%s", recNoControl, got)
	}
	// The old bare wording, and match_controlled's, are both
	// wrong here.
	if strings.Contains(got, "what was matched") {
		t.Errorf("claims match_controlled:\n%s", got)
	}
}

// TestRecycleStillWorksForTheOwner is the control: the per-type
// ownership test must not stop somebody recycling their own things.
func TestRecycleStillWorksForTheOwner(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		o := w.Create("locket", ref.TypeThing, h.wizRef())
		o.Home = here
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("@recycle locket")
	got := h.out()
	if !strings.Contains(got, "Thank you for recycling") {
		t.Errorf("the owner should recycle:\n%s", got)
	}
}

// The fork inside the outer refusal — a wizard who fails controls()
// being told "That's already garbage!" rather than that they may not
// touch it — is **not** covered. Reaching it needs controls() to
// refuse a wizard, which happens only under strict_god_priv against
// God's property, and the harness drives #1, who is God. A test that
// set it up and then asserted nothing useful was written and thrown
// away: a vacuous test is worse than none.

// TestRecycleGarbageSaysSo is the plain case, which does not depend
// on who is asking.
func TestRecycleGarbageSaysSo(t *testing.T) {
	h := newHarness(t)
	h.login()

	var junk ref.Ref
	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		o := w.Create("junk", ref.TypeThing, h.wizRef())
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Error(err)
		}
		junk = o.Ref
		if err := w.Recycle(o.Ref); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("@recycle " + junk.String())
	got := h.out()
	if !strings.Contains(got, "That's already garbage!") {
		t.Errorf("want the garbage answer, got:\n%s", got)
	}
}
