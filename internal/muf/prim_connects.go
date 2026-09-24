package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ref"

// Connection primitives: what a program can learn about who is
// online.
//
// Every mlev floor in src/p_connects.c has its own wording rather
// than the generic dispatcher's "Permission denied."/"Permission
// denied. Requires Wizbit." — three distinct level-3 variants and
// two level-4 variants appear across this one file, discovered while
// porting Phase 3 — so every primitive here checks its own floor
// inline rather than leaning on mlev_gen.go and primMLevel the way an
// unconditional floor with the dispatcher's own generic wording would
// (see gen_mlev.py's CUSTOM_ABORT_MESSAGE). That includes ONLINE,
// ONLINE_ARRAY, DESCRDBREF and DESCRSECURE?, ported before this phase
// with the generic wording; fixed here to match the C alongside
// everything new.
func init() {
	register("ONLINE", func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Mucker level 3 primitive.")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		who := h.Online()
		for _, p := range who {
			if err := f.Push(Obj(p)); err != nil {
				return nil, err
			}
		}
		return nil, f.Push(Int(int64(len(who))))
	})
	register("ONLINE_ARRAY", func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Mucker level 3 primitive.")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Arr(refList(h.Online())))
	})

	// DESCRCOUNT counts connections, which is not the same as
	// counting players: one player may be connected several
	// times. Unlike most of this file, prim_descrcount has no
	// mlev floor at all.
	register("DESCRCOUNT", func(f *Frame) (*Result, error) {
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		n := 0
		for _, p := range h.Online() {
			n += h.Connections(p)
		}
		return nil, f.Push(Int(int64(n)))
	})

	register("DESCRIPTORS", func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Mucker level 3 primitive.")
		}
		player, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		ds := h.Descriptors(player)
		for _, d := range ds {
			if err := f.Push(Int(int64(d))); err != nil {
				return nil, err
			}
		}
		return nil, f.Push(Int(int64(len(ds))))
	})
	register("DESCR_ARRAY", func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Mucker level 3 primitive.")
		}
		player, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		ds := h.Descriptors(player)
		vals := make([]Value, len(ds))
		for i, d := range ds {
			vals[i] = Int(int64(d))
		}
		return nil, f.Push(Arr(NewList(vals)))
	})

	// DESCR is the connection the program was started from.
	// Unlike most of this file, prim_descr has no mlev floor at
	// all.
	register("DESCR", func(f *Frame) (*Result, error) {
		return nil, f.Push(Int(int64(f.Descr)))
	})
	register("DESCRDBREF", func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Mucker level 3 primitive.")
		}
		d, err := popDescr(f, "(1)")
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Obj(h.DescrPlayer(d)))
	})

	register("WIDTH", descrSize(true))
	register("HEIGHT", descrSize(false))

	// Every connection is TLS here, so this is always true. The
	// primitive keeps its name because programs call it directly.
	register("DESCRSECURE?", func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Requires Mucker Level 3.")
		}
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if v.Type != TypeInteger {
			return nil, errf("Integer descriptor number expected.")
		}
		return nil, f.Push(Bool(true))
	})

	// DESCRIDLE and DESCRTIME are ports of
	// prim_descr_idle/prim_descr_time, upstream's
	// pdescridle/pdescrontime.
	register("DESCRIDLE", descrIntStat(Host.DescrIdle))
	register("DESCRTIME", descrIntStat(Host.DescrOnTime))

	// DESCRLEASTIDLE and DESCRMOSTIDLE are ports of
	// prim_descr_least_idle/prim_descr_most_idle. Unlike
	// DESCRIDLE/ DESCRTIME, neither one aborts on a -1 result —
	// upstream's own primitives push it straight through, "no
	// connection found" simply being a valid answer for "which of
	// a player's connections...".
	register("DESCRLEASTIDLE", descrPlayerStat(Host.DescrLeastIdle))
	register("DESCRMOSTIDLE", descrPlayerStat(Host.DescrMostIdle))

	// DESCRHOST and DESCRUSER are ports of
	// prim_descr_host/prim_descr_user.
	register("DESCRHOST", descrStringStat(Host.DescrHost))
	register("DESCRUSER", descrStringStat(Host.DescrUser))

	// DESCRBOOT is a port of prim_descr_boot.
	register("DESCRBOOT", func(f *Frame) (*Result, error) {
		if f.MLevel() < 4 {
			return nil, errf("Primitive is a wizbit only command.")
		}
		d, err := popDescr(f, "(1)")
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if !h.DescrBoot(d) {
			return nil, errf("Invalid descriptor number. (1)")
		}
		return nil, nil
	})

	// DESCRNOTIFY is a port of prim_descr_notify. The misspelled
	// "an string" in its argument-2 message is upstream's own
	// literal wording, preserved rather than corrected.
	register("DESCRNOTIFY", func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Mucker level 3 primitive.")
		}
		msgV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		descrV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if descrV.Type != TypeInteger {
			return nil, errf("Argument not an integer. (1)")
		}
		if msgV.Type != TypeString {
			return nil, errf("Argument not an string. (2)")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if !h.DescrNotify(int(descrV.Num), msgV.Str) {
			return nil, errf("Invalid descriptor number. (1)")
		}
		return nil, nil
	})

	// NEXTDESCR is a port of prim_nextdescr. Unlike most of this
	// file it never aborts on an invalid descriptor number — 0
	// already means "nothing next", the same answer an invalid
	// one gets.
	register("NEXTDESCR", func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Mucker level 3 primitive.")
		}
		d, err := popDescr(f, "(1)")
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Int(int64(h.NextDescr(d))))
	})

	// FIRSTDESCR and LASTDESCR are ports of
	// prim_firstdescr/prim_lastdescr — see
	// Host.FirstDescr/Host.LastDescr's own doc comment for the
	// player-scoped/global asymmetry the C has.
	register("FIRSTDESCR", descrByPlayer(Host.FirstDescr))
	register("LASTDESCR", descrByPlayer(Host.LastDescr))

	// DESCR_SETUSER is a port of prim_descr_setuser.
	register("DESCR_SETUSER", func(f *Frame) (*Result, error) {
		if f.MLevel() < 4 {
			return nil, errf("Requires Wizbit.")
		}
		passV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		whoV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		descrV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if descrV.Type != TypeInteger {
			return nil, errf("Integer descriptor number expected. (1)")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if whoV.Type != TypeObject ||
			(whoV.Ref != ref.Nothing && !isValidPlayer(h, whoV.Ref)) {
			return nil, errf("Argument must be a player dbref or NOTHING.")
		}
		if passV.Type != TypeString {
			return nil, errf("Password string expected.")
		}
		if whoV.Ref != ref.Nothing &&
			!h.CheckPassword(whoV.Ref, passV.Str) {
			return nil, errf("Incorrect password.")
		}
		return nil, f.Push(Bool(h.SetUser(int(descrV.Num), whoV.Ref)))
	})

	// DESCRFLUSH is a port of prim_descrflush. Upstream computes
	// a result from pdescrflush but never pushes it — genuinely
	// stack-neutral, consuming its descriptor argument and
	// returning nothing at all.
	register("DESCRFLUSH", func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Requires Mucker Level 3 or better.")
		}
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if v.Type != TypeInteger {
			return nil, errf("Integer descriptor number expected.")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		h.DescrFlush(int(v.Num))
		return nil, nil
	})

	// DESCRBUFSIZE is a port of prim_descr_bufsize.
	register("DESCRBUFSIZE", descrIntStat(func(h Host, d int) int {
		return h.DescrBufSize(d)
	}))

	register("SETWIDTH", setDescrSize(true))
	register("SETHEIGHT", setDescrSize(false))
}

