package game

import (
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
		case !s.canLinkTo(c.w, c.who, r) || r == o.Ref:
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
		if dest, ok := s.resolveLinkTarget(c, destName); ok {
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

// resolveLinkTarget is parse_linkable_dest (db.c:1971): what an exit
// should point at.
//
// Every refusal here names the object it is refusing, and the failed
// match goes through noisy_match_result like every other command's
// — "I don't understand 'X'." An earlier version said "I don't see
// that here." and refused without naming anything.
func (s *Server) resolveLinkTarget(c *ctx, name string) (ref.Ref, bool) {
	// There is no empty-name guard upstream. "Link it to what?"
	// was invented here, and it shadowed two different real
	// answers: for an exit, _link_exit's loop never runs on an
	// empty string, so nothing is matched at all and do_link says
	// "No destinations linked."; for everything else the empty
	// name reaches the matcher and noisy_match_result answers.
	r := match.New(c.w, c.who, name).
		PreferType(ref.TypeRoom).
		Absolute().Me().Here().Home().Nil().
		Possession().Neighbor().Player().Result()
	if !noisyMatch(c, name, r) {
		return ref.Nothing, false
	}
	if r == ref.Home || r == ref.Nil {
		return r, true
	}

	o := c.w.Get(r)
	// Linking to a player is a separate refusal from being unable
	// to link, and a separate @tune parameter — which nothing
	// in this server read before.
	if o.Type() == ref.TypePlayer &&
		!c.w.Tune.Bool("teleport_to_player") {
		c.tell("You can't link to players.  Destination "+
			"%s ignored.", unparse(c.w, c.who, r))
		return ref.Nothing, false
	}
	// Anyone may link to a room or thing flagged to allow it, or
	// to anything they control.
	linkable := o.Flags&ref.LinkOK != 0 ||
		(o.Type() == ref.TypeRoom || o.Type() == ref.TypeThing) && o.Flags&ref.Abode != 0
	if !linkable && !s.controls(c.w, c.who, r) {
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
	dest, ok := s.resolveLinkTarget(c, destName)
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
	dest, ok := s.resolveLinkTarget(c, destName)
	if !ok {
		return
	}
	if !s.controls(c.w, c.who, target) ||
		!s.canLinkTo(c.w, c.who, dest) {
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
func (s *Server) linkDropto(c *ctx, target ref.Ref, destName string) {
	dest, ok := s.resolveLinkTarget(c, destName)
	if !ok {
		return
	}
	if !s.controls(c.w, c.who, target) ||
		!s.canLinkTo(c.w, c.who, dest) || target == dest {
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

// wizardOnlyFlags may only be changed by a wizard.
var wizardOnlyFlags = map[ref.Flags]bool{
	ref.Wizard: true, ref.Builder: true, ref.Guest: true, ref.Quell: true,
	ref.XForcible: true, ref.Overt: true,
}

// cmdSet changes a flag or a property.
func (s *Server) cmdSet(c *ctx) {
	name, rest, ok := strings.Cut(c.arg, "=")
	if !ok {
		c.tell("Usage: @set <object>=[!]<flag>  or  @set <object>=<prop>:<value>")
		return
	}
	target, ok := s.matchControlled(c, strings.TrimSpace(name))
	if !ok {
		return
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		c.tell("Set what?")
		return
	}

	// A ':' means a property rather than a flag.
	if path, value, isProp := strings.Cut(rest, ":"); isProp {
		path = strings.TrimSpace(path)
		if path == "" {
			c.tell("Set which property?")
			return
		}
		if value == "" {
			c.w.Get(target).Props.Delete(path)
			c.w.Modified(target)
			c.tell("Property cleared.")
			return
		}
		c.w.SetProp(target, path, props.Value{Type: props.String, Str: value})
		c.tell("Property set.")
		return
	}

	clear := strings.HasPrefix(rest, "!")
	flagName := ascii.Fold(strings.TrimSpace(strings.TrimPrefix(rest, "!")))

	// Mucker levels are named where a flag would be, and are read
	// before the flag table so "M2" is a level rather than a
	// prefix of "mucker".
	if flagName == "4" || flagName == "m4" {
		c.tell("To set Mucker Level 4, set the Wizard bit and another Mucker bit.")
		return
	}
	bit, isLevel := parseMLevel(flagName, clear)
	if isLevel {
		if !s.requireWizard(c) {
			return
		}
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
		if wizardOnlyFlags[bit] &&
			!c.w.Get(c.who).Flags.IsWizard() {
			c.tell("Permission denied.")
			return
		}
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
	for _, p := range c.w.Tune.Params() {
		if p.Type != tune.TypeDbref {
			continue
		}
		if v, _ := c.w.Tune.Get(p.Name); v.Ref == target {
			c.tell("%s", noRecycleTuned)
			return
		}
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

// canLinkTo reports whether someone may attach something to a
// destination: they control it, or it is open to anyone through its
// LINK_OK or ABODE flag.
func (s *Server) canLinkTo(w *world.World, who, where ref.Ref) bool {
	if s.controls(w, who, where) {
		return true
	}
	o := w.Get(where)
	if o == nil {
		return false
	}
	return o.Flags&ref.LinkOK != 0 ||
		o.Type() != ref.TypeThing && o.Flags&ref.Abode != 0
}
