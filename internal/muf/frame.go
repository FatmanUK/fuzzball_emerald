package muf

import (
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/boolexp"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Host is what a running program can reach outside itself.
//
// The interpreter takes it as an interface so this package needs no world:
// primitives that only move values around never touch it, and the ones that
// do are explicit about it.
type Host interface {
	// Notify sends a line to an object's connections.
	Notify(who ref.Ref, msg string)
	// NotifyExcept sends a line to everything in a room, skipping some.
	NotifyExcept(room ref.Ref, except []ref.Ref, msg string)

	// Name returns an object's name, and SetName changes it.
	Name(obj ref.Ref) string
	SetName(obj ref.Ref, name string) error

	// Location, Owner and Home answer the obvious questions.
	Location(obj ref.Ref) ref.Ref
	Owner(obj ref.Ref) ref.Ref
	Home(obj ref.Ref) ref.Ref
	// Links returns what an object points at: an exit's destinations, a
	// room's drop-to, or a thing's or player's home.
	Links(obj ref.Ref) []ref.Ref

	// Contents and Exits walk the containment chains.
	Contents(obj ref.Ref) []ref.Ref
	Exits(obj ref.Ref) []ref.Ref
	// MoveTo relocates an object.
	MoveTo(what, dest ref.Ref) error

	// Valid reports whether a ref names a live object, ObjType gives its
	// type, and Flags its flag word.
	Valid(obj ref.Ref) bool
	ObjType(obj ref.Ref) ref.ObjType
	Flags(obj ref.Ref) ref.Flags
	SetFlags(obj ref.Ref, f ref.Flags)
	// Top is one past the highest ref in use.
	Top() ref.Ref

	// Properties. GetProp returns the stored value; the string, integer and
	// dbref forms of the primitives convert from it.
	GetProp(obj ref.Ref, path string) (props.Value, bool)
	SetProp(obj ref.Ref, path string, v props.Value)
	RemoveProp(obj ref.Ref, path string)
	// PropChildren lists the names directly under a property path.
	PropChildren(obj ref.Ref, path string) []string

	// Match resolves a name the way a player's command would, MatchPlayer
	// looks only at player names, and MatchPlayerPrefix accepts a partial
	// one.
	Match(who ref.Ref, name string) ref.Ref
	MatchPlayer(name string) ref.Ref
	MatchPlayerPrefix(name string) ref.Ref

	// Create makes an object and returns its ref.
	Create(t ref.ObjType, name string, parent, owner ref.Ref) (ref.Ref, error)
	// Recycle destroys one.
	Recycle(obj ref.Ref) error
	// SetOwner and SetLinks change what an object belongs to and points at.
	SetOwner(obj, owner ref.Ref)
	SetLinks(obj ref.Ref, dests []ref.Ref)
	// Timestamps returns when an object was created, modified and last
	// used, and how often.
	Timestamps(obj ref.Ref) (created, modified, used int64, count int32)
	// Entrances lists the exits that lead to an object.
	Entrances(target ref.Ref) []ref.Ref

	// CheckPassword and SetPassword handle a player's credential.
	CheckPassword(player ref.Ref, pass string) bool
	SetPassword(player ref.Ref, pass string) error

	// Connections returns how many times a player is connected, and
	// Descriptors the descriptor numbers.
	Connections(player ref.Ref) int
	Descriptors(player ref.Ref) []int
	// Online lists the players with a live connection.
	Online() []ref.Ref
	// DescrPlayer resolves a descriptor number to whoever is on it.
	DescrPlayer(descr int) ref.Ref
	// DescrSize is a connection's reported terminal width and height.
	DescrSize(descr int) (width, height int)

	// ParseProp evaluates the MPI in a property and returns the result. It
	// is a host method because MUF and MPI are separate languages that the
	// server joins, not layers of one another.
	ParseProp(obj ref.Ref, path, arg string, private bool) (string, error)

	// MCPMinLevel is the mucker level the mcp_muf_mlev parameter names.
	MCPMinLevel() int

	// MCPSupports reports the version of an MCP package a connection
	// agreed to, as major and minor; both zero means it did not.
	MCPSupports(descr int, pkg string) (int, int)
	// MCPSend sends an out-of-band message. Args pairs a name with its
	// lines, in the order given.
	MCPSend(descr int, pkg, name string, args []MCPArg) error
	// MCPBind registers a program's procedure as the handler for one
	// message, which is how a MUF program serves its own package.
	MCPBind(prog ref.Ref, pkg, name string, addr int) error
	// MCPRegister offers a package on a connection's behalf.
	MCPRegister(pkg string, minMajor, minMinor, maxMajor, maxMinor int) error

	// GUINew opens a dialog on a connection and returns its id.
	GUINew(descr int, frame *Frame) (string, error)
	// GUIDialog reports which connection a dialog is on.
	GUIDialog(id string) (descr int, ok bool)
	// GUIClose forgets a dialog.
	GUIClose(id string) bool
	// GUIValue reads one line of a control's value, and GUIValues every
	// control's first line.
	GUIValue(id, ctrl string, line int) (string, bool)
	GUIValueLines(id, ctrl string) ([]string, bool)
	GUIValues(id string) ([]string, [][]string, bool)
	// GUISetValue records a value locally as well as sending it, so a
	// program reads back what it just set.
	GUISetValue(id, ctrl string, lines []string)

	// Now is the server's clock, which tests replace.
	Now() time.Time
	// Uptime is how long the server has been running.
	Uptime() time.Duration
	// Version identifies the server.
	Version() string

	// TestLock evaluates an already-parsed lock (from PARSELOCK) against
	// testPlayer, the way TESTLOCK does. thingSource is what the lock is
	// evaluated as being "on" — upstream picks trig or caller depending on
	// the consistent_lock_source @tune parameter, which only the host can
	// read. level is the calling frame's own Level; ok is false, with a
	// non-nil error, once it exceeds TESTLOCK's hardcoded recursion limit.
	TestLock(descr, level int, testPlayer ref.Ref, lock *boolexp.Expr, trig, caller ref.Ref) (bool, error)
	// Locked reports whether player is locked out of thing, the way LOCKED?
	// does: could_doit's exit-destination rules, then thing's own lock.
	Locked(descr, level int, player, thing ref.Ref) (bool, error)
	// MaxInterpRecursion is the max_interp_recursion @tune parameter, which
	// bounds LOCKED?'s own recursion guard (TESTLOCK's is hardcoded).
	MaxInterpRecursion() int

	// LockString reads an object's @lock property in its stored, unparsed
	// form, or "*UNLOCKED*" if it has none — GETLOCKSTR.
	LockString(obj ref.Ref) string
	// SetLockString parses raw with matchPlayer's own matching context and
	// stores it as obj's @lock, or clears the lock when raw is empty —
	// SETLOCKSTR (which is _set_lock with silent true). It reports whether
	// the lock was set, which is false only when raw failed to parse.
	SetLockString(descr int, matchPlayer, obj ref.Ref, raw string) bool
	// ParseLock parses a lock expression with matchPlayer's own matching
	// context, upstream's PARSELOCK. A raw that fails to parse notifies
	// matchPlayer directly, the way parse_boolexp's own match failures do,
	// and returns nil (TRUE_BOOLEXP) rather than an error.
	ParseLock(descr int, matchPlayer ref.Ref, raw string) *boolexp.Expr
	// UnparseLock renders a parsed lock back to its stored form, upstream's
	// UNPARSELOCK. A nil lock (TRUE_BOOLEXP) renders as "", not
	// "*UNLOCKED*" — that is UNPARSELOCK's own special case, not shared with
	// LockString/GETLOCKSTR. matchPlayer is upstream's ProgUID, unused unless
	// a dbref inside the lock ever needs rendering with a viewer's
	// permissions, which UNPARSELOCK's own fullname-false form never does.
	UnparseLock(matchPlayer ref.Ref, lock *boolexp.Expr) string
	// PrettyLock renders a parsed lock as a human-readable string, upstream's
	// PRETTYLOCK: unparse_boolexp with fullname true, so a CONST dbref shows
	// the way matchPlayer (ProgUID) would see it rather than as a bare
	// "#123".
	PrettyLock(matchPlayer ref.Ref, lock *boolexp.Expr) string

	// ForceLevel is upstream's global force_level: how deeply @force, the
	// FORCE primitive and the {force} MPI function are currently nested,
	// shared across all three call paths — FORCE_LEVEL.
	ForceLevel() int
	// IsPID reports whether pid names a live process other than the caller's
	// own frame, which ISPID? checks in addition to the caller's own PID.
	IsPID(pid int) bool
	// Instances counts the processes currently running prog — INSTANCES.
	Instances(prog ref.Ref) int
	// CanCall reports whether a caller at callerLevel, acting as callerUID
	// (progUID), may CALL prog's public function name — CANCALL?. prog is
	// compiled on demand if it has not been already; a program that fails to
	// compile, or declares no public by that name, reports false rather than
	// an error, matching upstream's own silent failure there.
	CanCall(callerLevel int, callerUID ref.Ref, prog ref.Ref, name string) bool
}

// MCPArg is one argument of an outgoing MCP message: a name and its lines.
type MCPArg struct {
	Name  string
	Lines []string
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

	// Mode is the multitasking mode, which decides how readily the program
	// yields.
	Mode int

	// Block says why the program stopped, when Run returned Blocked. The
	// scheduler reads it to decide what the program is waiting for.
	Block BlockReason

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
	// Descr is the connection the program was started from.
	Descr int

	// Level is upstream's fr->level: how deeply nested this frame's own
	// interp_loop call is. A program started from a command runs at level 1;
	// TESTLOCK and LOCKED? propagate level+1 to a frame they start to
	// evaluate a program-type lock constant, so a chain of locks that each
	// trigger another lock check cannot recurse forever.
	Level int

	// PID is this frame's process id, upstream's fr->pid, for the PID and
	// ISPID? primitives. It is assigned by whoever registers the frame as a
	// process (internal/game's procQueue) and stays zero for a frame that
	// never becomes one, such as a lock-triggered program RunLock evaluates
	// synchronously.
	PID int

	// Supplicant is upstream's fr->supplicant: the object being tested
	// against a lock, for a frame a program-type lock constant is running.
	// It is ref.Nothing outside that context.
	Supplicant ref.Ref

	host Host
}

// NewFrame prepares a program to run.
func NewFrame(p *Program, host Host) *Frame {
	f := &Frame{
		Prog:       p,
		PC:         p.Start,
		Level:      1,
		Vars:       make([]Value, len(p.Vars)),
		LVars:      make([]Value, len(p.LVars)),
		Supplicant: ref.Nothing,
		host:       host,
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

// BlockReason says what a suspended program is waiting for.
type BlockReason struct {
	Kind BlockKind
	// Seconds is how long a SLEEP asked for.
	Seconds int64
	// Events lists what an EVENT_WAITFOR is waiting on.
	Events []string
}

// BlockKind enumerates the ways a program can suspend.
type BlockKind int

const (
	BlockNone BlockKind = iota
	// BlockRead waits for a line of input from the player.
	BlockRead
	// BlockSleep waits for a time to pass.
	BlockSleep
	// BlockEvent waits for a named event.
	BlockEvent
)

// Multitasking modes, from the MODE and SETMODE primitives.
const (
	ModePreempt    = 0
	ModeForeground = 1
	ModeBackground = 2
)

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

// MLevel is the mucker level the program runs at, which bounds what its
// primitives may do.
func (f *Frame) MLevel() int { return f.Prog.MLevel }

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
