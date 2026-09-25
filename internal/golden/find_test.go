package golden

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// findScript covers the four checkflags searches — @find, @owned,
// @contents and @entrances — which are one mechanism with four
// sources, so most of what is worth comparing is the flag expression
// and the six display modes rather than the four commands.
//
// The oracle's player is #1, a wizard, so the "somebody else's
// things" half of @owned is reachable; the control refusals are not,
// and are unit tests.
var findScript = Script{
	// A few things to find, of assorted types and flags. The
	// fixture starts with #0 the room, #1 the wizard, #2 the
	// program and #3 its exit.
	"@create widget",
	"@create wingnut",
	"@dig Workshop",
	"@open outward;out=#0",
	"@set widget=D",
	"@set wingnut=S",

	// The plain search, and the wrapping in "*...*" that makes a
	// substring match.
	"@find",
	"@find wid",
	"@find nut",
	"@find nosuchthing",
	// The wildcards a player writes work too, since the pattern
	// goes through smatch.
	"@find w*t",

	// Type filters.
	"@find =R",
	"@find =T",
	"@find =E",
	"@find =P",
	"@find =F",
	"@find =!T",

	// Flag filters, and negation.
	"@find =D",
	"@find =!D",
	"@find =S",
	"@find =U",
	"@find =!U",

	// The display modes. "locations" is tested before "links", so
	// "=l" is locations and "=li" is links.
	"@find wid=",
	"@find =T=owners",
	"@find =T=o",
	"@find =T=locations",
	"@find =T=l",
	"@find =T=links",
	"@find =T=li",
	"@find =E=links",
	"@find =T=count",
	"@find =T=c",
	"@find =T=nonsense",
	// The mode comes after a *second* "=", because the first
	// separates the name from the flags and init_checkflags
	// splits what is left again. So "@find wid=owners" reads
	// "owners" as six flag letters and finds nothing.
	"@find wid=owners",
	"@find wid==owners",
	"@find wid==count",
	"@find wid==size",

	// @owned: mine, then somebody else's, then a name that is not
	// a player.
	"@owned",
	"@owned =T",
	"@owned One",
	"@owned One=owners",
	"@owned nosuchplayer",

	// @contents: here, a named object, and the
	// contents-then-exits order.
	"@contents",
	"@contents here",
	"@contents #0",
	"@contents #0=E",
	"@contents me",
	"@contents widget",
	"@contents nosuchthing",

	// @entrances: what points at a thing. #0 is what the new exit
	// leads to, and the players and things that call it home.
	"@entrances",
	"@entrances here",
	"@entrances #0=E",
	"@entrances #0=T",
	"@entrances #0=count",
	"@entrances nosuchthing",

	// The abbreviations. @cont needs five characters and @conl is
	// the lock; @ow reaches @owned rather than @ownlock.
	"@fi wid",
	"@ow",
	"@cont",
	"@ent",
}

// TestSearchCommandsMatchFuzzball checks the four against the C
// server.
func TestSearchCommandsMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, findScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, findScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range findScript {
		var want, got string
		if i < len(oracle) {
			want = oracle[i]
		}
		if i < len(emerald) {
			got = emerald[i]
		}
		if diffs := Compare(maskSize(want),
			maskSize(got)); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}

// sizeColumn is display_objinfo's "=size" mode, the one of the six
// that cannot be reproduced: it reports size_object's byte count, and
// this server's objects are laid out nothing like the C's. The same
// judgement as examine's "Memory used" line, which is masked for
// exactly that reason — Emerald prints the object alone, and the
// column is masked off here rather than the case being dropped, so
// the line keeps its place in the transcript.
var sizeColumn = regexp.MustCompile(`\s+\d+ bytes\.$`)

func maskSize(s string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSuffix(line, "\r")
		kept = append(kept,
			sizeColumn.ReplaceAllString(line, ""))
	}
	return strings.Join(kept, "\n")
}
