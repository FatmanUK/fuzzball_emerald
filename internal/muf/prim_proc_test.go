package muf

import (
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

func TestPIDPushesFramesOwnPID(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.PID = 42

	if _, err := prims[PrimNumber("PID")](f); err != nil {
		t.Fatalf("PID: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != 42 {
		t.Fatalf("result = %+v, want 42", v)
	}
}

func TestIsPIDMatchesOwnPIDWithoutHostCall(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.PID = 7
	if err := f.Push(Int(7)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("ISPID?")](f); err != nil {
		t.Fatalf("ISPID?: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != 1 {
		t.Fatalf("result = %+v, want true", v)
	}
}

func TestIsPIDAsksHostForOtherPIDs(t *testing.T) {
	h := newLockTestHost()
	h.isPIDResult = true
	f := newTestFrame(h)
	f.PID = 7
	if err := f.Push(Int(8)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("ISPID?")](f); err != nil {
		t.Fatalf("ISPID?: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != 1 {
		t.Fatalf("result = %+v, want true", v)
	}
}

func TestIsPIDRejectsNonInteger(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	if err := f.Push(Str("nope")); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("ISPID?")](f); err == nil {
		t.Fatal("ISPID? with a string argument should fail")
	}
}

func TestForceLevelPushesHostValue(t *testing.T) {
	h := newLockTestHost()
	h.forceLevel = 3
	f := newTestFrame(h)

	if _, err := prims[PrimNumber("FORCE_LEVEL")](f); err != nil {
		t.Fatalf("FORCE_LEVEL: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != 3 {
		t.Fatalf("result = %+v, want 3", v)
	}
}

func TestInstancesRequiresAProgram(t *testing.T) {
	h := newLockTestHost()
	h.types[testThing] = ref.TypeThing
	h.valid[testThing] = true
	f := newTestFrame(h)
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("INSTANCES")](f); err == nil {
		t.Fatal("INSTANCES on a non-program should fail")
	}
}

func TestInstancesPushesHostCount(t *testing.T) {
	h := newLockTestHost()
	h.types[testProgram] = ref.TypeProgram
	h.valid[testProgram] = true
	h.instancesResult = 5
	f := newTestFrame(h)
	if err := f.Push(Obj(testProgram)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("INSTANCES")](f); err != nil {
		t.Fatalf("INSTANCES: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != 5 {
		t.Fatalf("result = %+v, want 5", v)
	}
}

func TestSupplicantDefaultsToNothing(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.Supplicant = ref.Nothing

	if _, err := prims[PrimNumber("SUPPLICANT")](f); err != nil {
		t.Fatalf("SUPPLICANT: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeObject || v.Ref != ref.Nothing {
		t.Fatalf("result = %+v, want #%d", v, ref.Nothing)
	}
}

func TestSupplicantReturnsWhatWasSet(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.Supplicant = testThing

	if _, err := prims[PrimNumber("SUPPLICANT")](f); err != nil {
		t.Fatalf("SUPPLICANT: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeObject || v.Ref != testThing {
		t.Fatalf("result = %+v, want #%d", v, testThing)
	}
}

const testProgram2 ref.Ref = 100

func TestCanCallRejectsNonProgramArg(t *testing.T) {
	h := newLockTestHost()
	h.types[testThing] = ref.TypeThing
	h.valid[testThing] = true
	f := newTestFrame(h)
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("foo")); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("CANCALL?")](f)
	if err == nil || err.Error() != "Invalid program dbref argument. (1)" {
		t.Fatalf("err = %v, want the invalid-program message", err)
	}
}

func TestCanCallRejectsEmptyName(t *testing.T) {
	h := newLockTestHost()
	h.types[testProgram2] = ref.TypeProgram
	h.valid[testProgram2] = true
	f := newTestFrame(h)
	if err := f.Push(Obj(testProgram2)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("")); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("CANCALL?")](f)
	if err == nil || err.Error() != "Invalid string argument. Must be non-null. (2)" {
		t.Fatalf("err = %v, want the invalid-string message", err)
	}
}

func TestCanCallForwardsToHostWithProgUID(t *testing.T) {
	h := newLockTestHost()
	h.types[testProgram2] = ref.TypeProgram
	h.valid[testProgram2] = true
	h.canCallResult = true
	h.owner[10] = 20 // f.Caller's owner, for progUID at mlev >= 2

	f := newTestFrame(h)
	f.Caller = 10
	if err := f.Push(Obj(testProgram2)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("foo")); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("CANCALL?")](f); err != nil {
		t.Fatalf("CANCALL?: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != 1 {
		t.Fatalf("result = %+v, want true", v)
	}

	if len(h.canCallCalls) != 1 {
		t.Fatalf("CanCall called %d times, want 1", len(h.canCallCalls))
	}
	call := h.canCallCalls[0]
	if call.callerLevel != 3 || call.callerUID != 20 || call.prog != testProgram2 || call.name != "foo" {
		t.Fatalf("unexpected call: %+v", call)
	}
}

func TestKillRejectsNonInteger(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	if err := f.Push(Str("nope")); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("KILL")](f)
	if err == nil || err.Error() != "Non-integer argument (1)." {
		t.Fatalf("err = %v, want the non-integer message", err)
	}
}

// TestKillOwnPIDAbortsSilently checks upstream's do_abort_silent special
// case: killing your own pid ends the program via errSilentAbort rather than
// an ordinary error, and never reaches Host.KillPID.
func TestKillOwnPIDAbortsSilently(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.PID = 9
	if err := f.Push(Int(9)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("KILL")](f)
	if err != errSilentAbort {
		t.Fatalf("err = %v, want errSilentAbort", err)
	}
	if len(h.killPIDCalls) != 0 {
		t.Fatalf("KillPID should not be called for a self-kill")
	}
}

func TestKillDeniedBelowMlevelThreeWithoutControl(t *testing.T) {
	h := newLockTestHost()
	h.controlsProcessResult = false
	f := newTestFrame(h)
	f.Prog.MLevel = 2
	f.PID = 1
	if err := f.Push(Int(5)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("KILL")](f)
	if err == nil || err.Error() != "Permission Denied." {
		t.Fatalf("err = %v, want the permission-denied message", err)
	}
	if len(h.killPIDCalls) != 0 {
		t.Fatalf("KillPID should not be called once permission is denied")
	}
}

func TestKillAllowedBelowMlevelThreeWithControl(t *testing.T) {
	h := newLockTestHost()
	h.controlsProcessResult = true
	h.killPIDResult = true
	f := newTestFrame(h)
	f.Prog.MLevel = 2
	f.PID = 1
	if err := f.Push(Int(5)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("KILL")](f); err != nil {
		t.Fatalf("KILL: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != 1 {
		t.Fatalf("result = %+v, want true", v)
	}
	if len(h.killPIDCalls) != 1 || h.killPIDCalls[0] != 5 {
		t.Fatalf("KillPID calls = %v, want [5]", h.killPIDCalls)
	}
}

func TestKillAtMlevelThreeSkipsControlCheck(t *testing.T) {
	h := newLockTestHost()
	h.controlsProcessResult = false
	h.killPIDResult = false
	f := newTestFrame(h)
	f.Prog.MLevel = 3
	f.PID = 1
	if err := f.Push(Int(999)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("KILL")](f); err != nil {
		t.Fatalf("KILL: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != 0 {
		t.Fatalf("result = %+v, want false (pid does not exist)", v)
	}
	if len(h.controlsProcessCalls) != 0 {
		t.Fatalf("ControlsProcess should not be consulted at mlev 3")
	}
}

// TestForkGatedByGeneratedMlevFloor checks FORK's mlev gate at the layer it
// actually runs at: mlev_gen.go's "FORK": 3 (a genuine unconditional floor,
// unlike KILL's own ownership-escaping check), enforced by the dispatcher in
// primitive() before FORK's own primFunc ever runs — which is why FORK's
// primFunc has no mlev check of its own to test directly, unlike KILL's.
func TestForkGatedByGeneratedMlevFloor(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.Prog.MLevel = 2

	_, err := f.primitive(PrimNumber("FORK"))
	if err == nil || err.Error() != "Permission denied." {
		t.Fatalf("err = %v, want the dispatcher's generic message", err)
	}
	if len(h.forkCalls) != 0 {
		t.Fatal("Host.Fork should not be called below mlevel 3")
	}
}

// TestForkBuildsAnIndependentChildStartingAfterItself checks the pieces
// FORK's own primFunc is responsible for, on top of fork()'s own tested
// deep-copy: the child's PC lands one past the FORK instruction, it starts
// with a 0 already on its stack, and it is the exact frame handed to
// Host.Fork — not some other copy.
func TestForkBuildsAnIndependentChildStartingAfterItself(t *testing.T) {
	h := newLockTestHost()
	h.forkResult = 7
	f := newTestFrame(h)
	f.Prog.MLevel = 3
	f.PC = 10
	f.Stack = []Value{Int(123)}

	if _, err := prims[PrimNumber("FORK")](f); err != nil {
		t.Fatalf("FORK: %v", err)
	}

	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != 7 {
		t.Fatalf("parent result = %+v, want the child's pid, 7", v)
	}
	// The primitive dispatcher advances f.PC itself after a plain primFunc
	// returns; FORK's own primFunc must not have touched it, so it is still
	// sitting on the FORK instruction here.
	if f.PC != 10 {
		t.Fatalf("f.PC = %d, want 10 (unadvanced — the dispatcher moves it)", f.PC)
	}

	if len(h.forkCalls) != 1 {
		t.Fatalf("Host.Fork called %d times, want 1", len(h.forkCalls))
	}
	child := h.forkCalls[0]
	if child.PC != 11 {
		t.Errorf("child.PC = %d, want 11 (one past FORK)", child.PC)
	}
	// The parent's own stack (still holding its 123) must be untouched; the
	// child got its own copy plus the pushed 0.
	if len(f.Stack) != 1 || f.Stack[0].Num != 123 {
		t.Errorf("parent stack = %+v, should be unaffected by the fork", f.Stack)
	}
	if len(child.Stack) != 2 || child.Stack[0].Num != 123 || child.Stack[1].Num != 0 {
		t.Errorf("child stack = %+v, want [123 0]", child.Stack)
	}
}

func TestForkPushesMinusOneWhenTheHostRefuses(t *testing.T) {
	h := newLockTestHost()
	h.forkResult = 0
	f := newTestFrame(h)
	f.Prog.MLevel = 3

	if _, err := prims[PrimNumber("FORK")](f); err != nil {
		t.Fatalf("FORK: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != -1 {
		t.Fatalf("result = %+v, want -1", v)
	}
}

func TestQueueRejectsNonIntegerSeconds(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	if err := f.Push(Str("not seconds")); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Obj(testProgram2)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("arg")); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("QUEUE")](f)
	if err == nil || err.Error() != "Non-integer argument (1)." {
		t.Fatalf("err = %v, want the non-integer message", err)
	}
	if len(h.queueCalls) != 0 {
		t.Fatal("Host.Queue should not be called with a bad seconds argument")
	}
}

func TestQueueRejectsNonProgramArg(t *testing.T) {
	h := newLockTestHost()
	h.types[testThing] = ref.TypeThing
	h.valid[testThing] = true
	f := newTestFrame(h)
	if err := f.Push(Int(5)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("arg")); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("QUEUE")](f)
	if err == nil || err.Error() != "Invalid program dbref argument (2)." {
		t.Fatalf("err = %v, want the invalid-program message", err)
	}
	if len(h.queueCalls) != 0 {
		t.Fatal("Host.Queue should not be called with a bad program argument")
	}
}

func TestQueueForwardsToHostAndPushesResult(t *testing.T) {
	h := newLockTestHost()
	h.types[testProgram2] = ref.TypeProgram
	h.valid[testProgram2] = true
	h.queueResult = 42
	f := newTestFrame(h)
	f.Descr = 9
	if err := f.Push(Int(5)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Obj(testProgram2)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("hello")); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("QUEUE")](f); err != nil {
		t.Fatalf("QUEUE: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != 42 {
		t.Fatalf("result = %+v, want 42", v)
	}

	if len(h.queueCalls) != 1 {
		t.Fatalf("Queue called %d times, want 1", len(h.queueCalls))
	}
	call := h.queueCalls[0]
	if call.descr != 9 || call.prog != testProgram2 || call.seconds != 5 || call.arg != "hello" {
		t.Fatalf("unexpected call: %+v", call)
	}
}

// TestQueueTreatsANonStringArgAsEmpty mirrors upstream's own type-blind read
// of oper1->data.string: any non-string value's Str field is Go's zero value
// "" anyway, so QUEUE need not (and does not) check the type explicitly.
func TestQueueTreatsANonStringArgAsEmpty(t *testing.T) {
	h := newLockTestHost()
	h.types[testProgram2] = ref.TypeProgram
	h.valid[testProgram2] = true
	f := newTestFrame(h)
	if err := f.Push(Int(0)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Obj(testProgram2)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Int(123)); err != nil { // not a string
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("QUEUE")](f); err != nil {
		t.Fatalf("QUEUE: %v", err)
	}
	if len(h.queueCalls) != 1 || h.queueCalls[0].arg != "" {
		t.Fatalf("queueCalls = %+v, want arg \"\"", h.queueCalls)
	}
}

func TestForceRejectsRecursionGuard(t *testing.T) {
	h := newLockTestHost()
	h.types[testPlayer] = ref.TypePlayer
	h.valid[testPlayer] = true
	f := newTestFrame(h)
	f.Prog.MLevel = 4
	f.Level = 9
	if err := f.Push(Obj(testPlayer)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("look")); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("FORCE")](f)
	if err == nil || err.Error() != "Interp call loops not allowed." {
		t.Fatalf("err = %v, want the recursion-guard message", err)
	}
	if len(h.forceCalls) != 0 {
		t.Fatal("Host.Force should not run once the guard trips")
	}
}

func TestForceRejectsNonStringCommand(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.Prog.MLevel = 4
	if err := f.Push(Obj(testPlayer)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Int(0)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("FORCE")](f)
	if err == nil || err.Error() != "Non-string argument (2)." {
		t.Fatalf("err = %v, want the non-string message", err)
	}
}

func TestForceRejectsInvalidVictim(t *testing.T) {
	h := newLockTestHost()
	h.types[testExit] = ref.TypeExit
	h.valid[testExit] = true
	f := newTestFrame(h)
	f.Prog.MLevel = 4
	if err := f.Push(Obj(testExit)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("look")); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("FORCE")](f)
	if err == nil || err.Error() != "Invalid player or thing argument (1)." {
		t.Fatalf("err = %v, want the invalid-victim message", err)
	}
}

