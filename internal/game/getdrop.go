package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// get, drop, and the five spellings of the two, from move.c:829 and
// :956.
//
// Emerald's own versions were about twenty lines each — match, type
// check, move, "Taken."/"Dropped." — where do_get is ninety and
// do_drop a hundred. That is why put, throw and hand could not be
// aliases of them: most of what those three do is the second argument
// the old ones did not have.
//
// "take" is another spelling of get; "put", "throw" and "hand" are
// other spellings of drop. Upstream really does dispatch them to the
// same two functions, and its own comment on do_drop says the three
// differ only in their help files.

func init() {
	register("get", (*Server).cmdGet)
	register("take", (*Server).cmdGet)
	register("drop", (*Server).cmdDrop)
	register("put", (*Server).cmdDrop)
	register("throw", (*Server).cmdDrop)
	register("hand", (*Server).cmdDrop)
}

// cmdGet is do_get: pick something up, optionally out of something
// else.
//
// With two arguments the *first* names the container and the second
// what to take out of it, which reads backwards until you notice that
// "get bag=key" is the same shape as every other two-argument command
// here.
func (s *Server) cmdGet(c *ctx) {
	name, inner, hasInner := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	inner = strings.TrimSpace(inner)
	hasInner = hasInner && inner != ""

	wizard := isWizard(c.w, ownerOf(c.w, c.who))

	m := match.New(c.w, c.who, name).Neighbor().Possession()
	if wizard {
		// The wizard has long fingers, as upstream puts it.
		m = m.Absolute()
	}
	thing := m.Result()
	if !noisyMatch(c, name, thing) {
		return
	}

	cont := thing
	if hasInner {
		m := match.New(c.w, c.who, inner).Inside(cont)
		if wizard {
			m = m.Absolute()
		}
		thing = m.Result()
		if !noisyMatch(c, inner, thing) {
			return
		}
		// A container's @conlock defaults to *false*: a
		// container nobody has unlocked is shut.
		if !s.lockPasses(c.w, c.d.ID, 1, c.who, cont,
			propConLock, false) {
			c.tell("You can't open that container.")
			return
		}
	}

	o := c.w.Get(thing)
	me := c.w.Get(c.who)
	if o == nil || me == nil {
		return
	}

	// A puppet may not take something out of anywhere but a room
	// unless it belongs to the same owner, which upstream reads
	// as stopping it raiding other people's containers.
	if me.Type() != ref.TypePlayer && o.Location != ref.Nothing {
		if loc := c.w.Get(o.Location); loc != nil &&
			loc.Type() != ref.TypeRoom &&
			ownerOf(c.w, c.who) != o.Owner {
			c.tell("Zombies aren't allowed to be " +
				"thieves!")
			return
		}
	}
	if o.Location == c.who {
		c.tell("You already have that!")
		return
	}
	if cp := c.w.Get(cont); cp != nil &&
		cp.Type() == ref.TypePlayer {
		c.tell("You can't steal stuff from players.")
		return
	}
	if parentLoopCheck(c.w, thing, c.who) {
		c.tell("You can't pick yourself up by your " +
			"bootstraps!")
		return
	}

	switch o.Type() {
	case ref.TypeThing, ref.TypeProgram:
		if o.Type() == ref.TypeThing {
			c.w.Used(thing)
		}
		// The two paths differ in their messages, not only in
		// whether they print one: taking from a container
		// reports could_doit's failure in its own words and
		// shows no success messages, where taking from the
		// floor runs can_doit and so shows @succ and @osucc.
		if hasInner {
			ok := couldDoit(s, c.w, c.d.ID, 1, c.who,
				thing)
			if !ok {
				c.tell("You can't get that.")
				return
			}
		} else if !s.canDoit(c.w, c.d.ID, c.who, thing,
			"You can't pick that up.") {
			return
		}
		s.moveThing(c.w, c.d.ID, thing, c.who, o.Location)
		c.tell("Taken.")
	default:
		c.tell("You can't take that!")
	}
}

