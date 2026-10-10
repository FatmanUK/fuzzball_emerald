package mpi

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
)

// impl is one MPI function's implementation.
type impl func(env *Env, fn *Func, args []string) (string, error)

// impls holds the implementations, keyed by the table's name. A
// function in the table with no entry here reports itself
// unimplemented rather than silently producing nothing.
var impls = map[string]impl{}

// register attaches an implementation to a name from the generated
// table.
//
// It panics on a **duplicate** as well as on an unknown name, and the
// duplicate check is the one that earned its place: SUBLIST was
// registered twice, a stub here and the real thing in impl_list.go,
// and the real one won only because Go runs a package's init
// functions in filename order and "." sorts before "_". Renaming
// either file would have broken {sublist:...} silently. A panic at
// init is the right severity for a programming error that no test
// could otherwise see.
func register(name string, fn impl) {
	if _, ok := functions[name]; !ok {
		panic("mpi: registering an unknown function " + name)
	}
	if _, dup := impls[name]; dup {
		panic("mpi: " + name + " registered twice")
	}
	impls[name] = fn
}

// stubs records the functions that are *registered* but not
// implemented, keyed by name with the reason each is still a stub.
//
// It exists for the reason internal/muf's own does: a stub is
// registered like anything else, so len(impls) counted it and the
// 140-of-140 figure never moved. MPI's SUBLIST stub was dead code
// that happened to be shadowed by the real implementation, and
// nothing about the count would have changed had it not been.
var stubs = map[string]string{}

// registerStub attaches an implementation that aborts, and counts it
// as a stub in the same call so the two cannot come apart. Use it
// instead of register for anything not actually implemented.
//
// Currently unused — Stubs() is empty and the coverage test asserts
// as much — which is the point: the next stub is counted when it is
// written rather than when somebody audits a claim nobody could
// check.
func registerStub(name, reason string) {
	register(name, stubImpl(name))
	stubs[name] = reason
}

// stubImpl is the abort a stub answers with. The reason stays out of
// the message, because MPI errors are shown to players; Stubs() is
// where a maintainer reads it.
func stubImpl(name string) impl {
	return func(*Env, *Func, []string) (string, error) {
		return "", errf(name, "Not implemented yet.")
	}
}

// Stubs lists the functions registered but not implemented, with the
// reason each is still a stub. The map is a copy, so a caller cannot
// quietly empty the real one.
func Stubs() map[string]string {
	out := make(map[string]string, len(stubs))
	for name, why := range stubs {
		out[name] = why
	}
	return out
}

// Implemented is how many functions actually have implementations:
// the registered ones, less any that are only stubs.
func Implemented() int { return len(impls) - len(stubs) }

