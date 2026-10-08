package world

import (
	"fmt"
	"strconv"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/tune"
)

// World is the object graph. It is owned by a single goroutine and
// has no internal locking; reach it through an Engine.
type World struct {
	objs map[ref.Ref]*Object
	// top is one past the highest ref ever allocated, matching
	// db_top.
	top ref.Ref

	// players indexes player refs by case-folded name, as
	// upstream's player hash table does.
	players map[string]ref.Ref

	// dirty accumulates refs changed since the last flush;
	// deleted holds refs that need removing from the store.
	dirty   map[ref.Ref]struct{}
	deleted map[ref.Ref]struct{}

	// programs holds MUF source by program ref. Source is loaded
	// at boot and compiled on demand, so an edit only needs to
	// invalidate a cache.
	programs map[ref.Ref]string
	// progDirty records sources changed since the last flush.
	// Source is tracked apart from the object because leaving the
	// editor rewrites a program's text without touching any of
	// its fields.
	progDirty map[ref.Ref]struct{}

	// macros is the MUF editor's macro table, keyed by folded
	// name.
	macros map[string]Macro
	// macrosDirty records that the table changed. It is small and
	// changes rarely, so the whole table is written rather than
	// each entry.
	macrosDirty bool

	// help is the help system's corpora, keyed by folded corpus
	// name. Like macros, they belong to the world rather than to
	// any object.
	help map[string]*helpCorpus
	// helpDirty names the corpora changed since the last flush.
	// It is per corpus rather than a single flag, so appending to
	// the motd does not rewrite the manual.
	helpDirty map[string]struct{}

	// gripes are the complaints on record, oldest first, bounded
	// to the most recent gripeLimit. newGripes are the ones made
	// since the last flush: the list is append-only, so unlike
	// macros and help it is written incrementally rather than
	// whole.
	gripes    []Gripe
	newGripes []Gripe

	// recyclable is upstream's free list of garbage dbrefs
	// (db.c:53), which Create hands out before allocating a fresh
	// one. It is LIFO — the last ref recycled is the first
	// reused — and it is rebuilt from the graph on load rather
	// than stored, because garbage is recognisable by its type.
	recyclable []ref.Ref

	Tune *tune.Set
	// tuneDirty records that the parameter table changed.
	tuneDirty bool

	now func() time.Time
}

// New returns an empty world.
func New() *World {
	return &World{
		objs:     make(map[ref.Ref]*Object),
		players:  make(map[string]ref.Ref),
		dirty:    make(map[ref.Ref]struct{}),
		deleted:  make(map[ref.Ref]struct{}),
		programs: make(map[ref.Ref]string),

		progDirty: make(map[ref.Ref]struct{}),
		macros:    make(map[string]Macro),
		help:      make(map[string]*helpCorpus),
		helpDirty: make(map[string]struct{}),

		Tune: tune.NewSet(),
		now:  time.Now,
	}
}

// SetClock replaces the world's clock. Tests use it to make
// timestamps predictable.
func (w *World) SetClock(f func() time.Time) { w.now = f }

// Now returns the world's current time.
func (w *World) Now() time.Time { return w.now() }

// Top returns one past the highest allocated ref.
func (w *World) Top() ref.Ref { return w.top }

// SetTop raises the ref ceiling, which the store does on load so a
// world whose highest objects were recycled still hands out fresh
// refs.
func (w *World) SetTop(top ref.Ref) {
	if top > w.top {
		w.top = top
	}
}

// Len returns the number of live objects.
func (w *World) Len() int { return len(w.objs) }

// Get returns an object, or nil if there is none at r.
func (w *World) Get(r ref.Ref) *Object { return w.objs[r] }

// Valid reports whether r names a live, non-garbage object.
func (w *World) Valid(r ref.Ref) bool {
	o := w.objs[r]
	return o != nil && o.Type() != ref.TypeGarbage
}

// Touch marks an object as changed so the next flush persists it.
func (w *World) Touch(r ref.Ref) {
	if _, ok := w.objs[r]; ok {
		w.dirty[r] = struct{}{}
	}
}

