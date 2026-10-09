package golden

import (
	"context"
	"testing"
)

// MPI's property safety layer was ported nowhere.
// `safegetprop_strict` (`msgparse.c:165`) makes four refusals before
// it reads anything and `safeputprop` (`:98`) makes six before it
// writes; `env.getProp` went straight to `Host.GetPropStr` and
// `{store}` straight to `Host.SetPropStr`. So an unblessed
// description could read a `@__sys__` property, read a hidden one,
// read somebody else's private one, and overwrite a see-only property
// or a world's MPI macros.
//
// The two halves report differently, which is the detail worth
// pinning. A refused **read** notifies "PropFetch: ..." where the
// refusal happens and the caller then aborts with its own name and
// "Failed read.", so the player gets *two* lines. A refused **write**
// reports nothing of its own: `safeputprop` answers false and the
// caller aborts, so the player gets one.
//
// The probes run through PARSEPROP, which evaluates the property it
// is handed with `perms` set to the object carrying it
// (`do_parse_mesg`, `msgparse.c:2020`, passes `what` twice) and
// blessed only if that property is. An ordinary description is
// unblessed, which is where every one of these refusals lives.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const safePropSource = `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop
  "[" swap strcat "]" strcat me @ swap notify
;
: bshow[ str:s -- ]
  ( The same, with the property blessed -- which is what makes the
    evaluation blessed, since PARSEPROP sets MPI_ISBLESSED from
    Prop_Blessed on the property it is handed. BLESSPROP is mucker 4,
    which is why the script raises the program. )
  me @ "_/de" s @ setprop
  me @ "_/de" blessprop
  me @ "_/de" "" 0 parseprop
  "[" swap strcat "]" strcat me @ swap notify
  me @ "_/de" unblessprop
;
: main
  ( The four read refusals, through the strict form so the walk
    cannot find a readable answer further out. A system property is
    refused for everybody, blessed or not. )
  "{prop!:@__sys__/x}" show

  ( Hidden and see-only are the unblessed refusals. Only hidden is
    on the read side; see-only is a write rule, so reading one is
    allowed -- which is the asymmetry the two lists exist for. )
  "{prop!:@hid}" show
  "{prop!:~see}" show

  ( An empty name is "Propname required." rather than a permission
    failure, and a lone slash reaches it: the leading delimiters are
    trimmed first. )
  "{prop!:/}" show

  ( A private property is readable only when the object carrying the
    message and the object being read have the same owner -- not
    when the reader does. PARSEPROP passes one object as both, so the
    only way to make the two owners differ without a second argument
    is to let the WALK reach somebody else's object: the vault is
    Bob's and is this room's parent. #1 is a wizard and it makes no
    difference, because safegetprop_strict has no wizard escape at
    all.

    The second-argument route is the other half of this rule and is
    not reachable yet: upstream's mesg_dbref refuses Bob's gem
    outright, before the property is looked at, which is one line
    rather than two and belongs with the three resolution wrappers. )
  "{prop:.priv}" show

  ( The walking form finds a property further out; the strict form
    does not. )
  "{prop:_walkme}" show
  "{prop!:_walkme}" show

  ( And a refusal stops the walk rather than being stepped over,
    which is upstream's "if -not ptr or *ptr- return ptr". )
  "{prop:@hid}" show

  ( Now the write side. Six refusals, one line each. )
  "{store:v,@__sys__/x}" show
  "{store:v,@hid}" show
  "{store:v,~see}" show
  "{store:v,_msgmacs/evil}" show

  ( is_valid_propname refuses a ':' in a name, which is the
    character @set uses to separate a name from a value. )
  "{store:v,a:b}" show
  "{store:v,/}" show

  ( Private is refused on the read side and allowed on the write
    side: a private property is somebody's to read and anybody's to
    overwrite. )
  "{store:v,.priv}" show

  ( The one that works, read back, and removed. {store} answers with
    what it wrote -- this server answered empty. )
  "{store:written,_ok}" show
  "{prop!:_ok}" show
  "{delprop:~see}" show
  "{delprop:_ok}" show
  "{prop!:_ok}" show

  ( Blessed, the unblessed refusals lift and the system one does
    not -- which is the only way to tell the two apart, since an
    "@__sys__" path is hidden as well and both answer with the same
    message. )
  "{prop!:@__sys__/x}" bshow
  "{prop!:@hid}" bshow
  "{store:v,@__sys__/x}" bshow
  "{store:v,~see}" bshow

  ( The macro lookup's middle step is a WALK, not a read:
    safegetprop_limited follows the environment chain accepting a
    value only off an object with the same owner as the trigger. This
    server did three flat reads, so a macro directory on the room
    something is standing in was never found. )
  "{mac}" show
;`

// safePropScript makes Bob a second owner so a private property can
// belong to somebody else, puts the macro on the room rather than on
// the player, and leaves the program as #1's so `perms` stays the
// wizard.
var safePropScript = Script{
	"@pcreate Bob=secret", // #4

	// Bob owns the vault and the player stands in a room of #1's
	// *inside* it, so the environment walk from the player passes
	// an object #1 owns and then one Bob does -- which is what
	// Prop_Private's read clause compares, and the only way to
	// make the two owners differ without a second argument.
	//
	// @teleport on a room is a reparent, and its destination must
	// be *linkable*: ABODE on a room (`db.h:576`), not LINK_OK.
	// The fixture's player starts in #0, which cannot be moved at
	// all, so the pair has to be dug and the player walked into
	// the inner one.
	"@dig Vault", // #5
	"@set Vault=abode",
	"@chown Vault=Bob",
	"@set Vault=.priv:bob's secret",
	"@dig Inner", // #6
	"@teleport Inner=Vault",
	// By ref: @teleport's destination match drops match_neighbor,
	// and a dug room is detached, so nothing but a dbref or a
	// registration can name it.
	"@teleport me=#6",

	// The readable one further out, for the walk.
	"@set here=_walkme:found on the room",

	// The macro, on the room: the walk has to reach it.
	"@set here=_msgmacs/mac:I am the macro",

	// The properties the refusals name, so a refusal is a refusal
	// rather than a miss.
	"@set me=@hid:hidden value",
	"@set me=~see:see-only value",
	"@set here=@hid:hidden on the room",

	// BLESSPROP is mucker 4, and that takes both lines on the
	// program -- the Wizard bit plus any mucker bit is level 4
	// outright and neither alone is.
	"@set test.muf=wizard",
	"@set test.muf=3",

	"test",

	// What landed and what did not.
	"examine me=_**",
	"examine me=~**",
}

// TestSafePropMatchesFuzzball compares the ladder.
func TestSafePropMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), safePropSource)
	if err != nil {
		t.Fatal(err)
	}
	script := safePropScript
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
