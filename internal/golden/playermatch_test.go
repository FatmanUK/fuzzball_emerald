package golden

import (
	"context"
	"testing"
)

// `match_player` (`match.c:252`) matches a player from anywhere in
// the game, and **its whole body is guarded by the leading star**:
//
//	if (*(md->match_name) == LOOKUP_TOKEN
//	    && payfor(OWNER(md->match_from), tp_lookup_cost)) {
//
// `Matcher.Player` treated the '*' as a prefix to strip if present,
// so every search carrying the stage resolved a bare player name from
// across the database. Four commands in one earlier script answered
// "I don't understand 'Bob'." upstream and succeeded here, which is
// how this was found.
//
// The stage appears in `match_everything` for a wizard, in
// `@teleport`'s victim match, in `@tune`'s dbref matcher, in
// `parse_boolexp`'s lock keys, in `look`'s own short list, in MPI's
// resolver, and in `@give`, `page` and `@toad`. Each probe below is
// the same command twice, bare and starred.
//
// Bob is put somewhere else so that no stage looking *nearby* can
// answer for him. A name that resolves either way would compare
// nothing.
const playerMatchSource = `: main 1 pop ;`

var playerMatchScript = Script{
	// `penny_rate` gives a mover a chance of finding a penny
	// (`enter.go:440`), both servers roll their own dice, and Bob
	// is teleported three times below -- so any probe that shows
	// his money is nondeterministic. Turning it off makes the
	// whole case deterministic, which is worth doing in any
	// fixture that moves somebody and then counts anything.
	"@tune penny_rate=0",

	"@pcreate Bob=secret", // #4
	"@dig Far",            // #5
	"@teleport *Bob=#5",

	// match_everything's player stage, through @describe's
	// match_controlled.
	"@describe Bob=a person",
	"@describe *Bob=a person",

	// @teleport's victim match takes match_player
	// unconditionally. Bob is sent straight back afterwards:
	// while he stands in the room, match_neighbor answers for a
	// bare name and every probe below compares nothing.
	"@teleport Bob=#0",
	"@teleport *Bob=#0",
	"@teleport *Bob=#5",

	// parse_boolexp's lock keys, which is a different list again
	// and reports a failed match itself -- "I don't see X here."
	// -- before the caller's own "I don't understand that key."
	"@create gem",
	"drop gem",
	"@lock gem=Bob",
	"@lock gem=*Bob",
	"@readlock gem=Bob",
	"@readlock gem=*Bob",
	"@linklock gem=Bob",
	"@linklock gem=*Bob",

	// @tune's dbref matcher: match_absolute, match_registered,
	// match_player, match_me, match_here and nothing else. A
	// failed match is bad *syntax* and a wrong type is a bad
	// *value*, so the two answers differ.
	"@tune lost_and_found=Bob",
	"@tune lost_and_found=*Bob",

	// look builds its own short list, match_absolute plus
	// match_player.
	"look Bob",
	"look *Bob",

	// give's target match.
	"give Bob=1",
	"give *Bob=1",

	// And the one that must NOT change: a thing in the room is
	// still found by its bare name, because the other stages
	// answer for it.
	"look gem",
	"@describe gem=a gem",

	// examine shows what landed where.
	//
	"examine *Bob",
	"examine gem",

	// The other half of match_player: `payfor(OWNER(match_from),
	// tp_lookup_cost)` runs **before** the lookup, so the attempt
	// is charged whether or not it finds anybody, and a searcher
	// who cannot afford it finds nothing. Nothing here read the
	// parameter. A wizard pays for nothing, so #1 quells first.
	// "score" rather than "examine", because it is one line. The
	// fixture's wizard starts with nothing, so the money has to
	// be minted first -- which a wizard can do with "give".
	//
	// A **lock key**, because almost every other site gates
	// match_player on `Wizard(OWNER(player))` -- `look.c:283` and
	// `pennies.c:50` both do -- so a quelled wizard never reaches
	// the stage there, and an unquelled one pays for nothing.
	// `parse_boolexp` (`boolexp.c:406`) is one of the few that
	// takes it unconditionally, which makes it the only place the
	// charge can be watched. Two lookups at 40, and the third
	// cannot be afforded.
	"@tune lookup_cost=40",
	"give me=100",
	"@set me=quell",
	"score",
	"@lock gem=*Bob",
	"score",
	"@lock gem=*Bob",
	"score",
	"@lock gem=*Bob",
	"score",
	"@set me=!quell",
	"@tune lookup_cost=0",

	// The control, and the reason this is a *matcher* fix rather
	// than a parsing one: `@toad` takes its victim through
	// `lookup_player` and not through the matcher at all, so a
	// bare name is right there. Last, because it cannot be
	// undone.
	"@toad Bob",
	"@toad *Bob",
}

// TestPlayerMatchStarMatchesFuzzball compares the ladder.
func TestPlayerMatchStarMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), playerMatchSource)
	if err != nil {
		t.Fatal(err)
	}
	script := playerMatchScript
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
