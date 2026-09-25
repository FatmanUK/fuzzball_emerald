// Package game joins connections to the world: the login flow,
// command dispatch, and the commands themselves.
//
// Everything here runs on the world goroutine. Transports call the
// On* methods from their own goroutines; those only enqueue work.
package game

import (
	"log/slog"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/logging"
	"github.com/FatmanUK/fuzzball_emerald/internal/mcp"
	"github.com/FatmanUK/fuzzball_emerald/internal/mpi"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// maxInputLen bounds a single line of input, matching Fuzzball's
// MAX_COMMAND_LEN. Anything longer is truncated rather than
// allocated.
const maxInputLen = 2048

// Server owns the connection hub and dispatches commands.
type Server struct {
	engine *world.Engine
	hub    *session.Hub
	log    *slog.Logger

	// shutdown asks the process to stop, set by the server
	// binary.
	shutdown func()

	// started is when the server came up, for uptime.
	started time.Time

	// programs caches compiled MUF.
	programs map[ref.Ref]compiled

	// editors holds one open MUF editor session per player. The
	// program text being edited lives here rather than on the
	// object, so an abandoned session cannot corrupt what is
	// stored.
	editors map[ref.Ref]*editSession
	// editLine remembers each program's current line between
	// sessions, as upstream keeps it on the program itself. It is
	// not persisted.
	editLine map[ref.Ref]int

	// mpiEvents holds what MPI's {delay} has scheduled, fired by
	// the tick.
	mpiEvents []mpiEvent

	// lastQuotaRefill is where the spam limiter's clock stands,
	// advanced a whole time slice at a time.
	lastQuotaRefill time.Time

	// forceDepth counts how deep @force is nested, so a command
	// that forces something that forces back cannot recurse
	// without end.
	forceDepth int
	// forcelist is upstream's own global objnode stack: who is
	// forcing what, most recently pushed last, read by
	// FORCEDBY/FORCEDBY_ARRAY. Both @force (cmdForce) and the
	// FORCE primitive push onto and pop from it around their own
	// call to force/commandAs.
	forcelist []ref.Ref

	// procs holds suspended programs: those sleeping, waiting for
	// input, or waiting for an event.
	procs *procQueue

	// dialogs holds the MCP-GUI dialogs open on every connection,
	// and dialogOwner the frame that opened each one.
	dialogs     *mcp.Dialogs
	dialogOwner map[string]*muf.Frame

	// mcpPackages is what a new connection is offered, and
	// mcpBindings maps a message to the program procedure that
	// claimed it.
	mcpPackages []mcp.Package
	mcpBindings map[mcpBinding]mcpTarget
}

// OnShutdown sets what @shutdown calls.
func (s *Server) OnShutdown(fn func()) { s.shutdown = fn }

// Options configure a Server.
type Options struct {
	Logger *slog.Logger
}

// New returns a Server driving engine.
func New(engine *world.Engine, opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	s := &Server{
		engine:   engine,
		hub:      session.NewHub(),
		log:      opts.Logger,
		started:  time.Now(),
		programs: map[ref.Ref]compiled{},
		procs:    newProcQueue(),
		editors:  map[ref.Ref]*editSession{},

		dialogs:     mcp.NewDialogs(),
		dialogOwner: map[string]*muf.Frame{},
		mcpBindings: map[mcpBinding]mcpTarget{},
		editLine:    map[ref.Ref]int{},
	}
	s.installMCPHandlers()
	return s
}

func defaultWelcome() []string {
	return []string{
		"",
		"  Fuzzball Emerald",
		"",
		"  connect <name> <password>   to enter",
		"  create <name> <password>    to make a new character",
		"  QUIT                        to disconnect",
		"",
	}
}

// Connect registers a new connection and shows the welcome banner. It
// blocks until the descriptor exists, because the transport needs it
// to start pumping output.
func (s *Server) Connect(tr session.Transport, host string) (*session.Descriptor, error) {
	var d *session.Descriptor
	done := make(chan struct{})
	err := s.engine.Go(func(w *world.World) {
		d = s.hub.Add(tr, host, w.Now())
		d.Quota.Set(int(w.Tune.Int("command_burst_size")))
		close(done)

		// MCP is offered before the banner, so a client that
		// speaks it has answered by the time anything else
		// arrives. A client that does not simply sees a line
		// it ignores.
		d.MCP.StartNegotiation()

		for _, line := range s.welcomeLines(w, d) {
			d.Send(line)
		}
		// Someone arriving at a full server is told so now
		// rather than after they have typed a password, which
		// is upstream's own welcome_user behaviour.
		if s.serverFull(w) {
			if msg := w.Tune.String("playermax_warnmesg"); msg != "" {
				d.Send(msg)
			}
		}
	})
	if err != nil {
		return nil, err
	}
	<-done
	return d, nil
}

// Input handles one line from a client.
//
// This runs on the transport's own goroutine, which is where the spam
// limiter lives: a connection that has spent its allowance waits here
// for the next one rather than queueing work the world would have to
// throttle later. Nothing is dropped, and only that one connection is
// held up.
func (s *Server) Input(d *session.Descriptor, line string) {
	if len(line) > maxInputLen {
		line = line[:maxInputLen]
	}
	// An out-of-band message is a client talking to the server,
	// not a player typing, so it costs nothing — upstream
	// excludes it too.
	if !strings.HasPrefix(line, mcp.Prefix) {
		if !d.Quota.Take(d.Done()) {
			return
		}
	}
	_ = s.engine.Go(func(w *world.World) {
		d.LastActive = w.Now()

		// Out-of-band messages are taken off the line before
		// anything else sees it. A line the client quoted
		// comes back as the text it was quoting.
		line, ok := d.MCP.ProcessInput(line)
		if !ok {
			return
		}

		if d.Connected {
			// The order here is do_command's. Interface
			// commands are answered first, then a program
			// waiting on a READ takes the line, then the
			// editor, and only then does the command
			// parser see it.
			//
			// The editor and a READ both take the line
			// untrimmed: leading spaces are part of
			// program text, and an empty line is a blank
			// line to insert.
			switch {
			case s.interfaceCommand(w, d, line):
			case s.readInput(w, d.ID, line):
			case s.editing(d.Player) != nil:
				s.editInput(w, d, line)
			default:
				s.command(w, d, line)
			}
			return
		}
		s.login(w, d, line)
	})
}

// Disconnect tears a connection down.
func (s *Server) Disconnect(d *session.Descriptor) {
	_ = s.engine.Go(func(w *world.World) {
		// A dialog cannot be closed from a connection that
		// has gone, so anything still open on it is forgotten
		// here.
		s.closeDialogsFor(d.ID)
		if d.Connected {
			s.announceDisconnect(w, d)
			s.log.Info("disconnected",
				"descriptor", d.ID,
				"player", d.Player.String(),
				"name", nameOf(w, d.Player),
				"host", d.Hostname,
			)
		}
		s.hub.Remove(d)
	})
}

// Resize records a client's reported terminal size.
func (s *Server) Resize(d *session.Descriptor, ws session.WindowSize) {
	_ = s.engine.Go(func(*world.World) {
		if ws.Width > 0 {
			d.Width = ws.Width
		}
		if ws.Height > 0 {
			d.Height = ws.Height
		}
	})
}

// Tick is called on the world goroutine at each flush interval. It
// runs whatever the process queue has due, which is how a sleeping
// program wakes.
func (s *Server) OnTick() func(*world.World) {
	return func(w *world.World) { s.Tick(w) }
}

// Hub exposes the connection hub. Only the world goroutine may use
// it.
func (s *Server) Hub() *session.Hub { return s.hub }

// notify sends a formatted line to every descriptor a player is
// connected on.
func (s *Server) notify(w *world.World, player ref.Ref, format string, args ...any) {
	s.send(w, player, sprintf(format, args...))
}

// send delivers a line verbatim. Use it for text that came from the
// world, such as a description or a player's own words, where a stray
// '%' must not be read as a format verb.
//
// A puppet's output is forwarded to whoever owns it, prefixed,
// because a THING has no connection of its own. That is how anything
// a puppet is told — by a program, or by @force — reaches a
// person at all.
func (s *Server) send(w *world.World, player ref.Ref, text string) {
	s.hub.Tell(player, text)
	if owner, prefix, ok := puppetRelay(s, w, player); ok {
		s.hub.Tell(owner, prefix+text)
	}
}

// puppetRelay reports whether a target's output should also reach its
// owner, and with what prefix.
//
// The conditions are upstream's, and each excludes a way of using a
// puppet to spy: a DARK puppet is silent unless a wizard owns it, a
// room flagged ZOMBIE is a no-puppet zone, and an owner who is
// themselves flagged ZOMBIE has opted out of hearing any of it.
func puppetRelay(s *Server, w *world.World, target ref.Ref) (ref.Ref, string, bool) {
	if !w.Tune.Bool("allow_zombies") {
		return ref.Nothing, "", false
	}
	o := w.Get(target)
	if o == nil || o.Type() != ref.TypeThing ||
		o.Flags&ref.Zombie == 0 {
		return ref.Nothing, "", false
	}
	owner := w.Get(o.Owner)
	if owner == nil || owner.Flags&ref.Zombie != 0 {
		return ref.Nothing, "", false
	}
	wizardOwned := owner.Flags.IsWizard()
	if o.Flags&ref.Dark != 0 && !wizardOwned {
		return ref.Nothing, "", false
	}
	if room := w.Get(o.Location); !wizardOwned && room != nil &&
		room.Type() == ref.TypeRoom && room.Flags&ref.Zombie != 0 {
		return ref.Nothing, "", false
	}

	// Everything sent this way is a private message — room
	// speech reaches people through notifyRoom instead — so
	// upstream's "unless the owner is standing right here" test
	// is always satisfied.
	prefix := o.Name + "> "
	if v, ok := w.GetProp(target, propPuppetEcho); ok &&
		v.Type == props.String {
		// Upstream evaluates this one with no descriptor at
		// all — do_parse_prop(-1, ...) at interface.c:4722
		// — because the text is being relayed rather than
		// triggered by anyone in particular.
		got := s.evalMPI(w, -1, target, target, v.Str,
			v.Blessed, mpi.Private)
		if got != "" {
			prefix = got + " "
		}
	}
	return o.Owner, prefix, true
}

// propPuppetEcho overrides the prefix a puppet's output reaches its
// owner with, from include/db.h.
const propPuppetEcho = "_/pecho"

// notifyRoom sends a line to everyone in a room, optionally skipping
// some.
//
// The container itself hears it too when it is a player or a thing,
// which is how someone carrying a puppet hears what the puppet says:
// upstream's notify_except notifies the object the contents belong to
// before walking them. Rooms are skipped, because a room is not an
// audience.
//
// Listener objects and the propqueues that drive them are not
// implemented.
func (s *Server) notifyRoom(w *world.World, room ref.Ref, except []ref.Ref, format string, args ...any) {
	text := sprintf(format, args...)

	tell := func(r ref.Ref) {
		o := w.Get(r)
		if o == nil || containsRef(except, r) {
			return
		}
		switch o.Type() {
		case ref.TypePlayer:
			s.hub.Tell(r, text)
		case ref.TypeThing:
			// A thing hears nothing itself, but a puppet
			// relays what it hears to whoever owns it.
			if owner, prefix, ok := puppetRelay(s, w, r); ok {
				s.hub.Tell(owner, prefix+text)
			}
		}
	}

	tell(room)
	for _, r := range w.Contents(room) {
		tell(r)
	}
}

func containsRef(list []ref.Ref, r ref.Ref) bool {
	for _, x := range list {
		if x == r {
			return true
		}
	}
	return false
}

// nameOf returns an object's name, or a placeholder when it is gone.
func nameOf(w *world.World, r ref.Ref) string {
	if o := w.Get(r); o != nil {
		return o.Name
	}
	return "<nothing>"
}

// unparse renders an object the way @examine and wizard output do:
// the name, followed by its dbref when the viewer may see it.
//
// The virtual refs render as their names rather than as numbers,
// because they are what a link or a location field says when it
// points at nothing real, and a report that says "#-1" tells the
// reader less than "*NOTHING*" does.
//
// A viewer of ref.Nothing is the sanity checker rather than a person,
// and sees everything: there is nobody to keep a secret from.
func unparse(w *world.World, viewer, target ref.Ref) string {
	switch target {
	case ref.Nothing:
		return "*NOTHING*"
	case ref.Ambiguous:
		return "*AMBIGUOUS*"
	case ref.Home:
		return "*HOME*"
	case ref.Nil:
		return "*NIL*"
	}
	o := w.Get(target)
	if o == nil {
		return "*INVALID*"
	}
	v := w.Get(viewer)
	if viewer == ref.Nothing ||
		v != nil && (v.Flags.IsWizard() || o.Owner == viewer || target == viewer) {
		return o.Name + "(" + target.String() + o.Flags.Unparse() + ")"
	}
	return o.Name
}

// statusLog returns the logger for server-lifecycle messages.
func (s *Server) statusLog() *slog.Logger {
	return logging.On(s.log, logging.Status)
}

// securityLog returns the logger for the audit trail: who tried to
// authenticate, whose password changed, and who ran a privileged
// command.
func (s *Server) securityLog() *slog.Logger {
	return logging.On(s.log, logging.Security)
}

// mufLog returns the logger for MUF diagnostics.
func (s *Server) mufLog() *slog.Logger {
	return logging.On(s.log, logging.MUFError)
}

// commandLog returns the logger for player commands.
func (s *Server) commandLog() *slog.Logger {
	return logging.On(s.log, logging.Command)
}

// trimCommand splits a line into a verb and its argument.
func trimCommand(line string) (verb, arg string) {
	line = strings.TrimSpace(line)
	verb, arg, _ = strings.Cut(line, " ")
	return verb, strings.TrimSpace(arg)
}

// idleFor renders a duration the way WHO does.
func idleFor(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "0m"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return itoa(int(d.Hours())) + "h"
	default:
		return itoa(int(d.Hours()/24)) + "d"
	}
}
