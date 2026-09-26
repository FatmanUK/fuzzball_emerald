package game

import (
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
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

func init() {
	register("@examine", (*Server).cmdExamineSanity)
	register("@debug", (*Server).cmdDebug)
}

// cmdExamineSanity is do_examine_sanity (sanity.c:187), the fourth of
// the @san family: every raw field of one object, and everything in
// the database that points at it.
//
// It is not `examine`. That one renders an object for a player; this
// prints the fields a chain repair would act on, which is what makes
// it useful on a world that will not boot. The names are unparsed
// with **no viewer** — unparse_object(NOTHING, ...) — so every
// dbref shows, whatever the flags say.
func (s *Server) cmdExamineSanity(c *ctx) {
	// There is no "here" default: an empty argument goes straight
	// to the matcher and fails, which is upstream's own behaviour
	// and not the same as `examine`'s.
	name := strings.TrimSpace(c.arg)
	d := match.New(c.w, c.who, name).Everything().Result()
	if !noisyMatch(c, name, d) {
		return
	}

	o := c.w.Get(d)
	if o == nil || o.Type() == ref.TypeGarbage {
		c.tell("Object:         *GARBAGE* %s", d)
	} else {
		c.tell("Object:         %s", sanName(c.w, d))
	}
	if o == nil {
		c.tell("Done.")
		return
	}

	c.tell("  Owner:          %s", sanName(c.w, o.Owner))
	c.tell("  Location:       %s", sanName(c.w, o.Location))
	c.tell("  Contents Start: %s", sanName(c.w, o.Contents))
	c.tell("  Exits Start:    %s", sanName(c.w, o.Exits))
	c.tell("  Next:           %s", sanName(c.w, o.Next))

	switch o.Type() {
	case ref.TypeThing:
		c.tell("  Home:           %s", sanName(c.w, o.Home))
		c.tell("  Value:          %d", valueOf(c.w, d))
	case ref.TypeRoom:
		c.tell("  Drop-to:        %s", sanName(c.w, o.Dropto))
	case ref.TypePlayer:
		c.tell("  Home:           %s", sanName(c.w, o.Home))
		c.tell("  Pennies:        %d", valueOf(c.w, d))
	case ref.TypeExit:
		c.tell("  Links:")
		for _, dest := range o.Dest {
			c.tell("    %s", sanName(c.w, dest))
		}
	}

	// Every object whose chain fields point here, which is what a
	// damaged chain looks like from the other end.
	c.tell("Referring Objects:")
	c.w.Each(func(other *world.Object) bool {
		if other.Contents == d {
			c.tell("  By contents field: %s",
				sanName(c.w, other.Ref))
		}
		if other.Exits == d {
			c.tell("  By exits field:    %s",
				sanName(c.w, other.Ref))
		}
		if other.Next == d {
			c.tell("  By next field:     %s",
				sanName(c.w, other.Ref))
		}
		return true
	})
	c.tell("Done.")
}

// sanName unparses a ref with no viewer, which is what the sanity
// commands do: SanPrintObject passes NOTHING as the player, so the
// flags and the dbref always show whoever is looking.
func sanName(w *world.World, r ref.Ref) string {
	return unparse(w, ref.Nothing, r)
}

// cmdDebug is do_debug (wiz.c:1309), whose only option is "display
// propcache" and only under DISKBASE.
//
// Emerald has no diskbase — properties live in memory and in
// Postgres — so every argument reaches the same answer, which is
// exactly what upstream compiled without DISKBASE does. The command
// is here rather than declined because it is not missing: this *is*
// its behaviour.
func (s *Server) cmdDebug(c *ctx) {
	c.tell("Unrecognized option.")
}
