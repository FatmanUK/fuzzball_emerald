package golden

import (
	"context"
	"testing"
)

// The `ARRAY_*` property family, where the read/write asymmetry is
// the finding: **`p_array.c` skips where `p_props.c` aborts.**
//
// A read over a propdir leaves out what the program may not see and
// returns a shorter answer — ARRAY_GET_PROPVALS,
// ARRAY_GET_PROPDIRS, ARRAY_GET_PROPLIST and ARRAY_FILTER_PROP all
// wrap the append in the test. A *write* aborts on the first key it
// may not set, and the `PUT_PROP*` pair aborts with its own longer
// sentence, "Permission denied while trying to set protected
// property." (`p_array.c:2182`, `:2356`), where every other property
// primitive says just "Permission denied."
//
// The single-name reflist pair is the exception in the other
// direction: ARRAY_GET_REFLIST and ARRAY_PUT_REFLIST test one name
// and abort with the plain message.
//
// **Every probe is wrapped in a catch**, which the mucker-1 rung
// needs: several of these primitives have a floor of 3, so an
// unguarded run ends on its first line and nothing after it is
// reached — including the one probe that rung exists for.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const arrayPropsSource = `: ts[ s -- ] me @ s @ notify ;
: n[ x -- ] x @ intostr ts ;
: main
  ( A dir holding one plain child and one hidden one. At mucker 3
    the hidden one is skipped, so the count is one; at 4 it is two. )
  "propvals count:" ts
  0 try #4 "d" array_get_propvals array_count n
    catch "  " swap strcat ts endcatch
  "propdirs count:" ts
  0 try #4 "d" array_get_propdirs array_count n
    catch "  " swap strcat ts endcatch

  ( A proplist whose second element is readable and whose dir is
    not. The list comes back shorter rather than refusing. )
  "proplist count:" ts
  0 try #4 "@hidlist" array_get_proplist array_count n
    catch "  " swap strcat ts endcatch
  "plainlist count:" ts
  0 try #4 "plainlist" array_get_proplist array_count n
    catch "  " swap strcat ts endcatch

  ( ARRAY_FILTER_PROP tests per object inside its loop and skips
    one whose property it may not read, so the filter silently
    returns fewer objects. )
  "filter on a hidden prop:" ts
  0 try { #4 }list "d/@hid" "p" array_filter_prop array_count n
    catch "  " swap strcat ts endcatch
  "filter on a plain prop:" ts
  0 try { #4 }list "d/plain" "p" array_filter_prop array_count n
    catch "  " swap strcat ts endcatch

  ( A reflist refuses outright, with the plain message. )
  "hidden reflist:" ts
  0 try #4 "@hidrefs" array_get_reflist array_count n
    catch "  " swap strcat ts endcatch

  ( And a write refuses with the *longer* message. )
  "put_proplist onto a hidden dir:" ts
  0 try #4 "@hidlist" { #1 }list array_put_proplist "  ok" ts
    catch "  " swap strcat ts endcatch
  "put_propvals onto a hidden key:" ts
  0 try #4 "d" { "@k" #1 }dict array_put_propvals "  ok" ts
    catch "  " swap strcat ts endcatch
  "put_reflist onto a hidden dir:" ts
  0 try #4 "@hidrefs" { #1 }list array_put_reflist "  ok" ts
    catch "  " swap strcat ts endcatch

  ( ARRAY_PUT_PROPLIST tests the **count** property as well as every
    element, and the only way to tell the two apart is a rule that
    matches a whole name rather than a path segment: gender_prop,
    which is tuned to "gl#" here. So "gl#" is refused and "gl#/1"
    is not, and without the count test the write would go through.
    It needs an object ProgUID does not own, since that clause sits
    inside the ownership test. )
  "put_proplist where only the count is protected:" ts
  0 try #5 "gl" { #1 }list array_put_proplist "  ok" ts
    catch "  " swap strcat ts endcatch
;`

// arrayPropsScript puts a plain and a hidden child under one dir,
// plus two proplists and a reflist. #2 is test.muf, #3 its exit.
var arrayPropsScript = Script{
	"@create widget", // #4

	// Two children of "d", one of them hidden. Each needs a
	// **value of its own** as well as a child: upstream's
	// propvals adds only value-bearing children, so two bare
	// directories counted for nothing and the probe compared 0
	// against 0.
	"@set widget=d/plain:p",
	"@set widget=d/plain/leaf:p",
	"@set widget=d/@hid:p",
	"@set widget=d/@hid/leaf:p",

	// A proplist under a hidden dir, and one under a plain dir.
	"@set widget=@hidlist#:^2",
	"@set widget=@hidlist#/1:a",
	"@set widget=@hidlist#/2:b",
	"@set widget=plainlist#:^2",
	"@set widget=plainlist#/1:a",
	"@set widget=plainlist#/2:b",
	"@set widget=@hidrefs:#1",

	// A second object, Bob's, for the gender_prop probe: that
	// clause is inside prop_write_perms's ownership test, so it
	// cannot fire on anything ProgUID owns.
	"@create gem", // #5
	"@pcreate Bob=pw",
	"@chown gem=Bob",
	"@tune gender_prop=gl#",

	// Mucker 1 first, because the gender_prop clause sits inside
	// prop_write_perms's "mlev < 3" block and so cannot fire at 3
	// or 4. Several of the reads have a floor of 3 and abort
	// outright here, which both servers do alike.
	"@set test.muf=1",
	"test",

	// Mucker 3, the fixture's own level: the hidden names are
	// invisible to the reads and refuse the writes.
	"@set test.muf=3",
	"test",

	// Mucker 4 lifts both.
	"@set test.muf=wizard",
	"@set test.muf=3",
	"test",
}

// TestArrayPropsMatchesFuzzball compares the family at two levels.
func TestArrayPropsMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), arrayPropsSource)
	if err != nil {
		t.Fatal(err)
	}
	script := arrayPropsScript
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
		if diffs := Compare(maskVariable(want),
			maskVariable(got)); len(diffs) > 0 {
			t.Errorf("step %d, %q\n%s", i, cmd,
				Render(diffs))
		}
	}
}
