package store

import (
	"context"
	"errors"
	"testing"
)

func TestLeaseExcludesASecondHolder(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	held, err := s.LeaseHeld(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if held {
		t.Fatal("a fresh world reports its lease as held")
	}

	lease, err := s.AcquireLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	if held, err = s.LeaseHeld(ctx); err != nil || !held {
		t.Errorf("LeaseHeld = %v, err=%v, want true", held, err)
	}
	if _, err := s.AcquireLease(ctx); !errors.Is(err, ErrLeaseHeld) {
		t.Errorf("a second acquire returned %v, want ErrLeaseHeld", err)
	}

	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	if held, err = s.LeaseHeld(ctx); err != nil || held {
		t.Errorf("LeaseHeld after release = %v, err=%v", held, err)
	}
	// Releasing twice must be safe: the serve path defers it and
	// may also release on a clean shutdown.
	if err := lease.Release(); err != nil {
		t.Errorf("releasing twice: %v", err)
	}
}

// TestLeaseClearsOnACrash is the property the whole design rests on.
// Killing a server must leave the world usable without anything
// having to notice, so the marker has to be one the database drops by
// itself. Closing the pinned connection without unlocking is what a
// process dying does to it.
func TestLeaseClearsOnACrash(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	lease, err := s.AcquireLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if held, err := s.LeaseHeld(ctx); err != nil || !held {
		t.Fatalf("LeaseHeld = %v, err=%v, want true", held, err)
	}

	// The crash. Closing the *sql.Conn would not do it: that
	// hands the session back to the pool alive, and the lock with
	// it — which is exactly why Release has to unlock
	// explicitly rather than rely on closing. Killing the backend
	// is what a dying process actually does.
	lease.stopOnce.Do(func() { close(lease.stop) })
	<-lease.done

	var pid int
	err = lease.conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").
		Scan(&pid)
	if err != nil {
		t.Fatal(err)
	}
	var killed bool
	err = s.db.Raw("SELECT pg_terminate_backend($1)", pid).
		Scan(&killed).Error
	if err != nil || !killed {
		t.Fatalf("terminating the backend: killed=%v err=%v",
			killed, err)
	}
	// The connection is deliberately not closed. Handing a
	// terminated session back to the pool is not what a crash
	// does — the process is gone — and doing it here would
	// only mean the next query in this test drew the dead one.

	if held, err := s.LeaseHeld(ctx); err != nil || held {
		t.Errorf("the lease survived a crash: held=%v err=%v",
			held, err)
	}
	next, err := s.AcquireLease(ctx)
	if err != nil {
		t.Fatalf("the world could not be taken again: %v", err)
	}
	next.Release()
}

// TestLeaseIsPerSchema checks the scoping. Advisory locks belong to a
// database, but a world is a schema: the store tests each make their
// own, and two worlds in one database must not lock each other out.
func TestLeaseIsPerSchema(t *testing.T) {
	a := testStore(t)
	b := testStore(t)
	ctx := context.Background()

	leaseA, err := a.AcquireLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer leaseA.Release()

	leaseB, err := b.AcquireLease(ctx)
	if err != nil {
		t.Fatalf("a second world could not take its own lease: %v", err)
	}
	defer leaseB.Release()

	if held, err := b.LeaseHeld(ctx); err != nil || !held {
		t.Errorf("the second world's lease reads as %v", held)
	}
}

// TestLeaseSurvivesOtherQueries is the pooling trap written down as a
// test: a lock taken through GORM would be released the moment its
// connection went back to the pool, and enough traffic to cycle the
// pool is what would expose that.
func TestLeaseSurvivesOtherQueries(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	lease, err := s.AcquireLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	for i := 0; i < 50; i++ {
		var n int64
		if err := s.db.Raw("SELECT 1").Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
	}
	if held, err := s.LeaseHeld(ctx); err != nil || !held {
		t.Errorf("the lease did not survive pool traffic: %v", held)
	}
}

func TestLeaseObjIDDiffersBySchema(t *testing.T) {
	if leaseObjID("public") == leaseObjID("fbe_test_1") {
		t.Error("two schemas hash to the same lock")
	}
	if leaseObjID("public") != leaseObjID("public") {
		t.Error("the hash is not stable")
	}
}

// TestLeaseObjIDIsNotNegative keeps the id readable. pg_locks reports
// classid and objid as oid, so a negative int4 is displayed wrapped:
// the lock still matches, but the number in pg_locks and the number
// in the code no longer look like the same thing.
func TestLeaseObjIDIsNotNegative(t *testing.T) {
	for _, schema := range []string{
		"public", "fbemerald", "world", "a", "",
		"fbe_test_12345_7", "schema-with-dashes",
	} {
		if got := leaseObjID(schema); got < 0 {
			t.Errorf("leaseObjID(%q) = %d", schema, got)
		}
	}
	if leaseClass < 0 {
		t.Errorf("leaseClass = %d", leaseClass)
	}
}
