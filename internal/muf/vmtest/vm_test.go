// Package vmtest runs compiled MUF, joining the compiler to the interpreter.
//
// It lives apart from both so neither has to import the other for testing.
package vmtest

import (
	"strings"
	"testing"

	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/boolexp"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf/compiler"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// fakeHost is a minimal world for programs that reach outside themselves.
type fakeHost struct {
	told  []string
	props map[string]props.Value
	names map[ref.Ref]string
}

func newHost() *fakeHost {
	return &fakeHost{props: map[string]props.Value{}, names: map[ref.Ref]string{}}
}

func (h *fakeHost) Notify(_ ref.Ref, msg string) { h.told = append(h.told, msg) }
func (h *fakeHost) NotifyExcept(_ ref.Ref, _ []ref.Ref, msg string) {
	h.told = append(h.told, msg)
}

func (h *fakeHost) Name(o ref.Ref) string { return h.names[o] }
func (h *fakeHost) SetName(o ref.Ref, n string) error {
	h.names[o] = n
	return nil
}

func (h *fakeHost) Location(ref.Ref) ref.Ref      { return ref.GlobalEnvironment }
func (h *fakeHost) Owner(ref.Ref) ref.Ref         { return ref.God }
func (h *fakeHost) Home(ref.Ref) ref.Ref          { return ref.GlobalEnvironment }
func (h *fakeHost) Links(ref.Ref) []ref.Ref       { return nil }
func (h *fakeHost) Contents(ref.Ref) []ref.Ref    { return nil }
func (h *fakeHost) Exits(ref.Ref) []ref.Ref       { return nil }
func (h *fakeHost) MoveTo(ref.Ref, ref.Ref) error { return nil }

func (h *fakeHost) Valid(o ref.Ref) bool        { return o.Ok() }
func (h *fakeHost) ObjType(ref.Ref) ref.ObjType { return ref.TypeThing }
func (h *fakeHost) Flags(ref.Ref) ref.Flags     { return 0 }
func (h *fakeHost) SetFlags(ref.Ref, ref.Flags) {}
func (h *fakeHost) Top() ref.Ref                { return ref.Ref(100) }

func (h *fakeHost) GetProp(o ref.Ref, p string) (props.Value, bool) {
	v, ok := h.props[o.String()+"/"+p]
	return v, ok
}
func (h *fakeHost) SetProp(o ref.Ref, p string, v props.Value) {
	h.props[o.String()+"/"+p] = v
}
func (h *fakeHost) RemoveProp(o ref.Ref, p string) {
	delete(h.props, o.String()+"/"+p)
}
func (h *fakeHost) PropChildren(ref.Ref, string) []string { return nil }

func (h *fakeHost) Match(ref.Ref, string) ref.Ref    { return ref.Nothing }
func (h *fakeHost) MatchPlayer(string) ref.Ref       { return ref.Nothing }
func (h *fakeHost) MatchPlayerPrefix(string) ref.Ref { return ref.Nothing }

func (h *fakeHost) Create(ref.ObjType, string, ref.Ref, ref.Ref) (ref.Ref, error) {
	return ref.Ref(50), nil
}
func (h *fakeHost) Recycle(ref.Ref) error       { return nil }
func (h *fakeHost) SetOwner(ref.Ref, ref.Ref)   {}
func (h *fakeHost) SetLinks(ref.Ref, []ref.Ref) {}
func (h *fakeHost) Entrances(ref.Ref) []ref.Ref { return nil }

func (h *fakeHost) Timestamps(ref.Ref) (int64, int64, int64, int32) {
	return 1, 2, 3, 4
}

func (h *fakeHost) CheckPassword(ref.Ref, string) bool { return false }
func (h *fakeHost) SetPassword(ref.Ref, string) error  { return nil }

func (h *fakeHost) Connections(ref.Ref) int   { return 1 }
func (h *fakeHost) Descriptors(ref.Ref) []int { return []int{1} }

func (h *fakeHost) Online() []ref.Ref        { return []ref.Ref{ref.God} }
func (h *fakeHost) DescrPlayer(int) ref.Ref  { return ref.God }
func (h *fakeHost) DescrSize(int) (int, int) { return 80, 24 }

func (h *fakeHost) ParseProp(ref.Ref, string, string, bool) (string, error) {
	return "", nil
}
func (h *fakeHost) ParseMPI(ref.Ref, string, string, bool) (string, error) {
	return "", nil
}
func (h *fakeHost) BlessProp(ref.Ref, string, bool)    {}
func (h *fakeHost) IsPropBlessed(ref.Ref, string) bool { return false }
func (h *fakeHost) Controls(ref.Ref, ref.Ref) bool     { return false }
func (h *fakeHost) CompiledSize(ref.Ref) int           { return 0 }
func (h *fakeHost) Compile(ref.Ref) (int, error)       { return 0, nil }
func (h *fakeHost) Uncompile(ref.Ref)                  {}
func (h *fakeHost) ProgramLines(ref.Ref) []string      { return nil }
func (h *fakeHost) SetProgramLines(ref.Ref, []string)  {}
func (h *fakeHost) ToadPlayer(ref.Ref, ref.Ref)        {}
func (h *fakeHost) TuneRefersTo(ref.Ref) bool          { return false }
func (h *fakeHost) DumpNow()                           {}
func (h *fakeHost) TimerCount(int) int                 { return 0 }
func (h *fakeHost) TimerStart(int, string, int64)      {}
func (h *fakeHost) TimerStop(int, string)              {}

func (h *fakeHost) SendEvent(int, string, muf.Value) bool { return false }

func (h *fakeHost) NewPlayer(string, string) (ref.Ref, error) {
	return ref.Nothing, nil
}

func (h *fakeHost) CopyPlayer(ref.Ref, string, string) (ref.Ref, error) {
	return ref.Nothing, nil
}

func (h *fakeHost) CopyObject(ref.Ref, bool) (ref.Ref, error) {
	return ref.Nothing, nil
}

func (h *fakeHost) SMTPConfigured() bool                             { return false }
func (h *fakeHost) SMTPModesValid() (bool, bool)                     { return true, true }
func (h *fakeHost) SendMail(string, string, string, string, ref.Ref) {}

func (h *fakeHost) ParsePropEx(ref.Ref, string, []muf.MPIVar, bool) (string, []muf.MPIVar, error) {
	return "", nil, nil
}

func (h *fakeHost) Interp(int, int, ref.Ref, ref.Ref, string) (muf.Value, bool) {
	return muf.Value{}, false
}

// The MCP methods are stubs: this host has no connections, so a program that
// reaches for one gets the same answer as a player with no MCP-capable client.
func (h *fakeHost) MCPMinLevel() int                   { return 1 }
func (h *fakeHost) MCPSupports(int, string) (int, int) { return 0, 0 }
func (h *fakeHost) MCPSend(int, string, string, []muf.MCPArg) error {
	return nil
}
func (h *fakeHost) MCPBind(ref.Ref, string, string, int) error   { return nil }
func (h *fakeHost) MCPRegister(string, int, int, int, int) error { return nil }

func (h *fakeHost) GUINew(int, *muf.Frame) (string, error) { return "", nil }
func (h *fakeHost) GUIDialog(string) (int, bool)           { return 0, false }
func (h *fakeHost) GUIClose(string) bool                   { return false }
func (h *fakeHost) GUIValue(string, string, int) (string, bool) {
	return "", false
}
func (h *fakeHost) GUIValueLines(string, string) ([]string, bool) { return nil, false }
func (h *fakeHost) GUIValues(string) ([]string, [][]string, bool) {
	return nil, nil, false
}
func (h *fakeHost) GUISetValue(string, string, []string) {}

func (h *fakeHost) Now() time.Time        { return time.Unix(1_700_000_000, 0).UTC() }
func (h *fakeHost) Uptime() time.Duration { return time.Hour }
func (h *fakeHost) Version() string       { return "test" }

func (h *fakeHost) TestLock(int, int, ref.Ref, *boolexp.Expr, ref.Ref, ref.Ref) (bool, error) {
	return false, nil
}
func (h *fakeHost) Locked(int, int, ref.Ref, ref.Ref) (bool, error) { return false, nil }
func (h *fakeHost) MaxInterpRecursion() int                         { return 8 }

func (h *fakeHost) LockString(ref.Ref) string                        { return "*UNLOCKED*" }
func (h *fakeHost) SetLockString(int, ref.Ref, ref.Ref, string) bool { return true }
func (h *fakeHost) ParseLock(int, ref.Ref, string) *boolexp.Expr     { return nil }
func (h *fakeHost) UnparseLock(ref.Ref, *boolexp.Expr) string        { return "" }
func (h *fakeHost) PrettyLock(ref.Ref, *boolexp.Expr) string         { return "*UNLOCKED*" }

func (h *fakeHost) ForceLevel() int                              { return 0 }
func (h *fakeHost) IsPID(int) bool                               { return false }
func (h *fakeHost) Instances(ref.Ref) int                        { return 0 }
func (h *fakeHost) CanCall(int, ref.Ref, ref.Ref, string) bool   { return false }
func (h *fakeHost) ControlsProcess(ref.Ref, int) bool            { return false }
func (h *fakeHost) KillPID(int) bool                             { return false }
func (h *fakeHost) Fork(*muf.Frame) int                          { return 0 }
func (h *fakeHost) Queue(int, ref.Ref, int64, string) int        { return 0 }
func (h *fakeHost) Force(int, ref.Ref, ref.Ref, ref.Ref, string) {}
func (h *fakeHost) ForcedBy() ref.Ref                            { return ref.Nothing }
func (h *fakeHost) ForcedByArray() []ref.Ref                     { return nil }
func (h *fakeHost) GetPIDs(ref.Ref, int) []int                   { return nil }
func (h *fakeHost) PIDInfo(int) (muf.PIDInfo, bool)              { return muf.PIDInfo{}, false }
func (h *fakeHost) WatchPID(int, int) bool                       { return false }
func (h *fakeHost) SetDescrSize(int, int, int) bool              { return false }
func (h *fakeHost) DescrIdle(int) int                            { return -1 }
func (h *fakeHost) DescrOnTime(int) int                          { return -1 }
func (h *fakeHost) DescrHost(int) (string, bool)                 { return "", false }
func (h *fakeHost) DescrUser(int) (string, bool)                 { return "", false }
func (h *fakeHost) DescrBoot(int) bool                           { return false }
func (h *fakeHost) DescrNotify(int, string) bool                 { return false }
func (h *fakeHost) DescrFlush(int) int                           { return 0 }
func (h *fakeHost) DescrBufSize(int) int                         { return -1 }
func (h *fakeHost) DescrLeastIdle(ref.Ref) int                   { return -1 }
func (h *fakeHost) DescrMostIdle(ref.Ref) int                    { return -1 }
func (h *fakeHost) NextDescr(int) int                            { return 0 }
func (h *fakeHost) FirstDescr(ref.Ref) int                       { return 0 }
func (h *fakeHost) LastDescr(ref.Ref) int                        { return 0 }
func (h *fakeHost) SetUser(int, ref.Ref) bool                    { return false }
func (h *fakeHost) TuneGet(string) (string, bool)                { return "", false }
func (h *fakeHost) TuneReadMLevel(string) (int, bool)            { return 0, false }
func (h *fakeHost) TuneWriteMLevel(string) (int, bool)           { return 0, false }
func (h *fakeHost) TuneSet(string, string) (bool, error)         { return false, nil }
func (h *fakeHost) TuneList(string, int) []muf.TuneEntry         { return nil }
func (h *fakeHost) TuneBool(string) bool                         { return false }
func (h *fakeHost) TuneInt(string) int64                         { return 0 }
func (h *fakeHost) TuneSpan(string) time.Duration                { return 0 }
func (h *fakeHost) NameOK(string, ref.ObjType) bool              { return true }
func (h *fakeHost) UserLog(ref.Ref, ref.Ref, string)             {}
func (h *fakeHost) IsIgnoring(ref.Ref, ref.Ref) bool             { return false }
func (h *fakeHost) IgnoreAdd(ref.Ref, ref.Ref)                   {}
func (h *fakeHost) IgnoreDel(ref.Ref, ref.Ref)                   {}
func (h *fakeHost) Stats(ref.Ref) [7]int                         { return [7]int{} }

// run compiles and executes a program, returning the frame and the host.
func run(t *testing.T, src string) (*muf.Frame, *fakeHost) {
	t.Helper()
	p, err := compiler.Compile(src, compiler.Options{MLevel: 3})
	if err != nil {
		t.Fatalf("compiling %q: %v", src, err)
	}
	h := newHost()
	f := muf.NewFrame(p, h)
	f.SetReserved(ref.God, ref.GlobalEnvironment, ref.Nothing, "")

	res, err := f.Run(muf.Limits{})
	if err != nil {
		t.Fatalf("running %q: %v", src, err)
	}
	if res != muf.Done {
		t.Fatalf("running %q stopped with %v, want Done", src, res)
	}
	return f, h
}

// runFails requires the program to fail with a message containing want.
func runFails(t *testing.T, src, want string) {
	t.Helper()
	p, err := compiler.Compile(src, compiler.Options{MLevel: 3})
	if err != nil {
		t.Fatalf("compiling %q: %v", src, err)
	}
	f := muf.NewFrame(p, newHost())
	f.SetReserved(ref.God, ref.GlobalEnvironment, ref.Nothing, "")
	if _, err := f.Run(muf.Limits{}); err == nil {
		t.Fatalf("running %q should have failed", src)
	} else if !strings.Contains(err.Error(), want) {
		t.Errorf("error for %q = %q, want it to mention %q", src, err, want)
	}
}

// stack returns the final stack as strings, minus the argument the
// interpreter puts there before a program starts.
//
// A MUF program is handed its command's argument on the stack, so an empty
// argument still leaves one value below whatever the program produced.
// TestProgramStartsWithItsArgument covers that directly; every other test
// looks past it.
func stack(f *muf.Frame) []string {
	depth := f.Depth()
	out := make([]string, 0, depth)
	for i := depth - 1; i >= 0; i-- {
		v, _ := f.Peek(i)
		out = append(out, v.String())
	}
	if len(out) > 0 {
		return out[1:]
	}
	return out
}

// wantStack requires the final stack to match.
func wantStack(t *testing.T, src string, want ...string) {
	t.Helper()
	f, _ := run(t, src)
	got := stack(f)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("%q left %v, want %v", src, got, want)
	}
}

