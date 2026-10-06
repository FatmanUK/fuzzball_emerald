package golden

import (
	"context"
	"testing"
)

// Boarding a vehicle was unreachable until @action landed, and then
// untested. trigger() boards a thing when the exit is **inside** it
// and it is a VEHICLE — `dest == LOCATION(exit)` (move.c:482) —
// so making one needs @action, which attaches an exit to a named
// object. @open always attaches to the room, so while @action was its
// alias no boarding exit could exist at all.
//
// This also drives `leave` from the inside, which was unit-tested
// rather than compared, and the vehicle guards in trigger(): a
// VEHICLE may not enter another VEHICLE, and a non-wizard THING may
// not enter a ZOMBIE room.
var boardScript = Script{
	// The vehicle, and an exit inside it pointing back at it.
	"@create bus",
	"@set bus=vehicle",
	// The bus has to be **dropped** first: @create leaves it in
	// the player's inventory, and entering something you are
	// carrying is a loop -- "That would cause a paradox." Both
	// servers say so, so without this the case compares clean and
	// boards nothing.
	"drop bus",
	"@action board=bus",
	"@link board=bus",
	"examine board",

	// Boarding, which puts the player inside the thing.
	"board",
	"look",
	"@contents bus",

	// leave, from the inside.
	"leave",
	"look",

	// trigger()'s other two guards are **not** reached here, and
	// cannot be from a player: "a VEHICLE may not enter a
	// VEHICLE" and "a non-wizard THING may not enter a ZOMBIE
	// room" both test the object being *moved*, and a player is
	// neither. They need a thing traversing an exit -- an exit
	// that fetches one into a vehicle -- which is its own case.
	//
	// Naming an object by name from *inside* it does not work
	// either: "@contents bus" and "@teleport bus=tram" both
	// answer "I don't understand 'bus'." on both servers, so they
	// measure the matcher rather than boarding and are left out.

	// leave with nowhere to go: the three refusals, from the
	// outside this time.
	"leave",
}

// TestBoardMatchesFuzzball compares the lot.
func TestBoardMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), `: main "unused" pop ;`)
	if err != nil {
		t.Fatal(err)
	}
	script := boardScript
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
		if diffs := Compare(maskVariable(want),
			maskVariable(got)); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}
