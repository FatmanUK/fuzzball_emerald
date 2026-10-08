package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// Message and lock properties, from include/db.h.
const (
	propDesc     = "_/de"
	propIDesc    = "_/ide"
	propSucc     = "_/sc"
	propOSucc    = "_/osc"
	propFail     = "_/fl"
	propOFail    = "_/ofl"
	propDrop     = "_/dr"
	propODrop    = "_/odr"
	propDoing    = "_/do"
	propRoomEcho = "_/oecho"

	propLock      = "_/lok"
	propConLock   = "_/clk"
	propChownLock = "_/chlk"
	propLinkLock  = "_/lklk"
	propForceLock = "@/flk"
	propReadLock  = "@/rlk"
	propOwnLock   = "@/olk"
)

// unlockedValue is what an unset lock reads as, from include/props.h.
const unlockedValue = "*UNLOCKED*"

// getMesg reads a message property.
func getMesg(w *world.World, r ref.Ref, path string) string {
	o := w.Get(r)
	if o == nil {
		return ""
	}
	v, ok := o.Props.Get(path)
	if !ok || v.Type != props.String {
		return ""
	}
	return v.Str
}

// cmdLook shows the room, an object in it, or a look trap.
//
// do_look_at takes two arguments, arg1 and arg2, and this server
// passed only the first — so "look <thing>=<detail>" was a syntax
// it did not accept at all. The split is upstream's: arg1 is trimmed
// both ends and arg2 is **left-trimmed only**, which is
// skip_whitespace_var against remove_ending_whitespace
// (game.c:701-709).
func (s *Server) cmdLook(c *ctx) {
	name, detail, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	detail = strings.TrimLeft(detail, " \t")

	// Upstream tests *name* alone here and never looks at the
	// detail, so "look =foo" shows the room.
	if name == "" || ascEqual(name, "here") {
		s.lookHere(c.w, c.d.ID, c.who)
		return
	}
	// do_look_at's own matcher, which is *narrower* than
	// match_everything: no registrations, so "look $thing" finds
	// nothing even when a program could resolve the name. And a
	// failed match no longer reports itself — it falls into the
	// look-trap branch, which has match_msg_nomatch at the end of
	// it.
	m := match.New(c.w, c.who, name).Exits().Neighbor().
		Possession()
	if isWizard(c.w, ownerOf(c.w, c.who)) {
		m = m.Absolute().Player()
	}
	target := m.Here().Me().Result()

	switch {
	case target != ref.Nothing && target != ref.Ambiguous &&
		detail == "":
		s.lookAt(c.w, c.d.ID, c.who, target)

	case target == ref.Nothing ||
		(detail != "" && target != ref.Ambiguous):
		s.lookDetail(c, target, name, detail)

	default:
		// Upstream passes **detail** here, not name
		// (look.c:438), so an ambiguous name with no detail
		// reports an empty one: "I don't know which '' you
		// mean!" That reads like a mistake and is reproduced,
		// because a program matching on the line would see
		// it.
		c.tell("I don't know which '%s' you mean!", detail)
	}
}

// detailsPropdir is DETAILS_PROPDIR (game.h:64), the propdir a look
// trap lives in.
const detailsPropdir = "_details"

// lookDetail is do_look_at's second branch (look.c:369-439), the one
// this server did not have: the _details propdir, which is how a
// world describes a part of something.
//
// It is reached two ways, and which one decides what is searched and
// what is searched *for*:
//
//   - nothing matched, so the details of the room the player is
//     standing in are searched for what they typed; or
//   - something matched and a detail was given, so that object's
//     details are searched for the detail.
//
// Upstream's own @TODO at look.c:380 flags the consequence as "kind
// of ... technically wrong maybe": a trap called "feh" on the room is
// found by "look feh", but "look feh=whatever" matches feh as an
// object and then searches *its* details, so the trap is bypassed.
// That is reproduced rather than improved.
//
// The walk is in nextprop order and stops at the **second** match, so
// two traps that both answer are ambiguous rather than resolved. Only
// a string-valued property runs; anything else falls through to the
// no-match messages below, which is why a propdir with a dbref-valued
// detail reads as if the detail were not there.
func (s *Server) lookDetail(c *ctx, target ref.Ref,
	name, detail string) {

	thing, typed := target, detail
	if target == ref.Nothing {
		thing = c.w.Get(c.who).Location
		typed = name
	}

	o := c.w.Get(thing)
	if o == nil {
		c.tell("I don't understand '%s'.", typed)
		return
	}

	var found string
	ambiguous := false
	for _, kid := range o.Props.Children(detailsPropdir) {
		if !detailMatches(kid, typed) {
			continue
		}
		if found != "" {
			found, ambiguous = "", true
			break
		}
		found = kid
	}

	path := detailsPropdir + "/" + found
	v, ok := c.w.GetProp(thing, path)
	switch {
	case found != "" && ok && v.Type == props.String:
		// exec_or_notify with "(@detail)" as the caller
		// context, and the property's own blessing — so
		// @bless on a trap makes its MPI wizardly, like any
		// other message property.
		s.execOrNotify(c.w, c.d.ID, c.who, thing, v.Str,
			"(@detail)", v.Blessed, mesgArgs{})
	case ambiguous:
		c.tell("I don't know which '%s' you mean!", typed)
	case detail != "":
		c.send(c.w.Tune.String("description_default"))
	default:
		c.tell("I don't understand '%s'.", typed)
	}
}

