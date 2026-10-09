package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

const restrictedFlag = "Permission denied. (restricted flag)"

// The rules in `unable_to_set_flag` (`set.c:537`) that no transcript
// can reach. Most of the function *is* comparable, because a quelled
// wizard is a mortal for every `Wizard(OWNER(player))` test in it and
// the oracle can quell itself — `internal/golden/setflag_test.go`
// does exactly that. What is left needs either a guest, a second
// player, or an asker who is a wizard and not God, and the oracle
// drives #1.

// mortal makes a player with no flags at all, connected.
func mortal(t *testing.T, h *harness,
	name string) (ref.Ref, *session.Descriptor) {

	t.Helper()
	who, d := connectAs(t, h, name, false)
	if err := h.engine.Do(context.Background(),
		func(w *world.World) {
			if o := w.Get(who); o != nil {
				o.Flags &^= ref.Wizard
			}
		}); err != nil {
		t.Fatal(err)
	}
	h.out()
	return who, d
}

// TestSetFlagRefusesAGuest covers the guard that sits between the
// property branch and the flag branch, and which this server had
// nowhere.
//
// A guest may `@set` a **property** — the check is after the
// property branch returns — and exactly one flag: its own GUEST
// bit, and only while it is also a wizard, which is how a world lets
// a guest stop being one. Note the condition reads the asker's own
// wizardry rather than its owner's, alone in this command.
//
// It is not oracle-visible because ISGUEST is `(FLAGS & GUEST) &&
// !God(x)` under GOD_PRIV, so #1 can never be a guest however it is
// flagged.
func TestSetFlagRefusesAGuest(t *testing.T) {
	h := newHarness(t)
	h.login()

	who, d := mortal(t, h, "Visitor")
	ctx := context.Background()
	if err := h.engine.Do(ctx, func(w *world.World) {
		w.Get(who).Flags |= ref.Guest
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	const refused = "Guests are not allowed to @set."

	// Every flag, including one a mortal would otherwise be
	// allowed, and including the empty name — the guard runs
	// before "You must specify a flag to set."
	for _, cmd := range []string{"@set me=haven", "@set me=!G",
		"@set me=", "@set me=!"} {

		if got := sendAs(t, h, d, cmd); !strings.Contains(got,
			refused) {
			t.Errorf("%q was allowed:\n%s", cmd, got)
		}
	}

	// A property is not a flag, and goes through.
	if got := sendAs(t, h, d, "@set me=_fine:yes"); !strings.
		Contains(got, "Property set.") {
		t.Errorf("a guest was refused a property:\n%s", got)
	}

	// A guest who is also a wizard may clear GUEST, and nothing
	// else. "!G" is the only spelling that passes all three
	// halves of the condition.
	if err := h.engine.Do(ctx, func(w *world.World) {
		w.Get(who).Flags |= ref.Wizard
	}); err != nil {
		t.Fatal(err)
	}
	h.out()
	if got := sendAs(t, h, d, "@set me=!guest"); !strings.
		Contains(got, "Flag reset.") {
		t.Errorf("a guest wizard could not un-guest "+
			"itself:\n%s", got)
	}
	if err := h.engine.Do(ctx, func(w *world.World) {
		if w.Get(who).Flags&ref.Guest != 0 {
			t.Error("the GUEST bit survived")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestSetFlagMuckerCeilingForAMortal covers the half of the
// mucker-bit rules a wizard cannot demonstrate.
//
// A non-wizard may raise their own program only to their own **raw**
// mucker level — `MLevRaw`, so the wizard bit lends nothing — and
// the refusal interpolates the level asked for. This server put every
// mucker level behind a blanket `requireWizard`, so a mortal could
// not set one at all.
//
// The oracle reaches the *other* clause of the same rule, by quelling
// itself and naming a thing; it cannot reach this one, because #1 is
// raw mucker 3 and so has no ceiling to hit.
func TestSetFlagMuckerCeilingForAMortal(t *testing.T) {
	h := newHarness(t)
	h.login()

	who, d := mortal(t, h, "Coder")
	ctx := context.Background()
	if err := h.engine.Do(ctx, func(w *world.World) {
		o := w.Get(who)
		o.Flags = o.Flags.SetMLevel(2)
		p := w.Create("widget.muf", ref.TypeProgram, who)
		p.Home = who
		if err := w.MoveTo(p.Ref, who); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	// Up to their own level, and not past it.
	for _, tc := range []struct{ cmd, want string }{
		{"@set widget.muf=1", "Mucker level set."},
		{"@set widget.muf=2", "Mucker level set."},
		{"@set widget.muf=3",
			"Permission denied. (You can't set that M3)"},
		{"@set widget.muf=M0", "Mucker level reset."},
	} {
		got := sendAs(t, h, d, tc.cmd)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%q: want %q, got:\n%s", tc.cmd,
				tc.want, got)
		}
	}

	// Mucker 4 is never assigned by name, whoever asks.
	if got := sendAs(t, h, d, "@set widget.muf=4"); !strings.
		Contains(got, "To set Mucker Level 4") {
		t.Errorf("M4 was not refused by name:\n%s", got)
	}
}

// TestSetFlagMuckerOwnershipClauseIsReachedThroughTheOwnlock covers
// the clause that used to be unreachable, and the route that makes it
// live.
//
// Both mucker rules test `OWNER(player) != OWNER(thing)` as well as
// the type, but `do_set` runs `match_controlled` first, and
// `controls` (`db.c:1822`) refuses a non-wizard anything they do not
// own — so the clause looks dead. Upstream has two ways past
// `controls` without owning the object: `tp_realms_control`, which is
// off by default and still unported, and an **ownership lock**, which
// any mortal may be let through.
//
// `@ownlock` wrote `@/olk`, `examine` displayed it as "Ownership
// Key", and nothing read it. Now `World.OwnLockPasses` does, so a
// mortal the lock admits reaches `unableToSetFlag` and gets *its*
// refusal instead of the matcher's.
//
// Two things it takes to see the clause at all, both found by running
// it. The lock has to be stored as a **Lock-typed** property, which
// is what `@ownlock` itself writes — `lockPasses` ignores a plain
// string, so an earlier version set `props.Value{Str: ...}` and
// proved nothing either way. And the program has to belong to
// somebody who is **not God**, because `do_set`'s own
// `strict_god_priv` guard (`build.go:1212`) sits between the matcher
// and `unableToSetFlag` and answers "Only God may touch God's
// property." first — a guard that was itself unreachable for a
// mortal until the ownlock was read.
func TestSetFlagMuckerClauseReachedThroughOwnlock(
	t *testing.T) {

	h := newHarness(t)
	h.login()

	_, d := mortal(t, h, "Stranger")
	author, _ := mortal(t, h, "Author")
	ctx := context.Background()
	if err := h.engine.Do(ctx, func(w *world.World) {
		wiz := h.wizRef()
		p := w.Create("theirs.muf", ref.TypeProgram, author)
		p.Home = w.Get(wiz).Location
		if err := w.MoveTo(p.Ref,
			w.Get(wiz).Location); err != nil {
			t.Fatal(err)
		}
		// The ownlock every mortal passes.
		w.SetProp(p.Ref, "@/olk",
			props.Value{Type: props.Lock, Str: "me|!me"})
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	const matched = "You don't control what was matched"
	for cmd, want := range map[string]string{
		"@set theirs.muf=2":  "(You can't set that M2)",
		"@set theirs.muf=M0": "(You can't set that M0)",
	} {
		got := sendAs(t, h, d, cmd)
		if !strings.Contains(got, want) {
			t.Errorf("%q: want %q, got:\n%s", cmd, want,
				got)
		}
		if strings.Contains(got, matched) {
			t.Errorf("%q was refused by the matcher, so "+
				"the ownlock is not being read:\n%s",
				cmd, got)
		}
	}
}

// TestOwnlockIsAskedAboutTheOwnerNotTheAsker pins the one line of
// `controls` that no transcript can see: `who = OWNER(who)` happens
// **before** the ownlock is consulted, so a puppet is admitted by a
// lock naming its owner.
//
// The golden case cannot reach it because the asker there is always
// the player, who owns themselves; a lock that admits everybody
// cannot tell the two apart either. Nor can a lock **constant**
// naming the owner, because `eval_boolexp_rec`'s CONST case already
// passes for `OWNER(player)` — so the substitution is invisible
// through it. A **property** lock is what separates them:
// `has_property` looks at the player and what the player carries, and
// a thing carries none of its owner's properties.
func TestOwnlockIsAskedAboutTheOwnerNotTheAsker(t *testing.T) {
	h := newHarness(t)
	h.login()

	stranger, _ := mortal(t, h, "Stranger")
	author, _ := mortal(t, h, "Author")
	ctx := context.Background()
	if err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(stranger).Location
		cart := w.Create("cart", ref.TypeThing, stranger)
		cart.Home = here
		if err := w.MoveTo(cart.Ref, here); err != nil {
			t.Fatal(err)
		}
		p := w.Create("theirs.muf", ref.TypeProgram, author)
		p.Home = here
		if err := w.MoveTo(p.Ref, here); err != nil {
			t.Fatal(err)
		}
		// A property lock, and the property on Stranger
		// rather than on the cart.
		w.SetProp(stranger, "key",
			props.Value{Type: props.String,
				Str: "yes"})
		w.SetProp(p.Ref, "@/olk", props.Value{
			Type: props.Lock,
			Str:  "key:yes",
		})
		if !w.Controls(stranger, p.Ref) {
			t.Error("the owner should be admitted")
		}
		if !w.Controls(cart.Ref, p.Ref) {
			t.Error("their thing should be " +
				"admitted too: controls asks " +
				"about OWNER(who)")
		}
		if w.Controls(author, cart.Ref) {
			t.Error("an unlocked object should not be")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestOwnlockIsNotConsultedWhenUnset is the other half: without the
// lock, the matcher refuses first and the clause above is never
// reached. test_lock_false_default answers **false** for an unset
// lock rather than passing it, which is the whole reason a property
// nobody has written does not open every object in the world.
func TestOwnlockIsNotConsultedWhenUnset(t *testing.T) {
	h := newHarness(t)
	h.login()

	_, d := mortal(t, h, "Stranger")
	ctx := context.Background()
	if err := h.engine.Do(ctx, func(w *world.World) {
		wiz := h.wizRef()
		p := w.Create("theirs.muf", ref.TypeProgram, wiz)
		p.Home = w.Get(wiz).Location
		if err := w.MoveTo(p.Ref,
			w.Get(wiz).Location); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	got := sendAs(t, h, d, "@set theirs.muf=2")
	if !strings.Contains(got,
		"You don't control what was matched") {
		t.Errorf("want the matcher's refusal, got:\n%s", got)
	}
}

// TestSetFlagVehicleAndZombieSelfRestrictions covers the two rules
// that read a flag on the **asker** rather than on the object, and
// which exist so a wizard can stop one player using puppets or
// vehicles without stopping anybody else.
//
// Both were unguarded here: ZOMBIE and VEHICLE were absent from
// `wizardOnlyFlags` altogether, so the restriction a wizard applies
// to a player did nothing at all. Neither is oracle-visible, because
// the restricted asker has to be somebody other than #1 — quelling
// will not do, since the point is the asker's own flag.
func TestSetFlagVehicleAndZombieSelfRestrictions(t *testing.T) {
	h := newHarness(t)
	h.login()

	who, d := mortal(t, h, "Grounded")
	ctx := context.Background()
	if err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(who).Location
		for _, n := range []string{"cart", "doll"} {
			o := w.Create(n, ref.TypeThing, who)
			o.Home = here
			if err := w.MoveTo(o.Ref, here); err != nil {
				t.Fatal(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	// Unrestricted, both go through: these are a mortal's own
	// things and neither flag is wizard-only.
	for _, cmd := range []string{"@set cart=vehicle",
		"@set doll=zombie"} {

		if got := sendAs(t, h, d, cmd); !strings.Contains(got,
			"Flag set.") {
			t.Errorf("%q was refused:\n%s", cmd, got)
		}
	}
	if got := sendAs(t, h, d, "@set cart=!V"); !strings.
		Contains(got, "Flag reset.") {
		t.Errorf("clearing V was refused:\n%s", got)
	}
	if got := sendAs(t, h, d, "@set doll=!Z"); !strings.
		Contains(got, "Flag reset.") {
		t.Errorf("clearing Z was refused:\n%s", got)
	}

	// Now restrict the player, which is what the flags mean on a
	// player rather than on a thing.
	if err := h.engine.Do(ctx, func(w *world.World) {
		w.Get(who).Flags |= ref.Vehicle | ref.Zombie
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	for _, cmd := range []string{"@set cart=vehicle",
		"@set doll=zombie"} {

		if got := sendAs(t, h, d, cmd); !strings.Contains(got,
			restrictedFlag) {
			t.Errorf("%q was allowed to a restricted "+
				"player:\n%s", cmd, got)
		}
	}

	// VEHICLE's restriction reads the asker's own flag where
	// ZOMBIE's reads the owner's. They coincide for a player, who
	// owns itself, and the two are kept apart because upstream
	// does.
	if err := h.engine.Do(ctx, func(w *world.World) {
		if w.Get(who).Flags&ref.Vehicle == 0 {
			t.Error("the asker lost its own V bit")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestSetFlagWizardAndQuellAreGodOnly covers the two rules whose gate
// is God rather than wizardry, which the oracle cannot see because
// its only player *is* God.
//
// Under GOD_PRIV — which upstream defines by default — only God
// may make or unmake a wizard, and only God may quell or unquell one.
// This server had both flags in a wizard-only map, so any wizard
// could promote anybody and quell a colleague.
func TestSetFlagWizardAndQuellAreGodOnly(t *testing.T) {
	h := newHarness(t)
	h.login()

	// The harness's own player is #1, so this one is a wizard who
	// is not God.
	deputyRef, deputy := connectAs(t, h, "Deputy", true)
	var hopeful, peer, brick ref.Ref
	ctx := context.Background()
	if err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		for _, spec := range []struct {
			name string
			wiz  bool
			dst  *ref.Ref
		}{{"Hopeful", false, &hopeful},
			{"Peer", true, &peer}} {

			o := w.Create(spec.name, ref.TypePlayer,
				ref.Nothing)
			o.Owner = o.Ref
			if spec.wiz {
				o.Flags |= ref.Wizard
			}
			o.Home = here
			if err := w.MoveTo(o.Ref, here); err != nil {
				t.Fatal(err)
			}
			*spec.dst = o.Ref
		}
		// The brick belongs to the deputy. The harness's own
		// wizard is God, and strict_god_priv stops anybody
		// else controlling God's things, so match_controlled
		// would refuse a brick of God's before the flag rule
		// was reached -- which is how the first draft of this
		// test failed.
		o := w.Create("brick", ref.TypeThing, deputyRef)
		o.Home = here
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Fatal(err)
		}
		brick = o.Ref
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	// A wizard who is not God may not touch the wizard bit on a
	// player, in either direction.
	for _, cmd := range []string{"@set Hopeful=W",
		"@set Peer=!W"} {

		if got := sendAs(t, h, deputy, cmd); !strings.
			Contains(got, restrictedFlag) {
			t.Errorf("%q was allowed to a non-God "+
				"wizard:\n%s", cmd, got)
		}
	}

	// On anything that is not a player the same wizard may, which
	// is what makes this a *type* rule rather than a God-only
	// flag.
	if got := sendAs(t, h, deputy, "@set brick=W"); !strings.
		Contains(got, "Flag set.") {
		t.Errorf("W on a thing was refused to a wizard:\n%s",
			got)
	}

	// And the self-mortal message comes first, so a wizard
	// clearing its own bit is told why rather than being told it
	// is not God.
	got := sendAs(t, h, deputy, "@set me=!W")
	if !strings.Contains(got,
		"You cannot make yourself mortal.") {
		t.Errorf("want the self-mortal message, "+
			"got:\n%s", got)
	}

	// Quell: another wizard is God's business, a mortal is
	// anybody's, and your own bit is always your own.
	if got := sendAs(t, h, deputy, "@set Peer=Q"); !strings.
		Contains(got, restrictedFlag) {
		t.Errorf("a non-God wizard quelled a colleague:\n%s",
			got)
	}
	for _, cmd := range []string{"@set Hopeful=Q", "@set me=Q",
		"@set me=!Q"} {

		if got := sendAs(t, h, deputy, cmd); !strings.
			Contains(got, "Flag ") {
			t.Errorf("%q was refused:\n%s", cmd, got)
		}
	}

	// God may, which is the clause the gate is made of.
	h.send("@set Peer=Q")
	if got := h.out(); !strings.Contains(got, "Flag set.") {
		t.Errorf("God could not quell a wizard:\n%s", got)
	}
	if err := h.engine.Do(ctx, func(w *world.World) {
		if w.Get(peer).Flags&ref.Quell == 0 {
			t.Error("the QUELL bit was not written")
		}
		if w.Get(hopeful).Flags&ref.Wizard != 0 {
			t.Error("Hopeful was promoted")
		}
		if w.Get(brick).Flags&ref.Wizard == 0 {
			t.Error("the brick did not get W")
		}
	}); err != nil {
		t.Fatal(err)
	}
}
