package timefmt

import "strconv"

// longUnits is timestr_long's own scale (fbtime.c:318). A year is
// 365.24 days and a month a twelfth of that, neither of which is a
// calendar month — this is arithmetic on a duration, not on a date.
var longUnits = [7]struct {
	name string
	secs int
}{
	{"year", 31556736},
	{"month", 2621376},
	{"week", 604800},
	{"day", 86400},
	{"hour", 3600},
	{"minute", 60},
	{"second", 1},
}

// Long is timestr_long: a duration in seconds spelled out, naming
// only the units that are non-zero.
//
// So a duration of nothing spells out as nothing at all rather than
// "0 seconds", which reads oddly in "Up since ..." and is what
// upstream prints.
//
// It is here rather than in the two callers because both are ports of
// the same C function: MPI's {ltimestr} and the uptime command.
func Long(seconds int) string {
	out := ""
	for _, u := range longUnits {
		if seconds < u.secs {
			continue
		}
		n := seconds / u.secs
		seconds %= u.secs
		if out != "" {
			out += ", "
		}
		out += strconv.Itoa(n) + " " + u.name
		if n != 1 {
			out += "s"
		}
	}
	return out
}
