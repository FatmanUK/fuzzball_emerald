package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// `dequeue_prog(player, 2)` is reached from `@Q` and from a
// **disconnect**, and the second had no port: a player who dropped
// their connection left their foreground programs running, so
// `mpi_continue_after_logout` had nothing to decide.
//
// None of this is oracle-reachable. The sweep's whole observable
// effect is on a connection that has gone, and the line upstream
// prints there ("Foreground program aborted.") can reach nobody,
// because the sweep only runs when this was the player's last
// descriptor.

// TestAtQStopsASleepingForegroundProgram is the half that was
// narrower than upstream's: `@Q` killed only the program *reading*
// the descriptor, where `dequeue_prog` takes the **player's**
// foreground programs.
func TestAtQStopsASleepingForegroundProgram(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.installProgram(t, "napper", `: main
  me @ "napping" notify
  600 sleep
  me @ "woke" notify
;`)

	h.send("napper")
	if got := h.out(); !strings.Contains(got, "napping") {
		t.Fatalf("the program did not start:\n%s", got)
	}
	if n := h.procCount(); n != 1 {
		t.Fatalf("%d processes, want 1", n)
	}

	h.send("@Q")
	if got := h.out(); !strings.Contains(got,
		"Foreground program aborted.") {

		t.Errorf("@Q said:\n%s", got)
	}
	if n := h.procCount(); n != 0 {
		t.Errorf("%d processes survived @Q", n)
	}
}

// TestTheSweepIsPerPlayer is the filter: `dequeue_prog` is given a
// player, so somebody else's foreground program is not touched. With
// one connection the harness cannot start one, so the second process
// is put in the table directly -- which is what the sweep reads.
func TestTheSweepIsPerPlayer(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.installProgram(t, "napper", `: main
  600 sleep
;`)

	h.send("napper")
	h.out()
	if err := h.engine.Do(context.Background(),
		func(*world.World) {
			h.s.procs.add(&process{
				state:  procSleeping,
				player: ref.GlobalEnvironment,
			})
		}); err != nil {
		t.Fatal(err)
	}
	if n := h.procCount(); n != 2 {
		t.Fatalf("%d processes, want 2", n)
	}

	h.send("@Q")
	if n := h.procCount(); n != 1 {
		t.Errorf("%d processes left, want 1", n)
	}
}

// TestBackgroundSurvivesTheSweep is the rule that makes killmode 2
// "foreground only": a program that has put itself in the background
// is not somebody's foreground program any more.
func TestBackgroundSurvivesTheSweep(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.installProgram(t, "backer", `: main
  background
  me @ "backgrounded" notify
  600 sleep
;`)

	h.send("backer")
	h.out()
	if n := h.procCount(); n != 1 {
		t.Fatalf("%d processes, want 1", n)
	}

	h.send("@Q")
	if n := h.procCount(); n != 1 {
		t.Errorf("a background process was killed")
	}
}

// TestDisconnectStopsForegroundPrograms is the call site that was
// missing entirely.
func TestDisconnectStopsForegroundPrograms(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.installProgram(t, "napper", `: main
  600 sleep
;`)

	h.send("napper")
	h.out()
	if n := h.procCount(); n != 1 {
		t.Fatalf("%d processes, want 1", n)
	}

	h.s.Disconnect(h.d)
	h.syncEngine()
	if n := h.procCount(); n != 0 {
		t.Errorf("%d processes survived the disconnect", n)
	}
}

// TestMpiContinueAfterLogout is the parameter itself, and it reads
// the opposite way round from its name: by **default** a queued MPI
// event does *not* continue, so an MPI `{delay}` dies with the
// connection.
func TestMpiContinueAfterLogout(t *testing.T) {
	for _, tc := range []struct {
		tune string
		want int
	}{{"no", 0}, {"yes", 1}} {
		h := newHarness(t)
		h.login()
		h.send("@tune mpi_continue_after_logout=" + tc.tune)
		h.send("@describe me={delay:600,later}")
		h.send("look me")
		h.out()
		if n := h.mpiEventCount(); n != 1 {
			t.Fatalf("%s: %d events queued, want 1",
				tc.tune, n)
		}

		h.s.Disconnect(h.d)
		h.syncEngine()
		if n := h.mpiEventCount(); n != tc.want {
			t.Errorf("%s: %d events left, want %d",
				tc.tune, n, tc.want)
		}
	}
}
