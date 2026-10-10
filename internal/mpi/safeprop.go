package mpi

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
)

// MPI's property safety layer: `safegetprop_strict`, `safegetprop`
// and `safeputprop` (`msgparse.c:98`, `:165`, `:327`). None of the
// three was ported, so every MPI read went straight to
// `Host.GetPropStr` and every write to `Host.SetPropStr` — which
// means a description could read a `@__sys__` property, read a hidden
// one, or write over a world's MPI macros.
//
// The read side is **two** functions rather than one with a flag,
// because the walking form propagates a refusal rather than stepping
// over it, and that is the whole difference.

// strictGetProp is `safegetprop_strict` (`msgparse.c:165`): one
// object, with the four refusals MPI's property reads all share and
// which were ported nowhere. So a message could read a hidden
// property, or one under `@__sys__`, or somebody else's private one.
//
// Each refusal is a **direct notify** carrying the "PropFetch:"
// prefix, and the caller then aborts with its own name and "Failed
// read." — so a refused read puts *two* lines in front of the
// player, which is upstream's own shape and what the golden case
// compares.
//
// The name is trimmed of leading '/' first, and an empty one is
// "Propname required." rather than a permission failure.
//
// The second result is the property's **own** blessing, which is
// upstream's `*blessed` out parameter. It decides what {exec} and
// {eval} run the text as, and it is the one clause of the three
// resolvers' dependencies that had no port — so a blessed
// property's text ran unblessed and, worse, an unblessed one's ran
// *blessed* whenever the outer message was.
func (env *Env) strictGetProp(fn string, obj Ref,
	path string) (string, bool, error) {

	path = strings.TrimLeft(path, "/")
	if path == "" {
		env.Host.Notify(env.Who,
			"PropFetch: Propname required.")
		return "", false, errf(fn, "Failed read.")
	}
	denied := func() (string, bool, error) {
		env.Host.Notify(env.Who,
			"PropFetch: Permission denied.")
		return "", false, errf(fn, "Failed read.")
	}
	if props.IsSystem(path) {
		return denied()
	}
	if !env.Blessed {
		if props.IsHidden(path) {
			return denied()
		}
		// A private property is readable only when the
		// permissions object and the object carrying it have
		// the same owner — not when the *reader* does.
		if props.IsPrivate(path) && env.Host.Owner(
			env.Perms) != env.Host.Owner(obj) {
			return denied()
		}
	}
	// Upstream's guard here is `if (ptr)` — whether the
	// property **exists**, not whether it has anything in it —
	// so a blessed but empty property still reports blessed.
	// `PropBlessed` answers false for one that is absent, which
	// is the same test; an emptiness check beside it would be
	// stricter than the C and is deliberately not there.
	return env.Host.GetPropStr(obj, path),
		env.Host.PropBlessed(obj, path), nil
}

// getProp reads a property, walking outwards through the environment
// until something answers — upstream's safegetprop
// (`msgparse.c:327`), as against the strict form that looks only at
// the object named.
//
// An unset property and an empty one are not distinguished, which is
// upstream's own: the walk continues past either. A **refusal**,
// though, stops the walk and propagates — `if (!ptr || *ptr) return
// ptr;` — so a hidden property on the first object is not quietly
// stepped over in favour of a readable one further out.
func (env *Env) getProp(fn string, obj Ref,
	path string) (string, bool, error) {

	for i := 0; i < maxEnvDepth && obj != nothing; i++ {
		v, blessed, err := env.strictGetProp(fn, obj, path)
		if err != nil {
			return "", false, err
		}
		if v != "" {
			return v, blessed, nil
		}
		obj = env.Host.Parent(obj)
	}
	return "", false, nil
}

// safePutProp is `safeputprop` (`msgparse.c:98`): the write half, and
// the one with teeth. Unlike the read side it reports nothing — it
// answers false and the caller aborts with its own name and
// "Permission denied." — so a refused write puts **one** line in
// front of the player where a refused read puts two.
//
// Its refusals are not the read side's. `Prop_SeeOnly` is here and
// not there, which is the point of that sigil; `Prop_Private` is
// there and not here, because a private property is somebody's to
// read and anybody's to overwrite. And the MPI macros propdir is
// refused outright for an unblessed message, because a macro is code:
// writing one is writing MPI that something else will run.
//
// `is_valid_propname` plus the explicit loop are upstream's, and
// upstream's are redundant with each other three times over —
// `is_valid_propname` already rejects an empty name, a '\r' and a
// ':', which are exactly what the leading test and the loop after it
// check. All of it is kept because all of it is there; the emptiness
// test here is therefore unobservable, which a mutation confirms.
//
// A nil value removes; this takes a `set` flag instead, since Go has
// no null string.
func (env *Env) safePutProp(obj Ref, path, val string,
	set bool) bool {

	path = strings.TrimLeft(path, "/")
	if path == "" || !validPropName(path) {
		return false
	}
	if props.IsSystem(path) {
		return false
	}
	if !env.Blessed {
		if props.IsHidden(path) || props.IsSeeOnly(path) {
			return false
		}
		if ascii.HasPrefix(path, mpiMacrosPropDir) {
			return false
		}
	}
	if set {
		env.Host.SetPropStr(obj, path, val)
	} else {
		env.Host.DelProp(obj, path)
	}
	return true
}

// mpiMacrosPropDir is MPI_MACROS_PROPDIR (`include/game.h:65`).
const mpiMacrosPropDir = "_msgmacs"

// validPropName is `is_valid_propname` (`property.c`): non-empty, and
// holding neither a carriage return nor the ':' that separates a
// property's name from its value on the command line.
func validPropName(s string) bool {
	if s == "" {
		return false
	}
	return !strings.ContainsAny(s, "\r:")
}

// limitedGetProp is `safegetprop_limited` (`msgparse.c:278`): the
// walking read with an **ownership** restriction on top, and the only
// caller is the macro lookup.
//
// It walks the environment as getProp does, but a value only counts
// when the object carrying it has the owner the caller named — so a
// macro directory on a room somebody else owns is read past rather
// than used. The refusals still fire on every object walked, which is
// why the notifies are upstream's and not this function's.
//
// A **blessed** value is accepted whoever owns it, which is
// upstream's `|| *blessed` and was the one clause of the three
// resolvers' dependencies left unported: a macro directory on
// somebody else's room counts when a wizard has blessed it, which is
// how a world publishes macros from a room nobody owns personally.
func (env *Env) limitedGetProp(fn string, obj Ref, whom Ref,
	path string) (string, bool, error) {

	for i := 0; i < maxEnvDepth && obj != nothing; i++ {
		v, blessed, err := env.strictGetProp(fn, obj, path)
		if err != nil {
			return "", false, err
		}
		if v != "" &&
			(env.Host.Owner(obj) == whom || blessed) {
			return v, blessed, nil
		}
		obj = env.Host.Parent(obj)
	}
	return "", false, nil
}
