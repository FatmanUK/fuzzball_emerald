package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ref"

// Result says why the interpreter stopped.
type Result int

const (
	// Done means the program ran to completion.
	Done Result = iota
	// Blocked means it is waiting for input or a timer. M7 wires
	// those up; until then the interpreter reports it and stops.
	Blocked
	// Yielded means its instruction slice ran out and it should
	// be resumed.
	Yielded
)

// Limits bound what a program may do.
type Limits struct {
	// Slice is instr_slice: how many instructions run before the
	// interpreter yields, so one program cannot monopolise the
	// world goroutine. Zero means the default.
	Slice int
	// Total is max_instr_count, which upstream uses for **two**
	// different ceilings — the preempt-mode abort and the total
	// ceiling below mucker 3. Zero means the default.
	Total int
	// ML4Preempt is max_ml4_preempt_count, the preempt ceiling
	// for a mucker-4 program. **Zero disables the check** rather
	// than meaning a default: upstream resets the counter instead
	// (interp.c:1735), so a level-4 program runs unbounded in
	// preempt mode when the parameter is zero.
	ML4Preempt int
}

// Default instruction limits. Upstream tunes both; these stand in
// until the @tune wiring arrives with the rest of the process
// machinery.
const (
	DefaultSlice = 10_000
	DefaultTotal = 20_000_000
)

func (l Limits) slice() int {
	if l.Slice <= 0 {
		return DefaultSlice
	}
	return l.Slice
}

func (l Limits) total() int {
	if l.Total <= 0 {
		return DefaultTotal
	}
	return l.Total
}

// Run executes until the program finishes, yields, or fails.
//
// The three instruction limits are upstream's and none of them was
// applied the way interp_loop applies it. This server counted every
// program against one unconditional ceiling and aborted with an
// invented message; upstream has a **mode-dependent** ceiling, a
// **mucker-dependent** one, and a yield condition in two parts.
//
//   - **Preempt** mode never yields. Instead the instructions run
//     since this resume are capped: at mucker 4 by ML4Preempt when
//     it is non-zero, and otherwise by Total, aborting "Maximum
//     preempt instruction count exceeded" (interp.c:1731, :1743).
//     A program flagged BUILDER counts as preempt whatever its
//     mode says (:1728), which is what the "B" flag means on a
//     program.
//   - **Foreground and background** yield, but only once the frame
//     has run `Slice * 4` instructions in total *and* `Slice` since
//     this resume (:1753). So a short program runs to completion
//     without ever yielding, where a single budget would have made
//     it yield at Slice.
//   - **Below mucker 3** there is a total ceiling as well, at
//     `Total` for level 1 and **four times** that for level 2
//     (:1876), aborting "Maximum total instruction count exceeded."
//     At level 3 and above there is no total ceiling at all — this
//     server imposed one on every program.
func (f *Frame) Run(lim Limits) (Result, error) {
	slice, total := lim.slice(), lim.total()

	// local is upstream's instr_count: instructions run since
	// this resume, as against f.Instructions, which is the
	// frame's own lifetime count (fr->instcnt).
	local := 0

	// Whether this program is being traced is settled here rather
	// than per instruction: it depends on a flag and a control
	// check, and asking the host for both on every instruction
	// would cost more than the tracing does. DEBUG_ON and
	// DEBUG_OFF set Traced directly, so a program that turns
	// tracing on part-way through takes effect at once; an @set
	// from outside is picked up when this frame next resumes.
	f.Traced = f.tracing()

	for {
		if f.PC < 0 || f.PC >= len(f.Prog.Code) {
			return Done, nil
		}

		// Upstream increments both counters *before* every
		// check below (interp.c:1721), so the first
		// instruction after a resume counts as one.
		f.Instructions++
		local++

		if f.preempting() {
			if f.MLevel() == 4 {
				over := lim.ML4Preempt > 0 &&
					local >= lim.ML4Preempt
				if over {
					return Done, f.raise(
						errf(preemptCapMsg))
				}
				if lim.ML4Preempt <= 0 {
					local = 0
				}
			} else if local >= total {
				return Done, f.raise(
					errf(preemptCapMsg))
			}
		} else if f.Instructions > slice*4 && local >= slice {
			return Yielded, nil
		}

		if lv := f.MLevel(); lv < 3 {
			ceiling := total
			if lv == 2 {
				ceiling *= 4
			}
			if f.Instructions > ceiling {
				return Done, f.raise(errf(
					"Maximum total instruction " +
						"count exceeded."))
			}
		}

		in := f.Prog.Code[f.PC]
		if f.Traced {
			f.trace(in)
		}
		res, err := f.step(in)
		if err != nil {
			// errSilentAbort is upstream's ERROR_DIE_NOW:
			// KILLing the running program's own pid ends
			// it immediately, skipping even an open TRY,
			// and produces no error report at all — the
			// one abort that is not decorated or unwound
			// like every other.
			if err == errSilentAbort {
				return Done, nil
			}
			// A raised error unwinds to the innermost
			// TRY; if none is open it ends the program.
			if caught := f.unwind(err); !caught {
				return Done, f.decorate(err, in)
			}
			continue
		}
		if res != nil {
			return *res, nil
		}
	}
}

