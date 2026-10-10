package game

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// The descriptor sweep cannot be reached from the golden harness: it
// runs on the tick, and the oracle is driven one command at a time
// with no way to move its clock.

// TestAWizardIsNotIdleBooted and the quelled half below are one rule:
// `!Wizard(d->player)` (`interface.c:4564`), where `Wizard()`
// excludes QUELL. So the exemption is not "a wizard" but "a wizard
// using their powers".
func TestAWizardIsNotIdleBooted(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.out()

	// Well past maxidle, which defaults to two hours.
	h.advance(3 * time.Hour)
	if h.d.Closed() {
		t.Fatal("a wizard was idle-booted")
	}
	if got := h.out(); strings.Contains(got,
		"Autodisconnecting") {

		t.Errorf("a wizard was told it was idle:\n%s", got)
	}

	// Quelled, the same connection is booted on the next tick.
	h.send("@set me=Q")
	h.out()
	h.advance(3 * time.Hour)
	const idleMesg = "Autodisconnecting for inactivity."
	got := h.out()
	if !strings.Contains(got, idleMesg) {
		t.Errorf("no idle boot message:\n%s", got)
	}
	if !h.d.Closed() {
		t.Error("the connection was not dropped")
	}
}

// TestIdlebootCanBeTurnedOff is the parameter itself, which defaults
// **true** -- so before the sweep existed an unconfigured world
// diverged from upstream with no configuration at all.
func TestIdlebootCanBeTurnedOff(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("@tune idleboot=no")
	h.send("@set me=Q")
	h.out()

	h.advance(3 * time.Hour)
	if h.d.Closed() {
		t.Error("idleboot=no still booted the connection")
	}
}

// TestIdleTimeIsInputNotOutput is what makes `maxidle` mean
// something: the clock it reads is `last_time`, stamped when a line
// *arrives*, so a player being talked at stays idle.
func TestIdleTimeIsInputNotOutput(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("@set me=Q")
	h.out()

	// An hour of being sent things, which resets nothing.
	h.advance(time.Hour)
	for i := 0; i < 3; i++ {
		if err := h.engine.Do(context.Background(),
			func(w *world.World) {
				h.s.send(w, h.d.Player, "chatter")
			}); err != nil {
			t.Fatal(err)
		}
	}
	h.out()
	h.advance(90 * time.Minute)
	if !h.d.Closed() {
		t.Error("output kept the connection from going idle")
	}
}

// TestTheLoginScreenTimesOut is the one number in the sweep that is
// not tunable: upstream hardcodes 300 seconds and logs the number
// back.
func TestTheLoginScreenTimesOut(t *testing.T) {
	h := newHarness(t)
	h.out()

	h.advance(299 * time.Second)
	if h.d.Closed() {
		t.Fatal("dropped before the timeout")
	}
	h.advance(2 * time.Second)
	if !h.d.Closed() {
		t.Error("the login screen never timed out")
	}
}

// TestAnIdleConnectionIsPinged is `idle_ping_enable` and
// `idle_ping_time`, both of which had no reader -- and the clock they
// read is **not** the idle clock. `last_pinged_at` is stamped in
// `socket_write` (`interface.c:2120`), so it measures time since
// anything was *sent*, which is why a chatty room never pings.
func TestAnIdleConnectionIsPinged(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.out()

	// idle_ping_time defaults to 55 seconds.
	h.advance(30 * time.Second)
	if pinged(h.d) {
		t.Fatal("pinged before idle_ping_time")
	}
	h.advance(40 * time.Second)
	if !pinged(h.d) {
		t.Fatal("never pinged")
	}

	// And the clock is the *output* one: a line sent to the
	// player postpones the next ping, where a line typed by them
	// would not.
	h.advance(40 * time.Second)
	if pinged(h.d) {
		t.Fatal("pinged twice inside one interval")
	}
	if err := h.engine.Do(context.Background(),
		func(w *world.World) {
			h.s.send(w, h.d.Player, "chatter")
		}); err != nil {
		t.Fatal(err)
	}
	h.out()
	h.advance(40 * time.Second)
	if pinged(h.d) {
		t.Error("output did not postpone the ping")
	}
}

// TestNoIdlePingOptsOut is `_/sys/no_idle_ping` on the player, read
// with `get_property_class` -- so a string value counts and nothing
// else does.
func TestNoIdlePingOptsOut(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.out()

	if err := h.engine.Do(context.Background(),
		func(w *world.World) {
			w.SetProp(h.d.Player, noIdlePingProp,
				props.Value{Type: props.String,
					Str: "y"})
		}); err != nil {
		t.Fatal(err)
	}

	h.advance(2 * time.Minute)
	if pinged(h.d) {
		t.Error("the opt-out property was not read")
	}
}

// TestAKeepaliveIsNothingWithoutTelnet is upstream's zero-byte write
// reproduced as an empty answer: `send_keepalive` queues `""` for a
// client that has never spoken telnet, so `socket_write` is called
// with a count of zero -- the timestamp is stamped and no byte
// leaves.
func TestAKeepaliveIsNothingWithoutTelnet(t *testing.T) {
	if b := session.Keepalive(false); len(b) != 0 {
		t.Errorf("a non-telnet keepalive sent %v", b)
	}
	b := session.Keepalive(true)
	if len(b) != 2 || b[0] != session.IAC ||
		b[1] != session.NOP {

		t.Errorf("a telnet keepalive is not IAC NOP: %v", b)
	}
}

// pinged reports whether a keepalive request is outstanding, taking
// it if so.
func pinged(d *session.Descriptor) bool {
	select {
	case <-d.Keepalive():
		return true
	default:
		return false
	}
}
