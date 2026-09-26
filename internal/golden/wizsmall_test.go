package golden

import (
	"context"
	"testing"
	"time"
)

// wizSmallScript covers @examine and @debug, the two small wizard
// commands left over from the audit.
//
// @examine is not `examine`: it is the fourth of the @san family, and
// prints the raw chain fields a repair would act on rather than an
// object as a player sees it. That is what makes it useful on a world
// that will not boot.
//
// @debug's only option is "display propcache", and only under
// DISKBASE — which neither this server nor the oracle's build has,
// so both answer the same way to everything.
var wizSmallScript = Script{
	"@create widget",
	"@dig Workshop",
	"@open north=#5",

	// Every type, since the per-type tail differs.
	"@examine",
	"@examine me",
	"@examine widget",
	"@examine #5",
	"@examine north",
	"@examine test.muf",
	"@examine #0",
	"@examine nosuchthing",

	// @debug takes anything and says the same thing.
	"@debug",
	"@debug display propcache",
	"@debug nonsense",

	// @examine is exact-match — @exa is not enough, because
	// @examine sits under a node that commits further — and
	// @debug is strcasecmp, so it has no abbreviation at all.
	"@exam widget",
	"@deb",
	"@DEBUG",
}

// TestWizSmallMatchesFuzzball checks @examine and @debug against the
// C server.
func TestWizSmallMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, wizSmallScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, wizSmallScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range wizSmallScript {
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
