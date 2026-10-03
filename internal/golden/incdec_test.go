package golden

import (
	"context"
	"testing"
)

// {inc} and {dec} are variable operations, not arithmetic. mfn_inc
// (mfuns.c:2279) reads a variable *name*, refuses "No such variable
// currently defined." when there is none, adds or subtracts an
// optional amount, writes the result **back into the variable**, and
// returns it.
//
// Emerald read the first argument as a number, so "{inc:abc}"
// answered 1 and "{inc:5}" answered 6 — neither of which upstream
// can produce, since naming a bound variable is the only way to reach
// the arithmetic.

const incDecSource = `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop
  var! out me @ "[" out @ strcat "]" strcat notify
;
: main
  ( --- no such variable: the only answer without one ---------- )
  "{inc:abc}" show
  "{dec:abc}" show
  "{inc:5}" show
  "{inc:n}" show

  ( --- a bound variable, and the write-back ------------------- )
  "{with:n,5,{inc:n}}" show
  "{with:n,5,{inc:n}{&n}}" show
  "{with:n,5,{inc:n}{inc:n}{inc:n}}" show
  "{with:n,5,{dec:n}}" show
  "{with:n,5,{dec:n}{&n}}" show

  ( --- the optional amount ------------------------------------ )
  "{with:n,5,{inc:n,3}}" show
  "{with:n,5,{dec:n,3}}" show
  "{with:n,5,{inc:n,abc}}" show
  "{with:n,5,{inc:n,-2}}" show

  ( --- a non-numeric starting value reads as zero ------------- )
  "{with:n,abc,{inc:n}}" show
  "{with:n,,{inc:n}}" show

  ( --- the name is matched case-insensitively, as get_mvar
        uses strcasecmp ----------------------------------------- )
  "{with:n,5,{inc:N}{&n}}" show

  ( A probe for nested bindings of one name lived here and found
    a separate bug, so it has moved to its own case rather than
    making this one fail over something else: upstream *shadows*
    a rebinding and Emerald overwrites it. Measured —
    "{with:n,1,{with:n,2,{&n}}{&n}}" is "21" upstream and an
    "Unrecognized variable." error here. See
    docs/upstream-coverage.md. )
;`

// TestMPIIncDecMatchesFuzzball drives them through both servers.
func TestMPIIncDecMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), incDecSource)
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
