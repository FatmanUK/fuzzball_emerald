package golden

import (
	"context"
	"testing"
	"time"
)

// blessScript covers @bless, @unbless and @relink.
//
// Blessing is the one command that hands out privilege by pattern: a
// blessed property's MPI runs with the permissions of whoever blessed
// it rather than of whoever triggers it. So which properties a
// pattern reaches is a security question, not a convenience.
var blessScript = Script{
	"@create widget",
	"@propset widget=:one:first",
	"@propset widget=:two:second",
	"@propset widget=:deep/inner:nested",
	"@propset widget=:deep/further/down:deeper",

	// One property, then a wildcard narrow enough to reach only
	// value-bearing ones.
	//
	// Nothing here uses "*", "**" or a bare propdir. Upstream
	// also blesses *directories*, which Emerald does not — see
	// blessMatches — so a pattern that reaches one diverges in
	// its count and in the marker examine prints.
	"@bless widget=one",
	"examine widget=/",
	"@unbless widget=one",
	"@bless widget=t*",
	"examine widget=/",
	"@unbless widget=t*",
	"@bless widget=deep/inner",
	"examine widget=/deep/**",
	"@unbless widget=deep/inner",

	// A pattern that matches nothing still reports a count, with
	// its plural agreed — and @unbless says "unblessed" where
	// @bless says "blessed".
	"@bless widget=nosuchprop",
	"@unbless widget=nosuchprop",
	"@bless widget=o*",
	"@unbless widget=o*",

	// The refusals.
	"@bless widget",
	"@bless widget=",
	"@bless nosuchthing=x",

	// @relink checks the new target before breaking the old link,
	// which is the whole point of the command.
	"@dig Workshop",
	"@dig Cellar",
	"@open north=#5",
	"@relink north=#6",
	"north",
	"@relink north=nosuchthing",
	"@relink north=#6",

	// Its refusals are worded differently from @link's for the
	// same conditions.
	"@relink widget=here",
	"@relink here=#5",
	"@relink test.muf=here",
	"@relink nosuchthing=here",

	// The abbreviations. @unb is the shortest @unbless, and @rel
	// reaches @relink.
	"@bl widget=one",
	"@unb widget=one",
	"@rel north=#5",
}

// TestBlessMatchesFuzzball checks @bless, @unbless and @relink
// against the C server.
func TestBlessMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, blessScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, blessScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range blessScript {
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
