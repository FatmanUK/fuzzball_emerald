package mpi

import (
	"strconv"
	"strings"
)

// How MPI turns a string into an object.
//
// There are **four** functions upstream and this package had one.
// `mesg_dbref_raw` (`msgparse.c:676`) is the match itself and makes
// no permission test at all; the other three are thin wrappers that
// each add a different one, and which wrapper a function uses is part
// of its contract:
//
//   - `mesg_dbref` (`:742`) adds `mesg_read_perms` — ownership, a
//     read lock, or a blessed message.
//   - `mesg_dbref_strict` (`:775`) demands the blessed bit or common
//     ownership outright, with no lock route. The four functions that
//     *write* use it.
//   - `mesg_dbref_local` (`:809`) adds `mesg_local_perms`: a
//     **locality** test, so a message can read an object standing
//     near it even when it owns nothing.
//
// Emerald resolved every argument with one unconditional match, so
// `{loc}`, `{tell}`, `{otell}`, `{flags}`, `{contains}`, `{holds}`
// and `{pronouns}` had **no locality test**, and `{store}`,
// `{bless}`, `{unbless}` and `{delprop}` had only the weaker read
// rule. A description could name any object in the database.
//
// The *match* needed no work: `Host.Match` is already
// `mesg_dbref_raw`, keywords and two-phase retry included.

// resolveFail says how a resolution failed.
//
// Upstream keeps the two apart with its own sentinels — UNKNOWN is
// -88 and PERMDENIED is -89 (`include/mpi.h:37`) — and its callers
// word them differently, several of them inconsistently with each
// other, so collapsing them into one error would lose a distinction
// every call site makes.
type resolveFail int

const (
	resolveOK resolveFail = iota
	resolveUnknown
	resolveDenied
)

// resolver names which wrapper a function uses.
type resolver int

const (
	matchRaw resolver = iota
	matchRead
	matchStrict
	matchLocal
)

// matchRef is the match plus one wrapper's test.
//
// **An empty name is a failed match, not the default object.**
// `mesg_dbref_raw` guards its whole body with `if (buf && *buf)` and
// leaves `obj` at UNKNOWN otherwise, so `{prop:x,}` — an argument
// present and empty — is "Match failed." where `{prop:x}` reads the
// object carrying the message. This server treated the two the same.
func (env *Env) matchRef(r resolver, name string) (Ref, resolveFail) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, resolveUnknown
	}
	obj := env.lookup(name)
	// `!OkObj(obj) -> UNKNOWN` is upstream's closing line, and it
	// is what makes the NOTHING, AMBIGUOUS and HOME branches in a
	// dozen functions dead code: none of the three can reach
	// them.
	if !env.Host.Valid(obj) {
		return 0, resolveUnknown
	}
	switch r {
	case matchRead:
		if !env.readPerms(obj) {
			return 0, resolveDenied
		}
	case matchStrict:
		if !env.Blessed && env.Host.Owner(
			env.Perms) != env.Host.Owner(obj) {
			return 0, resolveDenied
		}
	case matchLocal:
		if !env.localPerms(obj) {
			return 0, resolveDenied
		}
	}
	return obj, resolveOK
}

// resolveAs reads an object argument, defaulting to the object
// carrying the message when the argument is **absent**.
//
// That is how every one of upstream's callers spells it — `if (argc
// > N)` around the matcher — so a default is never
// permission-tested.
func (env *Env) resolveAs(r resolver, args []string,
	at int) (Ref, resolveFail) {

	if at >= len(args) {
		return env.What, resolveOK
	}
	return env.matchRef(r, args[at])
}

// resolveMsg is resolveAs with the two messages a function words for
// itself.
func (env *Env) resolveMsg(r resolver, fn string, args []string,
	at int, nomatch, denied string) (Ref, error) {

	obj, fail := env.resolveAs(r, args, at)
	switch fail {
	case resolveUnknown:
		return 0, &Error{Func: fn, Msg: nomatch}
	case resolveDenied:
		return 0, &Error{Func: fn, Msg: denied}
	}
	return obj, nil
}

