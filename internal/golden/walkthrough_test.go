package golden

import (
	"context"
	"testing"
)

// A walkthrough transcribed from Fuzzball's own MUCK Manual
// (https://fuzzball-muck.github.io/muckman/toc.html), one script per
// chapter, driven by what the manual says a *user* does rather than
// by what we thought to test.
//
// Three things about the transcription, all of them deliberate.
//
// **The manual is older than Fuzzball 7 and is not a specification.**
// Its `@dig` reply is worded differently from 7's, and it lists
// `UPTIME`, `SWEEP` and `@RESTART` under *user-created programs*
// where all three are server commands in the C. So it supplies the
// **scenario** and the oracle supplies the expected output, which is
// how every golden case already works. Where the manual and the C
// disagree, the C wins.
//
// **The std-db programs it leans on are not here.** `$nothing`,
// `$obvex`, `lsedit` and `@$obvex` are MUF written by upstream's
// starter world, not server commands -- the manual says so itself.
// Where a script needs a do-nothing program to link to, it registers
// the fixture's own `test.muf` under that name, which is the same
// shape and says so at the call site.
//
// **Themed scripts, not one long one.** A divergence that shifts a
// dbref poisons every later reference, so each chapter gets its own
// fixture and `t.Run` names the failure.
//
// Two harness constraints the plan called for are deliberately
// absent. `@tune commands_per_time=1000` is unnecessary at this
// length: the oracle spends two command tokens a step and the default
// burst is 500, so throttling starts at ~250 steps and the longest
// script here is under 60. And `@tune penny_rate=0` is unnecessary
// because every script drives #1, who controls every room it walks
// through, and `maybeFindPenny` exempts exactly that.

// buildInnScript is §4.5 and its two tutorial pages, `btut2` (an
// environment room) and `btut3` (exits and their four message
// properties).
//
// The fixture is #0 the room, #1 the wizard, #2 test.muf and #3 the
// exit in front of it, so the first thing dug is #4.
var buildInnScript = Script{
	// btut2: an environment room, registered on the player --
	// @register writes `_reg/` on #1, not on #0 -- and flagged
	// ABODE so rooms dug beneath it inherit it as a parent.
	"@dig Amberside Environment Room",
	"@register #4 = aer",
	"@set $aer = A",

	// A room parented to it by registration. @dig's reply says
	// what it tried and what it got, which is two lines.
	"@dig Amberside Inn: Tavern = $aer",
	"@register #5 = ai",

	// The manual teleports in and then hangs a personal action on
	// itself to get back, which is @action attaching an exit to a
	// *named object* rather than to the room -- the thing that
	// separates @action from @open.
	"@tel me = $ai",
	"@act ai = me",
	"@link ai = $ai",

	// btut3: the west wing, and an exit with a display name and
	// three aliases. "<W>" is part of the name, not syntax.
	"@dig Amberside Inn: West Wing",
	"@open West Wing <W>;west wing;west;w = #6",
	"@desc w = A sturdy wooden door leading to rooms " +
		"on the west wing.",
	"@succ w = You pull open the door to the West Wing " +
		"hallway...",
	"@osucc w = goes into the West Wing.",
	"@odrop w = comes in from the Tavern.",

	// And walking it. The @succ shows, the @osucc reaches nobody
	// -- notify_except excludes the actor, which is what step 4's
	// puppet is for -- and the @odrop is read on arrival.
	"w",
	"@find tavern",

	// The way back. The manual's name begins "Tavern " with the
	// space inside it, before the first ';', which is why it
	// survives: only the whole of arg1 is trimmed.
	"@open Tavern ;tavern;t;east;e = #5",
	"@desc t = A strong wooden door.",
	"@succ t = You open the door into the inn's tavern.",
	"@osucc t = goes into the Tavern.",
	"@odrop t = comes out of the west hallway.",
	"t",
	"l",

	// The manual's `@act map;look map;loo map;lo map;l map = $ae`
	// links to `$nothing`, a std-db program. The fixture's own
	// program stands in under that name, which is the same shape
	// -- an exit secured by having somewhere to go.
	"@register #2 = nothing",
	"@act map;look map;loo map;lo map;l map = $ai",
	"@link map = $nothing",
	"@succ map = You unfold a map of the inn.",
	"map",

	// What the manual's `@succ here = @$obvex` would do cannot be
	// transcribed: $obvex is upstream's obvious-exits program and
	// this fixture has no std-db. A @succ naming a program *is*
	// ported, so the shape is checked with the program there is.
	"@succ here = @$nothing a map of the inn",
	"l",

	// And the two commands the chapter ends on that need a second
	// seat -- `p jessy = ...` and a bare `p` -- answer the way a
	// page to nobody answers, which is still worth comparing.
	"p jessy = Can you tell me the dbref of the obvious " +
		"exits program?",
	"p = OK, thanks.",

	// Examine the pair, so the four message properties and the
	// parent chain are read back rather than only written.
	"examine w",
	"examine here",
}

// TestWalkthroughMatchesFuzzball drives the manual's own scenarios
// through both servers.
func TestWalkthroughMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	for _, tc := range []struct {
		name   string
		script Script
	}{
		{"BuildAnInn", buildInnScript},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compareWalkthrough(t, tc.script)
		})
	}
}

// compareWalkthrough runs one script through both servers on its own
// fixture and diffs it step by step.
func compareWalkthrough(t *testing.T, script Script) {
	t.Helper()
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}
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
