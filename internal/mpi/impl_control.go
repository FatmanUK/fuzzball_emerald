package mpi

import "strings"

// The looping and evaluating functions.
//
// These are the ones whose table entries do not pre-evaluate their arguments,
// because a loop body has to be evaluated once per iteration rather than once
// before the loop starts. Each therefore calls Parse itself on whichever
// arguments it wants evaluated, and exactly once on those it does not.
func init() {
	// Evaluating text and properties.
	register("EVAL", evalText(false))
	register("EVAL!", evalText(true))
	register("EXEC", execProp(true))
	register("EXEC!", execProp(false))

	register("WHILE", func(env *Env, _ *Func, args []string) (string, error) {
		var out strings.Builder
		for i := 0; i < maxListLen; i++ {
			cond, err := Parse(env, args[0])
			if err != nil {
				return "", err
			}
			if !truthy(cond) {
				return out.String(), nil
			}
			// Upstream overwrites its output buffer each time round rather
			// than appending, so a {while} yields only its last iteration.
			out.Reset()
			body, err := Parse(env, args[1])
			if err != nil {
				return "", err
			}
			out.WriteString(body)
		}
		return "", errf("WHILE", "Iteration limit exceeded")
	})

	register("FOR", func(env *Env, _ *Func, args []string) (string, error) {
		name, err := Parse(env, args[0])
		if err != nil {
			return "", err
		}
		start, err := parseInt(env, "FOR", args[1])
		if err != nil {
			return "", err
		}
		end, err := parseInt(env, "FOR", args[2])
		if err != nil {
			return "", err
		}
		step, err := parseInt(env, "FOR", args[3])
		if err != nil {
			return "", err
		}
		if err := env.SetVar(name, ""); err != nil {
			return "", err
		}
		defer env.PopVar()

		var out strings.Builder
		n := 0
		for i := start; (step >= 0 && i <= end) || (step < 0 && i >= end); i += step {
			if n++; n >= maxListLen {
				return "", errf("FOR", "Iteration limit exceeded")
			}
			if err := env.SetVar(name, itoa(i)); err != nil {
				return "", err
			}
			// As with {while}, each pass replaces the last rather than
			// adding to it.
			out.Reset()
			body, err := Parse(env, args[4])
			if err != nil {
				return "", err
			}
			out.WriteString(body)
			// A zero step would never reach the end, so stop after one pass
			// rather than running to the iteration limit.
			if step == 0 {
				break
			}
		}
		return out.String(), nil
	})

	register("FOREACH", func(env *Env, _ *Func, args []string) (string, error) {
		name, err := Parse(env, args[0])
		if err != nil {
			return "", err
		}
		items, err := env.parseList(args, 1, 3, "FOREACH")
		if err != nil {
			return "", err
		}
		if err := env.SetVar(name, ""); err != nil {
			return "", err
		}
		defer env.PopVar()

		var out strings.Builder
		for _, item := range items {
			if err := env.SetVar(name, item); err != nil {
				return "", err
			}
			out.Reset()
			body, err := Parse(env, args[2])
			if err != nil {
				return "", err
			}
			out.WriteString(body)
		}
		return out.String(), nil
	})

	register("PARSE", func(env *Env, _ *Func, args []string) (string, error) {
		name, err := Parse(env, args[0])
		if err != nil {
			return "", err
		}
		items, err := env.parseList(args, 1, 3, "PARSE")
		if err != nil {
			return "", err
		}
		outSep, err := env.parseSep(args, 4, "PARSE", env.sepOr(args, 3))
		if err != nil {
			return "", err
		}
		if err := env.SetVar(name, ""); err != nil {
			return "", err
		}
		defer env.PopVar()

		var out []string
		for _, item := range items {
			if err := env.SetVar(name, item); err != nil {
				return "", err
			}
			got, err := Parse(env, args[2])
			if err != nil {
				return "", err
			}
			out = append(out, got)
		}
		return strings.Join(out, outSep), nil
	})

	register("FILTER", func(env *Env, _ *Func, args []string) (string, error) {
		name, err := Parse(env, args[0])
		if err != nil {
			return "", err
		}
		items, err := env.parseList(args, 1, 3, "FILTER")
		if err != nil {
			return "", err
		}
		outSep, err := env.parseSep(args, 4, "FILTER", env.sepOr(args, 3))
		if err != nil {
			return "", err
		}
		if err := env.SetVar(name, ""); err != nil {
			return "", err
		}
		defer env.PopVar()

		var out []string
		for _, item := range items {
			if err := env.SetVar(name, item); err != nil {
				return "", err
			}
			got, err := Parse(env, args[2])
			if err != nil {
				return "", err
			}
			if truthy(got) {
				out = append(out, item)
			}
		}
		return strings.Join(out, outSep), nil
	})

	register("FOLD", func(env *Env, _ *Func, args []string) (string, error) {
		// "{fold:accvar,itemvar,list,body[,sep]}" — the accumulator starts
		// as the list's first item, so a fold over one item never runs the
		// body at all.
		accName, err := Parse(env, args[0])
		if err != nil {
			return "", err
		}
		itemName, err := Parse(env, args[1])
		if err != nil {
			return "", err
		}
		items, err := env.parseList(args, 2, 4, "FOLD")
		if err != nil {
			return "", err
		}
		if err := env.SetVar(accName, ""); err != nil {
			return "", err
		}
		defer env.PopVar()
		if err := env.SetVar(itemName, ""); err != nil {
			return "", err
		}
		defer env.PopVar()

		if len(items) == 0 {
			return "", nil
		}
		acc := items[0]
		for _, item := range items[1:] {
			if err := env.SetVar(accName, acc); err != nil {
				return "", err
			}
			if err := env.SetVar(itemName, item); err != nil {
				return "", err
			}
			if acc, err = Parse(env, args[3]); err != nil {
				return "", err
			}
		}
		return acc, nil
	})

	// {func} defines a macro for the rest of this evaluation. Its body is
	// wrapped in a {with} per named parameter, binding each to the
	// positional argument the call supplies — which is how a language with
	// no stack gets named parameters.
	register("FUNC", func(env *Env, _ *Func, args []string) (string, error) {
		name, err := Parse(env, args[0])
		if err != nil {
			return "", err
		}
		body := args[len(args)-1]
		for i := 1; i < len(args)-1; i++ {
			param, err := Parse(env, args[i])
			if err != nil {
				return "", err
			}
			body = "{with:" + param + ",{:" + itoa(i) + "}," + body + "}"
		}
		if env.funcs == nil {
			env.funcs = map[string]string{}
		}
		if len(env.funcs) >= maxFuncs {
			return "", errf("FUNC", "Too many functions defined.")
		}
		env.funcs[upper(name)] = body
		return "", nil
	})
}

