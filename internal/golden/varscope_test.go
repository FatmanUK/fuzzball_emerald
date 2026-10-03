package golden

import (
	"context"
	"testing"
)

// Upstream's {with} pushes a new variable with new_mvar and pops it
// with free_top_mvar, so binding a name that is already bound
// **shadows** it and the outer value comes back afterwards.
//
// Emerald's Env.SetVar searches for an existing name and overwrites
// it rather than appending, and PopVar then removes whatever is last
// — so the outer binding is destroyed instead of restored.
//
// This case is **expected to fail** until that is fixed, and is
// skipped rather than deleted so the measurement is not lost. The fix
// is to separate the two operations upstream keeps apart: {with} and
// the looping functions bind, while {set}, {inc} and {dec} assign to
// an existing binding in place.
func TestMPIVariableScopeMatchesFuzzball(t *testing.T) {
	t.Skip("variable shadowing is a known gap — see " +
		"docs/upstream-coverage.md item 6")

	requireOracle(t)
	ctx := context.Background()

	const src = `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop
  var! out me @ "[" out @ strcat "]" strcat notify
;
: main
  "{with:n,1,{with:n,2,{&n}}{&n}}" show
  "{with:n,1,{with:n,2,{inc:n}}{&n}}" show
  "{with:n,1,{with:n,2,{set:n,9}}{&n}}" show
  "{with:a,1,{with:b,2,{&a}{&b}}{&a}}" show
;`
	fx, err := WriteFixture(t.TempDir(), src)
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
