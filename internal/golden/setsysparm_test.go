package golden

import (
	"context"
	"testing"
)

// SETSYSPARM reports tune_setparm's result, and upstream has a
// different message for each (p_misc.c's own switch): "Unknown
// parameter. (1)", "Bad parameter syntax. (2)", "Bad parameter value.
// (2)" and "Permission denied. (1)". This server collapsed them and
// always said the third.
//
// It also called the **wrong setter**. tune_setparm is stricter than
// the loader's, and visibly so: a boolean reads only its first
// character, and a timespan refuses a bare count of seconds. So
// SETSYSPARM used to accept values upstream rejects, which is the
// half that mattered -- a program could put a value in the database
// that @tune itself would not.
const setsysparmSource = `: ts[ s -- ] me @ s @ notify ;
: main
  ( a name that is not a parameter )
  0 try "nosuchparameter" "x" setsysparm "ok" catch endcatch ts

  ( a boolean reads its first character, so "yellow" is true and
    "true" is a syntax error -- which is the stricter setter's
    doing and used to be accepted )
  0 try "quiet_moves" "yellow" setsysparm "okA" catch endcatch ts
  0 try "quiet_moves" "true" setsysparm "okB" catch endcatch ts

  ( an integer is sign-then-digits and nothing else )
  0 try "penny_rate" "12abc" setsysparm "okC" catch endcatch ts
  0 try "penny_rate" "7" setsysparm "okD" catch endcatch ts

  ( a timespan refuses a bare count of seconds )
  0 try "clean_interval" "3600" setsysparm "okE" catch endcatch ts
  0 try "clean_interval" "1d12h" setsysparm "okF" catch endcatch ts

  ( a dbref is matched, and a program can only name one by number
    or registration )
  0 try "player_start" "#0" setsysparm "okG" catch endcatch ts
  0 try "player_start" "here" setsysparm "okH" catch endcatch ts

  ( and the reset form )
  0 try "%penny_rate" "" setsysparm "okI" catch endcatch ts
;`

// setsysparmScript raises the fixture to mucker 4, which SETSYSPARM
// requires, and then reads two of the parameters back to show the
// strict setter actually wrote what it accepted.
var setsysparmScript = Script{
	"@set test.muf=wizard",
	"@set test.muf=3",
	"test",
	"@tune quiet_moves",
	"@tune penny_rate",
	"@tune clean_interval",
}

// TestSetsysparmMatchesFuzzball compares the lot.
func TestSetsysparmMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), setsysparmSource)
	if err != nil {
		t.Fatal(err)
	}
	script := setsysparmScript
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
