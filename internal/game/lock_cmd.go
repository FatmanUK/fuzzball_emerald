package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/boolexp"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// lockCommandSpec is one of the standard lock-setting commands, all
// driven by upstream's set_standard_lock (src/property.c): @lock,
// @flock (aliased as @force_lock), @chlock (aliased as @chown_lock),
// @conlock, @linklock, @ownlock and @readlock. verb is upstream's own
// spelling of the command, used in its NOGUEST/NOFORCE messages.
type lockCommandSpec struct {
	verb    string
	path    string
	label   string
	noForce bool
}

var lockCommandSpecs = []lockCommandSpec{
	{verb: "@lock", path: propLock, label: "Lock"},
	{verb: "@flock", path: propForceLock, label: "Force Lock", noForce: true},
	{verb: "@chlock", path: propChownLock, label: "Chown Lock"},
	{verb: "@conlock", path: propConLock, label: "Container Lock"},
	{verb: "@linklock", path: propLinkLock, label: "Link Lock"},
	{verb: "@ownlock", path: propOwnLock, label: "Ownership Lock", noForce: true},
	{verb: "@readlock", path: propReadLock, label: "Read Lock", noForce: true},
}

// The @lock family registers itself here rather than in atCommands's
// own declaration, the way @force does: a closure over
// lockCommandSpecs in that literal would refer back to the table
// being initialised.
func init() {
	for _, spec := range lockCommandSpecs {
		spec := spec
		register(spec.verb, func(s *Server, c *ctx) {
			s.cmdSetLock(c, spec)
		})
	}
	// @force_lock and @chown_lock are upstream's alternate full
	// spellings of @flock and @chlock. They are separate rows in
	// the dispatch table rather than aliases, because upstream
	// separates them on the length of what was typed —
	// strlen(command) < 7 picks @chown over @chown_lock — and
	// the table carries that as a max on one and a min on the
	// other.
	for alias, of := range map[string]string{
		"@force_lock": "@flock",
		"@chown_lock": "@chlock",
	} {
		spec := lockSpecFor(of)
		register(alias, func(s *Server, c *ctx) {
			s.cmdSetLock(c, spec)
		})
	}

	register("@unlock", (*Server).cmdUnlock)
}

// lockSpecFor finds a spec by its verb, so the alias registrations
// name what they alias rather than indexing the table by position.
func lockSpecFor(verb string) lockCommandSpec {
	for _, spec := range lockCommandSpecs {
		if spec.verb == verb {
			return spec
		}
	}
	panic("no lock command " + verb)
}

// cmdUnlock is do_unlock: clear the ordinary lock, and only that one.
//
// "@lock <thing>=" with nothing after the "=" already does this and
// says "Lock cleared."; upstream keeps the separate spelling with its
// own shorter message, and programs and players both know it.
func (s *Server) cmdUnlock(c *ctx) {
	if isGuest(c.w, c.who) {
		c.tell("Guests are not allowed to @unlock.")
		return
	}
	target, ok := s.resolveControlled(c, strings.TrimSpace(c.arg))
	if !ok {
		return
	}
	c.w.SetProp(target, propLock, props.Value{Type: props.Lock})
	c.tell("Unlocked.")
}

// cmdSetLock implements set_standard_lock: with no "=" it reports the
// named object's current lock; with "=" but nothing after it, it
// clears the lock; otherwise it parses and sets it. An empty object
// name targets the caller.
//
// NOFORCE's original guard — force_level, a global incremented for
// the duration of a MUF {force} call — is not reproduced, since
// {force} is not ported yet; only @force's own s.forceDepth is
// checked. Once {force} exists it needs to increment the same counter
// for this to stay correct.
func (s *Server) cmdSetLock(c *ctx, spec lockCommandSpec) {
	if isGuest(c.w, c.who) {
		c.tell("Guests are not allowed to %s.", spec.verb)
		return
	}
	if spec.noForce && s.forceDepth > 0 {
		c.tell("You can't use %s from a @force or {force}.", spec.verb)
		return
	}

	objname, keyvalue, set := strings.Cut(c.arg, "=")
	objname = strings.TrimSpace(objname)

	target := c.who
	if objname != "" {
		t, ok := s.resolveControlled(c, objname)
		if !ok {
			return
		}
		target = t
	}

	if !set {
		s.reportLock(c, target, spec)
		return
	}

	s.setLock(c.w, c.d.ID, c.who, target, spec.path, spec.label, strings.TrimSpace(keyvalue), false)
}

// reportLock shows a lock's current human-readable value, the way
// typing "@lock" (or any of its family) with no "=" does.
func (s *Server) reportLock(c *ctx, target ref.Ref, spec lockCommandSpec) {
	host := &lockHost{s: s, w: c.w}

	var b *boolexp.Expr
	if v, ok := c.w.GetProp(target, spec.path); ok &&
		v.Type == props.Lock && v.Str != "" {
		if parsed, err := boolexp.Parse(host, c.d.ID, c.who, v.Str, true); err == nil {
			b = parsed
		}
	}
	c.tell("%s: %s", spec.label, boolexp.Unparse(host, c.who, b, true))
}
