package golden

import (
	"context"
	"regexp"
	"testing"
)

// tuneScript covers do_tune, which an earlier movement case had to
// avoid entirely: this server answered a bare @tune with a usage
// message, kept its listing behind a `#list` subcommand upstream has
// never had, and reported a set as "X set to Y." where upstream says
// "Parameter set." and then shows the parameter.
//
// It never lists the whole table. Emerald's table is deliberately not
// upstream's — the eight ssl_* parameters are gone and
// smtp_ssl_type is renamed — so only a pattern that selects
// parameters both servers carry can agree. Every parameter touched
// here is also one whose value changes nothing the rest of the script
// can see.
var tuneScript = Script{
	// Reading one, reading several, and reading none. equalstr is
	// not prefix matching: "penn" selects nothing, "pen*" selects
	// four.
	"@tune penny",
	"@tune pen*",
	"@tune penn",
	"@tune nosuchparameter",

	// Every type renders differently, and a dbref is unparsed for
	// the reader rather than printed as a number.
	"@tune muckname",
	"@tune registration",
	"@tune max_pennies",
	"@tune aging_time",
	"@tune player_start",

	// Setting: the reply is fixed and the parameter is shown
	// after it, which is how the [default] marker disappears.
	"@tune muckname=Emerald Test",
	"@tune muckname",
	"@tune %muckname",
	"@tune muckname",

	// A boolean reads only its first character, so "yes" and
	// "yellow" both set it and "true" is a syntax error.
	"@tune registration=no",
	"@tune registration=yellow",
	"@tune registration=true",
	"@tune registration=0",
	"@tune %registration",

	// An integer is number(): a sign and digits, nothing else.
	"@tune max_pennies=12345",
	"@tune max_pennies=12.5",
	"@tune max_pennies=lots",
	"@tune max_pennies=-1",
	"@tune %max_pennies",

	// A timespan wants days-and-clock or unit suffixes. A bare
	// count of seconds is refused, which reads like a bug and is
	// what tune_timespan_seconds does.
	"@tune aging_time=1d12h",
	"@tune aging_time=30m",
	"@tune aging_time=0d 4:00:00",
	"@tune aging_time=3600",
	"@tune aging_time=4:00:00",
	"@tune %aging_time",

	// A dbref is matched, not parsed, and against a short list
	// that does not include the room's contents. The type
	// constraint is a bad value rather than bad syntax.
	"@tune player_start=#0",
	"@tune player_start=here",
	"@tune player_start=me",
	"@tune player_start=nosuchroom",
	"@tune %player_start",

	// The empty value is a bad value for a parameter that is not
	// nullable, and a set all the same — the line has an "=",
	// so it is never read as a request to display.
	"@tune muckname=",
	"@tune reserved_names=",
	"@tune %reserved_names",

	// An unknown name says so on the set path and says nothing on
	// the display path, which are different messages for the same
	// mistake.
	"@tune nosuchparameter=1",
	"@tune %nosuchparameter",

	// "info" adds the group and the label. Its argument is
	// space-separated, because it shares arg1 with the pattern.
	"@tune info penny",
	"@tune info pen*",
	"@tune info nosuchparameter",

	// A @tune cannot be forced, which is the only permission
	// check do_tune makes for itself.
	"@force me=@tune muckname=Forced",
	"@tune muckname",

	// The abbreviation: @tune needs three characters, and @tu
	// reaches nothing.
	"@tun penny",
	"@tu penny",
}

// TestTuneMatchesFuzzball checks do_tune against the C server.
// maskDefaultMuckname blanks muckname's value on a line that carries
// the [default] marker.
//
// muckname is the one parameter whose compiled-in default this server
// answers differently on purpose -- "Emerald" where upstream ships
// "TygryssMUCK", because a fresh world introducing itself as somebody
// else's MUCK is a leak of a different kind. The [default] marker
// itself still compares, which is the half that proves a reset
// worked, and a value this script *sets* has no marker and so
// compares in full.
func maskDefaultMuckname(s string) string {
	return muckDefaultRe.ReplaceAllString(s, "${1}<default>${2}")
}

// No "$" anchor: the oracle's transcript still has its CRLF line
// endings at this point, because Normalize runs inside Compare and so
// after the mask. Anchoring to end-of-line matched Emerald's side and
// not upstream's, which is a mask that hides a divergence in one
// direction only -- the worst possible failure for one of these.
var muckDefaultRe = regexp.MustCompile(
	`(?m)^(\(str\)\s+muckname\s+= ).*?( \[default\])`)

func TestTuneMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	oracle, err := RunOracleSteps(ctx, fx, tuneScript, nil)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, tuneScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range tuneScript {
		var want, got string
		if i < len(oracle) {
			want = oracle[i]
		}
		if i < len(emerald) {
			got = emerald[i]
		}
		if diffs := Compare(maskDefaultMuckname(want),
			maskDefaultMuckname(got)); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}
