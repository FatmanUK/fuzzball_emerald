package golden

import (
	"context"
	"testing"
	"time"
)

// speechScript covers the commands that take upstream's full_command
// rather than a trimmed argument: say, pose, @wall and gripe. What
// they print is what was typed, so the trimming is visible in all
// four.
//
// It also covers the two things about say and pose that are easy to
// assume and wrong: neither has an emptiness guard, and pose's space
// is omitted before any of four separator characters rather than
// before an apostrophe alone.
var speechScript = Script{
	// The ordinary cases first, so a divergence in the odd ones
	// is not just "everything differs".
	"say hello",
	"pose waves",
	`"hello`,
	":waves",

	// Leading whitespace after the command word survives, because
	// full_command skips exactly one character.
	"say   spaced out",
	"pose   spaced out",
	`"  spaced out`,
	":  spaced out",

	// And it survives inside the line too, which trimming would
	// not have touched but is worth pinning beside the rest.
	"say two  spaces  inside",

	// No emptiness guard on either.
	"say",
	"pose",
	`"`,
	":",

	// pose omits its space before a pose separator: an
	// apostrophe, a space, a comma or a hyphen.
	":'s hat is askew",
	":, yes indeed",
	":- definitely",
	":;not a separator",
	":.also not",

	// @wall and gripe print what was typed too. #1 is a wizard,
	// so both are reachable and both answer to the one
	// connection.
	"@wall   shouted with spaces",
	"gripe   griped with spaces",
	"gripe",
}

// TestSpeechMatchesFuzzball checks the full_command commands against
// the C server.
func TestSpeechMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, speechScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, speechScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range speechScript {
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
