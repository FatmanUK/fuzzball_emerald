// Package ref defines the database reference type and the object type and
// flag bits that Fuzzball stores alongside every object.
//
// Values here are wire-compatible with Fuzzball 7: the flag bit positions and
// the special reference numbers are read verbatim from legacy database dumps,
// so they must not be renumbered.
package ref

import (
	"fmt"
	"strconv"
	"strings"
)

// Ref is a database reference — an index into the object table.
type Ref int32

// Special references. These are negative so they can never collide with a
// real object index.
const (
	Nothing   Ref = -1 // no object
	Ambiguous Ref = -2 // a match resolved to more than one object
	Home      Ref = -3 // virtual room: wherever the mover's home is
	Nil       Ref = -4 // a link that deliberately does nothing
)

// GlobalEnvironment is the root room, parent of every other room.
const GlobalEnvironment Ref = 0

// God is the first player, created with the database.
const God Ref = 1

// Ok reports whether r is a plausible index into the object table. It does not
// prove the object exists; callers holding a world still need to look it up.
func (r Ref) Ok() bool { return r >= 0 }

// String renders a ref the way the MUCK does: #123, or a name for the
// special values.
func (r Ref) String() string {
	switch r {
	case Nothing:
		return "#-1"
	case Ambiguous:
		return "#-2"
	case Home:
		return "#-3"
	case Nil:
		return "#-4"
	default:
		return "#" + strconv.Itoa(int(r))
	}
}

// Parse reads a ref in "#123" or "123" form.
func Parse(s string) (Ref, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return Nothing, fmt.Errorf("bad dbref %q: %w", s, err)
	}
	return Ref(n), nil
}
