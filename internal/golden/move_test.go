package golden

import (
	"context"
	"testing"
	"time"
)

// moveScript covers enter_room, which Emerald used to approximate
// with an unconditional "has left."/"has arrived." pair.
//
// The fixture is one connection, so the room notifications are only
// visible when the *mover* is the one reading — which they are not.
// What is comparable from a single seat is therefore the order and
// the content of what the mover sees: the exit's own messages, the
// autolook, and what a redirected or self-looping move prints
// instead.
//
// Nothing here uses @tune, though three of enter_room's conditions
// are parameters. @tune's own reply diverges — upstream says
// "Parameter set." and then echoes the parameter, where Emerald names
// it — and do_tune is not this step's work, so quiet_moves,
// autolook_cmd and penny_rate are unit tests instead. penny_rate
// would need turning off here anyway: it is a coin flip on every
// move, and two servers cannot agree about one.
var moveScript = Script{
	// Two rooms and a way between them. #4 is the Workshop, since
	// the fixture ends at #3.
	"@dig Workshop",
	"@open north;n=#4",
	"@open south;s=#0",

	// A plain move: the exit's messages, then the room.
	"@succ north=You climb the stair.",
	"@drop north=The workshop smells of oil.",
	"north",
	"look",
	"south",

	// A room with a description, so the autolook is comparable.
	"@describe #0=A bare room.",
	"north",
	"south",

	// An exit leading where the player already is prints nothing
	// about moving, because enter_room's self-loop test skips the
	// whole block — but the autolook still happens.
	"@open nowhere=#0",
	"@succ nowhere=You go nowhere in particular.",
	"nowhere",

	// HOME resolution, and the exit that leads to it.
	"@open bed=home",
	"bed",

	// A DARK exit silences the room's half of the move, which
	// from one seat shows up as the mover's own output being
	// unchanged.
	"@set north=D",
	"north",
	"south",
	"@set north=!D",

	// An unlinked exit is not special-cased: could_doit's first
	// check is the destination count, so it gets the same default
	// every other failure gets.
	"@open blocked",
	"blocked",
	"@fail blocked=The way is barred.",
	"blocked",

	// A drop-to that is STICKY sweeps the room when the last
	// player leaves. #0 is where the player returns to, so the
	// sweep is checked from the Workshop.
	"@create crate",
	"north",
	"drop crate",
	"@contents here",
	"@link here=#0",
	"@set here=S",
	"south",
	"@contents #0",
	"north",
	"@contents here",
	"south",
}

// TestMoveMatchesFuzzball checks enter_room against the C server.
func TestMoveMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, moveScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, moveScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range moveScript {
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
