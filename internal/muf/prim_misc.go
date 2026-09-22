package muf

import (
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/timefmt"
)

// String formatting, pattern matching, time and the remaining odds and ends.

func init() {
	register("TELL", func(f *Frame) (*Result, error) {
		msg, err := f.popStr()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		// TELL is "me @ swap notify": it speaks to whoever ran the
		// program.
		me := f.Vars[VarMe]
		for _, line := range strings.Split(msg, "\r") {
			h.Notify(me.Ref, line)
		}
		return nil, nil
	})

	register("NOTIFY_EXCLUDE", func(f *Frame) (*Result, error) {
		// "room obj1..objN count message notify_exclude"
		msg, err := f.popStr()
		if err != nil {
			return nil, err
		}
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, errf("NOTIFY_EXCLUDE needs a count that is not negative")
		}
		excluded, err := f.PopN(int(n))
		if err != nil {
			return nil, err
		}
		room, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		except := make([]ref.Ref, 0, len(excluded))
		for _, v := range excluded {
			if v.Type == TypeObject {
				except = append(except, v.Ref)
			}
		}
		for _, line := range strings.Split(msg, "\r") {
			h.NotifyExcept(room, except, line)
		}
		return nil, nil
	})

	register("STRCUT", func(f *Frame) (*Result, error) {
		at, err := f.popInt()
		if err != nil {
			return nil, err
		}
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		// STRCUT splits before the given one-based position.
		if at < 0 {
			at = 0
		}
		if at > int64(len(s)) {
			at = int64(len(s))
		}
		if err := f.Push(Str(s[:at])); err != nil {
			return nil, err
		}
		return nil, f.Push(Str(s[at:]))
	})

	register("STRNCMP", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		if v[0].Type != TypeString || v[1].Type != TypeString {
			return nil, errf("Non-string argument.")
		}
		return nil, f.Push(Int(int64(cStrcmp(
			truncate(v[0].Str, n), truncate(v[1].Str, n)))))
	})

	register("SUBST", func(f *Frame) (*Result, error) {
		// "string replacement pattern subst"
		pattern, err := f.popStr()
		if err != nil {
			return nil, err
		}
		with, err := f.popStr()
		if err != nil {
			return nil, err
		}
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		if pattern == "" {
			return nil, f.Push(Str(s))
		}
		return nil, f.Push(Str(strings.ReplaceAll(s, pattern, with)))
	})

	register("SMATCH", func(f *Frame) (*Result, error) {
		pattern, err := f.popStr()
		if err != nil {
			return nil, err
		}
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(ascii.SMatch(s, pattern)))
	})

	register("FMTSTRING", func(f *Frame) (*Result, error) {
		format, err := f.popStr()
		if err != nil {
			return nil, err
		}
		if format == "" {
			return nil, f.Push(Str(""))
		}
		out, err := formatWith(f.hostOrNil(), format, f.stackDialect())
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Str(out))
	})

	register("VERSION", func(f *Frame) (*Result, error) {
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Str(h.Version()))
	})
	register("CMD", func(f *Frame) (*Result, error) {
		// The command as typed, which the compiler stores in the fourth
		// reserved variable.
		return nil, f.Push(f.Vars[VarCommand])
	})

	register("TIME", func(f *Frame) (*Result, error) {
		now, err := f.now()
		if err != nil {
			return nil, err
		}
		h, m, s := now.Clock()
		for _, v := range []int{s, m, h} {
			if err := f.Push(Int(int64(v))); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	register("DATE", func(f *Frame) (*Result, error) {
		now, err := f.now()
		if err != nil {
			return nil, err
		}
		for _, v := range []int{now.Day(), int(now.Month()), now.Year()} {
			if err := f.Push(Int(int64(v))); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	register("SYSTIME", func(f *Frame) (*Result, error) {
		now, err := f.now()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Int(now.Unix()))
	})
	register("SYSTIME_PRECISE", func(f *Frame) (*Result, error) {
		now, err := f.now()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Float(float64(now.UnixNano()) / 1e9))
	})
	register("GMTOFFSET", func(f *Frame) (*Result, error) {
		now, err := f.now()
		if err != nil {
			return nil, err
		}
		_, offset := now.Zone()
		return nil, f.Push(Int(int64(offset)))
	})
	register("TIMESPLIT", func(f *Frame) (*Result, error) {
		secs, err := f.popInt()
		if err != nil {
			return nil, err
		}
		t := time.Unix(secs, 0).UTC()
		// Seconds, minutes, hours, day, month, year, day of week, day of
		// year: the order TIMESPLIT pushes them.
		vals := []int{
			t.Second(), t.Minute(), t.Hour(), t.Day(), int(t.Month()),
			t.Year(), int(t.Weekday()) + 1, t.YearDay(),
		}
		for _, v := range vals {
			if err := f.Push(Int(int64(v))); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	register("TIMEFMT", func(f *Frame) (*Result, error) {
		secs, err := f.popInt()
		if err != nil {
			return nil, err
		}
		format, err := f.popStr()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Str(timefmt.Format(format, time.Unix(secs, 0).UTC())))
	})

	register("RANDOM", func(f *Frame) (*Result, error) {
		// A full-width random integer, as upstream's RANDOM gives.
		return nil, f.Push(Int(int64(rand.Uint32())))
	})
	register("SRAND", func(f *Frame) (*Result, error) {
		// Unlike RANDOM, this draws from the frame's own seeded state, so a
		// program that records a seed with GETSEED replays the same run.
		if f.rndbuf == nil {
			f.rndbuf = newSeed()
		}
		// Upstream's result is a plain int, so the top bit is a sign bit.
		return nil, f.Push(Int(int64(int32(rndFrom(f.rndbuf)))))
	})

	register("AWAKE?", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Int(int64(h.Connections(obj))))
	})

	register("ABORT", func(f *Frame) (*Result, error) {
		msg, err := f.popStr()
		if err != nil {
			return nil, err
		}
		// ABORT raises an error the program's own TRY can catch.
		return nil, errf("%s", msg)
	})

	register("INTOSTR", func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		switch v.Type {
		case TypeInteger:
			return nil, f.Push(Str(strconv.FormatInt(v.Num, 10)))
		case TypeObject:
			// A dbref renders as a bare number, with no '#': upstream
			// prints the union's integer field either way.
			return nil, f.Push(Str(strconv.FormatInt(int64(v.Ref), 10)))
		case TypeFloat:
			out := strconv.FormatFloat(v.Float, 'g', 15, 64)
			if !strings.ContainsAny(out, ".ne") {
				out += ".0"
			}
			return nil, f.Push(Str(out))
		}
		return nil, errf("Invalid argument.")
	})
}

// now returns the server's clock.
func (f *Frame) now() (time.Time, error) {
	h, err := f.needHost()
	if err != nil {
		return time.Time{}, err
	}
	return h.Now(), nil
}

// truncate shortens a string to n bytes.
func truncate(s string, n int64) string {
	if n < 0 || n >= int64(len(s)) {
		return s
	}
	return s[:n]
}