// isValidPlayer is upstream's valid_player: a live dbref of type
// player.
func isValidPlayer(h Host, r ref.Ref) bool {
	return h.Valid(r) && h.ObjType(r) == ref.TypePlayer
}

// popDescr pops an integer descriptor number, upstream's own
// "Argument not an integer. <arg>" wording parameterised by which
// argument this was.
func popDescr(f *Frame, arg string) (int, error) {
	v, err := f.Pop()
	if err != nil {
		return 0, err
	}
	if v.Type != TypeInteger {
		return 0, errf("Argument not an integer. %s", arg)
	}
	return int(v.Num), nil
}

// descrSize builds WIDTH and HEIGHT, which report what a connection's
// telnet negotiation said about its terminal.
func descrSize(wantWidth bool) primFunc {
	return func(f *Frame) (*Result, error) {
		d, err := f.popInt()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		w, ht := h.DescrSize(int(d))
		if wantWidth {
			return nil, f.Push(Int(int64(w)))
		}
		return nil, f.Push(Int(int64(ht)))
	}
}

// setDescrSize builds SETWIDTH and SETHEIGHT, ports of prim_setwidth/
// prim_setheight: descr size -- , upstream's own argument order (the
// descriptor is pushed first, so it sits under the size on the
// stack).
func setDescrSize(wantWidth bool) primFunc {
	arg := "Width"
	if !wantWidth {
		arg = "Height"
	}
	return func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Mucker level 3 primitive.")
		}
		sizeV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		descrV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if sizeV.Type != TypeInteger {
			return nil, errf("Argument not an integer. (1)")
		}
		if descrV.Type != TypeInteger {
			return nil, errf("Argument not an integer. (2)")
		}
		if sizeV.Num < 0 || sizeV.Num > 65535 {
			return nil, errf("%s must be between 0 and 65535.", arg)
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		width, height := -1, -1
		if wantWidth {
			width = int(sizeV.Num)
		} else {
			height = int(sizeV.Num)
		}
		if !h.SetDescrSize(int(descrV.Num), width, height) {
			return nil, errf("Invalid descriptor number (2)")
		}
		return nil, nil
	}
}