func init() {
	// Text.
	register("LIT", func(_ *Env, _ *Func, args []string) (string, error) {
		// LIT returns its argument without evaluating it,
		// which is why its table entry does not parse.
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
	register("MIDSTR", midstr)
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
		n := atoiArg(args[0])
		if n < 0 {
			n = -n
		}
		return itoa(n), nil
	})
	register("SIGN", func(_ *Env, _ *Func, args []string) (string, error) {
		n := atoiArg(args[0])
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

	// Comparison and logic. These return "1" for true and "" for
	// false, which is what MPI treats as a boolean.
	register("EQ", compare(func(c int) bool { return c == 0 }))
	register("NE", compare(func(c int) bool { return c != 0 }))
	register("GT", compare(func(c int) bool { return c > 0 }))
	register("LT", compare(func(c int) bool { return c < 0 }))
	register("GE", compare(func(c int) bool { return c >= 0 }))
	register("LE", compare(func(c int) bool { return c <= 0 }))
	register("NOT", func(_ *Env, _ *Func, args []string) (string, error) {
		return boolOf(!truthy(args[0])), nil
	})

	// {and} and {or} do not parse their arguments up front, so
	// they can stop as soon as the answer is known.
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
	register("NAME", objectName(true))
	register("PROP", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("PROP", args, 1)
		if err != nil {
			return "", err
		}
		// {prop} searches outwards through the environment;
		// {prop!} is the form that looks only at the object
		// named.
		v, _, err := env.getProp("PROP", obj, args[0])
		return v, err
	})
	register("STORE", func(env *Env, _ *Func, args []string) (string, error) {
		// "{store:value,property,object}" Strict: blessed, or
		// the same owner. The four functions that *write* all
		// use this wrapper, and none of them had it.
		obj, err := env.resolveStrict("STORE", args, 2)
		if err != nil {
			return "", err
		}
		if !env.safePutProp(obj, args[1], args[0], true) {
			return "", errf("STORE", "Permission denied.")
		}
		// {store} answers with what it wrote, which nothing
		// else in the property set does.
		return args[0], nil
	})
	// {loc} is local and {owner} is raw, which is why they cannot
	// share a factory: anybody may ask who owns an object, and
	// only a neighbour may ask where it is.
	register("LOC", func(env *Env, _ *Func,
		args []string) (string, error) {

		obj, err := env.resolveLocal("LOC", args, 0)
		if err != nil {
			return "", err
		}
		return env.render(env.Host.Location(obj)), nil
	})
	register("OWNER", func(env *Env, _ *Func,
		args []string) (string, error) {

		// "Failed match.", not "Match failed." -- mfn_owner
		// is one of the three that reverse the words.
		obj, err := env.resolveMsg(matchRaw, "OWNER", args, 0,
			"Failed match.", "Permission denied.")
		if err != nil {
			return "", err
		}
		return env.render(env.Host.Owner(obj)), nil
	})
	register("AWAKE", func(env *Env, _ *Func,
		args []string) (string, error) {

		// Local, and every failure answers "0" rather than
		// aborting -- so {awake} cannot be used to probe
		// whether a distant object exists.
		obj, fail := env.resolveAs(matchLocal, args, 0)
		if fail != resolveOK {
			return "0", nil
		}
		// A ZOMBIE thing is redirected to its **owner**, so
		// asking whether a puppet is awake asks whether the
		// person behind it is; anything else that is not a
		// player is "0". This read the thing, and answered a
		// boolean where upstream answers PLAYER_DESCRCOUNT --
		// so a player connected twice reads "2".
		if env.Host.TypeName(obj) == "Thing" &&
			env.Host.HasFlag(obj, "zombie") {
			obj = env.Host.Owner(obj)
		} else if env.Host.TypeName(obj) != "Player" {
			return "0", nil
		}
		return itoa(env.Host.DescrCount(obj)), nil
	})
	register("ISTYPE", func(env *Env, _ *Func,
		args []string) (string, error) {

		want := strings.TrimSpace(args[len(args)-1])
		// A failed match is the type "Bad", and a *refusal*
		// is too when that is what was asked for -- which
		// upstream's own TODO calls a bug and asks to have
		// removed, because "Bad" should not bypass a
		// permission check. Reproduced, with the comment.
		obj, fail := env.resolveAs(matchLocal, args, 0)
		bad := ascii.EqualFold(want, "Bad")
		if fail == resolveUnknown || (fail == resolveDenied &&
			bad) {
			return boolOf(bad), nil
		}
		if fail == resolveDenied {
			return "", errf("TYPE", "Permission Denied.")
		}
		return boolOf(ascii.EqualFold(want,
			env.Host.TypeName(obj))), nil
	})
	// {isdbref} is **not** a match: mfn_isdbref (`mfuns.c:2235`)
	// demands a literal "#N" and answers "0" for anything else,
	// so "{isdbref:me}" is false. This resolved names, which made
	// it a way to ask whether an object existed under any
	// spelling.
	register("ISDBREF", func(env *Env, _ *Func, args []string) (string, error) {
		p := strings.TrimSpace(args[0])
		if len(p) < 2 || p[0] != '#' {
			return "0", nil
		}
		n, err := strconv.Atoi(p[1:])
		if err != nil {
			return "0", nil
		}
		return boolOf(env.Host.Valid(Ref(n))), nil
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
		if err := env.BindVar("WITH", args[0],
			value); err != nil {
			return "", err
		}
		defer env.PopVar()

		// **Only the last body's result is returned**, not
		// all of them joined. mfn_with (mfuns2.c:1039) parses
		// each remaining argument into one reused buffer and
		// returns the pointer afterwards, so every body but
		// the last is evaluated for its side effects and then
		// discarded: "{with:n,5,a,b,c}" is "c".
		//
		// This server concatenated, answering "abc". {for}
		// already had the right shape for the same reason —
		// see its out.Reset() — so the two disagreed about
		// the same question.
		//
		// Min is 3, so there is always at least one body; a
		// bodyless {with} would return upstream's parsed
		// *name*, which cannot be reached.
		var out string
		for _, body := range args[2:] {
			got, err := Parse(env, body)
			if err != nil {
				return "", err
			}
			out = got
		}
		return out, nil
	})
	register("SET", func(env *Env, _ *Func, args []string) (string, error) {
		// Only an already-bound variable may be set, and the
		// new value is also what the call produces — so
		// "{set:n,5}{&n}" reads "55", not "5".
		//
		// It assigns rather than binds: upstream writes
		// through get_mvar's pointer, so the value changed is
		// the innermost binding's and nothing is pushed.
		if !env.AssignVar(args[0], args[1]) {
			return "", errf("SET",
				"No such variable currently defined.")
		}
		return args[1], nil
	})

	register("TELL", func(env *Env, _ *Func, args []string) (string, error) {
		// A listener may only speak through a room. Upstream
		// tests the object carrying the message, not the
		// target: a thing that hears something must not be
		// able to send a private message to anyone it likes.
		//
		// It runs **after** the match, which is the order
		// mfn_tell writes its aborts in: a listener naming an
		// object it may not reach is told so before it is
		// told it is a listener.
		target := env.Who
		if len(args) > 1 {
			var err error
			target, err = env.resolveLocal("TELL",
				args, 1)
			if err != nil {
				return "", err
			}
		}
		if env.Type.Has(Listener) &&
			env.Host.TypeName(env.What) != "Room" {
			return "", errf("TELL", "Permission denied.")
		}
		room := env.Host.Location(env.Who)
		mark := env.tellPrefix(target)
		named := ""
		if env.tellNames(target, args[0]) {
			named = env.speakerPrefix(args[0])
		}
		for _, line := range splitLinesCR(args[0], true) {
			env.Host.NotifyFrom(env.Who, target, room,
				mark+named+line)
		}
		// mfn_tell returns its **message**, not the empty
		// string, so {tell} inside a larger expression
		// contributes the text it sent.
		return args[0], nil
	})

	// Time.
	register("TIME", clock("15:04:05"))
	register("DATE", clock("Mon Jan 02 2006"))
	register("SECS", func(env *Env, _ *Func, _ []string) (string, error) {
		return itoa(int(env.Host.Now())), nil
	})
	register("CONVSECS", func(env *Env, _ *Func, args []string) (string, error) {
		n := atoiArg(args[0])
		return time.Unix(int64(n), 0).UTC().Format("Mon Jan 02 15:04:05 2006"), nil
	})

	register("VERSION", func(*Env, *Func, []string) (string, error) {
		return "Muck2.2fb7.21", nil
	})
}

// midstr is mfn_midstr (mfuns2.c:2897), and its second number is a
// **position**, not a length.
//
// This server read it as a length, which made every three-argument
// call wrong: "{midstr:hello,2,4}" is "ell" upstream and was "ello"
// here. Two more behaviours were missing with it.
//
// A **negative** position counts from the end, by adding len + 1 —
// so "{midstr:hello,-2,-1}" is "lo".
//
// And when the second position is **lower** than the first, upstream
// walks backwards and returns the span **reversed**:
// "{midstr:hello,4,2}" is "lle". That reads like a bug and is not —
// the C has two explicit loops for it — so it is reproduced.
//
// The clamping order is load-bearing and is upstream's: a position of
// zero returns the empty string before any clamping happens, and only
// then is a position above the length pulled down, a negative one
// wrapped, and anything still below one raised.
func midstr(_ *Env, _ *Func, args []string) (string, error) {
	s := args[0]
	n := len(s)
	pos1 := atoiArg(args[1])
	pos2 := pos1
	if len(args) > 2 {
		pos2 = atoiArg(args[2])
	}

	// clamp is each position's four tests, in upstream's order.
	// The zero case is reported separately because it returns
	// from the function rather than settling on a position.
	clamp := func(p int) (int, bool) {
		if p == 0 {
			return 0, false
		}
		if p > n {
			p = n
		}
		if p < 0 {
			p += n + 1
		}
		if p < 1 {
			p = 1
		}
		return p, true
	}
	var ok bool
	if pos1, ok = clamp(pos1); !ok {
		return "", nil
	}
	if pos2, ok = clamp(pos2); !ok {
		return "", nil
	}

	// An empty input leaves both positions clamped to 1, and
	// upstream then copies the string's own NUL terminator —
	// which reads back as the empty string. Indexing s[0] here
	// would panic instead.
	if n == 0 {
		return "", nil
	}

	var b strings.Builder
	if pos2 >= pos1 {
		for i := pos1; i <= pos2; i++ {
			b.WriteByte(s[i-1])
		}
	} else {
		for i := pos1; i >= pos2; i-- {
			b.WriteByte(s[i-1])
		}
	}
	return b.String(), nil
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
		total := atoiArg(args[0])
		for _, a := range args[1:] {
			total = op(total, atoiArg(a))
		}
		return itoa(total), nil
	}
}

