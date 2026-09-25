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
	// descriptor that typed the line, but a forced command
	// answers to whoever was forced, not to whoever did the
	// forcing.
	out func(string)
	// verb is the command as typed, arg is the rest of the line.
	verb string
	arg  string
	// rest is upstream's full_command: the line after the verb
	// with exactly *one* character skipped, so leading whitespace
	// survives where arg has lost it.
	//
	// The two are different strings upstream — full_command and
	// arg1/arg2 — and the handful of commands that take the
	// line verbatim read this one: say, pose, @wall and gripe.
	// Losing the spaces is visible in all four, because what they
	// print is what was typed.
	rest string
}

// tell sends a formatted line to the player the command is running
// for.
func (c *ctx) tell(format string, args ...any) {
	c.out(sprintf(format, args...))
}

// send delivers a line verbatim, for text that came from the world
// and must not be read as a format string.
func (c *ctx) send(text string) { c.out(text) }

// noisyMatch reports a failed name resolution the way upstream's
// noisy_match_result does, and reports whether the caller should go
// on.
//
// The wording quotes the name back, which matters: programs and
// players both read these, and upstream's other failure messages ("I
// don't see that here.") belong to commands that match quietly and
// complain in their own words.
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

// The two maps that used to live here are gone: resolution is now
// commandTable in dispatch_table.go, which is upstream's own
// dispatcher rather than a unique-prefix approximation of it.
// Handlers attach to a name with register(), below and in each
// command file's init.
//
// Several registrations have to happen in an init rather than in a
// literal because they close over something that refers back to the
// table — @force dispatches other commands — and Go rejects that
// as an initialisation cycle.
func init() {
	register("look", (*Server).cmdLook)
	register("say", (*Server).cmdSay)
	register("pose", (*Server).cmdPose)
	register("page", (*Server).cmdPage)
	register("whisper", (*Server).cmdWhisper)
	register("goto", (*Server).cmdGo)
	register("move", (*Server).cmdGo)
	register("home", (*Server).cmdHome)
	register("inventory", (*Server).cmdInventory)
	register("get", (*Server).cmdGet)
	register("take", (*Server).cmdGet)
	register("drop", (*Server).cmdDrop)
	register("examine", (*Server).cmdExamine)

	register("@create", (*Server).cmdCreate)
	register("@dig", (*Server).cmdDig)
	register("@open", (*Server).cmdOpen)
	// @action is upstream's do_action, which attaches an exit to
	// a named object rather than to the room, and says so in its
	// own words. Until that is ported it is an alias for @open,
	// which differs in where the exit lands.
	register("@action", (*Server).cmdOpen)
	register("@link", (*Server).cmdLink)
	register("@unlink", (*Server).cmdUnlink)
	register("@name", (*Server).cmdName)
	register("@set", (*Server).cmdSet)
	register("@password", (*Server).cmdPassword)
	register("@find", (*Server).cmdFind)
	register("@teleport", (*Server).cmdTeleport)
	register("@recycle", (*Server).cmdRecycle)
	register("@dump", (*Server).cmdDump)
	register("@shutdown", (*Server).cmdShutdown)
	register("@tune", (*Server).cmdTune)
	register("@program", (*Server).cmdProgram)
	register("@edit", (*Server).cmdEdit)
	register("@list", (*Server).cmdList)
	register("@toad", (*Server).cmdToad)
	register("@boot", (*Server).cmdBoot)
	register("@stats", (*Server).cmdStats)
	register("@version", (*Server).cmdVersion)
}

// command dispatches one line from a logged-in player.
//
// A handler that panics must not leave the player staring at nothing:
// the engine contains the panic, and this reports it so the failure
// is visible at both ends.
func (s *Server) command(w *world.World, d *session.Descriptor, line string) {
	s.commandAs(w, d, d.Player, line)
}

