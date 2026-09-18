package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ref"

// Stack and variable primitives.

func init() {
	register("POP", func(f *Frame) (*Result, error) {
		_, err := f.Pop()
		return nil, err
	})
	register("DUP", func(f *Frame) (*Result, error) {
		v, err := f.Peek(0)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(v)
	})
	register("?DUP", func(f *Frame) (*Result, error) {
		v, err := f.Peek(0)
		if err != nil {
			return nil, err
		}
		if !v.Truthy() {
			return nil, nil
		}
		return nil, f.Push(v)
	})
	register("SWAP", func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		if err := f.Push(v[1]); err != nil {
			return nil, err
		}
		return nil, f.Push(v[0])
	})
	register("OVER", func(f *Frame) (*Result, error) {
		v, err := f.Peek(1)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(v)
	})
	register("NIP", func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(v[1])
	})
	register("TUCK", func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		for _, x := range []Value{v[1], v[0], v[1]} {
			if err := f.Push(x); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	register("ROT", func(f *Frame) (*Result, error) {
		v, err := f.PopN(3)
		if err != nil {
			return nil, err
		}
		for _, x := range []Value{v[1], v[2], v[0]} {
			if err := f.Push(x); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	register("-ROT", func(f *Frame) (*Result, error) {
		v, err := f.PopN(3)
		if err != nil {
			return nil, err
		}
		for _, x := range []Value{v[2], v[0], v[1]} {
			if err := f.Push(x); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	register("DEPTH", func(f *Frame) (*Result, error) {
		return nil, f.Push(Int(int64(f.Depth())))
	})
	register("PICK", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		if n < 1 {
			return nil, errf("PICK needs a positive position")
		}
		v, err := f.Peek(int(n) - 1)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(v)
	})
	register("POPN", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, errf("POPN needs a count that is not negative")
		}
		_, err = f.PopN(int(n))
		return nil, err
	})

	// Variable access. "@" reads whatever kind of variable reference is on
	// the stack, and "!" writes one.
	register("@", func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		got, err := f.readVar(v)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(got)
	})
	register("!", func(f *Frame) (*Result, error) {
		vals, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		return nil, f.writeVar(vals[1], vals[0])
	})

	register("INT?", typeTest(TypeInteger))
	register("STRING?", typeTest(TypeString))
	register("DBREF?", typeTest(TypeObject))
	register("FLOAT?", typeTest(TypeFloat))
	register("ARRAY?", typeTest(TypeArray))
	register("ADDRESS?", typeTest(TypeAddress))
	register("DICTIONARY?", func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(v.Type == TypeArray && v.Array != nil && !v.Array.IsList()))
	})

	register("{", func(f *Frame) (*Result, error) {
		return nil, f.Push(Mark())
	})
	register("}", func(f *Frame) (*Result, error) {
		// Count what has been pushed since the matching mark and replace
		// the mark with that count, which is what array_make consumes.
		n := 0
		for i := len(f.Stack) - 1; i >= 0; i-- {
			if f.Stack[i].Type == TypeMark {
				// Drop the mark, leaving the values above it.
				copy(f.Stack[i:], f.Stack[i+1:])
				f.Stack = f.Stack[:len(f.Stack)-1]
				return nil, f.Push(Int(int64(n)))
			}
			n++
		}
		return nil, errf("} without a matching {")
	})

	register("PROG", func(f *Frame) (*Result, error) {
		return nil, f.Push(Obj(f.Prog.Ref))
	})
	register("TRIG", func(f *Frame) (*Result, error) {
		return nil, f.Push(Obj(f.Trig))
	})
	register("CALLER", func(f *Frame) (*Result, error) {
		return nil, f.Push(Obj(f.Caller))
	})
}

// typeTest builds a primitive that reports whether the top value has a type.
func typeTest(want Type) primFunc {
	return func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(v.Type == want))
	}
}

// readVar resolves a variable reference to its value.
func (f *Frame) readVar(v Value) (Value, error) {
	switch v.Type {
	case TypeVar:
		if int(v.Num) >= len(f.Vars) {
			return Value{}, errf("Variable number out of range.")
		}
		return f.Vars[v.Num], nil
	case TypeLVar:
		if int(v.Num) >= len(f.LVars) {
			return Value{}, errf("Variable number out of range.")
		}
		return f.LVars[v.Num], nil
	case TypeSVar:
		return f.getScoped(int(v.Num))
	}
	return Value{}, errf("Invalid datatype in variable.")
}

// writeVar stores a value through a variable reference.
func (f *Frame) writeVar(target, val Value) error {
	switch target.Type {
	case TypeVar:
		if int(target.Num) >= len(f.Vars) {
			return errf("Variable number out of range.")
		}
		f.Vars[target.Num] = val
		return nil
	case TypeLVar:
		if int(target.Num) >= len(f.LVars) {
			return errf("Variable number out of range.")
		}
		f.LVars[target.Num] = val
		return nil
	case TypeSVar:
		return f.setScoped(int(target.Num), val)
	}
	return errf("Invalid datatype in variable.")
}

