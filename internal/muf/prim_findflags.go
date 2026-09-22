package muf

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// ARRAY_FILTER_FLAGS and FINDNEXT are the two primitives that take a flag
// expression — see checkflags.go for the language itself. They are together
// here rather than with the rest of their own modules because the expression
// is the whole of what they do.
func init() {
	register("ARRAY_FILTER_FLAGS", func(f *Frame) (*Result, error) {
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
		for _, v := range arrV.Array.Values() {
			if v.Type != TypeObject {
				return nil, errf("Argument not an array of dbrefs. (1)")
			}
		}
		if flagsV.Type != TypeString || flagsV.Str == "" {
			return nil, errf("Argument not a non-null string. (2)")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		check := parseFlagCheck(flagsV.Str)
		var kept []Value
		for _, v := range arrV.Array.Values() {
			if h.Valid(v.Ref) && check.matches(h, v.Ref) {
				kept = append(kept, v)
			}
		}
		return nil, f.Push(Arr(NewList(kept)))
	})

	// FINDNEXT walks the database from one object to the next one matching
	// an owner, a name pattern and a flag expression — what @find is built
	// on, exposed so a program can page through the results itself rather
	// than being handed all of them at once.
	register("FINDNEXT", func(f *Frame) (*Result, error) {
		flagsV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		patternV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		ownerV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		startV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if flagsV.Type != TypeString {
			return nil, errf("Expected string argument. (4)")
		}
		if patternV.Type != TypeString {
			return nil, errf("Expected string argument. (3)")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if ownerV.Type != TypeObject ||
			(ownerV.Ref != ref.Nothing && !isPlayer(h, ownerV.Ref)) {
			return nil, errf("Expected player argument. (2)")
		}
		if startV.Type != TypeObject ||
			(startV.Ref != ref.Nothing && !h.Valid(startV.Ref)) {
			return nil, errf("Invalid dbref argument. (1)")
		}
		owner := ownerV.Ref
		if f.MLevel() < 2 {
			return nil, errf("Permission denied.  Requires at least Mucker Level 2.")
		}
		if f.MLevel() < 3 {
			switch {
			case owner == ref.Nothing:
				return nil, errf("Permission denied.  " +
					"Owner inspecific searches require Mucker Level 3.")
			case owner != h.Owner(f.Prog.Ref):
				return nil, errf("Permission denied.  Searching for " +
					"other people's stuff requires Mucker Level 3.")
			}
		}

		// NOTHING starts the walk at #0; anything else resumes past it, so
		// feeding the last result back in finds the one after it.
		start := ref.Ref(0)
		if startV.Ref != ref.Nothing {
			start = startV.Ref + 1
		}
		check := parseFlagCheck(flagsV.Str)
		pattern := patternV.Str
		for i := start; i < h.Top(); i++ {
			if owner != ref.Nothing && h.Owner(i) != owner {
				continue
			}
			if h.ObjType(i) == ref.TypeGarbage || !check.matches(h, i) {
				continue
			}
			if pattern != "" && !ascii.SMatch(h.Name(i), pattern) {
				continue
			}
			return nil, f.Push(Obj(i))
		}
		return nil, f.Push(Obj(ref.Nothing))
	})
}
