package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestWhoRaisesTheHighWaterMark is the one line of `dump_users` the
// oracle cannot reach: it raises `con_players_max` itself, and
// upstream's own comment calls this "an odd place to update this
// variable". With one connection, login has already recorded the same
// number, so the only way to see WHO do it is to take the record away
// first.
func TestWhoRaisesTheHighWaterMark(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.out()

	// A zero integer is an empty value, so this removes the
	// property rather than setting it -- which is the state a
	// freshly booted world is in before anybody logs in.
	err := h.engine.Do(context.Background(),
		func(w *world.World) {
			w.SetProp(ref.GlobalEnvironment,
				world.SysMaxConnects, props.Value{
					Type: props.Int, Num: 0})
		})
	if err != nil {
		t.Fatal(err)
	}

	h.send("WHO")
	if got := h.out(); !strings.Contains(got,
		"(Max was 1)") {

		t.Errorf("WHO did not raise the mark:\n%s", got)
	}
}
