package golden

import (
	"context"
	"testing"
	"time"
)

// registerScript covers @register and @propset, the two commands that
// write a property the player names rather than one a verb implies.
//
// Nothing in Emerald wrote `_reg/` except the MUF editor's own `q`,
// so $include and the "$name" matcher read a propdir only one thing
// filled in. @propset was absent outright.
var registerScript = Script{
	"@create widget",

	// The default form: on #0, propdir _reg. The reply names the
	// path, the object and where it landed.
	"@register widget=wid",
	"@register widget=wid",
	"@register #0",

	// A registered name resolves for anything that takes one.
	"look $wid",
	"@describe $wid=A registered widget.",
	"look widget",

	// With no object, the entry is removed — and removing one
	// that is not there says so.
	"@register =wid",
	"@register =wid",
	"@register #0",

	// #me puts it on the caller instead.
	"@register #me widget=mine",
	"@register #me",
	"@register #me =mine",

	// #prop names a propdir, with or without a target.
	"@register #prop :_stash widget=w",
	"@register #prop :_stash",
	"@register #prop me:_stash widget=w2",
	"@register #prop me:_stash",
	"@register #prop widget=oops",

	// An invalid registry name, and a name with a colon in it.
	"@register widget=",
	"@register widget=a:b",

	// @propset's six types. The type may be abbreviated to any
	// prefix, and an empty type is a string.
	"@propset widget=:plain:some text",
	"@propset widget=string:str:more text",
	"@propset widget=i:num:42",
	"@propset widget=integer:num2:7",
	"@propset widget=f:fl:1.5",
	"@propset widget=d:dbref:me",
	"@propset widget=lock:lok:me",
	"examine widget=/",

	// The refusals.
	"@propset widget=i:num:notanumber",
	"@propset widget=f:fl:notafloat",
	"@propset widget=d:dbref:nosuchthing",
	"@propset widget=lock:lok:nosuchthing",
	"@propset widget=nonsense:x:y",
	"@propset widget=:",
	"@propset widget=erase:num:leftover",

	// Erasing, and the leading-slash equivalence.
	"@propset widget=erase:num",
	"@propset widget=:/slashed:value",
	"@propset widget=:slashed",
	"examine widget=/",

	// A system property is out of bounds even to a wizard.
	"@propset widget=:@__sys__/x:y",

	// The abbreviations. @pro is ambiguous between @program,
	// @propset and @program's siblings, so each needs enough
	// letters.
	"@prop widget=:a:b",
	"@reg widget=r",
	"@register #0",
}

// TestRegisterMatchesFuzzball checks @register and @propset against
// the C server.
func TestRegisterMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, registerScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, registerScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range registerScript {
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
