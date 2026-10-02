package match

import (
	"math/rand"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// choose_thing (match.c:130) decides which of two candidates the
// searcher meant, and it is the last word on an **exact**-match tie
// and nothing else.
//
// That narrowness is the thing to know. Upstream calls it from
// exactly two places — match_contents (match.c:471), for two
// objects in one container with the same name, and match_exits
// (:636), for two exits at the same priority with the same longest
// alias — and both are resolving `exact_match`. A *partial* match
// never comes here: it overwrites last_match and bumps the count, so
// two half-matching names are reported as ambiguous rather than
// picked between.
//
// Emerald used to take the later of two exact matches and nothing
// else, which is a fifth answer upstream never gives.
//
// The four tie-breaks, in order:
//
//  1. Either being NOTHING makes it easy.
//  2. A **preferred type**, which is whatever the command's own
//     init_match asked for. Six commands want an exit, five a
//     program, five a room, three a player and three a thing.
//  3. **check_keys**, set by three call sites and no more: do_move's
//     direction and both of do_get's matches. An object the searcher
//     could actually use beats one locked against them — so walking
//     into a room with two doors of the same name takes the one that
//     will open.
//  4. **Environment distance**, which prefers the nearer of the two.
//
// And then it tosses a coin. That is upstream's own last resort and
// is reproduced rather than settled deterministically: a world that
// has two identically-named things in one room gets an arbitrary
// answer either way, and making it stable here would be inventing a
// behaviour programs could come to rely on. **No golden case can pin
// it**, which is why the clone suite clones each thing once.

// PreferType sets choose_thing's preferred type, which is what a
// command's init_match passes. Without it no type is preferred, which
// is upstream's NOTYPE.
func (m *Matcher) PreferType(t ref.ObjType) *Matcher {
	m.preferred = t
	m.hasPreferred = true
	return m
}

// Usable is choose_thing's check_keys: given two exact matches,
// prefer one the searcher can actually use.
//
// The test is could_doit, which needs the lock evaluator, so it
// arrives as a callback the way every other cross-layer question in
// this server does. Passing nil is the same as not calling this.
func (m *Matcher) Usable(fn func(ref.Ref) bool) *Matcher {
	m.couldDoit = fn
	return m
}

// chooseThing resolves a tie between two exact matches.
func (m *Matcher) chooseThing(a, b ref.Ref) ref.Ref {
	if a == ref.Nothing {
		return b
	}
	if b == ref.Nothing {
		return a
	}

	// A virtual ref — HOME, NIL — has no type, no lock and no
	// place in the environment, so there is nothing to compare.
	// Upstream cannot reach this: the two call sites only ever
	// offer it real objects.
	oa, ob := m.w.Get(a), m.w.Get(b)
	if oa == nil || ob == nil {
		if oa != nil {
			return a
		}
		return b
	}

	if m.hasPreferred {
		if oa.Type() == m.preferred {
			if ob.Type() != m.preferred {
				return a
			}
		} else if ob.Type() == m.preferred {
			return b
		}
	}

	if m.couldDoit != nil {
		hasA, hasB := m.couldDoit(a), m.couldDoit(b)
		if hasA && !hasB {
			return a
		}
		if hasB && !hasA {
			return b
		}
		// Both or neither: fall through.
	}

	switch da, db := m.w.EnvDistance(m.from, a),
		m.w.EnvDistance(m.from, b); {
	case da < db:
		return a
	case db < da:
		return b
	}

	if rand.Intn(2) == 0 {
		return a
	}
	return b
}
