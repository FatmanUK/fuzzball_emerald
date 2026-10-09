package golden

import (
	"context"
	"testing"
)

// The two property primitives whose shape is not a single gate.
//
// **NEXTPROP skips rather than refuses.** `prim_nextprop`
// (`p_props.c:1100`) loops `while (pname && !prop_read_perms(...))`,
// advancing past every name the program may not read — so a hidden
// property is *invisible* to a walk rather than stopping it, and the
// walk ends with the empty string. It is also the one property
// primitive with no `CHECKREMOTE` at all.
//
// **ENVPROP tests where the walk landed**, not where it started
// (`:573`). The search runs first; the read test is then made against
// the object the property was found on. So a property a program may
// not read on a *parent* room refuses the lookup even though the
// starting object was fair game.
//
// Both need a mucker level below 4 to bite, and the walk needs two
// rooms deep to be worth anything.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const envPropSource = `: ts[ s -- ] me @ s @ notify ;
: walk[ o -- ]
  ( Every name under the object's root, in order, as NEXTPROP sees
    them. A name the program may not read is stepped over, so the
    hidden ones simply do not appear. )
  "" var! n
  begin
    o @ n @ nextprop n !
    n @ "" strcmp while
    "  " n @ strcat ts
  repeat
;
: main
  "--- names #1 can walk on the room:" ts
  #0 walk

  ( ENVPROP from the widget, which is in the room: the walk leaves
    the widget and finds the property on #0, so the read test is
    made against #0 and not against the widget. )
  "--- envprop from the widget:" ts
  0 try #4 "@aparentonly" envprop pop pop "  @aparentonly ok" ts
    catch "  @aparentonly: " swap strcat ts endcatch
  0 try #4 "plainonly" envprop pop pop "  plainonly ok" ts
    catch "  plainonly: " swap strcat ts endcatch

  ( A hidden name that exists *nowhere*. The walk finds nothing, so
    "what" is NOTHING and upstream makes no read test at all -- the
    guard is on the landing, not unconditional. Without it this
    would refuse rather than answer NOTHING and 0. )
  0 try #4 "@nosuchprop" envprop pop pop "  @nosuchprop ok" ts
    catch "  @nosuchprop: " swap strcat ts endcatch
;`

// envPropScript puts a hidden and a plain property on the room, and
// nothing on the widget, so every ENVPROP lookup has to walk.
var envPropScript = Script{
	"@create widget", // #4
	"drop widget",
	"@set here=plainonly:p",
	"@set here=zlast:p",

	// **Two** hidden names, and adjacent in sort order, so the
	// skip has to loop rather than step once. With only one, a
	// single `if` is indistinguishable from the `while`.
	"@set here=@aparentonly:p",
	"@set here=@bparentonly:p",

	// Mucker 3, which the fixture's program already is: the
	// hidden name is skipped by the walk and refuses the ENVPROP,
	// because both tests key on "< 4".
	//
	// Mucker *1* would show neither, because NEXTPROP has a floor
	// of 3 (`mlev_gen.go`) and is refused outright before the
	// skip loop can run — which is what the first draft of this
	// case did, aborting the program on its first iteration.
	"test",

	// Mucker 4: both appear and both resolve.
	"@set test.muf=wizard",
	"@set test.muf=3",
	"test",
}

// TestEnvPropMatchesFuzzball compares both at two levels.
func TestEnvPropMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), envPropSource)
	if err != nil {
		t.Fatal(err)
	}
	script := envPropScript
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
