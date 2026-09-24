package compiler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Error is a compile failure, carrying the line it happened on.
type Error struct {
	Line int
	Msg  string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
	}
	return e.Msg
}

// Options configure a compile.
type Options struct {
	// Ref is the program object being compiled, which "__PROG__"
	// expands to.
	Ref ref.Ref
	// MLevel is the mucker level the program runs at.
	MLevel int

	// Defines supplies compile-time definitions held in the
	// database: the _defs/ propdir on #0 and on the program's
	// owner. Fuzzball reads them itself; the compiler takes them
	// as input so it needs no world.
	Defines map[string]string

	// Macros is the MUF editor's macro table, which a program
	// reaches by prefixing a name with '.'.
	Macros map[string]string

	// Include resolves a $include target to the definitions that
	// object exposes in its _defs/ propdir. Targets are usually
	// registered names such as "$lib/alias", which the caller
	// looks up; the compiler needs no world of its own.
	//
	// It also answers $iflib. When nil, every $include is skipped
	// and every $iflib is false.
	Include func(target string) (map[string]string, bool)

	// MuckName and Version fill the __muckname and __version
	// built-ins, whose values come from the running server.
	MuckName string
	Version  string
}

// control is an open control structure awaiting its closing word.
type control struct {
	kind controlKind
	// addr is the instruction that needs patching, or the loop's
	// start.
	addr int
	line int
	// exits collects the jumps a WHILE or BREAK made, patched
	// when the loop closes.
	exits []int
	// trys counts TRY blocks opened inside this loop, so BREAK,
	// CONTINUE and WHILE can unwind them.
	trys int
}

type controlKind int

const (
	ctrlIf controlKind = iota + 1
	ctrlElse
	ctrlBegin
	ctrlFor
	ctrlTry
	ctrlCatch
)

func (k controlKind) String() string {
	switch k {
	case ctrlIf, ctrlElse:
		// Named for the pair, as upstream's messages are: an
		// open IF is reported as an unterminated IF-THEN.
		return "IF-THEN"
	case ctrlBegin, ctrlFor:
		return "loop"
	case ctrlTry:
		return "TRY-CATCH"
	case ctrlCatch:
		return "CATCH-ENDCATCH"
	}
	return "?"
}

// compiler holds the state of one compile.
type compiler struct {
	lex  *lexer
	opts Options

	code []muf.Inst

	// vars, lvars and svars are the three variable scopes. svars
	// belong to the procedure being compiled and reset at each
	// ':'.
	vars  []string
	lvars []string
	svars []string

	procs       map[string]int
	publics     map[string]*muf.Public
	publicOrder []string
	// procOrder keeps declaration order, so a program with no
	// PUBLIC entry starts at its last procedure as upstream does.
	procOrder []string

	// curProc is the procedure being compiled, nil at the top
	// level.
	curProc *muf.Proc
	// procStart is where the current procedure's header
	// instruction sits.
	procStart int

	ctrl []control

	// defs holds $define substitutions, as the text they expand
	// to.
	defs map[string]string

	// pending is a pushback queue, which macro expansion feeds.
	pending []token

	// conds tracks open $ifdef blocks whose taken branch is being
	// compiled, so $else and $endif know what they close.
	conds []bool

	// notes collects messages a directive asked to show the
	// compiler, and props the program properties a directive
	// asked to set.
	notes []string
	props []propSet

	line int
}

// Compile turns MUF source into a program.
func Compile(src string, opts Options) (*muf.Program, error) {
	c := &compiler{
		lex:     newLexer(src),
		opts:    opts,
		procs:   map[string]int{},
		publics: map[string]*muf.Public{},
		defs:    map[string]string{},
	}
	if err := c.init(); err != nil {
		return nil, err
	}
	if err := c.run(); err != nil {
		return nil, err
	}
	return c.finish()
}

