package game

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// makeProgrammer creates a player with mucker level 3, so a program it owns
// compiles with a real effective mlevel rather than being capped at 0 the
// way the harness's own "Wizard" player (Wizard|Builder, no mucker bits) —
// see CLAUDE.md's "a wizard with no mucker bits has level 0" — would cap it.
func makeProgrammer(w *world.World, name string) ref.Ref {
	p := w.Create(name, ref.TypePlayer, ref.Nothing)
	p.Owner = p.Ref
	p.Flags = p.Flags.SetMLevel(3)
	return p.Ref
}

// TestCanCallOwnerAlwaysReaches checks that CanCall's permission gate — the
// owner/wizard/Linkable check, separate from the public's own mlev floor —
// lets the target's own owner through even without being a wizard or the
// program being Linkable.
func TestCanCallOwnerAlwaysReaches(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := makeProgrammer(w, "Owner")
		p := w.Create("lib.muf", ref.TypeProgram, owner)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": foo ;\npublic foo\n")

		host := &mufHost{s: h.s, w: w}
		if !host.CanCall(1, owner, p.Ref, "foo") {
			t.Error("the owner should reach a declared public despite not being a wizard")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCanCallMlevelFourAlwaysReaches checks the other unconditional branch:
// a level-4 caller passes even for someone else's non-linkable program.
func TestCanCallMlevelFourAlwaysReaches(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := makeProgrammer(w, "Owner")
		p := w.Create("lib.muf", ref.TypeProgram, owner)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": foo ;\npublic foo\n")

		stranger := w.Create("Stranger", ref.TypePlayer, ref.Nothing)
		stranger.Owner = stranger.Ref

		host := &mufHost{s: h.s, w: w}
		if !host.CanCall(4, stranger.Ref, p.Ref, "foo") {
			t.Error("a level-4 caller should reach any declared public")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCanCallDeniesNonLinkableStranger checks the permission gate's failing
// case: a low-level caller who neither owns the program nor finds it
// Linkable is refused before the public table is even consulted.
func TestCanCallDeniesNonLinkableStranger(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := makeProgrammer(w, "Owner")
		p := w.Create("lib.muf", ref.TypeProgram, owner)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": foo ;\npublic foo\n")

		stranger := w.Create("Stranger", ref.TypePlayer, ref.Nothing)
		stranger.Owner = stranger.Ref

		host := &mufHost{s: h.s, w: w}
		if host.CanCall(1, stranger.Ref, p.Ref, "foo") {
			t.Error("a non-owning, non-wizard, non-linkable caller should be refused")
		}

		p.Flags |= ref.LinkOK
		if !host.CanCall(1, stranger.Ref, p.Ref, "foo") {
			t.Error("a LinkOK program should let a stranger call its publics")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCanCallRespectsThePublicsOwnMlevelFloor checks that reaching the
// permission gate is not enough on its own: the caller must also meet the
// mlev the public itself was declared at (1 for "public", 4 for "wizcall").
func TestCanCallRespectsThePublicsOwnMlevelFloor(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := makeProgrammer(w, "Owner")
		p := w.Create("lib.muf", ref.TypeProgram, owner)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": foo ;\nwizcall foo\n")

		host := &mufHost{s: h.s, w: w}
		if host.CanCall(3, owner, p.Ref, "foo") {
			t.Error("a wizcall public should refuse a level-3 caller")
		}
		if !host.CanCall(4, owner, p.Ref, "foo") {
			t.Error("a wizcall public should accept a level-4 caller")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCanCallIsCaseInsensitiveAndCompilesOnDemand checks the two mechanical
// details prim_cancallp handles itself: the public name is matched with
// strcasecmp, and a program that has never been run yet still answers
// correctly because CanCall compiles it rather than assuming it already is.
func TestCanCallIsCaseInsensitiveAndCompilesOnDemand(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := makeProgrammer(w, "Owner")
		p := w.Create("lib.muf", ref.TypeProgram, owner)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": foo ;\npublic foo\n")

		host := &mufHost{s: h.s, w: w}
		if !host.CanCall(1, owner, p.Ref, "FOO") {
			t.Error("the public name should be matched case-insensitively")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCanCallMissingNameFails checks that a name the target never declared
// public reports false rather than erroring.
func TestCanCallMissingNameFails(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := makeProgrammer(w, "Owner")
		p := w.Create("lib.muf", ref.TypeProgram, owner)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": foo ;\npublic foo\n")

		host := &mufHost{s: h.s, w: w}
		if host.CanCall(3, owner, p.Ref, "bar") {
			t.Error("an undeclared name should not be callable")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCanCallUncompiledMlevelZeroFails checks that ProgMLevel(target) > 0 is
// its own unconditional gate: a program whose effective mlevel is 0 — here,
// because its owner has no mucker bits — can never be called, even by its
// own owner.
func TestCanCallUncompiledMlevelZeroFails(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := w.Create("NoBits", ref.TypePlayer, ref.Nothing)
		owner.Owner = owner.Ref
		p := w.Create("lib.muf", ref.TypeProgram, owner.Ref)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": foo ;\npublic foo\n")

		host := &mufHost{s: h.s, w: w}
		if host.CanCall(4, owner.Ref, p.Ref, "foo") {
			t.Error("a program whose owner grants it mlevel 0 should never be callable")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestControlsProcessOwnPlayerControls checks the third of control_process's
// three OR'd branches: the process's own player controls it, even without
// owning its program or trigger.
func TestControlsProcessOwnPlayerControls(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		player := w.Create("Runner", ref.TypePlayer, ref.Nothing)
		player.Owner = player.Ref
		prog := w.Create("lib.muf", ref.TypeProgram, h.wizRef())

		pid := h.s.procs.add(&process{player: player.Ref, program: prog.Ref, trigger: ref.Nothing})

		host := &mufHost{s: h.s, w: w}
		if !host.ControlsProcess(player.Ref, pid) {
			t.Error("the process's own player should control it")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestControlsProcessProgramOrTriggerOwnerControls checks the other two
// branches: controlling the process's program, or its trigger, is each
// enough on its own, independent of who the process is running for.
func TestControlsProcessProgramOrTriggerOwnerControls(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		player := w.Create("Runner", ref.TypePlayer, ref.Nothing)
		player.Owner = player.Ref

		progOwner := w.Create("ProgOwner", ref.TypePlayer, ref.Nothing)
		progOwner.Owner = progOwner.Ref
		prog := w.Create("lib.muf", ref.TypeProgram, progOwner.Ref)

		trigOwner := w.Create("TrigOwner", ref.TypePlayer, ref.Nothing)
		trigOwner.Owner = trigOwner.Ref
		trig := w.Create("lever", ref.TypeThing, trigOwner.Ref)

		host := &mufHost{s: h.s, w: w}

		pid1 := h.s.procs.add(&process{player: player.Ref, program: prog.Ref, trigger: ref.Nothing})
		if !host.ControlsProcess(progOwner.Ref, pid1) {
			t.Error("the program's owner should control the process")
		}

		pid2 := h.s.procs.add(&process{player: player.Ref, program: prog.Ref, trigger: trig.Ref})
		if !host.ControlsProcess(trigOwner.Ref, pid2) {
			t.Error("the trigger's owner should control the process")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestControlsProcessDeniesAStranger checks the failing case: someone who
// controls neither the process's program nor its trigger, and is not the
// player it runs for, does not control it.
func TestControlsProcessDeniesAStranger(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		player := w.Create("Runner", ref.TypePlayer, ref.Nothing)
		player.Owner = player.Ref
		prog := w.Create("lib.muf", ref.TypeProgram, h.wizRef())

		stranger := w.Create("Stranger", ref.TypePlayer, ref.Nothing)
		stranger.Owner = stranger.Ref

		pid := h.s.procs.add(&process{player: player.Ref, program: prog.Ref, trigger: ref.Nothing})

		host := &mufHost{s: h.s, w: w}
		if host.ControlsProcess(stranger.Ref, pid) {
			t.Error("an unrelated player should not control the process")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestControlsProcessMissingPIDIsFalse checks that a pid naming no process
// at all answers false, the same as upstream's control_process falling
// through both the timequeue and event-queue searches empty-handed.
func TestControlsProcessMissingPIDIsFalse(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		host := &mufHost{s: h.s, w: w}
		if host.ControlsProcess(h.wizRef(), 99999) {
			t.Error("a nonexistent pid should never be controlled")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestKillPIDRemovesAnExistingProcess and TestKillPIDMissingIsFalse check
// KillPID's dequeue_process semantics directly.
func TestKillPIDRemovesAnExistingProcess(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		pid := h.s.procs.add(&process{player: h.wizRef()})

		host := &mufHost{s: h.s, w: w}
		if !host.KillPID(pid) {
			t.Error("KillPID should report success for a live pid")
		}
		if h.s.procs.get(pid) != nil {
			t.Error("the process should be gone afterward")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

func TestKillPIDMissingIsFalse(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		host := &mufHost{s: h.s, w: w}
		if host.KillPID(99999) {
			t.Error("KillPID on a nonexistent pid should report false")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestKillPrimitiveStopsASuspendedProgram exercises KILL end to end: a
// program suspended on READ, killed by its pid from a second, separate
// player's own program, never resumes even though its player later types
// the line it was waiting for.
//
// The killer has to be a separate connection: the target process is
// suspended on READ, and a line sent on its own connection would be
// swallowed as that READ's input rather than reaching the command parser —
// see CLAUDE.md's "A line typed while a program is reading goes to that
// program, not the command parser."
func TestKillPrimitiveStopsASuspendedProgram(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.installProgram(t, "waits", `: main
  me @ "waiting" notify
  read
  "Hello, " swap strcat me @ swap notify
;`)
	h.send("waits")
	h.out()

	var pid int
	killer, d := connectAs(t, h, "Reaper", false)
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		procs := h.s.procs.all()
		if len(procs) != 1 {
			t.Fatalf("expected exactly one suspended process, got %d", len(procs))
		}
		pid = procs[0].pid

		w.Get(killer).Flags = w.Get(killer).Flags.SetMLevel(3)
		here := w.Get(killer).Location

		prog := w.Create("reap.muf", ref.TypeProgram, killer)
		prog.Flags = prog.Flags.SetMLevel(3)
		w.SetSource(prog.Ref, fmt.Sprintf(`: main
  %d kill if "killed" else "notkilled" then me @ swap notify
;`, pid))

		e := w.Create("reap", ref.TypeExit, killer)
		e.Dest = []ref.Ref{prog.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	if got := sendAs(t, h, d, "reap"); !strings.Contains(got, "killed") {
		t.Fatalf("KILL should have reported success:\n%s", got)
	}

	h.send("Igor")
	if got := h.out(); strings.Contains(got, "Hello, Igor") {
		t.Errorf("the killed program should not have resumed:\n%s", got)
	}
}
