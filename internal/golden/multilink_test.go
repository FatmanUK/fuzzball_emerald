package golden

import (
	"context"
	"strings"
	"testing"
)

// An exit's destinations are a **list**, written as one ';'-separated
// argument to `@link` or to `@open`'s second half.
//
// `_link_exit` (`db.c:2042`) walks that argument and links every
// destination it can, reporting each one as it goes — so the
// command says "Linked to X." once per destination rather than once
// in total. This linked only the first, and `trigger` has always
// traversed a list: an exit that fetches three things, which is what
// the list is for, could not be built from inside the game at all.
//
// Four of its refusals are per-destination, and each names the one it
// skipped: a second player, room or program; an exit that would close
// a loop; a name that cannot be resolved, which `parse_linkable_dest`
// reports for itself; and the MAX_LINKS bound, which **ends** the
// loop rather than skipping an entry.
//
// `@relink` checks every destination before it breaks the existing
// link, and in that mode a skipped one fails the whole command rather
// than being carried past.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears. Every probe here is a command, so the
// fixture's program is never run and is the shortest thing that
// compiles.
const multiLinkSource = `: main 1 pop ;`

var multiLinkScript = Script{
	"@tune penny_rate=0",

	"@create rock", // #4
	"drop rock",
	"@create stick", // #5
	"drop stick",
	"@dig Hall", // #6
	"@dig Cave", // #7

	// Three things and a room: every "Linked to" line shows, and
	// the room is allowed because it is the first of its kind.
	"@open fetch", // #8
	"@link fetch=rock;stick;#6",
	"examine fetch",

	// A second room is refused by name, and the exit keeps what
	// it already had.
	"@open two", // #9
	"@link two=#6;#7",
	"examine two",

	// A name that resolves to nothing is reported by
	// parse_linkable_dest and skipped; the rest still link.
	"@open skip", // #10
	"@link skip=rock;nosuchplace;stick",
	"examine skip",

	// An exit that would close a loop is skipped by name, which
	// is a different message from MUF SETLINK's.
	"@open ring1", // #11
	"@open ring2", // #12
	"@link ring1=#12",
	"@link ring2=#11;rock",
	"examine ring2",

	// Nothing resolvable at all is "No destinations linked." and
	// a refund.
	"@open none", // #15
	"@link none=nosuchplace",

	// HOME is appended and announced without going through the
	// type switch at all -- upstream reaches `Typeof(HOME)`
	// there, which indexes three objects before the start of its
	// database, so only what a transcript can see is worth
	// reproducing. A list mixing HOME with a room is deliberately
	// not probed: whether HOME sets the one-room flag is that
	// allocator's business.
	"@open homed=home", // #13
	"examine homed",

	// NIL is announced by its own line and skips the generic one,
	// so a list holding it says two different things.
	"@open nilfirst", // #14
	"@link nilfirst=nil;rock",
	"examine nilfirst",

	// @open's own second argument takes the same list.
	"@open both=rock;stick", // #16
	"examine both",

	// And @relink checks the whole list first: a second room
	// fails the command outright rather than linking what it
	// could.
	"@relink fetch=#6;#7",
	"examine fetch",
	"@relink fetch=stick;rock",
	"examine fetch",

	// MAX_LINKS is 50, and the bound **ends** the loop: the
	// fifty-first destination is not skipped, it is never
	// reached, and everything after it goes with it.
	"@open many=" + manyLinks(51),
	"examine many",
}

// manyLinks builds a ';'-separated argument naming the same thing n
// times. Each copy is a THING, so each one is appended and the
// MAX_LINKS bound is what answers rather than the one-room-or-player
// rule.
func manyLinks(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "rock"
	}
	return strings.Join(parts, ";")
}

// TestMultiLinkMatchesFuzzball compares the ladder.
func TestMultiLinkMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), multiLinkSource)
	if err != nil {
		t.Fatal(err)
	}
	script := multiLinkScript
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
