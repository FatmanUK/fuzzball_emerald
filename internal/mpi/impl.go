package mpi

import (
	"strconv"
	"strings"
	"time"
)

// impl is one MPI function's implementation.
type impl func(env *Env, fn *Func, args []string) (string, error)

// impls holds the implementations, keyed by the table's name. A function in
// the table with no entry here reports itself unimplemented rather than
// silently producing nothing.
var impls = map[string]impl{}

func register(name string, fn impl) {
	if _, ok := functions[name]; !ok {
		panic("mpi: registering an unknown function " + name)
	}
	impls[name] = fn
}

// Implemented is how many functions have implementations.
func Implemented() int { return len(impls) }

func init() {
	// Text.
	register("LIT", func(_ *Env, _ *Func, args []string) (string, error) {
		// LIT returns its argument without evaluating it, which is why
		// its table entry does not parse.
		return strings.Join(args, string(argSep)), nil
	})
	register("NULL", func(*Env, *Func, []string) (string, error) { return "", nil })
	register("STRIP", one(strings.TrimSpace))
	register("TOUPPER", one(strings.ToUpper))
	register("TOLOWER", one(strings.ToLower))

	register("STRLEN", func(_ *Env, _ *Func, args []string) (string, error) {
		return itoa(len(args[0])), nil
	})
	register("SUBST", func(_ *Env, _ *Func, args []string) (string, error) {
		// "{subst:string,old,new}"
		return strings.ReplaceAll(args[0], args[1], args[2]), nil
	})
	register("MIDSTR", func(_ *Env, _ *Func, args []string) (string, error) {
		s := args[0]
		start, err := atoiArg("MIDSTR", args[1])
		if err != nil {
			return "", err
		}
		length := len(s)
		if len(args) > 2 {
			if length, err = atoiArg("MIDSTR", args[2]); err != nil {
				return "", err
			}
		}
		// MPI indexes strings from one.
		if start < 1 || length < 0 || start > len(s) {
			return "", nil
		}
		end := start - 1 + length
		if end > len(s) {
			end = len(s)
		}
		return s[start-1 : end], nil
	})
	register("INSTR", func(_ *Env, _ *Func, args []string) (string, error) {
		return itoa(strings.Index(args[0], args[1]) + 1), nil
	})
	register("CENTER", pad(padCenter))
	register("LEFT", pad(padLeft))
	register("RIGHT", pad(padRight))

	// Arithmetic.
	register("ADD", fold(func(a, b int) int { return a + b }))
	register("SUBT", fold(func(a, b int) int { return a - b }))
	register("MULT", fold(func(a, b int) int { return a * b }))
	register("DIV", foldDiv(false))
	register("MOD", foldDiv(true))
	register("ABS", func(_ *Env, _ *Func, args []string) (string, error) {
		n, err := atoiArg("ABS", args[0])
		if err != nil {
			return "", err
		}
		if n < 0 {
			n = -n
		}
		return itoa(n), nil
	})
	register("SIGN", func(_ *Env, _ *Func, args []string) (string, error) {
		n, err := atoiArg("SIGN", args[0])
		if err != nil {
			return "", err
		}
		switch {
		case n > 0:
			return "1", nil
		case n < 0:
			return "-1", nil
		}
		return "0", nil
	})
	register("INC", stepBy(1))
	register("DEC", stepBy(-1))
	register("MAX", extreme(true))
	register("MIN", extreme(false))

	// Comparison and logic. These return "1" for true and "" for false,
	// which is what MPI treats as a boolean.
	register("EQ", compare(func(c int) bool { return c == 0 }))
	register("NE", compare(func(c int) bool { return c != 0 }))
	register("GT", compareNum(func(a, b int) bool { return a > b }))
	register("LT", compareNum(func(a, b int) bool { return a < b }))
	register("GE", compareNum(func(a, b int) bool { return a >= b }))
	register("LE", compareNum(func(a, b int) bool { return a <= b }))
	register("NOT", func(_ *Env, _ *Func, args []string) (string, error) {
		return boolOf(!truthy(args[0])), nil
	})

	// {and} and {or} do not parse their arguments up front, so they can
	// stop as soon as the answer is known.
	register("AND", shortCircuit(false))
	register("OR", shortCircuit(true))

	register("IF", func(env *Env, _ *Func, args []string) (string, error) {
		cond, err := Parse(env, args[0])
		if err != nil {
			return "", err
		}
		if truthy(cond) {
			return Parse(env, args[1])
		}
		if len(args) > 2 {
			return Parse(env, args[2])
		}
		return "", nil
	})

	// Objects and properties.
	register("NAME", objectText(func(env *Env, obj Ref) string {
		return env.Host.Name(obj)
	}))
	register("PROP", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("PROP", args, 1)
		if err != nil {
			return "", err
		}
		return env.Host.GetPropStr(obj, args[0]), nil
	})
	register("STORE", func(env *Env, _ *Func, args []string) (string, error) {
		// "{store:value,property,object}"
		obj, err := env.resolve("STORE", args, 2)
		if err != nil {
			return "", err
		}
		if !env.mayWrite(obj) {
			return "", errf("STORE", "Permission denied.")
		}
		env.Host.SetPropStr(obj, args[1], args[0])
		return "", nil
	})
	register("LOC", objectRef(func(env *Env, obj Ref) Ref {
		return env.Host.Location(obj)
	}))
	register("OWNER", objectRef(func(env *Env, obj Ref) Ref {
		return env.Host.Owner(obj)
	}))
	register("AWAKE", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("AWAKE", args, 0)
		if err != nil {
			return "", err
		}
		return boolOf(env.Host.Online(obj)), nil
	})
	register("ISTYPE", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("ISTYPE", args, 0)
		if err != nil {
			return "", err
		}
		want := strings.ToUpper(strings.TrimSpace(args[len(args)-1]))
		return boolOf(want == "PLAYER" && env.Host.IsPlayer(obj)), nil
	})
	register("ISDBREF", func(env *Env, _ *Func, args []string) (string, error) {
		obj := env.lookup(args[0])
		return boolOf(env.Host.Valid(obj)), nil
	})
	register("ISNUM", func(_ *Env, _ *Func, args []string) (string, error) {
		_, err := strconv.Atoi(strings.TrimSpace(args[0]))
		return boolOf(err == nil), nil
	})

	// Variables and control.
	register("WITH", func(env *Env, _ *Func, args []string) (string, error) {
		// "{with:name,value,body...}"
		value, err := Parse(env, args[1])
		if err != nil {
			return "", err
		}
		if err := env.SetVar(args[0], value); err != nil {
			return "", err
		}
		defer env.PopVar()

		var out strings.Builder
		for _, body := range args[2:] {
			got, err := Parse(env, body)
			if err != nil {
				return "", err
			}
			out.WriteString(got)
		}
		return out.String(), nil
	})
	// A "{&name}" reference compiles to a SUBLIST call whose first argument
	// is the variable's value. With nothing further to slice, that value is
	// the answer; the list-slicing form needs the list functions, which are
	// not implemented yet.
	register("SUBLIST", func(_ *Env, _ *Func, args []string) (string, error) {
		if len(args) == 1 {
			return args[0], nil
		}
		return "", errf("SUBLIST", "Not implemented yet.")
	})

	register("SET", func(env *Env, _ *Func, args []string) (string, error) {
		if err := env.SetVar(args[0], args[1]); err != nil {
			return "", err
		}
		return "", nil
	})

	register("TELL", func(env *Env, _ *Func, args []string) (string, error) {
		target := env.Who
		if len(args) > 1 {
			var err error
			if target, err = env.resolve("TELL", args, 1); err != nil {
				return "", err
			}
		}
		for _, line := range strings.Split(args[0], "\r") {
			env.Host.Notify(target, line)
		}
		return "", nil
	})

	// Time.
	register("TIME", clock("15:04:05"))
	register("DATE", clock("Mon Jan 02 2006"))
	register("SECS", func(env *Env, _ *Func, _ []string) (string, error) {
		return itoa(int(env.Host.Now())), nil
	})
	register("CONVSECS", func(env *Env, _ *Func, args []string) (string, error) {
		n, err := atoiArg("CONVSECS", args[0])
		if err != nil {
			return "", err
		}
		return time.Unix(int64(n), 0).UTC().Format("Mon Jan 02 15:04:05 2006"), nil
	})

	register("VERSION", func(*Env, *Func, []string) (string, error) {
		return "Muck2.2fb7.21", nil
	})
}

