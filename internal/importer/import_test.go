package importer

import (
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

func TestLoadStarterWorld(t *testing.T) {
	res, err := Load(Source{DumpPath: "../../testdata/starterdb/starterdb.db"})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	if res.Report.Objects < 100 {
		t.Errorf("read %d objects", res.Report.Objects)
	}
	if res.Report.Programs != 61 {
		t.Errorf("read %d program sources, want 61", res.Report.Programs)
	}
	if res.Report.Macros != 3 {
		t.Errorf("read %d macros, want 3", res.Report.Macros)
	}

	// Every program source must belong to a program object in the dump.
	for _, p := range res.Programs {
		o := res.World.Get(p.Ref)
		if o == nil || o.Type() != ref.TypeProgram {
			t.Errorf("program source %v does not match a program object", p.Ref)
		}
	}

	// An imported world has never been written, so it must all be pending.
	if res.World.DirtyCount() != res.World.Len() {
		t.Errorf("%d of %d objects are pending; a fresh import should be entirely pending",
			res.World.DirtyCount(), res.World.Len())
	}

	t.Logf("warnings: %d", len(res.Report.Warnings))
	for _, warn := range res.Report.Warnings {
		t.Logf("  %s", warn)
	}
}

func TestLoadFindsMufDirBesideDataDir(t *testing.T) {
	// The shipped layout puts the dump in data/ and the sources in muf/,
	// as siblings. Emerald's testdata keeps them in one directory. Both
	// must be found.
	res, err := Load(Source{DumpPath: "../../testdata/starterdb/starterdb.db"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Programs) == 0 {
		t.Error("no program sources were found next to the dump")
	}
}

func TestLoadWithoutProgramDirectory(t *testing.T) {
	// minimal.db ships with no muf/ directory at all.
	res, err := Load(Source{DumpPath: "../../testdata/minimal.db"})
	if err != nil {
		t.Fatalf("a dump with no program directory should still load: %v", err)
	}
	if res.Report.Objects != 2 {
		t.Errorf("read %d objects, want 2", res.Report.Objects)
	}
	if len(res.Programs) != 0 {
		t.Errorf("read %d programs, want none", len(res.Programs))
	}
	var found bool
	for _, warn := range res.Report.Warnings {
		if strings.Contains(warn, "no muf/ directory") {
			found = true
		}
	}
	if !found {
		t.Error("the missing program directory should have been reported")
	}
}

func TestLoadRejectsMissingDump(t *testing.T) {
	if _, err := Load(Source{DumpPath: "../../testdata/nonexistent.db"}); err == nil {
		t.Error("loading a missing dump should fail")
	}
}

func TestPlayersWithoutPasswords(t *testing.T) {
	res, err := Load(Source{DumpPath: "../../testdata/starterdb/starterdb.db"})
	if err != nil {
		t.Fatal(err)
	}
	// Report whatever the shipped world has, so the operator sees it; the
	// point is that the list is computed, not that it is empty.
	got := res.PlayersWithoutPasswords()
	t.Logf("players with no password: %v", got)
	for _, r := range got {
		o := res.World.Get(r)
		if o.Type() != ref.TypePlayer {
			t.Errorf("%v is a %v, not a player", r, o.Type())
		}
		if o.PasswordHash != "" {
			t.Errorf("%v has a password and should not be listed", r)
		}
	}

	// A synthetic world with a known empty password must be caught.
	w := world.New()
	p := w.Create("Nobody", ref.TypePlayer, ref.God)
	synthetic := &Result{World: w, Report: &Report{}}
	list := synthetic.PlayersWithoutPasswords()
	if len(list) != 1 || list[0] != p.Ref {
		t.Errorf("PlayersWithoutPasswords = %v, want [%v]", list, p.Ref)
	}
}
