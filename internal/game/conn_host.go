package game

import (
	"sort"

	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
)

// SetDescrSize implements muf.Host for SETWIDTH and SETHEIGHT — the
// write side of DescrSize/WIDTH/HEIGHT.
func (h *mufHost) SetDescrSize(descr, width, height int) bool {
	d := h.s.hub.Get(descr)
	if d == nil {
		return false
	}
	if width >= 0 {
		d.Width = width
	}
	if height >= 0 {
		d.Height = height
	}
	return true
}

// DescrIdle and DescrOnTime implement muf.Host for DESCRIDLE and
// DESCRTIME.
func (h *mufHost) DescrIdle(descr int) int {
	d := h.s.hub.Get(descr)
	if d == nil {
		return -1
	}
	return int(d.IdleSince(h.w.Now()).Seconds())
}

func (h *mufHost) DescrOnTime(descr int) int {
	d := h.s.hub.Get(descr)
	if d == nil {
		return -1
	}
	return int(h.w.Now().Sub(d.ConnectedAt).Seconds())
}

// DescrHost implements muf.Host for DESCRHOST.
func (h *mufHost) DescrHost(descr int) (string, bool) {
	d := h.s.hub.Get(descr)
	if d == nil {
		return "", false
	}
	return d.Hostname, true
}

// DescrUser is a port of prim_descr_user/pdescruser. Upstream's
// "username" is not an identity at all — it is the client's own
// ephemeral source TCP port, parsed out of the same "host(port)"
// string addrout builds for logging, a naming accident from when this
// field really did hold ident data. Emerald's transports
// (internal/transport/tlsline, internal/transport/wss) discard the
// remote port on accept — nothing downstream has ever needed it —
// so there is no value to report here; a live connection always
// answers "".
func (h *mufHost) DescrUser(descr int) (string, bool) {
	if h.s.hub.Get(descr) == nil {
		return "", false
	}
	return "", true
}

// DescrBoot implements muf.Host for DESCRBOOT, upstream's pdescrboot.
// It mirrors Server.Disconnect's own body rather than calling it:
// Disconnect re-enters the world goroutine through Engine.Go, which
// is for a transport reporting a connection it already closed, not
// for ending one from inside a handler that is already running on
// that goroutine.
func (h *mufHost) DescrBoot(descr int) bool {
	d := h.s.hub.Get(descr)
	if d == nil {
		return false
	}
	h.s.closeDialogsFor(d.ID)
	if d.Connected {
		h.s.announceDisconnect(h.w, d)
	}
	h.s.hub.Remove(d)
	return true
}

// DescrNotify implements muf.Host for DESCRNOTIFY.
func (h *mufHost) DescrNotify(descr int, msg string) bool {
	d := h.s.hub.Get(descr)
	if d == nil {
		return false
	}
	if msg != "" {
		d.Send(msg)
	}
	return true
}

// DescrFlush implements muf.Host for DESCRFLUSH. Emerald's output
// channel (internal/session.Descriptor.out) has no buffering step of
// its own to flush — a transport drains it continuously — so this
// only counts how many connections descr matched, the one part of
// upstream's own contract that is still observable here.
func (h *mufHost) DescrFlush(descr int) int {
	if descr == -1 {
		return len(h.s.hub.All())
	}
	if h.s.hub.Get(descr) == nil {
		return 0
	}
	return 1
}

// DescrBufSize is a port of pdescrbufsize. Upstream reports
// tp_max_output minus what descr's own OS socket buffer already
// holds; Emerald's output channel is an unbounded-by-byte-count Go
// channel (internal/session.Descriptor's own outputDepth is a message
// count, not a byte budget), so there is no equivalent figure to
// subtract from — a live connection simply reports the tune
// parameter's own ceiling untouched.
func (h *mufHost) DescrBufSize(descr int) int {
	if h.s.hub.Get(descr) == nil {
		return -1
	}
	return int(h.w.Tune.Int("max_output"))
}

