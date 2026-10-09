package golden

import (
	"context"
	"testing"
)

// MUF `SETLINK` and `SETLINKS_ARRAY` were fifteen lines standing in
// for a hundred and twenty. Both popped their arguments, checked the
// destination existed, and stored it: no permission test, no type
// rules, no loop check, and none of the dozen refusals between them.
// So a program could link a player to a thing, link an exit into a
// ring, relink an exit that was already linked, and give a room four
// drop-tos.
//
// `prog_can_link_to` (`p_db.c:1680`) was ported nowhere. It is
// **not** `can_link_to`, the command side's rule: the type rules are
// the same four, but the permission tail asks about the *destination*
// — its ownership, then its `Linkable` flag and its link lock —
// where the command side asks about the linker. Upstream keeps the
// two apart with no comment saying why.
//
// Three of upstream's own branches are **dead**, and the first three
// probes pin that rather than the code: `valid_object(oper1)` runs
// before anything looks at SETLINK's destination, and it is
// `ObjExists && !GARBAGE` — so NOTHING, HOME and NIL all abort with
// "Invalid object. (2)" first. The documented "a target of NOTHING
// unlinks the given exit or room source" cannot happen, and
// `prog_can_link_to`'s HOME and NIL clauses are unreachable from
// SETLINK. Both are live for SETLINKS_ARRAY, which tests for the two
// *before* validating.
//
// Mucker 4 passes `prog_can_link_to` outright and skips the ownership
// gate, so the fixture is left at **3**, which is what it compiles
// at.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const setLinkSource = `: ts[ s -- ] me @ s @ notify ;
: oops[ s -- ] me @ s @ notify ;
: many ( n -- a   an array of n copies of the pebble )
  ( Copies of a THING, not of a room: an exit may carry any number
    of things, so the count check is what answers rather than the
    one-player-room-or-program rule. )
  { }list swap
  1 swap 1 for pop #6 swap array_appenditem repeat
