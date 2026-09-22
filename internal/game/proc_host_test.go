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

// makeWizardProgram creates a player and a program it owns, both flagged
// Wizard with mucker bits set. flags.MLevel() treats Wizard-plus-any-mucker-
// bit as level 4 outright (see ref.Flags.MLevel), and compileSource takes
// the lower of a program's own level and its owner's — so both need it for
// the program to actually compile at mlevel 4, which FORCE and FORCEDBY's
// generated floor require. Returns the player and the program.
func makeWizardProgram(w *world.World, name, source string) (owner, prog ref.Ref) {
	p := w.Create(name+"Owner", ref.TypePlayer, ref.Nothing)
	p.Owner = p.Ref
	p.Flags |= ref.Wizard
	p.Flags = p.Flags.SetMLevel(3)

	pr := w.Create(name+".muf", ref.TypeProgram, p.Ref)
	pr.Flags |= ref.Wizard
	pr.Flags = pr.Flags.SetMLevel(3)
	w.SetSource(pr.Ref, source)

	return p.Ref, pr.Ref
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

// TestQueuePrimitiveRunsLaterWithItsOwnArgAndCommand exercises QUEUE end to
// end: the queued program does not run until the process queue is drained,
// and when it does, its COMMAND variable is "Queued Event." while its
// initial stack argument is the string QUEUE was given — two different
// strings, unlike a command-driven program where SetReserved's own
// convention makes them the same one.
func TestQueuePrimitiveRunsLaterWithItsOwnArgAndCommand(t *testing.T) {
	h := newHarness(t)
	h.login()

	var target ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz := h.wizRef()
		p := w.Create("fired.muf", ref.TypeProgram, wiz)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, `: main
  me @ swap notify
  me @ command @ notify
;`)
		target = p.Ref
	}); err != nil {
		t.Fatal(err)
	}

	h.installProgram(t, "queuer", fmt.Sprintf(`: main
  0 #%d "myarg" queue if "queued" else "failed" then me @ swap notify
;`, int(target)))

	h.send("queuer")
	got := h.out()
	if !strings.Contains(got, "queued") {
		t.Fatalf("QUEUE should have reported success:\n%s", got)
	}
	if strings.Contains(got, "myarg") {
		t.Fatalf("the queued program should not have run yet:\n%s", got)
	}

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		h.s.Tick(w)
	}); err != nil {
		t.Fatal(err)
	}
	got = h.out()
	if !strings.Contains(got, "myarg") {
		t.Errorf("the queued program's stack argument should be \"myarg\":\n%s", got)
	}
	if !strings.Contains(got, "Queued Event.") {
		t.Errorf("the queued program's COMMAND should be \"Queued Event.\":\n%s", got)
	}
}

// TestQueueRejectedWhenPlayerProcessLimitExceeded checks that QUEUE respects
// processLimitOK the same way FORK does, and pushes 0 rather than FORK's -1.
func TestQueueRejectedWhenPlayerProcessLimitExceeded(t *testing.T) {
	h := newHarness(t)
	h.login()

	var target ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		if err := w.Tune.SetString("max_plyr_processes", "0"); err != nil {
			t.Fatal(err)
		}
		p := w.Create("never.muf", ref.TypeProgram, h.wizRef())
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": main ;")
		target = p.Ref
	}); err != nil {
		t.Fatal(err)
	}

	_, d := connectAs(t, h, "Queuer", false)
	var owner ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		r, ok := w.PlayerNamed("Queuer")
		if !ok {
			t.Fatal("Queuer not found")
		}
		owner = r
		w.Get(owner).Flags = w.Get(owner).Flags.SetMLevel(3)
		here := w.Get(owner).Location

		prog := w.Create("overqueue.muf", ref.TypeProgram, owner)
		prog.Flags = prog.Flags.SetMLevel(3)
		w.SetSource(prog.Ref, fmt.Sprintf(`: main
  0 #%d "" queue if "accepted" else "rejected" then me @ swap notify
;`, int(target)))

		e := w.Create("overqueue", ref.TypeExit, owner)
		e.Dest = []ref.Ref{prog.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	got := sendAs(t, h, d, "overqueue")
	if !strings.Contains(got, "Event killed.  Timequeue table full.") {
		t.Errorf("missing the process-limit message:\n%s", got)
	}
	if !strings.Contains(got, "rejected") {
		t.Errorf("QUEUE should have returned 0:\n%s", got)
	}
}

