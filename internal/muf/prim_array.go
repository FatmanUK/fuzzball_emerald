package muf

import (
	"sort"
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Array and dictionary primitives.

func init() {
	register("ARRAY_MAKE_DICT", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, errf("ARRAY_MAKE_DICT needs a count that is not negative")
		}
		// Key and value alternate on the stack, key deepest.
		vals, err := f.PopN(int(n) * 2)
		if err != nil {
			return nil, err
		}
		d := NewDict()
		for i := 0; i+1 < len(vals); i += 2 {
			d.Set(vals[i], vals[i+1])
		}
		return nil, f.Push(Arr(d))
	})

	register("ARRAY_DELITEM", func(f *Frame) (*Result, error) {
		key, err := f.Pop()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		c := a.Copy()
		c.Delete(key)
		return nil, f.Push(Arr(c))
	})

	register("ARRAY_INSERTITEM", func(f *Frame) (*Result, error) {
		key, err := f.Pop()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		val, err := f.Pop()
		if err != nil {
			return nil, err
		}
		c := a.Copy()
		c.Insert(key, val)
		return nil, f.Push(Arr(c))
	})

	register("ARRAY_GETRANGE", func(f *Frame) (*Result, error) {
		to, err := f.Pop()
		if err != nil {
			return nil, err
		}
		from, err := f.Pop()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Arr(a.Range(from, to)))
	})

	register("ARRAY_FIRST", edge(true))
	register("ARRAY_LAST", edge(false))
	register("ARRAY_NEXT", step(true))
	register("ARRAY_PREV", step(false))

	register("ARRAY_REVERSE", func(f *Frame) (*Result, error) {
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		vals := a.Values()
		for i, j := 0, len(vals)-1; i < j; i, j = i+1, j-1 {
			vals[i], vals[j] = vals[j], vals[i]
		}
		return nil, f.Push(Arr(NewList(vals)))
	})

	register("ARRAY_SORT", func(f *Frame) (*Result, error) {
		flags, err := f.popInt()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Arr(NewList(sortValues(a.Values(), int(flags)))))
	})

	register("ARRAY_EXPLODE", func(f *Frame) (*Result, error) {
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		keys, vals := a.Keys(), a.Values()
		for i := range keys {
			if err := f.Push(keys[i]); err != nil {
				return nil, err
			}
			if err := f.Push(vals[i]); err != nil {
				return nil, err
			}
		}
		return nil, f.Push(Int(int64(len(keys))))
	})

	register("ARRAY_SETRANGE", func(f *Frame) (*Result, error) {
		items, err := f.popArray()
		if err != nil {
			return nil, err
		}
		start, err := f.Pop()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		c := a.Copy()
		if start.Type == TypeInteger {
			for i, v := range items.Values() {
				c.Set(Int(start.Num+int64(i)), v)
			}
		}
		return nil, f.Push(Arr(c))
	})

	register("ARRAY_GET_PROPLIST", func(f *Frame) (*Result, error) {
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		// A proplist's **count** is looked for under two
		// spellings and each as an integer and then as a
		// string: "path#" first, then "path/#". And each
		// **item** is looked for under three: "path#/N", then
		// "path/N", then "pathN". This read only "path#" and
		// "path#/N", so a world using any of the other
		// spellings -- which `array_put_proplist` itself does
		// not write, but older code and hand-built lists do
		// -- read an empty list.
		n := listCount(h, obj, path+"#")
		if n == 0 {
			n = listCount(h, obj, path+"/#")
		}
		if max := h.TuneInt("max_propfetch"); n > max {
			n = max
		}
		vals := make([]Value, 0, n)
		for i := int64(1); i <= n; i++ {
			elem, v, ok := listItemProp(h, obj, path, i)
			// p_array.c:2035 wraps the append in the read
			// test rather than aborting, so an element
			// the program may not read is left out and
			// the list comes back shorter.
			if !f.propReadPerms(h, obj, elem) {
				continue
			}
			if !ok {
				// A missing item is integer zero
				// rather than a gap, which is what
				// keeps the list the length the count
				// claimed.
				vals = append(vals, Int(0))
				continue
			}
			vals = append(vals, fromProp(v))
		}
		return nil, f.Push(Arr(NewList(vals)))
	})

	register("ARRAY_GET_REFLIST", func(f *Frame) (*Result, error) {
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		if !f.propReadPerms(h, obj, path) {
			return nil, propDenied()
		}
		// A reflist is a single string of space-separated
		// dbrefs.
		v, _ := h.GetProp(obj, path)
		var vals []Value
		for _, field := range strings.Fields(v.Str) {
			r, err := ref.Parse(field)
			if err != nil {
				continue
			}
			vals = append(vals, Obj(r))
		}
		return nil, f.Push(Arr(NewList(vals)))
	})

	register("ARRAY_NOTIFY", func(f *Frame) (*Result, error) {
		targets, err := f.popArray()
		if err != nil {
			return nil, err
		}
		lines, err := f.popArray()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		// prim_array_notify (p_array.c:2426) walks the whole
		// ref array **first**, aborting on an invalid entry
		// and CHECKREMOTEing each, so nothing is notified
		// unless every target passes. This skipped a
		// non-object silently and notified the rest.
		for _, who := range targets.Values() {
			if who.Type != TypeObject ||
				!h.Valid(who.Ref) {
				return nil, errf("Dbref array " +
					"contains invalid " +
					"object. (2)")
			}
			err := f.checkRemote(h, who.Ref)
			if err != nil {
				return nil, err
			}
		}
		for _, who := range targets.Values() {
			if who.Type != TypeObject {
				continue
			}
			for _, line := range lines.Values() {
				h.Notify(who.Ref, mlev1Prefix(f, h,
					line.String(), ref.Nothing))
			}
		}
		return nil, nil
	})
}

