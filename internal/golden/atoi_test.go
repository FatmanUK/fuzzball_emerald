package golden

import (
	"context"
	"testing"
)

// MPI read every numeric argument through a helper that aborted with
// "Non-numeric argument." on anything unparseable. That string does
// not exist in Fuzzball — `grep -rc "Non-numeric" fuzzball/src/`
// finds nothing, and the full list of upstream's MPI aborts has no
// numeric-parse error of any kind. Every mfun uses a bare atoi, which
// yields 0 and carries on.
//
// The difference is not academic: MPI runs over descriptions and
// succeed/fail messages, which routinely receive whatever a player
// typed, so an invented abort turned silent upstream behaviour into a
// visible error across about thirty functions.
//
// This drives a non-numeric argument, an empty one, a partly-numeric
// one and a signed one through everything affected, and the oracle
// decides each. C's atoi takes digits until the first character that
// is not one, so "12abc" is 12 and "abc" is 0.
const atoiSource = `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop
  var! out me @ "[" out @ strcat "]" strcat notify
;
: main
  ( --- arithmetic: every argument is an atoi --------------- )
  "{add:abc,3}" show
  "{add:2,abc}" show
  "{add:,}" show
  "{add:12abc,1}" show
  "{add:-4,2}" show
  "{add:+4,2}" show
  "{add: 7 ,1}" show
  "{subt:abc,3}" show
  "{mult:abc,3}" show
  "{div:abc,3}" show
  "{div:12,abc}" show
  "{mod:abc,3}" show
  "{mod:12,abc}" show
  "{abs:abc}" show
  "{abs:-abc}" show
  "{sign:abc}" show
  ( {max}, {min} and the four ordering tests are deliberately
    absent: they do not use atoi at all. msg_compare compares
    numerically only when *both* arguments are numbers and as
    strings otherwise, and {max} returns the argument text rather
    than a number -- so "{max:abc,2}" is "abc". Recorded in
    docs/upstream-coverage.md, not fixed here. )

  ( --- strings ---------------------------------------------- )
  ( only the zero cases here: midstr's own argument handling is
    wrong in three further ways and is recorded rather than fixed,
    so a case exercising non-zero positions would be testing that
    bug instead of this one. )
  "{midstr:hello,abc}" show
  "{midstr:hello,2,abc}" show
  "{midstr:hello,abc,3}" show

  ( --- justification: fieldwidth is an atoi, and upstream
        refuses on *range* rather than on parsing ----------- )
  "{left:hi,abc}|" show
  "{right:hi,abc}|" show
  "{center:hi,abc}|" show
  ( the one-argument form, which all three accept and which used
    to panic here on args[1] )
  "{left:hi}|" show
  "{right:hi}|" show
  "{center:hi}|" show
  ( and the two range refusals upstream has instead of a parse
    refusal )
  "{left:hi,9999999}" show
  "{right:hi,9999999}" show
  "{center:hi,9999999}" show
  "{left:hi,5,}" show
  "{right:hi,5,}" show
  "{center:hi,5,}" show

  ( --- durations ------------------------------------------- )
  "{timestr:abc}" show
  "{stimestr:abc}" show
  "{ltimestr:abc}" show

  ( --- lists ----------------------------------------------- )
  "{sublist:a b c,abc}" show
  "{sublist:a b c,1,abc}" show

  ( --- dice: a range abort upstream does have -------------- )
  ( these two are deterministic because a zero count or a zero
    number of sides rolls nothing. A case with both non-zero
    cannot be compared at all -- the roll is random, like
    choose_thing's coin toss. )
  "{dice:abc}" show
  "{dice:6,abc}" show

  ( --- {for}, whose bounds go through the same read -------- )
  "{for:i,abc,3,1,x}" show
  "{for:i,1,abc,1,y}" show
;`

// TestMPIAtoiMatchesFuzzball drives the lot through both servers.
func TestMPIAtoiMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), atoiSource)
	if err != nil {
		t.Fatal(err)
	}
	script := Script{"test"}
	oracle, err := RunOracleSteps(ctx, fx, script, nil)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, script, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}
	for i, cmd := range script {
		var want, got string
		if i < len(oracle) {
			want = oracle[i]
		}
		if i < len(emerald) {
			got = emerald[i]
		}
		if diffs := Compare(want, got); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}