// init installs the reserved variables and the compile-time
// definitions, in the order init_defs does: the server's built-ins
// first, then what the database supplies, so a world can override a
// built-in.
func (c *compiler) init() error {
	c.vars = append(c.vars, reservedVars()...)

	install := func(name, body string) error {
		// Definitions are kept as text: expansion re-lexes
		// them, so a definition that produces a string
		// literal stays one literal.
		if _, err := lexTokens(body, 0); err != nil {
			return fmt.Errorf("built-in definition %s: %w", name, err)
		}
		c.defs[ascii.Fold(name)] = body
		return nil
	}

	for name, body := range muf.BuiltinDefines {
		if err := install(name, body); err != nil {
			return err
		}
	}
	// These two are literals in the C, built from server values.
	if c.opts.Version != "" {
		if err := install("__version", strconv.Quote(c.opts.Version)); err != nil {
			return err
		}
	}
	if c.opts.MuckName != "" {
		if err := install("__muckname", strconv.Quote(c.opts.MuckName)); err != nil {
			return err
		}
	}
	for name, body := range c.opts.Defines {
		if err := install(name, body); err != nil {
			// A malformed definition in the database must
			// not stop every program from compiling.
			c.notes = append(c.notes,
				"ignoring the stored definition "+name+": "+err.Error())
		}
	}
	return nil
}

func reservedVars() []string {
	return []string{"me", "loc", "trigger", "command"}
}

// errf builds a compile error at the current line.
func (c *compiler) errf(format string, args ...any) error {
	return &Error{Line: c.line, Msg: fmt.Sprintf(format, args...)}
}

// maxSubstitutions bounds macro and define expansion, from
// SUBSTITUTIONS in src/compile.c. It is what stops a definition that
// names itself from looping forever.
const maxSubstitutions = 20

// next returns the next token, expanding definitions and macros.
//
// Expansion happens here rather than when a word is compiled,
// matching upstream's next_token. That ordering is observable: a
// definition shadows a procedure, a variable and a primitive alike,
// because none of them are ever consulted for a name that expanded.
func (c *compiler) next() (token, bool, error) {
	subs := 0
	for {
		tok, ok, err := c.rawNext()
		if err != nil || !ok {
			return tok, ok, err
		}

		// A quoted literal is never expanded.
		if tok.isString {
			return tok, true, nil
		}

		// A leading backslash escapes expansion, so a program
		// can name something that a definition would
		// otherwise have replaced.
		if len(tok.text) > 1 && tok.text[0] == beginEscape {
			tok.text = tok.text[1:]
			return tok, true, nil
		}

		body, expanded := c.expansion(tok.text)
		if !expanded {
			return tok, true, nil
		}
		subs++
		if subs > maxSubstitutions {
			return token{}, false, c.errf("too many macro substitutions")
		}
		toks, err := lexTokens(body, tok.line)
		if err != nil {
			return token{}, false, err
		}
		c.push(toks)
	}
}

// expansion looks a token up as a definition or a macro, returning
// the text it stands for.
func (c *compiler) expansion(word string) (string, bool) {
	if word == "" {
		return "", false
	}
	// A '.' prefix names an entry in the editor's macro table.
	if word[0] == beginMacro && len(word) > 1 {
		body, ok := c.opts.Macros[ascii.Fold(word[1:])]
		return body, ok
	}
	if body, ok := c.defs[ascii.Fold(word)]; ok {
		return body, true
	}
	return "", false
}

// rawNext returns the next token without expanding anything.
func (c *compiler) rawNext() (token, bool, error) {
	if len(c.pending) > 0 {
		tok := c.pending[0]
		c.pending = c.pending[1:]
		c.line = tok.line
		return tok, true, nil
	}
	tok, ok, err := c.lex.next()
	if ok {
		c.line = tok.line
	}
	return tok, ok, err
}

// push puts tokens back, which is how a $define expands.
func (c *compiler) push(toks []token) {
	c.pending = append(append([]token{}, toks...), c.pending...)
}

// emit appends an instruction and returns its address.
func (c *compiler) emit(in muf.Inst) int {
	in.Line = c.line
	c.code = append(c.code, in)
	return len(c.code) - 1
}

// here is the address the next instruction will take.
func (c *compiler) here() int { return len(c.code) }