// TestForcePrimitiveRunsCommandAsVictim exercises FORCE end to end: a
// mlev-4 program forces a THING with no descriptor of its own to "say"
// something, and the broadcast reaches the forcing player because they are
// in the same room — proving the command genuinely ran as the victim, not
// as the forcer, since the speaker's own name in the line is the victim's.
func TestForcePrimitiveRunsCommandAsVictim(t *testing.T) {
	h := newHarness(t)
	h.login()

	var owner ref.Ref
	var victimName string
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		here := w.Get(h.wizRef()).Location

		victim := w.Create("Gizmo", ref.TypeThing, h.wizRef())
		if err := w.MoveTo(victim.Ref, here); err != nil {
			t.Fatal(err)
		}
		victimName = victim.Name

		var prog ref.Ref
		owner, prog = makeWizardProgram(w, "forcer", fmt.Sprintf(`: main
  #%d "say hello" force
;`, int(victim.Ref)))
		if err := w.MoveTo(owner, here); err != nil {
			t.Fatal(err)
		}

		e := w.Create("dothings", ref.TypeExit, owner)
		e.Dest = []ref.Ref{prog}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	d := secondSessionFor(t, h, owner)
	got := sendAs(t, h, d, "dothings")
	want := fmt.Sprintf(`%s says, "hello"`, victimName)
	if !strings.Contains(got, want) {
		t.Fatalf("expected %q in:\n%s", want, got)
	}
}

