package compiler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
)

// directive handles a $-prefixed compiler directive.
//
// Directives run at compile time and change what the tokens after
// them mean. The ones that read a program's own properties —
// $iflib, $ifver, $ifcancall and their negations — need a live
// database, so they are recognised and skipped rather than evaluated;
// a program that depends on one compiles as though the condition were
// false.
//
// Several of them do not affect the code at all: $author, $note,
// $version, $lib-version, $doccmd, $pubdef and $libdef each ask for a
// property on the program object, which the caller applies from
// Result.Props. Nothing reaches the database from here, so the
// compiler still needs no world.
func (c *compiler) directive(word string) error {
	name := ascii.Fold(strings.TrimPrefix(word, string(beginDirective)))
	if name == "" {
		return c.errf("I don't understand that compiler directive")
	}

	switch name {
	case "define", "def":
		return c.defineDirective(name == "def")
	case "enddef":
		return c.errf("$enddef without $define")
	case "undef":
		tok, ok, err := c.argToken("$undef")
		if err != nil || !ok {
			return err
		}
		delete(c.defs, ascii.Fold(tok.text))
	case "cleardefs":
		// The argument, if any, is ignored; upstream clears
		// everything and reinstates its built-ins.
		_, _, _ = c.argToken("$cleardefs")
		c.defs = map[string]string{}
	case "echo":
		c.notes = append(c.notes, c.lex.restOfLine())
	case "abort":
		return c.errf("%s", c.lex.restOfLine())

	case "ifdef", "ifndef":
		return c.conditional(name == "ifndef")
	case "else":
		// Reached while compiling a taken branch: the other
		// half is the one to discard.
		if len(c.conds) == 0 {
			return c.errf("$else without a matching conditional")
		}
		return c.skipToEndif()
	case "endif":
		if len(c.conds) == 0 {
			return c.errf("$endif without a matching conditional")
		}
		c.conds = c.conds[:len(c.conds)-1]

	case "iflib", "ifnlib":
		tok, ok, err := c.argToken("$" + name)
		if err != nil || !ok {
			return err
		}
		_, found := c.include(tok.text)
		return c.skipConditional(found == (name == "iflib"))

	case "ifver", "ifnver", "iflibver", "ifnlibver":
		return c.ifVersion(name)
	case "ifcancall", "ifncancall":
		return c.ifCanCall(name)

	// Directives that set a property on the program object. The
	// value is recorded so the caller can apply it; none of them
	// affect the code.
	case "author", "note", "version", "lib-version":
		c.props = append(c.props, propSet{
			path:  metaProps[name],
			value: c.lex.restOfLine(),
		})
	case "doccmd":
		// One of the two directives whose value is written
		// with __PROG__ expanded, so "__PROG__ #help" names
		// the program being compiled.
		c.props = append(c.props, propSet{
			path:  metaProps[name],
			value: c.expandProg(c.lex.restOfLine()),
		})
	case "pubdef":
		return c.pubdef()
	case "libdef":
		return c.libdef()

	case "pragma":
		return c.pragma()
	case "entrypoint":
		return c.entrypoint()
	case "language":
		return c.language()

	case "include":
		tok, ok, err := c.argToken("$include")
		if err != nil || !ok {
			return err
		}
		defs, found := c.include(tok.text)
		if !found {
			return c.errf("$include: %s is not a program", tok.text)
		}
		for k, v := range defs {
			c.defs[ascii.Fold(k)] = v
		}

	default:
		return c.errf("I don't understand the compiler directive $%s", name)
	}
	return nil
}

// include resolves a $include or $iflib target through the caller's
// resolver.
func (c *compiler) include(target string) (map[string]string, bool) {
	if c.opts.Include == nil {
		return nil, false
	}
	return c.opts.Include(target)
}

