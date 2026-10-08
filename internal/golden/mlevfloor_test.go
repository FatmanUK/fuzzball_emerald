package golden

import (
	"context"
	"testing"
)

// Nine of the thirty-four entries in internal/muf/mlev_gen.go were
// floors the C does not have, and one real floor was missing
// entirely. This runs the same probes at mucker 1 and at mucker 3,
// which is the comparison nothing in this suite had ever made: a
// fixture compiles at 3, so no golden case had run a program below
// it, and that is precisely where a false floor shows.
//
// Two shapes of mistake are covered.
//
// The five notify primitives recorded a floor of 2 taken from
//
//	if (tp_force_mlev1_name_notify && mlev < 2
//	    && player != target)
//	    prefix_message(buf, msg, NAME(player), BUFFER_LEN, 1);
//
// where there is **no abort_interp on that path at all** -- the
// branch picks a message prefix. So a mucker-1 program could not
// produce output, which is the most basic thing a MUF program does.
//
// ADDPENNIES, MOVEPENNIES and SETOWN recorded floors taken from an
// "if (mlev < N)" that opens a block of extra mortal-only
// restrictions rather than refusing. Their real gates are three @tune
// parameters that this server read nowhere -- so the floors were both
// too strict and, for PENNIES, absent.
//
// RECYCLE is the other direction, and the only "wrongly allowed" one:
// p_db.c:2200 is "(mlev < 3) || ((mlev < 4) && !permissions(...))",
// two independent disjuncts, and the extractor's skip regex saw the
// permission test and discarded the whole condition. RECYCLE was in
// no table and had no level check anywhere, so a mucker-1 program
// could recycle objects upstream refuses it.
//
// Note what reports results: ts notifies the running player, and
// notifying *yourself* is exempt from the name prefix, so every
// reported line reads the same at both levels. Without that exemption
// this case would be measuring the prefix rather than the refusals.
//
// No parentheses appear inside the MUF comments below: ")" closes a
// "( ... )" comment wherever it appears, and a comment mentioning
// "permissions()" ends two characters early and takes the rest of the
// line with it.
const mlevFloorSource = `: t[ x -- ] me @ x @ intostr notify ;
: ts[ s -- ] me @ s @ notify ;
: main
  ( NOTIFY at all. Notifying yourself is exempt from the prefix, so
    this line is identical at both levels -- what it proves is that
    the primitive runs, where the false floor aborted it. )
  0 try "notify ran" ts catch ts endcatch

  ( NOTIFY_EXCLUDE has no self-exemption, and this room holds only
    the one player, so what comes back is the prefixed form at
    mucker 1 and the plain one above it. )
  0 try me @ location 0 "excluded" notify_exclude catch ts endcatch

  ( ARRAY_NOTIFY likewise: strings array first, dbrefs second. )
  0 try { "arrayed" }list { me @ }list array_notify catch ts endcatch

  ( PENNIES is gated by tp_pennies_muf_mlev, which defaults to 1 and
    so permits both runs. What it proves is that the parameter is
    consulted at all, since nothing here read it. )
  0 try me @ pennies t catch ts endcatch

  ( ADDPENNIES is gated by tp_addpennies_muf_mlev, default 2:
    refused at mucker 1, allowed at 3. Below mucker 4 the range
    tests apply as well, so a huge amount is still refused at 3. )
  0 try me @ 1 addpennies "addpennies ran" ts catch ts endcatch
  0 try me @ 999999999 addpennies catch ts endcatch

  ( Level 4 is needed to give pennies to a *thing*, which is a type
    test rather than a floor. )
  0 try #4 1 addpennies catch ts endcatch

  ( MOVEPENNIES is gated by tp_movepennies_muf_mlev, default 2. Its
    object arguments are tested against TYPE_PLAYER alone, despite
    every one of its messages saying "player or thing". )
  0 try me @ me @ 1 movepennies "movepennies ran" ts catch ts endcatch
  0 try #4 me @ 1 movepennies catch ts endcatch

  ( SETOWN's "mlev < 4" opens four mortal-only refusals, each with
    its own sentence, rather than refusing outright -- so a mortal
    program may chown a CHOWN_OK object to its own owner, which the
    false floor forbade. #5 is a second player, so the first
    refusal is reachable. )
  0 try #4 #5 setown catch ts endcatch
  0 try me @ me @ setown catch ts endcatch
  0 try #4 me @ setown "setown ran" ts catch ts endcatch

  ( SETOWN's room rule needs a room the player is not standing in,
    and the hall at #7 is marked CHOWN_OK so that the flag test is
    not what refuses first. )
  0 try #7 me @ setown catch ts endcatch

  ( RECYCLE's floor of 3 is real and was absent. The gem at #6
    belongs to Bob, so the second disjunct -- mucker below 4 and no
    permissions on the object -- is what refuses it at mucker 3,
    where the floor alone would let it through. )
  0 try #6 recycle catch ts endcatch

  ( And one the player does own, which is last because above
    mucker 2 it succeeds and takes #4 with it. )
  0 try #4 recycle "recycle ran" ts catch ts endcatch
;`

// mlevFloorScript makes the widget at #4 and a second player at #5,
// then runs the program at each level. #2 is the program and #3 its
// exit.
//
// The widget is CHOWN_OK so that SETOWN's flag test is not what
// refuses, and dropped so it is in the room rather than in the
// player's inventory -- @create leaves it carried, which is how three
// cases last tranche came to compare byte-identical while testing
// nothing.
var mlevFloorScript = Script{
	"@create widget",
	"drop widget",
	"@set widget=chown_ok",
	"@pcreate Bob=secret",

	// A gem Bob owns, so RECYCLE's permission clause is
	// reachable, and a hall the player is not standing in for
	// SETOWN's room rule. Both are dropped or detached rather
	// than carried.
	"@create gem",
	"drop gem",
	"@chown gem=Bob",
	"@dig hall",
	"@set hall=chown_ok",

	// Mucker 1: below every floor in question. The reply is
	// "Mucker level set." and setting any mucker bit clears both
	// first, so this is an assignment rather than an or.
	"@set test.muf=1",
	"test",

	// And back to the fixture's own level, above the three
	// tunable floors and at RECYCLE's.
	"@set test.muf=3",
	"test",
}

// TestMuckerFloorsMatchFuzzball compares both runs.
func TestMuckerFloorsMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), mlevFloorSource)
	if err != nil {
		t.Fatal(err)
	}
	script := mlevFloorScript
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
