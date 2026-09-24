package muf

import (
	"sort"
	"strings"
)

// ARRAY_INSERTRANGE, ARRAY_SORT_INDEXED, ARRAY_PUT_PROPVALS,
// ARRAY_GET_IGNORELIST, ARRAY_INTERPRET and ARRAY_NOTIFY_SECURE are
// ports of the more tractable primitives left in src/p_array.c.
// ARRAY_FILTER_FLAGS is not ported: it needs
// init_checkflags/checkflags, a flag-matching mini-language (the same
// one @find would need) that nothing in this codebase has built yet.
func init() {
	register("ARRAY_INSERTRANGE", func(f *Frame) (*Result, error) {
		itemsV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		startV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		arrV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if itemsV.Type != TypeArray {
			return nil, errf("Argument not an array. (3)")
		}
		if startV.Type != TypeInteger &&
			startV.Type != TypeString {
			return nil, errf("Argument not an integer or string. (2)")
		}
		if arrV.Type != TypeArray {
			return nil, errf("Argument not an array. (1)")
		}
		items, a := itemsV.Array, arrV.Array
		c := a.Copy()
		if startV.Type == TypeInteger {
			for i, v := range items.Values() {
				c.Insert(Int(startV.Num+int64(i)), v)
			}
		} else {
			keys, vals := items.Keys(), items.Values()
			for i := range keys {
				c.Set(keys[i], vals[i])
			}
		}
		return nil, f.Push(Arr(c))
	})

	register("ARRAY_SORT_INDEXED", func(f *Frame) (*Result, error) {
		indexKey, err := f.Pop()
		if err != nil {
			return nil, err
		}
		flagsV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		arrV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if arrV.Type != TypeArray {
			return nil, errf("Argument not an array. (1)")
		}
		a := arrV.Array
		if !a.IsList() {
			return nil, errf("Argument must be a list type array. (1)")
		}
		if flagsV.Type != TypeInteger {
			return nil, errf("Expected integer argument to specify sort type. (2)")
		}
		if indexKey.Type != TypeInteger &&
			indexKey.Type != TypeString {
			return nil, errf("Index argument not an integer or string. (3)")
		}

		rows := a.Values()
		for _, row := range rows {
			if row.Type != TypeArray {
				return nil, errf("Argument must be a list array of arrays. (1)")
			}
		}
		keyed := make([]Value, len(rows))
		for i, row := range rows {
			keyed[i], _ = row.Array.Get(indexKey)
		}

		order := make([]int, len(rows))
		for i := range order {
			order[i] = i
		}
		flags := int(flagsV.Num)
		if flags&sortShuffle == 0 {
			sort.SliceStable(order, func(i, j int) bool {
				less := valueLess(keyed[order[i]], keyed[order[j]], flags&sortCaseInsensitive != 0)
				if flags&sortDescending != 0 {
					return valueLess(keyed[order[j]], keyed[order[i]], flags&sortCaseInsensitive != 0)
				}
				return less
			})
		}

		out := make([]Value, len(rows))
		for i, idx := range order {
			out[i] = rows[idx]
		}
		return nil, f.Push(Arr(NewList(out)))
	})

	register("ARRAY_PUT_PROPVALS", func(f *Frame) (*Result, error) {
		vals, err := f.popArray()
		if err != nil {
			return nil, err
		}
		dir, err := f.popStr()
		if err != nil {
			return nil, err
		}
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		if !h.Valid(obj) {
			return nil, errf("Invalid dbref. (1)")
		}
		keys, vs := vals.Keys(), vals.Values()
		for i, k := range keys {
			var name string
			switch k.Type {
			case TypeString:
				name = k.Str
			case TypeInteger:
				name = itoa64(k.Num)
			default:
				continue
			}
			h.SetProp(obj, dir+"/"+name, toProp(vs[i]))
		}
		return nil, nil
	})

	// ARRAY_GET_IGNORELIST's own "if (mlev < 3)" uses the
	// dispatcher's own generic wording, so mlev_gen.go's
	// generated floor (via primMLevel) already gates it before
	// this ever runs — no inline check needed.
	register("ARRAY_GET_IGNORELIST", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		if !h.Valid(obj) {
			return nil, errf("Invalid dbref.")
		}
		list := readRefList(h, h.Owner(obj), ignorePropPath)
		vals := make([]Value, len(list))
		for i, r := range list {
			vals[i] = Obj(r)
		}
		return nil, f.Push(Arr(NewList(vals)))
	})

	register("ARRAY_INTERPRET", func(f *Frame) (*Result, error) {
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		var b strings.Builder
		for _, v := range a.Values() {
			if v.Type == TypeObject {
				b.WriteString(h.Name(v.Ref))
				continue
			}
			b.WriteString(v.String())
		}
		return nil, f.Push(Str(b.String()))
	})

	register("ARRAY_NOTIFY_SECURE", func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Mucker level 3 primitive.")
		}
		refsV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		secureV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		// Every connection here is TLS, so upstream's
		// insecure-message array and its listen-prop
		// triggering (only reachable from that branch
		// upstream, despite what its own doc comment claims)
		// never apply — see internal/muf/prim_connects.go's
		// own DESCRSECURE? comment for the same "always true
		// here" divergence.
		insecureV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if refsV.Type != TypeArray {
			return nil, errf("Argument not an array of dbrefs. (3)")
		}
		if secureV.Type != TypeArray {
			return nil, errf("Argument not an array of strings. (2)")
		}
		if insecureV.Type != TypeArray {
			return nil, errf("Argument not an array of strings. (1)")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		for _, r := range refsV.Array.Values() {
			if r.Type != TypeObject {
				continue
			}
			for _, descr := range h.Descriptors(r.Ref) {
				for _, msg := range secureV.Array.Values() {
					h.DescrNotify(descr, msg.String())
				}
			}
		}
		return nil, nil
	})
}

// ignorePropPath is upstream's IGNORE_PROP — see
// internal/game/misc_host.go for why the same literal is duplicated
// rather than shared: that side needs no MUF Value/Frame machinery,
// this side is package muf.
const ignorePropPath = "@__sys__/ignore/def"
