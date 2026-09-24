package mpi

import (
	"math/rand"
	"sort"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
)

// The list functions. Two different things are called a list here,
// and the distinction runs through the whole group:
//
//   - A *property* list, named by a string and read off an object — what
//     {list}, {lexec}, {concat}, {rand} and {select} take.
//   - A *text* list, carriage-return delimited, which is what those produce
//     and what the {l...} set operate on.
//
// So {lsort:{list:names}} is the usual shape: read a property list
// into text, then work on the text.
func init() {
	// Reading a property list.
	register("LIST", propList(concatLines))
	register("CONCAT", propList(concatSentences))
	register("LEXEC", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("LEXEC", args, 1)
		if err != nil {
			return "", err
		}
		name := strings.TrimLeft(args[0], "/")
		text := concatList(env.listItems(obj, name), concatTight)
		// Unlike {list}, the text is then evaluated as MPI
		// — which is the whole point: a property list
		// holding a program, run as one.
		return Parse(env, text)
	})

	register("RAND", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("RAND", args, 1)
		if err != nil {
			return "", err
		}
		n := env.listCount(obj, args[0])
		if n <= 0 {
			return "", errf("RAND", "Failed list read.")
		}
		return env.listItem(obj, args[0], rand.Intn(n)+1), nil
	})

	register("SELECT", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("SELECT", args, 2)
		if err != nil {
			return "", err
		}
		want, err := atoiArg("SELECT", args[0])
		if err != nil {
			return "", err
		}
		// Upstream searches down from the index asked for,
		// first a short contiguous run and then through the
		// properties that actually exist, so a sparse list
		// still answers. Both phases look for the same thing
		// — the nearest item at or below the index that has
		// a value — which a plain walk down finds too,
		// since a list is bounded at 512 entries.
		for i := want; i >= 1; i-- {
			if v := env.listItem(obj, args[1], i); v != "" {
				return v, nil
			}
		}
		return "", nil
	})

	// Building and measuring a text list.
	register("MKLIST", func(_ *Env, _ *Func, args []string) (string, error) {
		return strings.Join(args, "\r"), nil
	})
	register("NL", func(*Env, *Func, []string) (string, error) { return "\r", nil })
	register("TAB", func(*Env, *Func, []string) (string, error) { return "\t", nil })

	register("COUNT", func(_ *Env, _ *Func, args []string) (string, error) {
		sep, err := listSep("COUNT", args, 1)
		if err != nil {
			return "", err
		}
		return itoa(countItems(args[0], sep)), nil
	})

	// SUBLIST slices a list, and is also what "{&var}" compiles
	// to — with only the variable's value and nothing to slice
	// by, that value is the answer.
	register("SUBLIST", func(_ *Env, _ *Func, args []string) (string, error) {
		if len(args) < 2 {
			return args[0], nil
		}
		sep, err := listSep("SUBLIST", args, 3)
		if err != nil {
			return "", err
		}
		items := strings.Split(args[0], sep)
		count := countItems(args[0], sep)

		first, err := atoiArg("SUBLIST", args[1])
		if err != nil {
			return "", err
		}
		last := first
		if len(args) > 2 {
			if last, err = atoiArg("SUBLIST", args[2]); err != nil {
				return "", err
			}
		}
		if first == 0 || last == 0 {
			return "", nil
		}
		first = clampIndex(first, count)
		last = clampIndex(last, count)

		// A range given backwards walks the list backwards
		// rather than producing nothing.
		var out []string
		if first <= last {
			out = append(out, items[first-1:last]...)
		} else {
			for i := first; i >= last; i-- {
				out = append(out, items[i-1])
			}
		}
		return strings.Join(out, sep), nil
	})

	// The set operations. Note the two different ideas of "the
	// same line": these three compare case-insensitively and
	// LREMOVE does not, which is upstream's own split between
	// string_prefix and strncmp.
	register("LUNIQUE", func(_ *Env, _ *Func, args []string) (string, error) {
		return strings.Join(dedupeFold(splitLines(args[0])), "\r"), nil
	})
	register("LUNION", func(_ *Env, _ *Func, args []string) (string, error) {
		both := append(splitLines(args[0]), splitLines(args[1])...)
		return strings.Join(dedupeFold(both), "\r"), nil
	})
	register("LCOMMON", func(_ *Env, _ *Func, args []string) (string, error) {
		// Upstream walks the *second* list and keeps what the
		// first also holds, so the result follows the second
		// list's order.
		var out []string
		for _, line := range splitLines(args[1]) {
			if containsFold(splitLines(args[0]), line) &&
				!containsFold(out, line) {
				out = append(out, line)
			}
		}
		return strings.Join(out, "\r"), nil
	})
	register("LREMOVE", func(_ *Env, _ *Func, args []string) (string, error) {
		remove := splitLines(args[1])
		var out []string
		for _, line := range splitLines(args[0]) {
			if containsExact(remove, line) ||
				containsExact(out, line) {
				continue
			}
			out = append(out, line)
		}
		return strings.Join(out, "\r"), nil
	})
	register("LMEMBER", func(_ *Env, _ *Func, args []string) (string, error) {
		sep, err := listSep("LMEMBER", args, 2)
		if err != nil {
			return "", err
		}
		for i, item := range strings.Split(args[0], sep) {
			if item == args[1] {
				return itoa(i + 1), nil
			}
		}
		return "0", nil
	})
	register("LRAND", func(_ *Env, _ *Func, args []string) (string, error) {
		sep, err := listSep("LRAND", args, 1)
		if err != nil {
			return "", err
		}
		items := strings.Split(args[0], sep)
		if args[0] == "" {
			return "", nil
		}
		return items[rand.Intn(len(items))], nil
	})

	register("LSORT", func(env *Env, _ *Func, args []string) (string, error) {
		if len(args) > 1 && len(args) < 4 {
			return "", errf("LSORT", "Takes 1 or 4 arguments.")
		}
		// LSORT is one of the functions whose arguments
		// arrive unevaluated, so that its comparison body can
		// be re-evaluated per pair rather than once up front.
		list, err := Parse(env, args[0])
		if err != nil {
			return "", err
		}
		items := splitLines(list)
		if len(items) > maxListLen {
			return "", errf("LSORT", "Iteration limit exceeded")
		}
		if len(args) == 1 {
			sort.SliceStable(items, func(i, j int) bool {
				return ascii.AlphanumCompare(items[i], items[j]) < 0
			})
			return strings.Join(items, "\r"), nil
		}

		// The four-argument form names two variables and a
		// body: the body is evaluated with each pair bound,
		// and a true result swaps them.
		nameA, err := Parse(env, args[1])
		if err != nil {
			return "", err
		}
		nameB, err := Parse(env, args[2])
		if err != nil {
			return "", err
		}
		if err := env.SetVar(nameA, ""); err != nil {
			return "", err
		}
		defer env.PopVar()
		if err := env.SetVar(nameB, ""); err != nil {
			return "", err
		}
		defer env.PopVar()

		// Upstream's own selection sort, kept rather than
		// replaced: the comparison is arbitrary MPI and need
		// not be a consistent ordering, so which permutation
		// comes out depends on the exact sequence of
		// comparisons made.
		for i := 0; i < len(items); i++ {
			for j := i + 1; j < len(items); j++ {
				if err := env.SetVar(nameA, items[i]); err != nil {
					return "", err
				}
				if err := env.SetVar(nameB, items[j]); err != nil {
					return "", err
				}
				got, err := Parse(env, args[3])
				if err != nil {
					return "", err
				}
				if truthy(got) {
					items[i], items[j] = items[j], items[i]
				}
			}
		}
		return strings.Join(items, "\r"), nil
	})
}