func TestForceRejectsEmptyCommand(t *testing.T) {
	h := newLockTestHost()
	h.types[testPlayer] = ref.TypePlayer
	h.valid[testPlayer] = true
	f := newTestFrame(h)
	f.Prog.MLevel = 4
	if err := f.Push(Obj(testPlayer)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("")); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("FORCE")](f)
	if err == nil || err.Error() != "Empty command argument (2)." {
		t.Fatalf("err = %v, want the empty-command message", err)
	}
}

func TestForceRejectsCarriageReturn(t *testing.T) {
	h := newLockTestHost()
	h.types[testPlayer] = ref.TypePlayer
	h.valid[testPlayer] = true
	f := newTestFrame(h)
	f.Prog.MLevel = 4
	if err := f.Push(Obj(testPlayer)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("look\rme")); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("FORCE")](f)
	if err == nil || err.Error() != "Carriage returns not allowed in command string. (2)." {
		t.Fatalf("err = %v, want the carriage-return message", err)
	}
}

func TestForceRejectsGodUnlessOwnedByGod(t *testing.T) {
	h := newLockTestHost()
	h.types[ref.God] = ref.TypePlayer
	h.valid[ref.God] = true
	h.owner[testProgram] = testPlayer // program's owner is not God
	f := newTestFrame(h)
	f.Prog.MLevel = 4
	if err := f.Push(Obj(ref.God)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("look")); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("FORCE")](f)
	if err == nil || err.Error() != "Cannot force god (1)." {
		t.Fatalf("err = %v, want the cannot-force-god message", err)
	}
	if len(h.forceCalls) != 0 {
		t.Fatal("Host.Force should not run when forcing god is refused")
	}
}

