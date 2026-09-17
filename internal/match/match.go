// Package match resolves the names players type into database objects.
//
// It follows Fuzzball's rules: names match on word-prefix boundaries, exits
// carry ';'-separated aliases and a priority level, and a search walks out
// through the environment tree rather than stopping at the current room.
package match

import (
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// ExitDelimiter separates an exit's aliases.
const ExitDelimiter = ';'

// StringMatch reports whether sub is a prefix of any word in src, ignoring
// ASCII case. This is how "rusty" finds "a rusty key".
func StringMatch(src, sub string) bool {
	if sub == "" {
		return false
	}
	for i := 0; i < len(src); {
		if ascii.HasPrefix(src[i:], sub) {
			return true
		}
		// Skip to the start of the next word.
		for i < len(src) && isAlnum(src[i]) {
			i++
		}
		for i < len(src) && !isAlnum(src[i]) {
			i++
		}
	}
	return false
}

func isAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// Matcher accumulates candidates for one name, then reports the winner.
//
// A search records one exact match and counts inexact ones, so an ambiguous
// name can be reported as such instead of resolving arbitrarily.
type Matcher struct {
	w    *world.World
	name string

	// who is the player the search is for, used by "me" and "here".
	who ref.Ref
	// from is the object the search happens around, usually the player.
	from ref.Ref

	exact ref.Ref
	last  ref.Ref
	count int

	// level and longest implement the exit priority rules: a
	// higher-priority exit wins, and at equal priority the longer alias
	// does.
	level   int
	longest int
}

// New starts a search for name on behalf of who.
func New(w *world.World, who ref.Ref, name string) *Matcher {
	return &Matcher{
		w: w, name: strings.TrimSpace(name), who: who, from: who,
		exact: ref.Nothing, last: ref.Nothing,
	}
}

// Around changes the object the search happens around, which is how a program
// matches from somewhere other than the player.
func (m *Matcher) Around(from ref.Ref) *Matcher {
	m.from = from
	return m
}

// Result returns the match: the object found, ref.Ambiguous when several
// inexact matches tied, or ref.Nothing when there was none.
func (m *Matcher) Result() ref.Ref {
	if m.exact != ref.Nothing {
		return m.exact
	}
	switch m.count {
	case 0:
		return ref.Nothing
	case 1:
		return m.last
	default:
		return ref.Ambiguous
	}
}

// addExact records an unambiguous hit.
func (m *Matcher) addExact(r ref.Ref) { m.exact = r }

// add records an inexact hit.
func (m *Matcher) add(r ref.Ref) {
	if r == m.last {
		return // the same object found twice is not ambiguity
	}
	m.last = r
	m.count++
}

// Me matches the literal "me".
func (m *Matcher) Me() *Matcher {
	if ascii.EqualFold(m.name, "me") {
		m.addExact(m.who)
	}
	return m
}

// Here matches the literal "here", the room the player is in.
func (m *Matcher) Here() *Matcher {
	if ascii.EqualFold(m.name, "here") {
		if o := m.w.Get(m.who); o != nil && o.Location != ref.Nothing {
			m.addExact(o.Location)
		}
	}
	return m
}

// Home matches the literal "home".
func (m *Matcher) Home() *Matcher {
	if ascii.EqualFold(m.name, "home") {
		m.addExact(ref.Home)
	}
	return m
}

// Nil matches the literal "nil", the exit destination that does nothing.
func (m *Matcher) Nil() *Matcher {
	if ascii.EqualFold(m.name, "nil") {
		m.addExact(ref.Nil)
	}
	return m
}

// Absolute matches a "#123" reference. Only a wizard may name arbitrary
// objects this way; for anyone else it resolves only to what they control.
func (m *Matcher) Absolute() *Matcher {
	r, ok := parseAbsolute(m.name)
	if !ok {
		return m
	}
	if !m.w.Valid(r) {
		return m
	}
	if !m.controls(r) {
		return m
	}
	m.addExact(r)
	return m
}

// parseAbsolute reads a "#123" reference.
func parseAbsolute(name string) (ref.Ref, bool) {
	if !strings.HasPrefix(name, "#") {
		return ref.Nothing, false
	}
	n, err := strconv.ParseInt(name[1:], 10, 32)
	if err != nil {
		return ref.Nothing, false
	}
	return ref.Ref(n), true
}

// controls reports whether the searching player may name r directly.
func (m *Matcher) controls(r ref.Ref) bool {
	who := m.w.Get(m.who)
	target := m.w.Get(r)
	if who == nil || target == nil {
		return false
	}
	if who.Flags.IsWizard() {
		return true
	}
	owner := who.Owner
	if who.Type() == ref.TypePlayer {
		owner = m.who
	}
	return target.Owner == owner || r == m.who
}

// Neighbor matches something in the same room as the player.
func (m *Matcher) Neighbor() *Matcher {
	o := m.w.Get(m.from)
	if o == nil || o.Location == ref.Nothing {
		return m
	}
	m.matchContents(o.Location)
	return m
}

// Possession matches something the player is carrying.
func (m *Matcher) Possession() *Matcher {
	m.matchContents(m.from)
	return m
}

func (m *Matcher) matchContents(container ref.Ref) {
	for _, r := range m.w.Contents(container) {
		o := m.w.Get(r)
		if o == nil {
			continue
		}
		if ascii.EqualFold(o.Name, m.name) {
			m.addExact(r)
			continue
		}
		if StringMatch(o.Name, m.name) {
			m.add(r)
		}
	}
}

// Player matches a player by name, anywhere in the game. A leading '*' is
// Fuzzball's way of forcing a player match.
func (m *Matcher) Player() *Matcher {
	name := strings.TrimPrefix(m.name, "*")
	if name == "" {
		return m
	}
	if r, ok := m.w.PlayerNamed(name); ok {
		m.addExact(r)
	}
	return m
}

// Exits matches an exit reachable from the player, walking out through the
// environment tree. A nearer exit does not automatically win: exits carry a
// priority level, and the highest one reached takes precedence.
func (m *Matcher) Exits() *Matcher {
	o := m.w.Get(m.from)
	if o == nil {
		return m
	}
	// Exits attached to what the player is carrying are reachable too.
	m.matchExitsOn(m.from)

	loc := o.Location
	// Bounded, so a cycle in a damaged environment tree cannot hang the
	// world goroutine.
	for i := 0; loc != ref.Nothing && i <= m.w.Len(); i++ {
		m.matchExitsOn(loc)
		parent := m.w.Get(loc)
		if parent == nil {
			break
		}
		loc = parent.Location
	}
	return m
}

// matchExitsOn tries every exit attached to one object.
func (m *Matcher) matchExitsOn(on ref.Ref) {
	for _, r := range m.w.Exits(on) {
		e := m.w.Get(r)
		if e == nil {
			continue
		}
		alias, ok := matchAlias(e.Name, m.name)
		if !ok {
			continue
		}
		lev := priority(e.Flags)
		switch {
		case lev > m.level:
			m.level, m.longest = lev, len(alias)
			m.exact, m.last, m.count = r, r, 1
		case lev == m.level && len(alias) > m.longest:
			m.longest = len(alias)
			m.exact, m.last, m.count = r, r, 1
		case lev == m.level && len(alias) == m.longest && r != m.last:
			m.count++
		}
	}
}

// matchAlias reports whether name matches any of an exit's ';'-separated
// aliases, returning the alias that matched.
func matchAlias(exitName, name string) (string, bool) {
	for _, alias := range strings.Split(exitName, string(ExitDelimiter)) {
		alias = strings.TrimSpace(alias)
		if alias != "" && ascii.EqualFold(alias, name) {
			return alias, true
		}
	}
	return "", false
}

// priority is Fuzzball's PLevel: an exit's mucker bits raise how strongly it
// binds, and an ABODE exit binds more weakly than the default.
func priority(f ref.Flags) int {
	if f&(ref.Mucker|ref.SMucker) != 0 {
		lev := 1
		if f&ref.Mucker != 0 {
			lev += 2
		}
		if f&ref.SMucker != 0 {
			lev++
		}
		return lev
	}
	if f&ref.Abode != 0 {
		return 0
	}
	return 1
}

// Everything runs the searches a bare command name should try, in the order
// Fuzzball tries them.
func (m *Matcher) Everything() *Matcher {
	return m.Absolute().Me().Here().Possession().Neighbor().Exits()
}

// Thing runs the searches for naming an object to act on, which excludes
// exits.
func (m *Matcher) Thing() *Matcher {
	return m.Absolute().Me().Here().Possession().Neighbor()
}
