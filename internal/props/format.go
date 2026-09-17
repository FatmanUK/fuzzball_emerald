package props

import (
	"math"
	"strconv"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// ftoa renders a float the way Fuzzball's "%g" does, including its
// non-standard spellings of the infinities.
func ftoa(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "Inf"
	case math.IsInf(f, -1):
		return "-Inf"
	case math.IsNaN(f):
		return "NaN"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}
