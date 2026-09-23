package game

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// dropWizardBit makes the harness's player an ordinary one, for the checks
// that a wizard is exempt from.
func dropWizardBit(t *testing.T, h *harness) {
	t.Helper()
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		w.Get(h.wizRef()).Flags &^= ref.Wizard
	}); err != nil {
		t.Fatal(err)
	}
}

// setTune changes a parameter on the harness's world.
func setTune(t *testing.T, h *harness, name, value string) {
	t.Helper()
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		if err := w.Tune.SetString(name, value); err != nil {
			t.Errorf("setting %s: %v", name, err)
		}
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPlayermaxRefusesLoginWhenFull(t *testing.T) {
	h := newHarness(t)
	// The cap counts connections that have *finished* logging in, as
	// upstream's con_players_curr does — this one has not, so a limit of
	// zero is what makes the server full for it. That is also a real
	// configuration: it is how a world is closed for maintenance.
	setTune(t, h, "playermax", "yes")
	setTune(t, h, "playermax_limit", "0")

	// The wizard is exempt, so it has to lose the bit to be refused.
	dropWizardBit(t, h)

	h.send("connect Wizard secret")
	got := h.out()
	if !strings.Contains(got, "too many players") {
		t.Errorf("a full server should have refused the login:\n%s", got)
	}
	if strings.Contains(got, "The Study") {
		t.Errorf("the refused login got in anyway:\n%s", got)
	}
}

// TestPlayermaxCountsLoggedInConnections pins the distinction the test above
// depends on: someone sitting at the login screen does not occupy a place.
func TestPlayermaxCountsLoggedInConnections(t *testing.T) {
	h := newHarness(t)
	setTune(t, h, "playermax", "yes")
	setTune(t, h, "playermax_limit", "1")
	dropWizardBit(t, h)

	// This connection is at the login screen, so the one place is free.
	h.send("connect Wizard secret")
	if got := h.out(); !strings.Contains(got, "The Study") {
		t.Fatalf("the first login should have been allowed:\n%s", got)
	}

	// Now the place is taken, so a second connection is refused.
	second := secondSessionFor(t, h, h.wizRef())
	_ = second
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		if !h.s.serverFull(w) {
			t.Error("the server should be full with its one place taken")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestPlayermaxExemptsWizards is the half that matters operationally: an
// admin has to be able to get in to deal with whatever filled the server up.
func TestPlayermaxExemptsWizards(t *testing.T) {
	h := newHarness(t)
	setTune(t, h, "playermax", "yes")
	setTune(t, h, "playermax_limit", "1")

	h.send("connect Wizard secret")
	got := h.out()
	if strings.Contains(got, "too many players") {
		t.Errorf("a wizard should be exempt from the cap:\n%s", got)
	}
	if !strings.Contains(got, "The Study") {
		t.Errorf("the wizard did not get in:\n%s", got)
	}
}

func TestPlayermaxOffAllowsEveryone(t *testing.T) {
	h := newHarness(t)
	setTune(t, h, "playermax", "no")
	setTune(t, h, "playermax_limit", "1")
	dropWizardBit(t, h)

	h.send("connect Wizard secret")
	if got := h.out(); strings.Contains(got, "too many players") {
		t.Errorf("the cap applied with playermax off:\n%s", got)
	}
}

// TestQuotaRefillsOnTick checks the wiring rather than the bucket itself,
// which internal/session tests directly: a tick has to hand out the
// allowance the @tune parameters name.
func TestQuotaRefillsOnTick(t *testing.T) {
	h := newHarness(t)
	h.login()

	setTune(t, h, "commands_per_time", "3")
	setTune(t, h, "command_burst_size", "100")
	setTune(t, h, "command_time_msec", "1000")

	// Spend everything, then let two slices of the clock pass.
	h.d.Quota.Set(0)
	advance := func(seconds int64) {
		base := h.w.Now()
		h.w.SetClock(func() time.Time { return base.Add(time.Duration(seconds) * time.Second) })
	}
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		h.s.Tick(w) // establishes the refill clock
	}); err != nil {
		t.Fatal(err)
	}
	advance(2)
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		h.s.Tick(w)
	}); err != nil {
		t.Fatal(err)
	}

	if got := h.d.Quota.Remaining(); got != 6 {
		t.Errorf("Remaining() = %d, want 6 (3 per slice, two slices)", got)
	}
}
