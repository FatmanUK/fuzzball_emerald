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
	owner map[ref.Ref]ref.Ref

	testLockCalls []testLockCall
	testLockOK    bool
	testLockErr   error

	lockedOK  bool
	lockedErr error

	maxRecursion int

	lockStrings     map[ref.Ref]string
	setLockCalls    []setLockCall
	setLockOK       bool
	parseLockCalls  []parseLockCall
	parseLockResult *boolexp.Expr
	unparseLockArg  *boolexp.Expr
	unparseLockStr  string
}

type setLockCall struct {
	descr            int
	matchPlayer, obj ref.Ref
	raw              string
}

type parseLockCall struct {
	descr       int
	matchPlayer ref.Ref
	raw         string
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
		owner:        map[ref.Ref]ref.Ref{},
		maxRecursion: 8,
		lockStrings:  map[ref.Ref]string{},
	}
}

func (h *lockTestHost) Valid(r ref.Ref) bool          { return h.valid[r] }
func (h *lockTestHost) ObjType(r ref.Ref) ref.ObjType { return h.types[r] }
func (h *lockTestHost) Owner(r ref.Ref) ref.Ref       { return h.owner[r] }
func (h *lockTestHost) Location(ref.Ref) ref.Ref      { return ref.Nothing }

func (h *lockTestHost) LockString(obj ref.Ref) string { return h.lockStrings[obj] }

func (h *lockTestHost) SetLockString(descr int, matchPlayer, obj ref.Ref, raw string) bool {
	h.setLockCalls = append(h.setLockCalls, setLockCall{descr, matchPlayer, obj, raw})
	return h.setLockOK
}

func (h *lockTestHost) ParseLock(descr int, matchPlayer ref.Ref, raw string) *boolexp.Expr {
	h.parseLockCalls = append(h.parseLockCalls, parseLockCall{descr, matchPlayer, raw})
	return h.parseLockResult
}

func (h *lockTestHost) UnparseLock(lock *boolexp.Expr) string {
	h.unparseLockArg = lock
	return h.unparseLockStr
}

func (h *lockTestHost) TestLock(descr, level int, testPlayer ref.Ref, lock *boolexp.Expr, trig, caller ref.Ref) (bool, error) {
	h.testLockCalls = append(h.testLockCalls, testLockCall{descr, level, testPlayer, lock, trig, caller})
	return h.testLockOK, h.testLockErr
}

func (h *lockTestHost) Locked(descr, level int, player, thing ref.Ref) (bool, error) {
	return h.lockedOK, h.lockedErr
}

func (h *lockTestHost) MaxInterpRecursion() int { return h.maxRecursion }

const testProgram ref.Ref = 99

// newTestFrame builds a frame at mucker level 3 (so checkRemote and progUID
// take their "at or above level 2" branch, matching every other test in this
// file) with the given host and no compiled code.
func newTestFrame(host Host) *Frame {
	return &Frame{Level: 1, host: host, Prog: &Program{Ref: testProgram, MLevel: 3}}
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

func TestGetlockstrPushesHostResult(t *testing.T) {
	h := newLockTestHost()
	h.valid[testThing] = true
	h.lockStrings[testThing] = "#1&#2"

	f := newTestFrame(h)
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("GETLOCKSTR")](f); err != nil {
		t.Fatalf("GETLOCKSTR: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeString || v.Str != "#1&#2" {
		t.Fatalf("result = %+v, want the lock string", v)
	}
}

func TestGetlockstrInvalidArg(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	if err := f.Push(Int(5)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("GETLOCKSTR")](f)
	if err == nil || err.Error() != "Invalid argument type" {
		t.Fatalf("err = %v, want the invalid-argument message", err)
	}
}

func TestGetlockstrPermissionDenied(t *testing.T) {
	h := newLockTestHost()
	h.valid[testThing] = true
	h.types[testThing] = ref.TypeThing
	h.owner[testThing] = ref.Ref(200)
	h.owner[testPlayer] = ref.Ref(300) // a different owner than testThing's

	f := &Frame{Level: 1, host: h, Prog: &Program{Ref: testProgram, MLevel: 1}, Caller: testPlayer}
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("GETLOCKSTR")](f)
	if err == nil || err.Error() != "Permission denied." {
		t.Fatalf("err = %v, want permission denied", err)
	}
}