// TestForcedByReflectsTheForcingProgram exercises FORCEDBY/FORCEDBY_ARRAY
// end to end: a victim forced to run a program sees the forcing program's
// own dbref from FORCEDBY, and [program, player] from FORCEDBY_ARRAY —
// prim_force's own "if (player != program)" second push, since a program
// (not a player typing @force directly) did the forcing here.
func TestForcedByReflectsTheForcingProgram(t *testing.T) {
	h := newHarness(t)
	h.login()

	var owner ref.Ref
	var forcerProg ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz := h.wizRef()
		// FORCEDBY/FORCEDBY_ARRAY need mlevel 4, which needs the owner —
		// here, the harness's own default player — to carry mucker bits
		// too: Wizard alone caps a program's effective mlevel at 0.
		w.Get(wiz).Flags = w.Get(wiz).Flags.SetMLevel(3)
		here := w.Get(wiz).Location

		victim := w.Create("Gizmo", ref.TypeThing, wiz)
		if err := w.MoveTo(victim.Ref, here); err != nil {
			t.Fatal(err)
		}

		var prog ref.Ref
		owner, prog = makeWizardProgram(w, "forcer2", fmt.Sprintf(`: main
  #%d "report" force
;`, int(victim.Ref)))
		forcerProg = prog
		if err := w.MoveTo(owner, here); err != nil {
			t.Fatal(err)
		}

		// Notifying "me" here would be notifying the victim, Gizmo, which
		// has no descriptor of its own to receive anything on — so this
		// reports straight to owner's own dbref instead, the one real
		// connection in this test that can actually hear it.
		reporter := w.Create("report.muf", ref.TypeProgram, wiz)
		reporter.Flags |= ref.Wizard
		reporter.Flags = reporter.Flags.SetMLevel(3)
		w.SetSource(reporter.Ref, fmt.Sprintf(`: main
  forcedby intostr #%d swap notify
  forcedby_array array_count intostr #%d swap notify
  forcedby_array 0 array_getitem intostr #%d swap notify
  forcedby_array 1 array_getitem intostr #%d swap notify
;`, int(owner), int(owner), int(owner), int(owner)))
		e := w.Create("report", ref.TypeExit, h.wizRef())
		e.Dest = []ref.Ref{reporter.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}

		e2 := w.Create("dothings2", ref.TypeExit, owner)
		e2.Dest = []ref.Ref{prog}
		if err := w.MoveTo(e2.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	d := secondSessionFor(t, h, owner)
	got := sendAs(t, h, d, "dothings2")

	// intostr renders a dbref as a bare number, no '#' — matching upstream's
	// own union-agnostic print, per INTOSTR's own doc comment.
	want := fmt.Sprintf("%d\n2\n%d\n%d", forcerProg, forcerProg, owner)
	if strings.TrimRight(got, "\n") != want {
		t.Errorf("output = %q, want %q (forcedby, forcedby_array count, [0], [1])", got, want)
	}
}

// TestForcedByEmptyOutsideAForce checks the not-forced baseline: FORCEDBY
// is #-1 (NOTHING) and FORCEDBY_ARRAY is empty for a program nobody forced.
func TestForcedByEmptyOutsideAForce(t *testing.T) {
	h := newHarness(t)
	h.login()

	var ownerRef ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		var p ref.Ref
		ownerRef, p = makeWizardProgram(w, "solo", `: main
  forcedby intostr me @ swap notify
  forcedby_array array_count intostr me @ swap notify
;`)
		if err := w.MoveTo(ownerRef, here); err != nil {
			t.Fatal(err)
		}
		e := w.Create("checkforced", ref.TypeExit, ownerRef)
		e.Dest = []ref.Ref{p}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	d := secondSessionFor(t, h, ownerRef)
	got := sendAs(t, h, d, "checkforced")
	want := "-1\n0"
	if strings.TrimRight(got, "\n") != want {
		t.Errorf("output = %q, want %q (forcedby, forcedby_array count)", got, want)
	}
}

// TestAtForcePopulatesForcedByWithJustThePlayer checks that @force (not
// just the FORCE primitive) also pushes onto the shared forcelist, and that
// it pushes only the player — upstream's do_force has no "program" to push,
// unlike prim_force.
func TestAtForcePopulatesForcedByWithJustThePlayer(t *testing.T) {
	h := newHarness(t)
	h.login()

	var wiz ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz = h.wizRef()
		// FORCEDBY/FORCEDBY_ARRAY need mlevel 4; Wizard alone caps a
		// program's effective mlevel at 0 without mucker bits too.
		w.Get(wiz).Flags = w.Get(wiz).Flags.SetMLevel(3)
		here := w.Get(wiz).Location

		th := w.Create("Puppet", ref.TypeThing, wiz)
		th.Flags |= ref.XForcible
		if err := w.MoveTo(th.Ref, here); err != nil {
			t.Fatal(err)
		}

		// Notifying "me" would notify Puppet, which has no descriptor of
		// its own — report straight to wiz's own dbref instead.
		reporter := w.Create("report2.muf", ref.TypeProgram, wiz)
		reporter.Flags |= ref.Wizard
		reporter.Flags = reporter.Flags.SetMLevel(3)
		w.SetSource(reporter.Ref, fmt.Sprintf(`: main
  forcedby intostr #%d swap notify
  forcedby_array array_count intostr #%d swap notify
;`, int(wiz), int(wiz)))
		e := w.Create("report2", ref.TypeExit, wiz)
		e.Dest = []ref.Ref{reporter.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	h.send("@force Puppet=report2")
	got := h.out()
	want := fmt.Sprintf("%d\n1", wiz)
	if strings.TrimRight(got, "\n") != want {
		t.Errorf("output = %q, want %q (forcedby, forcedby_array count)", got, want)
	}
}

// TestGetPIDsMatchesByPlayerProgramOrNegativeArgument exercises GETPIDS end
// to end: a program blocked on READ (so it stays in procQueue for the whole
// test), matched by its player; a nonexistent dbref, matching nothing; and
// -1, matching everything — which in this architecture includes the
// currently-running querying process itself, unlike upstream's own
// timequeue (see GETPIDS's own doc comment in prim_proc.go).
func TestGetPIDsMatchesByPlayerProgramOrNegativeArgument(t *testing.T) {
	h := newHarness(t)
	h.login()

	waiter, d := connectAs(t, h, "Waiter", false)
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		w.Get(waiter).Flags = w.Get(waiter).Flags.SetMLevel(2)
		here := w.Get(waiter).Location
		p := w.Create("waits3.muf", ref.TypeProgram, waiter)
		p.Flags = p.Flags.SetMLevel(2)
		w.SetSource(p.Ref, `: main
  me @ "waiting" notify
  read
  pop
;`)
		e := w.Create("waits3", ref.TypeExit, waiter)
		e.Dest = []ref.Ref{p.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	sendAs(t, h, d, "waits3")

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		if len(h.s.procs.all()) != 1 {
			t.Fatalf("expected exactly one suspended process, got %d", len(h.s.procs.all()))
		}
	}); err != nil {
		t.Fatal(err)
	}

	var owner ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		var checker ref.Ref
		owner, checker = makeWizardProgram(w, "getpids", fmt.Sprintf(`: main
  #%d getpids array_count intostr me @ swap notify
  #99999 getpids array_count intostr me @ swap notify
  #-1 getpids array_count 1 >= intostr me @ swap notify
;`, int(waiter)))
		if err := w.MoveTo(owner, here); err != nil {
			t.Fatal(err)
		}
		e := w.Create("getpids", ref.TypeExit, owner)
		e.Dest = []ref.Ref{checker}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	d2 := secondSessionFor(t, h, owner)
	got := sendAs(t, h, d2, "getpids")
	want := "1\n0\n1"
	if strings.TrimRight(got, "\n") != want {
		t.Errorf("output = %q, want %q (matches by player, no match, -1 matches at least one)", got, want)
	}
}

// TestGetPIDInfoOtherPIDReportsReadState checks mufHost.PIDInfo end to end:
// a program at mlevel 3 inspects a sibling process blocked on READ, and gets
// back SUBTYPE "READ", CALLED_DATA "READ", and MLEVEL hardcoded to 0 — the
// same documented quirk upstream's own get_pidinfo has.
func TestGetPIDInfoOtherPIDReportsReadState(t *testing.T) {
	h := newHarness(t)
	h.login()

	waiter, d := connectAs(t, h, "Waiter", false)
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		w.Get(waiter).Flags = w.Get(waiter).Flags.SetMLevel(2)
		here := w.Get(waiter).Location
		p := w.Create("waits4.muf", ref.TypeProgram, waiter)
		p.Flags = p.Flags.SetMLevel(2)
		w.SetSource(p.Ref, `: main
  me @ "waiting" notify
  read
  pop
;`)
		e := w.Create("waits4", ref.TypeExit, waiter)
		e.Dest = []ref.Ref{p.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	sendAs(t, h, d, "waits4")

	var waiterPID int
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		procs := h.s.procs.all()
		if len(procs) != 1 {
			t.Fatalf("expected exactly one suspended process, got %d", len(procs))
		}
		waiterPID = procs[0].pid
	}); err != nil {
		t.Fatal(err)
	}

	var owner ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		var checker ref.Ref
		owner, checker = makeWizardProgram(w, "getpidinfo", fmt.Sprintf(`: main
  %d getpidinfo "SUBTYPE" [] me @ swap notify
  %d getpidinfo "CALLED_DATA" [] me @ swap notify
  %d getpidinfo "MLEVEL" [] intostr me @ swap notify
  %d getpidinfo array_count intostr me @ swap notify
;`, waiterPID, waiterPID, waiterPID, waiterPID))
		if err := w.MoveTo(owner, here); err != nil {
			t.Fatal(err)
		}
		e := w.Create("getpidinfo", ref.TypeExit, owner)
		e.Dest = []ref.Ref{checker}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	d2 := secondSessionFor(t, h, owner)
	got := sendAs(t, h, d2, "getpidinfo")
	want := "READ\nREAD\n0\n14"
	if strings.TrimRight(got, "\n") != want {
		t.Errorf("output = %q, want %q (SUBTYPE, CALLED_DATA, MLEVEL, key count)", got, want)
	}
}

