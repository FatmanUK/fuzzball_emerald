package timefmt

import (
	"strconv"
	"time"
)

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

// Format1 is time_format_1 (`fbtime.c:153`), WHO's "On For" column:
// "HH:MM", or "Nd HH:MM" once the duration passes a day.
//
// Upstream runs the *delta* through `gmtime`, so the day count is
// `tm_yday` — which **wraps at a year**, because gmtime rolls a
// delta of 400 days into 1971 and starts counting days again. That is
// reproduced rather than corrected: a connection up for longer than a
// year reads as a short one on both servers.
func Format1(seconds int64) string {
	days, hh, mm, _ := gmtimeDelta(seconds)
	if days > 0 {
		return strconv.Itoa(days) + "d " +
			pad2(hh) + ":" + pad2(mm)
	}
	return pad2(hh) + ":" + pad2(mm)
}

// Format2 is time_format_2 (`fbtime.c:185`), WHO's "Idle" column: the
// largest non-zero unit of the same gmtime breakdown, with a
// one-letter suffix. A duration under a minute reads in seconds, so a
// player who has just typed something shows "0s" rather than nothing.
func Format2(seconds int64) string {
	days, hh, mm, ss := gmtimeDelta(seconds)
	switch {
	case days > 0:
		return strconv.Itoa(days) + "d"
	case hh > 0:
		return strconv.Itoa(hh) + "h"
	case mm > 0:
		return strconv.Itoa(mm) + "m"
	}
	return strconv.Itoa(ss) + "s"
}

// gmtimeDelta is `gmtime` on a duration, which is what both formats
// above are built on: the year-day, hour, minute and second of the
// instant that many seconds after the epoch.
func gmtimeDelta(seconds int64) (days, hh, mm, ss int) {
	if seconds < 0 {
		seconds = 0
	}
	t := time.Unix(seconds, 0).UTC()
	// tm_yday is zero-based where Go's YearDay is one-based.
	return t.YearDay() - 1, t.Hour(), t.Minute(), t.Second()
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
