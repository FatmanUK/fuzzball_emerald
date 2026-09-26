package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// @action, @attach and @clone, from create.c:713, :782 and :498.
//
// @action is the one that mattered most: it was *registered* as an
// alias for @open, which is worse than being missing, because
// upstream attaches the exit to a **named object** rather than to the
// room. A world using it got exits in the wrong place and no
// complaint.

func init() {
	register("@action", (*Server).cmdAction)
	register("@attach", (*Server).cmdAttach)
	register("@clone", (*Server).cmdClone)
}

// parseSource is create.c:653's parse_source: find something an
// action may be attached to.
//
// Three refusals, each its own sentence: not controlling the
// attachment point, and the two types that cannot hold one.
func (s *Server) parseSource(c *ctx, name string) (ref.Ref, bool) {
	r := match.New(c.w, c.who, name).Neighbor().Me().Here().
		Possession().Registered().Absolute().Result()
	if !noisyMatch(c, name, r) {
		return ref.Nothing, false
	}
	if !s.controls(c.w, c.who, r) {
		c.tell("Permission denied. (you don't control the " +
			"attachment point)")
		return ref.Nothing, false
	}
	switch c.w.Get(r).Type() {
	case ref.TypeExit:
		c.tell("You can't attach an action to an action.")
		return ref.Nothing, false
	case ref.TypeProgram:
		c.tell("You can't attach an action to a program.")
		return ref.Nothing, false
	}
	return r, true
}

// cmdAction is do_action: make an exit and hang it on a named object
// rather than on the room.
//
// It does not link, which is what separates it from @open — an
// action does nothing until @link points it somewhere, and
// autolink_actions decides whether it is pointed at NIL to begin
// with.
func (s *Server) cmdAction(c *ctx) {
	name, rest, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	sourceName, rname, _ := strings.Cut(rest, "=")
	sourceName = strings.TrimSpace(sourceName)
	rname = strings.TrimSpace(rname)

	if sourceName == "" {
		c.tell("You must specify a source object.")
		return
	}
	source, ok := s.parseSource(c, sourceName)
	if !ok {
		return
	}
	if !s.payFor(c.w, c.who, int(c.w.Tune.Int("exit_cost"))) {
		c.tell("Sorry, you don't have enough %s to make an "+
			"action.", c.w.Tune.String("pennies"))
		return
	}

	o := c.w.Create(name, ref.TypeExit, c.who)
	if err := c.w.MoveTo(o.Ref, source); err != nil {
		c.tell("The action could not be attached.")
		return
	}
	c.tell("Action %s created and attached.",
		unparse(c.w, c.who, o.Ref))
	if c.w.Tune.Bool("autolink_actions") {
		// Upstream says this and links nothing: an action is
		// created with no destinations either way, and the
		// parameter only decides whether it claims to be
		// pointed at NIL.
		c.tell("Linked to NIL.")
	}
	s.registerBuilt(c, rname, o.Ref)
}

// cmdAttach is do_attach: move an existing action onto something
// else.
//
// The priority reset is announced separately and with a different
// word from @unlink's — "zero" rather than "0" — which looks like
// a slip and is upstream's.
func (s *Server) cmdAttach(c *ctx) {
	name, sourceName, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	sourceName = strings.TrimSpace(sourceName)
	if name == "" || sourceName == "" {
		c.tell("You must specify an action name and a " +
			"source object.")
		return
	}

	action := match.New(c.w, c.who, name).Exits().Registered().
		Absolute().Result()
	if !noisyMatch(c, name, action) {
		return
	}
	o := c.w.Get(action)
	if o.Type() != ref.TypeExit {
		c.tell("That's not an action!")
		return
	}
	if !s.controls(c.w, c.who, action) {
		c.tell("Permission denied. (you don't control the " +
			"action you're trying to reattach)")
		return
	}
	source, ok := s.parseSource(c, sourceName)
	if !ok {
		return
	}

	if err := c.w.MoveTo(action, source); err != nil {
		c.tell("The action could not be attached.")
		return
	}
	c.tell("Action re-attached.")
	if o.Flags.RawMLevel() != 0 {
		o.Flags = o.Flags.SetMLevel(0)
		c.w.Modified(action)
		c.tell("Action priority Level reset to zero.")
	}
}

