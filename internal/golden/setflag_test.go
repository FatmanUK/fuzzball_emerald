package golden

import (
	"context"
	"testing"
)

// `@set`'s flag form had no permission model. `unable_to_set_flag`
// (`set.c:537`) is a force-level guard, two mucker-bit rules that
// interpolate the level into their message, and a per-flag, per-type
// table; this server had `wizardOnlyFlags`, a six-entry map. So
// YIELD, ABODE, ZOMBIE, VEHICLE and DARK were unguarded, the
// `wiz_vehicles`, `exit_darking` and `thing_darking` parameters had
// no reader anywhere, a *forced* `@set me=W` was not refused, and
// three flags were guarded too tightly instead.
//
// **A quelled wizard is a mortal** for every `Wizard(OWNER(player))`
// test in that function, which is what makes nearly all of it
// comparable from the oracle's single God seat. Six rules still are
// not, each needing a second player, and those are unit tests in
// `internal/game`.
//
// Note what quell does *not* reach: ABODE asks `TrueWizard`, QUELL
// asks `God`, and BUILDER-on-a-program asks `MLevel`, none of which
// quelling changes — so those three stay permitted throughout,
// which the script checks rather than assumes.
var setFlagScript = Script{
	// #0 is the room, #1 the wizard, #2 test.muf and #3 the exit
	// in front of it, so the widget is #4.
	"@create widget",
	"drop widget",

	// YIELD and OVERT take a wizard *and* one of the two types
	// the env-chain walk consults. An exit, a program or a player
	// is refused even to God, which makes these the one set of
	// rules a God-only transcript could already see — and the
	// generic refusal is "Permission denied. (restricted flag)"
	// where this server said "Permission denied."
	"@set widget=yield",
	"@set widget=!yield",
	"@set here=overt",
	"@set here=!overt",
	"@set test=yield",
	"@set test=overt",
	"@set test.muf=yield",
	"@set me=yield",

	// INTERACTIVE is in str_to_flag (`db.c:2379`) and was missing
	// from this server's table, which is a flag name refused
	// outright rather than a permission question. A thing, not
	// the player: INTERACTIVE on a player means "is in the
	// editor".
	"@set widget=interactive",
	"@set widget=!interactive",

	// The two readings of the argument. `negated` is the *first
	// character alone*, so "!!H" clears where `has_flag`'s "!!x =
	// x" rule would set; `p` skips every leading '!' and space,
	// so "! H" names H.
	"@set widget=haven",
	"@set widget=!!H",
	"@set widget=H",
	"@set widget=!  H",

	// A bare "!" is an empty flag *name*, and this server read it
	// as a mucker level: the empty string is a prefix of
	// "mucker", so `@set widget=!` cleared both mucker bits and
	// said "Mucker level reset."
	"@set widget=!",
	"@set widget=",

	// There is deliberately no probe for a trailing space on the
	// flag name, and the reason is a finding of its own. Upstream
	// answers `@set widget=kill_ok ` with "I don't recognize that
	// flag.", because `string_prefix` cannot match past the space
	// and nothing right-trims arg2 -- only arg1 is right-trimmed,
	// after the '=' split (`game.c:706`). This server trims the
	// **whole line** at intake (`command.go:153`), where
	// `process_command` trims nothing, so the second argument of
	// every '='-taking command loses its trailing whitespace.
	// That is wider than @set and is recorded in
	// docs/upstream-coverage.md rather than patched here; a probe
	// for it would be a case that cannot pass.
	"@set widget=K",
	"@set widget=!K",

	// BUILDER is BOUND on a program and reads `mlev`, the only
	// use that argument has. #1 is a wizard at mucker 3, so
	// MLevel is 4 and this passes; it is the rule a wizard with
	// no mucker bits would fail.
	"@set test.muf=bound",
	"@set test.muf=!bound",

	// Clearing the wizard bit on yourself has its own message,
	// and this server simply did it -- leaving a world with one
	// fewer wizard than it had.
	"@set me=!W",

	// A vehicle with somebody inside may not stop being one.
	// @create leaves it in the inventory and entering something
	// you carry is a loop, so it is dropped first, and the
	// boarding exit must hang on the vehicle rather than the room
	// -- which only @action can do.
	"@create car",
	"@set car=vehicle",
	"drop car",
	"@action board=car",
	"@link board=car",
	"board",
	// From *inside* the car it has to be named "here": the victim
	// search is match_everything, which has no stage for the
	// searcher's own location. Naming it "car" answers "I don't
	// understand 'car'." and never reaches the message this probe
	// exists for -- which is how the first draft of this case
	// passed while testing nothing.
	"@set here=!V",
	"leave",
	"@set car=!V",

	// The force guard, which had no equivalent at all. WIZARD and
	// the mucker bits may never be forced; XFORCIBLE may be
	// forced **on an exit** and nowhere else, which is exactly
	// the type the switch refuses to a mortal.
	//
	// Forcing cannot be reached through "me": do_force refuses
	// God outright (`wiz.c:553`), so every probe answered "You
	// cannot force God to do anything." and the guard was never
	// entered. A **thing** is a valid victim and a wizard needs
	// neither XFORCIBLE nor a flock to force one -- and a ZOMBIE
	// thing relays what it is told back to its owner, prefixed
	// with its name, which is the only way the forced command's
	// output is visible at all. And a third guard sits in front
	// of even that: with strict_god_priv on -- its default -- a
	// forced thing may not touch anything God owns, and God owns
	// every object in this fixture, so every probe answered "Only
	// God may touch God's property." That guard is this server's
	// too, ported last tranche, so the agreement was real and the
	// coverage nil.
	"@tune strict_god_priv=no",
	"@set widget=zombie",
	"@force widget=@set car=W",
	"@force widget=@set car=M2",
	"@force widget=@set car=X",
	"@force widget=@set test=X",
	"@set test=!X",
	"@force widget=@set car=K",
	"@set car=!K",
	"@set widget=!zombie",
	"@tune %strict_god_priv",

	// --- and now as a mortal.
	//
	// The three parameters this function is the only reader of,
	// set to their restrictive values first because a quelled
	// wizard can no longer write them.
	"@tune wiz_vehicles=yes",
	"@tune exit_darking=no",
	"@tune thing_darking=no",
	"@set me=Q",

	// Quelling does not stop you quelling yourself: the rule is
	// `TrueWizard(thing) && thing != player`, so your own bit is
	// always yours.
	"@set widget=yield",
	"@set widget=guest",

	// DARK is the flag whose answer differs for all five types. A
	// room and a program are anybody's -- on a program DARK is
	// the debugger -- a player is never, and an exit and a thing
	// only while the matching parameter allows it.
	"@set me=D",
	"@set test=D",
	"@set widget=D",
	"@set here=D",
	"@set here=!D",
	"@set test.muf=D",
	"@set test.muf=!D",

	// wiz_vehicles is on, so a mortal may not make one.
	"@set car=V",

	// ZOMBIE restricts a *player* and creates a *puppet*. The
	// thing is permitted because #1 is not itself ZOMBIE.
	"@set widget=zombie",
	"@set widget=!zombie",
	"@set me=zombie",

	// The two interpolated mucker messages. A mortal may change
	// only a program they own, so a thing fails both; #1 is raw
	// mucker 3, so its own program passes every level.
	"@set widget=M3",
	"@set widget=M0",
	"@set test.muf=M1",
	"@set test.muf=M0",
	"@set test.muf=3",

	// XFORCIBLE is restricted on an **exit** only, which is the
	// one place this server's map was too strict rather than too
	// loose: a mortal may make their own thing forcible.
	"@set widget=X",
	"@set widget=!X",
	"@set test=X",

	// BUILDER needs a wizard off a program and `mlev` on one, and
	// MLevel ignores quell -- so the thing is refused while the
	// program is not.
	"@set widget=B",
	"@set test.muf=B",
	"@set test.muf=!B",

	// ABODE asks TrueWizard, which quelling leaves alone, so
	// AUTOSTART is still permitted here.
	"@set test.muf=abode",
	"@set test.muf=!abode",

	// And the wizard bit, which needs unquelled powers.
	"@set widget=W",
	"@set me=!Q",

	// Back to a wizard, and the flags are where the script left
	// them.
	"examine widget",
}

// TestSetFlagMatchesFuzzball compares the ladder.
func TestSetFlagMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}
	script := setFlagScript
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
