package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// testStore opens a store against a scratch schema, or skips if no test
// database is configured. Set FBE_TEST_DATABASE_URL to run these.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("FBE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set FBE_TEST_DATABASE_URL to run store integration tests")
	}

	s, err := Open(context.Background(), dsn, nil)
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}

	// Each test gets its own schema so they cannot see each other's rows.
	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if err := s.db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	if err := s.db.Exec("SET search_path TO " + schema).Error; err != nil {
		t.Fatalf("setting search path: %v", err)
	}
	t.Cleanup(func() {
		if err := s.db.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Errorf("dropping schema: %v", err)
		}
		s.Close()
	})

	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	return s
}

// buildWorld makes a small world with one of everything worth persisting.
func buildWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.New()
	w.SetClock(func() time.Time { return time.Unix(1_700_000_000, 0).UTC() })

	room := w.Create("Room Zero", ref.TypeRoom, ref.God)
	god := w.Create("One", ref.TypePlayer, ref.God)
	thing := w.Create("a rusty key", ref.TypeThing, god.Ref)
	exit := w.Create("north;n", ref.TypeExit, god.Ref)
	prog := w.Create("cmd-look", ref.TypeProgram, god.Ref)

	god.PasswordHash = "$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA"
	god.Flags |= ref.Wizard
	god.Home = room.Ref
	room.Dropto = room.Ref
	thing.Home = room.Ref
	exit.Dest = []ref.Ref{room.Ref, god.Ref}
	prog.Flags = prog.Flags.SetMLevel(3)

	for _, r := range []ref.Ref{god.Ref, thing.Ref, exit.Ref} {
		if err := w.MoveTo(r, room.Ref); err != nil {
			t.Fatal(err)
		}
	}

	w.SetProp(room.Ref, "_/de", props.Value{Type: props.String, Str: "You are in Room Zero."})
	w.SetProp(god.Ref, "_/de", props.Value{Type: props.String, Str: "You see Number One."})
	w.SetProp(thing.Ref, "@/value", props.Value{Type: props.Int, Num: 42})
	w.SetProp(thing.Ref, "_/lok", props.Value{Type: props.Lock, Str: "me|#1"})
	w.SetProp(thing.Ref, "weight", props.Value{Type: props.Float, Float: 1.5})
	w.SetProp(thing.Ref, "owner", props.Value{Type: props.Ref, Ref: god.Ref})
	w.SetProp(thing.Ref, "deep/nested/prop", props.Value{Type: props.String, Str: "buried"})
	w.SetProp(god.Ref, "_prefs/colour", props.Value{Type: props.String, Str: "on", Blessed: true})

	if err := w.SetTune("penny", "Groat"); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestMigrateIsIdempotent(t *testing.T) {
	s := testStore(t)
	// A second migration on an existing schema must be a no-op, not an error.
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

func TestIsEmpty(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	empty, err := s.IsEmpty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !empty {
		t.Error("a fresh database should be empty")
	}

	w := buildWorld(t)
	if err := s.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}
	if empty, err = s.IsEmpty(ctx); err != nil {
		t.Fatal(err)
	} else if empty {
		t.Error("a database with objects should not be empty")
	}
}

// TestRoundTrip is the M1 acceptance check: a world written out and read back
// must be identical.
func TestRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	original := buildWorld(t)
	if err := s.Flush(ctx, original.TakeSnapshot()); err != nil {
		t.Fatalf("flushing: %v", err)
	}

	reloaded := world.New()
	rep, err := s.Load(ctx, reloaded)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if rep.Objects != original.Len() {
		t.Errorf("loaded %d objects, want %d", rep.Objects, original.Len())
	}
	if rep.ChainsRepaired != 0 {
		t.Errorf("a cleanly written world needed %d chains repaired", rep.ChainsRepaired)
	}
	assertWorldsMatch(t, original, reloaded)
}

