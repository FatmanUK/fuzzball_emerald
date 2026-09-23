package muf

import (
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

func TestValueTextRendersLikeUpstream(t *testing.T) {
	long := "a string comfortably longer than the thirty characters a trace shows"
	tests := []struct {
		v    Value
		want string
	}{
		{Str("hi"), `"hi"`},
		{Str(""), `""`},
		// Cut at twenty-nine characters, with an underscore marking it.
		{Str(long), `"` + long[:29] + `"_`},
		{Int(42), "42"},
		{Int(-1), "-1"},
		{Obj(ref.Ref(58)), "#58"},
		// A float always shows a decimal point, so it cannot be mistaken
		// for an integer.
		{Float(3), "3.0"},
		{Float(1.5), "1.5"},
		{Arr(NewList([]Value{Int(1), Int(2)})), "2{...}"},
	}
	for _, tt := range tests {
		if got := valueText(tt.v); got != tt.want {
			t.Errorf("valueText(%v) = %q, want %q", tt.v, got, tt.want)
		}
	}
}

// TestDebugLineShape pins the layout: the stack sits in parentheses before
// the instruction, reading bottom to top, because upstream builds the line
// backwards.
func TestDebugLineShape(t *testing.T) {
	f := &Frame{
		Prog:  &Program{Ref: ref.Ref(58)},
		PID:   7,
		Stack: []Value{Str(""), Int(3)},
	}
	got := f.debugLine(Inst{Type: TypePrimitive, Line: 6, Num: int64(mustPrim("POP"))})
	if want := `Debug> Pid 7: #58 6 ("", 3) POP`; got != want {
		t.Errorf("debugLine() = %q, want %q", got, want)
	}
}

// TestDebugLineTruncatesTheStack checks the eight-item cut and its marker.
func TestDebugLineTruncatesTheStack(t *testing.T) {
	f := &Frame{Prog: &Program{Ref: ref.Ref(1)}, PID: 1}
	for i := 0; i < 12; i++ {
		f.Stack = append(f.Stack, Int(int64(i)))
	}
	got := f.debugLine(Inst{Type: TypePrimitive, Line: 1, Num: int64(mustPrim("POP"))})
	if !strings.Contains(got, "(..., 4, 5, 6, 7, 8, 9, 10, 11)") {
		t.Errorf("the stack was not cut to its last eight: %q", got)
	}
}
