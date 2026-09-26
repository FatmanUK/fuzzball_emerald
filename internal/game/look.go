package game

import (
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

// cmdLook shows the room, or an object in it.
func (s *Server) cmdLook(c *ctx) {
	if c.arg == "" {
		s.lookHere(c.w, c.d.ID, c.who)
		return
	}
	// do_look_at's own matcher, which is *narrower* than
	// match_everything: no registrations, so "look $thing" finds
	// nothing even when a program could resolve the name. And a
	// failed match says what match_msg_nomatch says, which is
	// where the look-trap branch ends up.
	m := match.New(c.w, c.who, c.arg).Exits().Neighbor().
		Possession()
	if isWizard(c.w, ownerOf(c.w, c.who)) {
		m = m.Absolute().Player()
	}
	target := m.Here().Me().Result()
	if !noisyMatch(c, c.arg, target) {
		return
	}
	s.lookAt(c.w, c.d.ID, c.who, target)
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
			!s.canLinkTo(w, who, target) {
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

	default:
		s.lookSimple(w, descr, who, target)
		// A program's use count means how often it has run,
		// so looking at one does not touch it.
		if o.Type() != ref.TypeProgram {
			w.Used(target)
		}
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
	s.execOrNotifyProp(w, descr, who, target, propDesc, "(@Desc)")
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
				propDesc, "(@Desc)")
		}
		// can_doit with no default failure message, which is
		// how a room's @succ and @ofail come to show on a
		// plain look.
		s.canDoit(w, descr, who, loc, "")
	} else if hasMesg(w, loc, propIDesc) {
		s.execOrNotifyProp(w, descr, who, loc, propIDesc,
			"(@Idesc)")
	}

	w.Used(loc)

	// Exits are deliberately not listed. Upstream's look_room
	// gives the name, the description and the contents and stops
	// there; a world that wants an "obvious exits" line supplies
	// it from its own programs, as the starter world does.
	s.listContents(w, who, loc, "Contents:")
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

// controls reports whether a player may modify an object.
//
// The test is made on whoever owns the asking object, not the object
// itself, so a puppet controls exactly what its owner does — which
// is what lets a program running as a thing touch its owner's things.
//
// A wizard controls everything, with one exception: while
// strict_god_priv is set, only God may touch God's objects. Without
// that a wizard could edit God's programs and so give themselves
// God's powers.
func (s *Server) controls(w *world.World, who, target ref.Ref) bool {
	o := w.Get(target)
	if o == nil {
		return false
	}
	owner := ownerOf(w, who)
	p := w.Get(owner)
	if p == nil {
		return false
	}
	if p.Flags.IsWizard() {
		if w.Tune.Bool("strict_god_priv") &&
			o.Owner == ref.God && owner != ref.God {
			return false
		}
		return true
	}
	if who == target {
		return true
	}
	return o.Owner == owner
}

// cmdInventory lists what the player is carrying.
func (s *Server) cmdInventory(c *ctx) {
	contents := c.w.Contents(c.who)
	if len(contents) == 0 {
		c.tell("You aren't carrying anything.")
		return
	}
	c.tell("You are carrying:")
	for _, r := range contents {
		c.send(unparse(c.w, c.who, r))
	}
}