func TestArithmetic(t *testing.T) {
	wantStack(t, ": main 2 3 + ;", "5")
	wantStack(t, ": main 10 3 - ;", "7")
	wantStack(t, ": main 6 7 * ;", "42")
	wantStack(t, ": main 20 4 / ;", "5")
	wantStack(t, ": main 17 5 % ;", "2")
	wantStack(t, ": main 2 3 + 4 * ;", "20")

	// A float on either side promotes the whole operation.
	wantStack(t, ": main 1 2.5 + ;", "3.5")
	wantStack(t, ": main 7.5 2.5 / ;", "3")
}

func TestDivisionByBadTypeFails(t *testing.T) {
	// Integer division by zero is not a failure; see
	// TestDivisionByZeroYieldsZeroAndAFlag. Dividing by a non-number is.
	// Upstream's wording, which programs and players read.
	runFails(t, `: main 1 "x" / ;`, "Invalid argument type.")
}

func TestStackOperations(t *testing.T) {
	wantStack(t, ": main 1 2 swap ;", "2 1")
	wantStack(t, ": main 1 dup ;", "1 1")
	wantStack(t, ": main 1 2 over ;", "1 2 1")
	wantStack(t, ": main 1 2 pop ;", "1")
	wantStack(t, ": main 1 2 3 rot ;", "2 3 1")
	wantStack(t, ": main 1 2 nip ;", "2")
	wantStack(t, ": main 1 2 tuck ;", "2 1 2")
	// depth counts the command argument the interpreter pushed, so three
	// values pushed here read as four.
	wantStack(t, ": main 1 2 3 depth ;", "1 2 3 4")
	wantStack(t, ": main 1 2 3 2 pick ;", "1 2 3 2")
}