// itoa64 renders an integer for a property path.
func itoa64(n int64) string {
	return Int(n).String()
}

// edge builds ARRAY_FIRST and ARRAY_LAST, which push a key and a
// flag.
func edge(first bool) primFunc {
	return func(f *Frame) (*Result, error) {
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		keys := a.Keys()
		if len(keys) == 0 {
			return nil, f.Push(Bool(false))
		}
		k := keys[0]
		if !first {
			k = keys[len(keys)-1]
		}
		if err := f.Push(k); err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(true))
	}
}

// step builds ARRAY_NEXT and ARRAY_PREV, which walk from a key.
func step(forward bool) primFunc {
	return func(f *Frame) (*Result, error) {
		key, err := f.Pop()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		keys := a.Keys()
		for i, k := range keys {
			if !k.Equal(key) {
				continue
			}
			j := i + 1
			if !forward {
				j = i - 1
			}
			if j < 0 || j >= len(keys) {
				break
			}
			if err := f.Push(keys[j]); err != nil {
				return nil, err
			}
			return nil, f.Push(Bool(true))
		}
		return nil, f.Push(Bool(false))
	}
}

// Sort flags, from include/array.h.
const (
	sortCaseInsensitive = 1
	sortDescending      = 2
	sortShuffle         = 4
)

// sortValues orders values the way ARRAY_SORT does: numbers before
// strings, each ascending unless the flags say otherwise.
func sortValues(vals []Value, flags int) []Value {
	out := append([]Value{}, vals...)
	if flags&sortShuffle != 0 {
		return out
	}
	sort.SliceStable(out, func(i, j int) bool {
		less := valueLess(out[i], out[j], flags&sortCaseInsensitive != 0)
		if flags&sortDescending != 0 {
			return valueLess(out[j], out[i], flags&sortCaseInsensitive != 0)
		}
		return less
	})
	return out
}

// valueLess orders two values for sorting.
func valueLess(a, b Value, foldCase bool) bool {
	if a.Type == TypeString && b.Type == TypeString {
		if foldCase {
			return ascii.Compare(a.Str, b.Str) < 0
		}
		return a.Str < b.Str
	}
	x, xok := a.asFloat()
	y, yok := b.asFloat()
	if xok && yok {
		return x < y
	}
	// Numbers sort before strings.
	return xok && !yok
}

// The remaining array primitives: set operations, searching, nesting,
// and the property-list forms.

