package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

const propRestrictedMsg = "Permission denied. (The property is " +
	"restricted.)"

// TestSetPropRestrictedForAMortal is the half of `@set`'s property
// guard no transcript can reach.
//
// `propRestricted` refuses a system property to everybody and a
// hidden or see-only one — a path with a segment starting `@` or
// `~` — to anybody who is not a wizard. `@propset` has consulted it
// since it was written; `@set` never did, so `@set` could write what
// `@propset` refused. The system half is comparable because it
// refuses a wizard too; these two halves need a mortal, and the
// oracle drives #1.
func TestSetPropRestrictedForAMortal(t *testing.T) {
	h := newHarness(t)
	h.login()

	who, d := connectAs(t, h, "Mortal", false)
	ctx := context.Background()
	if err := h.engine.Do(ctx, func(w *world.World) {
		if o := w.Get(who); o != nil {
			o.Flags &^= ref.Wizard
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	// A mortal setting a hidden or see-only property on their own
	// object is still refused: the sigil is the question, not who
	// owns it.
	for _, path := range []string{"@mine", "~mine",
		"_ok/@buried", "_ok/~buried"} {

		got := sendAs(t, h, d, "@set me="+path+":x")
		if !strings.Contains(got, propRestrictedMsg) {
			t.Errorf("@set me=%s:x was allowed:\n%s",
				path, got)
		}
		if err := h.engine.Do(ctx, func(w *world.World) {
			if _, ok := w.GetProp(who, path); ok {
				t.Errorf("%s was written", path)
			}
		}); err != nil {
			t.Fatal(err)
		}
	}

	// The control: an ordinary property on the same object goes
	// through, so the guard is the sigil and not a blanket
	// refusal.
	got := sendAs(t, h, d, "@set me=_fine:yes")
	if !strings.Contains(got, "Property set.") {
		t.Errorf("an ordinary property was refused:\n%s", got)
	}
}

// TestSetPropGodGuardIsUnreachable records a guard that cannot fire,
// in upstream or here, and pins what refuses instead.
//
// do_set checks strict_god_priv itself (set.c:752) and answers "Only
// God may touch God's property." -- a wording upstream uses nowhere
// else, since wiz.c:429 and :469 say "God's stuff" for the same idea.
// I added it as the seventh of @set's divergences and it is not one:
// controls() (db.c) already contains
//
//	if (tp_strict_god_priv && God(OWNER(what)) && !God(who))
//	    return 0;
//
// which is the same condition, and do_set calls match_controlled
// *before* its own check. So every case the guard would catch has
// already been refused, with match_controlled's wording, and the
// observable behaviour was correct before the guard was added.
//
// The guard is kept because it is upstream's line and because
// controls() is the sort of thing that changes; this test exists so
// that if it ever *does* become reachable, the change is noticed
// rather than discovered.
func TestSetPropGodGuardIsUnreachable(t *testing.T) {
	h := newHarness(t)
	h.login()

	// A wizard who is not God. The harness's own player is #1.
	_, d := connectAs(t, h, "Deputy", true)
	ctx := context.Background()
	if err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		o := w.Create("relic", ref.TypeThing, ref.God)
		o.Home = here
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	// match_controlled is what answers, not the God guard.
	const matched = "Permission denied. (You don't control " +
		"what was matched)"
	for _, cmd := range []string{"@set relic=_x:y",
		"@set relic=dark"} {

		got := sendAs(t, h, d, cmd)
		if !strings.Contains(got, matched) {
			t.Errorf("%q: want match_controlled's "+
				"refusal, got:\n%s", cmd, got)
		}
		if strings.Contains(got, "God's property") {
			t.Errorf("%q reached do_set's God "+
				"guard, which controls() makes "+
				"unreachable; if that changed "+
				"deliberately, update this "+
				"test:\n%s", cmd, got)
		}
	}

	// God is not stopped, which is what makes controls()'s clause
	// a guard rather than a ban.
	h.send("@set relic=_x:y")
	if got := h.out(); !strings.Contains(got, "Property set.") {
		t.Errorf("God was refused:\n%s", got)
	}
}

// TestSetClearLeavesWizardPropsForAMortal is remove_property_list's
// `allp == 0` half: ":clear" from somebody who is not a wizard
// removes their own properties and leaves the '@' and '~' ones and
// the whole "_/" propdir alone — which is where the message
// properties the verbs write live.
func TestSetClearLeavesWizardPropsForAMortal(t *testing.T) {
	h := newHarness(t)
	h.login()

	who, d := connectAs(t, h, "Mortal", false)
	ctx := context.Background()
	if err := h.engine.Do(ctx, func(w *world.World) {
		if o := w.Get(who); o != nil {
			o.Flags &^= ref.Wizard
		}
		// Written behind the command's back, because the
		// command is what refuses to write them.
		for _, path := range []string{"@wiz", "~see", "_/de",
			"_mine", "_also/mine"} {

			w.SetProp(who, path, props.Value{
				Type: props.String, Str: "x",
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	got := sendAs(t, h, d, "@set me=:clear")
	const want = "All user-owned properties removed."
	if !strings.Contains(got, want) {
		t.Errorf("want %q, got:\n%s", want, got)
	}

	if err := h.engine.Do(ctx, func(w *world.World) {
		kept := []string{"@wiz", "~see", "_/de"}
		for _, path := range kept {
			if _, ok := w.GetProp(who, path); !ok {
				t.Errorf("%s removed by a mortal's "+
					":clear", path)
			}
		}
		for _, path := range []string{"_mine", "_also/mine"} {
			if _, ok := w.GetProp(who, path); ok {
				t.Errorf("%s survived :clear", path)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
}