func TestComparisons(t *testing.T) {
	wantStack(t, ": main 1 2 < ;", "1")
	wantStack(t, ": main 2 1 < ;", "0")
	wantStack(t, ": main 2 2 = ;", "1")
	wantStack(t, ": main \"a\" \"b\" < ;", "1")
	wantStack(t, ": main 1 not ;", "0")
	wantStack(t, ": main 0 not ;", "1")
	wantStack(t, ": main 1 0 and ;", "0")
	wantStack(t, ": main 1 0 or ;", "1")
}

func TestStrings(t *testing.T) {
	wantStack(t, `: main "foo" "bar" strcat ;`, "foobar")
	wantStack(t, `: main "hello" strlen ;`, "5")
	wantStack(t, `: main "hello" toupper ;`, "HELLO")
	wantStack(t, `: main "  x  " strip ;`, "x")
	// MUF indexes strings from one.
	wantStack(t, `: main "hello" 2 3 midstr ;`, "ell")
	wantStack(t, `: main "hello" "ll" instr ;`, "3")
	wantStack(t, `: main "hello" "zz" instr ;`, "0")
}

func TestConditionals(t *testing.T) {
	wantStack(t, ": main 1 if 111 else 222 then ;", "111")
	wantStack(t, ": main 0 if 111 else 222 then ;", "222")
	wantStack(t, ": main 1 if 111 then ;", "111")
	wantStack(t, ": main 0 if 111 then 999 ;", "999")
}

