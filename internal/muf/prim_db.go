package muf

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Database and property primitives.

func init() {
	register("NAME", refToStr(func(h Host, r ref.Ref) string { return h.Name(r) }))
	register("SETNAME", func(f *Frame) (*Result, error) {
		name, err := f.popStr()
		if err != nil {
			return nil, err
		}
		obj, err := f.popRef()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if err := h.SetName(obj, name); err != nil {
			return nil, errf("%s", err.Error())
		}
		return nil, nil
	})

	register("LOCATION", refToRef(func(h Host, r ref.Ref) ref.Ref { return h.Location(r) }))
	register("OWNER", refToRef(func(h Host, r ref.Ref) ref.Ref { return h.Owner(r) }))
	register("GETLINK", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		links := h.Links(obj)
		if len(links) == 0 {
			return nil, f.Push(Obj(ref.Nothing))
		}
		return nil, f.Push(Obj(links[0]))
	})
	register("GETLINKS", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		links := h.Links(obj)
		for _, l := range links {
			if err := f.Push(Obj(l)); err != nil {
				return nil, err
			}
		}
		return nil, f.Push(Int(int64(len(links))))
	})
	register("GETLINKS_ARRAY", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Arr(refList(h.Links(obj))))
	})

	register("CONTENTS", chainPrim(func(h Host, r ref.Ref) []ref.Ref { return h.Contents(r) }))
	register("EXITS", chainPrim(func(h Host, r ref.Ref) []ref.Ref { return h.Exits(r) }))
	register("CONTENTS_ARRAY", chainArray(func(h Host, r ref.Ref) []ref.Ref { return h.Contents(r) }))
	register("EXITS_ARRAY", chainArray(func(h Host, r ref.Ref) []ref.Ref { return h.Exits(r) }))

	// NEXT walks a containment chain one step, which is how older programs
	// iterate before arrays existed.
	register("NEXT", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		loc := h.Location(obj)
		if !h.Valid(loc) {
			return nil, f.Push(Obj(ref.Nothing))
		}
		siblings := h.Contents(loc)
		if h.ObjType(obj) == ref.TypeExit {
			siblings = h.Exits(loc)
		}
		for i, s := range siblings {
			if s == obj && i+1 < len(siblings) {
				return nil, f.Push(Obj(siblings[i+1]))
			}
		}
		return nil, f.Push(Obj(ref.Nothing))
	})

	register("MOVETO", func(f *Frame) (*Result, error) {
		dest, err := f.popRef()
		if err != nil {
			return nil, err
		}
		what, err := f.popRef()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if err := h.MoveTo(what, dest); err != nil {
			return nil, errf("%s", err.Error())
		}
		return nil, nil
	})

	register("DBTOP", func(f *Frame) (*Result, error) {
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Obj(h.Top()))
	})
	register("OK?", func(f *Frame) (*Result, error) {
		r, err := f.popRef()
		if err != nil {
			return nil, err
		}
		if f.host == nil {
			return nil, f.Push(Bool(false))
		}
		return nil, f.Push(Bool(f.host.Valid(r)))
	})

	register("PLAYER?", typeOfTest(ref.TypePlayer))
	register("ROOM?", typeOfTest(ref.TypeRoom))
	register("EXIT?", typeOfTest(ref.TypeExit))
	register("PROGRAM?", typeOfTest(ref.TypeProgram))
	register("THING?", typeOfTest(ref.TypeThing))

	register("FLAG?", func(f *Frame) (*Result, error) {
		name, err := f.popStr()
		if err != nil {
			return nil, err
		}
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(hasFlag(h.Flags(obj), name)))
	})
	register("SET", func(f *Frame) (*Result, error) {
		name, err := f.popStr()
		if err != nil {
			return nil, err
		}
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		clear := len(name) > 0 && name[0] == '!'
		if clear {
			name = name[1:]
		}
		bit, ok := flagNamed(name)
		if !ok {
			return nil, errf("unknown flag %q", name)
		}
		flags := h.Flags(obj)
		if clear {
			flags &^= bit
		} else {
			flags |= bit
		}
		h.SetFlags(obj, flags)
		return nil, nil
	})

	register("CONTROLS", func(f *Frame) (*Result, error) {
		obj, err := f.popRef()
		if err != nil {
			return nil, err
		}
		who, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(controls(h, who, obj)))
	})

	register("MATCH", func(f *Frame) (*Result, error) {
		name, err := f.popStr()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Obj(h.Match(f.Caller, name)))
	})
	register("PMATCH", func(f *Frame) (*Result, error) {
		name, err := f.popStr()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Obj(h.MatchPlayer(name)))
	})

	// Property access. MUF distinguishes the three forms by what they
	// convert the stored value to, rather than by what was stored.
	register("GETPROPSTR", func(f *Frame) (*Result, error) {
		v, err := f.getProp()
		if err != nil {
			return nil, err
		}
		if v.Type == props.Int || v.Type == props.Float || v.Type == props.Ref {
			// Only a string property reads as a string; the others
			// read as empty, as upstream's get_property_class does.
			return nil, f.Push(Str(""))
		}
		return nil, f.Push(Str(v.Str))
	})
	register("GETPROPVAL", func(f *Frame) (*Result, error) {
		v, err := f.getProp()
		if err != nil {
			return nil, err
		}
		if v.Type != props.Int {
			return nil, f.Push(Int(0))
		}
		return nil, f.Push(Int(v.Num))
	})
	register("GETPROPFVAL", func(f *Frame) (*Result, error) {
		v, err := f.getProp()
		if err != nil {
			return nil, err
		}
		if v.Type != props.Float {
			return nil, f.Push(Float(0))
		}
		return nil, f.Push(Float(v.Float))
	})
	register("GETPROP", func(f *Frame) (*Result, error) {
		v, err := f.getProp()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(fromProp(v))
	})

	register("SETPROP", func(f *Frame) (*Result, error) {
		val, err := f.Pop()
		if err != nil {
			return nil, err
		}
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		h.SetProp(obj, path, toProp(val))
		return nil, nil
	})
	register("ADDPROP", func(f *Frame) (*Result, error) {
		// "obj path strval intval addprop": a non-empty string wins,
		// otherwise the integer is stored.
		num, err := f.popInt()
		if err != nil {
			return nil, err
		}
		str, err := f.popStr()
		if err != nil {
			return nil, err
		}
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		if str != "" {
			h.SetProp(obj, path, props.Value{Type: props.String, Str: str})
		} else {
			h.SetProp(obj, path, props.Value{Type: props.Int, Num: num})
		}
		return nil, nil
	})
	register("REMOVE_PROP", func(f *Frame) (*Result, error) {
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		h.RemoveProp(obj, path)
		return nil, nil
	})
	register("PROPDIR?", func(f *Frame) (*Result, error) {
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(len(h.PropChildren(obj, path)) > 0))
	})
	register("NEXTPROP", func(f *Frame) (*Result, error) {
		// "obj path nextprop": the next name at the same level.
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Str(nextProp(h, obj, path)))
	})
}

