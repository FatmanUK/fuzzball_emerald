package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestLeaveRefusals covers do_leave's three refusals that the golden
// case cannot reach. Getting inside a vehicle needs an exit *inside*
// it — trigger() requires dest == LOCATION(exit) — and no
// implemented command makes one, since @action still aliases @open.
//
// Each refusal is its own sentence and they are not interchangeable.
func TestLeaveRefusals(t *testing.T) {
	h := newHarness(t)
	h.login()

	// In a room.
	h.send("leave")
	if got := h.out(); !strings.Contains(got,
		"You can't go that way.") {
		t.Errorf("leaving a room said:\n%s", got)
	}

	ctx := context.Background()
	var crate, cart, pocket ref.Ref
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		c := w.Create("crate", ref.TypeThing, h.wizRef())
		c.Home = here
		if err := w.MoveTo(c.Ref, here); err != nil {
			t.Error(err)
		}
		crate = c.Ref

		v := w.Create("cart", ref.TypeThing, h.wizRef())
		v.Flags |= ref.Vehicle
		v.Home = here
		if err := w.MoveTo(v.Ref, here); err != nil {
			t.Error(err)
		}
		cart = v.Ref

		// A vehicle inside a *player* — somebody else,
		// since a vehicle inside the player about to board it
		// would be a containment loop. This is the third
		// refusal: there is nowhere outside the vehicle to
		// stand.
		bearer := w.Create("Bearer", ref.TypePlayer,
			ref.Nothing)
		bearer.Owner = bearer.Ref
		bearer.Home = here
		if err := w.MoveTo(bearer.Ref, here); err != nil {
			t.Error(err)
		}
		p := w.Create("pocket", ref.TypeThing, h.wizRef())
		p.Flags |= ref.Vehicle
		p.Home = here
		if err := w.MoveTo(p.Ref, bearer.Ref); err != nil {
			t.Error(err)
		}
		pocket = p.Ref
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	// Inside a thing that is not a vehicle.
	board := func(where ref.Ref) {
		t.Helper()
		if err := h.engine.Do(ctx, func(w *world.World) {
			err := w.MoveTo(h.wizRef(), where)
			if err != nil {
				t.Error(err)
			}
		}); err != nil {
			t.Fatal(err)
		}
		h.out()
	}

	board(crate)
	h.send("leave")
	if got := h.out(); !strings.Contains(got,
		"You can only exit vehicles.") {
		t.Errorf("leaving a plain thing said:\n%s", got)
	}

	board(pocket)
	h.send("leave")
	if got := h.out(); !strings.Contains(got,
		"You can't exit a vehicle inside of a player.") {
		t.Errorf("leaving a pocketed vehicle said:\n%s", got)
	}

	// And leaving a real vehicle works, through both spellings.
	board(cart)
	h.send("leave")
	if got := h.out(); !strings.Contains(got,
		"You exit the vehicle.") {
		t.Errorf("leaving a vehicle said:\n%s", got)
	}
	board(cart)
	h.send("disembark")
	if got := h.out(); !strings.Contains(got,
		"You exit the vehicle.") {
		t.Errorf("disembark is not do_leave:\n%s", got)
	}
}

