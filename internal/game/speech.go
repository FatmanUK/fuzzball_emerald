package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// cmdSay speaks to the room.
func (s *Server) cmdSay(c *ctx) {
	if c.arg == "" {
		c.tell("Say what?")
		return
	}
	o := c.w.Get(c.who)
	c.tell("You say, \"%s\"", c.arg)
	if o.Location != ref.Nothing {
		s.notifyRoom(c.w, o.Location, []ref.Ref{c.who},
			"%s says, \"%s\"", o.Name, c.arg)
	}
}

// cmdPose emotes to the room.
func (s *Server) cmdPose(c *ctx) {
	if c.arg == "" {
		c.tell("Do what?")
		return
	}
	o := c.w.Get(c.who)
	// A pose beginning with an apostrophe is possessive: ":'s hat" reads as
	// "Igor's hat", with no space before the apostrophe.
	sep := " "
	if strings.HasPrefix(c.arg, "'") {
		sep = ""
	}
	line := o.Name + sep + c.arg
	c.send(line)
	if o.Location != ref.Nothing {
		s.notifyRoom(c.w, o.Location, []ref.Ref{c.who}, "%s", line)
	}
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
