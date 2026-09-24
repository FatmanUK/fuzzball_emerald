package muf

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// The MUF instruction tracer, upstream's debug_inst.
//
// A program flagged DARK is being debugged: the interpreter prints a
// line for every instruction it is about to run, showing where it is
// and what is on the stack. That flag is what DEBUG_ON and DEBUG_OFF
// set, and DEBUG_LINE prints a single such line for a program that is
// *not* flagged, so a program can trace only the part it cares about.
//
// Upstream's debugger is interactive as well — a breakpoint drops
// the player into a prompt where they can step and inspect. Emerald
// has no such prompt; see DEBUGGER_BREAK for what it does instead.

// maxTraceStack is how many stack items a trace line shows,
// upstream's own "count > sp - 8".
const maxTraceStack = 8

// traceStrMax is where a string in a trace line is cut, upstream's strmax of
// 30. A cut string is marked with a trailing underscore.
const traceStrMax = 30

// debugLine renders one trace line: where the program is, what is on
// the stack, and the instruction about to run.
//
//	Debug> Pid 7: #58 6 ("", 3) DEBUG_OFF
//
// The stack reads bottom to top, so the rightmost item is the one the
// instruction is about to take. Upstream builds this line backwards,
// which is what puts the stack before the instruction despite the
// code writing the instruction first; only the last eight items are
// shown, with a leading "..." when there are more.
func (f *Frame) debugLine(in Inst) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Debug> Pid %d: %s %d (", f.PID, f.Prog.Ref.String(), in.Line)

	start := len(f.Stack) - maxTraceStack
	if start < 0 {
		start = 0
	} else if start > 0 {
		b.WriteString("..., ")
	}
	for i := start; i < len(f.Stack); i++ {
		if i > start {
			b.WriteString(", ")
		}
		b.WriteString(valueText(f.Stack[i]))
	}
	b.WriteString(") ")
	b.WriteString(instText(f, in))
	return b.String()
}

// instText renders the instruction itself, upstream's insttotext.
//
// A literal is shown the way the trace shows a stack value, so the
// same string appears the same way whether it is about to be pushed
// or already has been. A variable shows its slot, with the scoped
// kind naming itself too, since a scoped slot means nothing on its
// own.
func instText(f *Frame, in Inst) string {
	switch in.Type {
	case TypeString:
		return valueText(Str(in.Str))
	case TypeFloat:
		return valueText(Float(in.Float))
	case TypeFunction:
		if in.Proc == nil {
			return "INIT FUNC"
		}
		return fmt.Sprintf("INIT FUNC: %s (%d arg%s)",
			in.Proc.Name, in.Proc.Args, plural(in.Proc.Args))
	case TypeVar:
		return "V" + strconv.FormatInt(in.Num, 10)
	case TypeLVar:
		return "LV" + strconv.FormatInt(in.Num, 10)
	case TypeSVar, TypeSVarAt, TypeSVarBang:
		name := scopedVarName(f, int(in.Num))
		switch in.Type {
		case TypeSVarAt:
			return fmt.Sprintf("SV%d:%s @", in.Num, name)
		case TypeSVarBang:
			return fmt.Sprintf("SV%d:%s !", in.Num, name)
		}
		return fmt.Sprintf("SV%d:%s", in.Num, name)
	case TypeLVarAt:
		return fmt.Sprintf("LV%d @", in.Num)
	case TypeLVarBang:
		return fmt.Sprintf("LV%d !", in.Num)
	case TypeExec:
		return "EXEC"
	case TypeAddress:
		return "'" + procNameAt(f, int(in.Num))
	}
	return in.String()
}

// scopedVarName names a scoped variable slot, which the trace shows
// beside its number. The name comes from whichever procedure is
// running, so a slot means what it means where it is used.
func scopedVarName(f *Frame, slot int) string {
	if f == nil || f.Prog == nil {
		return "?"
	}
	if p := f.procAt(f.PC); p != nil && slot < len(p.VarNames) {
		return p.VarNames[slot]
	}
	return "?"
}

// procNameAt names the procedure an address points at.
func procNameAt(f *Frame, pc int) string {
	if f == nil || f.Prog == nil || pc < 0 ||
		pc >= len(f.Prog.Code) {
		return "?"
	}
	if p := f.Prog.Code[pc].Proc; p != nil {
		return p.Name
	}
	return "?"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// valueText renders one value the way a trace line shows it,
// upstream's insttotext: a string quoted and cut at thirty
// characters, a float always carrying a decimal point, a variable by
// its slot number.
func valueText(v Value) string {
	switch v.Type {
	case TypeString:
		if len(v.Str) > traceStrMax {
			return strconv.Quote(v.Str[:traceStrMax-1]) + "_"
		}
		return strconv.Quote(v.Str)
	case TypeInteger:
		return strconv.FormatInt(v.Num, 10)
	case TypeFloat:
		s := strconv.FormatFloat(v.Float, 'g', 16, 64)
		if !strings.ContainsAny(s, ".ne") {
			s += ".0"
		}
		return s
	case TypeObject:
		return v.Ref.String()
	case TypeArray:
		if v.Array == nil {
			return "0{}"
		}
		return strconv.Itoa(v.Array.Len()) + "{...}"
	case TypeLock:
		if v.Str == "" {
			return "[TRUE_BOOLEXP]"
		}
		return "[" + v.Str + "]"
	}
	return v.String()
}

// tracing reports whether this frame should print a line per
// instruction.
//
// The answer is the program's DARK flag plus a control check, both
// upstream's: tracing prints to whoever is running the program, so it
// is only offered to someone who could have read the source anyway.
func (f *Frame) tracing() bool {
	if f.ForceTrace {
		return true
	}
	if f.host == nil {
		return false
	}
	return f.host.Flags(f.Prog.Ref).Has(ref.Dark) &&
		f.host.Controls(f.Caller, f.Prog.Ref)
}

// trace prints one line, if this frame is being traced.
func (f *Frame) trace(in Inst) {
	if f.host == nil {
		return
	}
	f.host.Notify(f.Caller, f.debugLine(in))
}
