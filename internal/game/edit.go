package game

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf/compiler"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// The editor's commands, from include/edit.h. Each is a single letter, and
// only the first character of the last word on the line is looked at, which is
// why "3 5 d" and "3 5 delete" both delete lines 3 to 5.
const (
	insertCommand     = 'i'
	deleteCommand     = 'd'
	quitEditCommand   = 'q'
	cancelEditCommand = 'x'
	compileCommand    = 'c'
	listCommand       = 'l'
	editorHelpCommand = 'h'
	killCommand       = 'k'
	showCommand       = 's'
	shortShowCommand  = 'a'
	viewCommand       = 'v'
	unassembleCommand = 'u'
	numberCommand     = 'n'
	publicsCommand    = 'p'

	// exitInsert leaves insert mode and returns to the command level.
	exitInsert = "."
	// maxEditArg is how many arguments an editor command takes, which
	// upstream fixes at MAX_ARG.
	maxEditArg = 2
)

// editSession is one player's editing state.
//
// The program's text lives here, not in the object, so an editor session costs
// nothing until someone opens one and an abandoned session cannot corrupt the
// stored source. The program is marked INTERNAL for as long as a session is
// open, which is what stops two people editing it at once.
type editSession struct {
	program ref.Ref
	lines   []string
	// curr is the insertion point, upstream's curr_line: a one-based line
	// number that new text is placed after.
	curr int
	// insert is set between "i" and ".".
	insert bool
}

// edit puts a player into the editor for a program. This is edit_program.
func (s *Server) edit(c *ctx, program ref.Ref) {
	o := c.w.Get(program)
	if o == nil || o.Type() != ref.TypeProgram || !s.controls(c.w, c.who, program) {
		c.tell("Permission denied!")
		return
	}
	if o.Flags&ref.Internal != 0 {
		c.tell("Sorry, this program is currently being edited by someone else.  Try again later.")
		return
	}

	src, _ := c.w.Source(program)
	e := &editSession{
		program: program,
		lines:   splitSource(src),
		// Upstream keeps the current line on the program rather than
		// the session, so reopening an editor resumes where the last
		// one left off. It is not persisted, so a restart forgets it.
		curr: s.editLine[program],
	}
	s.editors[c.who] = e

	o.Flags |= ref.Internal
	c.w.Modified(program)
	if p := c.w.Get(c.who); p != nil {
		p.Flags |= ref.Interactive
		c.w.Modified(c.who)
	}

	c.tell("Entering editor for %s.", unparse(c.w, c.who, program))
	s.listProgram(c, e, nil, false)
}

// splitSource breaks stored source into editable lines. A program saved with a
// trailing newline must not gain an empty last line from it.
func splitSource(src string) []string {
	if src == "" {
		return nil
	}
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.TrimSuffix(src, "\n")
	return strings.Split(src, "\n")
}

