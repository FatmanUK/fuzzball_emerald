package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestNextFirstLastDescrOrderByConnectionOrder checks Host.NextDescr,
// Host.FirstDescr and Host.LastDescr's global form (player ==
// ref.Nothing) against two real connections — descriptor ids are
// assigned by a monotonic counter (internal/session.Hub.Add), so
// ascending id order is connection order here, the same thing
// upstream's own doubly-linked list walk answers.
func TestNextFirstLastDescrOrderByConnectionOrder(t *testing.T) {
	h := newHarness(t)
	h.login()

	_, d1 := connectAs(t, h, "First", false)
	_, d2 := connectAs(t, h, "Second", false)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		host := &mufHost{s: h.s, w: w}

		if got := host.FirstDescr(ref.Nothing); got != h.d.ID {
			t.Errorf("FirstDescr(Nothing) = %d, want the harness's own oldest descriptor %d", got, h.d.ID)
		}
		if got := host.LastDescr(ref.Nothing); got != d2.ID {
			t.Errorf("LastDescr(Nothing) = %d, want the newest connection %d", got, d2.ID)
		}
		if got := host.NextDescr(h.d.ID); got != d1.ID {
			t.Errorf("NextDescr(%d) = %d, want %d", h.d.ID, got, d1.ID)
		}
		if got := host.NextDescr(d2.ID); got != 0 {
			t.Errorf("NextDescr(%d) = %d, want 0 (nothing newer)", d2.ID, got)
		}
		if got := host.NextDescr(999999); got != 0 {
			t.Errorf("NextDescr of a descriptor that never existed = %d, want 0", got)
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestFirstLastDescrPlayerScopedAsymmetry checks the real asymmetry
// this phase found in the C: prim_firstdescr's player-scoped branch
// answers a player's newest connection, and prim_lastdescr's answers
// their oldest — the opposite way around from the global form's own
// naming.
func TestFirstLastDescrPlayerScopedAsymmetry(t *testing.T) {
	h := newHarness(t)
	h.login()

	who, dOld := connectAs(t, h, "Multi", false)
	dNew := secondSessionFor(t, h, who)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		host := &mufHost{s: h.s, w: w}

		if got := host.FirstDescr(who); got != dNew.ID {
			t.Errorf("FirstDescr(player) = %d, want the newest connection %d", got, dNew.ID)
		}
		if got := host.LastDescr(who); got != dOld.ID {
			t.Errorf("LastDescr(player) = %d, want the oldest connection %d", got, dOld.ID)
		}

		stranger := w.Create("Stranger", ref.TypePlayer, ref.Nothing)
		stranger.Owner = stranger.Ref
		if got := host.FirstDescr(stranger.Ref); got != 0 {
			t.Errorf("FirstDescr for a disconnected player = %d, want 0", got)
		}
		if got := host.LastDescr(stranger.Ref); got != 0 {
			t.Errorf("LastDescr for a disconnected player = %d, want 0", got)
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestDescrLeastMostIdleAcrossTwoConnections checks
// Host.DescrLeastIdle and Host.DescrMostIdle: the connection that has
// seen input most recently is least idle, and the other is most idle.
func TestDescrLeastMostIdleAcrossTwoConnections(t *testing.T) {
	h := newHarness(t)
	h.login()

	who, dOld := connectAs(t, h, "Multi", false)
	dNew := secondSessionFor(t, h, who)

	// dOld has not been touched since connecting; dNew just sent
	// a line via secondSessionFor's own login bind — but
	// LastActive there is only set by Bind. Send a real line on
	// dNew so its LastActive moves ahead of dOld's.
	sendAs(t, h, dNew, "look")

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		host := &mufHost{s: h.s, w: w}

		if got := host.DescrLeastIdle(who); got != dNew.ID {
			t.Errorf("DescrLeastIdle = %d, want the most recently active connection %d", got, dNew.ID)
		}
		if got := host.DescrMostIdle(who); got != dOld.ID {
			t.Errorf("DescrMostIdle = %d, want the least recently active connection %d", got, dOld.ID)
		}

		nobody := w.Create("Nobody", ref.TypePlayer, ref.Nothing)
		nobody.Owner = nobody.Ref
		if got := host.DescrLeastIdle(nobody.Ref); got != -1 {
			t.Errorf("DescrLeastIdle for a disconnected player = %d, want -1", got)
		}
		if got := host.DescrMostIdle(nobody.Ref); got != -1 {
			t.Errorf("DescrMostIdle for a disconnected player = %d, want -1", got)
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestDescrBootDisconnectsAndAnnounces checks Host.DescrBoot: it
// tears the descriptor down the same way Server.Disconnect does —
// announcing the departure and freeing the connection — but runs
// synchronously since the caller is already on the world goroutine.
func TestDescrBootDisconnectsAndAnnounces(t *testing.T) {
	h := newHarness(t)
	h.login()

	who, d := connectAs(t, h, "Booted", false)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		w.Get(who).Location = w.Get(h.wizRef()).Location
	}); err != nil {
		t.Fatal(err)
	}

	var ok bool
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		host := &mufHost{s: h.s, w: w}
		ok = host.DescrBoot(d.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("DescrBoot should report true for a live descriptor")
	}
	if !d.Closed() {
		t.Error("DescrBoot should close the descriptor")
	}

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		host := &mufHost{s: h.s, w: w}
		if host.DescrBoot(999999) {
			t.Error("DescrBoot should report false for a descriptor that never existed")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestDescrNotifySendsRawTextToTheDescriptor checks Host.DescrNotify:
// it reaches a specific connection directly, bypassing any object or
// listen-prop machinery.
func TestDescrNotifySendsRawTextToTheDescriptor(t *testing.T) {
	h := newHarness(t)
	h.login()

	_, d := connectAs(t, h, "Listener", false)

	var ok bool
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		host := &mufHost{s: h.s, w: w}
		ok = host.DescrNotify(d.ID, "direct message")
	}); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("DescrNotify should report true for a live descriptor")
	}
	got := drainDescriptor(d)
	if !strings.Contains(got, "direct message") {
		t.Errorf("output = %q, want it to contain the notified text", got)
	}

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		host := &mufHost{s: h.s, w: w}
		if host.DescrNotify(999999, "nowhere") {
			t.Error("DescrNotify should report false for a descriptor that never existed")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestSetUserSwitchesADescriptorsPlayer checks Host.SetUser: it moves
// a live descriptor onto a different player, announcing the old
// player's departure and the new one's arrival, and going back to a
// pre-login state — Player ref.Nothing, Connected false — when
// who is ref.Nothing, unlike upstream's own pset_user, which leaves
// that case's state ambiguous (see the host method's own doc
// comment).
func TestSetUserSwitchesADescriptorsPlayer(t *testing.T) {
	h := newHarness(t)
	h.login()

	_, d := connectAs(t, h, "Original", false)

	var other ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		o := w.Create("Other", ref.TypePlayer, ref.Nothing)
		o.Owner = o.Ref
		o.Location = w.Get(h.wizRef()).Location
		other = o.Ref
	}); err != nil {
		t.Fatal(err)
	}

	var ok bool
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		host := &mufHost{s: h.s, w: w}
		ok = host.SetUser(d.ID, other)
	}); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("SetUser should report true for a live descriptor")
	}
	if d.Player != other {
		t.Errorf("d.Player = %v, want %v", d.Player, other)
	}
	if !d.Connected {
		t.Error("the descriptor should stay connected under its new player")
	}

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		host := &mufHost{s: h.s, w: w}
		ok = host.SetUser(d.ID, ref.Nothing)
	}); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("SetUser should report true when disconnecting to Nothing")
	}
	if d.Connected || d.Player != ref.Nothing {
		t.Errorf("SetUser to Nothing should return the descriptor to a pre-login state, got Player=%v Connected=%v", d.Player, d.Connected)
	}

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		host := &mufHost{s: h.s, w: w}
		if host.SetUser(999999, other) {
			t.Error("SetUser should report false for a descriptor that never existed")
		}
	}); err != nil {
		t.Fatal(err)
	}
}
