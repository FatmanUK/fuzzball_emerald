package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ascii"

// The arithmetic error flags, which a program inspects rather than being
// aborted by.

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
	register("ERROR_NAME", errorText(errorNames))
	register("ERROR_STR", errorText(errorDescriptions))
}

// errorText builds ERROR_NAME and ERROR_STR, which look a flag up by number.
func errorText(table []string) primFunc {
	return func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		if n < 0 || int(n) >= len(table) {
			return nil, f.Push(Str(""))
		}
		return nil, f.Push(Str(table[n]))
	}
}
