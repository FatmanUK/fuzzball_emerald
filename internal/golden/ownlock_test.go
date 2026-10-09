package golden

import (
	"context"
	"testing"
)

// The ownership lock is the one route past `controls`'s ownership
// test that a world can configure, and **nothing read it**.
//
// `controls` (`db.c:1822`) ends `test_lock_false_default(NOTHING,
// who, what, MESGPROP_OWNLOCK)`. `@ownlock` wrote `@/olk`, `examine`
// displayed it as "Ownership Key", and no reader existed — the
// shape `_/oecho` had. So every rule that depends on a non-owner
// reaching `controls` was unreachable here, including both of
// `unable_to_set_flag`'s mucker clauses and `do_set`'s own
// `strict_god_priv` guard.
//
// The oracle drives `#1`, who passes `controls` for everything, so
// the case has the wizard **quell itself** — which is what makes
// `Wizard(who)` false for every quell-aware test while leaving the
// player able to unquell afterwards. The lock is set *before* the gem
// is given away, since a quelled wizard could not set it after.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const ownLockSource = `: main 1 pop ;`

var ownLockScript = Script{
	"@pcreate Bob=secret", // #4

	// Two of Bob's things: one with an ownlock that admits
	// everybody, one with none.
	"@create keyed", // #5
	"drop keyed",
	"@ownlock keyed=me",
	"@chown keyed=Bob",

	"@create plain", // #6
	"drop plain",
	"@chown plain=Bob",

	// And one with an ownlock that admits nobody, so the lock is
	// shown to be *evaluated* rather than merely present.
	"@create shut", // #7
	"drop shut",
	"@ownlock shut=Bob",
	"@chown shut=Bob",

	// What the lock looks like before anything is quelled, and
	// that a wizard needed none of it.
	"examine keyed",

	"@set me=quell",

	// A quelled wizard owns none of the three. The lock decides.
	"@set keyed=kill_ok",
	"@set plain=kill_ok",
	"@set shut=kill_ok",

	// Every command that resolves through match_controlled gets
	// the same answer, so the lock is not a @set special case.
	"@describe keyed=a keyed thing",
	"@describe plain=a plain thing",
	"@lock keyed=me",
	"@lock plain=me",
	"@chown keyed=me",

	// examine shows more of an object the asker controls, which
	// is `can_see_flags` rather than a message of its own -- and
	// `examine keyed` while quelled is what showed
	// `unparse_object`'s missing clauses, since the ownlock is
	// what gives a non-owner a way to control something at all.
	"examine keyed",
	"examine plain",

	"@set me=!quell",
	"@set keyed=!kill_ok",
	"examine keyed",
}

// TestOwnLockMatchesFuzzball compares the ladder.
func TestOwnLockMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ownLockSource)
	if err != nil {
		t.Fatal(err)
	}
	script := ownLockScript
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
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}
