package boolexp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Lock syntax tokens, from fuzzball/include/game.h.
const (
	andToken    = '&'
	orToken     = '|'
	notToken    = '!'
	numberToken = '#'
)

// propDelimiter separates a property name from its expected value in
// a PROP_DELIMITER expression ("name:value"). propDirDelimiter
// separates path segments, for the hidden/system property checks.
const (
	propDelimiter    = ':'
	propDirDelimiter = '/'
)

// ParseError is what Parse returns on failure.
//
// Notify is true when upstream's own parser would have shown Msg to
// the player itself — a match failure or the hidden-property
// permission check, both driven by notify() calls inside
// parse_boolexp_F — so a caller should forward it regardless of
// whatever else it goes on to report. It is false for a bare syntax
// error (unbalanced parentheses, an empty property name or value),
// which upstream's parser fails on silently, producing TRUE_BOOLEXP
// with no message to the player at all.
type ParseError struct {
	Msg    string
	Notify bool
}

func (e *ParseError) Error() string { return e.Msg }

// isSpace matches C's isspace() in the "C" locale, which is what
// upstream's skip_whitespace and remove_ending_whitespace use.
func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	default:
		return false
	}
}

// parser walks a lock string left to right, mirroring the C's
// pointer-based parse_boolexp_E/_T/_F family.
type parser struct {
	s      string
	i      int
	host   Host
	descr  int
	player ref.Ref
	dbload bool
}

func (p *parser) skipWhitespace() {
	for p.i < len(p.s) && isSpace(p.s[p.i]) {
		p.i++
	}
}

func (p *parser) peek() byte {
	if p.i >= len(p.s) {
		return 0
	}
	return p.s[p.i]
}

// Parse compiles a lock string into an expression tree.
//
// dbload matches upstream's dbloadp: false is the ordinary path,
// which resolves bare names against player's surroundings via
// host.Match — this is what runs when a player types a lock
// expression, e.g. "@lock" or SETLOCKSTR. true is the disk-loader
// path, which trusts a "#123" dbref literal outright and performs no
// matching; that is what Emerald always uses to re-parse an
// already-stored lock property, since properties hold locks in the
// unparsed dbref form Unparse produces (see the package doc comment).
//
// On failure this returns (nil, err) with err's message worded
// exactly as upstream's notify calls — TRUE_BOOLEXP with a message
// to the player, in the C. Emerald returns the message as an error
// instead so the caller decides whether and how to tell the player.
func Parse(host Host, descr int, player ref.Ref, s string, dbload bool) (*Expr, error) {
	p := &parser{s: s, host: host, descr: descr, player: player, dbload: dbload}
	b, err := p.parseE()
	if err != nil {
		return nil, err
	}
	return b, nil
}

// parseE is parse_boolexp_E: the entry point, handling '|'.
func (p *parser) parseE() (*Expr, error) {
	b, err := p.parseT()
	if err != nil {
		return nil, err
	}
	p.skipWhitespace()
	if p.peek() == orToken {
		p.i++
		b2, err := p.parseE()
		if err != nil {
			return nil, err
		}
		return &Expr{Kind: Or, Sub1: b, Sub2: b2}, nil
	}
	return b, nil
}

// parseT is parse_boolexp_T: handles '&'.
func (p *parser) parseT() (*Expr, error) {
	b, err := p.parseF()
	if err != nil {
		return nil, err
	}
	p.skipWhitespace()
	if p.peek() == andToken {
		p.i++
		b2, err := p.parseT()
		if err != nil {
			return nil, err
		}
		return &Expr{Kind: And, Sub1: b, Sub2: b2}, nil
	}
	return b, nil
}

