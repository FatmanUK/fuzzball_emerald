package golden

import (
	"context"
	"testing"
)

// MUF `RECYCLE` made **none** of the five refusals `prim_recycle`
// makes after its permission test (`p_db.c:2206-2232`). It went
// straight to `World.Recycle`, which guards only nil-and-garbage —
// so a mucker-4 program could `#0 recycle` and turn the global
// environment into garbage, or recycle a player. `@recycle`, the
// command, refuses both.
//
// The previous tranche recorded these as "its other refusals are
// still missing" without noticing that one of them destroys the
// world. That is why this runs at **mucker 4**: below it, the `(mlev
// < 4) && !permissions(...)` clause refuses anything the program does
// not own, so it masks the whole set. At 4 these refusals are the
// only thing left.
//
// Raising the fixture's program to 4 takes **both** lines — the
// Wizard bit plus any mucker bit is level 4 outright and neither
// alone is — and they go on `test.muf`, the program, not on `test`,
// the exit, where mucker bits mean exit priority.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const recyclePrimSource = `: ts[ s -- ] me @ s @ notify ;
: main
  ( The global environment. Before this refusal existed, a mucker-4
    program ran World.Recycle on #0 and the world became garbage. )
  0 try #0 recycle catch ts endcatch

  ( A player. World.Recycle even removes them from the name index,
    so this was not a near miss either. )
  0 try me @ recycle catch ts endcatch

  ( Anything a dbref @tune parameter names, because recycling it
    leaves the server pointing at garbage. Every one of the six
    dbref parameters defaults to #0 or #1, both of which the two
    refusals above reach first -- which is the same shadowing
    do_recycle has -- so the script points lost_and_found at the
    attic to make this branch reachable at all. )
  0 try #4 recycle catch ts endcatch

  ( The running program itself. #2 is test.muf. )
  0 try #2 recycle catch ts endcatch

  ( And the one that should work, so the refusals above are not
    simply refusing everything: a thing the program's owner owns. )
  0 try #5 recycle "widget recycled" ts catch ts endcatch
  #5 ok? if "widget still there" else "widget is garbage" then ts

  ( The attic survived its refusal, which says the guard refused
    rather than failed. )
  #4 ok? if "attic still there" else "attic is garbage" then ts
;`

// recyclePrimScript builds the attic at #4 and the widget at #5 —
// #2 is the program and #3 its exit — then raises the program to
// mucker 4.
var recyclePrimScript = Script{
	"@dig Attic", // #4

	// @tune's dbref matcher is match_absolute, match_registered,
	// match_player, match_me and match_here and nothing else, so
	// the attic cannot be named here and has to be given by ref.
	"@tune lost_and_found=#4",

	"@create widget", // #5
	"drop widget",

	// Both lines, and on the program rather than the exit.
	"@set test.muf=wizard",
	"@set test.muf=3",

	"test",
}

// TestRecyclePrimRefusalsMatchFuzzball compares the ladder.
func TestRecyclePrimRefusalsMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), recyclePrimSource)
	if err != nil {
		t.Fatal(err)
	}
	script := recyclePrimScript
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