// resolve is the common shape: the read wrapper with upstream's two
// plain messages, which most functions use verbatim.
func (env *Env) resolve(fn string, args []string,
	at int) (Ref, error) {

	return env.resolveMsg(matchRead, fn, args, at,
		"Match failed.", "Permission denied.")
}

// resolveLocal and resolveStrict are the same for the other two
// wrappers.
func (env *Env) resolveLocal(fn string, args []string,
	at int) (Ref, error) {

	return env.resolveMsg(matchLocal, fn, args, at,
		"Match failed.", "Permission denied.")
}

func (env *Env) resolveStrict(fn string, args []string,
	at int) (Ref, error) {

	return env.resolveMsg(matchStrict, fn, args, at,
		"Match failed.", "Permission denied.")
}

// readPerms is `mesg_read_perms` (`msgparse.c:565`): whether this
// evaluation may look at an object at all.
//
// `#0` is readable by everybody, as are the reader and the object
// carrying the message. Otherwise the permissions object must own it,
// or pass its **read lock** — `test_lock_false_default`, so an
// unset lock refuses rather than passes — or the message must be
// blessed.
func (env *Env) readPerms(obj Ref) bool {
	if obj == 0 || obj == env.Who || obj == env.Perms {
		return true
	}
	owner := env.Host.Owner(env.Perms)
	if owner == env.Host.Owner(obj) {
		return true
	}
	if env.Host.ReadLockPasses(env.Descr, owner, obj) {
		return true
	}
	return env.Blessed
}

// localPerms is `mesg_local_perms` (`msgparse.c:640`): the same
// question asked of something **nearby**.
//
// Four routes before it falls back on readPerms, and the pair of
// isneighbor calls is the point of it: a message on a thing lying in
// a room may read the room's other contents. Note what the fourth
// tests — the read lock on the **owner of the object's location**,
// not on the object and not on the location, which reads like a slip
// and is reproduced.
func (env *Env) localPerms(obj Ref) bool {
	loc := env.Host.Location(obj)
	owner := env.Host.Owner(env.Perms)
	if loc != nothing && owner == env.Host.Owner(loc) {
		return true
	}
	if env.isNeighbor(env.Perms, obj) {
		return true
	}
	if env.isNeighbor(env.Who, obj) {
		return true
	}
	if loc != nothing && env.Host.ReadLockPasses(env.Descr,
		owner, env.Host.Owner(loc)) {
		return true
	}
	return env.readPerms(obj)
}

// isNeighbor is `isneighbor` (`msgparse.c:596`): the same object, one
// inside the other, or both in the same place — with a room
// excluded from each test it would otherwise pass trivially.
func (env *Env) isNeighbor(d1, d2 Ref) bool {
	if d1 == d2 {
		return true
	}
	room1, room2 := env.isRoom(d1), env.isRoom(d2)
	if !room1 && env.Host.Location(d1) == d2 {
		return true
	}
	if !room2 && env.Host.Location(d2) == d1 {
		return true
	}
	return !room1 && !room2 &&
		env.Host.Location(d1) == env.Host.Location(d2)
}

// isRoom asks the host for the type word rather than adding a method
// for one comparison.
func (env *Env) isRoom(obj Ref) bool {
	return env.Host.TypeName(obj) == "Room"
}

// lookup resolves a name or a "#123" reference to an object.
func (env *Env) lookup(name string) Ref {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "#") {
		if n, err := strconv.Atoi(name[1:]); err == nil {
			return Ref(n)
		}
	}
	return env.Host.Match(env.Who, env.What, name)
}

// failAs renders one of the two messages for a resolution that has
// already happened, which the functions resolving *both* their
// arguments before reporting either need.
func failAs(fn string, fail resolveFail,
	nomatch, denied string) error {

	switch fail {
	case resolveUnknown:
		return &Error{Func: fn, Msg: nomatch}
	case resolveDenied:
		return &Error{Func: fn, Msg: denied}
	}
	return nil
}