// needHost returns the host, or an error naming the problem.
func (f *Frame) needHost() (Host, error) {
	if f.host == nil {
		return nil, errf("this primitive needs a running server")
	}
	return f.host, nil
}

// refAndHost pops a dbref and returns it with the host.
func (f *Frame) refAndHost() (ref.Ref, Host, error) {
	r, err := f.popRef()
	if err != nil {
		return ref.Nothing, nil, err
	}
	h, err := f.needHost()
	return r, h, err
}

// propTarget pops a property path and the object it is on.
func (f *Frame) propTarget() (string, ref.Ref, Host, error) {
	path, err := f.popStr()
	if err != nil {
		return "", ref.Nothing, nil, err
	}
	obj, h, err := f.refAndHost()
	return path, obj, h, err
}

// getProp reads a property, yielding the zero value when unset.
func (f *Frame) getProp() (props.Value, error) {
	path, obj, h, err := f.propTarget()
	if err != nil {
		return props.Value{}, err
	}
	v, ok := h.GetProp(obj, path)
	if !ok {
		return props.Value{}, nil
	}
	return v, nil
}

// fromProp converts a stored property to a stack value.
func fromProp(v props.Value) Value {
	switch v.Type {
	case props.String, props.Lock:
		return Str(v.Str)
	case props.Int:
		return Int(v.Num)
	case props.Float:
		return Float(v.Float)
	case props.Ref:
		return Obj(v.Ref)
	}
	return Str("")
}

