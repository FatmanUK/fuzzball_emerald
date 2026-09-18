package compiler

import (
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
)

// compile is a shorthand that fails the test on error.
func compile(t *testing.T, src string) *muf.Program {
	t.Helper()
	p, err := Compile(src, Options{})
	if err != nil {
		t.Fatalf("compiling %q: %v", src, err)
	}
	return p
}

// mustFail requires a compile error containing want.
func mustFail(t *testing.T, src, want string) {
	t.Helper()
	_, err := Compile(src, Options{})
	if err == nil {
		t.Fatalf("compiling %q should have failed", src)
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error for %q = %q, want it to mention %q", src, err, want)
	}
}

func TestMinimalProgram(t *testing.T) {
	p := compile(t, `: main "hi" me @ notify ;`)
	if len(p.Code) == 0 {
		t.Fatal("no code was produced")
	}
	if p.Code[p.Start].Type != muf.TypeFunction {
		t.Errorf("the entry point is a %v, want a function header", p.Code[p.Start].Type)
	}
	if p.Code[p.Start].Proc.Name != "main" {
		t.Errorf("entry procedure = %q", p.Code[p.Start].Proc.Name)
	}
}

func TestReservedVariables(t *testing.T) {
	p := compile(t, ": main me @ loc @ trigger @ command @ pop pop pop pop ;")
	want := []string{"me", "loc", "trigger", "command"}
	for i, name := range want {
		if p.Vars[i] != name {
			t.Errorf("Vars[%d] = %q, want %q", i, p.Vars[i], name)
		}
	}
}

// TestEntryPointIsLastProcedure pins a convention that is easy to get
// backwards: a program starts at the last procedure it defines, not the first.
func TestEntryPointIsLastProcedure(t *testing.T) {
	p := compile(t, ": first 1 pop ; : second 2 pop ; : third 3 pop ;")
	if got := p.Code[p.Start].Proc.Name; got != "third" {
		t.Errorf("entry procedure = %q, want third", got)
	}
}

func TestIfThenJumpsPastTheBody(t *testing.T) {
	p := compile(t, ": main 1 if 2 pop then 3 pop ;")
	var ifAddr int = -1
	for i, in := range p.Code {
		if in.Type == muf.TypeIf {
			ifAddr = i
			break
		}
	}
	if ifAddr < 0 {
		t.Fatal("no conditional jump was emitted")
	}
	target := int(p.Code[ifAddr].Num)
	if target <= ifAddr || target > len(p.Code) {
		t.Errorf("IF at %d jumps to %d, which is not past its body", ifAddr, target)
	}
}

func TestIfElseThen(t *testing.T) {
	p := compile(t, ": main 1 if 2 else 3 then pop ;")
	var ifs, jmps int
	for _, in := range p.Code {
		switch in.Type {
		case muf.TypeIf:
			ifs++
		case muf.TypeJmp:
			jmps++
		}
	}
	if ifs != 1 || jmps != 1 {
		t.Errorf("got %d conditional and %d unconditional jumps, want 1 of each", ifs, jmps)
	}
}

func TestLoopJumpsBackwards(t *testing.T) {
	p := compile(t, ": main begin 1 pop 0 until ;")
	found := false
	for i, in := range p.Code {
		if in.Type == muf.TypeIf && int(in.Num) < i {
			found = true
		}
	}
	if !found {
		t.Error("UNTIL should emit a backwards conditional jump")
	}
}

func TestBreakAndContinue(t *testing.T) {
	// Both must resolve to real addresses rather than being left at zero.
	p := compile(t, ": main begin 1 if break else continue then repeat ;")
	for i, in := range p.Code {
		if in.Type == muf.TypeJmp && in.Num == 0 {
			t.Errorf("an unresolved jump was left at %d", i)
		}
	}
}

func TestProcedureArguments(t *testing.T) {
	// "type:name" documents the type; the variable is what follows the colon.
	p := compile(t, ": greet[ str:who int:times -- str:result ] who @ pop times @ pop \"\" ; : main \"x\" 1 greet pop ;")
	proc := p.Code[p.Procs["greet"]].Proc
	if proc.Args != 2 {
		t.Errorf("greet takes %d arguments, want 2", proc.Args)
	}
	want := []string{"who", "times"}
	for i, n := range want {
		if proc.VarNames[i] != n {
			t.Errorf("argument %d = %q, want %q", i, proc.VarNames[i], n)
		}
	}
}

