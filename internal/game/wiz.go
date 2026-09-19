package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/tune"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// cmdStats counts what is in the database, for everyone or for one player.
//
// A non-wizard asking for no one in particular is told only the size of the
// database, which is what upstream gives away.
func (s *Server) cmdStats(c *ctx) {
	name := strings.TrimSpace(c.arg)
	wizard := ownerIsWizard(c.w, c.who)

	if !wizard && name == "" {
		c.tell("The universe contains %d objects.", int32(c.w.Top()))
		return
	}

	owner := ref.Nothing
	if name != "" {
		r, ok := c.w.PlayerNamed(name)
		if !ok {
			c.tell("I can't find that player.")
			return
		}
		if !wizard && ownerOf(c.w, c.who) != r {
			c.tell("Permission denied. (you must be a wizard to get someone else's stats)")
			return
		}
		owner = r
	}

	var rooms, exits, things, players, programs, garbage, total, old int
	// Anything untouched for longer than aging_time is counted as old,
	// which is how an admin finds what a database has stopped using.
	aging := c.w.Tune.Duration("aging_time")
	now := c.w.Now()

	c.w.Each(func(o *world.Object) bool {
		mine := owner == ref.Nothing || o.Owner == owner
		if o.Type() == ref.TypePlayer && owner != ref.Nothing {
			// A player counts for themselves, not for whoever owns
			// them, so an owner search does not pick up everyone a
			// wizard happens to own.
			mine = o.Ref == owner
		}
		if !mine {
			return true
		}
		if aging > 0 && now.Sub(o.LastUsed) > aging {
			old++
		}
		switch o.Type() {
		case ref.TypeRoom:
			rooms++
		case ref.TypeExit:
			exits++
		case ref.TypeThing:
			things++
		case ref.TypePlayer:
			players++
		case ref.TypeProgram:
			programs++
		case ref.TypeGarbage:
			// Garbage belongs to no one, so it is only counted in
			// the whole-database figure.
			if owner != ref.Nothing {
				return true
			}
			garbage++
		default:
			return true
		}
		total++
		return true
	})

	c.tell("%7d room%s        %7d exit%s        %7d thing%s",
		rooms, padPlural(rooms), exits, padPlural(exits), things, padPlural(things))
	c.tell("%7d program%s     %7d player%s      %7d garbage",
		programs, padPlural(programs), players, padPlural(players), garbage)
	c.tell("%7d total object%s                     %7d old & unused",
		total, padPlural(total), old)
}

// padPlural is upstream's plural for the stats table: a trailing space keeps
// the columns lined up when the word is singular.
func padPlural(n int) string {
	if n == 1 {
		return " "
	}
	return "s"
}

// cmdBoot disconnects a player.
func (s *Server) cmdBoot(c *ctx) {
	if !s.requireWizard(c) {
		return
	}
	name := strings.TrimSpace(c.arg)
	victim, ok := c.w.PlayerNamed(name)
	if !ok {
		c.tell("That player does not exist.")
		return
	}
	o := c.w.Get(victim)
	if o == nil || o.Type() != ref.TypePlayer {
		c.tell("You can only boot players!")
		return
	}
	if victim == ref.God {
		c.tell("You can't boot God!")
		return
	}

	s.notify(c.w, victim, "You have been booted off the game.")
	// Upstream boots one connection, the most recent, rather than all of
	// them: booting someone with two clients open leaves the other one up.
	ds := s.hub.DescriptorsFor(victim)
	if len(ds) == 0 {
		c.tell("%s is not connected.", o.Name)
		return
	}
	ds[len(ds)-1].Close()
	s.statusLog().Warn("booted",
		"player", victim.String(), "name", o.Name,
		"by", c.who.String(), "byName", nameOf(c.w, c.who))
	if victim != c.who {
		c.tell("You booted %s off!", o.Name)
	}
}

