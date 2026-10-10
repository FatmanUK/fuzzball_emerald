package muf

import (
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// BacktraceFrame is one level of a failing program's call stack.
type BacktraceFrame struct {
	// Level counts outwards from the innermost call, which is
	// zero.
	Level int
	// Program is the object the code belongs to.
	Program ref.Ref
	// Line is the source line being executed at this level.
	Line int
	// Func is the procedure's name, or "???" when the address is
	// not inside one.
	Func string
	// Args are the procedure's arguments. Name and value are kept
	// apart because the backtrace colours them differently: the
	// name and its "=" are bold and the value is not.
	Args []BacktraceArg
}

// BacktraceArg is one argument in a backtrace frame.
type BacktraceArg struct {
	Name  string
	Value string
}

// Report describes a failure fully enough to print what upstream
// prints.
type Report struct {
	Program ref.Ref
	Line    int
	// Inst is the instruction that failed, which for a primitive
	// is its name. Upstream shows it before the message.
	Inst string
	Msg  string

	Frames []BacktraceFrame
}

// maxArgText is how much of an argument's value a backtrace shows,
// matching the limit upstream passes to insttotext.
const maxArgText = 30

// Backtrace renders the call stack, innermost first.
func (f *Frame) Backtrace() []BacktraceFrame {
	var out []BacktraceFrame

	// The innermost level is wherever the program counter is now;
	// the rest come from the return addresses stacked under it.
	pcs := make([]int, 0, len(f.calls)+1)
	pcs = append(pcs, f.PC)
	for i := len(f.calls) - 1; i >= 0; i-- {
		// The stored return address is what upstream prints,
		// not the call it came from. For "foo ;" that is the
		// EXIT after the call, so a caller's line is the line
		// of its own ';'.
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

// procAt finds the procedure an address belongs to, by scanning back
// to the nearest function header.
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
func (f *Frame) argText(level int, proc *Proc) []BacktraceArg {
	if proc.Args == 0 {
		return nil
	}
	// Level zero is the innermost scope, which is the last one
	// opened.
	idx := len(f.scopes) - 1 - level
	if idx < 0 || idx >= len(f.scopes) {
		return nil
	}
	scope := f.scopes[idx]

	out := make([]BacktraceArg, 0, proc.Args)
	for i := 0; i < proc.Args && i < len(proc.VarNames); i++ {
		val := "?"
		if i < len(scope) {
			val = valueTextIn(scope[i],
				f.expandedTrace())
		}
		out = append(out, BacktraceArg{
			Name: proc.VarNames[i], Value: val,
		})
	}
	return out
}

// A backtrace argument and a trace line's stack are rendered by the
// **same** function upstream — `insttotext`, with the same string
// cut at thirty characters and the same `expandarrs` of 1
// (`debugger.c:470` against `interp.c:2963`). Emerald had a second
// renderer here, `Value.Display`, which spelled an array "<array 3>"
// and cut a long string without the trailing marker; it is gone, and
// `valueTextIn` is the one function.

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
// The shape is fixed by what players and programs have read for
// decades: a header, one line naming the program, instruction and
// message, then the backtrace with the failing source line under each
// level.
//
// **Almost every line of it is coloured**, which is invisible to a
// player without COLOR because queue_ansi strips it on the way out
// — and is why Emerald could emit none of it and still match every
// golden transcript. The header is bold red on black (interp.c:1427),
// the program-and-message line is bold (:1436), and the backtrace's
// own three lines are bold yellow on black with the program, line,
// procedure name and each argument name picked out in bold
// (debugger.c:406, :320, :483, :495). The source line under each
// level is the one part that carries none.
//
// progName resolves a program's name, and source its text; both come
// from the server. sourceLine is one-based.
func (r *Report) Render(owned bool, ownerName string,
	progName func(ref.Ref) string, sourceLine func(ref.Ref, int) (string, bool)) []string {

	var out []string
	if owned {
		out = append(out, errRed+
			"Program Error.  Your program just got the "+
			"following error."+ansiOff)
	} else {
		out = append(out, errRed+
			"Programmer Error.  Please tell "+ownerName+
			" what you typed, and the following message."+
			ansiOff)
	}

	// The one line of the report that carries colour: upstream
	// wraps it in bold (interp.c:1435), which a player without
	// COLOR never sees because queue_ansi strips it on the way
	// out. That is why every golden transcript agrees with a
	// server that was not emitting it.
	out = append(out, ansiBold+progName(r.Program)+"("+
		r.Program.String()+"), line "+strconv.Itoa(r.Line)+
		"; "+r.Inst+": "+r.Msg+ansiOff)

	if len(r.Frames) == 0 {
		return out
	}

	out = append(out, errYellow+"System stack backtrace:"+ansiOff)
	for _, bf := range r.Frames {
		// The opening parenthesis is never closed. That is
		// how upstream prints it, and reproducing it keeps
		// transcripts comparable. It is also bold on its own,
		// separately from the procedure name before it.
		var b strings.Builder
		b.WriteString(errYellow + pad3(bf.Level) + ")" +
			ansiOff)
		b.WriteString(" " + ansiBold + progName(bf.Program) +
			"(" + bf.Program.String() + ")" + ansiOff)
		b.WriteString(" line " + ansiBold +
			strconv.Itoa(bf.Line) + ansiOff +
			", in " + ansiBold + bf.Func + ansiOff)
		b.WriteString(ansiBold + "(" + ansiOff)
		for i, a := range bf.Args {
			sep := ""
			if i > 0 {
				sep = ", "
			}
			b.WriteString(ansiBold + sep + a.Name + "=" +
				ansiOff + a.Value)
		}
		b.WriteString(":")
		out = append(out, b.String())

		if line, ok := sourceLine(bf.Program, bf.Line); ok {
			out = append(out, pad3(bf.Line)+": "+line)
		}
	}
	out = append(out, errYellow+"*done*"+ansiOff)
	return out
}

// The sequences the error report is built from. They are written out
// here rather than taken from internal/ansi, because these are the
// *source* of colour and that package only filters it.
const (
	ansiBold  = "\x1b[1m"
	ansiOff   = "\x1b[0m"
	errRed    = "\x1b[1;31;40m"
	errYellow = "\x1b[1;33;40m"
)

// pad3 right-aligns a number in three columns, as "%3d" does.
func pad3(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 3 {
		s = " " + s
	}
	return s
}
