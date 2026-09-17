package importer

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Macro is one entry of the MUF editor's macro table.
type Macro struct {
	Name       string
	Definition string
	Owner      ref.Ref
}

// ParseMacros reads the muf/macros file, which stores three lines per macro:
// the name, the definition, and the owner's dbref without a leading '#'.
func ParseMacros(r io.Reader) ([]Macro, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)

	var out []Macro
	line := 0
	read := func() (string, bool) {
		if !sc.Scan() {
			return "", false
		}
		line++
		return sc.Text(), true
	}

	for {
		name, ok := read()
		if !ok {
			break
		}
		// Upstream writes no blank lines, but tolerate a trailing one.
		if strings.TrimSpace(name) == "" {
			continue
		}
		def, ok := read()
		if !ok {
			return out, fmt.Errorf("line %d: macro %q has no definition", line, name)
		}
		ownerText, ok := read()
		if !ok {
			return out, fmt.Errorf("line %d: macro %q has no owner", line, name)
		}
		owner, err := strconv.ParseInt(strings.TrimSpace(ownerText), 10, 32)
		if err != nil {
			return out, fmt.Errorf("line %d: macro %q has a bad owner %q",
				line, name, ownerText)
		}
		out = append(out, Macro{Name: name, Definition: def, Owner: ref.Ref(owner)})
	}
	return out, sc.Err()
}

// ProgramSource is one program's text, keyed by the ref its file is named for.
type ProgramSource struct {
	Ref    ref.Ref
	Source string
}

// LoadProgramDir reads the muf/ directory that sits beside a dump. Fuzzball
// keeps program text in files named <dbref>.m rather than inside the dump, so
// a dump on its own carries no code at all.
//
// Files whose names are not a dbref are ignored, which is what lets the macro
// table live in the same directory.
func LoadProgramDir(dir string) ([]ProgramSource, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var out []ProgramSource
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".m") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".m")
		n, err := strconv.ParseInt(base, 10, 32)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", e.Name(), err)
		}
		out = append(out, ProgramSource{Ref: ref.Ref(n), Source: string(data)})
	}
	return out, nil
}