func init() {
	register("ARRAY_FINDVAL", searchVals(true))
	register("ARRAY_EXCLUDEVAL", searchVals(false))

	register("ARRAY_MATCHVAL", matchVals(func(a *Array) []Value { return a.Values() }))
	register("ARRAY_MATCHKEY", matchVals(func(a *Array) []Value { return a.Keys() }))

	register("ARRAY_CUT", func(f *Frame) (*Result, error) {
		at, err := f.Pop()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		left, right := a.Cut(at)
		if err := f.Push(Arr(left)); err != nil {
			return nil, err
		}
		return nil, f.Push(Arr(right))
	})

	register("ARRAY_DELRANGE", func(f *Frame) (*Result, error) {
		to, err := f.Pop()
		if err != nil {
			return nil, err
		}
		from, err := f.Pop()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		c := a.Copy()
		for _, k := range a.Range(from, to).Keys() {
			c.Delete(k)
		}
		return nil, f.Push(Arr(c))
	})

	register("ARRAY_EXTRACT", func(f *Frame) (*Result, error) {
		keys, err := f.popArray()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		out := NewDict()
		for _, k := range keys.Values() {
			if v, ok := a.Get(k); ok {
				out.Set(k, v)
			}
		}
		return nil, f.Push(Arr(out))
	})

	register("ARRAY_COMPARE", func(f *Frame) (*Result, error) {
		b, err := f.popArray()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Int(int64(compareArrays(a, b))))
	})

	register("ARRAY_NUNION", setOp(unionOf))
	register("ARRAY_NINTERSECT", setOp(intersectOf))
	register("ARRAY_NDIFF", setOp(differenceOf))

	register("ARRAY_NESTED_GET", nested(nestedGet))
	register("ARRAY_NESTED_SET", nestedSet)
	register("ARRAY_NESTED_DEL", nested(nestedDel))

	register("ARRAY_GET_PROPVALS", func(f *Frame) (*Result, error) {
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		out := NewDict()
		count := 0
		for _, name := range h.PropChildren(obj, path) {
			child := join(path, name)
			if !f.propReadPerms(h, obj, child) {
				continue
			}
			// **A valueless directory is not an entry.**
			// `get_property` answers for the node, and
			// the type switch that follows has no
			// directory case -- `goodflag` stays zero and
			// nothing is added (`p_array.c:1863`). This
			// added the zero value, so two bare propdirs
			// answered 2 where upstream answers 0.
			v, ok := h.GetProp(obj, child)
			if !ok {
				continue
			}
			if count >= int(h.TuneInt("max_propfetch")) {
				return nil, errf("Too many " +
					"properties to put in an " +
					"array!")
			}
			count++
			out.Set(Str(name), fromProp(v))
		}
		return nil, f.Push(Arr(out))
	})
	register("ARRAY_GET_PROPDIRS", func(f *Frame) (*Result, error) {
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		var vals []Value
		for _, name := range h.PropChildren(obj, path) {
			child := join(path, name)
			if !f.propReadPerms(h, obj, child) {
				continue
			}
			if len(h.PropChildren(obj, child)) == 0 {
				continue
			}
			// The cap is `>=` here and `>=` there too,
			// but the message is its own.
			if len(vals) >= int(h.TuneInt(
				"max_propfetch")) {
				return nil, errf("Too many " +
					"propdirs to put in an " +
					"array!")
			}
			vals = append(vals, Str(name))
		}
		return nil, f.Push(Arr(NewList(vals)))
	})
	register("ARRAY_PUT_PROPLIST", func(f *Frame) (*Result, error) {
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		vals := a.Values()
		// Two write tests upstream, and both say "protected
		// property" rather than the plain refusal every other
		// property primitive uses.
		if !f.propWritePerms(h, obj, path+"#") {
			return nil, propProtected()
		}
		for i := range vals {
			elem := path + "#/" + itoa64(int64(i+1))
			if !f.propWritePerms(h, obj, elem) {
				return nil, propProtected()
			}
		}
		h.SetProp(obj, path+"#", props.Value{Type: props.Int, Num: int64(len(vals))})
		for i, v := range vals {
			h.SetProp(obj, path+"#/"+itoa64(int64(i+1)), toProp(v))
		}
		return nil, nil
	})
	register("ARRAY_PUT_REFLIST", func(f *Frame) (*Result, error) {
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		if !f.propWritePerms(h, obj, path) {
			return nil, propDenied()
		}
		var refs []ref.Ref
		for _, v := range a.Values() {
			if v.Type == TypeObject {
				refs = append(refs, v.Ref)
			}
		}
		writeRefList(h, obj, path, refs)
		return nil, nil
	})

	// Pinning controls whether an array is shared or copied on
	// assignment. Arrays here are copied on write, so a program
	// may set the flag and read it back but nothing depends on
	// it.
	register("ARRAY_PIN", passThroughArray)
	register("ARRAY_UNPIN", passThroughArray)
	register("ARRAY_DEFAULT_PINNING", func(f *Frame) (*Result, error) {
		_, err := f.popInt()
		return nil, err
	})
}

