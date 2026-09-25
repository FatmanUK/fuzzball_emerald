package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/timefmt"
)

func init() {
	register("score", (*Server).cmdScore)
	register("uptime", (*Server).cmdUptime)
	register("@trace", (*Server).cmdTrace)
	register("@uncompile", (*Server).cmdUncompile)
	register("@wall", (*Server).cmdWall)
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
