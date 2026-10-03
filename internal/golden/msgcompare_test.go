package golden

import (
	"context"
	"testing"
)

// All eight of MPI's comparisons go through msg_compare
// (mfuns.c:1810), which is not arithmetic: two numbers compare as
// numbers, and **anything else compares as text, case-insensitively**
// — it is strcasecmp. Both arguments must be non-empty for the
// numeric path.
//
// Emerald had three different wrong answers here. {gt}, {lt}, {ge}
// and {le} read both sides with atoi, so a non-numeric argument
// silently became 0. {max} and {min} did the same and then returned a
// *number*, where upstream returns the chosen argument's text, and
// folded over every argument where upstream takes exactly two. {eq}
// and {ne} had the right shape but the wrong details: a TrimSpace
// made " 7 " numeric where number() rejects it, and the text fallback
// was case-sensitive.
//
// number() (fbstrings.c) is stricter than atoi: leading whitespace,
// an optional sign, then digits and nothing else. So "12abc" is not a
// number here even though {add} reads 12 from it.
const msgCompareSource = `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop
  var! out me @ "[" out @ strcat "]" strcat notify
;
: main
  ( --- {max} and {min} return the argument text --------------- )
  "{max:abc,2}" show
  "{min:abc,2}" show
  "{max:2,abc}" show
  "{min:2,abc}" show
  "{max:5,10}" show
  "{min:5,10}" show
  "{max:apple,banana}" show
  "{min:apple,banana}" show
  ( case-insensitive, so these are equal and the first wins )
  "{max:ABC,abc}" show
  "{min:ABC,abc}" show

  ( --- the ordering tests ------------------------------------- )
  "{gt:abc,1}" show
  "{lt:abc,1}" show
  "{ge:abc,0}" show
  "{le:abc,0}" show
  "{gt:10,5}" show
  "{lt:10,5}" show
  "{gt:apple,banana}" show
  "{lt:apple,banana}" show
  "{ge:ABC,abc}" show
  "{le:ABC,abc}" show

  ( a partly-numeric argument is not a number, so these compare
    as text even though atoi would read a value from them )
  "{gt:12abc,3}" show
  "{lt:12abc,3}" show
  "{max:12abc,3}" show

  ( --- an empty argument always compares as text -------------- )
  "{eq:,0}" show
  "{ne:,0}" show
  "{gt:,0}" show
  "{lt:,0}" show
  "{max:,0}" show

  ( --- {eq} and {ne}, which keep whitespace: EQ and NE are the
        two comparisons the table does not Strip ---------------- )
  "{eq:ABC,abc}" show
  "{ne:ABC,abc}" show
  "{eq:5,5}" show
  "{eq:05,5}" show
  "{eq:apple,apple}" show
;`

// TestMPIMsgCompareMatchesFuzzball drives all eight through both
// servers.
func TestMPIMsgCompareMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), msgCompareSource)
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