// cmdToad deletes a player, turning them into an object and handing what they
// owned to someone else.
func (s *Server) cmdToad(c *ctx) {
	if !s.requireWizard(c) {
		return
	}
	name, recipName, _ := strings.Cut(c.arg, "=")
	victim, ok := c.w.PlayerNamed(strings.TrimSpace(name))
	if !ok {
		c.tell("That player does not exist.")
		return
	}
	if victim == ref.God {
		c.tell("You cannot @toad God.")
		if c.who != ref.God {
			s.statusLog().Warn("toad attempt on God",
				"by", c.who.String(), "byName", nameOf(c.w, c.who))
		}
		return
	}
	if victim == c.who {
		c.tell("You cannot toad yourself.  Get someone else to do it for you.")
		return
	}
	// A player named by a @tune parameter is load-bearing — the default
	// toad recipient, lost-and-found, the starting room's owner — and
	// deleting one would leave the parameter pointing at an object of the
	// wrong type.
	if param, named := tuneRefersTo(c.w, victim); named {
		s.log.Info("refused to toad a tuned player",
			"player", victim.String(), "parameter", param)
		c.tell("That player cannot currently be @toaded.")
		return
	}

	recipient := c.w.Tune.Ref("toad_default_recipient")
	if r := strings.TrimSpace(recipName); r != "" {
		found, ok := c.w.PlayerNamed(r)
		if !ok || found == victim {
			c.tell("That recipient does not exist.")
			return
		}
		recipient = found
	}

	o := c.w.Get(victim)
	if o == nil || o.Type() != ref.TypePlayer {
		c.tell("You can only turn players into toads!")
		return
	}
	if o.Flags.IsTrueWizard() {
		c.tell("You can't turn a Wizard into a toad.")
		return
	}

	s.notify(c.w, victim, "You have been turned into a toad.")
	c.tell("You turned %s into a toad!", o.Name)
	s.statusLog().Warn("toaded",
		"player", victim.String(), "name", o.Name,
		"by", c.who.String(), "byName", nameOf(c.w, c.who),
		"recipient", recipient.String())

	s.toadPlayer(c, victim, recipient)
}

// toadPlayer performs the deletion itself.
func (s *Server) toadPlayer(c *ctx, victim, recipient ref.Ref) {
	w := c.w
	name := nameOf(w, victim)

	// Whatever they were carrying goes home rather than vanishing with
	// them or piling up wherever they happened to be standing.
	for _, r := range w.Contents(victim) {
		if err := w.SendHome(r); err != nil {
			s.log.Warn("could not send a toaded player's belongings home",
				"object", r.String(), "err", err)
		}
	}
	s.killProcessesFor(victim)

	// Everything they owned changes hands. A program passed to a wizard
	// loses the flags that would let it run with the new owner's powers.
	w.Each(func(o *world.Object) bool {
		if o.Owner == victim {
			switch o.Type() {
			case ref.TypeProgram:
				s.killProcessesOf(o.Ref)
				s.InvalidateProgram(o.Ref)
				if r := w.Get(recipient); r != nil && r.Flags.IsTrueWizard() {
					o.Flags &^= ref.Abode | ref.Wizard
					o.Flags = o.Flags.SetMLevel(1)
				}
				fallthrough
			case ref.TypeRoom, ref.TypeThing, ref.TypeExit:
				o.Owner = recipient
				w.Modified(o.Ref)
			}
		}
		if o.Type() == ref.TypeThing && o.Home == victim {
			o.Home = w.Tune.Ref("lost_and_found")
			w.Modified(o.Ref)
		}
		return true
	})
	w.ChownMacros(victim, recipient)

	// Close any editor the victim had open, so nothing is left holding a
	// program that now belongs to someone else.
	if e := s.editing(victim); e != nil {
		s.closeEditor(w, victim, e)
	}

	if err := w.Toad(victim, c.who, "A slimy toad named "+name); err != nil {
		c.send(err.Error())
		return
	}
	for _, d := range s.hub.DescriptorsFor(victim) {
		d.Close()
	}
	w.SetProp(victim, propValue, props.Value{Type: props.Int, Num: 1})

	if w.Tune.Bool("toad_recycle") {
		if err := w.Recycle(victim); err != nil {
			s.log.Warn("could not recycle a toad",
				"object", victim.String(), "err", err)
		}
	}
}

// tuneRefersTo reports whether a @tune parameter points at an object, naming
// the first one that does.
func tuneRefersTo(w *world.World, r ref.Ref) (string, bool) {
	for _, p := range w.Tune.Params() {
		if p.Type != tune.TypeDbref {
			continue
		}
		if v, ok := w.Tune.Get(p.Name); ok && v.Ref == r {
			return p.Name, true
		}
	}
	return "", false
}

// @force registers itself here rather than in the table's declaration: it
// dispatches commands, so naming it there would make the table refer to
// itself.
func init() {
	atCommands["@force"] = (*Server).cmdForce
	atCommands["@pcreate"] = (*Server).cmdPcreate
}

