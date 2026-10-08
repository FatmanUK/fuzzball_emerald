package golden

import (
	"context"
	"testing"
)

// A walkthrough transcribed from Fuzzball's own MUCK Manual
// (https://fuzzball-muck.github.io/muckman/toc.html), one script per
// chapter, driven by what the manual says a *user* does rather than
// by what we thought to test.
//
// Three things about the transcription, all of them deliberate.
//
// **The manual is older than Fuzzball 7 and is not a specification.**
// Its `@dig` reply is worded differently from 7's, and it lists
// `UPTIME`, `SWEEP` and `@RESTART` under *user-created programs*
// where all three are server commands in the C. So it supplies the
// **scenario** and the oracle supplies the expected output, which is
// how every golden case already works. Where the manual and the C
// disagree, the C wins.
//
// **The std-db programs it leans on are not here.** `$nothing`,
// `$obvex`, `lsedit` and `@$obvex` are MUF written by upstream's
// starter world, not server commands -- the manual says so itself.
// Where a script needs a do-nothing program to link to, it registers
// the fixture's own `test.muf` under that name, which is the same
// shape and says so at the call site.
//
// **Themed scripts, not one long one.** A divergence that shifts a
// dbref poisons every later reference, so each chapter gets its own
// fixture and `t.Run` names the failure.
//
// Two harness constraints the plan called for are deliberately
// absent. `@tune commands_per_time=1000` is unnecessary at this
// length: the oracle spends two command tokens a step and the default
// burst is 500, so throttling starts at ~250 steps and the longest
// script here is under 60. And `@tune penny_rate=0` is unnecessary
// because every script drives #1, who controls every room it walks
// through, and `maybeFindPenny` exempts exactly that.

// buildInnScript is §4.5 and its two tutorial pages, `btut2` (an
// environment room) and `btut3` (exits and their four message
// properties).
//
// The fixture is #0 the room, #1 the wizard, #2 test.muf and #3 the
// exit in front of it, so the first thing dug is #4.
var buildInnScript = Script{
	// btut2: an environment room, registered on the player --
	// @register writes `_reg/` on #1, not on #0 -- and flagged
	// ABODE so rooms dug beneath it inherit it as a parent.
	"@dig Amberside Environment Room",
	"@register #4 = aer",
	"@set $aer = A",

	// A room parented to it by registration. @dig's reply says
	// what it tried and what it got, which is two lines.
	"@dig Amberside Inn: Tavern = $aer",
	"@register #5 = ai",

	// The manual teleports in and then hangs a personal action on
	// itself to get back, which is @action attaching an exit to a
	// *named object* rather than to the room -- the thing that
	// separates @action from @open.
	"@tel me = $ai",
	"@act ai = me",
	"@link ai = $ai",

	// btut3: the west wing, and an exit with a display name and
	// three aliases. "<W>" is part of the name, not syntax.
	"@dig Amberside Inn: West Wing",
	"@open West Wing <W>;west wing;west;w = #6",
	"@desc w = A sturdy wooden door leading to rooms " +
		"on the west wing.",
	"@succ w = You pull open the door to the West Wing " +
		"hallway...",
	"@osucc w = goes into the West Wing.",
	"@odrop w = comes in from the Tavern.",

	// And walking it. The @succ shows, the @osucc reaches nobody
	// -- notify_except excludes the actor, which is what step 4's
	// puppet is for -- and the @odrop is read on arrival.
	"w",
	"@find tavern",

	// The way back. The manual's name begins "Tavern " with the
	// space inside it, before the first ';', which is why it
	// survives: only the whole of arg1 is trimmed.
	"@open Tavern ;tavern;t;east;e = #5",
	"@desc t = A strong wooden door.",
	"@succ t = You open the door into the inn's tavern.",
	"@osucc t = goes into the Tavern.",
	"@odrop t = comes out of the west hallway.",
	"t",
	"l",

	// The manual's `@act map;look map;loo map;lo map;l map = $ae`
	// links to `$nothing`, a std-db program. The fixture's own
	// program stands in under that name, which is the same shape
	// -- an exit secured by having somewhere to go.
	"@register #2 = nothing",
	"@act map;look map;loo map;lo map;l map = $ai",
	"@link map = $nothing",
	"@succ map = You unfold a map of the inn.",
	"map",

	// What the manual's `@succ here = @$obvex` would do cannot be
	// transcribed: $obvex is upstream's obvious-exits program and
	// this fixture has no std-db. A @succ naming a program *is*
	// ported, so the shape is checked with the program there is.
	"@succ here = @$nothing a map of the inn",
	"l",

	// And the two commands the chapter ends on that need a second
	// seat -- `p jessy = ...` and a bare `p` -- answer the way a
	// page to nobody answers, which is still worth comparing.
	"p jessy = Can you tell me the dbref of the obvious " +
		"exits program?",
	"p = OK, thanks.",

	// Examine the pair, so the four message properties and the
	// parent chain are read back rather than only written.
	"examine w",
	"examine here",
}