// Modified marks an object changed and updates its modification
// timestamp, which is what upstream's ts_modifyobject does.
func (w *World) Modified(r ref.Ref) {
	if o := w.objs[r]; o != nil {
		o.Modified = w.now()
		w.dirty[r] = struct{}{}
	}
}

// Used is ts_useobject (fbtime.c:47): an object's use count and
// last-used timestamp, and **for a room its parent's as well**, all
// the way up. Upstream's own comment says so — "Room parent rooms
// will be 'used' if their child rooms are 'used'" — and only
// LastUsed had the walk, so a parent room's use count stopped
// counting what happened beneath it. The composition case found it:
// twelve uses upstream against six here, in a world two rooms deep.
//
// The walk is bounded, where upstream's recursion is not: a cycle in
// a damaged parent chain would take the server down rather than
// return.
func (w *World) Used(r ref.Ref) {
	for i := 0; r != ref.Nothing && i <= lastUsedMaxDepth; i++ {
		o := w.objs[r]
		if o == nil {
			return
		}
		o.LastUsed = w.now()
		o.UseCount++
		w.dirty[r] = struct{}{}
		if o.Type() != ref.TypeRoom {
			return
		}
		r = o.Location
	}
}

// LastUsed is ts_lastuseobject (fbtime.c:70): the last-used timestamp
// *alone*, without the use count that Used bumps. Upstream's own
// comment calls which of the two is used where "a little arbitrary";
// MOVETO wants this one.
//
// For a room it walks up to the parent as well, and only for a room.
// The walk is bounded, where upstream's recursion is not: a cycle in
// a damaged parent chain would take the server down rather than
// return.
func (w *World) LastUsed(r ref.Ref) {
	for i := 0; r != ref.Nothing && i <= lastUsedMaxDepth; i++ {
		o := w.objs[r]
		if o == nil {
			return
		}
		o.LastUsed = w.now()
		w.dirty[r] = struct{}{}
		if o.Type() != ref.TypeRoom {
			return
		}
		r = o.Location
	}
}

// lastUsedMaxDepth bounds LastUsed's walk up the parent chain.
const lastUsedMaxDepth = 128

// Create allocates a new object and marks it dirty. It does not place
// the object anywhere; use MoveTo for that.
//
// A garbage dbref is reused if there is one, which is upstream's
// new_object (db.c:147) and is visible in every message that names a
// new object: recycle something and the next thing built takes its
// number. The one exception is a **player**, who always gets a fresh
// ref — upstream passes `isplayer` and skips the free list, so a
// name that was once somebody else's cannot come back attached to
// their old number.
func (w *World) Create(name string, t ref.ObjType, owner ref.Ref) *Object {
	r := w.nextRef(t)
	o := newObject(r, name, t, owner, w.now())
	w.objs[r] = o
	w.dirty[r] = struct{}{}
	if t == ref.TypePlayer {
		w.players[ascii.Fold(name)] = r
	}
	return o
}

// nextRef picks the dbref a new object gets: the most recently
// recycled one, or a fresh one past the ceiling.
func (w *World) nextRef(t ref.ObjType) ref.Ref {
	if t != ref.TypePlayer {
		for i := len(w.recyclable) - 1; i >= 0; i-- {
			r := w.recyclable[i]
			w.recyclable = w.recyclable[:i]
			// A ref that is no longer garbage was taken
			// by something else — a repair, or a load
			// — and is skipped rather than handed out
			// twice.
			if o := w.objs[r]; o == nil ||
				o.Type() == ref.TypeGarbage {
				return r
			}
		}
	}
	r := w.top
	w.top++
	return r
}

// RebuildRecyclable rebuilds the free list from the graph, which is
// what upstream does at the end of a load (db.c:1222): it walks every
// object in ascending order and pushes each garbage one, so the
// *highest* garbage ref ends up at the head and is reused first.
//
// It is also what a sanity repair wants, since the list is the one
// piece of state a damaged chain can corrupt without the objects
// themselves being wrong.
func (w *World) RebuildRecyclable() {
	w.recyclable = w.recyclable[:0]
	for r := ref.Ref(0); r < w.top; r++ {
		if o := w.objs[r]; o != nil &&
			o.Type() == ref.TypeGarbage {
			w.recyclable = append(w.recyclable, r)
		}
	}
}

