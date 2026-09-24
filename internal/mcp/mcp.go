// Package mcp implements MCP 2.1, the out-of-band message protocol
// MUD clients use to exchange structured data with a server alongside
// ordinary text.
//
// The specification is at https://www.moo.mud.org/mcp/. A message is
// a line beginning "#$#", carrying a package-qualified name and a set
// of key-value arguments; a value too long or too awkward for one
// line is sent on continuation lines tied together by a data tag.
//
// Everything here is per-connection and holds no world state, so it
// runs wherever the connection does rather than on the world
// goroutine.
package mcp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
)

// The line prefixes, from include/mcp.h.
const (
	// Prefix introduces an out-of-band message.
	Prefix = "#$#"
	// QuotePrefix introduces in-band text that happened to start
	// with the message prefix, and which the client has quoted so
	// it is not read as a message.
	QuotePrefix = `#$"`

	// InitPackage is the one package name that needs no
	// authentication key, because it is what establishes the key.
	InitPackage = "mcp"
	// NegotiatePackage carries the list of packages each side
	// supports.
	NegotiatePackage = "mcp-negotiate"
	// DataTag names the argument that ties continuation lines to
	// the message they belong to.
	DataTag = "_data-tag"

	emptyArg = `""`
)

// Version is an MCP version number.
type Version struct{ Major, Minor int }

func (v Version) String() string {
	return strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor)
}

// IsZero reports whether this is the null version, which means "no
// version in common".
func (v Version) IsZero() bool { return v.Major == 0 && v.Minor == 0 }

// less reports whether v sorts before other.
func (v Version) less(other Version) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}
	return v.Minor < other.Minor
}

// ParseVersion reads "2.1". It stops at the first character that is
// not part of a version, as upstream's hand-rolled parser does.
func ParseVersion(s string) (Version, bool) {
	major, rest, ok := leadingDigits(s)
	if !ok || !strings.HasPrefix(rest, ".") {
		return Version{}, false
	}
	minor, _, ok := leadingDigits(rest[1:])
	if !ok {
		return Version{}, false
	}
	return Version{Major: major, Minor: minor}, true
}

func leadingDigits(s string) (int, string, bool) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, s, false
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, s, false
	}
	return n, s[i:], true
}

// SelectVersion returns the highest version both sides support, or
// the null version when the ranges do not overlap.
func SelectVersion(minA, maxA, minB, maxB Version) Version {
	if maxA.less(minB) || maxB.less(minA) {
		return Version{}
	}
	if maxA.less(maxB) {
		return maxA
	}
	return maxB
}

// Arg is one argument of a message. A value may run to several lines,
// which is what the continuation lines are for.
type Arg struct {
	Name  string
	Lines []string
}

// Message is one MCP message.
type Message struct {
	// Package is the package name, and Name the message within
	// it. A message named only by its package has an empty Name.
	Package string
	Name    string
	Args    []Arg

	// dataTag ties continuation lines to this message while it is
	// still being received.
	dataTag string
	// incomplete is set while arguments are still arriving.
	incomplete bool
}

// NewMessage starts a message.
func NewMessage(pkg, name string) *Message {
	return &Message{Package: pkg, Name: name}
}

// AddArg appends a single-line argument.
func (m *Message) AddArg(name, value string) *Message {
	m.Args = append(m.Args, Arg{Name: name, Lines: []string{value}})
	return m
}

// AddMultiline appends an argument whose value spans several lines.
func (m *Message) AddMultiline(name string, lines []string) *Message {
	m.Args = append(m.Args, Arg{Name: name, Lines: lines})
	return m
}

// Arg returns an argument's first line.
func (m *Message) Arg(name string) (string, bool) {
	for _, a := range m.Args {
		if ascii.EqualFold(a.Name, name) && len(a.Lines) > 0 {
			return a.Lines[0], true
		}
	}
	return "", false
}

// Lines returns all of an argument's lines.
func (m *Message) Lines(name string) ([]string, bool) {
	for _, a := range m.Args {
		if ascii.EqualFold(a.Name, name) {
			return a.Lines, true
		}
	}
	return nil, false
}

// appendLine adds a line to an argument, creating it if it is new.
func (m *Message) appendLine(name, value string) {
	for i := range m.Args {
		if ascii.EqualFold(m.Args[i].Name, name) {
			m.Args[i].Lines = append(m.Args[i].Lines, value)
			return
		}
	}
	m.Args = append(m.Args, Arg{Name: name, Lines: []string{value}})
}

// removeArg drops an argument.
func (m *Message) removeArg(name string) {
	kept := m.Args[:0]
	for _, a := range m.Args {
		if !ascii.EqualFold(a.Name, name) {
			kept = append(kept, a)
		}
	}
	m.Args = kept
}

// FullName is how the message is written on the wire: the package,
// then the message name after a hyphen when there is one.
func (m *Message) FullName() string {
	if m.Name == "" {
		return m.Package
	}
	return m.Package + "-" + m.Name
}

// Package describes a package a connection may support.
type Package struct {
	Name           string
	MinVer, MaxVer Version
	// Handle is called for each message in this package once both
	// sides have agreed on it. It may be nil for a package that
	// is advertised but whose messages are handled elsewhere.
	Handle func(f *Frame, m *Message, v Version)
}

// escapeArg renders a value as a quoted MCP string.
func escapeArg(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		if s[i] == '"' || s[i] == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	b.WriteByte('"')
	return b.String()
}

// errNoPackage is returned when a message names a package the other
// side has not agreed to.
var errNoPackage = fmt.Errorf("mcp: the client does not support that package")

// errNotEnabled is returned when MCP has not been negotiated on a
// connection.
var errNotEnabled = fmt.Errorf("mcp: not negotiated on this connection")
