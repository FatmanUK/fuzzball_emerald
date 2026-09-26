package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// Command resolution is upstream's do_command dispatcher, ported.
//
// It had to be ported rather than approximated. Emerald used to take
// any unique prefix over its whole command table and answer nothing
// when a prefix was ambiguous; upstream switches on one character at
// a time and applies a *different* tie-break at each node —
// string_prefix, strcmp, strcasecmp, a length test, and one node with
// string_prefix's arguments reversed. The two algorithms agree often
// enough to look the same and disagree in ways nobody notices until a
// command does the wrong thing: "@chown" once resolved to
// "@chown_lock" here and quietly set a lock where a wizard meant to
// transfer ownership.
//
// commandTable (dispatch_table.go) carries every name upstream
// dispatches, in the order its switch visits them, with the minimum
// abbreviation the trie commits to. Resolution is a linear scan
// taking the first entry that accepts the typed word, which comes to
// the same answer: min encodes the depth at which the trie stopped
// branching, so a word too short to reach a leaf matches nothing,
// exactly as falling off the switch does.

// cmdRule is how a typed word is matched against a command's name.
type cmdRule uint8

const (
	// rulePrefix is upstream's Matched(): any case-insensitive
	// prefix of the name, subject to the entry's min and max.
	rulePrefix cmdRule = iota
	// rFold is strcasecmp: the whole name, case-insensitively.
	rFold
	// rExact is strcmp: the whole name, and case-sensitively, so
	// "@SHUTDOWN" is not "@shutdown". That looks like a bug and
	// is upstream's, for the commands that end a server.
	rExact
	// rStartsWith accepts anything beginning with the name,
	// trailing text included. Only "move" is reached this way.
	rStartsWith
)

// perm is the set of guards upstream applies at the dispatch site
// rather than inside the command, which is why they live on the table
// and not in the handlers.
type perm uint16

const (
	pGuest   perm = 1 << iota // NOGUEST
	pBuild                    // BUILDERONLY
	pMuck                     // MUCKERONLY
	pPlayer                   // PLAYERONLY
	pWiz                      // WIZARDONLY
	pGod                      // GODONLY
	pNoForce                  // NOFORCE
)

// command is one row of the dispatch table.
//
// The field names are short because there are a hundred of these and
// the table is generated; see commandTable's own comment.
type command struct {
	n    string
	rule cmdRule
	// min is the shortest abbreviation that reaches this command,
	// which is how deep upstream's switch commits before it gets
	// here. Zero means any prefix will do.
	min int
	// max is the longest, which only the three names sharing a
	// stem with a lock command need: upstream splits @chown from
	// @chown_lock on strlen(command) < 7.
	max int
	p   perm
}

// accepts reports whether a typed word reaches this command.
func (c command) accepts(word string) bool {
	switch c.rule {
	case rFold:
		return ascii.EqualFold(word, c.n)
	case rExact:
		return word == c.n
	case rStartsWith:
		return len(word) >= len(c.n) &&
			ascii.EqualFold(word[:len(c.n)], c.n)
	}
	if len(word) < c.min {
		return false
	}
	if c.max > 0 && len(word) > c.max {
		return false
	}
	return ascii.HasPrefix(c.n, word)
}

// handlers maps a command name to its implementation. Commands
// register themselves here in init; a name with no entry is one
// upstream has and this server does not, which still resolves so that
// the abbreviations either side of it stay correct.
var handlers = map[string]handler{}

// register attaches a handler to a name in commandTable. It panics on
// a name the table does not carry, because that is a typo rather than
// a runtime condition — the table is the whole command surface.
func register(name string, h handler) {
	if !knownCommand(name) {
		panic("registering a handler for " + name +
			", which is not a command Fuzzball dispatches")
	}
	handlers[name] = h
}

func knownCommand(name string) bool {
	for _, c := range commandTable {
		if c.n == name {
			return true
		}
	}
	return false
}

// resolve finds the command a typed word reaches, reporting false
// when nothing does — which is upstream's "goto bad", and what the
// huh_mesg parameter answers.
func resolve(word string) (command, bool) {
	for _, c := range commandTable {
		if c.accepts(word) {
			return c, true
		}
	}
	return command{}, false
}

