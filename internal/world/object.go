// Package world holds the in-memory object graph and the single
// goroutine that owns it.
//
// Fuzzball is single-threaded, and MUF depends on that: primitives
// mutate the graph non-atomically and multitasking is cooperative,
// yielding at instruction-count slices. Emerald keeps one goroutine
// that owns every object and runs the interpreter; connections and
// the persister talk to it over channels. That makes the port a close
// translation of the C control flow and removes data races by
// construction.
package world

import (
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Object is a single database object. Fields that apply to only some
// types are grouped below and documented with the types that use
// them; upstream keeps them in a union, which Go has no use for.
type Object struct {
	Ref   ref.Ref
	Name  string
	Flags ref.Flags
	Owner ref.Ref

	// Location is the container this object sits in. For an exit
	// it is the object the exit is attached to.
	Location ref.Ref

	// Contents, Exits and Next are the heads and links of the
	// intrusive lists Fuzzball threads objects onto. They are
	// kept because traversal order is observable from MUF, and
	// persisted as an explicit index so a damaged chain cannot
	// orphan objects on reload.
	Contents ref.Ref
	Exits    ref.Ref
	Next     ref.Ref

	Props *props.Tree

	Created  time.Time
	Modified time.Time
	LastUsed time.Time
	UseCount int32

	// Home is where a thing or player goes when sent home.
	Home ref.Ref
	// Dropto is a room's drop-to destination.
	Dropto ref.Ref
	// Dest lists an exit's destinations. A dump stores the count
	// first, then each entry.
	Dest []ref.Ref
	// PasswordHash is a player's credential. Argon2id for
	// anything Emerald wrote; a legacy dump supplies base64 MD5,
	// which is upgraded in place on the player's next successful
	// login.
	PasswordHash string
}

// Type returns the object's type.
func (o *Object) Type() ref.ObjType { return o.Flags.Type() }

// Link returns the field a dump stores in the type-specific slot
// shared by home and drop-to, so import and export do not have to
// special-case it.
func (o *Object) Link() ref.Ref {
	switch o.Type() {
	case ref.TypeRoom:
		return o.Dropto
	case ref.TypeThing, ref.TypePlayer:
		return o.Home
	default:
		return ref.Nothing
	}
}

// SetLink writes the shared home/drop-to slot for this object's type.
func (o *Object) SetLink(r ref.Ref) {
	switch o.Type() {
	case ref.TypeRoom:
		o.Dropto = r
	case ref.TypeThing, ref.TypePlayer:
		o.Home = r
	}
}

// Clone returns a deep copy. The persister gets one of these so the
// world can keep mutating the original while a flush is in flight.
func (o *Object) Clone() *Object {
	c := *o
	if o.Props != nil {
		c.Props = o.Props.Clone()
	}
	if o.Dest != nil {
		c.Dest = make([]ref.Ref, len(o.Dest))
		copy(c.Dest, o.Dest)
	}
	return &c
}

// newObject returns an object with the fields every type needs
// initialised.
func newObject(r ref.Ref, name string, t ref.ObjType, owner ref.Ref, now time.Time) *Object {
	return &Object{
		Ref:      r,
		Name:     name,
		Flags:    ref.Flags(0).WithType(t),
		Owner:    owner,
		Location: ref.Nothing,
		Contents: ref.Nothing,
		Exits:    ref.Nothing,
		Next:     ref.Nothing,
		Home:     ref.Nothing,
		Dropto:   ref.Nothing,
		Props:    props.New(),
		Created:  now,
		Modified: now,
		LastUsed: now,
	}
}