// lookHere shows what the player is standing in, which is look_room
// whether or not that is actually a room: a player inside a vehicle
// sees the vehicle's @idescribe.
func (s *Server) lookHere(w *world.World, descr int, who ref.Ref) {
	o := w.Get(who)
	if o == nil {
		return
	}
	if o.Location == ref.Nothing {
		s.notify(w, who, "You are nowhere.")
		return
	}
	s.lookRoom(w, descr, who, o.Location)
}

// The three refusals do_look_at gives, one per type. Each names the
// test that failed and they are not interchangeable.
const (
	noLookRoom = "Permission denied. (you're not where you " +
		"want to look, and can't link to it)"
	noLookPlayer = "Permission denied. (Your location isn't " +
		"the same as what you're looking at)"
	noLookThing = "Permission denied. (You're not in the same " +
		"room as or carrying the object)"
)

// lookAt describes one object to a player, which is do_look_at's
// per-type switch.
//
// A room is the only type that shows a name line, because that line
// is look_room's own first act; everything else goes through
// look_simple, which prints the description and stops. Each type has
// its own refusal and its own contents heading too. Emerald used to
// print the name for everything and head every listing "Contents:",
// so every look at a thing or a player diverged by a line.
//
// Look traps — the _details propdir, consulted when the match finds
// nothing — are not ported. They are additive, so their absence
// costs a world that uses them and changes nothing for one that does
// not.
func (s *Server) lookAt(w *world.World, descr int,
	who, target ref.Ref) {

	o := w.Get(target)
	me := w.Get(who)
	if o == nil || me == nil {
		s.notify(w, who, "I don't see that here.")
		return
	}

	switch o.Type() {
	case ref.TypeRoom:
		if me.Location != target &&
			!s.canLinkTo(w, descr, who, ref.TypeRoom,
				target) {
			s.notify(w, who, noLookRoom)
			return
		}
		s.lookRoom(w, descr, who, target)

	case ref.TypePlayer:
		if me.Location != o.Location &&
			!s.controls(w, who, target) {
			s.notify(w, who, noLookPlayer)
			return
		}
		s.lookSimple(w, descr, who, target)
		s.listContents(w, who, target, "Carrying:")
		s.lookQueue(w, descr, who, target)

	case ref.TypeThing:
		if me.Location != o.Location && o.Location != who &&
			!s.controls(w, who, target) {
			s.notify(w, who, noLookThing)
			return
		}
		s.lookSimple(w, descr, who, target)
		// A HAVEN thing keeps its contents to itself, and is
		// not marked as used either.
		if o.Flags&ref.Haven == 0 {
			s.listContents(w, who, target, "Contains:")
			w.Used(target)
		}
		s.lookQueue(w, descr, who, target)

	default:
		s.lookSimple(w, descr, who, target)
		// A program's use count means how often it has run,
		// so looking at one does not touch it.
		if o.Type() != ref.TypeProgram {
			w.Used(target)
		}
		s.lookQueue(w, descr, who, target)
	}
}

// lookSimple is look_simple: the description alone, with the
// nothing-special message when there is none.
func (s *Server) lookSimple(w *world.World, descr int,
	who, target ref.Ref) {

	if !hasMesg(w, target, propDesc) {
		s.send(w, who, w.Tune.String("description_default"))
		return
	}
	s.execOrNotifyProp(w, descr, who, target, propDesc,
		"(@Desc)", mesgArgs{})
}

