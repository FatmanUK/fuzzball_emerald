package golden

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Checking one dispatcher against another is awkward, because what
// matters is *which* command a typed word reaches and neither server
// will say. Comparing the replies does not answer it either: two
// servers can resolve a word identically and still print different
// things, which is true of most commands here and is a separate
// question from dispatch.
//
// So this case asks two narrower questions that together pin it.
//
// First, for every word: did both servers reach *something*, or
// neither? That is exactly the dispatch decision, and it catches an
// abbreviation that works on one and not the other — which is how
// every divergence found so far has shown up.
//
// Second, for the words where two commands compete: does the
// abbreviation produce the same reply as the full name it should
// reach, on each server separately? If "@cr" resolved to @credits
// rather than @create it could not possibly match "@create"'s own
// reply. That needs no hardcoded strings and no knowledge of what
// either server prints.

// dispatchScript is the first question. The words are the nodes where
// upstream's tie-break is not the obvious one.
//
// Deliberately absent: @shutdown, @armageddon, @restart, @dump,
// @toad, @boot and @pcreate in any spelling that would run them. The
// oracle is a live server and those either end it or move the
// database under the rest of the script.
var dispatchScript = Script{
	// The @co node demands a fourth and a fifth character.
	"@co", "@con", "@conl nosuchthing", "@cont nosuchthing",

	// strlen(command) < 7 splits a command from a lock.
	"@cho nosuchthing=me", "@chown_ nosuchthing=me",
	"@fo nosuchthing=look", "@force_lock nosuchthing=me",
	"@lin nosuchthing=here", "@linklock nosuchthing=me",

	// An exact sibling checked ahead of a prefix one.
	"@cr", "@cre", "@de nosuchthing=x", "@debug",

	// strcmp, so these have no abbreviation and no case folding.
	"@to", "@TOAD", "@res", "@rest", "@wal", "@WALL",
	"@san", "@sanit",

	// The reversed string_prefix: four characters minimum.
	"@un", "@unl", "@unb nosuchthing", "@unli nosuchthing",
	"@unlo nosuchthing", "@uncom",

	// Bare commands are prefix-matched, which Emerald's old
	// exact-match table did not do.
	"e nosuchthing", "ex nosuchthing", "i", "l", "lo", "le",
	"n", "sc", "gi nosuchthing=1",

	// "move" accepts trailing text, having no Matched() at all.
	"movex nosuchthing",

	// Words short enough to fall off the switch entirely.
	"@a", "@b", "@c", "@d", "@e", "@f", "@l", "@o", "@p", "@s",
	"@t", "@u", "@w",

	// And the ones that only just reach something.
	"@ac", "@at", "@di nosuchthing", "@ex nosuchthing",
	"@fa nosuchthing=x", "@fi", "@na nosuchthing=x",
	"@op nosuchthing", "@pa", "@se nosuchthing=x", "@st", "@tr",
}

// abbrevPairs is the second question: an abbreviation and the command
// it must reach, each given the same argument so their replies can
// only differ if they resolved differently.
var abbrevPairs = [][2]string{
	// Both halves of a pair run, so the argument has to be one
	// that changes nothing: "@cr widget" would create a widget
	// and "@create widget" a second one with a different dbref.
	{"@cr", "@create"},
	{"@cho nosuchthing=me", "@chown nosuchthing=me"},
	{"@chown_ nosuchthing=me", "@chown_lock nosuchthing=me"},
	{"@fo nosuchthing=look", "@force nosuchthing=look"},
	{"@force_l nosuchthing=me", "@force_lock nosuchthing=me"},
	{"@lin nosuchthing=nosuchthing",
		"@link nosuchthing=nosuchthing"},
	{"@ow nosuchthing", "@owned nosuchthing"},
	{"@ownl nosuchthing=me", "@ownlock nosuchthing=me"},
	{"@unb nosuchthing", "@unbless nosuchthing"},
	{"@unli nosuchthing", "@unlink nosuchthing"},
	{"@unlo nosuchthing", "@unlock nosuchthing"},
	{"@conl nosuchthing=me", "@conlock nosuchthing=me"},
	{"@cont nosuchthing", "@contents nosuchthing"},
	{"@de nosuchthing=x", "@describe nosuchthing=x"},
	{"e nosuchthing", "examine nosuchthing"},
	{"l", "look"},
	{"le", "leave"},
	{"i", "inventory"},
	{"movex nosuchthing", "move nosuchthing"},
}

func TestDispatchMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	script := append(Script{}, dispatchScript...)
	for _, p := range abbrevPairs {
		script = append(script, p[0], p[1])
	}

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}
	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, script, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, script, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}
	at := func(steps []string, i int) string {
		if i < len(steps) {
			return steps[i]
		}
		return ""
	}

	// Question one: did both reach something, or neither?
	for i, cmd := range dispatchScript {
		wantHuh, gotHuh := isHuh(at(oracle, i)), isHuh(at(emerald, i))
		if wantHuh == gotHuh {
			continue
		}
		// gotHuh is Emerald's answer, so Emerald is the one
		// that reached a command when it is false.
		reached, missed := "Emerald", "Fuzzball"
		if gotHuh {
			reached, missed = "Fuzzball", "Emerald"
		}
		t.Errorf("%q: %s reached a command and %s did not\n"+
			"  fuzzball: %s\n  emerald:  %s",
			cmd, reached, missed,
			firstLine(at(oracle, i)), firstLine(at(emerald, i)))
	}

	// Question two: does the abbreviation behave as the full
	// name?
	base := len(dispatchScript)
	for n, p := range abbrevPairs {
		short, full := base+2*n, base+2*n+1
		for _, side := range []struct {
			name  string
			steps []string
		}{{"fuzzball", oracle}, {"emerald", emerald}} {
			a, b := at(side.steps, short), at(side.steps, full)
			if a == b {
				continue
			}
			t.Errorf("%s: %q does not reach %q\n"+
				"  abbreviated: %s\n  full:        %s",
				side.name, p[0], p[1],
				firstLine(a), firstLine(b))
		}
	}
}

// isHuh reports whether a reply is the unrecognised-command message,
// which is what both servers say when a word reaches nothing.
func isHuh(s string) bool { return strings.Contains(s, "Huh?") }

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