// Directive diagnostics, worded as upstream's so a programmer who
// searches the manual for one finds it. The long ones are split only
// to fit the column limit.
const (
	errPragmaArg      = "Pragma requires at least one argument."
	warnPragmaUnknown = "Warning on line %d: Pragma %.64s " +
		"unrecognized.  Ignoring."
	warnPragmaExtra = "Warning on line %d: Ignoring extra " +
		"pragma arguments: %.256s"

	errEntryArg  = "$entrypoint - function name is required."
	errEntryName = "$entrypoint - unrecognized function " +
		"name '%s'."

	errLangArg    = "$language - argument is required."
	errLangQuotes = "$language - argument must be enclosed " +
		"in double quotes."
	errLangUnknown = "$language - '%s' is not implemented " +
		"on this server."

	errPubdefName = "Unexpected end of file looking for " +
		"$pubdef name."
	errLibdefName = "Unexpected end of file looking for " +
		"$libdef name."
	errDefName = "Invalid %s name.  No /, :, @ nor ~ are allowed."
)

// pragma changes how the rest of the source is parsed.
//
// Only the comment pragmas exist. An unrecognised one is a warning
// rather than an error, so a program written for a server with more
// of them still compiles here.
func (c *compiler) pragma() error {
	tok, ok, err := c.lineArgToken()
	if err != nil {
		return err
	}
	if !ok {
		return c.errf(errPragmaArg)
	}

	switch ascii.Fold(tok.text) {
	case "comment_strict":
		c.lex.comments = commentStrict
	case "comment_recurse":
		c.lex.comments = commentRecurse
	case "comment_loose":
		c.lex.comments = commentLoose
	default:
		c.notes = append(c.notes, fmt.Sprintf(
			warnPragmaUnknown, c.line, tok.text))
		// The rest of the line belonged to a pragma nobody
		// understands, so it is discarded rather than
		// compiled.
		c.lex.restOfLineRaw()
		return nil
	}

	if rest := c.lex.restOfLineRaw(); rest != "" {
		c.notes = append(c.notes, fmt.Sprintf(
			warnPragmaExtra, c.line, rest))
	}
	return nil
}

// entrypoint names the procedure the program starts at instead of the
// last one defined.
//
// The procedure must already have been compiled, because upstream
// searches the procedure list it has built so far — so a
// $entrypoint above its target is an error, not a forward reference.
func (c *compiler) entrypoint() error {
	tok, ok, err := c.lineArgToken()
	if err != nil {
		return err
	}
	if !ok {
		return c.errf(errEntryArg)
	}
	addr, found := c.procs[ascii.Fold(tok.text)]
	if !found {
		return c.errf(errEntryName, tok.text)
	}
	c.altStart = addr
	return nil
}

// language asserts what the source is written in. MUF is the only
// answer this server has, so the directive is a compile-time check
// rather than a switch.
func (c *compiler) language() error {
	tok, ok, err := c.lineArgToken()
	if err != nil {
		return err
	}
	if !ok {
		return c.errf(errLangArg)
	}
	if !tok.isString {
		return c.errf(errLangQuotes)
	}
	if !ascii.EqualFold(tok.text, "muf") {
		return c.errf(errLangUnknown, tok.text)
	}
	return nil
}

// pubdef exports a definition from a library, as a property under
// _defs that $include reads back.
func (c *compiler) pubdef() error {
	tok, ok, err := c.rawNext()
	if err != nil {
		return err
	}
	if !ok {
		return c.errf(errPubdefName)
	}

	// A bare colon clears everything the library exports. It is
	// checked before the name rules, which would otherwise refuse
	// it.
	if tok.text == ":" {
		c.lex.restOfLineRaw()
		c.props = append(c.props,
			propSet{path: definesPropdir, delete: true})
		return nil
	}
	if err := c.checkDefName("$pubdef", tok.text); err != nil {
		return err
	}

	name, keep := strings.CutPrefix(tok.text, string(beginEscape))
	value := c.expandProg(c.lex.restOfLine())
	c.props = append(c.props, propSet{
		path:         definesPropdir + "/" + name,
		value:        value,
		delete:       value == "",
		keepExisting: keep,
	})
	return nil
}