// popArrayArg takes an array, naming which argument it was when the
// type is wrong.
func (f *Frame) popArrayArg(n int, msg string) (*Array, error) {
	v, err := f.Pop()
	if err != nil {
		return nil, err
	}
	if v.Type != TypeArray || v.Array == nil {
		return nil, errf("%s (%d)", msg, n)
	}
	return v.Array, nil
}

// passThroughArray leaves an array as it is, for the pinning
// primitives.
func passThroughArray(f *Frame) (*Result, error) {
	a, err := f.popArray()
	if err != nil {
		return nil, err
	}
	return nil, f.Push(Arr(a))
}

// searchVals builds ARRAY_FINDVAL and ARRAY_EXCLUDEVAL, which return
// the keys whose values match, or do not.
func searchVals(want bool) primFunc {
	return func(f *Frame) (*Result, error) {
		target, err := f.Pop()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		keys, vals := a.Keys(), a.Values()
		var out []Value
		for i := range keys {
			if vals[i].Equal(target) == want {
				out = append(out, keys[i])
			}
		}
		return nil, f.Push(Arr(NewList(out)))
	}
}

// matchVals builds ARRAY_MATCHVAL and ARRAY_MATCHKEY, which filter by
// a SMATCH pattern.
func matchVals(pick func(*Array) []Value) primFunc {
	return func(f *Frame) (*Result, error) {
		pattern, err := f.popStr()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		keys, chosen := a.Keys(), pick(a)
		out := NewDict()
		for i := range chosen {
			if chosen[i].Type == TypeString &&
				ascii.SMatch(chosen[i].Str, pattern) {
				v, _ := a.Get(keys[i])
				out.Set(keys[i], v)
			}
		}
		return nil, f.Push(Arr(out))
	}
}

// setOp builds the n-way set operations, which take a count and that
// many arrays.
func setOp(combine func([]*Array) *Array) primFunc {
	return func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		if n < 1 {
			return nil, errf("Argument must be a positive integer.")
		}
		vals, err := f.PopN(int(n))
		if err != nil {
			return nil, err
		}
		arrays := make([]*Array, 0, len(vals))
		for _, v := range vals {
			if v.Type != TypeArray || v.Array == nil {
				return nil, errf("Argument not an array.")
			}
			arrays = append(arrays, v.Array)
		}
		return nil, f.Push(Arr(combine(arrays)))
	}
}

// unionOf collects every value that appears in any of the arrays.
func unionOf(arrays []*Array) *Array {
	var out []Value
	for _, a := range arrays {
		for _, v := range a.Values() {
			if !containsValue(out, v) {
				out = append(out, v)
			}
		}
	}
	return NewList(sortValues(out, 0))
}

// intersectOf collects the values every array holds.
func intersectOf(arrays []*Array) *Array {
	if len(arrays) == 0 {
		return NewList(nil)
	}
	var out []Value
	for _, v := range arrays[0].Values() {
		inAll := true
		for _, a := range arrays[1:] {
			if !containsValue(a.Values(), v) {
				inAll = false
				break
			}
		}
		if inAll && !containsValue(out, v) {
			out = append(out, v)
		}
	}
	return NewList(sortValues(out, 0))
}

// differenceOf collects the first array's values that no later one
// holds.
func differenceOf(arrays []*Array) *Array {
	if len(arrays) == 0 {
		return NewList(nil)
	}
	var out []Value
	for _, v := range arrays[0].Values() {
		inLater := false
		for _, a := range arrays[1:] {
			if containsValue(a.Values(), v) {
				inLater = true
				break
			}
		}
		if !inLater && !containsValue(out, v) {
			out = append(out, v)
		}
	}
	return NewList(sortValues(out, 0))
}

