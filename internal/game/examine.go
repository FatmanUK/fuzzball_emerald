package game

import (
	"fmt"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/boolexp"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// timeFormat is what strftime("%c %Z") produces in the C locale, which is
// where upstream's timestamps come from. The day is space-padded, so a single
// digit leaves two spaces after the month.
const timeFormat = "Mon Jan _2 15:04:05 2006 MST"

// cmdExamine shows everything about an object to someone who may see it.
//
// With an argument after '=' it lists properties instead, which is how the
// whole property tree is read: "examine me=/" lists the root, and "**" walks
// the tree.
func (s *Server) cmdExamine(c *ctx) {
	name, dir, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)

	target := c.w.Get(c.who).Location
	if name != "" && !ascii.EqualFold(name, "here") {
		target = match.New(c.w, c.who, name).Everything().Player().Result()
		if !noisyMatch(c, name, target) {
			return
		}
	}
	if c.w.Get(target) == nil {
		c.tell("I don't understand '%s'.", name)
		return
	}

	// Someone who could not link to it and cannot pass its read lock is
	// told only who owns it. That is the whole privacy model for examine.
	if !s.canLink(c.w, c.who, target) && !s.passesReadLock(c, target) {
		s.printOwner(c, target)
		return
	}

	if dir != "" {
		n := s.listProps(c, target, "", dir)
		c.tell("%d propert%s listed.", n, pluralY(n))
		return
	}
	s.examineObject(c, target)
}

// pluralY is the plural examine uses for "property".
func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// printOwner is all someone who may not examine an object is told.
func (s *Server) printOwner(c *ctx, target ref.Ref) {
	o := c.w.Get(target)
	switch o.Type() {
	case ref.TypePlayer:
		c.tell("%s is a player.", o.Name)
	case ref.TypeGarbage:
		c.tell("%s is garbage.", o.Name)
	default:
		c.tell("Owner: %s", nameOf(c.w, o.Owner))
	}
}

// examineObject prints the full report.
func (s *Server) examineObject(c *ctx, target ref.Ref) {
	w, o := c.w, c.w.Get(target)
	unp := func(r ref.Ref) string { return unparse(w, c.who, r) }

	// The heading differs by type: what a room is parented to, what a
	// thing is worth, how much money a player has.
	switch o.Type() {
	case ref.TypeRoom:
		c.tell("%s  Owner: %s  Parent: %s",
			unp(target), nameOf(w, o.Owner), unp(o.Location))
	case ref.TypeThing:
		c.tell("%s  Owner: %s  Value: %d",
			unp(target), nameOf(w, o.Owner), valueOf(w, target))
	case ref.TypePlayer:
		c.tell("%s  %s: %d  ",
			unp(target), w.Tune.String("cpennies"), valueOf(w, target))
	case ref.TypeGarbage:
		c.send(unp(target))
	default:
		c.tell("%s  Owner: %s", unp(target), nameOf(w, o.Owner))
	}

	c.send(flagDescription(o))
	if desc := getMesg(w, target, propDesc); desc != "" {
		c.send(desc)
	}

	// Locks are shown by the name each one is known by, and only when set.
	for _, lk := range []struct{ label, path string }{
		{"Key", propLock},
		{"Link_OK Key", propLinkLock},
		{"Chown_OK Key", propChownLock},
		{"Container Key", propConLock},
		{"Force Key", propForceLock},
		{"Read Key", propReadLock},
		{"Ownership Key", propOwnLock},
	} {
		if v := lockString(w, target, lk.path); v != unlockedValue {
			c.tell("%s: %s", lk.label, v)
		}
	}

	for _, m := range []struct{ label, path string }{
		{"Success", propSucc}, {"Fail", propFail}, {"Drop", propDrop},
		{"Osuccess", propOSucc}, {"Ofail", propOFail}, {"Odrop", propODrop},
		{"Doing", propDoing}, {"Oecho", propRoomEcho}, {"Pecho", propPuppetEcho},
		{"Idesc", propIDesc},
	} {
		if v := getMesg(w, target, m.path); v != "" {
			c.tell("%s: %s", m.label, v)
		}
	}

	c.tell("Created:  %s", o.Created.Format(timeFormat))
	c.tell("Modified: %s", o.Modified.Format(timeFormat))
	c.tell("Lastused: %s", o.LastUsed.Format(timeFormat))

	if o.Type() == ref.TypeProgram {
		// Instances counts how many copies are running, which the
		// process queue knows.
		c.tell("Usecount: %d     Instances: %d",
			o.UseCount, len(s.procs.forProgram(target)))
	} else {
		c.tell("Usecount: %d", o.UseCount)
	}

	c.tell("[ Use 'examine <object>=/' to list root properties. ]")
	c.tell("Memory used: %d bytes", sizeOfObject(o))

	if contents := w.Contents(target); len(contents) > 0 {
		if o.Type() == ref.TypePlayer {
			c.tell("Carrying:")
		} else {
			c.tell("Contents:")
		}
		for _, r := range contents {
			c.send(unp(r))
		}
	}

	switch o.Type() {
	case ref.TypeRoom:
		if exits := w.Exits(target); len(exits) > 0 {
			c.tell("Exits:")
			for _, r := range exits {
				c.send(unp(r))
			}
		} else {
			c.tell("No exits.")
		}
		if o.Dropto != ref.Nothing {
			c.tell("Dropped objects go to: %s", unp(o.Dropto))
		}

	case ref.TypeThing, ref.TypePlayer:
		c.tell("Home: %s", unp(o.Home))
		s.tellLocation(c, o)
		if exits := w.Exits(target); len(exits) > 0 {
			c.tell("Actions/exits:")
			for _, r := range exits {
				c.send(unp(r))
			}
		} else {
			c.tell("No actions attached.")
		}

	case ref.TypeExit:
		if o.Location != ref.Nothing {
			c.tell("Source: %s", unp(o.Location))
		}
		for _, d := range o.Dest {
			if d == ref.Nothing {
				continue
			}
			c.tell("Destination: %s", unp(d))
		}

	case ref.TypeProgram:
		// Reported from the compile cache, never by compiling: examine
		// says whether a program is compiled, and compiling it to find
		// out would make the answer always yes.
		if cached, ok := s.programs[target]; ok && cached.prog != nil {
			c.tell("Program compiled size: %d instructions", len(cached.prog.Code))
			// Upstream profiles every program's cumulative runtime.
			// This does not, so the figure is reported as zero
			// rather than invented.
			c.tell("Cumulative runtime: 0.000000 seconds ")
		} else {
			c.tell("Program not compiled.")
		}
		s.tellLocation(c, o)
	}
}