// libdef exports a caller for a public function: the property it
// writes expands, in whoever includes it, to the three words that
// call the function on this program.
//
// This is the part that was missing. Recording the directive without
// writing the property left a library that compiled but exported
// nothing.
func (c *compiler) libdef() error {
	tok, ok, err := c.rawNext()
	if err != nil {
		return err
	}
	if !ok {
		return c.errf(errLibdefName)
	}
	if err := c.checkDefName("$libdef", tok.text); err != nil {
		return err
	}
	// $libdef takes no value; anything after the name is dropped.
	c.lex.restOfLineRaw()

	name, keep := strings.CutPrefix(tok.text, string(beginEscape))
	c.props = append(c.props, propSet{
		path: definesPropdir + "/" + name,
		value: c.opts.Ref.String() + " \"" + name +
			"\" call",
		keepExisting: keep,
	})
	return nil
}

// checkDefName refuses a $pubdef or $libdef name that would land
// somewhere other than one entry under _defs, or that would carry a
// property permission character.
//
// Upstream also tests Prop_System, which is redundant: a system
// property starts with '@', which Prop_Hidden has already refused.
func (c *compiler) checkDefName(what, name string) error {
	bad := strings.ContainsAny(name, "/:") ||
		name != "" && (name[0] == '~' || name[0] == '@')
	if !bad {
		return nil
	}
	return c.errf(errDefName, what)
}

// expandProg substitutes __PROG__ for the program's own dbref, which
// $pubdef and $doccmd do to their values.
func (c *compiler) expandProg(s string) string {
	return strings.ReplaceAll(s, "__PROG__", c.opts.Ref.String())
}

// lineArgToken reads a directive argument that must be on the
// directive's own line.
//
// Upstream's handlers skip whitespace and test for end of line before
// calling next_token_raw, so a directive with nothing after it
// reports its own missing argument rather than swallowing the next
// line's first word.
func (c *compiler) lineArgToken() (token, bool, error) {
	if len(c.pending) == 0 && !c.lex.moreOnLine() {
		return token{}, false, nil
	}
	tok, ok, err := c.rawNext()
	return tok, ok, err
}

// propSet is a property a directive asked to be written on the
// program.
type propSet struct {
	path  string
	value string
	// delete removes the property rather than setting it, which
	// an empty $pubdef value and a bare "$pubdef :" both ask for.
	delete bool
	// keepExisting is the "\name" form: write only when nothing
	// is there already, so a library can offer a default its
	// owner may override.
	keepExisting bool
}

// metaProps are the property names the documentation directives
// write, from include/compile.h.
var metaProps = map[string]string{
	"author":      "_author",
	"doccmd":      "_docs",
	"note":        "_note",
	"version":     "_version",
	"lib-version": "_lib-version",
}

// definesPropdir is DEFINES_PROPDIR: where $pubdef and $libdef put
// what a library exports, and where $include reads it back.
const definesPropdir = "_defs"

// argToken reads a directive's argument.
//
// It reads without expanding, which upstream does with next_token_raw
// and which matters: "$ifdef X" must test whether X is defined, not
// look up whatever X expands to.
func (c *compiler) argToken(what string) (token, bool, error) {
	tok, ok, err := c.rawNext()
	if err != nil {
		return token{}, false, err
	}
	if !ok {
		return token{}, false, c.errf("unexpected end of program looking for %s's argument", what)
	}
	return tok, true, nil
}

// defineDirective reads "$define name ...tokens... $enddef".
//
// The body is kept as tokens rather than text, so a string literal in
// a definition stays one literal rather than being re-lexed and
// re-escaped.
func (c *compiler) defineDirective(short bool) error {
	nameTok, ok, err := c.argToken("$define")
	if err != nil || !ok {
		return err
	}
	name := ascii.Fold(nameTok.text)

	// "$def name body-to-end-of-line" is the one-line form.
	if short {
		c.defs[name] = c.lex.restOfLine()
		return nil
	}

	var body []token
	for {
		// Read without expanding: a definition's body is
		// expanded where it is used, not where it is written.
		tok, ok, err := c.rawNext()
		if err != nil {
			return err
		}
		if !ok {
			return c.errf("unexpected end of program looking for $enddef")
		}
		if !tok.isString &&
			ascii.EqualFold(tok.text, "$enddef") {
			c.defs[name] = joinTokens(body)
			return nil
		}
		body = append(body, tok)
	}
}

