package golden

import (
	"context"
	"testing"
)

// `@wall` goes to every **descriptor** through `notify_listeners`
// (`speech.c:125`), not straight to each connection — and this sent
// it straight.
//
// Two things follow, and **neither can be observed today**, which is
// worth stating rather than discovering twice. The ignore filter
// applies — but `@wall` is wizard-only and `ignore_is_ignoring_sub`
// refuses outright when either owner is a wizard, so for any shouter
// who may use the command the filter is dead; and a player with two
// connections is notified once per descriptor, each notify reaching
// all of their descriptors, so they hear it twice over, which needs a
// third seat. The change is the faithful shape rather than a fix: it
// puts `@wall` through the one function that holds the rules.
//
// What two seats *do* show here is that a shout crosses rooms where
// `say` does not, and that a quelled wizard is refused the command
// entirely.
//
// The connect and disconnect announcements go with it: the line is
// for the **first** connection only where the propqueues fire on
// every one, which upstream's own comment calls odd. That half was
// already ported; what two seats add here is that the *other* player
// sees the arrival at all.
const duetWallSource = `: main 1 pop ;`

var duetWallSetup = Script{
	"@tune penny_rate=0",
	"@pcreate Bob=secret",
}

var duetWall = Duet{
	Say("@teleport *Bob=#0"),

	// Heard by both, with the shouter's own name on it.
	Say("@wall everybody out"),

	// `say` for contrast: a room line, which both hear because
	// they are in the same room.
	Say("say in here"),

	// And from the other side, to show the wizard-only command
	// refusing a mortal.
	Hear("@wall let me out"),

	// Bob leaves the room: a wall still reaches him, which is the
	// whole difference between @wall and say.
	Say("@dig Far"),
	Say("@teleport *Bob=#5"),
	Say("@wall still here"),
	Say("say only in here"),

	// A quelled wizard may not @wall at all.
	Say("@set me=quell"),
	Say("@wall can you hear me"),
	Say("@set me=!quell"),
	Say("@wall and now"),
}

// TestDuetWallMatchesFuzzball compares both seats, step by step.
func TestDuetWallMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), duetWallSource)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := RunOracleDuet(ctx, fx, duetWallSetup, "Bob",
		"secret", duetWall)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldDuet(ctx, fx, duetWallSetup,
		"Bob", "secret", duetWall)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}
	CompareDuets(t, duetWall, oracle, emerald)
}
