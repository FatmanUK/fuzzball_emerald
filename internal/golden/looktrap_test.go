package golden

import (
	"context"
	"testing"
)

// Look traps are `do_look_at`'s second branch (`look.c:369-439`) and
// this server did not have it: a `_details` propdir is how a world
// describes a part of something, and `look <thing>=<detail>` was a
// syntax Emerald did not accept at all.
//
// Two things about the mechanism decide most of these cases.
//
// `exit_prefix` (`fbstrings.c:120`) is **not** a prefix test despite
// its name — it walks the `;`-separated aliases of the property
// name and wants the typed word to equal one of them whole. Its truth
// table was produced by compiling it and running it; see
// `internal/game/exitprefix_test.go`.
//
// And the walk stops at the **second** match rather than choosing
// between them, so two traps that both answer are ambiguous. That is
// reachable from a transcript where an ambiguous *object* is not:
// `choose_thing` tosses a coin for two exact matches, but two
// half-matching names are reported as ambiguous deterministically.
//
// No parentheses inside the MUF comments below — there are none,
// because this case needs no program.
var lookTrapScript = Script{
	// A trap on the room, and one with two aliases.
	"@propset here=str:_details/feh:A faint scratch.",
	"@propset here=str:_details/bar;baz:Two names, one trap.",

	// The plain case, and the one that shows it is not a prefix
	// test: "fe" matches nothing, so the no-match message comes
	// back instead.
	"look feh",
	"look fe",
	"look FEH",

	// Either alias answers; a prefix of one does not.
	"look bar",
	"look baz",
	"look ba",

	// Two distinct traps both answer their own names.
	"@propset here=str:_details/dup1:First.",
	"@propset here=str:_details/dup2:Second.",
	"look dup1",
	"look dup2",

	// Two traps that both match *one* typed word are ambiguous,
	// and the walk stops at the second rather than choosing
	// between them. Because the match is exact, the only way two
	// can collide is a shared alias — which is what makes this
	// branch reachable at all.
	"@propset here=str:_details/alpha;shared:First shared.",
	"@propset here=str:_details/beta;shared:Second shared.",
	"look shared",
	"look alpha",
	"look beta",

	// A trap whose value is not a string is not run, and the
	// no-match message comes back as if it were not there.
	"@propset here=int:_details/num:42",
	"look num",

	// The second syntax: an object, and a detail on it.
	"@create widget",
	"drop widget",
	"@propset widget=str:_details/rim:A worn rim.",
	"look widget=rim",
	"look widget=ri",
	"look widget=nosuch",

	// An object of the same name beats a trap outright: the match
	// is tried first and only a *failed* one reaches the details.
	// This is the shape of upstream's own @TODO at look.c:380 —
	// a trap sharing a name with something matchable cannot be
	// reached by "look <name>" at all.
	"@propset here=str:_details/widget:A trap, not the widget.",
	"look widget",

	// And a detail on something with no _details at all, which is
	// the description_default branch rather than a no-match.
	"@create plain",
	"drop plain",
	"look plain=anything",

	// With nothing matched and nothing trapped, the message is
	// still match_msg_nomatch — the behaviour this server had
	// before, which the new branch must not have changed.
	"look nosuchthing",

	// An empty name shows the room whatever the detail says,
	// because upstream tests name alone before matching.
	"look =feh",

	// An ambiguous *name* reports upstream's odd message: it
	// passes the detail rather than the name, so with no detail
	// the quoted word is empty.
	"@create ambig-one",
	"drop ambig-one",
	"@create ambig-two",
	"drop ambig-two",
	"look ambig-",
	"look ambig-=rim",
}

// TestLookTrapsMatchFuzzball compares the whole ladder.
func TestLookTrapsMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}
	script := lookTrapScript
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
