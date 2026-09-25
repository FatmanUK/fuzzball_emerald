package golden

import (
	"context"
	"testing"
	"time"
)

// execProgram reports both of the things exec_or_notify hands a
// program it runs: the initial stack argument, which is the
// MPI-evaluated remainder of the property, and the COMMAND variable,
// which is the caller context rather than anything the player typed.
//
// They are two different strings upstream — match_args and
// match_cmdname — so printing only one of them would not have caught
// the two being conflated.
const execProgram = `: main
  "arg[" swap strcat "]" strcat me @ swap notify
  "cmd[" command @ strcat "]" strcat me @ swap notify
;`

// execScript drives every branch of property.c's exec_or_notify. The
// fixture lays out #0 as the room, #1 as the wizard, #2 as the
// program above and #3 as its exit, so the dbrefs below are known
// before anything runs.
//
// The description goes on with @set rather than @describe, because
// @describe's own confirmation is one of the wordings
// set_standard_property still owes; the property is the same either
// way, and this case is about what reading it does.
var execScript = Script{
	"@create mirror",

	// The ordinary case that must not change: no '@', so the
	// value is MPI over text.
	"@set mirror=_/de:{name:me} looks back at you.",
	"look mirror",

	// A dbref naming a program. The argument is MPI-parsed before
	// the program sees it, and the program does all the talking —
	// exec_or_notify adds nothing of its own.
	"@set mirror=_/de:@2 for {name:me}",
	"look mirror",

	// A registered name, resolved through _reg/ on the object
	// carrying the description and then outwards.
	"@set #0=_reg/lib-desc:#2",
	"@set mirror=_/de:@$lib-desc registered",
	"look mirror",

	// A name that resolves to nothing prints the rest of the line
	// *unparsed* — no MPI, which upstream's own comment calls a
	// crazy edge case and leaves alone.
	"@set mirror=_/de:@9999 plain {name:me} text",
	"look mirror",
	"@set mirror=_/de:@$nosuchreg plain text",
	"look mirror",

	// ...and with nothing after it, the nothing-special message.
	"@set mirror=_/de:@9999",
	"look mirror",
	"@set mirror=_/de:@",
	"look mirror",

	// The scan does not skip leading space, so "@ spaced" names an
	// empty program and prints "spaced".
	"@set mirror=_/de:@ spaced",
	"look mirror",

	// #3 exists and is an exit, not a program, so it falls through
	// the same way a missing dbref does.
	"@set mirror=_/de:@3 not a program",
	"look mirror",

	// atoi stops at the first non-digit, so this is still #2.
	"@set mirror=_/de:@2x trailing",
	"look mirror",
}

// TestExecOrNotifyMatchesFuzzball checks the '@'-prefixed message
// property against the C server.
func TestExecOrNotifyMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), execProgram)
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, execScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, execScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range execScript {
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
