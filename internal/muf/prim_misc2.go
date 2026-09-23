package muf

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/timefmt"
)

// EVENT_COUNT, EVENT_EXISTS, EXT-NAME-OK?, READ_WANTS_BLANKS,
// READ_WANTS_NO_BLANKS, IGNORING?, IGNORE_ADD, IGNORE_DEL, CONVTIME, FMTTIME,
// STATS, STATS_ARRAY and USERLOG are ports of the more tractable primitives
// left in src/p_misc.c. Deliberately not ported, each needing substantially
// more than a primitive port on its own: TIMER_START/TIMER_STOP/EVENT_SEND (a
// delayed, out-of-band event-delivery scheduler distinct from WATCHPID's own
// synchronous one), DEBUGGER_BREAK/DEBUG_LINE/DEBUG_ON/DEBUG_OFF (the MUF
// single-step debugger, unimplemented entirely), and SMTP_SEND (a real SMTP
// client).
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
		secs, ok := timefmt.Seconds(s, "%T%t%D")
		if !ok {
			return nil, errf("Time string does not match expected format.")
		}
		return nil, f.Push(Int(secs))
	})

	// FMTTIME is CONVTIME with the format under the caller's control, its
	// name notwithstanding: it parses a time string rather than rendering
	// one. TIMEFMT is the primitive that formats.
	register("FMTTIME", func(f *Frame) (*Result, error) {
		format, err := f.Pop()
		if err != nil {
			return nil, err
		}
		value, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if format.Type != TypeString || format.Str == "" {
			return nil, errf("Invalid format string")
		}
		if value.Type != TypeString || value.Str == "" {
			return nil, errf("Invalid time string")
		}
		secs, ok := timefmt.Seconds(value.Str, format.Str)
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

// SMTP_SEND sends an email. It is the one primitive that reaches outside the
// server entirely, and is wizard-only for that reason.
func init() {
	register("SMTP_SEND", func(f *Frame) (*Result, error) {
		// "Permission Denied." with a capital D here, unlike most of the
		// server — upstream's own, so it is checked inline rather than
		// left to the generated table.
		if f.MLevel() < 4 {
			return nil, errf("Permission Denied.")
		}
		bodyV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		subjectV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		toNameV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		toEmailV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}

		// A server with no relay configured reports so rather than
		// failing, so a program can offer mail when it is available and
		// do without when it is not.
		if !h.SMTPConfigured() {
			return nil, f.Push(Int(-1))
		}
		if tlsOK, authOK := h.SMTPModesValid(); !tlsOK {
			return nil, errf("Server has SMTP enabled, but smtp_ssl_type is " +
				"less than 0 or greater than 2.")
		} else if !authOK {
			return nil, errf("Server has SMTP enabled, but smtp_auth_type is " +
				"less than 0 or greater than 3.")
		}

		if bodyV.Type != TypeArray {
			return nil, errf("Argument not an array.(4)")
		}
		if bodyV.Array.Len() == 0 {
			return nil, errf("Cannot send empty body.(4)")
		}
		if subjectV.Type != TypeString {
			return nil, errf("Argument not a string.(3)")
		}
		if subjectV.Str == "" {
			return nil, errf("Subject must be a non-empty string.(3)")
		}
		if toNameV.Type != TypeString {
			return nil, errf("Argument not a string.(2)")
		}
		if toEmailV.Type != TypeString {
			return nil, errf("Argument not a string.(1)")
		}
		if toEmailV.Str == "" {
			return nil, errf("To email must be a non-empty string.(1)")
		}

		lines := make([]string, 0, bodyV.Array.Len())
		for _, v := range bodyV.Array.Values() {
			lines = append(lines, v.String())
		}
		h.SendMail(toEmailV.Str, toNameV.Str, subjectV.Str,
			strings.Join(lines, "\r\n"), f.Caller)
		return nil, f.Push(Int(0))
	})
}

// The MUF debugger's four primitives.
//
// Upstream's debugger has two halves: an instruction tracer, and an
// interactive prompt a breakpoint drops the player into. The tracer is what
// DEBUG_ON, DEBUG_OFF and DEBUG_LINE drive, and it is ported — a program
// flagged DARK prints a line per instruction to whoever controls it. The
// prompt is not: see DEBUGGER_BREAK.
func init() {
	register("DEBUG_ON", debugFlag(true))
	register("DEBUG_OFF", debugFlag(false))

	// DEBUG_LINE prints a single trace line, for a program that is tracing
	// only the part it cares about rather than all of itself. It is
	// deliberately silent when the program *is* flagged for tracing, since
	// the line would be printed twice.
	register("DEBUG_LINE", func(f *Frame) (*Result, error) {
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if h.Flags(f.Prog.Ref).Has(ref.Dark) || !h.Controls(f.Caller, f.Prog.Ref) {
			return nil, nil
		}
		if f.PC >= 0 && f.PC < len(f.Prog.Code) {
			f.trace(f.Prog.Code[f.PC])
		}
		return nil, nil
	})

	// DEBUGGER_BREAK asks upstream to stop and hand the player a debugger
	// prompt, where they could step, inspect and continue. Emerald has no
	// such prompt — it would mean taking over a connection's input, which
	// nothing else in this server does — so the nearest honest thing is
	// done instead: tracing is forced on for the rest of this program's
	// run, so the player sees what a break would have let them step
	// through. A program that breaks is therefore not suspended, which is
	// the difference that matters.
	register("DEBUGGER_BREAK", func(f *Frame) (*Result, error) {
		f.ForceTrace = true
		f.Traced = true
		return nil, nil
	})
}

// debugFlag builds DEBUG_ON and DEBUG_OFF, which set and clear the running
// program's DARK flag — which is what "this program is being debugged" means.
func debugFlag(on bool) primFunc {
	return func(f *Frame) (*Result, error) {
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		flags := h.Flags(f.Prog.Ref)
		if on {
			flags |= ref.Dark
		} else {
			flags &^= ref.Dark
		}
		h.SetFlags(f.Prog.Ref, flags)
		// Taking effect at once rather than when the frame next resumes
		// is what makes "debug_on ... debug_off" trace the part between
		// them and nothing else.
		f.Traced = on || f.ForceTrace
		return nil, nil
	}
}
