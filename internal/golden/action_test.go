package golden

import (
	"context"
	"testing"
	"time"
)

// actionScript covers @action, @attach and @clone, plus the
// "=<regname>" every building command takes as a final argument.
//
// @action is the one that mattered: it was registered as an alias for
// @open, which is worse than being missing, because upstream attaches
// the exit to a named object rather than to the room.
var actionScript = Script{
	"@create widget",
	"@dig Workshop",

	// An action goes on a named object. The exit is not linked,
	// so using it does nothing until @link points it somewhere.
	"@action poke=widget",
	"@contents widget",
	"@contents here",
	"poke",
	"@link poke=#4",
	"poke",

	// The refusals, each its own sentence.
	"@action orphan=",
	"@action orphan=nosuchthing",
	"@action nested=poke",
	"@action onprog=test.muf",

	// @attach moves an existing action, and resets its priority
	// with a differently worded message from @unlink's.
	"@action mover=widget",
	"@attach mover=here",
	"@contents here",
	"@set mover=M3",
	"@attach mover=widget",
	"@attach nosuchaction=here",
	"@attach mover=",
	"@attach widget=here",

	// @clone copies a thing, its flags and its properties, and
	// charges what the original is worth.
	"@set widget=D",
	"@propset widget=:colour:green",
	"@clone widget",
	"examine widget",
	"@clone nosuchthing",
	"@clone here",
	"@clone test.muf",

	// The "=<regname>" argument, on each command that takes one.
	// It registers on the *player*, not on #0.
	//
	// Each thing is cloned at most once, because cloning leaves
	// two objects with the same name and choose_thing's last
	// resort for two exact matches is a *coin toss* (match.c:175)
	// — so naming one again could not agree between two
	// servers.
	"@create gizmo==g",
	"@dig Cellar==cel",
	"@open door=#4=dr",
	"@action prod=widget=pr",
	"@clone gizmo=cl",
	"@register #me",

	// A room dug inside an ABODE room is parented into it rather
	// than at the top of the world.
	"@set here=A",
	"@dig Inner",
	"examine Inner",
	"@set here=!A",

	// The abbreviations.
	"@ac nudge=widget",
	"@at nudge=here",
	"@create cloneme",
	"@clo cloneme",
}

// TestActionMatchesFuzzball checks @action, @attach and @clone
// against the C server.
func TestActionMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, actionScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, actionScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range actionScript {
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
