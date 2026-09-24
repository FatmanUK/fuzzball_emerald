package golden

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// helpScript drives the whole help system. Both servers are given the
// same texts — the C as files under its game directory, this one as
// corpora in its world — so the bodies can be compared and not only
// the wrapper around them.
//
// Bare "info" is deliberately absent: the C builds that listing from
// readdir, whose order is the filesystem's rather than anything
// either server decides. Its column arithmetic is pinned by a unit
// test against the rule ported from do_info.
var helpScript = Script{
	// The header block, which is what a bare command shows.
	"help",
	"news",
	"man",
	"mpi",

	// A topic, by its name and by each of its aliases. The "look"
	// block carries a blank line, which is how the two-space
	// rendering gets compared.
	"help look",
	"help l",
	"help read",
	"help LOOK",
	"help go",
	"news opening",
	"man stack",
	"mpi syntax",

	// Misses. The wording is what matters: players read it and
	// programs wrapping help match on it.
	"help nosuchtopic",
	"news nosuchtopic",
	"man nosuchtopic",
	"mpi nosuchtopic",

	// An index corpus matches a topic exactly; only info
	// prefix-matches.
	"help loo",

	// The info directory, which does prefix-match.
	"info server",
	"info newb",
	"info nosuchfile",

	// Segments cut a text to a line or a range, and only info
	// honours one: index_file never sees the segment, which is
	// why "help look=1" prints the whole block.
	"info building=1",
	"info server=1-1",
	"info newbie=1-",
	"info server=rubbish",
	"help look=1",
	"help look=rubbish",

	// The motd, and the wizard's half of it.
	"motd",
	"motd The east wing is closed.",
	"motd",
	"motd clear",
	"motd",

	"@credits",
}

// TestHelpMatchesFuzzball compares the help commands against the C.
func TestHelpMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, helpScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, helpScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range helpScript {
		var want, got string
		if i < len(oracle) {
			want = oracle[i]
		}
		if i < len(emerald) {
			got = emerald[i]
		}
		diffs := Compare(maskMOTDStamp(want), maskMOTDStamp(got))
		if len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}

// motdStamp is the date do_motd writes above each entry. Two servers
// running a second apart would disagree about it, and the stamp being
// there is what the comparison is for.
var motdStamp = regexp.MustCompile(
	`^[A-Z][a-z]{2} [A-Z][a-z]{2} \d\d .*\d{4}$`)

func maskMOTDStamp(s string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if motdStamp.MatchString(line) {
			line = "<motd timestamp>"
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
