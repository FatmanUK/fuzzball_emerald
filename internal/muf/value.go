package muf

import (
	"fmt"
	"math"
	"strconv"

	"github.com/FatmanUK/fuzzball_emerald/internal/boolexp"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Value is a runtime stack value.
//
// It is a tagged struct rather than an interface: MUF pushes and pops
// constantly, and boxing every integer would dominate the
// interpreter's cost.
type Value struct {
	Type  Type
	Num   int64
	Float float64
	Str   string
	Ref   ref.Ref
	Array *Array
	// Addr is a code address, for TypeAddress.
	Addr int
	// Lock holds a TypeLock value's parsed expression. A nil Lock
	// is TRUE_BOOLEXP — an unlocked lock — not the absence of
	// a value; PARSELOCK is the only primitive that produces one.
	Lock *boolexp.Expr
}

// Constructors, which keep call sites readable.
func Int(n int64) Value { return Value{Type: TypeInteger, Num: n} }
func Bool(b bool) Value {
	n := int64(0)
	if b {
		n = 1
	}
	return Int(n)
}
func Float(f float64) Value {
	return Value{Type: TypeFloat, Float: f}
}
func Str(s string) Value {
	return Value{Type: TypeString, Str: s}
}
func Obj(r ref.Ref) Value {
	return Value{Type: TypeObject, Ref: r}
}
func Arr(a *Array) Value {
	return Value{Type: TypeArray, Array: a}
}
func Mark() Value { return Value{Type: TypeMark} }
func LockVal(b *boolexp.Expr) Value {
	return Value{Type: TypeLock, Lock: b}
}

// Truthy reports whether a value counts as true, which MUF decides
// per type: a non-zero number, a non-empty string, a valid dbref, a
// non-empty array.
func (v Value) Truthy() bool {
	switch v.Type {
	case TypeInteger:
		return v.Num != 0
	case TypeFloat:
		return v.Float != 0
	case TypeString:
		return v.Str != ""
	case TypeObject:
		// #-1 and the other sentinels are false; a real
		// object is true.
		return v.Ref.Ok()
	case TypeArray:
		return v.Array != nil && v.Array.Len() > 0
	case TypeAddress, TypeLock:
		return true
	default:
		return false
	}
}

// String renders a value the way MUF's own conversions do.
func (v Value) String() string {
	switch v.Type {
	case TypeInteger:
		return strconv.FormatInt(v.Num, 10)
	case TypeFloat:
		return formatFloat(v.Float)
	case TypeString:
		return v.Str
	case TypeObject:
		return v.Ref.String()
	case TypeArray:
		return "<array>"
	case TypeAddress:
		return "<address>"
	case TypeMark:
		return "<mark>"
	default:
		return "<" + v.Type.String() + ">"
	}
}

// formatFloat renders a float the way MUF prints one, including the
// non-standard spellings Fuzzball uses for the infinities.
func formatFloat(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "Inf"
	case math.IsInf(f, -1):
		return "-Inf"
	case math.IsNaN(f):
		return "NaN"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// Equal reports whether two values compare equal, across the numeric
// types.
func (v Value) Equal(w Value) bool {
	if v.Type == TypeFloat || w.Type == TypeFloat {
		a, aok := v.asFloat()
		b, bok := w.asFloat()
		return aok && bok && a == b
	}
	if v.Type != w.Type {
		// An integer and a dbref never compare equal,
		// matching MUF's strictness about the two.
		return false
	}
	switch v.Type {
	case TypeInteger:
		return v.Num == w.Num
	case TypeString:
		return v.Str == w.Str
	case TypeObject:
		return v.Ref == w.Ref
	case TypeArray:
		return v.Array == w.Array
	case TypeAddress:
		return v.Addr == w.Addr
	}
	return false
}

// asFloat converts a numeric value to a float.
func (v Value) asFloat() (float64, bool) {
	switch v.Type {
	case TypeInteger:
		return float64(v.Num), true
	case TypeFloat:
		return v.Float, true
	}
	return 0, false
}

// Error is a MUF runtime failure. It carries the instruction that
// raised it so the interpreter can report a line.
type Error struct {
	Msg  string
	Prim string
	Line int
	Prog ref.Ref
}

func (e *Error) Error() string {
	if e.Prim != "" {
		return fmt.Sprintf("%s: %s", e.Prim, e.Msg)
	}
	return e.Msg
}

// errf builds a runtime error.
func errf(format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...)}
}

// errSilentAbort is upstream's ERROR_DIE_NOW, a sentinel Run
// recognises and handles differently from every other error: it is
// never caught by TRY and never produces an error report. KILL uses
// it when a program kills its own pid, matching prim_kill's
// do_abort_silent.
var errSilentAbort = &Error{Msg: "killed"}
