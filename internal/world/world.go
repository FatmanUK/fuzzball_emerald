package world

import (
	"fmt"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/tune"
)

// World is the object graph. It is owned by a single goroutine and has no
// internal locking; reach it through an Engine.
type World struct {
	objs map[ref.Ref]*Object
	// top is one past the highest ref ever allocated, matching db_top.
	top ref.Ref

	// players indexes player refs by case-folded name, as upstream's player
	// hash table does.
	players map[string]ref.Ref

	// dirty accumulates refs changed since the last flush; deleted holds
	// refs that need removing from the store.
	dirty   map[ref.Ref]struct{}
	deleted map[ref.Ref]struct{}

	Tune *tune.Set
	// tuneDirty records that the parameter table changed.
	tuneDirty bool

	now func() time.Time
}

// New returns an empty world.
func New() *World {
	return &World{
		objs:    make(map[ref.Ref]*Object),
		players: make(map[string]ref.Ref),
		dirty:   make(map[ref.Ref]struct{}),
		deleted: make(map[ref.Ref]struct{}),
		Tune:    tune.NewSet(),
		now:     time.Now,
	}
}

// SetClock replaces the world's clock. Tests use it to make timestamps
// predictable.
func (w *World) SetClock(f func() time.Time) { w.now = f }

// Now returns the world's current time.
func (w *World) Now() time.Time { return w.now() }

// Top returns one past the highest allocated ref.
func (w *World) Top() ref.Ref { return w.top }

// SetTop raises the ref ceiling, which the store does on load so a world whose
// highest objects were recycled still hands out fresh refs.
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

// Modified marks an object changed and updates its modification timestamp,
// which is what upstream's ts_modifyobject does.
func (w *World) Modified(r ref.Ref) {
	if o := w.objs[r]; o != nil {
		o.Modified = w.now()
		w.dirty[r] = struct{}{}
	}
}

// Used bumps an object's use count and last-used timestamp.
func (w *World) Used(r ref.Ref) {
	if o := w.objs[r]; o != nil {
		o.LastUsed = w.now()
		o.UseCount++
		w.dirty[r] = struct{}{}
	}
}

// Create allocates a new object and marks it dirty. It does not place the
// object anywhere; use MoveTo for that.
func (w *World) Create(name string, t ref.ObjType, owner ref.Ref) *Object {
	r := w.top
	w.top++
	o := newObject(r, name, t, owner, w.now())
	w.objs[r] = o
	w.dirty[r] = struct{}{}
	if t == ref.TypePlayer {
		w.players[ascii.Fold(name)] = r
	}
	return o
}

// Add inserts an already-built object, as the importer and the store loader
// do. It does not mark the object dirty, because both callers are reproducing
// state that is already persisted.
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

// Recycle turns an object into garbage, unlinking it from its container and
// clearing its properties. The ref itself is kept so existing references to it
// resolve to garbage rather than to some unrelated later object.
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
	o.Dest = nil
	o.Location = ref.Nothing
	o.Contents = ref.Nothing
	o.Exits = ref.Nothing
	o.Next = ref.Nothing
	o.Owner = ref.Nothing
	o.PasswordHash = ""
	o.Modified = w.now()
	w.dirty[r] = struct{}{}
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
		if existing, taken := w.players[ascii.Fold(name)]; taken && existing != r {
			return fmt.Errorf("the name %q is already taken", name)
		}
		delete(w.players, ascii.Fold(o.Name))
		w.players[ascii.Fold(name)] = r
	}
	o.Name = name
	w.Modified(r)
	return nil
}

// SetProp stores a property and marks the object changed.
func (w *World) SetProp(r ref.Ref, path string, v props.Value) {
	o := w.objs[r]
	if o == nil {
		return
	}
	o.Props.Set(path, v)
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

// SetTune changes a parameter and marks the table for persistence.
func (w *World) SetTune(name, value string) error {
	if err := w.Tune.SetString(name, value); err != nil {
		return err
	}
	w.tuneDirty = true
	return nil
}

// chainHead returns a pointer to the list head an object of this type belongs
// on: exits thread onto the Exits list, everything else onto Contents.
func chainHead(container *Object, member *Object) *ref.Ref {
	if member.Type() == ref.TypeExit {
		return &container.Exits
	}
	return &container.Contents
}

// MoveTo relocates an object into a container, unlinking it from wherever it
// was. Passing ref.Nothing as the destination just unlinks it.
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
		// Fuzzball pushes onto the head of the list, so the most
		// recently added object is listed first.
		o.Next = *head
		*head = what
		w.dirty[dest] = struct{}{}
	}
	w.Modified(what)
	return nil
}

// contains reports whether outer holds inner, at any depth. It is bounded by
// the object count so a corrupt chain cannot loop forever.
func (w *World) contains(outer, inner ref.Ref) bool {
	for i, r := 0, inner; r != ref.Nothing && i <= len(w.objs); i++ {
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

// removeFromChain unlinks member from its container's contents or exits list.
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
func (w *World) Contents(r ref.Ref) []ref.Ref { return w.chain(r, false) }

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
	// Bounded so a cycle in a damaged chain cannot hang the world goroutine.
	for cur, i := head, 0; cur != ref.Nothing && i <= len(w.objs); i++ {
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