// foldDiv builds division and modulus, which yield zero rather than
// failing when the divisor is zero, as MUF's do.
func foldDiv(mod bool) impl {
	return func(_ *Env, fn *Func, args []string) (string, error) {
		total := atoiArg(args[0])
		for _, a := range args[1:] {
			n := atoiArg(a)
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

// stepBy builds {inc} and {dec}, which are mfn_inc (mfuns.c:2279) and
// mfn_dec (:2318) — and are **variable** operations, not
// arithmetic.
//
// The first argument is a variable *name*. Upstream looks it up with
// get_mvar, refuses "No such variable currently defined." when there
// is none, adds or subtracts an optional amount, **writes the result
// back into the variable**, and returns it. So
// "{with:n,5,{inc:n}{inc:n}{&n}}" counts up: each call changes n.
//
// Emerald read the first argument as a number instead, so "{inc:abc}"
// answered 1 and "{inc:5}" answered 6 — neither of which upstream
// can produce, since the only way to reach the arithmetic at all is
// to name a bound variable.
//
// Upstream's doc comment for mfn_dec says "The variable is not
// updated." The code does update it (strcpyn into get_mvar's pointer,
// :2329), and the code is what the oracle agrees with.
func stepBy(sign int) impl {
	return func(env *Env, fn *Func,
		args []string) (string, error) {

		cur, ok := env.Var(args[0])
		if !ok {
			return "", errf(fn.Name,
				"No such variable currently defined.")
		}
		by := 1
		if len(args) > 1 {
			by = atoiArg(args[1])
		}
		out := itoa(atoiArg(cur) + sign*by)
		// Assign, not bind: the write lands on the binding
		// env.Var just read, which is the innermost one.
		env.AssignVar(args[0], out)
		return out, nil
	}
}

// extreme builds {max} and {min}. extreme builds {max} and {min},
// which are mfn_max (mfuns.c:2176) and mfn_min (:2146) and are not
// arithmetic at all.
//
// Both take exactly two arguments, decide with msgCompare, and return
// the chosen argument's **text** rather than a number — so
// "{max:abc,2}" is "abc". This used to read both as integers and fold
// over every argument, which got three things wrong at once: the
// comparison, the returned value, and the arity.
func extreme(wantMax bool) impl {
	return func(_ *Env, _ *Func, args []string) (string, error) {
		// Written as upstream's two tests rather than one
		// expression, because they agree on a **tie**: max
		// asks `>= 0` and min asks `<= 0`, so when the two
		// arguments compare equal both return the *first*. A
		// single symmetric formula gets min wrong there,
		// which is what "{min:ABC,abc}" caught — strcasecmp
		// makes those equal, and upstream answers "ABC".
		c := msgCompare(args[0], args[1])
		if wantMax {
			if c >= 0 {
				return args[0], nil
			}
			return args[1], nil
		}
		if c <= 0 {
			return args[0], nil
		}
		return args[1], nil
	}
}

// isNumber is fbstrings.c's number(): leading whitespace, an optional
// sign, then digits and nothing else. So "12abc" is not a number even
// though atoi reads 12 from it, and neither is " 7 ", because the
// trailing space is not a digit.
//
// internal/tune has the same function for the same reason. It is
// duplicated rather than shared because the alternative is a
// dependency from internal/mpi on internal/tune for nine lines.
func isNumber(s string) bool {
	s = strings.TrimLeft(s, " \t\r\n\v\f")
	if s != "" && (s[0] == '+' || s[0] == '-') {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// msgCompare is mfuns.c:1810's msg_compare, which is how **all
// eight** of MPI's comparisons decide: {eq}, {ne}, {gt}, {lt}, {ge},
// {le}, {max} and {min}.
//
// Two numbers compare as numbers; anything else compares as text,
// **case-insensitively** — it is strcasecmp, so "ABC" and "abc" are
// equal. Both arguments must be non-empty for the numeric path, so an
// empty string always compares as text.
//
// The numeric path returns a sign rather than upstream's `atoi(s1) -
// atoi(s2)`. Every caller tests only the sign, and the subtraction
// overflows for large values — which is undefined in C and would
// wrap here.
func msgCompare(a, b string) int {
	if a != "" && b != "" && isNumber(a) && isNumber(b) {
		x, y := atoiArg(a), atoiArg(b)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	}
	return ascii.Compare(a, b)
}

// compare builds the eight comparisons on msgCompare, which decides
// numerically only when both arguments are numbers.
//
// The old version read both with strconv.Atoi over a TrimSpace'd
// argument and fell back to strings.Compare. Both halves diverged:
// the trim made " 7 " numeric where number() rejects it, and
// strings.Compare is case-sensitive where strcasecmp is not.
func compare(ok func(int) bool) impl {
	return func(_ *Env, _ *Func, args []string) (string, error) {
		return boolOf(ok(msgCompare(args[0], args[1]))), nil
	}
}

// shortCircuit builds {and} and {or}, which stop as soon as the
// answer is settled rather than evaluating every argument.
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
	return func(env *Env, f *Func,
		args []string) (string, error) {

		// {left:string[,fieldwidth[,padstr]]}
		//
		// The fieldwidth is optional, and all three of these
		// take Min: 1 — so indexing args[1] unconditionally
		// **panicked** on the one-argument form, which is
		// legal and which a description can easily contain.
		// Upstream falls back to the descriptor's reported
		// width and then to 78 (mfuns.c:3833).
		width := 78
		if len(args) > 1 {
			width = atoiArg(args[1])
		} else if w := env.Host.DescrWidth(env.Descr); w > 0 {
			width = w
		}
		// This is the range check upstream relies on instead
		// of refusing a non-numeric argument, and it was
		// missing: {left:hi,9999999} really did build a
		// ten-million-character string.
		if width > bufferLen-1 {
			return "", errf(f.Name, "Fieldwidth too big.")
		}
		// An explicitly empty pad string is an abort, not a
		// silent space. Treating it as a space meant
		// {left:hi,5,} answered where upstream refuses.
		fill := " "
		if len(args) > 2 {
			fill = args[2]
		}
		if fill == "" {
			return "", errf(f.Name, "Null pad string.")
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

// render writes an object the way MPI does: a player as "*Name",
// anything else as its dbref.
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

// truthy decides whether a value counts as true, which MPI does by
// treating an empty string and a zero as false.
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

// atoiArg reads a numeric argument the way C's atoi does, which is
// the only way MPI ever reads one: leading whitespace is skipped, an
// optional sign is accepted, digits are taken until the first
// character that is not one, and anything unparseable is **zero**.
//
// It used to abort with "Non-numeric argument." That string does not
// exist anywhere in Fuzzball — `grep -rc "Non-numeric"
// fuzzball/src/` finds nothing, and the full list of upstream's MPI
// aborts has no numeric-parse error of any kind. Every mfun reads its
// numbers with a bare atoi and carries on.
//
// The difference is not academic. MPI is evaluated over descriptions
// and succeed/fail messages, which routinely receive whatever a
// player typed, so an invented abort turned silent upstream behaviour
// into a visible error across about thirty functions. Where upstream
// does refuse, it refuses on *range* after parsing — "Out of range
// time argument.", "Too many dice!", "Fieldwidth too big." — and
// those checks are each function's own.
func atoiArg(s string) int {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' ||
		s[i] == '\n' || s[i] == '\r' || s[i] == '\v' ||
		s[i] == '\f') {
		i++
	}
	start := i
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	digits := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == digits {
		return 0
	}
	n, err := strconv.Atoi(s[start:i])
	if err != nil {
		// Out of int range, which atoi leaves undefined.
		// Saturating is the one answer that cannot be
		// mistaken for a small number.
		if s[start] == '-' {
			return math.MinInt
		}
		return math.MaxInt
	}
	return n
}

func itoa(n int) string { return strconv.Itoa(n) }
