package golden

import (
	"context"
	"testing"
)

// Three seats, where **two of them are the same player**. That is the
// last shape of output no transcript could reach, and three things
// need it:
//
//   - `@wall` goes through `notify_listeners` once per connected
//     **descriptor** (`speech.c:125`), and each notify reaches all
//     of that player's descriptors — so a player connected twice
//     hears a shout *four* times, not two.
//   - The `connect` **action** fires only when
//     `PLAYER_DESCRCOUNT(player) == 1` (`interface.c:1082`), where
//     the `_connect` propqueues fire on every connection. Upstream's
//     own comment calls that odd.
//   - And the "has connected." line is **not** gated on the count at
//     all, which the plan for this work had backwards: it is tested
//     only against DARK, so it fires on every connection.
//
// The fixture's program announces to **One**, which reaches both of
// One's connections -- so the `connect` action's one firing is
// visible on seats A and C even though its own output goes to whoever
// connected.
const trioSource = `: main
  pop
  #1 "connect action ran" notify
;`

var trioSetup = Script{
	"@tune penny_rate=0",
	"@pcreate Bob=secret",

	// A `connect` exit at priority 1 -- `can_move(..., 1)` is the
	// match level announce_connect uses -- pointed at a program
	// that says so, which is how the first-connection rule
	// becomes visible. It is attached to the room, so everybody
	// arriving there can reach it.
	"@action connect=here",
	"@set connect=1",
	"@link connect=#2",

	// ...and the propqueue half, which fires every time.
	"@set here=_connect:&{null:{tell:connect propqueue ran,me}}",
}

// trioLogins seats Bob and then **One again**, so seats A and C are
// one player on two connections.
var trioLogins = []Login{
	{Name: "Bob", Password: "secret"},
	{Name: "One", Password: godPassword},
}

var trio = Duet{
	// The first step also carries what the seats already open
	// heard while the others were connecting, which is the only
	// way a connect is observable at all: the arrival **line**
	// for each, One's second arrival included, plus the
	// `_connect` propqueue on every connection and the `connect`
	// action on the first one only.
	//
	// And a shout, heard by Bob once and by One twice over on
	// each of One's two connections -- three descriptors, so
	// three notifies, and One's two of those each reach both of
	// One's connections.
	Say("@wall everybody out"),

	// From One's *other* connection, to show the arithmetic does
	// not depend on which descriptor typed it.
	Also("@wall again"),

	// Bob's is refused, so the lines above are the command and
	// not something else.
	Hear("@wall let me try"),

	// A room line for contrast: `notify_except` is per
	// **player**, so One hears a pose once per connection and not
	// once per descriptor-squared.
	Say("pose waves"),

	// A puppet, which announces itself in its own room when its
	// owner connects or disconnects -- and only on the first or
	// last connection, where the arrival line is on every one.
	Say("@create poppet"),
	Say("@set poppet=zombie"),
	Say("drop poppet"),
	Say("@set poppet=_/pcon:stirs."),
}

// TestTrioMatchesFuzzball compares the three-seat transcript.
func TestTrioMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), trioSource)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := RunOracleDuet(ctx, fx, trioSetup, trioLogins,
		trio)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldDuet(ctx, fx, trioSetup,
		trioLogins, trio)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}
	CompareDuets(t, trio, oracle, emerald)
}
