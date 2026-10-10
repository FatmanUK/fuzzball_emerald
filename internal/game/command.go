package game

import (
	"runtime/debug"
	"strings"
	"time"

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
	// "home" stays in the table so a name nothing else claims
	// still resolves, but it is reached before exit matching by
	// the direction test above — this entry only runs when
	// enable_home is clear, where upstream treats the word as
	// ordinary and the dispatcher says what it says for any
	// unknown command.
	register("home", (*Server).cmdHomeDisabled)
	register("inventory", (*Server).cmdInventory)
	register("examine", (*Server).cmdExamine)

	register("@create", (*Server).cmdCreate)
	register("@dig", (*Server).cmdDig)
	register("@open", (*Server).cmdOpen)
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
	register("@mcpedit", (*Server).cmdMcpedit)
	register("@mcpprogram", (*Server).cmdMcpprogram)
	register("@list", (*Server).cmdList)
	register("@toad", (*Server).cmdToad)
	register("@boot", (*Server).cmdBoot)
	register("@stats", (*Server).cmdStats)
	register("@sweep", (*Server).cmdSweep)
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
//
// They go to **every** connection that object has, not to the one
// that typed the line: upstream's `notify` is `notify_filtered` over
// a player's descriptors (`interface.c:4697`), so somebody connected
// twice sees their own command's output on both. Only the two
// interface commands are descriptor-local, and they build their own
// context — `WHO` is `queue_ansi(e, ...)` and QUIT's farewell is
// `queue_immediate_and_flush(d, ...)`, each writing to the one
// connection.
func (s *Server) commandAs(w *world.World, d *session.Descriptor, who ref.Ref, line string) {
	out := func(text string) { s.send(w, who, text) }
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

	// `cmd_log_threshold_msec`: nothing timed a command. Upstream
	// brackets the whole of process_command, the exit match and
	// the dispatch alike, and logs afterwards.
	started := time.Now()
	defer func() {
		s.logSlowCommand(w, d, line, time.Since(started))
	}()

	// **Left-trimmed only.** `process_command` runs
	// `skip_whitespace(&command)` (`game.c:610`) and never
	// touches the end of the line, so trailing whitespace reaches
	// the argument split — where arg1 loses it and arg2 keeps
	// it. This trimmed both ends, so the second argument of every
	// "="-taking command lost its trailing space, and `@set
	// x=kill_ok ` behaved differently here.
	line = trimLeftSpace(line)
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

	// `process_command`'s order (`game.c:616-671`), which is not
	// the obvious one and is load-bearing in three places.
	//
	// `enable_prefix` decides **where** the three
	// single-character shortcuts are expanded. Clear — the
	// default — and the expansion happens first, so an exit
	// cannot be named `"foo`. Set, and exit matching gets the raw
	// line first, so it can. Nothing read the parameter, and this
	// had the clear branch hard-coded.
	prefix := w.Tune.Bool("enable_prefix")
	if !prefix {
		line = expandPrefix(line)
	}

	// A true wizard may prefix a line with '!' to skip exit
	// matching, which is how you reach a built-in that a room has
	// shadowed with an exit. The test is on the **owner**'s bit,
	// which is what matters for a forced puppet.
	//
	// Because the test comes *after* the expansion above, a '!'
	// suppresses the shortcuts outright: `!"hello` is the command
	// word `"hello`, which nothing answers.
	override := strings.HasPrefix(line, string(overrideToken)) &&
		isTrueWiz(w, ownerOf(w, c.who))

	if !override {
		// Exits are matched before any built-in, which is
		// what lets a world define its own "look" or "@view".
		// The player's world beats ours.
		if s.tryMove(c, line, 0) {
			return
		}
		if prefix {
			// With the parameter set the expansion
			// happens here instead, and the expanded form
			// is offered to the matcher a second time —
			// so a world can name an exit `say hello`.
			// Whichever way that goes, the expansion
			// sticks for the dispatch below.
			if e := expandPrefix(line); e != line {
				line = e
				if s.tryMove(c, line, 0) {
					return
				}
			}
		}
		// `bad_pre_command` with no override to strip: a
		// world that has set `cmd_only_overrides` has
		// **turned every built-in off**, leaving them
		// reachable only to a true wizard's '!'. Including
		// `@tune`, so a wizard who sets it locks themselves
		// out of unsetting it the ordinary way.
		if w.Tune.Bool("cmd_only_overrides") {
			s.huh(c)
			return
		}
	} else {
		// `command++`, which skips the token and **not** the
		// whitespace after it. So "! @create x" has an empty
		// command word and reaches the dispatcher's default.
		line = line[1:]
	}

	c.verb, c.arg = trimCommand(line)
	c.rest = fullCommand(line)

	// One table for every command, "@"-prefixed or not, resolved
	// the way upstream's own dispatcher resolves: see
	// dispatch.go.
	if cmd, ok := resolve(c.verb); ok {
		s.logCommand(w, d, cmd.n, c.arg)
		s.dispatch(c, cmd)
		return
	}
	s.huh(c)
}

// isTrueWiz is `TrueWizard`, which — unlike `Wizard` — ignores
// QUELL: a quelled wizard still overrides with '!'.
func isTrueWiz(w *world.World, r ref.Ref) bool {
	o := w.Get(r)
	return o != nil && o.Flags.IsTrueWizard()
}

// expandPrefix rewrites a line beginning with one of the three
// single-character shortcuts, leaving anything else alone.
//
// The rewrite is textual — upstream builds "say %s" and hands the
// result back to the same code path — which is why the spacing
// survives into `full_command` and why the matcher gets a shot at the
// expanded string.
//
// `;` is the surprise: it expands to `delimiter`, and **there is no
// `delimiter` command**. Upstream's dispatcher has no entry for it,
// so `;hello` reaches "Huh?" by way of a command nobody wrote. The
// rewrite is reproduced rather than dropped, because the expanded
// form is offered to the exit matcher and a world could have an exit
// of that name.
func expandPrefix(line string) string {
	switch {
	case strings.HasPrefix(line, string(sayToken)):
		return "say " + line[1:]
	case strings.HasPrefix(line, string(poseToken)):
		return "pose " + line[1:]
	case strings.HasPrefix(line, string(exitDelimiter)):
		return "delimiter " + line[1:]
	}
	return line
}

// tryMove is `can_move` and `do_move` together (`predicates.c:505`,
// `move.c:60`), reporting whether the line was a direction after all.
//
// "home" is one, and it is tested **before** exits rather than after
// them: `can_move` answers yes for it outright when `enable_home` is
// set, so an exit of that name is unreachable. This server had it in
// the command table instead, which is consulted after exit matching,
// so the precedence was the other way round.
func (s *Server) tryMove(c *ctx, line string, lev int) bool {
	if ascEqual(line, "home") &&
		c.w.Tune.Bool("enable_home") {
		s.logCommand(c.w, c.d, line, "")
		s.goHome(c)
		return true
	}
	m := match.New(c.w, c.who, line).Level(lev).Exits()
	r := m.Result()
	if r == ref.Nothing || r == ref.Ambiguous {
		return false
	}
	s.logCommand(c.w, c.d, line, "")
	// An exit that runs a program takes the rest of the line as
	// its argument, and the part that matched its name as the
	// verb — upstream's match_args and match_cmdname, reset by
	// match_exits itself.
	c.verb, c.arg = m.Verb(), m.Arg()
	c.rest = c.arg
	s.useExit(c, r)
	return true
}

// huh is the dispatcher's `bad:` label (`game.c:1794`).
//
// `m3_huh` lets a world answer an unknown command with an **exit**,
// named "HUH? " plus the command word and matched at priority 3 —
// deliberately privileged, or any world could capture every typo. The
// name is built from the command *word* and not the whole line, where
// the parameter's own label says "with full command string".
//
// What it says otherwise is a @tune parameter, so a world can answer
// in its own voice.
func (s *Server) huh(c *ctx) {
	if c.w.Tune.Bool("m3_huh") &&
		s.tryMove(c, "HUH? "+c.verb, 3) {
		return
	}
	c.send(c.w.Tune.String("huh_mesg"))
	s.logFailedCommand(c, c.verb, c.rest)
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
	// `dequeue_prog(d->player, 2)`, which is the **player's**
	// foreground programs and not only the one reading this
	// descriptor — so "@Q" also stops a foreground program that
	// is asleep, and a queued MPI event unless
	// `mpi_continue_after_logout` says otherwise.
	return s.abortForegroundFor(w, d.Player)
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
