package muf

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// String, array and output primitives.

func init() {
	register("STRCAT", func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		if v[0].Type != TypeString || v[1].Type != TypeString {
			return nil, errf("STRCAT needs two strings")
		}
		return nil, f.Push(Str(v[0].Str + v[1].Str))
	})
	register("STRLEN", func(f *Frame) (*Result, error) {
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Int(int64(len(s))))
	})
	register("STRCMP", func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		if v[0].Type != TypeString || v[1].Type != TypeString {
			return nil, errf("STRCMP needs two strings")
		}
		return nil, f.Push(Int(int64(strings.Compare(v[0].Str, v[1].Str))))
	})
	register("STRINGCMP", func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		if v[0].Type != TypeString || v[1].Type != TypeString {
			return nil, errf("STRINGCMP needs two strings")
		}
		return nil, f.Push(Int(int64(ascii.Compare(v[0].Str, v[1].Str))))
	})
	register("STRINGPFX", func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		if v[0].Type != TypeString || v[1].Type != TypeString {
			return nil, errf("STRINGPFX needs two strings")
		}
		return nil, f.Push(Bool(ascii.HasPrefix(v[0].Str, v[1].Str)))
	})
	register("INSTR", instr(false))
	register("RINSTR", instr(true))
	register("MIDSTR", func(f *Frame) (*Result, error) {
		v, err := f.PopN(3)
		if err != nil {
			return nil, err
		}
		if v[0].Type != TypeString || v[1].Type != TypeInteger || v[2].Type != TypeInteger {
			return nil, errf("MIDSTR needs a string and two integers")
		}
		// MUF indexes strings from one.
		s, start, length := v[0].Str, v[1].Num, v[2].Num
		if start < 1 || length < 0 || start > int64(len(s)) {
			return nil, f.Push(Str(""))
		}
		from := start - 1
		to := from + length
		if to > int64(len(s)) {
			to = int64(len(s))
		}
		return nil, f.Push(Str(s[from:to]))
	})
	register("TOUPPER", mapString(strings.ToUpper))
	register("TOLOWER", mapString(strings.ToLower))
	register("STRIP", mapString(func(s string) string { return strings.TrimSpace(s) }))
	register("STRIPLEAD", mapString(func(s string) string { return strings.TrimLeft(s, " \t\r\n") }))
	register("STRIPTAIL", mapString(func(s string) string { return strings.TrimRight(s, " \t\r\n") }))

	register("SPLIT", func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		if v[0].Type != TypeString || v[1].Type != TypeString {
			return nil, errf("SPLIT needs two strings")
		}
		before, after, found := strings.Cut(v[0].Str, v[1].Str)
		if !found {
			after = ""
		}
		if err := f.Push(Str(before)); err != nil {
			return nil, err
		}
		return nil, f.Push(Str(after))
	})

	register("EXPLODE", func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		if v[0].Type != TypeString || v[1].Type != TypeString {
			return nil, errf("EXPLODE needs two strings")
		}
		if v[1].Str == "" {
			return nil, errf("EXPLODE needs a non-empty separator")
		}
		parts := strings.Split(v[0].Str, v[1].Str)
		// EXPLODE pushes the parts in reverse, then the count, so the
		// first part ends up on top.
		for i := len(parts) - 1; i >= 0; i-- {
			if err := f.Push(Str(parts[i])); err != nil {
				return nil, err
			}
		}
		return nil, f.Push(Int(int64(len(parts))))
	})

	register("ARRAY_MAKE", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, errf("ARRAY_MAKE needs a count that is not negative")
		}
		vals, err := f.PopN(int(n))
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Arr(NewList(vals)))
	})
	register("ARRAY_COUNT", func(f *Frame) (*Result, error) {
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Int(int64(a.Len())))
	})
	register("ARRAY_GETITEM", func(f *Frame) (*Result, error) {
		key, err := f.Pop()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		v, ok := a.Get(key)
		if !ok {
			// A missing key reads as #-1, not as a failure.
			v = Obj(ref.Nothing)
		}
		return nil, f.Push(v)
	})
	register("ARRAY_SETITEM", func(f *Frame) (*Result, error) {
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
		c.Set(key, val)
		return nil, f.Push(Arr(c))
	})
	register("ARRAY_APPENDITEM", func(f *Frame) (*Result, error) {
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		val, err := f.Pop()
		if err != nil {
			return nil, err
		}
		c := a.Copy()
		c.Append(val)
		return nil, f.Push(Arr(c))
	})
	register("ARRAY_KEYS", func(f *Frame) (*Result, error) {
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		keys := a.Keys()
		for _, k := range keys {
			if err := f.Push(k); err != nil {
				return nil, err
			}
		}
		return nil, f.Push(Int(int64(len(keys))))
	})
	register("ARRAY_VALS", func(f *Frame) (*Result, error) {
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		vals := a.Values()
		for _, v := range vals {
			if err := f.Push(v); err != nil {
				return nil, err
			}
		}
		return nil, f.Push(Int(int64(len(vals))))
	})
	register("ARRAY_JOIN", func(f *Frame) (*Result, error) {
		sep, err := f.popStr()
		if err != nil {
			return nil, err
		}
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		parts := make([]string, 0, a.Len())
		for _, v := range a.Values() {
			parts = append(parts, v.String())
		}
		return nil, f.Push(Str(strings.Join(parts, sep)))
	})

	register("NOTIFY", func(f *Frame) (*Result, error) {
		msg, err := f.popStr()
		if err != nil {
			return nil, err
		}
		who, err := f.popRef()
		if err != nil {
			return nil, err
		}
		if f.host == nil {
			return nil, errf("NOTIFY needs a running server")
		}
		// A carriage return inside a message separates lines, which is
		// how MUF writes multi-line output.
		for _, line := range strings.Split(msg, "\r") {
			f.host.Notify(who, line)
		}
		return nil, nil
	})
	register("NAME", hostRefToStr(func(h Host, r ref.Ref) string { return h.Name(r) }))
	register("GETPROPSTR", func(f *Frame) (*Result, error) {
		path, err := f.popStr()
		if err != nil {
			return nil, err
		}
		obj, err := f.popRef()
		if err != nil {
			return nil, err
		}
		if f.host == nil {
			return nil, errf("GETPROPSTR needs a running server")
		}
		return nil, f.Push(Str(f.host.GetPropStr(obj, path)))
	})
	register("SETPROP", func(f *Frame) (*Result, error) {
		val, err := f.Pop()
		if err != nil {
			return nil, err
		}
		path, err := f.popStr()
		if err != nil {
			return nil, err
		}
		obj, err := f.popRef()
		if err != nil {
			return nil, err
		}
		if f.host == nil {
			return nil, errf("SETPROP needs a running server")
		}
		f.host.SetPropStr(obj, path, val.String())
		return nil, nil
	})
	register("LOCATION", hostRefToRef(func(h Host, r ref.Ref) ref.Ref { return h.Location(r) }))
	register("OWNER", hostRefToRef(func(h Host, r ref.Ref) ref.Ref { return h.Owner(r) }))
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
}

