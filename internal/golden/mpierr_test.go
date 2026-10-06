package golden

import (
	"context"
	"testing"
)

// A nested MPI failure is reported as **several lines**, not one. The
// innermost ABORT_MPI notifies its own message, and then every
// enclosing function that was pre-parsing an argument notifies
// "{NAME} (arg N)" as the NULL propagates out (msgparse.c:1637).
//
// This server printed the first line and stopped, so anything that
// failed inside a nest gave no clue where it had failed from.
//
// The frames carry no colon, unlike the message line, and each is
// prefixed by the "how" variable the same way.
const mpiErrSource = `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop pop
;
: main
  ( one level of nesting )
  "{null:{force:me,look}}" show
  ( two, so two frames )
  "{null:{null:{force:me,look}}}" show
  ( the failing argument's position is reported, not just the
    function: here it is the second )
  "{midstr:abc,{force:me,look}}" show
  ( a function that does *not* pre-parse its arguments adds no
    frame -- {lit} is the clearest of those )
  "{lit:{force:me,look}}" show
  ( a deeper mixture )
  "{toupper:{null:{force:me,look}}}" show
  ( and one that fails for a different reason entirely )
  "{null:{&nosuchvariable}}" show
;`

// TestMPIErrorWalksBackOutMatchesFuzzball compares the whole report.
func TestMPIErrorWalksBackOutMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), mpiErrSource)
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
