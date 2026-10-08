package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestLoweringAProgramsMuckerLevelTakesEffect is the bug the
// mucker-floor golden case turned up, and it is the dangerous
// direction of it.
//
// find_mlev is computed when a frame is made, from the flags, so
// upstream honours "@set prog=1" on the program's very next run.
// Emerald passed the level to the *compiler* and carried it on the
// compiled muf.Program, which Server.programs then caches for the
// life of the process -- and nothing invalidates that cache on a flag
// change. So a mucker-4 program demoted to 1 went on running at 4
// until something happened to recompile it.
//
// Invalidating the cache instead would not have been enough: the
// owner's level caps the program's, so changing one player's bits
// would mean finding every program they own.
//
// The program is run twice with its level dropped in between, and
// what tells the two apart is a primitive the table gates at 3.
func TestLoweringAProgramsMuckerLevelTakesEffect(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	var prog ref.Ref
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		p := w.Create("probe.muf", ref.TypeProgram,
			h.wizRef())
		p.Flags = p.Flags.SetMLevel(3)
		prog = p.Ref
		// NEXTPROP is gated at mucker 3 by the table, so it
		// answers one way at 3 and refuses at 1. The result
		// is thrown away: what is being read is whether the
		// call was refused.
		w.SetSource(p.Ref, `: main
  0 try #0 "_sys" nextprop pop "ran" me @ swap notify
  catch me @ swap notify endcatch
;`)
		e := w.Create("probe", ref.TypeExit, h.wizRef())
		e.Dest = []ref.Ref{p.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("probe")
	atThree := h.out()
	if strings.Contains(atThree, "Permission denied") {
		t.Fatalf("mucker 3 should reach NEXTPROP:\n%s",
			atThree)
	}

	// Drop it to mucker 1. The compile is already cached, which
	// is the whole point.
	if err := h.engine.Do(ctx, func(w *world.World) {
		o := w.Get(prog)
		o.Flags = o.Flags.SetMLevel(1)
		w.Modified(prog)
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("probe")
	atOne := h.out()
	if !strings.Contains(atOne, "Permission denied") {
		t.Errorf("a demoted program still ran at its old "+
			"level:\n%s", atOne)
	}
}
