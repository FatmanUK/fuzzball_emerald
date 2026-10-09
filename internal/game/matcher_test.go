package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestMortalCannotNamePlayersByName covers the unconditional
// match_player four commands' matchers used to add.
//
// match_everything (match.c:712) adds match_player *only* when the
// searcher or its owner is a wizard. Emerald's matchControlled and
// resolveControlled added it outright, as did @chown's and examine's
// own matches, so a mortal could name any player in the game as a
// target. The control test then refused them, which means the bug
// showed up as the wrong *message*: "Permission denied." where
// upstream, having matched nothing at all, says "I don't understand
// 'X'." Programs match on both.
//
// No golden case can see this. The oracle drives #1, a wizard, for
// whom match_everything adds match_player anyway, so both servers
// agree whichever way it is written.
func TestMortalCannotNamePlayersByName(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		// Somebody elsewhere in the game, so only a player
		// search can reach them.
		other := w.Create("Stranger", ref.TypePlayer,
			ref.Nothing)
		other.Owner = other.Ref
		elsewhere := w.Create("Far", ref.TypeRoom, other.Ref)
		if err := w.MoveTo(elsewhere.Ref,
			ref.GlobalEnvironment); err != nil {
			t.Error(err)
		}
		err := w.MoveTo(other.Ref, elsewhere.Ref)
		if err != nil {
			t.Error(err)
		}
		// Stop being a wizard, so match_everything's own
		// match_player does not apply.
		w.Get(h.wizRef()).Flags &^= ref.Wizard
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	// One command per matcher that carried the extra search.
	for _, cmd := range []string{
		"@name Stranger=Bob",          // matchControlled
		"@describe Stranger=a person", // matchControlled
		"@unlink Stranger",            // resolveControlled
		"@recycle Stranger",           // resolveControlled
		"@chown Stranger",             // its own match
		"examine Stranger",            // its own match
	} {
		h.send(cmd)
		got := h.out()
		if !strings.Contains(got, "I don't understand") {
			t.Errorf("%q said:\n%s\nwant the no-match "+
				"message", cmd, got)
		}
		// The symptom of the extra search: the matcher found
		// the player, so the *permission* test is what
		// refused, and it says so.
		if strings.Contains(got, "Permission denied") {
			t.Errorf("%q matched a player a mortal "+
				"cannot see:\n%s", cmd, got)
		}
	}
}

// TestWizardStillNamesPlayersByName is the other half: dropping the
// unconditional search must not take away the case it was there for.
// match_everything adds match_player for a wizard, so @set on a
// player who is elsewhere still works.
func TestWizardStillNamesPlayersByName(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		other := w.Create("Stranger", ref.TypePlayer,
			ref.Nothing)
		other.Owner = other.Ref
		elsewhere := w.Create("Far", ref.TypeRoom, other.Ref)
		if err := w.MoveTo(elsewhere.Ref,
			ref.GlobalEnvironment); err != nil {
			t.Error(err)
		}
		err := w.MoveTo(other.Ref, elsewhere.Ref)
		if err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	// With a star: `match_player` (`match.c:252`) is guarded on
	// LOOKUP_TOKEN, so a bare name reaches only the stages that
	// look nearby. This test used to pass a bare "Stranger",
	// which worked because the Player stage treated the star as
	// optional.
	h.send("@describe *Stranger=a person")
	got := h.out()
	if strings.Contains(got, "I don't understand") {
		t.Errorf("a wizard should reach a remote player:\n%s",
			got)
	}
}
