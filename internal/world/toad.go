package world

import (
	"fmt"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// SendHome moves an object to where it belongs when its container is going
// away: a thing or player to its home, anything else to the global
// environment.
//
// A home that no longer exists, or that would put the object inside itself,
// falls back to the lost-and-found room, which is what that parameter is for.
func (w *World) SendHome(r ref.Ref) error {
	o := w.Get(r)
	if o == nil {
		return fmt.Errorf("no object at %v", r)
	}
	dest := o.Home
	if o.Type() != ref.TypeThing && o.Type() != ref.TypePlayer {
		dest = ref.GlobalEnvironment
	}
	if !w.Valid(dest) || w.contains(r, dest) || dest == r {
		dest = w.Tune.Ref("lost_and_found")
	}
	if !w.Valid(dest) {
		dest = ref.GlobalEnvironment
	}
	return w.MoveTo(r, dest)
}

// Toad turns a player into a thing, which is how a player is deleted.
//
// The object survives, because things all over the database point at it — as
// an owner, a home, a lock — and removing it outright would leave those
// dangling. It stops being a player instead: it loses its password, leaves the
// player index so the name can be taken again, and belongs to whoever did it.
func (w *World) Toad(victim, newOwner ref.Ref, newName string) error {
	o := w.objs[victim]
	if o == nil {
		return fmt.Errorf("no object at %v", victim)
	}
	if o.Type() != ref.TypePlayer {
		return fmt.Errorf("%v is not a player", victim)
	}

	delete(w.players, ascii.Fold(o.Name))
	o.Name = newName
	o.PasswordHash = ""
	o.Owner = newOwner
	// Every flag goes, not just the type: upstream assigns TYPE_THING over
	// the whole word, so a toaded wizard keeps none of their powers.
	o.Flags = ref.Flags(0).WithType(ref.TypeThing)
	if owner := w.Get(newOwner); owner != nil {
		o.Home = owner.Home
	}
	w.Modified(victim)
	return nil
}
