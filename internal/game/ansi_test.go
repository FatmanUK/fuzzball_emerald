package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestANSIGateIsReadLive covers the reason the gate is a callback
// rather than a flag pushed onto the descriptor: the answer has to
// change the moment somebody sets COLOR, with no window in which the
// descriptor still holds the old one.
func TestANSIGateIsReadLive(t *testing.T) {
	h := newHarness(t)
	h.login()

	const coloured = "\x1b[31mred\x1b[0m"
	ctx := context.Background()

	// A player starts without COLOR, so colour is removed.
	say := func() string {
		if err := h.engine.Do(ctx,
			func(w *world.World) {
				h.s.send(w, h.wizRef(), coloured)
			}); err != nil {
			t.Fatal(err)
		}
		return h.out()
	}

	if got := say(); strings.Contains(got, "\x1b") {
		t.Errorf("colour survived without COLOR: %q", got)
	} else if !strings.Contains(got, "red") {
		t.Errorf("the text itself was lost: %q", got)
	}

	// Setting it takes effect on the very next line.
	h.send("@set me=C")
	h.out()
	got := say()
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("COLOR did not keep the sequence: %q", got)
	}
	// Sanitize appends its own reset on top of the one the text
	// already carries, which is upstream's unconditional append.
	if !strings.Contains(got, "\x1b[0m\x1b[0m") {
		t.Errorf("no reset was appended: %q", got)
	}

	// And unsetting it takes effect immediately too.
	h.send("@set me=!C")
	h.out()
	if got := say(); strings.Contains(got, "\x1b") {
		t.Errorf("colour survived after !C: %q", got)
	}
}

// TestANSIGateBeforeLogin covers the branch no logged-in player can
// reach: before login the answer is not a flag at all but two @tune
// parameters, and **both** have to be on.
//
// That is upstream's, and it reads like an accident of
// implementation: the banner is the only pre-login text a world
// writes, so the parameters deciding whether MPI runs over it also
// decide whether its colour survives.
func TestANSIGateBeforeLogin(t *testing.T) {
	h := newHarness(t)

	// The harness's descriptor has not logged in yet.
	if h.d.Connected {
		t.Fatal("the descriptor is already connected")
	}
	if h.d.AllowANSI == nil {
		t.Fatal("the gate was not installed")
	}

	ctx := context.Background()
	for _, tc := range []struct {
		mpi, welcome string
		want         bool
	}{
		{"yes", "yes", true},
		{"yes", "no", false},
		{"no", "yes", false},
		{"no", "no", false},
	} {
		err := h.engine.Do(ctx, func(w *world.World) {
			if err := w.SetTune("do_mpi_parsing",
				tc.mpi); err != nil {
				t.Error(err)
			}
			if err := w.SetTune("do_welcome_parsing",
				tc.welcome); err != nil {
				t.Error(err)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := h.d.AllowANSI(); got != tc.want {
			t.Errorf("mpi=%s welcome=%s: = %v, want %v",
				tc.mpi, tc.welcome, got, tc.want)
		}
	}
}

// TestANSIGateIsNotTheFlagOnAThing pins the half of CHOWN_OK that is
// not about colour: the same bit means "may be @chowned" on anything
// but a player, so a THING set C does not start keeping colour — it
// has no connection to keep it on, and its owner's flag is what
// decides what a puppet's relay shows.
func TestANSIGateIsNotTheFlagOnAThing(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	var puppet ref.Ref
	err := h.engine.Do(ctx, func(w *world.World) {
		o := w.Create("puppet", ref.TypeThing, h.wizRef())
		o.Flags |= ref.Zombie | ref.ChownOK
		here := w.Get(h.wizRef()).Location
		o.Home = here
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Error(err)
		}
		puppet = o.Ref
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	// The puppet is CHOWN_OK and its owner is not COLOR, so the
	// relayed line arrives stripped.
	err = h.engine.Do(ctx, func(w *world.World) {
		h.s.send(w, puppet, "\x1b[32mgreen\x1b[0m")
	})
	if err != nil {
		t.Fatal(err)
	}
	got := h.out()
	if !strings.Contains(got, "green") {
		t.Fatalf("the relay did not reach the owner: %q", got)
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("a CHOWN_OK thing kept colour: %q", got)
	}
}
