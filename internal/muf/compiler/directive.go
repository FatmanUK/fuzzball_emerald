package compiler

import (
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

	// Conditionals that need more of a live server than the
	// compiler is given. Treated as false, taking the $else
	// branch when there is one.
	case "ifver", "ifnver", "iflibver", "ifnlibver", "ifcancall", "ifncancall":
		// These take two arguments: an object and a version
		// or name.
		_, _, _ = c.argToken("$" + name)
		_, _, _ = c.argToken("$" + name)
		c.notes = append(c.notes,
			"$"+name+" was treated as false: it needs more than the compiler is given")
		return c.skipConditional(false)

	// Directives that set a property on the program object. The
	// value is recorded so the caller can apply it; none of them
	// affect the code.
	case "author", "note", "version", "lib-version", "libdef", "pubdef", "doccmd":
		c.props = append(c.props, propSet{name: name, value: c.lex.restOfLine()})

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

// propSet is a property a directive asked to be written on the
// program.
type propSet struct {
	name  string
	value string
}

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
