package golden

import (
	"context"
	"testing"
	"time"
)

// sweepScript covers do_sweep, the last command in the table that had
// no handler for a reason other than a decision: it needs the
// LISTENER flag, which nothing maintained.
//
// It reports and never changes anything, so it ships ahead of the
// propqueues that give the flag its meaning — and that makes it the
// one honest way to see the flag before any of a world's inert
// `_listen` props start firing.
//
// #4 is bell, #5 puppet, #6 Cellar, #7 the trap exit.
var sweepScript = Script{
	// Nothing to find: a header, the environment header, and the
	// end. The environment header prints even when the walk finds
	// nothing, which looks like a bug and is upstream's loop.
	"@sweep",
	"@sweep here",

	// A listening thing. The flag is set by writing the property
	// and the sweep asks for both, so each half on its own is
	// silent.
	"@create bell",
	"drop bell",
	"@sweep",
	"@propset bell=str:_listen:&Ding.",
	"@sweep",

	// A zombie whose owner is connected is reported as a zombie;
	// one whose owner is asleep is not reported at all unless it
	// also listens. #1 is connected, so this is the awake case.
	"@create puppet",
	"drop puppet",
	"@set puppet=Z",
	"@sweep",

	// Both at once, in upstream's order: zombie first, then
	// listener, then the owner.
	"@propset puppet=str:_listen:&Hello.",
	"@sweep",

	// Deleting the property leaves the flag set — set_property
	// is the only thing that touches it — so the sweep goes
	// quiet without the flag changing.
	"@propset bell=erase:_listen",
	"@sweep",

	// The wizard-only halves count too, and are the point of the
	// command: a mortal cannot set them, so a sweep is the only
	// way to notice one.
	"@propset bell=str:~listen:&Quiet.",
	"@sweep",
	"@propset bell=erase:~listen",
	"@propset bell=str:~olisten:&Quieter.",
	"@sweep",
	"@propset bell=erase:~olisten",

	// A listening room, reported from the environment walk rather
	// than from the contents.
	"@propset here=str:_listen:&The walls have ears.",
	"@sweep",
	"@propset here=erase:_listen",

	// The four trapped commands. page, whisper and say are prefix
	// tests, so an exit named "p" traps page; pose is tried
	// exactly as pose, pos and po, and only the first that
	// matches is reported. The exit is named by dbref throughout,
	// because "p" is also a prefix of "puppet".
	"@dig Cellar",
	"@open p=#6",
	"@sweep",
	"@unlink #7",
	// An unlinked exit traps nothing, because exit_matches_name
	// wants a destination.
	"@sweep",
	"@link #7=#6",
	"@name #7=say;pose;whisper",
	"@sweep",
	"@name #7=po",
	"@sweep",
	"@name #7=pos",
	"@sweep",
	"@name #7=paget",
	"@sweep",

	// A trap on a thing in the room is reported from the contents
	// half, naming the thing rather than the room.
	"@action out=bell=#6",
	"@sweep",

	// Sweeping something that is not here at all, and something
	// that is not a room: upstream does not check the type, so a
	// thing's contents are swept like a room's.
	"@sweep bell",
	"@sweep #6",
	"@sweep nosuchplace",
	"@sweep me",

	// The abbreviation. @sweep needs three characters, and @s
	// reaches nothing.
	"@swe",
	"@sw",
}

// TestSweepMatchesFuzzball checks do_sweep against the C server.
func TestSweepMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, sweepScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, sweepScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range sweepScript {
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
