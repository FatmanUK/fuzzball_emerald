package golden

import (
	"context"
	"testing"
)

// connectsScript raises "test" to mlevel 4 the same way forceScript does —
// see its own doc comment for why both @set lines are needed.
var connectsScript = Script{
	"@set test=wizard",
	"@set test=3",
	"test",
}

// DESCRHOST and DESCRUSER's own successful-path values are environment-
// dependent — a container's view of the peer address, and (per DESCRUSER's
// own doc comment in internal/muf/frame.go) an OS-assigned ephemeral port
// upstream reports and Emerald has no equivalent for — so this checks shape
// (a live descriptor answers some string) and the argument-validation
// wording, both of which are deterministic, rather than content.
const connectsProgramSource = tellPrelude + `: main
  descr descrhost string? t
  descr descruser string? t
  0 try "x" descrhost catch ts endcatch
  0 try 999999 descrhost catch ts endcatch
  0 try "x" descruser catch ts endcatch
  0 try 999999 descruser catch ts endcatch
;`

// TestConnectsMatchesFuzzball checks DESCRHOST and DESCRUSER against the C
// server — the two primitives in Phase 3 that need mlevel 4, so cannot run
// in the shared "connects" case in golden_test.go, which compiles at 3.
func TestConnectsMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), connectsProgramSource)
	if err != nil {
		t.Fatal(err)
	}

	oracle, err := RunOracleSteps(ctx, fx, connectsScript, nil)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, connectsScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range connectsScript {
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
