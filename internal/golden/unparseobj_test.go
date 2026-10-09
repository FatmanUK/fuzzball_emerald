package golden

import (
	"context"
	"testing"
)

// `unparse_object` (`db.c:1428`) decides whether a name is followed
// by its dbref and flags, and **three of its four clauses were
// missing**. What stood in their place was an invented
// wizard-or-owner test, so a mortal saw a bare name for everything
// they did not own — where upstream shows the dbref of anything
// marked LINK_OK or CHOWN_OK, anything whose link they control, and
// anything their own link lock admits.
//
// The real condition has no wizardry of its own. Wizards and owners
// arrive through `controls`, three levels down inside `can_see_flags`
// → `can_teleport_to` → `controls`.
//
// The oracle drives `#1`, who controls everything, so the case has
// the wizard **quell itself** and gives Bob the objects. `look`'s
// contents listing is the read-back: every line of it is one
// `unparse_object` call.
const unparseObjSource = `: main 1 pop ;`

var unparseObjScript = Script{
	"@pcreate Bob=secret", // #4

	// The yard is Bob's, so a thing created in it is *homed* in
	// Bob's room — which is what stops `controls_link` passing
	// for everything by accident. "Elsewhere" stays #1's, and is
	// where the one thing with a controlled link is homed.
	"@dig Yard",      // #5
	"@dig Elsewhere", // #6
	"@teleport me=#5",
	// "here", because @chown's matcher cannot name the room the
	// player is standing in by its own name -- on both servers.
	"@chown here=Bob",

	// One object per clause.
	"@create plain",
	"drop plain", // #7
	"@create linkok",
	"@set linkok=link_ok",
	"drop linkok", // #8
	"@create chownok",
	"@set chownok=chown_ok",
	"drop chownok", // #9

	// ABODE counts for anything that is *not* a thing, so on a
	// thing it buys nothing.
	"@create abodeit",
	"@set abodeit=abode",
	"drop abodeit", // #10

	// LINK_OK with a link lock that admits only Bob: the lock is
	// tested *before* the flag is looked at, so this reverts to a
	// bare name.
	"@create locked",
	"@set locked=link_ok",
	// "*Bob", with the star: `match_player` (`match.c:252`)
	// matches only a name beginning with LOOKUP_TOKEN. This
	// server's own Player stage treats the star as optional, so a
	// bare "Bob" set the lock here and failed upstream --
	// recorded, and its own commit.
	"@linklock locked=*Bob",
	"drop locked", // #11

	// A thing homed in a room #1 owns, which is what
	// `controls_link` asks about for a THING.
	"@create homed",
	"@link homed=#6",
	"drop homed", // #12

	// And one of #1's own, as the control.
	"@create mine",
	"drop mine", // #13

	// Bob stands in the yard with his home in a room #1 owns and
	// CHOWN_OK set. Every one of these three names him with a
	// **star**: `match_player` wants LOOKUP_TOKEN, and a bare
	// "Bob" answers "I don't understand 'Bob'." upstream while
	// resolving here. so both of the clauses that are gated on
	// the target *not* being a player have something to refuse.
	// On a player CHOWN_OK means "sees colour", which is the flag
	// reuse that makes the guard matter.
	"@link *Bob=#6",
	"@set *Bob=chown_ok",
	"@teleport *Bob=#5",

	"@chown plain=Bob",
	"@chown linkok=Bob",
	"@chown chownok=Bob",
	"@chown abodeit=Bob",
	"@chown locked=Bob",
	"@chown homed=Bob",

	// A wizard sees everything, so this is the baseline.
	"look",

	// Quelled, the four clauses decide.
	"@set me=quell",
	"look",

	// A **STICKY viewer** sees only names, whatever else is true.
	// On a player STICKY means "your things go home when
	// dropped"; that it doubles as a display switch reads like an
	// accident of flag reuse, and is upstream's.
	"@set me=sticky",
	"look",
	"@set me=!sticky",

	"@set me=!quell",
	"look",

	// The test is made on whoever **owns** the viewer, which is
	// upstream's first line and commented "Handle ZOMBIE case".
	// Only the STICKY clause can show it: every other route goes
	// through `controls`, which substitutes the owner for itself.
	// So a STICKY puppet owned by a wizard who is not STICKY
	// still sees dbrefs.
	"@create z",
	"@set z=zombie",
	"@set z=sticky",
	"@force z=look",
}

// TestUnparseObjectMatchesFuzzball compares the ladder.
func TestUnparseObjectMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), unparseObjSource)
	if err != nil {
		t.Fatal(err)
	}
	script := unparseObjScript
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
