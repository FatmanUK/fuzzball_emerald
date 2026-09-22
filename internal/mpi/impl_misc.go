package mpi

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ansi"
	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/timefmt"
)

// The time, formatting and remaining odds and ends.
func init() {
	// Durations, rendered three ways: {timestr} as a clock, {stimestr} as
	// the single largest unit, {ltimestr} spelled out.
	register("TIMESTR", func(_ *Env, _ *Func, args []string) (string, error) {
		d, err := atoiArg("TIMESTR", args[0])
		if err != nil {
			return "", err
		}
		days, hours, mins, _ := splitDuration(d)
		if days > 0 {
			return fmt.Sprintf("%dd %02d:%02d", days, hours, mins), nil
		}
		return fmt.Sprintf("%02d:%02d", hours, mins), nil
	})
	register("STIMESTR", func(_ *Env, _ *Func, args []string) (string, error) {
		d, err := atoiArg("STIMESTR", args[0])
		if err != nil {
			return "", err
		}
		days, hours, mins, secs := splitDuration(d)
		switch {
		case days > 0:
			return itoa(days) + "d", nil
		case hours > 0:
			return itoa(hours) + "h", nil
		case mins > 0:
			return itoa(mins) + "m", nil
		}
		return itoa(secs) + "s", nil
	})
	// Unlike the other two, this counts in seven units rather than four, and
	// names only the ones that are non-zero — so a duration of nothing spells
	// out as nothing at all, not "0 seconds".
	register("LTIMESTR", func(_ *Env, _ *Func, args []string) (string, error) {
		d, err := atoiArg("LTIMESTR", args[0])
		if err != nil {
			return "", err
		}
		var parts []string
		for _, u := range longUnits {
			if d < u.secs {
				continue
			}
			n := d / u.secs
			d %= u.secs
			parts = append(parts, itoa(n)+" "+u.name+plural(n))
		}
		return strings.Join(parts, ", "), nil
	})

	register("CONVTIME", func(_ *Env, _ *Func, args []string) (string, error) {
		secs, ok := timefmt.Seconds(args[0], "%T%t%D")
		if !ok {
			return "", errf("CONVTIME", "Time string does not match expected format.")
		}
		return itoa(int(secs)), nil
	})

	register("FTIME", func(env *Env, _ *Func, args []string) (string, error) {
		when := env.Host.Now()
		if len(args) > 2 {
			n, err := atoiArg("FTIME", args[2])
			if err != nil {
				return "", err
			}
			when = int64(n)
		}
		if len(args) > 1 && args[1] != "" {
			off, err := atoiArg("FTIME", args[1])
			if err != nil {
				return "", err
			}
			// A small number is read as hours and a large one as seconds,
			// so both "{ftime:%H,-5}" and a raw offset work.
			if off < 25 && off > -25 {
				off *= 3600
			}
			when += int64(off)
		}
		return timefmt.Format(args[0], time.Unix(when, 0).UTC()), nil
	})
	register("TZOFFSET", func(*Env, *Func, []string) (string, error) {
		// Emerald reads and writes times in UTC throughout — see
		// internal/muf's own strptime for why — so there is no offset to
		// report.
		return "0", nil
	})

	register("TIMESUB", func(env *Env, _ *Func, args []string) (string, error) {
		period, err := atoiArg("TIMESUB", args[0])
		if err != nil {
			return "", err
		}
		offset, err := atoiArg("TIMESUB", args[1])
		if err != nil {
			return "", err
		}
		obj, err := env.resolve("TIMESUB", args, 3)
		if err != nil {
			return "", err
		}
		n := env.listCount(obj, args[2])
		if n == 0 {
			return "", errf("TIMESUB", "Failed list read.")
		}
		if period < 1 {
			return "", errf("TIMESUB", "Time period too short.")
		}
		// Which line of the list is showing depends on the clock, so a
		// property list becomes a slideshow that advances by itself.
		i := int(((env.Host.Now()+int64(offset))%int64(period))*int64(n)) / period
		return env.listItem(obj, args[2], i+1), nil
	})

	// Numbers and text.
	register("DICE", func(_ *Env, _ *Func, args []string) (string, error) {
		sides, err := atoiArg("DICE", args[0])
		if err != nil {
			return "", err
		}
		count := 1
		if len(args) > 1 {
			if count, err = atoiArg("DICE", args[1]); err != nil {
				return "", err
			}
		}
		offset := 0
		if len(args) > 2 {
			if offset, err = atoiArg("DICE", args[2]); err != nil {
				return "", err
			}
		}
		if count > 8888 {
			return "", errf("DICE", "Too many dice!")
		}
		if sides == 0 {
			return "0", nil
		}
		total := offset
		for ; count > 0; count-- {
			total += rand.Intn(abs(sides)) + 1
		}
		return itoa(total), nil
	})

	register("DIST", func(_ *Env, _ *Func, args []string) (string, error) {
		coords := make([]int, len(args))
		for i, a := range args {
			n, err := atoiArg("DIST", a)
			if err != nil {
				return "", err
			}
			coords[i] = n
		}
		// Two points in one, two or three dimensions, or one point taken
		// from the origin.
		var dx, dy, dz int
		switch len(coords) {
		case 2:
			dx, dy = coords[0], coords[1]
		case 3:
			dx, dy, dz = coords[0], coords[1], coords[2]
		case 4:
			dx, dy = coords[0]-coords[2], coords[1]-coords[3]
		case 6:
			dx, dy, dz = coords[0]-coords[3], coords[1]-coords[4], coords[2]-coords[5]
		default:
			return "", errf("DIST", "Takes 2,3,4, or 6 arguments.")
		}
		return itoa(isqrt(dx*dx + dy*dy + dz*dz)), nil
	})

	// {attr:tag,tag,...,text} — every argument but the last names an
	// attribute, and the text follows, always closed with a reset.
	register("ATTR", func(_ *Env, _ *Func, args []string) (string, error) {
		var b strings.Builder
		for _, tag := range args[:len(args)-1] {
			if tag == "" {
				continue
			}
			code, ok := ansi.Code(tag)
			if !ok {
				return "", errf("ATTR", "Unrecognized ansi tag.  Try one of "+ansi.TagList)
			}
			b.WriteString(code)
		}
		b.WriteString(args[len(args)-1])
		b.WriteString(ansi.Reset)
		return b.String(), nil
	})

	register("SMATCH", func(_ *Env, _ *Func, args []string) (string, error) {
		return boolOf(ascii.SMatch(args[0], args[1])), nil
	})
	register("XOR", func(_ *Env, _ *Func, args []string) (string, error) {
		return boolOf(truthy(args[0]) != truthy(args[1])), nil
	})

	// {escape} wraps text in backticks so the parser reads it literally,
	// escaping any backtick or backslash already in it.
	register("ESCAPE", func(_ *Env, _ *Func, args []string) (string, error) {
		var b strings.Builder
		b.WriteByte(litChar)
		for i := 0; i < len(args[0]); i++ {
			if c := args[0][i]; c == escape || c == litChar {
				b.WriteByte(escape)
			}
			b.WriteByte(args[0][i])
		}
		b.WriteByte(litChar)
		return b.String(), nil
	})

	register("COMMAS", func(env *Env, _ *Func, args []string) (string, error) {
		if len(args) == 3 {
			return "", errf("COMMAS", "Takes 1, 2, or 4 arguments.")
		}
		list, err := Parse(env, args[0])
		if err != nil {
			return "", err
		}
		items := splitLines(list)
		if len(items) == 0 {
			return "", nil
		}
		last := " and "
		if len(args) > 1 {
			if last, err = Parse(env, args[1]); err != nil {
				return "", err
			}
		}
		// The four-argument form names a variable and a body, so each item
		// can be rendered before being joined.
		if len(args) > 3 {
			name, err := Parse(env, args[2])
			if err != nil {
				return "", err
			}
			if err := env.SetVar(name, ""); err != nil {
				return "", err
			}
			defer env.PopVar()
			for i, item := range items {
				if err := env.SetVar(name, item); err != nil {
					return "", err
				}
				if items[i], err = Parse(env, args[3]); err != nil {
					return "", err
				}
			}
		}
		if len(items) == 1 {
			return items[0], nil
		}
		return strings.Join(items[:len(items)-1], ", ") + last + items[len(items)-1], nil
	})

	register("V", func(env *Env, _ *Func, args []string) (string, error) {
		v, ok := env.Var(args[0])
		if !ok {
			return "", errf("V", "No such variable defined.")
		}
		return v, nil
	})

	// {default} returns the first of its two arguments that is true, which
	// is why neither is pre-evaluated: the second must not run if the first
	// answers.
	register("DEFAULT", func(env *Env, _ *Func, args []string) (string, error) {
		first, err := Parse(env, args[0])
		if err != nil {
			return "", err
		}
		if truthy(first) {
			return first, nil
		}
		return Parse(env, args[1])
	})

	register("OTELL", func(env *Env, _ *Func, args []string) (string, error) {
		room := env.Host.Location(env.Who)
		if len(args) > 1 {
			var err error
			if room, err = env.resolve("OTELL", args, 1); err != nil {
				return "", err
			}
		}
		except := env.Who
		if len(args) > 2 {
			except = env.lookup(args[2])
		}
		for _, line := range strings.Split(args[0], "\r") {
			env.Host.NotifyExcept(room, []Ref{except}, line)
		}
		return "", nil
	})

	// {revoke} evaluates its argument without whatever blessing the message
	// carries, so a property can run text it does not trust.
	register("REVOKE", func(env *Env, _ *Func, args []string) (string, error) {
		sub := *env
		sub.Blessed = false
		return Parse(&sub, args[0])
	})

	// {debug} and {debugif} evaluate their argument with upstream's MPI
	// tracer on, printing every call and its result to the player. Emerald
	// has no tracer, so these evaluate plainly — the text they produce is
	// the same, only the diagnostics are missing.
	register("DEBUG", func(env *Env, _ *Func, args []string) (string, error) {
		return Parse(env, args[0])
	})
	register("DEBUGIF", func(env *Env, _ *Func, args []string) (string, error) {
		if _, err := Parse(env, args[0]); err != nil {
			return "", err
		}
		return Parse(env, args[1])
	})

	register("TIMING", func(env *Env, _ *Func, args []string) (string, error) {
		start := time.Now()
		out, err := Parse(env, args[0])
		if err != nil {
			return "", err
		}
		env.Host.Notify(env.Who,
			fmt.Sprintf("Time elapsed: %.6f seconds", time.Since(start).Seconds()))
		return out, nil
	})

	// The three that act on the world rather than describing it. Each is
	// gated, because a property anyone can write must not be able to run
	// commands as its reader.
	register("FORCE", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("FORCE", args, 0)
		if err != nil {
			return "", errf("FORCE", "Failed match. (arg1)")
		}
		switch env.Host.TypeName(obj) {
		case "Thing", "Player":
		default:
			return "", errf("FORCE", "Bad object reference. (arg1)")
		}
		if args[1] == "" {
			return "", errf("FORCE", "Null command string. (arg2)")
		}
		if !env.Blessed {
			return "", errf("FORCE", "Permission Denied.")
		}
		env.Host.Force(env.Descr, obj, args[1])
		return "", nil
	})

	register("KILL", func(env *Env, _ *Func, args []string) (string, error) {
		pid, err := atoiArg("KILL", args[0])
		if err != nil {
			return "", err
		}
		if pid < 0 {
			return "", errf("KILL", "Invalid process ID.")
		}
		if !env.Blessed {
			return "", errf("KILL", "Permission denied.")
		}
		return boolOf(env.Host.Kill(pid)), nil
	})

	register("MUF", func(env *Env, _ *Func, args []string) (string, error) {
		prog := env.lookup(args[0])
		if !env.Host.Valid(prog) || env.Host.TypeName(prog) != "Program" {
			return "", errf("MUF", "Bad program reference.")
		}
		if !env.Host.HasFlag(prog, "link_ok") &&
			!env.Host.Controls(env.Host.Owner(env.Perms), prog) {
			return "", errf("MUF", "Permission denied.")
		}
		if env.depth > mufCallLimit {
			return "", errf("MUF", "Too many call levels.")
		}
		out, err := env.Host.RunMUF(env.Descr, env.Who, prog, args[1])
		if err != nil {
			return "", errf("MUF", "%s", err.Error())
		}
		return out, nil
	})

	register("DELAY", func(env *Env, _ *Func, args []string) (string, error) {
		secs, err := atoiArg("DELAY", args[0])
		if err != nil {
			return "", err
		}
		if secs < 1 {
			secs = 1
		}
		if secs > 31622400 {
			return "", errf("DELAY", "Delaying more than a year in MPI is just silly.")
		}
		env.Host.Delay(env.Descr, env.Who, env.What, env.Perms, secs, args[1], env.Blessed)
		return "", nil
	})
}

// mufCallLimit is upstream's mpi_muf_call_levels bound.
const mufCallLimit = 18

// longUnits is timestr_long's own scale. A year is 365.24 days and a month a
// twelfth of that, neither of which is a calendar month — this is arithmetic
// on a duration, not on a date.
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

// splitDuration breaks a count of seconds into days, hours, minutes and
// seconds.
func splitDuration(d int) (days, hours, mins, secs int) {
	if d < 0 {
		d = 0
	}
	return d / 86400, d % 86400 / 3600, d % 3600 / 60, d % 60
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// isqrt is an integer square root, which is what {dist} reports: upstream
// computes the distance as a double and prints it with "%d".
func isqrt(n int) int {
	if n <= 0 {
		return 0
	}
	x := n
	y := (x + 1) / 2
	for y < x {
		x = y
		y = (x + n/x) / 2
	}
	return x
}
