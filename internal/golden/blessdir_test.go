package golden

import (
	"context"
	"testing"
)

// `@bless` blesses property **directories** as well as properties,
// and this could not: the flag lived inside the value, and a
// directory has no value.
//
// `blessprops_wildcard` (`wiz.c:327`) walks with
// `first_prop`/`next_prop`, which enumerate every node, and
// `set_property_flags` (`property.c:177`) flags whatever
// `get_property` returns — including a node that is only a
// directory. So `@bless obj=**` blesses every level of a tree, the
// count line says so, and `displayprop` renders the blessed character
// on a directory line as readily as on a value (`property.c:1340`),
// which this hardcoded as "-".
//
// `docs/upstream-coverage.md` recorded it as needing a flag that can
// live on a valueless node. It is `node.blessed` now, and
// `Value.Blessed` is a view of it, so the two cannot disagree.
const blessDirSource = `: main 1 pop ;`

var blessDirScript = Script{
	"@tune penny_rate=0",

	// A tree two levels deep: "_d" and "_d/b" are directories
	// with no values of their own.
	"@set me=_d/a:one",
	"@set me=_d/b/x:deep",
	"@set me=_d/c:three",
	"@set me=_d/c/y:also deep",
	"examine me=**",

	// "**" from the root blesses everything it reaches, and the
	// count includes the directories.
	"@bless me=**",
	"examine me=**",

	// And takes it all off again.
	"@unbless me=**",
	"examine me=**",

	// A narrower pattern: "_d/**" recurses *inside* "_d" and so
	// leaves "_d" itself alone, because the first segment is
	// matched before the recursive one is reached.
	"@bless me=_d/**",
	"examine me=**",
	"@unbless me=_d/**",

	// And a pattern naming one directory blesses that and nothing
	// under it.
	"@bless me=_d",
	"examine me=**",
	"@unbless me=_d",
	"examine me=**",
}

// TestBlessDirMatchesFuzzball compares the ladder.
func TestBlessDirMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), blessDirSource)
	if err != nil {
		t.Fatal(err)
	}
	script := blessDirScript
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
