package muf

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// TestPrimitiveCoverage reports which primitives are implemented, so the gap
// is visible rather than discovered one program at a time.
func TestPrimitiveCoverage(t *testing.T) {
	var missing []string
	for i := 1; i <= PrimCount(); i++ {
		name := PrimName(i)
		if strings.HasPrefix(name, " ") {
			continue // internal, emitted by the compiler
		}
		if _, ok := prims[i]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	t.Logf("%d of %d primitives implemented, %d missing",
		Implemented(), PrimCount(), len(missing))

	if path := os.Getenv("MUF_MISSING_OUT"); path != "" {
		if err := os.WriteFile(path, []byte(strings.Join(missing, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote the missing list to %s", path)
	}
}