;
: atfour
  ( Run again at mucker 4, where prog_can_link_to passes outright
    and the ownership gate is skipped: Bob's shut room becomes a
    legal destination and Bob's own exit becomes a legal source. )
  0 try #13 #11 setlink "shut room linked at 4" ts
    catch oops endcatch
  0 try #10 #5 setlink "Bob's exit linked at 4" ts
    catch oops endcatch
  0 try #22 { #5 }list setlinks_array "Bob's array set at 4" ts
    catch oops endcatch
;
: probes
  ( The three dead branches, on an exit that is not yet linked so
    that nothing else can answer first. )
  0 try #13 #-1 setlink "unlinked" ts catch oops endcatch
  0 try #13 #-3 setlink "linked to HOME" ts catch oops endcatch
  0 try #13 #-4 setlink "linked to NIL" ts catch oops endcatch

  ( A program may not be linked at all, and the message names the
    source as argument 1 even though the source is popped second. )
  0 try #2 #0 setlink "program linked" ts catch oops endcatch

  ( The four type rules: a player may only be homed to a room, a
    room's dropto may only be a thing or a room, and a thing may not
    be homed to an exit or a program. )
  0 try me @ #6 setlink "player homed to a thing" ts
    catch oops endcatch
  0 try #5 #3 setlink "room dropped to an exit" ts
    catch oops endcatch
  0 try #6 #2 setlink "thing homed to a program" ts
    catch oops endcatch

  ( And the three that are allowed. )
  0 try me @ #5 setlink "player homed to a room" ts
    catch oops endcatch
  0 try #5 #6 setlink "room dropped to a thing" ts
    catch oops endcatch
  0 try #6 #5 setlink "thing homed to a room" ts
    catch oops endcatch

  ( An exit already linked is refused rather than relinked. )
  0 try #7 #5 setlink "exit relinked" ts catch oops endcatch

  ( A ring of exits, which @link refuses and this did not. )
  0 try #8 #9 setlink "first exit linked" ts catch oops endcatch
  0 try #9 #8 setlink "ring closed" ts catch oops endcatch

  ( Ownership of the SOURCE, below mucker 4: the exit is Bob's. )
  0 try #10 #5 setlink "Bob's exit linked" ts catch oops endcatch

  ( And of the DESTINATION, which is prog_can_link_to's own tail.
    Bob's shut room is not Linkable; his open one is ABODE, and a
    room with no link lock passes; his locked one is ABODE and its
    link lock refuses -- so Linkable alone is not enough. )
  0 try #13 #11 setlink "linked to a shut room" ts
    catch oops endcatch
  0 try #14 #12 setlink "linked to an ABODE room" ts
    catch oops endcatch
  0 try #13 #19 setlink "linked past a link lock" ts
    catch oops endcatch

  ( Now the array form, which is a different function rather than
    SETLINK with a list. Its two argument indices are SWAPPED: the
    source ref is 2 and the array is 1, the opposite of their stack
    order. )
  0 try #15 { #5 }list setlinks_array "array set" ts
    catch oops endcatch
  0 try "notaref" { #5 }list setlinks_array catch oops endcatch
  0 try #15 "notanarray" setlinks_array catch oops endcatch
  0 try #16 { "x" }list setlinks_array catch oops endcatch
  0 try #16 { #9999 }list setlinks_array catch oops endcatch

  ( The array form has its own ownership gate, worded with an
    index where SETLINK's carries none. )
  0 try #10 { #5 }list setlinks_array "Bob's exit set" ts
    catch oops endcatch

  ( MAX_LINKS is 50, and the test is ">=" rather than ">" -- so
    forty-nine destinations are allowed and fifty are not. )
  0 try #16 49 many setlinks_array "49 links" ts
    catch oops endcatch
  0 try #16 50 many setlinks_array "50 links" ts
    catch oops endcatch

  ( Only exits take more than one destination. )
  0 try #5 { #6 #5 }list setlinks_array catch oops endcatch

  ( An exit may be linked to several THINGS, because each one is
    fetched -- but to only one player, room or program, because
    walking somewhere cannot happen twice. )
  0 try #16 { #6 #17 }list setlinks_array "two things" ts
    catch oops endcatch
  0 try #18 { #5 #0 }list setlinks_array catch oops endcatch

  ( An empty array unlinks an exit or a room, and nothing else. )
  0 try #16 { }list setlinks_array "exit unlinked" ts
    catch oops endcatch
  0 try #6 { }list setlinks_array catch oops endcatch

  ( HOME and NIL, which SETLINK cannot reach. )
  0 try #6 { #-3 }list setlinks_array catch oops endcatch
  0 try #18 { #-4 }list setlinks_array "exit to NIL" ts
    catch oops endcatch

  ( And an exit pointing at NIL counts as unlinked, so SETLINK may
    link it after all. )
  0 try #18 #5 setlink "relinked over NIL" ts catch oops endcatch

  ( The array form's own two loop checks, which are reported
    differently from SETLINK's: "Destination would create loop." for
    an exit, and a parent paradox for a thing homed inside itself --
    spelt "would case" upstream, which a program matching on the
    line sees. )
  0 try #9 { #8 }list setlinks_array "exit ring by array" ts
    catch oops endcatch
  0 try #6 { #6 }list setlinks_array "thing homed in itself" ts
    catch oops endcatch

  ( An exit's priority is reset by SETLINKS_ARRAY whether or not
    anything is being linked, which is not SETLINK's rule: there
    the reset belongs to the unlink branch alone. The script sets
    priority 2 on both exits first. )
  0 try #20 #5 setlink "priority kept" ts catch oops endcatch
  0 try #21 { #5 }list setlinks_array "priority reset" ts
    catch oops endcatch
;
: main
  "2" stringcmp not if atfour else probes then
;`

var setLinkScript = Script{
	// penny_rate makes anything that counts money after a move
	// nondeterministic; nothing here counts, but the homing
	// probes move nobody and this keeps it that way.
	"@tune penny_rate=0",

	"@pcreate Bob=secret", // #4
	"@dig Hall",           // #5
	"@create pebble",      // #6
	"drop pebble",
	"@open way", // #7
	"@link way=#5",
	"@open ring1", // #8
	"@open ring2", // #9
	"@open theirs",
	"@chown theirs=Bob", // #10
	"@dig Bobshut",
	"@chown Bobshut=Bob", // #11
	"@dig Bobopen",
	"@set Bobopen=abode",
	"@chown Bobopen=Bob", // #12
	"@open dst1",         // #13
	"@open dst2",         // #14
	"@open arr1",         // #15
	"@open arr2",         // #16
	"@create gravel",     // #17
	"drop gravel",
	"@open arr3", // #18
	"@dig Boblocked",
	"@set Boblocked=abode",
	"@linklock Boblocked=*Bob",
	"@chown Boblocked=Bob", // #19
	"@open prio1",
	"@set prio1=2", // #20
	"@open prio2",
	"@set prio2=2", // #21
	"@open theirs2",
	"@chown theirs2=Bob", // #22

	"test",

	// And again at mucker 4, where the two gates that depend on
	// the level are skipped. Both lines, and on the program.
	"@set test.muf=wizard",
	"@set test.muf=3",
	"test 2",

	// What landed where.
	"examine ring1",
	"examine arr2",
	"examine arr3",
	"examine pebble",
	"examine prio1",
	"examine prio2",
}

// TestSetLinkMatchesFuzzball compares the ladder.
func TestSetLinkMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), setLinkSource)
	if err != nil {
		t.Fatal(err)
	}
	script := setLinkScript
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
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}
