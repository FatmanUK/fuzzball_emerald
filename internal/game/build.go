package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
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

// resolveControlled finds an object the player may modify, which is
// upstream's match_controlled.
//
// A failed match is reported by noisyMatch, which is
// noisy_match_result: "I don't understand 'X'." Every upstream
// command that lands here reaches it through that function, directly
// or through match_controlled, so the wording is shared and programs
// match on it. An earlier version said "I don't see that here.",
// which belongs to the commands that match quietly and complain in
// their own words.
func (s *Server) resolveControlled(c *ctx, name string) (ref.Ref, bool) {
	// Player() is included so a wizard can name someone who is
	// elsewhere in the game, which @teleport and @set both need.
	r := match.New(c.w, c.who, name).Everything().Player().Result()
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
	name, costArg, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	if name == "" {
		c.tell("Usage: @create <name> [=<cost>]")
		return
	}

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
}

// cmdDig makes a room.
func (s *Server) cmdDig(c *ctx) {
	if !s.requireBuilder(c) {
		return
	}
	name, parentName, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	if name == "" {
		c.tell("Usage: @dig <name> [=<parent>]")
		return
	}

	if !s.payFor(c.w, c.who, int(c.w.Tune.Int("room_cost"))) {
		c.tell("Sorry, you don't have enough %s to dig a room.",
			c.w.Tune.String("pennies"))
		return
	}

	parent := c.w.Tune.Ref("default_room_parent")
	if !c.w.Valid(parent) {
		parent = ref.GlobalEnvironment
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
		r := match.New(c.w, c.who, p).Absolute().Registered().Here().Result()
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
}

// cmdOpen makes an exit in the current room.
func (s *Server) cmdOpen(c *ctx) {
	if !s.requireBuilder(c) {
		return
	}
	name, destName, hasDest := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	if name == "" {
		c.tell("Usage: @open <name>[;<alias>...] [=<destination>]")
		return
	}

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
		if dest, ok := s.resolveLinkTarget(c, strings.TrimSpace(destName)); ok {
			o.Dest = []ref.Ref{dest}
			c.w.Modified(o.Ref)
			c.tell("Linked to %s.", unparse(c.w, c.who, dest))
		}
	}
}

