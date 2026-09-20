// Package session owns live player connections: the descriptor abstraction
// both transports terminate into, telnet negotiation, and the login flow.
//
// A descriptor's output channel is the handoff point between the world
// goroutine and a connection's own goroutine. The world writes to it and never
// blocks; the connection drains it and does the actual I/O.
package session

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/mcp"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// outputDepth bounds how far a connection may fall behind before it is
// disconnected. A client that stops reading must not be able to stall the
// world goroutine or grow the heap without limit.
const outputDepth = 512

// Transport names how a descriptor is connected, for WHO and logging.
type Transport string

const (
	TransportLine Transport = "tls"
	TransportWSS  Transport = "wss"
)

// Descriptor is one connection.
//
// Fields written by the world goroutine are only ever read by it. The output
// channel and the atomics below are the exceptions, and are safe to touch from
// either side.
type Descriptor struct {
	ID        int
	Transport Transport
	Hostname  string

	// Player is the connected player, or ref.Nothing before login.
	Player ref.Ref
	// Connected reports whether login has completed.
	Connected   bool
	ConnectedAt time.Time
	// LastActive is when input last arrived.
	LastActive time.Time

	Width  int
	Height int

	// out carries queued output. It is never closed: Send runs on the world
	// goroutine while Close may run on the transport's, and closing a
	// channel out from under a sender is both a race and a panic. done is
	// the shutdown signal instead.
	out    chan string
	done   chan struct{}
	closed atomic.Bool

	// closeOnce guards done, which must be closed exactly once.
	closeOnce sync.Once

	// overflowed records that output was dropped because the client fell
	// too far behind, so the disconnect can say why.
	overflowed atomic.Bool

	// MCP is this connection's out-of-band protocol state. It is never
	// nil: a client that never negotiates simply leaves it disabled, and
	// every line then passes through untouched.
	MCP *mcp.Frame
}

// newDescriptor builds a descriptor. Transports get one from a Hub.
func newDescriptor(id int, tr Transport, host string, now time.Time, packages []mcp.Package) *Descriptor {
	d := &Descriptor{
		ID:         id,
		Transport:  tr,
		Hostname:   host,
		Player:     ref.Nothing,
		LastActive: now,
		Width:      80,
		Height:     24,
		out:        make(chan string, outputDepth),
		done:       make(chan struct{}),
	}
	d.MCP = mcp.NewFrame(d.sendRaw, packages)
	return d
}

// Output is the stream a transport writes to the client.
//
// It is never closed. A transport selects on it together with Done, and drains
// whatever is left when Done fires so a parting message is not lost.
func (d *Descriptor) Output() <-chan string { return d.out }

// Done is closed when the descriptor shuts down.
func (d *Descriptor) Done() <-chan struct{} { return d.done }

// Drain returns whatever output is still buffered, without waiting. A
// transport calls it after Done fires so the last lines still reach the
// client.
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

// Send queues a line for the client. It never blocks: a client that has
// stopped reading is marked for disconnection rather than allowed to stall the
// world goroutine.
func (d *Descriptor) Send(text string) {
	// Text that would look like an out-of-band message is quoted, so a
	// player cannot make everyone else's client obey a line they typed.
	d.MCP.SendInband(text)
}

// sendRaw queues a line exactly as given. MCP messages go out this way,
// because they must not be quoted as the text they resemble.
func (d *Descriptor) sendRaw(text string) {
	select {
	case <-d.done:
		return
	default:
	}
	select {
	case d.out <- text:
	case <-d.done:
	default:
		// The client has stopped reading. Dropping the connection is the
		// only option that does not either stall the world goroutine or
		// grow without bound.
		d.overflowed.Store(true)
		d.Close()
	}
}

// Close stops the descriptor. It is safe to call more than once, and from
// either goroutine.
func (d *Descriptor) Close() {
	d.closeOnce.Do(func() {
		d.closed.Store(true)
		close(d.done)
	})
}

// Closed reports whether the descriptor has been closed.
func (d *Descriptor) Closed() bool { return d.closed.Load() }

// Overflowed reports whether output was dropped because the client fell
// behind.
func (d *Descriptor) Overflowed() bool { return d.overflowed.Load() }

// IdleSince returns how long the descriptor has been quiet.
func (d *Descriptor) IdleSince(now time.Time) time.Duration {
	return now.Sub(d.LastActive)
}

// Hub tracks live descriptors.
//
// It is owned by the world goroutine: every method must be called from there.
// Transports hand connections in and take a descriptor back, then talk to it
// only through its output channel.
type Hub struct {
	next     int
	byID     map[int]*Descriptor
	byPlayer map[ref.Ref][]*Descriptor

	// packages is what a new connection is offered over MCP. A program
	// may add to it at runtime, and upstream keeps one such list for the
	// whole server rather than one per connection.
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

// SetMCPPackages replaces that list. Connections already open keep the
// packages they negotiated.
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

// Unbind detaches a descriptor from whoever it was bound to, leaving it open
// but back in the pre-login state — DESCR_SETUSER's own "set to no one"
// case, unlike Remove, which closes the connection outright.
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

// DescriptorsFor returns every descriptor a player is connected on. A player
// may be connected more than once.
func (h *Hub) DescriptorsFor(player ref.Ref) []*Descriptor {
	list := h.byPlayer[player]
	out := make([]*Descriptor, len(list))
	copy(out, list)
	return out
}

// Online reports whether a player has at least one live connection.
func (h *Hub) Online(player ref.Ref) bool { return len(h.byPlayer[player]) > 0 }

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

// Tell sends a line to every descriptor a player is connected on.
func (h *Hub) Tell(player ref.Ref, text string) {
	for _, d := range h.byPlayer[player] {
		d.Send(text)
	}
}

// sortByID orders descriptors by id, which is connection order.
func sortByID(ds []*Descriptor) {
	for i := 1; i < len(ds); i++ {
		for j := i; j > 0 && ds[j-1].ID > ds[j].ID; j-- {
			ds[j-1], ds[j] = ds[j], ds[j-1]
		}
	}
}
