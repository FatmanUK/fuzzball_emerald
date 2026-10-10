package golden

import (
	"context"
	"testing"
)

// `process_input`'s byte filter (`interface.c:3637`), which Emerald
// did not have: every byte a client sent reached the command parser.
// `tab_input_replaced_with_space` defaults **true** and had no
// reader, so a tab arrived as a tab.
//
// `say` is the probe because it prints `full_command` back untrimmed,
// so what survives the filter is visible exactly as it arrived.

var inputFilterScript = Script{
	// A tab becomes a space by default.
	"say a\tb",

	// ...and is kept when the parameter is off, which is visible
	// because `say` prints the line back rather than a word of
	// it.
	"@tune tab_input_replaced_with_space=no",
	"say a\tb",
	"@tune %tab_input_replaced_with_space",
	"say a\tb",

	// Backspace and delete remove the previous character, which
	// is a *line* edit and not a character the parser sees.
	"say ab\bc",
	"say ab\x7fc",

	// `isinput` is `isprint(q & 127)` -- the byte is masked to
	// seven bits for the test and stored **unmasked**. So 0xE9
	// survives, because 0x69 is printable, and 0x81 is dropped,
	// because 0x01 is not. That mask is what lets a high byte
	// into a name at all.
	"say caf\xe9",
	"say caf\x81e",

	// A stray carriage return is dropped like anything else
	// unprintable, rather than being part of what was said.
	"say a\rb",
}

// TestInputFilterMatchesFuzzball compares the ladder.
func TestInputFilterMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}
	script := inputFilterScript
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
