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

// requireBuilder reports whether the player may use construction commands,
// telling them if not.
func (s *Server) requireBuilder(c *ctx) bool {
	if c.w.Get(c.who).Flags.CanBuild() {
		return true
	}
	c.tell("Only builders are allowed to do that.")
	return false
}

// requireWizard reports whether the player has wizard powers, telling them if
// not.
func (s *Server) requireWizard(c *ctx) bool {
	if c.w.Get(c.who).Flags.IsWizard() {
		return true
	}
	c.tell("Permission denied.")
	return false
}

// resolveControlled finds an object the player may modify.
func (s *Server) resolveControlled(c *ctx, name string) (ref.Ref, bool) {
	// Player() is included so a wizard can name someone who is elsewhere in
	// the game, which @teleport and @set both need.
	r := match.New(c.w, c.who, name).Everything().Player().Result()
	switch r {
	case ref.Nothing:
		c.tell("I don't see that here.")
		return ref.Nothing, false
	case ref.Ambiguous:
		c.tell("I don't know which one you mean.")
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
	name := strings.TrimSpace(c.arg)
	if name == "" {
		c.tell("Usage: @create <name>")
		return
	}
	o := c.w.Create(name, ref.TypeThing, c.who)
	o.Home = c.w.Get(c.who).Location
	if err := c.w.MoveTo(o.Ref, c.who); err != nil {
		c.tell("Created, but it could not be given to you.")
		return
	}
	c.tell("%s created with number %v.", name, o.Ref)
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

	parent := c.w.Tune.Ref("default_room_parent")
	if p := strings.TrimSpace(parentName); p != "" {
		r := match.New(c.w, c.who, p).Thing().Result()
		if r == ref.Nothing || r == ref.Ambiguous {
			c.tell("I don't see that parent room.")
			return
		}
		parent = r
	}
	if !c.w.Valid(parent) {
		parent = ref.GlobalEnvironment
	}

	o := c.w.Create(name, ref.TypeRoom, c.who)
	o.Dropto = ref.Nothing
	if err := c.w.MoveTo(o.Ref, parent); err != nil {
		c.tell("Room created, but it could not be parented.")
		return
	}
	c.tell("%s created with number %v, parented to %s.",
		name, o.Ref, unparse(c.w, c.who, parent))
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
		c.tell("Permission denied.")
		return
	}

	o := c.w.Create(name, ref.TypeExit, c.who)
	if err := c.w.MoveTo(o.Ref, here); err != nil {
		c.tell("The exit could not be attached.")
		return
	}
	c.tell("Exit opened with number %v.", o.Ref)

	if hasDest {
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
	// Anyone may link to a room or thing flagged to allow it, or to
	// anything they control.
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

	// Renaming a player needs the same checks as creating one, and the
	// player's own password, which @name does not take.
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

// settableFlags maps the names @set accepts to their bits. Internal flags are
// deliberately absent: they describe live server state, not anything an
// operator should be able to write.
var settableFlags = map[string]ref.Flags{
	"abode":     ref.Abode,
	"builder":   ref.Builder,
	"chown_ok":  ref.ChownOK,
	"dark":      ref.Dark,
	"guest":     ref.Guest,
	"haven":     ref.Haven,
	"jump_ok":   ref.JumpOK,
	"kill_ok":   ref.KillOK,
	"link_ok":   ref.LinkOK,
	"overt":     ref.Overt,
	"quell":     ref.Quell,
	"sticky":    ref.Sticky,
	"vehicle":   ref.Vehicle,
	"wizard":    ref.Wizard,
	"xforcible": ref.XForcible,
	"yield":     ref.Yield,
	"zombie":    ref.Zombie,
}

// wizardOnlyFlags may only be changed by a wizard.
var wizardOnlyFlags = map[string]bool{
	"wizard": true, "builder": true, "guest": true, "quell": true,
	"xforcible": true, "overt": true,
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

	// A bare mucker level, as Fuzzball accepts: "@set foo=M3".
	if lvl, isMLev := parseMLevel(flagName); isMLev {
		if !s.requireWizard(c) {
			return
		}
		o := c.w.Get(target)
		if clear {
			lvl = 0
		}
		o.Flags = o.Flags.SetMLevel(lvl)
		c.w.Modified(target)
		c.tell("Mucker level set to %d.", lvl)
		return
	}

	bit, known := settableFlags[flagName]
	if !known {
		c.tell("I don't know that flag.")
		return
	}
	if wizardOnlyFlags[flagName] && !c.w.Get(c.who).Flags.IsWizard() {
		c.tell("Permission denied.")
		return
	}

	o := c.w.Get(target)
	if clear {
		o.Flags &^= bit
		c.tell("Flag reset.")
	} else {
		o.Flags |= bit
		c.tell("Flag set.")
	}
	c.w.Modified(target)
}

// parseMLevel reads "m0".."m3", "1".."3" as a mucker level.
func parseMLevel(s string) (int, bool) {
	t := strings.TrimPrefix(s, "m")
	if len(t) != 1 || t[0] < '0' || t[0] > '3' {
		return 0, false
	}
	// Only treat a bare digit as a level when it came with the M, or is a
	// lone digit; anything else is a flag name.
	if s != t && len(s) != 2 {
		return 0, false
	}
	return int(t[0] - '0'), true
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
		s.statusLog().Warn("failed password change",
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
	s.statusLog().Info("password changed",
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
		if o.Owner != c.who && !c.w.Get(c.who).Flags.IsWizard() {
			return true
		}
		if o.Type() == ref.TypeGarbage {
			return true
		}
		if pattern != "" && !match.StringMatch(o.Name, pattern) {
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
	// Only a wizard may drop things into somewhere they do not control.
	if !s.controls(c.w, c.who, dest) && c.w.Get(dest).Flags&ref.JumpOK == 0 {
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

// evictEditors throws anyone editing a program out of the editor before it is
// recycled, so nobody is left typing into a session whose program has gone.
func (s *Server) evictEditors(w *world.World, program ref.Ref) {
	for who, e := range s.editors {
		if e.program != program {
			continue
		}
		s.closeEditor(w, who, e)
		s.send(who, "The program you were editing has been recycled.  Exiting Editor.")
	}
}
