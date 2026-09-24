package world

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// Persister writes snapshots durably. It is an interface so this
// package stays free of database concerns and can be tested without
// one.
type Persister interface {
	// Flush writes a snapshot. It is called from the persister
	// goroutine, never from the world goroutine.
	Flush(ctx context.Context, s Snapshot) error
}

// ErrStopped is returned when work is submitted to an engine that has
// shut down.
var ErrStopped = errors.New("world engine stopped")

// Engine owns a World and serialises every access to it.
//
// One goroutine runs the world; nothing else may touch it. Slow work
// — DNS, SMTP, TLS handshakes, Postgres — belongs in other
// goroutines that post results back through Go or Do.
type Engine struct {
	world *World
	ops   chan operation

	persister Persister
	interval  time.Duration
	log       *slog.Logger

	// flushNow requests an immediate flush; @dump writes to it.
	flushNow chan chan error

	// done is closed when the engine stops accepting work.
	// stopOnce guards it so every Run exit path closes it exactly
	// once.
	done     chan struct{}
	stopOnce sync.Once

	// onPanic, when set, is called after a recovered panic so the
	// caller can tell whoever triggered it that their command
	// failed.
	onPanic func(any)

	// onTick, when set, runs on the world goroutine at each
	// interval. The process queue uses it to wake sleeping
	// programs.
	onTick func(*World)

	// onEachOp, when set, runs on the world goroutine after every
	// operation Run applies — not just at each flush interval.
	// The process queue uses it too, so a freshly-forked or
	// newly-queued process gets its first slice within the same
	// tick of activity that created it, rather than waiting up to
	// a full flush interval: upstream's own scheduler runs once
	// per main-loop pass, which in Emerald's model is once per
	// applied operation, not once per second. It is deliberately
	// the same shape as onTick — most callers wire both to the
	// same function — so periodic and event-driven scheduling
	// stay in one place rather than two.
	onEachOp func(*World)
}

// OnTick sets a callback run on the world goroutine at each flush
// interval.
func (e *Engine) OnTick(fn func(*World)) { e.onTick = fn }

// OnEachOp sets a callback run on the world goroutine after every
// operation, in addition to OnTick's periodic firing. See onEachOp's
// own comment for why this exists.
func (e *Engine) OnEachOp(fn func(*World)) { e.onEachOp = fn }

// OnPanic sets a callback run after a recovered panic in a world
// operation. It runs on the world goroutine.
func (e *Engine) OnPanic(fn func(any)) { e.onPanic = fn }

type operation struct {
	fn   func(*World)
	done chan struct{}
}

// Options configure an Engine.
type Options struct {
	Persister Persister
	// Interval bounds how much a crash can lose. It replaces
	// upstream's dump_interval, which froze the world for the
	// length of a full write.
	Interval time.Duration
	Logger   *slog.Logger
	// QueueDepth is how many operations may be pending before
	// submitters block. It bounds memory when a burst of input
	// arrives.
	QueueDepth int
}

// NewEngine returns an engine wrapping w. Call Run to start it.
func NewEngine(w *World, opts Options) *Engine {
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}
	if opts.QueueDepth <= 0 {
		opts.QueueDepth = 1024
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	return &Engine{
		world:     w,
		ops:       make(chan operation, opts.QueueDepth),
		persister: opts.Persister,
		interval:  opts.Interval,
		log:       opts.Logger,
		flushNow:  make(chan chan error, 1),
		done:      make(chan struct{}),
	}
}

// Go submits work without waiting for it. Use it for anything on the
// input path, where blocking a connection goroutine on the world
// would be wrong.
func (e *Engine) Go(fn func(*World)) error {
	// Check for shutdown first. The operation queue is buffered,
	// so a plain select would have a ready send case even after
	// the engine stopped, and would pick it half the time —
	// silently accepting work that never runs.
	if e.stopped() {
		return ErrStopped
	}
	select {
	case e.ops <- operation{fn: fn}:
		return nil
	case <-e.done:
		return ErrStopped
	}
}

// stopped reports whether the engine has stopped accepting work.
func (e *Engine) stopped() bool {
	select {
	case <-e.done:
		return true
	default:
		return false
	}
}

