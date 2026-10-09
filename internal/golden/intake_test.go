package golden

import (
	"context"
	"testing"
)

// `process_command` trims the **front** of a line and nothing else.
//
// `skip_whitespace(&command)` (`game.c:610`) is the only trim of the
// whole line; the end of it survives into the argument split, where
// `arg1` loses its trailing whitespace to `remove_ending_whitespace`
// and `arg2` keeps all of its own — `skip_whitespace_var(&arg2)` is
// a left trim only (`game.c:708`).
//
// This server ran `strings.TrimSpace` on the line at intake, so the
// **second argument of every "="-taking command lost its trailing
// space**: what a property holds, and whether an exit matches at all.
//
// Three further details go with the fix. The command word is cut at
// the first *whitespace*, not at the first space, so a tab-separated
// line was one word here. `init_match` (`match.c:63`) trims nothing,
// where `match.New` trimmed both ends — so a trailing space was
// invisible to every matcher, including the exit match that decides
// whether "out " goes anywhere. And the trims are C's `isspace`, not
// Unicode's.
//
// **The harness cannot see trailing whitespace.** `Normalize`
// right-trims every transcript line, which is why this divergence
// survived three tranches of golden cases — so every probe here
// either reads a value back through MUF with brackets round it, or
// turns on a *message* rather than on the text itself.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const intakeSource = `: ts[ s -- ] me @ s @ notify ;
: b[ s -- ] "[" s @ strcat "]" strcat ts ;
: main
  ( What each @set actually stored. The brackets are the point:
    Normalize would otherwise trim the spaces away before the two
    transcripts were compared. )
  #4 "_kept" getpropstr b
  #4 "_bare" getpropstr b

  ( Which name the third set used. arg1 is right-trimmed, but the
    property NAME lives in arg2 and keeps its spaces until @set's
    own two trims deal with it -- so this is "_trim" and the
    spaced spelling does not exist. )
  #4 "_trim" getpropstr b
  #4 "_trim   " getpropstr b

  ( A description set with a trailing space keeps it. )
  #5 "_/de" getpropstr b

  ( And a NAME does not, because a name is arg1 -- while a name
    set through arg2 keeps it, which is the contrast that shows
    which half of the split each one comes from. )
  #5 name b
  #6 name b
;`

var intakeScript = Script{
	"@tune penny_rate=0",
	"@create thing", // #4
	"drop thing",

	// arg2 keeps its trailing whitespace...
	"@set thing=_kept:value   ",
	"@set thing=_bare:value",

	// ...and @set's own trims, not the dispatcher's, are what
	// shorten a property name.
	"@set thing=_trim   :value",

	// arg1's right trim is what lets this match at all; without
	// it the name carries three spaces and nothing answers.
	"@describe thing   =trimmed arg1",

	// arg1 loses its own, so this names "spaced"...
	"@create spaced   ", // #5

	// ...and the same name reached through arg2 keeps it, because
	// `do_name` hands arg2 to ok_object_name as it stands. This
	// trimmed it, undoing the intake rule for the one command
	// whose whole argument *is* a name.
	"@create other", // #6
	"@name other=spaced2   ",

	// An empty new name says "Give it what **new** name?" -- this
	// said "Give it what name?".
	"@name thing=",

	// Renaming a **player** takes the player's own password as a
	// second word, which this did not ask for at all, and says so
	// in two lines. The name is cut at the first whitespace.
	"@pcreate Bob=secret", // #7
	"@name *Bob=Robert",
	"@name *Bob=Robert wrong",
	"@name *Bob=Robert secret",
	"@name *Robert=Robert secret",

	"@describe spaced=a thing   ",

	// Leading whitespace is skipped, so this is still a look.
	"   look",

	// A line of nothing but whitespace is dropped before anything
	// is dispatched — no "Huh?".
	"    ",

	// The command word is cut at the first whitespace, not the
	// first space.
	"look\tthing",

	// `full_command` skips exactly one character after the
	// command word, and that character may be a tab.
	"say\thello",

	// An exit is matched against the line as left-trimmed only,
	// so a trailing space is part of the name it has to match.
	"@open out", // #8
	"@link out=#0",
	"out",
	"out   ",
	"   out",

	// say and pose read `full_command`, so everything past that
	// one skipped character survives at both ends. The quotes
	// round what say prints make the spaces visible; pose's are
	// at the end of the line and the harness cannot see them, so
	// only its *leading* ones are compared.
	"say  hello   ",
	"pose   waves",
	`"  quoted   `,
	":  posed",

	"test",
}

// TestIntakeTrimMatchesFuzzball compares the ladder.
func TestIntakeTrimMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), intakeSource)
	if err != nil {
		t.Fatal(err)
	}
	script := intakeScript
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
