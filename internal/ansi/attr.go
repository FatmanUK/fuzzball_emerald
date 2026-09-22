// Package ansi holds the terminal attribute tags MUF's TEXTATTR and MPI's
// {attr} both accept, and the escape sequences they stand for.
package ansi

import "github.com/FatmanUK/fuzzball_emerald/internal/ascii"

// Reset ends every run of attributes, so text after one is unaffected.
const Reset = "\x1b[0m"

// codes is the tag table from fuzzball/include/color.h. Several tags have
// two spellings, both of which upstream accepts.
var codes = map[string]string{
	"reset": Reset, "normal": Reset,
	"bold": "\x1b[1m", "dim": "\x1b[2m", "italic": "\x1b[3m",
	"uline": "\x1b[4m", "underline": "\x1b[4m",
	"flash": "\x1b[5m", "reverse": "\x1b[7m",
	"ostrike": "\x1b[9m", "overstrike": "\x1b[9m",
	"black": "\x1b[30m", "red": "\x1b[31m", "green": "\x1b[32m",
	"yellow": "\x1b[33m", "blue": "\x1b[34m", "magenta": "\x1b[35m",
	"cyan": "\x1b[36m", "white": "\x1b[37m",
	"bg_black": "\x1b[40m", "bg_red": "\x1b[41m", "bg_green": "\x1b[42m",
	"bg_yellow": "\x1b[43m", "bg_blue": "\x1b[44m", "bg_magenta": "\x1b[45m",
	"bg_cyan": "\x1b[46m", "bg_white": "\x1b[47m",
}

// Code resolves one tag to its escape sequence.
func Code(tag string) (string, bool) {
	c, ok := codes[ascii.Fold(tag)]
	return c, ok
}

// TagList is the wording both callers use when a tag is not recognised. It
// is upstream's own, listing the tags rather than naming the bad one.
const TagList = "reset, bold, dim, italic, underline, flash, reverse, " +
	"overstrike, black, red, green, yellow, blue, magenta, cyan, white, " +
	"bg_black, bg_red, bg_green, bg_yellow, bg_blue, bg_magenta, " +
	"bg_cyan, or bg_white."
