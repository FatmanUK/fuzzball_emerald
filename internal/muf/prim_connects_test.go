package muf

import (
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// TestConnectsPrimitivesUseTheirOwnMlevWording spot-checks every distinct
// mlev-floor wording this session found in src/p_connects.c — three
// level-3 variants and two level-4 variants, none of them the dispatcher's
// generic "Permission denied."/"Permission denied.  Requires Wizbit." —
// across every primitive in the file, at one mucker level below the floor
// each needs.
func TestConnectsPrimitivesUseTheirOwnMlevWording(t *testing.T) {
	tests := []struct {
		prim  string
		mlev  int
		nargs int
		want  string
	}{
		{"ONLINE", 2, 0, "Mucker level 3 primitive."},
		{"ONLINE_ARRAY", 2, 0, "Mucker level 3 primitive."},
		{"DESCRIPTORS", 2, 1, "Mucker level 3 primitive."},
		{"DESCR_ARRAY", 2, 1, "Mucker level 3 primitive."},
		{"DESCRDBREF", 2, 1, "Mucker level 3 primitive."},
		{"DESCRSECURE?", 2, 1, "Requires Mucker Level 3."},
		{"DESCRIDLE", 2, 1, "Mucker level 3 primitive."},
		{"DESCRTIME", 2, 1, "Mucker level 3 primitive."},
		{"DESCRLEASTIDLE", 2, 1, "Mucker level 3 primitive."},
		{"DESCRMOSTIDLE", 2, 1, "Mucker level 3 primitive."},
		{"DESCRNOTIFY", 2, 2, "Mucker level 3 primitive."},
		{"NEXTDESCR", 2, 1, "Mucker level 3 primitive."},
		{"FIRSTDESCR", 2, 1, "Requires Mucker Level 3."},
		{"LASTDESCR", 2, 1, "Requires Mucker Level 3."},
		{"DESCRFLUSH", 2, 1, "Requires Mucker Level 3 or better."},
		{"DESCRBUFSIZE", 2, 1, "Mucker level 3 primitive."},
		{"SETWIDTH", 2, 2, "Mucker level 3 primitive."},
		{"SETHEIGHT", 2, 2, "Mucker level 3 primitive."},
		{"DESCRHOST", 3, 1, "Primitive is a wizbit only command."},
		{"DESCRUSER", 3, 1, "Primitive is a wizbit only command."},
		{"DESCRBOOT", 3, 1, "Primitive is a wizbit only command."},
		{"DESCR_SETUSER", 3, 3, "Requires Wizbit."},
	}
	for _, tt := range tests {
		t.Run(tt.prim, func(t *testing.T) {
			h := newLockTestHost()
			f := newTestFrame(h)
			f.Prog.MLevel = tt.mlev
			for i := 0; i < tt.nargs; i++ {
				if err := f.Push(Int(0)); err != nil {
					t.Fatal(err)
				}
			}
			_, err := prims[PrimNumber(tt.prim)](f)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDescrIdleForwardsAndAbortsOnNegativeResult(t *testing.T) {
	h := newLockTestHost()
	h.descrIdleResult = 42
	f := newTestFrame(h)
	if err := f.Push(Int(7)); err != nil {
		t.Fatal(err)
	}
	if _, err := prims[PrimNumber("DESCRIDLE")](f); err != nil {
		t.Fatalf("DESCRIDLE: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != 42 {
		t.Fatalf("result = %+v, want 42", v)
	}
	if len(h.descrIdleCalls) != 1 || h.descrIdleCalls[0] != 7 {
		t.Fatalf("DescrIdle calls = %+v, want [7]", h.descrIdleCalls)
	}

	h2 := newLockTestHost()
	h2.descrIdleResult = -1
	f2 := newTestFrame(h2)
	if err := f2.Push(Int(999)); err != nil {
		t.Fatal(err)
	}
	_, err = prims[PrimNumber("DESCRIDLE")](f2)
	if err == nil || err.Error() != "Invalid descriptor number. (1)" {
		t.Fatalf("err = %v, want the invalid-descriptor message", err)
	}
}

func TestDescrIdleRejectsNonInteger(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	if err := f.Push(Str("nope")); err != nil {
		t.Fatal(err)
	}
	_, err := prims[PrimNumber("DESCRIDLE")](f)
	if err == nil || err.Error() != "Argument not an integer. (1)" {
		t.Fatalf("err = %v, want the argument-type message", err)
	}
}

// TestDescrLeastMostIdleNeverAbortOnNegativeResult checks that, unlike
// DESCRIDLE/DESCRTIME/DESCRBUFSIZE, these two push a -1 result straight
// through rather than aborting — "no connection found" is itself a valid
// answer for a player-scoped query.
func TestDescrLeastMostIdleNeverAbortOnNegativeResult(t *testing.T) {
	h := newLockTestHost()
	h.valid[testPlayer] = true
	h.descrLeastIdleResult = -1
	h.descrMostIdleResult = -1
	f := newTestFrame(h)
	if err := f.Push(Obj(testPlayer)); err != nil {
		t.Fatal(err)
	}
	if _, err := prims[PrimNumber("DESCRLEASTIDLE")](f); err != nil {
		t.Fatalf("DESCRLEASTIDLE: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Type != TypeInteger || v.Num != -1 {
		t.Fatalf("result = %+v, want -1", v)
	}
}

func TestDescrLeastIdleRejectsNonObjectAndInvalidRef(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	if err := f.Push(Int(5)); err != nil {
		t.Fatal(err)
	}
	if _, err := prims[PrimNumber("DESCRLEASTIDLE")](f); err == nil {
		t.Fatal("a non-dbref argument should be rejected")
	} else if err.Error() != "Argument not a dbref." {
		t.Fatalf("err = %v, want the non-dbref message", err)
	}

	h2 := newLockTestHost() // testThing deliberately never marked valid
	f2 := newTestFrame(h2)
	if err := f2.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}
	if _, err := prims[PrimNumber("DESCRLEASTIDLE")](f2); err == nil || err.Error() != "Bad dbref." {
		t.Fatalf("err = %v, want the bad-dbref message", err)
	}
}

// TestDescrFlushIsStackNeutral checks upstream's own real quirk: it computes
// a result but never pushes it — the primitive consumes its descriptor
// argument and leaves nothing behind.
func TestDescrFlushIsStackNeutral(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	if err := f.Push(Int(3)); err != nil {
		t.Fatal(err)
	}
	if _, err := prims[PrimNumber("DESCRFLUSH")](f); err != nil {
		t.Fatalf("DESCRFLUSH: %v", err)
	}
	if f.Depth() != 0 {
		t.Fatalf("DESCRFLUSH should leave nothing on the stack, depth = %d", f.Depth())
	}
	if len(h.descrFlushCalls) != 1 || h.descrFlushCalls[0] != 3 {
		t.Fatalf("DescrFlush calls = %+v, want [3]", h.descrFlushCalls)
	}
}

func TestSetWidthValidatesRangeAndArgumentOrder(t *testing.T) {
	h := newLockTestHost()
	h.setDescrSizeResult = true
	f := newTestFrame(h)
	// Stack order is descr, size (upstream's own): descr sits under size.
	if err := f.Push(Int(9)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Int(132)); err != nil {
		t.Fatal(err)
	}
	if _, err := prims[PrimNumber("SETWIDTH")](f); err != nil {
		t.Fatalf("SETWIDTH: %v", err)
	}
	if len(h.setDescrSizeCalls) != 1 {
		t.Fatalf("SetDescrSize calls = %+v, want one", h.setDescrSizeCalls)
	}
	call := h.setDescrSizeCalls[0]
	if call.descr != 9 || call.width != 132 || call.height != -1 {
		t.Fatalf("call = %+v, want {descr:9 width:132 height:-1}", call)
	}

	h2 := newLockTestHost()
	f2 := newTestFrame(h2)
	if err := f2.Push(Int(1)); err != nil {
		t.Fatal(err)
	}
	if err := f2.Push(Int(70000)); err != nil {
		t.Fatal(err)
	}
	_, err := prims[PrimNumber("SETWIDTH")](f2)
	if err == nil || err.Error() != "Width must be between 0 and 65535." {
		t.Fatalf("err = %v, want the range message", err)
	}
}

func TestSetHeightReportsInvalidDescriptorWithoutAPeriod(t *testing.T) {
	h := newLockTestHost()
	h.setDescrSizeResult = false
	f := newTestFrame(h)
	if err := f.Push(Int(999)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Int(24)); err != nil {
		t.Fatal(err)
	}
	_, err := prims[PrimNumber("SETHEIGHT")](f)
	// Note: no trailing period, unlike most of this file's "Invalid
	// descriptor number. (1)" — upstream's own literal wording here.
	if err == nil || err.Error() != "Invalid descriptor number (2)" {
		t.Fatalf("err = %v, want the no-period invalid-descriptor message", err)
	}
}

func TestFirstDescrAndLastDescrDispatchByPlayerArgument(t *testing.T) {
	h := newLockTestHost()
	h.firstDescrResult = 5
	h.lastDescrResult = 9
	f := newTestFrame(h)
	if err := f.Push(Obj(ref.Nothing)); err != nil {
		t.Fatal(err)
	}
	if _, err := prims[PrimNumber("FIRSTDESCR")](f); err != nil {
		t.Fatalf("FIRSTDESCR: %v", err)
	}
	v, err := f.Pop()
	if err != nil {
		t.Fatal(err)
	}
	if v.Num != 5 {
		t.Fatalf("result = %+v, want 5", v)
	}
	if len(h.firstDescrCalls) != 1 || h.firstDescrCalls[0] != ref.Nothing {
		t.Fatalf("FirstDescr calls = %+v, want [Nothing]", h.firstDescrCalls)
	}
}

func TestFirstDescrRejectsNonPlayerObject(t *testing.T) {
	h := newLockTestHost() // testThing never marked a player
	h.types[testThing] = 0
	h.valid[testThing] = true
	f := newTestFrame(h)
	if err := f.Push(Obj(testThing)); err != nil {
		t.Fatal(err)
	}
	_, err := prims[PrimNumber("FIRSTDESCR")](f)
	if err == nil || err.Error() != "Player dbref expected (2)" {
		t.Fatalf("err = %v, want the player-dbref message", err)
	}
}

func TestDescrSetUserValidatesEachArgumentInOrder(t *testing.T) {
	h := newLockTestHost()
	f := newTestFrame(h)
	f.Prog.MLevel = 4
	if err := f.Push(Str("nope")); err != nil { // descr, wrong type
		t.Fatal(err)
	}
	if err := f.Push(Obj(ref.Nothing)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("pw")); err != nil {
		t.Fatal(err)
	}
	_, err := prims[PrimNumber("DESCR_SETUSER")](f)
	if err == nil || err.Error() != "Integer descriptor number expected. (1)" {
		t.Fatalf("err = %v, want the descriptor-type message", err)
	}
}

func TestDescrSetUserRejectsWrongPasswordBeforeCallingHost(t *testing.T) {
	h := newLockTestHost()
	h.checkPasswordResult = false
	h.types[testPlayer] = ref.TypePlayer
	h.valid[testPlayer] = true
	f := newTestFrame(h)
	f.Prog.MLevel = 4
	if err := f.Push(Int(3)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Obj(testPlayer)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("wrong")); err != nil {
		t.Fatal(err)
	}
	_, err := prims[PrimNumber("DESCR_SETUSER")](f)
	if err == nil || err.Error() != "Incorrect password." {
		t.Fatalf("err = %v, want the incorrect-password message", err)
	}
	if len(h.setUserCalls) != 0 {
		t.Fatal("SetUser should not be called once the password check fails")
	}
}

func TestDescrSetUserSkipsPasswordCheckForNothing(t *testing.T) {
	h := newLockTestHost()
	h.setUserResult = true
	f := newTestFrame(h)
	f.Prog.MLevel = 4
	if err := f.Push(Int(3)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Obj(ref.Nothing)); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(Str("")); err != nil {
		t.Fatal(err)
	}
	if _, err := prims[PrimNumber("DESCR_SETUSER")](f); err != nil {
		t.Fatalf("DESCR_SETUSER: %v", err)
	}
	if len(h.checkPasswordCalls) != 0 {
		t.Fatal("CheckPassword should not be called when who is NOTHING")
	}
	if len(h.setUserCalls) != 1 || h.setUserCalls[0] != (setUserCall{3, ref.Nothing}) {
		t.Fatalf("SetUser calls = %+v, want [{3 Nothing}]", h.setUserCalls)
	}
}
