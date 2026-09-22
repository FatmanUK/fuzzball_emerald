package timefmt

import (
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
)

// strptime parses a time string against a C-style format, which is what
// CONVTIME and FMTTIME both do — the second under whatever format the caller
// supplies, the first under a fixed one.
//
// The directives covered are the ones strftime emits, which is what a MUF
// program has any way of producing in the first place. Anything else in the
// format fails the parse rather than being skipped, so a program gets the
// same "does not match" answer it would from strptime returning NULL.
//
// The result is read in UTC, as strftime's own rendering is. Upstream's mktime
// reads it in the server's local zone instead; a server and its MUF programs
// that agree on one zone are unaffected, and Emerald has no per-world zone to
// agree on.
func Parse(value, format string) (time.Time, bool) {
	p := &timeParser{s: value}
	year, mon, day := 1900, 1, 1
	hour, min, sec := 0, 0, 0
	pm, hasPM := false, false
	format = expandCompound(format)

	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' {
			// Whitespace in a format matches any run of it, including none.
			if isSpaceByte(c) {
				p.skipSpace()
				continue
			}
			if !p.literal(c) {
				return time.Time{}, false
			}
			continue
		}
		i++
		if i >= len(format) {
			return time.Time{}, false
		}
		ok := true
		switch format[i] {
		case '%':
			ok = p.literal('%')
		case 'n', 't':
			p.skipSpace()
		case 'Y':
			year, ok = p.number(4)
		case 'y':
			year, ok = p.number(2)
			if ok {
				// strptime's own window: 69-99 is the 1900s, 0-68 the 2000s.
				if year >= 69 {
					year += 1900
				} else {
					year += 2000
				}
			}
		case 'm':
			mon, ok = p.number(2)
		case 'd', 'e':
			p.skipSpace()
			day, ok = p.number(2)
		case 'H', 'I':
			hour, ok = p.number(2)
		case 'M':
			min, ok = p.number(2)
		case 'S':
			sec, ok = p.number(2)
		case 'j':
			// A day of the year counts from January, which time.Date
			// normalises past the end of the month for us.
			mon = 1
			day, ok = p.number(3)
		case 'b', 'h', 'B':
			mon, ok = p.month()
		case 'a', 'A':
			ok = p.weekday()
		case 'p':
			pm, ok = p.meridiem()
			hasPM = ok
		default:
			ok = false
		}
		if !ok {
			return time.Time{}, false
		}
	}

	if hasPM {
		// %I counts 1-12, so noon and midnight each need moving.
		hour %= 12
		if pm {
			hour += 12
		}
	}
	return time.Date(year, time.Month(mon), day, hour, min, sec, 0, time.UTC), true
}

// expandCompound rewrites the directives that stand for a fixed sequence of
// simpler ones, so the parse itself is a single pass.
func expandCompound(format string) string {
	for _, sub := range [][2]string{
		{"%D", "%m/%d/%y"},
		{"%T", "%H:%M:%S"},
		{"%F", "%Y-%m-%d"},
		{"%R", "%H:%M"},
	} {
		format = strings.ReplaceAll(format, sub[0], sub[1])
	}
	return format
}

// timeParser walks the input string a directive at a time.
type timeParser struct {
	s string
	i int
}

func (p *timeParser) skipSpace() {
	for p.i < len(p.s) && isSpaceByte(p.s[p.i]) {
		p.i++
	}
}

func (p *timeParser) literal(c byte) bool {
	if p.i >= len(p.s) || p.s[p.i] != c {
		return false
	}
	p.i++
	return true
}

// number reads up to max digits, which is what strptime does: a field is as
// wide as the digits actually present, not padded to its nominal width.
func (p *timeParser) number(max int) (int, bool) {
	start := p.i
	n := 0
	for p.i < len(p.s) && p.i-start < max && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		n = n*10 + int(p.s[p.i]-'0')
		p.i++
	}
	if p.i == start {
		return 0, false
	}
	return n, true
}

func (p *timeParser) month() (int, bool) {
	for i, name := range monthNames {
		if p.word(name) {
			return i + 1, true
		}
	}
	return 0, false
}

func (p *timeParser) weekday() bool {
	for _, name := range dayNames {
		if p.word(name) {
			return true
		}
	}
	return false
}

func (p *timeParser) meridiem() (bool, bool) {
	switch {
	case p.word("PM"):
		return true, true
	case p.word("AM"):
		return false, true
	}
	return false, false
}

// word matches a name, full form first and then its three-letter
// abbreviation, comparing the way the rest of the server does: ASCII only.
func (p *timeParser) word(name string) bool {
	rest := p.s[p.i:]
	forms := [2]string{name, name}
	if len(name) > 3 {
		forms[1] = name[:3]
	}
	for _, form := range forms {
		if len(rest) >= len(form) && ascii.EqualFold(rest[:len(form)], form) {
			p.i += len(form)
			return true
		}
	}
	return false
}

var monthNames = [12]string{
	"January", "February", "March", "April", "May", "June",
	"July", "August", "September", "October", "November", "December",
}

var dayNames = [7]string{
	"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday",
	"Saturday",
}

// fmtTimeSeconds is upstream's time_string_to_seconds, which FMTTIME exposes
// directly and CONVTIME calls with a fixed format.
//
// The "%T%t%D" special case is upstream's own: %D's year is two digits, so a
// string carrying four would read the century as the whole year. Upstream
// looks ahead for how many digits the year actually has and swaps in an
// equivalent format with %Y when there are four.
func Seconds(value, format string) (int64, bool) {
	if format == "%T%t%D" && yearDigits(value) == 4 {
		format = "%T%t%m/%d/%Y"
	}
	t, ok := Parse(value, format)
	if !ok {
		return 0, false
	}
	return t.Unix(), true
}

// yearDigits counts the digits in the third slash-separated field of the date
// half of a "%T%t%D" string, upstream's own hand-rolled lookahead.
func yearDigits(s string) int {
	i := strings.IndexAny(s, " \t\n\r\v\f")
	if i < 0 {
		return 0
	}
	rest := strings.TrimLeft(s[i:], " \t\n\r\v\f")
	// Step over the month and the day, each ended by a '/'.
	for n := 0; n < 2; n++ {
		j := strings.IndexByte(rest, '/')
		if j < 0 {
			return 0
		}
		rest = rest[j+1:]
	}
	n := 0
	for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
		n++
	}
	return n
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'
}
