// Package boolexp implements Fuzzball's lock expressions: the small boolean
// language stored in PROP_LOKTYP properties and evaluated by TESTLOCK and
// every permission check that consults a lock.
//
// This is a port of fuzzball/src/boolexp.c. A lock is a tree of AND, OR, NOT,
// CONST (a dbref) and PROP (a property name/value pair) nodes. Unlike
// upstream, Emerald never caches the parsed tree: a lock property holds its
// unparsed string (internal/props.Value with Type Lock), and parsing happens
// at the point of use — see internal/props's doc comment on Value. That is
// why Parse always takes the dbload path upstream reserves for its disk
// loader: the string stored is already in "#123&#456" dbref form, produced by
// Unparse with fullname false, so nothing needs re-matching against a typed
// name. Name matching happens once, when a lock is set from player input
// (Parse with dbload false), and the result is stored back through Unparse.
package boolexp

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Kind is a lock expression node's type. Upstream's BOOLEXP_AND/OR/NOT/
// CONST/PROP.
type Kind int

const (
	And Kind = iota
	Or
	Not
	Const
	Prop
)

// Expr is one node of a parsed lock expression. A nil *Expr is upstream's
// TRUE_BOOLEXP: an unlocked lock, which always evaluates true.
type Expr struct {
	Kind Kind

	// Sub1 and Sub2 hold the operands of And and Or; Sub1 alone holds Not's.
	Sub1, Sub2 *Expr

	// Thing is Const's dbref.
	Thing ref.Ref

	// PropName and PropValue are Prop's property path and the string it must
	// match. parse_boolprop only ever builds a string-type check.
	PropName  string
	PropValue string
}

// Host is what boolexp needs from the game world: name matching to parse a
// lock a player typed, and object/property/program access to evaluate one.
// internal/game implements this against internal/world.
type Host interface {
	// Match resolves name against player's surroundings the way parsing a
	// lock does — upstream's match_neighbor, match_possession, match_me,
	// match_here, match_absolute, match_registered and match_player, tried
	// in that order. It returns ref.Nothing or ref.Ambiguous on failure.
	Match(player ref.Ref, name string) ref.Ref
	// Wizard reports whether player may reference a hidden property in a
	// lock.
	Wizard(player ref.Ref) bool
	// Name renders r the way Unparse's fullname form shows it to viewer —
	// upstream's unparse_object, which includes the dbref and flags when
	// viewer may see them, not just a bare name.
	Name(viewer, r ref.Ref) string

	// Valid reports whether r names a live object, upstream's OkObj.
	Valid(r ref.Ref) bool
	// Type, Owner, Location, Contents and Flags expose the object graph
	// eval_boolexp_rec walks.
	Type(r ref.Ref) ref.ObjType
	Owner(r ref.Ref) ref.Ref
	Location(r ref.Ref) ref.Ref
	Contents(r ref.Ref) []ref.Ref
	Flags(r ref.Ref) ref.Flags
	// Parent is upstream's getparent: the environment-tree step a property
	// search takes when the lock_envcheck @tune parameter is on.
	Parent(r ref.Ref) ref.Ref
	// LockEnvCheck mirrors the lock_envcheck @tune parameter.
	LockEnvCheck() bool

	// Prop returns a property's raw stored value. ok is false when there is
	// no such property.
	Prop(r ref.Ref, path string) (v props.Value, ok bool)
	// EvalLockProp runs a string property's value through MPI the way a lock
	// check does, with the property's Blessed flag granting it wizard
	// permissions. what is the object the property was read from, which MPI
	// runs with — not necessarily the object the lock itself is on. This is
	// upstream's do_parse_mesg(..., MPI_ISLOCK, ...).
	EvalLockProp(descr int, player, what ref.Ref, raw string, blessed bool) string

	// RunLock runs a TYPE_PROGRAM constant in the foreground, the way
	// eval_boolexp_rec does, and reports whether it ran to completion rather
	// than aborting.
	RunLock(descr int, player, prog, thing ref.Ref) bool
}
