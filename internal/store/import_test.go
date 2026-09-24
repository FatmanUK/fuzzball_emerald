package store

import (
	"context"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/importer"
	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestImportStarterWorldRoundTrips is the M2 acceptance check: the
// shipped starter database goes into Postgres and comes back
// unchanged.
func TestImportStarterWorldRoundTrips(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	res, err := importer.Load(importer.Source{
		DumpPath: "../../testdata/starterdb/starterdb.db",
	})
	if err != nil {
		t.Fatalf("importing: %v", err)
	}
	original := res.World

	if err := s.Flush(ctx, original.TakeSnapshot()); err != nil {
		t.Fatalf("writing: %v", err)
	}
	progs := make(map[ref.Ref]string, len(res.Programs))
	for _, p := range res.Programs {
		progs[p.Ref] = p.Source
	}
	if err := s.SavePrograms(ctx, progs); err != nil {
		t.Fatal(err)
	}
	macros := make([]Macro, 0, len(res.Macros))
	for _, m := range res.Macros {
		macros = append(macros, Macro{
			Name:       m.Name,
			Definition: m.Definition,
			Owner:      int32(m.Owner),
		})
	}
	if err := s.SaveMacros(ctx, macros); err != nil {
		t.Fatal(err)
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
		t.Errorf("%d chains needed repair after a clean import", rep.ChainsRepaired)
	}
	assertWorldsMatch(t, original, reloaded)

	// Programs and macros come back too.
	gotProgs := map[ref.Ref]string{}
	n, err := s.LoadPrograms(ctx, func(r ref.Ref, src string) error {
		gotProgs[r] = src
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != len(progs) {
		t.Errorf("loaded %d programs, want %d", n, len(progs))
	}
	for r, want := range progs {
		if gotProgs[r] != want {
			t.Errorf("%v source differs after a round trip", r)
		}
	}

	gotMacros, err := s.LoadMacros(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotMacros) != len(macros) {
		t.Errorf("loaded %d macros, want %d", len(gotMacros), len(macros))
	}

	// The documented starter password must still work after the
	// round trip, which is the whole point of carrying legacy
	// hashes across.
	one, ok := reloaded.PlayerNamed("One")
	if !ok {
		t.Fatal("player One is missing after the round trip")
	}
	if !password.Verify(reloaded.Get(one).PasswordHash, "potrzebie").OK {
		t.Error("One's starter password no longer verifies after import and reload")
	}
}

func TestResetEmptiesEverything(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	w := buildWorld(t)
	if err := s.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePrograms(ctx, map[ref.Ref]string{4: "source"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMacros(ctx, []Macro{{Name: "m", Definition: "d"}}); err != nil {
		t.Fatal(err)
	}

	if err := s.Reset(ctx); err != nil {
		t.Fatal(err)
	}

	empty, err := s.IsEmpty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !empty {
		t.Error("Reset left objects behind")
	}
	reloaded := world.New()
	rep, err := s.Load(ctx, reloaded)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Objects != 0 || rep.Properties != 0 || rep.Tune != 0 {
		t.Errorf("Reset left data behind: %+v", rep)
	}
	got, err := s.LoadMacros(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Reset left %d macros behind", len(got))
	}
}

func TestSaveMacrosReplacesWholesale(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.SaveMacros(ctx, []Macro{
		{Name: "a", Definition: "1"}, {Name: "b", Definition: "2"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMacros(ctx, []Macro{{Name: "c", Definition: "3"}}); err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadMacros(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "c" {
		t.Errorf("macros = %+v, want just c", got)
	}
}