// cmdDrop is do_drop: put something down, or into something, or into
// somebody's hands.
//
// The reply says which of those three happened — "Dropped.", "Put
// away.", or a pair of lines naming both people — and only the
// first runs the @drop and @odrop messages.
func (s *Server) cmdDrop(c *ctx) {
	name, target, hasTarget := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	target = strings.TrimSpace(target)
	hasTarget = hasTarget && target != ""

	me := c.w.Get(c.who)
	if me == nil {
		return
	}
	loc := me.Location

	thing := match.New(c.w, c.who, name).Possession().Result()
	if !noisyMatch(c, name, thing) {
		return
	}

	cont := loc
	if hasTarget {
		m := match.New(c.w, c.who, target).
			Possession().Neighbor()
		if isWizard(c.w, ownerOf(c.w, c.who)) {
			m = m.Absolute()
		}
		cont = m.Result()
		if !noisyMatch(c, target, cont) {
			return
		}
	}

	o, cp := c.w.Get(thing), c.w.Get(cont)
	if o == nil || cp == nil {
		return
	}
	if t := o.Type(); t != ref.TypeThing && t != ref.TypeProgram {
		c.tell("You can't drop that.")
		return
	}
	if o.Type() == ref.TypeThing {
		c.w.Used(thing)
	}
	if o.Location != c.who {
		// Upstream calls this one "shouldn't ever happen",
		// since the match was against what the player is
		// carrying.
		c.tell("You can't drop that.")
		return
	}
	switch cp.Type() {
	case ref.TypeRoom, ref.TypePlayer, ref.TypeThing:
	default:
		c.tell("You can't put anything in that.")
		return
	}
	if cp.Type() != ref.TypeRoom &&
		!s.lockPasses(c.w, c.d.ID, 1, c.who, cont,
			propConLock, false) {
		c.tell("You don't have permission to put " +
			"something in that.")
		return
	}
	if parentLoopCheck(c.w, thing, cont) {
		c.tell("You can't put something inside of itself.")
		return
	}

	switch {
	case cp.Type() == ref.TypeRoom && o.Type() == ref.TypeThing &&
		o.Flags&ref.Sticky != 0:
		// A STICKY thing dropped in a room goes home instead.
		s.sendHome(c.w, c.d.ID, thing, false)
	default:
		// A room with a drop-to that is *not* STICKY takes
		// the thing straight through it: the drop-to is
		// immediate and only a STICKY room holds things until
		// everybody leaves.
		dest := cont
		if cp.Type() == ref.TypeRoom &&
			cp.Dropto != ref.Nothing &&
			cp.Flags&ref.Sticky == 0 {
			dest = cp.Dropto
		}
		s.moveThing(c.w, c.d.ID, thing, dest, c.who)
	}

	switch cp.Type() {
	case ref.TypeThing:
		c.tell("Put away.")
		return
	case ref.TypePlayer:
		s.notify(c.w, cont, "%s hands you %s",
			me.Name, o.Name)
		c.tell("You hand %s to %s", o.Name, cp.Name)
		return
	}

	// Dropping into a room is the only case with messages, and
	// both halves fall back: the thing's own @drop or "Dropped.",
	// and its @odrop or "X drops Y." to the room. The *room* may
	// carry a @drop and an @odrop of its own as well, and those
	// have no fallback.
	if hasMesg(c.w, thing, propDrop) {
		s.execOrNotifyProp(c.w, c.d.ID, c.who, thing,
			propDrop, "(@Drop)")
	} else {
		c.tell("Dropped.")
	}
	if hasMesg(c.w, loc, propDrop) {
		s.execOrNotifyProp(c.w, c.d.ID, c.who, loc, propDrop,
			"(@Drop)")
	}
	if hasMesg(c.w, thing, propODrop) {
		s.parseOProp(c.w, c.d.ID, c.who, loc, thing,
			propODrop, me.Name, "(@Odrop)")
	} else {
		s.notifyRoom(c.w, loc, []ref.Ref{c.who},
			"%s drops %s.", me.Name, o.Name)
	}
	// The room's @odrop is prefixed with the *thing's* name
	// rather than the player's, which reads as the object doing
	// something on arrival.
	if hasMesg(c.w, loc, propODrop) {
		s.parseOProp(c.w, c.d.ID, c.who, loc, loc, propODrop,
			o.Name, "(@Odrop)")
	}
}

