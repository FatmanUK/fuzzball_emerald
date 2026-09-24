package world

import (
	"reflect"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

func TestRepairLeavesAHealthyWorldAlone(t *testing.T) {
	w := newTestWorld(t)
	room := w.Create("Room", ref.TypeRoom, ref.God)
	var things []ref.Ref
	for i := 0; i < 5; i++ {
		o := w.Create("thing", ref.TypeThing, ref.God)
		if err := w.MoveTo(o.Ref, room.Ref); err != nil {
			t.Fatal(err)
		}
		things = append(things, o.Ref)
	}
	before := w.Contents(room.Ref)

	if n := w.RepairChains(); n != 0 {
		t.Errorf("RepairChains repaired %d containers in a healthy world", n)
	}
	// Insertion order is newest-first and must survive untouched;
	// rebuilding in ref order here would silently reverse every
	// container in the game.
	after := w.Contents(room.Ref)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("contents order changed: %v -> %v", before, after)
	}
	if len(after) != len(things) {
		t.Errorf("contents = %v, want %d entries", after, len(things))
	}
}

func TestRepairRebuildsATruncatedChain(t *testing.T) {
	w := newTestWorld(t)
	room := w.Create("Room", ref.TypeRoom, ref.God)
	var things []ref.Ref
	for i := 0; i < 4; i++ {
		o := w.Create("thing", ref.TypeThing, ref.God)
		if err := w.MoveTo(o.Ref, room.Ref); err != nil {
			t.Fatal(err)
		}
		things = append(things, o.Ref)
	}

	// Break the chain after the first entry. Everything past the
	// break is now unreachable, though each object still knows
	// where it lives.
	head := w.Get(room.Contents)
	head.Next = ref.Nothing
	if got := len(w.Contents(room.Ref)); got != 1 {
		t.Fatalf("setup: chain has %d entries, want 1", got)
	}

	if n := w.RepairChains(); n != 1 {
		t.Errorf("RepairChains repaired %d containers, want 1", n)
	}
	got := w.Contents(room.Ref)
	if !reflect.DeepEqual(got, things) {
		t.Errorf("rebuilt contents = %v, want %v in ref order", got, things)
	}
}

func TestRepairBreaksACycle(t *testing.T) {
	w := newTestWorld(t)
	room := w.Create("Room", ref.TypeRoom, ref.God)
	a := w.Create("A", ref.TypeThing, ref.God)
	b := w.Create("B", ref.TypeThing, ref.God)
	for _, o := range []*Object{a, b} {
		if err := w.MoveTo(o.Ref, room.Ref); err != nil {
			t.Fatal(err)
		}
	}
	// Make the tail point back at the head.
	w.Get(a.Ref).Next = b.Ref

	if n := w.RepairChains(); n != 1 {
		t.Errorf("RepairChains repaired %d containers, want 1", n)
	}
	got := w.Contents(room.Ref)
	if !reflect.DeepEqual(got, []ref.Ref{a.Ref, b.Ref}) {
		t.Errorf("rebuilt contents = %v, want [%v %v]", got, a.Ref, b.Ref)
	}
}

func TestRepairSeparatesExitsFromContents(t *testing.T) {
	w := newTestWorld(t)
	room := w.Create("Room", ref.TypeRoom, ref.God)
	thing := w.Create("Thing", ref.TypeThing, ref.God)
	exit := w.Create("north", ref.TypeExit, ref.God)
	thing.Location = room.Ref
	exit.Location = room.Ref
	// Both threaded onto the contents list, which is wrong for
	// the exit.
	room.Contents = thing.Ref
	thing.Next = exit.Ref

	if n := w.RepairChains(); n != 1 {
		t.Errorf("RepairChains repaired %d containers, want 1", n)
	}
	if got := w.Contents(room.Ref); !reflect.DeepEqual(got, []ref.Ref{thing.Ref}) {
		t.Errorf("contents = %v, want just the thing", got)
	}
	if got := w.Exits(room.Ref); !reflect.DeepEqual(got, []ref.Ref{exit.Ref}) {
		t.Errorf("exits = %v, want just the exit", got)
	}
}

func TestRepairDropsAnObjectInAMissingContainer(t *testing.T) {
	w := newTestWorld(t)
	orphan := w.Create("Orphan", ref.TypeThing, ref.God)
	orphan.Location = ref.Ref(999) // no such object

	// Nothing to repair: there is no container to rebuild a chain
	// for.
	if n := w.RepairChains(); n != 0 {
		t.Errorf("RepairChains repaired %d containers, want 0", n)
	}
}

func TestSameMembersIgnoresOrderButCatchesDuplicates(t *testing.T) {
	if !sameMembers([]ref.Ref{3, 1, 2}, []ref.Ref{1, 2, 3}) {
		t.Error("the same refs in a different order should match")
	}
	if sameMembers([]ref.Ref{1, 1}, []ref.Ref{1, 2}) {
		t.Error("a chain visiting the same object twice should not match")
	}
	if sameMembers([]ref.Ref{1}, []ref.Ref{1, 2}) {
		t.Error("different lengths should not match")
	}
	if !sameMembers(nil, nil) {
		t.Error("two empty lists should match")
	}
}