// one builds a single-argument text function.
func one(fn func(string) string) impl {
	return func(_ *Env, _ *Func, args []string) (string, error) {
		return fn(args[0]), nil
	}
}

// fold builds an arithmetic function that combines every argument.
func fold(op func(a, b int) int) impl {
	return func(_ *Env, fn *Func, args []string) (string, error) {
		total, err := atoiArg(fn.Name, args[0])
		if err != nil {
			return "", err
		}
		for _, a := range args[1:] {
			n, err := atoiArg(fn.Name, a)
			if err != nil {
				return "", err
			}
			total = op(total, n)
		}
		return itoa(total), nil
	}
}

// foldDiv builds division and modulus, which yield zero rather than failing
// when the divisor is zero, as MUF's do.
func foldDiv(mod bool) impl {
	return func(_ *Env, fn *Func, args []string) (string, error) {
		total, err := atoiArg(fn.Name, args[0])
		if err != nil {
			return "", err
		}
		for _, a := range args[1:] {
			n, err := atoiArg(fn.Name, a)
			if err != nil {
				return "", err
			}
			if n == 0 {
				return "0", nil
			}
			if mod {
				total %= n
			} else {
				total /= n
			}
		}
		return itoa(total), nil
	}
}

// stepBy builds {inc} and {dec}, which take an optional amount.
func stepBy(sign int) impl {
	return func(_ *Env, fn *Func, args []string) (string, error) {
		n, err := atoiArg(fn.Name, args[0])
		if err != nil {
			return "", err
		}
		by := 1
		if len(args) > 1 {
			if by, err = atoiArg(fn.Name, args[1]); err != nil {
				return "", err
			}
		}
		return itoa(n + sign*by), nil
	}
}

