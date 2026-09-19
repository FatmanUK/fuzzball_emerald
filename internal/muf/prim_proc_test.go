package muf

import (
	"testing"

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