// TestZombieTheft covers do_get's puppet check, which needs a puppet
// and so cannot be driven from the golden harness's single player
// seat.
//
// A puppet may not take something out of anything but a room unless
// it shares an owner with it, which upstream reads as stopping it
// raiding other people's containers.
func TestZombieTheft(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	var theirs, mine ref.Ref
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		other := w.Create("Stranger", ref.TypePlayer,
			ref.Nothing)
		other.Owner = other.Ref

		p := w.Create("puppet", ref.TypeThing, h.wizRef())
		p.Flags |= ref.Zombie
		p.Home = here
		if err := w.MoveTo(p.Ref, here); err != nil {
			t.Error(err)
		}

		// A bag in the room, with two things in it: one the
		// puppet's owner owns and one a stranger does.
		b := w.Create("bag", ref.TypeThing, h.wizRef())
		b.Home = here
		if err := w.MoveTo(b.Ref, here); err != nil {
			t.Error(err)
		}
		w.SetProp(b.Ref, propConLock,
			props.Value{Type: props.Lock, Str: "me"})

		t1 := w.Create("theirs", ref.TypeThing, other.Ref)
		t1.Home = here
		if err := w.MoveTo(t1.Ref, b.Ref); err != nil {
			t.Error(err)
		}
		theirs = t1.Ref

		t2 := w.Create("mine", ref.TypeThing, h.wizRef())
		t2.Home = here
		if err := w.MoveTo(t2.Ref, b.Ref); err != nil {
			t.Error(err)
		}
		mine = t2.Ref
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	// The puppet is driven with @force, which runs the command as
	// it — so c.who is the puppet and its type is THING.
	h.send("@force puppet=get bag=theirs")
	if got := h.out(); !strings.Contains(got,
		"Zombies aren't allowed to be thieves!") {
		t.Errorf("a puppet robbed a stranger:\n%s", got)
	}

	// Something its own owner owns is fine.
	h.send("@force puppet=get bag=mine")
	if got := h.out(); !strings.Contains(got, "Taken.") {
		t.Errorf("a puppet was refused its owner's:\n%s", got)
	}

	_, _ = theirs, mine
}

// TestGetFromAPlayerIsRefused covers the check that needs a second
// player holding something — and the order it comes in, which is
// not the order the messages suggest.
//
// A player has no @conlock, and a container's conlock defaults to
// *false*, so the first attempt is refused as an unopenable container
// and never reaches the steal message at all. Reaching that one means
// the player has set a conlock that passes. Both halves are
// upstream's and this is what pins the order.
func TestGetFromAPlayerIsRefused(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	var other ref.Ref
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		p := w.Create("Stranger", ref.TypePlayer, ref.Nothing)
		p.Owner = p.Ref
		p.Home = here
		if err := w.MoveTo(p.Ref, here); err != nil {
			t.Error(err)
		}
		other = p.Ref
		o := w.Create("locket", ref.TypeThing, p.Ref)
		o.Home = here
		if err := w.MoveTo(o.Ref, p.Ref); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("get Stranger=locket")
	if got := h.out(); !strings.Contains(got,
		"You can't open that container.") {
		t.Errorf("the conlock did not refuse first:\n%s", got)
	}

	// With a conlock the getter passes, the steal check is
	// reached.
	err = h.engine.Do(ctx, func(w *world.World) {
		w.SetProp(other, propConLock, props.Value{
			Type: props.Lock, Str: h.wizRef().String()})
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("get Stranger=locket")
	if got := h.out(); !strings.Contains(got,
		"You can't steal stuff from players.") {
		t.Errorf("taking from a player said:\n%s", got)
	}
}

// TestSendHomeSweepsPossessionsFirst covers send_home's ordering,
// which upstream comments on: a player's things go home before the
// player does, so they are there to be seen on arrival.
func TestSendHomeSweepsPossessionsFirst(t *testing.T) {
	h := newHarness(t)
	h.login()

	ctx := context.Background()
	var elsewhere, bag ref.Ref
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		far := w.Create("Far Room", ref.TypeRoom, h.wizRef())
		elsewhere = far.Ref

		b := w.Create("bag", ref.TypeThing, h.wizRef())
		// The bag lives here; the player will be sent home
		// from elsewhere, and the bag home separately.
		b.Home = here
		if err := w.MoveTo(b.Ref, h.wizRef()); err != nil {
			t.Error(err)
		}

		if err := w.MoveTo(h.wizRef(), far.Ref); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	err = h.engine.Do(ctx, func(w *world.World) {
		h.s.sendHome(w, h.d.ID, h.wizRef(), false)
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	err = h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		if here == elsewhere {
			t.Error("the player did not go home")
		}
		if got := w.Get(bag).Location; got == h.wizRef() {
			t.Error("the bag went home with the player " +
				"rather than ahead of them")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}
