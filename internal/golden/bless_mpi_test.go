package golden

import (
	"context"
	"testing"
)

// `safegetprop_strict`'s **blessed out parameter**
// (`msgparse.c:239`), the last of the eleven function-fidelity gaps
// the MPI resolver sweep recorded. Nothing read a property's own
// blessing back, so `{exec}` and `{eval}` ran their text as the
// *outer* message's blessing — looser in one direction (a blessed
// property's text ran unblessed) and stricter in the other (an
// unblessed property's text ran **blessed** inside a blessed message,
// which is the one that matters).
//
// A **hidden** property is the probe, because that is the clearest
// thing the blessing buys: `safegetprop_strict` refuses a
// `Prop_Hidden` path for an unblessed message and allows it for a
// blessed one. So whether the inner text ran blessed is visible as
// either the value or a pair of refusal lines.

var blessMpiScript = Script{
	"@tune penny_rate=0",
	"@create widget",
	"drop widget",

	// A hidden property, and an unblessed one that tries to read
	// it: refused, with "PropFetch:" in front of the caller's own
	// "Failed read."
	"@set widget=@secret:found it",
	"@set widget=_inner:[{prop:@secret}]",
	"@describe widget={exec:_inner}",
	"look widget",

	// Blessed, and the same text answers.
	"@bless widget=_inner",
	"look widget",

	// Now the stricter half. The **description** is blessed and
	// the inner property is not, so the inner text must run
	// unblessed -- which it did not before, because the outer
	// blessing was simply inherited.
	"@unbless widget=_inner",
	"@bless widget=_/de",
	"look widget",

	// ...and with both blessed it answers again, so the rung
	// above is a refusal and not a broken read.
	"@bless widget=_inner",
	"look widget",
	"@unbless widget=_/de",

	// {eval} takes its text as an argument rather than reading a
	// property, so there is no property whose blessing could be
	// swapped in: an unblessed message stays unblessed however
	// the text got there.
	"@unbless widget=_inner",
	"@describe widget={eval:[{prop:@secret}]}",
	"look widget",
	"@bless widget=_/de",
	"look widget",
	"@unbless widget=_/de",

	// And the other thing {exec} does with the property it read:
	// `mesg_parse(descr, player, obj, trg, ...)` re-bases
	// **what** on the object the property came off, so "this"
	// inside the text names that object rather than whatever was
	// being described -- whichever sigil the name carries.
	"@create other",
	"drop other",
	"@set other=_who:[{name:this}]",
	"@set other=plain:[{name:this}]",
	"@describe widget={exec:_who,other}",
	"look widget",
	"@describe widget={exec:plain,other}",
	"look widget",

	// `perms` is re-based too, and differently: on the outer
	// *what* for an ordinary name and on the object for a
	// privileged one. A private property reads only when the
	// permissions object shares its owner, so an object owned by
	// somebody else is what tells the two apart -- and the
	// description has to be **blessed** to reach that object at
	// all, since the resolver would otherwise refuse it first.
	"@pcreate Bob=secret",
	"@set other=.private:deep",
	"@set other=plain:[{prop:.private,other}]",
	"@set other=_priv:[{prop:.private,other}]",
	"@chown other=Bob",
	// `@describe` writes a fresh value, so the blessing has to
	// come **after** it: blessing first and describing second
	// leaves the description unblessed, which is the shape of
	// `db_putprop` replacing the node rather than editing it.
	"@describe widget={exec:_priv,other}",
	"@bless widget=_/de",
	"look widget",
	"@describe widget={exec:plain,other}",
	"@bless widget=_/de",
	"look widget",

	// ...and that the ordinary name's perms is the outer **what**
	// rather than the outer perms takes two levels to see,
	// because a description's what and perms are the same object.
	// So: widget's description execs off Bob's `other`, whose
	// text execs off #1's `third`, whose text reads a private
	// property of third's. That read passes only if the
	// permissions object shares third's owner -- which it would
	// if perms came from the outer perms (widget, #1's) and does
	// not, because it comes from the outer what (other, Bob's).
	"@create third",
	"drop third",
	"@set third=.secret:deeper",
	"@set third=plain2:[{prop:.secret,third}]",
	"@set other=plain:[{exec:plain2,third}]",
	"@describe widget={exec:plain,other}",
	"@bless widget=_/de",
	"look widget",
	"@unbless widget=_/de",
}

// TestBlessedPropagationMatchesFuzzball compares the ladder.
func TestBlessedPropagationMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}
	script := blessMpiScript
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
