package golden

import (
	"context"
	"testing"
)

// `internal/muf` carried a **second** `controls`, parallel to
// `World.Controls` and thinner than it, and MUF's `CONTROLS` and
// `CHECKREMOTE` both used that one. Two differences were observable,
// and both are upstream's:
//
//   - **the wizard test reads the owner, not the object.**
//     `controls` (`db.c:1822`) opens `who = OWNER(who)` and then asks
//     `Wizard(who)`; the local copy asked `Wizard(who)` directly. So
//     a *thing* carrying a WIZARD bit controlled everything while a
//     wizard's ordinary thing controlled nothing — exactly inverted,
//     because `CONTROLS` takes its asker off the stack and a thing
//     can be named there.
//   - **`strict_god_priv` was missing**, so a non-God wizard
//     controlled God's objects through MUF where `@`-commands refuse
//     them.
//
// `muf.Host.Controls` already delegated to the right one; nothing
// called it. `prim_controls` (`p_db.c:1120`) also opens with a
// `CHECKREMOTE` that this server's CONTROLS did not have.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const controlsPrimSource = `: ts[ s -- ] me @ s @ notify ;
: ask[ a b -- ] a @ b @ controls if "yes" else "no" then ts ;
: main
  ( A thing #1 owns, against a thing Bob owns. The asker is the
    *thing*, which is where the two implementations part: the old
    one read the thing's own flags. )
  "widget vs gem, asker is widget: " ts
  #4 #5 ask

  ( A WIZARD-flagged thing owned by a *mortal*, which is where the
    two implementations give opposite answers: upstream asks Bob and
    says no, the old copy asked the sceptre's own bit and said yes.
    This is the probe that discriminates. )
  "Bob's wizard-flagged sceptre vs widget: " ts
  #6 #4 ask

  ( A non-God wizard against God's own object, which is
    strict_god_priv. The old copy had no such clause. )
  "Deputy vs #1's widget: " ts
  #7 #4 ask

  ( And the controls that should hold: Bob owns the gem. )
  "Bob vs gem: " ts
  #8 #5 ask
;`

// controlsPrimScript builds #4 widget, #5 gem (Bob's), #6 a
// wizard-flagged thing, #7 Deputy and #8 Bob — #2 is test.muf and
// #3 its exit.
var controlsPrimScript = Script{
	"@create widget",     // #4
	"@create gem",        // #5
	"@create sceptre",    // #6
	"@pcreate Deputy=pw", // #7
	"@pcreate Bob=pw",    // #8
	"@set Deputy=W",
	"@chown gem=Bob",
	"@set sceptre=W",
	"@chown sceptre=Bob",
	"examine sceptre",
	"test",

	// And from inside the game, so the command path and the
	// primitive agree about the same four questions.
	"examine gem",
}

// TestControlsPrimMatchesFuzzball compares the four answers.
//
// It builds its own fixture rather than calling compareWalkthrough,
// which hardcodes ": main 1 pop ;" because the walkthrough scripts
// need no MUF. Using it here compared a program that prints nothing,
// so the case passed while testing nothing -- found by reverting the
// change and watching the case still pass.
func TestControlsPrimMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), controlsPrimSource)
	if err != nil {
		t.Fatal(err)
	}
	script := controlsPrimScript
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
		if diffs := Compare(maskVariable(want),
			maskVariable(got)); len(diffs) > 0 {
			t.Errorf("step %d, %q\n%s", i, cmd,
				Render(diffs))
		}
	}
}
