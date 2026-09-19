package golden

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// examineScript examines one of each type of object, with and without flags,
// messages and properties set.
var examineScript = Script{
	// A room, a player and a program are already in the fixture.
	"ex here",
	"ex me",
	"ex test.muf",
	"ex test",

	// A thing, dressed up so every part of the report has something to
	// show: flags, messages, a lock, properties and a home.
	"@create widget",
	"@set widget=V",
	"@set widget=D",
	"@set widget=_/de:A small brass widget.",
	"@set widget=_/sc:It whirrs.",
	"@set widget=_/osc:whirrs at it.",
	"@set widget=_/fl:It refuses.",
	"@set widget=_/do:idle",
	"@set widget=colour:brass",
	"@set widget=size/height:3",
	"ex widget",

	// Property listing, which is the other half of the command.
	"ex widget=/",
	"ex widget=**",
	"ex widget=size/",
	"ex widget=nosuch*",
	"ex me=/",

	// A second room with an exit and a drop-to, to reach the branches the
	// fixture's own room does not.
	"@dig Cellar",
	"@dig Attic=#0",
	"@dig Vault=nosuchroom",
	"@open down=#0",
	"ex here",
	"ex down",

	// What someone sees of an object they do not control.
	"ex #-1",
	"ex nosuchthing",
}

// TestExamineMatchesFuzzball compares examine's whole report.
func TestExamineMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, examineScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, examineScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range examineScript {
		var want, got string
		if i < len(oracle) {
			want = oracle[i]
		}
		if i < len(emerald) {
			got = emerald[i]
		}
		if diffs := Compare(maskVariable(want), maskVariable(got)); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}

var (
	// timestampLine is one of examine's three time fields.
	timestampLine = regexp.MustCompile(`^(Created:|Modified:|Lastused:)\s+.*$`)
	// memoryLine is the size report, which counts this server's own
	// memory. The two lay an object out differently, so the numbers could
	// not agree even in principle; that the line is there, and where, is
	// what is compared.
	memoryLine = regexp.MustCompile(`^Memory used: \d+ bytes$`)
	// runtimeLine is a program's cumulative runtime, which upstream
	// profiles and this server does not.
	runtimeLine = regexp.MustCompile(`^Cumulative runtime: .*$`)
)

// maskVariable replaces the parts of a report that cannot match between two
// servers with a fixed string, keeping the line and its position.
func maskVariable(s string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		// The oracle's transcript keeps its line endings, so the
		// carriage return goes before anything is matched.
		line = strings.TrimSuffix(line, "\r")
		switch {
		case timestampLine.MatchString(line):
			line = timestampLine.ReplaceAllString(line, "$1 <time>")
		case memoryLine.MatchString(line):
			line = "Memory used: <n> bytes"
		case runtimeLine.MatchString(line):
			line = "Cumulative runtime: <t>"
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
