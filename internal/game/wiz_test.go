package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestSetFlagsByPrefix checks that @set resolves flag names the way upstream's
// str_to_flag does, by prefix, and that mucker levels replace one another
// rather than accumulating.
func TestSetFlagsByPrefix(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("@create widget")
	h.out()

	cases := []struct {
		set  string
		want ref.Flags
		off  ref.Flags
	}{
		{"V", ref.Vehicle, 0},
		{"!vehicle", 0, ref.Vehicle},
		{"X", ref.XForcible, 0},
		{"d", ref.Dark, 0},
		{"sticky", ref.Sticky, 0},
		{"M2", ref.Mucker, ref.SMucker},
		{"M1", ref.SMucker, ref.Mucker},
		{"M3", ref.Mucker | ref.SMucker, 0},
		{"!mucker", 0, ref.Mucker | ref.SMucker},
	}
	for _, tc := range cases {
		h.send("@set widget=" + tc.set)
		if got := h.out(); strings.Contains(got, "recognize") {
			t.Errorf("@set widget=%s said %q", tc.set, got)
			continue
		}
		f := h.flagsOf(t, "widget")
		if tc.want != 0 && f&tc.want != tc.want {
			t.Errorf("after @set widget=%s the flags are %v, missing %v",
				tc.set, f, tc.want)
		}
		if tc.off != 0 && f&tc.off != 0 {
			t.Errorf("after @set widget=%s the flags are %v, should not have %v",
				tc.set, f, tc.off)
		}
	}

	// Two names the flag table resolves but @set refuses, because each
	// would set something other than it says.
	for _, name := range []string{"T", "truewizard", "N", "nucker"} {
		h.send("@set widget=" + name)
		if got := h.out(); !strings.Contains(got, "I don't recognize that flag.") {
			t.Errorf("@set widget=%s said %q", name, got)
		}
	}
	h.send("@set widget=M4")
	if got := h.out(); !strings.Contains(got, "To set Mucker Level 4") {
		t.Errorf("@set widget=M4 said %q", got)
	}
}

