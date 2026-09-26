package game

import (
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/boolexp"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// @propset and @register, from set.c:929 and :1077.
//
// These are the two commands that write a property a player names
// rather than one a verb implies. Everything in mesg_cmd.go writes a
// fixed path; these write any path, which is why both carry a
// restriction check the message setters do not need.

func init() {
	register("@propset", (*Server).cmdPropset)
	register("@register", (*Server).cmdRegister)
}

// propRestricted is do_propset's guard: a system property is out of
// bounds to everyone, and a hidden or see-only one to anyone who is
// not a wizard.
//
// The three sigils are per *path segment*, not per path —
// "_stuff/@x" is hidden — which is Prop_Check.
func propRestricted(path string, wizard bool) bool {
	if isSystemProp(path) {
		return true
	}
	if wizard {
		return false
	}
	return isHiddenProp(path) || propSegmentStartsWith(path, '~')
}

// isSystemProp is upstream's Prop_System: anything under "@__sys__".
func isSystemProp(path string) bool {
	if !ascii.HasPrefix(path, "@__sys__") {
		return false
	}
	rest := path[len("@__sys__"):]
	return rest == "" || rest[0] == '/'
}

// propSegmentStartsWith is Prop_Check: whether the path or any
// segment after a '/' begins with the given character.
func propSegmentStartsWith(path string, c byte) bool {
	if len(path) > 0 && path[0] == c {
		return true
	}
	for i := 0; i+1 < len(path); i++ {
		if path[i] == '/' && path[i+1] == c {
			return true
		}
	}
	return false
}

// cmdPropset is do_propset: write any property, in any of six types.
//
// The argument is "<object>=<type>:<path>:<value>", and the type may
// be abbreviated to any prefix — so ":path:value" is a string and
// "i:path:3" an integer. An empty type is a string too, which is what
// makes "@propset me=:foo:bar" the short form.
func (s *Server) cmdPropset(c *ctx) {
	name, rest, _ := strings.Cut(c.arg, "=")
	target, ok := s.matchControlled(c, strings.TrimSpace(name))
	if !ok {
		return
	}

	// Three colon-separated fields, and only the first two are
	// trimmed: a value may begin or end with a space.
	rest = strings.TrimLeft(rest, " \t")
	kind, after, _ := strings.Cut(rest, ":")
	path, value, _ := strings.Cut(after, ":")
	kind = strings.TrimSpace(kind)
	// Leading '/' and whitespace are stripped from the path, so
	// "/foo" and "foo" are the same property.
	path = strings.TrimLeft(path, "/ \t")
	path = strings.TrimRight(path, " \t")

	if path == "" {
		c.tell("I don't know which property you want to set!")
		return
	}
	if propRestricted(path, isWizard(c.w, ownerOf(c.w, c.who))) {
		c.tell("Permission denied. (The property is " +
			"restricted.)")
		return
	}

	switch {
	case kind == "" || ascii.HasPrefix("string", kind):
		c.w.SetProp(target, path, props.Value{
			Type: props.String, Str: value})
	case ascii.HasPrefix("integer", kind):
		n, err := strconv.ParseInt(strings.TrimSpace(value),
			10, 64)
		if err != nil {
			c.tell("That's not an integer!")
			return
		}
		c.w.SetProp(target, path, props.Value{
			Type: props.Int, Num: n})
	case ascii.HasPrefix("float", kind):
		f, err := strconv.ParseFloat(
			strings.TrimSpace(value), 64)
		if err != nil {
			c.tell("That's not a floating point number!")
			return
		}
		c.w.SetProp(target, path, props.Value{
			Type: props.Float, Float: f})
	case ascii.HasPrefix("dbref", kind):
		r := match.New(c.w, c.who, strings.TrimSpace(value)).
			Everything().Result()
		if !noisyMatch(c, strings.TrimSpace(value), r) {
			return
		}
		c.w.SetProp(target, path, props.Value{
			Type: props.Ref, Ref: r})
	case ascii.HasPrefix("lock", kind):
		host := &lockHost{s: s, w: c.w}
		b, err := boolexp.Parse(host, c.d.ID, c.who,
			strings.TrimSpace(value), false)
		if err != nil {
			// The parser has its own message for a name
			// it could not resolve — "I don't see X
			// here." — and upstream shows both, the
			// specific one first. set_standard_lock does
			// the same.
			if pe, ok := err.(*boolexp.ParseError); ok &&
				pe.Notify {
				c.send(pe.Msg)
			}
			c.tell("I don't understand that lock.")
			return
		}
		c.w.SetProp(target, path, props.Value{
			Type: props.Lock,
			Str: boolexp.Unparse(host, c.who, b,
				false)})
	case ascii.HasPrefix("erase", kind):
		if value != "" {
			c.tell("Don't give a value when erasing a " +
				"property.")
			return
		}
		if o := c.w.Get(target); o != nil {
			o.Props.Delete(path)
			c.w.Modified(target)
		}
		c.tell("Property erased.")
		return
	default:
		c.tell("I don't know what type of property you " +
			"want to set!")
		c.tell("Valid types are string, integer, float, " +
			"dbref, lock, and erase.")
		return
	}
	c.tell("Property set.")
}

// registrationPropdir is REGISTRATION_PROPDIR (game.h:67).
const registrationPropdir = "_reg"

// cmdRegister is do_register: give an object a name a program can
// resolve with "$name".
//
// Nothing in Emerald wrote `_reg/` except the editor's own `q`, so
// `$include` and Matcher.Registered read a propdir only one thing
// filled in.
//
// The argument shape is upstream's and is genuinely complicated:
//
//	@register <object>=<name>          on #0, propdir _reg
//	@register #me <object>=<name>      on the caller
//	@register #prop <dir> <object>=<name>
//	@register #prop <target>:<dir> <object>=<name>
//
// and with no "=" at all it *lists* instead, which is what makes the
// prefixes worth having.
func (s *Server) cmdRegister(c *ctx) {
	head, name, assigning := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)

	target := ref.GlobalEnvironment
	propdir := registrationPropdir
	objectstr := strings.TrimSpace(head)

	// The prefix test is on the whole argument, not on its first
	// word, and it is a plain prefix rather than an abbreviation:
	// upstream writes string_prefix(arg1, "#me"), which asks
	// whether arg1 *starts with* "#me". Testing the other way
	// round makes an empty argument match every prefix, which is
	// how "@register =wid" came to act as "@register #me =wid".
	switch {
	case ascii.HasPrefix(objectstr, "#me"):
		target = c.who
		objectstr = strings.TrimSpace(
			afterFirstWord(objectstr))
	case ascii.HasPrefix(objectstr, "#prop"):
		rest := strings.TrimSpace(afterFirstWord(objectstr))
		spec := firstWord(rest)
		objectstr = strings.TrimSpace(afterFirstWord(rest))

		targetstr := "me"
		if strings.HasPrefix(spec, ":") {
			propdir = spec[1:]
		} else {
			targetstr, propdir, _ = strings.Cut(spec, ":")
		}
		propdir = strings.TrimRight(propdir, "/")
		if propdir == "" {
			c.tell("You must specify a propdir when " +
				"using #prop.")
			return
		}

		r := s.matchRegisterTarget(c, targetstr)
		if r == ref.Nothing {
			return
		}
		// match_controlled casts a much wider net than this,
		// so upstream checks control separately to keep the
		// narrower match — and says match_controlled's
		// words anyway.
		if !s.controls(c.w, c.who, r) {
			c.tell("Permission denied. (You don't " +
				"control what was matched)")
			return
		}
		target = r
	}

	if !assigning || name == "" {
		s.listRegistrations(c, target, propdir, objectstr)
		return
	}

	// With no object named, this *removes* the entry.
	if objectstr == "" {
		s.registerObject(c, target, propdir, name,
			ref.Nothing)
		return
	}
	object := s.matchRegisterTarget(c, objectstr)
	if object == ref.Nothing {
		return
	}

	path := propdir + "/" + name
	wizard := isWizard(c.w, ownerOf(c.w, c.who))
	if !wizard && (target != ownerOf(c.w, c.who) ||
		propSegmentStartsWith(path, '~') ||
		isHiddenProp(path)) {
		c.tell("Permission denied. (You can't register an " +
			"object there.)")
		return
	}
	s.registerObject(c, target, propdir, name, object)
}

