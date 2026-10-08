package world

import (
	"context"
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// sysVal reads one _sys value off #0, or reports it absent.
func sysVal(t *testing.T, w *World, path string) (int64, bool) {
	t.Helper()
	v, ok := w.GetProp(ref.GlobalEnvironment, path)
	return v.Num, ok
}

// withRoomZero gives a world a #0 to hang the properties on.
//
// World.New starts empty, and SetProp on an object that is not there
// does nothing at all — so without this every assertion below reads
// a missing property and the test measures the harness. A real world
// always has #0; the loader and the importer both put it in. That is
// why this is a test helper rather than a guard in WriteBootProps.
func withRoomZero(t *testing.T, w *World) {
	t.Helper()
	if w.Get(ref.GlobalEnvironment) == nil {
		w.Create("Room Zero", ref.TypeRoom, ref.God)
	}
}

// runningEngine starts an engine over a world that already has #0,
// and waits for the world goroutine to be live.
//
// startEngine cannot be used: #0 has to exist before Run writes the
// boot properties.
func runningEngine(t *testing.T, p Persister) (*Engine, *World,
	func()) {

	t.Helper()
	w := newTestWorld(t)
	withRoomZero(t, w)
	e := NewEngine(w, Options{Persister: p, Interval: time.Hour})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = e.Run(ctx); close(done) }()

	// One round trip, so Run has passed WriteBootProps.
	if err := e.Do(context.Background(), noop); err != nil {
		t.Fatal(err)
	}
	return e, w, func() {
		cancel()
		<-done
	}
}

// noop is an operation that only forces a round trip.
func noop(*World) {}

// TestBootPropsAreWrittenWhenTheEngineStarts covers the four values
// upstream sets once the database is loaded (game.c:487-490).
//
// Engine.Run is the hook because an Engine is what a *server* has:
// the importer, the configurator and `fbemerald tune` all load a
// world without making one, and "startup time" would mean nothing to
// them.
func TestBootPropsAreWrittenWhenTheEngineStarts(t *testing.T) {
	_, w, stop := runningEngine(t, &fakePersister{})
	defer stop()

	for _, path := range []string{
		SysStartupTime, SysMaxPennies, SysDumpInterval,
	} {
		v, ok := sysVal(t, w, path)
		if !ok {
			t.Errorf("%s was not written", path)
			continue
		}
		if v <= 0 {
			t.Errorf("%s = %d, want a positive value",
				path, v)
		}
	}

	// max_connects is the odd one, and both servers agree about
	// it for the same reason. Upstream writes it as
	// add_property(0, ..., NULL, 0), and add_prop_nofetch
	// (property.c:285) takes neither its string branch nor its
	// integer one for a NULL string with a zero value — so the
	// property is *removed* rather than created.
	// props.Value.IsEmpty treats a zero Int the same way, so this
	// server lands in the same place by its own rule. The call is
	// kept because it is upstream's line; it does nothing until a
	// connection raises the mark.
	if _, ok := sysVal(t, w, SysMaxConnects); ok {
		t.Error("max_connects exists at boot, where " +
			"upstream's zero write removes it")
	}
}

// TestMaxConnectsOnlyRises pins the high-water mark. Upstream raises
// con_players_max and never lowers it (interface.c:4583), so a
// quieter moment does not erase the busiest one.
func TestMaxConnectsOnlyRises(t *testing.T) {
	w := newTestWorld(t)
	withRoomZero(t, w)
	w.WriteBootProps(time.Now())

	w.RecordMaxConnects(3)
	if v, _ := sysVal(t, w, SysMaxConnects); v != 3 {
		t.Fatalf("got %d, want 3", v)
	}
	w.RecordMaxConnects(1)
	if v, _ := sysVal(t, w, SysMaxConnects); v != 3 {
		t.Errorf("got %d after a smaller count, want 3", v)
	}
	w.RecordMaxConnects(5)
	if v, _ := sysVal(t, w, SysMaxConnects); v != 5 {
		t.Errorf("got %d after a larger count, want 5", v)
	}
}

// TestLastDumpTimeIsStampedOnlyOnARealFlush is the decision this step
// had to make rather than port.
//
// Upstream writes _sys/lastdumptime per dump (events.c:99). Emerald
// has no dump cycle, so the analogue is a flush — but a flush skips
// when nothing is dirty, and stamping the property unconditionally
// would make the snapshot never empty. An idle world would then write
// to Postgres on every tick for ever, just to record that it had
// written. Engine.flush's HasPending guard is what stops that.
func TestLastDumpTimeIsStampedOnlyOnARealFlush(t *testing.T) {
	p := &fakePersister{}
	e, _, stop := runningEngine(t, p)
	defer stop()

	ctx := context.Background()

	// Boot wrote three properties, so the first flush has work.
	if err := e.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	var first int64
	if err := e.Do(ctx, func(w *World) {
		v, ok := w.GetProp(ref.GlobalEnvironment,
			SysLastDumpTime)
		if !ok {
			t.Error("no lastdumptime after a flush " +
				"that had work")
		}
		first = v.Num
	}); err != nil {
		t.Fatal(err)
	}

	writes := len(p.written())

	// Nothing has changed since, so these must be no-ops: no
	// write, and no new stamp.
	for i := 0; i < 2; i++ {
		if err := e.Flush(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(p.written()); n != writes {
		t.Errorf("an idle world wrote %d more snapshots",
			n-writes)
	}
	if err := e.Do(ctx, func(w *World) {
		v, _ := w.GetProp(ref.GlobalEnvironment,
			SysLastDumpTime)
		if v.Num != first {
			t.Errorf("an idle flush restamped it: "+
				"%d then %d", first, v.Num)
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestShutdownTimeIsWrittenBeforeTheFinalFlush is the ordering that
// makes the property worth writing at all: upstream sets it at
// shovechars's tail, just before the last dump (interface.c:4600), so
// it has to be in the snapshot that flush takes rather than written
// after it.
func TestShutdownTimeIsWrittenBeforeTheFinalFlush(t *testing.T) {
	p := &fakePersister{}
	e, _, stop := runningEngine(t, p)

	if err := e.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	// stop cancels Run's context, which is what reaches
	// Engine.shutdown, and waits for Run to return — so the
	// final flush has happened by the time the snapshots are
	// read.
	stop()

	// The property has to appear in a snapshot that actually
	// reached the persister, not merely in memory.
	var found bool
	for _, s := range p.written() {
		for _, o := range s.Objects {
			if o.Ref != ref.GlobalEnvironment {
				continue
			}
			v, ok := o.Props.Get(SysShutdownTime)
			if ok && v.Num > 0 {
				found = true
			}
		}
	}
	if !found {
		t.Error("shutdowntime never reached the store, so " +
			"it is written after the final snapshot")
	}
}
