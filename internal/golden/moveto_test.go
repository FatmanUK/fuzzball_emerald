package golden

import (
	"context"
	"testing"
)

// `prim_moveto` (`p_db.c:150`) is a type switch rather than a move,
// and this server was a bare `h.MoveTo(victim, dest)` — the raw
// store move, which refuses only self-containment — with a mucker
// floor of 3 in the generated table standing in for all of it.
//
// The floor was not real: its `if ((mlev < 3))` at `p_db.c:234` opens
// a block of extra mortal-only restrictions rather than refusing. So
// every M1 and M2 program was refused outright, and every M3 and M4
// one got an unvalidated move — no `enter_room` for a player, so no
// announcement, no autolook and no arrive propqueue; no loop check;
// no exit re-sourcing; no room reparenting.
//
// Thirteen of its tests are conditional on mucker level and **none**
// is a floor, which is why the probes run at mucker 1 and again at 3.
// It had no test of any kind before this.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const moveToSource = `: ts[ s -- ] me @ s @ notify ;
: main
  ( An exit victim below mucker 3 is refused outright, which is a
    *type* test and not a floor. #3 is this program's own exit. )
  0 try #3 me @ location moveto catch ts endcatch

  ( A thing the program does not own and which is not JUMP_OK is
    refused with its own sentence. #7 is a gem belonging to Bob. )
  0 try #7 me @ location moveto catch ts endcatch

  ( An exit as the *destination* is refused before any of it. )
  0 try #4 #10 moveto catch ts endcatch

  ( A player may only go to a room or to a vehicle thing. )
  0 try me @ #4 moveto catch ts endcatch

  ( A thing moved into itself is the loop check. )
  0 try #4 #4 moveto catch ts endcatch

  ( The mortal-only block at p_db.c:234. Bob's vault at #8 is not
    JUMP_OK and this program has no permissions on it, so the
    destination test is what refuses. )
  0 try #4 #8 moveto catch ts endcatch

  ( A vehicle destination that is not a thing, which can only be a
    room flagged VEHICLE -- upstream's way of spelling a vehicle
    room. #9 is one and #5 is a vehicle thing. )
  0 try #5 #9 moveto catch ts endcatch

  ( The other side of that clause: a vehicle thing *into* another
    vehicle thing is permitted, because the refusal only applies
    when the destination is not a thing. #12 is a second vehicle
    thing standing in the same room. )
  0 try #5 #12 moveto "vehicle into vehicle" ts catch ts endcatch

  ( The matchroom rule, which assigns twice without an else -- so
    the *victim's location* wins when both it and the destination
    are controlled. The gem at #7 is Bob's, so permissions on the
    victim fail and the rule can bite; it is JUMP_OK so the earlier
    "Object can" "t be moved." does not fire first; it sits in the
    hall at #11, which is JUMP_OK, and the destination #9 is not.
    Last-wins picks the hall and permits; first-wins picks the
    garage and refuses. )
  0 try #7 #9 moveto "matchroom allowed" ts catch ts endcatch

  ( And the move that should work at both levels: the player's own
    widget into the room they are standing in. )
  0 try #4 me @ location moveto "moved" ts catch ts endcatch
  #4 location me @ location = if "widget is here" else
    "widget went astray" then ts

  ( An exit re-source, which is the branch that is not a move: it
    changes the exit's location *and* resets its priority to 0,
    because an exit's mucker bits are how hard it competes. The exit
    is set to priority 2 by the script, so "mucker" reads true until
    the re-source clears it. )
  0 try #10 #4 moveto "exit resourced" ts catch ts endcatch
  #10 location intostr ts
  #10 "mucker" flag? if "priority kept" else "priority reset" then
    ts

  ( A room reparent into somewhere the player neither controls nor
    may link to is what can_teleport_to refuses -- Bob's vault at
    #8. Permissions on the room itself pass, so this isolates the
    second half of that condition. )
  0 try #11 #8 moveto "reparented into the vault" ts catch ts
  endcatch

  ( A room reparent, into the garage at #9 rather than where it
    already is -- so the location afterwards says whether the move
    happened at all. )
  0 try #11 #9 moveto "room reparented" ts catch ts endcatch
  #11 location intostr ts

  ( And a player, who is walked in through enter_room rather than
    put there -- so the move announces itself and the autolook runs
    where a bare moveto would be silent. This is last because it
    changes where the player is standing. #8 is Bob's vault, which
    the mortal-only block refuses below mucker 3; #11 is the hall. )
  0 try me @ #8 moveto catch ts endcatch
  0 try me @ #11 moveto catch ts endcatch
  me @ location intostr ts
;`

// moveToScript builds the shapes the probes need. #2 is the program
// and #3 its exit, so the objects start at #4.
//
// Everything is dropped rather than carried: @create leaves an object
// in the player's inventory, which is how three cases last tranche
// came to compare byte-identical while testing nothing.
var moveToScript = Script{
	"@create widget", // #4
	"drop widget",
	"@create cart", // #5, a vehicle thing
	"drop cart",
	"@set cart=vehicle",
	"@pcreate Bob=secret", // #6
	"@create gem",         // #7, made Bob's below
	"drop gem",
	"@chown gem=Bob",
	"@dig Vault", // #8, Bob's room
	"@chown Vault=Bob",
	"@dig Garage", // #9, a room flagged VEHICLE
	"@set Garage=vehicle",
	"@open door",  // #10
	"@set door=2", // priority 2, which the re-source resets
	"@dig Hall",   // #11, the player's own room
	"@set Hall=jump_ok",
	"@create sled", // #12, a second vehicle thing
	"drop sled",
	"@set sled=vehicle",

	// The gem is JUMP_OK so the "Object can't be moved." test
	// does not fire before the matchroom rule, and it is put in
	// the hall so its location and the destination differ.
	"@set gem=jump_ok",
	"@teleport gem=#11",

	// Mucker 1: below every conditional test.
	"@set test.muf=1",
	"test",

	// And at the fixture's own level, where the same probes take
	// the other branch.
	"@set test.muf=3",
	"test",
}

// TestMoveToMatchesFuzzball compares both runs.
func TestMoveToMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), moveToSource)
	if err != nil {
		t.Fatal(err)
	}
	script := moveToScript
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
