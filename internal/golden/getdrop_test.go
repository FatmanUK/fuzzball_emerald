package golden

import (
	"context"
	"testing"
	"time"
)

// getdropScript covers do_get and do_drop, which Emerald had as
// twenty lines each against upstream's ninety and hundred. Most of
// what is missing is the second argument, so most of this script has
// one.
//
// "take" is another spelling of get, and "put", "throw" and "hand" of
// drop; upstream's own comment says the three differ only in their
// help files, so they are exercised here rather than assumed.
var getdropScript = Script{
	"@create widget",
	"@create satchel",
	"@create pebble",

	// The one-argument forms, and picking up what you already
	// have.
	"drop widget",
	"get widget",
	"get widget",
	"take widget",
	"drop widget",
	"take widget",

	// Two arguments: the *first* names the container.
	"drop pebble=satchel",
	"get satchel=pebble",
	"put pebble=satchel",
	"get satchel=pebble",

	// A container defaults to locked against everyone, so
	// unlocking it is what makes the two-argument form work at
	// all — and the two failures are worded differently.
	"@conlock satchel=me",
	"put pebble=satchel",
	"get satchel=pebble",
	"@conlock satchel=",

	// Dropping into a room, with and without the messages. The
	// thing's own @drop replaces "Dropped."; the room's is extra.
	"drop widget",
	"@drop widget=It lands with a clink.",
	"get widget",
	"drop widget",
	"@drop here=The floor creaks.",
	"get widget",
	"drop widget",
	"@drop widget=",
	"@drop here=",

	// The "o" halves. The thing's @odrop replaces "X drops Y."
	// and is prefixed with the *player's* name; the room's is
	// extra and is prefixed with the *thing's*, which reads as
	// the object doing something on arrival.
	"get widget",
	"@odrop widget=lets the widget fall.",
	"drop widget",
	"get widget",
	"@odrop here=settles into the dust.",
	"drop widget",
	"@odrop widget=",
	"@odrop here=",

	// A room whose drop-to is not STICKY takes a dropped thing
	// straight through it, so the thing is not here afterwards.
	"@dig Cellar",
	"@link here=Cellar",
	"get widget",
	"drop widget",
	"look",
	"@contents #5",
	"@unlink here",

	// A STICKY thing goes home instead of landing here.
	"get widget",
	"@set widget=S",
	"drop widget",
	"look",
	"@set widget=!S",

	// The refusals, each in its own words.
	"drop nosuchthing",
	"get nosuchthing",
	"drop widget=nosuchthing",
	"get widget=nosuchthing",
	"get me",
	"drop me",
	"get here",
	"put widget=me",

	// Handing something to a player names both sides. #1 is the
	// only player, so this is handing to yourself.
	"get widget",
	"hand widget=me",

	// leave and disembark: both are do_leave. Each refusal is its
	// own sentence — a room, a thing that is not a vehicle, and
	// a vehicle whose outside is not somewhere you can stand.
	"leave",
	"disembark",
	//
	// Actually boarding a vehicle needs an exit *inside* it —
	// trigger() requires dest == LOCATION(exit) — which no
	// implemented command can make, since @action still aliases
	// @open. The other three refusals are unit tests.

	// The abbreviations. "g" is not get — "goto" comes first
	// — and "t" is not take.
	"g",
	"ge widget",
	"t",
	"ta widget",
	"dr widget",
	"thr widget",
	"le",
	"dis",
}

// TestGetDropMatchesFuzzball checks containment against the C server.
func TestGetDropMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, getdropScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, getdropScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range getdropScript {
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
