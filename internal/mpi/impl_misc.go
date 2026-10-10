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
	// Durations, rendered three ways: {timestr} as a clock,
	// {stimestr} as the single largest unit, {ltimestr} spelled
	// out.
	register("TIMESTR", func(_ *Env, _ *Func, args []string) (string, error) {
		d := atoiArg(args[0])
		days, hours, mins, _ := splitDuration(d)
		if days > 0 {
			return fmt.Sprintf("%dd %02d:%02d", days, hours, mins), nil
		}
		return fmt.Sprintf("%02d:%02d", hours, mins), nil
	})
	register("STIMESTR", func(_ *Env, _ *Func, args []string) (string, error) {
		d := atoiArg(args[0])
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
	// Unlike the other two, this counts in seven units rather
	// than four, and names only the ones that are non-zero — so
	// a duration of nothing spells out as nothing at all, not "0
	// seconds".
	register("LTIMESTR", func(_ *Env, _ *Func, args []string) (string, error) {
		d := atoiArg(args[0])
		return timefmt.Long(d), nil
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
			n := atoiArg(args[2])
			when = int64(n)
		}
		if len(args) > 1 && args[1] != "" {
			off := atoiArg(args[1])
			// A small number is read as hours and a large
			// one as seconds, so both "{ftime:%H,-5}" and
			// a raw offset work.
			if off < 25 && off > -25 {
				off *= 3600
			}
			when += int64(off)
		}
		return timefmt.Format(args[0], time.Unix(when, 0).UTC()), nil
	})
	register("TZOFFSET", func(*Env, *Func, []string) (string, error) {
		// Emerald reads and writes times in UTC throughout
		// — see internal/muf's own strptime for why — so
		// there is no offset to report.
		return "0", nil
	})

	register("TIMESUB", func(env *Env, _ *Func, args []string) (string, error) {
		period := atoiArg(args[0])
		offset := atoiArg(args[1])
		obj, err := env.resolve("TIMESUB", args, 3)
		if err != nil {
			return "", err
		}
		n, err := env.listCount("TIMESUB", obj, args[2])
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "", errf("TIMESUB", "Failed list read.")
		}
		if period < 1 {
			return "", errf("TIMESUB", "Time period too short.")
		}
		// Which line of the list is showing depends on the
		// clock, so a property list becomes a slideshow that
		// advances by itself.
		i := int(((env.Host.Now()+int64(offset))%int64(period))*int64(n)) / period
		return env.listItem("TIMESUB", obj, args[2], i+1)
	})

	// Numbers and text.
	register("DICE", func(_ *Env, _ *Func, args []string) (string, error) {
		sides := atoiArg(args[0])
		count := 1
		if len(args) > 1 {
			count = atoiArg(args[1])
		}
		offset := 0
		if len(args) > 2 {
			offset = atoiArg(args[2])
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
			n := atoiArg(a)
			coords[i] = n
		}
		// Two points in one, two or three dimensions, or one
		// point taken from the origin.
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

	// {attr:tag,tag,...,text} — every argument but the last
	// names an attribute, and the text follows, always closed
	// with a reset.
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

	// {escape} wraps text in backticks so the parser reads it
	// literally, escaping any backtick or backslash already in
	// it.
	register("ESCAPE", func(_ *Env, _ *Func, args []string) (string, error) {
		var b strings.Builder
		b.WriteByte(litChar)
		for i := 0; i < len(args[0]); i++ {
			if c := args[0][i]; c == escape ||
				c == litChar {
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
		// The four-argument form names a variable and a body,
		// so each item can be rendered before being joined.
		if len(args) > 3 {
			name, err := Parse(env, args[2])
			if err != nil {
				return "", err
			}
			if err := env.BindVar("COMMAS", name,
				""); err != nil {
				return "", err
			}
			defer env.PopVar()
			for i, item := range items {
				env.AssignVar(name, item)
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

	// {default} returns the first of its two arguments that is
	// true, which is why neither is pre-evaluated: the second
	// must not run if the first answers.
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
		// The room is local -- a message cannot broadcast
		// into somewhere it is not -- and the excluded object
		// is raw, because naming somebody to leave out tells
		// you nothing about them. The listener gate runs
		// after the match, which is mfn_otell's order.
		room := env.Host.Location(env.Who)
		if len(args) > 1 {
			var err error
			room, err = env.resolveLocal("OTELL", args, 1)
			if err != nil {
				return "", err
			}
		}
		if env.Type.Has(Listener) &&
			env.Host.TypeName(env.What) != "Room" {
			return "", errf("OTELL", "Permission denied.")
		}
		except := env.Who
		if len(args) > 2 {
			// The third argument **replaces** the default
			// whether or not it resolves, which is what
			// makes "#-1" the documented way to exclude
			// nobody: upstream assigns mesg_dbref_raw's
			// answer and then tests `thing != eobj`, so a
			// sentinel excludes nothing. This kept the
			// player excluded on a failed match, and so
			// swallowed the whole broadcast.
			except, _ = env.resolveAs(matchRaw, args, 2)
		}
		named := ""
		if env.otellNames(room, args[0]) {
			named = env.speakerPrefix(args[0])
		}
		// `all` is false: upstream's {otell} sends only the
		// first line, and splitLinesCR says why.
		for _, line := range splitLinesCR(args[0], false) {
			env.Host.NotifyExcept(env.Who, room,
				[]Ref{except}, named+line)
		}
		// mfn_otell returns its message, as mfn_tell does.
		return args[0], nil
	})

	// {revoke} evaluates its argument without whatever blessing
	// the message carries, so a property can run text it does not
	// trust.
	register("REVOKE", func(env *Env, _ *Func, args []string) (string, error) {
		sub := *env
		sub.Blessed = false
		return Parse(&sub, args[0])
	})

	// {debug} and {debugif} evaluate their argument with
	// upstream's MPI tracer on, printing every call and its
	// result to the player. Emerald has no tracer, so these
	// evaluate plainly — the text they produce is the same,
	// only the diagnostics are missing.
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

	// The three that act on the world rather than describing it.
	// Each is gated, because a property anyone can write must not
	// be able to run commands as its reader.
	register("FORCE", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolveMsg(matchRaw, "FORCE", args, 0,
			"Failed match. (arg1)",
			"Permission denied. (arg1)")
		if err != nil {
			return "", err
		}
		switch env.Host.TypeName(obj) {
		case "Thing", "Player":
		default:
			return "", errf("FORCE", "Bad object reference. (arg1)")
		}
		if args[1] == "" {
			return "", errf("FORCE",
				"Null command string. (arg2)")
		}
		if err := forceAllowed(env, obj); err != nil {
			return "", err
		}
		// The command string may be a **list**, split on
		// carriage returns, and each one is forced in turn.
		for _, cmd := range strings.Split(args[1], "\r") {
			// Repeated per command, and not gated on
			// blessing like the checks above: upstream
			// re-tests the name inside the loop for
			// anything that is not a player, so a blessed
			// {force} is refused here too. Its message
			// carries a "[2]" to tell the two apart.
			name := firstWord(env.Host.Name(obj))
			if env.Host.TypeName(obj) != "Player" &&
				env.Host.PlayerNamed(name) {
				return "", errf("FORCE",
					"Cannot force a thing named "+
						"after a player. [2]")
			}
			if cmd != "" {
				env.Host.Force(env.Descr, obj, cmd)
			}
		}
		return "", nil
	})

	register("KILL", func(env *Env, _ *Func, args []string) (string, error) {
		pid := atoiArg(args[0])
		if pid < 0 {
			return "", errf("KILL", "Invalid process ID.")
		}
		if !env.Blessed {
			return "", errf("KILL", "Permission denied.")
		}
		return boolOf(env.Host.Kill(pid)), nil
	})

	register("MUF", func(env *Env, _ *Func, args []string) (string, error) {
		// A failed match and a non-program are two different
		// messages, which this collapsed into one.
		prog, fail := env.resolveAs(matchRaw, args, 0)
		if fail != resolveOK {
			return "", errf("MUF", "Match failed.")
		}
		if env.Host.TypeName(prog) != "Program" {
			return "", errf("MUF", "Bad program reference.")
		}
		if !env.Host.HasFlag(prog, "link_ok") &&
			!env.Host.Controls(env.Host.Owner(env.Perms), prog) {
			return "", errf("MUF", "Permission denied.")
		}
		// A listener or a lock may only run a program at
		// mucker 3 or above (`mfuns2.c:2671`), which had no
		// port: a mortal's `_listen` could run a mucker-1
		// program through MPI.
		if env.Type.Has(Listener) || env.Type.Has(Lock) {
			if env.Host.MLevel(prog) < 3 {
				return "", errf("MUF",
					"Permission denied.")
			}
		}
		if env.depth > mufCallLimit {
			return "", errf("MUF", "Too many call levels.")
		}
		how, _ := env.Var("how")
		out, err := env.Host.RunMUF(env.Descr, env.Who, prog,
			env.Perms, how, args[1])
		if err != nil {
			return "", errf("MUF", "%s", err.Error())
		}
		return out, nil
	})

	register("DELAY", func(env *Env, _ *Func, args []string) (string, error) {
		secs := atoiArg(args[0])
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

// splitDuration breaks a count of seconds into days, hours, minutes
// and seconds.
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

// isqrt is an integer square root, which is what {dist} reports:
// upstream computes the distance as a double and prints it with "%d".
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

// firstWord is the leading run of non-space characters, which is how
// upstream reads a puppet's name when deciding whether it is named
// after a player: NAME(obj) is copied until the first isspace.
func firstWord(s string) string {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\r', '\n', '\v', '\f':
			return s[:i]
		}
	}
	return s
}

// forceAllowed is mfn_force's permission ladder (mfuns2.c:2770), and
// most of it is the **unblessed path this server did not have**.
//
// Emerald refused every unblessed {force} with "Permission Denied."
// Upstream refuses only when `allow_zombies` is off; with it on, an
// unblessed force proceeds and is then subject to six refusals of its
// own, four of which apply to a THING alone. A world that allows
// zombies therefore has a whole mechanism — puppets forced from
// their own descriptions — that could not run here at all.
//
// The order is upstream's and is observable, because the messages
// differ. Note that two of them are "Permission denied." with a
// lower-case d where the allow_zombies refusal above has a capital
// one; that is upstream's inconsistency, not a transcription slip.
func forceAllowed(env *Env, obj Ref) error {
	blessed := env.Blessed
	if !env.tuneBool("allow_zombies") && !blessed {
		return errf("FORCE", "Permission Denied.")
	}

	if !blessed {
		if env.Host.TypeName(obj) == "Thing" {
			if env.Host.HasFlag(obj, "dark") {
				return errf("FORCE",
					"Cannot force a dark puppet.")
			}
			owner := env.Host.Owner(obj)
			if env.Host.HasFlag(owner, "zombie") {
				return errf("FORCE",
					"Permission denied.")
			}
			// A no-puppets room, which is ZOMBIE on the
			// *room* rather than on the thing.
			loc := env.Host.Location(obj)
			if env.Host.Valid(loc) &&
				env.Host.HasFlag(loc, "zombie") &&
				env.Host.TypeName(loc) == "Room" {
				return errf("FORCE",
					"Cannot force a Puppet in a "+
						"no-puppets room.")
			}
			first := firstWord(env.Host.Name(obj))
			if env.Host.PlayerNamed(first) {
				return errf("FORCE",
					"Cannot force a thing named "+
						"after a player.")
			}
		}
		if !env.Host.HasFlag(obj, "xforcible") {
			return errf("FORCE", "Permission denied: "+
				"forced object not @set Xforcible.")
		}
		// The force lock, which nothing in this server
		// evaluated before: an unset lock is **false**, so a
		// thing has to be force-locked to the trigger for an
		// unblessed force to reach it at all.
		if !env.Host.ForceLockPasses(env.Descr, env.Perms,
			obj) {
			return errf("FORCE", "Permission denied: "+
				"Object not force-locked to trigger.")
		}
	}

	// These two are outside the blessed test, so they refuse a
	// blessed {force} as readily as any other.
	if obj == 1 {
		return errf("FORCE",
			"Permission denied: You can't force God.")
	}
	if env.Host.ForceLevel() > env.tuneInt("max_force_level")-1 {
		return errf("FORCE", "Permission denied: "+
			"You can't force recursively.")
	}
	return nil
}

// tuneBool and tuneInt read an @tune parameter through the host's
// string accessor, which is the only shape it offers.
//
// The boolean reads its **first character**, which is what upstream's
// own tune_setparm does (`y`, `Y` or `1` is true) and what a listing
// emits — "yes". Comparing the whole word would tie this to the
// listing's exact spelling for no gain.
func (env *Env) tuneBool(name string) bool {
	v, ok := env.Host.TuneGet(name)
	if !ok || v == "" {
		return false
	}
	return v[0] == 'y' || v[0] == 'Y' || v[0] == '1'
}

func (env *Env) tuneInt(name string) int {
	v, _ := env.Host.TuneGet(name)
	return atoiArg(v)
}
