package game

import (
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/logging"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// `process_command`'s logging (`game.c:570`, `:1810`, `:1836`) and
// `log_program_text` (`log.c:356`). `internal/logging` replaced
// *where* a log line goes; the decisions about *whether* were never
// ported, so five `@tune` parameters had no reader —
// `log_commands`, `log_failed_commands`, `log_interactive`,
// `log_programs` and `cmd_log_threshold_msec`. Every command was
// logged, nothing else was, and nothing was timed.

// logCommand records a command, if this world logs commands.
//
// The gate is `log_commands` **or** the player's owner being an
// unquelled wizard, so a wizard is logged whatever the parameter says
// — which is the point: the parameter is about the players, not
// about the staff.
func (s *Server) logCommand(w *world.World, d *session.Descriptor,
	verb, arg string) {

	if !s.logsCommands(w, d.Player) {
		return
	}
	s.commandLog().Debug("command",
		"descriptor", d.ID,
		"player", d.Player.String(),
		"name", nameOf(w, d.Player),
		"verb", verb,
		"arg", maskSecrets(verb + " " + arg)[len(verb):],
		"who", whowhere(w, d.Player),
	)
}

// logsCommands is the gate itself.
func (s *Server) logsCommands(w *world.World, who ref.Ref) bool {
	return w.Tune.Bool("log_commands") ||
		isWizard(w, ownerOf(w, who))
}

// logInteractive records a line that went to the editor or to a
// program's READ rather than to the command parser. Neither was
// logged at all.
//
// The marker is upstream's, and which one it is says which took the
// line: `[READ] ` for a program, `[INTERP] ` for the editor.
func (s *Server) logInteractive(w *world.World,
	d *session.Descriptor, line string, read bool) {

	if !s.logsCommands(w, d.Player) ||
		!w.Tune.Bool("log_interactive") {
		return
	}
	mode := "[INTERP]"
	if read {
		mode = "[READ]"
	}
	s.commandLog().Debug("interactive",
		"descriptor", d.ID,
		"player", d.Player.String(),
		"name", nameOf(w, d.Player),
		"mode", mode,
		"line", maskSecrets(line),
		"who", whowhere(w, d.Player),
	)
}

// logFailedCommand is the `bad:` label's own line (`game.c:1810`),
// which is **not** gated on `log_commands`: a world can log the typos
// without logging everything else.
//
// Its second condition is the surprising one — `!controls(player,
// LOCATION(player))` — so a command nobody understood is logged
// only where the player does *not* control the room. A builder
// fumbling in their own workshop is not recorded; a stranger fumbling
// in somebody else's is.
func (s *Server) logFailedCommand(c *ctx, verb, rest string) {
	if !c.w.Tune.Bool("log_failed_commands") {
		return
	}
	loc := ref.Nothing
	if o := c.w.Get(c.who); o != nil {
		loc = o.Location
	}
	if s.controls(c.w, c.who, loc) {
		return
	}
	s.commandLog().Info("HUH",
		"player", c.who.String(),
		"name", nameOf(c.w, c.who),
		"room", loc.String(),
		"roomName", nameOf(c.w, loc),
		"roomOwner", nameOf(c.w, ownerOf(c.w, loc)),
		"verb", verb,
		"rest", maskSecrets(rest),
	)
}

// logSlowCommand records a command that took longer than
// `cmd_log_threshold_msec`, which nothing timed.
//
// The comparison is upstream's and is **greater than**, so a
// threshold of zero logs every command that took any measurable time
// at all rather than none.
func (s *Server) logSlowCommand(w *world.World,
	d *session.Descriptor, line string, took time.Duration) {

	limit := w.Tune.Int("cmd_log_threshold_msec")
	if took.Seconds() <= float64(limit)/1000.0 {
		return
	}
	logging.On(s.log, logging.CmdTimes).Info("slow command",
		"seconds", took.Seconds(),
		"who", whowhere(w, d.Player),
		"line", maskSecrets(line),
	)
}

// logProgramText is `log_program_text` (`log.c:356`): the whole
// source of a program, every time it is saved, when `log_programs` is
// set. `logging.Program` had no caller at all.
//
// Upstream writes it to a file with a banner of hashes either side of
// the header; this is one record with the text as a field, since
// `internal/logging` is structured. The three save paths that reach
// it are the editor's `q`, MCP simpleedit, and MUF
// `PROGRAM_SETLINES`.
func (s *Server) logProgramText(w *world.World, player,
	prog ref.Ref, src string) {

	if !w.Tune.Bool("log_programs") {
		return
	}
	logging.On(s.log, logging.Program).Info("program text",
		"program", prog.String(),
		"name", s.unparse(w, player, prog),
		"by", nameOf(w, player),
		"player", player.String(),
		"text", src,
	)
}

// whowhere is `whowhere` (`log.c:343`), the "who and where" prefix
// every one of these lines carries: a WIZ marker, the object's own
// name when it is not a player, its owner, and the room.
func whowhere(w *world.World, who ref.Ref) string {
	var b strings.Builder
	if isWizard(w, ownerOf(w, who)) {
		b.WriteString("WIZ: ")
	}
	o := w.Get(who)
	if o != nil && o.Type() != ref.TypePlayer {
		b.WriteString(o.Name + " owned by ")
	}
	loc := ref.Nothing
	if o != nil {
		loc = o.Location
	}
	b.WriteString(nameOf(w, ownerOf(w, who)) + "(" +
		who.String() + ") in " + nameOf(w, loc) + "(" +
		loc.String() + ")")
	return b.String()
}

// maskSecrets is upstream's three-way password mask
// (`game.c:574-586`), applied to the **whole line** rather than to
// the verb — `string_prefix` there, so "@passwordfoo" is masked
// too.
//
// Each of the three truncates differently, and the lengths are the
// command names': nine characters for `@password`, twelve for
// `@newpassword`. `@pcreate` keeps everything up to the '=', so the
// new player's **name** survives into the log where the password does
// not; this used to redact the argument whole and lose the name with
// it.
func maskSecrets(line string) string {
	switch {
	case ascii.HasPrefix(line, "@newpassword"):
		return cut(line, 12) + " [***]"
	case ascii.HasPrefix(line, "@password"):
		return cut(line, 9) + " [***]"
	case ascii.HasPrefix(line, "@pcreate"):
		if i := strings.IndexByte(line, '='); i >= 0 {
			return line[:i+1] + "[***]"
		}
	}
	return line
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
