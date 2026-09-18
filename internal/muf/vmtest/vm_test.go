// Package vmtest runs compiled MUF, joining the compiler to the interpreter.
//
// It lives apart from both so neither has to import the other for testing.
package vmtest

import (
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf/compiler"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// fakeHost is a minimal world for programs that reach outside themselves.
type fakeHost struct {
	told  []string
	props map[string]string
	names map[ref.Ref]string
}

func newHost() *fakeHost {
	return &fakeHost{props: map[string]string{}, names: map[ref.Ref]string{}}
}

func (h *fakeHost) Notify(_ ref.Ref, msg string) { h.told = append(h.told, msg) }
func (h *fakeHost) GetPropStr(o ref.Ref, p string) string {
	return h.props[o.String()+"/"+p]
}
func (h *fakeHost) SetPropStr(o ref.Ref, p, v string) { h.props[o.String()+"/"+p] = v }
func (h *fakeHost) Name(o ref.Ref) string             { return h.names[o] }
func (h *fakeHost) Location(ref.Ref) ref.Ref          { return ref.GlobalEnvironment }
func (h *fakeHost) Owner(ref.Ref) ref.Ref             { return ref.God }
func (h *fakeHost) Valid(o ref.Ref) bool              { return o.Ok() }
func (h *fakeHost) ObjType(ref.Ref) ref.ObjType       { return ref.TypeThing }

// run compiles and executes a program, returning the frame and the host.
func run(t *testing.T, src string) (*muf.Frame, *fakeHost) {
	t.Helper()
	p, err := compiler.Compile(src, compiler.Options{})
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
	p, err := compiler.Compile(src, compiler.Options{})
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

// stack returns the final stack as strings, which reads well in failures.
func stack(f *muf.Frame) []string {
	out := make([]string, f.Depth())
	for i := range out {
		v, _ := f.Peek(f.Depth() - 1 - i)
		out[i] = v.String()
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

func TestDivisionByZeroFails(t *testing.T) {
	runFails(t, ": main 1 0 / ;", "division by zero")
	runFails(t, ": main 1 0 % ;", "modulus by zero")
}

func TestStackOperations(t *testing.T) {
	wantStack(t, ": main 1 2 swap ;", "2 1")
	wantStack(t, ": main 1 dup ;", "1 1")
	wantStack(t, ": main 1 2 over ;", "1 2 1")
	wantStack(t, ": main 1 2 pop ;", "1")
	wantStack(t, ": main 1 2 3 rot ;", "2 3 1")
	wantStack(t, ": main 1 2 nip ;", "2")
	wantStack(t, ": main 1 2 tuck ;", "2 1 2")
	wantStack(t, ": main 1 2 3 depth ;", "1 2 3 3")
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
	f, _ := run(t, ": main try 1 0 / catch pop 999 endcatch ;")
	if got := stack(f); len(got) != 1 || got[0] != "999" {
		t.Errorf("stack = %v, want [999]", got)
	}

	// Without a failure the handler is skipped.
	wantStack(t, ": main try 111 catch pop 999 endcatch ;", "111")
}

// TestTryRestoresTheStack checks that catching unwinds what the guarded block
// left behind, rather than handing the handler a half-built stack.
func TestTryRestoresTheStack(t *testing.T) {
	wantStack(t, ": main 42 try 1 2 3 1 0 / catch pop endcatch ;", "42")
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
	runFails(t, ": main pop ;", "stack underflow")
	runFails(t, ": main 1 + ;", "stack underflow")
}

func TestUnimplementedPrimitiveIsReported(t *testing.T) {
	// A primitive the compiler knows but the interpreter does not must say
	// so, rather than silently doing nothing.
	runFails(t, ": main DBTOP ;", "not implemented yet")
}

// TestRunawayProgramIsStopped checks the instruction ceiling.
func TestRunawayProgramIsStopped(t *testing.T) {
	p, err := compiler.Compile(": main begin 1 pop 0 until ;", compiler.Options{})
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
		compiler.Options{})
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
