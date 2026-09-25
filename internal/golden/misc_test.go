package golden

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// miscScript covers the five commands that needed no new engine:
// score, uptime, @trace, @uncompile and @wall.
//
// @wall is reachable here because the oracle's player is #1, who is a
// wizard, and the shout reaches only the one connection either server
// has.
var miscScript = Script{
	"score",

	// @trace walks the environment chain outwards. The fixture's
	// room is #0, which is its own top, so the interesting shapes
	// are a deeper object and the depth limit.
	"@trace",
	"@trace here",
	"@trace me",
	"@trace #1",
	"@trace me=1",
	"@trace me=0",
	"@trace nosuchthing",

	// @uncompile says one thing and then everything still runs,
	// which is the half worth checking.
	"@uncompile",
	"test",
	"@uncompile",

	"@wall hello everyone",
	"@wall",

	// uptime's own text cannot agree — the two servers started
	// at different moments — so only its shape is compared, by
	// the mask below.
	"uptime",

	// The abbreviations each is reachable by.
	"sc",
	"up",
	"@tr",
	"@uncom",
}

// TestMiscCommandsMatchFuzzball checks the five against the C server.
func TestMiscCommandsMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(),
		`: main me @ "the program ran" notify ;`)
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, miscScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, miscScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range miscScript {
		var want, got string
		if i < len(oracle) {
			want = oracle[i]
		}
		if i < len(emerald) {
			got = emerald[i]
		}
		if diffs := Compare(maskUptime(want),
			maskUptime(got)); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}

// uptimeLine is what uptime prints. Neither half of it can agree: the
// two servers started seconds apart and have been up for different
// lengths of time. What is compared is that the line is there, and
// that both halves parsed — timestr_long's spelling on the left and
// strftime's "%c %Z" on the right.
var uptimeLine = regexp.MustCompile(
	`^Up (|[0-9]+ [a-z]+s?(, [0-9]+ [a-z]+s?)*) since .+$`)

func maskUptime(s string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if uptimeLine.MatchString(line) {
			line = "Up <duration> since <time>"
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// restrictScript covers @restrict and gripe, which are together
// because both are short and neither needs a fixture of its own.
//
// @restrict really does shut the door, so it is turned back off
// before anything else runs — the oracle's own connection survives
// either way, since the test is applied at login.
var restrictScript = Script{
	"@restrict",
	// "on" and "off" are compared case-sensitively, so this
	// reports rather than setting.
	"@restrict ON",
	"@restrict on",
	"@restrict",
	"@restrict off",
	"@restrict",

	// gripe with a message thanks the complainer and tells every
	// connected wizard, which #1 is.
	"gripe the doors stick",
	"gripe and the floor creaks",

	// Reading them back. Upstream spits its log file and Emerald
	// renders its rows, so the timestamp cannot agree and is
	// masked; the rest of the line is upstream's own format.
	"gripe",
	"gr",
}

// TestRestrictAndGripeMatchFuzzball checks both against the C server.
func TestRestrictAndGripeMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, restrictScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, restrictScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range restrictScript {
		var want, got string
		if i < len(oracle) {
			want = oracle[i]
		}
		if i < len(emerald) {
			got = emerald[i]
		}
		if diffs := Compare(maskGripeTime(want),
			maskGripeTime(got)); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}

// gripeStamp is the timestamp on a logged gripe, which is the moment
// it was filed and so cannot agree between two servers.
var gripeStamp = regexp.MustCompile(
	`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d: GRIPE`)

func maskGripeTime(s string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSuffix(line, "\r")
		line = gripeStamp.ReplaceAllString(line,
			"<time>: GRIPE")
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
