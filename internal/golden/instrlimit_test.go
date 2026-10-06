package golden

import (
	"context"
	"regexp"
	"testing"
)

// instr_slice, max_instr_count and max_ml4_preempt_count were read
// nowhere: every Run passed an empty muf.Limits{}, so the compiled-in
// defaults applied and all three parameters were inert.
//
// Wiring them up meant fixing the rules they drive, because none was
// applied the way interp_loop applies it. The one this case measures
// is the **total ceiling**, which upstream imposes only **below
// mucker 3** and at **four times** the parameter for mucker 2
// (interp.c:1876) — where this server capped every program at the
// same unconditional figure and aborted with an invented message.
//
// So the same program and the same parameter give three different
// answers at the three levels, which is what the three runs below
// show. The loop is deliberately far from either boundary: the two
// compilers emit different code for the same source, so a program
// near the ceiling would diverge on the instruction count rather than
// on the rule.
const instrLimitSource = `: ts[ s -- ] me @ s @ notify ;
: main
  var i 0 i !
  begin i @ 1 + i ! i @ 30 >= until
  "finished the loop" ts
;`

// Three copies of the same program, because a mucker level only takes
// effect on a program's **first** run here: the level is baked into
// the cached compile and @set does not invalidate it, where upstream
// reads ProgMLevel afresh inside interp_loop (interp.c:1706). That is
// recorded in docs/upstream-coverage.md as its own gap; this case
// works around it by giving each level a program of its own, compiled
// cold.
var instrLimitPrograms = []Program{
	{Name: "one", Source: instrLimitSource},
	{Name: "two", Source: instrLimitSource},
	{Name: "three", Source: instrLimitSource},
}

// instrLimitScript lowers the ceiling and runs the same loop at each
// of the three mucker levels.
var instrLimitScript = Script{
	"@tune max_instr_count=100",
	"@set one.muf=1",
	"@set two.muf=2",
	"@set three.muf=3",

	// Mucker 1: the ceiling is the parameter itself, and a
	// thirty-iteration loop is well past it.
	"one",
	// Mucker 2: four times the parameter, which the same loop
	// fits inside.
	"two",
	// Mucker 3: no total ceiling at all.
	"three",
}

// TestInstrLimitMatchesFuzzball compares all four runs.
func TestInstrLimitMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteMultiFixture(t.TempDir(),
		instrLimitPrograms)
	if err != nil {
		t.Fatal(err)
	}
	script := instrLimitScript
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
		if diffs := Compare(maskInstr(want),
			maskInstr(got)); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}

// maskInstr blanks the instruction a MUF error report names, which
// cannot agree and is masked rather than dropped — the same answer
// examine's "Memory used" line gets.
//
// Upstream renders the current instruction with insttotext, so an
// abort on a jump reads "IF->line4"; this server's report names only
// *primitives*, so the field is empty there. Neither is wrong: the
// two compilers emit different code for the same source, which is why
// the debug trace is compared by source line rather than instruction
// for instruction (see CLAUDE.md). The line number either side of it
// still compares.
func maskInstr(s string) string {
	return instrField.ReplaceAllString(s, "$1<inst>$2")
}

var instrField = regexp.MustCompile(`(, line \d+; ).*?(: )`)
