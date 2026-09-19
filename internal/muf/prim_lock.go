package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ref"

// permissions is upstream's permissions(): whether player may act on thing
// through basic ownership — itself, HOME, an exit owned by them or by no one,
// or anything else they own outright. A player object never passes except by
// being thing itself.
func permissions(h Host, player, thing ref.Ref) bool {
	if thing == player || thing == ref.Home {
		return true
	}
	switch h.ObjType(thing) {
	case ref.TypePlayer:
		return false
	case ref.TypeExit:
		return h.Owner(thing) == h.Owner(player) || h.Owner(thing) == ref.Nothing
	case ref.TypeRoom, ref.TypeThing, ref.TypeProgram:
		return h.Owner(thing) == h.Owner(player)
	default:
		return false
	}
}

// progUID approximates upstream's ProgUID/find_uid: the permissions a
// running program acts with. The full macro also depends on fr->perms
// (STD_REGUID/SETUID/HARDUID, set by whoever calls interp()) and the STICKY
// and HAVEN program flags, none of which this codebase threads through yet —
// see RunLock's own doc comment for the same gap. This covers upstream's
// common REGUID path: below mucker level 2 a program always runs as its own
// owner; at or above it, as whoever is running it.
func (f *Frame) progUID(h Host) ref.Ref {
	if f.MLevel() < 2 {
		return h.Owner(f.Prog.Ref)
	}
	return h.Owner(f.Caller)
}

// checkRemote is upstream's CHECKREMOTE macro: below mucker level 2, a
// primitive may only read x if it is HOME, the running player, something at
// or holding the player's own location, or something ProgUID controls
// outright.
func (f *Frame) checkRemote(h Host, x ref.Ref) error {
	if f.MLevel() >= 2 || x == ref.Home {
		return nil
	}
	loc := h.Location(f.Caller)
	if h.Location(x) == f.Caller || h.Location(x) == loc || x == loc || x == f.Caller {
		return nil
	}
	if controls(h, f.progUID(h), x) {
		return nil
	}
	return errf("Mucker Level 2 required to get remote info.")
}

// TESTLOCK and LOCKED? are ports of prim_testlock (src/p_misc.c) and
// prim_lockedp (src/p_db.c). Both delegate the actual lock walk to the host,
// since that needs the world and internal/boolexp; what stays here is
// argument validation and the recursion guard, which read only the frame.
func init() {
	register("TESTLOCK", func(f *Frame) (*Result, error) {
		lockV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		playerV, err := f.Pop()
		if err != nil {
			return nil, err
		}

		if f.Level > 8 {
			return nil, errf("Interp call loops not allowed.")
		}

		h, err := f.needHost()
		if err != nil {
			return nil, err
		}

		if playerV.Type != TypeObject || !h.Valid(playerV.Ref) ||
			(h.ObjType(playerV.Ref) != ref.TypePlayer && h.ObjType(playerV.Ref) != ref.TypeThing) {
			return nil, errf("Invalid player or thing argument (1).")
		}

		if err := f.checkRemote(h, playerV.Ref); err != nil {
			return nil, err
		}

		if lockV.Type != TypeLock {
			return nil, errf("Invalid argument (2).")
		}

		ok, err := h.TestLock(f.Descr, f.Level, playerV.Ref, lockV.Lock, f.Trig, f.Caller)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(ok))
	})

	register("LOCKED?", func(f *Frame) (*Result, error) {
		thingV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		playerV, err := f.Pop()
		if err != nil {
			return nil, err
		}

		h, err := f.needHost()
		if err != nil {
			return nil, err
		}

		if f.Level > h.MaxInterpRecursion() {
			return nil, errf("Interp call loops not allowed.")
		}

		// Reproduced verbatim from upstream, bug and all: the condition is
		// written "!= TYPE_PLAYER && == TYPE_THING", so a THING argument is
		// the one thing this rejects rather than the one it was meant to
		// allow. Fuzzball 7.2.1's own doc comment claims both are accepted.
		if playerV.Type != TypeObject || !h.Valid(playerV.Ref) ||
			(h.ObjType(playerV.Ref) != ref.TypePlayer && h.ObjType(playerV.Ref) == ref.TypeThing) {
			return nil, errf("Invalid player or thing argument. (1)")
		}

		if err := f.checkRemote(h, playerV.Ref); err != nil {
			return nil, err
		}

		if thingV.Type != TypeObject || !h.Valid(thingV.Ref) {
			return nil, errf("Invalid object (2).")
		}

		if err := f.checkRemote(h, thingV.Ref); err != nil {
			return nil, err
		}

		locked, err := h.Locked(f.Descr, f.Level, playerV.Ref, thingV.Ref)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(locked))
	})
}

