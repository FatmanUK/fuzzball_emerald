package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestRestrictReportsAndSets covers do_restrict's three answers,
// including the one that looks like an oversight: "on" and "off" are
// compared case-sensitively, so "@restrict ON" reports.
func TestRestrictReportsAndSets(t *testing.T) {
	h := newHarness(t)
	h.login()

	for _, tc := range []struct{ send, want string }{
		{"@restrict", "Restricted connection mode is currently off."},
		{"@restrict ON", "Restricted connection mode is currently off."},
		{"@restrict on", "Login access is now restricted to wizards only."},
		{"@restrict", "Restricted connection mode is currently on."},
		{"@restrict OFF", "Restricted connection mode is currently on."},
		{"@restrict off", "Login access is now unrestricted."},
		{"@restrict", "Restricted connection mode is currently off."},
	} {
		h.send(tc.send)
		if got := h.out(); !strings.Contains(got, tc.want) {
			t.Errorf("%q said:\n%s\nwant %q",
				tc.send, got, tc.want)
		}
	}
}

// TestRestrictShutsOutMortals is what @restrict is for, and neither
// half of it is visible from the command's own reply: the banner a
// connection sees before typing anything, and the refusal when a
// mortal gets their password right anyway.
func TestRestrictShutsOutMortals(t *testing.T) {
	h := newHarness(t)
	h.login()

	// A mortal with a password, so the refusal is reached after
	// the password check rather than instead of it.
	ctx := context.Background()
	hashed, err := password.Hash("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	err = h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		o := w.Create("Mortal", ref.TypePlayer, ref.Nothing)
		o.Owner = o.Ref
		o.Home = here
		o.PasswordHash = hashed
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("@restrict on")
	h.out()

	// The banner says so before anyone types anything.
	d, err := h.s.Connect(session.TransportLine, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if got := drainDescriptor(d); !strings.Contains(got,
		"maintenance mode") {
		t.Errorf("the banner did not mention maintenance:\n%s", got)
	}

	// And a correct password is still refused.
	h.s.Input(d, "connect Mortal hunter2")
	if err := h.engine.Do(ctx, func(*world.World) {}); err != nil {
		t.Fatal(err)
	}
	got := drainDescriptor(d)
	if !strings.Contains(got, "only wizards are allowed to connect") {
		t.Errorf("a mortal was not refused:\n%s", got)
	}
	if d.Player != ref.Nothing {
		t.Errorf("the mortal was logged in anyway as %s", d.Player)
	}

	// A true wizard is exempt, which is what lets an admin get
	// back in to lift it.
	d2, err := h.s.Connect(session.TransportLine, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d2.Close() })
	drainDescriptor(d2)
	h.s.Input(d2, "connect Wizard secret")
	if err := h.engine.Do(ctx, func(*world.World) {}); err != nil {
		t.Fatal(err)
	}
	if d2.Player == ref.Nothing {
		t.Errorf("a wizard was shut out too:\n%s",
			drainDescriptor(d2))
	}
}

// TestGripeRecordsAndTellsTheWizards covers do_gripe's three paths.
func TestGripeRecordsAndTellsTheWizards(t *testing.T) {
	h := newHarness(t)
	h.login()

	// Nothing filed yet, and the wizard is told so rather than
	// shown an empty listing.
	h.send("gripe")
	if got := h.out(); !strings.Contains(got, "Nobody has griped.") {
		t.Errorf("an empty gripe log said:\n%s", got)
	}

	// A mortal who files one is thanked, and the wizard hears it
	// wherever they are.
	_, mortal := connectAs(t, h, "Complainer", false)
	got := sendAs(t, h, mortal, "gripe the doors stick")
	if !strings.Contains(got,
		"Your complaint has been duly noted.") {
		t.Errorf("the complainer was not thanked:\n%s", got)
	}
	if got := h.out(); !strings.Contains(got,
		"## GRIPE from Complainer: the doors stick") {
		t.Errorf("the wizard was not told:\n%s", got)
	}

	// A mortal asking to read them is told how to file one
	// instead.
	if got := sendAs(t, h, mortal, "gripe"); !strings.Contains(got,
		"If you wish to gripe, use 'gripe <message>'.") {
		t.Errorf("a mortal reading gripes saw:\n%s", got)
	}

	// The wizard reads it back, in the shape upstream's log file
	// holds it — where a dbref carries no '#', which is how
	// every log line upstream writes one.
	h.send("gripe")
	got = h.out()
	for _, want := range []string{"GRIPE from Complainer(2)",
		"in The Study(0)", "the doors stick"} {
		if !strings.Contains(got, want) {
			t.Errorf("the gripe log lacks %q:\n%s", want, got)
		}
	}

	// And it is on the world, so a flush would carry it.
	err := h.engine.Do(context.Background(), func(w *world.World) {
		if n := len(w.Gripes()); n != 1 {
			t.Errorf("the world holds %d gripes, want 1", n)
		}
		if s := w.TakeSnapshot(); len(s.Gripes) != 1 {
			t.Errorf("the snapshot carries %d gripes, want 1",
				len(s.Gripes))
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}