// containsValue reports whether a value appears in a slice.
func containsValue(vals []Value, want Value) bool {
	for _, v := range vals {
		if v.Equal(want) {
			return true
		}
	}
	return false
}

// compareArrays orders two arrays, comparing their values in turn.
func compareArrays(a, b *Array) int {
	av, bv := a.Values(), b.Values()
	n := len(av)
	if len(bv) < n {
		n = len(bv)
	}
	for i := 0; i < n; i++ {
		if valueLess(av[i], bv[i], false) {
			return -1
		}
		if valueLess(bv[i], av[i], false) {
			return 1
		}
	}
	return len(av) - len(bv)
}

// nested builds the nested get and delete primitives, which walk a
// path of keys into arrays of arrays.
func nested(fn func(*Array, []Value) (Value, *Array)) primFunc {
	return func(f *Frame) (*Result, error) {
		path, err := f.popArray()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		v, replaced := fn(a, path.Values())
		if replaced != nil {
			return nil, f.Push(Arr(replaced))
		}
		return nil, f.Push(v)
	}
}

// nestedGet reads through a path of keys.
func nestedGet(a *Array, path []Value) (Value, *Array) {
	cur := Arr(a)
	for _, k := range path {
		if cur.Type != TypeArray || cur.Array == nil {
			return Int(0), nil
		}
		v, ok := cur.Array.Get(k)
		if !ok {
			return Int(0), nil
		}
		cur = v
	}
	return cur, nil
}

// nestedDel removes the value at a path of keys.
func nestedDel(a *Array, path []Value) (Value, *Array) {
	if len(path) == 0 {
		return Value{}, a
	}
	c := a.Copy()
	if len(path) == 1 {
		c.Delete(path[0])
		return Value{}, c
	}
	inner, ok := c.Get(path[0])
	if !ok || inner.Type != TypeArray {
		return Value{}, c
	}
	_, replaced := nestedDel(inner.Array, path[1:])
	c.Set(path[0], Arr(replaced))
	return Value{}, c
}

// nestedSet writes a value at a path of keys, creating the arrays it
// needs.
func nestedSet(f *Frame) (*Result, error) {
	path, err := f.popArrayArg(3, "Argument not an array of indexes.")
	if err != nil {
		return nil, err
	}
	a, err := f.popArray()
	if err != nil {
		return nil, err
	}
	val, err := f.Pop()
	if err != nil {
		return nil, err
	}
	return nil, f.Push(Arr(setNested(a, path.Values(), val)))
}

// setNested is the recursive half of ARRAY_NESTED_SET.
func setNested(a *Array, path []Value, val Value) *Array {
	if len(path) == 0 {
		return a
	}
	c := a.Copy()
	if len(path) == 1 {
		c.Set(path[0], val)
		return c
	}
	inner := NewDict()
	if existing, ok := c.Get(path[0]); ok &&
		existing.Type == TypeArray {
		inner = existing.Array
	}
	c.Set(path[0], Arr(setNested(inner, path[1:], val)))
	return c
}

// listCount reads a proplist's count property, which upstream tries
// as an **integer and then as a string** — `get_property_value`
// then `get_property_class` with `atoi` over it (`p_array.c:1954`).
// So a count written as text works.
func listCount(h Host, obj ref.Ref, path string) int64 {
	v, ok := h.GetProp(obj, path)
	if !ok {
		return 0
	}
	if v.Type == props.Int {
		return v.Num
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v.Str), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// listItemProp reads one item of a proplist, trying upstream's three
// spellings in order (`p_array.c:2013`) and reporting which path it
// settled on — the read-permission test is made against that one,
// and against the *last* spelling tried when none of them answers.
func listItemProp(h Host, obj ref.Ref, path string,
	i int64) (string, props.Value, bool) {

	n := itoa64(i)
	for _, elem := range [3]string{
		path + "#/" + n,
		path + "/" + n,
		path + n,
	} {
		if v, ok := h.GetProp(obj, elem); ok {
			return elem, v, true
		}
	}
	return path + n, props.Value{}, false
}
