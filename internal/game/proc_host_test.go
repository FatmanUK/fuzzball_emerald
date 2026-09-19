package game

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// secondSessionFor connects a second descriptor bound to an already-existing
// player, for a test that needs one player to be reachable on two
// descriptors at once — e.g. one busy in a READ while the other sends
// commands, which a single descriptor cannot do since a line typed mid-READ
// goes to the reading program rather than the command parser.
func secondSessionFor(t *testing.T, h *harness, who ref.Ref) *session.Descriptor {
	t.Helper()
	d, err := h.s.Connect(session.TransportLine, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		h.s.Hub().Bind(d, who, w.Now())
	}); err != nil {
		t.Fatal(err)
	}
	drainDescriptor(d)
	t.Cleanup(func() { d.Close() })
	return d
}

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

// TestKillBelowMlevelThreeStillWorksForTheProcessesOwnPlayer pins the fix to
// internal/muf/internal/gen/gen_mlev.py this session made: KILL's own
// "mlev < 3 && !control_process(...)" is a conditional check, not a flat
// floor, but the generator's exemption regex did not recognise
// "control_process(" (only "controls(") and so had wrongly generated
// "KILL": 3 as an unconditional floor — which would have silently blocked
// this exact case, a mlev-2 player killing their own suspended program,
// with the wrong ("Permission denied.", generic) message before KILL's own
// ownership-aware check ever ran.
func TestKillBelowMlevelThreeStillWorksForTheProcessesOwnPlayer(t *testing.T) {
	h := newHarness(t)
	h.login()

	who, d := connectAs(t, h, "SoloPlayer", false)
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		// Mucker level 2: enough to call NOTIFY, still below KILL's
		// conditional floor of 3.
		w.Get(who).Flags = w.Get(who).Flags.SetMLevel(2)
		here := w.Get(who).Location

		waiter := w.Create("waits2.muf", ref.TypeProgram, who)
		waiter.Flags = waiter.Flags.SetMLevel(2)
		w.SetSource(waiter.Ref, `: main
  me @ "waiting" notify
  read
  pop
;`)
		e1 := w.Create("waits2", ref.TypeExit, who)
		e1.Dest = []ref.Ref{waiter.Ref}
		if err := w.MoveTo(e1.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	sendAs(t, h, d, "waits2")

	var pid int
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		procs := h.s.procs.all()
		if len(procs) != 1 {
			t.Fatalf("expected exactly one suspended process, got %d", len(procs))
		}
		pid = procs[0].pid

		here := w.Get(who).Location
		killer := w.Create("selfkill.muf", ref.TypeProgram, who)
		killer.Flags = killer.Flags.SetMLevel(2)
		w.SetSource(killer.Ref, fmt.Sprintf(`: main
  %d kill if "killed" else "notkilled" then me @ swap notify
;`, pid))
		e2 := w.Create("selfkill", ref.TypeExit, who)
		e2.Dest = []ref.Ref{killer.Ref}
		if err := w.MoveTo(e2.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	// A second descriptor for the same player: d's first descriptor is still
	// mid-READ in "waits2", and would swallow "selfkill" as that READ's
	// input rather than letting it reach the command parser.
	d2 := secondSessionFor(t, h, who)
	got := sendAs(t, h, d2, "selfkill")
	if !strings.Contains(got, "killed") {
		t.Fatalf("a mlev-2 player should be able to KILL their own process:\n%s", got)
	}
	if strings.Contains(got, "Permission denied") {
		t.Fatalf("KILL should not be blocked by a generic mlev floor here:\n%s", got)
	}
}

// TestForkPrimitiveRunsParentAndChildIndependently exercises FORK end to
// end: the parent finishes its command and reports its child's pid; the
// child does not run until the process queue is drained (the test harness
// deliberately does not wire Engine.OnEachOp — see BOOTSTRAP.md), and then
// reports its own distinct output, proving the two frames are genuinely
// independent rather than one clobbering the other's stack or variables.
func TestForkPrimitiveRunsParentAndChildIndependently(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.installProgram(t, "splitter", `: main
  "shared" var! label
  fork if
    "parent:" label @ strcat me @ swap notify
  else
    label @ "changed-by-child" label !
    "child:" label @ strcat me @ swap notify
  then
;`)

	h.send("splitter")
	got := h.out()
	if !strings.Contains(got, "parent:shared") {
		t.Fatalf("the parent should report immediately:\n%s", got)
	}
	if strings.Contains(got, "child:") {
		t.Fatalf("the child should not have run yet:\n%s", got)
	}

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		h.s.Tick(w)
	}); err != nil {
		t.Fatal(err)
	}
	got = h.out()
	if !strings.Contains(got, "child:changed-by-child") {
		t.Errorf("the child did not run independently:\n%s", got)
	}
}

