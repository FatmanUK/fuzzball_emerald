package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ascii"

// primIndex maps a folded primitive name to its number, which is its index in
// primNames plus one. Zero means "not a primitive", as get_primitive returns.
var primIndex = func() map[string]int {
	m := make(map[string]int, len(primNames))
	for i, n := range primNames {
		m[ascii.Fold(n)] = i + 1
	}
	return m
}()

// PrimNumber returns a primitive's number, or 0 when the name is not one.
//
// Internal primitives are excluded: their names begin with a space so the
// tokenizer can never produce them, but a lookup by a crafted name would
// otherwise find them.
func PrimNumber(name string) int {
	if name == "" || name[0] == ' ' {
		return 0
	}
	return primIndex[ascii.Fold(name)]
}

// PrimName returns a primitive's name, or "?" when the number is not one.
func PrimName(n int) string {
	if n < 1 || n > len(primNames) {
		return "?"
	}
	return primNames[n-1]
}

// primLevels indexes the mucker-level floors by primitive number, so the
// dispatcher can check one without a string lookup per instruction.
var primLevels = func() []int {
	out := make([]int, len(primNames)+1)
	for name, lv := range primMLevel {
		if n := primIndex[ascii.Fold(name)]; n != 0 {
			out[n] = lv
		}
	}
	return out
}()

// PrimMLevel is the mucker level a primitive requires, or zero when any
// program may call it.
func PrimMLevel(n int) int {
	if n < 1 || n >= len(primLevels) {
		return 0
	}
	return primLevels[n]
}

// PrimCount is how many names the table holds.
func PrimCount() int { return len(primNames) }

// Numbers of the instructions the compiler emits directly. Looking them up by
// name rather than hard-coding an index keeps them correct if the table moves.
var (
	InJmp           = mustPrim("JMP")
	InRead          = mustPrim("READ")
	InSleep         = mustPrim("SLEEP")
	InCall          = mustPrim("CALL")
	InExecute       = mustPrim("EXECUTE")
	InExit          = mustPrim("EXIT")
	InEventWaitFor  = mustPrim("EVENT_WAITFOR")
	InCatch         = mustPrim("CATCH")
	InCatchDetailed = mustPrim("CATCH_DETAILED")

	// The internal loop and try primitives, which a program cannot name.
	InFor     = mustInternalPrim(" FOR")
	InForeach = mustInternalPrim(" FOREACH")
	InForIter = mustInternalPrim(" FORITER")
	InForPop  = mustInternalPrim(" FORPOP")
	InTryPop  = mustInternalPrim(" TRYPOP")
)

func mustPrim(name string) int {
	n := PrimNumber(name)
	if n == 0 {
		panic("muf: missing primitive " + name)
	}
	return n
}

// mustInternalPrim looks up a name the tokenizer cannot produce, so PrimNumber
// deliberately refuses it.
func mustInternalPrim(name string) int {
	n := primIndex[ascii.Fold(name)]
	if n == 0 {
		panic("muf: missing internal primitive " + name)
	}
	return n
}
