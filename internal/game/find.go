package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// The four search commands — @find, @owned, @contents and
// @entrances — are one mechanism with four sources. Each parses the
// same flag expression, filters with checkflags, prints each survivor
// through display_objinfo and ends with the same two lines.
//
// The expression language was already ported, as internal/muf's
// FlagCheck: two primitives needed it first, and nothing about it is
// MUF's. What was genuinely missing is display_objinfo (look.c:1476),
// whose six modes parseFlagCheck deliberately dropped.

func init() {
	register("@owned", (*Server).cmdOwned)
	register("@contents", (*Server).cmdContents)
	register("@entrances", (*Server).cmdEntrances)
}

// objinfo is display_objinfo: one line per object, in whichever of
// the modes the "=word" asked for.
//
// The column is 38 wide and left-justified, then two spaces, which is
// upstream's "%-38.512s %.512s" — so a long name pushes the second
// column right rather than being cut.
func (s *Server) objinfo(c *ctx, obj ref.Ref, mode muf.OutputMode) {
	left := unparse(c.w, c.who, obj)
	right := ""

	switch mode {
	case muf.OutCount:
		// Counted and not shown, which is what leaves only
		// the total.
		return
	case muf.OutOwners:
		right = unparse(c.w, c.who, ownerOf(c.w, obj))
	case muf.OutLocations:
		right = unparse(c.w, c.who, locationOf(c.w, obj))
	case muf.OutLinks:
		right = s.linkColumn(c, obj)
	default:
		// OutPlain, and OutSize, which cannot be reproduced
		// — see muf.OutSize.
		c.send(left)
		return
	}
	c.send(sprintf("%-38.512s  %.512s", left, right))
}

// linkColumn is display_objinfo's "links" mode, whose idea of a link
// is per type: a room's drop-to, an exit's single destination, and a
// player's or thing's home. An exit with no destination and one with
// several each get a word of their own instead of a name.
func (s *Server) linkColumn(c *ctx, obj ref.Ref) string {
	o := c.w.Get(obj)
	if o == nil {
		return "N/A"
	}
	switch o.Type() {
	case ref.TypeRoom:
		return unparse(c.w, c.who, o.Dropto)
	case ref.TypeExit:
		switch len(o.Dest) {
		case 0:
			return "*UNLINKED*"
		case 1:
			return unparse(c.w, c.who, o.Dest[0])
		default:
			return "*METALINKED*"
		}
	case ref.TypePlayer, ref.TypeThing:
		return unparse(c.w, c.who, o.Home)
	default:
		return "N/A"
	}
}

func locationOf(w *world.World, r ref.Ref) ref.Ref {
	if o := w.Get(r); o != nil {
		return o.Location
	}
	return ref.Nothing
}

// endOfList is what all four commands close with. The marker comes
// first and the count second, which is upstream's order.
func (s *Server) endOfList(c *ctx, total int) {
	c.tell("***End of List***")
	c.tell("%d objects found.", total)
}

// cmdFind is do_find (look.c:1568): every object the player owns
// whose name matches, subject to the flag expression.
//
// Three things it does that Emerald's own version did not. The
// pattern is wrapped in "*...*" and matched with smatch, so "@find
// wid" finds "widget" but the wildcards a player writes work too. It
// charges lookup_cost, which nothing in this server read before. And
// it has no result cap — the invented limit of 200 meant a large
// world silently answered with part of the truth.
func (s *Server) cmdFind(c *ctx) {
	name, _, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	check, mode := muf.ParseFlagCheck(flagArg(c.arg))

	if !s.payFor(c.w, c.who, int(c.w.Tune.Int("lookup_cost"))) {
		c.tell("You don't have enough %s.",
			c.w.Tune.String("pennies"))
		return
	}

	host := &mufHost{s: s, w: c.w, caller: c.who}
	pattern := "*" + name + "*"
	wizard := isWizard(c.w, ownerOf(c.w, c.who))
	total := 0
	c.w.Each(func(o *world.Object) bool {
		if o.Type() == ref.TypeGarbage {
			return true
		}
		if !wizard && o.Owner != ownerOf(c.w, c.who) {
			return true
		}
		if !check.Matches(host, o.Ref) {
			return true
		}
		if name != "" && !ascii.SMatch(o.Name, pattern) {
			return true
		}
		s.objinfo(c, o.Ref, mode)
		total++
		return true
	})
	s.endOfList(c, total)
}