// step runs one instruction. It returns a non-nil Result when the
// program should stop.
func (f *Frame) step(in Inst) (*Result, error) {
	switch in.Type {
	case TypeFunction:
		// Entering a procedure opens a scope for its
		// variables. The arguments are already on the stack;
		// a procedure that declares them pops them itself
		// through SVAR!.
		f.scopes = append(f.scopes, make([]Value, in.Proc.Vars))
		if n := in.Proc.Args; n > 0 {
			args, err := f.PopN(n)
			if err != nil {
				return nil, err
			}
			copy(f.scopes[len(f.scopes)-1], args)
		}
		f.PC++

	case TypeInteger:
		f.PC++
		return nil, f.Push(Int(in.Num))
	case TypeFloat:
		f.PC++
		return nil, f.Push(Float(in.Float))
	case TypeString:
		f.PC++
		return nil, f.Push(Str(in.Str))
	case TypeObject:
		f.PC++
		return nil, f.Push(Obj(in.Ref))
	case TypeMark:
		f.PC++
		return nil, f.Push(Mark())

	case TypeVar:
		f.PC++
		return nil, f.Push(Value{Type: TypeVar, Num: in.Num})
	case TypeLVar:
		f.PC++
		return nil, f.Push(Value{Type: TypeLVar, Num: in.Num})
	case TypeSVar:
		f.PC++
		return nil, f.Push(Value{Type: TypeSVar, Num: in.Num})

	case TypeSVarBang:
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if err := f.setScoped(int(in.Num), v); err != nil {
			return nil, err
		}
		f.PC++

	case TypeSVarAt:
		v, err := f.getScoped(int(in.Num))
		if err != nil {
			return nil, err
		}
		f.PC++
		return nil, f.Push(v)

	case TypeLVarBang:
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if int(in.Num) >= len(f.LVars) {
			return nil, errf("local variable out of range")
		}
		f.LVars[in.Num] = v
		f.PC++

	case TypeLVarAt:
		if int(in.Num) >= len(f.LVars) {
			return nil, errf("local variable out of range")
		}
		v := f.LVars[in.Num]
		f.PC++
		return nil, f.Push(v)

	case TypeAddress:
		f.PC++
		return nil, f.Push(Value{Type: TypeAddress, Addr: int(in.Num)})

	case TypeJmp:
		f.PC = int(in.Num)

	case TypeIf:
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if v.Truthy() {
			f.PC++
		} else {
			f.PC = int(in.Num)
		}

	case TypeExec:
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		target := 0
		switch v.Type {
		case TypeInteger:
			target = int(v.Num)
		case TypeAddress:
			target = v.Addr
		default:
			return nil, errf("EXECUTE needs an address")
		}
		return nil, f.call(target)

	case TypeTry:
		// TRY takes a count: how many stack items the guarded
		// block consumes. Catching unwinds to exactly the
		// depth below them, so the handler sees the stack as
		// it was before the block ran rather than whatever it
		// left half-built.
		n, err := f.Pop()
		if err != nil {
			return nil, errf("Stack Underflow.")
		}
		if n.Type != TypeInteger || n.Num < 0 {
			return nil, errf("Argument is not a positive integer.")
		}
		if int(n.Num) > len(f.Stack) {
			return nil, errf("Stack Underflow.")
		}
		// A nested TRY may not reach below what the one
		// outside it protects.
		if len(f.trys) > 0 {
			outer := f.trys[len(f.trys)-1]
			if len(f.Stack)-outer.stackTop < int(n.Num) {
				return nil, errf("Stack protection fault.")
			}
		}
		f.trys = append(f.trys, tryBlock{
			catchPC:  int(in.Num),
			stackTop: len(f.Stack) - int(n.Num),
			callTop:  len(f.calls),
			forTop:   len(f.fors),
			scopeTop: len(f.scopes),
		})
		f.PC++

	case TypePrimitive:
		return f.primitive(int(in.Num))

	case TypeCleared:
		f.PC++

	default:
		return nil, errf("cannot execute a %v instruction", in.Type)
	}
	return nil, nil
}

