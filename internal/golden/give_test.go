package golden

import (
	"context"
	"testing"
	"time"
)

// giveScript covers give and @newpassword, and read as a spelling of
// look.
//
// give is four commands in one: to a player, from a wizard with a
// negative amount, to a *thing* — where it sets the value outright
// and says so differently — and refused.
var giveScript = Script{
	"@create widget",

	// The oracle's #1 is a wizard, so all four paths are
	// reachable from one seat.
	"give me=5",
	"score",
	"give me=-3",
	"score",
	"give widget=10",
	"examine widget",
	"give widget=-4",

	// The refusals.
	"give me=0",
	"give me=notanumber",
	"give nosuchthing=1",
	"give here=1",
	"give test.muf=1",

	// read is another spelling of look, dispatched to do_look_at.
	"read",
	"read widget",
	"read nosuchthing",

	// @newpassword: God may change a mortal's, and nobody may
	// change God's but God.
	"@newpassword One=hunter2",
	"@newpassword nosuchplayer=x",
	"@newpassword One=",

	// The abbreviations. "gi" is give and "g" is goto.
	"gi me=1",
	"rea widget",
}

// TestGiveMatchesFuzzball checks give, read and @newpassword against
// the C server.
func TestGiveMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, giveScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, giveScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range giveScript {
		var want, got string
		if i < len(oracle) {
			want = oracle[i]
		}
		if i < len(emerald) {
			got = emerald[i]
		}
		if diffs := Compare(maskVariable(want),
			maskVariable(got)); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}
