package game

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/tune"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// Version is stamped by the server binary.
var Version = "dev"

// cmdQuit disconnects.
func (s *Server) cmdQuit(c *ctx) {
	c.tell("Goodbye.")
	c.d.Close()
}

// cmdWho lists who is online.
func (s *Server) cmdWho(c *ctx) {
	s.writeWho(c.w, c.d, c.arg)
}

// writeWho renders the WHO table, optionally filtered by a name
// prefix.
func (s *Server) writeWho(w *world.World, d *session.Descriptor, filter string) {
	filter = strings.TrimSpace(filter)
	now := w.Now()

	d.Send(fmt.Sprintf("%-20s %8s %8s  %s", "Player name", "On for", "Idle", "Doing"))

	shown := 0
	for _, other := range s.hub.Connected() {
		o := w.Get(other.Player)
		if o == nil {
			continue
		}
		if filter != "" &&
			!match.StringMatch(o.Name, filter) {
			continue
		}
		d.Send(fmt.Sprintf("%-20s %8s %8s  %s",
			o.Name,
			idleFor(now.Sub(other.ConnectedAt)),
			idleFor(other.IdleSince(now)),
			getMesg(w, other.Player, "_/do"),
		))
		shown++
	}
	d.Send(fmt.Sprintf("%d player%s connected.", shown, plural(shown)))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// cmdVersion reports the server version.
func (s *Server) cmdVersion(c *ctx) {
	c.tell("Fuzzball Emerald %s", Version)
}

// cmdDump forces the world to be written now.
//
// In Fuzzball this froze the game for the length of a full database
// write. Here it only asks the persister to flush what is already
// pending, so it returns immediately and nothing pauses.
func (s *Server) cmdDump(c *ctx) {
	if !s.requireWizard(c) {
		return
	}
	c.tell("Flushing pending changes...")
	who := c.who
	// The flush waits on the persister, so it cannot run on the
	// world goroutine; hand it off and report back through the
	// hub.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		err := s.engine.Flush(ctx)
		_ = s.engine.Go(func(w *world.World) {
			if err != nil {
				s.notify(w, who, "The flush failed: %v", err)
				return
			}
			s.send(w, who, "Done.")
		})
	}()
}

// cmdShutdown stops the server.
func (s *Server) cmdShutdown(c *ctx) {
	if !s.requireWizard(c) {
		return
	}
	name := c.w.Get(c.who).Name
	s.securityLog().Warn("shutdown requested",
		"player", c.who.String(), "name", name)

	s.tellEveryoneShutdown()
	if s.shutdown != nil {
		s.shutdown()
	}
}

// AnnounceShutdown tells everyone still connected that the server is
// going away. It is what a signal-driven shutdown needs and @shutdown
// gets for free: cancelling the world's context drains and flushes,
// but says nothing to anyone, and by the time the drain is over there
// is no way left to send.
//
// The transports call this from their own goroutine, so it goes
// through the engine like any other outside caller. A failure to
// enqueue means the world has already stopped, which is exactly the
// case where there is nothing left to say.
func (s *Server) AnnounceShutdown() {
	_ = s.engine.Do(context.Background(), func(*world.World) {
		s.tellEveryoneShutdown()
	})
}

func (s *Server) tellEveryoneShutdown() {
	for _, d := range s.hub.All() {
		d.Send("## The server is shutting down. ##")
	}
}

