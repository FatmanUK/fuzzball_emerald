package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestTeleportPermissionsAreNotMatchControlled covers the half of
// do_teleport the golden harness cannot reach: its player is #1, who
// controls everything in the fixture, so none of the three refusals
// ever fires there.
//
// They are worth pinning because each names the test it failed, and
// because none of them is match_controlled's — the whole reason
// @teleport could not go through matchControlled is that the rule
// depends on the destination and so cannot be applied at match time.
func TestTeleportPermissionsAreNotMatchControlled(t *testing.T) {
	h := newHarness(t)
	h.login()

	var theirRoom, theirThing ref.Ref
	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		other := w.Create("Stranger", ref.TypePlayer, ref.Nothing)
		other.Owner = other.Ref

		r := w.Create("Vault", ref.TypeRoom, other.Ref)
		r.Dropto = ref.Nothing
		if err := w.MoveTo(r.Ref, ref.GlobalEnvironment); err != nil {
			t.Error(err)
		}
		theirRoom = r.Ref

		// Somewhere the test player stands but does not own,
		// so the victim is reachable and the control test is
		// the only thing in the way.
		o := w.Create("casket", ref.TypeThing, other.Ref)
		o.Home = theirRoom
		here := w.Get(h.wizRef()).Location
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Error(err)
		}
		theirThing = o.Ref

		// Stop being a wizard, so control is really tested.
		w.Get(h.wizRef()).Flags &^= ref.Wizard
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	for _, tc := range []struct{ cmd, want string }{
		// A player needs control of victim, destination, the
		// victim's location and the destination's.
		{"@teleport me=" + theirRoom.String(), noTelePlayer},
		// A thing needs the destination controlled *or*
		// linkable — and somebody else's room that is
		// neither LINK_OK nor ABODE is neither, even though
		// the casket is standing in a room the player does
		// own.
		{"@teleport " + theirThing.String() + "=" +
			theirRoom.String(), noTeleThing},
		// A room needs the victim controlled, which rules out
		// somebody else's.
		{"@teleport " + theirRoom.String() + "=here",
			noTeleRoom},
	} {
		h.send(tc.cmd)
		got := h.out()
		if !strings.Contains(got, tc.want) {
			t.Errorf("%q said:\n%s\nwant %q",
				tc.cmd, got, tc.want)
		}
		// None of the three is match_controlled's refusal,
		// and saying it would be would hide which rule
		// applied.
		if strings.Contains(got, "what was matched") {
			t.Errorf("%q claims match_controlled:\n%s",
				tc.cmd, got)
		}
	}
}

// TestTeleportRefusesGodsThings checks the strict_god_priv guard,
// which sits between the two matches and so applies whatever the
// destination turns out to be.
func TestTeleportRefusesGodsThings(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	var crown ref.Ref
	err := h.engine.Do(ctx, func(w *world.World) {
		o := w.Create("crown", ref.TypeThing, ref.God)
		here := w.Get(h.wizRef()).Location
		o.Home = here
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Error(err)
		}
		crown = o.Ref

		// The harness's player is #1, which *is* God, so the
		// guard needs somebody else holding the descriptor.
		p := w.Create("Archwizard", ref.TypePlayer, ref.Nothing)
		p.Owner = p.Ref
		p.Flags |= ref.Wizard
		p.Home = here
		if err := w.MoveTo(p.Ref, here); err != nil {
			t.Error(err)
		}
		h.d.Player = p.Ref
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("@teleport " + crown.String() + "=here")
	if got := h.out(); !strings.Contains(got, godsPlacing) {
		t.Errorf("a wizard moved God's thing:\n%s", got)
	}
}
