package game

import (
	"runtime/debug"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// ctx carries everything a command handler needs.
type ctx struct {
	w   *world.World
	d   *session.Descriptor
	who ref.Ref
	// verb is the command as typed, arg is the rest of the line.
	verb string
	arg  string
}

// tell sends a formatted line to the player who typed the command.
func (c *ctx) tell(format string, args ...any) { c.d.Send(sprintf(format, args...)) }

// send delivers a line verbatim, for text that came from the world and must
// not be read as a format string.
func (c *ctx) send(text string) { c.d.Send(text) }

// handler runs one command.
type handler func(s *Server, c *ctx)

// commands maps a verb to its handler. Names are matched case-insensitively
// and in full; Fuzzball's prefix matching for @-commands is applied separately,
// because a bare word must never be taken as a prefix of a command when it
// could be an exit.
var commands = map[string]handler{
	"look":      (*Server).cmdLook,
	"l":         (*Server).cmdLook,
	"say":       (*Server).cmdSay,
	"pose":      (*Server).cmdPose,
	"page":      (*Server).cmdPage,
	"whisper":   (*Server).cmdWhisper,
	"go":        (*Server).cmdGo,
	"move":      (*Server).cmdGo,
	"home":      (*Server).cmdHome,
	"inventory": (*Server).cmdInventory,
	"i":         (*Server).cmdInventory,
	"get":       (*Server).cmdGet,
	"take":      (*Server).cmdGet,
	"drop":      (*Server).cmdDrop,
	"examine":   (*Server).cmdExamine,
	"ex":        (*Server).cmdExamine,
}

// atCommands maps an @-command to its handler. These are matched by prefix,
// as Fuzzball does, so "@cr" reaches "@create".
var atCommands = map[string]handler{
	"@create":   (*Server).cmdCreate,
	"@dig":      (*Server).cmdDig,
	"@open":     (*Server).cmdOpen,
	"@action":   (*Server).cmdOpen,
	"@link":     (*Server).cmdLink,
	"@unlink":   (*Server).cmdUnlink,
	"@name":     (*Server).cmdName,
	"@describe": (*Server).cmdDescribe,
	"@set":      (*Server).cmdSet,
	"@password": (*Server).cmdPassword,
	"@find":     (*Server).cmdFind,
	"@teleport": (*Server).cmdTeleport,
	"@recycle":  (*Server).cmdRecycle,
	"@dump":     (*Server).cmdDump,
	"@shutdown": (*Server).cmdShutdown,
	"@tune":     (*Server).cmdTune,
	"@version":  (*Server).cmdVersion,
}

// command dispatches one line from a logged-in player.
//
// A handler that panics must not leave the player staring at nothing: the
// engine contains the panic, and this reports it so the failure is visible at
// both ends.
func (s *Server) command(w *world.World, d *session.Descriptor, line string) {
	defer func() {
		if r := recover(); r != nil {
			d.Send("Something went wrong running that command. It has been logged.")
			s.log.Error("panic handling a command",
				"descriptor", d.ID,
				"player", d.Player.String(),
				"line", line,
				"panic", r,
				"stack", string(debug.Stack()))
		}
	}()

	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	c := &ctx{w: w, d: d, who: d.Player}
	c.verb, c.arg = trimCommand(line)

	if w.Get(c.who) == nil {
		c.tell("Your character no longer exists.")
		d.Close()
		return
	}

	// Interface commands come first and are case-sensitive, as upstream's
	// is_interface_command has them. That is deliberate and load-bearing:
	// the starter world ships a lowercase "quit" exit whose only job is to
	// tell players the real command is in capitals, which only works
	// because lowercase "quit" falls through to exit matching.
	switch {
	case line == quitCommand:
		s.logCommand(w, d, line, "")
		s.cmdQuit(c)
		return
	case strings.HasPrefix(line, whoCommand):
		s.logCommand(w, d, whoCommand, c.arg)
		c.arg = strings.TrimSpace(line[len(whoCommand):])
		s.cmdWho(c)
		return
	case ascii.EqualFold(line, breakCommand), line == nullCommand:
		return
	}

	// A wizard may prefix a line with '!' to skip exit matching, which is
	// how you reach a built-in that a room has shadowed with an exit.
	overridden := false
	if strings.HasPrefix(line, string(overrideToken)) && w.Get(c.who).Flags.IsTrueWizard() {
		overridden = true
		line = strings.TrimSpace(line[1:])
		c.verb, c.arg = trimCommand(line)
		if line == "" {
			return
		}
	}

	// Single-character shortcuts take the rest of the line verbatim, so
	// punctuation and spacing survive.
	switch {
	case strings.HasPrefix(line, string(sayToken)):
		c.arg = strings.TrimSpace(line[1:])
		s.cmdSay(c)
		return
	case strings.HasPrefix(line, string(poseToken)):
		c.arg = strings.TrimSpace(line[1:])
		s.cmdPose(c)
		return
	}

	// Exits are matched before any built-in, which is what lets a world
	// define its own "look" or "@view". The player's world beats ours.
	if !overridden {
		m := match.New(w, c.who, line).Exits()
		if r := m.Result(); r != ref.Nothing && r != ref.Ambiguous {
			s.logCommand(w, d, line, "")
			// An exit that runs a program takes the rest of the line
			// as its argument.
			c.arg = m.Arg()
			s.useExit(c, r)
			return
		}
	}

	if strings.HasPrefix(c.verb, "@") {
		if h, name := lookupAtCommand(c.verb); h != nil {
			s.logCommand(w, d, name, c.arg)
			h(s, c)
			return
		}
		c.tell("I don't know that command.")
		return
	}

	if h, ok := commands[ascii.Fold(c.verb)]; ok {
		s.logCommand(w, d, c.verb, c.arg)
		h(s, c)
		return
	}

	c.tell("I don't understand that.")
}

// Interface commands and tokens, from include/game.h. QUIT and WHO are
// compared case-sensitively there, and the starter world depends on it.
const (
	quitCommand   = "QUIT"
	whoCommand    = "WHO"
	breakCommand  = "@Q"
	nullCommand   = "@@"
	overrideToken = '!'
	sayToken      = '"'
	poseToken     = ':'
)

// lookupAtCommand resolves an @-command by prefix. An exact name always wins,
// and an ambiguous prefix matches nothing rather than picking arbitrarily.
func lookupAtCommand(verb string) (handler, string) {
	v := ascii.Fold(verb)
	if h, ok := atCommands[v]; ok {
		return h, v
	}
	var found handler
	var name string
	n := 0
	for full, h := range atCommands {
		if strings.HasPrefix(full, v) {
			found, name = h, full
			n++
		}
	}
	if n == 1 {
		return found, name
	}
	return nil, ""
}

// logCommand records a command for the audit log. Anything that could carry a
// password is logged without its argument.
func (s *Server) logCommand(w *world.World, d *session.Descriptor, verb, arg string) {
	if isSecretCommand(verb) {
		arg = "<redacted>"
	}
	s.commandLog().Debug("command",
		"descriptor", d.ID,
		"player", d.Player.String(),
		"name", nameOf(w, d.Player),
		"verb", verb,
		"arg", arg,
	)
}

// isSecretCommand reports whether a command's argument contains a credential.
func isSecretCommand(verb string) bool {
	switch ascii.Fold(verb) {
	case "@password", "@newpassword", "@pcreate":
		return true
	}
	return false
}