func TestVariables(t *testing.T) {
	wantStack(t, ": main var x 5 x ! x @ ;", "5")
	wantStack(t, ": main 7 var! n n @ ;", "7")
	// A scoped variable is private to its procedure, so both may use "n".
	wantStack(t, ": a 1 var! n n @ ; : main a 2 var! n n @ + ;", "3")
}

func TestProcedureCallsAndArguments(t *testing.T) {
	wantStack(t, ": double[ int:n -- int:r ] n @ 2 * ; : main 21 double ;", "42")
	wantStack(t, ": add[ int:a int:b -- int:r ] a @ b @ + ; : main 2 3 add ;", "5")
	// Nested calls return correctly.
	wantStack(t, ": inc[ int:n -- int:r ] n @ 1 + ; : main 1 inc inc inc ;", "4")
}

func TestCountingLoop(t *testing.T) {
	// 1 to 5 inclusive, summed.
	wantStack(t, ": main var sum 0 sum ! 1 5 1 for sum @ + sum ! repeat sum @ ;", "15")
	// A descending loop.
	wantStack(t, ": main var n 0 n ! 5 1 -1 for pop 1 n @ + n ! repeat n @ ;", "5")
}

func TestBeginUntil(t *testing.T) {
	wantStack(t, ": main var i 0 i ! begin i @ 1 + i ! i @ 3 >= until i @ ;", "3")
}

