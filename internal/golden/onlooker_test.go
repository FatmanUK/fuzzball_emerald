package golden

import "testing"

// The `@o*` message family, heard.
//
// The harness drives **one** player and `notify_except` excludes the
// actor, so every `@osucc`, `@ofail`, `@odrop` and every "has
// arrived" in a golden run reached nobody. The plan's answer was a
// puppet as an observer, and that turns out to be the wrong way
// round: a puppet relays a *public* line to its owner only when the
// two are in different rooms (`interface.c:4712`), so a puppet
// standing beside its owner sees nothing the owner has not already
// seen.
//
// **The puppet is the actor and the owner is the audience.** That is
// what makes the family observable from one seat: `notify_except`
// excludes the puppet, so #1 standing in the room receives what the
// puppet's actions broadcast, exactly as another player would.
//
// `internal/golden/getdrop_test.go:56` is the case this replaces in
// spirit. It sets `@odrop widget=lets the widget fall.`, drops the
// widget, and that text reaches no stream at all; its comment
// describes behaviour it cannot observe, and both servers agree about
// a situation it never sets up.
var onlookerScript = Script{
	// God owns every object in this fixture, and a forced thing
	// may not touch God's things while strict_god_priv is set, so
	// the whole script would answer "Only God may touch God's
	// property." That guard is upstream's and this server's;
	// turning it off is what makes the rest reachable.
	"@tune strict_god_priv=no",

	// The actor. ZOMBIE so it announces its own movement -- a
	// *plain* thing does not, which is one of the five conditions
	// below -- and dropped so it is in the room rather than
	// carried.
	"@create Mime == mime", // #4
	"@set $mime = Z",
	"drop Mime",

	// Something for it to pick up, with all six messages set so
	// each half can be told apart.
	"@create widget", // #5
	"drop widget",
	"@succ widget = You take the widget.",
	"@osucc widget = picks up the widget.",
	"@fail widget = The widget will not budge.",
	"@ofail widget = tugs at the widget.",
	"@drop widget = You set the widget down.",
	"@odrop widget = lets the widget fall.",

	// get: the @succ reaches the actor and the @osucc reaches the
	// room. Only the second of those has ever been observable,
	// and only from here.
	"@force $mime = get widget",

	// drop: three things are said at once, and the one worth the
	// case is that the **room's** @odrop is prefixed with the
	// *thing's* name rather than the actor's.
	"@odrop here = is left lying in the test room.",
	"@drop here = Everything settles.",
	"@force $mime = drop widget",
	"@odrop here =",
	"@drop here =",

	// ...and the failing half, which needs a lock nobody passes.
	"@lock widget = me&!me",
	"@force $mime = get widget",
	"@unlock widget",

	// say and pose as *heard*, including the four separators
	// do_pose omits its space before: "'", " ", "," and "-".
	"@force $mime = say Hello.",
	"@force $mime = :waves.",
	"@force $mime = :'s hat falls off.",
	"@force $mime = :, thoughtfully, nods.",
	"@force $mime = :- and then stops.",
	"@force $mime = : leading space.",

	// The five conditions under which a move announces itself.
	// First the ordinary case, with somewhere to go and an exit
	// to go through.
	"@dig Wings", // #6
	"@open out = #6",
	"@open back = #0",
	"@force $mime = out",
	"@force $mime = back",

	// quiet_moves off entirely.
	"@tune quiet_moves=yes",
	"@force $mime = out",
	"@force $mime = back",
	"@tune quiet_moves=no",

	// A DARK mover is silent. The flag is on the puppet, and a
	// DARK puppet also stops relaying -- which does not matter
	// here, because the owner is the audience.
	"@set $mime = D",
	"@force $mime = out",
	"@force $mime = back",
	"@set $mime = !D",

	// A DARK exit is silent.
	"@set out = D",
	"@force $mime = out",
	"@set out = !D",
	"@force $mime = back",

	// A DARK room is silent. #0 is where the audience stands, so
	// this is the departure rather than the arrival.
	"@set here = D",
	"@force $mime = out",
	"@set here = !D",
	"@force $mime = back",

	// And a **plain thing** announces nothing at all, which is
	// what keeps a world full of furniture readable. The puppet
	// stops being a puppet for two moves.
	"@set $mime = !Z",
	"@force $mime = out",
	"@force $mime = back",

	// A VEHICLE announces itself like a ZOMBIE, which is the
	// other half of that condition -- and it is not a puppet, so
	// nothing is relayed and the announcement is all there is.
	"@set $mime = V",
	"@force $mime = out",
	"@force $mime = back",

	"examine $mime",
	"look",
}

// TestOnlookerMatchesFuzzball compares what a bystander hears.
func TestOnlookerMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	compareWalkthrough(t, onlookerScript)
}