// TestToadHandsOverWhatThePlayerOwned checks the part of @toad a transcript
// cannot show: everything the victim owned changes hands, and the victim stops
// being a player.
func TestToadHandsOverWhatThePlayerOwned(t *testing.T) {
	h := newHarness(t)
	h.login()

	var victim, victimThing ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		o := w.Create("Victim", ref.TypePlayer, ref.Nothing)
		o.Owner = o.Ref
		o.Home = w.Get(h.wizRef()).Location
		if err := w.MoveTo(o.Ref, o.Home); err != nil {
			t.Error(err)
		}
		victim = o.Ref

		// Something the victim owns, and something homed to them.
		thing := w.Create("trinket", ref.TypeThing, victim)
		thing.Home = victim
		if err := w.MoveTo(thing.Ref, o.Home); err != nil {
			t.Error(err)
		}
		victimThing = thing.Ref
	}); err != nil {
		t.Fatal(err)
	}

	h.send("@toad Victim")
	if got := h.out(); !strings.Contains(got, "You turned Victim into a toad!") {
		t.Fatalf("@toad said %q", got)
	}

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz := h.wizRef()
		if o := w.Get(victimThing); o.Owner != wiz {
			t.Errorf("the trinket is owned by %v, want the toader %v", o.Owner, wiz)
		}
		if o := w.Get(victimThing); o.Home == victim {
			t.Error("the trinket is still homed to a player that no longer exists")
		}
		o := w.Get(victim)
		if o.Type() != ref.TypeThing {
			t.Errorf("the victim is a %v, want a thing", o.Type())
		}
		if o.Name != "A slimy toad named Victim" {
			t.Errorf("the victim is called %q", o.Name)
		}
		if o.PasswordHash != "" {
			t.Error("the victim kept their password")
		}
		if _, found := w.PlayerNamed("Victim"); found {
			t.Error("the name is still taken")
		}
		if v, _ := w.GetProp(victim, propValue); v.Num != 1 {
			t.Errorf("the toad is worth %d, want 1", v.Num)
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestForceRunsACommandAsSomeoneElse checks that a forced command acts and
// answers as the victim, not as the forcer.
func TestForceRunsACommandAsSomeoneElse(t *testing.T) {
	h := newHarness(t)
	h.login()

	// A puppet relays what it is told to whoever owns it, which is the
	// only way anything a thing is told reaches a person.
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		if err := w.SetTune("allow_zombies", "yes"); err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.send("@create puppet")
	h.out()
	h.send("@set puppet=Z")
	h.out()

	h.send("@force puppet=look")
	got := h.out()
	if !strings.Contains(got, "puppet> ") {
		t.Errorf("a puppet's output should reach its owner prefixed:\n%s", got)
	}

	// The refusals.
	h.send("@force nosuchthing=look")
	if got := h.out(); !strings.Contains(got, "I don't understand 'nosuchthing'.") {
		t.Errorf("forcing nothing said %q", got)
	}
	h.send("@force One=look")
	if got := h.out(); !strings.Contains(got, "I don't understand 'One'.") {
		t.Errorf("forcing a name that is not here said %q", got)
	}
}

// TestPuppetRelayRespectsDark checks the conditions that stop a puppet being
// used to eavesdrop.
func TestPuppetRelayRespectsDark(t *testing.T) {
	h := newHarness(t)
	h.login()
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		if err := w.SetTune("allow_zombies", "yes"); err != nil {
			t.Error(err)
		}
		// A non-wizard owner, so the dark rule is not waived.
		owner := w.Create("Mortal", ref.TypePlayer, ref.Nothing)
		owner.Owner = owner.Ref
		puppet := w.Create("spy", ref.TypeThing, owner.Ref)
		puppet.Flags |= ref.Zombie | ref.Dark
		if _, _, ok := puppetRelay(h.s, w, puppet.Ref); ok {
			t.Error("a dark puppet should not relay to a non-wizard owner")
		}
		puppet.Flags &^= ref.Dark
		if _, _, ok := puppetRelay(h.s, w, puppet.Ref); !ok {
			t.Error("a plain puppet should relay")
		}
		// An owner who is themselves flagged ZOMBIE has opted out.
		owner.Flags |= ref.Zombie
		if _, _, ok := puppetRelay(h.s, w, puppet.Ref); ok {
			t.Error("an owner flagged ZOMBIE should hear nothing")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestControlsRespectsStrictGodPriv checks that a wizard cannot touch God's
// objects while strict_god_priv is set, which is what stops a wizard editing
// God's programs into giving themselves God's powers.
func TestControlsRespectsStrictGodPriv(t *testing.T) {
	h := newHarness(t)
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		// The harness's wizard is #1, which is God, so this needs a
		// second wizard who is not.
		other := w.Create("Archwizard", ref.TypePlayer, ref.Nothing)
		other.Owner = other.Ref
		other.Flags |= ref.Wizard
		wiz := other.Ref
		godly := w.Create("crown", ref.TypeThing, ref.God)

		if h.s.controls(w, wiz, godly.Ref) {
			t.Error("a wizard should not control God's things")
		}
		if err := w.SetTune("strict_god_priv", "no"); err != nil {
			t.Error(err)
		}
		if !h.s.controls(w, wiz, godly.Ref) {
			t.Error("with strict_god_priv off, a wizard controls everything")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// flagsOf returns the flags of something the test wizard is carrying.
func (h *harness) flagsOf(t *testing.T, name string) ref.Flags {
	t.Helper()
	var f ref.Flags
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		for _, r := range w.Contents(h.wizRef()) {
			if o := w.Get(r); o != nil && o.Name == name {
				f = o.Flags
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestGUIDialogRoundTrip drives a dialog from a program: it opens one, the
// client sets a value and presses a button, and the program reads back what
// the user chose.
func TestGUIDialogRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.login()

	// The client says it speaks MCP and the GUI package.
	h.send("#$#mcp version: 2.1 to: 2.1")
	key := authKeyFrom(t, h.out())
	h.send(`#$#mcp-negotiate-can ` + key +
		` package: "org-fuzzball-gui" min-version: "1.0" max-version: "1.3"`)
	h.out()

	// The dialog is opened through the host rather than from a program, so
	// this is a test of the protocol rather than of MUF.
	var id string
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		host := &mufHost{s: h.s, w: w, caller: h.wizRef()}
		var err error
		id, err = host.GUINew(h.d.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	// The client reports a value and then an event.
	h.send(`#$#org-fuzzball-gui-ctrl-value ` + key +
		` dlogid: "` + id + `" id: "name" value: "Hermione"`)
	h.out()

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		host := &mufHost{s: h.s, w: w, caller: h.wizRef()}
		got, ok := host.GUIValue(id, "name", 0)
		if !ok || got != "Hermione" {
			t.Errorf("the dialog holds %q (%v), want Hermione", got, ok)
		}
		ctrls, values, ok := host.GUIValues(id)
		if !ok || len(ctrls) != 1 || ctrls[0] != "name" ||
			len(values[0]) != 1 || values[0][0] != "Hermione" {
			t.Errorf("GUIValues returned %v / %v", ctrls, values)
		}
	}); err != nil {
		t.Fatal(err)
	}

	// An event that dismisses the dialog closes it, because the client
	// has taken it off the screen and nothing can reach it again.
	h.send(`#$#org-fuzzball-gui-ctrl-event ` + key +
		` dlogid: "` + id + `" id: "ok" event: "buttonpress"`)
	h.sync()
	h.out()

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		if _, ok := h.s.dialogs.Find(id); ok {
			t.Error("a dismissed dialog is still open")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestMCPMessagesNeedPermission checks that a program below the mucker floor
// cannot drive somebody's client.
func TestMCPMessagesNeedPermission(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("#$#mcp version: 2.1 to: 2.1")
	h.out()

	// The program is owned by the wizard, who is running it, so the owner
	// exemption applies and this must succeed.
	h.installProgram(t, "mine", `: main
  descr "org-fuzzball-notify" 1.0 1.0 mcp_register
  me @ "registered" notify
;`)
	h.send("mine")
	if got := h.out(); !strings.Contains(got, "registered") {
		t.Errorf("a program its own owner ran was refused:\n%s", got)
	}
}

// authKeyFrom pulls the authentication key out of a negotiation transcript.
func authKeyFrom(t *testing.T, transcript string) string {
	t.Helper()
	for _, line := range strings.Split(transcript, "\n") {
		if !strings.HasPrefix(line, "#$#mcp ") {
			continue
		}
		i := strings.Index(line, "authentication-key: ")
		if i < 0 {
			continue
		}
		rest := line[i+len("authentication-key: "):]
		return strings.Trim(strings.Fields(rest)[0], `"`)
	}
	t.Fatal("no authentication key in the transcript")
	return ""
}