// TestWatchPIDDeliversProcExitOnCompletion is an end-to-end test of the
// whole delivery path: a watcher blocked in EVENT_WAITFOR resumes
// immediately — within the same engine.Do the killer's own command runs in,
// not on the next tick — when the process it WATCHPID'd is killed, carrying
// the PROC.EXIT.<pid> event name and the dead pid as data, exactly as
// EVENT_WAITFOR leaves them on the stack.
func TestWatchPIDDeliversProcExitOnCompletion(t *testing.T) {
	h := newHarness(t)
	h.login()

	target, d := connectAs(t, h, "Target", false)
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		w.Get(target).Flags = w.Get(target).Flags.SetMLevel(2)
		here := w.Get(target).Location
		p := w.Create("waits6.muf", ref.TypeProgram, target)
		p.Flags = p.Flags.SetMLevel(2)
		w.SetSource(p.Ref, `: main
  me @ "waiting" notify
  read
  pop
;`)
		e := w.Create("waits6", ref.TypeExit, target)
		e.Dest = []ref.Ref{p.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	sendAs(t, h, d, "waits6")

	var targetPID int
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		procs := h.s.procs.all()
		if len(procs) != 1 {
			t.Fatalf("expected exactly one suspended process, got %d", len(procs))
		}
		targetPID = procs[0].pid
	}); err != nil {
		t.Fatal(err)
	}

	watcher, wd := connectAs(t, h, "Watcher", false)
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		w.Get(watcher).Flags = w.Get(watcher).Flags.SetMLevel(3)
		here := w.Get(watcher).Location
		p := w.Create("watches.muf", ref.TypeProgram, watcher)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, fmt.Sprintf(`: main
  %d watchpid
  { "PROC.EXIT.%d" }list event_waitfor
  "name:" swap strcat me @ swap notify
  intostr "data:" swap strcat me @ swap notify
;`, targetPID, targetPID))
		e := w.Create("watches", ref.TypeExit, watcher)
		e.Dest = []ref.Ref{p.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	sendAs(t, h, wd, "watches")

	var owner ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		var checker ref.Ref
		owner, checker = makeWizardProgram(w, "killtarget", fmt.Sprintf(`: main
  %d kill pop
;`, targetPID))
		if err := w.MoveTo(owner, here); err != nil {
			t.Fatal(err)
		}
		e := w.Create("killtarget", ref.TypeExit, owner)
		e.Dest = []ref.Ref{checker}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	d2 := secondSessionFor(t, h, owner)
	sendAs(t, h, d2, "killtarget")

	got := drainDescriptor(wd)
	want := fmt.Sprintf("name:PROC.EXIT.%d\ndata:%d", targetPID, targetPID)
	if strings.TrimRight(got, "\n") != want {
		t.Errorf("watcher output = %q, want %q", got, want)
	}
}

