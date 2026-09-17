package importer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// Source describes a legacy database on disk.
//
// Fuzzball splits a world across three places: the dump itself, a muf/
// directory of program sources named <dbref>.m, and a macros file inside it.
// A dump alone carries no code.
type Source struct {
	// DumpPath is the .db file.
	DumpPath string
	// MufDir holds the program sources and the macro table. When empty it
	// is guessed from the dump's location.
	MufDir string
}

// Result is a fully read legacy world, ready to be written to the store.
type Result struct {
	World    *world.World
	Programs []ProgramSource
	Macros   []Macro
	Report   *Report
}

// guessMufDir looks for the program directory in the two places Fuzzball's own
// layouts put it: beside the dump, and one level up from a data/ directory.
//
//	dbs/starterdb/muf/          with the dump in dbs/starterdb/data/
//	<dump dir>/muf/
func guessMufDir(dumpPath string) string {
	dir := filepath.Dir(dumpPath)
	candidates := []string{
		filepath.Join(dir, "muf"),
		filepath.Join(filepath.Dir(dir), "muf"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	return ""
}

// Load reads a legacy world from disk. It does not touch a database.
func Load(src Source) (*Result, error) {
	f, err := os.Open(src.DumpPath)
	if err != nil {
		return nil, fmt.Errorf("opening dump: %w", err)
	}
	defer f.Close()

	w := world.New()
	rep, err := Parse(f, w)
	if err != nil {
		return nil, err
	}

	res := &Result{World: w, Report: rep}

	mufDir := src.MufDir
	if mufDir == "" {
		mufDir = guessMufDir(src.DumpPath)
	}
	if mufDir == "" {
		rep.warnf("no muf/ directory found beside %s; no program source was imported",
			src.DumpPath)
		return res, nil
	}

	res.Programs, err = LoadProgramDir(mufDir)
	if err != nil {
		return nil, fmt.Errorf("reading program sources: %w", err)
	}
	rep.Programs = len(res.Programs)

	// A program file whose object is missing or is not a program would
	// otherwise be stored against nothing.
	kept := res.Programs[:0]
	for _, p := range res.Programs {
		o := w.Get(p.Ref)
		switch {
		case o == nil:
			rep.warnf("program source %v.m has no object in the dump", p.Ref)
		case o.Type() != ref.TypeProgram:
			rep.warnf("program source %v.m belongs to a %v, not a program",
				p.Ref, o.Type())
		default:
			kept = append(kept, p)
		}
	}
	res.Programs = kept
	rep.Programs = len(kept)

	// Programs in the dump with no source file cannot be run or listed.
	withSource := make(map[ref.Ref]bool, len(kept))
	for _, p := range kept {
		withSource[p.Ref] = true
	}
	missing := 0
	w.Each(func(o *world.Object) bool {
		if o.Type() == ref.TypeProgram && !withSource[o.Ref] {
			missing++
		}
		return true
	})
	if missing > 0 {
		rep.warnf("%d programs in the dump have no source file in %s", missing, mufDir)
	}

	macroPath := filepath.Join(mufDir, "macros")
	mf, err := os.Open(macroPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("opening macros: %w", err)
		}
	} else {
		defer mf.Close()
		res.Macros, err = ParseMacros(mf)
		if err != nil {
			return nil, fmt.Errorf("reading macros: %w", err)
		}
		rep.Macros = len(res.Macros)
	}

	// An imported world has never been written, so everything is pending.
	w.MarkAllDirty()
	return res, nil
}

// PlayersWithoutPasswords lists players whose dump record carried no password.
//
// Fuzzball treats an empty password as "any password works". Emerald refuses
// such logins instead, so these accounts need a password set before anyone can
// connect to them, and the operator needs telling.
func (r *Result) PlayersWithoutPasswords() []ref.Ref {
	var out []ref.Ref
	r.World.Each(func(o *world.Object) bool {
		if o.Type() == ref.TypePlayer && o.PasswordHash == "" {
			out = append(out, o.Ref)
		}
		return true
	})
	return out
}
