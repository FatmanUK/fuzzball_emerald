package world

import (
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// TestParentSkipsThings checks that the environment walk lands on the
// first ancestor that is not a thing, which is what getparent
// returns.
func TestParentSkipsThings(t *testing.T) {
	w := New()
	room := w.Create("Room", ref.TypeRoom, ref.God)
	box := w.Create("box", ref.TypeThing, ref.God)
	coin := w.Create("coin", ref.TypeThing, ref.God)
	if err := w.MoveTo(box.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}
	if err := w.MoveTo(coin.Ref, box.Ref); err != nil {
		t.Fatal(err)
	}

	if got := w.Parent(coin.Ref); got != room.Ref {
		t.Errorf("Parent(coin) = %v, want the room %v", got, room.Ref)
	}
}

// TestParentOfAVehicleIsItsHome checks the VEHICLE rule, including
// the extra step a vehicle takes when its home is a player.
func TestParentOfAVehicleIsItsHome(t *testing.T) {
	w := New()
	garage := w.Create("Garage", ref.TypeRoom, ref.God)
	house := w.Create("House", ref.TypeRoom, ref.God)
	driver := w.Create("Driver", ref.TypePlayer, ref.God)
	driver.Home = house.Ref
	car := w.Create("car", ref.TypeThing, ref.God)
	car.Flags |= ref.Vehicle
	car.Home = driver.Ref
	if err := w.MoveTo(car.Ref, garage.Ref); err != nil {
		t.Fatal(err)
	}

	// Not the garage it sits in, and not the player it belongs
	// to: a vehicle homed to a player inherits from that player's
	// home.
	if got := w.Parent(car.Ref); got != house.Ref {
		t.Errorf("Parent(car) = %v, want the house %v", got, house.Ref)
	}
}

// TestParentBreaksALoop checks that a cycle of vehicle homes resolves
// to the global environment rather than spinning.
func TestParentBreaksALoop(t *testing.T) {
	w := New()
	a := w.Create("a", ref.TypeThing, ref.God)
	b := w.Create("b", ref.TypeThing, ref.God)
	for _, o := range []*Object{a, b} {
		o.Flags |= ref.Vehicle
	}
	a.Home = b.Ref
	b.Home = a.Ref

	if got := w.Parent(a.Ref); got != ref.GlobalEnvironment {
		t.Errorf("Parent of a looped vehicle = %v, want #0", got)
	}
}

// TestEnvPropWalksOutwards checks that a property is found on an
// ancestor and that a nearer one wins.
func TestEnvPropWalksOutwards(t *testing.T) {
	w := New()
	outer := w.Create("Outer", ref.TypeRoom, ref.God)
	inner := w.Create("Inner", ref.TypeRoom, ref.God)
	who := w.Create("Someone", ref.TypePlayer, ref.God)
	if err := w.MoveTo(inner.Ref, outer.Ref); err != nil {
		t.Fatal(err)
	}
	if err := w.MoveTo(who.Ref, inner.Ref); err != nil {
		t.Fatal(err)
	}
	w.SetProp(outer.Ref, "_reg/lib", props.Value{Type: props.Ref, Ref: ref.Ref(42)})

	v, on, ok := w.EnvProp(who.Ref, "_reg/lib")
	if !ok || on != outer.Ref || v.Ref != ref.Ref(42) {
		t.Fatalf("EnvProp found %v on %v (ok=%v), want #42 on %v", v.Ref, on, ok, outer.Ref)
	}

	// A nearer registration shadows the outer one.
	w.SetProp(inner.Ref, "_reg/lib", props.Value{Type: props.Ref, Ref: ref.Ref(7)})
	if v, on, _ := w.EnvProp(who.Ref, "_reg/lib"); on != inner.Ref ||
		v.Ref != ref.Ref(7) {
		t.Errorf("EnvProp found %v on %v, want #7 on %v", v.Ref, on, inner.Ref)
	}
}