func TestBreakAndContinue(t *testing.T) {
	// Break leaves the loop early.
	wantStack(t, ": main var i 0 i ! begin i @ 1 + i ! i @ 3 = if break then 0 until i @ ;", "3")
}

func TestForeach(t *testing.T) {
	// FOREACH hands the body a key and a value.
	wantStack(t, ": main var sum 0 sum ! { 10 20 30 }list foreach swap pop sum @ + sum ! repeat sum @ ;", "60")
}

func TestArrays(t *testing.T) {
	wantStack(t, ": main { 1 2 3 }list array_count ;", "3")
	wantStack(t, ": main { 10 20 30 }list 1 array_getitem ;", "20")
	// A missing key reads as #-1 rather than failing.
	wantStack(t, ": main { 1 2 }list 99 array_getitem ;", "#-1")
	wantStack(t, `: main { "a" "b" }list "," array_join ;`, "a,b")
}

func TestTryCatch(t *testing.T) {
	// A failure inside TRY lands in the handler with the message.
	f, _ := run(t, `: main 0 try "x" 0 / catch pop 999 endcatch ;`)
	if got := stack(f); len(got) != 1 || got[0] != "999" {
		t.Errorf("stack = %v, want [999]", got)
	}

	// Without a failure the handler is skipped.
	wantStack(t, ": main 0 try 111 catch pop 999 endcatch ;", "111")
}