// furnishScript is §2.2.1 (droptos) and §2.2.2 (looktraps), the
// chapters about what a room does with what is put in it and what it
// says when a player looks at part of it.
var furnishScript = Script{
	"@dig Lost and Found", // #4

	// A dropto: things dropped here go straight through, because
	// the room is not STICKY.
	"@link here=#4",
	"@create bic", // #5
	"drop bic",
	"@contents here",
	"@contents #4",

	// STICKY postpones it until every player has left, and nobody
	// leaves in this script -- so the pen stays put and the bic
	// is already gone.
	"@set here=S",
	"@create pen", // #6
	"drop pen",
	"@contents here",
	"@contents #4",
	"@set here=!S",

	// And removing the dropto, which has its own wording.
	"@unlink here",
	"@create pad", // #7
	"drop pad",
	"@contents here",

	// §2.2.2: a looktrap is a `_details` property with
	// semicolon-separated aliases, and the manual uses `@set` for
	// it rather than `@propset` -- which is what sent the
	// previous commit at `@set`'s own rules.
	"@set here = _details/sign;plaque;notice:To see who " +
		"lives here, type `look mailboxes'.",
	"look sign",
	"look plaque",
	"look notice",

	// A name the trap does not carry, so the ordinary look
	// failure still answers.
	"look mailboxes",
	"examine here=_details/**",

	// MPI inside a detail. `{time}` is in the manual and cannot
	// be compared -- the two servers run in different zones -- so
	// this uses a function whose answer is the world's own.
	"@set here=_details/map;chart:The chart is headed " +
		"{name:this}.",
	"look map",
	"look chart",

	// A detail that is **not a string** does not run, which is
	// the reason `@set`'s `^N` form matters: it is the only way
	// to make an integer property from the command line.
	"@set here=_details/count:^42",
	"look count",
	"examine here=_details/**",

	// Two traps whose alias lists overlap. The walk stops at the
	// first match deterministically, so this is safe where an
	// ambiguous *object* would be a coin toss.
	"@set here=_details/sign;board:A second sign.",
	"look sign",
	"look board",
}

