package golden

import (
	"context"
	"testing"
)

// `ok_object_name` (`db.c:1690`) had only its type-independent half
// ported, so four `@tune` parameters had no reader anywhere:
// `reserved_names`, `reserved_player_names`, `7bit_other_names` and
// `7bit_thing_names`. The last defaults **true**, so an unconfigured
// world accepted high-byte thing names upstream refuses.
//
// Everything here is wizard-reachable, so the whole ladder is
// oracle-compared, including the one rung that was a guess until the
// oracle answered it: whether `isprint` lets a high byte into a
// player name. It does not.

// highByte is a name with a byte above 127 in it, which is what both
// 7-bit parameters test for. Written as an escape so it is one byte
// rather than a UTF-8 pair, and so nothing reformats it.
const highByte = "caf\xe9"

// highByte2 is a second one, because `@program` *finds or creates*:
// given a name something else already has it matches that and refuses
// for the wrong reason, which is how this rung came to compare
// "Permission denied!" on both servers and test nothing.
const highByte2 = "na\xefve"

var okNameScript = Script{
	// create_player has exactly two refusals and every one of its
	// four callers prints them verbatim. This server had five
	// messages of its own invention, and each caller printed a
	// different subset.
	"@pcreate me=secret",
	"@pcreate Bob=secret",
	"@pcreate Bob=secret",

	// A password with a space in it: ok_password is printable and
	// non-space throughout, and @pcreate cuts only at the '='.
	"@pcreate Carol=two words",

	// player_name_limit is 16.
	"@pcreate Verylongnameindeed=secret",

	// ok_player_name's character rule is `isgraph`, written as
	// `isprint(c) && !isspace(c)` -- and `isprint` in the C
	// locale is false above 126, so a player name can never hold
	// a high byte whatever the two 7-bit parameters say. That is
	// why there is no `7bit_player_names` beside them.
	"@pcreate " + highByte + "=secret",

	// reserved_player_names applies to players only, and is
	// `equalstr` -- so it is an smatch pattern and nothing else.
	"@tune reserved_player_names=Dave*",
	"@pcreate Dave=secret",
	"@pcreate Davidson=secret",
	"@pcreate Edward=secret",
	"@create Dave",

	// reserved_names applies to everything, and is tested before
	// the type switch -- so it refuses a player as well.
	//
	// The pattern is a plain "*" rather than an alternation on
	// purpose: `{a|b}` is a **word** pattern in upstream's smatch
	// and matches nothing here, which is a divergence of its own
	// and is not this case's subject.
	"@tune reserved_names=gold*",
	"@create goldfish",
	"@dig goldmine",
	"@pcreate Goldie=secret",
	"@create bronze",

	// do_name's else arm, which was missing outright: anything
	// that is not a player could be renamed to anything at all.
	"@name bronze=#bad",
	"@name bronze=me",
	"@name bronze=brass",

	// 7bit_thing_names defaults **true** and 7bit_other_names
	// defaults false, so out of the box a thing refuses a high
	// byte and a room accepts one.
	"@create " + highByte,
	"@dig " + highByte,
	"@tune 7bit_other_names=yes",
	"@dig " + highByte,
	"@tune 7bit_thing_names=no",
	"@create " + highByte,

	// And the parameters are read per creator, not once: an exit
	// and a program are "other" names too.
	"@tune 7bit_other_names=yes",
	"@open " + highByte,

	// A program is an "other" name too -- but `@program` is gated
	// on the *player's* mucker level, and the oracle's wizard has
	// none, so without this the rung would compare a permission
	// refusal and test nothing about names.
	"@set me=3",
	"@program " + highByte2,
}

// TestOkObjectNameMatchesFuzzball compares the ladder.
func TestOkObjectNameMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}
	script := okNameScript
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
