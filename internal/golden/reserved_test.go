package golden

import (
	"context"
	"testing"
	"time"
)

// reservedProgram reports the four reserved variables and the initial
// stack argument, which is everything interp() sets up before a
// program runs.
//
// COMMAND and the argument are the pair worth comparing: they are
// upstream's match_cmdname and match_args, two different strings, and
// every launch site chooses them differently.
const reservedProgram = `: main
  "arg[" swap strcat "]" strcat me @ swap notify
  "cmd[" command @ strcat "]" strcat me @ swap notify
  "trig[" trigger @ intostr strcat "]" strcat me @ swap notify
  "loc[" loc @ intostr strcat "]" strcat me @ swap notify
;`

// reservedScript reaches that program four ways, because upstream
// sets COMMAND differently at each.
var reservedScript = Script{
	// Through its own exit, with and without an argument. The
	// exit matches only its first word because it leads to a
	// program, so the verb and the argument really are two
	// strings.
	"test",
	"test hello there",
	// The verb is what was *typed*, not the exit's own spelling:
	// match_exits copies out of md->match_name.
	"TEST mixed Case",

	// Through INTERP, which does not touch match_cmdname — so
	// the called program inherits the caller's COMMAND rather
	// than getting one of its own.
	"caller inherited",

	// Through MPI's {muf}, which sets it to "<&how>(MPI)" and
	// passes the permissions object as the trigger rather than
	// the player.
	"@describe me={muf:#2,from mpi}",
	"look me",
	"@describe me=",

	// And through a message property that names the program,
	// where COMMAND is the caller context.
	"@succ me=@2 from a succ",
	"look me",
	"@succ me=",

	// The three variables do_parse_mesg_2 allocates for every
	// evaluation: {&how} is the caller context, {&cmd} is
	// match_cmdname and {&arg} is match_args.
	"@describe me=how[{&how}] cmd[{&cmd}] arg[{&arg}]",
	"look me",
	"l me",
	"@succ me=how[{&how}] cmd[{&cmd}] arg[{&arg}]",
	"look me",
	"@describe me=",
	"@succ me=",
}

// TestReservedVariablesMatchFuzzball checks what a program is handed
// against the C server.
func TestReservedVariablesMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	// #2 is reservedProgram and #3 its exit "test"; #4 is the
	// caller and #5 its exit.
	fx, err := WriteMultiFixture(t.TempDir(), []Program{
		{Name: "test", Source: reservedProgram},
		{Name: "caller", Source: `: main
  #2 me @ rot interp me @ swap notify
;`},
	})
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, reservedScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, reservedScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range reservedScript {
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