// RecyclableCount reports how many dbrefs are waiting to be reused,
// which @stats and the sanity report want.
func (w *World) RecyclableCount() int { return len(w.recyclable) }

// Add inserts an already-built object, as the importer and the store
// loader do. It does not mark the object dirty, because both callers
// are reproducing state that is already persisted.
func (w *World) Add(o *Object) error {
	if o.Ref < 0 {
		return fmt.Errorf("cannot add object at %v", o.Ref)
	}
	if _, exists := w.objs[o.Ref]; exists {
		return fmt.Errorf("object %v already exists", o.Ref)
	}
	if o.Props == nil {
		o.Props = props.New()
	}
	w.objs[o.Ref] = o
	if o.Ref >= w.top {
		w.top = o.Ref + 1
	}
	if o.Type() == ref.TypePlayer {
		w.players[ascii.Fold(o.Name)] = o.Ref
	}
	return nil
}

// Recycle turns an object into garbage, unlinking it from its
// container and clearing its properties. The ref itself is kept so
// existing references to it resolve to garbage rather than to some
// unrelated later object.
func (w *World) Recycle(r ref.Ref) error {
	o := w.objs[r]
	if o == nil {
		return fmt.Errorf("no object at %v", r)
	}
	if o.Type() == ref.TypeGarbage {
		return nil
	}
	if o.Type() == ref.TypePlayer {
		delete(w.players, ascii.Fold(o.Name))
	}
	if o.Location != ref.Nothing {
		w.removeFromChain(o.Location, r)
	}
	o.Flags = ref.Flags(0).WithType(ref.TypeGarbage)
	o.Name = "<garbage>"
	o.Props = props.New()
	// Upstream gives garbage a description as well as a name
	// (move.c:1314), which is what @examine and the sanity report
	// show when something still points at it.
	o.Props.SetString("_/de", "<recyclable>")
	o.Dest = nil
	o.Location = ref.Nothing
	o.Contents = ref.Nothing
	o.Exits = ref.Nothing
	o.Next = ref.Nothing
	o.Owner = ref.Nothing
	o.PasswordHash = ""
	o.Modified = w.now()
	w.dirty[r] = struct{}{}
	// The ref goes on the free list, so the next thing built
	// takes it. Upstream pushes onto the head, so the most recent
	// recycling is reused first.
	w.recyclable = append(w.recyclable, r)
	return nil
}

// PlayerNamed looks a player up by name, case-insensitively.
func (w *World) PlayerNamed(name string) (ref.Ref, bool) {
	r, ok := w.players[ascii.Fold(name)]
	return r, ok
}

// Rename changes an object's name, keeping the player index in step.
func (w *World) Rename(r ref.Ref, name string) error {
	o := w.objs[r]
	if o == nil {
		return fmt.Errorf("no object at %v", r)
	}
	if o.Type() == ref.TypePlayer {
		if existing, taken := w.players[ascii.Fold(name)]; taken &&
			existing != r {
			return fmt.Errorf("the name %q is already taken", name)
		}
		delete(w.players, ascii.Fold(o.Name))
		w.players[ascii.Fold(name)] = r
		w.recordNameHistory(o, name)
	}
	o.Name = name
	w.Modified(r)
	return nil
}

// nameHistoryDir is upstream's PNAME_HISTORY_PROPDIR: what a player
// has been called, keyed by when they were called it.
const nameHistoryDir = "@__sys__/name"

// recordNameHistory is upstream's change_player_name bookkeeping:
// note the new name against the current time, and drop entries older
// than the pname_history_threshold parameter. A threshold of zero
// keeps them forever.
//
// The history is recorded whatever pname_history_reporting says —
// that parameter only decides whether the PNAME_HISTORY primitive may
// read it back, which is the primitive's own check, not this one's.
func (w *World) recordNameHistory(o *Object, name string) {
	now := w.now().Unix()
	if threshold := w.Tune.Duration("pname_history_threshold"); threshold > 0 {
		cutoff := now - int64(threshold.Seconds())
		for _, key := range o.Props.Children(nameHistoryDir) {
			t, err := strconv.ParseInt(key, 10, 64)
			// created_as lives in this directory too and
			// is not a timestamp; it is never expired.
			if err != nil || t > cutoff {
				continue
			}
			o.Props.Delete(nameHistoryDir + "/" + key)
		}
	}
	o.Props.SetString(nameHistoryDir+"/"+strconv.FormatInt(now, 10), name)
}

