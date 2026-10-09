package golden

import (
	"context"
	"testing"
)

// The containment-chain and link primitives shared two factories and
// have eight different contracts between them.
//
// `CONTENTS` and `NEXT` **skip** what a low-mucker program may not
// see rather than refusing, and their two skip rules differ: CONTENTS
// hides a DARK object the program does not control, where NEXT hides
// a ROOM as well and exempts an exit from both — so a mucker-1
// program walking a chain steps over the rooms in it while the first
// step of the same walk does not. `EXITS` and `EXITS_ARRAY` have a
// mucker-3 ownership gate. Three of the four refuse a program or an
// exit argument and each words it differently, two of them with an
// empty array rather than an abort. `EXITS_ARRAY` asks its permission
// question *before* checking that the object exists, which is the
// opposite order from every one of its neighbours. `GETLINK`,
// `GETLINKS` and `GETLINKS_ARRAY` all refuse a program. And
// `NEXTOWNED` restarts at #0 for a player argument.
//
// None of it was here. The case runs at mucker **1** for the skip
// rules and again at 3 for the gates, since the fixture compiles at 3
// and the two halves need different levels.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const chainPrimSource = `: ts[ s -- ] me @ s @ notify ;
: oops[ s -- ] me @ s @ notify ;
: n[ x -- ] x @ intostr ts ;
: walk[ r -- ]
  ( Every ref a NEXT walk reaches from here, as one line. )
  ""
  begin r @ #-1 dbcmp not while
    r @ intostr strcat " " strcat
    r @ next r !
  repeat
  ts
;
: probes
  ( CONTENTS skips a DARK thing the program does not control, and
    the first visible one is what it answers. )
  0 try #0 contents unparseobj ts catch oops endcatch
  0 try #0 contents_array array_count n catch oops endcatch

  ( NEXT hides rooms too, so the same chain walked a step at a time
    is shorter than contents_array says. )
  0 try #0 contents walk catch oops endcatch

  ( NEXT exempts an EXIT from both halves of its rule, so the
    exits chain walks whole where the contents chain does not. )
  0 try #0 exits walk catch oops endcatch

  ( EXITS and EXITS_ARRAY are mucker 3 unless the program owns the
    object -- #7 is Bob's room, so the gate bites. )
  0 try #0 exits unparseobj ts catch oops endcatch
  0 try #0 exits_array array_count n catch oops endcatch
  0 try #7 exits unparseobj ts catch oops endcatch
  0 try #7 exits_array array_count n catch oops endcatch

  ( CONTENTS_ARRAY's type check runs BEFORE its CHECKREMOTE, so a
    remote exit answers an empty array rather than refusing. #13 is
    Bob's exit, attached to Bob's room. )
  0 try #13 contents_array array_count n catch oops endcatch

  ( Three argument refusals, three wordings. )
  0 try #2 exits unparseobj ts catch oops endcatch
  0 try #9999 exits unparseobj ts catch oops endcatch
  0 try #9999 contents unparseobj ts catch oops endcatch
  0 try #9999 contents_array array_count n catch oops endcatch
  0 try #9999 next unparseobj ts catch oops endcatch

  ( ...and two that answer with an empty array instead. )
  0 try #2 contents_array array_count n catch oops endcatch
  0 try #3 contents_array array_count n catch oops endcatch

  ( GETLINK and GETLINKS refuse a program and GETLINKS_ARRAY
    answers an empty array, which upstream says out loud. Their
    invalid-object messages are three different wordings. #2 is
    test.muf and #3 the exit in front of it. )
  0 try #2 getlink unparseobj ts catch oops endcatch
  0 try #2 getlinks n catch oops endcatch
  0 try #2 getlinks_array array_count n catch oops endcatch
  0 try #3 getlink unparseobj ts catch oops endcatch
  0 try #9999 getlink unparseobj ts catch oops endcatch
  0 try #9999 getlinks n catch oops endcatch
  0 try #9999 getlinks_array array_count n catch oops endcatch

  ( UNPARSEOBJ's four sentinels, which this collapsed into one. )
  0 try #-1 unparseobj ts catch oops endcatch
  0 try #-3 unparseobj ts catch oops endcatch
  0 try #-4 unparseobj ts catch oops endcatch
  0 try #9999 unparseobj ts catch oops endcatch
  0 try #1 unparseobj ts catch oops endcatch

  ( NEXTOWNED restarts at #0 for a player, so this reaches
    something below #1 -- which the old version could not. )
  0 try me @ nextowned unparseobj ts catch oops endcatch
  0 try #0 nextowned unparseobj ts catch oops endcatch
;
: main
  "3" stringcmp not if
    probes
    ( EXITS_ARRAY asks its permission question BEFORE checking
      that the object exists, and at mucker 1 that means upstream
      reads OWNER of an out-of-range dbref: the C server dies, and
      the harness reports EOF rather than a transcript. So the
      probe runs only at 3, where the level test short-circuits
      and "Invalid dbref" is reached safely. This server answers
      the same thing at both levels, because its permissions
      helper reads an invalid ref as owned by nobody. )
    0 try #9999 exits_array array_count n catch oops endcatch
  else
    probes
  then
;`

var chainPrimScript = Script{
	"@tune penny_rate=0",
	"@create lit", // #4
	"drop lit",
	"@open way", // #5
	"@link way=#0",
	"@pcreate Bob=secret", // #6

	// Two Bob-owned rooms in #0's contents chain, because the
	// NEXT skip only shows from the *second* step onwards: the
	// first element of a walk comes from CONTENTS, which keeps a
	// room.
	"@dig Bobroom", // #7
	"@chown Bobroom=Bob",
	"@dig Bobroom2", // #8
	"@chown Bobroom2=Bob",

	// A DARK thing of #1's, which ownership excuses even when
	// quelled — so the skip has to be shown with Bob's.
	"@create shadow", // #9
	"@set shadow=dark",
	"drop shadow",

	// A DARK Bob-owned **exit** in #0's exits chain, which NEXT
	// exempts from both halves of its rule.
	"@open bobway", // #10
	"@chown bobway=Bob",
	"@set bobway=dark",
	"@open bobway2", // #11
	"@chown bobway2=Bob",
	"@set bobway2=dark",

	// And a Bob-owned exit somewhere else, so CONTENTS_ARRAY's
	// type check can be shown to run *before* CHECKREMOTE.
	// `@action`, not `@open`: an exit has to be *created*
	// elsewhere, because @teleport cannot move one -- its victim
	// switch has no exit case at all. @action attaches an exit to
	// a named object, which is the only way to put one anywhere
	// but the room you are standing in.
	"@dig Far",          // #12
	"@action remote=#7", // #13
	// By ref: the exit is attached to Bob's room, so the matcher
	// cannot reach it from here.
	"@chown #13=Bob",

	// Last, so it is the head of #0's contents chain: a DARK
	// thing Bob owns.
	"@create theirs", // #14
	"@set theirs=dark",
	"drop theirs",
	"@chown theirs=Bob",

	// Quelled for both runs, because `controls(ProgUID, ...)`
	// excuses every skip for a wizard and ProgUID for an exit-run
	// program is the caller's owner. The first draft quelled only
	// the mucker-1 run, and the mucker-3 rung then agreed with it
	// for the wrong reason.
	"@set me=quell",
	"@set test.muf=1",
	"test",
	"@set test.muf=3",
	"test 3",
	"@set me=!quell",
}

// TestChainPrimsMatchFuzzball compares the ladder.
func TestChainPrimsMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), chainPrimSource)
	if err != nil {
		t.Fatal(err)
	}
	script := chainPrimScript
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