// cmdForce makes another object run a command as though it had typed it.
func (s *Server) cmdForce(c *ctx) {
	// A missing "=" is not a usage error: upstream hands both halves to the
	// matcher regardless, so "@force" on its own complains about the empty
	// name it could not find.
	what, command, _ := strings.Cut(c.arg, "=")
	what = strings.TrimSpace(what)
	if int64(s.forceDepth) >= c.w.Tune.Int("max_force_level") {
		c.tell("Can't force recursively.")
		return
	}
	wizard := c.w.Get(c.who).Flags.IsWizard()
	if !c.w.Tune.Bool("allow_zombies") && !wizard {
		c.tell("Zombies are not enabled here.")
		return
	}

	victim := match.New(c.w, c.who, what).
		Neighbor().Possession().Me().Here().Absolute().Registered().Player().Result()
	if !noisyMatch(c, what, victim) {
		return
	}
	if victim == ref.God {
		c.tell("You cannot force God to do anything.")
		return
	}

	o := c.w.Get(victim)
	if o.Type() != ref.TypePlayer && o.Type() != ref.TypeThing {
		c.tell("Permission Denied -- Target not a player or thing.")
		return
	}
	if !wizard && o.Flags&ref.XForcible == 0 {
		c.tell("Permission denied: forced object not @set Xforcible.")
		return
	}
	if !wizard && o.Type() == ref.TypeThing {
		if c.w.Get(c.who).Flags&ref.Zombie != 0 {
			c.tell("Permission denied -- you cannot use zombies.")
			return
		}
		if o.Flags&ref.Dark != 0 {
			c.tell("Permission denied -- you cannot force dark zombies.")
			return
		}
		// A puppet must not be able to impersonate a player, so its
		// first word may not be somebody's name.
		first, _, _ := strings.Cut(o.Name, " ")
		if _, taken := c.w.PlayerNamed(first); taken {
			c.tell("Puppet cannot share the name of a player.")
			return
		}
	}

	s.statusLog().Warn("forced",
		"target", victim.String(), "name", o.Name,
		"by", c.who.String(), "byName", nameOf(c.w, c.who),
		"command", command)

	// forcelist records who is forcing what, for FORCEDBY/FORCEDBY_ARRAY —
	// upstream's do_force pushes only the player, never a program, since
	// @force is not called from inside one.
	s.forcelist = append(s.forcelist, c.who)
	defer func() { s.forcelist = s.forcelist[:len(s.forcelist)-1] }()

	s.force(c.w, c.d, victim, command)
}

// force runs a command as another object.
//
// The forced object needs a descriptor, because a command may ask which
// connection typed it. It borrows the forcer's when it has none of its own,
// which is what dbref_first_descr comes to for a puppet.
func (s *Server) force(w *world.World, callerD *session.Descriptor, victim ref.Ref, command string) {
	d := callerD
	if ds := s.hub.DescriptorsFor(victim); len(ds) > 0 {
		d = ds[0]
	}

	s.forceDepth++
	defer func() { s.forceDepth-- }()

	// The command goes to the parser, not through Input: a forced object
	// must not be able to answer a READ or type into an editor session
	// belonging to whoever holds the descriptor.
	s.commandAs(w, d, victim, strings.TrimSpace(command))
}

// ownerOf returns the object a player's possessions belong to, which for a
// player is themselves.
func ownerOf(w *world.World, r ref.Ref) ref.Ref {
	o := w.Get(r)
	if o == nil {
		return ref.Nothing
	}
	if o.Type() == ref.TypePlayer {
		return r
	}
	return o.Owner
}

// ownerIsWizard reports whether whoever owns an object has wizard powers,
// which is the test upstream's Wizard(OWNER(player)) makes.
func ownerIsWizard(w *world.World, r ref.Ref) bool {
	o := w.Get(ownerOf(w, r))
	return o != nil && o.Flags.IsWizard()
}

// propValue is where an object's currency is kept, from include/db.h. A toad
// is left worth a single penny, as upstream leaves it.
const propValue = "@/value"

// cmdPcreate makes a player without them having to connect, which is how a
// registration-only world hands out characters.
func (s *Server) cmdPcreate(c *ctx) {
	if !s.requireWizard(c) {
		return
	}
	name, pass, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	if err := validPlayerName(c.w, name); err != nil {
		c.tell("You cannot use that name for a player.")
		return
	}
	if !okPassword(pass) {
		c.tell("You cannot use that password.")
		return
	}
	o, err := s.createPlayer(c.w, name, pass)
	if err != nil {
		c.send(err.Error())
		return
	}
	s.statusLog().Info("created player",
		"player", o.Ref.String(), "name", name,
		"by", c.who.String(), "byName", nameOf(c.w, c.who))
	c.tell("Player %s created as object #%d.", name, int32(o.Ref))
}

// okPassword applies upstream's rule: not empty, and no spaces or
// unprintable characters, because a password is read from a line of input.
func okPassword(pass string) bool {
	if pass == "" {
		return false
	}
	for i := 0; i < len(pass); i++ {
		if pass[i] <= ' ' || pass[i] == 0x7f {
			return false
		}
	}
	return true
}