func TestReloadPreservesTuneParameters(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	w := buildWorld(t)
	if err := s.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	reloaded := world.New()
	if _, err := s.Load(ctx, reloaded); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Tune.String("penny"); got != "Groat" {
		t.Errorf("penny = %q, want Groat", got)
	}
	// Parameters left alone must still read as defaults.
	if got := reloaded.Tune.String("cpenny"); got != w.Tune.String("cpenny") {
		t.Errorf("cpenny = %q, want %q", got, w.Tune.String("cpenny"))
	}
}

func TestIncrementalFlushOnlyWritesChanges(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	w := buildWorld(t)
	if err := s.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	// Change one object; the snapshot should carry only that one.
	if err := w.Rename(ref.Ref(2), "a shiny key"); err != nil {
		t.Fatal(err)
	}
	snap := w.TakeSnapshot()
	if len(snap.Objects) != 1 {
		t.Fatalf("snapshot has %d objects, want 1", len(snap.Objects))
	}
	if err := s.Flush(ctx, snap); err != nil {
		t.Fatal(err)
	}

	reloaded := world.New()
	if _, err := s.Load(ctx, reloaded); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get(ref.Ref(2)).Name; got != "a shiny key" {
		t.Errorf("name = %q, want the updated one", got)
	}
	// The untouched objects must still be there.
	if reloaded.Len() != w.Len() {
		t.Errorf("reloaded %d objects, want %d", reloaded.Len(), w.Len())
	}
}

func TestPropertyDeletionIsPersisted(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	w := buildWorld(t)
	if err := s.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	thing := ref.Ref(2)
	if !w.Get(thing).Props.Delete("weight") {
		t.Fatal("setup: expected to delete the weight property")
	}
	w.Touch(thing)
	if err := s.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	reloaded := world.New()
	if _, err := s.Load(ctx, reloaded); err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get(thing).Props.Get("weight"); ok {
		t.Error("a deleted property came back after a reload")
	}
	if _, ok := reloaded.Get(thing).Props.Get("@/value"); !ok {
		t.Error("deleting one property removed another")
	}
}

func TestRecycledObjectsPersistAsGarbage(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	w := buildWorld(t)
	if err := s.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	thing := ref.Ref(2)
	if err := w.Recycle(thing); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	reloaded := world.New()
	if _, err := s.Load(ctx, reloaded); err != nil {
		t.Fatal(err)
	}
	o := reloaded.Get(thing)
	if o == nil {
		t.Fatal("the ref should still resolve after recycling")
	}
	if o.Type() != ref.TypeGarbage {
		t.Errorf("type = %v, want garbage", o.Type())
	}
	if o.Props.Len() != 0 {
		t.Error("recycled objects should carry no properties")
	}
	// The ceiling must not drop, so the ref is never handed out again.
	if reloaded.Top() != w.Top() {
		t.Errorf("Top() = %v, want %v", reloaded.Top(), w.Top())
	}
}

func TestOverlongPropertyPathIsRejected(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	w := world.New()
	o := w.Create("Thing", ref.TypeThing, ref.God)
	long := make([]byte, maxPathLen+1)
	for i := range long {
		long[i] = 'x'
	}
	w.SetProp(o.Ref, string(long), props.Value{Type: props.String, Str: "v"})

	err := s.Flush(ctx, w.TakeSnapshot())
	if err == nil {
		t.Fatal("an over-long property path should be rejected, not truncated")
	}
}

