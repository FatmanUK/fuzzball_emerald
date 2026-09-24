package golden

import (
	"context"
	"testing"
	"time"
)

// forceScript exercises FORCE, FORCEDBY and FORCEDBY_ARRAY against
// both servers. WriteFixture's own "test" program compiles at mlevel
// 3 (Mucker and SMucker, no Wizard bit), one short of FORCE's floor
// of 4 — @set raises it before running: the Wizard bit plus any
// mucker bit is level 4 outright, which is also why both @set lines
// are needed and neither alone would do.
//
// The program forces itself: "test" checks its own FORCEDBY first.
// Unset (#-1, nobody has forced it yet), it forces #1 to run "test"
// again and reports that it did; on that second, forced run FORCEDBY
// is now set — to "test"'s own dbref, since the forcing player (#1)
// and the forcing program (test) differ, prim_force's own "if (player
// != program)" pushes both — so it reports FORCEDBY and
// FORCEDBY_ARRAY's count instead of forcing again, which is what
// stops it recursing.
//
// It is driven without markers for the same reason wizScript is:
// @force (and here, FORCE) runs a command as someone else, and a
// marker pose sent afterwards would be attributed to whoever was left
// holding the line.
var forceScript = Script{
	"@set test=wizard",
	"@set test=3",
	"test",
}

const forceProgramSource = `: main
  forcedby #-1 = if
    #1 "test" force
    me @ "did-force" notify
  else
    me @ "forcedby=" forcedby intostr strcat notify
    me @ "count=" forcedby_array array_count intostr strcat notify
  then
;`

// TestForceMatchesFuzzball checks FORCE, FORCEDBY and FORCEDBY_ARRAY
// against the C server.
func TestForceMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), forceProgramSource)
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, forceScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, forceScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range forceScript {
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
