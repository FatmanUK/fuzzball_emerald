package golden

import (
	"context"
	"testing"
)

// Three ways of reading a property *list* and a property *directory*
// into an array, and five divergences between them.
//
// `ARRAY_GET_PROPVALS` counted a **valueless directory** as an entry.
// Upstream reaches `get_property` for the node and then a type switch
// with no directory case, so `goodflag` stays zero and nothing is
// added (`p_array.c:1863`) — two bare propdirs answer 0 there and
// answered 2 here.
//
// `ARRAY_GET_PROPLIST` read one spelling of the count and one of the
// items. Upstream tries the count as `dir#` and then `dir/#`, each as
// an **integer and then as a string** (`:1954`), and each item as
// `dir#/N`, then `dir/N`, then `dirN` (`:2013`) — so a hand-built
// list, or one written by older code, read as empty here. A missing
// item is integer zero rather than a gap, which keeps the list the
// length the count claimed.
//
// And **`max_propfetch` capped nothing.** It bounds all three, two of
// them with a message of their own and the proplist by silent
// truncation — which upstream's own TODO calls the wrong choice and
// asks to have made consistent.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const propListSource = `: ts[ s -- ] me @ s @ notify ;
: oops[ s -- ] me @ s @ notify ;
: n[ x -- ] x @ intostr ts ;
: sh dup string? if ts else intostr ts then ;
: probes
  ( A directory holding a valued child, a bare directory, and a
    child that is both. Only the two with values of their own are
    propvals; only the two with children are propdirs. )
  0 try me @ "_d" array_get_propvals array_count n
    catch oops endcatch
  0 try me @ "_d" array_get_propdirs array_count n
    catch oops endcatch

  ( The count under "dir#" as an integer, and the items under
    "dir#/N" -- the only pair this server read. )
  0 try me @ "_l1" array_get_proplist array_count n
    catch oops endcatch
  0 try me @ "_l1" array_get_proplist 0 [] sh
    catch oops endcatch

  ( The count as a STRING, which atoi reads. )
  0 try me @ "_l2" array_get_proplist array_count n
    catch oops endcatch
  0 try me @ "_l2" array_get_proplist 1 [] sh
    catch oops endcatch

  ( The count under "dir/#" and the items under "dir/N". )
  0 try me @ "_l3" array_get_proplist array_count n
    catch oops endcatch
  0 try me @ "_l3" array_get_proplist 0 [] sh
    catch oops endcatch

  ( And the items under "dirN", with no delimiter at all. )
  0 try me @ "_l4" array_get_proplist array_count n
    catch oops endcatch
  0 try me @ "_l4" array_get_proplist 1 [] sh
    catch oops endcatch

  ( A count that promises more than is there: the gaps are integer
    zero and the length is what the count said. )
  0 try me @ "_l5" array_get_proplist array_count n
    catch oops endcatch
  0 try me @ "_l5" array_get_proplist 1 [] sh
    catch oops endcatch
;
: capped
  ( With max_propfetch at 1: two of the three abort and the third
    truncates in silence. )
  0 try me @ "_d" array_get_propvals array_count n
    catch oops endcatch
  0 try me @ "_d" array_get_propdirs array_count n
    catch oops endcatch
  0 try me @ "_l1" array_get_proplist array_count n
    catch oops endcatch
;
: main
  "cap" stringcmp not if capped else probes then
;`

var propListScript = Script{
	"@tune penny_rate=0",

	// _d/a has a value; _d/b is a bare directory; _d/c has both.
	"@set me=_d/a:one",
	"@set me=_d/b/x:deep",
	"@set me=_d/c:three",
	"@set me=_d/c/y:deep",

	// Four spellings of one list.
	"@set me=_l1#:^2",
	"@set me=_l1#/1:one",
	"@set me=_l1#/2:two",

	"@set me=_l2#:2",
	"@set me=_l2#/1:one",
	"@set me=_l2#/2:two",

	"@set me=_l3/#:^2",
	"@set me=_l3/1:one",
	"@set me=_l3/2:two",

	"@set me=_l4#:^2",
	"@set me=_l41:one",
	"@set me=_l42:two",

	// A count of three with one item set.
	"@set me=_l5#:^3",
	"@set me=_l5#/1:one",

	"test",

	"@tune max_propfetch=1",
	"test cap",
	"@tune max_propfetch=1023",
}

// TestPropListMatchesFuzzball compares the ladder.
func TestPropListMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), propListSource)
	if err != nil {
		t.Fatal(err)
	}
	script := propListScript
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
