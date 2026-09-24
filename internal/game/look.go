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
		s.lookHere(c.w, c.who)
		return
	}
	target := match.New(c.w, c.who, c.arg).Everything().Result()
	switch target {
	case ref.Nothing:
		c.tell("I don't see that here.")
	case ref.Ambiguous:
		c.tell("I don't know which one you mean.")
	default:
		s.lookAt(c.w, c.who, target)
	}
}

// lookHere shows the room a player is standing in.
func (s *Server) lookHere(w *world.World, who ref.Ref) {
	o := w.Get(who)
	if o == nil {
		return
	}
	if o.Location == ref.Nothing {
		s.notify(w, who, "You are nowhere.")
		return
	}
	s.lookAt(w, who, o.Location)
}

// lookAt describes one object to a player.
func (s *Server) lookAt(w *world.World, who, target ref.Ref) {
	o := w.Get(target)
	if o == nil {
		s.notify(w, who, "I don't see that here.")
		return
	}

	s.send(w, who, unparse(w, who, target))

	desc := s.mesgProp(w, who, target, propDesc)
	if desc == "" {
		desc = w.Tune.String("description_default")
	}
	s.send(w, who, desc)

	w.Used(target)

	// Exits are deliberately not listed. Upstream's look_room
	// gives the name, the description and the contents and stops
	// there; a world that wants an "obvious exits" line supplies
	// it from its own programs, as the starter world does.
	if o.Type() == ref.TypeRoom {
		s.listContents(w, who, target)
		return
	}
	// A container's contents are listed too, so a player can see
	// what is inside a thing they are examining.
	if o.Type() == ref.TypeThing || o.Type() == ref.TypePlayer {
		s.listContents(w, who, target)
	}
}

// listContents lists what is in a container, skipping the viewer and
// anything dark they may not see.
func (s *Server) listContents(w *world.World, who, container ref.Ref) {
	var names []string
	for _, r := range w.Contents(container) {
		if r == who {
			continue
		}
		o := w.Get(r)
		if o == nil {
			continue
		}
		if !s.canSee(w, who, r) {
			continue
		}
		names = append(names, unparse(w, who, r))
	}
	if len(names) == 0 {
		return
	}
	s.notify(w, who, "Contents:")
	for _, n := range names {
		s.send(w, who, n)
	}
}

// canSee reports whether a player may see an object in a listing.
func (s *Server) canSee(w *world.World, who, target ref.Ref) bool {
	o := w.Get(target)
	if o == nil {
		return false
	}
	if o.Flags&ref.Dark == 0 {
		return true
	}
	// A dark object is still visible to anyone who controls it.
	return s.controls(w, who, target)
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
