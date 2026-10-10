package muf

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
)

// TestPrimitiveCoverage is what makes the primitive count
// trustworthy, and it used to be what made it untrustworthy.
//
// It reported the figures with t.Logf and asserted nothing, so it
// passed whatever they were. That is how "412 of the 417 primitive
// names" came to be repeated in the README, CLAUDE.md and
// docs/upstream-coverage.md while NEWPROGRAM and CHECKARGS were
// registered stubs that aborted when a program reached them: a stub
// is registered like anything else, so it was counted, and no test
// could tell the difference.
//
// It asserts now. The command count has always been trustworthy
// because dispatch_table.go *is* the command surface and a name with
// no handler says so when typed; this is the primitives' equivalent.
//
// A submodule bump is expected to break it, and should: moving to a
// new upstream release can add names to the table, and a new name
// with no implementation is exactly what this should refuse to let
// through quietly.
func TestPrimitiveCoverage(t *testing.T) {
	// The internal primitives have names beginning with a space
	// so the tokenizer can never produce them. They are the five
	// in "412 of 417" and nothing can implement them.
	var nameable int
	var missing []string
	for i := 1; i <= PrimCount(); i++ {
		name := PrimName(i)
		if strings.HasPrefix(name, " ") {
			continue
		}
		nameable++
		// A primitive the compiler emits as an instruction is
		// answered by Frame.primitive rather than from the
		// prims map. Counting those as missing is what made
		// every previous survey of this file report nine gaps
		// that were not there.
		if _, ok := prims[i]; !ok && !Dispatched(i) {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	t.Logf("%d of %d primitives implemented, %d nameable, "+
		"%d missing, %d stubs",
		Implemented(), PrimCount(), nameable, len(missing),
		len(stubs))

	if len(missing) > 0 {
		t.Errorf("%d primitives have no implementation: %s",
			len(missing), strings.Join(missing, ", "))
	}
	// A stub counts as present to len(prims) but is not an
	// implementation. registerStub is the only way to add one and
	// it records the reason, so this names it.
	if got := Stubs(); len(got) > 0 {
		names := make([]string, 0, len(got))
		for name, why := range got {
			names = append(names, name+" ("+why+")")
		}
		sort.Strings(names)
		t.Errorf("%d primitives are stubs: %s", len(got),
			strings.Join(names, ", "))
	}
	// The arithmetic rather than a typed number, so implementing
	// a primitive a submodule bump introduced needs no edit here.
	if Implemented() != nameable {
		t.Errorf("Implemented() = %d, want %d — every "+
			"nameable primitive should be implemented",
			Implemented(), nameable)
	}

	if path := os.Getenv("MUF_MISSING_OUT"); path != "" {
		if err := os.WriteFile(path,
			[]byte(strings.Join(missing, "\n")+"\n"),
			0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote the missing list to %s", path)
	}
}

// TestDispatchedAreNotAlsoRegistered guards the one way Implemented()
// could overcount: it adds len(prims) and len(dispatched), so a
// primitive in both tables would be counted twice and could hide a
// genuine gap.
func TestDispatchedAreNotAlsoRegistered(t *testing.T) {
	for n := range dispatched {
		if _, ok := prims[n]; ok {
			t.Errorf("%s is dispatched and registered",
				PrimName(n))
		}
	}
}

// TestStubsAreNotCountedAsImplemented covers the mechanism itself,
// which is otherwise unused code: there are no stubs left, so nothing
// exercises registerStub's bookkeeping.
//
// It replaces TestUnimplementedPrimitiveIsReported in vmtest, which
// used CHECKARGS as its subject and had none once CHECKARGS landed.
// Testing the mechanism rather than naming a primitive is the better
// shape anyway — a named subject is exactly what went stale.
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

// TestRegisterStubDoesBothHalves is the function itself, which the
// two tests above do not reach: they put an entry in `stubs` by hand,
// so the thing that could come apart — installing the abort and
// recording the gap in **one** call — was never exercised.
//
// It unregisters afterwards, because `register` panics on a duplicate
// and every other test in this package counts `prims`.
func TestRegisterStubDoesBothHalves(t *testing.T) {
	// A real name nothing has claimed would be a contradiction:
	// every primitive is implemented. So this borrows one, clears
	// its entry, and puts it back.
	const name = "SMATCH"
	n := primIndex[ascii.Fold(name)]
	if n == 0 {
		t.Fatalf("%s is not a primitive", name)
	}
	saved := prims[n]
	prims[n] = nil
	t.Cleanup(func() {
		prims[n] = saved
		delete(stubs, name)
	})

	registerStub(name, "borrowed by a test")

	if why := Stubs()[name]; why != "borrowed by a test" {
		t.Errorf("the gap was not recorded: %q", why)
	}
	if prims[n] == nil {
		t.Fatal("no implementation was installed")
	}
	if _, err := prims[n](&Frame{}); err == nil {
		t.Error("the installed primitive did not abort")
	}
}

// TestStubFuncAborts checks that a stub actually refuses rather than
// silently doing nothing, which is the whole reason a stub is
// preferable to a missing entry.
func TestStubFuncAborts(t *testing.T) {
	_, err := stubFunc("WIDGET")(&Frame{})
	if err == nil {
		t.Fatal("a stub should abort")
	}
	if got := err.Error(); got !=
		"WIDGET is not implemented yet" {
		t.Errorf("got %q", got)
	}
}
