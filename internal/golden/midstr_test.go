package golden

import (
	"context"
	"testing"
)

// {midstr}'s second number is a **position**, not a length.
// mfn_midstr (mfuns2.c:2897) clamps both arguments as 1-based
// positions, lets a negative one count from the end by adding len +
// 1, and walks **backwards** when the second is lower than the first
// — returning the span reversed.
//
// Emerald read the second as a length, so every three-argument call
// was wrong, and neither the negative form nor the reversal existed.
//
// The clamping order is what the zero and out-of-range cases test: a
// position of zero returns the empty string before any clamping, and
// only then is a position above the length pulled down, a negative
// one wrapped, and anything still below one raised to 1.
const midstrSource = `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop
  var! out me @ "[" out @ strcat "]" strcat notify
;
: main
  ( --- two positions, forwards ------------------------------- )
  "{midstr:hello,2,4}" show
  "{midstr:hello,1,5}" show
  "{midstr:hello,1,1}" show
  "{midstr:hello,3,3}" show
  "{midstr:hello,2}" show

  ( --- backwards, which reverses ----------------------------- )
  "{midstr:hello,4,2}" show
  "{midstr:hello,5,1}" show
  "{midstr:hello,3,1}" show

  ( --- negative positions count from the end ----------------- )
  "{midstr:hello,-1,-1}" show
  "{midstr:hello,-2,-1}" show
  "{midstr:hello,-5,-1}" show
  "{midstr:hello,-1,-5}" show
  "{midstr:hello,1,-1}" show
  "{midstr:hello,-1,1}" show
  "{midstr:hello,-99,2}" show

  ( --- zero returns empty before anything else happens ------- )
  "{midstr:hello,0}" show
  "{midstr:hello,0,3}" show
  "{midstr:hello,3,0}" show

  ( --- past the end is pulled down to the length ------------- )
  "{midstr:hello,99}" show
  "{midstr:hello,2,99}" show
  "{midstr:hello,99,2}" show
  "{midstr:hello,99,99}" show

  ( --- an empty subject -------------------------------------- )
  "{midstr:,1}" show
  "{midstr:,1,2}" show
  "{midstr:,0}" show
  "{midstr:,-1}" show

  ( --- a non-numeric position reads as zero, so empty -------- )
  "{midstr:hello,abc}" show
  "{midstr:hello,2,abc}" show
  "{midstr:hello,abc,3}" show

  ( --- one character ----------------------------------------- )
  "{midstr:x,1,1}" show
  "{midstr:x,-1,-1}" show
  "{midstr:x,2,2}" show
;`

// TestMPIMidstrMatchesFuzzball drives the lot through both servers.
func TestMPIMidstrMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), midstrSource)
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