// cmdClone is do_clone: copy a thing, its flags and its properties.
//
// The cost is the *original's* value, floored at object_cost, so
// cloning something valuable costs what it is worth. The copy's value
// is the original's, capped at max_object_endowment.
//
// A wizard's clone copies hidden properties and anybody else's does
// not, which is clone_thing's copy_hidden_props argument.
func (s *Server) cmdClone(c *ctx) {
	name, rname, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	rname = strings.TrimSpace(rname)
	if name == "" {
		c.tell("Clone what?")
		return
	}

	thing := match.New(c.w, c.who, name).Possession().Neighbor().
		Registered().Absolute().Result()
	if !noisyMatch(c, name, thing) {
		return
	}
	o := c.w.Get(thing)
	if o.Type() != ref.TypeThing {
		c.tell("That is not a cloneable object.")
		return
	}
	if !s.controls(c.w, c.who, thing) {
		c.tell("Permission denied. (you can't clone this)")
		return
	}

	cost := int(endowmentCost(valueOf(c.w, thing)))
	if min := int(c.w.Tune.Int("object_cost")); cost < min {
		cost = min
	}
	if !s.payFor(c.w, c.who, cost) {
		c.tell("Sorry, you don't have enough %s.",
			c.w.Tune.String("pennies"))
		return
	}

	clone := c.w.Create(o.Name, ref.TypeThing, c.who)
	clone.Flags = o.Flags
	clone.Home = c.w.Get(c.who).Location
	wizard := isWizard(c.w, ownerOf(c.w, c.who))
	for _, e := range o.Props.All() {
		if !wizard && isHiddenProp(e.Path) {
			continue
		}
		clone.Props.Set(e.Path, e.Value)
	}
	// The copy's value is the original's, bounded the same way a
	// created object's endowment is.
	value := valueOf(c.w, thing)
	if max := c.w.Tune.Int("max_object_endowment"); value > max {
		value = max
	}
	if value < 0 {
		value = 0
	}
	clone.Props.Set(propValue,
		props.Value{Type: props.Int, Num: value})

	if err := c.w.MoveTo(clone.Ref, c.who); err != nil {
		c.tell("Cloned, but it could not be given to you.")
		return
	}
	c.w.Modified(clone.Ref)
	c.tell("Object %s cloned as %s.", unparse(c.w, c.who, thing),
		unparse(c.w, c.who, clone.Ref))
	s.registerBuilt(c, rname, clone.Ref)
}

// endowmentCost is OBJECT_GETCOST, the inverse of the endowment
// arithmetic: what an object of a given value cost to make.
func endowmentCost(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value*5 + 5
}

func init() {
	register("@relink", (*Server).cmdRelink)
	register("@bless", (*Server).cmdBless)
	register("@unbless", (*Server).cmdUnbless)
}

// cmdRelink is do_relink (set.c:253): check the new target first, and
// only then break the old link.
//
// That order is the whole point of the command — @unlink followed
// by @link leaves an exit pointing nowhere when the second half fails
// — and it is why the checks here duplicate @link's rather than
// calling it. Each refusal is its own sentence, and none of them
// matches @link's own wording for the same condition.
func (s *Server) cmdRelink(c *ctx) {
	name, destName, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	destName = strings.TrimSpace(destName)

	thing := match.New(c.w, c.who, name).Everything().Result()
	if !noisyMatch(c, name, thing) {
		return
	}
	o := c.w.Get(thing)
	if o.Type() != ref.TypeExit &&
		strings.Contains(destName, ";") {
		c.tell("Only actions and exits can be linked to " +
			"multiple destinations.")
		return
	}

	switch o.Type() {
	case ref.TypeExit:
		if len(o.Dest) != 0 {
			if !s.controls(c.w, c.who, thing) {
				c.tell("Permission denied. (The " +
					"exit is linked, and you " +
					"don't control it)")
				return
			}
		} else if !s.canSeizeExit(c) {
			return
		}
		// The destinations are all checked before anything is
		// broken, which is link_exit_dry's whole job.
		if _, ok := s.resolveLinkTarget(c, destName); !ok {
			c.tell("Invalid target.")
			return
		}
	case ref.TypeThing, ref.TypePlayer:
		m := match.New(c.w, c.who, destName).Neighbor().
			Absolute().Registered().Me().Here()
		if o.Type() == ref.TypeThing {
			m = m.Possession()
		}
		dest := m.Result()
		if !noisyMatch(c, destName, dest) {
			return
		}
		if !s.controls(c.w, c.who, thing) ||
			!s.canLinkTo(c.w, c.who, dest) {
			c.tell("Permission denied. (You can't link " +
				"to where you want to.")
			return
		}
		if parentLoopCheck(c.w, thing, dest) {
			c.tell("That would cause a parent paradox.")
			return
		}
	case ref.TypeRoom:
		dest := match.New(c.w, c.who, destName).Neighbor().
			Possession().Registered().Absolute().Home().
			Result()
		if !noisyMatch(c, destName, dest) {
			return
		}
		self := thing == dest
		if self || !s.controls(c.w, c.who, thing) ||
			!s.canLinkTo(c.w, c.who, dest) {
			c.tell("Permission denied. (You can't link " +
				"to the dropto like that)")
			return
		}
	case ref.TypeProgram:
		c.tell("You can't link programs to things!")
		return
	default:
		c.tell("Internal error: weird object type.")
		return
	}

	// _do_unlink is called *quietly* here, so the only thing said
	// between the checks and the link is this one line.
	s.unlinkQuietly(c, thing)
	c.tell("Attempting to relink...")
	s.cmdLink(&ctx{w: c.w, d: c.d, who: c.who, out: c.out,
		verb: "@link", arg: name + "=" + destName})
}

