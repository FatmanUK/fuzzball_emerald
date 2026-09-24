// Package muf implements the MUF language: its instruction set,
// runtime values, and the interpreter that executes them.
//
// MUF is a stack language in the Forth tradition. Fuzzball compiles a
// program to an array of instructions and walks it with a program
// counter; this does the same, so the control flow stays close enough
// to the C to check against it.
package muf

import (
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Type tags an instruction and a runtime value. The numbers follow
// include/inst.h so the two can be read side by side; nothing depends
// on them being stable, because Emerald compiles from source at load
// rather than storing compiled code.
type Type uint8

const (
	TypeCleared   Type = 0
	TypePrimitive Type = 1 // a builtin, numbered by primNames
	TypeInteger   Type = 2
	TypeFloat     Type = 3
	TypeObject    Type = 4 // a dbref
	TypeVar       Type = 5 // global variable slot
	TypeLVar      Type = 6 // program-local variable slot
	TypeSVar      Type = 7 // function-scoped variable slot
	TypeString    Type = 9
	TypeFunction  Type = 10 // a procedure header
	TypeLock      Type = 11
	TypeAddress   Type = 12 // the result of 'funcname
	TypeIf        Type = 13 // pop; jump when false
	TypeExec      Type = 14 // call the address on the stack
	TypeJmp       Type = 15
	TypeArray     Type = 16
	TypeMark      Type = 17 // the marker { pushes
	TypeSVarAt    Type = 18
	TypeSVarBang  Type = 20
	TypeTry       Type = 21
	TypeLVarAt    Type = 22
	TypeLVarBang  Type = 24
)

func (t Type) String() string {
	switch t {
	case TypeInteger:
		return "integer"
	case TypeFloat:
		return "float"
	case TypeObject:
		return "dbref"
	case TypeString:
		return "string"
	case TypeArray:
		return "array"
	case TypeAddress:
		return "address"
	case TypeLock:
		return "lock"
	case TypeMark:
		return "mark"
	case TypeVar, TypeLVar, TypeSVar:
		return "variable"
	default:
		return "type(" + strconv.Itoa(int(t)) + ")"
	}
}

// Inst is one compiled instruction.
type Inst struct {
	Type Type
	Line int

	// Num carries whatever the instruction's type calls for: a
	// primitive number, an integer literal, a variable slot, or a
	// jump target.
	Num   int64
	Float float64
	Str   string
	Ref   ref.Ref

	// Proc is set on TypeFunction and describes the procedure
	// that starts here.
	Proc *Proc
}

// Proc describes a MUF procedure.
type Proc struct {
	Name string
	// Args is how many arguments the procedure declares, for the
	// "name[ a b -- c ]" form. Vars counts its scoped variables,
	// arguments included.
	Args int
	Vars int
	// VarNames names the scoped variables, for the debugger and
	// error messages.
	VarNames []string
}

// Program is a compiled MUF program.
type Program struct {
	// Ref is the program object this was compiled from.
	Ref ref.Ref
	// Code is the instruction array. Execution starts at Start.
	Code  []Inst
	Start int

	// Vars names the program's global variables, of which the
	// first four are always ME, LOC, TRIGGER and COMMAND.
	Vars []string
	// LVars names the program-local variables.
	LVars []string

	// Procs maps a procedure name, folded, to its address in
	// Code.
	Procs map[string]int
	// Publics maps a name, folded, to the address callers reach
	// with CALL.
	Publics map[string]*Public
	// PublicOrder lists the folded public names in declaration
	// order, which is the order the editor's "p" command lists
	// them in.
	PublicOrder []string

	// MLevel is the mucker level the program was compiled at,
	// which bounds what its primitives may do.
	MLevel int
}

// Public is a procedure exposed to other programs.
type Public struct {
	Name   string
	Addr   int
	MLevel int
	Proc   *Proc
}

// Reserved variable slots. Every program has these four, in this
// order.
const (
	VarMe = iota
	VarLoc
	VarTrigger
	VarCommand

	ReservedVars
)

// MaxVars is the ceiling on variables of each kind, from
// include/inst.h.
const MaxVars = 54

// StackSize is the argument stack's ceiling, from include/inst.h.
const StackSize = 1024

// reservedVarNames are the variables every program starts with.
var reservedVarNames = []string{"me", "loc", "trigger", "command"}

// Disassemble renders the program's code, which is what the debugger
// and the compiler's tests read.
func (p *Program) Disassemble() string {
	var b strings.Builder
	for i := range p.Code {
		b.WriteString(strconv.Itoa(i))
		b.WriteByte(':')
		b.WriteByte(' ')
		b.WriteString(p.Code[i].String())
		b.WriteByte('\n')
	}
	return b.String()
}

// String renders one instruction.
func (in Inst) String() string {
	switch in.Type {
	case TypePrimitive:
		return PrimName(int(in.Num))
	case TypeInteger:
		return strconv.FormatInt(in.Num, 10)
	case TypeFloat:
		return strconv.FormatFloat(in.Float, 'g', -1, 64)
	case TypeString:
		return strconv.Quote(in.Str)
	case TypeObject:
		return in.Ref.String()
	case TypeVar:
		return "var " + strconv.FormatInt(in.Num, 10)
	case TypeLVar:
		return "lvar " + strconv.FormatInt(in.Num, 10)
	case TypeSVar:
		return "svar " + strconv.FormatInt(in.Num, 10)
	case TypeSVarAt:
		return "svar@ " + strconv.FormatInt(in.Num, 10)
	case TypeSVarBang:
		return "svar! " + strconv.FormatInt(in.Num, 10)
	case TypeLVarAt:
		return "lvar@ " + strconv.FormatInt(in.Num, 10)
	case TypeLVarBang:
		return "lvar! " + strconv.FormatInt(in.Num, 10)
	case TypeIf:
		return "if-false -> " + strconv.FormatInt(in.Num, 10)
	case TypeJmp:
		return "jmp -> " + strconv.FormatInt(in.Num, 10)
	case TypeTry:
		return "try -> " + strconv.FormatInt(in.Num, 10)
	case TypeExec:
		return "exec"
	case TypeAddress:
		return "'" + strconv.FormatInt(in.Num, 10)
	case TypeFunction:
		if in.Proc != nil {
			return ": " + in.Proc.Name + " (" +
				strconv.Itoa(in.Proc.Vars) + " vars)"
		}
		return ": ?"
	default:
		return in.Type.String()
	}
}