// lookRoom is look_room: the name, the description, the success
// messages and the contents, in that order.
//
// It is reached with whatever the player is inside, which need not be
// a room — hence the @idescribe branch. A room with no description
// says nothing at all, unlike look_simple, which has a message for
// the case.
func (s *Server) lookRoom(w *world.World, descr int,
	who, loc ref.Ref) {

	o := w.Get(loc)
	if o == nil {
		return
	}
	s.send(w, who, unparse(w, who, loc))

	if o.Type() == ref.TypeRoom {
		if hasMesg(w, loc, propDesc) {
			s.execOrNotifyProp(w, descr, who, loc,
				propDesc, "(@Desc)", mesgArgs{})
		}
		// can_doit with no default failure message, which is
		// how a room's @succ and @ofail come to show on a
		// plain look.
		s.canDoit(w, descr, who, loc, "", mesgArgs{})
	} else if hasMesg(w, loc, propIDesc) {
		s.execOrNotifyProp(w, descr, who, loc, propIDesc,
			"(@Idesc)", mesgArgs{})
	}

	w.Used(loc)

	// Exits are deliberately not listed. Upstream's look_room
	// gives the name, the description and the contents and stops
	// there; a world that wants an "obvious exits" line supplies
	// it from its own programs, as the starter world does.
	s.listContents(w, who, loc, "Contents:")

	s.lookQueue(w, descr, who, loc)
}

// lookQueue runs the _lookq propqueue, which is the last thing a look
// of any kind does.
//
// Its argument is the dbref of what was looked at, written "#123",
// where every other propqueue passes a word — so a program hooked
// here is told what it is describing and a hook on #0 can serve the
// whole world. It is a private queue: whatever it produces goes to
// whoever looked.
func (s *Server) lookQueue(w *world.World, descr int,
	who, target ref.Ref) {

	s.envpropqueue(w, propqRun{
		descr: descr, player: who, where: target,
		trigger: who, what: target, exclude: ref.Nothing,
		arg: propqArg(target), mlev: 1, private: true,
	}, propLookQueue)
}

// listContents is look_contents: a heading, then whatever the player
// can see, and no output at all when they can see nothing.
func (s *Server) listContents(w *world.World, who, loc ref.Ref,
	heading string) {

	o := w.Get(loc)
	if o == nil {
		return
	}
	// Whether the container itself is visible decides how much of
	// what is inside it is: in a dark room only what the player
	// controls shows at all.
	seeLoc := o.Flags&ref.Dark == 0 || s.controls(w, who, loc)

	var names []string
	for _, r := range w.Contents(loc) {
		if s.canSee(w, who, r, seeLoc) {
			names = append(names, unparse(w, who, r))
		}
	}
	if len(names) == 0 {
		return
	}
	s.notify(w, who, "%s", heading)
	for _, n := range names {
		s.send(w, who, n)
	}
}

// canSee is look.c's own can_see, which asks a narrower question than
// whether an object may be examined.
//
// seeLoc is whether the container is itself visible. When it is not,
// the only things that show are ones the player controls — and not
// even those if the player is STICKY, which is upstream's way of
// letting somebody turn their own wizardly sight off.
func (s *Server) canSee(w *world.World, who, thing ref.Ref,
	seeLoc bool) bool {

	o, me := w.Get(thing), w.Get(who)
	if o == nil || me == nil || who == thing {
		return false
	}
	// Exits and rooms are never listed among contents.
	if t := o.Type(); t == ref.TypeExit || t == ref.TypeRoom {
		return false
	}

	owned := s.controls(w, who, thing) &&
		me.Flags&ref.Sticky == 0
	if !seeLoc {
		return owned
	}
	switch o.Type() {
	case ref.TypeProgram:
		// A program in a room is machinery rather than
		// scenery, so it shows only to whoever controls it
		// — or if it is a VEHICLE, which means it is
		// something to be got into.
		return o.Flags&ref.Vehicle != 0 ||
			s.controls(w, who, thing)
	case ref.TypePlayer:
		if w.Tune.Bool("dark_sleepers") {
			return o.Flags&ref.Dark == 0 &&
				s.hub.Online(thing)
		}
	}
	return o.Flags&ref.Dark == 0 || owned
}

// controls is World.Controls, which moved into internal/world when
// the matcher turned out to need it: `match_exits` weighs an exit's
// owner against where the searcher is standing, and internal/match
// cannot import this package.
func (s *Server) controls(w *world.World, who,
	target ref.Ref) bool {

	return w.Controls(who, target)
}

// cmdInventory lists what the player is carrying, and then what they
// are worth: do_inventory ends with do_score, which is why "You
// aren't carrying anything." is not the end of the command.
func (s *Server) cmdInventory(c *ctx) {
	if contents := c.w.Contents(c.who); len(contents) == 0 {
		c.tell("You aren't carrying anything.")
	} else {
		c.tell("You are carrying:")
		for _, r := range contents {
			c.send(unparse(c.w, c.who, r))
		}
	}
	s.cmdScore(c)
}
