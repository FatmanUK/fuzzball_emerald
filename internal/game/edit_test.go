package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestProgramCreatesAndEdits walks the whole cycle: make a program, type it in,
// save it, and run it.
func TestProgramCreatesAndEdits(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.send("@program greeter")
	// The listing on entry says nothing is available: upstream's current
	// line starts at zero and its walk runs off the end of the buffer, so
	// this is what @program and @edit both print before anything is typed.
	if got := h.out(); !strings.Contains(got, "Program greeter(#") ||
		!strings.Contains(got, "Entering editor for greeter(#") ||
		!strings.Contains(got, "Line not available for display.") {
		t.Fatalf("@program said:\n%s", got)
	}

	h.send("i")
	if got := h.out(); !strings.Contains(got, "Entering insert mode.") {
		t.Fatalf("i said %q", got)
	}
	h.send(": main")
	h.send(`  me @ "Hi there." notify`)
	h.send(";")
	h.send(".")
	if got := h.out(); !strings.Contains(got, "Exiting insert mode.") {
		t.Fatalf("'.' said %q", got)
	}

	h.send("c")
	if got := h.out(); !strings.Contains(got, "Program compiled successfully.") ||
		!strings.Contains(got, "Compiler done.") {
		t.Fatalf("compiling said:\n%s", got)
	}

	h.send("q")
	if got := h.out(); !strings.Contains(got, "Editor exited.") {
		t.Fatalf("q said %q", got)
	}

	// The source is stored, and the object is no longer locked for editing.
	var src string
	var locked bool
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		r, _ := w.PlayerNamed("Wizard")
		for _, c := range w.Contents(r) {
			if o := w.Get(c); o != nil && o.Type() == ref.TypeProgram {
				src, _ = w.Source(c)
				locked = o.Flags&ref.Internal != 0
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if want := ": main\n  me @ \"Hi there.\" notify\n;\n"; src != want {
		t.Errorf("stored source is %q, want %q", src, want)
	}
	if locked {
		t.Error("the program is still marked as being edited")
	}
}

// TestEditorInsertPositions pins where typed lines land, which is the part of
// the editor most easily got subtly wrong.
func TestEditorInsertPositions(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("@program lines")
	h.out()

	// Insert three lines from empty.
	h.send("i")
	h.send("one")
	h.send("two")
	h.send("three")
	h.send(".")
	h.out()

	// "2 i" inserts before line 2.
	h.send("2 i")
	h.send("one-and-a-half")
	h.send(".")
	h.out()

	// Arguments come before the command letter, always: "1 n" is "n with
	// argument 1", and "n 1" would be read as the command "1".
	h.send("1 n")
	h.out()
	h.send("1 99 l")
	got := h.out()
	want := []string{
		"  1: one",
		"  2: one-and-a-half",
		"  3: two",
		"  4: three",
	}
	for _, line := range want {
		if !strings.Contains(got, line) {
			t.Errorf("listing missing %q; got:\n%s", line, got)
		}
	}
}

// TestEditorDeletesLines covers the delete forms and their reporting.
func TestEditorDeletesLines(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("@program cut")
	h.out()
	h.send("i")
	for _, l := range []string{"a", "b", "c", "d"} {
		h.send(l)
	}
	h.send(".")
	h.out()

	h.send("2 3 d")
	if got := h.out(); !strings.Contains(got, "2 lines deleted") {
		t.Errorf("range delete said %q", got)
	}
	h.send("1 99 l")
	got := h.out()
	if strings.Contains(got, "b") || strings.Contains(got, "c") {
		t.Errorf("deleted lines are still there:\n%s", got)
	}

	h.send("9 d")
	if got := h.out(); !strings.Contains(got, "No line to delete!") {
		t.Errorf("deleting past the end said %q", got)
	}
	h.send("3 1 d")
	if got := h.out(); !strings.Contains(got, "Nonsensical arguments.") {
		t.Errorf("a reversed range said %q", got)
	}
}

// TestEditorCancelDiscards checks that "x" leaves the stored source alone.
func TestEditorCancelDiscards(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("@program keep")
	h.out()
	h.send("i")
	h.send(": main 1 pop ;")
	h.send(".")
	h.send("q")
	h.out()

	h.send("@edit keep")
	h.out()
	h.send("i")
	h.send("( a comment nobody asked for )")
	h.send(".")
	h.send("x")
	if got := h.out(); !strings.Contains(got, "Changes cancelled.") {
		t.Fatalf("x said %q", got)
	}

	src := h.sourceOfProgram(t)
	if strings.Contains(src, "nobody asked for") {
		t.Errorf("cancelling kept the change:\n%s", src)
	}
}

// TestEditorReportsCompileErrors checks the error carries upstream's wording
// and the line it happened on, which is what a programmer reads.
func TestEditorReportsCompileErrors(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("@program broken")
	h.out()
	h.send("i")
	h.send(": main")
	h.send("  nosuchprimitive")
	h.send(";")
	h.send(".")
	h.out()

	h.send("c")
	got := h.out()
	if !strings.Contains(got, "Error in line 2:") {
		t.Errorf("the error does not name the line:\n%s", got)
	}
	if !strings.Contains(got, "Compiler done.") {
		t.Errorf("the editor did not finish the compile:\n%s", got)
	}
}

// TestEditorRefusesASecondEditor checks the INTERNAL lock.
func TestEditorRefusesASecondEditor(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("@program shared")
	h.out()
	h.send("q")
	h.out()

	h.send("@edit shared")
	h.out()
	// A second @edit arrives through the editor, not the parser, so it is
	// read as editor commands. Leave first, then prove the lock by setting
	// it by hand.
	h.send("q")
	h.out()

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		for _, c := range w.Contents(h.wizRef()) {
			if o := w.Get(c); o != nil && o.Type() == ref.TypeProgram {
				o.Flags |= ref.Internal
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.send("@edit shared")
	if got := h.out(); !strings.Contains(got, "being edited by someone else") {
		t.Errorf("a locked program was opened anyway: %q", got)
	}
}

// TestEditorTakesEveryLine checks that the editor, not the command parser,
// sees what a player types while a session is open.
func TestEditorTakesEveryLine(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("@program swallow")
	h.out()

	// Only the first letter of the last word means anything, so "jump" is
	// an illegal editor command rather than reaching the parser. ("look"
	// would not do here: it starts with 'l', the list command.)
	h.send("jump")
	if got := h.out(); !strings.Contains(got, "Illegal editor command.") {
		t.Errorf("the parser saw a line meant for the editor: %q", got)
	}

	// QUIT and @Q are answered before the editor sees them, so a player is
	// never trapped. @Q with no program running says nothing at all.
	h.send("@Q")
	if got := h.out(); got != "" {
		t.Errorf("@Q in the editor said %q, want nothing", got)
	}
	h.send("x")
	h.out()
	h.send("look")
	if got := h.out(); !strings.Contains(got, "A quiet study.") {
		t.Errorf("leaving the editor did not restore the parser: %q", got)
	}
}

// TestListShowsStoredSource checks @list, including that it reads what is
// saved rather than an open buffer.
func TestListShowsStoredSource(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("@program listed")
	h.out()
	h.send("i")
	h.send(": main 1 pop ;")
	h.send(".")
	h.send("q")
	h.out()

	h.send("@list listed")
	if got := h.out(); !strings.Contains(got, ": main 1 pop ;") {
		t.Errorf("@list showed:\n%s", got)
	}

	h.send("@list listed=#")
	if got := h.out(); !strings.Contains(got, "  1: : main 1 pop ;") {
		t.Errorf("@list with numbers showed:\n%s", got)
	}
}

// TestMacrosDefineAndDelete covers the editor's macro table, which the
// compiler reads when it meets a '.name' token.
func TestMacrosDefineAndDelete(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("@program macros")
	h.out()

	h.send("def twice 2 *")
	if got := h.out(); !strings.Contains(got, "Entry created.") {
		t.Fatalf("def said %q", got)
	}
	h.send("def twice 3 *")
	if got := h.out(); !strings.Contains(got, "That macro already exists!") {
		t.Errorf("redefining said %q", got)
	}

	// The definition reaches the compiler.
	h.send("i")
	h.send(": main 21 .twice pop ;")
	h.send(".")
	h.send("c")
	if got := h.out(); !strings.Contains(got, "Program compiled successfully.") {
		t.Errorf("a macro did not expand:\n%s", got)
	}

	h.send("twice k")
	if got := h.out(); !strings.Contains(got, "Macro entry deleted.") {
		t.Errorf("k said %q", got)
	}
	h.send("twice k")
	if got := h.out(); !strings.Contains(got, "Macro to delete not found.") {
		t.Errorf("deleting twice said %q", got)
	}
}

// TestPublicsAndDisassembly covers the two commands that read compiled output.
func TestPublicsAndDisassembly(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("@program lib")
	h.out()
	h.send("i")
	h.send(": helper 1 pop ;")
	h.send("public helper")
	h.send(": main 1 pop ;")
	h.send(".")
	h.out()

	h.send("p")
	got := h.out()
	if !strings.Contains(got, "PUBLIC functions:") || !strings.Contains(got, "helper") {
		t.Errorf("p said:\n%s", got)
	}

	// "u" shows what a compile produced, and never compiles anything
	// itself, so it says nothing is there until "c" has run.
	h.send("u")
	if got := h.out(); !strings.Contains(got, "Nothing to disassemble!") {
		t.Errorf("u before compiling said:\n%s", got)
	}
	h.send("c")
	h.out()
	h.send("u")
	if got := h.out(); !strings.Contains(got, "FUNCTION: helper") {
		t.Errorf("u said:\n%s", got)
	}
}

// TestEditingSurvivesReconnect checks that a session outlives the connection
// that opened it and says so, rather than silently eating input.
func TestEditingSurvivesReconnect(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("@program persist")
	h.out()
	h.send("i")
	h.out()

	h.d.Close()
	h.s.Disconnect(h.d)

	d, err := h.s.Connect("line", "test")
	if err != nil {
		t.Fatal(err)
	}
	h.d = d
	h.send("connect Wizard secret")
	got := h.out()
	if !strings.Contains(got, "inserting MUF program text") {
		t.Errorf("reconnecting said nothing about the open editor:\n%s", got)
	}
}

// sourceOfProgram returns the source of the only program the wizard is
// carrying.
func (h *harness) sourceOfProgram(t *testing.T) string {
	t.Helper()
	var src string
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		for _, c := range w.Contents(h.wizRef()) {
			if o := w.Get(c); o != nil && o.Type() == ref.TypeProgram {
				src, _ = w.Source(c)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	return src
}