// DescrLeastIdle and DescrMostIdle implement muf.Host for
// DESCRLEASTIDLE and DESCRMOSTIDLE.
func (h *mufHost) DescrLeastIdle(player ref.Ref) int {
	return h.extremeIdle(player, false)
}
func (h *mufHost) DescrMostIdle(player ref.Ref) int {
	return h.extremeIdle(player, true)
}

func (h *mufHost) extremeIdle(player ref.Ref, most bool) int {
	ds := h.s.hub.DescriptorsFor(player)
	if len(ds) == 0 {
		return -1
	}
	best := ds[0]
	for _, d := range ds[1:] {
		if most == d.LastActive.Before(best.LastActive) {
			best = d
		}
	}
	return best.ID
}

// NextDescr implements muf.Host for NEXTDESCR.
func (h *mufHost) NextDescr(descr int) int {
	if h.s.hub.Get(descr) == nil {
		return 0
	}
	connected := connectedIDs(h)
	for _, id := range connected {
		if id > descr {
			return id
		}
	}
	return 0
}

// FirstDescr and LastDescr implement muf.Host for FIRSTDESCR and
// LASTDESCR.
//
// The global form (player == ref.Nothing) answers the oldest
// connection for FirstDescr and the newest for LastDescr —
// upstream's own pfirstdescr and plastdescr. The player-scoped form
// is the opposite way around: upstream's prim_firstdescr reads
// darr[dcount-1] (a player's newest connection) and prim_lastdescr
// reads darr[0] (their oldest) — a real asymmetry against the
// global form's naming, not a copy-paste slip; both were checked
// against the C before landing this way.
func (h *mufHost) FirstDescr(player ref.Ref) int {
	if player == ref.Nothing {
		ids := connectedIDs(h)
		if len(ids) == 0 {
			return 0
		}
		return ids[0]
	}
	ds := h.s.hub.DescriptorsFor(player)
	if len(ds) == 0 {
		return 0
	}
	return newestOf(ds).ID
}

func (h *mufHost) LastDescr(player ref.Ref) int {
	if player == ref.Nothing {
		ids := connectedIDs(h)
		if len(ids) == 0 {
			return 0
		}
		return ids[len(ids)-1]
	}
	ds := h.s.hub.DescriptorsFor(player)
	if len(ds) == 0 {
		return 0
	}
	return oldestOf(ds).ID
}

// SetUser implements muf.Host for DESCR_SETUSER, upstream's
// pset_user, with the password check already done by the caller
// (DESCR_SETUSER itself, like prim_descr_setuser). who == ref.Nothing
// is treated as a clean disconnect — upstream's own C leaves
// d->player and d->connected untouched in this case, which reads as
// unfinished state management rather than a deliberate no-op, so it
// is not reproduced.
func (h *mufHost) SetUser(descr int, who ref.Ref) bool {
	d := h.s.hub.Get(descr)
	if d == nil {
		return false
	}
	if d.Connected {
		h.s.announceDisconnect(h.w, d)
	}
	if who == ref.Nothing {
		h.s.hub.Unbind(d)
		return true
	}
	alreadyOn := h.s.hub.Online(who)
	h.s.hub.Bind(d, who, h.w.Now())
	h.w.Used(who)
	if !alreadyOn {
		h.s.announceConnect(h.w, d, false)
	}
	return true
}

// connectedIDs lists every logged-in descriptor's id, ascending —
// id order is connection order here, since ids are assigned by a
// monotonic counter (internal/session.Hub.Add), unlike upstream's own
// doubly-linked list.
func connectedIDs(h *mufHost) []int {
	cs := h.s.hub.Connected()
	ids := make([]int, len(cs))
	for i, d := range cs {
		ids[i] = d.ID
	}
	sort.Ints(ids)
	return ids
}

func newestOf(ds []*session.Descriptor) *session.Descriptor {
	best := ds[0]
	for _, d := range ds[1:] {
		if d.ID > best.ID {
			best = d
		}
	}
	return best
}

func oldestOf(ds []*session.Descriptor) *session.Descriptor {
	best := ds[0]
	for _, d := range ds[1:] {
		if d.ID < best.ID {
			best = d
		}
	}
	return best
}

var _ muf.Host = (*mufHost)(nil)
