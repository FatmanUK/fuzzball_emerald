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
