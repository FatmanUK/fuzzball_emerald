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