// matchRegisterTarget is the narrow match do_register uses in three
// places: a "$name" is looked up as a registration and anything else
// is matched nearby, with a wizard also able to name a dbref or a
// player.
func (s *Server) matchRegisterTarget(c *ctx, name string) ref.Ref {
	m := match.New(c.w, c.who, name)
	if strings.HasPrefix(name, "$") {
		m = m.Registered()
	} else {
		m = m.Exits().Neighbor().Possession().Me().
			Here().Nil()
	}
	if isWizard(c.w, ownerOf(c.w, c.who)) {
		m = m.Absolute().Player()
	}
	r := m.Result()
	if !noisyMatch(c, name, r) {
		return ref.Nothing
	}
	return r
}

// registerObject is db.c:2238's register_object: write or remove one
// entry, reporting what was there before.
//
// The value is stored as a *ref*, which is what Matcher.Registered
// reads back first — though it also accepts the integer and string
// forms real databases contain.
func (s *Server) registerObject(c *ctx, location ref.Ref,
	propdir, name string, object ref.Ref) {

	if !validRegistryName(name) {
		c.tell("Registry name '%s' is not valid", name)
		return
	}
	path := propdir + "/" + name

	if v, ok := c.w.GetProp(location, path); ok {
		if prev, known := registeredRef(v); known {
			c.tell("Used to be registered as %s: %s",
				path, unparse(c.w, c.who, prev))
		}
	} else if object == ref.Nothing {
		c.tell("Nothing to remove.")
		return
	}

	if object == ref.Nothing {
		if o := c.w.Get(location); o != nil {
			o.Props.Delete(path)
			c.w.Modified(location)
		}
		c.tell("Registry entry on %s removed.",
			unparse(c.w, c.who, location))
		return
	}
	c.w.SetProp(location, path,
		props.Value{Type: props.Ref, Ref: object})
	c.tell("Now registered as %s: %s on %s", path,
		unparse(c.w, c.who, object),
		unparse(c.w, c.who, location))
}

