package muf

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// mpiMacrosPropDir is MPI_MACROS_PROPDIR (`include/game.h:65`): the
// propdir holding a world's MPI macros, which only mucker 3 may write
// because a macro is code.
const mpiMacrosPropDir = "_msgmacs"

// propReadPerms is `prop_read_perms` (`p_props.c:87`): whether the
// running program may read this property off this object.
//
// Neither this nor propWritePerms existed, and between them upstream
// calls the pair at **31 sites** across `p_props.c` and `p_array.c`.
// So every sigil the property system has was unenforced in MUF: a
// mucker-1 program could read a hidden property, write a read-only
// one, or write under `@__sys__`.
//
// The asker is always `ProgUID`, never the triggering player, at
// every one of those sites.
func (f *Frame) propReadPerms(h Host, obj ref.Ref,
	name string) bool {

	if name == "" || props.IsSystem(name) {
		return false
	}
	mlev := f.MLevel()
	if mlev < 3 && props.IsPrivate(name) &&
		!f.permissions(h, f.progUID(h), obj) {
		return false
	}
	return !(mlev < 4 && props.IsHidden(name))
}

// propWritePerms is `prop_write_perms` (`p_props.c:129`), which is
// not the read test with a different name: it weighs five sigils
// rather than two, and the `tp_gender_prop` clause is a **whole-name
// match** where every other test is per path segment.
func (f *Frame) propWritePerms(h Host, obj ref.Ref,
	name string) bool {

	if name == "" || props.IsSystem(name) {
		return false
	}
	mlev := f.MLevel()
	if mlev < 3 {
		if !f.permissions(h, f.progUID(h), obj) {
			if props.IsPrivate(name) ||
				props.IsReadOnly(name) {
				return false
			}
			// strcasecmp against the whole name, so a
			// world that moves its gender property moves
			// this guard with it.
			gender, ok := h.TuneGet("gender_prop")
			if ok && ascii.EqualFold(name, gender) {
				return false
			}
		}
		// A macro is code, so writing one takes mucker 3
		// whoever owns the object — this clause is outside
		// the permissions test.
		if ascii.HasPrefix(name, mpiMacrosPropDir) {
			return false
		}
	}
	if mlev < 4 {
		if props.IsSeeOnly(name) || props.IsHidden(name) {
			return false
		}
	}
	return true
}

// propDenied is the one message every one of those 31 sites aborts
// with.
func propDenied() error { return errf("Permission denied.") }

// envPropReadable is the read test ENVPROP and ENVPROPSTR make, and
// its placement is the whole of it: the walk runs **first**, and the
// test is then made against the object it **landed on** rather than
// the one the search started from (`p_props.c:573`, `:728`).
//
// So a property a program may not read on a parent room refuses the
// lookup even though the starting object was fair game — and when
// the walk finds nothing, `what` is NOTHING and no test is made at
// all, which is why this is guarded on the landing rather than
// unconditional.
//
// Upstream also passes the **untrimmed** name here, where the walk
// itself used a copy with its trailing slashes removed. That makes no
// observable difference — a trailing '/' starts no path segment, so
// no sigil test sees it, and `is_prop_prefix` ends at one anyway —
// but it is why the two strings are not interchangeable in the C.
func (f *Frame) envPropReadable(h Host, landed ref.Ref,
	name string) error {

	if landed == ref.Nothing {
		return nil
	}
	if !f.propReadPerms(h, landed, name) {
		return propDenied()
	}
	return nil
}

// propProtected is the message the `ARRAY_PUT_PROP*` family aborts
// with (`p_array.c:2182`, `:2356`, `:2436`), which is **not** the
// plain "Permission denied." the rest of the property surface uses.
func propProtected() error {
	return errf("Permission denied while trying to set " +
		"protected property.")
}
