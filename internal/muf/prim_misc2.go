package muf

import (
	"strconv"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// EVENT_COUNT, EVENT_EXISTS, EXT-NAME-OK?, READ_WANTS_BLANKS,
// READ_WANTS_NO_BLANKS, IGNORING?, IGNORE_ADD, IGNORE_DEL, CONVTIME, STATS,
// STATS_ARRAY and USERLOG are ports of the more tractable primitives left
// in src/p_misc.c. Deliberately not ported this phase, each needing
// substantially more than a primitive port on its own: TIMER_START/
// TIMER_STOP/EVENT_SEND (a delayed, out-of-band event-delivery scheduler
// distinct from WATCHPID's own synchronous one), FMTTIME (upstream calls
// strptime with an arbitrary caller-supplied format string — Go has no
// equivalent), DEBUGGER_BREAK/DEBUG_LINE/DEBUG_ON/DEBUG_OFF (the MUF
// single-step debugger, unimplemented entirely), GETSEED/SETSEED (a
// per-frame seeded RNG SRAND does not actually have here either), and
// SMTP_SEND (a real SMTP client).
func init() {
	register("EVENT_COUNT", func(f *Frame) (*Result, error) {
		return nil, f.Push(Int(int64(len(f.PendingEvents))))
	})
	register("EVENT_EXISTS", func(f *Frame) (*Result, error) {
		name, err := f.popStr()
		if err != nil {
			return nil, err
		}
		if name == "" {
			return nil, errf("Expected a non-null string eventid to search for.")
		}
		n := 0
		for _, ev := range f.PendingEvents {
			if ev.Name == name {
				n++
			}
		}
		return nil, f.Push(Int(int64(n)))
	})

	register("EXT-NAME-OK?", func(f *Frame) (*Result, error) {
		typeV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		nameV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if nameV.Type != TypeString {
			return nil, errf("Object name string expected (1).")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}

		var t ref.ObjType
		switch typeV.Type {
		case TypeObject:
			if !h.Valid(typeV.Ref) {
				return nil, errf("Invalid argument (2).")
			}
			t = h.ObjType(typeV.Ref)
		case TypeString:
			var ok bool
			t, ok = objTypeFromTag(typeV.Str)
			if !ok {
				return nil, errf("String must be a valid object type (2).")
			}
		default:
			return nil, errf("Dbref or object type name expected (2).")
		}
		return nil, f.Push(Bool(h.NameOK(nameV.Str, t)))
	})

	register("READ_WANTS_BLANKS", func(f *Frame) (*Result, error) {
		f.WantsBlanks = true
		return nil, nil
	})
	register("READ_WANTS_NO_BLANKS", func(f *Frame) (*Result, error) {
		f.WantsBlanks = false
		return nil, nil
	})

	register("IGNORING?", func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Permission Denied.")
		}
		who, err := f.popRef()
		if err != nil {
			return nil, err
		}
		player, err := f.popRef()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		// Checked in upstream's own order — oper1 (who) before oper2
		// (player) — even though the resulting argument numbers are the
		// other way around.
		if !h.Valid(who) {
			return nil, errf("Invalid object. (2)")
		}
		if !h.Valid(player) {
			return nil, errf("Invalid object. (1)")
		}
		return nil, f.Push(Bool(h.IsIgnoring(player, who)))
	})
	register("IGNORE_ADD", ignoreEdit((Host).IgnoreAdd))
	register("IGNORE_DEL", ignoreEdit((Host).IgnoreDel))

	register("CONVTIME", func(f *Frame) (*Result, error) {
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		if s == "" {
			return nil, errf("Invalid time string")
		}
		secs, ok := convTime(s)
		if !ok {
			return nil, errf("Time string does not match expected format.")
		}
		return nil, f.Push(Int(secs))
	})

	register("STATS", func(f *Frame) (*Result, error) {
		owner, h, err := statsArg(f)
		if err != nil {
			return nil, err
		}
		s := h.Stats(owner)
		for _, n := range s {
			if err := f.Push(Int(int64(n))); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	register("STATS_ARRAY", func(f *Frame) (*Result, error) {
		owner, h, err := statsArg(f)
		if err != nil {
			return nil, err
		}
		s := h.Stats(owner)
		// Upstream's own reversed build order: garbage, program, player,
		// thing, exit, room, total.
		vals := make([]Value, 7)
		for i, idx := range [7]int{6, 5, 4, 3, 2, 1, 0} {
			vals[i] = Int(int64(s[idx]))
		}
		return nil, f.Push(Arr(NewList(vals)))
	})

	register("USERLOG", func(f *Frame) (*Result, error) {
		msg, err := f.popStr()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if f.MLevel() < int(h.TuneInt("userlog_mlev")) {
			return nil, errf("Permission Denied (mlev < tp_userlog_mlev)")
		}
		h.UserLog(f.Caller, f.Prog.Ref, msg)
		return nil, nil
	})
}

// objTypeFromTag maps EXT-NAME-OK?'s own single-letter/word type tags.
func objTypeFromTag(s string) (ref.ObjType, bool) {
	switch strings.ToLower(s) {
	case "e", "exit":
		return ref.TypeExit, true
	case "r", "room":
		return ref.TypeRoom, true
	case "t", "thing":
		return ref.TypeThing, true
	case "p", "player":
		return ref.TypePlayer, true
	case "f", "muf", "program":
		return ref.TypeProgram, true
	}
	return 0, false
}

// ignoreEdit builds IGNORE_ADD and IGNORE_DEL, which share their argument
// shape: player who -- .
func ignoreEdit(edit func(Host, ref.Ref, ref.Ref)) primFunc {
	return func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Permission Denied.")
		}
		who, err := f.popRef()
		if err != nil {
			return nil, err
		}
		player, err := f.popRef()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		// Checked in upstream's own order — oper1 (who) before oper2
		// (player) — even though the resulting argument numbers are the
		// other way around.
		if !h.Valid(who) {
			return nil, errf("Invalid object. (2)")
		}
		if !h.Valid(player) {
			return nil, errf("Invalid object. (1)")
		}
		edit(h, player, who)
		return nil, nil
	}
}