// permitted checks a resolved command's guards and reports whether to
// run it, having already said why not.
//
// The wording is upstream's, from the macros in include/db.h, and the
// order is theirs too: a command carrying several guards reports the
// first that fails in the order they are written at the dispatch
// site.
func (s *Server) permitted(c *ctx, cmd command) bool {
	who := c.who
	owner := ownerOf(c.w, who)
	o := c.w.Get(who)
	if o == nil {
		return false
	}

	switch {
	case cmd.p&pGuest != 0 && isGuest(c.w, who):
		c.tell("Guests are not allowed to %s.", cmd.n)
	case cmd.p&pNoForce != 0 && s.forceDepth > 0:
		c.tell("You can't use %s from a @force or {force}.", cmd.n)
	case cmd.p&pBuild != 0 && !canBuild(c.w, owner):
		c.tell("Only builders are allowed to %s.", cmd.n)
	case cmd.p&pMuck != 0 && !isMucker(c.w, owner):
		c.tell("Only programmers are allowed to %s.", cmd.n)
	case cmd.p&pPlayer != 0 && o.Type() != ref.TypePlayer:
		c.tell("Only players are allowed to %s.", cmd.n)
	case cmd.p&(pWiz|pGod) != 0 && !isWizard(c.w, owner):
		c.tell("You are not allowed to %s.", cmd.n)
		// Audited rather than merely refused: one of these is
		// a typo, and a run of them from one player is
		// somebody trying the doors. This used to live in
		// Server.requireWizard, which the table now shadows.
		s.securityLog().Warn("refused a wizard command",
			"player", c.who.String(),
			"name", nameOf(c.w, c.who), "command", cmd.n)
	default:
		return true
	}
	return false
}

// canBuild is upstream's Builder macro: the BUILDER bit or
// wizardhood.
func canBuild(w *world.World, who ref.Ref) bool {
	o := w.Get(who)
	return o != nil && o.Flags.CanBuild()
}

// isMucker is upstream's Mucker macro: any mucker level at all.
func isMucker(w *world.World, who ref.Ref) bool {
	o := w.Get(who)
	return o != nil && o.Flags.MLevel() > 0
}

// declined are the commands this server will not implement, and why.
//
// They stay in the table for the same reason every other unported
// name does — dropping one widens the abbreviations around it —
// but "not yet" would be a promise, and these are decisions. Each is
// recorded in docs/upstream-coverage.md too.
var declined = map[string]string{
	"@memory": "it reports the C allocator's own mallinfo " +
		"counters, which Go has no equivalent of",
	"@usage": "it reports getrusage counters, which Go has no " +
		"equivalent of",
	"@reconfiguressl": "TLS is configured from the environment, " +
		"because a TLS-only server cannot read its listener " +
		"settings from a database it has not opened",
	"@tops": "this server does not profile programs, which is " +
		"also why examine reports no cumulative runtime",
	"@teledump": "it base64-encodes the flat-file dump over " +
		"the connection, and this server has no dump " +
		"file to send — the world lives in Postgres",
}

// dispatch runs one resolved command, or reports that this server
// does not implement it.
//
// A name in the table with no handler is deliberate rather than an
// omission: it keeps the abbreviation space upstream's, and it tells
// somebody who types it what is actually going on. Answering "Huh?"
// would be both less true and less useful.
func (s *Server) dispatch(c *ctx, cmd command) {
	// The guards come first, before the handler is even looked
	// for: upstream applies them at the dispatch site, so what a
	// guest or a non-wizard is told does not depend on whether
	// this server happens to implement the command.
	if !s.permitted(c, cmd) {
		return
	}
	h, ok := handlers[cmd.n]
	if !ok {
		if why, no := declined[cmd.n]; no {
			c.tell("%s is not available on this server: %s.",
				cmd.n, why)
			return
		}
		c.tell("%s is a Fuzzball command this server does not "+
			"implement yet.", cmd.n)
		return
	}
	h(s, c)
}

// trimVerb splits a line into its command word and the rest, which is
// what upstream does before it reaches the switch.
func trimVerb(line string) (verb, rest string) {
	line = strings.TrimLeft(line, " \t")
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.TrimLeft(line[i+1:], " \t")
}
