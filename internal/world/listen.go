package world

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// The three propdirs that make an object a listener, upstream's
// LISTEN_PROPQUEUE, WLISTEN_PROPQUEUE and WOLISTEN_PROPQUEUE
// (game.h:123, :129, :130).
//
// The two beginning '~' are wizard-only props, which is what the
// character means: a mortal cannot set them, so a world can listen
// without the object advertising it in `examine`.
const (
	ListenProp   = "_listen"
	WListenProp  = "~listen"
	WOListenProp = "~olisten"
)

// listenProps is the order the sweep and the queues consult them in.
var listenProps = [...]string{ListenProp, WListenProp, WOListenProp}

// IsListenPath reports whether a property path names one of the three
// listen propqueues.
//
// It is `string_prefix(pname, LISTEN_PROPQUEUE)` and the two beside
// it (`property.c:101`), which asks whether the *path* starts with
// the propqueue's name — so `_listen/greeting` counts, and so does
// `_listenup`, while `foo/_listen` does not. Only a root-level
// property can make a listener.
func IsListenPath(path string) bool {
	for len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}
	for _, p := range listenProps {
		if ascii.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// markListener sets LISTENER when a property write names a listen
// propqueue.
//
// Upstream only ever *sets* it: `set_property` turns it on and
// nothing in a non-DISKBASE build turns it off, so removing the last
// `_listen` prop leaves a thing flagged until the world is reloaded.
// That is reproduced rather than improved, because `@sweep` and
// `listenqueue` both test the flag *and* the property, so a stale
// flag is invisible — and clearing it would have to happen on every
// delete, where upstream's own DISKBASE path recomputes instead.
//
// The flag is in ref.DumpMask, so a load clears it and
// RecomputeListeners puts it back. That is what makes the staleness
// bounded.
func (w *World) markListener(r ref.Ref, path string) {
	if !IsListenPath(path) {
		return
	}
	if o := w.objs[r]; o != nil {
		o.Flags |= ref.Listener
	}
}

// RefreshListener recomputes the flag from what an object actually
// holds. It is the DISKBASE path's `skipproperties`
// (`diskprop.c:239`) — the one place upstream clears the flag —
// and is used where a whole property tree arrives at once rather than
// a property at a time: loading a world, importing one, and copying
// props onto a clone.
func (w *World) RefreshListener(r ref.Ref) {
	o := w.objs[r]
	if o == nil || o.Props == nil {
		return
	}
	o.Flags &^= ref.Listener
	for _, name := range o.Props.Children("") {
		if IsListenPath(name) {
			o.Flags |= ref.Listener
			return
		}
	}
}

// RecomputeListeners refreshes every object's LISTENER flag, which a
// world does once after loading: the flag is live state rather than
// stored state, cleared by ref.DumpMask on the way in.
func (w *World) RecomputeListeners() {
	for r := range w.objs {
		w.RefreshListener(r)
	}
}

// HasListenProp reports whether an object carries any of the three
// listen propqueues at its root, which is the second half of what
// @sweep and listenqueue test. The flag alone is not enough: upstream
// never clears it on a delete.
func (w *World) HasListenProp(r ref.Ref) bool {
	o := w.objs[r]
	if o == nil || o.Props == nil {
		return false
	}
	for _, p := range listenProps {
		if _, ok := o.Props.Get(p); ok {
			return true
		}
		if o.Props.IsDir(p) {
			return true
		}
	}
	return false
}

// IsListener reports whether an object really listens: the flag set
// and a property to back it.
func (w *World) IsListener(r ref.Ref) bool {
	o := w.objs[r]
	return o != nil && o.Flags&ref.Listener != 0 && w.HasListenProp(r)
}
