package muf

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
)

// BLESSPROP, UNBLESSPROP, BLESSED?, PROP-NAME-OK?, PARSEMPI, PARSEMPIBLESSED
// and ARRAY_FILTER_PROP are ports of the more tractable primitives left in
// src/p_props.c. PARSEPROPEX is not ported: unlike PARSEMPI, it converts a
// whole caller-supplied dictionary into MPI variables and hands one back —
// materially more plumbing than a primitive port on its own, and deferred
// rather than rushed.
//
// None of these primitives check prop_read_perms/prop_write_perms —
// Emerald's property primitives have no read/write permission model at all
// yet (SETPROP and GETPROP do not either), so adding one only here would
// make these primitives stricter than the rest of the property surface.
func init() {
	register("BLESSPROP", blessEdit(true))
	register("UNBLESSPROP", blessEdit(false))

	// BLESSED?'s own mlev<2 uses the dispatcher's own generic wording, so
	// mlev_gen.go's generated floor already gates it — no inline check.
	register("BLESSED?", func(f *Frame) (*Result, error) {
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		if !h.Valid(obj) {
			return nil, errf("Invalid dbref (1)")
		}
		if path == "" {
			return nil, errf("Null string not allowed. (2)")
		}
		return nil, f.Push(Bool(h.IsPropBlessed(obj, path)))
	})

	register("PROP-NAME-OK?", func(f *Frame) (*Result, error) {
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(isValidPropName(s)))
	})

	register("PARSEMPI", parseMPI(false))
	register("PARSEMPIBLESSED", parseMPI(true))

	register("ARRAY_FILTER_PROP", func(f *Frame) (*Result, error) {
		patternV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		nameV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		refsV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if refsV.Type != TypeArray {
			return nil, errf("Argument not an array. (1)")
		}
		if nameV.Type != TypeString || nameV.Str == "" {
			return nil, errf("Argument not a non-null string. (2)")
		}
		if patternV.Type != TypeString {
			return nil, errf("Argument not a string pattern. (3)")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}

		path := trimPropDelim(nameV.Str)
		var out []Value
		for _, r := range refsV.Array.Values() {
			if r.Type != TypeObject || !h.Valid(r.Ref) {
				continue
			}
			v, ok := h.GetProp(r.Ref, path)
			if !ok {
				continue
			}
			if match.StringMatch(v.StringValue(), patternV.Str) {
				out = append(out, r)
			}
		}
		return nil, f.Push(Arr(NewList(out)))
	})
}

// isValidPropName is a port of is_valid_propname: non-empty, and containing
// neither a carriage return nor the ':' property-flag delimiter.
func isValidPropName(s string) bool {
	return s != "" && !strings.ContainsAny(s, "\r:")
}

// trimPropDelim strips trailing '/' from a property path, upstream's own
// repeated "yet another implementation of removing trailing slashes".
func trimPropDelim(s string) string {
	return strings.TrimRight(s, "/")
}