func TestSetlockstrClearsOnEmptyString(t *testing.T) {
	h := newLockTestHost()
	h.valid[testThing] = true
	h.setLockOK = true

	f := newTestFrame(h)
	f.Caller = testPlayer
	f.Descr = 7
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("")); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("SETLOCKSTR")](f); err != nil {
		t.Fatalf("SETLOCKSTR: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != 1 {
		t.Fatalf("result = %+v, want true", v)
	}
	if len(h.setLockCalls) != 1 {
		t.Fatalf("SetLockString called %d times, want 1", len(h.setLockCalls))
	}
	call := h.setLockCalls[0]
	if call.obj != testThing || call.matchPlayer != testPlayer || call.raw != "" || call.descr != 7 {
		t.Fatalf("unexpected call: %+v", call)
	}
}

func TestSetlockstrInvalidArgs(t *testing.T) {
	h := newLockTestHost()
	h.valid[testThing] = true

	f := newTestFrame(h)
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Int(0)); err != nil { // not a string
		t.Fatal(err)
	}
	_, err := prims[PrimNumber("SETLOCKSTR")](f)
	if err == nil || err.Error() != "Non-string argument (2)" {
		t.Fatalf("err = %v, want the non-string message", err)
	}
}

func TestSetlockstrPermissionDenied(t *testing.T) {
	h := newLockTestHost()
	h.valid[testThing] = true
	h.types[testThing] = ref.TypeThing
	h.owner[testThing] = ref.Ref(200)
	h.owner[testPlayer] = ref.Ref(300)

	f := &Frame{Level: 1, host: h, Prog: &Program{Ref: testProgram, MLevel: 3}, Caller: testPlayer}
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("#1")); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("SETLOCKSTR")](f)
	if err == nil || err.Error() != "Permission denied." {
		t.Fatalf("err = %v, want permission denied", err)
	}
	if len(h.setLockCalls) != 0 {
		t.Fatalf("SetLockString should not run once permission is denied")
	}
}

func TestParselockPushesLockValue(t *testing.T) {
	h := newLockTestHost()
	lock := &boolexp.Expr{Kind: boolexp.Const, Thing: testThing}
	h.parseLockResult = lock

	f := newTestFrame(h)
	f.Caller = testPlayer
	f.Descr = 3
	if err := f.Push(Str("#11")); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("PARSELOCK")](f); err != nil {
		t.Fatalf("PARSELOCK: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeLock || v.Lock != lock {
		t.Fatalf("result = %+v, want the parsed lock", v)
	}
	if len(h.parseLockCalls) != 1 || h.parseLockCalls[0].raw != "#11" || h.parseLockCalls[0].descr != 3 {
		t.Fatalf("unexpected call: %+v", h.parseLockCalls)
	}
}

func TestParselockInvalidArg(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	if err := f.Push(Int(0)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("PARSELOCK")](f)
	if err == nil || err.Error() != "Invalid argument." {
		t.Fatalf("err = %v, want the invalid-argument message", err)
	}
}

func TestUnparselockPushesString(t *testing.T) {
	h := newLockTestHost()
	h.unparseLockStr = "#11"
	lock := &boolexp.Expr{Kind: boolexp.Const, Thing: testThing}

	f := newTestFrame(h)
	if err := f.Push(LockVal(lock)); err != nil {
		t.Fatal(err)
	}

	if _, err := prims[PrimNumber("UNPARSELOCK")](f); err != nil {
		t.Fatalf("UNPARSELOCK: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeString || v.Str != "#11" {
		t.Fatalf("result = %+v, want #11", v)
	}
	if h.unparseLockArg != lock {
		t.Fatalf("UnparseLock was not called with the pushed lock")
	}
}

func TestUnparselockInvalidArg(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}

	_, err := prims[PrimNumber("UNPARSELOCK")](f)
	if err == nil || err.Error() != "Invalid argument." {
		t.Fatalf("err = %v, want the invalid-argument message", err)
	}
}
