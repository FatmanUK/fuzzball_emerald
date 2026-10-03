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
// The others are resolveControlled, below.
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

// resolveControlled is what @link, @unlink and @recycle use, and it
// is **not** upstream's match_controlled. None of those three goes
// through it: each matches for itself and then applies a check of its
// own, with its own wording and — more importantly — its own
// rules.
//
// The differences are behavioural, not cosmetic, and none is fixed
// here:
//
//   - @unlink also accepts controls_link, so upstream lets the
//     destination's owner unlink an exit and this refuses them.
//   - @link lets a builder who controls nothing *seize* an unlinked
//     exit, paying for it; this refuses before that can happen.
//   - @recycle is stricter than controls: upstream requires actual
//     ownership of a room or thing even of a wizard, so this server
//     currently lets a wizard recycle objects upstream refuses.
//
// @teleport was the fourth, and is ported: its check depends on the
// destination, so it cannot happen at match time at all. See
// teleport.go.
//
// prefer is choose_thing's preferred type, which differs per caller:
// @link and @unlink want an exit and @recycle a thing. It only
// decides a tie between two *exact* matches, so it is invisible until
// a room holds two objects of the same name.
//
// Each of the three needs its own commit. Until then they keep the
// message they have always had, which is at least not pretending to
// be upstream's.
//
// What *is* fixed is the matcher: this used to add Player()
// unconditionally, where match_everything adds match_player only for
// a wizard, so a mortal could name any player in the game and be
// refused on permission instead of on the match. See matcher_test.go.
func (s *Server) resolveControlled(c *ctx, name string,
	prefer ref.ObjType) (ref.Ref, bool) {

	r := match.New(c.w, c.who, name).
		PreferType(prefer).Everything().Result()
	if !noisyMatch(c, name, r) {
		return ref.Nothing, false
	}
	if !s.controls(c.w, c.who, r) {
		c.tell("Permission denied.")
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
	if name == "" {
		c.tell("Link it to what?")
		return ref.Nothing, false
	}
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

// cmdLink points an exit at a destination, or sets a home.
func (s *Server) cmdLink(c *ctx) {
	name, destName, ok := strings.Cut(c.arg, "=")
	if !ok {
		c.tell("Usage: @link <object>=<destination>")
		return
	}
	// do_link's own init_match asks for an exit (create.c:147).
	target, ok := s.resolveControlled(c, strings.TrimSpace(name),
		ref.TypeExit)
	if !ok {
		return
	}
	dest, ok := s.resolveLinkTarget(c, strings.TrimSpace(destName))
	if !ok {
		return
	}

	// What @link says it did depends on the type, because it is
	// three different operations wearing one name: an exit gets a
	// destination, a thing or a player gets a home, and a room
	// gets a drop-to. Only the first is "linked" in upstream's
	// own words.
	o := c.w.Get(target)
	switch o.Type() {
	case ref.TypeExit:
		o.Dest = []ref.Ref{dest}
		c.w.Modified(target)
		c.tell("%s", linkedTo(c, dest))
		return
	case ref.TypeThing, ref.TypePlayer:
		o.Home = dest
		c.w.Modified(target)
		c.tell("Home set.")
		return
	case ref.TypeRoom:
		o.Dropto = dest
		c.w.Modified(target)
		c.tell("Dropto set.")
		return
	case ref.TypeProgram:
		c.tell("You can't link programs to things!")
		return
	}
	c.tell("You can't link that.")
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

// noRecycleRoot is do_recycle's answer for #0, which nothing reaches
// in practice: the @tune guard above it catches #0 first, since it is
// default_room_parent's value.
const noRecycleRoot = "If you want to do that, why don't you " +
	"just delete the database instead?  Room #0 contains " +
	"everything, and is needed for database sanity."

// noRecycleTuned is do_recycle's guard on anything a dbref @tune
// parameter points at.
const noRecycleTuned = "That object cannot currently be @recycled."

// cmdRecycle destroys an object.
//
// Three of its replies are upstream's and three are not. The
// confirmation, the global-environment refusal and the guard on an
// object a @tune parameter points at are do_recycle's own
// (create.c:841); the per-type ownership rules are still
// resolveControlled's, which is **stricter** than upstream for a
// wizard and laxer for an owner — see that function's doc comment,
// and docs/upstream-coverage.md.
func (s *Server) cmdRecycle(c *ctx) {
	// do_recycle asks for a thing (create.c:848).
	target, ok := s.resolveControlled(c, c.arg, ref.TypeThing)
	if !ok {
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

	o := c.w.Get(target)
	switch {
	case o.Type() == ref.TypePlayer:
		c.tell("You can't recycle a player!")
		return
	case o.Type() == ref.TypeGarbage:
		c.tell("That's already garbage!")
		return
	case target == ref.GlobalEnvironment:
		c.tell("%s", noRecycleRoot)
		return
	case target == c.who:
		c.tell("You can't recycle yourself.")
		return
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