// blessEdit builds BLESSPROP and UNBLESSPROP, which share their argument
// shape and mlev-4 wizard-only floor.
func blessEdit(blessed bool) primFunc {
	return func(f *Frame) (*Result, error) {
		nameV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		objV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if nameV.Type != TypeString {
			return nil, errf("Non-string argument (2)")
		}
		if objV.Type != TypeObject {
			return nil, errf("Non-object argument (1)")
		}
		if f.MLevel() < 4 {
			return nil, errf("Permission denied.")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if !h.Valid(objV.Ref) {
			return nil, errf("Non-object argument (1)")
		}
		if strings.ContainsRune(nameV.Str, '\r') || strings.ContainsRune(nameV.Str, ':') {
			return nil, errf("Illegal propname")
		}
		h.BlessProp(objV.Ref, trimPropDelim(nameV.Str), blessed)
		return nil, nil
	}
}

// parseMPI builds PARSEMPI and PARSEMPIBLESSED.
func parseMPI(blessed bool) primFunc {
	floor := 3
	if blessed {
		floor = 4
	}
	return func(f *Frame) (*Result, error) {
		privV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		argV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		mpiV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		objV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		// PARSEMPI's own mlev<3 uses the dispatcher's generic wording, so
		// mlev_gen.go's generated floor already gates it. PARSEMPIBLESSED's
		// mlev<4 does not — plain "Permission denied.", not the generic
		// "...Requires Wizbit." — so only that one needs checking here.
		if blessed && f.MLevel() < floor {
			return nil, errf("Permission denied.")
		}
		if objV.Type != TypeObject {
			return nil, errf("Non-object argument (1)")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if !h.Valid(objV.Ref) {
			return nil, errf("Invalid object (1)")
		}
		if mpiV.Type != TypeString {
			return nil, errf("String expected (2)")
		}
		if argV.Type != TypeString {
			return nil, errf("String expected (3)")
		}
		if privV.Type != TypeInteger {
			return nil, errf("Integer expected (4)")
		}
		if privV.Num != 0 && privV.Num != 1 {
			return nil, errf("Integer of 0 or 1 expected (4)")
		}
		out, err := h.ParseMPI(objV.Ref, mpiV.Str, argV.Str, blessed)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Str(out))
	}
}

// PARSEPROPEX is PARSEPROP with a dictionary of variables handed in and
// handed back: the MPI in the property can read them, and whatever it leaves
// in them comes out the other side. It is how a MUF program and a property's
// MPI exchange more than one value.
func init() {
	register("PARSEPROPEX", func(f *Frame) (*Result, error) {
		// The floor's wording is its own, so it is checked here rather
		// than left to the generated table — see PARSEPROP.
		if f.MLevel() < 3 {
			return nil, errf("Mucker level 3 or greater required.")
		}
		privateV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		varsV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		pathV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		objV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if objV.Type != TypeObject {
			return nil, errf("Non-object argument. (1)")
		}
		if pathV.Type != TypeString {
			return nil, errf("Non-string argument. (2)")
		}
		if varsV.Type != TypeArray {
			return nil, errf("Non-array argument. (3)")
		}
		if varsV.Array.IsList() {
			return nil, errf("Dictionary array expected. (3)")
		}
		if privateV.Type != TypeInteger {
			return nil, errf("Non-integer argument. (4)")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if !h.Valid(objV.Ref) {
			return nil, errf("Invalid object. (1)")
		}
		if privateV.Num != 0 && privateV.Num != 1 {
			return nil, errf("Integer of 0 or 1 expected. (4)")
		}

		keys, vals := varsV.Array.Keys(), varsV.Array.Values()
		vars := make([]MPIVar, 0, len(keys))
		for i, k := range keys {
			if k.Type != TypeString {
				return nil, errf("Only string keys supported. (3)")
			}
			if k.Str == "" {
				return nil, errf("Empty string keys not supported. (3)")
			}
			if len(k.Str) > maxMPINameLen {
				return nil, errf("Key too long to be an MPI variable. (3)")
			}
			// Every value becomes text, because that is all an MPI
			// variable can hold — a dbref as "#123", a float in %g.
			switch vals[i].Type {
			case TypeInteger, TypeFloat, TypeObject, TypeString, TypeLock:
			default:
				return nil, errf("Only integer, float, dbref, string and " +
					"lock values supported. (3)")
			}
			vars = append(vars, MPIVar{Name: k.Str, Value: mpiValue(vals[i])})
		}

		out, after, err := h.ParsePropEx(objV.Ref, trimPropDelim(pathV.Str),
			vars, privateV.Num != 0)
		if err != nil {
			return nil, errf("%s", err.Error())
		}

		// The same dictionary comes back, its values replaced by what the
		// MPI left in each variable — always strings, whatever went in.
		d := NewDict()
		for _, kv := range after {
			d.Set(Str(kv.Name), Str(kv.Value))
		}
		if err := f.Push(Arr(d)); err != nil {
			return nil, err
		}
		return nil, f.Push(Str(out))
	})
}

// maxMPINameLen is upstream's MAX_MFUN_NAME_LEN, which bounds a variable's
// name as well as a function's.
const maxMPINameLen = 16

// mpiValue renders a MUF value as the text an MPI variable holds. A dbref
// keeps its '#', unlike INTOSTR's own rendering, because MPI's own object
// functions expect to read one back.
func mpiValue(v Value) string {
	if v.Type == TypeObject {
		return v.Ref.String()
	}
	return v.String()
}
