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

// TestDivisionByZero checks that a failure reports the same way in both.
func TestDivisionByZero(t *testing.T) {
	runCase(t, Case{
		Name:   "divide by zero",
		Source: `: main 1 0 / me @ swap intostr notify ;`,
	})
}
