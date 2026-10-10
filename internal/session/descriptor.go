// Package session owns live player connections: the descriptor
// abstraction both transports terminate into, telnet negotiation, and
// the login flow.
//
// A descriptor's output channel is the handoff point between the
// world goroutine and a connection's own goroutine. The world writes
// to it and never blocks; the connection drains it and does the
// actual I/O.
package session

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ansi"
	"github.com/FatmanUK/fuzzball_emerald/internal/mcp"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// outputDepth bounds how far a connection may fall behind before it
// is disconnected. A client that stops reading must not be able to
// stall the world goroutine or grow the heap without limit.
const outputDepth = 512

// Transport names how a descriptor is connected, for WHO and logging.
type Transport string

const (
	TransportLine Transport = "tls"
	TransportWSS  Transport = "wss"
)

// Descriptor is one connection.
//
// Fields written by the world goroutine are only ever read by it. The
// output channel and the atomics below are the exceptions, and are
// safe to touch from either side.
type Descriptor struct {
	ID        int
	Transport Transport
	Hostname  string
	// Port is the peer's port, which is upstream's `d->username`:
	// `addrout` builds a hostname of the form "1.2.3.4(56789)"
	// and `initializesock` splits it at the parentheses, so what
	// WHO shows God beside the host is the port and never a user
	// name.
	Port string

	// Player is the connected player, or ref.Nothing before
	// login.
	Player ref.Ref
	// Connected reports whether login has completed.
	Connected   bool
	ConnectedAt time.Time
	// AcceptedAt is when the connection arrived, which is
	// upstream's `connected_at` — stamped in `initializesock`
	// (`interface.c:2586`) rather than at login, because what
	// reads it is the login screen's own timeout. Emerald's
	// ConnectedAt is the login moment and WHO's "On For" wants
	// that one, so the two are separate fields here where
	// upstream has one.
	AcceptedAt time.Time
	// LastActive is when input last arrived.
	LastActive time.Time

	// lastSent is when something was last written to this
	// connection. It is upstream's `last_pinged_at`, whose name
	// its own struct comment contradicts: "last time we sent data
	// to them", stamped in `socket_write` (`interface.c:2120`) on
	// *every* write and never by the keepalive as such. So the
	// keepalive fires only when nothing at all has gone out for
	// `idle_ping_time` — a chatty room never pings.
	//
	// It is an atomic because sendRaw is reached from the
	// transport's goroutine as well as the world's: the MCP frame
	// answers a negotiation from wherever the line arrived.
	lastSent atomic.Int64

	// keepalive carries a request for a protocol-level keepalive,
	// which only a transport can spell. It is buffered at one and
	// written without blocking: a second request while one is
	// outstanding is the same request.
	keepalive chan struct{}

	// telnet records that the client has spoken telnet, which is
	// upstream's `telnet_enabled` — set by any WILL, DO, WONT
	// or DONT it sends (`interface.c:3558` and three more). It
	// decides what a keepalive *is*.
	telnet atomic.Bool

	// Clock is the world's clock, so a test that freezes time
	// freezes these timestamps too. Nil means time.Now, which is
	// right for a descriptor the game has not adopted yet.
	Clock func() time.Time

	Width  int
	Height int

	// out carries queued output. It is never closed: Send runs on
	// the world goroutine while Close may run on the transport's,
	// and closing a channel out from under a sender is both a
	// race and a panic. done is the shutdown signal instead.
	out    chan string
	done   chan struct{}
	closed atomic.Bool

	// closeOnce guards done, which must be closed exactly once.
	closeOnce sync.Once

	// overflowed records that output was dropped because the
	// client fell too far behind, so the disconnect can say why.
	overflowed atomic.Bool

	// MCP is this connection's out-of-band protocol state. It is
	// never nil: a client that never negotiates simply leaves it
	// disabled, and every line then passes through untouched.
	MCP *mcp.Frame

	// Quota is how many more commands this connection may send
	// before it has to wait, upstream's spam limiter. It is never
	// nil.
	Quota *Quota

	// AllowANSI reports whether this connection keeps colour.
	//
	// It is a callback rather than a flag because the answer is
	// the world's — a player's COLOR flag and, before login,
	// two @tune parameters — and because it has to be read
	// *live*: a pushed copy would go stale the moment somebody
	// typed "@set me=!C". Every Send runs on the world goroutine,
	// so reading it here is safe.
	//
	// Nil means strip, which is the right answer for a descriptor
	// the game has not adopted yet and the safe one for a client
	// that has asked for nothing.
	AllowANSI func() bool
}

