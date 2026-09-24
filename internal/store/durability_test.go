package store

import (
	"context"
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestWritesBecomeDurableWithinTheInterval is the durability half of
// M1.
//
// The guarantee Emerald replaces the dump cycle with is: at any
// instant, every change older than the flush interval is already in
// Postgres. This asserts it directly, by reading the database from a
// second connection while the engine is still running — which is
// the same thing a crash would observe, without needing to kill
// anything.
func TestWritesBecomeDurableWithinTheInterval(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	const interval = 100 * time.Millisecond

	w := world.New()
	engine := world.NewEngine(w, world.Options{Persister: s, Interval: interval})

	runCtx, cancel := context.WithCancel(ctx)
	errc := make(chan error, 1)
	go func() { errc <- engine.Run(runCtx) }()

	// Write steadily for a while, recording when each object was
	// accepted.
	written := make(map[ref.Ref]time.Time)
	stopWriting := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(stopWriting) {
		var made ref.Ref
		if err := engine.Do(ctx, func(w *world.World) {
			made = w.Create("thing", ref.TypeThing, ref.God).Ref
		}); err != nil {
			t.Fatal(err)
		}
		written[made] = time.Now()
		time.Sleep(5 * time.Millisecond)
	}

	// Everything written before this instant must be durable once
	// the interval, plus room for the write itself, has elapsed.
	cutoff := time.Now()
	time.Sleep(3 * interval)

	reloaded := world.New()
	if _, err := s.Load(ctx, reloaded); err != nil {
		t.Fatalf("loading: %v", err)
	}

	var missing int
	for r, at := range written {
		if at.After(cutoff) {
			continue
		}
		if reloaded.Get(r) == nil {
			missing++
		}
	}
	if missing > 0 {
		t.Errorf("%d of %d objects written before the cutoff were not durable "+
			"after %v; the flush interval is %v",
			missing, len(written), 3*interval, interval)
	}

	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("Run returned %v", err)
	}
}

// TestCrashLosesOnlyRecentWrites simulates losing the process without
// a clean shutdown: the engine is abandoned mid-flight and the
// database is read back. Older writes must survive; only the tail
// since the last flush may be lost.
func TestCrashLosesOnlyRecentWrites(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	const interval = 100 * time.Millisecond

	w := world.New()
	engine := world.NewEngine(w, world.Options{Persister: s, Interval: interval})

	// A context that is never cancelled: nothing gets a chance to
	// flush on the way out, which is what a kill -9 looks like
	// from Postgres's side.
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = engine.Run(runCtx) }()

	// An early batch, given time to reach the database.
	var early []ref.Ref
	for i := 0; i < 20; i++ {
		var made ref.Ref
		if err := engine.Do(ctx, func(w *world.World) {
			made = w.Create("early", ref.TypeThing, ref.God).Ref
		}); err != nil {
			t.Fatal(err)
		}
		early = append(early, made)
	}
	time.Sleep(3 * interval)

	// A late batch, written and then immediately abandoned.
	var late []ref.Ref
	for i := 0; i < 20; i++ {
		var made ref.Ref
		if err := engine.Do(ctx, func(w *world.World) {
			made = w.Create("late", ref.TypeThing, ref.God).Ref
		}); err != nil {
			t.Fatal(err)
		}
		late = append(late, made)
	}

	// Read back without ever letting the engine shut down.
	reloaded := world.New()
	if _, err := s.Load(ctx, reloaded); err != nil {
		t.Fatalf("loading: %v", err)
	}

	for _, r := range early {
		if reloaded.Get(r) == nil {
			t.Errorf("%v was written well before the crash and should have survived", r)
		}
	}
	// The late batch may or may not have made it; what matters is
	// that its absence is the only loss, and that nothing is
	// corrupt.
	lost := 0
	for _, r := range late {
		if reloaded.Get(r) == nil {
			lost++
		}
	}
	t.Logf("lost %d of %d writes made in the %v before the crash", lost, len(late), interval)
	if reloaded.Len() < len(early) {
		t.Errorf("reloaded %d objects, want at least the %d early ones",
			reloaded.Len(), len(early))
	}
}
