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

// TestBlessedMacroIsAcceptedFromAnyOwner is `safegetprop_limited`'s
// `|| *blessed` (`msgparse.c:286`), the clause the resolver sweep
// left out: a macro directory on an object the caller does not own
// counts when the property is **blessed**, which is how a world
// publishes macros from a room nobody owns personally.
//
// It is not oracle-reachable with one wizard, who owns everything in
// the fixture and so passes the ownership test without the blessing
// being consulted at all.
func TestBlessedMacroIsAcceptedFromAnyOwner(t *testing.T) {
	for _, blessed := range []bool{false, true} {
		h := &macroHost{stubHost: &stubHost{
			props:   map[string]string{},
			blessed: map[string]bool{},
		}}
		// The macro sits on **#6**, one hop out of the
		// trigger: the first step looks at the trigger's
		// owner (#9) and finds nothing, the third looks at #0
		// and finds nothing, so the limited walk in the
		// middle is the only thing that can accept it -- and
		// #6 belongs to #8 rather than #9.
		h.props["6/_msgmacs/greet"] = "hi"
		h.blessed["_msgmacs/greet"] = blessed

		env := &Env{Who: 1, What: 5, Perms: 5, Host: h}
		got, err := Parse(env, "{greet}")
		if err != nil {
			got = err.Error()
		}
		want := "hi"
		if !blessed {
			want = "greet"
		}
		if !strings.Contains(got, want) {
			t.Errorf("blessed=%v: got %q, want %q",
				blessed, got, want)
		}
	}
}

// macroHost owns #5 by somebody who is not its own owner, so the
// ownership half of the limited walk fails and only the blessing can
// carry it.
type macroHost struct{ *stubHost }

// Owner says #5 belongs to #9 and everything else to #8, so the
// limited walk is looking for #9's property and finds one of #8's:
// only the blessing can carry it.
func (h *macroHost) Owner(obj Ref) Ref {
	if obj == 5 {
		return 9
	}
	return 8
}

// The chain is 5 -> 6 -> 0, so there is a rung between the trigger
// and the global environment for the walk to have to accept.
func (h *macroHost) Parent(obj Ref) Ref {
	switch obj {
	case 5:
		return 6
	case 6:
		return 0
	}
	return -1
}
