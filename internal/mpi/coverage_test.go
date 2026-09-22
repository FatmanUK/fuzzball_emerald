package mpi

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// TestFunctionCoverage reports which MPI functions have implementations, the
// way internal/muf's own TestPrimitiveCoverage does for primitives: the table
// is generated from upstream's mfun_list, so the gap between it and the impls
// map is visible here rather than discovered one property at a time.
func TestFunctionCoverage(t *testing.T) {
	var missing []string
	for name := range functions {
		if _, ok := impls[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	t.Logf("%d of %d MPI functions implemented, %d missing",
		Implemented(), Count(), len(missing))

	if path := os.Getenv("MPI_MISSING_OUT"); path != "" {
		if err := os.WriteFile(path, []byte(strings.Join(missing, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote the missing list to %s", path)
	}
}
