package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ref"

// Host is what a running program can reach outside itself.
//
// The interpreter takes it as an interface so this package needs no world:
// primitives that only move values around never touch it, and the ones that
// do are explicit about it.
type Host interface {
	// Notify sends a line to an object's connections.
	Notify(who ref.Ref, msg string)
	// GetPropStr reads a property as a string, empty when unset.
	GetPropStr(obj ref.Ref, path string) string
	// SetPropStr writes a string property.
	SetPropStr(obj ref.Ref, path, val string)
	// Name returns an object's name.
	Name(obj ref.Ref) string
	// Location returns what an object is inside.
	Location(obj ref.Ref) ref.Ref
	// Owner returns who owns an object.
	Owner(obj ref.Ref) ref.Ref
	// Valid reports whether a ref names a live object.
	Valid(obj ref.Ref) bool
	// ObjType returns an object's type.
	ObjType(obj ref.Ref) ref.ObjType
}

// callSite records where a call came from, so EXIT can return to it.
type callSite struct {
	pc int
	// scopeBase is how many scope frames were open before the call.
	scopeBase int
}

// forLoop is one active FOR or FOREACH.
type forLoop struct {
	// list is what FOREACH walks; nil for a counting FOR.
	keys []Value
	vals []Value
	idx  int

	// cur, end and step drive a counting FOR.
	cur, end, step int64
	counting       bool
}

// tryBlock is one active TRY, recording where to resume and how much stack to
// restore when something is caught.
type tryBlock struct {
	catchPC  int
	stackTop int
	callTop  int
	forTop   int
	scopeTop int
	detailed bool
}

// Frame is one running program.
type Frame struct {
	Prog *Program
	PC   int

	// Stack is the argument stack the program pushes and pops.
	Stack []Value

	// Vars are the program's globals, LVars its program-locals, and scopes
	// the per-call scoped variables.
	Vars   []Value
	LVars  []Value
	scopes [][]Value

	calls []callSite
	fors  []forLoop
	trys  []tryBlock

	// Instructions counts what has run, which bounds a runaway program.
	Instructions int

	// Err holds the error a TRY has not yet caught.
	err *Error

	// ErrorFlags records the arithmetic conditions a program can ask about
	// with is_set?, rather than being told about by an abort. Fuzzball
	// treats integer division by zero as a flag and a zero result, not a
	// failure.
	ErrorFlags ErrorFlags

	// Caller identifies who is running the program, for the reserved
	// variables and for permission checks.
	Caller ref.Ref
	Trig   ref.Ref

	host Host
}

// NewFrame prepares a program to run.
func NewFrame(p *Program, host Host) *Frame {
	f := &Frame{
		Prog:  p,
		PC:    p.Start,
		Vars:  make([]Value, len(p.Vars)),
		LVars: make([]Value, len(p.LVars)),
		host:  host,
	}
	// Every variable starts as integer zero, which is what interp() fills
	// them with before overwriting the four reserved ones.
	for i := range f.Vars {
		f.Vars[i] = Int(0)
	}
	for i := range f.LVars {
		f.LVars[i] = Int(0)
	}
	return f
}

// ErrorFlags are the arithmetic conditions is_set? reports. The order matches
// the bits union error_mask defines, because is_set? takes the number.
type ErrorFlags struct {
	DivZero   bool
	NaN       bool
	Imaginary bool
	FBounds   bool
	IBounds   bool
}

// Get reads a flag by the number is_set? uses.
func (e ErrorFlags) Get(n int) bool {
	switch n {
	case 0:
		return e.DivZero
	case 1:
		return e.NaN
	case 2:
		return e.Imaginary
	case 3:
		return e.FBounds
	case 4:
		return e.IBounds
	}
	return false
}

// Set writes a flag by number.
func (e *ErrorFlags) Set(n int, v bool) {
	switch n {
	case 0:
		e.DivZero = v
	case 1:
		e.NaN = v
	case 2:
		e.Imaginary = v
	case 3:
		e.FBounds = v
	case 4:
		e.IBounds = v
	}
}

// Clear resets every flag.
func (e *ErrorFlags) Clear() { *e = ErrorFlags{} }

// SetReserved fills the four variables every program starts with, and puts the
// command's argument on the stack.
//
// That last part is easy to miss and load-bearing: interp() pushes the
// argument string before the program runs, so a program starts with one value
// on the stack rather than none, and "depth" reflects it.
func (f *Frame) SetReserved(me, loc, trigger ref.Ref, command string) {
	if len(f.Vars) < ReservedVars {
		return
	}
	f.Vars[VarMe] = Obj(me)
	f.Vars[VarLoc] = Obj(loc)
	f.Vars[VarTrigger] = Obj(trigger)
	f.Vars[VarCommand] = Str(command)
	f.Caller, f.Trig = me, trigger
	f.Stack = append(f.Stack, Str(command))
}

// Push puts a value on the stack.
func (f *Frame) Push(v Value) error {
	if len(f.Stack) >= StackSize {
		return errf("stack overflow")
	}
	f.Stack = append(f.Stack, v)
	return nil
}

// Pop takes the top value.
func (f *Frame) Pop() (Value, error) {
	if len(f.Stack) == 0 {
		return Value{}, errf("stack underflow")
	}
	v := f.Stack[len(f.Stack)-1]
	f.Stack = f.Stack[:len(f.Stack)-1]
	return v, nil
}

// PopN takes the top n values, leftmost first.
func (f *Frame) PopN(n int) ([]Value, error) {
	if len(f.Stack) < n {
		return nil, errf("stack underflow")
	}
	out := make([]Value, n)
	copy(out, f.Stack[len(f.Stack)-n:])
	f.Stack = f.Stack[:len(f.Stack)-n]
	return out, nil
}

// Peek returns the value n places from the top without removing it.
func (f *Frame) Peek(n int) (Value, error) {
	if n < 0 || n >= len(f.Stack) {
		return Value{}, errf("stack underflow")
	}
	return f.Stack[len(f.Stack)-1-n], nil
}

// Depth is how many values are on the stack.
func (f *Frame) Depth() int { return len(f.Stack) }

// Host returns what the frame can reach outside itself.
func (f *Frame) Host() Host { return f.host }

// scope returns the scoped variables of the innermost call.
func (f *Frame) scope() []Value {
	if len(f.scopes) == 0 {
		return nil
	}
	return f.scopes[len(f.scopes)-1]
}