// lockScript is §2.3 and its three sub-chapters: an exit with a lock
// and its four messages, a **bogus** exit that goes nowhere, an
// **unsecured** exit and the two ways of securing one, and exit
// **priority**, which is mucker bits on an exit.
var lockScript = Script{
	"@dig Vault",         // #4
	"@open vault;v = #4", // #5

	// A lock that passes, with the fail messages that will not
	// show while it does.
	"@lock v = me",
	"@fail v = The vault is sealed.",
	"@ofail v = rattles the vault door.",
	"@succ v = The door swings open.",
	"@osucc v = opens the vault.",
	"v",
	"@tel me = #0",

	// And one nobody passes, which is the idiom §2.3.2 gives for
	// securing an exit without linking it.
	"@lock v = me&!me",
	"v",
	"examine v",
	"@unlock v",
	"v",
	"@tel me = #0",

	// §2.3.1: a bogus exit is one that leads nowhere and exists
	// for its messages. The manual links it to $nothing, a std-db
	// program; the fixture's own program stands in.
	"@register #2 = nothing",
	"@open Grandma's Rocker;grandmas rocker;rocker;chair;sit",
	"@link chair = $nothing",
	"@desc chair = An old, old rocker that has been in the " +
		"family for generations.",
	"@succ chair = You take a seat in the old rocker.",
	"@osucc chair = takes a seat in the old rocker.",
	"look rocker",
	"examine chair",

	// §2.3.2: an unlinked exit can be seized by anybody who uses
	// it, so it is either linked or locked shut. This one is
	// locked rather than linked, which is the other half of the
	// advice.
	"@open bench",
	"@lock bench = me&!me",
	"bench",
	"examine bench",

	// §2.3.3: priority is mucker bits on the exit, and an exit
	// at a higher level wins outright -- so two exits of one name
	// at *different* levels are decided deterministically, where
	// two at the same level are a coin toss `choose_thing`
	// deliberately reproduces. Both are named by dbref, because
	// naming "bank" while two of them exist would be ambiguous
	// for `@set` as well.
	"@create till", // #8
	"drop till",
	"@action bank = till", // #9, on the thing
	"@open bank",          // #10, on the room
	"@link #9 = $nothing",
	"@link #10 = $nothing",
	"@succ #9 = The till rattles open.",
	"@succ #10 = The branch is closed.",
	"@set #9 = M1",
	"examine #9",
	"examine #10",
	"bank",
	"@set #9 = M0",
	"@set #10 = M2",
	"examine #10",
	"bank",
}

// multiActionScript is §4.2, "Making a Multi-Action": one action
// carrying many names, which answers differently for each because
// `{exec:{&cmd}}` looks up a property named after the alias that was
// typed. It is the manual's own demonstration that `&cmd` holds the
// **verb as typed** rather than the action's own first name.
var multiActionScript = Script{
	// The manual links its action to $nothing, a std-db
	// do-nothing program; the fixture's own program stands in.
	"@register #2 = nothing",

	// One action, four names, attached to the player -- @action,
	// not @open, because it hangs on a named object.
	"@act ref;note;watch = me", // #4
	"@link ref = $nothing",

	// A lock nobody passes, so using it always reaches @fail --
	// which is where the dispatch lives. The exit is linked all
	// the same, which is §2.3.2's advice about not leaving one
	// unsecured.
	"@lock ref = me&!me",
	"@fail ref = {exec:{&cmd}}",

	// One property per alias, on the action itself.
	"@set ref = ref:This is the reference action.",
	"@set ref = note:A note from {name:me}.",
	"@set ref = watch:You glance at your watch.",

	// And each name answers with its own.
	"ref",
	"note",
	"watch",

	// A name the action carries with no property behind it
	// evaluates to nothing, which is what makes a typo silent.
	"@name ref = ref;note;watch;clock;full",
	"clock",
	"@set ref = clock:The clock says {name:this}.",
	"clock",

	// {name} and {fullname} differ in exactly one line, and an
	// exit is the only place it shows: NAME cuts the name at the
	// first ';' and FULLNAME does not, so the same action reports
	// one alias through one and the whole list through the other.
	"@set ref = full:[{name:this}][{fullname:this}]",
	"full",

	// An abbreviation of an alias reaches it too, because exit
	// matching is by alias and not by whole word -- and the
	// *typed* text is what `&cmd` holds, so the property looked
	// up is the abbreviation and finds nothing.
	"wat",

	"examine ref",
	"examine ref=/",
}