// toProp converts a stack value to a stored property.
func toProp(v Value) props.Value {
	switch v.Type {
	case TypeInteger:
		return props.Value{Type: props.Int, Num: v.Num}
	case TypeFloat:
		return props.Value{Type: props.Float, Float: v.Float}
	case TypeObject:
		return props.Value{Type: props.Ref, Ref: v.Ref}
	default:
		return props.Value{Type: props.String, Str: v.String()}
	}
}

// nextProp returns the property name after path at the same level, or "" at
// the end. An empty path starts the walk at the top.
func nextProp(h Host, obj ref.Ref, path string) string {
	parent, name := splitProp(path)
	children := h.PropChildren(obj, parent)
	if name == "" {
		if len(children) == 0 {
			return ""
		}
		return join(parent, children[0])
	}
	for i, c := range children {
		if equalFoldASCII(c, name) && i+1 < len(children) {
			return join(parent, children[i+1])
		}
	}
	return ""
}

// splitProp separates a property path into its directory and last name.
func splitProp(path string) (dir, name string) {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i], path[i+1:]
		}
	}
	return "", path
}

// join puts a property path back together.
func join(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// refList builds a MUF array of dbrefs.
func refList(refs []ref.Ref) *Array {
	vals := make([]Value, len(refs))
	for i, r := range refs {
		vals[i] = Obj(r)
	}
	return NewList(vals)
}

// chainPrim builds a primitive that pushes a chain's head, the way the older
// CONTENTS and EXITS do.
func chainPrim(fn func(Host, ref.Ref) []ref.Ref) primFunc {
	return func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		chain := fn(h, obj)
		if len(chain) == 0 {
			return nil, f.Push(Obj(ref.Nothing))
		}
		return nil, f.Push(Obj(chain[0]))
	}
}

// chainArray builds the array form of the same.
func chainArray(fn func(Host, ref.Ref) []ref.Ref) primFunc {
	return func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Arr(refList(fn(h, obj))))
	}
}

// typeOfTest builds a primitive that reports an object's type.
func typeOfTest(want ref.ObjType) primFunc {
	return func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(h.Valid(obj) && h.ObjType(obj) == want))
	}
}

// refToStr builds a primitive that asks the host about an object.
func refToStr(fn func(Host, ref.Ref) string) primFunc {
	return func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Str(fn(h, obj)))
	}
}

// refToRef builds a primitive that resolves one object to another.
func refToRef(fn func(Host, ref.Ref) ref.Ref) primFunc {
	return func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Obj(fn(h, obj)))
	}
}

// controls reports whether one object may modify another, which is ownership
// or an unquelled wizard bit.
func controls(h Host, who, what ref.Ref) bool {
	if !h.Valid(who) || !h.Valid(what) {
		return false
	}
	if h.Flags(who).IsWizard() {
		return true
	}
	owner := who
	if h.ObjType(who) != ref.TypePlayer {
		owner = h.Owner(who)
	}
	return who == what || h.Owner(what) == owner
}

// Environment-walking property lookups, reflists, and the remaining object
// queries.

