package golden

import (
	"context"
	"testing"
)

// The seven `@*lock` commands against `set_standard_lock`
// (`property.c:2394`), which the MPI resolver sweep flagged for a
// matcher difference: `@readlock Bob=me` was reported as setting a
// lock here and refusing there.
//
// It no longer does, and the sweep's reading of *why* was wrong:
// `match_everything` **does** include `match_player`, for a wizard
// (`match.c:712`), so the question was never whether the stage is
// there. A bare "Bob" resolves because a freshly created player
// stands in `player_start` and is therefore a **neighbour**; the star
// only matters for somebody who is somewhere else. Both rungs are
// below.
//
// All seven commands share one function upstream, so the ladder walks
// the shape once and then checks that each verb reaches it with its
// own label.

var lockFamScript = Script{
	"@tune penny_rate=0",
	"@pcreate Bob=secret",
	"@create widget",
	"drop widget",

	// A bare verb reports the lock **on the player**, because an
	// empty object name defaults to them rather than being a
	// usage error.
	"@lock",
	"@flock",
	"@chlock",
	"@conlock",
	"@linklock",
	"@ownlock",
	"@readlock",

	// The full spellings are the same commands.
	"@force_lock",
	"@chown_lock",

	// A set is recognised by the '=' being in the **line**, so an
	// empty value clears rather than reporting.
	"@lock widget=me",
	"@lock widget",
	"@lock widget=",
	"@lock widget",

	// The matcher is match_controlled: match_everything plus a
	// control test. A player needs the leading `*` even for a
	// wizard, so a bare name matches nothing and says so -- which
	// is a different message from the control refusal.
	"@readlock Bob=me",
	"@readlock *Bob=me",
	"@readlock nosuchthing=me",

	// ...and a player who is **not** a neighbour, where the star
	// is the only thing that finds them.
	"@dig Elsewhere",
	"@pcreate Carol=secret",
	"@teleport *Carol=#6",
	"@readlock Carol=me",
	"@readlock *Carol=me",

	// A registration resolves, which match_everything includes
	// and a hand-built chain would not.
	"@register widget=wid",
	"@lock $wid=me",
	"@lock $wid",

	// A lock is **stored** unparsed and displayed with names, so
	// reading one back re-parses it.
	"@lock widget=me&!*Bob",
	"@lock widget",
	"examine widget=@**",

	// And a lock nobody can parse is refused with the parser's
	// own message, before the caller's.
	"@lock widget=nosuchkey",
	"@lock widget=me|",

	// `@unlock` is a different function (`set.c:1297`) and clears
	// only the plain lock, whatever else is set -- and it reports
	// "Unlocked." where clearing with an empty value says "Lock
	// cleared."
	"@readlock widget=me",
	"@unlock widget",
	"@lock widget",
	"@readlock widget",
	"@unlock nosuchthing",
}

// TestLockFamilyMatchesFuzzball compares the ladder.
func TestLockFamilyMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}
	script := lockFamScript
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
