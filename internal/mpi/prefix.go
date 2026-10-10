package mpi

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
)

// {tell}'s and {otell}'s prefixing rules (`mfuns.c:3663`, `:3722`),
// neither of which was ported: both notified the line exactly as
// given. They decide whether the speaker's name is prepended, and —
// for {tell} — whether a "> " marker is, and they are the reason a
// description cannot forge a line that looks like somebody else
// speaking.

// tellPrefix is the marker {tell} puts before each line.
//
// It is "> " unless the recipient is the message's own owner or the
// player who triggered it: so a property telling *you* something
// speaks plainly, and a property telling somebody else about you is
// marked as relayed.
func (env *Env) tellPrefix(target Ref) string {
	if target == env.Host.Owner(env.Perms) || target == env.Who {
		return ""
	}
	return "> "
}

// tellNames reports whether {tell} prepends the triggering player's
// name, which is the other half of the same guard.
//
// The name is **omitted** — the line stands alone — when any of
// five things is true: the object carrying the message is a room, its
// owner is the recipient, the recipient is the triggering player
// themselves, it is an exit sitting directly in a room, or the
// message already begins with the player's name.
//
// So the default for a *thing* lying around is to have the speaker's
// name forced on, and only a room, a room's exit, or a message about
// yourself escapes it. That is what stops a thing in your pocket
// writing "Bob says hello" to the room.
func (env *Env) tellNames(target Ref, msg string) bool {
	what := env.What
	switch {
	case env.Host.TypeName(what) == "Room":
		return false
	case env.Host.Owner(what) == target:
		return false
	case env.Who == target:
		return false
	case env.Host.TypeName(what) == "Exit" &&
		env.Host.TypeName(env.Host.Location(what)) == "Room":
		return false
	case ascii.HasPrefix(msg, env.Host.Name(env.Who)):
		return false
	}
	return true
}

// otellNames is the same question for {otell}, and the condition is
// not the same one.
//
// The name is omitted when the message already begins with it, or
// when **both** of two things hold: the carrying object and the
// target room share an owner or the object is an ancestor of the
// room, *and* the object is a room or an exit sitting in a room.
func (env *Env) otellNames(room Ref, msg string) bool {
	what := env.What
	if ascii.HasPrefix(msg, env.Host.Name(env.Who)) {
		return false
	}
	related := env.Host.Owner(what) == env.Host.Owner(room) ||
		env.isAncestor(what, room)
	placed := env.Host.TypeName(what) == "Room" ||
		(env.Host.TypeName(what) == "Exit" &&
			env.Host.TypeName(env.Host.Location(what)) ==
				"Room")
	return !(related && placed)
}

// speakerPrefix is the name and the space that follows it.
//
// The name is cut to **sixteen** characters, which is upstream's
// `%.16s` and is shorter than `player_name_limit`'s default of
// sixteen plus whatever a world raises it to. And the space is
// omitted before a pose separator, which here is an apostrophe or any
// whitespace — the same suppression `prefix_message` makes, and
// narrower than `do_pose`'s four characters.
func (env *Env) speakerPrefix(msg string) string {
	name := env.Host.Name(env.Who)
	if len(name) > 16 {
		name = name[:16]
	}
	if msg != "" && (msg[0] == '\'' || isSpace(msg[0])) {
		return name
	}
	return name + " "
}

// isAncestor is `isancestor` (`mfuns.c:3630`): whether child sits
// anywhere under parent in the environment tree, walked with
// getparent. It had no port, and both {otell}'s rule and nothing else
// needed one.
//
// The walk is bounded, which upstream's is not: `getparent` collapses
// a cycle it detects to #0, so upstream's loop terminates on a
// damaged world by reaching the top. Emerald is handed dumps it did
// not write and `Parent` is the same tortoise-and-hare, but a bound
// costs nothing and is one less way for a property to hang the world
// goroutine.
func (env *Env) isAncestor(parent, child Ref) bool {
	for range maxEnvDepth {
		if child == parent {
			return true
		}
		next := env.Host.Parent(child)
		if next == child {
			return false
		}
		child = next
	}
	return false
}

// splitLinesCR is the carriage-return walk {tell} and {otell} share.
//
// {otell}'s own version is **broken**: `mfn_otell` writes the NUL
// over the carriage return without stepping past it (`mfuns.c:3744`
// — `*ptr2 = '\0'` where `mfn_tell` writes `*ptr2++ = '\0'`), so
// the next iteration starts on the terminator and the loop ends.
// Upstream's {otell} therefore sends only the **first line** of a
// multi-line message, silently.
//
// That is reproduced, because a world's multi-line {otell} has only
// ever shown one line and making the rest appear would change what it
// does.
func splitLinesCR(msg string, all bool) []string {
	if all {
		return strings.Split(msg, "\r")
	}
	if i := strings.IndexByte(msg, '\r'); i >= 0 {
		return []string{msg[:i]}
	}
	return []string{msg}
}