// TestScopedVariablesArePerProcedure covers a rule that bit the port: VAR and
// VAR! inside a procedure declare a scoped variable, so two procedures may
// each declare the same name. Only at the top level does VAR create a global.
func TestScopedVariablesArePerProcedure(t *testing.T) {
	compile(t, ": one 1 var! pos pos @ pop ; : two 2 var! pos pos @ pop ;")
	compile(t, ": one var pos ; : two var pos ;")

	// At the top level VAR is a global, so a repeat really is a clash.
	mustFail(t, "var pos var pos : main ;", "already declared")
	// And VAR! has nothing to store from.
	mustFail(t, "var! pos : main ;", "outside a procedure")
}

func TestUnterminatedBlocksAreReported(t *testing.T) {
	mustFail(t, ": main 1 if 2 ;", "unterminated IF-THEN")
	mustFail(t, ": main begin 1 ;", "unterminated loop")
	mustFail(t, ": main try 1 ;", "unterminated TRY")
	mustFail(t, ": main 1 ", "unterminated procedure")
	mustFail(t, ": main then ;", "THEN without IF")
	mustFail(t, ": main else ;", "ELSE without IF")
	mustFail(t, ": main repeat ;", "loop start not found")
	mustFail(t, ": main endcatch ;", "no CATCH found")
}

func TestDefinitionsExpand(t *testing.T) {
	p := compile(t, "$define TWO 2 $enddef : main TWO pop ;")
	found := false
	for _, in := range p.Code {
		if in.Type == muf.TypeInteger && in.Num == 2 {
			found = true
		}
	}
	if !found {
		t.Error("the definition did not expand to its value")
	}
}

// TestDefinitionsShadowEverything pins where expansion happens. Upstream
// expands in the tokenizer, so a definition replaces a name before the
// compiler ever considers whether it is a variable or a primitive.
func TestDefinitionsShadowEverything(t *testing.T) {
	p := compile(t, "$define pop 42 $enddef : main pop pop ;")
	ints := 0
	for _, in := range p.Code {
		if in.Type == muf.TypeInteger && in.Num == 42 {
			ints++
		}
	}
	if ints != 2 {
		t.Errorf("the definition expanded %d times, want 2: it should shadow the primitive", ints)
	}
}

// TestBackslashEscapesExpansion covers the escape a program uses to name
// something a definition would otherwise replace.
func TestBackslashEscapesExpansion(t *testing.T) {
	p := compile(t, "$define pop 42 $enddef : main \\pop ;")
	for _, in := range p.Code {
		if in.Type == muf.TypeInteger && in.Num == 42 {
			t.Error("a backslash-escaped word should not expand")
		}
	}
}

func TestRecursiveDefinitionIsBounded(t *testing.T) {
	mustFail(t, "$define loop loop $enddef : main loop ;", "too many macro substitutions")
}

func TestBuiltinDefines(t *testing.T) {
	// "[]" and "}tell" are built-in definitions, not primitives.
	compile(t, ": main { 1 2 }list 0 [] pop ;")
	compile(t, ": main { \"hi\" }tell ;")
	// "desc" expands to a property read.
	p := compile(t, ": main me @ desc pop ;")
	found := false
	for _, in := range p.Code {
		if in.Type == muf.TypeString && in.Str == "_/de" {
			found = true
		}
	}
	if !found {
		t.Error("desc should expand to a read of the _/de property")
	}
}

func TestConditionalCompilation(t *testing.T) {
	// The taken branch compiles and the other is discarded.
	p := compile(t, "$define X 1 $enddef : main $ifdef X 111 $else 222 $endif pop ;")
	for _, in := range p.Code {
		if in.Type == muf.TypeInteger && in.Num == 222 {
			t.Error("the untaken branch was compiled")
		}
	}

	p = compile(t, ": main $ifdef NOPE 111 $else 222 $endif pop ;")
	for _, in := range p.Code {
		if in.Type == muf.TypeInteger && in.Num == 111 {
			t.Error("the untaken branch was compiled")
		}
	}
}

