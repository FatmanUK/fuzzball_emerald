package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ref"

// TESTLOCK and LOCKED? are ports of prim_testlock (src/p_misc.c) and
// prim_lockedp (src/p_db.c). Both delegate the actual lock walk to the host,
// since that needs the world and internal/boolexp; what stays here is
// argument validation and the recursion guard, which read only the frame.
//
// Neither reproduces CHECKREMOTE, upstream's "mlev 2 required to read a
// remote object" gate: that needs a permissions()/ProgUID concept this
// codebase has not ported anywhere yet, and a half-built version of it here
// would be worse than the honest gap.
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

		if thingV.Type != TypeObject || !h.Valid(thingV.Ref) {
			return nil, errf("Invalid object (2).")
		}

		locked, err := h.Locked(f.Descr, f.Level, playerV.Ref, thingV.Ref)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(locked))
	})
}
