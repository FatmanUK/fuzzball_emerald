package golden

import (
	"context"
	"testing"
)

// MUF `SET` had **no permission model at all**. It popped a flag
// name, resolved the bit, ORed it in and called the host's setter.
// `prim_set` (`fuzzball/src/p_db.c:1001`) does three things first:
// `CHECKREMOTE`, then `(mlev < 4) && !permissions(ProgUID, ref)`,
// then `unable_to_set_flag` (`set.c:537`) in full. So **a mucker-1
// program could set WIZARD on any object**, including its own owner.
//
// The cause is systematic and is written in the mucker table's own
// doc comment: `mlev_gen.go` records *unconditional* floors and
// deliberately skips `(mlev < N) && !permissions(...)`, because that
// means "a wizard **or** the owner" and recording it as a floor would
// refuse the owner. Correct — and the skipped half was meant to be
// hand-ported per primitive, which for `SET` never happened.
//
// **`ProgUID` is `Owner(Caller)` for an exit-run program**, not the
// program's owner (`prim_lock.go:67`), so chowning `test.muf` changes
// nothing — and it also *caps* `find_mlev` at the new owner's
// level, which silently flattens the ladder. The first draft of this
// case did exactly that and tested nothing twice over: every probe
// answered identically at both levels, because mucker 4 was never
// reached.
//
// What works instead is to leave the program with `#1` and chown the
// **targets**. `ProgUID` is then `#1`, so `permissions(#1, X)` is
// true for `#1`'s own objects and false for Bob's — which is
// exactly what `(mlev < 4) && !permissions` turns on, and the same
// program answers differently at mucker 3 and mucker 4 with nothing
// else changed.
//
// `CHECKREMOTE` is **not** covered here and cannot be: it only bites
// when `ProgUID` is a non-wizard, and `#1` is a wizard. Reaching it
// needs the program chowned *and* run so that `progUID` takes its
// STICKY or `mlev < 2` branch, which is a rig worth building once for
// all 33 sites rather than here.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const setPrimSource = `: ts[ s -- ] me @ s @ notify ;
: try1[ o s -- ] 0 try o @ s @ set "set ok" ts catch ts endcatch ;
: main
  ( A ref naming nothing. valid_object refuses it before any
    permission test, and this server had no such check at all --
    refAndHost only pops a ref, so it fell through and answered
    "Permission denied." instead. )
  #99 "sticky" try1

  ( The ownership gate. Bob owns the pebble, ProgUID is #1, so
    permissions is false -- refused at mucker 3 and allowed at
    mucker 4, which is the whole meaning of the guard. )
  #5 "sticky" try1
  #5 "!sticky" try1

  ( The same flag on an object ProgUID does own, which passes the
    gate at either level. )
  #4 "sticky" try1
  #4 "!sticky" try1

  ( Past the gate, unable_to_set_flag is what answers. YIELD wants a
    wizard -- ProgUID is one -- *and* a thing or a room, and #3 is
    the exit, so this is the generic refusal. Nothing checked any of
    this before. )
  #3 "yield" try1

  ( And the one rule with its own sentence: clearing the wizard bit
    on yourself. ProgUID is #1, so "thing == player" holds. )
  #1 "!wizard" try1

  ( A name str_to_flag does not know, which is reached only after
    the gate -- so it needs an object ProgUID controls. )
  #4 "nosuchflag" try1

  ( "truewizard" resolves to the WIZARD bit here, because prim_set
    uses bare str_to_flag where do_set refuses the spelling. On a
    thing, with a wizard asking, that is allowed. )
  #4 "truewizard" try1
  #4 "!wizard" try1
;`

// setPrimScript leaves the program with #1 and chowns the target, so
// ProgUID stays a wizard and find_mlev is the program's own level.
var setPrimScript = Script{
	"@create widget", // #4, One's
	"@create pebble", // #5, to become Bob's
	"@pcreate Bob=pw",
	"@chown pebble=Bob",

	// Mucker 3: the gate bites on what ProgUID does not own.
	"test",

	// Mucker 4 lifts it — both lines, and on the program rather
	// than the exit, since the Wizard bit plus any mucker bit is
	// level 4 outright and neither alone is.
	"@set test.muf=wizard",
	"@set test.muf=3",
	"test",

	// What actually landed.
	"examine widget",
	"examine pebble",
}

// TestSetPrimMatchesFuzzball compares the ladder at two levels.
func TestSetPrimMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), setPrimSource)
	if err != nil {
		t.Fatal(err)
	}
	script := setPrimScript
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
		if diffs := Compare(maskVariable(want),
			maskVariable(got)); len(diffs) > 0 {
			t.Errorf("step %d, %q\n%s", i, cmd,
				Render(diffs))
		}
	}
}
