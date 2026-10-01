package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// internal/game/sweep.go is look.c:1961's do_sweep, a security sweep
// of a room: who or what here could be repeating what is said, and
// which of the four talking commands have been replaced by an exit.
//
// Upstream's own doc comment calls it "not much of a security sweep
// really", and it is right: a dark player in a lit room is not
// reported, and neither is anything a level deeper than the room's
// own contents. It reports rather than fixes, which is why it ships
// on the LISTENER flag alone and ahead of the propqueues that make
// the flag mean anything.
//
// **It tests the flag *and* the property.** `set_property` only ever
// sets LISTENER and nothing in a non-DISKBASE build clears it, so a
// thing that has had its `_listen` deleted stays flagged for the life
// of the process. Asking for the property as well is what makes that
// invisible — and it is upstream's own code, not a correction.
//
// **The four commands it checks for are not checked the same way.**
// `page`, `whisper` and `say` are prefix tests — an exit named `p`
// traps `page` — while `pose` is tried exactly as `pose`, then
// `pos`, then `po`, stopping at the first that matches. An unlinked
// exit traps nothing, because `exit_matches_name` requires a
// destination.
//
// **The environment header is printed unconditionally**, before
// anything is found and even when the walk reports nothing, because
// upstream's `flag` is tested at the top of a loop that always runs
// at least once.
func (s *Server) cmdSweep(c *ctx) {
	name := strings.TrimSpace(c.arg)

	var thing ref.Ref
	if name == "" || ascii.EqualFold(name, "here") {
		thing = c.w.Get(c.who).Location
	} else {
		thing = match.New(c.w, c.who, name).Everything().Result()
		if !noisyMatch(c, name, thing) {
			return
		}
	}

	// "here" by name is checked, and "here" by default is not:
	// upstream tests the argument rather than the result, so a
	// bare @sweep works in somebody else's room and "@sweep here"
	// does not.
	if name != "" &&
		!s.controls(c.w, ownerOf(c.w, c.who), thing) {
		c.tell("Permission denied. (You can't perform a " +
			"security sweep in a room you don't own)")
		return
	}

	c.tell("Listeners in %s:", unparse(c.w, c.who, thing))

	dark := hasFlag(c.w, thing, ref.Dark)
	for _, r := range c.w.Contents(thing) {
		o := c.w.Get(r)
		if o == nil {
			continue
		}
		switch o.Type() {
		case ref.TypePlayer:
			s.sweepPlayer(c, r, dark)
		case ref.TypeThing:
			s.sweepThing(c, r)
			s.sweepTraps(c, r)
		}
	}

	c.tell("Listening rooms down the environment:")
	for loc := thing; loc != ref.Nothing; loc = getParent(c.w, loc) {
		if c.w.IsListener(loc) {
			c.tell("  %s is a listening room.",
				unparse(c.w, c.who, loc))
		}
		s.sweepTraps(c, loc)
	}

	c.tell("**End of list**")
}

// sweepPlayer reports a player in the room, awake or asleep.
//
// A player in a DARK room is reported only while connected, which is
// upstream's one concession to a room that is meant to hide its
// contents — and the reason its own comment calls the command weak:
// a DARK *player* in a lit room is reported like anybody else.
func (s *Server) sweepPlayer(c *ctx, r ref.Ref, darkRoom bool) {
	awake := s.hub.Online(r)
	if darkRoom && !awake {
		return
	}
	asleep := "sleeping "
	if awake {
		asleep = ""
	}
	c.tell("  %s is a %splayer.", unparse(c.w, c.who, r), asleep)
}

// sweepThing reports a thing that is a zombie, a listener, or both.
//
// The line is built up a clause at a time and then printed only if
// something in it had teeth: a sleeping zombie that is not also a
// listener is assembled and thrown away, which is why a puppet whose
// owner is offline does not appear.
func (s *Server) sweepThing(c *ctx, r ref.Ref) {
	o := c.w.Get(r)
	if o.Flags&(ref.Zombie|ref.Listener) == 0 {
		return
	}

	var b strings.Builder
	b.WriteString("  " + unparse(c.w, c.who, r) + " is a")
	tell := false

	if o.Flags&ref.Zombie != 0 {
		tell = true
		if !s.hub.Online(ownerOf(c.w, r)) {
			tell = false
			b.WriteString(" sleeping")
		}
		b.WriteString(" zombie")
	}
	if c.w.IsListener(r) {
		b.WriteString(" listener")
		tell = true
	}

	b.WriteString(" object owned by " +
		unparse(c.w, c.who, ownerOf(c.w, r)) + ".")
	if tell {
		c.send(b.String())
	}
}

// sweepTraps reports an exit on obj that has taken over one of the
// four commands used to talk.
func (s *Server) sweepTraps(c *ctx, obj ref.Ref) {
	s.sweepTrap(c, obj, "page", false)
	s.sweepTrap(c, obj, "whisper", false)

	// pose is the odd one: three exact names, longest first, and
	// only the first that matches is reported.
	if !s.sweepTrap(c, obj, "pose", true) &&
		!s.sweepTrap(c, obj, "pos", true) {
		s.sweepTrap(c, obj, "po", true)
	}

	s.sweepTrap(c, obj, "say", false)
}

// sweepTrap is exit_match_exists (look.c:1911), and reports whether
// it found anything so pose's three-way test can stop.
func (s *Server) sweepTrap(c *ctx, obj ref.Ref, name string,
	exact bool) bool {

	for _, e := range c.w.Exits(obj) {
		if !exitTrapsName(c.w, e, name, exact) {
			continue
		}
		c.tell("  %ss are trapped on %s", name,
			unparse(c.w, c.who, obj))
		return true
	}
	return false
}

// exitTrapsName is exit_matches_name: does any of an exit's aliases
// claim this command?
//
// The inexact form is `string_prefix(name, alias)` — the *alias* is
// the prefix — so an exit named "p" traps "page" and one named
// "paget" does not. An exit with no destination traps nothing,
// whatever it is called.
func exitTrapsName(w *world.World, e ref.Ref, name string,
	exact bool) bool {

	o := w.Get(e)
	if o == nil || len(o.Dest) == 0 {
		return false
	}
	for _, alias := range strings.Split(o.Name,
		string(match.ExitDelimiter)) {
		if alias == "" {
			continue
		}
		if exact && ascii.EqualFold(name, alias) {
			return true
		}
		if !exact && ascii.HasPrefix(name, alias) {
			return true
		}
	}
	return false
}