func init() {
	// ENVPROP walks out through the environment tree until it finds the
	// property, which is how a world puts a default on a parent room.
	register("ENVPROP", func(f *Frame) (*Result, error) {
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		where, v := envProp(h, obj, path)
		if err := f.Push(Obj(where)); err != nil {
			return nil, err
		}
		return nil, f.Push(fromProp(v))
	})
	register("ENVPROPSTR", func(f *Frame) (*Result, error) {
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		where, v := envProp(h, obj, path)
		if err := f.Push(Obj(where)); err != nil {
			return nil, err
		}
		if v.Type == props.Int || v.Type == props.Float || v.Type == props.Ref {
			return nil, f.Push(Str(""))
		}
		return nil, f.Push(Str(v.Str))
	})

	// A reflist is a property holding space-separated dbrefs, which older
	// programs use where an array would serve today.
	register("REFLIST_FIND", func(f *Frame) (*Result, error) {
		target, err := f.popRef()
		if err != nil {
			return nil, err
		}
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		list := readRefList(h, obj, path)
		for i, r := range list {
			if r == target {
				// One-based, as the primitive reports it.
				return nil, f.Push(Int(int64(i + 1)))
			}
		}
		return nil, f.Push(Int(0))
	})
	register("REFLIST_ADD", func(f *Frame) (*Result, error) {
		target, err := f.popRef()
		if err != nil {
			return nil, err
		}
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		list := readRefList(h, obj, path)
		for _, r := range list {
			if r == target {
				return nil, nil // already there
			}
		}
		writeRefList(h, obj, path, append(list, target))
		return nil, nil
	})
	register("REFLIST_DEL", func(f *Frame) (*Result, error) {
		target, err := f.popRef()
		if err != nil {
			return nil, err
		}
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		v, _ := h.GetProp(obj, path)
		h.SetProp(obj, path, props.Value{
			Type: props.String, Str: spliceRef(v.Str, target),
		})
		return nil, nil
	})

	register("UNPARSEOBJ", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		if !h.Valid(obj) {
			return nil, f.Push(Str("*NOTHING*"))
		}
		return nil, f.Push(Str(h.Name(obj) + "(" + obj.String() +
			h.Flags(obj).Unparse() + ")"))
	})

	register("PENNIES", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		v, _ := h.GetProp(obj, propValue)
		return nil, f.Push(Int(v.Num))
	})
	register("ADDPENNIES", func(f *Frame) (*Result, error) {
		amount, err := f.popInt()
		if err != nil {
			return nil, err
		}
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		v, _ := h.GetProp(obj, propValue)
		h.SetProp(obj, propValue, props.Value{Type: props.Int, Num: v.Num + amount})
		return nil, nil
	})
}

// propValue is where an object's currency is kept, from include/db.h.
const propValue = "@/value"

// envProp looks a property up on an object and then on each of its containers
// in turn, returning where it was found.
func envProp(h Host, obj ref.Ref, path string) (ref.Ref, props.Value) {
	// Bounded, so a cycle in a damaged environment tree terminates.
	for i := 0; obj != ref.Nothing && i <= maxEnvDepth; i++ {
		if v, ok := h.GetProp(obj, path); ok {
			return obj, v
		}
		obj = h.Location(obj)
	}
	return ref.Nothing, props.Value{}
}

// maxEnvDepth bounds an environment walk.
const maxEnvDepth = 256

// readRefList parses a space-separated dbref property.
func readRefList(h Host, obj ref.Ref, path string) []ref.Ref {
	v, _ := h.GetProp(obj, path)
	var out []ref.Ref
	for _, field := range strings.Fields(v.Str) {
		r, err := ref.Parse(field)
		if err == nil {
			out = append(out, r)
		}
	}
	return out
}

// spliceRef removes one dbref from a reflist by cutting it out of the text.
//
// Upstream does the same, which leaves the separator that preceded the removed
// entry: deleting #1 from "#1 #0" gives " #0", not "#0". That leading space is
// observable, so it is reproduced rather than tidied away.
func spliceRef(list string, target ref.Ref) string {
	want := target.String()
	for i := 0; i < len(list); i++ {
		if list[i] != '#' {
			continue
		}
		end := i + len(want)
		if end > len(list) || list[i:end] != want {
			continue
		}
		// The match must end at a separator or the end of the list, so
		// "#1" does not match inside "#12".
		if end < len(list) && list[end] != ' ' {
			continue
		}
		return list[:i] + list[end:]
	}
	return list
}

// writeRefList stores a reflist, removing the property when it empties.
func writeRefList(h Host, obj ref.Ref, path string, list []ref.Ref) {
	if len(list) == 0 {
		h.RemoveProp(obj, path)
		return
	}
	parts := make([]string, len(list))
	for i, r := range list {
		parts[i] = r.String()
	}
	h.SetProp(obj, path, props.Value{Type: props.String, Str: strings.Join(parts, " ")})
}
