// Package match resolves the names players type into database
// objects.
//
// It follows Fuzzball's rules: names match on word-prefix boundaries,
// exits carry ';'-separated aliases and a priority level, and a
// search walks out through the environment tree rather than stopping
// at the current room.
package match

import (
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// ExitDelimiter separates an exit's aliases.
const ExitDelimiter = ';'

// StringMatch reports whether sub is a prefix of any word in src,
// ignoring ASCII case. This is how "rusty" finds "a rusty key".
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

// Matcher accumulates candidates for one name, then reports the
// winner.
//
// A search records one exact match and counts inexact ones, so an
// ambiguous name can be reported as such instead of resolving
// arbitrarily.
type Matcher struct {
	w    *world.World
	name string

	// who is the player the search is for, used by "me" and
	// "here".
	who ref.Ref
	// from is the object the search happens around, usually the
	// player.
	from ref.Ref

	exact ref.Ref
	last  ref.Ref
	count int

	// level and longest implement the exit priority rules: a
	// higher-priority exit wins, and at equal priority the longer
	// alias does.
	level   int
	longest int

	// arg is what followed the matched exit alias, for an exit
	// that runs a program and so matches a prefix of the line.
	arg string
	// verb is the part of the typed line that matched the alias,
	// which is upstream's match_cmdname. It is the text as
	// *typed* rather than the exit's own spelling, because
	// match_exits copies out of md->match_name — so an exit
	// named "West" reached by typing "west foo" gives "west".
	verb string
}

// Arg returns the text that followed a matched exit's name.
//
// An exit that leads to a program may match just the first word of
// what the player typed, and the rest becomes the program's argument:
// "@shout hello" reaches the "@shout" exit with "hello" as its
// argument.
func (m *Matcher) Arg() string { return m.arg }

// Verb returns the part of the typed line that matched an exit's
// name, which is what a program run through that exit sees in its
// COMMAND variable.
//
// It is not always the first word: an exit that does not run a
// program has to match the whole line, and then the whole line is the
// verb.
func (m *Matcher) Verb() string { return m.verb }

// New starts a search for name on behalf of who.
func New(w *world.World, who ref.Ref, name string) *Matcher {
	return &Matcher{
		w:     w,
		name:  strings.TrimSpace(name),
		who:   who,
		from:  who,
		exact: ref.Nothing, last: ref.Nothing,
	}
}

// Around changes the object the search happens around, which is how a
// program matches from somewhere other than the player.
func (m *Matcher) Around(from ref.Ref) *Matcher {
	m.from = from
	return m
}

// Result returns the match: the object found, ref.Ambiguous when
// several inexact matches tied, or ref.Nothing when there was none.
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
		if o := m.w.Get(m.who); o != nil &&
			o.Location != ref.Nothing {
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

// Nil matches the literal "nil", the exit destination that does
// nothing.
func (m *Matcher) Nil() *Matcher {
	if ascii.EqualFold(m.name, "nil") {
		m.addExact(ref.Nil)
	}
	return m
}

// Absolute matches a "#123" reference. Only a wizard may name
// arbitrary objects this way; for anyone else it resolves only to
// what they control.
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

// Registered matches a "$name" registration, looked up in the _reg
// propdir on the searching object and then outwards through the
// environment. This is how a world names its libraries:
// "$lib-strings" resolves wherever it is registered, usually on #0.
//
// The value may be stored as a dbref, an integer, or a string with or
// without a leading '#', because all three appear in real databases.
func (m *Matcher) Registered() *Matcher {
	if !strings.HasPrefix(m.name, "$") || len(m.name) == 1 {
		return m
	}
	v, _, ok := m.w.EnvProp(m.from, "_reg/"+m.name[1:])
	if !ok {
		return m
	}
	var r ref.Ref
	switch v.Type {
	case props.Ref:
		r = v.Ref
		// HOME and NIL are meaningful registrations and are
		// returned without a validity check, as upstream
		// does.
		if r == ref.Home || r == ref.Nil {
			m.addExact(r)
			return m
		}
	case props.Int:
		r = ref.Ref(v.Num)
	case props.String:
		n, err := strconv.ParseInt(strings.TrimPrefix(v.Str, "#"), 10, 32)
		if err != nil {
			return m
		}
		r = ref.Ref(n)
	default:
		return m
	}
	if m.w.Valid(r) {
		m.addExact(r)
	}
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

// Inside is match_rmatch (match.c:1001): what is in a named
// container, its exits included.
//
// Only a room, a player or a thing has anything to search — an exit
// or a program contributes nothing rather than being an error, which
// is how "get key from sword" comes to say it cannot find the key
// rather than complaining about the sword.
func (m *Matcher) Inside(container ref.Ref) *Matcher {
	o := m.w.Get(container)
	if o == nil {
		return m
	}
	switch o.Type() {
	case ref.TypeRoom, ref.TypePlayer, ref.TypeThing:
		m.matchContents(container)
		m.matchExitsOn(container)
	}
	return m
}

// Player matches a player by name, anywhere in the game. A leading
// '*' is Fuzzball's way of forcing a player match.
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

// Exits is match_all_exits (match.c:794): every action reachable from
// the searcher, in upstream's order.
//
// There are five places to look, and three of them were missing. The
// room, then **actions on things the searcher is carrying**, then
// **actions on things in the room**, then the searcher's own, and
// only then the environment chain walking out. Without the two
// object-action stages an action attached to a thing — which is
// exactly what @action makes — could not be reached at all.
//
// Two more details are upstream's. A searcher standing inside a THING
// is in a vehicle, so the environment walk continues from that
// vehicle's *home* rather than its location. And the walk is bounded
// at 88 levels, which upstream hard-codes.
//
// A nearer exit does not automatically win: exits carry a priority
// level and the highest reached takes precedence. Upstream also
// tracks block_equals, which makes an exact match at one stage
// suppress equal-priority ties at later ones; that belongs to
// choose_thing, whose tie-break this package does not model.
func (m *Matcher) Exits() *Matcher {
	o := m.w.Get(m.from)
	if o == nil {
		return m
	}

	loc := o.Location
	// A YIELD room blocks the environment chain behind it: only a
	// room flagged OVERT is matched past one.
	blocking := false
	if room := m.w.Get(loc); room != nil &&
		room.Flags&ref.Yield != 0 {
		blocking = true
	}
	m.matchRoomExits(loc)
	m.matchObjectActions(m.from)
	m.matchObjectActions(loc)
	m.matchRoomExits(m.from)

	if loc == ref.Nothing {
		return m
	}
	// Inside a vehicle, the chain continues from where the
	// vehicle lives rather than from where it happens to be.
	if room := m.w.Get(loc); room != nil &&
		room.Type() == ref.TypeThing {
		loc = room.Home
		if loc == ref.Nothing {
			return m
		}
		m.matchRoomExits(loc)
	}

	for limit := 88; limit > 0; limit-- {
		room := m.w.Get(loc)
		if room == nil {
			break
		}
		loc = room.Location
		if loc == ref.Nothing {
			break
		}
		next := m.w.Get(loc)
		if next == nil {
			break
		}
		if !blocking || next.Flags&ref.Overt != 0 {
			m.matchRoomExits(loc)
		}
		if !blocking && next.Flags&ref.Yield != 0 {
			blocking = true
		}
	}
	return m
}

// matchRoomExits is match_room_exits: the actions attached to one
// object, when that object is a kind that can hold any.
func (m *Matcher) matchRoomExits(loc ref.Ref) {
	o := m.w.Get(loc)
	if o == nil {
		return
	}
	switch o.Type() {
	case ref.TypePlayer, ref.TypeRoom, ref.TypeThing:
		m.matchExitsOn(loc)
	}
}

// matchObjectActions is match_invobj_actions and
// match_roomobj_actions, which are the same function with a different
// container: the actions attached to any *thing* inside it.
func (m *Matcher) matchObjectActions(container ref.Ref) {
	if container == ref.Nothing {
		return
	}
	for _, r := range m.w.Contents(container) {
		o := m.w.Get(r)
		if o != nil && o.Type() == ref.TypeThing {
			m.matchExitsOn(r)
		}
	}
}

// matchExitsOn tries every exit attached to one object.
func (m *Matcher) matchExitsOn(on ref.Ref) {
	for _, r := range m.w.Exits(on) {
		e := m.w.Get(r)
		if e == nil {
			continue
		}
		alias, verb, arg, ok := matchAlias(e.Name, m.name,
			m.runsProgram(e))
		if !ok {
			continue
		}
		lev := priority(e.Flags)
		switch {
		case lev > m.level:
			m.level, m.longest = lev, len(alias)
			m.verb, m.arg = verb, arg
			m.exact, m.last, m.count = r, r, 1
		case lev == m.level && len(alias) > m.longest:
			m.longest = len(alias)
			m.verb, m.arg = verb, arg
			m.exact, m.last, m.count = r, r, 1
		case lev == m.level && len(alias) == m.longest && r != m.last:
			m.count++
		}
	}
}

// runsProgram reports whether an exit leads somewhere that takes an
// argument rather than moving the player.
//
// Such an exit matches only the first word of what was typed, leaving
// the rest as its argument. HAVEN marks an exit as taking one even
// when it does not lead to a program.
func (m *Matcher) runsProgram(e *world.Object) bool {
	if e.Flags&ref.Haven != 0 {
		return true
	}
	for _, d := range e.Dest {
		if d == ref.Nil {
			return true
		}
		if o := m.w.Get(d); o != nil &&
			o.Type() == ref.TypeProgram {
			return true
		}
	}
	return false
}

// matchAlias reports whether name matches any of an exit's
// ';'-separated aliases. It returns the alias that matched, the part
// of the typed line that matched it, and whatever followed.
//
// An exit that takes an argument matches a prefix ending at a space;
// any other exit must match the whole of what was typed.
//
// The alias and the verb differ in case: the alias is the exit's own
// spelling, which is what the priority rules measure, and the verb is
// what the player typed, which is what upstream's match_cmdname
// carries.
func matchAlias(exitName, name string, takesArg bool) (
	alias, verb, arg string, ok bool) {

	first := name
	rest := ""
	if takesArg {
		if before, after, found := strings.Cut(name, " "); found {
			first, rest = before, strings.TrimSpace(after)
		}
	}

	for _, a := range strings.Split(exitName, string(ExitDelimiter)) {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if ascii.EqualFold(a, name) {
			// An exact match on the whole line wins, and
			// leaves no argument.
			return a, name, "", true
		}
		if takesArg && ascii.EqualFold(a, first) {
			return a, first, rest, true
		}
	}
	return "", "", "", false
}

// priority is Fuzzball's PLevel: an exit's mucker bits raise how
// strongly it binds, and an ABODE exit binds more weakly than the
// default.
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

// Everything is match_everything (match.c:884): the searches a bare
// command name should try, in the order Fuzzball tries them.
//
// Two of them were missing and their absence was wide. **Registered**
// means a "$name" resolves for every command that takes an object —
// without it "look $wid" and "@describe $wid=..." could not find
// anything a program had registered. And **Player**, which upstream
// adds when the searcher or its owner is a wizard, so a wizard can
// name somebody who is elsewhere; several callers were adding it by
// hand, which is now redundant rather than wrong.
func (m *Matcher) Everything() *Matcher {
	m = m.Exits().Neighbor().Possession().Me().Here().
		Registered().Absolute()
	if m.wizardSearcher() {
		m = m.Player()
	}
	return m
}

// wizardSearcher is match_everything's own test: the object the
// search happens around, or its owner, or the player being answered.
func (m *Matcher) wizardSearcher() bool {
	for _, r := range []ref.Ref{m.from, m.who} {
		o := m.w.Get(r)
		if o == nil {
			continue
		}
		if o.Flags.IsWizard() {
			return true
		}
		if owner := m.w.Get(o.Owner); owner != nil &&
			owner.Flags.IsWizard() {
			return true
		}
	}
	return false
}

// Thing runs the searches for naming an object to act on, which
// excludes exits.
func (m *Matcher) Thing() *Matcher {
	return m.Absolute().Me().Here().Possession().Neighbor()
}
