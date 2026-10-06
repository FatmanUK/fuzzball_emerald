package golden

import (
	"context"
	"testing"
)

// mfn_force (mfuns2.c:2753) has an **unblessed path** this server did
// not have. Emerald refused every unblessed {force} with "Permission
// Denied."; upstream refuses only when allow_zombies is off, and with
// it on an unblessed force proceeds and is then subject to six
// refusals of its own — four of which apply to a THING alone.
//
// So a world that allows zombies had a whole mechanism, puppets
// forced from their own descriptions, that could not run here.
//
// Two of the refusals read "Permission denied." with a lower-case d
// where the allow_zombies one has a capital D. That is upstream's
// inconsistency, and the oracle pins it.
//
// The force lock is the part that mattered most: `@/flk` could be set
// with @flock and was shown by examine, but **nothing in this server
// evaluated it** — neither {force} nor @force — so a lock meant
// to say who may force a puppet protected nothing at all. An unset
// lock is false (test_lock_false_default, boolexp.c:906), so the
// XFORCIBLE flag alone is not enough.
const forceMPISource = `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop
  var! out me @ "[" out @ strcat "]" strcat notify
;
: main
  ( --- the refusals before the blessed test ------------------ )
  "{force:nosuchthing,look}" show
  "{force:#0,look}" show
  "{force:me,}" show

  ( --- unblessed, with allow_zombies on: the ladder ---------- )
  ( a puppet with no XFORCIBLE gets that refusal )
  "{force:puppet,look}" show
  ( one that is DARK is refused earlier, before XFORCIBLE )
  "{force:darkpup,look}" show
  ( one whose first word is a player's name )
  "{force:One,look}" show
  ( XFORCIBLE but no force lock, which defaults to false )
  "{force:xpup,look}" show

  ( --- a player target skips the four THING-only refusals and
        lands on XFORCIBLE ---------------------------------- )
  "{force:me,look}" show

  ( --- #1 is a player with no XFORCIBLE, so that refusal
        comes first: the God test sits *after* the unblessed
        block and an unblessed force never reaches it -------- )
  "{force:#1,look}" show
;`

// forceMPIScript builds the puppets. #4 puppet, #5 darkpup, #6 One,
// #7 xpup — #2 is the fixture's program and #3 its exit. It is run
// three times, because two of the six refusals depend on state
// outside the puppet and both come *before* the ones above them in
// the ladder — so toggling each in turn is the only way to see
// them, and it also shows the order.
var forceMPIScript = Script{
	"@tune allow_zombies=yes",
	"@create puppet",
	"@create darkpup",
	"@set darkpup=dark",
	"@create One",
	"@create xpup",
	"@set xpup=xforcible",
	"test",

	// A no-puppets room is ZOMBIE on the *room*, and its refusal
	// outranks the XFORCIBLE one.
	//
	// The puppet has to be **dropped** first: @create puts it in
	// the player's inventory, so its location is the player and
	// `Typeof(loc) == TYPE_ROOM` never holds. Without the drop
	// this step's transcript is identical to the one above it —
	// which is to say it tests nothing, and did until the
	// transcripts were read rather than just compared.
	"drop xpup",
	"@set here=zombie",
	"test",

	// An owner flagged ZOMBIE cannot use puppets at all, and that
	// outranks the room.
	"@set me=zombie",
	"test",
}

// TestMPIForceMatchesFuzzball drives the ladder through both servers.
func TestMPIForceMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), forceMPISource)
	if err != nil {
		t.Fatal(err)
	}
	script := forceMPIScript
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
