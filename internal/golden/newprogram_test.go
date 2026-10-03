package golden

import (
	"context"
	"testing"
	"time"
)

// NEWPROGRAM was a stub that aborted "NEWPROGRAM is not implemented
// yet", on the stated grounds that the editor arrived with the rest
// of the interactive machinery. The editor arrived; the comment did
// not keep up, and the docs went on claiming every primitive was
// ported.
//
// This is its own fixture rather than a case in the shared `cases`
// list, for two reasons. The shared fixture's program compiles at
// mucker 3 and NEWPROGRAM's floor is 4, so it has to be raised first;
// and creating objects shifts every later dbref in a shared database.
//
// The raise must name **`test.muf`**, the program, and not `test`,
// which is the exit in front of it. `@set test=wizard` cheerfully
// reports "Flag set." and sets WIZARD on the *exit*, where it means
// nothing to find_mlev — the program's authority is the lower of
// its own level and its owner's, and an exit's level is exit
// priority. Both lines are still needed on the program: the Wizard
// bit plus any mucker bit is level 4 outright and neither alone is.
const newProgramSource = `: ts[ s -- ] me @ s @ notify ;
: main
  "made" newprogram var! p

  ( what create_program does besides creating the object: the stock
    description, the owner, the location, and the mucker cap )
  p @ name ts
  p @ "_/de" getpropstr ts
  p @ program? if "program: yes" else "program: no" then ts
  p @ owner me @ = if "owner: me" else "owner: other" then ts
  p @ location me @ = if "in my hands" else "elsewhere" then ts
  p @ mlevel intostr "mlevel=" swap strcat ts

  ( a new program starts with no source, which is what the editor
    opens onto )
  p @ "    " ts

  ( ok_object_name, which lives inside create_program upstream and so
    reaches @program too. Each of these is refused for a different
    clause of it. )
  0 try "me" newprogram pop catch ts endcatch
  0 try "here" newprogram pop catch ts endcatch
  0 try "home" newprogram pop catch ts endcatch
  0 try "nil" newprogram pop catch ts endcatch
  0 try "#4" newprogram pop catch ts endcatch
  0 try "$lib" newprogram pop catch ts endcatch
  0 try "*who" newprogram pop catch ts endcatch
  0 try "!bang" newprogram pop catch ts endcatch
  0 try "a=b" newprogram pop catch ts endcatch
  0 try "a&b" newprogram pop catch ts endcatch
  0 try "a|b" newprogram pop catch ts endcatch

  ( the empty string passes the type test -- upstream's string is
    NULL for it -- and fails ok_object_name, so it is the name
    refusal rather than the type one )
  0 try "" newprogram pop catch ts endcatch

  ( and the type error, which carries no argument index unlike every
    other use of that wording upstream )
  0 try 5 newprogram pop catch ts endcatch
  0 try me @ newprogram pop catch ts endcatch
;`

// newProgramScript raises the fixture program to mucker 4, runs it,
// and then looks at what it made from the outside.
var newProgramScript = Script{
	"@set test.muf=wizard",
	"@set test.muf=3",
	"test",
	"examine made",
	"@list made",
	"inventory",
}

// TestNewProgramMatchesFuzzball checks prim_newprogram and the
// create_program behind it against the C server.
func TestNewProgramMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), newProgramSource)
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	script := newProgramScript
	oracle, err := RunOracleQuiet(ctx, fx, script, quiet)
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
		// examine's timestamps and memory figure cannot agree
		// in principle — a timezone and a different object
		// layout — so they are masked, keeping the line and
		// its position. maskVariable is examine_test.go's,
		// shared rather than duplicated.
		if diffs := Compare(maskVariable(want),
			maskVariable(got)); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}