// extreme builds {max} and {min}.
func extreme(wantMax bool) impl {
	return func(_ *Env, fn *Func, args []string) (string, error) {
		best, err := atoiArg(fn.Name, args[0])
		if err != nil {
			return "", err
		}
		for _, a := range args[1:] {
			n, err := atoiArg(fn.Name, a)
			if err != nil {
				return "", err
			}
			if (wantMax && n > best) || (!wantMax && n < best) {
				best = n
			}
		}
		return itoa(best), nil
	}
}

// compare builds the equality tests, which compare as text when either side is
// not a number.
func compare(ok func(int) bool) impl {
	return func(_ *Env, _ *Func, args []string) (string, error) {
		a, aerr := strconv.Atoi(strings.TrimSpace(args[0]))
		b, berr := strconv.Atoi(strings.TrimSpace(args[1]))
		if aerr == nil && berr == nil {
			switch {
			case a < b:
				return boolOf(ok(-1)), nil
			case a > b:
				return boolOf(ok(1)), nil
			}
			return boolOf(ok(0)), nil
		}
		return boolOf(ok(strings.Compare(args[0], args[1]))), nil
	}
}

// compareNum builds the ordering tests, which are numeric.
func compareNum(ok func(a, b int) bool) impl {
	return func(_ *Env, fn *Func, args []string) (string, error) {
		a, err := atoiArg(fn.Name, args[0])
		if err != nil {
			return "", err
		}
		b, err := atoiArg(fn.Name, args[1])
		if err != nil {
			return "", err
		}
		return boolOf(ok(a, b)), nil
	}
}

