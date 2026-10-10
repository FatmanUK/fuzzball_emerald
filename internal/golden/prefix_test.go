package golden

import (
	"context"
	"testing"
)

// `process_command`'s head (`game.c:616-671`), where three `@tune`
// parameters had no reader at all: `enable_prefix`,
// `cmd_only_overrides` and `m3_huh`. This server hard-coded the clear
// branch of the first, short-circuited the two shortcuts straight
// into their commands rather than rewriting the line, and had no
// third shortcut.

var prefixScript = Script{
	// The default: the shortcuts are expanded **before** exit
	// matching, so the expanded form is what the matcher sees.
	`"hello`,
	":waves",

	// The third shortcut, which expands to a command that does
	// not exist. `;hello` therefore reaches "Huh?" by way of
	// `delimiter hello` -- and the rewrite is still observable,
	// because the expanded form is offered to the exit matcher.
	";hello",
	"@open delimiter hello",
	";hello",

	// The override token is tested *after* the expansion, so it
	// suppresses the shortcuts outright: the command word becomes
	// the token itself.
	`!"hello`,
	"!:waves",
	"!;hello",

	// ...and `command++` skips the token and **not** the
	// whitespace after it, so a space leaves an empty command
	// word.
	"! @create spaced",

	// With enable_prefix set, exit matching gets the raw line
	// first -- so an exit really can be named `"quoted`.
	"@open \"quoted",
	`"quoted`,
	"@tune enable_prefix=yes",
	`"quoted`,
	// The expansion still happens when no exit matched, and the
	// expanded form is offered to the matcher a second time.
	`"hello`,
	":waves",
	";hello",
	"@tune %enable_prefix",
	`"quoted`,

	// m3_huh answers an unknown command with an **exit**, named
	// "HUH? " plus the command *word* and matched at priority 3
	// -- so an ordinary exit of that name is not enough, which is
	// the whole point: any world could otherwise capture every
	// typo.
	"flibble",
	"@action HUH? flibble=here",
	"flibble",
	"@tune m3_huh=yes",
	"flibble",
	"@set HUH? flibble=3",
	"flibble",
	// The word, not the line: an argument is matched as the
	// exit's own argument rather than part of its name.
	"flibble sideways",
	"@tune %m3_huh",
	"flibble",

	// cmd_only_overrides turns **every** built-in off, leaving
	// them reachable only through a true wizard's '!' -- which
	// includes @tune, so a wizard who sets it cannot unset it the
	// ordinary way.
	"@tune cmd_only_overrides=yes",
	"@create thing",
	"look",
	`"hello`,
	"@tune %cmd_only_overrides",
	"!@create thing",
	"!@tune %cmd_only_overrides",
	"@create another",
}

// TestPrefixAndOverridesMatchFuzzball compares the ladder.
func TestPrefixAndOverridesMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}
	script := prefixScript
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
