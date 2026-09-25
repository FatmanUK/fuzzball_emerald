package game

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// useExit moves a player through an exit.
func (s *Server) useExit(c *ctx, exit ref.Ref) {
	e := c.w.Get(exit)
	if e == nil {
		return
	}
	c.w.Used(exit)

	// could_doit gates every kind of exit traversal, the
	// no-destination case included: its own @lock, and — when
	// the exit does not sit directly in a room — the
	// destination-reachability rules (JUMP_OK, GUEST rooms,
	// BUILDER sources, secure_teleport).
	//
	// An unlinked exit used to be handled separately and answered
	// "That exit doesn't go anywhere.", which was invented here.
	// Upstream does not special-case it at all: the destination
	// count is could_doit's own first check, so an unlinked exit
	// gets the one default that every other failure gets.
	if !couldDoit(s, c.w, c.d.ID, 1, c.who, exit) {
		s.exitFailMessages(c, exit)
		return
	}

	dest := e.Dest[0]
	if dest == ref.Home {
		dest = c.w.Get(c.who).Home
	}
	if dest == ref.Nil {
		// A nil link runs whatever messages the exit carries
		// and stops.
		s.exitMessages(c, exit)
		return
	}
	if !c.w.Valid(dest) {
		c.tell("That exit leads nowhere.")
		return
	}
	if c.w.Get(dest).Type() == ref.TypeProgram {
		s.runProgram(c, dest, exit, c.arg)
		return
	}

	s.exitMessages(c, exit)
	s.moveTo(c.w, c.d.ID, c.who, dest, exit)
}

// exitMessages shows an exit's success messages to the player and the
// room.
func (s *Server) exitMessages(c *ctx, exit ref.Ref) {
	s.execOrNotifyProp(c.w, c.d.ID, c.who, exit, propSucc,
		"(@Succ)")
	o := c.w.Get(c.who)
	if o.Location != ref.Nothing {
		s.parseOProp(c.w, c.d.ID, c.who, o.Location, exit,
			propOSucc, o.Name, "(@Osucc)")
	}
}

// exitFailMessages shows an exit's failure messages to the player and
// the room, upstream's can_doit failure branch: the exit's own @fail
// message, or "You can't go that way." if it has none, plus @ofail to
// the room.
func (s *Server) exitFailMessages(c *ctx, exit ref.Ref) {
	if hasMesg(c.w, exit, propFail) {
		s.execOrNotifyProp(c.w, c.d.ID, c.who, exit, propFail,
			"(@Fail)")
	} else {
		c.tell("You can't go that way.")
	}
	o := c.w.Get(c.who)
	if o.Location != ref.Nothing {
		s.parseOProp(c.w, c.d.ID, c.who, o.Location, exit,
			propOFail, o.Name, "(@Ofail)")
	}
}

// moveTo relocates a player, showing the exit's drop messages before
// enter_room narrates the move itself.
//
// Upstream's own order: do_move prints @drop and @odrop and *then*
// calls enter_room, so a message about arriving is read before "has
// arrived" and before the look. Emerald had it the other way round.
func (s *Server) moveTo(w *world.World, descr int,
	who, dest, via ref.Ref) {

	o := w.Get(who)
	if via != ref.Nothing {
		s.execOrNotifyProp(w, descr, who, via, propDrop,
			"(@Drop)")
		// A DARK player announces nothing to the room.
		if o.Flags&ref.Dark == 0 {
			s.parseOProp(w, descr, who, dest, via,
				propODrop, o.Name, "(@Odrop)")
		}
	}
	s.enterRoom(w, descr, who, dest, via)
}

// cmdGo moves through a named exit.
func (s *Server) cmdGo(c *ctx) {
	if c.arg == "" {
		c.tell("Go where?")
		return
	}
	if ascEqual(c.arg, "home") {
		s.cmdHome(c)
		return
	}
	r := match.New(c.w, c.who, c.arg).Exits().Result()
	switch r {
	case ref.Nothing:
		c.tell("You can't go that way.")
	case ref.Ambiguous:
		c.tell("I don't know which way you mean.")
	default:
		s.useExit(c, r)
	}
}

// cmdHome sends the player to their home.
func (s *Server) cmdHome(c *ctx) {
	if !c.w.Tune.Bool("enable_home") {
		c.tell("That command is disabled.")
		return
	}
	home := c.w.Get(c.who).Home
	if !c.w.Valid(home) {
		c.tell("You have no home to go to.")
		return
	}
	c.tell("There's no place like home...")
	s.moveTo(c.w, c.d.ID, c.who, home, ref.Nothing)
}

// cmdGet picks something up.
func (s *Server) cmdGet(c *ctx) {
	if c.arg == "" {
		c.tell("Get what?")
		return
	}
	target := match.New(c.w, c.who, c.arg).Neighbor().Absolute().Result()
	switch target {
	case ref.Nothing:
		c.tell("I don't see that here.")
		return
	case ref.Ambiguous:
		c.tell("I don't know which one you mean.")
		return
	}

	o := c.w.Get(target)
	if o.Type() != ref.TypeThing {
		c.tell("You can't pick that up.")
		return
	}
	if o.Location == c.who {
		c.tell("You already have that.")
		return
	}
	if err := c.w.MoveTo(target, c.who); err != nil {
		c.tell("You can't pick that up.")
		return
	}
	c.tell("Taken.")
	me := c.w.Get(c.who)
	if me.Location != ref.Nothing {
		s.notifyRoom(c.w, me.Location, []ref.Ref{c.who},
			"%s picks up %s.", me.Name, o.Name)
	}
}

// cmdDrop puts something down.
func (s *Server) cmdDrop(c *ctx) {
	if c.arg == "" {
		c.tell("Drop what?")
		return
	}
	target := match.New(c.w, c.who, c.arg).Possession().Result()
	switch target {
	case ref.Nothing:
		c.tell("You aren't carrying that.")
		return
	case ref.Ambiguous:
		c.tell("I don't know which one you mean.")
		return
	}

	me := c.w.Get(c.who)
	if me.Location == ref.Nothing {
		c.tell("There is nowhere to drop it.")
		return
	}
	o := c.w.Get(target)

	// A STICKY thing goes home instead of landing where it was
	// dropped.
	dest := me.Location
	if o.Flags&ref.Sticky != 0 && c.w.Valid(o.Home) {
		dest = o.Home
	}
	if err := c.w.MoveTo(target, dest); err != nil {
		c.tell("You can't drop that.")
		return
	}
	c.tell("Dropped.")
	s.notifyRoom(c.w, me.Location, []ref.Ref{c.who},
		"%s drops %s.", me.Name, o.Name)
}
