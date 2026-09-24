package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

func init() { register("@chown", (*Server).cmdChown) }

// cmdChown is do_chown: change who owns an object.
//
// It was found missing by the upstream-coverage audit, and it had to
// be added rather than only recorded, because its absence was not
// silent in the harmless way a missing command usually is: "@chown"
// is a prefix of "@chown_lock", so lookupAtCommand resolved it to
// that and a wizard transferring ownership quietly set a lock
// instead.
func (s *Server) cmdChown(c *ctx) {
	name, ownerName, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	ownerName = strings.TrimSpace(ownerName)

	if name == "" {
		c.tell("You must specify what you want to take " +
			"ownership of.")
		return
	}
	thing := match.New(c.w, c.who, name).Everything().Player().Result()
	if !noisyMatch(c, name, thing) {
		return
	}

	me := ownerOf(c.w, c.who)
	owner := me
	if ownerName != "" && !ascii.EqualFold(ownerName, "me") {
		r, ok := c.w.PlayerNamed(ownerName)
		if !ok {
			c.tell("I couldn't find that player.")
			return
		}
		owner = r
	}

	wizard := isWizard(c.w, me)
	if !wizard && owner != me {
		c.tell("Only wizards can transfer ownership to others.")
		return
	}
	if !s.mayTakePossession(c, thing, wizard) {
		return
	}
	if !s.chargeForSeizedExit(c, thing, owner) {
		return
	}

	o := c.w.Get(thing)
	switch o.Type() {
	case ref.TypeRoom:
		// Without wizard powers you may only claim the room
		// you are standing in, which is what stops somebody
		// claiming a world from a distance.
		if !wizard && c.w.Get(c.who).Location != thing {
			c.tell("You can only chown \"here\".")
			return
		}
	case ref.TypeThing:
		if !wizard && o.Location != c.who {
			c.tell("You aren't carrying that.")
			return
		}
	case ref.TypePlayer:
		c.tell("Players always own themselves.")
		return
	case ref.TypeGarbage:
		c.tell("No one wants to own garbage.")
		return
	}

	o.Owner = ownerOf(c.w, owner)
	c.w.Modified(thing)

	if owner == c.who {
		c.tell("Owner changed to you.")
		return
	}
	c.tell("Owner changed to %s.", unparse(c.w, c.who, owner))
}

// mayTakePossession is upstream's permission check, which is written
// as one condition with three ways out and reads more clearly split
// up.
//
// A wizard may take anything. Anybody may take an exit they control
// the link of, or one that points nowhere. Otherwise the object has
// to be CHOWN_OK, must not be a program, and its chown lock has to
// pass.
func (s *Server) mayTakePossession(c *ctx, thing ref.Ref,
	wizard bool) bool {

	if wizard {
		return true
	}
	o := c.w.Get(thing)
	if o.Type() == ref.TypeExit &&
		(len(o.Dest) == 0 || s.canLink(c.w, c.who, thing)) {
		return true
	}
	if o.Flags&ref.ChownOK == 0 || o.Type() == ref.TypeProgram ||
		!s.lockPasses(c.w, c.d.ID, 1, c.who, thing,
			propChownLock, true) {
		c.tell("You can't take possession of that.")
		return false
	}
	return true
}

// chargeForSeizedExit charges exit_cost for taking somebody else's
// exit, and pays the old owner what was charged.
//
// Only that one case costs anything: upstream charges when a builder
// claims an exit for themselves, and nothing else about @chown has a
// price.
func (s *Server) chargeForSeizedExit(c *ctx,
	thing, owner ref.Ref) bool {

	o := c.w.Get(thing)
	if owner != c.who || o.Type() != ref.TypeExit ||
		ownerOf(c.w, thing) == ownerOf(c.w, c.who) {
		return true
	}
	if !c.w.Get(c.who).Flags.CanBuild() {
		c.tell("Only authorized builders may seize exits.")
		return false
	}
	cost := int(c.w.Tune.Int("exit_cost"))
	if !s.payFor(c.w, c.who, cost) {
		unit := c.w.Tune.String("pennies")
		if cost == 1 {
			unit = c.w.Tune.String("penny")
		}
		c.tell("It costs %d %s to seize this exit.", cost, unit)
		return false
	}
	// The old owner is paid what the new one was charged, so
	// seizing an exit moves value rather than destroying it.
	if prev := ownerOf(c.w, thing); c.w.Get(prev) != nil {
		c.w.SetProp(prev, propValue, props.Value{
			Type: props.Int,
			Num:  valueOf(c.w, prev) + int64(cost),
		})
	}
	return true
}
