package game

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestMoveToRoomNeedsCanTeleportTo covers the half of MOVETO's room
// reparent that no golden case can reach.
//
// p_db.c:160 is
//
//	(mlev < 3) && (!permissions(ProgUID, victim)
//	    || !can_teleport_to(ProgUID, dest))
//
// and the two tests are not the same question. permissions() is pure
// ownership with no wizard escape at all (interp.c:2706), where
// can_teleport_to goes through controls() — which **every wizard
// passes for everything**. The oracle drives #1, so its
// can_teleport_to is always true and the clause is dead there: a
// mutation removing it survives every transcript.
//
// So this runs a mucker-1 program owned by a mortal, reparenting a
// room they own into one they neither own nor may link to.
func TestMoveToRoomNeedsCanTeleportTo(t *testing.T) {
	h := newHarness(t)
	h.login()

	who, d := connectAs(t, h, "Mortal", false)

	var hall, vault, mine ref.Ref
	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(who).Location

		// A stranger's room, with neither LINK_OK nor ABODE,
		// so can_teleport_to has nothing to let the mortal
		// through on.
		other := w.Create("Stranger", ref.TypePlayer,
			ref.Nothing)
		other.Owner = other.Ref
		v := w.Create("Vault", ref.TypeRoom, other.Ref)
		if err := w.MoveTo(v.Ref,
			ref.GlobalEnvironment); err != nil {
			t.Fatal(err)
		}
		vault = v.Ref

		// Two rooms the mortal owns: the one being
		// reparented, and the control destination.
		hl := w.Create("Hall", ref.TypeRoom, who)
		if err := w.MoveTo(hl.Ref,
			ref.GlobalEnvironment); err != nil {
			t.Fatal(err)
		}
		hall = hl.Ref
		mn := w.Create("Mine", ref.TypeRoom, who)
		if err := w.MoveTo(mn.Ref,
			ref.GlobalEnvironment); err != nil {
			t.Fatal(err)
		}
		mine = mn.Ref

		prog := w.Create("reparent.muf", ref.TypeProgram, who)
		prog.Flags = prog.Flags.SetMLevel(1)
		w.SetSource(prog.Ref, fmt.Sprintf(`: main
  0 try %s %s moveto "allowed" me @ swap notify
  catch me @ swap notify endcatch
;`, hall, vault))
		e := w.Create("reparent", ref.TypeExit, who)
		e.Dest = []ref.Ref{prog.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	got := sendAs(t, h, d, "reparent")
	if !strings.Contains(got, "Permission denied.") {
		t.Errorf("a mortal reparented a room into one they "+
			"cannot link to:\n%s", got)
	}
	if err := h.engine.Do(ctx, func(w *world.World) {
		loc := w.Get(hall).Location
		if loc != ref.GlobalEnvironment {
			t.Errorf("the hall moved to %v anyway", loc)
		}
	}); err != nil {
		t.Fatal(err)
	}

	// The control: the same program reparenting into a room the
	// mortal *does* own, which can_teleport_to permits through
	// controls(). Without this the test would pass just as well
	// against a MOVETO that refused every reparent.
	if err := h.engine.Do(ctx, func(w *world.World) {
		p := w.Get(hall)
		_ = p
		for r := ref.Ref(0); r <= w.Top(); r++ {
			o := w.Get(r)
			if o == nil || o.Type() != ref.TypeProgram ||
				o.Name != "reparent.muf" {
				continue
			}
			w.SetSource(r, fmt.Sprintf(`: main
  0 try %s %s moveto "allowed" me @ swap notify
  catch me @ swap notify endcatch
;`, hall, mine))
			h.s.InvalidateProgram(r)
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	got = sendAs(t, h, d, "reparent")
	if !strings.Contains(got, "allowed") {
		t.Errorf("a mortal should reparent into their own "+
			"room:\n%s", got)
	}
}