// canSeizeExit is do_relink's branch for an exit that points nowhere:
// anyone may take one over, for the price of both an exit and a link,
// and only a builder may do it at all.
func (s *Server) canSeizeExit(c *ctx) bool {
	cost := c.w.Tune.Int("link_cost") + c.w.Tune.Int("exit_cost")
	if !isWizard(c.w, ownerOf(c.w, c.who)) &&
		valueOf(c.w, ownerOf(c.w, c.who)) < cost {
		unit := c.w.Tune.String("pennies")
		if cost == 1 {
			unit = c.w.Tune.String("penny")
		}
		c.tell("It costs %d %s to link this exit.",
			cost, unit)
		return false
	}
	if !canBuild(c.w, ownerOf(c.w, c.who)) {
		c.tell("Only authorized builders may seize exits.")
		return false
	}
	c.tell("Claiming unlinked exits: %s", deprecatedFeature)
	return true
}

// deprecatedFeature is DEPRECATED_FEATURE, the notice upstream
// appends to things it intends to remove.
const deprecatedFeature = "This is a deprecated feature " +
	"and may be removed in a future version."

// unlinkQuietly is _do_unlink with its quiet flag set: the same
// clearing and the same refund, and none of the messages.
func (s *Server) unlinkQuietly(c *ctx, thing ref.Ref) {
	o := c.w.Get(thing)
	if o == nil {
		return
	}
	switch o.Type() {
	case ref.TypeExit:
		if len(o.Dest) != 0 {
			s.refund(c.w, o.Owner,
				int(c.w.Tune.Int("link_cost")))
		}
		o.Dest = nil
		o.Flags = o.Flags.SetMLevel(0)
	case ref.TypeRoom:
		o.Dropto = ref.Nothing
	case ref.TypeThing:
		o.Home = o.Owner
	case ref.TypePlayer:
		o.Home = c.w.Tune.Ref("player_start")
	}
	c.w.Modified(thing)
}

// cmdBless is do_bless (wiz.c:453) and cmdUnbless its twin: set or
// clear the blessed flag on properties matching a pattern.
//
// A blessed property's MPI runs with the permissions of whoever
// blessed it rather than of whoever triggers it, so this is the one
// command that hands out privilege by pattern. Every property touched
// is named, and the count is reported with its plural agreed.
func (s *Server) cmdBless(c *ctx)   { s.blessProps(c, true) }
func (s *Server) cmdUnbless(c *ctx) { s.blessProps(c, false) }

func (s *Server) blessProps(c *ctx, bless bool) {
	name, pattern, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	pattern = strings.TrimSpace(pattern)

	victim := match.New(c.w, c.who, name).Everything().Result()
	if !noisyMatch(c, name, victim) {
		return
	}
	if c.w.Tune.Bool("strict_god_priv") && c.who != ref.God &&
		ownerOf(c.w, victim) == ref.God {
		c.tell("Only God may touch God's stuff.")
		return
	}
	// Only @bless has this guard. @unbless goes straight through
	// with an empty pattern, matches nothing, and reports zero
	// — which reads like an oversight and is upstream's.
	if bless && pattern == "" {
		c.tell("Usage is @bless object=propname.")
		return
	}

	o := c.w.Get(victim)
	n := 0
	for _, e := range o.Props.All() {
		if isSystemProp(e.Path) {
			continue
		}
		if !blessMatches(pattern, e.Path) {
			continue
		}
		v := e.Value
		v.Blessed = bless
		c.w.SetProp(victim, e.Path, v)
		n++
		if bless {
			c.send("Blessed /" + e.Path)
		} else {
			c.send("Unblessed /" + e.Path)
		}
	}
	word := "properties"
	if n == 1 {
		word = "property"
	}
	done := "blessed"
	if !bless {
		done = "unblessed"
	}
	c.tell("%d %s %s.", n, word, done)
}

// blessMatches is blessprops_wildcard's pattern rule: each path
// segment is matched with smatch against the corresponding segment of
// the pattern, and "**" matches everything below.
//
// A pattern ending in '/' gains a '*', so "_stuff/" means everything
// directly under it.
//
// One thing upstream does is not reproduced: it also blesses
// **directories**, which first_prop walks and Emerald's props.Tree
// does not report because a directory carries no value. The effect is
// confined to the count and to the marker examine prints, since
// Prop_Blessed reads a path's own flags and a blessed directory does
// not bless its children — but making it agree means a flag that
// can live on a valueless node, which touches props, examine and the
// store together. Recorded in docs/upstream-coverage.md.
func blessMatches(pattern, path string) bool {
	if strings.HasSuffix(pattern, "/") {
		pattern += "*"
	}
	pattern = strings.TrimLeft(pattern, "/")
	pats := strings.Split(pattern, "/")
	segs := strings.Split(path, "/")

	for i, p := range pats {
		if p == "**" {
			// Everything at or below this point.
			return true
		}
		if i >= len(segs) {
			return false
		}
		if !ascii.SMatch(segs[i], p) {
			return false
		}
	}
	return len(pats) == len(segs)
}