// maxFuncs is upstream's MAX_MFUN_LIST_LEN reused as the function-table
// bound, matching its "Too many functions defined." check.
const maxFuncs = 64

// evalText builds {eval} and {eval!}, which evaluate their argument a second
// time — the table has already evaluated it once.
//
// The two differ only in permissions: {eval!} keeps whatever blessing the
// message has, and {eval} drops it, so text from an untrusted source cannot
// borrow a blessed property's authority by being run through it.
func evalText(keepBlessed bool) impl {
	return func(env *Env, _ *Func, args []string) (string, error) {
		if keepBlessed {
			return Parse(env, args[0])
		}
		sub := *env
		sub.Blessed = false
		return Parse(&sub, args[0])
	}
}

// execProp builds {exec} and {exec!}: read a property and run it as MPI.
// {exec} searches the environment for it, {exec!} does not.
func execProp(walk bool) impl {
	return func(env *Env, f *Func, args []string) (string, error) {
		obj, err := env.resolve(f.Name, args, 1)
		if err != nil {
			return "", err
		}
		path := strings.TrimLeft(args[0], "/")
		var text string
		if walk {
			text = env.getProp(obj, path)
		} else {
			text = env.Host.GetPropStr(obj, path)
		}
		return Parse(env, text)
	}
}

// parseList evaluates a looping function's list argument and splits it on its
// separator, which is itself an optional argument.
func (env *Env) parseList(args []string, at, sepAt int, fn string) ([]string, error) {
	list, err := Parse(env, args[at])
	if err != nil {
		return nil, err
	}
	sep, err := env.parseSep(args, sepAt, fn, "\r")
	if err != nil {
		return nil, err
	}
	if list == "" {
		return nil, nil
	}
	items := strings.Split(list, sep)
	if len(items) > maxListLen {
		return nil, errf(fn, "Iteration limit exceeded")
	}
	return items, nil
}

// parseSep evaluates an optional separator argument.
func (env *Env) parseSep(args []string, at int, fn, fallback string) (string, error) {
	if at >= len(args) {
		return fallback, nil
	}
	sep, err := Parse(env, args[at])
	if err != nil {
		return "", err
	}
	if sep == "" {
		return "", errf(fn, "Can't use null separator string")
	}
	return sep, nil
}

// sepOr evaluates the input separator so it can stand in as the output one,
// which is what the functions taking both do when only the first is given. A
// failure here is reported when that argument is evaluated for real.
func (env *Env) sepOr(args []string, at int) string {
	if at >= len(args) {
		return "\r"
	}
	sep, err := Parse(env, args[at])
	if err != nil || sep == "" {
		return "\r"
	}
	return sep
}

// parseInt evaluates an argument and reads it as a number.
func parseInt(env *Env, fn, arg string) (int, error) {
	got, err := Parse(env, arg)
	if err != nil {
		return 0, err
	}
	return atoiArg(fn, got)
}
