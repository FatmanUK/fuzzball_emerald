package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ascii"

// primFunc implements one primitive. Returning a non-nil Result stops the
// interpreter; most primitives return nil and let it advance.
type primFunc func(f *Frame) (*Result, error)

// prims maps a primitive's number to its implementation. A number with no
// entry is a primitive the compiler knows but this server does not yet run,
// which is reported as such rather than silently doing nothing.
var prims = map[int]primFunc{}

// register installs a primitive by name.
func register(name string, fn primFunc) {
	n := primIndex[ascii.Fold(name)]
	if n == 0 {
		panic("muf: registering an unknown primitive " + name)
	}
	prims[n] = fn
}

// Implemented reports how many primitives have implementations, which the
// server logs at startup so the gap is visible.
func Implemented() int { return len(prims) }

// primitive runs one primitive by number.
func (f *Frame) primitive(n int) (*Result, error) {
	// The control-flow instructions the compiler emits as primitives are
	// handled here rather than in the table, because they move the program
	// counter themselves.
	switch n {
	case InExit:
		f.ret()
		return nil, nil
	case InJmp:
		return nil, errf("JMP is emitted by the compiler, not run directly")
	case InExecute:
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if v.Type != TypeAddress {
			return nil, errf("EXECUTE needs an address")
		}
		return nil, f.call(v.Addr)
	case InFor:
		return nil, f.beginCountingFor()
	case InForeach:
		return nil, f.beginForeach()
	case InForIter:
		return nil, f.forIterate()
	case InForPop:
		if len(f.fors) > 0 {
			f.fors = f.fors[:len(f.fors)-1]
		}
		f.PC++
		return nil, nil
	case InTryPop:
		if len(f.trys) > 0 {
			f.trys = f.trys[:len(f.trys)-1]
		}
		f.PC++
		return nil, nil
	case InCatch, InCatchDetailed:
		return nil, f.enterCatch(n == InCatchDetailed)
	case InRead, InSleep, InEventWaitFor:
		// Blocking needs the process queue, which arrives with M7.
		blocked := Blocked
		return &blocked, nil
	}

	fn, ok := prims[n]
	if !ok {
		return nil, errf("%s is not implemented yet", PrimName(n))
	}

	// A primitive that does not move the program counter itself just runs
	// and falls through to the next instruction.
	before := f.PC
	res, err := fn(f)
	if err != nil {
		return nil, err
	}
	if f.PC == before {
		f.PC++
	}
	return res, nil
}

// enterCatch makes the caught error available to the handler.
func (f *Frame) enterCatch(detailed bool) error {
	err := f.err
	f.err = nil
	if err == nil {
		err = errf("unknown error")
	}

	if !detailed {
		f.PC++
		return f.Push(Str(err.Msg))
	}

	// CATCH_DETAILED hands the handler a dictionary describing the failure.
	d := NewDict()
	d.Set(Str("error"), Str(err.Msg))
	d.Set(Str("instr"), Str(err.Prim))
	d.Set(Str("line"), Int(int64(err.Line)))
	d.Set(Str("program"), Obj(err.Prog))
	f.PC++
	return f.Push(Arr(d))
}

// beginCountingFor starts "start end step FOR".
func (f *Frame) beginCountingFor() error {
	vals, err := f.PopN(3)
	if err != nil {
		return err
	}
	start, end, step := vals[0], vals[1], vals[2]
	if start.Type != TypeInteger || end.Type != TypeInteger || step.Type != TypeInteger {
		return errf("FOR needs three integers")
	}
	if step.Num == 0 {
		return errf("FOR needs a non-zero step")
	}
	f.fors = append(f.fors, forLoop{
		counting: true,
		cur:      start.Num,
		end:      end.Num,
		step:     step.Num,
	})
	f.PC++
	return nil
}

// beginForeach starts "array FOREACH".
func (f *Frame) beginForeach() error {
	v, err := f.Pop()
	if err != nil {
		return err
	}
	if v.Type != TypeArray || v.Array == nil {
		return errf("FOREACH needs an array")
	}
	f.fors = append(f.fors, forLoop{
		keys: v.Array.Keys(),
		vals: v.Array.Values(),
	})
	f.PC++
	return nil
}

// forIterate pushes the next iteration's values and a flag saying whether the
// loop continues. The compiler follows it with a conditional jump out.
func (f *Frame) forIterate() error {
	if len(f.fors) == 0 {
		return errf("loop iteration outside a loop")
	}
	loop := &f.fors[len(f.fors)-1]
	f.PC++

	if loop.counting {
		done := loop.step > 0 && loop.cur > loop.end ||
			loop.step < 0 && loop.cur < loop.end
		if done {
			return f.Push(Bool(false))
		}
		if err := f.Push(Int(loop.cur)); err != nil {
			return err
		}
		loop.cur += loop.step
		return f.Push(Bool(true))
	}

	if loop.idx >= len(loop.keys) {
		return f.Push(Bool(false))
	}
	// FOREACH hands the body a key and a value.
	if err := f.Push(loop.keys[loop.idx]); err != nil {
		return err
	}
	if err := f.Push(loop.vals[loop.idx]); err != nil {
		return err
	}
	loop.idx++
	return f.Push(Bool(true))
}
