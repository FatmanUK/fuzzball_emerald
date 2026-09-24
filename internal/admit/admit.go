// Package admit decides whether a new connection may be accepted at
// all.
//
// This is the layer in front of everything else: it runs before the
// TLS handshake, before a descriptor exists, and before the world
// goroutine hears about the connection. Nothing here consults the
// @tune table, for the same reason the TLS settings do not — a
// server under a connection flood has to keep refusing while the
// database is unreachable, and the decisions are about the machine
// rather than about the game.
//
// Upstream has no equivalent. Fuzzball's own limits (playermax, the
// command spam quota) all sit after authentication, which is too late
// to help against a peer that never authenticates.
package admit

import (
	"sync"
	"time"
)

// Limits bound what one peer, and the server as a whole, may open. A
// zero value in any field means that particular limit is not
// enforced.
type Limits struct {
	// Total is the ceiling on concurrent connections across every
	// peer.
	Total int
	// PerHost is the ceiling on concurrent connections from one
	// address.
	PerHost int
	// Rate is how many new connections one address may open per
	// Window.
	Rate int
	// Window is the period Rate is counted over.
	Window time.Duration
}

// Verdict says why a connection was refused, for the log.
type Verdict string

const (
	// Allowed means the connection may proceed.
	Allowed Verdict = ""
	// TooManyTotal means the server is at its overall ceiling.
	TooManyTotal Verdict = "server connection limit reached"
	// TooManyPerHost means this address already holds its share.
	TooManyPerHost Verdict = "per-host connection limit reached"
	// TooFast means this address is opening connections faster
	// than the rate allows.
	TooFast Verdict = "per-host connection rate exceeded"
)

// Gate enforces a set of limits. The zero Gate allows everything,
// which is what a server started with no limits configured gets.
type Gate struct {
	limits Limits

	mu    sync.Mutex
	total int
	// held counts the live connections per address.
	held map[string]int
	// recent records when each address last opened connections,
	// oldest first, so the rate can be counted without a timer
	// per address.
	recent map[string][]time.Time

	// now is the clock, which tests replace.
	now func() time.Time
}

// New returns a Gate enforcing limits.
func New(limits Limits) *Gate {
	return &Gate{
		limits: limits,
		held:   map[string]int{},
		recent: map[string][]time.Time{},
		now:    time.Now,
	}
}

// Admit asks whether host may open another connection, and records it
// if so. A caller that is admitted must call Release exactly once
// when the connection ends.
func (g *Gate) Admit(host string) Verdict {
	if g == nil {
		return Allowed
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.limits.Total > 0 && g.total >= g.limits.Total {
		return TooManyTotal
	}
	if g.limits.PerHost > 0 && g.held[host] >= g.limits.PerHost {
		return TooManyPerHost
	}
	if g.limits.Rate > 0 && g.limits.Window > 0 {
		now := g.now()
		cutoff := now.Add(-g.limits.Window)
		kept := g.recent[host][:0]
		for _, t := range g.recent[host] {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		if len(kept) >= g.limits.Rate {
			// The attempt is not recorded, so a peer that
			// keeps hammering cannot push its own window
			// forward and stay refused forever once it
			// stops.
			g.recent[host] = kept
			return TooFast
		}
		g.recent[host] = append(kept, now)
	}

	g.total++
	g.held[host]++
	return Allowed
}

// Release gives back a slot taken by Admit.
func (g *Gate) Release(host string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.total > 0 {
		g.total--
	}
	if n := g.held[host]; n <= 1 {
		delete(g.held, host)
	} else {
		g.held[host] = n - 1
	}
	// An address with nothing live and no recent attempts is
	// forgotten, so the table tracks current activity rather than
	// every peer ever seen.
	if len(g.held) == 0 && len(g.recent) > forgetAbove {
		g.forget()
	}
}

// forgetAbove is how many addresses the rate table may hold before a
// sweep, which stops a long run of one-off connections from growing
// it without end.
const forgetAbove = 4096

// forget drops rate history that has aged out. It is called with the
// lock held.
func (g *Gate) forget() {
	cutoff := g.now().Add(-g.limits.Window)
	for host, times := range g.recent {
		live := false
		for _, t := range times {
			if t.After(cutoff) {
				live = true
				break
			}
		}
		if !live {
			delete(g.recent, host)
		}
	}
}
