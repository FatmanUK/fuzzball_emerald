package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ascii"

// The arithmetic error flags, which a program inspects rather than
// being aborted by.

// errorNames are the flags in the order is_set? numbers them.
var errorNames = []string{"DIV_ZERO", "NAN", "IMAGINARY", "FBOUNDS", "IBOUNDS"}

// errorDescriptions are what ERROR_STR reports.
var errorDescriptions = []string{
	"Division by zero attempted.",
	"Result was not a number.",
	"Result was imaginary.",
	"Floating-point inputs were infinite or out of range.",
	"Calculation resulted in an integer overflow.",
}

func init() {
	register("ERROR_NUM", func(f *Frame) (*Result, error) {
		return nil, f.Push(Int(int64(len(errorNames))))
	})
	// ERROR_BIT is the one that takes a **string only**
	// (`p_error.c:484`), where the other five accept either.
	register("ERROR_BIT", func(f *Frame) (*Result, error) {
		name, err := f.popStr()
		if err != nil {
			return nil, err
		}
		for i, n := range errorNames {
			if ascii.EqualFold(n, name) {
				return nil, f.Push(Int(int64(i)))
			}
		}
		return nil, f.Push(Int(-1))
	})
	// ERROR_NAME takes an **integer only** (`p_error.c:430`),
	// where ERROR_STR beside it takes either. Three primitives
	// over one table, three different argument rules; none of
	// them is a typo in the C.
	register("ERROR_NAME", func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if v.Type != TypeInteger {
			return nil, errf("Invalid argument type. (1)")
		}
		n := v.Num
		if n < 0 || int(n) >= len(errorNames) {
			return nil, f.Push(Str(""))
		}
		return nil, f.Push(Str(errorNames[n]))
	})
	register("ERROR_STR", errorText(errorDescriptions))
}

// popErrorFlag is what `IS_SET?`, `SET_ERROR`, `CLEAR_ERROR`,
// `ERROR_NAME` and `ERROR_STR` all take: a flag **name or number**.
// Emerald's five took only a number, so a program written the
// readable way — `"DIV_ZERO" is_set?` — aborted.
//
// A name nothing matches and a number out of range are both -1, and
// each caller decides what to do with that; upstream's own type
// refusal is "Invalid argument type. (1)" in every one of them.
func (f *Frame) popErrorFlag() (int, error) {
	v, err := f.Pop()
	if err != nil {
		return -1, err
	}
	switch v.Type {
	case TypeInteger:
		if v.Num >= 0 && int(v.Num) < len(errorNames) {
			return int(v.Num), nil
		}
		return -1, nil
	case TypeString:
		for i, n := range errorNames {
			if ascii.EqualFold(n, v.Str) {
				return i, nil
			}
		}
		return -1, nil
	}
	return -1, errf("Invalid argument type. (1)")
}

// errorText builds ERROR_STR, which answers the empty string for a
// flag it cannot resolve.
func errorText(table []string) primFunc {
	return func(f *Frame) (*Result, error) {
		n, err := f.popErrorFlag()
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, f.Push(Str(""))
		}
		return nil, f.Push(Str(table[n]))
	}
}
