package mpi

import "testing"

// The clauses of `isneighbor` and `mesg_local_perms` that one seat
// cannot separate.
//
// `internal/golden/resolveperm_test.go` pins the three wrappers
// against each other and four of the five local routes, but two
// things are structurally out of its reach:
//
//   - **`isneighbor`'s second and third clauses** — "d1 is inside
//     d2" and "d2 is inside d1" — because the first clause of
//     `mesg_local_perms` passes whenever the target sits somewhere
//     the permissions object's owner owns, which is exactly when
//     those hold for a player who owns itself.
//   - **`mesg_local_perms`'s two `isneighbor` calls**, one against
//     the permissions object and one against the reader, because
//     PARSEPROP hands one object in as both `what` and `perms`, so
//     the two arguments coincide.
//   - **`mesg_read_perms`'s `obj == 0`**, because `#1` owns `#0` in
//     every world the harness builds, so the ownership clause
//     answers first.
//
// So they are tested directly, against a world small enough to
// arrange.

// permHost is a world with a settable shape.
type permHost struct {
	*stubHost
	loc   map[Ref]Ref
	own   map[Ref]Ref
	rooms map[Ref]bool
	locks map[Ref]bool
}

func newPermHost() *permHost {
	return &permHost{
		stubHost: newStub(),
		loc:      map[Ref]Ref{},
		own:      map[Ref]Ref{},
		rooms:    map[Ref]bool{},
		locks:    map[Ref]bool{},
	}
}

func (h *permHost) Location(obj Ref) Ref {
	if l, ok := h.loc[obj]; ok {
		return l
	}
	return -1
}

func (h *permHost) Owner(obj Ref) Ref {
	if o, ok := h.own[obj]; ok {
		return o
	}
	return obj
}

func (h *permHost) TypeName(obj Ref) string {
	if h.rooms[obj] {
		return "Room"
	}
	return "Thing"
}

func (h *permHost) ReadLockPasses(_ int, _, obj Ref) bool {
	return h.locks[obj]
}

// TestIsNeighborFourClauses walks each of upstream's four in turn,
// with the three that do not apply arranged to fail.
func TestIsNeighborFourClauses(t *testing.T) {
	h := newPermHost()
	// #1 and #2 lie in #10; #3 lies in #1; #4 is a room holding
	// #5; #9 is alone in #11.
	h.loc[1], h.loc[2] = 10, 10
	h.loc[3] = 1
	h.rooms[4] = true
	h.loc[5] = 4
	h.loc[9] = 11
	// #6 is a room lying inside #12, and #40 and #41 are two
	// rooms in one place: both shapes exist only to pin the "is
	// not a room" guards, which are what stop a room being
	// everybody's neighbour.
	h.rooms[6] = true
	h.loc[6] = 12
	h.rooms[40], h.rooms[41] = true, true
	h.loc[40], h.loc[41] = 0, 0
	env := &Env{Who: 1, What: 1, Perms: 1, Host: h}

	cases := []struct {
		name string
		a, b Ref
		want bool
	}{
		{"the same object", 1, 1, true},
		{"d1 is inside d2", 3, 1, true},
		{"d2 is inside d1", 1, 3, true},
		{"both in one place", 1, 2, true},
		{"a room containing it", 5, 4, true},
		{"nothing in common", 1, 9, false},
		// A room is excluded from the test it would pass
		// trivially: #4 holds #5, so "both in one place" must
		// not fire on Location(#4) == Location(#5).
		{"a room is not its own neighbour by location",
			4, 9, false},
		// A room inside something is not that thing's
		// neighbour, in either direction...
		{"a room inside something", 6, 12, false},
		{"something holding a room", 12, 6, false},
		// ...and two rooms in one place are not neighbours
		// either, which is the third clause's pair of guards.
		{"two rooms in one place", 40, 41, false},
	}
	for _, c := range cases {
		if got := env.isNeighbor(c.a, c.b); got != c.want {
			t.Errorf("%s: isNeighbor(%d, %d) = %v, "+
				"want %v", c.name, c.a, c.b,
				got, c.want)
		}
	}
}

// TestLocalPermsSeparatesPermsFromReader is the pair PARSEPROP
// collapses: `mesg_local_perms` asks isneighbor twice, once about the
// permissions object and once about the reader, and either alone is
// enough.
func TestLocalPermsSeparatesPermsFromReader(t *testing.T) {
	// The reader is #1 in room #10; the permissions object is #2
	// in room #20. Both rooms belong to #99, so the first clause
	// cannot answer, and nothing is locked.
	base := func() *permHost {
		h := newPermHost()
		h.rooms[10], h.rooms[20] = true, true
		h.own[10], h.own[20] = 99, 99
		h.loc[1], h.loc[2] = 10, 20
		h.own[1], h.own[2] = 1, 2
		return h
	}

	// A thing beside the reader and not beside the permissions
	// object: the reader's own isneighbor call is the only route.
	h := base()
	h.loc[7], h.own[7] = 10, 77
	env := &Env{Who: 1, What: 2, Perms: 2, Host: h}
	if !env.localPerms(7) {
		t.Error("a thing beside the reader should be local")
	}

	// And one beside the permissions object instead.
	h = base()
	h.loc[8], h.own[8] = 20, 77
	env = &Env{Who: 1, What: 2, Perms: 2, Host: h}
	if !env.localPerms(8) {
		t.Error("a thing beside the trigger should be local")
	}

	// Neither, and nothing else passes either.
	h = base()
	h.rooms[30] = true
	h.own[30] = 99
	h.loc[9], h.own[9] = 30, 77
	env = &Env{Who: 1, What: 2, Perms: 2, Host: h}
	if env.localPerms(9) {
		t.Error("a thing in a third room should not be local")
	}

	// The fourth clause is the read lock on the **owner of the
	// object's location**, not on the object or the location --
	// which reads like a slip in the C and is reproduced.
	h = base()
	h.rooms[30] = true
	h.own[30] = 99
	h.loc[9], h.own[9] = 30, 77
	h.locks[99] = true
	env = &Env{Who: 1, What: 2, Perms: 2, Host: h}
	if !env.localPerms(9) {
		t.Error("a lock on the location's owner should pass")
	}

	// Locking the object itself passes too, but through
	// mesg_read_perms at the end rather than through that clause.
	h = base()
	h.rooms[30] = true
	h.own[30] = 99
	h.loc[9], h.own[9] = 30, 77
	h.locks[9] = true
	env = &Env{Who: 1, What: 2, Perms: 2, Host: h}
	if !env.localPerms(9) {
		t.Error("a lock on the object should pass")
	}
}

// TestReadPermsAllowsZero is the clause no fixture can reach: #0 is
// readable by everybody regardless of who owns it.
func TestReadPermsAllowsZero(t *testing.T) {
	h := newPermHost()
	h.own[0] = 99
	h.own[2] = 2
	env := &Env{Who: 1, What: 2, Perms: 2, Host: h}
	if !env.readPerms(0) {
		t.Error("#0 should be readable by anybody")
	}
	h.own[5] = 99
	if env.readPerms(5) {
		t.Error("another of #99's objects should not be")
	}
}
