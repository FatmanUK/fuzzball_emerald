package muf

import "sort"

// Array is a MUF array, which is either a list indexed by consecutive
// integers from zero, or a dictionary keyed by integers or strings.
//
// Fuzzball keeps both in one type and lets a program move between them, so
// this does too: keys preserve insertion-independent order by sorting, which
// is what array_keys and the iteration primitives expose.
type Array struct {
	// list holds a packed array's values. It is nil for a dictionary.
	list []Value
	// dict holds a dictionary's entries, keyed by a value's comparable form.
	dict map[arrayKey]Value
	// keys keeps the dictionary's keys in sorted order.
	keys []Value
}

// arrayKey is a dictionary key reduced to something comparable.
type arrayKey struct {
	isStr bool
	num   int64
	str   string
}

func keyOf(v Value) (arrayKey, bool) {
	switch v.Type {
	case TypeInteger:
		return arrayKey{num: v.Num}, true
	case TypeString:
		return arrayKey{isStr: true, str: v.Str}, true
	}
	return arrayKey{}, false
}

// NewList returns a packed array.
func NewList(vals []Value) *Array {
	return &Array{list: append([]Value{}, vals...)}
}

// NewDict returns an empty dictionary.
func NewDict() *Array { return &Array{dict: map[arrayKey]Value{}} }

// IsList reports whether the array is packed.
func (a *Array) IsList() bool { return a.dict == nil }

// Len returns the number of entries.
func (a *Array) Len() int {
	if a.IsList() {
		return len(a.list)
	}
	return len(a.dict)
}

// Get returns the value at a key.
func (a *Array) Get(key Value) (Value, bool) {
	if a.IsList() {
		if key.Type != TypeInteger || key.Num < 0 || int(key.Num) >= len(a.list) {
			return Value{}, false
		}
		return a.list[key.Num], true
	}
	k, ok := keyOf(key)
	if !ok {
		return Value{}, false
	}
	v, found := a.dict[k]
	return v, found
}

// Set stores a value at a key, converting a list to a dictionary when the key
// does not extend it.
func (a *Array) Set(key, val Value) {
	if a.IsList() {
		if key.Type == TypeInteger && key.Num >= 0 && int(key.Num) <= len(a.list) {
			if int(key.Num) == len(a.list) {
				a.list = append(a.list, val)
			} else {
				a.list[key.Num] = val
			}
			return
		}
		a.toDict()
	}
	k, ok := keyOf(key)
	if !ok {
		return
	}
	if _, exists := a.dict[k]; !exists {
		a.keys = append(a.keys, key)
		a.sortKeys()
	}
	a.dict[k] = val
}

// Delete removes a key.
func (a *Array) Delete(key Value) {
	if a.IsList() {
		if key.Type != TypeInteger || key.Num < 0 || int(key.Num) >= len(a.list) {
			return
		}
		a.list = append(a.list[:key.Num], a.list[key.Num+1:]...)
		return
	}
	k, ok := keyOf(key)
	if !ok {
		return
	}
	if _, exists := a.dict[k]; !exists {
		return
	}
	delete(a.dict, k)
	for i, existing := range a.keys {
		if ek, _ := keyOf(existing); ek == k {
			a.keys = append(a.keys[:i], a.keys[i+1:]...)
			break
		}
	}
}

// Append adds a value to the end of a list.
func (a *Array) Append(val Value) {
	if !a.IsList() {
		a.Set(Int(int64(a.Len())), val)
		return
	}
	a.list = append(a.list, val)
}

// Keys returns the keys in order: 0..n-1 for a list, sorted for a dictionary.
func (a *Array) Keys() []Value {
	if a.IsList() {
		out := make([]Value, len(a.list))
		for i := range a.list {
			out[i] = Int(int64(i))
		}
		return out
	}
	return append([]Value{}, a.keys...)
}

// Values returns the values, in key order.
func (a *Array) Values() []Value {
	if a.IsList() {
		return append([]Value{}, a.list...)
	}
	out := make([]Value, 0, len(a.keys))
	for _, k := range a.keys {
		kk, _ := keyOf(k)
		out = append(out, a.dict[kk])
	}
	return out
}

// Copy returns a shallow copy, which is what MUF's assignment semantics give.
func (a *Array) Copy() *Array {
	if a.IsList() {
		return NewList(a.list)
	}
	c := NewDict()
	for _, k := range a.keys {
		kk, _ := keyOf(k)
		c.Set(k, a.dict[kk])
	}
	return c
}

// toDict converts a packed array in place.
func (a *Array) toDict() {
	d := map[arrayKey]Value{}
	keys := make([]Value, 0, len(a.list))
	for i, v := range a.list {
		k := Int(int64(i))
		kk, _ := keyOf(k)
		d[kk] = v
		keys = append(keys, k)
	}
	a.dict, a.keys, a.list = d, keys, nil
}

// sortKeys orders a dictionary's keys: integers before strings, each ascending.
func (a *Array) sortKeys() {
	sort.SliceStable(a.keys, func(i, j int) bool {
		x, y := a.keys[i], a.keys[j]
		if x.Type != y.Type {
			return x.Type == TypeInteger
		}
		if x.Type == TypeInteger {
			return x.Num < y.Num
		}
		return x.Str < y.Str
	})
}

// Insert adds a value at a key, shifting a list's later entries up rather than
// replacing one.
func (a *Array) Insert(key, val Value) {
	if a.IsList() && key.Type == TypeInteger {
		i := int(key.Num)
		if i < 0 {
			i = 0
		}
		if i > len(a.list) {
			i = len(a.list)
		}
		a.list = append(a.list, Value{})
		copy(a.list[i+1:], a.list[i:])
		a.list[i] = val
		return
	}
	a.Set(key, val)
}

// Range returns the entries between two keys, inclusive.
func (a *Array) Range(from, to Value) *Array {
	if a.IsList() && from.Type == TypeInteger && to.Type == TypeInteger {
		lo, hi := int(from.Num), int(to.Num)
		if lo < 0 {
			lo = 0
		}
		if hi >= len(a.list) {
			hi = len(a.list) - 1
		}
		if lo > hi {
			return NewList(nil)
		}
		return NewList(a.list[lo : hi+1])
	}

	out := NewDict()
	keys, vals := a.Keys(), a.Values()
	for i, k := range keys {
		if inRange(k, from, to) {
			out.Set(k, vals[i])
		}
	}
	return out
}

// inRange reports whether a key falls between two others.
func inRange(k, from, to Value) bool {
	return !valueLess(k, from, false) && !valueLess(to, k, false)
}

// Cut splits an array at a key, returning what comes before it and what comes
// from it onwards.
func (a *Array) Cut(at Value) (*Array, *Array) {
	keys, vals := a.Keys(), a.Values()
	split := len(keys)
	for i, k := range keys {
		if !valueLess(k, at, false) {
			split = i
			break
		}
	}
	if a.IsList() {
		return NewList(vals[:split]), NewList(vals[split:])
	}
	left, right := NewDict(), NewDict()
	for i, k := range keys {
		if i < split {
			left.Set(k, vals[i])
		} else {
			right.Set(k, vals[i])
		}
	}
	return left, right
}
