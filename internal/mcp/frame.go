package mcp

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
)

// Frame is the MCP state of one connection.
//
// Until the client sends an "#$#mcp" line, the frame is disabled and
// every line is ordinary text. Negotiation establishes a version, an
// authentication key that every later message must carry, and the set
// of packages both sides understand.
type Frame struct {
	// Send writes one line to the client. It is set by whoever
	// owns the connection, and must not be nil once the frame is
	// in use.
	Send func(string)

	// mu guards everything below. A frame belongs to a connection
	// rather than to the world, and a connection is read on its
	// transport's goroutine while the game writes to it from the
	// world's, so unlike the object graph this state really is
	// reached from two at once.
	mu sync.Mutex

	enabled bool
	authKey string
	version Version

	// packages holds what the client said it supports, by folded
	// name.
	packages map[string]Version

	// partial holds messages still receiving their continuation
	// lines, keyed by data tag.
	partial map[string]*Message

	// registry is the set of packages this server offers.
	registry []Package

	// Context is whatever the owner needs to reach from a package
	// handler — the descriptor, the player, the server.
	Context any
}

// NewFrame returns a frame offering the given packages.
func NewFrame(send func(string), packages []Package) *Frame {
	return &Frame{
		Send:     send,
		packages: map[string]Version{},
		partial:  map[string]*Message{},
		registry: packages,
	}
}

// Enabled reports whether MCP has been negotiated.
func (f *Frame) Enabled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.enabled
}

// Version returns the negotiated protocol version.
func (f *Frame) Version() Version {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.version
}

// Supports returns the version agreed for a package, or the null
// version.
func (f *Frame) Supports(pkg string) Version {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.supports(pkg)
}

// supports is Supports without the lock, for callers that already
// hold it.
func (f *Frame) supports(pkg string) Version {
	return f.packages[ascii.Fold(pkg)]
}

// serverVersion is the protocol version this server speaks. MCP 2.1
// is the only version in use; 1.0 was never deployed widely.
var serverVersion = Version{Major: 2, Minor: 1}

// ProcessInput examines one line from the client.
//
// It returns the text to treat as ordinary input and whether there is
// any: a line that was a complete MCP message is consumed, and
// produces nothing for the command parser.
//
// A line beginning with the quote prefix is in-band text the client
// has escaped because it would otherwise look like a message; the
// prefix is stripped and the rest handed back.
func (f *Frame) ProcessInput(line string) (string, bool) {
	f.mu.Lock()
	text, pass, msg := f.readLine(line)
	f.mu.Unlock()

	// Handlers run with the lock released. They are given the
	// frame and may send on it, and holding the lock across a
	// callback would turn any such send into a deadlock.
	if msg != nil {
		f.dispatch(msg)
	}
	return text, pass
}

// readLine parses one line, returning the text to pass on, whether
// there is any, and any message that is now complete. The caller
// holds the lock.
func (f *Frame) readLine(line string) (string, bool, *Message) {
	switch {
	case strings.HasPrefix(line, Prefix):
		// Before negotiation only the opening "mcp" message
		// is read as a message; anything else starting with
		// the prefix is text, which is what lets a world talk
		// about MCP without a client swallowing the
		// conversation.
		if !f.enabled &&
			!ascii.HasPrefix(line[len(Prefix):], "mcp ") {
			return line, true, nil
		}
		if done, ok := f.parse(line[len(Prefix):]); ok {
			return "", false, done
		}
		// Something that looked like a message but did not
		// parse is passed through rather than dropped, so a
		// malformed line is visible instead of silently
		// vanishing.
		return line, true, nil

	case f.enabled && strings.HasPrefix(line, QuotePrefix):
		return line[len(QuotePrefix):], true, nil

	default:
		return line, true, nil
	}
}

// parse reads one message line, returning any message it completed
// and whether the line was a message at all.
func (f *Frame) parse(in string) (*Message, bool) {
	if done, ok := f.parseContinuation(in); ok {
		return done, true
	}
	if done, ok := f.parseEnd(in); ok {
		return done, true
	}
	return f.parseStart(in)
}

