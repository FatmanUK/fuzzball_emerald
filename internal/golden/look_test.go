package golden

import (
	"context"
	"testing"
	"time"
)

// lookScript pins the shape of do_look_at, which differs by type in
// ways that are easy to get wrong and were: a name line belongs to
// look_room alone, and each type heads its contents listing
// differently — "Contents:" for a room, "Carrying:" for a player,
// "Contains:" for a thing.
//
// The fixture is #0 the room, #1 the wizard, #2 the program and #3
// its exit, so every dbref below is known before anything runs.
var lookScript = Script{
	// A room: the name, the description, then the contents.
	"look",
	"look here",
	"look #0",

	// A thing with no description, then with one. Neither shows a
	// name line.
	//
	// The description goes on with @set rather than @describe so
	// this case depends on nothing but the property, which is
	// what look reads.
	"@create widget",
	"look widget",
	"@set widget=_/de:A small widget.",
	"look widget",

	// A player: the description and "Carrying:", no name line. #1
	// is carrying the widget, having just created it.
	"look me",
	"@set me=_/de:An ordinary wizard.",
	"look me",

	// HAVEN keeps a thing's contents to itself. There is nothing
	// inside the widget to hide yet — no implemented command
	// puts a thing inside a thing, so the "Contains:" heading is
	// a unit test in internal/game — but the flag must not
	// change anything else either.
	"@set widget=H",
	"look widget",
	"@set widget=!H",
	"look widget",

	// An exit and a program are neither rooms nor things.
	"look test",
	"look #2",
	"look #3",

	// A dark thing is not listed to somebody who does not control
	// it — but #1 controls everything here, so this only checks
	// that being dark does not hide it from its owner.
	"@set widget=D",
	"look",
}

// TestLookMatchesFuzzball checks look against the C server.
func TestLookMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, lookScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, lookScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range lookScript {
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
