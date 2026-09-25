package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// cmdSay speaks to the room. cmdSay is do_say (speech.c): what the
// player typed, in quotes.
//
// It prints full_command rather than the trimmed argument, so leading
// whitespace survives — and it has no emptiness guard at all, so a
// bare "say" really does produce You say, "". Both of those are
// upstream's and both are compared by the golden case.
func (s *Server) cmdSay(c *ctx) {
	o := c.w.Get(c.who)
	c.tell("You say, \"%s\"", c.rest)
	if o.Location != ref.Nothing {
		s.notifyRoom(c.w, o.Location, []ref.Ref{c.who},
			"%s says, \"%s\"", o.Name, c.rest)
	}
}

// cmdPose is do_pose: the player's name, then what they typed.
//
// The space before it is omitted when the text starts with a pose
// separator — an apostrophe, a space, a comma or a hyphen — so
// ":'s hat" reads "Igor's hat" and ":, yes" reads "Igor, yes". That
// is the same is_valid_pose_separator prefix_message uses for the "o"
// messages.
//
// The poser is *not* excluded from the broadcast: upstream passes
// NOTHING as notify_except's exception and lets the room deliver it,
// rather than telling the poser separately.
func (s *Server) cmdPose(c *ctx) {
	o := c.w.Get(c.who)
	sep := " "
	if isPoseSeparator(c.rest) {
		sep = ""
	}
	line := o.Name + sep + c.rest
	if o.Location == ref.Nothing {
		c.send(line)
		return
	}
	s.notifyRoom(c.w, o.Location, nil, "%s", line)
}

// cmdWhisper speaks privately to someone in the same room.
func (s *Server) cmdWhisper(c *ctx) {
	name, text, ok := strings.Cut(c.arg, "=")
	if !ok {
		c.tell("Usage: whisper <player>=<message>")
		return
	}
	name, text = strings.TrimSpace(name), strings.TrimSpace(text)

	target := match.New(c.w, c.who, name).Thing().Result()
	switch target {
	case ref.Nothing:
		c.tell("I don't see that person here.")
		return
	case ref.Ambiguous:
		c.tell("I don't know which one you mean.")
		return
	}
	if c.w.Get(target).Type() != ref.TypePlayer {
		c.tell("You can only whisper to a player.")
		return
	}

	me := c.w.Get(c.who)
	c.tell("You whisper, \"%s\" to %s.", text, c.w.Get(target).Name)
	s.notify(c.w, target, "%s whispers, \"%s\"", me.Name, text)
}

// cmdPage messages a player anywhere in the game.
func (s *Server) cmdPage(c *ctx) {
	name, text, ok := strings.Cut(c.arg, "=")
	if !ok {
		c.tell("Usage: page <player>=<message>")
		return
	}
	name, text = strings.TrimSpace(name), strings.TrimSpace(text)

	target, found := c.w.PlayerNamed(name)
	if !found {
		c.tell("I don't recognise that player.")
		return
	}
	if !s.hub.Online(target) {
		c.tell("%s is not connected.", c.w.Get(target).Name)
		return
	}

	me := c.w.Get(c.who)
	if text == "" {
		c.tell("You page %s.", c.w.Get(target).Name)
		s.notify(c.w, target, "You sense that %s is looking for you.", me.Name)
		return
	}
	c.tell("You page, \"%s\" to %s.", text, c.w.Get(target).Name)
	s.notify(c.w, target, "%s pages: %s", me.Name, text)
}