// TestTryRestoresTheStack checks that catching unwinds what the guarded block
// left behind, rather than handing the handler a half-built stack.
func TestTryRestoresTheStack(t *testing.T) {
	wantStack(t, `: main 42 0 try 1 2 3 "x" 0 / catch pop endcatch ;`, "42")
}

// TestKillOwnPIDEndsProgramSilentlyAndUncatchably checks upstream's
// ERROR_DIE_NOW special case end to end: a program that KILLs its own pid
// stops immediately — reaching neither the notify after it nor an open TRY's
// catch block — and Run reports it as a plain, unreported Done rather than
// an error.
func TestKillOwnPIDEndsProgramSilentlyAndUncatchably(t *testing.T) {
	f, h := run(t, `: main
  0 try
    pid kill
    me @ "unreached" notify
  catch
    me @ "caught" notify
  endcatch
;`)
	if len(h.told) != 0 {
		t.Errorf("told = %v, want nothing — self-kill should reach neither branch", h.told)
	}
	if got := stack(f); len(got) != 0 {
		t.Errorf("stack = %v, want empty", got)
	}
}

func TestNotify(t *testing.T) {
	_, h := run(t, `: main me @ "hello world" notify ;`)
	if len(h.told) != 1 || h.told[0] != "hello world" {
		t.Errorf("told = %v, want [hello world]", h.told)
	}
}

// TestNotifySplitsOnCarriageReturns covers MUF's in-string line separator.
func TestNotifySplitsOnCarriageReturns(t *testing.T) {
	_, h := run(t, `: main me @ "one\rtwo" notify ;`)
	if len(h.told) != 2 || h.told[0] != "one" || h.told[1] != "two" {
		t.Errorf("told = %v, want [one two]", h.told)
	}
}

func TestProperties(t *testing.T) {
	_, h := run(t, `: main me @ "test/prop" "value" setprop me @ "test/prop" getpropstr me @ swap notify ;`)
	if len(h.told) != 1 || h.told[0] != "value" {
		t.Errorf("told = %v, want [value]", h.told)
	}
}

func TestReservedVariables(t *testing.T) {
	wantStack(t, ": main me @ ;", ref.God.String())
	wantStack(t, ": main loc @ ;", ref.GlobalEnvironment.String())
}

func TestStackUnderflowIsReported(t *testing.T) {
	// Two pops, because one value is already there: the argument.
	runFails(t, ": main pop pop ;", "stack underflow")
	runFails(t, ": main pop 1 + ;", "stack underflow")
}