// parseStart reads the first line of a message.
func (f *Frame) parseStart(in string) (*Message, bool) {
	name, rest, ok := readIdent(in)
	if !ok {
		return nil, false
	}

	// Every message but the opening one carries the
	// authentication key, which is what stops a world echoing
	// text that a client would then obey as a message.
	if !ascii.EqualFold(name, InitPackage) {
		var key string
		rest, ok = skipSpace(rest)
		if !ok {
			return nil, false
		}
		key, rest, ok = readUnquoted(rest)
		if !ok || key != f.authKey {
			return nil, false
		}
	}

	pkg, sub, ok := f.splitPackage(name)
	if !ok {
		return nil, false
	}

	msg := NewMessage(pkg, sub)
	for rest != "" {
		if rest, ok = f.readKeyval(msg, rest); !ok {
			return nil, false
		}
	}

	if msg.incomplete {
		// The message is still arriving. The data tag is how
		// its continuation lines will find it again.
		tag, _ := msg.Arg(DataTag)
		msg.removeArg(DataTag)
		msg.dataTag = tag
		f.partial[tag] = msg
		return nil, true
	}
	return msg, true
}

// splitPackage works out which registered package a message name
// belongs to.
//
// The longest registered name that the message name starts with wins,
// because package names are hierarchical:
// "org-fuzzball-gui-ctrl-value" belongs to "org-fuzzball-gui" and not
// to "org-fuzzball".
func (f *Frame) splitPackage(name string) (pkg, sub string, ok bool) {
	longest := 0
	if !ascii.HasPrefix(name, InitPackage) ||
		len(name) > len(InitPackage) {
		for _, p := range f.registry {
			n := len(p.Name)
			if !ascii.HasPrefix(name, p.Name) {
				continue
			}
			if len(name) == n || name[n] == '-' {
				if n > longest {
					longest = n
				}
			}
		}
	}
	if longest == 0 {
		switch {
		case ascii.HasPrefix(name, NegotiatePackage):
			longest = len(NegotiatePackage)
		case ascii.EqualFold(name, InitPackage):
			longest = len(name)
		default:
			return "", "", false
		}
	}
	pkg, sub = name[:longest], name[longest:]
	sub = strings.TrimPrefix(sub, "-")
	return pkg, sub, true
}

// readKeyval reads one " key: value" pair.
func (f *Frame) readKeyval(msg *Message, in string) (string, bool) {
	rest, ok := skipSpace(in)
	if !ok {
		return in, false
	}
	key, rest, ok := readIdent(rest)
	if !ok {
		return in, false
	}

	// A '*' after the key means the value is coming on
	// continuation lines rather than here.
	deferred := false
	if strings.HasPrefix(rest, "*") {
		deferred = true
		msg.incomplete = true
		rest = rest[1:]
	}
	if !strings.HasPrefix(rest, ":") {
		return in, false
	}
	rest = rest[1:]
	if rest, ok = skipSpace(rest); !ok {
		return in, false
	}

	value, rest, ok := readUnquoted(rest)
	if !ok {
		if value, rest, ok = readQuoted(rest); !ok {
			return in, false
		}
	}
	if deferred {
		// The argument exists but has no lines yet.
		msg.Args = append(msg.Args, Arg{Name: key})
	} else {
		msg.appendLine(key, value)
	}
	return rest, true
}

// parseContinuation reads a "* <tag> <key>: <value>" line, which
// carries one line of a multi-line argument.
func (f *Frame) parseContinuation(in string) (*Message, bool) {
	if !strings.HasPrefix(in, "*") {
		return nil, false
	}
	rest, ok := skipSpace(in[1:])
	if !ok {
		return nil, false
	}
	tag, rest, ok := readUnquoted(rest)
	if !ok {
		return nil, false
	}
	if rest, ok = skipSpace(rest); !ok {
		return nil, false
	}
	key, rest, ok := readIdent(rest)
	if !ok || !strings.HasPrefix(rest, ":") {
		return nil, false
	}
	rest = rest[1:]
	if rest == "" || rest[0] != ' ' {
		return nil, false
	}
	// Exactly one space is consumed: the rest of the line is the
	// value, leading whitespace included, because this is how
	// arbitrary text — program source, say — survives the
	// round trip.
	msg, known := f.partial[tag]
	if !known {
		return nil, false
	}
	msg.appendLine(key, rest[1:])
	return nil, true
}

// parseEnd reads a ": <tag>" line, which closes a multi-line message.
func (f *Frame) parseEnd(in string) (*Message, bool) {
	if !strings.HasPrefix(in, ":") {
		return nil, false
	}
	rest, ok := skipSpace(in[1:])
	if !ok {
		return nil, false
	}
	tag, rest, ok := readUnquoted(rest)
	if !ok || rest != "" {
		return nil, false
	}
	msg, known := f.partial[tag]
	if !known {
		return nil, false
	}
	delete(f.partial, tag)
	msg.incomplete = false
	return msg, true
}

