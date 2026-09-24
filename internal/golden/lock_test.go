package golden

import (
	"context"
	"testing"
	"time"
)

// lockCmdScript exercises the @lock family of commands — set,
// report, clear, aliases, a bad key, and an exit whose own @lock
// actually gates traversal. The exit links to the fixture's own room
// (#0), so the player never leaves it and every command stays
// addressable without a dbref only known after a @create.
var lockCmdScript = Script{
	// No object defaults to the caller; no "=" reports instead of
	// setting.
	"@lock",
	"@lock me=me",
	"@lock",
	"@lock me=",
	"@lock",

	// @force_lock/@chown_lock are alternate spellings, not
	// abbreviations.
	"@force_lock me=me",
	"@flock me",
	"@chown_lock me=me",
	"@chlock me",

	"@conlock me=me",
	"@linklock me=me",
	"@ownlock me=me",
	"@readlock me=me",

	// A bad key reports the parse failure rather than setting
	// anything.
	"@lock me=nosuchthingatall",

	// An exit linking back to the room it's in: lock it out, try
	// to pass, unlock it, try again.
	"@open down;d=#0",
	"@lock down",
	"@lock down=#-1",
	"down",
	"@lock down=#1",
	"down",

	// @flock/@ownlock/@readlock refuse to run from inside a
	// @force; @lock does not.
	"@force me=@flock down=me",
	"@force me=@lock down=me",

	// @unlock clears the ordinary lock and only that one, with a
	// message of its own.
	"@unlock",
	"@lock me=me",
	"@readlock me=me",
	"@unlock me",
	"@lock me",
	"@readlock me",
	"@unlock nosuchthing",
	"@lock nosuchthing=me",
	"@readlock nosuchthing",
}

// TestLockCommandsMatchFuzzball checks the @lock family against the C
// server.
func TestLockCommandsMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, lockCmdScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, lockCmdScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range lockCmdScript {
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
