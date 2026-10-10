package golden

import (
	"context"
	"testing"
)

// `smatch` (`fbstrings.c:1583`) is `equalstr`, and `equalstr` is read
// in fourteen places here: `@tune`'s listing pattern, a listener's
// conditional value, `examine`'s property pattern, `@find`'s name,
// `@bless`'s path, the `file_*` family, MUF `SMATCH`, `FINDNEXT`,
// `ARRAY_FILTER_*`, MPI's `{smatch}` and `{listprops}`, a `@lock`
// string comparison, and now `reserved_names`.
// `internal/ascii.SMatch` had **no test at all**, and it was not
// upstream's function.
//
// MUF `SMATCH` is the cheapest way to compare it: one program, a
// table of pairs, one transcript, and the compiled C decides every
// answer.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const smatchSource = `
: one[ s p -- ]
  s @ "|" strcat p @ strcat "| = " strcat
  s @ p @ smatch if "1" else "0" then strcat
  me @ swap notify
;
: main
  ( A plain star, which is the case everything else is built on. )
  "dog" "d*g" one
  "dg" "d*g" one
  "dorfg" "d*g" one
  "dogs" "d*g" one
  "dog" "*" one
  "" "*" one

  ( '?' is one character, and a star absorbing a following '?'
    consumes one character of the subject. )
  "dog" "d?g" one
  "dg" "d?g" one
  "drug" "d?g" one
  "abcd" "*?d" one
  "d" "*?d" one

  ( Brace alternation is a WORD pattern. It matches only at the
    start of the subject or just after a space, and it consumes a
    whole word -- so a brace followed by anything but a space, a
    wildcard-at-end or the end of the pattern matches nothing. )
  "gold" "{gold|silver}" one
  "goldfish" "{gold|silver}*" one
  "gold fish" "{gold|silver}*" one
  "silver fish" "{gold|silver}*" one
  "bronze fish" "{gold|silver}*" one
  "foop" "{foo}p" one
  "pfoo" "*{foo}" one
  "p foo" "*{foo}" one
  "Moira snores" "{Moira|Chupchup}*" one
  "Moira' snores" "{Moira|Chupchup}*" one
  "a gold ring" "*{gold}*" one
  "a golden ring" "*{gold}*" one

  ( The anchor is on the SUBJECT, not the pattern: a set reached
    with characters already consumed and no space before it matches
    nothing, which is the one place the s2 == start test has teeth.
    Reached through the main loop rather than through a star. )
  "say gold" "say {gold}" one
  "xgold" "x{gold}" one

  ( A leading '^' inside the braces inverts it. )
  "gold" "{^gold|silver}" one
  "bronze" "{^gold|silver}" one

  ( Character classes, which this server did not have at all. )
  "Mr." "M[rs]." one
  "Ms." "M[rs]." one
  "Mx." "M[rs]." one
  "Mb" "M[a-z]" one
  "M0" "M[a-z]" one
  "q" "[^a-z]" one
  "0" "[^a-z]" one
  "a-b" "a[-]b" one
  "azb" "a[z-]b" one
  "a-b" "a[z-]b" one

  ( A star followed by a class tries every position. )
  "xyzq" "*[q]" one
  "xyzq" "*[a-c]" one

  ( Backslash makes the next character literal, and a trailing
    backslash matches nothing. )
  "a*b" "a\\*b" one
  "axb" "a\\*b" one
  "a{b" "a\\{b" one
  "ab" "ab\\" one

  ( Case is folded on both sides. )
  "DOG" "d*g" one
  "dog" "D*G" one

  ( An unterminated class or set matches nothing at all, rather
    than being taken literally. )
  "abc" "a[bc" one
  "a[bc" "a[bc" one
  "gold" "{gold" one

  ( An empty set matches an empty word, which only a leading space
    can provide. )
  " x" "{}*" one
  "x" "{}*" one

  ( An escape inside a set hides the separator and the closing
    brace. )
  "a|b" "{a\\|b}" one
  "a}b" "{a\\}b}" one

  ( The manual's own compound example. )
  "Foxen tickles?" "{Foxen|Lynx|Fier[ao]} *t[iy]ckle*\\?" one
  "Fiera tyckles?" "{Foxen|Lynx|Fier[ao]} *t[iy]ckle*\\?" one
  "Fierb tyckles?" "{Foxen|Lynx|Fier[ao]} *t[iy]ckle*\\?" one
  "Lynx tickles!" "{Foxen|Lynx|Fier[ao]} *t[iy]ckle*\\?" one

  ( A set after a star that is not at the start of the pattern,
    which is the one branch the s2 == start test guards. )
  "say gold now" "say *{gold}*" one
  "say golden now" "say *{gold}*" one

  ( And an empty pattern matches only an empty subject, which is
    the final tolower difference rather than a special case. )
  "" "" one
  "x" "" one
  "" "x" one
;`

var smatchScript = Script{
	"@set test.muf=wizard",
	"@set test.muf=3",
	"test",
}

// TestSMatchMatchesFuzzball compares the table.
func TestSMatchMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), smatchSource)
	if err != nil {
		t.Fatal(err)
	}
	script := smatchScript
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
