package admit

import (
	"testing"
	"time"
)

func TestNilGateAllowsEverything(t *testing.T) {
	var g *Gate
	if v := g.Admit("1.2.3.4"); v != Allowed {
		t.Errorf("a nil gate refused a connection: %q", v)
	}
	g.Release("1.2.3.4") // must not panic
}

func TestPerHostLimit(t *testing.T) {
	g := New(Limits{PerHost: 2})
	for i := 0; i < 2; i++ {
		if v := g.Admit("a"); v != Allowed {
			t.Fatalf("connection %d refused: %q", i, v)
		}
	}
	if v := g.Admit("a"); v != TooManyPerHost {
		t.Errorf("third connection: got %q, want %q", v, TooManyPerHost)
	}
	// Another address is unaffected.
	if v := g.Admit("b"); v != Allowed {
		t.Errorf("a different host was refused: %q", v)
	}
	// Releasing frees a slot.
	g.Release("a")
	if v := g.Admit("a"); v != Allowed {
		t.Errorf("after a release: %q", v)
	}
}

func TestTotalLimit(t *testing.T) {
	g := New(Limits{Total: 2})
	if v := g.Admit("a"); v != Allowed {
		t.Fatalf("first: %q", v)
	}
	if v := g.Admit("b"); v != Allowed {
		t.Fatalf("second: %q", v)
	}
	if v := g.Admit("c"); v != TooManyTotal {
		t.Errorf("third: got %q, want %q", v, TooManyTotal)
	}
}

func TestRateLimit(t *testing.T) {
	now := time.Unix(1000, 0)
	g := New(Limits{Rate: 2, Window: time.Minute})
	g.now = func() time.Time { return now }

	for i := 0; i < 2; i++ {
		if v := g.Admit("a"); v != Allowed {
			t.Fatalf("connection %d: %q", i, v)
		}
		g.Release("a")
	}
	if v := g.Admit("a"); v != TooFast {
		t.Errorf("third connection in the window: got %q, want %q", v, TooFast)
	}

	// Once the window has passed, the address is allowed again.
	now = now.Add(2 * time.Minute)
	if v := g.Admit("a"); v != Allowed {
		t.Errorf("after the window: %q", v)
	}
}

// TestRefusalDoesNotExtendTheWindow covers the trap in a naive rate
// limiter: counting refused attempts would let a peer that keeps
// retrying hold itself permanently shut out.
func TestRefusalDoesNotExtendTheWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	g := New(Limits{Rate: 1, Window: time.Minute})
	g.now = func() time.Time { return now }

	if v := g.Admit("a"); v != Allowed {
		t.Fatalf("first: %q", v)
	}
	// Hammer away for most of the window.
	for i := 0; i < 10; i++ {
		now = now.Add(5 * time.Second)
		if v := g.Admit("a"); v != TooFast {
			t.Fatalf("retry %d: got %q, want %q", i, v, TooFast)
		}
	}
	// The window is measured from the one accepted connection, so
	// it is over now despite the retries.
	now = now.Add(11 * time.Second)
	if v := g.Admit("a"); v != Allowed {
		t.Errorf("after the window, despite retries: %q", v)
	}
}