// defaultBurst stands in for command_burst_size until the world
// supplies the real one, which it does as the connection is welcomed.
const defaultBurst = 500

// newDescriptor builds a descriptor. Transports get one from a Hub.
func newDescriptor(id int, tr Transport, host string, now time.Time, packages []mcp.Package) *Descriptor {
	d := &Descriptor{
		ID:         id,
		Transport:  tr,
		Hostname:   host,
		Player:     ref.Nothing,
		AcceptedAt: now,
		LastActive: now,
		Width:      80,
		Height:     24,
		out:        make(chan string, outputDepth),
		done:       make(chan struct{}),
		keepalive:  make(chan struct{}, 1),
		// A connection starts with a nominal allowance so
		// input works before the world has told it what this
		// world's burst size is.
		Quota: newQuota(defaultBurst),
	}
	d.lastSent.Store(now.UnixNano())
	d.MCP = mcp.NewFrame(d.sendRaw, packages)
	return d
}

// now reads the descriptor's clock.
func (d *Descriptor) now() time.Time {
	if d.Clock != nil {
		return d.Clock()
	}
	return time.Now()
}

// Output is the stream a transport writes to the client.
//
// It is never closed. A transport selects on it together with Done,
// and drains whatever is left when Done fires so a parting message is
// not lost.
func (d *Descriptor) Output() <-chan string { return d.out }

// Done is closed when the descriptor shuts down.
func (d *Descriptor) Done() <-chan struct{} { return d.done }

// Drain returns whatever output is still buffered, without waiting. A
// transport calls it after Done fires so the last lines still reach
// the client.
func (d *Descriptor) Drain() []string {
	var out []string
	for {
		select {
		case text := <-d.out:
			out = append(out, text)
		default:
			return out
		}
	}
}

// Send queues a line for the client. It never blocks: a client that
// has stopped reading is marked for disconnection rather than allowed
// to stall the world goroutine.
func (d *Descriptor) Send(text string) {
	// ANSI is filtered before the MCP quoting and not after,
	// which is queue_ansi's own order (interface.c:673): a
	// sequence stripped out of a line cannot then be what makes
	// the line look like a message.
	//
	// The two filters are different functions. A player who has
	// asked for colour gets it made well-formed; everybody else
	// gets it removed. See internal/ansi.
	if d.AllowANSI != nil && d.AllowANSI() {
		text = ansi.Sanitize(text)
	} else {
		text = ansi.Strip(text)
	}
	// Text that would look like an out-of-band message is quoted,
	// so a player cannot make everyone else's client obey a line
	// they typed.
	d.MCP.SendInband(text)
}

// sendRaw queues a line exactly as given. MCP messages go out this
// way, because they must not be quoted as the text they resemble.
func (d *Descriptor) sendRaw(text string) {
	select {
	case <-d.done:
		return
	default:
	}
	select {
	case d.out <- text:
		d.lastSent.Store(d.now().UnixNano())
	case <-d.done:
	default:
		// The client has stopped reading. Dropping the
		// connection is the only option that does not either
		// stall the world goroutine or grow without bound.
		d.overflowed.Store(true)
		d.Close()
	}
}

// Close stops the descriptor. It is safe to call more than once, and
// from either goroutine.
func (d *Descriptor) Close() {
	d.closeOnce.Do(func() {
		d.closed.Store(true)
		close(d.done)
	})
}

// Closed reports whether the descriptor has been closed.
func (d *Descriptor) Closed() bool { return d.closed.Load() }

// Overflowed reports whether output was dropped because the client
// fell behind.
func (d *Descriptor) Overflowed() bool { return d.overflowed.Load() }

// IdleSince returns how long the descriptor has been quiet.
func (d *Descriptor) IdleSince(now time.Time) time.Duration {
	return now.Sub(d.LastActive)
}

// LastSent is when something was last written to this connection.
func (d *Descriptor) LastSent() time.Time {
	return time.Unix(0, d.lastSent.Load())
}

// Keepalive is the stream of keepalive requests. A transport selects
// on it beside Output and spells the keepalive its own protocol's
// way.
func (d *Descriptor) Keepalive() <-chan struct{} {
	return d.keepalive
}

// RequestKeepalive asks the transport for one, without blocking.
//
// It stamps lastSent whether or not the request was queued, because
// upstream's does: the write it asks for goes through socket_write,
// which stamps. Without that the condition stays true and a keepalive
// would be requested on every tick.
func (d *Descriptor) RequestKeepalive() {
	d.lastSent.Store(d.now().UnixNano())
	select {
	case d.keepalive <- struct{}{}:
	default:
	}
}

// SetTelnet records that the client has spoken telnet.
func (d *Descriptor) SetTelnet(on bool) { d.telnet.Store(on) }

