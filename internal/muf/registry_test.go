package muf

import "testing"

func TestPrimTableIsPopulated(t *testing.T) {
	// 9 base instructions plus 408 primitives, from Fuzzball's
	// tables.
	if got := PrimCount(); got != 417 {
		t.Errorf("PrimCount() = %d, want 417", got)
	}
}

func TestPrimLookupIsCaseInsensitive(t *testing.T) {
	for _, name := range []string{"notify", "NOTIFY", "NoTiFy"} {
		if PrimNumber(name) == 0 {
			t.Errorf("PrimNumber(%q) = 0, want a primitive", name)
		}
	}
	if PrimNumber("notify") != PrimNumber("NOTIFY") {
		t.Error("case should not change a primitive's number")
	}
}

func TestPrimNumberAndNameRoundTrip(t *testing.T) {
	for _, name := range []string{"POP", "DUP", "SWAP", "@", "!", "{", "}", "+", "strcat"} {
		n := PrimNumber(name)
		if n == 0 {
			t.Errorf("PrimNumber(%q) = 0", name)
			continue
		}
		// The table stores the canonical spelling, which may
		// differ in case from what was looked up.
		if got := PrimName(n); PrimNumber(got) != n {
			t.Errorf("PrimName(PrimNumber(%q)) = %q, which does not round trip", name, got)
		}
	}
}

func TestUnknownNamesAreNotPrimitives(t *testing.T) {
	for _, name := range []string{"", "nosuchprimitive", "IF", "THEN", "BEGIN"} {
		if got := PrimNumber(name); got != 0 {
			t.Errorf("PrimNumber(%q) = %d, want 0", name, got)
		}
	}
}

// TestInternalPrimitivesAreUnreachable checks that the primitives the
// compiler emits for loops and try blocks cannot be named by a
// program. Their names begin with a space, which the tokenizer can
// never produce, but a lookup must refuse them too.
func TestInternalPrimitivesAreUnreachable(t *testing.T) {
	for _, name := range []string{" FOR", " FOREACH", " FORITER", " FORPOP", " TRYPOP"} {
		if got := PrimNumber(name); got != 0 {
			t.Errorf("PrimNumber(%q) = %d, want 0: internal primitives must not be nameable", name, got)
		}
	}
	// They still resolve internally.
	for _, n := range []int{InFor, InForeach, InForIter, InForPop, InTryPop} {
		if n == 0 {
			t.Error("an internal primitive did not resolve")
		}
	}
}

func TestPrimNameOutOfRange(t *testing.T) {
	for _, n := range []int{-1, 0, PrimCount() + 1} {
		if got := PrimName(n); got != "?" {
			t.Errorf("PrimName(%d) = %q, want ?", n, got)
		}
	}
}
