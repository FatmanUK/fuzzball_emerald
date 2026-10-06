package golden

import (
	"context"
	"testing"
)

// Upstream's {with} pushes a new variable with new_mvar and pops it
// with free_top_mvar, so binding a name that is already bound
// **shadows** it and the outer value comes back afterwards.
//
// Emerald's Env.SetVar searched for an existing name and overwrote it
// rather than appending, and PopVar then removed whatever was last
// — so the outer binding was destroyed instead of restored.
//
// The fix was to separate the two operations upstream keeps apart:
// Env.BindVar pushes, which is what {with} and the looping functions
// need, and Env.AssignVar writes to the innermost binding, which is
// what {set}, {inc} and {dec} need. One function doing both could do
// neither correctly.
func TestMPIVariableScopeMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	const src = `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop
  var! out me @ "[" out @ strcat "]" strcat notify
;
: main
  ( --- a rebinding shadows, and the outer value comes back --- )
  "{with:n,1,{with:n,2,{&n}}{&n}}" show
  "{with:n,1,{with:n,2,{inc:n}}{&n}}" show
  "{with:n,1,{with:n,2,{set:n,9}}{&n}}" show
  "{with:n,1,{with:n,2,{set:n,9}{&n}}{&n}}" show
  "{with:a,1,{with:b,2,{&a}{&b}}{&a}}" show
  "{with:n,1,{with:n,2,{with:n,3,{&n}}{&n}}{&n}}" show

  ( --- assignment reaches the innermost binding only --------- )
  "{with:n,1,{with:n,2,{inc:n}{&n}}{&n}}" show
  "{with:n,1,{with:n,2,{dec:n,5}{&n}}{&n}}" show

  ( --- the loops bind too, so the same shadowing applies ----- )
  "{with:i,9,{for:i,1,3,1,{&i}}{&i}}" show
  "{with:x,9,{foreach:x,a b c,{&x}}{&x}}" show
  "{for:i,1,2,1,{for:i,5,6,1,{&i}}}" show

  ( --- a name the outer scope never had is gone afterwards --- )
  "{with:n,1,{&n}}{&n}" show

  ( --- only the last body's result is returned, not all of
        them joined: each is parsed into one reused buffer and
        the pointer is returned afterwards ------------------- )
  "{with:n,5,a,b,c}" show
  "{with:n,5,{&n},x,y}" show
  "{with:n,5,{&n}}" show
  "{with:n,5,{set:n,7},{&n}}" show

  ( --- upstream's two binding refusals, neither of which this
        server had: it answered an invented "Variable limit
        exceeded." and never checked the name at all ---------- )
  "{with:abcdefghijklmnop,1,{&abcdefghijklmnop}}" show
  "{with:abcdefghijklmnopq,1,x}" show
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
