package world

import (
	"fmt"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Violation is one problem found in the object graph.
//
// The message is upstream's, because these reports are read by people who
// already know what Fuzzball's say, and because a database salvaged by hand
// is followed by a re-run that has to be comparable.
type Violation struct {
	// Ref is the object at fault. It may not exist, which is itself the
	// finding in some cases.
	Ref ref.Ref
	// Problem completes the sentence "Object <name> ...!".
	Problem string
}

// Check walks the whole object graph looking for inconsistency.
//
// Findings and progress notes are handed to the callbacks as the scan reaches
// them, rather than collected and returned, because a scan of a large database
// takes long enough that an operator wants to see it moving.
//
// This checks the in-memory graph rather than Postgres, because the in-memory
// graph is what the game runs on: the store is a write-behind copy of it, and
// a check made against the copy would pass while the running world was broken.
// Whether the store agrees with memory is a different question, and one the
// flush already answers by writing the whole of every changed object.
func (w *World) Check(note func(string), report func(Violation)) {
	violate := func(r ref.Ref, problem string) {
		report(Violation{Ref: r, Problem: problem})
	}

	// Progress is reported in blocks, as upstream does, so the numbers in a
	// transcript line up with the ones people are used to seeing.
	const block = 10000
	for r := ref.Ref(0); r < w.top; r++ {
		if r%block == 0 {
			last := r + block - 1
			if last >= w.top {
				last = w.top - 1
			}
			note(fmt.Sprintf("Checking objects %d to %d...", int32(r), int32(last)))
		}
		if o := w.objs[r]; o != nil {
			w.checkObject(o, violate)
		}
	}

	note("Searching for orphan objects...")
	w.findOrphans(violate)
}

// checkObject applies the checks that do not need the whole database.
func (w *World) checkObject(o *Object, report func(ref.Ref, string)) {
	if o.Name == "" {
		report(o.Ref, "doesn't have a name")
	}

	if o.Type() != ref.TypeGarbage {
		switch owner := w.Get(o.Owner); {
		case !w.Valid(o.Owner):
			report(o.Ref, "has an invalid object as its owner.")
		case owner.Type() != ref.TypePlayer:
			report(o.Ref, "has a non-player object as its owner.")
		}
		// The global environment is the one object allowed to be
		// nowhere: it is the root that everything else hangs from.
		if !w.Valid(o.Location) &&
			!(o.Ref == ref.GlobalEnvironment && o.Location == ref.Nothing) {
			report(o.Ref, "has an invalid object as its location")
		}
	}

	if loc := w.Get(o.Location); loc != nil {
		switch loc.Type() {
		case ref.TypeGarbage, ref.TypeExit, ref.TypeProgram:
			report(o.Ref, "thinks it is located in a non-container object")
		}
	}
	if o.Type() == ref.TypeGarbage && o.Location != ref.Nothing {
		report(o.Ref, "is a garbage object with a location that isn't #-1")
	}

	w.checkContentsList(o, report)
	w.checkExitsList(o, report)

	switch o.Type() {
	case ref.TypeRoom:
		// A drop-to may be HOME, or a room or thing to drop into.
		if !w.Valid(o.Dropto) && o.Dropto != ref.Nothing && o.Dropto != ref.Home {
			report(o.Ref, "has its dropto set to an invalid object")
		} else if d := w.Get(o.Dropto); d != nil &&
			d.Type() != ref.TypeThing && d.Type() != ref.TypeRoom {
			report(o.Ref, "has its dropto set to a non-room, non-thing object")
		}
	case ref.TypeThing:
		switch h := w.Get(o.Home); {
		case h == nil:
			report(o.Ref, "has its home set to an invalid object")
		case h.Type() != ref.TypeRoom && h.Type() != ref.TypeThing &&
			h.Type() != ref.TypePlayer:
			report(o.Ref, "has its home set to an object that is not a room, thing, or player")
		}
	case ref.TypePlayer:
		switch h := w.Get(o.Home); {
		case h == nil:
			report(o.Ref, "has its home set to an invalid object")
		case h.Type() != ref.TypeRoom:
			report(o.Ref, "has its home set to a non-room object")
		}
	case ref.TypeExit:
		for _, d := range o.Dest {
			if !w.Valid(d) && d != ref.Home && d != ref.Nil {
				report(o.Ref, "has an invalid object as one of its link destinations")
			}
		}
	case ref.TypeGarbage:
		if n := w.Get(o.Next); n != nil && n.Type() != ref.TypeGarbage {
			report(o.Ref, "has a non-garbage object as the 'next' object in the garbage chain")
		}
	case ref.TypeProgram:
		// Nothing type-specific: a program's source is checked by
		// compiling it, not by looking at the object.
	default:
		report(o.Ref, "has an unknown object type, and its flags may also be corrupt")
	}
}

// holdsChains reports whether an object is one that may have contents and
// exits at all. Programs, exits and garbage may not.
func holdsChains(o *Object) bool {
	switch o.Type() {
	case ref.TypeProgram, ref.TypeExit, ref.TypeGarbage:
		return false
	}
	return true
}

// emptyChainProblem names the complaint for a type that should have no chain.
func emptyChainProblem(o *Object, which string) string {
	switch o.Type() {
	case ref.TypeExit:
		return "is an exit/action whose " + which
	case ref.TypeGarbage:
		return "is a garbage object whose " + which
	default:
		return "is a program whose " + which
	}
}

// checkContentsList walks an object's contents, which must all be non-exits
// that agree about where they are.
func (w *World) checkContentsList(o *Object, report func(ref.Ref, string)) {
	if !holdsChains(o) {
		if o.Contents != ref.Nothing {
			report(o.Ref, emptyChainProblem(o, "contents aren't #-1"))
		}
		return
	}

	at, limit := o.Contents, w.Len()+1
	for {
		m := w.Get(at)
		if m == nil || m.Location != o.Ref || m.Type() == ref.TypeExit {
			break
		}
		if limit--; limit == 0 {
			// The walk outlasted the database, so the chain must
			// loop. Which link closes it is a separate finding.
			w.checkNextChain(o.Contents, report)
			report(o.Ref, "is the containing object, and has a loop in its contents chain")
			return
		}
		at = m.Next
	}
	if at == ref.Nothing {
		return
	}
	m := w.Get(at)
	if m == nil {
		report(o.Ref, "has an invalid object in its contents list")
		return
	}
	if m.Type() == ref.TypeExit {
		report(o.Ref, "has an exit in its contents list (it shouldn't)")
	}
	if m.Location != o.Ref {
		report(o.Ref, "has an object in its contents lists that thinks it is located elsewhere")
	}
}

// checkExitsList is the same for the exits chain, which must hold only exits.
func (w *World) checkExitsList(o *Object, report func(ref.Ref, string)) {
	if !holdsChains(o) {
		if o.Exits != ref.Nothing {
			report(o.Ref, emptyChainProblem(o, "exits list isn't #-1"))
		}
		return
	}

	at, limit := o.Exits, w.Len()+1
	for {
		m := w.Get(at)
		if m == nil || m.Location != o.Ref || m.Type() != ref.TypeExit {
			break
		}
		if limit--; limit == 0 {
			w.checkNextChain(o.Exits, report)
			report(o.Ref, "is the containing object, and has the loop in its exits chain")
			return
		}
		at = m.Next
	}
	if at == ref.Nothing {
		return
	}
	m := w.Get(at)
	if m == nil {
		report(o.Ref, "has an invalid object in its exits list")
		return
	}
	if m.Type() != ref.TypeExit {
		report(o.Ref, "has a non-exit in its exits list")
	}
	if m.Location != o.Ref {
		report(o.Ref, "has an exit in its exits lists that thinks it is located elsewhere")
	}
}

// checkNextChain finds the link that closes a loop, so a report names the
// object that has to be cut rather than only the container.
func (w *World) checkNextChain(head ref.Ref, report func(ref.Ref, string)) {
	seen := map[ref.Ref]bool{}
	at := head
	for at != ref.Nothing {
		o := w.Get(at)
		if o == nil {
			report(at, "has an invalid object in its 'next' chain")
			return
		}
		if seen[o.Next] {
			report(at, "has a 'next' field that forms an illegal loop in an object chain")
			return
		}
		seen[at] = true
		at = o.Next
	}
}

// findOrphans looks for objects nothing points at, and for objects more than
// one thing points at.
//
// Every object should appear exactly once across all the contents, exits and
// next links in the database — the global environment and the head of the
// recycle chain excepted, which are roots. An object appearing twice means two
// containers believe they hold it; an object appearing nowhere has been
// dropped out of the graph and is unreachable.
func (w *World) findOrphans(report func(ref.Ref, string)) {
	seen := make(map[ref.Ref]bool, w.Len())
	seen[ref.GlobalEnvironment] = true

	claim := func(r ref.Ref) {
		if r == ref.Nothing {
			return
		}
		if seen[r] {
			report(r, "is referred to by more than one object's Next, Contents, or Exits field")
			return
		}
		seen[r] = true
	}
	w.Each(func(o *Object) bool {
		claim(o.Exits)
		claim(o.Contents)
		claim(o.Next)
		return true
	})
	w.Each(func(o *Object) bool {
		if !seen[o.Ref] {
			report(o.Ref, "appears to be an orphan object, that is not referred to by any other object")
		}
		return true
	})
}
