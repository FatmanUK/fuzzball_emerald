package game

import (
	"fmt"
	"strconv"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
)

// sprintf formats a message, treating a format string with no
// arguments as a literal so a stray '%' in player text cannot corrupt
// output.
func sprintf(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

func itoa(n int) string { return strconv.Itoa(n) }

// ascEqual is a shorthand for case-insensitive comparison.
func ascEqual(a, b string) bool { return ascii.EqualFold(a, b) }