// Do submits work and waits for it to run. Use it when the caller
// needs the result, and never from inside another Do: the world
// goroutine cannot wait on itself.
func (e *Engine) Do(ctx context.Context, fn func(*World)) error {
	if e.stopped() {
		return ErrStopped
	}
	op := operation{fn: fn, done: make(chan struct{})}
	select {
	case e.ops <- op:
	case <-e.done:
		return ErrStopped
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-op.done:
		return nil
	case <-e.done:
		return ErrStopped
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Flush forces an immediate write and waits for it. This is what
// @dump does; unlike upstream it does not pause the world, so it
// returns as soon as the snapshot is durable.
func (e *Engine) Flush(ctx context.Context) error {
	reply := make(chan error, 1)
	select {
	case e.flushNow <- reply:
	case <-e.done:
		return ErrStopped
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Run drives the world until ctx is cancelled, then writes everything
// outstanding and returns. It blocks, so run it in its own goroutine.
func (e *Engine) Run(ctx context.Context) error {
	defer e.stop()

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case op := <-e.ops:
			e.apply(op)
			if e.onEachOp != nil {
				e.apply(operation{fn: e.onEachOp})
			}

		case <-ticker.C:
			if e.onTick != nil {
				e.apply(operation{fn: e.onTick})
			}
			if err := e.flush(ctx); err != nil {
				// A failed write is not fatal: the
				// objects stay dirty and the next
				// tick tries again. Losing the world
				// because Postgres blipped would be a
				// worse outcome than running on.
				e.log.Error("flush failed", "error", err)
			}

		case reply := <-e.flushNow:
			reply <- e.flush(ctx)

		case <-ctx.Done():
			return e.shutdown()
		}
	}
}

// apply runs one operation, containing any panic so a single bad
// command cannot take the whole world down.
//
// The stack is captured and logged: a swallowed panic that leaves no
// trace is worse than a crash, because the symptom is a command that
// silently does nothing at all.
func (e *Engine) apply(op operation) {
	defer func() {
		if op.done != nil {
			close(op.done)
		}
		if r := recover(); r != nil {
			e.log.Error("panic in world operation",
				"panic", r, "stack", string(debug.Stack()))
			if e.onPanic != nil {
				e.onPanic(r)
			}
		}
	}()
	op.fn(e.world)
}

// flush snapshots on the world goroutine and writes from it. The
// snapshot is a deep copy, so a slow write never blocks a mutation
// — but this call is synchronous, which keeps ordering simple and
// means a flush cannot overlap itself.
func (e *Engine) flush(ctx context.Context) error {
	if e.persister == nil {
		return nil
	}
	s := e.world.TakeSnapshot()
	if s.Empty() {
		return nil
	}
	if err := e.persister.Flush(ctx, s); err != nil {
		// Put the work back so the next attempt retries it,
		// rather than dropping changes on the floor.
		e.world.requeue(s)
		return err
	}
	return nil
}

// stop closes the done channel, after which Go and Do refuse new
// work.
func (e *Engine) stop() { e.stopOnce.Do(func() { close(e.done) }) }

// shutdown stops accepting work, runs whatever is already queued, and
// makes one final write. It uses a fresh context because the one that
// triggered shutdown is already cancelled, and the whole point is to
// finish writing.
func (e *Engine) shutdown() error {
	// Refuse new submissions before draining, so the queue cannot
	// be refilled behind the drain loop.
	e.stop()

	for {
		select {
		case op := <-e.ops:
			e.apply(op)
		default:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := e.flush(ctx); err != nil {
				return fmt.Errorf("final flush: %w", err)
			}
			return nil
		}
	}
}

// requeue restores a failed snapshot's work to the dirty set.
func (w *World) requeue(s Snapshot) {
	for _, o := range s.Objects {
		if _, ok := w.objs[o.Ref]; ok {
			w.dirty[o.Ref] = struct{}{}
		}
	}
	for _, r := range s.Deleted {
		w.deleted[r] = struct{}{}
	}
	for r := range s.Programs {
		if _, ok := w.programs[r]; ok {
			w.progDirty[r] = struct{}{}
		}
	}
	if s.Tune != nil {
		w.tuneDirty = true
	}
	if s.Macros != nil {
		w.macrosDirty = true
	}
}
