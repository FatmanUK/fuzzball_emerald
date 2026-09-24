package golden

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// traceProgram turns the tracer on, runs a few instructions of each
// shape the trace renders differently, and turns it off again.
//
// The shapes that matter are the ones with their own rendering: a
// literal, a global variable, a scoped variable, a call and its
// return, and a string long enough to be cut.
const traceProgram = `: helper[ str:s -- str:r ]
  s @ "!" strcat
;
: main
  var counter
  debug_on
  7 counter !
  counter @ pop
  "hi" helper pop
  "a string comfortably longer than the thirty characters a trace shows" pop
  debug_off
  me @ "done" notify
;`

// TestDebugTraceMatchesFuzzball compares which source lines the
// tracer walks through, and where tracing starts and stops.
//
// It deliberately does not compare the trace line for line, because
// the two servers do not compile to the same instructions. Upstream
// fuses a variable reference with the "!" or "@" that follows it, and
// a procedure address with the call that consumes it; Emerald emits
// each as its own instruction. So upstream prints
//
//	Debug> Pid 7: #58 7 ("", 7) SV0:counter !
//
// where Emerald prints two lines, the second with the variable on the
// stack. Every *rendering* agrees — the source line, the stack, the
// string cut at thirty characters, "INIT FUNC: helper (1 arg)",
// "EXIT" — and that is what this compares, by walking the sequence
// of source lines each server traced.
func TestDebugTraceMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), traceProgram)
	if err != nil {
		t.Fatal(err)
	}
	script := Script{"test"}

	oracle, err := RunOracleSteps(ctx, fx, script, nil)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, script, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}
	if len(oracle) == 0 || len(emerald) == 0 {
		t.Fatalf("got %d oracle and %d emerald transcripts", len(oracle), len(emerald))
	}

	want, got := tracedLines(oracle[0]), tracedLines(emerald[0])
	if len(want) == 0 {
		t.Fatalf("the oracle produced no trace at all:\n%s", oracle[0])
	}
	if diffs := Compare(strings.Join(want, "\n"), strings.Join(got, "\n")); len(diffs) > 0 {
		t.Errorf("the tracer walked different source lines:\n%s", Render(diffs))
	}

	// The output either side of the traced region has to match
	// too, so a tracer that never stopped would be caught.
	if diffs := Compare(withoutTrace(oracle[0]), withoutTrace(emerald[0])); len(diffs) > 0 {
		t.Errorf("the untraced output differs:\n%s", Render(diffs))
	}
}

// tracedLines is the sequence of source lines the tracer reported,
// with consecutive repeats collapsed: one server may take three
// instructions over a line where the other takes two, but both must
// walk the same lines in the same order.
func tracedLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		m := traceLine.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
		if m == nil {
			continue
		}
		if len(out) == 0 || out[len(out)-1] != m[1] {
			out = append(out, m[1])
		}
	}
	return out
}

// withoutTrace is everything the program printed that was not a trace
// line.
func withoutTrace(s string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if !traceLine.MatchString(line) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// traceLine matches one tracer line, capturing the source line
// number. The pid and the program's dbref differ between the two
// servers and are not captured.
var traceLine = regexp.MustCompile(`^Debug> Pid \d+: #\d+ (\d+) \(`)