// dispatch hands a complete message to whatever handles its package.
// It runs with the lock released, so a handler may send.
func (f *Frame) dispatch(msg *Message) {
	if ascii.EqualFold(msg.Package, InitPackage) {
		f.handleInit(msg)
		return
	}
	for _, p := range f.registry {
		if ascii.EqualFold(p.Name, msg.Package) &&
			p.Handle != nil {
			p.Handle(f, msg, f.Supports(msg.Package))

			return
		}
	}
}

// handleInit answers the opening message: it settles the version,
// issues an authentication key, and announces the packages this
// server offers.
func (f *Frame) handleInit(msg *Message) {
	if msg.Name != "" {
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if key, ok := msg.Arg("authentication-key"); ok {
		f.authKey = key
	} else {
		f.authKey = newAuthKey()
		reply := NewMessage(InitPackage, "").
			AddArg("version", serverVersion.String()).
			AddArg("to", serverVersion.String()).
			AddArg("authentication-key", f.authKey)
		_ = f.sendMessage(reply)
	}

	from, ok := ParseVersion(firstArg(msg, "version"))
	if !ok {
		return
	}
	to := from
	if s, ok := msg.Arg("to"); ok {
		if v, ok := ParseVersion(s); ok {
			to = v
		} else {
			return
		}
	}

	f.version = SelectVersion(serverVersion, serverVersion, from, to)
	if f.version.IsZero() {
		return
	}
	f.enabled = true

	// Offered in reverse, because upstream builds its package
	// list by prepending each registration and then walks it from
	// the head. The order carries no meaning, but a transcript is
	// compared against it.
	for i := len(f.registry) - 1; i >= 0; i-- {
		p := f.registry[i]
		if ascii.EqualFold(p.Name, InitPackage) {
			continue
		}
		_ = f.sendMessage(NewMessage(NegotiatePackage, "can").
			AddArg("package", p.Name).
			AddArg("min-version", p.MinVer.String()).
			AddArg("max-version", p.MaxVer.String()))
	}
	_ = f.sendMessage(NewMessage(NegotiatePackage, "end"))
}

func firstArg(msg *Message, name string) string {
	v, _ := msg.Arg(name)
	return v
}

// NegotiateHandler records what the client says it can do. It is the
// handler for the mcp-negotiate package itself.
func NegotiateHandler(f *Frame, msg *Message, _ Version) {
	if !ascii.EqualFold(msg.Name, "can") {
		return // "end" needs no answer
	}
	pkg, ok := msg.Arg("package")
	if !ok {
		return
	}
	minVer, ok := ParseVersion(firstArg(msg, "min-version"))
	if !ok {
		return
	}
	maxVer := minVer
	if s, ok := msg.Arg("max-version"); ok {
		if v, ok := ParseVersion(s); ok {
			maxVer = v
		} else {
			return
		}
	}

	// Record the best version both sides can speak, which is what
	// later messages in that package are sent at.
	for _, p := range f.registry {
		if !ascii.EqualFold(p.Name, pkg) {
			continue
		}
		if v := SelectVersion(p.MinVer, p.MaxVer, minVer, maxVer); !v.IsZero() {
			f.agree(pkg, v)
		}
		return
	}
}

// agree records the version settled on for a package.
func (f *Frame) agree(pkg string, v Version) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.packages[ascii.Fold(pkg)] = v
}

// newAuthKey returns a key for this connection.
//
// Upstream builds one from two calls to its own generator; this uses
// the system source, because the key is what stops text a world
// prints from being obeyed as a message by a client, and a guessable
// one would defeat that.
func newAuthKey() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on any supported
		// platform, and a predictable key would be worse than
		// no MCP at all.
		panic("mcp: no randomness available: " + err.Error())
	}
	return fmt.Sprintf("%08X", binary.BigEndian.Uint32(b[:4]))
}

// StartNegotiation offers MCP to a client that has just connected.
//
// The frame is briefly marked enabled so the opening message passes
// its own "only the mcp package may be sent before negotiation"
// check, then put back: a client that does not answer has not agreed
// to anything, and everything stays plain text.
func (f *Frame) StartNegotiation() {
	f.mu.Lock()
	defer f.mu.Unlock()

	was := f.enabled
	f.enabled = true
	_ = f.sendMessage(NewMessage(InitPackage, "").
		AddArg("version", serverVersion.String()).
		AddArg("to", serverVersion.String()))
	f.enabled = was
}