// puppetScript is §4.3 ("Making Puppets") and §4.4 ("Making
// Vehicles"), which the manual builds the same way: a thing, a flag,
// an action attached to it, and `{force:...,{&arg}}` to drive it.
//
// This is the transcription half only: what a **third party** sees of
// the `@o*` messages reaches nobody in a one-seat transcript and is
// the next step's work. A vehicle's exterior output turned out *not*
// to need a second seat -- the driver is inside while the car speaks
// in the room -- which is how the `@oecho` gap was found here rather
// than there.
var puppetScript = Script{
	"@register #2 = nothing",

	// §4.3. "== pup" is an empty cost and a registration, which
	// is @create's third argument.
	"@create Squiggy == pup", // #4

	// The two flags, by prefix on two different spellings of the
	// name, which is the manual's own way of writing it.
	"@set squig = Z",
	"@set squiggy = X",
	"@flock squiggy = me",
	"drop squiggy",

	// Forcing it to pose. The relay prefixes what the puppet is
	// told with its name, which is the only reason the owner sees
	// anything at all.
	"@force $pup = :jumps!",

	// And the prefix is settable.
	"@pecho squiggy = *",
	"@force $pup = :jumps again!",

	// The action that drives it without a @force each time:
	// locked shut so @fail is what runs, and @fail is MPI that
	// forces the puppet with whatever followed the verb.
	"@act z = me", // #5
	"@link z = $nothing",
	"@lock z = me&!me",
	"@fail z = {force:$pup,{&arg}}",
	"z :bounces!",
	"z look",
	"examine squiggy",

	// §4.4. The same shape with VEHICLE instead of ZOMBIE, and a
	// boarding exit -- which must hang on the vehicle, so only
	// @action can make it.
	"@create 1967 Corvette Sting Ray == vette", // #6
	"@set $vette = V",
	"@act getin = $vette", // #7
	"@link getin = $vette",

	// `drop` cannot take a registration: do_drop's matcher is
	// match_possession and match_me only, with no
	// match_registered, so "$vette" reaches nothing. That is
	// upstream's and both servers agree -- it is here as a probe
	// rather than a mistake, because the next line only works by
	// name and the reason is worth recording.
	"drop $vette",
	"drop Corvette",
	"getin",

	// @idescribe is what a vehicle's occupants see, and `here`
	// from inside is the vehicle.
	"@idesc here = The interior is pristine: gleaming chrome " +
		"and smooth blue vinyl.",
	"look",

	// Driving it from inside, the same way the puppet is driven.
	"@set $vette = X",
	"@flock $vette = me",
	"@act drive = $vette", // #8

	// The manual omits this and the action does not work without
	// it: an **unlinked** exit cannot partial-match, so "drive
	// :vroom" never reaches the action at all and both servers
	// answer "Huh?" -- a case that passes while testing nothing.
	// match_exits allows a partial match only when the exit runs
	// a program or is NIL-linked, which is `exitprog`
	// (`match.c:551`).
	"@link drive = $nothing",
	"@lock drive = me&!me",
	"@fail drive = {force:$vette,{&arg}}",
	"drive :vroom vrooOOOOmms!",

	// @oecho is the prefix a vehicle's occupants see on output
	// from **outside** it -- and one seat *can* produce it,
	// because the driver is inside while the car speaks in the
	// room. The line above showed the default, "Outside>"; these
	// show the property being read, which nothing in this server
	// did.
	"@oecho $vette = >>>",
	"drive :vroom vrooOOOOmms!",

	// An MPI prefix, evaluated with the speaker as the viewer and
	// the vehicle as the object carrying it.
	"@oecho $vette = [{name:this}]",
	"drive :idles.",

	// And cleared, which puts the default back rather than
	// leaving an empty prefix.
	"@oecho $vette =",
	"drive :stalls.",

	"examine here",
	"leave",
	"examine $vette",
}

// TestWalkthroughMatchesFuzzball drives the manual's own scenarios
// through both servers.
func TestWalkthroughMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	for _, tc := range []struct {
		name   string
		script Script
	}{
		{"BuildAnInn", buildInnScript},
		{"FurnishIt", furnishScript},
		{"LockIt", lockScript},
		{"OneActionManyNames", multiActionScript},
		{"PuppetsAndVehicles", puppetScript},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compareWalkthrough(t, tc.script)
		})
	}
}

// compareWalkthrough runs one script through both servers on its own
// fixture and diffs it step by step.
func compareWalkthrough(t *testing.T, script Script) {
	t.Helper()
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}
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