// TelnetEnabled reports whether it has.
func (d *Descriptor) TelnetEnabled() bool { return d.telnet.Load() }

// Hub tracks live descriptors.
//
// It is owned by the world goroutine: every method must be called
// from there. Transports hand connections in and take a descriptor
// back, then talk to it only through its output channel.
type Hub struct {
	next     int
	byID     map[int]*Descriptor
	byPlayer map[ref.Ref][]*Descriptor

	// packages is what a new connection is offered over MCP. A
	// program may add to it at runtime, and upstream keeps one
	// such list for the whole server rather than one per
	// connection.
	packages []mcp.Package
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{
		byID:     map[int]*Descriptor{},
		byPlayer: map[ref.Ref][]*Descriptor{},
		packages: MCPPackages(),
	}
}

// MCPPackageList returns what connections are currently offered.
func (h *Hub) MCPPackageList() []mcp.Package { return h.packages }

// SetMCPPackages replaces that list. Connections already open keep
// the packages they negotiated.
func (h *Hub) SetMCPPackages(p []mcp.Package) { h.packages = p }

// Add registers a new connection and returns its descriptor.
func (h *Hub) Add(tr Transport, host string, now time.Time) *Descriptor {
	h.next++
	d := newDescriptor(h.next, tr, host, now, h.packages)
	h.byID[d.ID] = d
	return d
}

// Remove deregisters a descriptor and closes it.
func (h *Hub) Remove(d *Descriptor) {
	delete(h.byID, d.ID)
	h.unindex(d)
	d.Close()
}

// Bind attaches a descriptor to a player after a successful login.
func (h *Hub) Bind(d *Descriptor, player ref.Ref, now time.Time) {
	h.unindex(d)
	d.Player = player
	d.Connected = true
	d.ConnectedAt = now
	d.LastActive = now
	h.byPlayer[player] = append(h.byPlayer[player], d)
}

// Unbind detaches a descriptor from whoever it was bound to, leaving
// it open but back in the pre-login state — DESCR_SETUSER's own
// "set to no one" case, unlike Remove, which closes the connection
// outright.
func (h *Hub) Unbind(d *Descriptor) {
	h.unindex(d)
	d.Player = ref.Nothing
	d.Connected = false
}

func (h *Hub) unindex(d *Descriptor) {
	if d.Player == ref.Nothing {
		return
	}
	list := h.byPlayer[d.Player]
	for i, other := range list {
		if other == d {
			h.byPlayer[d.Player] = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(h.byPlayer[d.Player]) == 0 {
		delete(h.byPlayer, d.Player)
	}
}

// Get returns a descriptor by id.
func (h *Hub) Get(id int) *Descriptor { return h.byID[id] }

// DescriptorsFor returns every descriptor a player is connected on. A
// player may be connected more than once.
func (h *Hub) DescriptorsFor(player ref.Ref) []*Descriptor {
	list := h.byPlayer[player]
	out := make([]*Descriptor, len(list))
	copy(out, list)
	return out
}

// Online reports whether a player has at least one live connection.
func (h *Hub) Online(player ref.Ref) bool {
	return len(h.byPlayer[player]) > 0
}

// Connected returns every logged-in descriptor, in connection order.
func (h *Hub) Connected() []*Descriptor {
	out := make([]*Descriptor, 0, len(h.byID))
	for _, d := range h.byID {
		if d.Connected {
			out = append(out, d)
		}
	}
	sortByID(out)
	return out
}

// All returns every descriptor, logged in or not.
func (h *Hub) All() []*Descriptor {
	out := make([]*Descriptor, 0, len(h.byID))
	for _, d := range h.byID {
		out = append(out, d)
	}
	sortByID(out)
	return out
}

// PlayersOnline counts distinct players with a live connection.
func (h *Hub) PlayersOnline() int { return len(h.byPlayer) }

// Tell sends a line to every descriptor a player is connected on. It
// reports how many connections it reached, which is what upstream's
// `notify_nolisten` returns and what tells `page` and `whisper`
// whether to say "Your message has been sent." or "X is not
// connected."
func (h *Hub) Tell(player ref.Ref, text string) int {
	ds := h.byPlayer[player]
	for _, d := range ds {
		d.Send(text)
	}
	return len(ds)
}

// sortByID orders descriptors by id, which is connection order.
func sortByID(ds []*Descriptor) {
	for i := 1; i < len(ds); i++ {
		for j := i; j > 0 && ds[j-1].ID > ds[j].ID; j-- {
			ds[j-1], ds[j] = ds[j], ds[j-1]
		}
	}
}
