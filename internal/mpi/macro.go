package mpi

import "strings"

// MPI macros: named bodies of MPI a call can invoke as though they
// were built-in functions. {func} defines one for the rest of an
// evaluation, and a world can store more permanently under a
// "_msgmacs" property directory.
//
// A macro has no parameter list. Its body reads its arguments
// positionally as "{:1}", "{:2}" and so on, which are substituted as
// *text* before the body is evaluated — so a macro is closer to a C
// preprocessor macro than to a function, and an argument containing
// MPI is evaluated in the macro's own context rather than the
// caller's.

// macroPropDir is upstream's MPI_MACROS_PROPDIR.
const macroPropDir = "_msgmacs"

// macro finds a macro's body by name.
//
// The search order is upstream's: a {func} definition first, then the
// property directory on the object's owner, then on the object
// itself, then on #0 — so a world can offer macros to everything
// while an object can still override one for itself.
//
// **The middle step is a walk, not a read** (`msg_macro_val`,
// `msgparse.c:1063`): `safegetprop_limited` follows the object's
// environment chain, accepting a value only off an object with the
// same owner as the trigger. Emerald did three flat `GetPropStr`
// calls, so a macro directory on the room a thing is standing in was
// never found — and none of the three went through the property
// safety layer, so `{foo}` could read a hidden or `@__sys__` property
// out of `_msgmacs`.
//
// **A refusal does not abort here.** Upstream's three steps each test
// `!ptr || !*ptr`, so a step that refused falls through to the next
// exactly as an unset property does. The player still gets the
// "PropFetch:" line, because that is printed where the refusal
// happens rather than where it is reported.
func (env *Env) macro(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if body, ok := env.funcs[upper(name)]; ok && body != "" {
		return body, true
	}

	path := macroPropDir + "/" + name
	owner := env.Host.Owner(env.What)
	if body, _, err := env.strictGetProp("", owner,
		path); err == nil && body != "" {
		return body, true
	}
	if body, _, err := env.limitedGetProp("", env.What, owner,
		path); err == nil && body != "" {
		return body, true
	}
	if body, _, err := env.strictGetProp("", 0,
		path); err == nil && body != "" {
		return body, true
	}
	return "", false
}

// expandMacro substitutes a call's arguments into a macro body and
// evaluates the result.
func (env *Env) expandMacro(body string, args []string) (string, error) {
	return Parse(env, substituteArgs(body, args))
}

// substituteArgs replaces each "{:N}" in a macro body with the Nth
// argument, counting from one. A reference past the end of the
// argument list produces nothing rather than failing, which is how a
// macro takes optional arguments.
func substituteArgs(body string, args []string) string {
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		// The sequence is exactly "{:", one digit, "}" — a
		// two-digit reference is not recognised, so a macro
		// has at most nine arguments, the same bound a call
		// has.
		if i+3 < len(body) && body[i] == leadChar &&
			body[i+1] == argStart &&
			body[i+2] >= '0' && body[i+2] <= '9' && body[i+3] == argEnd {
			if n := int(body[i+2] - '1'); n >= 0 &&
				n < len(args) {
				b.WriteString(args[n])
			}
			i += 3
			continue
		}
		b.WriteByte(body[i])
	}
	return b.String()
}
