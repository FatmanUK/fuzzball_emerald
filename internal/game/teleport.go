package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// internal/game/teleport.go is wiz.c:102's do_teleport, which is four
// commands wearing one name: a player is walked into the destination
// through enter_room, a thing or a program is simply put there, and a
// room is *reparented*. Each has its own permission rule, its own
// refusals and its own confirmation line — "Parent of X set to Y."
// for a room, "X teleported to Y." for everything else, where this
// server used to say "Teleported." for all of them.
//
// Three things about it are easy to get wrong.
//
// The permission test is **not** match_controlled, which is why
// @teleport was one of the four commands left on resolveControlled.
// It cannot be: the test depends on the destination, so it has to
// happen after both matches, and what it asks differs per victim
// type. A player needs control of the victim, the destination, the
// victim's location and — when the destination is a thing — the
// thing's location too. A thing or a program needs the destination
// controlled *or* linkable, and the victim *or* the victim's location
// controlled. A room needs the victim controlled and the destination
// linkable, and #0 is refused outright.
//
// The destination match is narrower than the victim's. Neither is
// match_everything: the victim search has no exits and takes
// match_player unconditionally, while the destination search drops
// match_neighbor as well and adds both back only for a wizard. So a
// mortal cannot name something lying in the room as a destination,
// which reads like an oversight and is reproduced.
//
// And a non-STICKY room with a drop-to swallows a thing teleported
// into it, exactly as dropping one there would.
const (
	// Upstream's refusals name the test that failed, which is
	// unusual enough that programs match on the whole line.
	noTelePlayer = "Permission denied. (must control " +
		"victim, dest, victim's loc, and dest's loc)"
	noTeleThing = "Permission denied. (must control dest " +
		"and be able to link to it, or control dest's loc)"
	noTeleRoom = "Permission denied. (Can't move #0, dest " +
		"must be linkable, and must control victim)"

	badDest       = "Bad destination."
	wrenching     = "You feel a wrenching sensation..."
	noTeleGarbage = "That object is in a place where " +
		"magic cannot reach it."
	godsPlacing = "God has already set that where God " +
		"wants it to be."
)

// cmdTeleport implements @teleport.
func (s *Server) cmdTeleport(c *ctx) {
	name, to, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	to = strings.TrimSpace(to)

	// One argument teleports the player themselves, and the
	// argument is the destination. Upstream tests arg2 for
	// emptiness rather than for an "=", so "@teleport here=" is
	// the same command as "@teleport here".
	if to == "" {
		name, to = "me", name
	}

	victim := match.New(c.w, c.who, name).
		Neighbor().Possession().Me().Here().
		Absolute().Registered().Player().Result()
	if !noisyMatch(c, name, victim) {
		return
	}
	if c.w.Tune.Bool("strict_god_priv") && c.who != ref.God &&
		ownerOf(c.w, victim) == ref.God {
		c.tell(godsPlacing)
		return
	}

	// The destination search asks for a player (wiz.c:117), where
	// the victim search above asks for nothing.
	m := match.New(c.w, c.who, to).
		PreferType(ref.TypePlayer).
		Possession().Me().Here().Home().
		Absolute().Registered()
	if isWizard(c.w, ownerOf(c.w, c.who)) {
		m = m.Neighbor().Player()
	}
	dest := m.Result()
	if !noisyMatch(c, to, dest) {
		return
	}

	// The victim's name is taken before anything moves, because a
	// move can change what unparse shows: a thing that lands
	// somewhere its owner cannot see renders differently.
	vname := s.unparse(c.w, c.who, victim)
	if dest == ref.Home {
		dest = teleportHome(c.w, victim)
	}

	o := c.w.Get(victim)
	if o == nil {
		c.tell(noTeleGarbage)
		return
	}
	switch o.Type() {
	case ref.TypePlayer:
		s.teleportPlayer(c, victim, dest, vname)
	case ref.TypeThing, ref.TypeProgram:
		s.teleportThing(c, victim, dest, vname)
	case ref.TypeRoom:
		s.teleportRoom(c, victim, dest, vname)
	case ref.TypeGarbage:
		c.tell(noTeleGarbage)
	default:
		c.tell("You can't teleport that.")
	}
}

