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
	// out is where this command's replies go. It is normally the
	// descriptor that typed the line, but a forced command answers to
	// whoever was forced, not to whoever did the forcing.
	out func(string)
	// verb is the command as typed, arg is the rest of the line.
	verb string
	arg  string
}

// tell sends a formatted line to the player the command is running for.
func (c *ctx) tell(format string, args ...any) { c.out(sprintf(format, args...)) }

// send delivers a line verbatim, for text that came from the world and must
// not be read as a format string.
func (c *ctx) send(text string) { c.out(text) }

// noisyMatch reports a failed name resolution the way upstream's
// noisy_match_result does, and reports whether the caller should go on.
//
// The wording quotes the name back, which matters: programs and players both
// read these, and upstream's other failure messages ("I don't see that here.")
// belong to commands that match quietly and complain in their own words.
func noisyMatch(c *ctx, name string, r ref.Ref) bool {
	switch r {
	case ref.Nothing:
		c.tell("I don't understand '%s'.", name)
		return false
	case ref.Ambiguous:
		c.tell("I don't know which '%s' you mean!", name)
		return false
	}
	return true
}

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
//
// Commands that dispatch other commands — @force — register themselves in an
// init instead, because naming them here makes the table refer to itself and
// Go rejects that as an initialisation cycle.
var atCommands = map[string]handler{
	"@create": (*Server).cmdCreate,
	"@dig":    (*Server).cmdDig,
	"@open":   (*Server).cmdOpen,
	// @action is upstream's do_action, which attaches an exit to a named
	// object rather than to the room, and says so in its own words. Until
	// that is ported it is an alias for @open, which differs in where the
	// exit lands.
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
	"@program":  (*Server).cmdProgram,
	"@edit":     (*Server).cmdEdit,
	"@list":     (*Server).cmdList,
	"@toad":     (*Server).cmdToad,
	"@boot":     (*Server).cmdBoot,
	"@stats":    (*Server).cmdStats,
	"@version":  (*Server).cmdVersion,
}

// command dispatches one line from a logged-in player.
//
// A handler that panics must not leave the player staring at nothing: the
// engine contains the panic, and this reports it so the failure is visible at
// both ends.
func (s *Server) command(w *world.World, d *session.Descriptor, line string) {
	s.commandAs(w, d, d.Player, line)
}

// commandAs dispatches a line on behalf of an object that is not the one that
// typed it, which is what @force does. Replies go to that object.
func (s *Server) commandAs(w *world.World, d *session.Descriptor, who ref.Ref, line string) {
	out := d.Send
	if who != d.Player {
		out = func(text string) { s.send(w, who, text) }
	}
	defer func() {
		if r := recover(); r != nil {
			out("Something went wrong running that command. It has been logged.")
			s.log.Error("panic handling a command",
				"descriptor", d.ID,
				"player", who.String(),
				"line", line,
				"panic", r,
				"stack", string(debug.Stack()))
		}
	}()

	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	c := &ctx{w: w, d: d, who: who, out: out}
	c.verb, c.arg = trimCommand(line)

	if w.Get(c.who) == nil {
		c.tell("Your character no longer exists.")
		d.Close()
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
		c.send(w.Tune.String("huh_mesg"))
		return
	}

	if h, ok := commands[ascii.Fold(c.verb)]; ok {
		s.logCommand(w, d, c.verb, c.arg)
		h(s, c)
		return
	}

	// What an unrecognised command says is a @tune parameter, so a world
	// can answer in its own voice.
	c.send(w.Tune.String("huh_mesg"))
}

// interfaceCommand handles the lines the descriptor layer answers itself,
// before anything else looks at them. It reports whether the line was consumed.
//
// These are case-sensitive, which is deliberate and load-bearing: the starter
// world ships a lowercase "quit" exit whose only job is to say that the real
// command is in capitals, and that only works because lowercase "quit" falls
// through to exit matching.
//
// They are answered ahead of a READ and ahead of the editor, as do_command has
// them, so QUIT always disconnects and "@Q" always escapes — a program waiting
// on input cannot swallow either.
func (s *Server) interfaceCommand(w *world.World, d *session.Descriptor, line string) bool {
	// WHO is the exception: while a program is reading or the editor is
	// open, it belongs to whatever has the line.
	busy := s.procs.readerFor(d.ID) != nil || s.editing(d.Player) != nil

	switch {
	case ascii.EqualFold(line, breakCommand):
		if s.abortForeground(w, d) {
			d.Send("Foreground program aborted.")
		}
		return true
	case line == quitCommand:
		s.logCommand(w, d, line, "")
		s.cmdQuit(&ctx{w: w, d: d, who: d.Player, out: d.Send})
		return true
	case line == nullCommand && w.Tune.Bool("recognize_null_command"):
		return true
	case !busy && strings.HasPrefix(line, whoCommand):
		arg := strings.TrimSpace(line[len(whoCommand):])
		s.logCommand(w, d, whoCommand, arg)
		s.cmdWho(&ctx{w: w, d: d, who: d.Player, out: d.Send, verb: whoCommand, arg: arg})
		return true
	}
	return false
}

// abortForeground stops the program a descriptor is waiting on, reporting
// whether there was one.
func (s *Server) abortForeground(w *world.World, d *session.Descriptor) bool {
	p := s.procs.readerFor(d.ID)
	if p == nil {
		return false
	}
	s.procs.remove(p.pid)
	return true
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

// exactOnlyCommands may not be reached by an abbreviation. Upstream compares
// these with strcmp rather than by prefix, and the reason is plain: each can
// damage the database outright, and "@san" should not be enough to run one.
var exactOnlyCommands = map[string]bool{
	"@sanity": true, "@sanfix": true, "@sanchange": true,
}

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
		if exactOnlyCommands[full] {
			continue
		}
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
