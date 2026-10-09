package golden

import (
	"context"
	"testing"
)

// `CHECKREMOTE` (`include/interp.h:490`) is upstream's rule that
// **below mucker level 2 a program may only look at what is near
// it**: the object itself, HOME, the running player, anything at or
// holding the player's location, or anything `ProgUID` controls
// outright. Everything else is "Mucker Level 2 required to get remote
// info."
//
// Upstream applies it at **61 sites across six primitive files**;
// this server applied it at three. So a mucker-1 program could read
// any object anywhere in the database — its name, owner, location,
// flags, properties, links, contents — and notify anybody.
//
// **Reaching it needs `ProgUID` to be a non-wizard**, and that falls
// out of the rule's own threshold: `progUID` (`prim_lock.go:58`)
// returns the *program's owner* whenever `MLevel() < 2`, which is
// exactly when CHECKREMOTE applies. So chowning the program to Bob is
// enough, and no STICKY bit is needed — but Bob must have a mucker
// level of his own, or `find_mlev` caps the program at 0 and every
// probe aborts on its floor instead.
//
// The object under test is in a **different room** and owned by `#1`,
// so Bob neither stands near it nor controls it.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const checkRemoteSource = `: ts[ s -- ] me @ s @ notify ;
: oops[ s -- ] "  " s @ strcat ts ;
: main
  ( One probe per primitive family, each catching its own abort so
    the run reaches the end instead of stopping at the first. #5 is
    the gem, in the vault; #4 is the widget, here in the room. )
  "name:" ts
  0 try #5 name ts catch oops endcatch
  "owner:" ts
  0 try #5 owner intostr ts catch oops endcatch
  "location:" ts
  0 try #5 location intostr ts catch oops endcatch
  "thing?:" ts
  0 try #5 thing? intostr ts catch oops endcatch
  "flag?:" ts
  0 try #5 "dark" flag? intostr ts catch oops endcatch
  "mlevel:" ts
  0 try #5 mlevel intostr ts catch oops endcatch
  "pennies:" ts
  0 try #5 pennies intostr ts catch oops endcatch
  "contents:" ts
  0 try #5 contents intostr ts catch oops endcatch
  "getpropstr:" ts
  0 try #5 "plain" getpropstr ts catch oops endcatch
  "setprop:" ts
  0 try #5 "plain" "x" setprop "  ok" ts catch oops endcatch
  "notify:" ts
  0 try #5 "hi" notify "  ok" ts catch oops endcatch
  "fmtstring D:" ts
  0 try #5 "%D" fmtstring ts catch oops endcatch

  ( A remote object ProgUID **controls**, which is the escape the
    rule ends on: every location test fails for it, so only the
    final controls test can let it through. Bob owns the pebble and
    it sits in the vault with the gem. )
  "bob's remote pebble:" ts
  0 try #8 name ts catch oops endcatch

  ( ...and the same probes against something that *is* near: the
    widget in the room the player is standing in. These must all
    succeed, or the rule is simply refusing everything. )
  "near name:" ts
  0 try #4 name ts catch oops endcatch
  "near owner:" ts
  0 try #4 owner intostr ts catch oops endcatch
  "near prop:" ts
  0 try #4 "plain" getpropstr ts catch oops endcatch
;`

// checkRemoteScript hands the program to Bob at mucker 3 and runs it
// at 1, so ProgUID is Bob and CHECKREMOTE applies. #2 is test.muf and
// #3 its exit.
var checkRemoteScript = Script{
	"@create widget", // #4, here in the room
	"@create gem",    // #5, bound for the vault
	"@dig Vault",     // #6
	"@set widget=plain:p",
	"@set gem=plain:p",
	"@teleport gem=Vault",

	"@pcreate Bob=pw",
	"@set Bob=3",
	"@chown test.muf=Bob",

	// A remote object Bob owns, so the rule's last clause --
	// controls(ProgUID, x) -- is the only thing that can admit
	// it. Without this the clause could be deleted and every
	// probe would still answer the same way.
	"@create pebble", // #8 — Bob took #7
	"@chown pebble=Bob",
	"@teleport pebble=Vault",

	// Mucker 1: ProgUID is Bob, who is neither near the gem nor
	// in control of it.
	"@set test.muf=1",
	"test",

	// Mucker 2 lifts CHECKREMOTE entirely, and nothing else about
	// the program changes.
	"@set test.muf=2",
	"test",
}

// TestCheckRemoteMatchesFuzzball compares the sweep at two levels.
func TestCheckRemoteMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), checkRemoteSource)
	if err != nil {
		t.Fatal(err)
	}
	script := checkRemoteScript
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
