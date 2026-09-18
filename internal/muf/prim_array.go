package muf

import (
	"sort"
	"strings"

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
		// A proplist is "path#" holding a count, and "path#/1".."path#/n"
		// holding the entries.
		countVal, _ := h.GetProp(obj, path+"#")
		n := countVal.Num
		vals := make([]Value, 0, n)
		for i := int64(1); i <= n; i++ {
			v, _ := h.GetProp(obj, path+"#/"+itoa64(i))
			vals = append(vals, fromProp(v))
		}
		return nil, f.Push(Arr(NewList(vals)))
	})

	register("ARRAY_GET_REFLIST", func(f *Frame) (*Result, error) {
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		// A reflist is a single string of space-separated dbrefs.
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
		for _, who := range targets.Values() {
			if who.Type != TypeObject {
				continue
			}
			for _, line := range lines.Values() {
				h.Notify(who.Ref, line.String())
			}
		}
		return nil, nil
	})
}

// itoa64 renders an integer for a property path.
func itoa64(n int64) string {
	return Int(n).String()
}

// edge builds ARRAY_FIRST and ARRAY_LAST, which push a key and a flag.
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

// sortValues orders values the way ARRAY_SORT does: numbers before strings,
// each ascending unless the flags say otherwise.
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
