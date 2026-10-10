package golden

import (
	"context"
	"testing"
)

// MUF `RMATCH` and `MATCH` are both matchers and neither was one.
//
// `prim_rmatch` (`p_db.c:860`) is `init_match` with THING as the
// preferred type and then `match_rmatch` — the contents and exits
// of one named object. This walked the two chains comparing whole
// names with an ASCII fold, so there was no word-prefix matching, no
// alias splitting, no exit priority, no ambiguity (`#-2` was
// unreachable) and no preferred type. `Matcher.Inside` had been
// written for it and had no callers.
//
// `prim_match` (`p_db.c:815`) is **not `match_everything`** either: a
// `$name` gets `match_registered` alone, anything else gets exits,
// neighbour, possession, me, here, home and nil, and absolute refs
// and player names are added only for `Wizard(ProgUID) || mlev >= 4`.
// This called `Everything().Player()`, which is neither the same list
// nor the same gate — so a mucker-1 program could resolve `#5` and,
// before the star became compulsory, any player by name.
//
// Its two argument messages carry **no full stop**, unlike almost
// every neighbour.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const rmatchSource = `: ts[ s -- ] me @ s @ notify ;
: oops[ s -- ] me @ s @ notify ;
: probes
  ( A word prefix, which an exact fold cannot do. )
  0 try #0 "rust" rmatch unparseobj ts catch oops endcatch
  0 try #0 "key" rmatch unparseobj ts catch oops endcatch

  ( An exit alias: match_exits splits on ';'. )
  0 try #0 "foo" rmatch unparseobj ts catch oops endcatch
  0 try #0 "bar" rmatch unparseobj ts catch oops endcatch

  ( Two PARTIAL matches are AMBIGUOUS -- #-2, which unparseobj
    renders "*INVALID*" since its switch has no AMBIGUOUS case. An
    exact fold had no way to reach it at all.

    Partial rather than two of one name: match_contents settles two
    EXACT matches with choose_thing, which tosses a coin -- so no
    transcript can pin that one, which is exactly what the first
    draft of this probe tried to do. Upstream and this server then
    disagreed at random. )
  0 try #0 "twin" rmatch unparseobj ts catch oops endcatch

  ( Nothing at all is #-1. )
  0 try #0 "nosuchthing" rmatch unparseobj ts catch oops endcatch

  ( The two argument refusals, neither with a full stop. )
  0 try #2 "x" rmatch unparseobj ts catch oops endcatch
  0 try #3 "x" rmatch unparseobj ts catch oops endcatch
  0 try #9999 "x" rmatch unparseobj ts catch oops endcatch
  0 try #0 1 rmatch unparseobj ts catch oops endcatch

  ( MATCH's own list. "me", "here", "home" and "nil" are in it. )
  0 try "me" match unparseobj ts catch oops endcatch
  0 try "here" match unparseobj ts catch oops endcatch
  0 try "home" match unparseobj ts catch oops endcatch
  0 try "nil" match unparseobj ts catch oops endcatch

  ( A registration gets match_registered and nothing else, so a
    thing of the same name in the room cannot shadow it. )
  0 try "$spot" match unparseobj ts catch oops endcatch

  ( And an absolute ref and a player name are only for a wizard or
    mucker 4 -- except that match_contents and match_exits each
    carry their own #N branch, gated on the searcher's owner
    CONTROLLING the object. So #4 resolves at mucker 1 because #1
    owns it, and #10 does not because Bob does. )
  0 try "#4" match unparseobj ts catch oops endcatch
  0 try "#10" match unparseobj ts catch oops endcatch
  0 try #0 "#10" rmatch unparseobj ts catch oops endcatch

  ( match_exits carries the same branch as match_contents, and an
    EXIT is only ever in the exits chain -- so this is the one that
    reaches it. The two copies differ in one line: this one
    continues where the other returns. )
  0 try "#5" match unparseobj ts catch oops endcatch
  0 try #0 "#5" rmatch unparseobj ts catch oops endcatch
  0 try "*Bob" match unparseobj ts catch oops endcatch
;
: main pop probes ;`

var rmatchScript = Script{
	"@tune penny_rate=0",

	"@create a rusty key", // #4
	"drop a rusty key",
	"@open foo;bar", // #5
	"@link foo=#0",
	"@create twinkle", // #6
	"drop twinkle",
	"@create twinset", // #7
	"drop twinset",

	// A registration on the player, and a thing of the same name
	// in the room so that the two lists can be told apart.
	"@create spot", // #8
	"drop spot",
	"@register #4=spot",

	"@pcreate Bob=secret", // #9

	// Bob's thing in the room, which the searcher's owner does
	// not control -- so the #N branch inside match_contents
	// refuses it where it accepts #4.
	"@create theirs", // #10
	"drop theirs",
	"@chown theirs=Bob",

	// Mucker 1 first: the program's own level is what decides
	// whether absolute refs and player names are in the list, and
	// the wizard quells itself so `Wizard(ProgUID)` cannot widen
	// it regardless.
	"@set me=quell",
	"@set test.muf=1",
	"test",

	// And mucker 3, still quelled, which is still below 4.
	"@set test.muf=3",
	"test 3",

	// Then unquelled at mucker 3: `Wizard(ProgUID)` is the other
	// half of the gate, so absolute refs and player names come
	// into the list without the level changing.
	"@set me=!quell",
	"test 3",
}

// TestRMatchMatchesFuzzball compares the ladder.
func TestRMatchMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), rmatchSource)
	if err != nil {
		t.Fatal(err)
	}
	script := rmatchScript
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
