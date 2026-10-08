package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// The half of `can_link_to` (`predicates.c:117`) a wizard cannot
// demonstrate: `controls` short-circuits the flag test and the link
// lock, and the oracle's only player is a wizard who owns the world.
//
// Two things were wrong and both needed a mortal to see. `Linkable`
// (`db.h:576`) asks for **ABODE on a room or a thing** and **LINK_OK
// on anything else**; what stood here asked for LINK_OK first and
// then ABODE for everything but a thing, which is right for neither.
// And `@linklock` was written by its command, displayed by `examine`
// as "Link_OK Key", and consulted only by `can_teleport_to` -- never
// by the link path it is named for.
func TestCanLinkToFlagsAndLockForAMortal(t *testing.T) {
	h := newHarness(t)
	h.login()

	who, d := mortal(t, h, "Hiker")
	var hall, shed ref.Ref
	ctx := context.Background()
	if err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(who).Location
		// A room and a thing somebody else owns, so the
		// mortal has to go through the flag test.
		r := w.Create("Hall", ref.TypeRoom, h.wizRef())
		r.Location = ref.GlobalEnvironment
		hall = r.Ref
		o := w.Create("shed", ref.TypeThing, h.wizRef())
		o.Home = here
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Fatal(err)
		}
		shed = o.Ref
		// And something of the mortal's own to link.
		p := w.Create("pack", ref.TypeThing, who)
		p.Home = here
		if err := w.MoveTo(p.Ref, who); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	const refused = "Permission denied. (you don't control the " +
		"thing, or you can't link to dest)"

	set := func(r ref.Ref, add, clear ref.Flags) {
		t.Helper()
		if err := h.engine.Do(ctx, func(w *world.World) {
			o := w.Get(r)
			o.Flags = (o.Flags | add) &^ clear
		}); err != nil {
			t.Fatal(err)
		}
		h.out()
	}

	// A room with LINK_OK and no ABODE is **not** linkable: the
	// old test would have allowed this, which is the bug in one
	// line.
	set(hall, ref.LinkOK, ref.Abode)
	if got := sendAs(t, h, d, "@link pack=#"+
		itoa(int(hall))); !strings.Contains(got, refused) {
		t.Errorf("a LINK_OK room was accepted as a home:\n%s",
			got)
	}

	// With ABODE it is.
	set(hall, ref.Abode, ref.LinkOK)
	if got := sendAs(t, h, d, "@link pack=#"+
		itoa(int(hall))); !strings.Contains(got,
		"Home set.") {
		t.Errorf("an ABODE room was refused:\n%s", got)
	}

	// A thing is the same way round, and the old test's one
	// correct case: ABODE, not LINK_OK.
	set(shed, ref.LinkOK, ref.Abode)
	if got := sendAs(t, h, d, "@link pack=shed"); !strings.
		Contains(got, refused) {
		t.Errorf("a LINK_OK thing was accepted:\n%s", got)
	}
	set(shed, ref.Abode, ref.LinkOK)
	if got := sendAs(t, h, d, "@link pack=shed"); !strings.
		Contains(got, "Home set.") {
		t.Errorf("an ABODE thing was refused:\n%s", got)
	}

	// And the link lock, which nothing on this path read. A lock
	// nobody passes closes an ABODE room again; test_lock
	// defaults *true*, so removing the lock reopens it.
	if err := h.engine.Do(ctx, func(w *world.World) {
		w.Get(hall).Flags |= ref.Abode
	}); err != nil {
		t.Fatal(err)
	}
	h.out()
	h.send("@linklock #" + itoa(int(hall)) + "=me&!me")
	h.out()
	if got := sendAs(t, h, d, "@link pack=#"+
		itoa(int(hall))); !strings.Contains(got, refused) {
		t.Errorf("the link lock was not consulted:\n%s", got)
	}
	// With no "=" the command *reports* the lock rather than
	// clearing it; an empty value is what clears one.
	h.send("@linklock #" + itoa(int(hall)) + "=")
	h.out()
	if got := sendAs(t, h, d, "@link pack=#"+
		itoa(int(hall))); !strings.Contains(got,
		"Home set.") {
		t.Errorf("an unlocked ABODE room was refused:\n%s",
			got)
	}
}
