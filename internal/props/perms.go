package props

import "strings"

// The five sigil characters a property name can carry
// (`include/props.h:202-206`), and the prefix that marks a system
// property (`:224`). A sigil is tested per **path segment**, not per
// path, so "_stuff/@x" is hidden.
const (
	sigilReadOnly  = '_' // PROP_RDONLY
	sigilReadOnly2 = '%' // PROP_RDONLY2
	sigilPrivate   = '.' // PROP_PRIVATE
	sigilHidden    = '@' // PROP_HIDDEN
	sigilSeeOnly   = '~' // PROP_SEEONLY

	// SystemPropPrefix is what Prop_System tests for.
	SystemPropPrefix = "@__sys__"

	// dirDelimiter is PROPDIR_DELIMITER.
	dirDelimiter = '/'
)

// Check is upstream's `Prop_Check` macro: whether c is the first
// character of path, or of any segment after a '/'.
//
// These five predicates live here rather than in internal/game
// because three packages need them and they are `include/props.h`'s
// own — upstream's property module, not game logic. They take a
// path string and no ref, so nothing about them couples a caller to
// the object graph.
//
// Before the move there were **three copies** — two in
// internal/game, one in internal/boolexp — and they had already
// drifted: internal/game's Prop_System used a *case-insensitive*
// prefix test and skipped the leading-'/' trim, where
// `is_prop_prefix` (`fbstrings.c:1152`) compares bytes and trims both
// sides. So "@__SYS__/x" was a system property to one copy and not
// the other, and "/@__sys__/x" was one to neither.
func Check(path string, c byte) bool {
	if len(path) > 0 && path[0] == c {
		return true
	}
	for i := 0; i+1 < len(path); i++ {
		if path[i] == dirDelimiter && path[i+1] == c {
			return true
		}
	}
	return false
}

// IsReadOnly is `Prop_ReadOnly`: a segment beginning '_' or '%'.
// These are the properties a player may read and only the server may
// write.
func IsReadOnly(path string) bool {
	return Check(path, sigilReadOnly) ||
		Check(path, sigilReadOnly2)
}

// IsPrivate is `Prop_Private`: a segment beginning '.', readable only
// by whoever has permissions on the object.
func IsPrivate(path string) bool {
	return Check(path, sigilPrivate)
}

// IsHidden is `Prop_Hidden`: a segment beginning '@', which takes
// mucker level 4 to read.
func IsHidden(path string) bool {
	return Check(path, sigilHidden)
}

// IsSeeOnly is `Prop_SeeOnly`: a segment beginning '~', which a
// player may read and only a wizard may write.
func IsSeeOnly(path string) bool {
	return Check(path, sigilSeeOnly)
}

// IsSystem is `Prop_System`: anything at or under "@__sys__", which
// is out of bounds to everybody including a wizard.
func IsSystem(path string) bool {
	return HasPropPrefix(path, SystemPropPrefix)
}

// HasPropPrefix is `is_prop_prefix` (`fbstrings.c:1152`): whether
// path, with any leading '/' removed, is prefix or sits under it.
//
// The comparison is **case-sensitive**, which is the one place in the
// property system that is: lookup and ordering are strcasecmp, and
// this is a byte compare.
func HasPropPrefix(path, prefix string) bool {
	path = strings.TrimLeft(path, string(dirDelimiter))
	prefix = strings.TrimLeft(prefix, string(dirDelimiter))
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	rest := path[len(prefix):]
	return rest == "" || rest[0] == dirDelimiter
}
