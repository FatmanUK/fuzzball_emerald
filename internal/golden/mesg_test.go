package golden

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// mesgScript drives the eleven commands set_standard_property backs.
// They differ only in which property and which label they carry, so
// the interesting part is not the eleven but the shape they share: no
// "=" reports, "=" with nothing after it clears, an empty object name
// means the caller, and the label appears verbatim in all three
// replies.
var mesgScript = Script{
	"@create widget",

	// Report before set, which is where an unset property's
	// answer shows.
	"@describe widget",
	"@describe widget=A small widget.",
	"@describe widget",
	"look widget",
	"@describe widget=",
	"@describe widget",

	// An empty object name is the caller, and the report/set
	// decision is made on the whole argument — so "=shiny"
	// describes the player rather than reporting.
	"@describe",
	"@describe =An ordinary wizard.",
	"@describe",
	"@describe =",

	// The value keeps its trailing space and loses its leading
	// one; the object name loses both.
	"@describe   widget   =  padded  ",
	"@describe widget",

	// Every other member of the family, set and reported, so the
	// labels are all compared.
	"@idescribe widget=Inside the widget.",
	"@idescribe widget",
	"@success widget=You did it.",
	"@success widget",
	"@osuccess widget=did it.",
	"@osuccess widget",
	"@fail widget=You failed.",
	"@fail widget",
	"@ofail widget=failed.",
	"@ofail widget",
	"@drop widget=Dropped it.",
	"@drop widget",
	"@odrop widget=dropped it.",
	"@odrop widget",
	"@oecho widget=Widget>",
	"@oecho widget",
	"@pecho widget=Widget>",
	"@pecho widget",

	// @doing refuses any object but the caller, which upstream's
	// own comment calls senseless and does anyway.
	"@doing=Testing.",
	"@doing",
	"@doing me=Still testing.",
	"@doing me",
	"@doing widget=Nope.",

	// A name that matches nothing, and one the player does not
	// control — #0 is owned by #1 here, so only the first is
	// reachable.
	"@describe nosuchthing=x",
	"@success nosuchthing",

	// The abbreviations these commands are reachable by. @de is
	// @describe rather than @debug, and @o alone reaches nothing.
	"@desc widget",
	"@suc widget",
	"@osu widget",
	"@ofa widget",
	"@odr widget",
	"@oec widget",
	"@pec widget",
	"@ide widget",
}

// TestMessageSettersMatchFuzzball checks the set_standard_property
// family against the C server.
func TestMessageSettersMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, mesgScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, mesgScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range mesgScript {
		var want, got string
		if i < len(oracle) {
			want = oracle[i]
		}
		if i < len(emerald) {
			got = emerald[i]
		}
		if diffs := Compare(maskUnset(want),
			maskUnset(got)); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}

// unsetReport is a report of a property that is not set.
//
// Upstream prints "(null)" there, which is not a message it chose:
// GETMESG hands the NULL that get_property_class returns straight to
// notifyf_nolisten's "%s", and glibc renders a NULL argument that
// way. It is undefined behaviour with a stable-looking output, not a
// wording, and a Go server has no null pointer to print — so
// Emerald prints nothing after the colon and the line is masked here
// rather than dropped, the same treatment examine's "Memory used"
// gets.
var unsetReport = regexp.MustCompile(`^(.*): (\(null\))?$`)

// maskUnset collapses the two spellings of an unset property to one,
// keeping the line and its position.
func maskUnset(s string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if unsetReport.MatchString(line) {
			line = unsetReport.ReplaceAllString(line,
				"$1: <unset>")
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
