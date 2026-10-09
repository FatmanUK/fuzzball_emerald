package game

import (
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/tune"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// requireBuilder reports whether the player may use construction
// commands, telling them if not.
func (s *Server) requireBuilder(c *ctx) bool {
	if c.w.Get(c.who).Flags.CanBuild() {
		return true
	}
	c.tell("Only builders are allowed to do that.")
	return false
}

// requireWizard reports whether the player has wizard powers, telling
// them if not.
func (s *Server) requireWizard(c *ctx) bool {
	if c.w.Get(c.who).Flags.IsWizard() {
		return true
	}
	c.tell("Permission denied.")
	// Audited rather than merely refused: one of these is a typo,
	// and a run of them from one player is someone trying the
	// doors.
	s.securityLog().Warn("refused a wizard command",
		"player", c.who.String(), "name", nameOf(c.w, c.who),
		"command", c.verb)
	return false
}

// matchControlled finds an object the player may modify: upstream's
// match_controlled (match.c:1034), message for message.
//
// A failed match is reported by noisyMatch, which is
// noisy_match_result: "I don't understand 'X'." Every upstream
// command that lands here reaches it through that function, directly
// or through match_controlled, so the wording is shared and programs
// match on it. An earlier version said "I don't see that here.",
// which belongs to the commands that match quietly and complain in
// their own words.
//
// Only the commands upstream really routes through match_controlled
// may use this: @name, @describe, @set, @unlock and the @lock family.
// @link, @unlink, @recycle and @teleport each match for themselves
// and apply a rule of their own, which is why resolveControlled —
// the shared stand-in they used to share — is gone rather than
// reworded.
func (s *Server) matchControlled(c *ctx,
	name string) (ref.Ref, bool) {

	// match_everything already adds match_player when the
	// searcher or its owner is a wizard (match.c:712), which is
	// the case @set needs. Adding it unconditionally here let a
	// *mortal* name any player in the game: the control test then
	// refused them, so the refusal was "Permission denied. (You
	// don't control what was matched)" where upstream, having
	// matched nothing, says "I don't understand 'X'." Programs
	// match on both.
	r := match.New(c.w, c.who, name).Everything().Result()
	if !noisyMatch(c, name, r) {
		return ref.Nothing, false
	}
	if !s.controls(c.w, c.who, r) {
		c.tell("Permission denied. " +
			"(You don't control what was matched)")
		return ref.Nothing, false
	}
	return r, true
}

// cmdCreate makes a thing and puts it in the player's inventory.
func (s *Server) cmdCreate(c *ctx) {
	if !s.requireBuilder(c) {
		return
	}
	name, rest, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	if name == "" {
		c.tell("Usage: @create <name> [=<cost>[=<regname>]]")
		return
	}
	costArg, rname, _ := strings.Cut(rest, "=")
	rname = strings.TrimSpace(rname)

	// A thing costs money to make and is worth a fraction of what
	// was paid, which is what gives objects a value at all.
	// Paying more than the minimum endows the object with more.
	cost := leadingInt(strings.TrimSpace(costArg))
	if cost < 0 {
		c.tell("You can't create an object for less than nothing!")
		return
	}
	if min := int(c.w.Tune.Int("object_cost")); cost < min {
		cost = min
	}
	if !s.payFor(c.w, c.who, cost) {
		c.tell("Sorry, you don't have enough %s.", c.w.Tune.String("pennies"))
		return
	}

	if !s.checkName(c, name, ref.TypeThing) {
		return
	}
	o := c.w.Create(name, ref.TypeThing, c.who)
	o.Home = c.w.Get(c.who).Location
	o.Props.Set(propValue, props.Value{Type: props.Int, Num: int64(endowment(c.w, cost))})
	if err := c.w.MoveTo(o.Ref, c.who); err != nil {
		c.tell("Created, but it could not be given to you.")
		return
	}
	c.tell("Object %s created.", unparse(c.w, c.who, o.Ref))
	s.registerBuilt(c, rname, o.Ref)
}

// cmdDig makes a room.
func (s *Server) cmdDig(c *ctx) {
	if !s.requireBuilder(c) {
		return
	}
	name, rest, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	if name == "" {
		c.tell("Usage: @dig <name> [=<parent>[=<regname>]]")
		return
	}
	parentName, rname, _ := strings.Cut(rest, "=")
	rname = strings.TrimSpace(rname)

	if !s.payFor(c.w, c.who, int(c.w.Tune.Int("room_cost"))) {
		c.tell("Sorry, you don't have enough %s to dig a room.",
			c.w.Tune.String("pennies"))
		return
	}

	// The default parent is the nearest ABODE room *above* the
	// player's own room, and default_room_parent only when there
	// is none — so digging inside somebody's realm parents the
	// new room into that realm rather than at the top of the
	// world.
	parent := defaultRoomParent(c.w, c.who)

	if !s.checkName(c, name, ref.TypeRoom) {
		return
	}
	o := c.w.Create(name, ref.TypeRoom, c.who)
	o.Dropto = ref.Nothing
	if err := c.w.MoveTo(o.Ref, parent); err != nil {
		c.tell("Room created, but it could not be parented.")
		return
	}
	c.tell("Room %s created.", unparse(c.w, c.who, o.Ref))

	// A room that could not be parented where it was asked to go
	// still exists, at the default parent, and is reported that
	// way rather than failing the whole command.
	if p := strings.TrimSpace(parentName); p != "" {
		c.tell("Trying to set parent...")
		r := match.New(c.w, c.who, p).
			PreferType(ref.TypeRoom).
			Absolute().Registered().Here().Result()
		switch {
		case !noisyMatch(c, p, r):
			// The matcher has already said what went
			// wrong; this says what happened as a result.
			c.tell("Parent set to default.")
		case !s.canLinkTo(c.w, c.d.ID, c.who, ref.TypeRoom,
			r) || r == o.Ref:
			c.tell("Permission denied.  Parent set to default.")
		default:
			if err := c.w.MoveTo(o.Ref, r); err != nil {
				c.tell("Parent set to default.")
				break
			}
			c.tell("Parent set to %s.", unparse(c.w, c.who, r))
		}
	}
	s.registerBuilt(c, rname, o.Ref)
}

// cmdOpen makes an exit in the current room.
func (s *Server) cmdOpen(c *ctx) {
	if !s.requireBuilder(c) {
		return
	}
	name, rest, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	if name == "" {
		c.tell("Usage: @open <name>[;<alias>...] " +
			"[=<destination>[=<regname>]]")
		return
	}
	destName, rname, _ := strings.Cut(rest, "=")
	destName = strings.TrimSpace(destName)
	rname = strings.TrimSpace(rname)
	hasDest := destName != ""

	here := c.w.Get(c.who).Location
	if !c.w.Valid(here) {
		c.tell("You are nowhere; there is nothing to attach an exit to.")
		return
	}
	if !s.controls(c.w, c.who, here) {
		c.tell("Permission denied. (you don't control the location)")
		return
	}
	if !s.payFor(c.w, c.who, int(c.w.Tune.Int("exit_cost"))) {
		c.tell("Sorry, you don't have enough %s to open an exit.",
			c.w.Tune.String("pennies"))
		return
	}

	if !s.checkName(c, name, ref.TypeExit) {
		return
	}
	o := c.w.Create(name, ref.TypeExit, c.who)
	if err := c.w.MoveTo(o.Ref, here); err != nil {
		c.tell("The exit could not be attached.")
		return
	}
	c.tell("Exit %s opened.", unparse(c.w, c.who, o.Ref))

	// Linking costs again, and is reported separately: an exit
	// that was opened but could not be linked still exists.
	if hasDest {
		c.tell("Trying to link...")
		if !s.payFor(c.w, c.who, int(c.w.Tune.Int("link_cost"))) {
			c.tell("You don't have enough %s to link.",
				c.w.Tune.String("pennies"))
			return
		}
		if dest, ok := s.resolveExitDest(c, o.Ref,
			destName); ok {
			o.Dest = []ref.Ref{dest}
			c.w.Modified(o.Ref)
			c.tell("%s", linkedTo(c, dest))
		}
	}
	s.registerBuilt(c, rname, o.Ref)
}

// defaultRoomParent is do_dig's own search for where a new room
// belongs: the nearest ABODE room *above* the digger's own room,
// falling back to default_room_parent and then to #0.
//
// Emerald used to go straight to default_room_parent, so digging
// inside somebody's realm put the new room at the top of the world
// instead of inside the realm.
func defaultRoomParent(w *world.World, who ref.Ref) ref.Ref {
	me := w.Get(who)
	if me == nil {
		return ref.GlobalEnvironment
	}
	// LOCATION(LOCATION(player)): the walk starts above the room
	// the digger is standing in, not at it.
	parent := ref.Nothing
	if room := w.Get(me.Location); room != nil {
		parent = room.Location
	}
	for i := 0; parent != ref.Nothing && i <= w.Len(); i++ {
		o := w.Get(parent)
		if o == nil {
			break
		}
		if o.Flags&ref.Abode != 0 {
			return parent
		}
		parent = o.Location
	}
	if fallback := w.Tune.Ref("default_room_parent"); w.Valid(
		fallback) {
		return fallback
	}
	return ref.GlobalEnvironment
}

// exitLoopCheck is `exit_loop_check` (`predicates.c:289`): whether
// linking source to dest would make a ring of exits. It is the whole
// recursive walk, not a self-link test — upstream catches A to A, A
// to B to A, and any depth beyond that.
//
// **`@link` does test for this**, which the notes in `CLAUDE.md`
// twice said it did not. Nothing here checked, and the matcher that
// made an exit nameable as a destination is what exposed it: before
// `resolveLinkTarget` ran `match_everything` an exit could not be
// named as a destination at all, so the loop was unbuildable through
// `@link` and the missing check could not be seen.
func exitLoopCheck(w *world.World, source, dest ref.Ref) bool {
	return exitLoopFrom(w, source, dest, 0)
}

// exitLoopFrom carries the depth, which upstream does not need: its
// recursion terminates because a ring already in the database is
// impossible if every link went through this check, and Emerald
// cannot assume that of a dump it has been handed.
func exitLoopFrom(w *world.World, source, dest ref.Ref,
	depth int) bool {

	if source == dest {
		return true
	}
	o := w.Get(dest)
	if o == nil || o.Type() != ref.TypeExit ||
		depth > maxMetalinkDepth {
		return false
	}
	for _, cur := range o.Dest {
		if !w.Valid(cur) {
			continue
		}
		if cur == source {
			return true
		}
		if w.Get(cur).Type() == ref.TypeExit &&
			exitLoopFrom(w, source, cur, depth+1) {
			return true
		}
	}
	return false
}

// resolveExitDest is `_link_exit`'s TYPE_EXIT case (`db.c:2117`):
// `parse_linkable_dest` and then, when the destination is itself an
// exit, the loop check. Every site that links an *exit* goes through
// it; a home or a dropto does not, because upstream applies the check
// only in that one branch of its switch.
func (s *Server) resolveExitDest(c *ctx, exit ref.Ref,
	name string) (ref.Ref, bool) {

	dest, ok := s.resolveLinkTarget(c, exit, name)
	if !ok {
		return ref.Nothing, false
	}
	if c.w.Valid(dest) &&
		c.w.Get(dest).Type() == ref.TypeExit &&
		exitLoopCheck(c.w, exit, dest) {
		c.tell("Destination %s would create a loop, "+
			"ignored.", unparse(c.w, c.who, dest))
		return ref.Nothing, false
	}
	return dest, true
}

// resolveLinkTarget is parse_linkable_dest (db.c:1971): what an exit
// should point at.
//
// Every refusal here names the object it is refusing, and the failed
// match goes through noisy_match_result like every other command's
// — "I don't understand 'X'." An earlier version said "I don't see
// that here." and refused without naming anything.
func (s *Server) resolveLinkTarget(c *ctx, target ref.Ref,
	name string) (ref.Ref, bool) {

	// There is no empty-name guard upstream. "Link it to what?"
	// was invented here, and it shadowed two different real
	// answers: for an exit, _link_exit's loop never runs on an
	// empty string, so nothing is matched at all and do_link says
	// "No destinations linked."; for everything else the empty
	// name reaches the matcher and noisy_match_result answers.
	//
	// The match list is `match_everything`, then home, then nil,
	// with NOTYPE so there is no preferred type. The chain here
	// was hand-built and wrong three ways: no `match_registered`,
	// so `@link w = $tavern` could not resolve a registration the
	// player had just made; no `match_all_exits`, so the metalink
	// `trigger` traverses could not be built at all, which is
	// what hid the missing loop check; and `match_player` taken
	// unconditionally where `match_everything` gates it on
	// wizardry. It also preferred a room, which was invented.
	r := match.New(c.w, c.who, name).
		Everything().Home().Nil().Result()
	if !noisyMatch(c, name, r) {
		return ref.Nothing, false
	}
	// Upstream does **not** short-circuit HOME and NIL. The
	// player refusal skips NIL explicitly, `can_link` still
	// applies, and `can_link_to` is what accepts the pair --
	// returning them early here made its own HOME and NIL cases
	// dead, which is how a mutation that deleted the NIL case
	// survived.
	special := r == ref.Home || r == ref.Nil
	// Linking to a player is a separate refusal from being unable
	// to link, and a separate @tune parameter — which nothing
	// in this server read before.
	if !special && c.w.Get(r).Type() == ref.TypePlayer &&
		!c.w.Tune.Bool("teleport_to_player") {
		c.tell("You can't link to players.  Destination "+
			"%s ignored.", unparse(c.w, c.who, r))
		return ref.Nothing, false
	}
	// can_link on the thing being linked *from*, which is its own
	// refusal and comes before the destination's. Only the exit
	// path reaches this function, and `linkExit` has already made
	// the same test, so the guard is unreachable today; it is
	// upstream's line and goes where upstream has it.
	if !s.canLink(c.w, c.who, target) {
		c.tell("You can't link that.")
		return ref.Nothing, false
	}
	// can_link_to on the destination, carrying the type of what
	// is being linked *from*.
	if !s.canLinkTo(c.w, c.d.ID, c.who,
		c.w.Get(target).Type(), r) {
		c.tell("You can't link to %s.",
			unparse(c.w, c.who, r))
		return ref.Nothing, false
	}
	return r, true
}

// exitDelimiter is EXIT_DELIMITER (game.h:57), which separates an
// exit's destinations from each other.
const exitDelimiter = ';'

// cmdLink points an exit at a destination, or sets a home.
//
// Its permission rule is **not** match_controlled's, and there is no
// control test up front at all. do_link (create.c:138) matches,
// switches on the type, and each branch applies its own rule — so
// this cannot go through resolveControlled, which refused before any
// of that could run.
//
// The exit branch is the surprising one and is ported in linkExit
// below: an **unlinked** exit is linkable by anybody. The other two
// test controls() themselves, and each names what it failed.
//
// Two things about @link are still divergent and are recorded in
// docs/upstream-coverage.md rather than fixed here, because neither
// is a permission refusal: resolveLinkTarget's matcher is not
// parse_linkable_dest's, and @link cannot build a multi-destination
// exit even though trigger() traverses one.
func (s *Server) cmdLink(c *ctx) {
	name, destName, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	destName = strings.TrimSpace(destName)

	// init_match(..., TYPE_EXIT, ...) then match_everything.
	// There is no usage guard upstream: @link with no "=" reaches
	// link_exit with an empty destination, which links nothing
	// and says so — after charging for it.
	target := match.New(c.w, c.who, name).
		PreferType(ref.TypeExit).Everything().Result()
	if !noisyMatch(c, name, target) {
		return
	}

	o := c.w.Get(target)
	if o.Type() != ref.TypeExit &&
		strings.ContainsRune(destName, exitDelimiter) {
		c.tell("Only actions and exits can be linked to " +
			"multiple destinations.")
		return
	}

	// What @link says it did depends on the type, because it is
	// three different operations wearing one name: an exit gets a
	// destination, a thing or a player gets a home, and a room
	// gets a drop-to. Only the first is "linked" in upstream's
	// own words.
	switch o.Type() {
	case ref.TypeExit:
		s.linkExit(c, target, destName)
	case ref.TypeThing, ref.TypePlayer:
		s.linkHome(c, target, destName)
	case ref.TypeRoom:
		s.linkDropto(c, target, destName)
	case ref.TypeProgram:
		c.tell("You can't link programs to things!")
	default:
		// Upstream logs a PANIC here and carries on.
		c.tell("Internal error: weird object type.")
	}
}

// linkExit is do_link's TYPE_EXIT branch (create.c:159), and the
// order of its four steps is observable.
//
// **The permission test runs only when the exit already points
// somewhere.** An exit with no destinations is linkable by anybody,
// which is the "seizing" path: a builder who controls nothing may
// claim somebody else's unlinked exit by paying for it. Emerald
// refused before that could happen, so an abandoned exit could only
// ever be relinked by its owner.
//
// A **NIL** destination is not "already linked". Upstream tests
// dest[0] != NIL inside the controls() branch, so an exit parked at
// NIL may be relinked by whoever controls it while one pointing at a
// real room may not.
//
// Then the costs, which differ by who owns the exit; then the
// ownership transfer, which happens **before** the destination is
// resolved — so a failed destination leaves the exit transferred
// and the money spent, less the one refund below.
func (s *Server) linkExit(c *ctx, target ref.Ref, destName string) {
	o := c.w.Get(target)
	if len(o.Dest) != 0 {
		if !s.controls(c.w, c.who, target) {
			c.tell("Permission denied. (you don't " +
				"control the exit to relink)")
			return
		}
		if o.Dest[0] != ref.Nil {
			c.tell("That exit is already linked.")
			return
		}
	}

	linkCost := int(c.w.Tune.Int("link_cost"))
	exitCost := int(c.w.Tune.Int("exit_cost"))
	if ownerOf(c.w, target) == ownerOf(c.w, c.who) {
		if !s.payFor(c.w, c.who, linkCost) {
			c.tell("It costs %d %s to link this exit.",
				linkCost, s.pennies(c, linkCost))
			return
		}
	} else {
		if !c.w.Get(c.who).Flags.CanBuild() {
			c.tell("Only authorized builders may seize " +
				"exits.")
			return
		}
		total := linkCost + exitCost
		if !s.payFor(c.w, c.who, total) {
			c.tell("It costs %d %s to link this exit.",
				total, s.pennies(c, total))
			return
		}
		// The old owner is paid for the loss, so seizing an
		// exit moves value rather than destroying it.
		s.refund(c.w, ownerOf(c.w, target), exitCost)
		c.tell("Claiming unlinked exits: This feature will " +
			"be removed in the next version of Fuzzball.")
	}

	// Validated and paid for, so the exit changes hands whatever
	// the destination turns out to be.
	o.Owner = ownerOf(c.w, c.who)
	c.w.Modified(target)

	// _link_exit iterates over the destination string, so an
	// empty one matches nothing without ever calling the matcher.
	noneLinked := func() {
		// The refund is link_cost only — a seized exit's
		// exit_cost is not returned — and upstream skips it
		// when the exit's *new* owner is a wizard, who paid
		// nothing anyway.
		c.tell("No destinations linked.")
		if !isWizard(c.w, o.Owner) {
			s.refund(c.w, c.who, linkCost)
		}
	}
	if destName == "" {
		noneLinked()
		return
	}
	dest, ok := s.resolveExitDest(c, target, destName)
	if !ok {
		noneLinked()
		return
	}
	o.Dest = []ref.Ref{dest}
	c.w.Modified(target)
	c.tell("%s", linkedTo(c, dest))
}

// linkHome is do_link's TYPE_THING and TYPE_PLAYER branch
// (create.c:219), which sets a home rather than a destination.
//
// Its refusal names both halves of what it tested, and the parent
// loop check is its own separate answer.
func (s *Server) linkHome(c *ctx, target ref.Ref, destName string) {
	// Its **own** matcher, not parse_linkable_dest's: this branch
	// never calls that function, and the list is different in
	// five ways -- it prefers a room, has no `match_all_exits`,
	// no `match_home` and no `match_nil`, and takes
	// `match_absolute` and `match_registered` without the wizard
	// gate `match_everything` puts on its player search. So a
	// thing's home cannot be set to HOME through @link, and a
	// player elsewhere cannot be named even by a wizard. Sharing
	// one matcher is what made all five wrong here.
	m := match.New(c.w, c.who, destName).
		PreferType(ref.TypeRoom).
		Neighbor().Absolute().Registered().Me().Here()
	if c.w.Get(target).Type() == ref.TypeThing {
		m = m.Possession()
	}
	dest := m.Result()
	if !noisyMatch(c, destName, dest) {
		return
	}
	if !s.controls(c.w, c.who, target) ||
		!s.canLinkTo(c.w, c.d.ID, c.who,
			c.w.Get(target).Type(), dest) {
		c.tell("Permission denied. (you don't control the " +
			"thing, or you can't link to dest)")
		return
	}
	if parentLoopCheck(c.w, target, dest) {
		c.tell("That would cause a parent paradox.")
		return
	}
	c.w.Get(target).Home = dest
	c.w.Modified(target)
	c.tell("Home set.")
}

// linkDropto is do_link's TYPE_ROOM branch (create.c:261): a room's
// drop-to, with a third wording again and a self-link refused as part
// of the same condition.
func (s *Server) linkDropto(c *ctx, target ref.Ref,
	destName string) {

	// A third list again, and not the home branch's: this one has
	// `match_home`, so a dropto may be HOME, and has *neither*
	// `match_me` nor `match_here`, so the room being linked
	// cannot be named -- which `thing == dest` would refuse
	// anyway.
	dest := match.New(c.w, c.who, destName).
		PreferType(ref.TypeRoom).
		Neighbor().Possession().Registered().Absolute().
		Home().Result()
	if !noisyMatch(c, destName, dest) {
		return
	}
	if !s.controls(c.w, c.who, target) ||
		!s.canLinkTo(c.w, c.d.ID, c.who, ref.TypeRoom,
			dest) || target == dest {
		c.tell("Permission denied. (you don't control the " +
			"room, or can't link to the dropto)")
		return
	}
	c.w.Get(target).Dropto = dest
	c.w.Modified(target)
	c.tell("Dropto set.")
}

// pennies is upstream's (cost == 1) ? tp_penny : tp_pennies, which
// every priced refusal spells out inline.
func (s *Server) pennies(c *ctx, cost int) string {
	if cost == 1 {
		return c.w.Tune.String("penny")
	}
	return c.w.Tune.String("pennies")
}

// linkedTo is what @link says it did to an *exit*. HOME is named
// rather than unparsed, because unparsing it gives "*HOME*" — the
// spelling a lock or a dump uses, not the one db.c:2143 prints.
func linkedTo(c *ctx, dest ref.Ref) string {
	if dest == ref.Home {
		return "Linked to HOME."
	}
	if dest == ref.Nil {
		return "Linked to NIL."
	}
	return sprintf("Linked to %s.", unparse(c.w, c.who, dest))
}

// cmdUnlink removes an exit's destination or a room's drop-to.
//
// Its permission rule is **not** match_controlled's and not
// resolveControlled's either. `_do_unlink` (set.c:148) asks
//
//	!controls(player, exit) && !controls_link(player, exit)
//
// so controlling the exit is only one of two ways in: the owner of
// what an exit *points at* may unlink it, and so may the owner of the
// room it hangs in. Emerald refused both, which made the
// destination's owner powerless over an exit somebody else had aimed
// at their room — the case controls_link exists for.
//
// The refusal is its own sentence too: "Permission denied. (You don't
// control the exit or its link)".
func (s *Server) cmdUnlink(c *ctx) {
	// init_match(..., TYPE_EXIT, ...) then match_everything, and
	// nothing else — the same shape resolveControlled had,
	// minus its permission test.
	target := match.New(c.w, c.who, c.arg).
		PreferType(ref.TypeExit).Everything().Result()
	if !noisyMatch(c, c.arg, target) {
		return
	}
	if !s.controls(c.w, c.who, target) &&
		!s.controlsLink(c.w, c.who, target) {
		c.tell("Permission denied. " +
			"(You don't control the exit or its link)")
		return
	}
	// Like @link, this is four operations wearing one name, and
	// each says what it did. An exit is unlinked, a room loses
	// its drop-to, a thing's home goes back to its owner and a
	// player's to player_start.
	//
	// The refund and the priority reset are upstream's too: an
	// exit that had a destination returns link_cost to whoever
	// owns it, and an exit with any mucker bits loses them, which
	// is announced separately because it changes how strongly the
	// exit binds.
	o := c.w.Get(target)
	switch o.Type() {
	case ref.TypeExit:
		if len(o.Dest) != 0 {
			s.refund(c.w, o.Owner,
				int(c.w.Tune.Int("link_cost")))
		}
		o.Dest = nil
		c.w.Modified(target)
		c.tell("Unlinked.")
		if o.Flags.RawMLevel() != 0 {
			o.Flags = o.Flags.SetMLevel(0)
			c.w.Modified(target)
			c.tell("Action priority Level reset to 0.")
		}
	case ref.TypeRoom:
		o.Dropto = ref.Nothing
		c.w.Modified(target)
		c.tell("Dropto removed.")
	case ref.TypeThing:
		o.Home = o.Owner
		c.w.Modified(target)
		c.tell("Thing's home reset to owner.")
	case ref.TypePlayer:
		o.Home = c.w.Tune.Ref("player_start")
		c.w.Modified(target)
		c.tell("Player's home reset to default player " +
			"start room.")
	default:
		c.tell("You can't unlink that!")
	}
}

// refund puts money back in somebody's pocket, which @unlink does
// when it takes a destination away.
func (s *Server) refund(w *world.World, owner ref.Ref, amount int) {
	w.SetProp(owner, propValue, props.Value{Type: props.Int,
		Num: valueOf(w, owner) + int64(amount)})
}

// cmdName renames an object.
func (s *Server) cmdName(c *ctx) {
	name, newName, ok := strings.Cut(c.arg, "=")
	if !ok {
		c.tell("Usage: @name <object>=<new name>")
		return
	}
	target, ok := s.matchControlled(c, strings.TrimSpace(name))
	if !ok {
		return
	}
	newName = strings.TrimSpace(newName)
	if newName == "" {
		c.tell("Give it what name?")
		return
	}

	// Renaming a player needs the same checks as creating one,
	// and the player's own password, which @name does not take.
	if c.w.Get(target).Type() == ref.TypePlayer {
		if err := validPlayerName(c.w, newName); err != nil {
			c.send(err.Error())
			return
		}
	}
	if err := c.w.Rename(target, newName); err != nil {
		c.send(err.Error())
		return
	}
	c.tell("Name set.")
}

// settableFlags lists the names @set accepts, in the order upstream's
// str_to_flag tests them.
//
// Matching is by prefix, so "X" reaches XFORCIBLE and "dark" and "d"
// are the same flag. The order is load-bearing for single letters:
// "n" is the second mucker bit because "nucker" is tested before
// nothing else claims the letter, and "t" is the wizard bit through
// "truewizard".
//
// Internal flags are deliberately absent, except INTERACTIVE, which
// upstream exposes: they describe live server state rather than
// anything an operator should write.
var settableFlags = []struct {
	names []string
	bit   ref.Flags
}{
	{[]string{"abode", "autostart", "abate"}, ref.Abode},
	{[]string{"builder", "bound"}, ref.Builder},
	{[]string{"chown_ok", "color"}, ref.ChownOK},
	{[]string{"dark", "debug"}, ref.Dark},
	{[]string{"guest"}, ref.Guest},
	{[]string{"haven", "hide", "harduid"}, ref.Haven},
	{[]string{"interactive"}, ref.Interactive},
	{[]string{"jump_ok"}, ref.JumpOK},
	{[]string{"kill_ok"}, ref.KillOK},
	{[]string{"link_ok"}, ref.LinkOK},
	{[]string{"mucker"}, ref.Mucker},
	{[]string{"nucker"}, ref.SMucker},
	{[]string{"overt"}, ref.Overt},
	{[]string{"quell"}, ref.Quell},
	{[]string{"sticky", "silent", "setuid"}, ref.Sticky},
	{[]string{"vehicle", "viewable"}, ref.Vehicle},
	{[]string{"wizard"}, ref.Wizard},
	{[]string{"truewizard"}, ref.Wizard},
	{[]string{"xforcible", "xpress"}, ref.XForcible},
	{[]string{"yield"}, ref.Yield},
	{[]string{"zombie"}, ref.Zombie},
}

// strToFlag resolves a flag name or any unambiguous prefix of one.
func strToFlag(name string) (ref.Flags, bool) {
	if name == "" {
		return 0, false
	}
	for _, f := range settableFlags {
		for _, n := range f.names {
			if ascii.HasPrefix(n, name) {
				return f.bit, true
			}
		}
	}
	return 0, false
}

// unableToSetFlag is `unable_to_set_flag` (`set.c:537`): whether this
// asker may change this flag on this object. It answers with a
// message and true when refused, and an empty message means the
// caller's own "Permission denied. (restricted flag)".
//
// What stood here was `wizardOnlyFlags`, a six-entry map, and the
// shape is what a map cannot express. The answer depends on the
// object's **type** as much as on the flag, on whether the flag is
// being set or cleared, on three `@tune` parameters that had no
// reader anywhere in this server, and on whether a `@force` is
// running. The map left YIELD, ABODE, ZOMBIE, VEHICLE and DARK
// unguarded altogether, and was too strict for three others.
//
// `mlev` is the asker's owner's effective mucker level, which only
// the BUILDER case reads. It takes a world and a player rather than a
// `ctx`, because MUF `SET` needs it too — `prim_set`
// (`p_db.c:1039`) calls `unable_to_set_flag(ProgUID, mlev, ...)` with
// exactly these two. Reimplementing it in `internal/muf` would be two
// ports of one rule, which is the mistake CLAUDE.md records for
// `can_link_to`/`can_teleport_to`.
func (s *Server) unableToSetFlag(w *world.World, player ref.Ref,
	mlev int, thing ref.Ref, flag ref.Flags,
	value bool) (string, bool) {

	owner := ownerOf(w, player)
	wiz := isWizard(w, owner)
	typ := w.Get(thing).Type()
	mucker := flag&(ref.Mucker|ref.SMucker) != 0

	// A @force may not touch the flags that would let it grant
	// itself more. XFORCIBLE is exempt **on an exit**, which is
	// exactly the type the switch below refuses to a mortal, so
	// the two guards are complementary rather than inconsistent.
	if s.forceDepth > 0 && (flag == ref.Wizard || mucker ||
		(flag == ref.XForcible && typ != ref.TypeExit)) {
		return "That flag cannot be forced.", true
	}

	// Clearing a mucker level and setting one are two rules, not
	// one, and each interpolates the level into its message. A
	// non-wizard may change only a program they own, and may not
	// set one above their own **raw** level, so the wizard bit
	// does not lend the level it would otherwise give.
	if !value && mucker {
		if !wiz && (owner != ownerOf(w, thing) ||
			typ != ref.TypeProgram) {
			return "Permission denied. " +
				"(You can't set that M0)", true
		}
		return "", false
	}
	if mucker {
		want := 0
		if flag&ref.Mucker != 0 {
			want += 2
		}
		if flag&ref.SMucker != 0 {
			want++
		}
		if !wiz && (owner != ownerOf(w, thing) ||
			typ != ref.TypeProgram ||
			w.Get(player).Flags.RawMLevel() < want) {
			return "Permission denied. (You can't " +
				"set that M" + itoa(want) + ")", true
		}
		return "", false
	}

	switch flag {
	case ref.Abode:
		// ABODE on a program is AUTOSTART, which runs code at
		// boot. It takes `TrueWizard` rather than `Wizard`,
		// so a quelled wizard may still set one.
		return "", !hasFlag(w, owner, ref.Wizard) &&
			typ == ref.TypeProgram

	case ref.Guest:
		return "", !wiz

	case ref.Yield, ref.Overt:
		// These two decide env-chain matching, so a wizard
		// only — and only on the two types the walk
		// consults. An exit or a program is refused even to
		// God, which makes this the one rule here a
		// transcript can see.
		if !wiz {
			return "", true
		}
		return "", typ != ref.TypeThing && typ != ref.TypeRoom

	case ref.Zombie:
		// On a player the flag is a **restriction** — "may
		// not use zombies" — so applying it takes a wizard.
		// On a thing it is the puppet itself, and is refused
		// to an asker who has been restricted that way.
		if typ == ref.TypePlayer {
			return "", !wiz
		}
		if typ == ref.TypeThing &&
			hasFlag(w, owner, ref.Zombie) {
			return "", !wiz
		}
		return "", false

	case ref.Vehicle:
		if typ == ref.TypePlayer {
			return "", !wiz
		}
		// A vehicle with somebody inside may not stop being
		// one: its passengers would be sitting in a thing
		// that nothing can leave.
		if !value && typ == ref.TypeThing {
			for _, o := range w.Contents(thing) {
				if w.Get(o).Type() ==
					ref.TypePlayer {
					return "That vehicle " +
						"still has players " +
						"in it!", true
				}
			}
		}
		if w.Tune.Bool("wiz_vehicles") {
			if typ == ref.TypeThing {
				return "", !wiz
			}
			return "", false
		}
		// Note this one reads the **asker's** own flag where
		// every other test here reads the owner's.
		if typ == ref.TypeThing &&
			hasFlag(w, player, ref.Vehicle) {
			return "", !wiz
		}
		return "", false

	case ref.Dark:
		// A room or a program may be darked by anybody — on
		// a program DARK is the debugger. A player may not,
		// and an exit or a thing only while the matching
		// `@tune` parameter allows it. Both parameters
		// default true and this is their only reader.
		if !wiz {
			if typ == ref.TypePlayer {
				return "", true
			}
			if !w.Tune.Bool("exit_darking") &&
				typ == ref.TypeExit {
				return "", true
			}
			if !w.Tune.Bool("thing_darking") &&
				typ == ref.TypeThing {
				return "", true
			}
		}
		return "", false

	case ref.Quell:
		// Only God may quell or unquell another wizard, which
		// is both narrower and wider than a wizard-only rule:
		// a mortal may set QUELL on their own things freely,
		// and a plain wizard may not touch a colleague's.
		return "", hasFlag(w, thing, ref.Wizard) &&
			thing != player && owner != ref.God &&
			typ == ref.TypePlayer

	case ref.Builder:
		// BUILDER on a program is BOUND, which the owner of a
		// mucker-2 program may set without being a wizard —
		// the one place `mlev` is read.
		if typ == ref.TypeProgram {
			return "", mlev < 2
		}
		return "", !wiz

	case ref.Wizard:
		if !wiz {
			return "", true
		}
		if !value && thing == player {
			return "You cannot make yourself " +
				"mortal.", true
		}
		// Under GOD_PRIV, which upstream defines by default,
		// only God may make or unmake a wizard.
		return "", typ == ref.TypePlayer && player != ref.God

	case ref.XForcible:
		// Restricted on an **exit** only. The map this
		// replaces made it wizard-only for every type, a
		// divergence the other way: upstream lets a mortal
		// make their own thing or program forcible.
		return "", !wiz && typ == ref.TypeExit
	}
	// No other flag is restricted.
	return "", false
}

// setProperty is do_set's property branch (set.c:763-842), which is
// most of a second command inside @set and had almost none of its
// rules.
//
// What it gained: the restricted-property guard that @propset has
// always had, ":clear", integer values, the trailing-'/' trim, and
// upstream's wording in place of three invented messages.
func (s *Server) setProperty(c *ctx, target ref.Ref,
	path, value string) {

	wizard := isWizard(c.w, ownerOf(c.w, c.who))

	// Upstream left-trims the name and then asks whether what is
	// left begins with the colon, so ":clear" is recognised
	// before anything else and " :clear" is too.
	if strings.TrimLeft(path, " \t") == "" {
		// Only "clear" is accepted after a bare colon, and
		// the refusal quotes the syntax rather than
		// describing it.
		if !ascEqual(strings.TrimSpace(value), "clear") {
			c.tell("Use '@set <obj>=:clear' to " +
				"clear all props on an object")
			return
		}
		n := s.clearProperties(c.w, target, wizard)
		_ = n
		// "All properties removed." for a wizard and "All
		// user-owned properties removed." for anybody else,
		// because the two clear different amounts.
		if wizard {
			c.tell("All properties removed.")
		} else {
			c.tell("All user-owned properties removed.")
		}
		return
	}

	// Two trims, and the *order* is the whole of it. Upstream
	// right-trims whitespace first and only then strips trailing
	// '/' (set.c:805-809) — so a name ending "b /" meets the
	// whitespace loop, which sees the '/' and stops at once, and
	// the two spaces SURVIVE. The property really is called
	// "_test/b ", and "_test/b" does not exist. Doing the two in
	// the other order gives "_test/b" and is a different
	// property; the golden case pins both.
	//
	// The '/' strip is unobservable on its own, because
	// props.split drops empty segments and so resolves "_test/b
	// /" to the same node as "_test/b " — a mutation removing
	// just this line survives, correctly. It is kept because it
	// is upstream's step and because it is what makes the order
	// above matter.
	path = strings.TrimRight(path, " \t")
	path = strings.TrimRight(path, "/")
	path = strings.TrimLeft(path, " \t")
	if path == "" {
		c.tell("%s", noFlagGiven)
		return
	}

	// A value of "^" followed by a number is an integer property.
	// It is the only way to make one from the command line, and
	// look traps care: a non-string trap does not run.
	var v props.Value
	if n, ok := caretInt(value); ok {
		v = props.Value{Type: props.Int, Num: n}
	} else {
		v = props.Value{Type: props.String, Str: value}
	}

	// The guard @propset has and this did not. A system property
	// is out of bounds to everybody and a hidden or see-only one
	// to anybody who is not a wizard, so @set could write what
	// @propset refused.
	if propRestricted(path, wizard) {
		c.tell("Permission denied. (The property is " +
			"restricted.)")
		return
	}

	if value == "" {
		c.w.Get(target).Props.Delete(path)
		c.w.Modified(target)
		c.tell("Property removed.")
		return
	}
	c.w.SetProp(target, path, v)
	c.tell("Property set.")
}

// caretInt reads upstream's "^N" integer value: a caret followed by
// what number() accepts, which is an optional sign and then digits
// and nothing else.
func caretInt(value string) (int64, bool) {
	if !strings.HasPrefix(value, "^") {
		return 0, false
	}
	rest := value[1:]
	if rest == "" {
		return 0, false
	}
	body := rest
	if body[0] == '-' || body[0] == '+' {
		body = body[1:]
	}
	if body == "" {
		return 0, false
	}
	for i := 0; i < len(body); i++ {
		if body[i] < '0' || body[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// clearProperties is remove_property_list (property.c:327), which
// clears less for a non-wizard than for a wizard: the '@' and '~'
// properties and the whole "_/" propdir are left alone, because those
// are not the asker's to remove.
func (s *Server) clearProperties(w *world.World, target ref.Ref,
	wizard bool) int {

	o := w.Get(target)
	if o == nil {
		return 0
	}
	var doomed []string
	o.Props.Walk(func(e props.Entry) bool {
		if !wizard && !userOwnedProp(e.Path) {
			return true
		}
		doomed = append(doomed, e.Path)
		return true
	})
	for _, path := range doomed {
		o.Props.Delete(path)
	}
	if len(doomed) > 0 {
		w.Modified(target)
	}
	return len(doomed)
}

// userOwnedProp reports whether a non-wizard's ":clear" may remove a
// property: anything but a hidden or see-only one and anything
// outside the "_/" propdir, which holds the message properties the
// verbs write.
func userOwnedProp(path string) bool {
	if isHiddenProp(path) || propSegmentStartsWith(path, '~') {
		return false
	}
	return !ascii.HasPrefix(path, "_/")
}

// noFlagGiven is do_set's answer to an empty flag, and to the two
// cases this server used to answer with invented usage text: there is
// no usage message in do_set at all, so a missing '=' and an empty
// value both arrive here.
const noFlagGiven = "You must specify a flag to set."

// cmdSet changes a flag or a property.
func (s *Server) cmdSet(c *ctx) {
	// No usage message: do_set has none. It matches, checks God's
	// property, and then an empty flag reaches "You must specify
	// a flag to set." — so a missing '=' and an empty value
	// give the same answer, where this server invented three
	// different ones.
	name, rest, _ := strings.Cut(c.arg, "=")
	target, ok := s.matchControlled(c, strings.TrimSpace(name))
	if !ok {
		return
	}
	// set.c:752. Note the wording: do_set says "God's property"
	// where wiz.c:429 and :469 say "God's stuff" for the same
	// idea, and this command had neither.
	if c.w.Tune.Bool("strict_god_priv") && c.who != ref.God &&
		ownerOf(c.w, target) == ref.God {
		c.tell("Only God may touch God's property.")
		return
	}
	// arg2 is left-trimmed only (game.c:709), and do_set then
	// skips leading '!' and whitespace together when it looks for
	// the flag.
	rest = strings.TrimLeft(rest, " \t")

	// A ':' anywhere in the argument means a property rather than
	// a flag, which is strchr(flag, PROP_DELIMITER) and so splits
	// on the *first* colon (set.c:763).
	if path, value, isProp := strings.Cut(rest, ":"); isProp {
		s.setProperty(c, target, path, value)
		return
	}

	// Two readings of the same argument, and they disagree.
	// `negated` is the *first character* alone, so "!!W" clears
	// the wizard bit rather than setting it — `has_flag`'s "!!x
	// = x" rule is not this one. `p` skips every leading '!' and
	// space together, so "! W" and "!!!W" both name W. The tail
	// is deliberately not trimmed, because upstream's p runs to
	// the end of arg2 and `string_prefix` fails on a trailing
	// space; nothing can observe it here, since ctx.arg arrives
	// trimmed at both ends.
	clear := strings.HasPrefix(rest, "!")
	flagName := ascii.Fold(strings.TrimLeft(rest, "! \t"))

	// A guest may use @set on a *property* — this check sits
	// after the property branch — and on exactly one flag: a
	// guest who is also a wizard may clear its own GUEST bit,
	// which is how a world lets one stop being a guest. Note it
	// reads the asker's own wizardry rather than its owner's, and
	// that it comes before the empty-flag message.
	if isGuest(c.w, c.who) &&
		(!c.w.Get(c.who).Flags.IsWizard() || !clear ||
			!ascii.HasPrefix("guest", flagName)) {
		c.tell("Guests are not allowed to @set.")
		return
	}

	// Upstream tests the flag *name*, not the whole argument, so
	// a bare "!" reaches this rather than being read as a mucker
	// level: "mucker" has the empty string as a prefix, and `@set
	// me=!` used to clear both mucker bits.
	if flagName == "" {
		c.tell("%s", noFlagGiven)
		return
	}

	// Mucker levels are named where a flag would be, and are read
	// before the flag table so "M2" is a level rather than a
	// prefix of "mucker".
	if flagName == "4" || flagName == "m4" {
		c.tell("To set Mucker Level 4, set the Wizard bit and another Mucker bit.")
		return
	}
	bit, isLevel := parseMLevel(flagName, clear)
	if isLevel {
		// Level zero, and clearing any level, both come to
		// the same thing: remove both bits.
		if flagName == "0" || flagName == "m0" ||
			ascii.HasPrefix("mucker", flagName) &&
				clear {
			clear = true
		}
	} else {
		var known bool
		bit, known = strToFlag(flagName)
		// @set refuses two names that str_to_flag resolves:
		// "truewizard" is the wizard bit under another name,
		// and "nucker" is half a mucker level. Both would set
		// something other than they say.
		if !known ||
			ascii.HasPrefix("truewizard", flagName) ||
			ascii.HasPrefix("nucker", flagName) {
			c.tell("I don't recognize that flag.")
			return
		}
	}

	// One function decides every flag, including the mucker
	// levels, which this server used to put behind a blanket
	// wizard test: upstream lets a mortal set their own program
	// up to their own level.
	mlev := c.w.Get(ownerOf(c.w, c.who)).Flags.MLevel()
	if msg, no := s.unableToSetFlag(c.w, c.who, mlev, target,
		bit, !clear); no {
		if msg == "" {
			msg = "Permission denied. (restricted flag)"
		}
		c.tell("%s", msg)
		return
	}

	o := c.w.Get(target)
	// Setting either mucker bit replaces the level rather than
	// adding to it, so a level is never assembled out of two
	// commands.
	if bit&(ref.Mucker|ref.SMucker) != 0 {
		o.Flags &^= ref.Mucker | ref.SMucker
	}
	if clear {
		o.Flags &^= bit
	} else {
		o.Flags |= bit
	}
	c.w.Modified(target)

	what := "Flag"
	if bit&(ref.Mucker|ref.SMucker) != 0 {
		what = "Mucker level"
	}
	if clear {
		c.tell("%s reset.", what)
	} else {
		c.tell("%s set.", what)
	}
}

// parseMLevel reads the names @set accepts for a mucker level,
// returning the bits it sets. "mucker" is level 2, and negated is
// level 0.
func parseMLevel(name string, negated bool) (ref.Flags, bool) {
	switch name {
	case "0", "m0":
		return ref.Mucker | ref.SMucker, true
	case "1", "m1":
		return ref.SMucker, true
	case "2", "m2":
		return ref.Mucker, true
	case "3", "m3":
		return ref.Mucker | ref.SMucker, true
	}
	if ascii.HasPrefix("mucker", name) {
		if negated {
			return ref.Mucker | ref.SMucker, true
		}
		return ref.Mucker, true
	}
	return 0, false
}

// cmdPassword changes the player's own password.
func (s *Server) cmdPassword(c *ctx) {
	oldPass, newPass, ok := strings.Cut(c.arg, "=")
	if !ok {
		c.tell("Usage: @password <old password>=<new password>")
		return
	}
	oldPass, newPass = strings.TrimSpace(oldPass), strings.TrimSpace(newPass)

	o := c.w.Get(c.who)
	if !password.Verify(o.PasswordHash, oldPass).OK {
		c.tell("Your old password is incorrect.")
		s.securityLog().Warn("failed password change",
			"player", c.who.String(), "name", o.Name)
		return
	}
	if newPass == "" {
		c.tell("You must give a new password.")
		return
	}
	hashed, err := password.Hash(newPass)
	if err != nil {
		c.tell("That password could not be used.")
		return
	}
	o.PasswordHash = hashed
	c.w.Modified(c.who)
	c.tell("Password changed.")
	s.securityLog().Info("password changed",
		"player", c.who.String(), "name", o.Name)
}

// poofPuppet is do_recycle's TYPE_THING special case (create.c:897):
// a puppet told by its owner to recycle itself says so to the room
// and twice to the owner, and is then recycled like anything else.
// Reachable only through @force, since otherwise the command's actor
// is a player and the player branch has already refused.
func (s *Server) poofPuppet(c *ctx, target ref.Ref) {
	o := c.w.Get(target)
	msg := o.Name + "'s owner commands it to kill " +
		"itself.  It blinks a few times in shock, and " +
		"says, \"But.. but.. WHY?\"  It suddenly " +
		"clutches it's heart, grimacing with pain..  " +
		"Staggers a few steps before falling to it's " +
		"knees, then plops down on it's face.  *thud*  " +
		"It kicks its legs a few times, with weakening " +
		"force, as it suffers a seizure.  It's color " +
		"slowly starts changing to purple, before it " +
		"explodes with a fatal *POOF*!"
	s.notifyRoomFrom(c.w, target, o.Location,
		[]ref.Ref{target}, "%s", msg)
	owner := ownerOf(c.w, c.who)
	s.send(c.w, owner, msg)
	s.send(c.w, owner, "Now don't you feel guilty?")
}

// noRecycleRoot is do_recycle's answer for #0, which nothing reaches
// in practice: the @tune guard above it catches #0 first, since it is
// default_room_parent's value.
const noRecycleRoot = "If you want to do that, why don't you " +
	"just delete the database instead?  Room #0 contains " +
	"everything, and is needed for database sanity."

// tuneNamesObject reports whether any dbref-typed @tune parameter
// currently points at obj.
//
// Both do_recycle and prim_recycle make this test and **they word
// their refusals differently** — "That object cannot currently be
// @recycled." against "Cannot currently recycle that object." — so
// what is shared is the scan and not the message.
func tuneNamesObject(w *world.World, obj ref.Ref) bool {
	for _, p := range tune.Params() {
		if p.Type != tune.TypeDbref {
			continue
		}
		if v, _ := w.Tune.Get(p.Name); v.Ref == obj {
			return true
		}
	}
	return false
}

// noRecycleTuned is do_recycle's guard on anything a dbref @tune
// parameter points at.
const noRecycleTuned = "That object cannot currently be @recycled."

// cmdRecycle destroys an object, and is do_recycle (create.c:842).
//
// **Its per-type rules are stricter than controls(), which is the one
// place this server used to do more than upstream rather than less.**
// controls() is only the outer gate; each type then demands
// `OWNER(thing) == OWNER(player)` as well, so a wizard who does not
// own a room, thing, exit or program may not recycle it however
// freely controls() lets them touch it. Each of the four says so in
// wording of its own.
//
// The @tune guard still runs before all of that, which is most of
// what anybody sees — see noRecycleTuned.
func (s *Server) cmdRecycle(c *ctx) {
	// init_match(..., TYPE_THING, ...) then match_everything
	// (create.c:848).
	target := match.New(c.w, c.who, c.arg).
		PreferType(ref.TypeThing).Everything().Result()
	if !noisyMatch(c, c.arg, target) {
		return
	}
	// controls() is the outer gate, and its refusal has two
	// forms: a wizard looking at garbage is told what it is
	// rather than that they may not touch it.
	if !s.controls(c.w, c.who, target) {
		if isWizard(c.w, ownerOf(c.w, c.who)) &&
			c.w.Get(target).Type() == ref.TypeGarbage {
			c.tell("That's already garbage!")
		} else {
			c.tell("Permission denied. (You don't " +
				"control what you want to recycle)")
		}
		return
	}
	// The @tune guard comes *first*, and that ordering is most of
	// what anybody sees: #0 is default_room_parent's value and #1
	// is toad_default_recipient's, so "@recycle here" and
	// "@recycle me" both answer this rather than the per-type
	// refusal below them. Recycling a parameter's target would
	// leave the server pointing at garbage with nothing to say
	// about it.
	if tuneNamesObject(c.w, target) {
		c.tell("%s", noRecycleTuned)
		return
	}

	// **Each type then demands actual ownership, and that is
	// stricter than controls().** `OWNER(thing) != OWNER(player)`
	// refuses a wizard who does not own the object, even though
	// controls() has just let them through — so this server
	// used to recycle things upstream will not. Every one of the
	// four has wording of its own, and programs match on it.
	o := c.w.Get(target)
	mine := ownerOf(c.w, target) == ownerOf(c.w, c.who)
	switch o.Type() {
	case ref.TypePlayer:
		c.tell("You can't recycle a player!")
		return
	case ref.TypeGarbage:
		c.tell("That's already garbage!")
		return
	case ref.TypeRoom:
		if !mine {
			c.tell("Permission denied. (You don't " +
				"control the room you want to " +
				"recycle)")
			return
		}
		if target == ref.GlobalEnvironment {
			c.tell("%s", noRecycleRoot)
			return
		}
	case ref.TypeThing:
		if !mine {
			c.tell("Permission denied. (You can't " +
				"recycle a thing you don't control)")
			return
		}
		// A puppet recycling itself, which is reachable only
		// through @force: upstream announces it at length and
		// then goes ahead. This server refused with an
		// invented "You can't recycle yourself.", so the
		// refusal is gone and the announcement is here.
		if target == c.who {
			s.poofPuppet(c, target)
		}
	case ref.TypeExit:
		if !mine {
			c.tell("Permission denied. (You may not " +
				"recycle an exit you don't own)")
			return
		}
	case ref.TypeProgram:
		if !mine {
			c.tell("Permission denied. (You can't " +
				"recycle a program you don't own)")
			return
		}
	}

	s.evictEditors(c.w, target)
	// Named before the recycling, because that is what renames
	// the object to "<garbage>".
	name := o.Name
	if err := c.w.Recycle(target); err != nil {
		c.send(err.Error())
		return
	}
	c.tell("Thank you for recycling %s (%s).", name, target)
}

// evictEditors throws anyone editing a program out of the editor
// before it is recycled, so nobody is left typing into a session
// whose program has gone.
func (s *Server) evictEditors(w *world.World, program ref.Ref) {
	for who, e := range s.editors {
		if e.program != program {
			continue
		}
		s.closeEditor(w, who, e)
		s.send(w, who, "The program you were editing has been recycled.  Exiting Editor.")
	}
}

// payFor takes the cost of something out of a player's pocket,
// reporting whether they could afford it. A wizard pays for nothing.
func (s *Server) payFor(w *world.World, who ref.Ref, cost int) bool {
	owner := ownerOf(w, who)
	o := w.Get(owner)
	if o == nil {
		return false
	}
	if o.Flags.IsWizard() {
		return true
	}
	have := valueOf(w, owner)
	if have < int64(cost) {
		return false
	}
	w.SetProp(owner, propValue, props.Value{Type: props.Int, Num: have - int64(cost)})
	return true
}

// endowment is what an object made for a given price is worth, from
// include/db.h. It is bounded so an admin can stop a rich player
// minting value by creating expensive objects.
func endowment(w *world.World, cost int) int {
	n := (cost - 5) / 5
	if max := int(w.Tune.Int("max_object_endowment")); n > max {
		n = max
	}
	if n < 0 {
		n = 0
	}
	return n
}

// controlsLink is db.c:1883's controls_link: whether someone may
// change what an object points at, which is a different question from
// whether they control the object.
//
// It is what lets the owner of a *destination* unlink an exit leading
// to it, and upstream's own comment explains the asymmetry: for
// unlinking it decides outright, while for linking it is applied only
// once the thing is known to be an exit, because otherwise "you can
// allow someone to arbitrarily re-home other players that live in a
// room that someone owns".
//
// Two details are load-bearing. For an exit, controlling **any one**
// of its destinations is enough — the loop returns on the first —
// and the fallback compares `who` to the owner of the exit's location
// *without* going through controls, so it is a raw ownership test
// rather than a control one. A program is never linkable by this
// route, and neither is anything else: the default is false.
func (s *Server) controlsLink(w *world.World, who,
	what ref.Ref) bool {

	o := w.Get(what)
	if o == nil {
		return false
	}
	switch o.Type() {
	case ref.TypeExit:
		for _, dest := range o.Dest {
			if s.controls(w, who, dest) {
				return true
			}
		}
		// OWNER(LOCATION(what)), compared to who directly.
		loc := w.Get(o.Location)
		return loc != nil && who == loc.Owner
	case ref.TypeRoom:
		return s.controls(w, who, o.Dropto)
	case ref.TypePlayer, ref.TypeThing:
		return s.controls(w, who, o.Home)
	default:
		return false
	}
}

// canLinkTo is `can_link_to` (`predicates.c:117`): whether something
// of type `what` may be attached to `where`.
//
// What stood here was `can_teleport_to`'s rule under this name -- no
// type argument at all, so **none** of the four type rules existed,
// `LINK_OK` and `ABODE` were tested the wrong way round, HOME and NIL
// were not special-cased, and the link lock was not consulted. So a
// thing's home could be a program, a room's dropto could be an exit,
// and a program could be linked. Upstream keeps `can_link_to` and
// `can_teleport_to` apart with a comment saying the rules could
// diverge; this had collapsed them, and the one it kept was the wrong
// one.
//
// `Linkable` (`db.h:576`) is the flag half, and it is not what the
// old code said: a **room or thing** is linkable when ABODE is set,
// and anything else when LINK_OK is. The old test asked for LINK_OK
// first and then ABODE for everything but a thing, which is right for
// neither.
func (s *Server) canLinkTo(w *world.World, descr int, who ref.Ref,
	what ref.ObjType, where ref.Ref) bool {

	// HOME is always linkable, and an exit may point at NIL. Both
	// are before the validity check, because neither is an
	// object.
	if where == ref.Home {
		return true
	}
	if what == ref.TypeExit && where == ref.Nil {
		return true
	}
	if !w.Valid(where) {
		return false
	}
	to := w.Get(where).Type()
	switch {
	case what == ref.TypePlayer && to != ref.TypeRoom:
		return false
	case what == ref.TypeRoom && to != ref.TypeThing &&
		to != ref.TypeRoom:
		return false
	case what == ref.TypeThing && (to == ref.TypeExit ||
		to == ref.TypeProgram):
		return false
	case what == ref.TypeProgram:
		return false
	}
	if s.controls(w, who, where) {
		return true
	}
	return linkableFlag(w, where) &&
		s.lockPasses(w, descr, 1, who, where, propLinkLock,
			true)
}

// linkableFlag is the `Linkable` macro's flag test, without its HOME
// and NIL cases, which `canLinkTo` answers first.
func linkableFlag(w *world.World, where ref.Ref) bool {
	o := w.Get(where)
	if o == nil {
		return false
	}
	if t := o.Type(); t == ref.TypeRoom || t == ref.TypeThing {
		return o.Flags&ref.Abode != 0
	}
	return o.Flags&ref.LinkOK != 0
}
