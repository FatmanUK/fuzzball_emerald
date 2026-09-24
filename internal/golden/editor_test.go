package golden

import (
	"context"
	"strings"
	"testing"
	"time"
)

// editorScript drives a whole editing session, from creating a
// program to saving it and listing it back.
//
// Every line is deliberate. The session covers where inserted lines
// land, the argument forms each command takes, what happens when a
// command is given nonsense, and which lines the parser is still
// allowed to see while the editor holds the input.
var editorScript = Script{
	"@program probe",

	// Typing a program in from empty.
	"i",
	"one",
	"two",
	"three",
	".",

	// Listing, with and without numbers. The bare "l" lists the
	// current line, which after an insert run is past the end of
	// the buffer.
	"1 n",
	"1 99 l",
	"l",
	"2 l",
	"3 1 l",
	"1 2 3 l",

	// Inserting in the middle, at the front, and past the end.
	"2 i",
	"inserted",
	".",
	"1 99 l",
	"0 i",
	"at the front",
	".",
	"1 99 l",
	"99 i",
	"at the back",
	"",
	".",
	"1 99 l",

	// Deleting: a range, a single line, and the arguments that
	// are refused.
	"2 3 d",
	"1 99 l",
	"9 d",
	"3 1 d",
	"0 d",
	"1 2 3 d",

	// Line numbers off again, then a listing to prove it.
	"n",
	"1 99 l",
	"0 n",
	"1 n",

	// Macros.
	"def dbl 2 *",
	"def dbl 3 *",
	"def 9bad 1 +",
	"def .dotted 1 +",
	"def lonely",
	"def",
	"def aaa 1 +",
	"def zzz 2 +",
	"s",
	"a",
	"aaa s",
	"aaa dbl s",
	"aaa dbl a",
	"dbl k",
	"dbl k",

	// Lines the editor answers itself, and the two that reach
	// past it.
	"jump",
	"WHO",
	"look",
	"@Q",

	// Compiling what is in the buffer: this program does not
	// compile, so the error names the line.
	"c",
	"p",
	"u",

	// Replace it with something that does compile, then check the
	// same three commands again.
	"1 99 d",
	"i",
	": helper 1 pop ;",
	"public helper",
	": main me @ \"ok\" notify ;",
	".",
	"c",
	"p",
	"q",

	// Outside the editor again.
	"@list probe",
	"@list probe=#",
	"@list probe=2",
	"@list probe=2-3",
	"@list probe=!",
	"@list probe=@",

	// Reading another program's header comment.
	"@program viewed",
	"i",
	"( what this library does )",
	"( and how to call it )",
	": main 1 pop ;",
	".",
	"q",
	"@edit probe",
	"5 v",
	"4 v",
	"v",
	"1 2 v",
	"0 v",
	"x",

	// Reopening resumes at the line the last session left.
	"@edit probe",
	"x",
	"@edit probe",
	"q",
}

// TestEditorMatchesFuzzball runs a full editor session against both
// servers.
//
// The marker-bounded driver cannot be used here: the editor reads the
// marker pose as a command, because only the first letter of the last
// word means anything and "!pose EMERALDDONE" ends in one beginning
// with 'x'. This case waits for quiet instead, which is slower and so
// is kept to one case.
func TestEditorMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, editorScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, editorScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range editorScript {
		var want, got string
		if i < len(oracle) {
			want = oracle[i]
		}
		if i < len(emerald) {
			got = emerald[i]
		}
		if diffs := Compare(dropOptimizerNote(want), got); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}

// dropOptimizerNote removes the line upstream prints when its
// peephole pass fuses instructions.
//
// Emerald does not implement that pass, which is a known and
// documented divergence: it changes the instruction numbers an error
// message quotes and nothing else. Everything the compile actually
// produces is still compared.
func dropOptimizerNote(s string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "Program optimized by ") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