// TestProgramStartsWithItsArgument pins a detail that is easy to miss and that
// programs depend on: interp() pushes the command's argument before the
// program runs, so "depth" is one higher than what the program itself pushed.
func TestProgramStartsWithItsArgument(t *testing.T) {
	p, err := compiler.Compile(": main depth ;", compiler.Options{MLevel: 3})
	if err != nil {
		t.Fatal(err)
	}
	f := muf.NewFrame(p, newHost())
	f.SetReserved(ref.God, ref.GlobalEnvironment, ref.Nothing, "some args")
	if _, err := f.Run(muf.Limits{}); err != nil {
		t.Fatal(err)
	}
	// The argument, then the depth that counted it.
	depth, _ := f.Peek(0)
	if depth.String() != "1" {
		t.Errorf("depth = %s, want 1: the argument should already be on the stack", depth)
	}
	arg, _ := f.Peek(1)
	if arg.String() != "some args" {
		t.Errorf("the value below is %q, want the command argument", arg.String())
	}
}

// TestDivisionByZeroYieldsZeroAndAFlag covers a difference the golden harness
// found: MUF does not abort on integer division by zero. The result is zero
// and a flag the program can read with is_set?.
func TestDivisionByZeroYieldsZeroAndAFlag(t *testing.T) {
	wantStack(t, ": main 1 0 / ;", "0")
	wantStack(t, ": main 1 0 % ;", "0")
	wantStack(t, ": main 1 0 / pop 0 is_set? ;", "1")
	// Nothing is flagged when the division is fine.
	wantStack(t, ": main 4 2 / pop 0 is_set? ;", "0")
}

func TestUnimplementedPrimitiveIsReported(t *testing.T) {
	// A primitive the compiler knows but the interpreter does not must say
	// so, rather than silently doing nothing.
	runFails(t, ": main CHECKARGS ;", "not implemented yet")
}

// TestRunawayProgramIsStopped checks the instruction ceiling.
func TestRunawayProgramIsStopped(t *testing.T) {
	p, err := compiler.Compile(": main begin 1 pop 0 until ;", compiler.Options{MLevel: 3})
	if err != nil {
		t.Fatal(err)
	}
	f := muf.NewFrame(p, newHost())
	f.SetReserved(ref.God, ref.GlobalEnvironment, ref.Nothing, "")
	if _, err := f.Run(muf.Limits{Slice: 1 << 30, Total: 5000}); err == nil {
		t.Error("an endless loop should hit the instruction limit")
	}
}

// TestSlicingYields checks that a long-running program hands control back
// rather than monopolising the goroutine it runs on.
func TestSlicingYields(t *testing.T) {
	p, err := compiler.Compile(": main var i 0 i ! begin i @ 1 + i ! i @ 10000 >= until i @ ;",
		compiler.Options{MLevel: 3})
	if err != nil {
		t.Fatal(err)
	}
	f := muf.NewFrame(p, newHost())
	f.SetReserved(ref.God, ref.GlobalEnvironment, ref.Nothing, "")

	res, err := f.Run(muf.Limits{Slice: 100})
	if err != nil {
		t.Fatal(err)
	}
	if res != muf.Yielded {
		t.Fatalf("result = %v, want Yielded", res)
	}

	// Resuming finishes the work.
	for i := 0; i < 10_000; i++ {
		res, err = f.Run(muf.Limits{Slice: 100})
		if err != nil {
			t.Fatal(err)
		}
		if res == muf.Done {
			break
		}
	}
	if res != muf.Done {
		t.Fatal("the program never finished")
	}
	if got := stack(f); len(got) != 1 || got[0] != "10000" {
		t.Errorf("stack = %v, want [10000]", got)
	}
}

func TestRecursionIsBounded(t *testing.T) {
	runFails(t, ": rec rec ; : main rec ;", "call depth exceeded")
}
