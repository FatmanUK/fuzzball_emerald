package golden

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// `dump_users` (`interface.c:744`). Emerald had a table of its own
// invention — one format for everybody, its own column widths, its
// own header, no wizard mode, and `who_hides_dark` unread, so a DARK
// wizard was listed where the parameter defaults to hiding one.
//
// Two columns cannot agree and are masked rather than dropped. The
// **secure** marker is `@` here always, because every connection is
// TLS and `DESCRSECURE?` answers the same way for the same reason,
// while the oracle is driven over plain TCP inside a container. And a
// wizard row's **host** is whatever address each server sees.

// The two row shapes, anchored on the fixed columns before the
// INTERACTIVE and secure markers. The markers are `.{0,2}` rather
// than `..` because the harness right-trims every line, so a mortal
// row whose Doing is empty ends before them.
var (
	whoWizRow = regexp.MustCompile(
		`^(.*?\] .{10} .{4}).{0,2} (.*)$`)
	whoRow = regexp.MustCompile(`^(.{17} .{10} .{4}).{0,2}(.*)$`)
)

// maskWho blanks the two marker characters, and a wizard row's host.
func maskWho(s string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSuffix(line, "\r")
		m := whoWizRow.FindStringSubmatch(line)
		if m != nil {
			line = m[1] + "<mk> <host>"
			kept = append(kept, line)
			continue
		}
		if m = whoRow.FindStringSubmatch(line); m != nil {
			line = m[1] + "<mk>" + m[2]
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

var whoScript = Script{
	// The mortal table, which is what a wizard gets without the
	// star: `wizard` is only set by a `*` in the argument.
	"WHO",

	// The Doing column is GETDOING, cut to whatever is left of 79
	// once the name and the fixed columns have had theirs.
	"@doing me=watching the rain",
	"WHO",

	// The name filter is `string_prefix`, and a name it filters
	// out is still **counted** -- `++players` sits between the
	// who_hides_dark test and the filter.
	"WHO Wiz",
	"WHO nosuchplayer",

	// Leading spaces and stars are scanned together, so all of
	// these are the wizard table.
	"WHO *",
	"WHO  * ",
	"WHO *Wiz",

	// who_hides_dark defaults **true** and hides a DARK player
	// from the mortal table and from its count, while the wizard
	// table shows them.
	"@set me=D",
	"WHO",
	"WHO *",
	"@tune who_hides_dark=no",
	"WHO",
	"@set me=!D",

	// A quelled wizard is a mortal for the star scan, so the star
	// is ignored rather than refused.
	"@set me=Q",
	"WHO *",
	"@set me=!Q",

	// And the footer's high-water mark, which WHO itself raises
	// -- upstream's own comment calls this "an odd place to
	// update this variable".
	"@tune %who_hides_dark",
	"WHO",
}

// TestWhoMatchesFuzzball compares the two tables.
func TestWhoMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}
	script := whoScript
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
		if diffs := Compare(maskWho(want),
			maskWho(got)); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}
