package mpi

import "strings"

// MPI macros: named bodies of MPI a call can invoke as though they were
// built-in functions. {func} defines one for the rest of an evaluation, and a
// world can store more permanently under a "_msgmacs" property directory.
//
// A macro has no parameter list. Its body reads its arguments positionally as
// "{:1}", "{:2}" and so on, which are substituted as *text* before the body
// is evaluated — so a macro is closer to a C preprocessor macro than to a
// function, and an argument containing MPI is evaluated in the macro's own
// context rather than the caller's.

// macroPropDir is upstream's MPI_MACROS_PROPDIR.
const macroPropDir = "_msgmacs"

// macro finds a macro's body by name.
//
// The search order is upstream's: a {func} definition first, then the
// property directory on the object's owner, then on the object itself, then
// on #0 — so a world can offer macros to everything while an object can still
// override one for itself.
func (env *Env) macro(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if body, ok := env.funcs[upper(name)]; ok && body != "" {
		return body, true
	}

	path := macroPropDir + "/" + name
	for _, obj := range [3]Ref{env.Host.Owner(env.What), env.What, 0} {
		if body := env.Host.GetPropStr(obj, path); body != "" {
			return body, true
		}
	}
	return "", false
}

// expandMacro substitutes a call's arguments into a macro body and evaluates
// the result.
func (env *Env) expandMacro(body string, args []string) (string, error) {
	return Parse(env, substituteArgs(body, args))
}

// substituteArgs replaces each "{:N}" in a macro body with the Nth argument,
// counting from one. A reference past the end of the argument list produces
// nothing rather than failing, which is how a macro takes optional arguments.
func substituteArgs(body string, args []string) string {
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		// The sequence is exactly "{:", one digit, "}" — a two-digit
		// reference is not recognised, so a macro has at most nine
		// arguments, the same bound a call has.
		if i+3 < len(body) && body[i] == leadChar && body[i+1] == argStart &&
			body[i+2] >= '0' && body[i+2] <= '9' && body[i+3] == argEnd {
			if n := int(body[i+2] - '1'); n >= 0 && n < len(args) {
				b.WriteString(args[n])
			}
			i += 3
			continue
		}
		b.WriteByte(body[i])
	}
	return b.String()
}