func TestProgramSourceRoundTrips(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	const src = ": main\n  \"Hello, world!\" .tell\n;\n"
	if err := s.SaveProgram(ctx, ref.Ref(4), src); err != nil {
		t.Fatal(err)
	}
	// Saving again must replace, not duplicate.
	if err := s.SaveProgram(ctx, ref.Ref(4), src); err != nil {
		t.Fatal(err)
	}

	got := map[ref.Ref]string{}
	n, err := s.LoadPrograms(ctx, func(r ref.Ref, source string) error {
		got[r] = source
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("loaded %d programs, want 1", n)
	}
	if got[ref.Ref(4)] != src {
		t.Errorf("source = %q, want %q", got[ref.Ref(4)], src)
	}
}

// assertWorldsMatch compares two worlds field by field.
func assertWorldsMatch(t *testing.T, want, got *world.World) {
	t.Helper()
	if got.Len() != want.Len() {
		t.Fatalf("object count = %d, want %d", got.Len(), want.Len())
	}
	if got.Top() != want.Top() {
		t.Errorf("Top() = %v, want %v", got.Top(), want.Top())
	}

	want.Each(func(a *world.Object) bool {
		b := got.Get(a.Ref)
		if b == nil {
			t.Errorf("%v is missing after reload", a.Ref)
			return true
		}
		if a.Name != b.Name {
			t.Errorf("%v name = %q, want %q", a.Ref, b.Name, a.Name)
		}
		if a.Flags != b.Flags {
			t.Errorf("%v flags = %#x, want %#x", a.Ref, uint32(b.Flags), uint32(a.Flags))
		}
		for _, f := range []struct {
			name   string
			wa, gb ref.Ref
		}{
			{"owner", a.Owner, b.Owner},
			{"location", a.Location, b.Location},
			{"contents", a.Contents, b.Contents},
			{"exits", a.Exits, b.Exits},
			{"next", a.Next, b.Next},
			{"home", a.Home, b.Home},
			{"dropto", a.Dropto, b.Dropto},
		} {
			if f.wa != f.gb {
				t.Errorf("%v %s = %v, want %v", a.Ref, f.name, f.gb, f.wa)
			}
		}
		if !a.Created.Equal(b.Created) || !a.Modified.Equal(b.Modified) || !a.LastUsed.Equal(b.LastUsed) {
			t.Errorf("%v timestamps differ: %v/%v/%v vs %v/%v/%v", a.Ref,
				b.Created, b.Modified, b.LastUsed,
				a.Created, a.Modified, a.LastUsed)
		}
		if a.UseCount != b.UseCount {
			t.Errorf("%v use count = %d, want %d", a.Ref, b.UseCount, a.UseCount)
		}
		if a.PasswordHash != b.PasswordHash {
			t.Errorf("%v password hash differs", a.Ref)
		}
		if len(a.Dest) != len(b.Dest) {
			t.Errorf("%v has %d destinations, want %d", a.Ref, len(b.Dest), len(a.Dest))
		} else {
			for i := range a.Dest {
				if a.Dest[i] != b.Dest[i] {
					t.Errorf("%v destination %d = %v, want %v", a.Ref, i, b.Dest[i], a.Dest[i])
				}
			}
		}

		wantProps, gotProps := a.Props.All(), b.Props.All()
		if len(wantProps) != len(gotProps) {
			t.Errorf("%v has %d properties, want %d", a.Ref, len(gotProps), len(wantProps))
			return true
		}
		for i := range wantProps {
			if wantProps[i] != gotProps[i] {
				t.Errorf("%v property %d = %+v, want %+v",
					a.Ref, i, gotProps[i], wantProps[i])
			}
		}
		return true
	})

	// Containment chains must survive in the same order.
	want.Each(func(a *world.Object) bool {
		for _, c := range []struct {
			name   string
			wa, gb []ref.Ref
		}{
			{"contents", want.Contents(a.Ref), got.Contents(a.Ref)},
			{"exits", want.Exits(a.Ref), got.Exits(a.Ref)},
		} {
			if len(c.wa) != len(c.gb) {
				t.Errorf("%v %s = %v, want %v", a.Ref, c.name, c.gb, c.wa)
				continue
			}
			for i := range c.wa {
				if c.wa[i] != c.gb[i] {
					t.Errorf("%v %s order = %v, want %v", a.Ref, c.name, c.gb, c.wa)
					break
				}
			}
		}
		return true
	})
}
