package mcp

import (
	"fmt"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
)

// maxInlineValue is how long an argument may be before it is sent on
// continuation lines instead of inline.
//
// Upstream compares against half of whatever is left of its line buffer, which
// comes to about this for a message with a few short arguments. The exact
// threshold is not observable — a client must accept either form — so a fixed
// number is used rather than reproducing the arithmetic.
const maxInlineValue = 400

// SendMessage writes a message to the client.
//
// An argument that is multi-line, or too long to sit comfortably on the first
// line, is announced with a "*" and sent afterwards on its own lines, tied to
// the message by a data tag.
func (f *Frame) SendMessage(msg *Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sendMessage(msg)
}

// sendMessage is SendMessage without the lock, for callers that hold it.
func (f *Frame) sendMessage(msg *Message) error {
	if !f.enabled && !ascii.EqualFold(msg.Package, InitPackage) {
		return errNotEnabled
	}

	name := msg.FullName()
	var b strings.Builder
	b.WriteString(Prefix)
	b.WriteString(name)

	// Every message but the opening one carries the key, and every
	// message outside the negotiation package needs the client to have
	// agreed to that package first.
	if !ascii.EqualFold(name, InitPackage) {
		b.WriteString(" ")
		b.WriteString(f.authKey)
		if !ascii.EqualFold(msg.Package, NegotiatePackage) &&
			f.supports(msg.Package).IsZero() {
			return errNoPackage
		}
	}

	// A value containing a line break becomes several lines.
	args := make([]Arg, len(msg.Args))
	for i, a := range msg.Args {
		args[i] = Arg{Name: a.Name, Lines: splitLines(a.Lines)}
	}

	var deferred []Arg
	for _, a := range args {
		switch {
		case len(a.Lines) == 0:
			b.WriteString(" " + a.Name + ": " + emptyArg)
		case len(a.Lines) > 1 || len(a.Lines[0])+len(a.Name) > maxInlineValue:
			b.WriteString(" " + a.Name + "*: " + emptyArg)
			deferred = append(deferred, a)
		default:
			b.WriteString(" " + a.Name + ": " + escapeArg(a.Lines[0]))
		}
	}

	if len(deferred) == 0 {
		f.Send(b.String())
		return nil
	}

	tag := newAuthKey()
	b.WriteString(" " + DataTag + ": " + tag)
	f.Send(b.String())
	for _, a := range deferred {
		for _, line := range a.Lines {
			f.Send(fmt.Sprintf("%s* %s %s: %s", Prefix, tag, a.Name, line))
		}
	}
	f.Send(Prefix + ": " + tag)
	return nil
}

// splitLines breaks any embedded line endings out into separate lines, so a
// value carrying a newline is sent as the several lines it really is.
func splitLines(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
		out = append(out, strings.Split(s, "\n")...)
	}
	return out
}

// SendInband writes ordinary text to a client that has MCP enabled, quoting it
// if it would otherwise be read as a message.
//
// Without this, a player who types a line beginning "#$#" could make every
// other client in the room act on it.
func (f *Frame) SendInband(line string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.enabled && strings.HasPrefix(line, Prefix) {
		f.Send(QuotePrefix + line)
		return
	}
	f.Send(line)
}
