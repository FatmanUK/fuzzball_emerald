package world

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Snapshot is a batch of changes handed to the persister. Objects in
// it are deep copies taken on the world goroutine, so the persister
// can take as long as it likes without ever observing a half-written
// object.
type Snapshot struct {
	Objects []*Object
	Deleted []ref.Ref
	// Tune carries the whole parameter table when it changed, and
	// is nil otherwise. The table is small and changes rarely, so
	// there is no point tracking individual parameters.
	Tune map[string]string
	// Programs carries the MUF source of every program saved
	// since the last snapshot. Source is held apart from the
	// object because the editor rewrites text without changing
	// any field on the object itself.
	Programs map[ref.Ref]string
	// Macros carries the whole editor macro table when it
	// changed, for the same reason Tune does.
	Macros []Macro
	// Top is the world's ref ceiling at the time of the snapshot.
	Top ref.Ref
}

// Empty reports whether there is nothing to write.
func (s Snapshot) Empty() bool {
	return len(s.Objects) == 0 && len(s.Deleted) == 0 && s.Tune == nil &&
		len(s.Programs) == 0 && s.Macros == nil
}

// TakeSnapshot copies out everything changed since the last call and
// clears the dirty set. It must run on the world goroutine.
func (w *World) TakeSnapshot() Snapshot {
	s := Snapshot{Top: w.top}

	if len(w.dirty) > 0 {
		s.Objects = make([]*Object, 0, len(w.dirty))
		for r := range w.dirty {
			if o := w.objs[r]; o != nil {
				s.Objects = append(s.Objects, o.Clone())
			}
		}
		clear(w.dirty)
	}

	if len(w.deleted) > 0 {
		s.Deleted = make([]ref.Ref, 0, len(w.deleted))
		for r := range w.deleted {
			s.Deleted = append(s.Deleted, r)
		}
		clear(w.deleted)
	}

	if len(w.progDirty) > 0 {
		s.Programs = make(map[ref.Ref]string, len(w.progDirty))
		for r := range w.progDirty {
			s.Programs[r] = w.programs[r]
		}
		clear(w.progDirty)
	}

	if w.macrosDirty {
		s.Macros = w.Macros()
		w.macrosDirty = false
	}

	if w.tuneDirty {
		s.Tune = make(map[string]string)
		for _, p := range w.Tune.Params() {
			v, _ := w.Tune.Get(p.Name)
			s.Tune[p.Name] = p.Format(v)
		}
		w.tuneDirty = false
	}
	return s
}

// DirtyCount reports how many objects are waiting to be written.
// Tests and diagnostics use it; the flush loop does not need it.
func (w *World) DirtyCount() int { return len(w.dirty) }

// MarkAllDirty queues every object for writing, which is what a fresh
// import needs.
func (w *World) MarkAllDirty() {
	for r := range w.objs {
		w.dirty[r] = struct{}{}
	}
	for r := range w.programs {
		w.progDirty[r] = struct{}{}
	}
	w.tuneDirty = true
	w.macrosDirty = true
}