func TestForceAllowsGodWhenProgramOwnedByGod(t *testing.T) {
	h := newLockTestHost()
	h.types[ref.God] = ref.TypePlayer
	h.valid[ref.God] = true
	h.owner[testProgram] = ref.God
	f := newTestFrame(h)
	f.Prog.MLevel = 4
	if err := f.Push(Obj(ref.God)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("look")); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("FORCE")](f); err != nil {
		t.Fatalf("FORCE: %v", err)
	}
	if len(h.forceCalls) != 1 {
		t.Fatal("Host.Force should run when the program is God's own")
	}
}

func TestForceForwardsToHostWithNoStackEffect(t *testing.T) {
	h := newLockTestHost()
	h.types[testThing] = ref.TypeThing
	h.valid[testThing] = true
	f := newTestFrame(h)
	f.Prog.MLevel = 4
	f.Caller = 20
	f.Descr = 5
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("look")); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("FORCE")](f); err != nil {
		t.Fatalf("FORCE: %v", err)
	}
	if f.Depth() != 0 {
		t.Fatalf("stack depth = %d, want 0 — FORCE consumes both arguments and pushes nothing", f.Depth())
	}
	if len(h.forceCalls) != 1 {
		t.Fatalf("Force called %d times, want 1", len(h.forceCalls))
	}
	call := h.forceCalls[0]
	if call.descr != 5 || call.player != 20 || call.program != testProgram || call.victim != testThing || call.command != "look" {
		t.Fatalf("unexpected call: %+v", call)
	}
}

