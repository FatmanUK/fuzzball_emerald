package world

import "github.com/FatmanUK/fuzzball_emerald/internal/ref"

// RepairChains checks every container's contents and exits lists
// against what the objects themselves claim, and rebuilds any that
// disagree.
//
// The lists are stored as Fuzzball keeps them, because MUF can
// observe their order. Each object also records its own location,
// which is redundant but cheap, and that redundancy is what makes
// recovery possible: a chain that has been truncated, cycled or
// crossed would otherwise silently orphan everything past the break.
// Rebuilt lists come out in ref order, which is not necessarily the
// order they had, but no object goes missing.
//
// It returns the number of containers that had to be rebuilt.
func (w *World) RepairChains() int {
	// What each container should hold, in ref order.
	wantContents := make(map[ref.Ref][]ref.Ref)
	wantExits := make(map[ref.Ref][]ref.Ref)

	for r := ref.Ref(0); r < w.top; r++ {
		o := w.objs[r]
		if o == nil || o.Location == ref.Nothing {
			continue
		}
		if w.objs[o.Location] == nil {
			// The container is gone; the object is
			// unreachable wherever it thinks it is.
			continue
		}
		if o.Type() == ref.TypeExit {
			wantExits[o.Location] = append(wantExits[o.Location], r)
		} else {
			wantContents[o.Location] = append(wantContents[o.Location], r)
		}
	}

	repaired := 0
	for r := ref.Ref(0); r < w.top; r++ {
		c := w.objs[r]
		if c == nil {
			continue
		}
		fixed := false
		// Compare membership, not order: a healthy chain is
		// in insertion order, which is not ref order, and
		// reordering it on every boot would be a bug of its
		// own.
		if !sameMembers(w.chain(r, false), wantContents[r]) {
			w.relink(wantContents[r], &c.Contents)
			fixed = true
		}
		if !sameMembers(w.chain(r, true), wantExits[r]) {
			w.relink(wantExits[r], &c.Exits)
			fixed = true
		}
		if fixed {
			repaired++
			w.dirty[r] = struct{}{}
		}
	}
	return repaired
}

// relink threads members onto a list, writing the head into head.
func (w *World) relink(members []ref.Ref, head *ref.Ref) {
	*head = ref.Nothing
	// Build backwards so the list comes out in the given order.
	for i := len(members) - 1; i >= 0; i-- {
		o := w.objs[members[i]]
		if o == nil {
			continue
		}
		o.Next = *head
		*head = members[i]
		w.dirty[members[i]] = struct{}{}
	}
}

// sameMembers reports whether two ref lists hold exactly the same
// refs, in any order and with no duplicates in a.
func sameMembers(a, b []ref.Ref) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[ref.Ref]struct{}, len(b))
	for _, r := range b {
		seen[r] = struct{}{}
	}
	for _, r := range a {
		if _, ok := seen[r]; !ok {
			return false
		}
		// Delete as we go, so a chain that visits the same
		// object twice is caught rather than passing on
		// length alone.
		delete(seen, r)
	}
	return len(seen) == 0
}
