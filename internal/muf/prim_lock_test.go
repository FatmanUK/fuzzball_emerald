package muf

import (
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/boolexp"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// lockTestHost implements only what TESTLOCK and LOCKED? need. Embedding the
// Host interface itself satisfies it at compile time and panics on anything
// else called, which surfaces a test relying on an unstubbed method loudly
// rather than silently returning a zero value.
type lockTestHost struct {
	Host

	types map[ref.Ref]ref.ObjType
	valid map[ref.Ref]bool

	testLockCalls []testLockCall
	testLockOK    bool
	testLockErr   error

	lockedOK  bool
	lockedErr error

	maxRecursion int
}

type testLockCall struct {
	descr, level int
	testPlayer   ref.Ref
	lock         *boolexp.Expr
	trig, caller ref.Ref
}

func newLockTestHost() *lockTestHost {
	return &lockTestHost{
		types:        map[ref.Ref]ref.ObjType{},
		valid:        map[ref.Ref]bool{},
		maxRecursion: 8,
	}
}

func (h *lockTestHost) Valid(r ref.Ref) bool          { return h.valid[r] }
func (h *lockTestHost) ObjType(r ref.Ref) ref.ObjType { return h.types[r] }

func (h *lockTestHost) TestLock(descr, level int, testPlayer ref.Ref, lock *boolexp.Expr, trig, caller ref.Ref) (bool, error) {
	h.testLockCalls = append(h.testLockCalls, testLockCall{descr, level, testPlayer, lock, trig, caller})
	return h.testLockOK, h.testLockErr
}

func (h *lockTestHost) Locked(descr, level int, player, thing ref.Ref) (bool, error) {
	return h.lockedOK, h.lockedErr
}

func (h *lockTestHost) MaxInterpRecursion() int { return h.maxRecursion }

func newTestFrame(host Host) *Frame {
	f := &Frame{Level: 1, host: host}
	return f
}

const (
	testPlayer ref.Ref = 10
	testThing  ref.Ref = 11
	testExit   ref.Ref = 12
)

func TestTestlockPushesHostResult(t *testing.T) {
	h := newLockTestHost()
	h.types[testPlayer] = ref.TypePlayer
	h.valid[testPlayer] = true
	h.testLockOK = true

	f := newTestFrame(h)
	f.Trig, f.Caller = 20, 21
	f.Descr = 5
	if err := f.Push(Obj(testPlayer)); err != nil {
		t.Fatal(err)
	}
	lock := &boolexp.Expr{Kind: boolexp.Const, Thing: testThing}
	if err := f.Push(LockVal(lock)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("TESTLOCK")](f); err != nil {
		t.Fatalf("TESTLOCK: %v", err)
	}

	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != 1 {
		t.Fatalf("result = %+v, want true", v)
	}

	if len(h.testLockCalls) != 1 {
		t.Fatalf("TestLock called %d times, want 1", len(h.testLockCalls))
	}
	call := h.testLockCalls[0]
	if call.testPlayer != testPlayer || call.lock != lock || call.trig != 20 || call.caller != 21 || call.descr != 5 || call.level != 1 {
		t.Fatalf("unexpected call: %+v", call)
	}
}

func TestTestlockInvalidPlayerArg(t *testing.T) {
	h := newLockTestHost()
	h.types[testThing] = ref.TypeThing
	h.valid[testThing] = false // not a valid object at all

	f := newTestFrame(h)
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(LockVal(nil)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("TESTLOCK")](f)
	if err == nil || err.Error() != "Invalid player or thing argument (1)." {
		t.Fatalf("err = %v, want the invalid-player message", err)
	}
}

func TestTestlockInvalidLockArg(t *testing.T) {
	h := newLockTestHost()
	h.types[testPlayer] = ref.TypePlayer
	h.valid[testPlayer] = true

	f := newTestFrame(h)
	if err := f.Push(Obj(testPlayer)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Int(0)); err != nil { // not a lock
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("TESTLOCK")](f)
	if err == nil || err.Error() != "Invalid argument (2)." {
		t.Fatalf("err = %v, want the invalid-argument message", err)
	}
}

func TestTestlockRecursionGuard(t *testing.T) {
	h := newLockTestHost()
	h.types[testPlayer] = ref.TypePlayer
	h.valid[testPlayer] = true

	f := newTestFrame(h)
	f.Level = 9
	if err := f.Push(Obj(testPlayer)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(LockVal(nil)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("TESTLOCK")](f)
	if err == nil || err.Error() != "Interp call loops not allowed." {
		t.Fatalf("err = %v, want the recursion-guard message", err)
	}
	if len(h.testLockCalls) != 0 {
		t.Fatalf("Host.TestLock should not run once the guard trips")
	}
}

func TestLockedPushesHostResult(t *testing.T) {
	h := newLockTestHost()
	h.types[testPlayer] = ref.TypePlayer
	h.valid[testPlayer] = true
	h.types[testThing] = ref.TypeThing
	h.valid[testThing] = true
	h.lockedOK = true

	f := newTestFrame(h)
	if err := f.Push(Obj(testPlayer)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("LOCKED?")](f); err != nil {
		t.Fatalf("LOCKED?: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != 1 {
		t.Fatalf("result = %+v, want true", v)
	}
}

// TestLockedRejectsThingArgument reproduces Fuzzball 7.2.1's own bug: its
// condition for the player/thing argument is written "!= TYPE_PLAYER &&
// == TYPE_THING", which rejects a THING rather than allowing it as its own
// doc comment claims. This is deliberately kept, not fixed.
func TestLockedRejectsThingArgument(t *testing.T) {
	h := newLockTestHost()
	h.types[testPlayer] = ref.TypeThing
	h.valid[testPlayer] = true
	h.types[testThing] = ref.TypeThing
	h.valid[testThing] = true

	f := newTestFrame(h)
	if err := f.Push(Obj(testPlayer)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("LOCKED?")](f)
	if err == nil || err.Error() != "Invalid player or thing argument. (1)" {
		t.Fatalf("err = %v, want the invalid-player message", err)
	}
}

func TestLockedInvalidObjectArg(t *testing.T) {
	h := newLockTestHost()
	h.types[testPlayer] = ref.TypePlayer
	h.valid[testPlayer] = true
	h.valid[testThing] = false

	f := newTestFrame(h)
	if err := f.Push(Obj(testPlayer)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("LOCKED?")](f)
	if err == nil || err.Error() != "Invalid object (2)." {
		t.Fatalf("err = %v, want the invalid-object message", err)
	}
}

func TestLockedRecursionGuard(t *testing.T) {
	h := newLockTestHost()
	h.maxRecursion = 3

	f := newTestFrame(h)
	f.Level = 4
	if err := f.Push(Obj(testPlayer)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("LOCKED?")](f)
	if err == nil || err.Error() != "Interp call loops not allowed." {
		t.Fatalf("err = %v, want the recursion-guard message", err)
	}
}
