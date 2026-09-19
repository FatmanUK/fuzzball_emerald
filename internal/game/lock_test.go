package game

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestLockedAgainstWizardAndStranger exercises LOCKED? end to end: a MUF
// program calling it against a thing locked to the wizard only. The wizard
// should pass (a dbref lock's CONST checks player == the locked-to dbref);
// an unrelated player should not.
func TestLockedAgainstWizardAndStranger(t *testing.T) {
	h := newHarness(t)
	h.login()

	var thing, wiz, other ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz = h.wizRef()
		here := w.Get(wiz).Location

		th := w.Create("gizmo", ref.TypeThing, wiz)
		if err := w.MoveTo(th.Ref, here); err != nil {
			t.Fatal(err)
		}
		thing = th.Ref

		otherP := w.Create("Rando", ref.TypePlayer, ref.Nothing)
		otherP.Owner = otherP.Ref
		other = otherP.Ref

		w.SetProp(thing, propLock, props.Value{Type: props.Lock, Str: fmt.Sprintf("#%d", int(wiz))})
	}); err != nil {
		t.Fatal(err)
	}

	h.installProgram(t, "checklock2", fmt.Sprintf(`: main
  #%d #%d LOCKED? if "wizard:locked" else "wizard:open" then me @ swap notify
  #%d #%d LOCKED? if "other:locked" else "other:open" then me @ swap notify
;`, int(wiz), int(thing), int(other), int(thing)))

	h.send("checklock2")
	got := h.out()
	if !strings.Contains(got, "wizard:open") {
		t.Errorf("wizard should pass its own lock:\n%s", got)
	}
	if !strings.Contains(got, "other:locked") {
		t.Errorf("an unrelated player should not pass the lock:\n%s", got)
	}
}

// TestLockStringPrimitives exercises SETLOCKSTR, GETLOCKSTR, PARSELOCK,
// UNPARSELOCK and PRETTYLOCK together through a real MUF program: setting a
// lock string, reading it back, round-tripping it through PARSELOCK/
// UNPARSELOCK, and rendering it human-readably with PRETTYLOCK.
func TestLockStringPrimitives(t *testing.T) {
	h := newHarness(t)
	h.login()

	var thing, wiz ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz = h.wizRef()
		here := w.Get(wiz).Location
		th := w.Create("gizmo", ref.TypeThing, wiz)
		if err := w.MoveTo(th.Ref, here); err != nil {
			t.Fatal(err)
		}
		thing = th.Ref
	}); err != nil {
		t.Fatal(err)
	}

	h.installProgram(t, "checklockstr", fmt.Sprintf(`: main
  #%d "#%d" SETLOCKSTR if "set:ok" else "set:fail" then me @ swap notify
  #%d GETLOCKSTR me @ swap notify
  "#%d" PARSELOCK UNPARSELOCK me @ swap notify
  "#%d" PARSELOCK PRETTYLOCK me @ swap notify
  #%d "" SETLOCKSTR if "clear:ok" else "clear:fail" then me @ swap notify
  #%d GETLOCKSTR me @ swap notify
;`, int(thing), int(wiz), int(thing), int(wiz), int(wiz), int(thing), int(thing)))

	h.send("checklockstr")
	got := h.out()

	for _, want := range []string{
		"set:ok",
		fmt.Sprintf("#%d", int(wiz)),
		"Wizard(#" + fmt.Sprintf("%d", int(wiz)), // PRETTYLOCK's fullname rendering
		"clear:ok",
		"*UNLOCKED*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestCouldDoitUnlinkedExit checks the exit-specific branch of couldDoit
// directly: an exit with no destinations at all can never be done.
func TestCouldDoitUnlinkedExit(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz := h.wizRef()
		here := w.Get(wiz).Location
		e := w.Create("nowhere", ref.TypeExit, wiz)
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}

		if couldDoit(h.s, w, h.d.ID, 1, wiz, e.Ref) {
			t.Error("an unlinked exit should never be doable")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCouldDoitNilLinkChecksLock checks that a NIL-linked exit falls straight
// through to its own @lock, skipping the destination checks entirely.
func TestCouldDoitNilLinkChecksLock(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz := h.wizRef()
		here := w.Get(wiz).Location
		e := w.Create("message-only", ref.TypeExit, wiz)
		e.Dest = []ref.Ref{ref.Nil}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}

		if !couldDoit(h.s, w, h.d.ID, 1, wiz, e.Ref) {
			t.Error("a NIL-linked exit with no lock set should pass")
		}

		w.SetProp(e.Ref, propLock, props.Value{Type: props.Lock, Str: "#-1"})
		if couldDoit(h.s, w, h.d.ID, 1, wiz, e.Ref) {
			t.Error("a NIL-linked exit locked to NOTHING should never pass")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCouldDoitPlayerDestRequiresJumpOK checks the JUMP_OK gate on a
// player-linked exit: the destination player must allow being jumped to.
func TestCouldDoitPlayerDestRequiresJumpOK(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz := h.wizRef()
		here := w.Get(wiz).Location

		target := w.Create("Target", ref.TypePlayer, ref.Nothing)
		target.Owner = target.Ref
		targetHome := w.Create("Target's Room", ref.TypeRoom, target.Ref)
		target.Home = targetHome.Ref
		if err := w.MoveTo(target.Ref, targetHome.Ref); err != nil {
			t.Fatal(err)
		}

		e := w.Create("totarget", ref.TypeExit, wiz)
		e.Dest = []ref.Ref{target.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}

		if couldDoit(h.s, w, h.d.ID, 1, wiz, e.Ref) {
			t.Error("jumping to a player without JUMP_OK should not be doable")
		}

		target.Flags |= ref.JumpOK
		if !couldDoit(h.s, w, h.d.ID, 1, wiz, e.Ref) {
			t.Error("jumping to a JUMP_OK player should be doable")
		}
	}); err != nil {
		t.Fatal(err)
	}
}
