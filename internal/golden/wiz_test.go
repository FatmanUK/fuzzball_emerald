package golden

import (
	"context"
	"testing"
	"time"
)

// wizScript exercises the wizard commands against both servers.
//
// It is driven without markers because @force runs a command as
// someone else, and a marker pose sent afterwards would be attributed
// to whoever the force left holding the line.
var wizScript = Script{
	// @stats, for the whole database and for one player.
	"@stats",
	"@stats One",
	"@stats Nobody",

	// @boot.
	"@boot Nobody",
	"@boot One",

	// @force: the arguments it refuses, and one that works.
	"@force",
	"@force nosuchthing=look",
	"@force One",
	"@force One=:waves.",
	"@force One=@stats",

	// A puppet: forcing a thing that is not Xforcible, then one
	// that is.
	"@create puppet",
	"@force puppet=:wiggles.",
	"@set puppet=X",
	"@force puppet=:wiggles.",

	// @set: flags by letter and by name, mucker levels, and the
	// two names it refuses even though the flag table resolves
	// them. What each one did shows up in the flags the next
	// message unparses.
	"@set puppet=V",
	"@set puppet=!vehicle",
	"@set puppet=nosuchflag",
	"@set puppet=T",
	"@set puppet=N",
	"@set puppet=M4",
	"@set puppet=M2",
	"@set puppet=M1",
	"@set puppet=!mucker",

	// @pcreate, then @toad for real: the victim's things change
	// hands and the victim stops being a player.
	"@pcreate Victim=hunter2",
	"@pcreate Victim=hunter2",
	"@pcreate =hunter2",
	"@pcreate Nobody2=",
	"@stats Victim",
	"@toad Nobody=Victim",
	"@toad Victim",
	"@stats Victim",
	"@toad Victim",

	// @toad refuses the cases that would damage the database.
	"@toad Nobody",
	"@toad One",
	"@toad One=Nobody",
}

// TestWizardCommandsMatchFuzzball checks @stats, @boot, @force and
// @toad against the C server.
func TestWizardCommandsMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, wizScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, wizScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range wizScript {
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
