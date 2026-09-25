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

// TestExitLockBlocksTraversal checks that useExit actually enforces
// an exit's @lock now, rather than moving the player through
// unconditionally.
func TestExitLockBlocksTraversal(t *testing.T) {
	h := newHarness(t)
	h.login()

	var exit ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz := h.wizRef()
		here := w.Get(wiz).Location
		cellar := w.Create("Cellar", ref.TypeRoom, wiz)
		e := w.Create("down", ref.TypeExit, wiz)
		e.Dest = []ref.Ref{cellar.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
		exit = e.Ref
		// Locked to NOTHING, which never passes for anyone.
		w.SetProp(exit, propLock, props.Value{Type: props.Lock, Str: "#-1"})
	}); err != nil {
		t.Fatal(err)
	}

	h.send("down")
	got := h.out()
	if !strings.Contains(got, "You can't go that way.") {
		t.Fatalf("locked exit should refuse passage:\n%s", got)
	}
	if strings.Contains(got, "Cellar") {
		t.Fatalf("player should not have moved:\n%s", got)
	}

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		w.SetProp(exit, propLock, props.Value{Type: props.Lock, Str: fmt.Sprintf("#%d", int(h.wizRef()))})
	}); err != nil {
		t.Fatal(err)
	}

	h.send("down")
	got = h.out()
	if !strings.Contains(got, "Cellar") {
		t.Fatalf("an exit locked to the player should let them through:\n%s", got)
	}
}

// TestExitFailMessages checks that a locked exit's own @fail/@ofail
// messages are shown instead of the default, when set.
func TestExitFailMessages(t *testing.T) {
	h := newHarness(t)
	h.login()

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz := h.wizRef()
		here := w.Get(wiz).Location
		cellar := w.Create("Cellar", ref.TypeRoom, wiz)
		e := w.Create("down", ref.TypeExit, wiz)
		e.Dest = []ref.Ref{cellar.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
		w.SetProp(e.Ref, propLock, props.Value{Type: props.Lock, Str: "#-1"})
		w.SetProp(e.Ref, propFail, props.Value{Type: props.String, Str: "It won't budge."})
	}); err != nil {
		t.Fatal(err)
	}

	h.send("down")
	got := h.out()
	if !strings.Contains(got, "It won't budge.") {
		t.Fatalf("expected the exit's own @fail message:\n%s", got)
	}
	if strings.Contains(got, "You can't go that way.") {
		t.Fatalf("the default fail message should not also show:\n%s", got)
	}
}

// TestLockedAgainstWizardAndStranger exercises LOCKED? end to end: a
// MUF program calling it against a thing locked to the wizard only.
// The wizard should pass (a dbref lock's CONST checks player == the
// locked-to dbref); an unrelated player should not.
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

// TestLockStringPrimitives exercises SETLOCKSTR, GETLOCKSTR,
// PARSELOCK, UNPARSELOCK and PRETTYLOCK together through a real MUF
// program: setting a lock string, reading it back, round-tripping it
// through PARSELOCK/ UNPARSELOCK, and rendering it human-readably
// with PRETTYLOCK.
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

// TestSetLockStringForwardsMatchFailure checks that SETLOCKSTR's
// "silent" call to _set_lock still shows a match failure's own
// message, even though it suppresses _set_lock's own "Lock set."/"I
// don't understand that key."
func TestSetLockStringForwardsMatchFailure(t *testing.T) {
	h := newHarness(t)
	h.login()

	var thing ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz := h.wizRef()
		here := w.Get(wiz).Location
		th := w.Create("gizmo", ref.TypeThing, wiz)
		if err := w.MoveTo(th.Ref, here); err != nil {
			t.Fatal(err)
		}
		thing = th.Ref
	}); err != nil {
		t.Fatal(err)
	}

	h.installProgram(t, "badkey", fmt.Sprintf(`: main
  #%d "nosuchthingatall" SETLOCKSTR if "ok" else "fail" then me @ swap notify
;`, int(thing)))

	h.send("badkey")
	got := h.out()
	if !strings.Contains(got, "I don't see nosuchthingatall here.") {
		t.Fatalf("missing the match-failure message:\n%s", got)
	}
	if strings.Contains(got, "I don't understand that key.") {
		t.Fatalf("SETLOCKSTR should stay silent about its own generic message:\n%s", got)
	}
	if !strings.Contains(got, "fail") {
		t.Fatalf("SETLOCKSTR should report failure:\n%s", got)
	}
}

// TestCouldDoitUnlinkedExit checks the exit-specific branch of
// couldDoit directly: an exit with no destinations at all can never
// be done.
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

// TestCouldDoitNilLinkChecksLock checks that a NIL-linked exit falls
// straight through to its own @lock, skipping the destination checks
// entirely.
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
// player-linked exit: the destination player must allow being jumped
// to.
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

// TestLockProgramSeesTheSupplicant pins which object SUPPLICANT
// answers with inside a program used as a lock constant.
//
// It is whoever is being *tested* — the primitive's whole purpose
// is letting a lock program ask who is asking. This used to answer
// with the locked object instead, which no lock could have made use
// of.
func TestLockProgramSeesTheSupplicant(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	var prog, gate, mortal ref.Ref
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location

		p := w.Create("lockprog.muf", ref.TypeProgram,
			h.wizRef())
		p.Flags = p.Flags.SetMLevel(3)
		prog = p.Ref

		g := w.Create("gate", ref.TypeThing, h.wizRef())
		if err := w.MoveTo(g.Ref, here); err != nil {
			t.Error(err)
		}
		gate = g.Ref

		m := w.Create("Mortal", ref.TypePlayer, ref.Nothing)
		m.Owner = m.Ref
		mortal = m.Ref
	})
	if err != nil {
		t.Fatal(err)
	}

	// The program reports to the wizard, who is the connected
	// player: the frame's own "me" is the supplicant, who is not.
	src := sprintf(": main %s supplicant \"%%d\" fmtstring "+
		"notify 1 ;\n", h.wizRef().String())
	err = h.engine.Do(ctx, func(w *world.World) {
		w.SaveSource(prog, src)
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	err = h.engine.Do(ctx, func(w *world.World) {
		host := &lockHost{s: h.s, w: w}
		host.RunLock(h.d.ID, mortal, prog, gate)
	})
	if err != nil {
		t.Fatal(err)
	}
	got := h.out()
	if !strings.Contains(got, mortal.String()) {
		t.Errorf("SUPPLICANT reported %q, want the tested "+
			"player %v", got, mortal)
	}
	if strings.Contains(got, gate.String()) {
		t.Errorf("SUPPLICANT gave the locked object %v: %q",
			gate, got)
	}
}
