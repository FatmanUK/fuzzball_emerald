package golden

import "testing"

// `exit_loop_check` (`predicates.c:289`) had no equivalent here, and
// `CLAUDE.md` twice said upstream had none either — that an exit
// linked to itself "exhausts the C stack and takes the server down,
// and `@link` does not test for it". It does test for it, in all
// three places an exit's destination is set: `_link_exit`
// (`db.c:2117`), MUF `SETLINK` (`p_db.c:1805`) and `SETLINKS_ARRAY`
// (`p_db.c:3931`), each with a different message.
//
// The check could not be *seen* to be missing until the matcher was
// fixed. `resolveLinkTarget` had a hand-built chain with no
// `match_registered` and no `match_all_exits`, so an exit was not
// nameable as a destination and `@link` could not build a loop at
// all. Porting `match_everything` made it buildable, and this is what
// upstream answers instead.
//
// A **metalink is legitimate** — `trigger` traverses one
// deliberately — so the case links a chain two deep before closing
// it into a ring, to show the check refuses the ring and not the
// chain.
var linkLoopScript = Script{
	// #0 room, #1 wizard, #2 test.muf, #3 the test exit.
	"@open alpha", // #4
	"@open beta",  // #5
	"@open gamma", // #6

	// A legitimate metalink: one hop.
	"@link alpha=beta",

	// And the ring that closes it, which is the depth-one catch.
	"@link beta=alpha",

	// An exit linked to itself, the case the old note described.
	"@link beta=beta",

	// Two deep, then closed: gamma points at alpha, which points
	// at beta, so linking beta to gamma would make beta -> gamma
	// -> alpha -> beta. Only recursing finds that, which is the
	// half a self-link test would miss. The names avoid the
	// fixture's player, who is called "One": `match_everything`
	// adds `match_player` for a wizard, so an exit named "one"
	// resolves to the player instead and the chain is never built
	// -- which is how the first draft of this case passed while
	// testing nothing.
	"@link gamma=alpha",
	"@link beta=gamma",
	"@unlink alpha",
	"@link alpha=beta",

	// @open links through the same function, so its second
	// argument is checked too -- and it prints no "No
	// destinations linked.", because do_open has none.
	"@open delta=delta",

	// @relink checks every destination *before* breaking the old
	// link, which is the whole point of the command, so a loop
	// has to be refused there as well rather than leaving an exit
	// pointing nowhere.
	"@relink gamma=gamma",
	"examine gamma",

	// A room, a thing and a program are not exits, so the check
	// does not apply to them -- upstream runs it in one branch of
	// a type switch.
	"@dig Cellar", // #8
	// Still linked from the chain above, so this is the
	// already-linked guard rather than the loop check.
	"@link alpha=Cellar",
	"@unlink alpha",
	"@link alpha=Cellar",

	// What each exit ended up pointing at.
	"examine alpha",
	"examine beta",
	"examine delta",
}

// linkMatcherScript covers the other half of the same discovery.
// `@link` is three operations wearing one name and **each has its own
// match list**, which `do_link` writes out inline rather than
// sharing: only the exit branch calls `parse_linkable_dest`
// (`db.c:1971`), and the home and dropto branches build lists that
// differ from it and from each other. This server used one function
// for all three, so each of the three was wrong.
//
// The visible consequences are HOME and the two self-names. A thing's
// home may *not* be set to HOME, because the home branch has no
// `match_home`; a dropto may, because the dropto branch has one. And
// the dropto branch has neither `match_me` nor `match_here`, where
// the home branch has both.
var linkMatcherScript = Script{
	"@create crate", // #4
	"drop crate",
	"@dig Pantry", // #5

	// The home branch: "me" and "here" resolve and HOME does not,
	// which is the difference that matters.
	"@link crate=me",
	"@link crate=here",
	"@link crate=home",

	// "test" is worth a word, because it does *not* show the
	// absence of `match_all_exits`: the branch adds
	// `match_possession` for a thing, and the wizard is carrying
	// test.muf, so "test" partial-matches the **program** and the
	// refusal is `can_link_to`'s. The exit of that name is never
	// reached, and nothing here can tell the two apart.
	"@link crate=test",
	"@link crate=#5",

	// The dropto branch, from inside the room being linked. HOME
	// is accepted here and nowhere else in the command.
	"@link here=home",
	"examine here",
	"@link here=me",
	"@link here=here",
	"@link here=#5",
	"examine here",

	// can_link_to's type matrix, which had no equivalent at all.
	// A player's home must be a room; a room's dropto must be a
	// thing or a room, so the program the wizard is carrying is
	// refused; and NIL is linkable only from an exit.
	"@link me=crate",
	"@link me=#5",
	"@link here=test",
	"@link crate=nil",

	// The fixture's exit already points at its program, so it has
	// to be unlinked before NIL can be reached at all -- without
	// this the probe answered "That exit is already linked." and
	// the rule was never tested.
	"@unlink test",
	"@link test=nil",
	"examine test",

	// And the registration both branches do have, which is what
	// the walkthrough's inn needed.
	"@register #5 = pantry",
	"@link crate=$pantry",
	"@unlink here",
	"@link here=$pantry",
	"examine crate",
}

// TestLinkMatchesFuzzball compares both ladders.
func TestLinkMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	for _, tc := range []struct {
		name   string
		script Script
	}{
		{"Loops", linkLoopScript},
		{"Matchers", linkMatcherScript},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compareWalkthrough(t, tc.script)
		})
	}
}
