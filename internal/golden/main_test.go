package golden

import (
	"fmt"
	"os"
	"testing"
)

// TestMain clears away any oracle container a killed run left behind
// before the first case asks for one. See SweepStaleOracles.
func TestMain(m *testing.M) {
	if n := SweepStaleOracles(); n > 0 {
		fmt.Printf("golden: removed %d stale oracle "+
			"container(s)\n", n)
	}
	os.Exit(m.Run())
}
