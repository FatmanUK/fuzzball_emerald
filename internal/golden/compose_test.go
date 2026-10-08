package golden

import "testing"

// Composition: build a thing and then *use* it, many commands later.
//
// Every other case in this suite sets up exactly what it needs and
// probes immediately. What none of them checks is that state laid
// down a dozen commands ago still behaves after everything else has
// happened to the world — and that the **dbref numbering survives
// the whole build**, which is why the scripts here are themed rather
// than one enormous one: a divergence that shifts a ref poisons every
// later reference.
//
// The order is deliberate. Nothing is probed until the whole shape
// exists, so a failure says "this stopped working once the rest was
// built" rather than "this never worked".
var composeScript = Script{
	// --- build.
	//
	// A pair of rooms, linked both ways, with the second parented
	// to the first so the environment chain is two deep.
	"@dig Courtyard", // #4
	"@register #4 = yard",
	"@set $yard = A",
	"@dig Cellar = $yard", // #5
	"@register #5 = cellar",

	// Exits both ways, with aliases and messages, and a lock on
	// the way down that only the owner passes.
	"@open down;d = $cellar", // #6
	"@open up;u = $yard",     // #7
	"@lock down = me",
	"@fail down = The hatch is bolted.",
	"@succ down = You climb down the ladder.",
	"@odrop down = climbs down from the courtyard.",

	// A drop-to on the courtyard, so anything dropped there goes
	// to the cellar -- and STICKY on the cellar, so the cellar
	// holds what is dropped in it.
	"@link $yard = $cellar",
	"@set $cellar = S",

	// A thing, and an action hung on the thing rather than on a
	// room, which only @action can do.
	"@create lantern", // #8
	"@succ lantern = The lantern glows.",
	"@action light;rub = lantern", // #9
	"@register #2 = nothing",
	"@link light = $nothing",
	"@succ light = The lantern flares up.",
	"@fail light = Nothing happens.",

	// A second thing with a conlock, so taking from it is a
	// separate rule from taking from the floor.
	"@create satchel", // #10
	"@conlock satchel = me",
	"@create map", // #11

	// --- and only now, use it.
	//
	// The refs first: if any of the eleven above landed
	// differently the rest of this script is meaningless, so it
	// is worth one line to say so out loud.
	"@find",

	// Walk the pair, both ways, through the locked exit and the
	// unlocked one. The @succ, the @odrop and the arrival look
	// all come from state set twenty commands ago.
	"@tel me = $yard",
	"down",
	"look",
	"up",

	// The drop-to, which is the courtyard's and fires because the
	// courtyard is not STICKY.
	"@tel me = $yard",
	"drop lantern",
	"look",
	"@contents $cellar",

	// And the cellar's STICKY, which holds a thing until every
	// player has left -- so dropping the map there leaves it
	// there while we are standing in it.
	"down",
	"drop map",
	"look",
	"up",

	// The action on the lantern, which is now in the cellar: an
	// action attached to a thing is reachable from the room the
	// thing is in, and not from anywhere else. So this fails here
	// and works below.
	"light",
	"down",
	"light",

	// The container and its conlock, done **in the cellar**. Two
	// drafts of this section tested nothing: the first left the
	// map lying in the cellar while probing from the courtyard,
	// so every line answered "I don't understand 'map'."; the
	// second dropped the satchel in the courtyard, where the
	// drop-to built above swallowed it into the cellar, so every
	// line answered "I don't understand 'satchel'." The cellar is
	// STICKY, so what is dropped there stays there.
	"get map",
	"drop satchel",
	"put map=satchel",
	"get satchel=map",

	// A lock nobody passes shuts the container.
	"@conlock satchel = me&!me",
	"put map=satchel",
	"@conlock satchel = me",
	"put map=satchel",
	"@conlock satchel = me&!me",
	"get satchel=map",

	// And **clearing** it does not re-open it: an unset conlock
	// defaults to *false*, so an unopened container is shut. That
	// ordering is load-bearing and not what the messages suggest,
	// since the conlock is tested before the "can't steal stuff
	// from players" check.
	"@conlock satchel =",
	"get satchel=map",
	"@conlock satchel = me",
	"get satchel=map",
	"up",

	// The lock on the way down still refuses somebody it should,
	// which needs the lock changed rather than the player.
	"@lock down = me&!me",
	"down",
	"@unlock down",
	"down",

	// What the world looks like at the end, which is the real
	// assertion: every ref, every flag, every message where the
	// build left it.
	"examine $yard",
	"examine $cellar",
	"examine down",
	"examine lantern",
	"examine light",
	"examine satchel",
	// @owned takes a player by name: its matcher has no match_me,
	// so "me" answers "I couldn't find that player."
	"@owned One",
}

// TestComposeMatchesFuzzball builds a small world and then uses it.
func TestComposeMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	compareWalkthrough(t, composeScript)
}
