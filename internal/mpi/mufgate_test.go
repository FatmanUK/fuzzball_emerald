package mpi

import (
	"strings"
	"testing"
)

// mufHost answers {muf}'s three permission questions and nothing
// else: a program at a mucker level this test chooses, LINK_OK so the
// ownership gate above the floor passes.
type mufGateHost struct {
	*stubHost
	mlev int
}

func (h *mufGateHost) TypeName(obj Ref) string {
	if obj == 9 {
		return "Program"
	}
	return h.stubHost.TypeName(obj)
}

func (h *mufGateHost) HasFlag(_ Ref, flag string) bool {
	return flag == "link_ok"
}

func (h *mufGateHost) MLevel(Ref) int { return h.mlev }

func (h *mufGateHost) RunMUF(int, Ref, Ref, Ref, string,
	string) (string, error) {
	return "it ran", nil
}

// TestMufListenerFloor is `mfuns2.c:2671`, which had no port: a
// listener or a lock may only run a program at **mucker 3 or above**,
// so a mortal's `_listen` could not reach a mucker-1 program. It is
// not oracle-reachable from one seat — a listener needs somebody to
// speak and a lock needs a lock to be tested — so it is here.
func TestMufListenerFloor(t *testing.T) {
	const denied = "Permission denied."
	for _, tc := range []struct {
		name string
		typ  MesgType
		mlev int
		want string
	}{
		{"an ordinary call at 1", 0, 1, "it ran"},
		{"a listener at 1", Listener, 1, denied},
		{"a listener at 3", Listener, 3, "it ran"},
		{"a lock at 2", Lock, 2, denied},
		{"a lock at 4", Lock, 4, "it ran"},
	} {
		h := &mufGateHost{stubHost: &stubHost{},
			mlev: tc.mlev}
		env := &Env{Who: 1, What: 9, Perms: 9, Host: h,
			Type: tc.typ}
		got, err := Parse(env, "{muf:#9,}")
		if err != nil {
			got = err.Error()
		}
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.name, got,
				tc.want)
		}
	}
}

// TestIsAncestorIsBounded is the walk {otell}'s naming rule needs.
// Upstream's `isancestor` is unbounded and safe only because
// `getparent` collapses a cycle it detects; this one is bounded as
// well, because Emerald is handed dumps it did not write.
func TestIsAncestorIsBounded(t *testing.T) {
	env := &Env{Who: 1, What: 2, Perms: 2,
		Host: &loopHost{stubHost: &stubHost{}}}
	// Two objects in a ring that is nobody's ancestor: the walk
	// has to give up rather than spin.
	if env.isAncestor(99, 5) {
		t.Error("a ring reported an ancestor")
	}
	// ...and the obvious answers still hold.
	if !env.isAncestor(5, 5) {
		t.Error("an object is its own ancestor")
	}
}

// loopHost's environment is a two-object ring.
type loopHost struct{ *stubHost }

func (h *loopHost) Parent(obj Ref) Ref {
	if obj == 5 {
		return 6
	}
	return 5
}

// TestAwakeCountsDescriptors is the other half of {awake}: it answers
// PLAYER_DESCRCOUNT, so a player connected twice reads "2". A
// single-seat oracle cannot tell that from a boolean — both say "1"
// — so the count is asserted here.
func TestAwakeCountsDescriptors(t *testing.T) {
	h := &twiceHost{stubHost: &stubHost{}}
	env := &Env{Who: 1, What: 2, Perms: 2, Host: h}
	got, err := Parse(env, "{awake:me}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "2" {
		t.Errorf("{awake:me} = %q, want \"2\"", got)
	}
}

// twiceHost has one player connected twice.
type twiceHost struct{ *stubHost }

func (h *twiceHost) DescrCount(Ref) int { return 2 }
