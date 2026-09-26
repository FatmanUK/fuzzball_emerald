package game

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// useExit is do_move's tail: can_doit, then trigger.
func (s *Server) useExit(c *ctx, exit ref.Ref) {
	if c.w.Get(exit) == nil {
		return
	}
	c.w.Used(exit)

	// can_doit gates every kind of exit traversal, the
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
	if !s.canDoit(c.w, c.d.ID, c.who, exit,
		"You can't go that way.") {
		return
	}
	s.trigger(c, exit, true)
}

// trigger is move.c:482: do whatever an exit's destinations say.
//
// An exit has a *list* of destinations and every one of them fires,
// which is what a metalink is: an exit whose destination is another
// exit runs that one too. What each destination does depends on its
// type, and Emerald used to treat everything that was not a room, a
// program or NIL as a plain move:
//
//   - A room is walked into.
//   - A thing is *boarded* if the exit is inside it and it is a
//     VEHICLE; otherwise the thing is brought to the exit — to the
//     exit's own location, or to its location's location when
//     the exit hangs on a thing. A non-STICKY exit that moved a
//     thing this way then sends the exit's home object home.
//   - A player is jumped to, if they are JUMP_OK.
//   - An exit is another trigger, with pflag off so its rooms and
//     players are ignored.
//   - A program is run.
//
// pflag is upstream's: with it off, rooms and players are skipped,
// which is how a metalink avoids moving the player twice.
//
// "Done." is what an exit says when nothing it pointed at counted as
// a success, which includes an exit with no destinations at all.
func (s *Server) trigger(c *ctx, exit ref.Ref, pflag bool) {
	e := c.w.Get(exit)
	if e == nil {
		return
	}
	me := c.w.Get(c.who)
	if me == nil {
		return
	}

	succ := false
	// sobjact is upstream's sticky-object-action flag: a thing
	// moved by a non-STICKY exit sends the exit's own home object
	// home afterwards.
	sobjact := false

	for _, dest := range e.Dest {
		if dest == ref.Home {
			dest = me.Home
			if d := c.w.Get(dest); d != nil &&
				d.Type() == ref.TypeThing {
				c.tell("That would be an undefined " +
					"operation.")
				continue
			}
		}
		if dest == ref.Nil {
			// An exit that goes nowhere does nothing, and
			// that counts as a success rather than an
			// error.
			succ = true
			continue
		}
		d := c.w.Get(dest)
		if d == nil {
			continue
		}

		switch d.Type() {
		case ref.TypeRoom:
			if pflag && s.enterViaExit(c, exit, dest) {
				succ = true
			}
		case ref.TypeThing:
			boarding := dest == e.Location &&
				d.Flags&ref.Vehicle != 0
			if boarding {
				if pflag && s.enterViaExit(c, exit,
					dest) {
					succ = true
				}
				break
			}
			if s.bringThing(c, exit, dest) {
				sobjact = true
			}
			if hasMesg(c.w, exit, propSucc) {
				succ = true
			}
		case ref.TypePlayer:
			if !pflag || d.Location == ref.Nothing {
				break
			}
			if parentLoopCheck(c.w, c.who, dest) {
				c.tell("That would cause a paradox.")
				break
			}
			succ = true
			if d.Flags&ref.JumpOK == 0 {
				c.tell("That player does not wish " +
					"to be disturbed.")
				break
			}
			s.enterViaExit(c, exit, d.Location)
		case ref.TypeExit:
			// A metalink: run the other exit, with pflag
			// off so it cannot move the player itself.
			c.w.Used(dest)
			s.trigger(c, dest, false)
			if hasMesg(c.w, exit, propSucc) {
				succ = true
			}
		case ref.TypeProgram:
			if isGuest(c.w, c.who) &&
				(d.Flags|e.Flags)&ref.Guest != 0 {
				c.tell("You can't go that way.")
				break
			}
			s.runProgram(c, dest, exit, c.arg)
			return
		}
	}

	if sobjact {
		s.sendHome(c.w, c.d.ID, e.Location, false)
	}
	if !succ && pflag {
		c.tell("Done.")
	}
}

// enterViaExit is the move a room, a vehicle or a jump makes: the
// exit's @drop and @odrop, then enter_room. It reports whether it
// happened.
//
// The three destination types that reach it share this shape exactly,
// which is why it is one function; what differs is the loop check and
// the guards each does first.
func (s *Server) enterViaExit(c *ctx, exit, dest ref.Ref) bool {
	if parentLoopCheck(c.w, c.who, dest) {
		c.tell("That would cause a paradox.")
		return false
	}
	me, d, e := c.w.Get(c.who), c.w.Get(dest), c.w.Get(exit)
	if me == nil || d == nil || e == nil {
		return false
	}
	// A zombie may not enter a room set ZOMBIE, a vehicle may not
	// enter a vehicle, and a guest may not pass a GUEST room or
	// exit. All three say the same thing.
	wizard := isWizard(c.w, ownerOf(c.w, c.who))
	switch {
	case !wizard && me.Type() == ref.TypeThing &&
		d.Flags&ref.Zombie != 0,
		me.Flags&ref.Vehicle != 0 &&
			(d.Flags|e.Flags)&ref.Vehicle != 0,
		isGuest(c.w, c.who) &&
			(d.Flags|e.Flags)&ref.Guest != 0:
		c.tell("You can't go that way.")
		return false
	}

	s.moveTo(c.w, c.d.ID, c.who, dest, exit)
	return true
}

// bringThing is trigger's other thing branch: an exit linked to a
// thing it does not live inside *fetches* that thing rather than
// moving the player.
//
// Where it fetches it to depends on what the exit hangs on: the
// exit's own location normally, and its location's location when the
// exit hangs on a thing — so an exit on a bag brings something to
// the room the bag is in, not into the bag.
//
// It reports whether the caller should then send the exit's home
// object home, which a non-STICKY exit does.
func (s *Server) bringThing(c *ctx, exit, thing ref.Ref) bool {
	e := c.w.Get(exit)
	if e == nil {
		return false
	}
	to := e.Location
	onAThing := false
	if src := c.w.Get(to); src != nil &&
		src.Type() == ref.TypeThing {
		to = src.Location
		onAThing = true
	}
	if parentLoopCheck(c.w, thing, to) {
		c.tell("That would cause a paradox.")
		return false
	}
	s.moveThing(c.w, c.d.ID, thing, to, exit)
	return onAThing && e.Flags&ref.Sticky == 0
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
