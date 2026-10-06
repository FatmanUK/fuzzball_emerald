package golden

import (
	"context"
	"testing"
)

// SETNAME was gated at mucker 4 by the generated mucker table, and
// that floor does not exist. prim_setname's rule is "(mlev < 4) &&
// !permissions(ProgUID, ref)" — a wizard **or** whoever has
// permissions on the object — so a mortal program could not rename
// an object it owned, and the refusal said "Permission denied.
// Requires Wizbit." where upstream says a bare "Permission denied."
//
// The false floor came from a *second*, bare "if (mlev < 4)" nested
// inside "if (Typeof(ref) == TYPE_PLAYER)": the extractor reads the
// inner condition without its enclosing one. CLAUDE.md already warns
// that the extractor is approximate; this is the first case of the
// enclosing-guard shape rather than the same-condition one it already
// skips.
//
// The **order** matters too: both type tests come before the
// permission one, so a program handed a non-string name is told about
// the argument rather than about permission.
//
// This runs at the fixture's own mucker 3, which is below the false
// floor and above nothing — exactly where the difference shows.
const setnameSource = `: ts[ s -- ] me @ s @ notify ;
: main
  ( the two type tests, which come first. The second target is a
    *different* player, which the permissions test refuses -- so
    this is the one case that can tell the order apart. With the
    permission check first it would answer "Permission denied."
    instead. )
  0 try me @ 5 setname catch ts endcatch
  0 try #5 5 setname catch ts endcatch
  0 try 5 "x" setname catch ts endcatch
  0 try #-1 "x" setname catch ts endcatch

  ( an object this program's owner owns, which the permissions
    test lets through -- so mucker 3 is enough and the false floor
    was the only thing refusing it )
  0 try #4 "renamed" setname "ok" catch endcatch ts
  #4 name ts

  ( a bad name for a thing )
  0 try #4 "" setname catch ts endcatch

  ( a player is never permitted below wizard level, and the
    password path is behind that )
  0 try me @ "Bob secret" setname catch ts endcatch
;`

// setnameScript makes the thing at #4. #2 is the program, #3 its
// exit.
var setnameScript = Script{
	"@create widget",
	// A second player, so there is an object the program has no
	// permissions on at all: permissions() answers false for any
	// player but the asker, and nothing else in a one-player
	// fixture can fail that test.
	"@pcreate Bob=secret",
	"test",
}

// TestSetnameMatchesFuzzball compares the ladder.
func TestSetnameMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), setnameSource)
	if err != nil {
		t.Fatal(err)
	}
	script := setnameScript
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