// popArray takes an array from the stack.
func (f *Frame) popArray() (*Array, error) {
	v, err := f.Pop()
	if err != nil {
		return nil, err
	}
	if v.Type != TypeArray || v.Array == nil {
		return nil, errf("expected an array, got a %v", v.Type)
	}
	return v.Array, nil
}

// mapString builds a primitive that transforms a string.
func mapString(fn func(string) string) primFunc {
	return func(f *Frame) (*Result, error) {
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Str(fn(s)))
	}
}

// instr builds INSTR or RINSTR, which report a one-based position or zero.
func instr(fromEnd bool) primFunc {
	return func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		if v[0].Type != TypeString || v[1].Type != TypeString {
			return nil, errf("INSTR needs two strings")
		}
		var i int
		if fromEnd {
			i = strings.LastIndex(v[0].Str, v[1].Str)
		} else {
			i = strings.Index(v[0].Str, v[1].Str)
		}
		return nil, f.Push(Int(int64(i + 1)))
	}
}

// hostRefToStr builds a primitive that asks the host about an object.
func hostRefToStr(fn func(Host, ref.Ref) string) primFunc {
	return func(f *Frame) (*Result, error) {
		r, err := f.popRef()
		if err != nil {
			return nil, err
		}
		if f.host == nil {
			return nil, errf("this primitive needs a running server")
		}
		return nil, f.Push(Str(fn(f.host, r)))
	}
}

// hostRefToRef builds a primitive that resolves one object to another.
func hostRefToRef(fn func(Host, ref.Ref) ref.Ref) primFunc {
	return func(f *Frame) (*Result, error) {
		r, err := f.popRef()
		if err != nil {
			return nil, err
		}
		if f.host == nil {
			return nil, errf("this primitive needs a running server")
		}
		return nil, f.Push(Obj(fn(f.host, r)))
	}
}