// cmdTune reads and writes @tune parameters.
func (s *Server) cmdTune(c *ctx) {
	arg := strings.TrimSpace(c.arg)

	if arg == "" {
		c.tell("Usage: @tune <parameter>  or  @tune <parameter>=<value>")
		c.tell("       @tune #list [pattern]")
		return
	}

	if strings.HasPrefix(arg, "#list") {
		s.tuneList(c, strings.TrimSpace(strings.TrimPrefix(arg, "#list")))
		return
	}

	name, value, assigning := strings.Cut(arg, "=")
	name = strings.TrimSpace(name)

	p, found := tune.Lookup(name)
	if !found {
		if env, dropped := tune.DroppedReplacement(name); dropped {
			if env != "" {
				c.tell("%s is not a server parameter here; it is set with the %s environment variable.", name, env)
			} else {
				c.tell("%s no longer applies: every connection is already encrypted.", name)
			}
			return
		}
		c.tell("I don't know that parameter.")
		return
	}

	mlev := c.w.Get(c.who).Flags.MLevel()
	if mlev < p.ReadMLev {
		c.tell("Permission denied.")
		return
	}

	if !assigning {
		v, _ := c.w.Tune.Get(p.Name)
		c.tell("%s: %s", p.Name, p.Format(v))
		if p.Inert != "" {
			c.tell("  (has no effect: %s)", p.Inert)
		}
		return
	}

	if mlev < p.WriteMLev || !c.w.Get(c.who).Flags.IsWizard() {
		c.tell("Permission denied.")
		return
	}
	if err := c.w.SetTune(p.Name, strings.TrimSpace(value)); err != nil {
		c.tell("%s", err.Error())
		return
	}
	v, _ := c.w.Tune.Get(p.Name)
	c.tell("%s set to %s.", p.Name, p.Format(v))
	s.statusLog().Info("tune parameter changed",
		"player", c.who.String(), "parameter", p.Name, "value", p.Format(v))
}

// tuneList shows the parameters a player may read.
func (s *Server) tuneList(c *ctx, pattern string) {
	mlev := c.w.Get(c.who).Flags.MLevel()
	shown := 0
	for _, p := range tune.Params() {
		if mlev < p.ReadMLev {
			continue
		}
		if pattern != "" &&
			!match.StringMatch(p.Name, pattern) {
			continue
		}
		v, _ := c.w.Tune.Get(p.Name)
		c.tell("%-32s %s", p.Name, p.Format(v))
		shown++
	}
	c.tell("%d parameter%s.", shown, plural(shown))
}

// boot disconnects every descriptor for a player.
func (s *Server) boot(player ref.Ref, why string) {
	for _, d := range s.hub.DescriptorsFor(player) {
		if why != "" {
			d.Send(why)
		}
		d.Close()
	}
}

// Process control.

func init() {
	register("@ps", (*Server).cmdPs)
	register("@kill", (*Server).cmdKill)
}

// cmdPs lists the running and suspended programs.
func (s *Server) cmdPs(c *ctx) {
	procs := s.procs.all()
	wizard := c.w.Get(c.who).Flags.IsWizard()

	c.tell("%5s %-16s %-6s %-20s %s", "PID", "Player", "State", "Program", "Command")
	shown := 0
	for _, p := range procs {
		// A player sees their own processes; a wizard sees
		// them all.
		if !wizard && p.player != c.who {
			continue
		}
		c.tell("%5d %-16s %-6s %-20s %s",
			p.pid, nameOf(c.w, p.player), p.state,
			unparse(c.w, c.who, p.program), p.command)
		shown++
	}
	c.tell("%d process%s.", shown, pluralES(shown))
}

func pluralES(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

// cmdKill stops a process.
func (s *Server) cmdKill(c *ctx) {
	pid, err := strconv.Atoi(strings.TrimSpace(c.arg))
	if err != nil {
		c.tell("Usage: @kill <process id>")
		return
	}
	p := s.procs.get(pid)
	if p == nil {
		c.tell("There is no process %d.", pid)
		return
	}
	// A player may stop their own; a wizard may stop anyone's.
	if p.player != c.who && !c.w.Get(c.who).Flags.IsWizard() {
		c.tell("Permission denied.")
		return
	}
	s.finishProcess(c.w, p)
	if p.player != c.who {
		s.send(c.w, p.player, "Your program was stopped.")
	}
	c.tell("Process %d killed.", pid)
}