// statsArg pops STATS/STATS_ARRAY's shared player-or-NOTHING argument.
func statsArg(f *Frame) (ref.Ref, Host, error) {
	owner, h, err := f.refAndHost()
	if err != nil {
		return ref.Nothing, nil, err
	}
	if owner != ref.Nothing && (!h.Valid(owner) || h.ObjType(owner) != ref.TypePlayer) {
		return ref.Nothing, nil, errf("non-player argument (1)")
	}
	if f.MLevel() < 3 && h.Owner(owner) != f.Caller {
		return ref.Nothing, nil, errf("Requires Mucker Level 3.")
	}
	return owner, h, nil
}

// convTime is a port of time_string_to_seconds for CONVTIME's own fixed
// "%T%t%D" format: "HH:MM:SS MO/DY/YR", the year either 2 or 4 digits.
// FMTTIME, which lets a caller supply an arbitrary strptime-style format,
// is not ported — see this file's own top-of-file doc comment.
func convTime(s string) (int64, bool) {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return 0, false
	}
	hms := strings.Split(fields[0], ":")
	if len(hms) != 3 {
		return 0, false
	}
	h, err1 := strconv.Atoi(hms[0])
	m, err2 := strconv.Atoi(hms[1])
	sec, err3 := strconv.Atoi(hms[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, false
	}

	dmy := strings.Split(fields[1], "/")
	if len(dmy) != 3 {
		return 0, false
	}
	mo, err4 := strconv.Atoi(dmy[0])
	day, err5 := strconv.Atoi(dmy[1])
	yr, err6 := strconv.Atoi(dmy[2])
	if err4 != nil || err5 != nil || err6 != nil {
		return 0, false
	}
	if len(dmy[2]) != 4 {
		// A 2-digit year is a %y year: 69-99 -> 1900s, 0-68 -> 2000s,
		// strptime's own convention.
		if yr >= 69 {
			yr += 1900
		} else {
			yr += 2000
		}
	}

	t := time.Date(yr, time.Month(mo), day, h, m, sec, 0, time.UTC)
	return t.Unix(), true
}
