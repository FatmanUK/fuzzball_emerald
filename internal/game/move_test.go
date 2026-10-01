package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestSelfLinkedExitDoesNotCrash covers the one deliberate divergence
// in trigger: upstream recurses through a metalink with nothing to
// stop it, so an exit linked to itself exhausts the C stack and takes
// the server down. There is no wording to compare against, because
// upstream has no answer to compare.
func TestSelfLinkedExitDoesNotCrash(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		e := w.Create("ouroboros", ref.TypeExit, h.wizRef())
		// Pointing at itself, which @link does not refuse.
		e.Dest = []ref.Ref{e.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("ouroboros")
	if got := h.out(); !strings.Contains(got,
		"Exit aborted because of metalink loop.") {
		t.Errorf("a self-linked exit said:\n%s", got)
	}

	// And the counter comes back down, or every later exit in the
	// session would be refused.
	if h.s.metaDepth != 0 {
		t.Errorf("metaDepth left at %d", h.s.metaDepth)
	}
}
