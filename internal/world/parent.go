package world

import "github.com/FatmanUK/fuzzball_emerald/internal/ref"

// MaxParentDepth is MAX_PARENT_DEPTH (config.h:118), the bound
// upstream puts on the two loop checks that walk the environment.
//
// It is not envDepth, which bounds the *property* walk in env.go and
// is this server's own invention: a property search that goes 128
// levels out has found nothing useful, where a loop check has to be
// right about a chain it may have to refuse.
const MaxParentDepth = 256

// EnvDistance is db.c:2327's env_distance: how far `from` is from
// `to`'s *parent*, counted in getparent hops. It is choose_thing's
// third tie-break.
//
// Two things about it are worth knowing before using the number for
// anything but a comparison.
//
// It measures to `to`'s parent rather than to `to`, so an object
// sitting directly in `from` is at distance zero — which is what
// makes it a sensible answer to "which of these two did they mean".
//
// And when `from` is **not** a descendant of that parent at all, the
// walk runs off the top of the world and the answer is the number of
// hops from `from` to #0. Two unrelated objects therefore compare by
// how deep the *searcher* is rather than by how far either of them
// is, which reads like an oversight and is what upstream does.
func (w *World) EnvDistance(from, to ref.Ref) int {
	dest := w.Parent(to)
	if from == dest {
		return 0
	}
	distance := 0
	// The walk is bounded where upstream's is not. Parent already
	// collapses a detected cycle to #0, which terminates for
	// every graph this server can build, so this is insurance
	// against a damaged one rather than a behaviour.
	for i := 0; i < MaxParentDepth; i++ {
		distance++
		from = w.Parent(from)
		if from == dest || from == ref.Nothing {
			break
		}
	}
	return distance
}