// shortCircuit builds {and} and {or}, which stop as soon as the answer is
// settled rather than evaluating every argument.
func shortCircuit(stopOn bool) impl {
	return func(env *Env, _ *Func, args []string) (string, error) {
		for _, a := range args {
			got, err := Parse(env, a)
			if err != nil {
				return "", err
			}
			if truthy(got) == stopOn {
				return boolOf(stopOn), nil
			}
		}
		return boolOf(!stopOn), nil
	}
}

// pad builds the alignment functions.
type padFunc func(s string, width int, fill string) string

func pad(fn padFunc) impl {
	return func(_ *Env, f *Func, args []string) (string, error) {
		width, err := atoiArg(f.Name, args[1])
		if err != nil {
			return "", err
		}
		fill := " "
		if len(args) > 2 && args[2] != "" {
			fill = args[2]
		}
		return fn(args[0], width, fill), nil
	}
}

func padLeft(s string, width int, fill string) string {
	return s + repeatTo(width-len(s), fill)
}

func padRight(s string, width int, fill string) string {
	return repeatTo(width-len(s), fill) + s
}

func padCenter(s string, width int, fill string) string {
	gap := width - len(s)
	if gap <= 0 {
		return s
	}
	left := gap / 2
	return repeatTo(left, fill) + s + repeatTo(gap-left, fill)
}

// repeatTo builds a run of fill exactly n characters long.
func repeatTo(n int, fill string) string {
	if n <= 0 || fill == "" {
		return ""
	}
	out := strings.Repeat(fill, n/len(fill)+1)
	return out[:n]
}

// objectText builds a function that reports something about an object.
func objectText(fn func(*Env, Ref) string) impl {
	return func(env *Env, f *Func, args []string) (string, error) {
		obj, err := env.resolve(f.Name, args, 0)
		if err != nil {
			return "", err
		}
		return fn(env, obj), nil
	}
}

// objectRef builds a function that resolves one object to another.
func objectRef(fn func(*Env, Ref) Ref) impl {
	return func(env *Env, f *Func, args []string) (string, error) {
		obj, err := env.resolve(f.Name, args, 0)
		if err != nil {
			return "", err
		}
		return env.render(fn(env, obj)), nil
	}
}

// render writes an object the way MPI does: a player as "*Name", anything
// else as its dbref.
func (env *Env) render(obj Ref) string {
	if env.Host.IsPlayer(obj) {
		return "*" + env.Host.Name(obj)
	}
	return "#" + itoa(int(obj))
}

// clock builds the time functions.
func clock(layout string) impl {
	return func(env *Env, _ *Func, _ []string) (string, error) {
		return time.Unix(env.Host.Now(), 0).UTC().Format(layout), nil
	}
}

// resolve reads an object argument at the given position, defaulting to the
// object carrying the message.
func (env *Env) resolve(fn string, args []string, at int) (Ref, error) {
	if at >= len(args) || strings.TrimSpace(args[at]) == "" {
		return env.What, nil
	}
	obj := env.lookup(args[at])
	if !env.Host.Valid(obj) {
		return 0, errf(fn, "Match failed.")
	}
	return obj, nil
}

// lookup resolves a name or a "#123" reference to an object.
func (env *Env) lookup(name string) Ref {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "#") {
		if n, err := strconv.Atoi(name[1:]); err == nil {
			return Ref(n)
		}
	}
	return env.Host.Match(env.Who, name)
}

// mayWrite reports whether this evaluation may change an object.
//
// A blessed property carries wizard permissions; otherwise the permissions
// object must own what is being written.
func (env *Env) mayWrite(obj Ref) bool {
	if env.Blessed {
		return true
	}
	return env.Host.Owner(obj) == env.Host.Owner(env.Perms) || obj == env.Perms
}

// truthy decides whether a value counts as true, which MPI does by treating an
// empty string and a zero as false.
func truthy(s string) bool {
	t := strings.TrimSpace(s)
	return t != "" && t != "0"
}

// boolOf renders a boolean the way MPI does.
func boolOf(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// atoiArg reads a numeric argument.
func atoiArg(fn, s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, errf(fn, "Non-numeric argument.")
	}
	return n, nil
}

func itoa(n int) string { return strconv.Itoa(n) }
