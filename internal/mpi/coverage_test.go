package mpi

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// TestFunctionCoverage is internal/muf's TestPrimitiveCoverage for
// MPI, and it had the same flaw: it reported the figures with t.Logf
// and asserted nothing, so "140 of 140" passed whatever was true.
//
// The table is generated from upstream's mfun_list, so the gap
// between it and the impls map is visible here rather than discovered
// one property at a time — but only now that it is an assertion. A
// submodule bump is expected to break it, and should.
func TestFunctionCoverage(t *testing.T) {
	var missing []string
	for name := range functions {
		if _, ok := impls[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	t.Logf("%d of %d MPI functions implemented, %d missing, "+
		"%d stubs",
		Implemented(), Count(), len(missing), len(stubs))

	if len(missing) > 0 {
		t.Errorf("%d functions have no implementation: %s",
			len(missing), strings.Join(missing, ", "))
	}
	// A stub counts as present to len(impls) but is not an
	// implementation. SUBLIST's was dead code shadowed by the
	// real thing, and nothing about the count would have changed
	// had it not been.
	if got := Stubs(); len(got) > 0 {
		names := make([]string, 0, len(got))
		for name, why := range got {
			names = append(names, name+" ("+why+")")
		}
		sort.Strings(names)
		t.Errorf("%d functions are stubs: %s", len(got),
			strings.Join(names, ", "))
	}
	// Every name in the table should have an implementation, so
	// the two counts are the same number. Asserting the
	// relationship rather than 140 means implementing a function
	// a submodule bump introduced needs no edit here.
	if Implemented() != Count() {
		t.Errorf("Implemented() = %d, want %d — every MPI "+
			"function should be implemented",
			Implemented(), Count())
	}

	if path := os.Getenv("MPI_MISSING_OUT"); path != "" {
		if err := os.WriteFile(path,
			[]byte(strings.Join(missing, "\n")+"\n"),
			0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote the missing list to %s", path)
	}
}

// TestStubsAreNotCountedAsImplemented covers the bookkeeping, which
// is otherwise unused code: there are no stubs left, so nothing
// exercises registerStub.
func TestStubsAreNotCountedAsImplemented(t *testing.T) {
	before := Implemented()

	const name = "TESTONLY"
	stubs[name] = "a test's own stub"
	defer delete(stubs, name)

	if got := Implemented(); got != before-1 {
		t.Errorf("Implemented() = %d with one stub, want %d",
			got, before-1)
	}
	if why := Stubs()[name]; why != "a test's own stub" {
		t.Errorf("Stubs()[%q] = %q, want the reason", name,
			why)
	}

	// Stubs returns a copy, so a caller cannot quietly empty the
	// real map and make the coverage assertion above pass.
	got := Stubs()
	delete(got, name)
	if Stubs()[name] == "" {
		t.Error("Stubs() handed out the real map")
	}
}

// TestStubImplAborts checks that a stub refuses rather than silently
// yielding empty text, which in MPI would be invisible: an MPI
// failure is reported to the player and yields empty text, so a stub
// that returned "" with no error would be indistinguishable from a
// function that legitimately produced nothing.
func TestStubImplAborts(t *testing.T) {
	out, err := stubImpl("WIDGET")(nil, nil, nil)
	if err == nil {
		t.Fatal("a stub should abort")
	}
	if out != "" {
		t.Errorf("a stub should yield no text, got %q", out)
	}
	const want = "{WIDGET}: Not implemented yet."
	if got := err.Error(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
