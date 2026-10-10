package golden

import (
	"context"
	"testing"
)

// Five of the eleven function-fidelity gaps the MPI resolver sweep
// recorded and did not fix: `{tell}`'s and `{otell}`'s prefixing
// rules, `isancestor`, `{awake}`'s descriptor count and ZOMBIE
// redirect, and `{contents}`'s per-item filter.
//
// `{tell}`'s **"> " marker** and its name prefix are both unreachable
// from one seat: each needs the target to be neither the message's
// owner nor the triggering player, and the only connected object here
// is that player. `{otell}` is reachable, because its third argument
// can exclude nobody -- so the speaker hears their own broadcast.

var mpiTellScript = Script{
	"@tune penny_rate=0",
	"@pcreate Bob=secret",

	// {tell} returns its **message** rather than the empty
	// string, so a description that calls it shows the text as
	// well as sending it.
	"@create widget",
	"@describe widget={tell:hello}[end]",
	"look widget",

	// {otell} excluding nobody, from a **room**: the carrying
	// object is the room the message goes to, so it is "related"
	// and "placed" and the speaker's name is left off.
	"@describe here={otell:a message,here,#-1}[end]",
	"look",

	// ...and from a **thing**, which is neither, so the name is
	// forced on. That is what stops something in your pocket
	// writing a line that reads as somebody speaking.
	"@describe widget={otell:a message,here,#-1}[end]",
	"look widget",

	// The name is omitted when the message already begins with
	// it, and the space after it is omitted before a pose
	// separator -- an apostrophe or whitespace.
	"@describe widget={otell:One waves,here,#-1}[end]",
	"look widget",
	"@describe widget={otell:'s hat,here,#-1}[end]",
	"look widget",

	// And {otell} sends only the **first** line of a multi-line
	// message, which is a bug in the C reproduced rather than
	// fixed: mfn_otell writes its terminator without stepping
	// past it.
	"@describe widget={otell:first{nl}second,here,#-1}[end]",
	"look widget",
	// {tell} does not have it -- the same loop with the
	// increment.
	"@describe widget={tell:first{nl}second}[end]",
	"look widget",

	// {awake} answers a descriptor **count**, and redirects a
	// ZOMBIE thing to its owner -- so asking whether a puppet is
	// awake asks whether the person behind it is.
	"@describe widget={awake:me}/{awake:widget}[end]",
	"look widget",
	"@set widget=zombie",
	"look widget",
	"@describe widget={awake:here}/{awake:nosuchthing}[end]",
	"look widget",
	"@set widget=!zombie",

	// {contents} filters each item by DARK and by control
	// (`mfuns2.c:401`), which this did by type alone -- so a DARK
	// thing in somebody else's room was listed. The probe has to
	// be owned by somebody who does **not** control the room, or
	// every item passes on control alone.
	"@create probe",
	"@describe probe=[{contents:here}][end]",
	"@create pebble",
	"drop pebble",
	"@create shadow",
	"drop shadow",
	"@set shadow=dark",
	"look probe",
	"@chown probe=Bob",
	"look probe",

	// ...and the per-item control escape, which needs the
	// **item** controlled where the container is not: a DARK
	// thing of Bob's is listed to a probe of Bob's, in a room
	// neither of them controls.
	"@create bobthing",
	"drop bobthing",
	"@chown bobthing=Bob",
	"@set bobthing=dark",
	"look probe",
}

// TestMpiTellMatchesFuzzball compares the ladder.
func TestMpiTellMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}
	script := mpiTellScript
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