func TestForcedByPushesHostResult(t *testing.T) {
	h := newLockTestHost()
	h.forcedByResult = testPlayer
	f := newTestFrame(h)
	f.Prog.MLevel = 4

	if _, err := prims[PrimNumber("FORCEDBY")](f); err != nil {
		t.Fatalf("FORCEDBY: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeObject || v.Ref != testPlayer {
		t.Fatalf("result = %+v, want #%d", v, testPlayer)
	}
}

func TestForcedByArrayPushesHostResult(t *testing.T) {
	h := newLockTestHost()
	h.forcedByArrayResult = []ref.Ref{testProgram, testPlayer}
	f := newTestFrame(h)
	f.Prog.MLevel = 4

	if _, err := prims[PrimNumber("FORCEDBY_ARRAY")](f); err != nil {
		t.Fatalf("FORCEDBY_ARRAY: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeArray || v.Array == nil {
		t.Fatalf("result = %+v, want an array", v)
	}
	vals := v.Array.Values()
	if len(vals) != 2 || vals[0].Ref != testProgram || vals[1].Ref != testPlayer {
		t.Fatalf("array = %+v, want [%d %d]", vals, testProgram, testPlayer)
	}
}

// TestForceFamilyRejectsBelowMlevelFourWithWizbitWording pins the wording
// discovered via golden: FORCE, FORCEDBY and FORCEDBY_ARRAY all abort with
// upstream's own "Wizbit only primitive.", not the generic dispatcher
// message most other level-4 primitives get (see gen_mlev.py's
// CUSTOM_ABORT_MESSAGE) — which is also why these three check their own
// mlev inline instead of relying on primMLevel/mlev_gen.go.
func TestForceFamilyRejectsBelowMlevelFourWithWizbitWording(t *testing.T) {
	for _, name := range []string{"FORCE", "FORCEDBY", "FORCEDBY_ARRAY"} {
		t.Run(name, func(t *testing.T) {
			h := newLockTestHost()
			f := newTestFrame(h)
			f.Prog.MLevel = 3
			if name == "FORCE" {
				if err := f.Push(Obj(testPlayer)); err != nil {
					t.Fatal(err)
				}
				if err := f.Push(Str("look")); err != nil {
					t.Fatal(err)
				}
			}

			_, err := prims[PrimNumber(name)](f)
			if err == nil || err.Error() != "Wizbit only primitive." {
				t.Fatalf("err = %v, want the wizbit-only message", err)
			}
			if len(h.forceCalls) != 0 {
				t.Fatal("Host.Force should not run below mlevel 4")
			}
		})
	}
}

func TestGetPIDsRejectsBelowMlevelThreeWithItsOwnWording(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.Prog.MLevel = 2
	if err := f.Push(Obj(testPlayer)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("GETPIDS")](f)
	if err == nil || err.Error() != "Permission denied.  Requires Mucker Level 3." {
		t.Fatalf("err = %v, want the mucker-level-3 message", err)
	}
	if len(h.getPIDsCalls) != 0 {
		t.Fatal("Host.GetPIDs should not run below mlevel 3")
	}
}

func TestGetPIDsRejectsNonObjectArg(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	if err := f.Push(Int(5)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("GETPIDS")](f)
	if err == nil || err.Error() != "Non-object argument (1)" {
		t.Fatalf("err = %v, want the non-object message", err)
	}
}

func TestGetPIDsForwardsArgAndPushesResult(t *testing.T) {
	h := newLockTestHost()
	h.getPIDsResult = []int{3, 7, 12}
	f := newTestFrame(h)
	if err := f.Push(Obj(testProgram2)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("GETPIDS")](f); err != nil {
		t.Fatalf("GETPIDS: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeArray || v.Array == nil {
		t.Fatalf("result = %+v, want an array", v)
	}
	vals := v.Array.Values()
	if len(vals) != 3 || vals[0].Num != 3 || vals[1].Num != 7 || vals[2].Num != 12 {
		t.Fatalf("array = %+v, want [3 7 12]", vals)
	}

	if len(h.getPIDsCalls) != 1 || h.getPIDsCalls[0].obj != testProgram2 {
		t.Fatalf("GetPIDs calls = %+v, want obj = #%d", h.getPIDsCalls, testProgram2)
	}
}

// TestGetPIDsAppendsOwnPIDOnlyForItsOwnProgram checks GETPIDS's own final
// step, upstream's "if (program == ref) push fr->pid": the calling frame's
// pid is appended only when the argument is exactly the calling program's
// own ref, not for any other match — including upstream's "ref < 0"
// wildcard, which golden caught this not applying to.
func TestGetPIDsAppendsOwnPIDOnlyForItsOwnProgram(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.PID = 42

	if err := f.Push(Obj(testProgram)); err != nil { // testProgram == f.Prog.Ref
		t.Fatal(err)
	}
	if _, err := prims[PrimNumber("GETPIDS")](f); err != nil {
		t.Fatalf("GETPIDS: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	vals := v.Array.Values()
	if len(vals) != 1 || vals[0].Num != 42 {
		t.Fatalf("array = %+v, want [42] (own pid appended)", vals)
	}
	if len(h.getPIDsCalls) != 1 || h.getPIDsCalls[0].selfPID != 42 {
		t.Fatalf("GetPIDs should have been asked to exclude pid 42: %+v", h.getPIDsCalls)
	}

	// The wildcard match does not trigger the append — only an exact match
	// on the calling program's own ref does.
	h2 := newLockTestHost()
	f2 := newTestFrame(h2)
	f2.PID = 7
	if err := f2.Push(Obj(ref.Nothing)); err != nil {
		t.Fatal(err)
	}
	if _, err := prims[PrimNumber("GETPIDS")](f2); err != nil {
		t.Fatalf("GETPIDS: %v", err)
	}
	v2, err := f2.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if len(v2.Array.Values()) != 0 {
		t.Fatalf("array = %+v, want empty — #-1 should not append the caller's own pid", v2.Array.Values())
	}
}

// TestGetPIDsAcceptsAnyDbrefIncludingNonexistent checks that, unlike most
// other primitives taking an object argument, GETPIDS does not validate the
// dbref exists — upstream's own prim_getpids checks only that the value is
// dbref-typed at all, per its "no permission checking is done" doc comment
// on get_pids.
func TestGetPIDsAcceptsAnyDbrefIncludingNonexistent(t *testing.T) {
	h := newLockTestHost() // testThing is deliberately never marked valid
	f := newTestFrame(h)
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("GETPIDS")](f); err != nil {
		t.Fatalf("GETPIDS should not require the dbref to exist: %v", err)
	}
}

// TestGetPIDInfoRejectsNonIntegerArg checks GETPIDINFO's own argument-type
// check, upstream's "Non-integer argument (1)" — no trailing period, unlike
// most of this file's other argument-type messages, matching the C exactly.
func TestGetPIDInfoRejectsNonIntegerArg(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	if err := f.Push(Obj(testProgram)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("GETPIDINFO")](f)
	if err == nil || err.Error() != "Non-integer argument (1)" {
		t.Fatalf("err = %v, want Non-integer argument (1)", err)
	}
}

// TestGetPIDInfoRejectsBelowMlevelThreeForOtherPid checks GETPIDINFO's own
// conditional mlev check, upstream's "mlev < 3 && oper1->data.number !=
// fr->pid" — a program below mucker level 3 may not inspect a pid other than
// its own.
func TestGetPIDInfoRejectsBelowMlevelThreeForOtherPid(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.Prog.MLevel = 2
	f.PID = 42
	if err := f.Push(Int(99)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("GETPIDINFO")](f)
	if err == nil || err.Error() != "Permission denied.  Requires Mucker Level 3." {
		t.Fatalf("err = %v, want the Mucker Level 3 wording", err)
	}
}

// TestGetPIDInfoAllowsBelowMlevelThreeForOwnPid checks the escape hatch: a
// program may always inspect its own pid regardless of mucker level, which
// is exactly what kept GETPIDINFO out of mlev_gen.go's generated table.
func TestGetPIDInfoAllowsBelowMlevelThreeForOwnPid(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.Prog.MLevel = 1
	f.PID = 42
	if err := f.Push(Int(42)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("GETPIDINFO")](f); err != nil {
		t.Fatalf("GETPIDINFO should allow a program to inspect its own pid: %v", err)
	}
}

// TestGetPIDInfoSelfBranchBuildsFromLiveFrame checks the self branch's
// deliberately reproduced upstream quirks: CALLED_DATA is always "", NEXTRUN
// is always 0, SUBTYPE is always "", and MLEVEL reports the real effective
// level — exactly prim_getpidinfo's own hardcoding, not a gap.
func TestGetPIDInfoSelfBranchBuildsFromLiveFrame(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.PID = 42
	f.Descr = 7
	f.Instructions = 123
	f.Caller = 10
	f.Trig = 11
	f.Started = time.Unix(1000, 0)
	if err := f.Push(Int(42)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("GETPIDINFO")](f); err != nil {
		t.Fatalf("GETPIDINFO: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeArray || v.Array == nil || v.Array.IsList() {
		t.Fatalf("result = %+v, want a dictionary", v)
	}

	want := map[string]Value{
		"CALLED_DATA": Str(""),
		"CALLED_PROG": Obj(testProgram),
		"CPU":         Float(0),
		"DESCR":       Int(7),
		"INSTCNT":     Int(123),
		"MLEVEL":      Int(3),
		"NEXTRUN":     Int(0),
		"PID":         Int(42),
		"PLAYER":      Obj(10),
		"STARTED":     Int(1000),
		"SUBTYPE":     Str(""),
		"TRIG":        Obj(11),
		"TYPE":        Str("MUF"),
	}
	for key, wantV := range want {
		got, ok := v.Array.Get(Str(key))
		if !ok {
			t.Fatalf("missing key %q", key)
		}
		if got != wantV {
			t.Errorf("%s = %+v, want %+v", key, got, wantV)
		}
	}
	if filters, ok := v.Array.Get(Str("FILTERS")); !ok || filters.Type != TypeArray || len(filters.Array.Values()) != 0 {
		t.Errorf("FILTERS = %+v, want an empty array", filters)
	}
	if len(h.pidInfoCalls) != 0 {
		t.Errorf("Host.PIDInfo should not be called for the caller's own pid: %+v", h.pidInfoCalls)
	}
}

// TestGetPIDInfoOtherBranchForwardsToHostAndBuildsDict checks the other-pid
// branch: it reads Host.PIDInfo and fills PID/CPU/FILTERS/TYPE/MLEVEL itself
// — MLEVEL hardcoded to 0, matching upstream's own get_pidinfo, which is its
// own documented TODO, not an Emerald gap.
func TestGetPIDInfoOtherBranchForwardsToHostAndBuildsDict(t *testing.T) {
	h := newLockTestHost()
	h.pidInfoOK = true
	h.pidInfoResult = PIDInfo{
		CalledProg: 50,
		CalledData: "SLEEPING",
		Descr:      8,
		InstCnt:    99,
		NextRun:    2000,
		Player:     20,
		Started:    time.Unix(1500, 0),
		Subtype:    "DELAY",
		Trig:       21,
	}
	f := newTestFrame(h)
	f.PID = 42
	if err := f.Push(Int(99)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("GETPIDINFO")](f); err != nil {
		t.Fatalf("GETPIDINFO: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]Value{
		"CALLED_DATA": Str("SLEEPING"),
		"CALLED_PROG": Obj(50),
		"CPU":         Float(0),
		"DESCR":       Int(8),
		"INSTCNT":     Int(99),
		"MLEVEL":      Int(0),
		"NEXTRUN":     Int(2000),
		"PID":         Int(99),
		"PLAYER":      Obj(20),
		"STARTED":     Int(1500),
		"SUBTYPE":     Str("DELAY"),
		"TRIG":        Obj(21),
		"TYPE":        Str("MUF"),
	}
	for key, wantV := range want {
		got, ok := v.Array.Get(Str(key))
		if !ok {
			t.Fatalf("missing key %q", key)
		}
		if got != wantV {
			t.Errorf("%s = %+v, want %+v", key, got, wantV)
		}
	}
	if len(h.pidInfoCalls) != 1 || h.pidInfoCalls[0] != 99 {
		t.Fatalf("Host.PIDInfo calls = %+v, want [99]", h.pidInfoCalls)
	}
}

// TestGetPIDInfoReturnsEmptyDictWhenPIDNotFound matches upstream leaving a
// freshly allocated new_array_dictionary untouched when neither the
// timequeue nor the MUF-event queue know the pid.
func TestGetPIDInfoReturnsEmptyDictWhenPIDNotFound(t *testing.T) {
	h := newLockTestHost() // pidInfoOK defaults to false
	f := newTestFrame(h)
	f.PID = 42
	if err := f.Push(Int(999)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("GETPIDINFO")](f); err != nil {
		t.Fatalf("GETPIDINFO: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeArray || v.Array == nil || v.Array.Len() != 0 {
		t.Fatalf("result = %+v, want an empty dictionary", v)
	}
}

func TestWatchPIDRejectsBelowMlevelThree(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.Prog.MLevel = 2
	f.PID = 1
	if err := f.Push(Int(2)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("WATCHPID")](f)
	if err == nil || err.Error() != "Mucker level 3 required." {
		t.Fatalf("err = %v, want the Mucker level 3 wording", err)
	}
}

func TestWatchPIDRejectsOwnPID(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.PID = 42
	if err := f.Push(Int(42)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("WATCHPID")](f)
	want := "Integer expected. Must be different from current PID."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestWatchPIDRejectsNonInteger(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	if err := f.Push(Str("nope")); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("WATCHPID")](f)
	want := "Integer expected. Must be different from current PID."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// TestWatchPIDForwardsToHostWhenTargetExists checks that a live target is
// handled entirely by Host.WatchPID, with no event queued on the calling
// frame — that only happens in the "target does not exist" branch.
func TestWatchPIDForwardsToHostWhenTargetExists(t *testing.T) {
	h := newLockTestHost()
	h.watchPIDResult = true
	f := newTestFrame(h)
	f.PID = 1
	if err := f.Push(Int(2)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("WATCHPID")](f); err != nil {
		t.Fatalf("WATCHPID: %v", err)
	}
	if f.Depth() != 0 {
		t.Fatalf("WATCHPID should leave nothing on the stack, depth = %d", f.Depth())
	}
	if len(h.watchPIDCalls) != 1 || h.watchPIDCalls[0] != (watchPIDCall{1, 2}) {
		t.Fatalf("WatchPID calls = %+v, want [{1 2}]", h.watchPIDCalls)
	}
	if len(f.PendingEvents) != 0 {
		t.Fatalf("no event should be queued when the target exists: %+v", f.PendingEvents)
	}
}

// TestWatchPIDQueuesProcExitEventWhenTargetDoesNotExist checks upstream's
// else branch: watching a pid that names no live process queues a
// PROC.EXIT.<pid> event directly on the caller's own frame, immediately,
// rather than ever registering a wait.
func TestWatchPIDQueuesProcExitEventWhenTargetDoesNotExist(t *testing.T) {
	h := newLockTestHost()
	h.watchPIDResult = false
	f := newTestFrame(h)
	f.PID = 1
	if err := f.Push(Int(999)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("WATCHPID")](f); err != nil {
		t.Fatalf("WATCHPID: %v", err)
	}
	if len(f.PendingEvents) != 1 {
		t.Fatalf("PendingEvents = %+v, want one queued event", f.PendingEvents)
	}
	ev := f.PendingEvents[0]
	if ev.Name != "PROC.EXIT.999" || ev.Data.Type != TypeInteger || ev.Data.Num != 999 {
		t.Fatalf("event = %+v, want PROC.EXIT.999 carrying 999", ev)
	}
}

// TestEventWaitForServesAnAlreadyQueuedEvent checks that EVENT_WAITFOR does
// not unconditionally block: an event queued before it runs — exactly what
// WATCHPID does when the target is already gone — is served immediately.
func TestEventWaitForServesAnAlreadyQueuedEvent(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.AddEvent("PROC.EXIT.999", Int(999))
	if err := f.Push(Arr(NewList([]Value{Str("PROC.EXIT.999")}))); err != nil {
		t.Fatal(err)
	}

	res, err := f.primitive(InEventWaitFor)
	if err != nil {
		t.Fatalf("EVENT_WAITFOR: %v", err)
	}
	if res != nil {
		t.Fatalf("result = %+v, want nil (not blocked)", res)
	}
	name, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	data, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if name.Type != TypeString || name.Str != "PROC.EXIT.999" {
		t.Fatalf("name = %+v, want PROC.EXIT.999", name)
	}
	if data.Type != TypeInteger || data.Num != 999 {
		t.Fatalf("data = %+v, want 999", data)
	}
}