// descrIntStat builds DESCRIDLE, DESCRTIME and DESCRBUFSIZE: a
// descriptor number in, an int out, aborting when get reports it
// invalid with a negative result — upstream's own convention for
// all three.
func descrIntStat(get func(Host, int) int) primFunc {
	return func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Mucker level 3 primitive.")
		}
		d, err := popDescr(f, "(1)")
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		n := get(h, d)
		if n < 0 {
			return nil, errf("Invalid descriptor number. (1)")
		}
		return nil, f.Push(Int(int64(n)))
	}
}

// descrStringStat builds DESCRHOST and DESCRUSER: a descriptor number
// in, a string out, aborting when the descriptor does not exist.
func descrStringStat(get func(Host, int) (string, bool)) primFunc {
	return func(f *Frame) (*Result, error) {
		if f.MLevel() < 4 {
			return nil, errf("Primitive is a wizbit only command.")
		}
		d, err := popDescr(f, "(1)")
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		s, ok := get(h, d)
		if !ok {
			return nil, errf("Invalid descriptor number. (1)")
		}
		return nil, f.Push(Str(s))
	}
}

// descrPlayerStat builds DESCRLEASTIDLE and DESCRMOSTIDLE: a player
// dbref in, an int out, with no invalid-result abort — see their
// own registration comment.
func descrPlayerStat(get func(Host, ref.Ref) int) primFunc {
	return func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Mucker level 3 primitive.")
		}
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if v.Type != TypeObject {
			return nil, errf("Argument not a dbref.")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if !h.Valid(v.Ref) {
			return nil, errf("Bad dbref.")
		}
		return nil, f.Push(Int(int64(get(h, v.Ref))))
	}
}

// descrByPlayer builds FIRSTDESCR and LASTDESCR: a player dbref, or
// ref.Nothing for the whole server, in; a descriptor number out.
func descrByPlayer(get func(Host, ref.Ref) int) primFunc {
	return func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Requires Mucker Level 3.")
		}
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if v.Type != TypeObject ||
			(v.Ref != ref.Nothing && !isValidPlayer(h, v.Ref)) {
			return nil, errf("Player dbref expected (2)")
		}
		return nil, f.Push(Int(int64(get(h, v.Ref))))
	}
}
