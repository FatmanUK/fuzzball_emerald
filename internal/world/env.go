package world

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// envDepth bounds an environment walk. A damaged world can contain a cycle
// getparent's own detection does not catch, and no real environment is
// anywhere near this deep.
const envDepth = 128

// Parent returns the object one step out in the environment tree, which is
// what a property search walks. This is upstream's getparent.
//
// Ordinarily that is simply the object's location, but a THING set VEHICLE
// parents to its home instead — and to its home's home when that home is a
// player, so a vehicle carried by someone inherits from their home rather
// than from them. The walk then keeps going while it is still on things, so
// the parent of anything is the first ancestor that is not a THING.
//
// Chained vehicle homes can form a loop, so upstream walks a second pointer
// at twice the speed and falls back to the global environment when the two
// meet. That is reproduced rather than simplified, because a program can ask
// which object a property came from.
func (w *World) Parent(r ref.Ref) ref.Ref {
	if w.Tune.Bool("secure_thing_movement") {
		if o := w.Get(r); o != nil {
			return o.Location
		}
		return ref.Nothing
	}

	obj := r
	fast := w.parentLink(r)
	for {
		obj = w.parentLink(obj)
		mid := w.parentLink(fast)
		fast = w.parentLink(mid)

		if obj == mid || obj == fast {
			if obj != ref.Nothing {
				return ref.GlobalEnvironment
			}
			return ref.Nothing
		}
		if obj == ref.Nothing {
			return ref.Nothing
		}
		if o := w.Get(obj); o == nil || o.Type() != ref.TypeThing {
			return obj
		}
	}
}

// parentLink is one step of the walk: a vehicle's home, or a location.
func (w *World) parentLink(r ref.Ref) ref.Ref {
	o := w.Get(r)
	if o == nil {
		return ref.Nothing
	}
	if o.Type() == ref.TypeThing && o.Flags&ref.Vehicle != 0 {
		home := o.Home
		if h := w.Get(home); h != nil && h.Type() == ref.TypePlayer {
			return h.Home
		}
		return home
	}
	return o.Location
}

// EnvProp looks a property up on an object and then outwards through the
// environment tree, returning the value and the object it was found on. This
// is upstream's envprop, and is how "$lib-foo" and inherited settings resolve.
func (w *World) EnvProp(start ref.Ref, path string) (props.Value, ref.Ref, bool) {
	at := start
	for n := 0; n < envDepth && at != ref.Nothing; n++ {
		if v, ok := w.GetProp(at, path); ok {
			return v, at, true
		}
		at = w.Parent(at)
	}
	return props.Value{}, ref.Nothing, false
}
