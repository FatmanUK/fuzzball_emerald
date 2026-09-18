package golden

import (
	"context"
	"testing"
	"time"
)

// Case is one differential test: a MUF program, and what to type at it.
type Case struct {
	Name string
	// Source is the program's MUF. It is installed as test.muf and run by
	// an exit named "test".
	Source string
	// Script is what to type. When empty, the exit is triggered once.
	Script Script
}

// requireOracle skips unless the C server has been built.
func requireOracle(t *testing.T) {
	t.Helper()
	if !OracleAvailable() {
		t.Skip("the oracle image is not built; run 'make golden-build'")
	}
}

// runCase drives both servers and reports any difference.
func runCase(t *testing.T, c Case) {
	t.Helper()
	requireOracle(t)

	script := c.Script
	if len(script) == 0 {
		script = Script{"test"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Each server gets its own copy, so neither can see what the other left
	// behind in the database.
	oracleFx, err := WriteFixture(t.TempDir(), c.Source)
	if err != nil {
		t.Fatal(err)
	}
	emeraldFx, err := WriteFixture(t.TempDir(), c.Source)
	if err != nil {
		t.Fatal(err)
	}

	oracleOut, err := RunOracle(ctx, oracleFx, script)
	if err != nil {
		t.Fatalf("running the oracle: %v\ntranscript so far:\n%s", err, oracleOut)
	}
	emeraldOut, err := RunEmerald(ctx, emeraldFx, script)
	if err != nil {
		t.Fatalf("running emerald: %v\ntranscript so far:\n%s", err, emeraldOut)
	}

	if diffs := Compare(oracleOut, emeraldOut); len(diffs) > 0 {
		t.Errorf("transcripts differ:\n%s\nfuzzball said:\n%s\nemerald said:\n%s",
			Render(diffs), oracleOut, emeraldOut)
	}
}

// tell is the idiom the cases use to report a value, so a program's output is
// one line per result.
const tellPrelude = `: t[ x -- ] me @ x @ intostr notify ;
: ts[ s -- ] me @ s @ notify ;
`

func TestArithmetic(t *testing.T) {
	runCase(t, Case{
		Name: "arithmetic",
		Source: tellPrelude + `: main
  2 3 + t
  10 3 - t
  6 7 * t
  20 4 / t
  17 5 % t
  -7 2 / t
  -7 2 % t
  0 5 - t
;`,
	})
}

func TestComparisons(t *testing.T) {
	runCase(t, Case{
		Name: "comparisons",
		Source: tellPrelude + `: main
  1 2 < t
  2 1 < t
  2 2 = t
  2 2 >= t
  1 not t
  0 not t
  1 0 and t
  1 0 or t
  1 1 xor t
;`,
	})
}

func TestStrings(t *testing.T) {
	runCase(t, Case{
		Name: "strings",
		Source: tellPrelude + `: main
  "foo" "bar" strcat ts
  "hello" strlen t
  "hello" toupper ts
  "HELLO" tolower ts
  "  padded  " strip ts
  "hello" 2 3 midstr ts
  "hello" "ll" instr t
  "hello" "zz" instr t
  "abc" "abd" stringcmp t
;`,
	})
}

func TestStackOperations(t *testing.T) {
	runCase(t, Case{
		Name: "stack",
		Source: tellPrelude + `: main
  1 2 swap t t
  5 dup t t
  1 2 over t t t
  1 2 3 rot t t t
  1 2 nip t
  1 2 3 depth t pop pop pop
;`,
	})
}

func TestControlFlow(t *testing.T) {
	runCase(t, Case{
		Name: "control",
		Source: tellPrelude + `: main
  1 if 111 t else 222 t then
  0 if 111 t else 222 t then
  var i 0 i !
  begin i @ 1 + i ! i @ 3 >= until i @ t
  var sum 0 sum !
  1 5 1 for sum @ + sum ! repeat sum @ t
;`,
	})
}

func TestProcedures(t *testing.T) {
	runCase(t, Case{
		Name: "procedures",
		Source: tellPrelude + `: double[ int:n -- int:r ] n @ 2 * ;
: add[ int:a int:b -- int:r ] a @ b @ + ;
: main
  21 double t
  2 3 add t
  1 double double double t
;`,
	})
}

func TestArrays(t *testing.T) {
	runCase(t, Case{
		Name: "arrays",
		Source: tellPrelude + `: main
  { 1 2 3 }list array_count t
  { 10 20 30 }list 1 array_getitem t
  { "a" "b" "c" }list "," array_join ts
;`,
	})
}

func TestStringFormatting(t *testing.T) {
	runCase(t, Case{
		Name: "fmtstring",
		Source: tellPrelude + `: main
  "plain" "%s" fmtstring ts
  42 "%i" fmtstring ts
  "x" "%5s|" fmtstring ts
  "x" "%-5s|" fmtstring ts
  7 "%5i|" fmtstring ts
  1 2 "%i and %i" fmtstring ts
  "100%% sure" ts
;`,
	})
}

func TestStringCutAndCompare(t *testing.T) {
	runCase(t, Case{
		Name: "strcut",
		Source: tellPrelude + `: main
  "hello world" 5 strcut ts ts
  "hello" 0 strcut ts ts
  "hello" 99 strcut ts ts
  "abcdef" "abcxyz" 3 strncmp t
  "abcdef" "abcxyz" 4 strncmp t
  "one two one" "one" "X" subst ts
;`,
	})
}

func TestPatternMatching(t *testing.T) {
	runCase(t, Case{
		Name: "smatch",
		Source: tellPrelude + `: main
  "hello" "h*" smatch t
  "hello" "*o" smatch t
  "hello" "h?llo" smatch t
  "hello" "goodbye" smatch t
  "HELLO" "hello" smatch t
  "hello" "{hello|goodbye}" smatch t
;`,
	})
}

func TestObjectQueries(t *testing.T) {
	runCase(t, Case{
		Name: "objects",
		Source: tellPrelude + `: main
  me @ name ts
  me @ location intostr ts
  me @ owner intostr ts
  me @ player? t
  me @ room? t
  loc @ room? t
  me @ "wizard" flag? t
  me @ "dark" flag? t
  me @ me @ controls t
;`,
	})
}

func TestProperties(t *testing.T) {
	runCase(t, Case{
		Name: "properties",
		Source: tellPrelude + `: main
  me @ "test/str" "a value" setprop
  me @ "test/str" getpropstr ts
  me @ "test/num" "" 42 addprop
  me @ "test/num" getpropval t
  me @ "test/str" remove_prop
  me @ "test/str" getpropstr ts
  me @ "test" propdir? t
;`,
	})
}

func TestStackRotation(t *testing.T) {
	runCase(t, Case{
		Name: "rotate",
		Source: tellPrelude + `: main
  1 2 3 3 rotate t t t
  1 2 3 -3 rotate t t t
  1 2 3 2 rotate t t t
;`,
	})
}

func TestFloats(t *testing.T) {
	runCase(t, Case{
		Name: "floats",
		Source: tellPrelude + `: main
  2.5 ftostr ts
  4.0 sqrt ftostr ts
  2.0 10.0 pow ftostr ts
  -3.5 fabs ftostr ts
  3.7 floor ftostr ts
  3.2 ceil ftostr ts
  1.0 exp ftostr ts
  100.0 log10 ftostr ts
  "2.5" strtof ftostr ts
  7.0 2.0 fmod ftostr ts
;`,
	})
}

func TestArrayOperations(t *testing.T) {
	runCase(t, Case{
		Name: "array ops",
		Source: tellPrelude + `: main
  { 3 1 2 }list 0 array_sort array_vals t t t
  { 1 2 3 4 5 }list 1 3 array_getrange array_count t
  { 1 2 3 }list array_reverse array_vals t t t
  { "a" 1 "b" 2 }dict array_count t
  { 1 2 3 }list 1 array_delitem array_count t
;`,
	})
}

func TestTimeAndVersion(t *testing.T) {
	runCase(t, Case{
		Name: "timesplit",
		Source: tellPrelude + `: main
  0 timesplit
  t t t t t t t t
  "%Y-%m-%d" 0 timefmt ts
  "%H:%M:%S" 0 timefmt ts
;`,
	})
}

// TestProgramErrorFormat checks the block a failing program prints, which
// players and programs have read for decades: a header, the program and line,
// then a backtrace with the failing source line under each level.
func TestProgramErrorFormat(t *testing.T) {
	runCase(t, Case{
		Name: "error format",
		Source: `: main
  "not a number" 2 +
;`,
	})
}

// TestErrorInsideAProcedure checks that the backtrace names the procedure and
// shows the call above it.
func TestErrorInsideAProcedure(t *testing.T) {
	runCase(t, Case{
		Name: "nested error",
		Source: `: inner
  "bad" 2 +
;
: outer
  inner
;
: main
  outer
;`,
	})
}

// TestErrorWithArguments checks that a procedure's arguments appear in the
// backtrace.
func TestErrorWithArguments(t *testing.T) {
	runCase(t, Case{
		Name: "error with args",
		Source: `: boom[ str:what int:n -- ]
  what @ n @ +
;
: main
  "text" 7 boom
;`,
	})
}

// TestCaughtErrorPrintsNothing checks that a failure inside TRY is silent,
// which is what makes TRY usable.
func TestCaughtErrorPrintsNothing(t *testing.T) {
	runCase(t, Case{
		Name: "caught",
		Source: tellPrelude + `: main
  0 try "bad" 2 + catch ts endcatch
  "still here" ts
;`,
	})
}

// TestDivisionByZero checks that a failure reports the same way in both.
func TestDivisionByZero(t *testing.T) {
	runCase(t, Case{
		Name:   "divide by zero",
		Source: `: main 1 0 / me @ swap intostr notify ;`,
	})
}