// TestForkRejectsBelowMlevelThree checks the primitive's own gate end to
// end, matching upstream's "MUCKER level 3" requirement noted in
// prim_fork's doc comment. The message is the dispatcher's own generic
// "Permission denied." (see internal/muf/prim.go's primitive()), not
// upstream's differently-cased literal — FORK's floor is unconditional, so
// it is gated by primMLevel rather than a hand-written check; see FORK's own
// doc comment in internal/muf/prim_proc.go for why that is the convention
// this codebase already uses for every other unconditional-floor primitive.
func TestForkRejectsBelowMlevelThree(t *testing.T) {
	h := newHarness(t)
	h.login()

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := w.Create("NoBits", ref.TypePlayer, ref.Nothing)
		owner.Owner = owner.Ref
		here := w.Get(h.wizRef()).Location

		prog := w.Create("weak.muf", ref.TypeProgram, owner.Ref)
		prog.Flags = prog.Flags.SetMLevel(3)
		w.SetSource(prog.Ref, ": main fork if \"ok\" else \"ok\" then me @ swap notify ;")

		e := w.Create("weak", ref.TypeExit, owner.Ref)
		e.Dest = []ref.Ref{prog.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	h.send("weak")
	got := h.out()
	if !strings.Contains(got, "Permission denied.") {
		t.Fatalf("a program whose owner has no mucker bits should be refused:\n%s", got)
	}
}

// TestForkRejectedWhenPlayerProcessLimitExceeded checks processLimitOK's
// max_plyr_processes gate, and its own documented off-by-one against
// upstream: the parent's own currently-running process already counts
// against the limit here, unlike upstream's tqhead-only count. Run as a
// non-wizard: the harness's own default player is flagged Wizard, which
// max_plyr_processes deliberately exempts.
func TestForkRejectedWhenPlayerProcessLimitExceeded(t *testing.T) {
	h := newHarness(t)
	h.login()

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		if err := w.Tune.SetString("max_plyr_processes", "0"); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	_, d := connectAs(t, h, "Forker", false)
	var owner ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		r, ok := w.PlayerNamed("Forker")
		if !ok {
			t.Fatal("Forker not found")
		}
		owner = r
		w.Get(owner).Flags = w.Get(owner).Flags.SetMLevel(3)
		here := w.Get(owner).Location

		prog := w.Create("overfork.muf", ref.TypeProgram, owner)
		prog.Flags = prog.Flags.SetMLevel(3)
		w.SetSource(prog.Ref, `: main
  fork -1 = if
    "rejected" me @ swap notify
  else
    "accepted" me @ swap notify
  then
;`)

		e := w.Create("overfork", ref.TypeExit, owner)
		e.Dest = []ref.Ref{prog.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	got := sendAs(t, h, d, "overfork")
	if !strings.Contains(got, "Event killed.  Timequeue table full.") {
		t.Errorf("missing the process-limit message:\n%s", got)
	}
	if !strings.Contains(got, "rejected") {
		t.Errorf("FORK should have returned -1:\n%s", got)
	}
}
