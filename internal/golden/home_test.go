package golden

import (
	"context"
	"testing"
)

// "home" is a **direction**, not a command, and it is tested before
// exits rather than after them: can_move (predicates.c:509) answers
// yes for it outright when enable_home is set, so an exit of that
// name is unreachable. This server had it in the command table, which
// is consulted *after* exit matching, so the precedence was the other
// way round.
//
// And do_move's branch (move.c:730) is more than a move. It announces
// the departure to the room, says the same line **three times**,
// tells the player their possessions are gone, and then send_home
// sends the contents home *first* so they are there on arrival. This
// server printed one line, invented "You have no home to go to.", and
// left the inventory alone — a quiet teleport where upstream is a
// small ceremony with a cost.
//
// With enable_home clear the word is not special at all and falls
// through to exit matching, where this used to answer an invented
// "That command is disabled."
var homeScript = Script{
	// Somewhere to come back from, and something to lose.
	"@dig Elsewhere",
	"@create satchel",
	"inventory",

	// The ceremony, and the possessions left behind.
	"home",
	"inventory",
	"look",

	// "go home" is the same branch.
	"@teleport #4",
	"go home",

	// An exit named "home" is unreachable while enable_home is
	// set, because the direction test comes first.
	"@open home=#4",
	"home",

	// With enable_home clear the exit is reachable, and the word
	// is ordinary.
	"@tune enable_home=no",
	"home",
	"go home",
}

// TestHomeMatchesFuzzball compares the lot.
func TestHomeMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), `: main "unused" pop ;`)
	if err != nil {
		t.Fatal(err)
	}
	script := homeScript
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
