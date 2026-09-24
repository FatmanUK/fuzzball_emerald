package boolexp

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Eval reports whether a lock passes for player against thing, the
// object the lock is set on. A nil b (TRUE_BOOLEXP) always passes.
// This is eval_boolexp/eval_boolexp_rec; upstream copies the tree
// before walking it only to guard against a concurrent SETLOCKSTR
// mutating it mid-evaluation, which cannot happen here since Expr
// trees are parsed fresh per call and never shared.
func Eval(host Host, descr int, player ref.Ref, b *Expr, thing ref.Ref) bool {
	if b == nil {
		return true
	}

	switch b.Kind {
	case And:
		return Eval(host, descr, player, b.Sub1, thing) && Eval(host, descr, player, b.Sub2, thing)
	case Or:
		return Eval(host, descr, player, b.Sub1, thing) || Eval(host, descr, player, b.Sub2, thing)
	case Not:
		return !Eval(host, descr, player, b.Sub1, thing)
	case Const:
		return evalConst(host, descr, player, b.Thing, thing)
	case Prop:
		return evalProp(host, descr, player, b, thing)
	default:
		return false
	}
}

// evalConst is the BOOLEXP_CONST case: a program is run and its
// completion decides the result; any other dbref passes if it has
// some "relationship" with player — is player, is player's owner,
// is something player carries, or is player's location.
func evalConst(host Host, descr int, player, target, thing ref.Ref) bool {
	if target == ref.Nothing {
		return false
	}

	if host.Type(target) == ref.TypeProgram {
		return host.RunLock(descr, player, target, thing)
	}

	if target == player || target == host.Owner(player) ||
		target == host.Location(player) {
		return true
	}
	for _, c := range host.Contents(player) {
		if c == target {
			return true
		}
	}
	return false
}

// evalProp is the BOOLEXP_PROP case. Only string properties are
// checked — upstream's comment explains why: an integer- or
// float-typed property could otherwise leak past a lock expecting a
// string.
func evalProp(host Host, descr int, player ref.Ref, b *Expr, thing ref.Ref) bool {
	if host.Valid(thing) &&
		hasPropertyStrict(host, descr, player, thing, b.PropName, b.PropValue) {
		return true
	}
	return hasProperty(host, descr, player, player, b.PropName, b.PropValue)
}

// hasProperty is upstream's has_property: strict on what itself, then
// recursively through everything it contains, then — if the
// lock_envcheck @tune parameter is on — through its environment
// chain.
func hasProperty(host Host, descr int, player, what ref.Ref, name, value string) bool {
	if hasPropertyStrict(host, descr, player, what, name, value) {
		return true
	}

	for _, c := range host.Contents(what) {
		if hasProperty(host, descr, player, c, name, value) {
			return true
		}
	}

	if host.LockEnvCheck() {
		for at := host.Parent(what); at != ref.Nothing; at = host.Parent(at) {
			if hasPropertyStrict(host, descr, player, at, name, value) {
				return true
			}
		}
	}

	return false
}

// hasPropertyStrict is upstream's has_property_strict, checking a
// single object's own property. A string property is MPI-evaluated
// and wildcard- matched against value; upstream compares the lock's
// own call always passing an integer value of 0, so an integer
// property equal to 0 — or a float truncating to 0 — matches
// regardless of value. That is a known upstream oddity (flagged in
// its own source comment as a possible security hole) and is
// reproduced deliberately, since it is program-visible behaviour.
func hasPropertyStrict(host Host, descr int, player, what ref.Ref, name, value string) bool {
	v, ok := host.Prop(what, name)
	if !ok {
		return false
	}

	switch v.Type {
	case props.String:
		evaluated := host.EvalLockProp(descr, player, what, v.Str, v.Blessed)
		return ascii.SMatch(evaluated, value)
	case props.Int:
		return v.Num == 0
	case props.Float:
		return int64(v.Float) == 0
	default:
		return false
	}
}
