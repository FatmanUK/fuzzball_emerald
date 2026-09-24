package game

import (
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

func init() {
	register("@sanity", (*Server).cmdSanity)
	register("@sanfix", (*Server).cmdSanfix)
	register("@sanchange", (*Server).cmdSanchange)
}

// cmdSanity checks the object graph for inconsistency and reports
// what it finds, changing nothing.
func (s *Server) cmdSanity(c *ctx) {
	if !s.requireGod(c, "@sanity") {
		return
	}
	found := 0
	c.w.Check(c.send, func(v world.Violation) {
		found++
		c.tell("Object \"%s\" %s!", unparse(c.w, ref.Nothing, v.Ref), v.Problem)
	})
	c.tell("Done.")
	s.statusLog().Info("sanity check", "violations", found,
		"by", c.who.String(), "byName", nameOf(c.w, c.who))
}

// cmdSanfix repairs what @sanity reports.
func (s *Server) cmdSanfix(c *ctx) {
	if !s.requireGod(c, "@sanfix") {
		return
	}
	log, unfixed := c.w.Fix()
	for _, line := range log {
		c.send(line)
	}
	for _, v := range unfixed {
		c.tell("Object %q %s!", unparse(c.w, ref.Nothing, v.Ref), v.Problem)
	}

	// The repair log goes to the server's log as well as to the
	// screen: a database that needed repairing is something an
	// operator will want to look at again afterwards.
	s.securityLog().Warn("database repaired",
		"changes", len(log), "unfixed", len(unfixed),
		"by", c.who.String(), "byName", nameOf(c.w, c.who))
	for _, line := range log {
		s.statusLog().Info("sanfix", "change", line)
	}

	if len(unfixed) > 0 {
		c.tell("Database repair complete, however the database is still corrupt.  Please re-run @sanity.")
		return
	}
	c.tell("Database repair complete, please re-run @sanity.")
}

// requireNotForced refuses a command that arrived through @force. A
// command that can rewrite ownership must be typed by the person
// taking responsibility for it, not reached through something they
// were tricked into forcing.
func (s *Server) requireNotForced(c *ctx, cmd string) bool {
	if s.forceDepth == 0 {
		return true
	}
	c.tell("You can't use %s from a @force or {force}.", cmd)
	return false
}

// cmdSanchange edits one reference on one object by hand.
//
// This is the tool of last resort, for damage @sanfix cannot work out
// how to repair. It does no validation at all, which is the point: it
// can put the database into a state nothing else can, and can just as
// easily make things worse.
func (s *Server) cmdSanchange(c *ctx) {
	if !s.requireGod(c, "@sanchange") ||
		!s.requireNotForced(c, "@sanchange") {
		return
	}
	fields := strings.Fields(c.arg)
	if len(fields) != 3 {
		s.sanchangeHelp(c)
		return
	}
	target, _ := parseSanRef(fields[0])
	value, _ := parseSanRef(fields[2])

	// Upstream reads both numbers with sscanf and does not check
	// that they parsed, so anything unreadable comes through as
	// whatever was in the variable — which for the target means
	// zero, and is then rejected by the range check below.
	if !c.w.Valid(target) {
		c.tell("## %d is an invalid dbref.", int32(target))
		return
	}

	o := c.w.Get(target)
	name := ascii.Fold(fields[1])
	field, label, ok := sanField(o, name)
	if !ok {
		s.sanchangeHelp(c)
		return
	}
	if field == nil {
		// Only players and things have a home. Upstream
		// writes this complaint to the server's own output
		// rather than to the player, so the player sees
		// nothing at all; that silence is reproduced, because
		// a transcript is compared against it.
		s.log.Info("@sanchange: object has no home to set",
			"object", target.String())
		return
	}

	was := *field
	*field = value
	c.w.Modified(target)

	s.securityLog().Warn("sanchange",
		"object", target.String(), "field", name,
		"from", was.String(), "to", value.String(),
		"by", c.who.String(), "byName", nameOf(c.w, c.who))

	c.tell("## Setting #%d's %s %s", int32(target), label,
		unparse(c.w, ref.Nothing, value))
	c.tell("## Old value was %s", unparse(c.w, ref.Nothing, was))
}

// sanchangeHelp prints the field list.
func (s *Server) sanchangeHelp(c *ctx) {
	c.tell("@sanchange <dbref> <field> <object>")
	for _, line := range []string{
		"Fields are:     exits       Start of Exits list.",
		"                contents    Start of Contents list.",
		"                next        Next object in list.",
		"                location    Object's Location.",
		"                home        Object's Home.",
		"                owner       Object's Owner.",
	} {
		c.send(line)
	}
}

// sanField points at the reference a field name names, along with the
// label the change is reported under. A nil field with ok set means
// the object has no such reference to change.
//
// The label for "exits" says "next field", which is wrong and is
// upstream's: the two cases were written by copying one from the
// other. It is reproduced because the message is what a transcript is
// compared against.
func sanField(o *world.Object, name string) (*ref.Ref, string, bool) {
	switch name {
	case "next":
		return &o.Next, "next field to", true
	case "exits":
		return &o.Exits, "next field to", true
	case "contents":
		return &o.Contents, "Contents list start to", true
	case "location":
		return &o.Location, "location to", true
	case "owner":
		return &o.Owner, "owner to", true
	case "home":
		if o.Type() != ref.TypePlayer &&
			o.Type() != ref.TypeThing {
			return nil, "", true
		}
		return &o.Home, "home to:", true
	}
	return nil, "", false
}

// parseSanRef reads a dbref, with or without its '#'.
func parseSanRef(s string) (ref.Ref, bool) {
	n, err := strconv.ParseInt(strings.TrimPrefix(s, "#"), 10, 32)
	if err != nil {
		return ref.Nothing, false
	}
	return ref.Ref(n), true
}

// requireGod gates the commands that can damage the database
// outright. These are God-only upstream rather than wizard-only,
// because a wizard who could run them could rewrite ownership and so
// make themselves God.
func (s *Server) requireGod(c *ctx, cmd string) bool {
	if c.who == ref.God {
		return true
	}
	s.securityLog().Warn("refused a God-only command",
		"command", cmd, "by", c.who.String(), "byName", nameOf(c.w, c.who))
	c.tell("You are not allowed to %s.", cmd)
	return false
}