// run is the main compile loop.
func (c *compiler) run() error {
	for {
		tok, ok, err := c.next()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		if err := c.word(tok); err != nil {
			return err
		}
	}
	if c.curProc != nil {
		return c.errf("unterminated procedure %q", c.curProc.Name)
	}
	if len(c.ctrl) > 0 {
		open := c.ctrl[len(c.ctrl)-1]
		return &Error{Line: open.line,
			Msg: fmt.Sprintf("unterminated %s", open.kind)}
	}
	return nil
}

// word compiles one token.
//
// The order the cases are tried in matters and follows next_word in
// src/compile.c: a name that is both a procedure and a primitive
// resolves to the procedure, and a variable shadows both.
func (c *compiler) word(tok token) error {
	// A quoted literal is always a string, whatever it looks
	// like.
	if tok.isString {
		c.emit(muf.Inst{Type: muf.TypeString, Str: tok.text})
		return nil
	}

	word := tok.text
	if word == "" {
		return nil
	}

	// Directives and macros are handled before anything else,
	// because they change what the following tokens mean.
	if word[0] == beginDirective {
		return c.directive(word)
	}
	if addr, ok := c.procs[ascii.Fold(word)]; ok {
		c.emit(muf.Inst{Type: muf.TypeInteger, Num: int64(addr)})
		c.emit(muf.Inst{Type: muf.TypeExec})
		return nil
	}
	if i, ok := indexOf(c.svars, word); ok {
		c.emit(muf.Inst{Type: muf.TypeSVar, Num: int64(i)})
		return nil
	}
	if i, ok := indexOf(c.lvars, word); ok {
		c.emit(muf.Inst{Type: muf.TypeLVar, Num: int64(i)})
		return nil
	}
	if i, ok := indexOf(c.vars, word); ok {
		c.emit(muf.Inst{Type: muf.TypeVar, Num: int64(i)})
		return nil
	}
	if isSpecial(word) {
		return c.special(word)
	}
	if n := muf.PrimNumber(word); n != 0 {
		if c.curProc == nil {
			return c.errf("%s outside a procedure", word)
		}
		c.emit(muf.Inst{Type: muf.TypePrimitive, Num: int64(n)})
		return nil
	}
	if n, ok := parseInt(word); ok {
		c.emit(muf.Inst{Type: muf.TypeInteger, Num: n})
		return nil
	}
	if f, ok := parseFloat(word); ok {
		c.emit(muf.Inst{Type: muf.TypeFloat, Float: f})
		return nil
	}
	if r, ok := parseObject(word); ok {
		c.emit(muf.Inst{Type: muf.TypeObject, Ref: r})
		return nil
	}
	if strings.HasPrefix(word, "'") {
		return c.quoted(word[1:])
	}
	return c.errf("Unrecognized word %s.", word)
}

// quoted compiles 'name, which pushes a procedure's address rather
// than calling it.
func (c *compiler) quoted(name string) error {
	addr, ok := c.procs[ascii.Fold(name)]
	if !ok {
		return c.errf("unrecognized procedure %s", name)
	}
	c.emit(muf.Inst{Type: muf.TypeAddress, Num: int64(addr)})
	return nil
}

// indexOf finds a name in a variable table, case-insensitively.
func indexOf(names []string, want string) (int, bool) {
	for i, n := range names {
		if ascii.EqualFold(n, want) {
			return i, true
		}
	}
	return -1, false
}

// parseInt reads an integer literal. MUF integers are decimal and may
// be signed; a bare "-" or "+" is a primitive, not a number.
func parseInt(s string) (int64, bool) {
	if s == "" || s == "-" || s == "+" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// parseFloat reads a float literal. A token is only a float when it
// has a decimal point or an exponent, so "3" stays an integer.
func parseFloat(s string) (float64, bool) {
	if !strings.ContainsAny(s, ".eE") {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// parseObject reads a "#123" literal.
func parseObject(s string) (ref.Ref, bool) {
	if len(s) < 2 || s[0] != '#' {
		return ref.Nothing, false
	}
	n, err := strconv.ParseInt(s[1:], 10, 32)
	if err != nil {
		return ref.Nothing, false
	}
	return ref.Ref(n), true
}
