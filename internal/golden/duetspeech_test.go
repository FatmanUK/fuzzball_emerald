package golden

import (
	"context"
	"testing"
)

// The first case driven by two seats, and what it found.
//
// `page` and `whisper` are written for an audience that is not the
// actor, and a one-seat transcript sees only the actor's half. Both
// were wrong in nearly every line:
//
//   - `do_page` (`speech.c:432`) refuses a **HAVEN** player — "That
//     player does not wish to be disturbed." — the one flag
//     a player sets to stop being paged, and nothing read it.
//   - It **charges `lookup_cost`**, like any other lookup by name.
//   - The message names the **room** the pager is in, which is most
//     of the point of a page: it says where to come.
//   - The sender is told "Your message has been sent." — not what
//     was sent. There is no echo of the text at all.
//   - A failed name says "I don't recognize that name.", in
//     upstream's spelling.
//   - `do_whisper` (`speech.c:395`) has **no type check**:
//     `notify_listeners` decides what can hear, so a THING is a
//     legal target. "You can only whisper to a player." was invented
//     here, as were both of its match failures — upstream uses
//     `noisy_match_result` like every other command.
//   - Its match list is neighbours and "me", widened for a wizard
//     who is a **player**, so a wizard's puppet cannot whisper
//     across the game.
//   - And both decide "X is not connected." from whether anybody
//     *heard*, after the message has been composed — so the branch
//     comes last rather than first.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears. The program's only job is to let seat
// B ignore seat A, which has no command: the list is a reflist under
// "@__sys__", and @set refuses a system property. IGNORE_ADD is
// mucker 3, which the fixture compiles at, and takes the ignorer
// under the ignored.
const duetSpeechSource = `: main pop me @ #1 ignore_add ;`

// duetSpeechSetup runs on seat A before seat B connects, which is
// when the second player has to be made.
var duetSpeechSetup = Script{
	"@tune penny_rate=0",
	"@pcreate Bob=secret",
}

var duetSpeech = Duet{
	// In the same room to begin with: whisper needs a neighbour.
	Say("@teleport *Bob=#0"),

	// A page names the room, and the sender hears only that it
	// went.
	Say("page Bob=are you there"),
	Hear("page One=yes"),

	// An empty page is a nudge.
	Say("page Bob="),

	// A whisper, which both halves of show.
	Say("whisper Bob=come here"),
	Hear("whisper One=on my way"),

	// A whisper to a **thing** is legal, and goes nowhere because
	// a thing has no connection -- so the sender is told it is
	// not connected.
	Say("@create rock"),
	Say("drop rock"),
	Say("whisper rock=hello"),

	// ...unless the thing is a ZOMBIE, when its owner hears it
	// prefixed and the whisper counts as delivered.
	Say("@set rock=zombie"),
	Say("whisper rock=hello again"),

	// A failed name, and an ambiguous one.
	Say("page Nobody=hello"),
	Say("whisper nosuchthing=hello"),

	// HAVEN stops a page and does not stop a whisper.
	Hear("@set me=haven"),
	Say("page Bob=still there"),
	Say("whisper Bob=still there"),
	Hear("@set me=!haven"),

	// A whisper reaches somebody in another room only for a
	// wizard, through match_player -- and a page reaches them
	// either way, which is the difference between the two
	// commands.
	Say("@dig Far"), // #6, since the rock took #5
	Say("@teleport *Bob=#6"),
	Say("whisper Bob=across the world"),
	Say("whisper *Bob=across the world"),
	Say("page Bob=across the world"),

	// lookup_cost makes a page cost money, which nothing read.
	// The money is minted **before** the quell, because a quelled
	// wizard has to pay for what it gives.
	Say("@tune lookup_cost=40"),
	Say("give me=100"),
	Say("@set me=quell"),
	Say("score"),
	Say("page Bob=an expensive one"),
	Say("score"),
	Say("page Bob=another"),
	Say("score"),
	Say("page Bob=one too many"),
	Say("score"),
	// Unquelled before the parameter is put back, because a
	// quelled wizard is not allowed to @tune at all.
	Say("@set me=!quell"),
	Say("@tune lookup_cost=0"),

	// The ignore filter, which `notify_filtered` applies and a
	// bare `notify` does not -- so a page to somebody who is
	// ignoring you answers "not connected" even though they are,
	// and a whisper the same.
	//
	// It needs **two mortals**: `ignore_is_ignoring_sub` refuses
	// outright when either owner is a wizard, so #1 quells again
	// once the teleport is done.
	Say("@teleport *Bob=#0"),
	Say("@set me=quell"),
	Hear("test"),
	Say("page Bob=are you ignoring me"),
	Say("whisper Bob=are you ignoring me"),
	Say("@set me=!quell"),
}

// TestDuetSpeechMatchesFuzzball compares both seats, step by step.
func TestDuetSpeechMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), duetSpeechSource)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := RunOracleDuet(ctx, fx, duetSpeechSetup, "Bob",
		"secret", duetSpeech)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldDuet(ctx, fx, duetSpeechSetup,
		"Bob", "secret", duetSpeech)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}
	CompareDuets(t, duetSpeech, oracle, emerald)
}