// resolveLinkTarget finds what an exit should point at.
func (s *Server) resolveLinkTarget(c *ctx, name string) (ref.Ref, bool) {
	if name == "" {
		c.tell("Link it to what?")
		return ref.Nothing, false
	}
	r := match.New(c.w, c.who, name).Absolute().Me().Here().Home().Nil().
		Possession().Neighbor().Player().Result()
	switch r {
	case ref.Nothing:
		c.tell("I don't see that here.")
		return ref.Nothing, false
	case ref.Ambiguous:
		c.tell("I don't know which one you mean.")
		return ref.Nothing, false
	case ref.Home, ref.Nil:
		return r, true
	}
	// Anyone may link to a room or thing flagged to allow it, or
	// to anything they control.
	o := c.w.Get(r)
	linkable := o.Flags&ref.LinkOK != 0 ||
		(o.Type() == ref.TypeRoom || o.Type() == ref.TypeThing) && o.Flags&ref.Abode != 0
	if !linkable && !s.controls(c.w, c.who, r) {
		c.tell("You can't link to that.")
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
	target, ok := s.resolveControlled(c, strings.TrimSpace(name))
	if !ok {
		return
	}
	dest, ok := s.resolveLinkTarget(c, strings.TrimSpace(destName))
	if !ok {
		return
	}

	o := c.w.Get(target)
	switch o.Type() {
	case ref.TypeExit:
		o.Dest = []ref.Ref{dest}
	case ref.TypeThing, ref.TypePlayer:
		o.Home = dest
	case ref.TypeRoom:
		o.Dropto = dest
	default:
		c.tell("You can't link that.")
		return
	}
	c.w.Modified(target)
	c.tell("Linked to %s.", unparse(c.w, c.who, dest))
}

// cmdUnlink removes an exit's destination or a room's drop-to.
func (s *Server) cmdUnlink(c *ctx) {
	target, ok := s.resolveControlled(c, c.arg)
	if !ok {
		return
	}
	o := c.w.Get(target)
	switch o.Type() {
	case ref.TypeExit:
		o.Dest = nil
	case ref.TypeRoom:
		o.Dropto = ref.Nothing
	default:
		c.tell("You can't unlink that.")
		return
	}
	c.w.Modified(target)
	c.tell("Unlinked.")
}

// cmdName renames an object.
func (s *Server) cmdName(c *ctx) {
	name, newName, ok := strings.Cut(c.arg, "=")
	if !ok {
		c.tell("Usage: @name <object>=<new name>")
		return
	}
	target, ok := s.resolveControlled(c, strings.TrimSpace(name))
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

// cmdDescribe sets an object's description.
func (s *Server) cmdDescribe(c *ctx) {
	name, desc, ok := strings.Cut(c.arg, "=")
	if !ok {
		c.tell("Usage: @describe <object>=<description>")
		return
	}
	target, ok := s.resolveControlled(c, strings.TrimSpace(name))
	if !ok {
		return
	}
	c.w.SetProp(target, propDesc, props.Value{
		Type: props.String, Str: strings.TrimSpace(desc),
	})
	c.tell("Description set.")
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
	target, ok := s.resolveControlled(c, strings.TrimSpace(name))
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

// cmdFind lists objects the player owns whose name matches.
func (s *Server) cmdFind(c *ctx) {
	if !s.requireBuilder(c) {
		return
	}
	pattern := strings.TrimSpace(c.arg)
	found := 0
	c.w.Each(func(o *world.Object) bool {
		if o.Owner != c.who &&
			!c.w.Get(c.who).Flags.IsWizard() {
			return true
		}
		if o.Type() == ref.TypeGarbage {
			return true
		}
		if pattern != "" &&
			!match.StringMatch(o.Name, pattern) {
			return true
		}
		c.send(unparse(c.w, c.who, o.Ref))
		found++
		return found < 200
	})
	c.tell("%d objects found.", found)
}

// cmdTeleport moves an object somewhere else.
func (s *Server) cmdTeleport(c *ctx) {
	name, destName, ok := strings.Cut(c.arg, "=")
	if !ok {
		// With one argument, teleport the player themselves.
		name, destName = "me", c.arg
	}
	target, ok2 := s.resolveControlled(c, strings.TrimSpace(name))
	if !ok2 {
		return
	}
	dest := match.New(c.w, c.who, strings.TrimSpace(destName)).
		Absolute().Here().Home().Possession().Neighbor().Player().Result()
	switch dest {
	case ref.Nothing:
		c.tell("I don't see that destination.")
		return
	case ref.Ambiguous:
		c.tell("I don't know which destination you mean.")
		return
	case ref.Home:
		dest = c.w.Get(target).Home
	}
	if !c.w.Valid(dest) {
		c.tell("That destination doesn't exist.")
		return
	}
	// Only a wizard may drop things into somewhere they do not
	// control.
	if !s.controls(c.w, c.who, dest) &&
		c.w.Get(dest).Flags&ref.JumpOK == 0 {
		c.tell("You can't teleport there.")
		return
	}

	if target == c.who {
		s.moveTo(c.w, c.who, dest, ref.Nothing)
		return
	}
	if err := c.w.MoveTo(target, dest); err != nil {
		c.send(err.Error())
		return
	}
	c.tell("Teleported.")
}

// cmdRecycle destroys an object.
func (s *Server) cmdRecycle(c *ctx) {
	target, ok := s.resolveControlled(c, c.arg)
	if !ok {
		return
	}
	o := c.w.Get(target)
	switch {
	case o.Type() == ref.TypePlayer:
		c.tell("You can't recycle a player; use @toad.")
		return
	case target == ref.GlobalEnvironment:
		c.tell("You can't recycle the global environment.")
		return
	case target == c.who:
		c.tell("You can't recycle yourself.")
		return
	}
	s.evictEditors(c.w, target)
	name := o.Name
	if err := c.w.Recycle(target); err != nil {
		c.send(err.Error())
		return
	}
	c.tell("%s recycled.", name)
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