// tellLocation names where something is, but only to someone who may see it:
// a location is as private as the object standing in it.
func (s *Server) tellLocation(c *ctx, o *world.Object) {
	if o.Location == ref.Nothing {
		return
	}
	if !s.controls(c.w, c.who, o.Location) && !s.canSeeFlags(c, o.Location) {
		return
	}
	c.tell("Location: %s", unparse(c.w, c.who, o.Location))
}

// valueOf reads an object's currency.
func valueOf(w *world.World, r ref.Ref) int64 {
	v, ok := w.GetProp(r, propValue)
	if !ok || v.Type != props.Int {
		return 0
	}
	return v.Num
}

// lockString renders a lock property. Emerald stores a lock as the boolean
// expression it was written as, which is also how a dump stores it.
func lockString(w *world.World, r ref.Ref, path string) string {
	v, ok := w.GetProp(r, path)
	if !ok || v.Type != props.Lock || v.Str == "" {
		return unlockedValue
	}
	return v.Str
}

// sizeOfObject estimates what an object costs in memory.
//
// The number is this server's, not Fuzzball's: the two lay objects out
// differently, so they could not agree even in principle. What the line is
// for — telling an admin which objects are expensive — works either way.
func sizeOfObject(o *world.Object) int {
	// The fixed part of the struct, then what hangs off it.
	n := 160 + len(o.Name) + len(o.PasswordHash) + 4*len(o.Dest)
	for _, e := range o.Props.All() {
		n += len(e.Path) + len(e.Value.Str) + 48
	}
	return n
}

