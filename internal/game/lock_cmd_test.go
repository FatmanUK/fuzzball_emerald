package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// connectAs brings in a second, separate player with a live connection,
// bound without going through a password — the pattern
// TestSpeechReachesOthersInTheRoom uses. The harness's own login'd player
// (created second in newHarness, after the room) lands on dbref #1 == God,
// which bypasses NOGUEST and ownership checks entirely; testing those checks
// needs a player who is neither God nor a wizard.
func connectAs(t *testing.T, h *harness, name string, wizard bool) (ref.Ref, *session.Descriptor) {
	t.Helper()
	var who ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		o := w.Create(name, ref.TypePlayer, ref.Nothing)
		o.Owner = o.Ref
		if wizard {
			o.Flags |= ref.Wizard
		}
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Error(err)
		}
		who = o.Ref
	}); err != nil {
		t.Fatal(err)
	}

	d, err := h.s.Connect(session.TransportLine, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		h.s.Hub().Bind(d, who, w.Now())
	}); err != nil {
		t.Fatal(err)
	}
	drainDescriptor(d)
	t.Cleanup(func() { d.Close() })
	return who, d
}

// sendAs sends a line as a connected player and returns what came back.
func sendAs(t *testing.T, h *harness, d *session.Descriptor, line string) string {
	t.Helper()
	h.s.Input(d, line)
	if err := h.engine.Do(context.Background(), func(*world.World) {}); err != nil {
		t.Fatal(err)
	}
	return drainDescriptor(d)
}

func TestLockCommandSetReportClear(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.send("@dig Cellar")
	cellar := dbrefFrom(t, h.out())
	h.send("@open down=" + cellar)
	h.out()

	h.send("@lock down=me")
	if got := h.out(); !strings.Contains(got, "Lock set.") {
		t.Fatalf("@lock set: %q", got)
	}

	h.send("@lock down")
	if got := h.out(); !strings.Contains(got, "Lock:") || !strings.Contains(got, "Wizard") {
		t.Fatalf("@lock report: %q", got)
	}

	h.send("@lock down=")
	if got := h.out(); !strings.Contains(got, "Lock cleared.") {
		t.Fatalf("@lock clear: %q", got)
	}

	h.send("@lock down")
	if got := h.out(); !strings.Contains(got, "*UNLOCKED*") {
		t.Fatalf("@lock report after clear: %q", got)
	}
}

func TestLockCommandDefaultsToSelf(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.send("@lock =me")
	if got := h.out(); !strings.Contains(got, "Lock set.") {
		t.Fatalf("@lock =me: %q", got)
	}

	h.send("@lock")
	if got := h.out(); !strings.Contains(got, "Lock:") {
		t.Fatalf("@lock (bare): %q", got)
	}
}

func TestLockCommandAliases(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.send("@dig Cellar")
	cellar := dbrefFrom(t, h.out())
	h.send("@open down=" + cellar)
	h.out()

	h.send("@force_lock down=me")
	if got := h.out(); !strings.Contains(got, "Force Lock set.") {
		t.Fatalf("@force_lock (alias of @flock): %q", got)
	}
	h.send("@flock down")
	if got := h.out(); !strings.Contains(got, "Force Lock:") {
		t.Fatalf("@flock report: %q", got)
	}

	h.send("@chown_lock down=me")
	if got := h.out(); !strings.Contains(got, "Chown Lock set.") {
		t.Fatalf("@chown_lock (alias of @chlock): %q", got)
	}
}

func TestLockCommandGuestRejected(t *testing.T) {
	h := newHarness(t)
	h.login()

	guest, d := connectAs(t, h, "Guesty", false)
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		w.Get(guest).Flags |= ref.Guest
	}); err != nil {
		t.Fatal(err)
	}

	got := sendAs(t, h, d, "@lock me=me")
	if !strings.Contains(got, "Guests are not allowed to @lock.") {
		t.Fatalf("got %q", got)
	}
}

func TestLockCommandNoForce(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.s.forceDepth = 1
	defer func() { h.s.forceDepth = 0 }()

	h.send("@flock me=me")
	if got := h.out(); !strings.Contains(got, "You can't use @flock from a @force or {force}.") {
		t.Fatalf("@flock under force: %q", got)
	}

	// @lock has no NOFORCE guard and should go through unaffected.
	h.send("@lock me=me")
	if got := h.out(); !strings.Contains(got, "Lock set.") {
		t.Fatalf("@lock under force should still work: %q", got)
	}
}

// TestLockCommandBadKey checks both messages a bad key produces: the match
// failure's own ("I don't see ... here."), which comes from inside
// boolexp.Parse itself, and _set_lock's own generic one after it. The golden
// harness caught this exact gap once — the first message was being dropped.
func TestLockCommandBadKey(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.send("@lock me=nosuchthingatall")
	got := h.out()
	if !strings.Contains(got, "I don't see nosuchthingatall here.") {
		t.Fatalf("missing the match-failure message:\n%s", got)
	}
	if !strings.Contains(got, "I don't understand that key.") {
		t.Fatalf("missing the generic message:\n%s", got)
	}
}

func TestLockCommandPermissionDenied(t *testing.T) {
	h := newHarness(t)
	h.login()

	// A non-wizard player trying to lock something they do not own and did
	// not create — owned by the harness's wizard, who is not who is
	// connected here.
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz := h.wizRef()
		here := w.Get(wiz).Location
		th := w.Create("theirs", ref.TypeThing, wiz)
		if err := w.MoveTo(th.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	_, d := connectAs(t, h, "Rando", false)
	got := sendAs(t, h, d, "@lock theirs=me")
	if !strings.Contains(got, "Permission denied.") {
		t.Fatalf("got %q", got)
	}
}
