package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/timefmt"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

func init() {
	register("score", (*Server).cmdScore)
	register("uptime", (*Server).cmdUptime)
	register("@trace", (*Server).cmdTrace)
	register("@uncompile", (*Server).cmdUncompile)
	register("@wall", (*Server).cmdWall)
	register("gripe", (*Server).cmdGripe)
	register("@restrict", (*Server).cmdRestrict)
}

// cmdScore is do_score (pennies.c:112): how much currency the player
// has, in whatever the world calls it.
//
// The singular and the plural are separate @tune parameters, so a
// world that renames its money renames both.
func (s *Server) cmdScore(c *ctx) {
	n := valueOf(c.w, c.who)
	unit := c.w.Tune.String("pennies")
	if n == 1 {
		unit = c.w.Tune.String("penny")
	}
	c.tell("You have %d %s.", n, unit)
}

// cmdUptime is do_uptime (look.c:994).
//
// Upstream reads the start time back out of a property on #0,
// _sys/startuptime, which it writes at boot and which MUF can read
// too. Emerald has the time on the server and does not write that
// property — so this agrees, and a program asking #0 does not.
// Worth closing, along with the other three _sys values upstream sets
// beside it.
func (s *Server) cmdUptime(c *ctx) {
	up := int(c.w.Now().Sub(s.started).Seconds())
	c.tell("Up %s since %s", timefmt.Long(up),
		timefmt.Format("%c %Z", s.started))
}

// cmdTrace is do_trace (look.c:1660): the environment chain from an
// object outwards, one line each, ending with the same marker @find
// and @owned use.
//
// A depth of zero means no limit, which is also what a missing or
// unreadable second argument gives — it is atoi, so "@trace here=x"
// walks the whole chain.
func (s *Server) cmdTrace(c *ctx) {
	name, depthArg, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	depth := leadingInt(strings.TrimSpace(depthArg))

	thing := c.w.Get(c.who).Location
	if name != "" && !ascEqual(name, "here") {
		r := match.New(c.w, c.who, name).Everything().Result()
		if !noisyMatch(c, name, r) {
			return
		}
		thing = r
	}

	for i := 0; (depth == 0 || i < depth) &&
		thing != ref.Nothing; i++ {
		c.send(unparse(c.w, c.who, thing))
		o := c.w.Get(thing)
		if o == nil {
			break
		}
		thing = o.Location
	}
	c.tell("***End of List***")
}

// cmdUncompile is do_uncompile (compile.c:899): drop every compiled
// program from memory.
//
// Upstream frees the compiled form and leaves the source; Emerald's
// compiled forms are a cache keyed by ref, so emptying it is the same
// thing. Each program is recompiled the next time it runs.
func (s *Server) cmdUncompile(c *ctx) {
	s.programs = map[ref.Ref]compiled{}
	c.tell("All programs decompiled.")
}

// cmdWall is do_wall (speech.c:117): shout to everyone connected.
//
// It reads like say and is not say: upstream sends the same line to
// every logged-in descriptor rather than to a room, and does no
// permission checking of its own — WIZARDONLY and PLAYERONLY are
// applied at the dispatch site, which for Emerald means the perm
// flags on commandTable.
//
// One thing is deliberately not reproduced. Upstream loops over
// descriptors and calls notify_listeners, which itself writes to
// every descriptor that player has — so somebody connected twice
// receives the shout four times. This writes to each descriptor once.
func (s *Server) cmdWall(c *ctx) {
	msg := sprintf("%s shouts, \"%s\"", nameOf(c.w, c.who), c.arg)
	for _, d := range s.hub.Connected() {
		d.Send(msg)
	}
	s.securityLog().Warn("wall",
		"player", c.who.String(), "name", nameOf(c.w, c.who),
		"message", c.arg)
}

// cmdGripe is do_gripe (speech.c:147): file a complaint.
//
// With no message a wizard is shown what has been filed and anybody
// else is told how to file one. With a message it is recorded, the
// complainer is thanked, and every connected wizard is told at once
// — which is the part that makes gripe useful rather than a
// suggestion box nobody opens.
//
// Where the complaints go is the decision this needed. Upstream
// appends to the file named by file_log_gripes; Emerald has no game
// directory, so they are rows in Postgres — the same answer the
// help texts got, loaded at boot and written behind like everything
// else. That keeps the reading in memory, since nothing in this
// server touches the database after boot, and it gives the
// configurator something to show. Only the most recent
// world.GripeLimit are held, because upstream's file grows without
// bound and a wizard on a world that has been up for years should not
// have it all thrown at them.
func (s *Server) cmdGripe(c *ctx) {
	msg := c.arg
	if msg == "" {
		if !isWizard(c.w, ownerOf(c.w, c.who)) {
			c.tell("If you wish to gripe, use " +
				"'gripe <message>'.")
			return
		}
		got := c.w.Gripes()
		if len(got) == 0 {
			c.tell("Nobody has griped.")
			return
		}
		for _, g := range got {
			c.send(g.GripeLine())
		}
		return
	}

	me := c.w.Get(c.who)
	loc := me.Location
	c.w.AddGripe(world.Gripe{
		Who:       c.who,
		WhoName:   me.Name,
		Where:     loc,
		WhereName: nameOf(c.w, loc),
		Message:   msg,
	})
	c.tell("Your complaint has been duly noted.")

	shout := sprintf("## GRIPE from %s: %s", me.Name, msg)
	for _, d := range s.hub.Connected() {
		if isWizard(c.w, ownerOf(c.w, d.Player)) {
			d.Send(shout)
		}
	}
}

// cmdRestrict is do_restrict (game.c:520): wizards-only login.
//
// "on" and "off" are compared case-sensitively upstream, so
// "@restrict ON" reports the current state rather than setting it.
// That reads like an oversight and is reproduced.
//
// The mode is a field on the server rather than a @tune parameter,
// because upstream's wizonly_mode is a runtime global: persisting it
// would make a maintenance window survive a restart, which is the
// opposite of what somebody turning it on wants. Upstream sets it
// from two more places Emerald does not have — a -wizonly command
// line flag, and a sanity violation found at boot.
func (s *Server) cmdRestrict(c *ctx) {
	switch strings.TrimSpace(c.arg) {
	case "on":
		s.wizOnly = true
		c.tell("Login access is now restricted to " +
			"wizards only.")
	case "off":
		s.wizOnly = false
		c.tell("Login access is now unrestricted.")
	default:
		state := "off"
		if s.wizOnly {
			state = "on"
		}
		c.tell("Restricted connection mode is currently %s.",
			state)
	}
}