// cmdOwned is do_owned (look.c:1613): everything one player owns.
//
// A wizard may name somebody else; anyone else gets their own things
// whatever they typed, which is upstream's own reading of the
// argument rather than a refusal.
func (s *Server) cmdOwned(c *ctx) {
	name, _, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	check, mode := muf.ParseFlagCheck(flagArg(c.arg))

	if !s.payFor(c.w, c.who, int(c.w.Tune.Int("lookup_cost"))) {
		c.tell("You don't have enough %s.",
			c.w.Tune.String("pennies"))
		return
	}

	victim := c.who
	if name != "" && isWizard(c.w, ownerOf(c.w, c.who)) {
		r, ok := c.w.PlayerNamed(name)
		if !ok {
			c.tell("I couldn't find that player.")
			return
		}
		victim = r
	}

	host := &mufHost{s: s, w: c.w, caller: c.who}
	owner := ownerOf(c.w, victim)
	total := 0
	c.w.Each(func(o *world.Object) bool {
		if o.Owner != owner || !check.Matches(host, o.Ref) {
			return true
		}
		s.objinfo(c, o.Ref, mode)
		total++
		return true
	})
	s.endOfList(c, total)
}

// cmdContents is do_contents (look.c:1798): what is inside an object,
// its exits included.
//
// The contents chain comes first and the exit chain second, which is
// upstream's order and is observable. A type that has no exit chain
// — an exit, a program — contributes nothing from the second pass
// rather than being refused.
func (s *Server) cmdContents(c *ctx) {
	thing, ok := s.searchTarget(c,
		"Permission denied. (You can't get the contents "+
			"of something you don't control)")
	if !ok {
		return
	}
	check, mode := muf.ParseFlagCheck(flagArg(c.arg))

	host := &mufHost{s: s, w: c.w, caller: c.who}
	total := 0
	for _, r := range c.w.Contents(thing) {
		if check.Matches(host, r) {
			s.objinfo(c, r, mode)
			total++
		}
	}
	switch c.w.Get(thing).Type() {
	case ref.TypeRoom, ref.TypeThing, ref.TypePlayer:
		for _, r := range c.w.Exits(thing) {
			if check.Matches(host, r) {
				s.objinfo(c, r, mode)
				total++
			}
		}
	}
	s.endOfList(c, total)
}

// cmdEntrances is do_entrances (look.c:1707): everything in the
// database that points *at* an object.
//
// What counts as pointing at it is per type, the same four cases
// display_objinfo's links column renders: an exit's destinations, a
// player's or thing's home, a room's drop-to. An exit with the same
// destination twice is reported twice, which is upstream's own loop
// over ndest.
func (s *Server) cmdEntrances(c *ctx) {
	thing, ok := s.searchTarget(c,
		"Permission denied. (You can't list entrances of "+
			"objects you don't control)")
	if !ok {
		return
	}
	check, mode := muf.ParseFlagCheck(flagArg(c.arg))

	host := &mufHost{s: s, w: c.w, caller: c.who}
	total := 0
	c.w.Each(func(o *world.Object) bool {
		if !check.Matches(host, o.Ref) {
			return true
		}
		hits := 0
		switch o.Type() {
		case ref.TypeExit:
			for _, d := range o.Dest {
				if d == thing {
					hits++
				}
			}
		case ref.TypePlayer, ref.TypeThing:
			if o.Home == thing {
				hits = 1
			}
		case ref.TypeRoom:
			if o.Dropto == thing {
				hits = 1
			}
		}
		for i := 0; i < hits; i++ {
			s.objinfo(c, o.Ref, mode)
			total++
		}
		return true
	})
	s.endOfList(c, total)
}

// searchTarget is the opening @contents and @entrances share: an
// object name or "here", matched noisily, then a control test with
// the command's own wording.
//
// The control test is made on the player's *owner*, which is
// upstream's "controls(OWNER(player), thing)" — so a puppet listing
// contents is asking on behalf of whoever owns it.
func (s *Server) searchTarget(c *ctx, denied string) (ref.Ref, bool) {
	name, _, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)

	thing := c.w.Get(c.who).Location
	if name != "" && !ascEqual(name, "here") {
		r := match.New(c.w, c.who, name).Everything().Result()
		if !noisyMatch(c, name, r) {
			return ref.Nothing, false
		}
		thing = r
	}
	if !s.controls(c.w, ownerOf(c.w, c.who), thing) {
		c.tell("%s", denied)
		return ref.Nothing, false
	}
	return thing, true
}

// flagArg hands ParseFlagCheck what upstream hands init_checkflags:
// the whole argument, whose first half it discards itself. It is a
// named helper because passing the argument rather than the flags is
// the sort of thing that looks like a mistake.
func flagArg(arg string) string {
	_, flags, ok := strings.Cut(arg, "=")
	if !ok {
		return ""
	}
	return flags
}