// flagDescription is the "Type: ... Flags: ..." line.
//
// Several flags are shown under a different name depending on the type they
// are on, because the same bit means different things: STICKY on a program is
// SETUID, DARK on one is DEBUG, and so on.
func flagDescription(o *world.Object) string {
	var b strings.Builder
	b.WriteString("Type: ")
	switch o.Type() {
	case ref.TypeRoom:
		b.WriteString("ROOM")
	case ref.TypeExit:
		b.WriteString("EXIT/ACTION")
	case ref.TypeThing:
		b.WriteString("THING")
	case ref.TypePlayer:
		b.WriteString("PLAYER")
	case ref.TypeProgram:
		b.WriteString("PROGRAM")
	case ref.TypeGarbage:
		b.WriteString("GARBAGE")
	default:
		b.WriteString("***UNKNOWN TYPE***")
	}

	f := o.Flags &^ ref.Flags(ref.TypeMask) &^ ref.DumpMask
	if f == 0 {
		return b.String()
	}
	b.WriteString("  Flags:")

	t := o.Type()
	named := func(bit ref.Flags, name string) {
		if o.Flags&bit != 0 {
			b.WriteString(" " + name)
		}
	}
	byType := func(bit ref.Flags, general string, special map[ref.ObjType]string) {
		if o.Flags&bit == 0 {
			return
		}
		if name, ok := special[t]; ok {
			b.WriteString(" " + name)
			return
		}
		b.WriteString(" " + general)
	}

	named(ref.Wizard, "WIZARD")
	named(ref.Quell, "QUELL")
	byType(ref.Sticky, "STICKY", map[ref.ObjType]string{
		ref.TypeProgram: "SETUID", ref.TypePlayer: "SILENT"})
	byType(ref.Dark, "DARK", map[ref.ObjType]string{ref.TypeProgram: "DEBUG"})
	named(ref.LinkOK, "LINK_OK")
	named(ref.KillOK, "KILL_OK")
	if lv := o.Flags.RawMLevel(); lv != 0 {
		fmt.Fprintf(&b, " MUCKER%d", lv)
	}
	byType(ref.Builder, "BUILDER", map[ref.ObjType]string{ref.TypeProgram: "BOUND"})
	byType(ref.ChownOK, "CHOWN_OK", map[ref.ObjType]string{ref.TypePlayer: "COLOR"})
	named(ref.JumpOK, "JUMP_OK")
	byType(ref.Vehicle, "VEHICLE", map[ref.ObjType]string{ref.TypeProgram: "VIEWABLE"})
	named(ref.Yield, "YIELD")
	named(ref.Overt, "OVERT")
	byType(ref.XForcible, "XFORCIBLE", map[ref.ObjType]string{ref.TypeExit: "XPRESS"})
	named(ref.Zombie, "ZOMBIE")
	if o.Flags&ref.Guest != 0 {
		if t == ref.TypePlayer || t == ref.TypeThing {
			b.WriteString(" GUEST")
		} else {
			b.WriteString(" NOGUEST")
		}
	}
	byType(ref.Haven, "HAVEN", map[ref.ObjType]string{
		ref.TypeProgram: "HARDUID", ref.TypeThing: "HIDE"})
	byType(ref.Abode, "ABODE", map[ref.ObjType]string{
		ref.TypeProgram: "AUTOSTART", ref.TypeExit: "ABATE"})

	return b.String()
}

// listProps prints the properties under a directory that match a pattern, and
// returns how many it printed.
//
// A pattern ending in '/' means everything directly below it, and "**" means
// recursively. System properties are never listed, and hidden ones only to a
// wizard. Paths are shown from the root, with the leading '/' upstream prints.
func (s *Server) listProps(c *ctx, target ref.Ref, dir, pattern string) int {
	// The trailing slash is expanded before the leading ones are stripped,
	// which is upstream's order and is what makes "/" mean "everything at
	// the root" rather than nothing.
	if strings.HasSuffix(pattern, "/") {
		pattern += "*"
	}
	pattern = strings.TrimLeft(pattern, "/")
	recurse := pattern == "**"

	head, rest, _ := strings.Cut(pattern, "/")
	o := c.w.Get(target)
	if o == nil {
		return 0
	}

	wizard := ownerIsWizard(c.w, c.who)
	count := 0
	for _, name := range o.Props.Children(dir) {
		if !ascii.SMatch(name, head) {
			continue
		}
		path := name
		if dir != "" {
			path = dir + "/" + name
		}
		if propIsSystem(path) || propIsHidden(path) && !wizard {
			continue
		}
		if rest == "" || recurse {
			count++
			c.send(displayProp(c.w, c.who, o.Props, path))
		}
		next := rest
		if recurse {
			next = "**"
		}
		count += s.listProps(c, target, path, next)
	}
	return count
}

