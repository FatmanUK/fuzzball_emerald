package golden

import (
	"context"
	"testing"
	"time"
)

// sanityScript runs the checker over a clean database, damages it with
// @sanchange, checks again, repairs, and checks once more.
//
// @sanchange is the only way to create the damage from inside the game, which
// is exactly what it exists for.
var sanityScript = Script{
	"@sanity",

	// A room whose drop-to points at an exit, and a thing homed to
	// something that cannot be a home.
	"@sanchange #0 home #3",
	"@sanchange #2 location #3",
	"@sanity",

	// Break a containment chain outright.
	"@sanchange #1 contents #999",
	"@sanity",

	"@sanfix",
	"@sanity",

	// The arguments it refuses.
	"@sanchange",
	"@sanchange #0 nosuchfield #1",
	"@sanchange #99999 home #1",
	"@sanchange notadbref home #1",
}

// uncompared are the steps whose output is deliberately not diffed.
//
// @sanfix prints what it changed, and the two servers repair differently:
// upstream cuts the damaged chains and then hunts for whatever fell out of
// them, while Emerald corrects each object and rebuilds the chains from the
// locations, because it stores each object's location as well as the chain.
// What is compared instead is the check either side of the repair, which is
// what says whether they agree about the state of the database.
//
// The malformed-dbref case is upstream reading a stale stack variable:
// sscanf leaves its target untouched when the input does not parse, so the
// number it reports is left over from the previous command. That is not
// behaviour to reproduce — it is not stable enough for anything to depend on
// — so this server reports the dbref it actually failed to read.
var uncompared = map[string]bool{
	"@sanfix":                      true,
	"@sanchange notadbref home #1": true,
}

// TestSanityMatchesFuzzball checks the sanity reports against the C server.
func TestSanityMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, sanityScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, sanityScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range sanityScript {
		if uncompared[cmd] {
			continue
		}
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
