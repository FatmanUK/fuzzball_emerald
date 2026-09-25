package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
)

// mesgCommandSpec is one of the standard message-setting commands,
// all driven by upstream's set_standard_property (property.c:2282).
// It is set_standard_lock's exact twin, down to the "no '=' means
// report it" rule, which is why this file reads like lock_cmd.go.
//
// label is upstream's proplabel, which appears in both messages and
// is therefore part of the interface rather than decoration: a
// world's programs match on "Object Description set." the same way
// they match on any other reply.
type mesgCommandSpec struct {
	verb  string
	path  string
	label string
	// selfOnly is @doing's alone. Upstream's own comment calls
	// the restriction senseless — a wizard ought to be able to
	// set someone else's — but it is what do_doing does, and
	// the shipped help documents it.
	selfOnly bool
}

var mesgCommandSpecs = []mesgCommandSpec{
	{verb: "@describe", path: propDesc, label: "Object Description"},
	{verb: "@idescribe", path: propIDesc, label: "Inside Description"},
	{verb: "@success", path: propSucc, label: "Success Message"},
	{verb: "@osuccess", path: propOSucc, label: "OSuccess Message"},
	{verb: "@fail", path: propFail, label: "Fail Message"},
	{verb: "@ofail", path: propOFail, label: "OFail Message"},
	{verb: "@drop", path: propDrop, label: "Drop Message"},
	{verb: "@odrop", path: propODrop, label: "ODrop Message"},
	{verb: "@oecho", path: propRoomEcho,
		label: "Outside-echo Prefix"},
	{verb: "@pecho", path: propPuppetEcho,
		label: "Puppet-echo Prefix"},
	{verb: "@doing", path: propDoing, label: "Doing",
		selfOnly: true},
}

// The message setters register themselves here for the same reason
// the lock family does: a closure over the table inside atCommands's
// own literal would refer back to the table being initialised.
//
// NOGUEST is not checked here. Upstream applies it at the dispatch
// site and so does Emerald, from the perm flags on commandTable —
// which is what keeps a guest's refusal the same whether or not this
// server implements the command behind it.
func init() {
	for _, spec := range mesgCommandSpecs {
		spec := spec
		register(spec.verb, func(s *Server, c *ctx) {
			s.cmdSetMesg(c, spec)
		})
	}
}

// cmdSetMesg is set_standard_property: with no "=" anywhere in the
// argument it reports the property, with "=" and nothing after it
// clears it, and otherwise sets it. An empty object name means the
// caller.
//
// The report/set decision is made on the *whole* argument rather than
// on whether a name was given, which is upstream's own test —
// strchr(match_args, ARG_DELIMITER) — so "@describe =shiny"
// describes the caller rather than reporting.
func (s *Server) cmdSetMesg(c *ctx, spec mesgCommandSpec) {
	objname, value, set := strings.Cut(c.arg, "=")
	objname = strings.TrimSpace(objname)

	if spec.selfOnly && objname != "" &&
		!ascii.EqualFold(objname, "me") {
		c.tell("This property can only be set on yourself.")
		return
	}

	target := c.who
	if objname != "" {
		t, ok := s.matchControlled(c, objname)
		if !ok {
			return
		}
		target = t
	}

	if !set {
		c.tell("%s: %s", spec.label,
			getMesg(c.w, target, spec.path))
		return
	}

	// Upstream trims only the left of the value — arg2 is
	// skip_whitespace'd and not remove_ending_whitespace'd —
	// where the object name is trimmed both ends.
	value = strings.TrimLeft(value, " \t")
	if value == "" {
		if o := c.w.Get(target); o != nil {
			o.Props.Delete(spec.path)
			c.w.Modified(target)
		}
		c.tell("%s cleared.", spec.label)
		return
	}
	c.w.SetProp(target, spec.path, props.Value{
		Type: props.String, Str: value,
	})
	c.tell("%s set.", spec.label)
}