// popInt takes an integer from the stack.
func (f *Frame) popInt() (int64, error) {
	v, err := f.Pop()
	if err != nil {
		return 0, err
	}
	if v.Type != TypeInteger {
		return 0, errf("Non-integer argument.")
	}
	return v.Num, nil
}

// popStrArg takes a string, naming which argument it was when the type is
// wrong. Upstream's messages carry that number and programs match on them.
func (f *Frame) popStrArg(n int) (string, error) {
	v, err := f.Pop()
	if err != nil {
		return "", err
	}
	if v.Type != TypeString {
		return "", errf("Non-string argument (%d)", n)
	}
	return v.Str, nil
}

// popStr takes a string from the stack.
func (f *Frame) popStr() (string, error) {
	v, err := f.Pop()
	if err != nil {
		return "", err
	}
	if v.Type != TypeString {
		return "", errf("Non-string argument.")
	}
	return v.Str, nil
}

// popRef takes a dbref from the stack.
func (f *Frame) popRef() (ref.Ref, error) {
	v, err := f.Pop()
	if err != nil {
		return ref.Nothing, err
	}
	if v.Type != TypeObject {
		return ref.Nothing, errf("Non-object argument.")
	}
	return v.Ref, nil
}

// Stack primitives that move several values at once, and the remaining type
// and mode queries.

func init() {
	register("ROTATE", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		if n == 0 || n == 1 || n == -1 {
			return nil, nil
		}
		count := n
		if count < 0 {
			count = -count
		}
		if int(count) > f.Depth() {
			return nil, errf("stack underflow")
		}
		vals, err := f.PopN(int(count))
		if err != nil {
			return nil, err
		}
		if n > 0 {
			// Bring the deepest of the group to the top.
			vals = append(vals[1:], vals[0])
		} else {
			// Send the top of the group to the bottom.
			vals = append(vals[len(vals)-1:], vals[:len(vals)-1]...)
		}
		for _, v := range vals {
			if err := f.Push(v); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})

	register("PUT", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if n < 1 || int(n) > f.Depth() {
			return nil, errf("PUT needs a position on the stack")
		}
		f.Stack[f.Depth()-int(n)] = v
		return nil, nil
	})

	register("REVERSE", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, errf("REVERSE needs a count that is not negative")
		}
		vals, err := f.PopN(int(n))
		if err != nil {
			return nil, err
		}
		for i := len(vals) - 1; i >= 0; i-- {
			if err := f.Push(vals[i]); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})

	register("LREVERSE", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, errf("LREVERSE needs a count that is not negative")
		}
		vals, err := f.PopN(int(n))
		if err != nil {
			return nil, err
		}
		for i := len(vals) - 1; i >= 0; i-- {
			if err := f.Push(vals[i]); err != nil {
				return nil, err
			}
		}
		return nil, f.Push(Int(n))
	})

	register("DUPN", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		if n < 0 || int(n) > f.Depth() {
			return nil, errf("stack underflow")
		}
		base := f.Depth() - int(n)
		for i := 0; i < int(n); i++ {
			if err := f.Push(f.Stack[base+i]); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})

	register("LDUP", func(f *Frame) (*Result, error) {
		top, err := f.Peek(0)
		if err != nil {
			return nil, err
		}
		if top.Type != TypeInteger {
			return nil, errf("LDUP needs a count on top of the stack")
		}
		n := int(top.Num)
		if n < 0 || n+1 > f.Depth() {
			return nil, errf("stack underflow")
		}
		base := f.Depth() - n - 1
		for i := 0; i <= n; i++ {
			if err := f.Push(f.Stack[base+i]); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})

	register("FULLDEPTH", func(f *Frame) (*Result, error) {
		return nil, f.Push(Int(int64(f.Depth())))
	})

	register("LOCK?", typeTest(TypeLock))
	register("VARIABLE", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Value{Type: TypeVar, Num: n})
	})
	register("LOCALVAR", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Value{Type: TypeLVar, Num: n})
	})

	// The multitasking modes. Real scheduling arrives with the process
	// queue; for now a program may set and read its mode.
	register("MODE", func(f *Frame) (*Result, error) {
		return nil, f.Push(Int(int64(f.Mode)))
	})
	register("SETMODE", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		f.Mode = int(n)
		return nil, nil
	})
	register("PREEMPT", func(f *Frame) (*Result, error) {
		f.Mode = ModePreempt
		return nil, nil
	})
	register("FOREGROUND", func(f *Frame) (*Result, error) {
		f.Mode = ModeForeground
		return nil, nil
	})
	register("BACKGROUND", func(f *Frame) (*Result, error) {
		f.Mode = ModeBackground
		return nil, nil
	})
}
