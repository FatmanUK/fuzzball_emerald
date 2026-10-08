package golden

import (
	"context"
	"testing"
)

// TUNE_MLEV (`include/db.h:669`) is `God(player) ? 255 :
// MLevel(player)`, and upstream uses it at all six of its @tune
// permission sites. `gen_params.py` mapped MLEV_GOD to 4 and recorded
// a `GodOnly` field beside it that only the configurator read, so the
// ten parameters whose *read* level is MLEV_GOD — smtp_password
// among them — were readable by any plain wizard.
//
// Most of that is invisible here, because the oracle drives #1 and
// God reads everything either way; those halves are unit-tested. What
// this case can see is that `SYSPARM_ARRAY` reports each parameter's
// levels **verbatim**, and the numbers do not depend on who is asking
// (`tune.c:270`). So 36 parameters go from 4 to 255 on their write
// level and 10 on their read level, and a program can read that back.
//
// It also pins the pattern. `tune_parms_array` filters with
// `equalstr`, which is `smatch` (`tune.c:267`), where `TuneList` used
// `strings.EqualFold` — the wrong comparison, and the function
// CLAUDE.md forbids, since upstream folds only A–Z. So `"file_*"`
// matched nothing here and twenty-odd entries upstream.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const tuneMlevSource = `: t[ x -- ] me @ x @ intostr notify ;
: ts[ s -- ] me @ s @ notify ;
: main
  ( file_help is read at wizard level and written at God's, so its
    two levels differ and both are reported. The write level is the
    one that was collapsed to 4. )
  "file_help" sysparm_array 0 array_getitem
    "readmlev" array_getitem t
  "file_help" sysparm_array 0 array_getitem
    "writemlev" array_getitem t

  ( smtp_password is one of the ten gated on *reading*. )
  "smtp_password" sysparm_array 0 array_getitem
    "readmlev" array_getitem t
  "smtp_password" sysparm_array 0 array_getitem
    "writemlev" array_getitem t

  ( An ordinary parameter, to show the common case is untouched:
    read by anybody, written by a wizard. )
  "penny_rate" sysparm_array 0 array_getitem
    "readmlev" array_getitem t
  "penny_rate" sysparm_array 0 array_getitem
    "writemlev" array_getitem t

  ( "mlev" is an alias for the read level, which upstream emits
    alongside both. )
  "penny_rate" sysparm_array 0 array_getitem
    "mlev" array_getitem t

  ( The pattern is smatch. A wildcard matched nothing at all before,
    because the filter was an exact fold. )
  "file_*" sysparm_array array_count t
  "*_cost" sysparm_array array_count t

  ( And an exact name still matches exactly one, which is what made
    the wrong comparison so hard to notice. )
  "muckname" sysparm_array array_count t

  ( A pattern matching nothing is an empty array rather than an
    error. )
  "no_such_parameter_*" sysparm_array array_count t

  ( SYSPARM reads through tune_get_parmstring, whose gate answers
    the empty string rather than refusing -- so a program cannot
    tell "not for you" from "no such parameter". Both of these are
    God here, so both come back with a value. )
  "smtp_password" sysparm "[" swap strcat "]" strcat ts
  "no_such_parameter" sysparm "[" swap strcat "]" strcat ts
;`

// tuneMlevScript needs nothing built: every probe reads the parameter
// table, which both servers have at boot.
var tuneMlevScript = Script{"test"}

// TestTuneMlevMatchesFuzzball compares what a program reads back.
func TestTuneMlevMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), tuneMlevSource)
	if err != nil {
		t.Fatal(err)
	}
	script := tuneMlevScript
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