// joinSource renders the editing buffer back to storable text.
func joinSource(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// editing returns the player's open session, if any.
func (s *Server) editing(who ref.Ref) *editSession { return s.editors[who] }

// editInput handles one line typed by a player who is in the editor. It
// reports whether the line was consumed, which it always is: the editor sees
// every line until the player leaves it.
//
// The line arrives untrimmed, because in insert mode leading whitespace is
// part of the program text and an empty line is a blank line to insert.
func (s *Server) editInput(w *world.World, d *session.Descriptor, line string) {
	c := &ctx{w: w, d: d, who: d.Player}
	e := s.editors[c.who]
	if e == nil {
		return
	}
	if e.insert {
		s.insertLine(c, e, line)
		return
	}
	s.editCommand(c, e, line)
}

// editCommand parses and runs one editor command. This is editor().
//
// The parse is upstream's: the line is split into at most three words, each
// read as a number, and the command is the first character of the last
// non-empty word. "def" is special-cased before that, because its definition
// is the rest of the line rather than a word.
func (s *Server) editCommand(c *ctx, e *editSession, line string) {
	words, args, ok := parseEditLine(c, line)
	if !ok {
		return
	}
	if len(words) == 0 {
		return
	}
	// "def" is only a macro definition once a second word follows it:
	// upstream recognises it while parsing that word, so a bare "def" never
	// reaches the special case and is read as the delete command instead.
	if len(words) > 1 {
		if def, name, body := parseDef(line); def {
			s.defineMacro(c, name, body)
			return
		}
	}

	last := words[len(words)-1]
	argc := len(words) - 1

	switch ascii.Fold(last)[0] {
	case killCommand:
		if !isWizard(c.w, c.who) {
			c.tell("I'm sorry Dave, but I can't let you do that.")
			return
		}
		if c.w.KillMacro(words[0]) {
			c.tell("Macro entry deleted.")
		} else {
			c.tell("Macro to delete not found.")
		}
	case showCommand:
		s.listMacros(c, words[:argc], true)
	case shortShowCommand:
		s.listMacros(c, words[:argc], false)
	case insertCommand:
		e.insert = true
		if argc > 0 {
			e.curr = args[0] - 1
		}
		c.tell("Entering insert mode.")
	case deleteCommand:
		s.deleteLines(c, e, args, argc)
	case quitEditCommand:
		s.leaveEditor(c, e, true)
		c.tell("Editor exited.")
	case cancelEditCommand:
		s.leaveEditor(c, e, false)
		c.tell("Changes cancelled.")
	case compileCommand:
		s.compileEdited(c, e)
		c.tell("Compiler done.")
	case listCommand:
		s.listProgram(c, e, args[:argc], false)
	case editorHelpCommand:
		s.editorHelp(c)
	case viewCommand:
		s.viewHeader(c, args, argc)
	case unassembleCommand:
		s.disassemble(c, e)
	case numberCommand:
		s.toggleNumbers(c, args, argc)
	case publicsCommand:
		s.listPublics(c, e, args, argc)
	default:
		c.tell("Illegal editor command.")
	}
}

// parseEditLine splits an editor command into its words and their numeric
// values. It reports false when the line is rejected outright.
func parseEditLine(c *ctx, line string) ([]string, []int, bool) {
	words := strings.Fields(line)
	if len(words) > maxEditArg+1 {
		words = words[:maxEditArg+1]
	}
	args := make([]int, maxEditArg+1)
	for i, w := range words {
		// Upstream uses atoi, which reads a leading number and ignores
		// the rest, so "3d" is line 3 and the delete command at once.
		n := leadingInt(w)
		if n < 0 {
			c.tell("Negative arguments not allowed!")
			return nil, nil, false
		}
		args[i] = n
	}
	return words, args, true
}

// leadingInt reads the integer at the front of a word, as atoi does: no digits
// means zero, and trailing text is ignored.
func leadingInt(w string) int {
	i := 0
	if i < len(w) && (w[i] == '-' || w[i] == '+') {
		i++
	}
	start := i
	for i < len(w) && w[i] >= '0' && w[i] <= '9' {
		i++
	}
	if i == start {
		return 0
	}
	n, err := strconv.Atoi(w[:i])
	if err != nil {
		return 0
	}
	return n
}

// parseDef recognises a macro definition, "def <name> <body>". The body is the
// rest of the line verbatim, so a definition may contain spaces.
func parseDef(line string) (ok bool, name, body string) {
	rest := strings.TrimLeft(line, " \t")
	word, rest, _ := strings.Cut(rest, " ")
	if !ascii.EqualFold(word, "def") {
		return false, "", ""
	}
	rest = strings.TrimLeft(rest, " \t")
	name, body, _ = strings.Cut(rest, " ")
	return true, name, strings.TrimLeft(body, " \t")
}

// defineMacro adds an entry to the editor's macro table.
func (s *Server) defineMacro(c *ctx, name, body string) {
	// A macro whose name starts with a digit or a dot could never be typed,
	// because '.' introduces the expansion and a number is a literal.
	if name == "" || name[0] == '.' || name[0] >= '0' && name[0] <= '9' {
		c.tell("Invalid macro name.")
		return
	}
	if body == "" {
		c.tell("Invalid definition syntax.")
		return
	}
	if c.w.DefineMacro(name, body, c.who) {
		c.tell("Entry created.")
	} else {
		c.tell("That macro already exists!")
	}
}

// listMacros prints the macro table, or the part of it between two names.
//
// The bounds are compared only as far as the bound is long, which is
// upstream's strncmp: "aaa" as both ends selects every macro whose name starts
// with "aaa", not the single macro called "aaa". One bound is used as both
// ends, and none means the whole table.
//
// The full form gives each macro's definition and owner; the short form packs
// names into fixed columns and breaks the line once it passes 70 characters,
// which is why a row holds five names rather than a round number.
func (s *Server) listMacros(c *ctx, bounds []string, full bool) {
	first, last := "", ""
	if n := len(bounds); n > 0 {
		first, last = bounds[0], bounds[n-1]
	}

	var row strings.Builder
	for _, m := range c.w.Macros() {
		if boundedBefore(m.Name, first) || boundedAfter(m.Name, last) {
			continue
		}
		if full {
			c.send(sprintf("%-16s %-16s  %s",
				m.Name, nameOf(c.w, m.Owner), m.Definition))
			continue
		}
		row.WriteString(sprintf("%-16s", m.Name))
		if row.Len() > 70 {
			c.send(row.String())
			row.Reset()
		}
	}
	if !full {
		c.send(row.String())
	}
	c.tell("End of list.")
}

// boundedBefore reports whether a name sorts before the lower bound, comparing
// only as many bytes as the bound has.
func boundedBefore(name, bound string) bool {
	return bound != "" && compareN(name, bound) < 0
}

// boundedAfter is the same for the upper bound.
func boundedAfter(name, bound string) bool {
	return bound != "" && compareN(name, bound) > 0
}

// compareN compares name against the whole of bound, ignoring anything in name
// beyond the bound's length. This is strncmp(name, bound, strlen(bound)).
func compareN(name, bound string) int {
	if len(name) > len(bound) {
		name = name[:len(bound)]
	}
	return strings.Compare(name, bound)
}

// insertLine handles a line typed in insert mode.
//
// Where the line lands is upstream's, and its edge cases matter: a current
// line past the end of the program appends, a current line of zero inserts
// before the first line, and an empty line is stored as a single space so the
// listing shows something.
func (s *Server) insertLine(c *ctx, e *editSession, line string) {
	if ascii.EqualFold(line, exitInsert) {
		e.insert = false
		c.tell("Exiting insert mode.")
		return
	}
	text := line
	if text == "" {
		text = " "
	}

	switch {
	case len(e.lines) == 0:
		e.lines = []string{text}
		e.curr = 2
	case e.curr == 0:
		e.lines = slices.Insert(e.lines, 0, text)
		e.curr = 1
	case e.curr < 0 || e.curr-1 >= len(e.lines):
		// Walking off the end appends, and so does a negative current
		// line: upstream's walk never terminates on one, so it runs
		// out of list instead.
		e.lines = append(e.lines, text)
		e.curr = len(e.lines) + 1
	default:
		e.lines = slices.Insert(e.lines, e.curr, text)
		e.curr++
	}
}

// deleteLines removes one line, a range, or the current line.
func (s *Server) deleteLines(c *ctx, e *editSession, arg []int, argc int) {
	from, to, ok := editRange(c, e, arg, argc, "Nonsensical arguments.")
	if !ok {
		return
	}
	if from < 1 || from > len(e.lines) {
		c.tell("No line to delete!")
		return
	}
	e.curr = from
	if to > len(e.lines) {
		to = len(e.lines)
	}
	n := to - from + 1
	e.lines = slices.Delete(e.lines, from-1, to)
	c.tell("%d line%s deleted", n, plural(n))
}

// editRange resolves the argument forms every line-taking command shares: no
// argument means the current line, one means that line alone, two are a range.
func editRange(c *ctx, e *editSession, arg []int, argc int, badOrder string) (from, to int, ok bool) {
	switch argc {
	case 0:
		from, to = e.curr, e.curr
	case 1:
		from, to = arg[0], arg[0]
	case 2:
		from, to = arg[0], arg[1]
	default:
		c.tell("Too many arguments!")
		return 0, 0, false
	}
	if from > to && to != -1 {
		c.tell("%s", badOrder)
		return 0, 0, false
	}
	return from, to, true
}

// listProgram prints lines from the editing buffer. A nil arg lists everything,
// which is what entering the editor does.
//
// When sysmsg is set the counts and complaints are wrapped in MUF comment
// parentheses, so the output of "@list !" can be pasted straight back into a
// program.
func (s *Server) listProgram(c *ctx, e *editSession, arg []int, sysmsg bool) {
	s.listLines(c, e.lines, e.curr, arg, sysmsg)
}

func (s *Server) listLines(c *ctx, lines []string, curr int, arg []int, sysmsg bool) {
	open, close := "", ""
	if sysmsg {
		open, close = "( ", " )"
	}

	var from, to int
	switch len(arg) {
	case 0:
		from, to = curr, curr
	case 1:
		from, to = arg[0], arg[0]
	case 2:
		from, to = arg[0], arg[1]
	default:
		c.tell("%sToo many arguments!%s", open, close)
		return
	}
	if from > to && to != -1 {
		c.tell("%sArguments don't make sense!%s", open, close)
		return
	}
	if from < 1 || from > len(lines) {
		c.tell("%sLine not available for display.%s", open, close)
		return
	}
	if to == -1 || to > len(lines) {
		to = len(lines)
	}

	numbered := hasFlag(c.w, c.who, ref.Internal)
	for i := from; i <= to; i++ {
		if numbered {
			c.send(sprintf("%3d: %s", i, lines[i-1]))
		} else {
			c.send(lines[i-1])
		}
	}
	if n := to - from + 1; n > 1 {
		c.tell("%s%d lines displayed.%s", open, n, close)
	}
}

// leaveEditor ends a session, saving the text or discarding it.
func (s *Server) leaveEditor(c *ctx, e *editSession, save bool) {
	if save {
		c.w.SaveSource(e.program, joinSource(e.lines))
		s.log.Info("program saved",
			"program", e.program.String(),
			"name", nameOf(c.w, e.program),
			"by", nameOf(c.w, c.who),
			"player", c.who.String(),
			"lines", len(e.lines))
	}
	// Either way the cached compile is stale: saving replaced the source,
	// and cancelling may follow a "c" that compiled the buffer.
	s.InvalidateProgram(e.program)
	s.closeEditor(c.w, c.who, e)
}

// closeEditor drops the session and clears the flags that mark it open.
func (s *Server) closeEditor(w *world.World, who ref.Ref, e *editSession) {
	delete(s.editors, who)
	s.editLine[e.program] = e.curr
	if o := w.Get(e.program); o != nil {
		o.Flags &^= ref.Internal
		w.Modified(e.program)
	}
	if p := w.Get(who); p != nil {
		p.Flags &^= ref.Interactive
		w.Modified(who)
	}
}

// compileEdited compiles the buffer without saving it, which is how a
// programmer checks their work before leaving the editor.
func (s *Server) compileEdited(c *ctx, e *editSession) {
	prog, ok := s.compileForEditor(c, e.program, joinSource(e.lines))
	if !ok {
		return
	}
	// The compile is cached under the program's ref, so a caller that runs
	// it before the editor saves gets the version just checked. Cancelling
	// the session drops it again.
	s.cacheProgram(e.program, prog, nil)
	c.tell("Program compiled successfully.")
}

// compileForEditor compiles text and reports the failure to the player.
//
// A compile error is always shown to someone in the editor, whichever command
// triggered it: upstream's do_abort_compile prints rather than logs whenever
// the player is interactive, so "p" and "u" name the bad line just as "c"
// does.
func (s *Server) compileForEditor(c *ctx, program ref.Ref, src string) (*muf.Program, bool) {
	prog, err := s.compileSource(c.w, program, src)
	if err != nil {
		c.send(compileErrorText(err))
		return nil, false
	}
	return prog, true
}

// compileErrorText renders a compile failure the way upstream's
// do_abort_compile does, because that wording is what programmers read.
func compileErrorText(err error) string {
	var ce *compiler.Error
	if errors.As(err, &ce) && ce.Line > 0 {
		return sprintf("Error in line %d: %s", ce.Line, ce.Msg)
	}
	return sprintf("Error: %s", err.Error())
}

// editorHelp shows the editor's command summary.
func (s *Server) editorHelp(c *ctx) {
	for _, line := range editorHelpText {
		c.send(line)
	}
}

// viewHeader shows another program's leading comment block, which is the
// convention for documenting what a library does and how to call it.
func (s *Server) viewHeader(c *ctx, arg []int, argc int) {
	if argc != 1 {
		c.tell("I don't understand which header you're trying to look at.")
		return
	}
	program := ref.Ref(arg[0])
	o := c.w.Get(program)
	if o == nil || o.Type() != ref.TypeProgram {
		c.tell("That isn't a program.")
		return
	}
	if !s.viewable(c, program) {
		c.tell("That's not a public program.")
		return
	}
	src, _ := c.w.Source(program)
	for _, line := range splitSource(src) {
		if !strings.HasPrefix(line, "(") {
			break
		}
		c.send(line)
	}
	c.tell("Done.")
}

// viewable reports whether a player may read a program's source: either they
// control it, or it is set VEHICLE, which for a program means "viewable".
func (s *Server) viewable(c *ctx, program ref.Ref) bool {
	if s.controls(c.w, c.who, program) {
		return true
	}
	o := c.w.Get(program)
	return o != nil && o.Flags&ref.Vehicle != 0
}

// toggleNumbers turns the listing's line numbers on or off. Upstream keeps
// this on the player's INTERNAL flag, so it survives between sessions.
func (s *Server) toggleNumbers(c *ctx, arg []int, argc int) {
	p := c.w.Get(c.who)
	if p == nil {
		return
	}
	on := p.Flags&ref.Internal == 0
	if argc > 0 {
		on = arg[0] != 0
	}
	if on {
		p.Flags |= ref.Internal
		c.tell("Line numbers on.")
	} else {
		p.Flags &^= ref.Internal
		c.tell("Line numbers off.")
	}
	c.w.Modified(c.who)
}

// listPublics names the procedures another program may CALL.
func (s *Server) listPublics(c *ctx, e *editSession, arg []int, argc int) {
	if argc > 1 {
		c.tell("I don't understand which program you want to list PUBLIC functions for.")
		return
	}
	program := e.program
	if argc == 1 {
		program = ref.Ref(arg[0])
	}
	o := c.w.Get(program)
	if o == nil || o.Type() != ref.TypeProgram {
		c.tell("That isn't a program.")
		return
	}
	if !s.viewable(c, program) {
		c.tell("That's not a public program.")
		return
	}

	// The buffer is what the programmer is asking about when it is their
	// own session; anything else is compiled from what is stored.
	src := joinSource(e.lines)
	if program != e.program {
		src, _ = c.w.Source(program)
	}
	prog, ok := s.compileForEditor(c, program, src)
	if !ok {
		c.tell("Unable to compile %s.", unparse(c.w, c.who, program))
		return
	}
	c.tell("PUBLIC functions:")
	for _, name := range prog.PublicOrder {
		c.send(prog.Publics[name].Name)
	}
}

// disassemble prints the compiled instruction array.
//
// It never compiles anything: upstream reads the code the program already
// carries, so "u" before a successful "c" says there is nothing there rather
// than reporting a compile error.
//
// Emerald has no peephole optimiser, so where upstream shows a fused
// instruction this shows the pair it was built from. The listing is a
// debugging aid rather than something a program reads, so that difference is
// left as it is.
func (s *Server) disassemble(c *ctx, e *editSession) {
	cached, ok := s.programs[e.program]
	prog := cached.prog
	if !ok || prog == nil || len(prog.Code) == 0 {
		c.tell("Nothing to disassemble!")
		return
	}
	for i, in := range prog.Code {
		c.send(sprintf("%d: (line %d) %s", i, in.Line, describeInst(prog, in)))
	}
}

// describeInst renders one instruction the way upstream's disassemble does.
func describeInst(prog *muf.Program, in muf.Inst) string {
	switch in.Type {
	case muf.TypePrimitive:
		if name := muf.PrimName(int(in.Num)); name != "?" {
			return "PRIMITIVE: " + name
		}
		return sprintf("PRIMITIVE: %d", in.Num)
	case muf.TypeMark:
		return "MARK"
	case muf.TypeString:
		return sprintf("STRING: %q", in.Str)
	case muf.TypeArray:
		return sprintf("ARRAY: %d items", in.Num)
	case muf.TypeFunction:
		name, vars, args := "", 0, 0
		if in.Proc != nil {
			name, vars, args = in.Proc.Name, in.Proc.Vars, in.Proc.Args
		}
		return sprintf("FUNCTION: %s, VARS: %d, ARGS: %d", name, vars, args)
	case muf.TypeLock:
		return sprintf("LOCK: [%s]", in.Str)
	case muf.TypeInteger:
		return sprintf("INTEGER: %d", in.Num)
	case muf.TypeFloat:
		return sprintf("FLOAT: %.17g", in.Float)
	case muf.TypeAddress:
		return sprintf("ADDRESS: %d", in.Num)
	case muf.TypeTry:
		return sprintf("TRY: %d", in.Num)
	case muf.TypeIf:
		return sprintf("IF: %d", in.Num)
	case muf.TypeJmp:
		return sprintf("JMP: %d", in.Num)
	case muf.TypeExec:
		return sprintf("EXEC: %d", in.Num)
	case muf.TypeObject:
		return sprintf("OBJECT REF: %d", int32(in.Ref))
	case muf.TypeVar:
		return sprintf("VARIABLE: %d", in.Num)
	case muf.TypeSVar:
		return sprintf("SCOPEDVAR: %d (%s)", in.Num, scopedVarName(prog, in))
	case muf.TypeSVarAt:
		return sprintf("FETCH SCOPEDVAR: %d (%s)", in.Num, scopedVarName(prog, in))
	case muf.TypeSVarBang:
		return sprintf("SET SCOPEDVAR: %d (%s)", in.Num, scopedVarName(prog, in))
	case muf.TypeLVar:
		return sprintf("LOCALVAR: %d", in.Num)
	case muf.TypeLVarAt:
		return sprintf("FETCH LOCALVAR: %d", in.Num)
	case muf.TypeLVarBang:
		return sprintf("SET LOCALVAR: %d", in.Num)
	}
	return "UNKNOWN INST"
}

// scopedVarName names a function-scoped variable slot, which needs the
// procedure the instruction belongs to.
func scopedVarName(prog *muf.Program, in muf.Inst) string {
	if in.Proc != nil && int(in.Num) < len(in.Proc.VarNames) {
		return in.Proc.VarNames[in.Num]
	}
	return ""
}

// hasFlag reports whether an object carries a flag.
func hasFlag(w *world.World, r ref.Ref, f ref.Flags) bool {
	o := w.Get(r)
	return o != nil && o.Flags&f != 0
}

// isWizard reports whether a player holds wizard powers right now, which a
// quelled wizard does not.
func isWizard(w *world.World, r ref.Ref) bool {
	o := w.Get(r)
	return o != nil && o.Flags.IsWizard()
}

// --- the commands that open the editor --------------------------------------

// cmdProgram implements @program: find or create a program, then edit it.
func (s *Server) cmdProgram(c *ctx) {
	name, rname, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	rname = strings.TrimSpace(rname)
	if name == "" {
		c.tell("No program name given.")
		return
	}
	if !s.requireNotGuest(c, "@program") || !s.requireMucker(c, "@program") {
		return
	}

	program := matchProgram(c, name)
	if program == ref.Ambiguous {
		c.tell("I don't know which one you mean.")
		return
	}
	if rname != "" || program == ref.Nothing {
		var err error
		program, err = s.createProgram(c, name)
		if err != nil {
			c.send(err.Error())
			return
		}
		c.tell("Program %s created.", unparse(c.w, c.who, program))
		if rname != "" {
			c.w.SetProp(ref.GlobalEnvironment, "_reg/"+rname,
				propRef(program))
			c.tell("Registered as $%s", rname)
		}
	}
	s.edit(c, program)
}

// cmdEdit implements @edit: open an existing program.
func (s *Server) cmdEdit(c *ctx) {
	name := strings.TrimSpace(c.arg)
	if name == "" {
		c.tell("No program name given.")
		return
	}
	if !s.requireNotGuest(c, "@edit") || !s.requireMucker(c, "@edit") {
		return
	}
	switch program := matchProgram(c, name); program {
	case ref.Nothing:
		c.tell("I don't see that here.")
	case ref.Ambiguous:
		c.tell("I don't know which one you mean.")
	default:
		s.edit(c, program)
	}
}

// matchProgram resolves a program name the way @program and @edit do: what the
// player is carrying, what is in the room, a registered name, or a dbref.
func matchProgram(c *ctx, name string) ref.Ref {
	return match.New(c.w, c.who, name).
		Possession().Neighbor().Registered().Absolute().Result()
}

// cmdList implements @list, which prints a program's stored source without
// entering the editor.
//
// The line specification may be prefixed with '!' to comment out the counts,
// '#' to force line numbers on, or '@' to force them off. Those prefixes are
// how a programmer pastes a listing back into a program.
func (s *Server) cmdList(c *ctx) {
	name, linespec, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	if name == "" {
		c.tell("List what?")
		return
	}
	if !s.requireNotGuest(c, "@list") {
		return
	}
	program := matchProgram(c, name)
	switch program {
	case ref.Nothing:
		c.tell("I don't see that here.")
		return
	case ref.Ambiguous:
		c.tell("I don't know which one you mean.")
		return
	}
	if o := c.w.Get(program); o == nil || o.Type() != ref.TypeProgram {
		c.tell("You can't list anything but a program.")
		return
	}
	if !s.viewable(c, program) {
		c.tell("Permission denied. (You don't control the program, and it's not set Viewable)")
		return
	}

	sysmsg, numbers, linespec := listOptions(c, strings.TrimSpace(linespec))
	// The numbering override applies for this listing only, so it is set
	// and put back rather than saved.
	if numbers != nil {
		restore := hasFlag(c.w, c.who, ref.Internal)
		setFlag(c.w, c.who, ref.Internal, *numbers)
		defer setFlag(c.w, c.who, ref.Internal, restore)
	}

	arg := parseLineSpec(linespec)
	src, _ := c.w.Source(program)
	s.listLines(c, splitSource(src), 0, arg, sysmsg)
}

// listOptions strips the '!', '#' and '@' prefixes from a line specification.
func listOptions(c *ctx, spec string) (sysmsg bool, numbers *bool, rest string) {
	for spec != "" {
		switch spec[0] {
		case '!':
			sysmsg = true
		case '#':
			on := true
			numbers = &on
		case '@':
			off := false
			numbers = &off
		default:
			return sysmsg, numbers, spec
		}
		spec = spec[1:]
	}
	return sysmsg, numbers, spec
}

// parseLineSpec reads "12", "12-20", "12-" or "-20" into a range. An empty
// specification lists the whole program.
func parseLineSpec(spec string) []int {
	spec = strings.Join(strings.Fields(spec), "")
	if spec == "" {
		return []int{1, -1}
	}
	from := 1
	if spec != "" && spec[0] >= '0' && spec[0] <= '9' {
		from = leadingInt(spec)
		for spec != "" && spec[0] >= '0' && spec[0] <= '9' {
			spec = spec[1:]
		}
	}
	if spec == "" {
		return []int{from}
	}
	spec = strings.TrimPrefix(spec, "-")
	if spec == "" {
		return []int{from, -1}
	}
	return []int{from, leadingInt(spec)}
}

// setFlag turns a flag on or off without touching the rest.
func setFlag(w *world.World, r ref.Ref, f ref.Flags, on bool) {
	o := w.Get(r)
	if o == nil {
		return
	}
	if on {
		o.Flags |= f
	} else {
		o.Flags &^= f
	}
}

// requireMucker reports whether the player may write programs, telling them if
// not. This is upstream's MUCKERONLY: any mucker bit at all will do.
func (s *Server) requireMucker(c *ctx, cmd string) bool {
	if o := c.w.Get(c.who); o != nil && o.Flags.MLevel() != 0 {
		return true
	}
	c.tell("Only programmers are allowed to %s.", cmd)
	return false
}

// requireNotGuest refuses a command to a guest account, as NOGUEST does.
func (s *Server) requireNotGuest(c *ctx, cmd string) bool {
	o := c.w.Get(c.who)
	if o == nil || o.Flags&ref.Guest == 0 || o.Flags.IsTrueWizard() {
		return true
	}
	s.log.Info("guest refused a command",
		"player", c.who.String(), "name", nameOf(c.w, c.who), "command", cmd)
	c.tell("Guests are not allowed to %s.", cmd)
	return false
}

// createProgram makes a new program owned by the player, as create_program
// does: it goes into the player's inventory, gets a stock description, and
// takes its flags from the new_program_flags parameter.
func (s *Server) createProgram(c *ctx, name string) (ref.Ref, error) {
	o := c.w.Create(name, ref.TypeProgram, c.who)
	o.Props.SetString("_/de", sprintf("A scroll containing a spell called %s", name))
	applyTuneFlags(o, c.w.Tune.String("new_program_flags"))

	// A program may not be created with more authority than its author
	// holds, which is the same rule find_mlev applies when it runs.
	if lv := o.Flags.MLevel(); lv == 0 || lv > c.w.Get(c.who).Flags.MLevel() {
		o.Flags = o.Flags.SetMLevel(c.w.Get(c.who).Flags.MLevel())
	}
	if err := c.w.MoveTo(o.Ref, c.who); err != nil {
		return ref.Nothing, err
	}
	c.w.SaveSource(o.Ref, "")
	return o.Ref, nil
}

// applyTuneFlags sets flags named by a tune string: one letter per flag, with
// a digit giving the mucker level. This is set_flags_from_tunestr.
func applyTuneFlags(o *world.Object, spec string) {
	for i := 0; i < len(spec); i++ {
		ch := spec[i]
		if ch == '\n' || ch == '\r' {
			break
		}
		if ch >= '1' && ch <= '3' {
			o.Flags = o.Flags.SetMLevel(int(ch - '0'))
			continue
		}
		if ch == 'm' || ch == 'M' {
			o.Flags = o.Flags.SetMLevel(2)
			continue
		}
		// '0' is accepted and ignored, as upstream has it: clearing the
		// level this way would fight the cap applied afterwards.
		if ch == '0' {
			continue
		}
		if f, ok := tuneFlagLetters[ascii.Fold(string(ch))[0]]; ok {
			o.Flags |= f
		}
	}
}

// tuneFlagLetters maps the letters new_program_flags accepts to their bits.
// 'M' is a mucker level rather than a flag and is handled with the digits, and
// 'W' is ignored: upstream will not auto-set the wizard bit.
var tuneFlagLetters = map[byte]ref.Flags{
	'a': ref.Abode, 'b': ref.Builder, 'c': ref.ChownOK, 'd': ref.Dark,
	'g': ref.Guest, 'h': ref.Haven, 'j': ref.JumpOK, 'k': ref.KillOK,
	'l': ref.LinkOK, 'o': ref.Overt, 'q': ref.Quell, 's': ref.Sticky,
	'v': ref.Vehicle, 'x': ref.XForcible, 'y': ref.Yield, 'z': ref.Zombie,
}

// propRef makes a dbref-valued property.
func propRef(r ref.Ref) props.Value {
	return props.Value{Type: props.Ref, Ref: r}
}

// editorHelpText is the command summary "h" prints. Upstream reads this from a
// help file on disk; a single binary has nowhere to read one from, so it is
// built in.
var editorHelpText = []string{
	"MUF editor commands:",
	"  i [line]        insert before line, or at the current line; '.' ends insert mode",
	"  <n> d           delete line n",
	"  <n> <m> d       delete lines n to m",
	"  l               list the current line",
	"  <n> l           list line n",
	"  <n> <m> l       list lines n to m",
	"  n [0|1]         toggle, or set, line numbers in listings",
	"  c               compile without leaving the editor",
	"  q               save the program and leave the editor",
	"  x               leave the editor, discarding every change",
	"  <dbref> v       show another program's header comment",
	"  [dbref] p       list PUBLIC functions",
	"  u               disassemble the compiled program",
	"  def <name> <definition>   define an editor macro",
	"  <name> k        delete a macro (wizards only)",
	"  [from [to]] s   show macros in full",
	"  [from [to]] a   show macro names only",
	"  h               this list",
}