// joinTokens renders tokens back to source, re-quoting string
// literals so re-lexing produces the same tokens.
func joinTokens(toks []token) string {
	var b strings.Builder
	for i, t := range toks {
		if i > 0 {
			b.WriteByte(' ')
		}
		if t.isString {
			b.WriteByte(beginString)
			for j := 0; j < len(t.text); j++ {
				switch ch := t.text[j]; ch {
				case endString, beginEscape:
					b.WriteByte(beginEscape)
					b.WriteByte(ch)
				case '\r':
					b.WriteByte(beginEscape)
					b.WriteByte('r')
				case escapeChar:
					b.WriteByte(beginEscape)
					b.WriteByte('[')
				default:
					b.WriteByte(ch)
				}
			}
			b.WriteByte(endString)
			continue
		}
		b.WriteString(t.text)
	}
	return b.String()
}

// lexTokens splits a fragment into tokens, for the one-line $def
// form.
func lexTokens(src string, line int) ([]token, error) {
	l := newLexer(src)
	var out []token
	for {
		tok, ok, err := l.next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return out, nil
		}
		tok.line = line
		out = append(out, tok)
	}
}

// conditional compiles the branch of "$ifdef" that applies.
//
// The condition is either a bare name, true when it is defined, or a
// comparison "name=value", "name>value" or "name<value" against what
// the name expands to. An undefined name makes any comparison false.
func (c *compiler) conditional(negate bool) error {
	tok, ok, err := c.argToken("$ifdef")
	if err != nil || !ok {
		return err
	}
	return c.skipConditional(c.testDefined(tok.text) != negate)
}

// testDefined evaluates an $ifdef condition.
func (c *compiler) testDefined(cond string) bool {
	// The operator is never the first character, so a name may
	// begin with one. This mirrors the C, which starts scanning
	// at index 1.
	op, at := byte(0), -1
	for i := 1; i < len(cond); i++ {
		if ch := cond[i]; ch == '=' || ch == '>' ||
			ch == '<' {
			op, at = ch, i
			break
		}
	}

	name := cond
	var want string
	if at >= 0 {
		name, want = cond[:at], cond[at+1:]
	}

	body, defined := c.defs[ascii.Fold(name)]
	if !defined {
		return false
	}
	if op == 0 {
		return true
	}

	cmp := ascii.Compare(body, want)
	switch op {
	case '=':
		return cmp == 0
	case '>':
		return cmp > 0
	case '<':
		return cmp < 0
	}
	return false
}

// skipConditional keeps the taken branch and discards the other.
//
// When the condition holds, compilation continues and the $else
// branch is skipped when reached. When it does not, tokens are
// discarded until $else or $endif.
func (c *compiler) skipConditional(taken bool) error {
	if taken {
		c.conds = append(c.conds, true)
		return nil
	}
	return c.skipToElseOrEndif()
}

// skipToElseOrEndif discards tokens until the matching $else or
// $endif, counting nested conditionals so an inner one does not end
// an outer.
func (c *compiler) skipToElseOrEndif() error {
	depth := 0
	for {
		tok, ok, err := c.rawNext()
		if err != nil {
			return err
		}
		if !ok {
			return c.errf("unexpected end of program looking for $endif")
		}
		if tok.isString || tok.text == "" ||
			tok.text[0] != beginDirective {
			continue
		}
		switch d := ascii.Fold(tok.text[1:]); {
		case isConditionalDirective(d):
			depth++
		case d == "endif":
			if depth == 0 {
				return nil
			}
			depth--
		case d == "else":
			if depth == 0 {
				// The other branch is the one to
				// compile.
				c.conds = append(c.conds, true)
				return nil
			}
		}
	}
}