// GETLOCKSTR, SETLOCKSTR, PARSELOCK, UNPARSELOCK and PRETTYLOCK are ports of
// prim_getlockstr and prim_setlockstr (src/p_db.c) and prim_parselock,
// prim_unparselock and prim_prettylock (src/p_misc.c). GETLOCKSTR/SETLOCKSTR
// read and write the standard @lock property directly; PARSELOCK/
// UNPARSELOCK/PRETTYLOCK convert between a lock string and a TypeLock value
// without touching any object — PRETTYLOCK differs from UNPARSELOCK only in
// rendering dbrefs the way a player would see them, not as bare "#123"s.
func init() {
	register("GETLOCKSTR", func(f *Frame) (*Result, error) {
		obj, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if obj.Type != TypeObject {
			return nil, errf("Invalid argument type")
		}

		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if !h.Valid(obj.Ref) {
			return nil, errf("Invalid argument type")
		}

		if err := f.checkRemote(h, obj.Ref); err != nil {
			return nil, err
		}

		if f.MLevel() < 3 && !permissions(h, f.progUID(h), obj.Ref) {
			return nil, errf("Permission denied.")
		}

		return nil, f.Push(Str(h.LockString(obj.Ref)))
	})

	register("SETLOCKSTR", func(f *Frame) (*Result, error) {
		keyV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		objV, err := f.Pop()
		if err != nil {
			return nil, err
		}

		if objV.Type != TypeObject {
			return nil, errf("Invalid argument type (1)")
		}
		if keyV.Type != TypeString {
			return nil, errf("Non-string argument (2)")
		}

		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if !h.Valid(objV.Ref) {
			return nil, errf("Invalid argument type (1)")
		}

		if f.MLevel() < 4 && !permissions(h, f.progUID(h), objV.Ref) {
			return nil, errf("Permission denied.")
		}

		ok := h.SetLockString(f.Descr, f.Caller, objV.Ref, keyV.Str)
		return nil, f.Push(Bool(ok))
	})

	register("PARSELOCK", func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if v.Type != TypeString {
			return nil, errf("Invalid argument.")
		}

		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(LockVal(h.ParseLock(f.Descr, f.progUID(h), v.Str)))
	})

	register("UNPARSELOCK", func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if v.Type != TypeLock {
			return nil, errf("Invalid argument.")
		}

		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Str(h.UnparseLock(f.progUID(h), v.Lock)))
	})

	register("PRETTYLOCK", func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if v.Type != TypeLock {
			return nil, errf("Invalid argument.")
		}

		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Str(h.PrettyLock(f.progUID(h), v.Lock)))
	})

	// ARRAY_FILTER_LOCK is a port of prim_array_filter_lock (src/p_array.c):
	// keep only the dbrefs in an array that pass a lock. It reuses
	// Host.TestLock per element rather than a bulk Host method, since that
	// already resolves the consistent_lock_source thing/trig choice — unlike
	// TESTLOCK, upstream has no recursion guard here, so none is added.
	register("ARRAY_FILTER_LOCK", func(f *Frame) (*Result, error) {
		lockV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		arrV, err := f.Pop()
		if err != nil {
			return nil, err
		}

		if arrV.Type != TypeArray || arrV.Array == nil {
			return nil, errf("Argument not an array. (1)")
		}
		items := arrV.Array.Values()
		for _, v := range items {
			if v.Type != TypeObject {
				return nil, errf("Argument not an array of dbrefs. (1)")
			}
		}
		if lockV.Type != TypeLock {
			return nil, errf("Argument not a lock. (2)")
		}

		h, err := f.needHost()
		if err != nil {
			return nil, err
		}

		out := NewList(nil)
		for _, v := range items {
			if !h.Valid(v.Ref) {
				continue
			}
			ok, err := h.TestLock(f.Descr, f.Level, v.Ref, lockV.Lock, f.Trig, f.Caller)
			if err != nil {
				return nil, err
			}
			if ok {
				out.Append(v)
			}
		}
		return nil, f.Push(Arr(out))
	})
}
