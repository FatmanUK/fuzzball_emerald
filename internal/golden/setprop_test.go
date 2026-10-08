package golden

import (
	"context"
	"testing"
)

// `@set`'s property form is most of a second command inside `@set`
// (`set.c:763-842`) and this server had almost none of its rules.
// Reading the MUCK Manual's looktrap examples is what surfaced it:
// they use `@set here=_details/sign;plaque:...` rather than
// `@propset`, which is the form nobody here had checked.
//
// Six of the seven divergences are comparable. The two that are not
// both need somebody who is neither a wizard nor God, which the
// oracle's player cannot be, and are unit-tested instead:
// `propRestricted`'s hidden/see-only half, and the `strict_god_priv`
// guard.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const setPropSource = `: t[ x -- ] me @ x @ intostr notify ;
: ts[ s -- ] me @ s @ notify ;
: main
  ( "^42" is the only way to make an integer property from the
    command line, and look traps care: a non-string detail does not
    run. getpropval reads 42 from an integer and 0 from a string,
    so this tells the two apart. )
  #0 "_num" getpropval t
  #0 "_num" getpropstr "[" swap strcat "]" strcat ts

  ( A string property for contrast, where getpropval reads 0. )
  #0 "_str" getpropval t
  #0 "_str" getpropstr ts

  ( The trailing-'/' trim, and the order of the two trims is the
    surprise. Upstream right-trims whitespace *first* and then
    strips trailing '/' -- so for a name ending "b  /" the
    whitespace loop sees the '/' and stops at once, the '/' alone
    comes off, and the two spaces SURVIVE. The property really is
    called "_test/b  " and "_test/b" does not exist. )
  #0 "_test/b" getpropstr "[" swap strcat "]" strcat ts
  #0 "_test/b  " getpropstr "[" swap strcat "]" strcat ts

  ( And the plain case, where the whitespace does come off because
    there is no '/' after it to stop the loop. )
  #0 "_plain" getpropstr "[" swap strcat "]" strcat ts
;`

var setPropScript = Script{
	// do_set has no usage message at all: it matches, checks
	// God's property, and an empty flag arrives at one message. A
	// missing '=' and an empty value give the same answer.
	"@set me",
	"@set me=",
	"@set me=   ",

	// A bare colon is ":clear" or nothing, and the refusal quotes
	// the syntax rather than describing it.
	"@set me=:",
	"@set me=:clearx",
	"@set me=:please clear",

	// A system property is out of bounds to *everybody*, wizard
	// included, so this one is comparable where the hidden and
	// see-only halves are not.
	"@set me=@__sys__/x:y",
	"@set me=@__sys__:y",

	// Setting, reading back, and removing. An empty value removes
	// and says so -- this server used to say "Property cleared."
	"@set here=_str:a string",
	"@set here=_num:^42",

	// A negative integer, and a caret that is not a number, which
	// falls through to being a plain string.
	"@set here=_neg:^-7",
	"@set here=_notnum:^abc",

	// The two trims and their order. "b /" keeps its spaces
	// because the whitespace loop stops at the '/'; "_plain "
	// loses them because nothing stops it.
	"@set here=_test/b  /:trimmed",
	"@set here=_plain   :spaces gone",

	// What the program reads back.
	"test",

	// examine's property listing, which shows what was written
	// where.
	"examine here=_**",

	// Removal, and its wording.
	"@set here=_str:",
	"examine here=_**",

	// And ":clear", last because for a wizard it removes
	// everything -- including the room's description, which the
	// look afterwards shows the absence of.
	"@set here=:clear",
	"examine here=_**",
	"look",
}

// TestSetPropMatchesFuzzball compares the ladder.
func TestSetPropMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), setPropSource)
	if err != nil {
		t.Fatal(err)
	}
	script := setPropScript
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
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}
