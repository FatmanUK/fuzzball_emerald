package session

import (
	"sync"
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

func TestSendAndClose(t *testing.T) {
	d := newDescriptor(1, TransportLine, "host", time.Now())
	d.Send("hello")
	select {
	case got := <-d.Output():
		if got != "hello" {
			t.Errorf("got %q", got)
		}
	default:
		t.Fatal("nothing was queued")
	}

	d.Close()
	if !d.Closed() {
		t.Error("the descriptor should be closed")
	}
	select {
	case <-d.Done():
	default:
		t.Error("Done should be closed")
	}
	// Sending after close is a no-op, not a panic.
	d.Send("ignored")
}

func TestCloseIsIdempotent(t *testing.T) {
	d := newDescriptor(1, TransportLine, "host", time.Now())
	d.Close()
	d.Close()
	d.Close()
}

// TestSendAndCloseDoNotRace is the regression test for a real bug: Send runs
// on the world goroutine while Close may run on a transport's, and an earlier
// version closed the output channel in Close. Closing a channel out from under
// a sender is a data race, and sending on a closed channel panics outright.
func TestSendAndCloseDoNotRace(t *testing.T) {
	for i := 0; i < 50; i++ {
		d := newDescriptor(1, TransportLine, "host", time.Now())
		var wg sync.WaitGroup
		wg.Add(3)

		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				d.Send("line")
			}
		}()
		go func() {
			defer wg.Done()
			d.Close()
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				select {
				case <-d.Output():
				case <-d.Done():
					return
				}
			}
		}()
		wg.Wait()
	}
}

func TestOverflowClosesRatherThanBlocking(t *testing.T) {
	d := newDescriptor(1, TransportLine, "host", time.Now())
	// Nobody is draining, so the buffer fills and the descriptor is dropped
	// rather than the sender stalling.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < outputDepth*4; i++ {
			d.Send("flood")
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Send blocked when the client stopped reading")
	}
	if !d.Closed() {
		t.Error("the descriptor should have been closed")
	}
	if !d.Overflowed() {
		t.Error("the overflow should have been recorded")
	}
}

func TestDrainReturnsBufferedOutput(t *testing.T) {
	d := newDescriptor(1, TransportLine, "host", time.Now())
	d.Send("one")
	d.Send("two")
	d.Close()
	// A parting message queued just before the close must still be
	// recoverable, which is what lets "Goodbye." reach the client.
	got := d.Drain()
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Errorf("Drain() = %v, want [one two]", got)
	}
	if len(d.Drain()) != 0 {
		t.Error("a second Drain should be empty")
	}
}

func TestHubTracksPlayers(t *testing.T) {
	h := NewHub()
	now := time.Now()

	a := h.Add(TransportLine, "host-a", now)
	b := h.Add(TransportWSS, "host-b", now)
	if a.ID == b.ID {
		t.Error("descriptors should have distinct ids")
	}
	if h.PlayersOnline() != 0 {
		t.Error("nobody is logged in yet")
	}

	player := ref.Ref(5)
	h.Bind(a, player, now)
	if !h.Online(player) {
		t.Error("the player should be online")
	}
	if h.PlayersOnline() != 1 {
		t.Errorf("PlayersOnline() = %d, want 1", h.PlayersOnline())
	}
	if len(h.Connected()) != 1 {
		t.Errorf("Connected() = %d, want 1", len(h.Connected()))
	}

	// A second connection for the same player.
	h.Bind(b, player, now)
	if got := len(h.DescriptorsFor(player)); got != 2 {
		t.Errorf("DescriptorsFor = %d, want 2", got)
	}
	if h.PlayersOnline() != 1 {
		t.Errorf("PlayersOnline() = %d, want 1: it counts players, not connections",
			h.PlayersOnline())
	}

	// Tell reaches both.
	h.Tell(player, "hi")
	for _, d := range []*Descriptor{a, b} {
		select {
		case got := <-d.Output():
			if got != "hi" {
				t.Errorf("got %q", got)
			}
		default:
			t.Errorf("descriptor %d received nothing", d.ID)
		}
	}

	h.Remove(a)
	if got := len(h.DescriptorsFor(player)); got != 1 {
		t.Errorf("after removing one, DescriptorsFor = %d, want 1", got)
	}
	if !h.Online(player) {
		t.Error("the player still has a connection")
	}
	h.Remove(b)
	if h.Online(player) {
		t.Error("the player should be offline now")
	}
	if h.PlayersOnline() != 0 {
		t.Error("nobody should be left")
	}
}

func TestConnectedIsInConnectionOrder(t *testing.T) {
	h := NewHub()
	now := time.Now()
	var ds []*Descriptor
	for i := 0; i < 5; i++ {
		d := h.Add(TransportLine, "host", now)
		h.Bind(d, ref.Ref(i+1), now)
		ds = append(ds, d)
	}
	got := h.Connected()
	for i := range got {
		if got[i].ID != ds[i].ID {
			t.Errorf("Connected()[%d] = %d, want %d", i, got[i].ID, ds[i].ID)
		}
	}
}