// validRegistryName is is_valid_propname (property.c:2642):
// non-empty, and containing neither a carriage return nor the ':' the
// property library treats as a terminator.
//
// internal/muf has the same port for PROPNAME-OK?; this one is here
// rather than exported from there because the two are three lines and
// a shared helper would need a package neither belongs in.
func validRegistryName(name string) bool {
	if name == "" {
		return false
	}
	return !strings.ContainsAny(name, "\r:")
}

// registeredRef reads whatever a registration entry holds as a dbref.
// Real databases carry all four forms, which is why
// Matcher.Registered accepts them too.
func registeredRef(v props.Value) (ref.Ref, bool) {
	switch v.Type {
	case props.Ref:
		return v.Ref, true
	case props.Int:
		return ref.Ref(v.Num), true
	case props.String:
		n, err := strconv.ParseInt(
			strings.TrimPrefix(v.Str, "#"), 10, 32)
		if err != nil {
			return ref.Ambiguous, true
		}
		return ref.Ref(n), true
	}
	return ref.Nothing, false
}

// listRegistrations is what @register does with no "=": show what is
// registered under a propdir, one line each, ending "Done."
func (s *Server) listRegistrations(c *ctx, target ref.Ref,
	propdir, sub string) {

	dir := propdir
	if sub != "" {
		dir = propdir + "/" + sub
	}
	c.tell("Registered objects on %s:",
		unparse(c.w, c.who, target))

	wizard := isWizard(c.w, ownerOf(c.w, c.who))
	o := c.w.Get(target)
	if o != nil {
		for _, child := range o.Props.Children(dir) {
			path := dir + "/" + child
			if isHiddenProp(path) && !wizard {
				continue
			}
			label := child
			if sub != "" {
				label = strings.TrimRight(sub, "/") +
					"/" + child
			}
			detail := s.registryDetail(c, target, path)
			c.send("  " + label + detail)
		}
	}
	c.tell("Done.")
}

// registryDetail is the ": <object>" or "/ (directory)" a listed
// entry carries. An entry whose value is not a readable dbref shows
// as ambiguous rather than being hidden.
func (s *Server) registryDetail(c *ctx, target ref.Ref,
	path string) string {

	v, ok := c.w.GetProp(target, path)
	if !ok {
		return "/ (directory)"
	}
	r, known := registeredRef(v)
	if !known {
		r = ref.Ambiguous
	}
	return ": " + unparse(c.w, c.who, r)
}

// firstWord and afterFirstWord split on whitespace the way
// do_register's strtok_r calls do.
func firstWord(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

func afterFirstWord(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[i+1:]
	}
	return ""
}

// registerBuilt is the "=<regname>" every building command takes as a
// final argument: @open, @dig, @create, @program, @action and @clone.
//
// It registers on the *player*, not on #0 — register_object(player,
// player, ...) at create.c:761 and its five siblings — and reports
// through register_object's own messages rather than one of its own.
func (s *Server) registerBuilt(c *ctx, rname string, object ref.Ref) {
	if rname == "" {
		return
	}
	s.registerObject(c, c.who, registrationPropdir, rname, object)
}
