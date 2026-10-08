package golden

import (
	"context"
	"testing"
)

// teleportScript covers wiz.c's do_teleport, which four earlier
// golden cases had to tiptoe around: this server answered
// "Teleported." where upstream names both ends, and reparented
// nothing when given a room.
//
// The dbrefs are spelled out rather than registered, because
// @teleport's own matcher is most of what is being tested here: a
// room made by @dig is detached from everything, so neither search
// can reach it by name, and the destination search is narrower than
// the victim search.
//
// #4 is widget, #5 Cellar, #6 Attic, #7 satchel and #8 the exit. #2
// is the fixture's program and #3 its exit.
var teleportScript = Script{
	"@create widget",
	"@dig Cellar",
	"@dig Attic",

	// A detached room cannot be named: @dig puts it nowhere, and
	// neither the victim search nor the destination search looks
	// anywhere but here, the player's hands, the room's contents
	// and the registry.
	"@teleport Cellar",

	// One argument is the destination and the player is the
	// victim, so the arrival goes through enter_room — the look
	// is autolook_cmd's, not @teleport's.
	"@teleport #5",
	"@teleport #0",

	// Teleporting where you already are prints the confirmation
	// and nothing else, because enter_room resolves and
	// loop-checks the destination before testing for a self-move.
	"@teleport #0",

	// Two arguments, and the confirmation names both ends.
	"@teleport #4=#5",
	"@contents #5",
	"@teleport #4=me",
	"inventory",

	// A thing into a thing.
	"@create satchel",
	"@teleport #4=#7",
	"@contents #7",
	"@teleport #4=me",

	// HOME resolves per victim type, and @create set the thing's
	// home to the room it was made in.
	"@teleport #4=home",
	"@contents here",
	"@teleport me=home",

	// A room whose drop-to is not STICKY swallows a thing
	// teleported into it, and the confirmation names the drop-to
	// rather than the room that was asked for.
	"@link #5=#6",
	"@teleport #4=#5",
	"@contents #6",
	"@unlink #5",
	"@teleport #4=me",

	// A room is reparented rather than moved, and says so in its
	// own words. The second attempt is what proves the first took
	// effect.
	"@teleport #6=#5",
	"@teleport #5=#6",
	"@teleport #6=#0",

	// #0 cannot be moved, and a room's destination must be a
	// room: the two refusals are different sentences.
	"@teleport #0=#5",
	"@teleport #5=#4",
	"@teleport #5=me",

	// A program is simply put somewhere: no announcement, no
	// autolook, no drop-to.
	"@teleport #2=#5",
	"@contents #5",
	"@teleport #2=me",

	// An exit cannot be teleported, and cannot be matched as a
	// victim by name either — the victim search has no
	// match_all_exits, so reaching one needs its dbref.
	"@open out=#5",
	"@teleport out=#5",
	"@teleport #8=#5",

	// A player goes inside a thing only if it is a vehicle, and
	// only a wizard skips that test.
	"@teleport #4=here",
	"@teleport me=#4",
	"@set #4=V",
	"@teleport me=#4",
	"@teleport #0",
	"@set #4=!V",

	// Nothing may contain itself, and the message differs by
	// type. A player into a player is a bad destination instead.
	"@teleport #4=#4",
	"@teleport #5=#5",
	"@teleport me=me",

	// A registered name works wherever a dbref does.
	"@register #5=cellar",
	"@teleport #4=$cellar",
	"@teleport #4=me",

	// noisy_match_result's refusals, on each half in turn. An
	// empty second argument means the first *is* the destination,
	// so "@teleport" with nothing at all fails on the
	// destination.
	"@teleport nosuchthing=#5",
	"@teleport #4=nosuchplace",
	"@teleport",
	"@teleport =#5",

	// @teledump is compared exactly, so @te is the shortest
	// @teleport, and @t reaches nothing.
	"@te #4=#5",
	"@t",
}

// TestTeleportMatchesFuzzball checks do_teleport against the C
// server.
func TestTeleportMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	oracle, err := RunOracleSteps(ctx, fx, teleportScript, nil)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, teleportScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range teleportScript {
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
