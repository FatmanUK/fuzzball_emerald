package muf

import (
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// BacktraceFrame is one level of a failing program's call stack.
type BacktraceFrame struct {
	// Level counts outwards from the innermost call, which is zero.
	Level int
	// Program is the object the code belongs to.
	Program ref.Ref
	// Line is the source line being executed at this level.
	Line int
	// Func is the procedure's name, or "???" when the address is not
	// inside one.
	Func string
	// Args renders the procedure's arguments as "name=value".
	Args []string
}

// Report describes a failure fully enough to print what upstream prints.
type Report struct {
	Program ref.Ref
	Line    int
	// Inst is the instruction that failed, which for a primitive is its
	// name. Upstream shows it before the message.
	Inst string
	Msg  string

	Frames []BacktraceFrame
}

// maxArgText is how much of an argument's value a backtrace shows, matching
// the limit upstream passes to insttotext.
const maxArgText = 30

// Backtrace renders the call stack, innermost first.
func (f *Frame) Backtrace() []BacktraceFrame {
	var out []BacktraceFrame

	// The innermost level is wherever the program counter is now; the rest
	// come from the return addresses stacked under it.
	pcs := make([]int, 0, len(f.calls)+1)
	pcs = append(pcs, f.PC)
	for i := len(f.calls) - 1; i >= 0; i-- {
		// The stored return address is what upstream prints, not the
		// call it came from. For "foo ;" that is the EXIT after the
		// call, so a caller's line is the line of its own ';'.
		pcs = append(pcs, f.calls[i].pc)
	}

	for level, pc := range pcs {
		bf := BacktraceFrame{
			Level:   level,
			Program: f.Prog.Ref,
			Func:    "???",
		}
		if pc >= 0 && pc < len(f.Prog.Code) {
			bf.Line = f.Prog.Code[pc].Line
		}
		if proc := f.procAt(pc); proc != nil {
			bf.Func = proc.Name
			bf.Args = f.argText(level, proc)
		}
		out = append(out, bf)
	}
	return out
}

// procAt finds the procedure an address belongs to, by scanning back to the
// nearest function header.
func (f *Frame) procAt(pc int) *Proc {
	if pc < 0 || pc >= len(f.Prog.Code) {
		return nil
	}
	for i := pc; i >= 0; i-- {
		if f.Prog.Code[i].Type == TypeFunction {
			return f.Prog.Code[i].Proc
		}
	}
	return nil
}

// argText renders a procedure's arguments at one call level.
func (f *Frame) argText(level int, proc *Proc) []string {
	if proc.Args == 0 {
		return nil
	}
	// Level zero is the innermost scope, which is the last one opened.
	idx := len(f.scopes) - 1 - level
	if idx < 0 || idx >= len(f.scopes) {
		return nil
	}
	scope := f.scopes[idx]

	out := make([]string, 0, proc.Args)
	for i := 0; i < proc.Args && i < len(proc.VarNames); i++ {
		val := "?"
		if i < len(scope) {
			val = truncateText(scope[i].Display(), maxArgText)
		}
		out = append(out, proc.VarNames[i]+"="+val)
	}
	return out
}

// Display renders a value the way a debugger shows it, quoting strings so an
// empty one is visible.
func (v Value) Display() string {
	switch v.Type {
	case TypeString:
		return strconv.Quote(v.Str)
	case TypeArray:
		if v.Array == nil {
			return "<array>"
		}
		return "<array " + strconv.Itoa(v.Array.Len()) + ">"
	default:
		return v.String()
	}
}

// truncateText shortens a value for display.
func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Report builds a failure report from an error raised by this frame.
func (f *Frame) Report(err error) *Report {
	r := &Report{Program: f.Prog.Ref, Msg: err.Error()}
	if me, ok := err.(*Error); ok {
		r.Line, r.Inst, r.Msg = me.Line, me.Prim, me.Msg
	}
	if r.Line == 0 && f.PC >= 0 && f.PC < len(f.Prog.Code) {
		r.Line = f.Prog.Code[f.PC].Line
	}
	r.Frames = f.Backtrace()
	return r
}

// Render writes the report the way Fuzzball does.
//
// The shape is fixed by what players and programs have read for decades: a
// header, one line naming the program, instruction and message, then the
// backtrace with the failing source line under each level.
//
// progName resolves a program's name, and source its text; both come from the
// server. sourceLine is one-based.
func (r *Report) Render(owned bool, ownerName string,
	progName func(ref.Ref) string, sourceLine func(ref.Ref, int) (string, bool)) []string {

	var out []string
	if owned {
		out = append(out, "Program Error.  Your program just got the following error.")
	} else {
		out = append(out,
			"Programmer Error.  Please tell "+ownerName+
				" what you typed, and the following message.")
	}

	out = append(out, progName(r.Program)+"("+r.Program.String()+"), line "+
		strconv.Itoa(r.Line)+"; "+r.Inst+": "+r.Msg)

	if len(r.Frames) == 0 {
		return out
	}

	out = append(out, "System stack backtrace:")
	for _, bf := range r.Frames {
		// The opening parenthesis is never closed. That is how upstream
		// prints it, and reproducing it keeps transcripts comparable.
		head := pad3(bf.Level) + ") " + progName(bf.Program) + "(" +
			bf.Program.String() + ") line " + strconv.Itoa(bf.Line) +
			", in " + bf.Func + "(" + strings.Join(bf.Args, ", ") + ":"
		out = append(out, head)

		if line, ok := sourceLine(bf.Program, bf.Line); ok {
			out = append(out, pad3(bf.Line)+": "+line)
		}
	}
	out = append(out, "*done*")
	return out
}

// pad3 right-aligns a number in three columns, as "%3d" does.
func pad3(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 3 {
		s = " " + s
	}
	return s
}
