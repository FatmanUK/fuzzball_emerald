package golden

import (
	"context"
	"testing"
	"time"
)

// The propqueues: tranche three, and the only work in the whole port
// that changes how an existing world *behaves* rather than what it
// says. A property named `_arrive`, `_depart`, `_connect`,
// `_disconnect`, `_lookq` or one of their "o" halves has sat inert in
// every imported database; now it runs.
//
// The MPI form is what a golden case can see. A program form needs
// the editor, which holds the input line and so cannot use the marker
// — and the two share every line of resolution, so the MPI half
// pins the mechanism and unit tests cover the program half.
//
// #4 is Cellar, #5 the exit into it, #6 widget.
var propqScript = Script{
	"@dig Cellar",
	"@open in=#4",
	"@create widget",

	// _lookq fires after everything else a look does — after
	// the contents, so its output is the last line.
	"look",
	"@propset here=str:_lookq:&You notice the dust.",
	"look",
	// Its argument is the dbref of what was looked at, written
	// "#123", where every other queue passes a word.
	"@propset here=str:_lookq:&Looked at {&cmd}.",
	"look",
	"@propset here=str:_lookq:&Where is {&arg}?",
	"look",

	// It fires for a thing and a player too, not only a room, and
	// the environment walk means a hook on the room serves
	// everything in it.
	"drop widget",
	"look widget",
	"look me",
	"@propset here=erase:_lookq",

	// On the thing itself, which the walk reaches first.
	"@propset widget=str:_lookq:&It hums.",
	"look widget",
	"look",
	"@propset widget=erase:_lookq",

	// A propdir runs every child, so a world can file several
	// hooks under one name.
	"@propset here=str:_lookq/one:&First.",
	"@propset here=str:_lookq/two:&Second.",
	"look",
	"@propset here=erase:_lookq/one",
	"@propset here=erase:_lookq/two",

	// _depart and _arrive, with their "o" halves. The non-"o"
	// queue answers the mover privately; the "o" queue is
	// broadcast to the room prefixed ">> ", which with one player
	// present means nobody hears it.
	"@propset here=str:_depart:&You step away.",
	"@propset #4=str:_arrive:&You step in.",
	"in",
	"@teleport #0",
	"@propset here=str:_odepart:&left quietly.",
	"@propset #4=str:_oarrive:&arrived quietly.",
	"in",
	"@teleport #0",

	// Their arguments are fixed words rather than a dbref.
	"@propset here=str:_depart:&Depart is {&cmd}.",
	"@propset #4=str:_arrive:&Arrive is {&cmd}.",
	"in",
	"@teleport #0",
	"@propset here=erase:_depart",
	"@propset here=erase:_odepart",
	"@propset #4=erase:_arrive",
	"@propset #4=erase:_oarrive",

	// The shared recursion limit is a unit test rather than a
	// case here: reaching it from MPI needs {force}, whose own
	// refusals Emerald has only half of — see
	// docs/upstream-coverage.md.

	// A value that names nothing runnable does nothing at all,
	// silently — which is why a typo in an _arrive is so hard
	// to notice.
	"@propset here=str:_lookq:nonsense",
	"look",
	"@propset here=str:_lookq:#9999",
	"look",
	"@propset here=str:_lookq:$nosuchprogram",
	"look",
	"@propset here=erase:_lookq",

	// And a non-string type is read as a dbref rather than as
	// text, so an integer property is a program reference.
	"@propset here=int:_lookq:9999",
	"look",
	"@propset here=erase:_lookq",
}

// TestPropqueuesMatchFuzzball checks propqueue and envpropqueue
// against the C server.
func TestPropqueuesMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, propqScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, propqScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range propqScript {
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
