package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sync"
	"time"
)

// The liveness lease answers one question: is a server running
// against this world?
//
// Nothing in the database said so before. Two servers sharing one
// world would each hold an authoritative in-memory graph and write
// over each other every flush interval, and the configurator needs
// the answer to know whether it may write at all.
//
// It is a Postgres **session-level advisory lock**, which is the one
// mechanism that answers correctly after a crash: it is held by a
// connection, so a process that dies — or is killed, or loses its
// network — releases it without anything having to notice or clean
// up. A row with a heartbeat column would need a timeout, and a
// timeout is a guess.
//
// The lock is scoped to the schema rather than the database, because
// a schema is what holds a world: the store tests each run in their
// own, and two worlds in one database must not be able to lock each
// other out.

// ErrLeaseHeld is returned when something else already holds the
// lease, which means a server is running against this world (or a
// configurator is part-way through a write).
var ErrLeaseHeld = errors.New("another process is using this world")

// leaseClass is the advisory lock's class id, chosen so that a lock
// taken by something else in the same database cannot collide with
// this one. It spells "FBME".
const leaseClass = 0x46424D45

// leaseKeepalive is how often the pinned connection is checked. It is
// well under any reasonable idle timeout on a connection or a
// firewall between here and Postgres.
const leaseKeepalive = 30 * time.Second

// Lease is a held liveness marker. Release it, or let the process
// die; either way the lock goes.
type Lease struct {
	conn  *sql.Conn
	objID int32
	log   *slog.Logger

	// reacquire is how a lost connection is replaced, which the
	// keepalive needs and which is why the lease keeps a way back
	// to its store.
	store *Store

	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}

	// mu guards conn, which the keepalive goroutine replaces when
	// it has to reconnect.
	mu sync.Mutex
}

// AcquireLease takes the lease for this world, or reports
// ErrLeaseHeld.
//
// The connection is pinned outside GORM's pool for the lease's whole
// life. That is not an optimisation: a session-level advisory lock
// belongs to the session that took it, so taking one through the pool
// would release it the moment that connection went back — silently,
// leaving a running server looking offline.
func (s *Store) AcquireLease(ctx context.Context) (*Lease, error) {
	conn, objID, err := s.pinLeaseConn(ctx)
	if err != nil {
		return nil, err
	}

	var got bool
	err = conn.QueryRowContext(ctx,
		"SELECT pg_try_advisory_lock($1, $2)",
		leaseClass, objID).Scan(&got)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("taking the world lease: %w", err)
	}
	if !got {
		conn.Close()
		return nil, ErrLeaseHeld
	}

	l := &Lease{
		conn:  conn,
		objID: objID,
		log:   s.log,
		store: s,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	go l.keepalive()
	return l, nil
}

// pinLeaseConn takes a connection out of the pool for good and works
// out which world it is looking at.
func (s *Store) pinLeaseConn(ctx context.Context) (*sql.Conn, int32, error) {
	sqlDB, err := s.db.DB()
	if err != nil {
		return nil, 0, err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("pinning a connection: %w", err)
	}
	var schema string
	err = conn.QueryRowContext(ctx, "SELECT current_schema()").
		Scan(&schema)
	if err != nil {
		conn.Close()
		return nil, 0, fmt.Errorf("reading the schema: %w", err)
	}
	return conn, leaseObjID(schema), nil
}

// leaseObjID turns a schema name into the lock's object id. Any
// stable hash will do; what matters is that two schemas in one
// database get different ids.
//
// The top bit is cleared so the id is never negative. Postgres
// matches a negative int4 against the unsigned oid in pg_locks
// correctly — that was checked, and it does — but it displays the
// wrapped value, so "objid 3432027008" in pg_locks and "-862940288"
// in a log line are the same lock and do not look like it. Keeping
// the id positive means the two agree, which is worth the one line
// when somebody is working out by hand who holds a world.
func leaseObjID(schema string) int32 {
	h := fnv.New32a()
	h.Write([]byte(schema))
	return int32(h.Sum32() & 0x7fffffff)
}

// LeaseHeld reports whether anything holds this world's lease.
//
// It reads pg_locks rather than trying to take the lock and letting
// go again: a probe that took the lock, even for a moment, would make
// a server starting at the same time fail for no reason.
func (s *Store) LeaseHeld(ctx context.Context) (bool, error) {
	var schema string
	err := s.db.WithContext(ctx).
		Raw("SELECT current_schema()").Scan(&schema).Error
	if err != nil {
		return false, fmt.Errorf("reading the schema: %w", err)
	}

	var n int64
	err = s.db.WithContext(ctx).Raw(`
		SELECT count(*) FROM pg_locks
		WHERE locktype = 'advisory'
		  AND classid = $1 AND objid = $2 AND granted
		  AND database = (
			SELECT oid FROM pg_database
			WHERE datname = current_database())`,
		leaseClass, leaseObjID(schema)).Scan(&n).Error
	if err != nil {
		return false, fmt.Errorf("checking the world lease: %w", err)
	}
	return n > 0, nil
}

// Release gives the lease up and returns the connection.
func (l *Lease) Release() error {
	l.stopOnce.Do(func() { close(l.stop) })
	<-l.done

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil {
		return nil
	}
	// The unlock is required, not tidiness. Closing an *sql.Conn
	// hands the session back to the pool alive, so the lock would
	// outlive the lease and travel to whoever got that connection
	// next. Only the process actually dying drops it on its own.
	_, err := l.conn.ExecContext(context.Background(),
		"SELECT pg_advisory_unlock($1, $2)", leaseClass, l.objID)
	closeErr := l.conn.Close()
	l.conn = nil
	if err != nil {
		return err
	}
	return closeErr
}

// keepalive notices a connection that has gone away and takes the
// lease again.
//
// Losing it is not fatal. A server that exited here would be one
// Postgres restart away from taking the world down, which is a worse
// failure than briefly looking offline to a configurator — so this
// logs and retries instead.
func (l *Lease) keepalive() {
	defer close(l.done)
	t := time.NewTicker(leaseKeepalive)
	defer t.Stop()

	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			if err := l.check(); err != nil {
				l.log.Error("the world lease was lost",
					"error", err)
			}
		}
	}
}

// check pings the pinned connection and reconnects if it has died.
func (l *Lease) check() error {
	ctx, cancel := context.WithTimeout(context.Background(),
		leaseKeepalive)
	defer cancel()

	l.mu.Lock()
	conn := l.conn
	l.mu.Unlock()
	if conn == nil {
		return nil
	}
	if err := conn.PingContext(ctx); err == nil {
		return nil
	}

	conn.Close()
	fresh, objID, err := l.store.pinLeaseConn(ctx)
	if err != nil {
		l.mu.Lock()
		l.conn = nil
		l.mu.Unlock()
		return err
	}
	var got bool
	err = fresh.QueryRowContext(ctx,
		"SELECT pg_try_advisory_lock($1, $2)",
		leaseClass, objID).Scan(&got)
	if err != nil || !got {
		fresh.Close()
		l.mu.Lock()
		l.conn = nil
		l.mu.Unlock()
		if err != nil {
			return err
		}
		return ErrLeaseHeld
	}

	l.mu.Lock()
	l.conn, l.objID = fresh, objID
	l.mu.Unlock()
	l.log.Warn("the world lease was reconnected")
	return nil
}
