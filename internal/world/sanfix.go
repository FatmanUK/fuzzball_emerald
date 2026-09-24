package world

import (
	"fmt"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Fix repairs what Check reports, and returns a log of what it
// changed.
//
// The order matters: every object's own fields are corrected first,
// and the containment chains are rebuilt afterwards from the
// locations that correction left behind. Upstream has to do the
// reverse — cut the bad chains, then guess where the loose objects
// belong — because a chain is the only record it has of where
// something is. Emerald stores each object's location as well, so a
// damaged chain can simply be discarded and rebuilt from what the
// objects themselves say.
//
// Some damage cannot be repaired, only reported: an object of an
// unknown type has nothing left to reason from. Those come back in
// the returned violations, and mean the database still needs a
// person.
func (w *World) Fix() (log []string, unfixed []Violation) {
	note := func(format string, args ...any) {
		log = append(log, fmt.Sprintf(format, args...))
	}

	// A player_start that is not a room would send every repaired
	// player nowhere, so it is the first thing checked.
	if start := w.Get(w.Tune.Ref("player_start")); start == nil ||
		start.Type() != ref.TypeRoom {
		_ = w.SetTune("player_start", ref.GlobalEnvironment.String())
		note("Reset invalid player_start to %v", ref.GlobalEnvironment)
	}

	var lostRoom, lostPlayer ref.Ref = ref.Nothing, ref.Nothing
	lostAndFound := func() (ref.Ref, ref.Ref) {
		if lostRoom == ref.Nothing {
			lostRoom, lostPlayer = w.createLostAndFound(&log)
		}
		return lostRoom, lostPlayer
	}

	for r := ref.Ref(0); r < w.top; r++ {
		o := w.objs[r]
		if o == nil {
			continue
		}
		if !knownType(o.Type()) {
			unfixed = append(unfixed, Violation{
				Ref:     r,
				Problem: "has an unknown object type, and its flags may also be corrupt",
			})
			continue
		}
		w.fixName(o, &log)
		if o.Type() == ref.TypeGarbage {
			w.fixGarbage(o, &log)
		} else {
			w.fixOwner(o, lostAndFound, &log)
			w.fixLocation(o, lostAndFound, &log)
		}
		w.fixLinks(o, &log)
	}

	// The global environment is the root: it is inside nothing
	// and on no chain, and a copy of it appearing in one would
	// make it reachable twice.
	if root := w.objs[ref.GlobalEnvironment]; root != nil {
		if root.Next != ref.Nothing {
			note("Removed the global environment %v from a chain", ref.GlobalEnvironment)
			root.Next = ref.Nothing
			w.Modified(ref.GlobalEnvironment)
		}
		if root.Location != ref.Nothing {
			note("Removed the global environment %v from %v",
				ref.GlobalEnvironment, root.Location)
			root.Location = ref.Nothing
			w.Modified(ref.GlobalEnvironment)
		}
	}

	if n := w.RepairChains(); n > 0 {
		note("Rebuilt the contents or exits chains of %d container%s", n, plural(n))
	}
	return log, unfixed
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// knownType reports whether a type is one the database defines.
func knownType(t ref.ObjType) bool {
	switch t {
	case ref.TypeRoom, ref.TypeThing, ref.TypePlayer,
		ref.TypeExit, ref.TypeProgram, ref.TypeGarbage:
		return true
	}
	return false
}

// fixName gives a nameless object one, because a name is how
// everything else refers to it.
func (w *World) fixName(o *Object, log *[]string) {
	if o.Name != "" {
		return
	}
	switch o.Type() {
	case ref.TypeGarbage:
		o.Name = "<garbage>"
	case ref.TypePlayer:
		name := "Unnamed"
		for n := 1; ; n++ {
			if _, taken := w.players[ascii.Fold(name)]; !taken {
				break
			}
			name = fmt.Sprintf("Unnamed%d", n)
		}
		o.Name = name
		w.players[ascii.Fold(name)] = o.Ref
	default:
		o.Name = "Unnamed"
	}
	*log = append(*log, fmt.Sprintf("Gave a name to %s", w.describe(o.Ref)))
	w.Modified(o.Ref)
}

// fixOwner points an object at a real player, because permission
// checks read the owner and an owner that is not a player answers
// nothing.
func (w *World) fixOwner(o *Object, lostAndFound func() (ref.Ref, ref.Ref), log *[]string) {
	owner := w.Get(o.Owner)
	if owner != nil && owner.Type() == ref.TypePlayer {
		return
	}
	_, player := lostAndFound()
	*log = append(*log, fmt.Sprintf("Set owner of %s to %s",
		w.describe(o.Ref), w.describe(player)))
	o.Owner = player
	w.Modified(o.Ref)
}

// fixLocation puts an object somewhere that can hold it.
func (w *World) fixLocation(o *Object, lostAndFound func() (ref.Ref, ref.Ref), log *[]string) {
	if o.Ref == ref.GlobalEnvironment {
		return
	}
	loc := w.Get(o.Location)
	ok := loc != nil
	if ok {
		switch loc.Type() {
		case ref.TypeGarbage, ref.TypeExit, ref.TypeProgram:
			ok = false
		case ref.TypePlayer:
			// A player inside a player is how a toad or a
			// half-finished move leaves things, and
			// neither can get out on their own.
			ok = o.Type() != ref.TypePlayer
		}
	}
	if ok {
		return
	}

	if o.Type() == ref.TypePlayer {
		o.Location = w.Tune.Ref("player_start")
	} else {
		room, _ := lostAndFound()
		o.Location = room
	}
	*log = append(*log, fmt.Sprintf("Set location of %s to %s",
		w.describe(o.Ref), w.describe(o.Location)))
	w.Modified(o.Ref)
}

// fixGarbage clears the fields recycled objects must not hold.
func (w *World) fixGarbage(o *Object, log *[]string) {
	if o.Owner != ref.Nothing {
		*log = append(*log, fmt.Sprintf("Set owner of recycled object %v to NOTHING", o.Ref))
		o.Owner = ref.Nothing
		w.Modified(o.Ref)
	}
	if o.Location != ref.Nothing {
		*log = append(*log, fmt.Sprintf("Set location of recycled object %v to NOTHING", o.Ref))
		o.Location = ref.Nothing
		w.Modified(o.Ref)
	}
}

// fixLinks corrects the type-specific references: a room's drop-to, a
// home, an exit's destinations.
func (w *World) fixLinks(o *Object, log *[]string) {
	switch o.Type() {
	case ref.TypeRoom:
		d := w.Get(o.Dropto)
		if o.Dropto == ref.Nothing || o.Dropto == ref.Home {
			return
		}
		if d == nil {
			*log = append(*log, fmt.Sprintf("Removing invalid drop-to from %s", w.describe(o.Ref)))
		} else if d.Type() != ref.TypeThing && d.Type() != ref.TypeRoom {
			*log = append(*log, fmt.Sprintf("Removing drop-to on %s to %s",
				w.describe(o.Ref), w.describe(o.Dropto)))
		} else {
			return
		}
		o.Dropto = ref.Nothing
		w.Modified(o.Ref)

	case ref.TypeThing:
		h := w.Get(o.Home)
		if h != nil &&
			(h.Type() == ref.TypeRoom || h.Type() == ref.TypeThing ||
				h.Type() == ref.TypePlayer) {
			return
		}
		*log = append(*log, fmt.Sprintf("Setting the home on %s to %s, its owner",
			w.describe(o.Ref), w.describe(o.Owner)))
		o.Home = o.Owner
		w.Modified(o.Ref)

	case ref.TypePlayer:
		if h := w.Get(o.Home); h != nil &&
			h.Type() == ref.TypeRoom {
			return
		}
		o.Home = w.Tune.Ref("player_start")
		*log = append(*log, fmt.Sprintf("Setting the home on %s to %s",
			w.describe(o.Ref), w.describe(o.Home)))
		w.Modified(o.Ref)

	case ref.TypeExit:
		kept := o.Dest[:0]
		for _, d := range o.Dest {
			if w.Valid(d) || d == ref.Home ||
				d == ref.Nil {
				kept = append(kept, d)
				continue
			}
			*log = append(*log, fmt.Sprintf("Removing invalid destination from %s",
				w.describe(o.Ref)))
		}
		if len(kept) != len(o.Dest) {
			o.Dest = kept
			w.Modified(o.Ref)
		}
	}
}

// createLostAndFound makes somewhere to put objects whose owner or
// location cannot be worked out, so nothing has to be thrown away to
// make the database consistent.
//
// The player it creates has no usable password: it exists to own
// things, not to be logged into. Upstream generates a random one and
// writes it to the repair log; leaving it unset is the same thing
// without a credential lying around in a file.
func (w *World) createLostAndFound(log *[]string) (room, player ref.Ref) {
	r := w.Create("lost+found", ref.TypeRoom, ref.Nothing)
	r.Location = ref.GlobalEnvironment
	r.Dropto = ref.Nothing
	*log = append(*log, fmt.Sprintf("Using %s to resolve unknown location", w.describe(r.Ref)))

	name := "lost+found"
	for n := 1; ; n++ {
		if _, taken := w.players[ascii.Fold(name)]; !taken {
			break
		}
		name = fmt.Sprintf("lost+found%d", n)
	}
	p := w.Create(name, ref.TypePlayer, ref.Nothing)
	p.Owner = p.Ref
	p.Location = r.Ref
	p.Home = r.Ref
	w.players[ascii.Fold(name)] = p.Ref
	*log = append(*log, fmt.Sprintf("Using %s to resolve unknown owner", w.describe(p.Ref)))

	r.Owner = p.Ref
	w.Modified(r.Ref)
	w.Modified(p.Ref)
	return r.Ref, p.Ref
}

// describe renders an object for a repair log: its name and dbref, or
// what went wrong if there is no object there.
func (w *World) describe(r ref.Ref) string {
	o := w.Get(r)
	if o == nil {
		return "*INVALID*"
	}
	return o.Name + "(" + r.String() + o.Flags.Unparse() + ")"
}