// teleportHome resolves HOME as a destination, which depends on what
// is being sent there.
//
// It is not fallbackHome: that ladder is moveto's, and this one is
// do_teleport's. They differ for a player, who upstream sends to
// their owner's home when their own would make a loop — which for a
// player is the same place, since a player owns themselves. The rung
// is reproduced because the C has it.
func teleportHome(w *world.World, victim ref.Ref) ref.Ref {
	o := w.Get(victim)
	if o == nil {
		return ref.Nothing
	}
	ownersHome := func() ref.Ref {
		if p := w.Get(ownerOf(w, victim)); p != nil {
			return p.Home
		}
		return ref.Nothing
	}
	switch o.Type() {
	case ref.TypePlayer:
		dest := o.Home
		if parentLoopCheck(w, victim, dest) {
			dest = ownersHome()
		}
		return dest
	case ref.TypeThing:
		dest := o.Home
		if parentLoopCheck(w, victim, dest) {
			dest = ownersHome()
			if parentLoopCheck(w, victim, dest) {
				dest = ref.GlobalEnvironment
			}
		}
		return dest
	case ref.TypeRoom:
		return ref.GlobalEnvironment
	case ref.TypeProgram:
		return o.Owner
	}
	// Garbage and exits fall through to the start room, which the
	// per-type switch refuses anyway.
	return w.Tune.Ref("player_start")
}

// teleportPlayer walks a player into the destination, which is the
// one branch that goes through enter_room and so announces itself,
// runs the autolook and fires the drop-to.
func (s *Server) teleportPlayer(c *ctx, victim, dest ref.Ref,
	vname string) {

	o, d := c.w.Get(victim), c.w.Get(dest)
	if !s.controls(c.w, c.who, victim) ||
		!s.controls(c.w, c.who, dest) ||
		!s.controls(c.w, c.who, o.Location) ||
		d != nil && d.Type() == ref.TypeThing &&
			!s.controls(c.w, c.who, d.Location) {
		c.tell(noTelePlayer)
		return
	}
	if d.Type() != ref.TypeRoom && d.Type() != ref.TypeThing {
		c.tell(badDest)
		return
	}
	// A wizard may be put inside anything; everybody else needs
	// the thing to be a vehicle.
	if !o.Flags.IsWizard() && d.Type() == ref.TypeThing &&
		d.Flags&ref.Vehicle == 0 {
		c.tell("Destination object is not a vehicle.")
		return
	}
	if parentLoopCheck(c.w, victim, dest) {
		c.tell("Objects can't contain themselves.")
		return
	}

	s.send(c.w, victim, wrenching)
	// Named before the move, because enter_room is what runs the
	// player's own arrival messages and they may rename nothing
	// but may well print first.
	dname := s.unparse(c.w, c.who, dest)
	s.enterRoom(c.w, c.d.ID, victim, dest, o.Location)
	c.tell("%s teleported to %s.", vname, dname)
}

// teleportThing puts a thing or a program somewhere. Upstream falls
// one case into the other, so the only thing a program skips is the
// container check — it has no contents to put itself inside.
func (s *Server) teleportThing(c *ctx, victim, dest ref.Ref,
	vname string) {

	o := c.w.Get(victim)
	if o.Type() == ref.TypeThing &&
		parentLoopCheck(c.w, victim, dest) {
		c.tell("You can't make a container contain itself!")
		return
	}
	d := c.w.Get(dest)
	if d == nil || d.Type() != ref.TypeRoom &&
		d.Type() != ref.TypePlayer &&
		d.Type() != ref.TypeThing {
		c.tell(badDest)
		return
	}
	if !((s.controls(c.w, c.who, dest) ||
		s.canTeleportTo(c, dest)) &&
		(s.controls(c.w, c.who, victim) ||
			s.controls(c.w, c.who, o.Location))) {
		c.tell(noTeleThing)
		return
	}

	// A room with a drop-to takes the thing straight through,
	// unless it is STICKY and so holds its contents until the
	// last person leaves. Same rule as dropping something there.
	if d.Type() == ref.TypeRoom && d.Dropto != ref.Nothing &&
		d.Flags&ref.Sticky == 0 {
		dest = d.Dropto
	}

	if c.w.Tune.Bool("secure_thing_movement") &&
		o.Type() == ref.TypeThing {
		if o.Flags&ref.Zombie != 0 {
			s.send(c.w, victim, wrenching)
		}
		s.enterRoom(c.w, c.d.ID, victim, dest, o.Location)
	} else {
		moveObject(c.w, victim, dest)
	}
	c.tell("%s teleported to %s.", vname,
		s.unparse(c.w, c.who, dest))
}

// teleportRoom reparents a room, which is what @teleport does to one
// — there is no other command for it, and the confirmation says so.
func (s *Server) teleportRoom(c *ctx, victim, dest ref.Ref,
	vname string) {

	d := c.w.Get(dest)
	if d == nil || d.Type() != ref.TypeRoom {
		c.tell(badDest)
		return
	}
	if !s.controls(c.w, c.who, victim) ||
		!s.canTeleportTo(c, dest) ||
		victim == ref.GlobalEnvironment {
		c.tell(noTeleRoom)
		return
	}
	if parentLoopCheck(c.w, victim, dest) {
		c.tell("Parent would create a loop.")
		return
	}
	moveObject(c.w, victim, dest)
	c.tell("Parent of %s set to %s.", vname,
		s.unparse(c.w, c.who, dest))
}
