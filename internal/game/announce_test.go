package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// `announce_disconnect` (`interface.c:2325`) splits the same way
// `announce_connect` does, and this server had the split wrong in
// both: the **line** and the **propqueues** fire on every disconnect,
// while the `disconnect` action and the puppets falling asleep wait
// for the last one.
//
// The trio in `internal/golden` compares the connect side, because a
// login is something the harness can cause. A disconnect is not —
// closing a seat leaves the harness nothing to read — so this half
// is here.

// watcher connects a second player who stays put and hears things.
func (h *harness) watcher(t *testing.T,
	name string) *session.Descriptor {

	t.Helper()
	h.send("@pcreate " + name + "=secret")
	h.out()
	d, err := h.s.Connect(session.TransportLine, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.s.Input(d, "connect "+name+" secret")
	h.syncEngine()
	// The arrival output is **not** drained here: one of these
	// tests is about what the arrival itself produces.
	h.out()
	return d
}

// second opens another connection for the player already logged in.
func (h *harness) second(t *testing.T) *session.Descriptor {
	t.Helper()
	d, err := h.s.Connect(session.TransportLine, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.s.Input(d, "connect Wizard secret")
	h.syncEngine()
	drainDescriptor(d)
	h.out()
	return d
}

// TestDisconnectLineIsNotGatedOnTheLast is the rule this had
// backwards: the line is tested against DARK and nothing else, so
// closing one of two connections announces it.
func TestDisconnectLineIsNotGatedOnTheLast(t *testing.T) {
	h := newHarness(t)
	h.login()
	watcher := h.watcher(t, "Bob")
	extra := h.second(t)

	h.s.Disconnect(extra)
	h.syncEngine()
	if got := drainDescriptor(watcher); !strings.Contains(got,
		"Wizard has disconnected.") {

		t.Errorf("no announcement for the first of two:\n%s",
			got)
	}
}

// TestDisconnectPropqueueRunsEveryTime is the other half of the
// split: the propqueues are not gated either.
func TestDisconnectPropqueueRunsEveryTime(t *testing.T) {
	h := newHarness(t)
	h.login()
	watcher := h.watcher(t, "Bob")
	h.send("@set here=_disconnect:&{null:{otell:the queue " +
		"ran,here,#-1}}")
	h.out()
	extra := h.second(t)

	h.s.Disconnect(extra)
	h.syncEngine()
	if got := drainDescriptor(watcher); !strings.Contains(got,
		"the queue ran") {

		t.Errorf("the propqueue did not run:\n%s", got)
	}
}

// TestPuppetsSleepOnlyOnTheLast is the half that **is** gated, and
// the whole-database walk `announce_puppets` makes to find them --
// which upstream's own comment calls brutal and does anyway on every
// login and every disconnect.
func TestPuppetsSleepOnlyOnTheLast(t *testing.T) {
	h := newHarness(t)
	h.login()
	watcher := h.watcher(t, "Bob")
	h.send("@create poppet")
	h.send("@set poppet=zombie")
	h.send("drop poppet")
	h.out()
	extra := h.second(t)
	drainDescriptor(watcher)

	// One of two going away: the puppet stays awake.
	h.s.Disconnect(extra)
	h.syncEngine()
	if got := drainDescriptor(watcher); strings.Contains(got,
		"falls asleep") {

		t.Errorf("a puppet slept on the first of two:\n%s",
			got)
	}

	// The last one: it sleeps, and `_/pcon`'s sibling replaces
	// the wording.
	h.send("@set poppet=_/pdcon:curls up.")
	h.out()
	drainDescriptor(watcher)
	h.s.Disconnect(h.d)
	h.syncEngine()
	got := drainDescriptor(watcher)
	if !strings.Contains(got, "poppet curls up.") {
		t.Errorf("the puppet did not sleep:\n%s", got)
	}
}

// TestPuppetsWakeOnlyOnTheFirst is the connect side of the same gate,
// which the trio case cannot separate: a puppet's room line goes to
// the room, and in the fixture every seat is in it.
func TestPuppetsWakeOnlyOnTheFirst(t *testing.T) {
	h := newHarness(t)
	h.login()
	watcher := h.watcher(t, "Bob")
	h.send("@create poppet")
	h.send("@set poppet=zombie")
	h.send("drop poppet")
	h.out()
	drainDescriptor(watcher)

	// A second connection for a player already on: no waking.
	extra := h.second(t)
	if got := drainDescriptor(watcher); strings.Contains(got,
		"wakes up") {

		t.Errorf("a puppet woke on a second connection:\n%s",
			got)
	}
	h.s.Disconnect(extra)
	h.syncEngine()
}

// TestDarkSuppressesTheAnnouncement is the one test the line does
// make, which this did not make at all.
func TestDarkSuppressesTheAnnouncement(t *testing.T) {
	h := newHarness(t)
	h.login()
	watcher := h.watcher(t, "Bob")
	if err := h.engine.Do(context.Background(),
		func(w *world.World) {
			w.Get(h.wizRef()).Flags |= ref.Dark
		}); err != nil {
		t.Fatal(err)
	}
	drainDescriptor(watcher)

	extra := h.second(t)
	if got := drainDescriptor(watcher); strings.Contains(got,
		"has connected") {

		t.Errorf("a DARK player was announced:\n%s", got)
	}
	h.s.Disconnect(extra)
	h.syncEngine()
	if got := drainDescriptor(watcher); strings.Contains(got,
		"has disconnected") {

		t.Errorf("a DARK player's leaving was announced:\n%s",
			got)
	}
}

// TestConnectActionIsFirstOnly is the `connect` exit, which had no
// port at all: matched as an exit at **priority 1**, and only on the
// first connection.
func TestConnectActionIsFirstOnly(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.installProgram(t, "greeter", `: main
  pop
  me @ "the action ran" notify
;`)
	// The exit has to be at priority 1 to be matched at all, so
	// an ordinary one of that name is not enough.
	if err := h.engine.Do(context.Background(),
		func(w *world.World) {
			wiz := h.wizRef()
			var target ref.Ref
			w.Each(func(o *world.Object) bool {
				if o.Name == "greeter.muf" {
					target = o.Ref
				}
				return true
			})
			e := w.Create("connect", ref.TypeExit, wiz)
			e.Dest = []ref.Ref{target}
			e.Flags = e.Flags.SetMLevel(1)
			if err := w.MoveTo(e.Ref,
				w.Get(wiz).Location); err != nil {
				t.Error(err)
			}
		}); err != nil {
		t.Fatal(err)
	}
	h.out()

	// A second connection for a player already on: no action.
	extra := h.second(t)
	if got := drainDescriptor(extra); strings.Contains(got,
		"the action ran") {

		t.Errorf("the action ran on a reconnect:\n%s", got)
	}
	h.s.Disconnect(extra)
	h.syncEngine()

	// A first connection, for somebody else: it runs. The arrival
	// output is what carries it, which is why `watcher` leaves it
	// queued.
	watcher := h.watcher(t, "Bob")
	if got := drainDescriptor(watcher); !strings.Contains(got,
		"the action ran") {

		t.Errorf("the action did not run:\n%s", got)
	}

	// And the floor of 1 is **not** a privilege: priority 1 is
	// what an exit with no mucker bits already has, so the only
	// thing excluded is an **ABODE** exit, the one shape that
	// binds more weakly than the default.
	if err := h.engine.Do(context.Background(),
		func(w *world.World) {
			w.Each(func(o *world.Object) bool {
				if o.Name == "connect" {
					o.Flags = o.Flags.SetMLevel(0)
					o.Flags |= ref.Abode
				}
				return true
			})
		}); err != nil {
		t.Fatal(err)
	}
	h.out()
	plain := h.watcher(t, "Carol")
	if got := drainDescriptor(plain); strings.Contains(got,
		"the action ran") {

		t.Errorf("an ABODE exit was matched:\n%s", got)
	}
}