// moveThing moves something the way get and drop do, which is not
// always the same way.
//
// With secure_thing_movement set, a THING travels through enter_room
// — announcing itself, running the autolook, everything a player's
// move does — and otherwise it is simply moved. That parameter is
// off by default, which is why picking something up is usually
// silent.
func (s *Server) moveThing(w *world.World, descr int,
	thing, dest, from ref.Ref) {

	o := w.Get(thing)
	if o != nil && o.Type() == ref.TypeThing &&
		w.Tune.Bool("secure_thing_movement") {
		s.enterRoom(w, descr, thing, dest, from)
		return
	}
	moveObject(w, thing, dest)
}

// sendHome is move.c's send_home: put something back where it lives.
//
// A player's possessions go home *first*, so they are there to be
// seen on arrival — which is upstream's own comment and the reason
// the order matters.
//
// The THING branch tests LISTENER as well as ZOMBIE, and Emerald does
// not maintain the LISTENER flag yet: it is derived on property write
// and belongs with the propqueues. So a listening thing that is not a
// zombie goes home quietly here where upstream announces it.
func (s *Server) sendHome(w *world.World, descr int,
	thing ref.Ref, puppetHome bool) {

	o := w.Get(thing)
	if o == nil {
		return
	}
	switch o.Type() {
	case ref.TypePlayer:
		s.sendContents(w, descr, thing, ref.Home)
		s.enterRoom(w, descr, thing, o.Home, o.Location)
	case ref.TypeThing:
		if puppetHome {
			s.sendContents(w, descr, thing, ref.Home)
		}
		if w.Tune.Bool("secure_thing_movement") ||
			o.Flags&ref.Zombie != 0 {
			s.enterRoom(w, descr, thing, o.Home,
				o.Location)
			return
		}
		moveObject(w, thing, ref.Home)
	case ref.TypeProgram:
		moveObject(w, thing, o.Owner)
	}
}

func init() {
	register("leave", (*Server).cmdLeave)
	register("disembark", (*Server).cmdDisembark)
}

// cmdLeave is do_leave (move.c:772): get out of a vehicle.
//
// Each refusal names a different reason and they are not
// interchangeable — a room, a thing that is not a vehicle, a
// vehicle inside a player, and a vehicle whose outside is inside
// itself.
func (s *Server) cmdLeave(c *ctx) {
	me := c.w.Get(c.who)
	if me == nil {
		return
	}
	loc := c.w.Get(me.Location)
	if loc == nil || loc.Type() == ref.TypeRoom {
		c.tell("You can't go that way.")
		return
	}
	if loc.Flags&ref.Vehicle == 0 {
		c.tell("You can only exit vehicles.")
		return
	}
	dest := loc.Location
	d := c.w.Get(dest)
	if d == nil || (d.Type() != ref.TypeRoom &&
		d.Type() != ref.TypeThing) {
		c.tell("You can't exit a vehicle inside of a player.")
		return
	}
	if parentLoopCheck(c.w, c.who, dest) {
		c.tell("You can't go that way.")
		return
	}
	c.tell("You exit the vehicle.")
	s.enterRoom(c.w, c.d.ID, c.who, dest, me.Location)
}

// cmdDisembark is another spelling of leave. Upstream dispatches both
// to do_leave, so they are the same command rather than one calling
// the other.
func (s *Server) cmdDisembark(c *ctx) { s.cmdLeave(c) }