// TestConditionalComparison covers "$ifdef name=value", which compares what
// the name expands to rather than only testing that it exists.
func TestConditionalComparison(t *testing.T) {
	src := "$define KIND alpha $enddef : main $ifdef KIND=alpha 111 $else 222 $endif pop ;"
	p := compile(t, src)
	want, unwanted := int64(111), int64(222)
	for _, in := range p.Code {
		if in.Type == muf.TypeInteger && in.Num == unwanted {
			t.Error("the comparison chose the wrong branch")
		}
	}
	found := false
	for _, in := range p.Code {
		if in.Type == muf.TypeInteger && in.Num == want {
			found = true
		}
	}
	if !found {
		t.Error("the matching branch was not compiled")
	}

	// A value that does not match takes the other branch.
	p = compile(t, "$define KIND alpha $enddef : main $ifdef KIND=beta 111 $else 222 $endif pop ;")
	for _, in := range p.Code {
		if in.Type == muf.TypeInteger && in.Num == 111 {
			t.Error("a non-matching comparison took the wrong branch")
		}
	}
}

func TestMacrosExpand(t *testing.T) {
	p, err := Compile(": main .double pop ;", Options{
		Macros: map[string]string{"double": "2 2 +"},
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, in := range p.Code {
		if in.Type == muf.TypePrimitive && muf.PrimName(int(in.Num)) == "+" {
			found = true
		}
	}
	if !found {
		t.Error("the macro did not expand")
	}
}

func TestIncludePullsDefinitions(t *testing.T) {
	p, err := Compile("$include $lib/test : main HELPER pop ;", Options{
		Include: func(target string) (map[string]string, bool) {
			if target == "$lib/test" {
				return map[string]string{"HELPER": "99"}, true
			}
			return nil, false
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, in := range p.Code {
		if in.Type == muf.TypeInteger && in.Num == 99 {
			found = true
		}
	}
	if !found {
		t.Error("the included definition was not available")
	}
}

func TestIncludeOfAMissingLibraryIsAnError(t *testing.T) {
	mustFail(t, "$include $lib/nope : main ;", "not a program")
}

func TestIflibGuardsAnInclude(t *testing.T) {
	// The common idiom: include a library only when it exists.
	_, err := Compile("$iflib $lib/nope $include $lib/nope $endif : main 1 pop ;",
		Options{})
	if err != nil {
		t.Errorf("a guarded include of a missing library should compile: %v", err)
	}
}

func TestLiteralsCompile(t *testing.T) {
	p := compile(t, `: main 42 -7 3.5 "text" #12 pop pop pop pop pop ;`)
	kinds := map[muf.Type]bool{}
	for _, in := range p.Code {
		kinds[in.Type] = true
	}
	for _, want := range []muf.Type{
		muf.TypeInteger, muf.TypeFloat, muf.TypeString, muf.TypeObject,
	} {
		if !kinds[want] {
			t.Errorf("no %v literal was compiled", want)
		}
	}
}

// TestQuotedStringsAreNotNumbers checks that a literal keeps its type even
// when its contents look like something else.
func TestQuotedStringsAreNotNumbers(t *testing.T) {
	p := compile(t, `: main "123" pop ;`)
	for _, in := range p.Code {
		if in.Type == muf.TypeInteger {
			t.Error(`"123" was compiled as an integer`)
		}
	}
}

func TestUnknownWordIsAnError(t *testing.T) {
	mustFail(t, ": main nosuchword ;", "unrecognized word")
}

func TestProgramWithNoProceduresIsAnError(t *testing.T) {
	mustFail(t, "1 2 3", "no procedures")
}

func TestPrimitiveOutsideProcedureIsAnError(t *testing.T) {
	mustFail(t, "pop : main ;", "outside a procedure")
}

func TestJumpTargetsAreInRange(t *testing.T) {
	p := compile(t, ": main begin 1 if 2 pop break then 0 until ;")
	for i, in := range p.Code {
		switch in.Type {
		case muf.TypeIf, muf.TypeJmp, muf.TypeTry:
			if in.Num < 0 || int(in.Num) > len(p.Code) {
				t.Errorf("instruction %d branches to %d, outside the program", i, in.Num)
			}
		}
	}
}

func TestTryCatch(t *testing.T) {
	p := compile(t, ": main try 1 pop catch pop endcatch ;")
	var trys int
	for _, in := range p.Code {
		if in.Type == muf.TypeTry {
			trys++
		}
	}
	if trys != 1 {
		t.Errorf("got %d try instructions, want 1", trys)
	}
}
