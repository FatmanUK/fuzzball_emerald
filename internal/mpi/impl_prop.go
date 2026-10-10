package mpi

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
)

// The property functions beyond {prop} and {store}.
//
// Every one of these comes in two flavours that are easy to confuse:
// the plain form searches outwards through the environment for a
// property, and the "!" form looks only at the object named. {prop}
// is the environment form — a description that reads
// "{prop:species}" finds one set on the room if the player has none.
func init() {
	register("PROP!", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("PROP!", args, 1)
		if err != nil {
			return "", err
		}
		return env.strictGetProp("PROP!", obj, args[0])
	})

	// INDEX reads a property whose value names another property,
	// and returns that one's value — one level of indirection,
	// both lookups against the same object.
	register("INDEX", indexProp(true))
	register("INDEX!", indexProp(false))

	register("PROPDIR", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("PROPDIR", args, 1)
		if err != nil {
			return "", err
		}
		return boolOf(len(env.Host.PropChildren(obj, args[0])) > 0), nil
	})

	register("DELPROP", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolveStrict("DELPROP", args, 1)
		if err != nil {
			return "", err
		}
		if !env.safePutProp(obj, args[0], "", false) {
			return "", errf("DELPROP", "Permission denied.")
		}
		return "", nil
	})

	register("BLESS", blessProp(true))
	register("UNBLESS", blessProp(false))

	// {listprops} resolves with the ordinary read wrapper and
	// then applies a blanket owner check of its own, which
	// repeats part of what the wrapper already asked. That
	// duplication is upstream's (`mfn_listprops`) and is harmless
	// — the stricter of two tests wins either way — so it is
	// left as it is rather than collapsed.
	register("LISTPROPS", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("LISTPROPS", args, 1)
		if err != nil {
			return "", err
		}
		dir := strings.TrimSuffix(args[0], "/")
		pattern := ""
		if len(args) > 2 {
			pattern = args[2]
		}

		var out []string
		for _, name := range env.Host.PropChildren(obj, dir) {
			path := name
			if dir != "" {
				path = dir + "/" + name
			}
			if !env.mayList(obj, path) {
				continue
			}
			if pattern != "" &&
				!ascii.SMatch(name, pattern) {
				continue
			}
			out = append(out, path)
		}
		return strings.Join(out, "\r"), nil
	})
}

// indexProp builds {index} and {index!}.
func indexProp(walk bool) impl {
	name := "INDEX"
	if !walk {
		name = "INDEX!"
	}
	read := func(env *Env, obj Ref,
		path string) (string, error) {

		if walk {
			return env.getProp(name, obj, path)
		}
		return env.strictGetProp(name, obj, path)
	}
	return func(env *Env, f *Func, args []string) (string, error) {
		obj, err := env.resolve(f.Name, args, 1)
		if err != nil {
			return "", err
		}
		idx, err := read(env, obj, args[0])
		if err != nil {
			return "", err
		}
		if idx == "" {
			return "", nil
		}
		return read(env, obj, idx)
	}
}

// blessProp builds {bless} and {unbless}, which only a blessed
// message may use — otherwise anything that could write a property
// could grant itself wizard permissions by blessing it.
func blessProp(set bool) impl {
	return func(env *Env, f *Func, args []string) (string, error) {
		// {bless} and {unbless} are strict, like the other
		// two that write.
		obj, err := env.resolveStrict(f.Name, args, 1)
		if err != nil {
			return "", err
		}
		// safeblessprop (`msgparse.c:144`) owes
		// is_valid_propname as well as the blessed bit, which
		// this did not check.
		path := strings.TrimLeft(args[0], "/")
		if path == "" || !validPropName(path) ||
			!env.Blessed {
			return "", errf(f.Name, "Permission denied.")
		}
		env.Host.BlessProp(obj, path, set)
		return "", nil
	}
}

// mayList decides whether {listprops} shows one property.
//
// A system property is never listed. Short of a blessed message,
// neither is a hidden one, nor a private one belonging to someone
// else — and in fact nothing at all is listed off an object that is
// neither the reader themselves nor owned by the same person as the
// object carrying the message, which is upstream's own blanket check
// after the finer-grained ones.
func (env *Env) mayList(obj Ref, path string) bool {
	if props.IsSystem(path) {
		return false
	}
	if env.Blessed {
		return true
	}
	if props.IsHidden(path) {
		return false
	}
	owner := env.Host.Owner(env.What)
	if props.IsPrivate(path) &&
		owner != env.Host.Owner(obj) {
		return false
	}
	return obj == env.Who || env.Host.Owner(obj) == owner
}