// propList builds {list} and {concat}, which differ only in how they
// join.
func propList(mode int) impl {
	return func(env *Env, f *Func, args []string) (string, error) {
		obj, err := env.resolve(f.Name, args, 1)
		if err != nil {
			return "", err
		}
		return concatList(env.listItems(obj, args[0]), mode), nil
	}
}

// listSep reads an optional separator argument, defaulting to the
// carriage return a text list is delimited by. An explicitly empty
// one is refused, since splitting on it would never terminate.
func listSep(fn string, args []string, at int) (string, error) {
	if at >= len(args) {
		return "\r", nil
	}
	if args[at] == "" {
		return "", errf(fn, "Can't use null separator string.")
	}
	return args[at], nil
}

// countItems counts separator-delimited items. An empty list holds
// none; anything else holds one more than it has separators.
func countItems(list, sep string) int {
	if list == "" {
		return 0
	}
	return strings.Count(list, sep) + 1
}

// clampIndex folds a one-based index into range, counting from the
// end when negative, which is how the slicing functions read their
// bounds.
func clampIndex(i, count int) int {
	if i > count {
		return count
	}
	if i < 0 {
		i += count + 1
	}
	if i < 1 {
		return 1
	}
	return i
}

// dedupeFold keeps the first of each line, comparing
// case-insensitively.
func dedupeFold(lines []string) []string {
	var out []string
	for _, line := range lines {
		if !containsFold(out, line) {
			out = append(out, line)
		}
	}
	return out
}

func containsFold(lines []string, want string) bool {
	for _, line := range lines {
		if ascii.EqualFold(line, want) {
			return true
		}
	}
	return false
}

func containsExact(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}
