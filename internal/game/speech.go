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
		s.notifyRoomFrom(c.w, c.who, o.Location,
			[]ref.Ref{c.who},
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
	s.notifyRoomFrom(c.w, c.who, o.Location, nil, "%s", line)
}

// cmdWhisper speaks privately to someone in the same room.
func (s *Server) cmdWhisper(c *ctx) {
	name, text, ok := strings.Cut(c.arg, "=")
	if !ok {
		c.tell("Usage: whisper <player>=<message>")
		return
	}
	name, text = strings.TrimSpace(name), strings.TrimSpace(text)

	// `do_whisper`'s own match list (`speech.c:395`): neighbours
	// and "me", with absolute refs and player names added for a
	// wizard who is a **player** -- so a wizard's puppet cannot
	// whisper across the game. The failure goes through
	// `noisy_match_result` like every other command's, where this
	// invented two messages of its own.
	m := match.New(c.w, c.who, name).
		PreferType(ref.TypePlayer).Neighbor().Me()
	if isWizard(c.w, c.who) &&
		c.w.Get(c.who).Type() == ref.TypePlayer {
		m = m.Absolute().Player()
	}
	target := m.Result()
	if !noisyMatch(c, name, target) {
		return
	}
	// **No type check.** `notify_listeners` decides what can
	// hear: a THING is a legal target and a puppet relays the
	// whisper to its owner. "You can only whisper to a player."
	// was invented here.
	me := c.w.Get(c.who)
	if !s.notifyPrivately(c.w, c.who, target,
		locationOf(c.w, c.who),
		sprintf("%s whispers, \"%s\"", me.Name, text)) {

		c.tell("%s is not connected.",
			c.w.Get(target).Name)
		return
	}
	// The confirmation comes **after** the delivery, because it
	// is the delivery that decides whether there is one.
	c.tell("You whisper, \"%s\" to %s.", text, c.w.Get(target).Name)
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
		// "name", and the American spelling, which is
		// upstream's.
		c.tell("I don't recognize that name.")
		return
	}
	// A HAVEN player is not to be disturbed, which this did not
	// check -- so the one flag a player sets to stop being paged
	// did nothing.
	if c.w.Get(target).Flags&ref.Haven != 0 {
		c.tell("That player does not wish to be disturbed.")
		return
	}
	// And it **costs** lookup_cost, like any other lookup by
	// name. Nothing here charged for it.
	if !s.payFor(c.w, c.who, int(c.w.Tune.Int("lookup_cost"))) {
		c.tell("You don't have enough %s.",
			c.w.Tune.String("pennies"))
		return
	}

	// The message names the **room** the pager is in, which is
	// most of the point of it: a page says where to come. And an
	// empty one is a nudge rather than a message.
	me := c.w.Get(c.who)
	where := nameOf(c.w, locationOf(c.w, c.who))
	msg := sprintf("You sense that %s is paging you from %s.",
		me.Name, where)
	if text != "" {
		msg = sprintf("%s pages from %s: \"%s\"", me.Name,
			where, text)
	}
	// The sender is told that it was *sent*, not what was sent:
	// there is no echo of the text at all.
	if s.notifyPrivately(c.w, c.who, target,
		locationOf(c.w, c.who), msg) {

		c.tell("Your message has been sent.")
		return
	}
	c.tell("%s is not connected.", c.w.Get(target).Name)
}
