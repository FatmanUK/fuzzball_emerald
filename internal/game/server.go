// Package game joins connections to the world: the login flow, command
// dispatch, and the commands themselves.
//
// Everything here runs on the world goroutine. Transports call the On* methods
// from their own goroutines; those only enqueue work.
package game

import (
	"log/slog"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/logging"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// maxInputLen bounds a single line of input, matching Fuzzball's
// MAX_COMMAND_LEN. Anything longer is truncated rather than allocated.
const maxInputLen = 2048

// Server owns the connection hub and dispatches commands.
type Server struct {
	engine *world.Engine
	hub    *session.Hub
	log    *slog.Logger

	// welcome is the banner shown before login.
	welcome []string

	// shutdown asks the process to stop, set by the server binary.
	shutdown func()

	// started is when the server came up, for uptime.
	started time.Time

	// programs caches compiled MUF, and macros is the editor's macro table.
	programs map[ref.Ref]compiled
	macros   map[string]string
}

// OnShutdown sets what @shutdown calls.
func (s *Server) OnShutdown(fn func()) { s.shutdown = fn }

// Options configure a Server.
type Options struct {
	Logger *slog.Logger
	// Welcome replaces the built-in banner.
	Welcome []string
}

// New returns a Server driving engine.
func New(engine *world.Engine, opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	welcome := opts.Welcome
	if len(welcome) == 0 {
		welcome = defaultWelcome()
	}
	return &Server{
		engine:   engine,
		hub:      session.NewHub(),
		log:      opts.Logger,
		welcome:  welcome,
		started:  time.Now(),
		programs: map[ref.Ref]compiled{},
		macros:   map[string]string{},
	}
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

// Connect registers a new connection and shows the welcome banner. It blocks
// until the descriptor exists, because the transport needs it to start
// pumping output.
func (s *Server) Connect(tr session.Transport, host string) (*session.Descriptor, error) {
	var d *session.Descriptor
	done := make(chan struct{})
	err := s.engine.Go(func(w *world.World) {
		d = s.hub.Add(tr, host, w.Now())
		close(done)
		for _, line := range s.welcome {
			d.Send(line)
		}
	})
	if err != nil {
		return nil, err
	}
	<-done
	return d, nil
}

// Input handles one line from a client.
func (s *Server) Input(d *session.Descriptor, line string) {
	if len(line) > maxInputLen {
		line = line[:maxInputLen]
	}
	_ = s.engine.Go(func(w *world.World) {
		d.LastActive = w.Now()
		if d.Connected {
			s.command(w, d, line)
			return
		}
		s.login(w, d, line)
	})
}

// Disconnect tears a connection down.
func (s *Server) Disconnect(d *session.Descriptor) {
	_ = s.engine.Go(func(w *world.World) {
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

// Hub exposes the connection hub. Only the world goroutine may use it.
func (s *Server) Hub() *session.Hub { return s.hub }

// notify sends a formatted line to every descriptor a player is connected on.
func (s *Server) notify(player ref.Ref, format string, args ...any) {
	s.hub.Tell(player, sprintf(format, args...))
}

// send delivers a line verbatim. Use it for text that came from the world,
// such as a description or a player's own words, where a stray '%' must not be
// read as a format verb.
func (s *Server) send(player ref.Ref, text string) {
	s.hub.Tell(player, text)
}

// notifyRoom sends a line to everyone in a room, optionally skipping some.
//
// Only players are notified for now; listener objects and the propqueues that
// drive them arrive with MUF in M4.
func (s *Server) notifyRoom(w *world.World, room ref.Ref, except []ref.Ref, format string, args ...any) {
	text := sprintf(format, args...)
	for _, r := range w.Contents(room) {
		o := w.Get(r)
		if o == nil || o.Type() != ref.TypePlayer {
			continue
		}
		if containsRef(except, r) {
			continue
		}
		s.hub.Tell(r, text)
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

// unparse renders an object the way @examine and wizard output do: the name,
// followed by its dbref when the viewer may see it.
func unparse(w *world.World, viewer, target ref.Ref) string {
	o := w.Get(target)
	if o == nil {
		return target.String()
	}
	v := w.Get(viewer)
	if v != nil && (v.Flags.IsWizard() || o.Owner == viewer || target == viewer) {
		return o.Name + "(" + target.String() + o.Flags.Unparse() + ")"
	}
	return o.Name
}

// statusLog returns the logger for server-lifecycle messages.
func (s *Server) statusLog() *slog.Logger { return logging.On(s.log, logging.Status) }

// mufLog returns the logger for MUF diagnostics.
func (s *Server) mufLog() *slog.Logger { return logging.On(s.log, logging.MUFError) }

// commandLog returns the logger for player commands.
func (s *Server) commandLog() *slog.Logger { return logging.On(s.log, logging.Command) }

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
