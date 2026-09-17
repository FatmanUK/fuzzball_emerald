package world

import (
	"reflect"
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// fixedClock makes timestamps predictable.
func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func newTestWorld(t *testing.T) *World {
	t.Helper()
	w := New()
	w.SetClock(fixedClock(time.Unix(1_700_000_000, 0).UTC()))
	return w
}

func TestCreateAllocatesSequentialRefs(t *testing.T) {
	w := newTestWorld(t)
	a := w.Create("Room Zero", ref.TypeRoom, ref.God)
	b := w.Create("One", ref.TypePlayer, ref.God)

	if a.Ref != 0 || b.Ref != 1 {
		t.Errorf("refs = %v, %v; want #0, #1", a.Ref, b.Ref)
	}
	if w.Top() != 2 {
		t.Errorf("Top() = %v, want #2", w.Top())
	}
	if w.Len() != 2 {
		t.Errorf("Len() = %d, want 2", w.Len())
	}
	if a.Type() != ref.TypeRoom || b.Type() != ref.TypePlayer {
		t.Error("types were not recorded")
	}
}

func TestPlayerIndex(t *testing.T) {
	w := newTestWorld(t)
	p := w.Create("Igor", ref.TypePlayer, ref.God)

	for _, name := range []string{"Igor", "igor", "IGOR"} {
		got, ok := w.PlayerNamed(name)
		if !ok || got != p.Ref {
			t.Errorf("PlayerNamed(%q) = %v, %v; want %v", name, got, ok, p.Ref)
		}
	}
	if _, ok := w.PlayerNamed("Nobody"); ok {
		t.Error("an unknown name should not resolve")
	}
}

func TestRenameUpdatesPlayerIndex(t *testing.T) {
	w := newTestWorld(t)
	p := w.Create("Before", ref.TypePlayer, ref.God)

	if err := w.Rename(p.Ref, "After"); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.PlayerNamed("Before"); ok {
		t.Error("the old name should no longer resolve")
	}
	if got, ok := w.PlayerNamed("After"); !ok || got != p.Ref {
		t.Error("the new name should resolve")
	}
}

func TestRenameRejectsTakenPlayerName(t *testing.T) {
	w := newTestWorld(t)
	w.Create("Taken", ref.TypePlayer, ref.God)
	other := w.Create("Other", ref.TypePlayer, ref.God)

	// Case-insensitively taken, as upstream's player table treats it.
	if err := w.Rename(other.Ref, "TAKEN"); err == nil {
		t.Error("renaming onto an existing player name should fail")
	}
	if other.Name != "Other" {
		t.Errorf("a failed rename changed the name to %q", other.Name)
	}
}

func TestMoveToLinksContentsChain(t *testing.T) {
	w := newTestWorld(t)
	room := w.Create("Room", ref.TypeRoom, ref.God)
	a := w.Create("A", ref.TypeThing, ref.God)
	b := w.Create("B", ref.TypeThing, ref.God)

	if err := w.MoveTo(a.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}
	if err := w.MoveTo(b.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}

	// Fuzzball pushes onto the head, so the newest object comes first.
	want := []ref.Ref{b.Ref, a.Ref}
	if got := w.Contents(room.Ref); !reflect.DeepEqual(got, want) {
		t.Errorf("Contents = %v, want %v", got, want)
	}
	if a.Location != room.Ref {
		t.Errorf("A's location = %v, want %v", a.Location, room.Ref)
	}
}

func TestExitsGoOnTheirOwnChain(t *testing.T) {
	w := newTestWorld(t)
	room := w.Create("Room", ref.TypeRoom, ref.God)
	thing := w.Create("Thing", ref.TypeThing, ref.God)
	exit := w.Create("north", ref.TypeExit, ref.God)

	if err := w.MoveTo(thing.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}
	if err := w.MoveTo(exit.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}

	if got := w.Contents(room.Ref); !reflect.DeepEqual(got, []ref.Ref{thing.Ref}) {
		t.Errorf("Contents = %v, want just the thing", got)
	}
	if got := w.Exits(room.Ref); !reflect.DeepEqual(got, []ref.Ref{exit.Ref}) {
		t.Errorf("Exits = %v, want just the exit", got)
	}
}

func TestMoveToUnlinksFromPreviousContainer(t *testing.T) {
	w := newTestWorld(t)
	r1 := w.Create("One", ref.TypeRoom, ref.God)
	r2 := w.Create("Two", ref.TypeRoom, ref.God)
	a := w.Create("A", ref.TypeThing, ref.God)
	b := w.Create("B", ref.TypeThing, ref.God)
	c := w.Create("C", ref.TypeThing, ref.God)

	for _, o := range []*Object{a, b, c} {
		if err := w.MoveTo(o.Ref, r1.Ref); err != nil {
			t.Fatal(err)
		}
	}
	// Move the middle of the chain, which is the case that exercises the
	// relink rather than just moving the head.
	if err := w.MoveTo(b.Ref, r2.Ref); err != nil {
		t.Fatal(err)
	}

	if got := w.Contents(r1.Ref); !reflect.DeepEqual(got, []ref.Ref{c.Ref, a.Ref}) {
		t.Errorf("source contents = %v, want [C A]", got)
	}
	if got := w.Contents(r2.Ref); !reflect.DeepEqual(got, []ref.Ref{b.Ref}) {
		t.Errorf("destination contents = %v, want [B]", got)
	}
}

func TestMoveToNothingUnlinks(t *testing.T) {
	w := newTestWorld(t)
	room := w.Create("Room", ref.TypeRoom, ref.God)
	a := w.Create("A", ref.TypeThing, ref.God)
	if err := w.MoveTo(a.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}
	if err := w.MoveTo(a.Ref, ref.Nothing); err != nil {
		t.Fatal(err)
	}
	if got := w.Contents(room.Ref); got != nil {
		t.Errorf("Contents = %v, want empty", got)
	}
	if a.Location != ref.Nothing {
		t.Errorf("location = %v, want #-1", a.Location)
	}
}

func TestMoveToRejectsLoops(t *testing.T) {
	w := newTestWorld(t)
	outer := w.Create("Outer", ref.TypeThing, ref.God)
	inner := w.Create("Inner", ref.TypeThing, ref.God)
	if err := w.MoveTo(inner.Ref, outer.Ref); err != nil {
		t.Fatal(err)
	}

	if err := w.MoveTo(outer.Ref, outer.Ref); err == nil {
		t.Error("an object must not contain itself")
	}
	if err := w.MoveTo(outer.Ref, inner.Ref); err == nil {
		t.Error("a container must not be moved inside its own contents")
	}
}

func TestChainTraversalSurvivesACycle(t *testing.T) {
	// A damaged chain must not hang the world goroutine.
	w := newTestWorld(t)
	room := w.Create("Room", ref.TypeRoom, ref.God)
	a := w.Create("A", ref.TypeThing, ref.God)
	b := w.Create("B", ref.TypeThing, ref.God)
	room.Contents = a.Ref
	a.Next = b.Ref
	b.Next = a.Ref // corrupt: points back at its predecessor

	done := make(chan []ref.Ref, 1)
	go func() { done <- w.Contents(room.Ref) }()
	select {
	case got := <-done:
		if len(got) > w.Len()+1 {
			t.Errorf("traversal returned %d entries for %d objects", len(got), w.Len())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("traversing a cyclic chain hung")
	}
}

func TestRecycle(t *testing.T) {
	w := newTestWorld(t)
	room := w.Create("Room", ref.TypeRoom, ref.God)
	p := w.Create("Doomed", ref.TypePlayer, ref.God)
	if err := w.MoveTo(p.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}
	p.Props.SetString("_/de", "a description")

	if err := w.Recycle(p.Ref); err != nil {
		t.Fatal(err)
	}
	if p.Type() != ref.TypeGarbage {
		t.Errorf("type = %v, want garbage", p.Type())
	}
	if w.Valid(p.Ref) {
		t.Error("garbage should not be a valid object")
	}
	if _, ok := w.PlayerNamed("Doomed"); ok {
		t.Error("a recycled player should leave the name index")
	}
	if got := w.Contents(room.Ref); got != nil {
		t.Errorf("room still holds %v", got)
	}
	if p.Props.Len() != 0 {
		t.Error("recycling should clear properties")
	}
	// The ref itself survives, so dangling references resolve to garbage
	// rather than to some unrelated later object.
	if w.Get(p.Ref) == nil {
		t.Error("the ref should still resolve, to garbage")
	}
}

func TestLinkMapsToTypeSpecificField(t *testing.T) {
	w := newTestWorld(t)
	room := w.Create("Room", ref.TypeRoom, ref.God)
	thing := w.Create("Thing", ref.TypeThing, ref.God)
	player := w.Create("Player", ref.TypePlayer, ref.God)

	room.SetLink(ref.Ref(7))
	thing.SetLink(ref.Ref(8))
	player.SetLink(ref.Ref(9))

	if room.Dropto != 7 || room.Link() != 7 {
		t.Errorf("room drop-to = %v", room.Dropto)
	}
	if thing.Home != 8 || thing.Link() != 8 {
		t.Errorf("thing home = %v", thing.Home)
	}
	if player.Home != 9 || player.Link() != 9 {
		t.Errorf("player home = %v", player.Home)
	}
}

func TestDirtyTracking(t *testing.T) {
	w := newTestWorld(t)
	a := w.Create("A", ref.TypeThing, ref.God)
	if w.DirtyCount() != 1 {
		t.Errorf("a new object should be dirty, got %d", w.DirtyCount())
	}

	s := w.TakeSnapshot()
	if len(s.Objects) != 1 || s.Objects[0].Ref != a.Ref {
		t.Errorf("snapshot = %+v, want just A", s.Objects)
	}
	if w.DirtyCount() != 0 {
		t.Error("taking a snapshot should clear the dirty set")
	}
	if w.TakeSnapshot().Empty() != true {
		t.Error("a second snapshot with no changes should be empty")
	}

	w.SetProp(a.Ref, "_/de", props.Value{Type: props.String, Str: "x"})
	if w.DirtyCount() != 1 {
		t.Error("setting a property should dirty the object")
	}
}

func TestSnapshotIsDeepCopy(t *testing.T) {
	w := newTestWorld(t)
	a := w.Create("Original", ref.TypeThing, ref.God)
	a.Props.SetString("p", "before")
	a.Dest = []ref.Ref{1, 2}

	s := w.TakeSnapshot()

	// Mutate the live object after snapshotting.
	a.Name = "Changed"
	a.Props.SetString("p", "after")
	a.Dest[0] = 99

	got := s.Objects[0]
	if got.Name != "Original" {
		t.Errorf("snapshot name = %q, want Original", got.Name)
	}
	if v, _ := got.Props.Get("p"); v.Str != "before" {
		t.Errorf("snapshot property = %q, want before", v.Str)
	}
	if got.Dest[0] != 1 {
		t.Errorf("snapshot destination = %v, want #1", got.Dest[0])
	}
}

func TestTuneChangesAreSnapshotted(t *testing.T) {
	w := newTestWorld(t)
	if s := w.TakeSnapshot(); s.Tune != nil {
		t.Error("an unchanged parameter table should not be snapshotted")
	}
	if err := w.SetTune("penny", "Groat"); err != nil {
		t.Fatal(err)
	}
	s := w.TakeSnapshot()
	if s.Tune == nil {
		t.Fatal("a changed parameter table should be snapshotted")
	}
	if s.Tune["penny"] != "Groat" {
		t.Errorf("snapshot penny = %q, want Groat", s.Tune["penny"])
	}
	if len(s.Tune) != len(w.Tune.Params()) {
		t.Errorf("snapshot has %d parameters, want the whole table (%d)",
			len(s.Tune), len(w.Tune.Params()))
	}
}

func TestAddRejectsDuplicates(t *testing.T) {
	w := newTestWorld(t)
	o := newObject(ref.Ref(5), "Thing", ref.TypeThing, ref.God, w.Now())
	if err := w.Add(o); err != nil {
		t.Fatal(err)
	}
	if w.Top() != 6 {
		t.Errorf("Top() = %v, want #6: Add should raise the ceiling", w.Top())
	}
	if err := w.Add(o); err == nil {
		t.Error("adding the same ref twice should fail")
	}
	if w.DirtyCount() != 0 {
		t.Error("Add should not dirty: it replays state that is already stored")
	}
}

func TestEachVisitsInRefOrder(t *testing.T) {
	w := newTestWorld(t)
	for i := 0; i < 5; i++ {
		w.Create("thing", ref.TypeThing, ref.God)
	}
	var got []ref.Ref
	w.Each(func(o *Object) bool {
		got = append(got, o.Ref)
		return true
	})
	want := []ref.Ref{0, 1, 2, 3, 4}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Each order = %v, want %v", got, want)
	}
}
