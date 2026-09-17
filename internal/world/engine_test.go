package world

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// fakePersister records what it was asked to write and can be made to fail.
type fakePersister struct {
	mu       sync.Mutex
	flushes  []Snapshot
	failNext int
	err      error
}

func (f *fakePersister) Flush(_ context.Context, s Snapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext > 0 {
		f.failNext--
		return f.err
	}
	f.flushes = append(f.flushes, s)
	return nil
}

func (f *fakePersister) written() []Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Snapshot, len(f.flushes))
	copy(out, f.flushes)
	return out
}

func (f *fakePersister) objectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.flushes {
		n += len(s.Objects)
	}
	return n
}

// startEngine runs an engine and returns it with a stop function.
func startEngine(t *testing.T, p Persister, interval time.Duration) (*Engine, *World, func()) {
	t.Helper()
	w := newTestWorld(t)
	e := NewEngine(w, Options{Persister: p, Interval: interval})

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- e.Run(ctx) }()

	return e, w, func() {
		cancel()
		select {
		case err := <-errc:
			if err != nil {
				t.Errorf("Run returned %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("engine did not shut down")
		}
	}
}

func TestEngineSerialisesOperations(t *testing.T) {
	// Every operation runs on one goroutine, so an unsynchronised counter
	// is safe. Under -race this would fail loudly if that stopped holding.
	e, _, stop := startEngine(t, &fakePersister{}, time.Hour)
	defer stop()

	const n = 500
	counter := 0
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.Go(func(*World) { counter++ }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	// Drain: a Do runs after everything queued before it.
	if err := e.Do(context.Background(), func(*World) {}); err != nil {
		t.Fatal(err)
	}
	if counter != n {
		t.Errorf("counter = %d, want %d", counter, n)
	}
}

func TestEngineDoReturnsAfterTheWorkRan(t *testing.T) {
	e, _, stop := startEngine(t, &fakePersister{}, time.Hour)
	defer stop()

	var created ref.Ref
	err := e.Do(context.Background(), func(w *World) {
		created = w.Create("Thing", ref.TypeThing, ref.God).Ref
	})
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Errorf("created = %v, want #0", created)
	}
}

func TestEnginePanicDoesNotKillTheWorld(t *testing.T) {
	e, _, stop := startEngine(t, &fakePersister{}, time.Hour)
	defer stop()

	if err := e.Go(func(*World) { panic("boom") }); err != nil {
		t.Fatal(err)
	}
	// The engine must still be serving after a panicking operation.
	alive := false
	if err := e.Do(context.Background(), func(*World) { alive = true }); err != nil {
		t.Fatal(err)
	}
	if !alive {
		t.Error("the engine stopped serving after a panic")
	}
}

func TestEngineFlushesOnInterval(t *testing.T) {
	p := &fakePersister{}
	e, _, stop := startEngine(t, p, 10*time.Millisecond)
	defer stop()

	if err := e.Do(context.Background(), func(w *World) {
		w.Create("Thing", ref.TypeThing, ref.God)
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(3 * time.Second)
	for p.objectCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("no flush happened within the timeout")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestEngineExplicitFlush(t *testing.T) {
	p := &fakePersister{}
	// A long interval, so only the explicit flush can be responsible.
	e, _, stop := startEngine(t, p, time.Hour)
	defer stop()

	if err := e.Do(context.Background(), func(w *World) {
		w.Create("Thing", ref.TypeThing, ref.God)
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := p.objectCount(); got != 1 {
		t.Errorf("wrote %d objects, want 1", got)
	}
	// Flushing again with nothing outstanding must not write an empty batch.
	if err := e.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(p.written()); got != 1 {
		t.Errorf("%d flushes reached the persister, want 1", got)
	}
}

func TestFailedFlushRetainsWork(t *testing.T) {
	boom := errors.New("postgres is having a moment")
	p := &fakePersister{failNext: 1, err: boom}
	e, _, stop := startEngine(t, p, time.Hour)
	defer stop()

	if err := e.Do(context.Background(), func(w *World) {
		w.Create("Thing", ref.TypeThing, ref.God)
	}); err != nil {
		t.Fatal(err)
	}

	if err := e.Flush(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("Flush error = %v, want %v", err, boom)
	}
	// The change must not have been dropped.
	var dirty int
	if err := e.Do(context.Background(), func(w *World) { dirty = w.DirtyCount() }); err != nil {
		t.Fatal(err)
	}
	if dirty != 1 {
		t.Errorf("DirtyCount after a failed flush = %d, want 1", dirty)
	}

	// The retry succeeds and the object reaches the store.
	if err := e.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := p.objectCount(); got != 1 {
		t.Errorf("wrote %d objects after retry, want 1", got)
	}
}

func TestShutdownDrainsAndFlushes(t *testing.T) {
	p := &fakePersister{}
	w := newTestWorld(t)
	e := NewEngine(w, Options{Persister: p, Interval: time.Hour})

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- e.Run(ctx) }()

	const n = 50
	for i := 0; i < n; i++ {
		if err := e.Go(func(w *World) { w.Create("Thing", ref.TypeThing, ref.God) }); err != nil {
			t.Fatal(err)
		}
	}

	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("engine did not shut down")
	}

	// Everything queued before shutdown must have run and been written.
	if got := p.objectCount(); got != n {
		t.Errorf("wrote %d objects on shutdown, want %d", got, n)
	}
}

func TestSubmittingAfterShutdownFails(t *testing.T) {
	p := &fakePersister{}
	w := newTestWorld(t)
	e := NewEngine(w, Options{Persister: p, Interval: time.Hour})

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- e.Run(ctx) }()
	cancel()
	<-errc

	if err := e.Go(func(*World) {}); !errors.Is(err, ErrStopped) {
		t.Errorf("Go after shutdown = %v, want ErrStopped", err)
	}
	if err := e.Do(context.Background(), func(*World) {}); !errors.Is(err, ErrStopped) {
		t.Errorf("Do after shutdown = %v, want ErrStopped", err)
	}
}

func TestDoRespectsContextCancellation(t *testing.T) {
	e, _, stop := startEngine(t, &fakePersister{}, time.Hour)
	defer stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.Do(ctx, func(*World) {}); !errors.Is(err, context.Canceled) {
		t.Errorf("Do with a cancelled context = %v, want context.Canceled", err)
	}
}
