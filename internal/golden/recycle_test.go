package golden

import (
	"context"
	"testing"
)

// recycleScript covers the half of do_recycle that is not a
// permission rule, and the thing that made the @sweep case diverge:
// **a recycled dbref comes back**. Upstream keeps a free list and
// hands the most recently recycled ref to the next object built,
// which Emerald did not — so every dbref in every message after a
// @recycle was one too high, and a long-lived world's numbering
// drifted from the C's for ever.
//
// The per-type ownership rules are still divergent and are not
// exercised here; the harness's player is #1 and owns everything
// anyway. See docs/upstream-coverage.md.
var recycleScript = Script{
	"@create widget",
	"@create gadget",

	// The confirmation names the object and its number, and it is
	// taken before the recycling — which is what renames the
	// object to "<garbage>".
	"@recycle gadget",
	"@recycle gadget",

	// The next thing built takes the freed number, and the one
	// after that is fresh.
	"@create sprocket",
	"@create flange",

	// Last in, first out: recycle two and the second comes back
	// first.
	"@recycle sprocket",
	"@recycle flange",
	"@create first",
	"@create second",
	"@create third",

	// Garbage is still there to be looked at, and keeps the
	// description upstream gives it.
	"@recycle third",
	"@contents here",
	"@owned me",

	// The guard on an object a dbref @tune parameter points at
	// runs *before* the per-type refusals, and that ordering is
	// most of what anybody sees: #0 is default_room_parent's
	// value and #1 is toad_default_recipient's, so neither
	// "@recycle here" nor "@recycle me" ever reaches "Room #0
	// contains everything" or "You can't recycle a player!"
	"@recycle here",
	"@recycle #0",
	"@recycle me",

	// The same guard, on a parameter set by hand — and the
	// object becomes recyclable again when the parameter is put
	// back.
	"@dig Nowhere",
	"@tune player_start=#8",
	"@recycle #8",
	"@tune %player_start",
	"@recycle #8",

	// A recycled room's number comes back like anything else's.
	"@create afterroom",

	// The refusals the matcher gives.
	"@recycle nosuchthing",
	"@recycle",

	// The abbreviation: @rec is the shortest, since @recycle
	// shares its stem with nothing else at three characters.
	"@create spare",
	"@rec spare",
	"@re",
}

// TestRecycleMatchesFuzzball checks do_recycle and the free list
// against the C server.
func TestRecycleMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	oracle, err := RunOracleSteps(ctx, fx, recycleScript, nil)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, recycleScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range recycleScript {
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