// commandAs dispatches a line on behalf of an object that is not the
// one that typed it, which is what @force does. Replies go to that
// object.
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
	c.rest = fullCommand(line)

	if w.Get(c.who) == nil {
		c.tell("Your character no longer exists.")
		d.Close()
		return
	}

	// A wizard may prefix a line with '!' to skip exit matching,
	// which is how you reach a built-in that a room has shadowed
	// with an exit.
	overridden := false
	if strings.HasPrefix(line, string(overrideToken)) &&
		w.Get(c.who).Flags.IsTrueWizard() {
		overridden = true
		line = strings.TrimSpace(line[1:])
		c.verb, c.arg = trimCommand(line)
		c.rest = fullCommand(line)
		if line == "" {
			return
		}
	}

	// Single-character shortcuts take the rest of the line
	// verbatim, so punctuation and spacing survive.
	//
	// Upstream rewrites the line as "say <rest>" and then takes
	// full_command off it, which comes to exactly the text after
	// the token — leading spaces and all.
	switch {
	case strings.HasPrefix(line, string(sayToken)):
		c.arg, c.rest = strings.TrimSpace(line[1:]), line[1:]
		s.cmdSay(c)
		return
	case strings.HasPrefix(line, string(poseToken)):
		c.arg, c.rest = strings.TrimSpace(line[1:]), line[1:]
		s.cmdPose(c)
		return
	}

	// Exits are matched before any built-in, which is what lets a
	// world define its own "look" or "@view". The player's world
	// beats ours.
	if !overridden {
		m := match.New(w, c.who, line).Exits()
		if r := m.Result(); r != ref.Nothing &&
			r != ref.Ambiguous {
			s.logCommand(w, d, line, "")
			// An exit that runs a program takes the rest
			// of the line as its argument, and the part
			// that matched its name as the verb —
			// upstream's match_args and match_cmdname,
			// reset by match_exits itself.
			c.verb, c.arg = m.Verb(), m.Arg()
			c.rest = c.arg
			s.useExit(c, r)
			return
		}
	}

	// One table for every command, "@"-prefixed or not, resolved
	// the way upstream's own dispatcher resolves: see
	// dispatch.go.
	if cmd, ok := resolve(c.verb); ok {
		s.logCommand(w, d, cmd.n, c.arg)
		s.dispatch(c, cmd)
		return
	}

	// What an unrecognised command says is a @tune parameter, so
	// a world can answer in its own voice.
	c.send(w.Tune.String("huh_mesg"))
}

// interfaceCommand handles the lines the descriptor layer answers
// itself, before anything else looks at them. It reports whether the
// line was consumed.
//
// These are case-sensitive, which is deliberate and load-bearing: the
// starter world ships a lowercase "quit" exit whose only job is to
// say that the real command is in capitals, and that only works
// because lowercase "quit" falls through to exit matching.
//
// They are answered ahead of a READ and ahead of the editor, as
// do_command has them, so QUIT always disconnects and "@Q" always
// escapes — a program waiting on input cannot swallow either.
func (s *Server) interfaceCommand(w *world.World, d *session.Descriptor, line string) bool {
	// WHO is the exception: while a program is reading or the
	// editor is open, it belongs to whatever has the line.
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

// abortForeground stops the program a descriptor is waiting on,
// reporting whether there was one.
func (s *Server) abortForeground(w *world.World, d *session.Descriptor) bool {
	p := s.procs.readerFor(d.ID)
	if p == nil {
		return false
	}
	s.finishProcess(w, p)
	return true
}

// Interface commands and tokens, from include/game.h. QUIT and WHO
// are compared case-sensitively there, and the starter world depends
// on it.
const (
	quitCommand   = "QUIT"
	whoCommand    = "WHO"
	breakCommand  = "@Q"
	nullCommand   = "@@"
	overrideToken = '!'
	sayToken      = '"'
	poseToken     = ':'
)

// logCommand records a command for the audit log. Anything that could
// carry a password is logged without its argument.
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

// isSecretCommand reports whether a command's argument contains a
// credential.
func isSecretCommand(verb string) bool {
	switch ascii.Fold(verb) {
	case "@password", "@newpassword", "@pcreate":
		return true
	}
	return false
}