// skipToEndif discards the untaken half of a conditional after its
// $else.
func (c *compiler) skipToEndif() error {
	depth := 0
	for {
		tok, ok, err := c.rawNext()
		if err != nil {
			return err
		}
		if !ok {
			return c.errf("unexpected end of program looking for $endif")
		}
		if tok.isString || tok.text == "" ||
			tok.text[0] != beginDirective {
			continue
		}
		switch d := ascii.Fold(tok.text[1:]); {
		case isConditionalDirective(d):
			depth++
		case d == "endif":
			if depth == 0 {
				c.conds = c.conds[:len(c.conds)-1]
				return nil
			}
			depth--
		}
	}
}

// isConditionalDirective reports whether a directive opens a
// conditional.
func isConditionalDirective(d string) bool {
	switch d {
	case "ifdef", "ifndef", "iflib", "ifnlib", "ifver", "ifnver",
		"iflibver", "ifnlibver", "ifcancall", "ifncancall":
		return true
	}
	return false
}

// ifVersion is $ifver, $ifnver, $iflibver and $ifnlibver: compare a
// version property against a number.
//
// The comparison is "is the required version at most the one the
// object has", and both sides are parsed as floats with anything
// unparseable reading as 0.0 — so "$ifver $lib 1.2" is true when
// the library says 1.2 or more. An object with no version property
// reads as "0.0" rather than failing.
//
// Failing to *resolve* the object is a compile error, not a false
// condition, which is the one place these differ from $iflib.
func (c *compiler) ifVersion(name string) error {
	target, ok, err := c.argToken("$" + name)
	if err != nil || !ok {
		return err
	}
	lib := name == "iflibver" || name == "ifnlibver"

	have, found := "", false
	if c.opts.ObjVersion != nil {
		have, found = c.opts.ObjVersion(target.text, lib)
	}
	if !found {
		if c.opts.ObjVersion == nil {
			// No host to ask. Recorded rather than
			// failing the compile, since a program using
			// it would otherwise not build at all.
			_, _, _ = c.argToken("$" + name)
			c.notes = append(c.notes, "$"+name+
				" was treated as false: no database"+
				" to read a version from")
			return c.skipConditional(false)
		}
		return c.errf("I don't understand what object you " +
			"want to check with $ifver.")
	}

	want, ok, err := c.argToken("$" + name)
	if err != nil || !ok {
		return err
	}
	if want.text == "" {
		return c.errf("I don't understand what version you " +
			"want to compare to with $ifver.")
	}
	c.lex.restOfLine()

	met := parseVersion(want.text) <= parseVersion(have)
	if name == "ifnver" || name == "ifnlibver" {
		met = !met
	}
	return c.skipConditional(met)
}

// parseVersion reads a version the way upstream does: sscanf with
// "%lg", and 0.0 for anything that is not a float at all.
func parseVersion(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

// ifCanCall is $ifcancall and $ifncancall: whether the program being
// compiled may call a named public function of another object.
//
// Upstream's test has four parts, all of which need the *target's*
// compiled publics — it compiles the target if it has to — so the
// whole question is asked of the host through Options.CanCall. What
// the compiler keeps is the argument handling and which way round the
// answer goes.
func (c *compiler) ifCanCall(name string) error {
	target, ok, err := c.argToken("$" + name)
	if err != nil || !ok {
		return err
	}

	fn, ok, err := c.argToken("$" + name)
	if err != nil || !ok {
		return err
	}
	if fn.text == "" {
		return c.errf("I don't understand what function " +
			"you want to check for.")
	}
	c.lex.restOfLine()

	if c.opts.CanCall == nil {
		c.notes = append(c.notes, "$"+name+
			" was treated as false: no database was "+
			"available to check a public function")
		return c.skipConditional(false)
	}
	can, found := c.opts.CanCall(target.text, fn.text)
	if !found {
		return c.errf("I don't understand what object you " +
			"want to check in ifcancall.")
	}
	if name == "ifncancall" {
		can = !can
	}
	return c.skipConditional(can)
}
