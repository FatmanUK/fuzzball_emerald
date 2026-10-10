package game

import (
	"context"

	"strconv"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/tune"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// Version is stamped by the server binary.
var Version = "dev"

// cmdQuit disconnects. `goodbye_user` (`interface.c:2232`) prints
// `leave_mesg`, not a fixed "Goodbye." — surrounded by blank lines,
// and flushed before the socket goes.
//
// It fires on `booted == 2`, which only `do_command` returning false
// sets (`interface.c:2014`) — that is, QUIT and nothing else. An
// idle boot is `booted == 1` and gets `idle_boot_mesg` instead, and a
// `@boot` gets neither.
func (s *Server) cmdQuit(c *ctx) {
	c.d.Send("")
	c.d.Send(c.w.Tune.String("leave_mesg"))
	c.d.Send("")
	c.d.Close()
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

// cmdTune is tune.c:669's do_tune, which is three commands and not
// the two Emerald had.
//
// **A bare `@tune` is the listing**, and `@tune <pattern>` narrows it
// with `equalstr` — so wildcards work and nothing else does, which
// is why `@tune penny` shows one parameter and `@tune penn` shows
// none. Emerald had its own `#list` subcommand for this and answered
// a bare `@tune` with a usage message; upstream has neither, and a
// program reading a listing back could not have found it.
//
// **A set is recognised by the line containing an `=`**, not by the
// value being non-empty — upstream tests `match_args`, the whole
// typed line — so `@tune muckname=` really is a set, and fails as a
// bad value rather than reading the parameter back. A name prefixed
// with `%` is the other set: reset to the compiled-in default.
//
// **`@tune info` is the third**, adding the group and the one-line
// label under each parameter. Its argument is space-separated rather
// than `=`-separated, because it shares `arg1` with the pattern.
//
// Each reply is upstream's, and there are more of them than Emerald
// had: "Parameter set." or "Parameter reset to default." followed by
// the parameter as a listing would show it, or one of "Unknown
// parameter.", "Bad parameter syntax.", "Bad parameter value." and
// "Permission denied." — and a listing always ends "*done*", which
// is how a program knows it has the lot.
func (s *Server) cmdTune(c *ctx) {
	name, value, hasEq := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	// arg2 is left-trimmed only upstream, so a value may end in a
	// space. It cannot begin with one.
	value = strings.TrimLeft(value, " \t")

	// TUNE_MLEV (tune.c:674), not MLevel: God gets 255, which is
	// what reserves the ten MLEV_GOD-read parameters to them.
	mlev := (&mufHost{s: s, w: c.w}).TuneMLevel(c.who)

	resetting := strings.HasPrefix(name, string(tune.ResetFlag))
	switch {
	case name != "" && (hasEq || resetting):
		s.tuneSet(c, name, value, mlev)
	case name != "" && ascii.HasPrefix(name, tuneInfoCmd):
		s.tuneInfo(c, name, mlev)
	default:
		s.tuneDisplay(c, name, mlev, false)
	}
}

// tuneInfoCmd is upstream's TP_INFO_CMD.
const tuneInfoCmd = "info"

// tuneInfo handles "@tune info [pattern]", whose argument is split on
// a space because it arrives in the same string as the command word.
//
// The usage message is unreachable from a typed line — arg1 has its
// trailing whitespace removed before do_tune sees it, so "info " with
// nothing after it cannot happen — and is reproduced anyway.
func (s *Server) tuneInfo(c *ctx, name string, mlev int) {
	rest, hasSpace := cutAfterSpace(name)
	if !hasSpace {
		s.tuneDisplay(c, "", mlev, true)
		return
	}
	if rest = strings.TrimLeft(rest, " \t"); rest == "" {
		c.tell("Usage is @tune %s [optional: <parameter>]",
			tuneInfoCmd)
		return
	}
	s.tuneDisplay(c, rest, mlev, true)
}

// cutAfterSpace is strchr(s, ' ') followed by a step past it.
func cutAfterSpace(s string) (string, bool) {
	i := strings.IndexByte(s, ' ')
	if i < 0 {
		return "", false
	}
	return s[i+1:], true
}

// tuneSet is do_tune's setting half, reporting tune_setparm's answer
// and then showing what the parameter now holds.
func (s *Server) tuneSet(c *ctx, name, value string, mlev int) {
	// The one permission check do_tune makes for itself, and it
	// is not about who is asking: a @tune inside a @force would
	// let a program change the server's configuration through
	// somebody else's hands.
	if s.forceDepth > 0 {
		c.tell("You cannot force setting a @tune.")
		return
	}

	bare := strings.TrimPrefix(name, string(tune.ResetFlag))
	old := ""
	if p, ok := tune.Lookup(bare); ok {
		v, _ := c.w.Tune.Get(p.Name)
		old = p.Format(v)
	}

	switch c.w.SetParm(name, value, mlev, s.tuneRefResolver(c)) {
	case tune.SetSuccess:
		c.tell("Parameter set.")
		s.logTuned(c, bare, old)
		s.tuneDisplay(c, bare, mlev, false)
	case tune.SetSuccessDefault:
		c.tell("Parameter reset to default.")
		s.logTuned(c, bare, old)
		s.tuneDisplay(c, bare, mlev, false)
	case tune.SetUnknown:
		// Emerald's one addition: the parameters it dropped
		// on purpose say where their setting went, rather
		// than reading as a typo.
		env, dropped := tune.DroppedReplacement(bare)
		if dropped {
			s.tellDropped(c, bare, env)
			return
		}
		c.tell("Unknown parameter.")
	case tune.SetSyntax:
		c.tell("Bad parameter syntax.")
	case tune.SetBadVal:
		c.tell("Bad parameter value.")
	case tune.SetDenied:
		c.tell("Permission denied.")
	}
}

// tellDropped explains a Fuzzball parameter this server does not
// have, which is always a consequence of being TLS-only.
func (s *Server) tellDropped(c *ctx, name, env string) {
	if env != "" {
		c.tell("%s is not a server parameter here; "+
			"it is set with the %s environment variable.",
			name, env)
		return
	}
	c.tell("%s no longer applies: "+
		"every connection is already encrypted.", name)
}

func (s *Server) logTuned(c *ctx, name, old string) {
	p, ok := tune.Lookup(name)
	if !ok {
		return
	}
	v, _ := c.w.Tune.Get(p.Name)
	s.statusLog().Info("tune parameter changed",
		"player", c.who.String(), "parameter", p.Name,
		"from", old, "to", p.Format(v))
}

// tuneRefResolver is the match list tune_setparm uses for a dbref
// parameter: absolute, registered, player, me, here — and nothing
// nearby, so a room cannot be named by standing in it.
func (s *Server) tuneRefResolver(c *ctx) func(string) (ref.Ref,
	ref.ObjType, bool) {

	return s.tuneRefResolverFor(c.w, c.who)
}

// tuneRefResolverFor is the same without a ctx, for the MCP
// simpleedit handler — which sets a parameter on behalf of a
// connection rather than from a typed command.
func (s *Server) tuneRefResolverFor(w *world.World,
	who ref.Ref) func(string) (ref.Ref, ref.ObjType, bool) {

	return func(name string) (ref.Ref, ref.ObjType, bool) {
		r := match.New(w, who, name).
			Absolute().Registered().Player().Me().Here().
			Result()
		o := w.Get(r)
		if o == nil {
			return ref.Nothing, 0, false
		}
		return r, o.Type(), true
	}
}

// tuneDisplay is tune_display_parms (tune.c:144): every parameter the
// asker may read whose name the pattern matches, in the table's own
// order, each as a type tag, a name, a value and up to two markers.
//
// "[default]" means nobody has changed it, and is what decides the
// leading '%' when the table is written out. "[inactive]" is
// upstream's word for a parameter whose feature was compiled out,
// which here means a module this server does not have — see
// tune.Param.Active, and note that Emerald's *inert* parameters are
// not marked, because upstream does not mark its equivalents either.
//
// The ending matters: "No matching parameters." when nothing matched,
// and "*done*" always.
func (s *Server) tuneDisplay(c *ctx, pattern string, mlev int,
	extended bool) {

	// A pattern keeps its reset flag when it arrives from a set,
	// and is treated as an ordinary name.
	pattern = strings.TrimPrefix(pattern, string(tune.ResetFlag))

	found := false
	for _, p := range tune.Params() {
		if p.ReadMLev > mlev {
			continue
		}
		if pattern != "" && !ascii.SMatch(p.Name, pattern) {
			continue
		}
		inactive := ""
		if !p.Active() {
			inactive = " [inactive]"
		}
		dflt := ""
		if c.w.Tune.IsDefault(p.Name) {
			dflt = " [default]"
		}
		c.send(sprintf("%-6s %-20s = %s%s%s", p.Type.Tag(),
			p.Name, s.tuneValue(c, &p), inactive, dflt))
		if extended {
			c.send(sprintf("%-27s %s", p.Group, p.Label))
		}
		found = true
	}
	if !found {
		c.tell("No matching parameters.")
	}
	c.tell("*done*")
}

// tuneValue renders a parameter for display, which is not quite how
// it is stored: a dbref is unparsed for the reader, so it shows a
// name and the flags that reader may see, where a dump writes "#0".
func (s *Server) tuneValue(c *ctx, p *tune.Param) string {
	v, _ := c.w.Tune.Get(p.Name)
	if p.Type == tune.TypeDbref {
		return s.unparse(c.w, c.who, v.Ref)
	}
	return p.Format(v)
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
			s.unparse(c.w, c.who, p.program), p.command)
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
