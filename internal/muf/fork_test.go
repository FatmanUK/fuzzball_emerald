package muf

import (
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

func TestForkCopiesTheStackDeeply(t *testing.T) {
	arr := NewList([]Value{Int(1), Int(2)})
	parent := newTestFrame(newLockTestHost())
	parent.Stack = []Value{Int(7), Arr(arr)}

	child := parent.fork()

	if len(child.Stack) != 2 || child.Stack[0].Num != 7 {
		t.Fatalf("child.Stack = %+v", child.Stack)
	}
	if child.Stack[1].Array == arr {
		t.Fatal("the array should be a distinct copy, not the same pointer")
	}

	// Mutating the child's copy must not reach the parent's, and vice versa.
	child.Stack[1].Array.Set(Int(0), Int(99))
	if v, _ := parent.Stack[1].Array.Get(Int(0)); v.Num != 1 {
		t.Errorf("mutating the child's array changed the parent's: %+v", v)
	}
	parent.Stack[1].Array.Set(Int(1), Int(-1))
	if v, _ := child.Stack[1].Array.Get(Int(1)); v.Num != 2 {
		t.Errorf("mutating the parent's array changed the child's: %+v", v)
	}
}

func TestForkCopiesVarsAndLVarsDeeply(t *testing.T) {
	arr := NewList([]Value{Str("x")})
	parent := newTestFrame(newLockTestHost())
	parent.Vars = []Value{Int(1), Arr(arr)}
	parent.LVars = []Value{Str("hi")}

	child := parent.fork()

	if len(child.Vars) != 2 || child.Vars[1].Array == arr {
		t.Fatalf("Vars should be deep-copied, got %+v", child.Vars)
	}
	if len(child.LVars) != 1 || child.LVars[0].Str != "hi" {
		t.Fatalf("LVars = %+v", child.LVars)
	}

	child.Vars[1].Array.Set(Int(0), Str("changed"))
	if v, _ := parent.Vars[1].Array.Get(Int(0)); v.Str != "x" {
		t.Errorf("mutating the child's Vars array changed the parent's: %+v", v)
	}
}

func TestForkCopiesScopesDeeply(t *testing.T) {
	arr := NewList([]Value{Int(5)})
	parent := newTestFrame(newLockTestHost())
	parent.scopes = [][]Value{{Arr(arr)}}

	child := parent.fork()

	if len(child.scopes) != 1 || len(child.scopes[0]) != 1 {
		t.Fatalf("scopes = %+v", child.scopes)
	}
	if child.scopes[0][0].Array == arr {
		t.Fatal("scoped array should be a distinct copy")
	}
}

func TestForkCopiesCallsForsAndTrysIndependently(t *testing.T) {
	parent := newTestFrame(newLockTestHost())
	parent.calls = []callSite{{pc: 3, scopeBase: 1}}
	parent.trys = []tryBlock{{catchPC: 9}}
	parent.fors = []forLoop{{
		keys: []Value{Str("k")},
		vals: []Value{Arr(NewList([]Value{Int(1)}))},
		cur:  0, end: 10, step: 1, counting: true,
	}}

	child := parent.fork()

	if len(child.calls) != 1 || child.calls[0].pc != 3 {
		t.Fatalf("calls = %+v", child.calls)
	}
	if len(child.trys) != 1 || child.trys[0].catchPC != 9 {
		t.Fatalf("trys = %+v", child.trys)
	}
	if len(child.fors) != 1 || child.fors[0].end != 10 {
		t.Fatalf("fors = %+v", child.fors)
	}
	if child.fors[0].vals[0].Array == parent.fors[0].vals[0].Array {
		t.Fatal("a FOREACH array should be deep-copied too")
	}

	// Appending to the child's slices must not touch the parent's backing
	// arrays.
	child.calls = append(child.calls, callSite{pc: 100})
	if len(parent.calls) != 1 {
		t.Errorf("appending to child.calls grew parent.calls: %+v", parent.calls)
	}
}

func TestForkPreservesScalarFieldsAndBackgroundsTheChild(t *testing.T) {
	parent := newTestFrame(newLockTestHost())
	parent.PC = 42
	parent.Caller = ref.Ref(5)
	parent.Trig = ref.Ref(6)
	parent.Descr = 7
	parent.Level = 3
	parent.Supplicant = ref.Ref(8)
	parent.ErrorFlags.DivZero = true
	parent.Instructions = 12345
	parent.Mode = ModeForeground

	child := parent.fork()

	if child.PC != 42 {
		t.Errorf("PC = %d, want 42 (unadvanced — the caller moves it past FORK)", child.PC)
	}
	if child.Caller != 5 || child.Trig != 6 || child.Descr != 7 || child.Level != 3 || child.Supplicant != 8 {
		t.Errorf("scalar fields not preserved: %+v", child)
	}
	if !child.ErrorFlags.DivZero {
		t.Error("ErrorFlags should be copied")
	}
	if child.Instructions != 0 {
		t.Errorf("Instructions = %d, want 0 — a forked frame starts its own fresh count", child.Instructions)
	}
	if child.Mode != ModeBackground {
		t.Errorf("Mode = %d, want ModeBackground", child.Mode)
	}
	if child.PID != 0 {
		t.Errorf("PID = %d, want 0 — assigned once the host registers the process", child.PID)
	}
	if child.Prog != parent.Prog {
		t.Error("the child should share the same compiled program, not a copy")
	}
}
