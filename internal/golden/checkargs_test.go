package golden

import (
	"context"
	"testing"
)

// CHECKARGS was the last stub in the language, and the only one left
// after NEWPROGRAM. prim_checkargs (p_stack.c:1143) is about 350
// lines of C and much the largest single primitive, so it gets its
// own fixture and an exhaustive table rather than a case in the
// shared list.
//
// It is golden-reachable, which the plan did not expect it to be. The
// route is TRY/CATCH: do_abort_interp (interp.c:2844) puts the
// message in fr->errorstr when a try frame is open instead of ending
// the program, so one program can put a hundred signatures through
// the primitive and print what each one said. That makes the compiled
// C the arbiter for every message, which beats both reading it and
// standing up a separate harness around the function.
//
// ck reports the signature beside the answer, so a diff names the
// input rather than a line number; d prints the depth between
// sections, so a bookkeeping slip in the fixture shows up as a diff
// instead of silently shifting every offset after it.
const checkargsSource = `: ck[ s -- ]
  0 try s @ checkargs "ok" catch endcatch var! r
  me @ s @ " -> " strcat r @ strcat notify ;

: d depth intostr var! n me @ "depth=" n @ strcat notify ;

( every one of the twelve dbref characters against one ref, each
  pushed and popped so the failing position is always the top )
: allrefs[ x -- ]
  me @ "-- " x @ intostr strcat notify
  x @ "d" ck pop   x @ "D" ck pop
  x @ "p" ck pop   x @ "P" ck pop
  x @ "r" ck pop   x @ "R" ck pop
  x @ "t" ck pop   x @ "T" ck pop
  x @ "e" ck pop   x @ "E" ck pop
  x @ "f" ck pop   x @ "F" ck pop ;

: main
  ( a program starts with the command's argument on the stack;
    drop it so the underflow cases start from nothing at all )
  pop
  d

  ( --- the signature itself, with an empty stack --------------- )

  ( the empty string is a NULL shared_string upstream, which it
    reads as "no arguments expected" and returns on before the
    loop -- so it checks nothing and cannot fail )
  "" ck

  ( unknown characters. The default case does not test for stack
    underflow first, so these report the character even with
    nothing on the stack. )
  "q" ck
  "Z" ck
  "I" ck
  "N" ck
  "A" ck
  "L" ck
  "V" ck
  "X" ck
  "*" ck
  "-" ck

  ( multipliers )
  "0" ck
  "00" ck
  "1024" ck
  "99999" ck
  "99999999999999999999" ck
  "1023" ck
  "1" ck

  ( braces )
  "{" ck
  "{{" ck
  "}" ck
  "{3" ck

  ( a space describes no argument at all, so it needs no stack
    item and an all-space signature succeeds on an empty stack )
  " " ck
  "  " ck
  "   " ck

  ( and the underflow itself, which names the top with no
    offset at depth zero, because the suffix is relative )
  "i" ck
  "ii" ck
  "?" ck
  "s" ck
  d

  ( --- one integer -------------------------------------------- )
  5
  d
  "i" ck
  "n" ck
  "s" ck
  "S" ck
  "?" ck
  "l" ck
  "v" ck
  "a" ck
  "x" ck
  "y" ck
  "Y" ck
  "d" ck
  "i " ck
  " i" ck
  " i " ck
  "i1" ck
  "i2" ck
  "i10" ck
  pop
  d

  ( --- two integers, which is where the offsets show ---------- )
  5 6
  d
  "ii" ck
  "is" ck
  "si" ck
  "ss" ck
  "i2" ck
  "i3" ck
  "sss" ck
  "iss" ck
  "ssi" ck
  "i i" ck
  "i  i" ck
  pop pop
  d

  ( --- a float ------------------------------------------------ )
  1.5
  "n" ck
  "i" ck
  "y" ck
  "?" ck
  pop

  ( --- strings, and the null one ------------------------------ )
  "abc"
  "s" ck
  "S" ck
  "i" ck
  pop
  ""
  "s" ck
  "S" ck
  pop

  ( --- a lock, a variable, an address, the two array kinds ---- )
  "me" parselock
  "l" ck
  "i" ck
  "?" ck
  pop

  'main
  "a" ck
  "i" ck
  pop

  { 1 2 }list
  "y" ck
  "Y" ck
  "x" ck
  "i" ck
  pop

  { }dict
  "y" ck
  "Y" ck
  "x" ck
  pop
  d

  ( --- { } groups, whose repeat count comes off the stack ----- )
  5 6 2
  "{i}" ck
  pop pop pop
  5 6 7 3
  "{i}" ck
  pop pop pop pop
  5 0
  "{i}" ck
  pop pop
  5 6 1
  "{i}" ck
  pop pop pop

  ( a group that asks for more than is there )
  5 3
  "{i}" ck
  pop pop

  ( a negative count, and a non-integer one )
  5 -1
  "{i}" ck
  pop pop
  5 "x"
  "{i}" ck
  pop pop

  ( the zero-count skip scan does not understand nesting: it stops
    at the first { to its left, which for a nested group is the
    inner one, leaving the outer { unbalanced. A nesting-aware
    scan would accept this, so the two are told apart here. )
  0
  "{{i}}" ck
  pop

  ( and its bound is currpos > 0, so index 0 is never examined
    against the brace. With the brace absent altogether the scan
    moves nothing and the character at 0 is type-checked after
    all, which is what distinguishes it from a scan that runs
    off the left-hand end. )
  "x" 0
  "i}" ck
  pop pop
  5 0
  "i}" ck
  pop pop

  ( an empty group: upstream forces the count to 1 rather than
    repeat nothing however many times the stack asked, which its
    own comment says could hang the server )
  500
  "{}" ck
  pop
  500
  "{ }" ck
  pop
  500
  "{   }" ck
  pop
  d

  ( --- MAX_COMPLEXITY, which is 18 ---------------------------- )
  1 1 1 1 1 1 1 1 1
  1 1 1 1 1 1 1 1 1
  "{{{{{{{{{{{{{{{{{{i}}}}}}}}}}}}}}}}}}" ck
  pop pop pop pop pop pop pop pop pop
  pop pop pop pop pop pop pop pop pop
  1 1 1 1 1 1 1 1 1 1
  1 1 1 1 1 1 1 1 1
  "{{{{{{{{{{{{{{{{{{{i}}}}}}}}}}}}}}}}}}}" ck
  pop pop pop pop pop pop pop pop pop pop
  pop pop pop pop pop pop pop pop pop

  ( and with exactly eighteen counters for nineteen groups, the
    nineteenth brace is both too deep and short of an argument.
    The complexity test comes first, which is the only thing
    that decides which of the two it reports. )
  1 1 1 1 1 1 1 1 1
  1 1 1 1 1 1 1 1 1
  "{{{{{{{{{{{{{{{{{{{i}}}}}}}}}}}}}}}}}}}" ck
  pop pop pop pop pop pop pop pop pop
  pop pop pop pop pop pop pop pop pop
  d

  ( --- the twelve dbref characters --------------------------- )
  me @ allrefs
  #0 allrefs
  trig allrefs
  prog allrefs
  #4 allrefs
  #5 allrefs
  #999 allrefs
  #-1 allrefs
  #-2 allrefs
  #-3 allrefs
  d
;`

// checkargsScript makes the two objects the dbref table needs: a
// THING at #4, and a garbage ref at #5, which is the only way to
// reach the 'D' character's own TYPE_GARBAGE refusal. #2 is the
// fixture's program and #3 its exit.
var checkargsScript = Script{
	"@create widget",
	"@create junk",
	"@recycle junk",
	"test",
}

// TestCheckargsMatchesFuzzball drives every signature through both
// servers and compares what each said.
func TestCheckargsMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), checkargsSource)
	if err != nil {
		t.Fatal(err)
	}

	script := checkargsScript
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
