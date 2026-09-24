package session

import "sync"

// Quota is a descriptor's command allowance, upstream's own d->quota.
//
// A connection starts with command_burst_size commands in hand and is
// given commands_per_time more every command_time_msec, never
// accumulating past the burst size. Spending it all does not
// disconnect anyone or lose what they typed: the connection simply
// waits for its next allowance, which is what upstream does by
// leaving the line on its input queue.
//
// It is reached from two goroutines — taken on the transport's as
// input arrives, refilled on the world's by the tick — so unlike
// most of this server it locks.
type Quota struct {
	mu     sync.Mutex
	tokens int
	// refill is closed and replaced whenever tokens are added,
	// which is how a waiter learns to look again. A channel
	// rather than a sync.Cond so that waiting composes with the
	// descriptor shutting down.
	refill chan struct{}
}

func newQuota(initial int) *Quota {
	return &Quota{tokens: initial, refill: make(chan struct{})}
}

// Take spends one command, waiting for one to become available. It
// reports false if done fired first, which means the connection is
// going away.
func (q *Quota) Take(done <-chan struct{}) bool {
	for {
		q.mu.Lock()
		if q.tokens > 0 {
			q.tokens--
			q.mu.Unlock()
			return true
		}
		wait := q.refill
		q.mu.Unlock()

		select {
		case <-wait:
		case <-done:
			return false
		}
	}
}

// Add grants n more commands, up to a ceiling of max.
func (q *Quota) Add(n, max int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tokens += n
	if q.tokens > max {
		q.tokens = max
	}
	if q.tokens > 0 {
		close(q.refill)
		q.refill = make(chan struct{})
	}
}

// Set replaces the allowance outright, which a connection needs once
// at the point it learns how large a burst this world permits.
func (q *Quota) Set(n int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tokens = n
	if q.tokens > 0 {
		close(q.refill)
		q.refill = make(chan struct{})
	}
}

// Remaining reports how many commands are in hand, for WHO-style
// reporting and for tests.
func (q *Quota) Remaining() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.tokens
}