// parseF is parse_boolexp_F: parentheses, '!', and the atomic
// dbref/prop tokens.
func (p *parser) parseF() (*Expr, error) {
	p.skipWhitespace()

	switch p.peek() {
	case '(':
		p.i++
		b, err := p.parseE()
		if err != nil {
			return nil, err
		}
		p.skipWhitespace()
		if b == nil || p.i >= len(p.s) || p.s[p.i] != ')' {
			return nil, &ParseError{Msg: "unbalanced parentheses in lock"}
		}
		p.i++
		return b, nil

	case notToken:
		p.i++
		sub, err := p.parseF()
		if err != nil {
			return nil, err
		}
		return &Expr{Kind: Not, Sub1: sub}, nil

	default:
		start := p.i
		for p.i < len(p.s) && p.s[p.i] != andToken &&
			p.s[p.i] != orToken && p.s[p.i] != ')' {
			p.i++
		}
		buf := strings.TrimRightFunc(p.s[start:p.i], func(r rune) bool { return isSpace(byte(r)) })

		if idx := strings.IndexByte(buf, propDelimiter); idx >= 0 {
			if !p.dbload {
				if isSystemProp(buf) ||
					(!p.host.Wizard(p.player) && isHiddenProp(buf)) {
					return nil, &ParseError{
						Msg:    "Permission denied. (You cannot use a hidden property in a lock.)",
						Notify: true,
					}
				}
			}
			return parseProp(buf)
		}

		if !p.dbload {
			thing := p.host.Match(p.player, buf)
			switch thing {
			case ref.Nothing:
				return nil, &ParseError{Msg: fmt.Sprintf("I don't see %s here.", buf), Notify: true}
			case ref.Ambiguous:
				return nil, &ParseError{Msg: fmt.Sprintf("I don't know which %s you mean!", buf), Notify: true}
			default:
				return &Expr{Kind: Const, Thing: thing}, nil
			}
		}

		if len(buf) < 2 || buf[0] != numberToken {
			return nil, &ParseError{Msg: fmt.Sprintf("invalid dbref %q in lock", buf)}
		}
		n, err := strconv.Atoi(buf[1:])
		if err != nil {
			return nil, &ParseError{Msg: fmt.Sprintf("invalid dbref %q in lock", buf)}
		}
		return &Expr{Kind: Const, Thing: ref.Ref(n)}, nil
	}
}

// parseProp is parse_boolprop: "name:value" into a PROP node. Unlike
// upstream's fixed BUFFER_LEN scan, Go strings need no length cap.
func parseProp(buf string) (*Expr, error) {
	idx := strings.IndexByte(buf, propDelimiter)
	name := strings.TrimRightFunc(buf[:idx], func(r rune) bool { return isSpace(byte(r)) })
	if name == "" {
		return nil, &ParseError{Msg: "empty property name in lock"}
	}

	rest := buf[idx+1:]
	rest = strings.TrimLeftFunc(rest, func(r rune) bool { return isSpace(byte(r)) })
	// A value cannot contain spaces — upstream stops at the
	// first one.
	if sp := strings.IndexFunc(rest, func(r rune) bool { return isSpace(byte(r)) }); sp >= 0 {
		rest = rest[:sp]
	}
	if rest == "" {
		return nil, &ParseError{Msg: "empty property value in lock"}
	}

	return &Expr{Kind: Prop, PropName: name, PropValue: rest}, nil
}

// isHiddenProp reports whether any path segment of name starts with
// '@', upstream's Prop_Hidden.
func isHiddenProp(name string) bool { return propCheck(name, '@') }

// isSystemProp reports whether name is under "@__sys__", upstream's
// Prop_System (is_prop_prefix(name, "@__sys__")).
func isSystemProp(name string) bool {
	return isPropPrefix(name, "@__sys__")
}

// propCheck is upstream's Prop_Check: true if 'what' is the first
// character of name or of any path segment after a '/'.
func propCheck(name string, what byte) bool {
	if len(name) > 0 && name[0] == what {
		return true
	}
	for i := 0; i < len(name); i++ {
		if name[i] == propDirDelimiter && i+1 < len(name) &&
			name[i+1] == what {
			return true
		}
	}
	return false
}

// isPropPrefix is upstream's is_prop_prefix: true if property, with
// leading slashes stripped, starts with prefix (also slash-stripped)
// followed by either the end of the string or another slash.
func isPropPrefix(property, prefix string) bool {
	property = strings.TrimLeft(property, string(propDirDelimiter))
	prefix = strings.TrimLeft(prefix, string(propDirDelimiter))

	if !strings.HasPrefix(property, prefix) {
		return false
	}
	rest := property[len(prefix):]
	return rest == "" || rest[0] == propDirDelimiter
}