// SetProp stores a property and marks the object changed.
//
// It also maintains the LISTENER flag, which is upstream's
// set_property doing the same thing (property.c:101) and the only
// place a non-DISKBASE build ever sets it. See listen.go for why that
// is one-way.
func (w *World) SetProp(r ref.Ref, path string, v props.Value) {
	o := w.objs[r]
	if o == nil {
		return
	}
	o.Props.Set(path, v)
	w.markListener(r, path)
	w.Modified(r)
}

// GetProp reads a property.
func (w *World) GetProp(r ref.Ref, path string) (props.Value, bool) {
	o := w.objs[r]
	if o == nil {
		return props.Value{}, false
	}
	return o.Props.Get(path)
}

// SetSource stores a program's MUF source without queueing it for
// writing, which is what loading a world wants.
func (w *World) SetSource(r ref.Ref, src string) {
	w.programs[r] = src
}

// SaveSource replaces a program's source and queues it for writing.
// This is the editor's path: leaving the editor rewrites the text,
// and the object itself is touched too so its modification time
// moves.
func (w *World) SaveSource(r ref.Ref, src string) {
	w.programs[r] = src
	w.progDirty[r] = struct{}{}
	w.Modified(r)
}

// Source returns a program's MUF source.
func (w *World) Source(r ref.Ref) (string, bool) {
	src, ok := w.programs[r]
	return src, ok
}

// SetTune changes a parameter and marks the table for persistence.
func (w *World) SetTune(name, value string) error {
	if err := w.Tune.SetString(name, value); err != nil {
		return err
	}
	w.tuneDirty = true
	return nil
}

// SetParm applies tune_setparm's rules and marks the table for
// persistence. It is what @tune and SETSYSPARM go through, as
// upstream's two callers of tune_setparm do; SetTune and ResetTune
// stay the loader's and the repairer's paths, where the value is one
// this server wrote itself. TuneRefResolver answers a dbref
// parameter's value the way tune_setparm's own match does, and is
// passed in because the matcher is a layer above this one.
type TuneRefResolver func(string) (ref.Ref, ref.ObjType, bool)

func (w *World) SetParm(name, val string, mlev int,
	resolve TuneRefResolver) tune.SetResult {

	r := w.Tune.SetParm(name, val, mlev, resolve)
	if r == tune.SetSuccess || r == tune.SetSuccessDefault {
		w.tuneDirty = true
	}
	return r
}

// ResetTune returns a parameter to its default and marks the table
// for persistence — SETSYSPARM's own "%name" reset convention.
func (w *World) ResetTune(name string) error {
	if err := w.Tune.Reset(name); err != nil {
		return err
	}
	w.tuneDirty = true
	return nil
}

// chainHead returns a pointer to the list head an object of this type
// belongs on: exits thread onto the Exits list, everything else onto
// Contents.
func chainHead(container *Object, member *Object) *ref.Ref {
	if member.Type() == ref.TypeExit {
		return &container.Exits
	}
	return &container.Contents
}

// MoveTo relocates an object into a container, unlinking it from
// wherever it was. Passing ref.Nothing as the destination just
// unlinks it.
func (w *World) MoveTo(what, dest ref.Ref) error {
	o := w.objs[what]
	if o == nil {
		return fmt.Errorf("no object at %v", what)
	}
	if dest != ref.Nothing {
		if w.objs[dest] == nil {
			return fmt.Errorf("no destination at %v", dest)
		}
		if what == dest {
			return fmt.Errorf("%v cannot contain itself", what)
		}
		if w.contains(what, dest) {
			return fmt.Errorf("%v is already inside %v", dest, what)
		}
	}

	if o.Location != ref.Nothing {
		w.removeFromChain(o.Location, what)
	}
	o.Location = dest
	o.Next = ref.Nothing

	if dest != ref.Nothing {
		container := w.objs[dest]
		head := chainHead(container, o)
		// Fuzzball pushes onto the head of the list, so the
		// most recently added object is listed first.
		o.Next = *head
		*head = what
		w.dirty[dest] = struct{}{}
	}
	w.Modified(what)
	return nil
}

