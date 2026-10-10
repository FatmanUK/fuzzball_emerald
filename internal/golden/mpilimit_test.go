package golden

import (
	"context"
	"testing"
)

// `mpi_max_commands` had no reader: `Env.MaxInstructions` was set by
// **none** of the seven places that build an Env, so only the
// fallback constant applied — which happens to equal the default,
// which is why nothing noticed. A world that retuned the parameter
// got the default anyway, in either direction.

var mpiLimitScript = Script{
	// Five calls, which the default of 2048 allows and a limit of
	// three does not. The refusal names the function the count
	// ran out on, so where in the expression the limit bites is
	// visible too.
	"@describe here={null:{null:{null:{null:{null:}}}}}ok",
	"look",
	"@tune mpi_max_commands=3",
	"look",
	"@tune mpi_max_commands=1",
	"look",
	"@tune %mpi_max_commands",
	"look",

	// A flat expression rather than a nested one, so where in it
	// the count runs out is visible rather than how deep.
	"@describe here={null:}{null:}{null:}{null:}done",
	"@tune mpi_max_commands=2",
	"look",

	// A `{&var}` keeps its own spelling in the refusal where an
	// ordinary call is named by the table -- `varflag` against
	// `mfun_list[s].name`.
	"@describe here={null:{null:{&how}}}",
	"look",
	"@tune %mpi_max_commands",
}

// TestMpiMaxCommandsMatchesFuzzball compares the ladder.
func TestMpiMaxCommandsMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}
	script := mpiLimitScript
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
