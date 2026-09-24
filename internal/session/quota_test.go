package session

import (
	"testing"
	"time"
)

func TestQuotaSpendsAndRefills(t *testing.T) {
	q := newQuota(2)
	done := make(chan struct{})

	for i := 0; i < 2; i++ {
		if !q.Take(done) {
			t.Fatalf("take %d should have succeeded", i)
		}
	}
	if got := q.Remaining(); got != 0 {
		t.Fatalf("Remaining() = %d, want 0", got)
	}

	// A third take has to wait, so it only completes once Add
	// runs.
	taken := make(chan bool, 1)
	go func() { taken <- q.Take(done) }()
	select {
	case <-taken:
		t.Fatal("a take with no allowance left should have waited")
	case <-time.After(20 * time.Millisecond):
	}

	q.Add(1, 10)
	select {
	case ok := <-taken:
		if !ok {
			t.Error("the waiting take should have succeeded once refilled")
		}
	case <-time.After(time.Second):
		t.Error("the waiting take was never woken")
	}
}

func TestQuotaAddIsCapped(t *testing.T) {
	q := newQuota(0)
	q.Add(100, 5)
	if got := q.Remaining(); got != 5 {
		t.Errorf("Remaining() = %d, want the ceiling of 5", got)
	}
}

// TestQuotaTakeUnblocksOnClose covers the case that would otherwise
// leak a transport goroutine: a connection that goes away while its
// input is being held by the limiter.
func TestQuotaTakeUnblocksOnClose(t *testing.T) {
	q := newQuota(0)
	done := make(chan struct{})

	taken := make(chan bool, 1)
	go func() { taken <- q.Take(done) }()

	close(done)
	select {
	case ok := <-taken:
		if ok {
			t.Error("Take should report failure once the connection is gone")
		}
	case <-time.After(time.Second):
		t.Error("Take did not notice the connection going away")
	}
}
