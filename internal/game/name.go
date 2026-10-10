package game

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// ok_object_name (`db.c:1690`) in full, with the four `@tune`
// parameters that had no reader anywhere: `reserved_names`,
// `reserved_player_names`, `7bit_other_names` and `7bit_thing_names`.
// The last defaults **true**, so an unconfigured world accepted
// high-byte thing names upstream refuses.
//
// `nameForbidden` was the type-independent half and the only half
// there was, so every creator applied the same rule to every type.

// okObjectName is the whole of it. The reserved-name pattern is
// tested *before* the type switch, so it applies to a player as well
// as to everything else — and `reserved_player_names` then applies
// to a player only.
func okObjectName(w *world.World, name string,
	t ref.ObjType) bool {

	if nameForbidden(name) {
		return false
	}
	// `equalstr`, which is smatch — so a world writes patterns
	// here and nothing else works. An empty pattern is not tested
	// at all, because upstream guards on `*tp_...`: otherwise the
	// empty pattern would match the empty name, which
	// `nameForbidden` has already refused anyway.
	if p := w.Tune.String("reserved_names"); p != "" &&
		ascii.SMatch(name, p) {

		return false
	}
	switch t {
	case ref.TypeRoom, ref.TypeExit, ref.TypeProgram:
		return !w.Tune.Bool("7bit_other_names") ||
			okASCIIAny(name)
	case ref.TypeThing:
		return !w.Tune.Bool("7bit_thing_names") ||
			okASCIIAny(name)
	case ref.TypePlayer:
		return okPlayerName(w, name)
	}
	// TYPE_GARBAGE, and anything else, is refused outright.
	return false
}

// okPlayerName is ok_player_name (`db.c:1627`).
//
// Its character rule is `isgraph`, which its own comment admits:
// written as `isprint(c) && !isspace(c)`, with four character
// exceptions — '(', ')', '\” and ',' — after it that are **dead
// code**, since every one of them is already printable and non-space,
// so the first half of the test never refuses them.
//
// `isprint` in the C locale is false for every byte above 126, so a
// player name can never hold one. That is why there is no
// `7bit_player_names` to go with the other two: the rule is
// unconditional here.
func okPlayerName(w *world.World, name string) bool {
	if len(name) > int(w.Tune.Int("player_name_limit")) {
		return false
	}
	for i := 0; i < len(name); i++ {
		if name[i] < 0x21 || name[i] > 0x7e {
			return false
		}
	}
	if p := w.Tune.String("reserved_player_names"); p != "" &&
		ascii.SMatch(name, p) {

		return false
	}
	_, taken := w.PlayerNamed(name)
	return !taken
}

// okASCIIAny is ok_ascii_any (`db.c:1661`): no byte above 127. Its
// own comment notes that it does *not* check for low bytes, so a
// control character passes this and is caught, if at all, by
// `nameForbidden`.
func okASCIIAny(name string) bool {
	for i := 0; i < len(name); i++ {
		if name[i] > 127 {
			return false
		}
	}
	return true
}

// okPassword is ok_password (`player.c:539`): not empty, and
// printable and non-space throughout.
//
// Emerald's version allowed a byte above 0x7e, where `isprint` in the
// C locale does not — so a password with a high byte in it was
// accepted here and refused upstream, and the player could then not
// connect to the same world built from the C.
func okPassword(pass string) bool {
	if pass == "" {
		return false
	}
	for i := 0; i < len(pass); i++ {
		if pass[i] < 0x21 || pass[i] > 0x7e {
			return false
		}
	}
	return true
}