// propIsSystem reports whether a path is in the server's own propdir.
func propIsSystem(path string) bool {
	return ascii.HasPrefix(path, "@__sys__/") || ascii.EqualFold(path, "@__sys__")
}

// propIsHidden reports whether any segment of a path starts with '@', which is
// how a property is marked wizard-only.
func propIsHidden(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, "@") {
			return true
		}
	}
	return false
}

// displayProp renders one property the way examine lists it: a blessed marker,
// the type, the path, and the value. A path naming a directory as well as a
// value keeps its trailing slash.
// displayProp renders one property the way examine lists it: a blessed
// marker, the type, the path from the root, and the value.
//
// A path that holds children shows a trailing slash, and one that holds only
// children shows as a directory with no value of its own.
func displayProp(w *world.World, who ref.Ref, tree *props.Tree, path string) string {
	shown := "/" + path
	if len(tree.Children(path)) > 0 {
		shown += "/"
	}

	v, ok := tree.Get(path)
	if !ok {
		return "- dir " + shown + ":(no value)"
	}
	blessed := "-"
	if v.Blessed {
		blessed = "B"
	}

	switch v.Type {
	case props.String:
		return fmt.Sprintf("%s str %s:%s", blessed, shown, v.Str)
	case props.Ref:
		return fmt.Sprintf("%s ref %s:%s", blessed, shown, unparse(w, who, v.Ref))
	case props.Int:
		return fmt.Sprintf("%s int %s:%d", blessed, shown, v.Num)
	case props.Float:
		return fmt.Sprintf("%s flt %s:%.17g", blessed, shown, v.Float)
	case props.Lock:
		s := v.Str
		if s == "" {
			s = unlockedValue
		}
		return fmt.Sprintf("%s lok %s:%s", blessed, shown, s)
	}
	return "- dir " + shown + ":(no value)"
}

// canLink reports whether someone may link an object, which is also the test
// for whether they may examine it: anyone may link an exit that points
// nowhere, so an unlinked exit is examinable by anyone.
func (s *Server) canLink(w *world.World, who, what ref.Ref) bool {
	if s.controls(w, who, what) {
		return true
	}
	o := w.Get(what)
	return o != nil && o.Type() == ref.TypeExit && len(o.Dest) == 0
}

// canSeeFlags reports whether someone may be told where an object is, which
// upstream ties to whether they could teleport there.
func (s *Server) canSeeFlags(c *ctx, where ref.Ref) bool {
	if s.controls(c.w, c.who, where) {
		return true
	}
	o := c.w.Get(where)
	if o == nil || !s.lockPasses(c.w, c.d.ID, 1, c.who, where, propLinkLock, true) {
		return false
	}
	return o.Flags&ref.LinkOK != 0 ||
		o.Type() != ref.TypeThing && o.Flags&ref.Abode != 0
}

// passesReadLock reports whether someone may read an object's details. An
// unset read lock means no, which is why examine normally shows only the
// owner to anyone who does not control the object.
func (s *Server) passesReadLock(c *ctx, what ref.Ref) bool {
	return s.lockPasses(c.w, c.d.ID, 1, c.who, what, propReadLock, false)
}

// lockPasses evaluates a lock property against who.
//
// A lock property holds its unparsed boolean expression in dbref form (see
// internal/boolexp's package doc), so it is re-parsed on every check via
// Parse's dbload path rather than cached. A lock that fails to parse — which
// should not happen to a lock Unparse produced itself — is treated as
// TRUE_BOOLEXP, upstream's own parse failure result, which always passes.
//
// level is this check's own interpreter nesting depth (muf.Frame.Level for a
// check TESTLOCK or LOCKED? made; 1 for a fresh check with no calling MUF
// frame), which a program-type lock constant's RunLock propagates onward.
func (s *Server) lockPasses(w *world.World, descr, level int, who, what ref.Ref, path string, defaultWhenUnset bool) bool {
	v, ok := w.GetProp(what, path)
	if !ok || v.Type != props.Lock || v.Str == "" {
		return defaultWhenUnset
	}
	host := &lockHost{s: s, w: w, level: level}
	b, err := boolexp.Parse(host, descr, who, v.Str, true)
	if err != nil {
		return true
	}
	return boolexp.Eval(host, descr, who, b, what)
}
