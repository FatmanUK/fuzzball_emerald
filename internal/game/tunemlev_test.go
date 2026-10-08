package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// godRead is a parameter whose *read* level is MLEV_GOD, and whose
// value is never empty — so an empty answer means the gate fired
// rather than that there was nothing to read. Ten parameters are
// gated this way; smtp_password is one, but its default is "", which
// cannot tell a refusal from a blank.
const godRead = "strict_god_priv"

// TestMPISysparmIsGatedForAMortal is the leak this step exists for.
//
// mfn_sysparm (mfuns.c:4141) calls tune_get_parmstring with
// TUNE_MLEV(player), and player is the *triggering* player — so an
// unblessed message property is read at whoever looked at it.
// Emerald's {sysparm} called mufHost.TuneGet, which is
// tune_get_parmstring **minus its own mlev gate** and says so in its
// doc comment. So any mortal could read all 55 parameters gated at
// wizard level or above by writing {sysparm:...} in their own
// description and looking at themselves.
//
// No golden case can see this: the oracle drives #1, for whom every
// read is permitted.
func TestMPISysparmIsGatedForAMortal(t *testing.T) {
	h := newHarness(t)
	h.login()

	who, d := connectAs(t, h, "Mortal", false)
	ctx := context.Background()
	if err := h.engine.Do(ctx, func(w *world.World) {
		// The mortal's own description, so nothing but their
		// own mucker level is in play.
		w.SetProp(who, "_/de", props.Value{Type: props.String,
			Str: "<{sysparm:" + godRead + "}>"})
		if o := w.Get(who); o != nil {
			o.Flags &^= ref.Wizard
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	got := sendAs(t, h, d, "look me")
	if !strings.Contains(got, "<>") {
		t.Errorf("a mortal read a God-only parameter:\n%s",
			got)
	}

	// The control: as God the same property reads a value, so the
	// gate is what refused and not a broken evaluation.
	if err := h.engine.Do(ctx, func(w *world.World) {
		w.SetProp(h.wizRef(), "_/de",
			props.Value{Type: props.String,
				Str: "<{sysparm:" + godRead + "}>"})
	}); err != nil {
		t.Fatal(err)
	}
	h.out()
	h.send("look me")
	asGod := h.out()
	if strings.Contains(asGod, "<>") {
		t.Errorf("God should read it:\n%s", asGod)
	}
}

// TestTuneListingHidesGodReadFromAPlainWizard is the other half.
//
// TUNE_MLEV gives God 255 and everybody else their own level, and
// gen_params.py used to map MLEV_GOD to 4 — so a plain wizard read
// what upstream reserves to #1. @tune's listing filters on p.ReadMLev
// > mlev, so the parameter simply does not appear.
func TestTuneListingHidesGodReadFromAPlainWizard(t *testing.T) {
	h := newHarness(t)
	h.login()

	_, d := connectAs(t, h, "Wiz", true)
	h.out()

	got := sendAs(t, h, d, "@tune "+godRead)
	if strings.Contains(got, godRead) {
		t.Errorf("a plain wizard saw a God-read "+
			"parameter:\n%s", got)
	}
	if !strings.Contains(got, "No matching parameters.") {
		t.Errorf("want the no-match line, got:\n%s", got)
	}

	// God sees it, which is what makes the level a gate rather
	// than a parameter nobody can read.
	h.send("@tune " + godRead)
	asGod := h.out()
	if !strings.Contains(asGod, godRead) {
		t.Errorf("God should see it:\n%s", asGod)
	}
}
