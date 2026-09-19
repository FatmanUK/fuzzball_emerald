package boolexp

import (
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Unlocked is what Unparse renders a nil (TRUE_BOOLEXP) expression as, and
// what GETLOCKSTR shows for an object with no lock set. PROP_UNLOCKED_VAL
// upstream.
const Unlocked = "*UNLOCKED*"

// Unparse renders a lock expression back to text, the format Parse's dbload
// path accepts. This is unparse_boolexp/unparse_boolexp1.
//
// With fullname false, dbrefs render as "#123" — this is the form stored in
// a Lock property, and the one Parse's dbload path expects back. With
// fullname true, dbrefs render the way host.Name shows them to viewer, for
// display to a player (e.g. PRETTYLOCK). viewer is unused when fullname is
// false, since that form never consults it.
func Unparse(host Host, viewer ref.Ref, b *Expr, fullname bool) string {
	var sb strings.Builder
	unparse1(host, &sb, viewer, b, Const, fullname) // Const stands in for "no outer type"
	return sb.String()
}

func unparse1(host Host, sb *strings.Builder, viewer ref.Ref, b *Expr, outer Kind, fullname bool) {
	if b == nil {
		sb.WriteString(Unlocked)
		return
	}

	switch b.Kind {
	case And:
		if outer == Not {
			sb.WriteByte('(')
		}
		unparse1(host, sb, viewer, b.Sub1, b.Kind, fullname)
		sb.WriteByte(andToken)
		unparse1(host, sb, viewer, b.Sub2, b.Kind, fullname)
		if outer == Not {
			sb.WriteByte(')')
		}
	case Or:
		if outer == Not || outer == And {
			sb.WriteByte('(')
		}
		unparse1(host, sb, viewer, b.Sub1, b.Kind, fullname)
		sb.WriteByte(orToken)
		unparse1(host, sb, viewer, b.Sub2, b.Kind, fullname)
		if outer == Not || outer == And {
			sb.WriteByte(')')
		}
	case Not:
		sb.WriteByte('!')
		unparse1(host, sb, viewer, b.Sub1, b.Kind, fullname)
	case Const:
		if fullname {
			sb.WriteString(host.Name(viewer, b.Thing))
		} else {
			sb.WriteByte(numberToken)
			sb.WriteString(strconv.Itoa(int(b.Thing)))
		}
	case Prop:
		sb.WriteString(b.PropName)
		sb.WriteByte(propDelimiter)
		sb.WriteString(b.PropValue)
	}
}