// TestTimerWakesAWaitingProgram covers the half of TIMER_START the golden
// harness cannot: a timer that is not already due when EVENT_WAITFOR asks
// for it, so the program genuinely suspends and a later tick delivers the
// event. The golden case uses a zero delay instead, because the two servers
// run their timequeues at different intervals.
func TestTimerWakesAWaitingProgram(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.installProgram(t, "ticker", `: main
  me @ "before" notify
  0 "t" timer_start
  { "TIMER.t" }list event_waitfor
  pop pop
  me @ "after" notify
;`)
	h.send("ticker")
	if got := h.out(); !strings.Contains(got, "before") {
		t.Fatalf("the program did not start:\n%s", got)
	}
	if got := h.out(); strings.Contains(got, "after") {
		t.Fatalf("the program did not wait for its timer:\n%s", got)
	}

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		h.s.Tick(w)
	}); err != nil {
		t.Fatal(err)
	}
	if got := h.out(); !strings.Contains(got, "after") {
		t.Errorf("the timer did not wake the program:\n%s", got)
	}
}

// TestTimerStopCancelsBeforeItFires checks that a stopped timer delivers
// nothing, even once its deadline has passed.
func TestTimerStopCancelsBeforeItFires(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.installProgram(t, "cancels", `: main
  0 "t" timer_start
  "t" timer_stop
  0 sleep
  "TIMER.t" event_exists intostr me @ swap notify
;`)
	h.send("cancels")
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		h.s.Tick(w)
	}); err != nil {
		t.Fatal(err)
	}
	if got := h.out(); !strings.Contains(got, "0") {
		t.Errorf("a cancelled timer still delivered its event:\n%s", got)
	}
}
