package golden

import (
	"context"
	"testing"
	"time"
)

// ANSI gating: queue_ansi (interface.c:673) runs one of two different
// filters over every line on its way to a client, and which one
// depends on the player's COLOR flag — upstream's CHOWN_OK, which
// means something else on anything but a player.
//
// Emerald ran neither. A world that sent colour sent it to every
// client including those that had asked for none, and a malformed
// sequence went out as written.
//
// Writing this case turned up the larger half of the work: **almost
// every line of a MUF error report is coloured**, and none of it was
// being emitted. It had never shown up because the harness's player
// has no COLOR, so the oracle was stripping colour Emerald was not
// producing — two wrongs that cancelled in every transcript until
// one of them was fixed.
//
// The escapes come from MPI's {attr} and from the error report rather
// than being typed, so nothing here depends on a raw control
// character surviving the harness. The byte-level behaviour of the
// two filters is pinned in internal/ansi's own tests, against the C
// functions compiled and run directly. ansiErrSource fails with a
// message both servers word identically, which rules out most of
// them: see docs/upstream-coverage.md for the three that still
// differ.
const ansiErrSource = `: main pop 1 2 3 4 strcat ;`

var ansiScript = Script{
	// A description built with {attr}, which is the way a world
	// actually emits colour.
	"@describe here={attr:red,bold,A crimson room.}",

	// Without COLOR every escape is removed, which is the state
	// every player starts in.
	"look",
	"err",

	// With it, sequences are kept and made well-formed — and
	// the whole error report turns out to be coloured.
	"@set me=C",
	"look",
	"err",

	// "color" is the other spelling of the same flag, and
	// unsetting it goes back to a plain transcript.
	"@set me=!C",
	"look",
	"@set me=color",
	"look",
	"err",
	"@set me=!color",
	"look",

	// The flag means CHOWN_OK on anything but a player, so
	// setting it on a thing changes nothing about its output.
	"@create widget",
	"@set widget=C",
	"@describe widget={attr:blue,A blue widget.}",
	"look widget",
	"@set me=C",
	"look widget",
	"@set me=!C",

	// A lock's failure message goes through the same filter, so
	// an @fail with colour in it is stripped too.
	"@fail here={attr:green,Nope.}",
	"@lock here=me",
	"@unlock here",
	"@fail here=",
	"@describe here=",
}

// TestANSIGatingMatchesFuzzball checks queue_ansi's two filters
// against the C server.
func TestANSIGatingMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteMultiFixture(t.TempDir(), []Program{
		{Name: "err", Source: ansiErrSource},
	})
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, ansiScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, ansiScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range ansiScript {
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
