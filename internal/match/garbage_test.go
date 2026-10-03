package match

import (
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestAbsoluteMatchesGarbage pins the difference between upstream's
// two existence macros, which is one word apart and hid two branches.
//
// absolute_name (match.c:333) guards on ObjExists (db.h:440), which
// is only `d >= 0 && d < db_top`. World.Valid is OkObj (db.h:462),
// which is ObjExists *and* not garbage. Using Valid here made every
// recycled object unnameable by number, so do_recycle's "That's
// already garbage!" and the `<recyclable>` description @examine shows
// for something still pointed at were both unreachable.
func TestAbsoluteMatchesGarbage(t *testing.T) {
	w := world.New()
	room := w.Create("Room", ref.TypeRoom, ref.Nothing)
	who := w.Create("Player", ref.TypePlayer, ref.Nothing)
	who.Owner = who.Ref
	if err := w.MoveTo(who.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}
	junk := w.Create("junk", ref.TypeThing, who.Ref)
	if err := w.MoveTo(junk.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}
	if err := w.Recycle(junk.Ref); err != nil {
		t.Fatal(err)
	}
	if w.Valid(junk.Ref) {
		t.Fatal("the object should be garbage by now")
	}

	got := New(w, who.Ref, junk.Ref.String()).Absolute().Result()
	if got != junk.Ref {
		t.Errorf("Absolute() = %v, want %v — garbage is "+
			"nameable by number", got, junk.Ref)
	}

	// A ref past the end of the database still matches nothing.
	if got := New(w, who.Ref, "#9999").
		Absolute().Result(); got != ref.Nothing {
		t.Errorf("a nonexistent ref matched %v", got)
	}
}