// call enters a procedure at addr.
func (f *Frame) call(addr int) error {
	if addr < 0 || addr >= len(f.Prog.Code) {
		return errf("call to an address outside the program")
	}
	if len(f.calls) >= maxCallDepth {
		return errf("call depth exceeded")
	}
	f.calls = append(f.calls, callSite{pc: f.PC + 1, scopeBase: len(f.scopes)})
	f.PC = addr
	return nil
}

// maxCallDepth bounds recursion, so a program that calls itself
// forever fails rather than exhausting memory.
const maxCallDepth = 1024

// ret returns from a procedure.
func (f *Frame) ret() {
	if len(f.calls) == 0 {
		// Returning from the outermost procedure ends the
		// program.
		f.PC = len(f.Prog.Code)
		return
	}
	site := f.calls[len(f.calls)-1]
	f.calls = f.calls[:len(f.calls)-1]
	f.scopes = f.scopes[:site.scopeBase]
	f.PC = site.pc
}

// getScoped reads a scoped variable.
func (f *Frame) getScoped(slot int) (Value, error) {
	s := f.scope()
	if slot < 0 || slot >= len(s) {
		return Value{}, errf("Scoped variable number out of range.")
	}
	return s[slot], nil
}

// setScoped writes a scoped variable, growing the scope when a
// procedure declares more variables as it goes.
func (f *Frame) setScoped(slot int, v Value) error {
	if len(f.scopes) == 0 {
		return errf("no scope for a scoped variable")
	}
	s := f.scopes[len(f.scopes)-1]
	for slot >= len(s) {
		s = append(s, Obj(ref.Nothing))
	}
	s[slot] = v
	f.scopes[len(f.scopes)-1] = s
	return nil
}

// unwind hands an error to the innermost TRY, restoring the stack to
// where the block started. It reports whether anything caught it.
func (f *Frame) unwind(err error) bool {
	if len(f.trys) == 0 {
		return false
	}
	t := f.trys[len(f.trys)-1]
	f.trys = f.trys[:len(f.trys)-1]

	f.Stack = f.Stack[:t.stackTop]
	f.calls = f.calls[:t.callTop]
	f.fors = f.fors[:t.forTop]
	f.scopes = f.scopes[:t.scopeTop]

	me, ok := err.(*Error)
	if !ok {
		me = errf("%s", err.Error())
	}
	f.err = me
	f.PC = t.catchPC
	return true
}

// decorate attaches the failing instruction's line to an error.
func (f *Frame) decorate(err error, in Inst) error {
	me, ok := err.(*Error)
	if !ok {
		return err
	}
	if me.Line == 0 {
		me.Line = in.Line
	}
	if me.Prim == "" && in.Type == TypePrimitive {
		me.Prim = PrimName(int(in.Num))
	}
	me.Prog = f.Prog.Ref
	return me
}

// raise decorates an error with the current instruction.
func (f *Frame) raise(err error) error {
	if f.PC >= 0 && f.PC < len(f.Prog.Code) {
		return f.decorate(err, f.Prog.Code[f.PC])
	}
	return err
}

// preemptCapMsg is abort_loop_hard's wording at interp.c:1732 and
// :1744, which both sites share.
const preemptCapMsg = "Maximum preempt instruction count exceeded"

// preempting reports whether this frame is subject to the
// preempt-mode instruction cap rather than the yield.
//
// It is PREEMPT mode **or** the BUILDER flag on the program
// (interp.c:1728), which is what "B" means on a program object: it
// runs without yielding, so it must be bounded by an instruction
// count instead.
func (f *Frame) preempting() bool {
	if f.Mode == ModePreempt {
		return true
	}
	if f.host == nil || f.Prog == nil {
		return false
	}
	return f.host.Flags(f.Prog.Ref)&ref.Builder != 0
}
