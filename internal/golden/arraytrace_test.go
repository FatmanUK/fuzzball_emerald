package golden

import (
	"context"
	"testing"
)

// `expanded_debug_trace` defaults **true** and had no reader, so an
// array in a trace line or a backtrace read "3{...}" where upstream
// spells out its contents.
//
// The trace itself cannot be compared line for line -- the two
// compilers emit different instructions, which is why
// debugtrace_test.go compares the sequence of source lines instead --
// but `muf_backtrace` renders a procedure's arguments with the same
// function and the same `expandarrs` of 1 (`debugger.c:470`), and a
// backtrace **is** comparable. So the probe aborts inside a procedure
// holding one.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const arrayTraceSource = `
: show[ a -- ]
  "boom" abort
;
: main
  { "a" "b" "c" }list show
;`

// arrayTraceBig has more than the eight items a rendering shows, so
// the truncation marker is visible, and a nested array, which is
// rendered **unexpanded** however the parameter is set. Its second
// argument is a long string, which the same function cuts at thirty
// characters and marks with a trailing underscore -- where this had a
// second renderer that cut without the marker.
const arrayTraceBigSource = `
: show[ a b -- ]
  "boom" abort
;
: main
  { { "in" }list 1 2 3 4 5 6 7 8 9 10 }list
  "a string comfortably longer than the thirty characters"
  show
;`

var arrayTraceScript = Script{
	"@set test.muf=wizard",
	"@set test.muf=3",
	"test",
	"@tune expanded_debug_trace=no",
	"test",
	"@tune %expanded_debug_trace",
	"test",
}

// TestArrayTraceMatchesFuzzball compares both renderings.
func TestArrayTraceMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	for _, src := range []string{arrayTraceSource,
		arrayTraceBigSource} {

		runArrayTrace(t, src)
	}
}

func runArrayTrace(t *testing.T, src string) {
	t.Helper()
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), src)
	if err != nil {
		t.Fatal(err)
	}
	script := arrayTraceScript
	oracle, err := RunOracleSteps(ctx, fx, script, nil)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, script, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}
	for i, cmd := range script {
		var want, got string
		if i < len(oracle) {
			want = oracle[i]
		}
		if i < len(emerald) {
			got = emerald[i]
		}
		if diffs := Compare(want, got); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}