// contains reports whether outer holds inner, at any depth. It is
// bounded by the object count so a corrupt chain cannot loop forever.
func (w *World) contains(outer, inner ref.Ref) bool {
	for i, r := 0, inner; r != ref.Nothing &&
		i <= len(w.objs); i++ {
		if r == outer {
			return true
		}
		o := w.objs[r]
		if o == nil {
			return false
		}
		r = o.Location
	}
	return false
}

// removeFromChain unlinks member from its container's contents or
// exits list.
func (w *World) removeFromChain(container, member ref.Ref) {
	c := w.objs[container]
	m := w.objs[member]
	if c == nil || m == nil {
		return
	}
	head := chainHead(c, m)
	if *head == member {
		*head = m.Next
		w.dirty[container] = struct{}{}
		return
	}
	for r := *head; r != ref.Nothing; {
		o := w.objs[r]
		if o == nil {
			return
		}
		if o.Next == member {
			o.Next = m.Next
			w.dirty[r] = struct{}{}
			w.dirty[container] = struct{}{}
			return
		}
		r = o.Next
	}
}

// Contents lists what a container holds, in the order MUF walks it.
func (w *World) Contents(r ref.Ref) []ref.Ref {
	return w.chain(r, false)
}

// Exits lists a container's exits, in the order MUF walks them.
func (w *World) Exits(r ref.Ref) []ref.Ref { return w.chain(r, true) }

func (w *World) chain(r ref.Ref, exits bool) []ref.Ref {
	o := w.objs[r]
	if o == nil {
		return nil
	}
	head := o.Contents
	if exits {
		head = o.Exits
	}
	var out []ref.Ref
	// Bounded so a cycle in a damaged chain cannot hang the world
	// goroutine.
	for cur, i := head, 0; cur != ref.Nothing &&
		i <= len(w.objs); i++ {
		out = append(out, cur)
		next := w.objs[cur]
		if next == nil {
			break
		}
		cur = next.Next
	}
	return out
}

// Each visits every object, in ref order.
func (w *World) Each(fn func(*Object) bool) {
	for r := ref.Ref(0); r < w.top; r++ {
		if o := w.objs[r]; o != nil {
			if !fn(o) {
				return
			}
		}
	}
}

// OwnerOf returns the object a player's possessions belong to, which
// for a player is themselves.
//
// It lives here rather than in internal/game because the matcher
// needs it too: `match_exits` weighs an exit's owner against where
// the searcher is standing, and internal/match cannot import the
// package that owns the commands.
func (w *World) OwnerOf(r ref.Ref) ref.Ref {
	o := w.Get(r)
	if o == nil {
		return ref.Nothing
	}
	if o.Type() == ref.TypePlayer {
		return r
	}
	return o.Owner
}

// Controls is `controls` (`db.c:1822`): whether a player may modify
// an object.
//
// The test is made on whoever owns the asking object, not the object
// itself, so a puppet controls exactly what its owner does — which
// is what lets a program running as a thing touch its owner's things.
//
// A wizard controls everything, with one exception: while
// strict_god_priv is set, only God may touch God's objects. Without
// that a wizard could edit God's programs and so give themselves
// God's powers.
//
// Two of upstream's routes past the ownership test are **not** here
// and are recorded in docs/upstream-coverage.md: `tp_realms_control`,
// which defaults off, and an **ownership lock**, which does not —
// `@ownlock` writes a property that nothing reads.
func (w *World) Controls(who, target ref.Ref) bool {
	o := w.Get(target)
	if o == nil {
		return false
	}
	owner := w.OwnerOf(who)
	p := w.Get(owner)
	if p == nil {
		return false
	}
	if p.Flags.IsWizard() {
		if w.Tune.Bool("strict_god_priv") &&
			o.Owner == ref.God && owner != ref.God {
			return false
		}
		return true
	}
	if who == target {
		return true
	}
	return o.Owner == owner
}
