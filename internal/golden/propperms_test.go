package golden

import (
	"context"
	"testing"
)

// `prop_read_perms` (`p_props.c:87`) and `prop_write_perms` (`:129`)
// are called at **31 sites** across `p_props.c` and `p_array.c` and
// were ported nowhere, so every sigil the property system has was
// unenforced in MUF: a mucker-1 program could read a hidden property,
// write a read-only one, or write under `@__sys__`.
//
// The two are not one test with a flag. Reading weighs **two**
// sigils, writing weighs **five** — and writing's `tp_gender_prop`
// clause is a whole-name match where every other test is per path
// segment, while its `_msgmacs` clause sits *outside* the ownership
// test, so a macro takes mucker 3 even on your own object.
//
// Three rungs separate the thresholds: at mucker 1 both `< 3` and `<
// 4` bite, at 3 only `< 4` does, at 4 neither. A mucker-2 rung would
// add nothing, since nothing here keys on 2.
//
// Two objects, because the `< 3` clauses are `!permissions(ProgUID,
// obj)` and `ProgUID` is `#1`: its own widget passes, Bob's pebble
// does not.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const propPermsSource = `: ts[ s -- ] me @ s @ notify ;
: probe[ o p -- ]
  0 try o @ p @ getpropstr pop "  str ok" ts
    catch "  str: " swap strcat ts endcatch
  0 try o @ p @ getpropval pop "  val ok" ts
    catch "  val: " swap strcat ts endcatch
  0 try o @ p @ 5 setprop "  set ok" ts
    catch "  set: " swap strcat ts endcatch
  0 try o @ p @ remove_prop "  rm ok" ts
    catch "  rm: " swap strcat ts endcatch
;
: sweep[ o -- ]
  o @ intostr "--- object " swap strcat ts
  "plain" ts        o @ "plain" probe
  "_ro" ts          o @ "_ro" probe
  "%ro2" ts         o @ "%ro2" probe
  ".priv" ts        o @ ".priv" probe
  "@hid" ts         o @ "@hid" probe
  "~see" ts         o @ "~see" probe
  "_msgmacs/m" ts   o @ "_msgmacs/m" probe
  "@__sys__/s" ts   o @ "@__sys__/s" probe
  "sex" ts          o @ "sex" probe
;
: main
  #4 sweep
  #5 sweep
;`

// propPermsScript sets each sigil on both objects, chowns one to Bob
// so ProgUID does not own it, then runs the sweep at three levels. #2
// is test.muf and #3 its exit, so the widget is #4.
var propPermsScript = Script{
	"@create widget", // #4, ProgUID's own
	"@create pebble", // #5, to become Bob's
	"@pcreate Bob=pw",

	// Set them while #1 is a wizard and before the chown, since
	// @set's own propRestricted refuses a *hidden* property to
	// anyone who is not one. "@__sys__" is refused to everybody,
	// so that one stays unset — the refusal is what is under
	// test, not the value.
	"@set widget=plain:p",
	"@set widget=_ro:p",
	"@set widget=%ro2:p",
	"@set widget=.priv:p",
	"@set widget=@hid:p",
	"@set widget=~see:p",
	"@set widget=_msgmacs/m:p",
	"@set widget=sex:p",
	"@set pebble=plain:p",
	"@set pebble=_ro:p",
	"@set pebble=%ro2:p",
	"@set pebble=.priv:p",
	"@set pebble=@hid:p",
	"@set pebble=~see:p",
	"@set pebble=_msgmacs/m:p",
	"@set pebble=sex:p",
	"@chown pebble=Bob",

	// Mucker 1: both thresholds bite.
	"@set test.muf=1",
	"test",

	// Mucker 3: only "< 4" does.
	"@set test.muf=3",
	"test",

	// Mucker 4 lifts both — the Wizard bit plus any mucker bit,
	// since neither alone is level 4.
	"@set test.muf=wizard",
	"@set test.muf=3",
	"test",
}

// TestPropPermsMatchesFuzzball compares the sweep at three levels.
func TestPropPermsMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), propPermsSource)
	if err != nil {
		t.Fatal(err)
	}
	script := propPermsScript
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
