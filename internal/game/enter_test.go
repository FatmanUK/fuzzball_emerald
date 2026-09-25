package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// twoRooms builds a second room with an exit to it and back, and
// returns both rooms. Movement needs somewhere to move to, and the
// harness's world has one room.
func twoRooms(t *testing.T, h *harness) (here, there ref.Ref) {
	t.Helper()
	err := h.engine.Do(context.Background(), func(w *world.World) {
		here = w.Get(h.wizRef()).Location
		room := w.Create("Workshop", ref.TypeRoom, h.wizRef())
		there = room.Ref

		out := w.Create("north", ref.TypeExit, h.wizRef())
		out.Dest = []ref.Ref{there}
		if err := w.MoveTo(out.Ref, here); err != nil {
			t.Error(err)
		}
		back := w.Create("south", ref.TypeExit, h.wizRef())
		back.Dest = []ref.Ref{here}
		if err := w.MoveTo(back.Ref, there); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()
	return here, there
}

// TestQuietMovesSilencesTheRoom covers the parameter the golden case
// cannot reach, because @tune's own reply diverges and do_tune is not
// ported.
//
// The mover's own output is unchanged either way; what quiet_moves
// suppresses is what everybody else hears.
func TestQuietMovesSilencesTheRoom(t *testing.T) {
	h := newHarness(t)
	h.login()
	twoRooms(t, h)

	// A bystander who stays put, so the room's half is readable.
	_, other := connectAs(t, h, "Bystander", false)

	h.send("north")
	h.out()
	if got := drainDescriptor(other); !strings.Contains(got,
		"Wizard has left.") {
		t.Errorf("the room was not told:\n%s", got)
	}
	h.send("south")
	h.out()
	drainDescriptor(other)

	err := h.engine.Do(context.Background(), func(w *world.World) {
		if err := w.Tune.SetString("quiet_moves",
			"yes"); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	h.send("north")
	if got := h.out(); got == "" {
		t.Error("quiet_moves silenced the mover's own output too")
	}
	if got := drainDescriptor(other); strings.Contains(got,
		"has left.") {
		t.Errorf("quiet_moves did not silence the room:\n%s", got)
	}
}

// TestAThingDoesNotAnnounceItself covers the condition that keeps a
// puppet-heavy world readable: a THING moving is silent unless it is
// a ZOMBIE or a VEHICLE, which is somebody's puppet or something
// people ride in.
func TestAThingDoesNotAnnounceItself(t *testing.T) {
	h := newHarness(t)
	h.login()
	here, there := twoRooms(t, h)

	ctx := context.Background()
	var thing ref.Ref
	err := h.engine.Do(ctx, func(w *world.World) {
		o := w.Create("trolley", ref.TypeThing, h.wizRef())
		o.Home = here
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Error(err)
		}
		thing = o.Ref
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	// The wizard stays in the room and listens.
	move := func() string {
		err := h.engine.Do(ctx, func(w *world.World) {
			s := h.s
			s.enterRoom(w, h.d.ID, thing, there, ref.Nothing)
			s.enterRoom(w, h.d.ID, thing, here, ref.Nothing)
		})
		if err != nil {
			t.Fatal(err)
		}
		return h.out()
	}

	if got := move(); strings.Contains(got, "trolley has") {
		t.Errorf("a plain thing announced itself:\n%s", got)
	}

	err = h.engine.Do(ctx, func(w *world.World) {
		w.Get(thing).Flags |= ref.Zombie
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()
	if got := move(); !strings.Contains(got, "trolley has") {
		t.Errorf("a zombie did not announce itself:\n%s", got)
	}
}

// TestPennyFind covers enter_room's tail. It is a coin flip, so the
// golden case cannot compare it; what is checkable is that a rate of
// zero never pays, a rate of one always does, and the three
// exemptions hold.
func TestPennyFind(t *testing.T) {
	h := newHarness(t)
	h.login()
	here, there := twoRooms(t, h)

	ctx := context.Background()
	// A mortal, and a room they do not control — controlling it
	// exempts the move, so a builder cannot farm their own house.
	who, mortal := connectAs(t, h, "Wanderer", false)
	err := h.engine.Do(ctx, func(w *world.World) {
		w.Get(there).Owner = ref.God
		w.Get(here).Owner = ref.God
		if err := w.Tune.SetString("penny_rate", "0"); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()
	drainDescriptor(mortal)

	if got := sendAs(t, h, mortal, "north"); strings.Contains(got,
		"You found one") {
		t.Errorf("a rate of zero paid out:\n%s", got)
	}
	sendAs(t, h, mortal, "south")

	err = h.engine.Do(ctx, func(w *world.World) {
		if err := w.Tune.SetString("penny_rate", "1"); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := sendAs(t, h, mortal, "north"); !strings.Contains(got,
		"You found one penny!") {
		t.Errorf("a rate of one did not pay out:\n%s", got)
	}
	sendAs(t, h, mortal, "south")

	// Being too rich stops it.
	err = h.engine.Do(ctx, func(w *world.World) {
		limit := w.Tune.Int("max_pennies")
		w.SetProp(who, propValue, props.Value{Type: props.Int,
			Num: limit + 1})
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := sendAs(t, h, mortal, "north"); strings.Contains(got,
		"You found one") {
		t.Errorf("a rich player was still paid:\n%s", got)
	}
}

// TestAutolookFallsBackAndStops covers autolook_cmd, and the
// recursion guard around it.
//
// The command is looked up as an *exit* first, which is what lets a
// world replace what a player sees on arriving — and is also how a
// world can write a loop, hence the counter and its own message.
func TestAutolookFallsBackAndStops(t *testing.T) {
	h := newHarness(t)
	h.login()
	_, there := twoRooms(t, h)

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		w.SetProp(there, propDesc, props.Value{Type: props.String,
			Str: "A tidy workshop."})
		if err := w.Tune.SetString("autolook_cmd",
			"nosuchcommand"); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	// An autolook command that resolves to nothing falls back to
	// look_room, so the room still appears.
	h.send("north")
	if got := h.out(); !strings.Contains(got, "A tidy workshop.") {
		t.Errorf("the fallback look did not happen:\n%s", got)
	}
	h.send("south")
	h.out()

	// An autolook exit that moves the player again loops, and the
	// guard stops it with its own message rather than hanging the
	// world goroutine.
	err = h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		loop := w.Create("loopy", ref.TypeExit, h.wizRef())
		loop.Dest = []ref.Ref{there}
		if err := w.MoveTo(loop.Ref, here); err != nil {
			t.Error(err)
		}
		back := w.Create("loopy", ref.TypeExit, h.wizRef())
		back.Dest = []ref.Ref{here}
		if err := w.MoveTo(back.Ref, there); err != nil {
			t.Error(err)
		}
		if err := w.Tune.SetString("autolook_cmd",
			"loopy"); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("north")
	if got := h.out(); !strings.Contains(got,
		"Look aborted because of look action loop.") {
		t.Errorf("the autolook loop was not stopped:\n%s", got)
	}
}
