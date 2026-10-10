package golden

import (
	"context"
	"testing"
)

// `ieee_bounds_handling` (`p_float.c`, seventeen sites across eleven
// primitives) defaults **true** and had no reader, so every
// out-of-range float operation answered 0.0 where upstream answers
// NAN or INF — and several of them set a different error flag, or
// none at all.
//
// The flags are read back beside each result, because which one is
// set is half of what the parameter decides.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const ieeeSource = `
: flags[ -- s ]
  "DIV_ZERO" is_set? if "Z" else "-" then
  "NAN" is_set? if "N" else "-" then strcat
  "IMAGINARY" is_set? if "I" else "-" then strcat
  "FBOUNDS" is_set? if "B" else "-" then strcat
;
: r[ lbl f -- ]
  lbl @ " = " strcat f @ ftostrc strcat
  " [" strcat flags strcat "]" strcat
  me @ swap notify
  clear
;
: all[ -- ]
  "sqrt -1"    -1.0 sqrt r
  "sqrt inf"   inf sqrt r
  "sin inf"    inf sin r
  "cos inf"    inf cos r
  "tan inf"    inf tan r
  "tan pi/2"   pi 2.0 / tan r
  "asin 2"     2.0 asin r
  "acos 2"     2.0 acos r
  "exp 1"      1.0 exp r
  "exp inf"    inf exp r
  "log 0"      0.0 log r
  "log -1"     -1.0 log r
  "log10 -1"   -1.0 log10 r
  ( POW takes the base **below** the exponent, so this is
    0.333333 raised to -8. )
  "0.3^-8"     0.333333 -8.0 pow r
  "inf^2"      inf 2.0 pow r
  "round inf"  inf 2 round r
;
: two[ lbl a b -- ]
  lbl @ " = " strcat a @ ftostrc strcat
  " , " strcat b @ ftostrc strcat
  " [" strcat flags strcat "]" strcat
  me @ swap notify
  clear
;
: rest[ -- ]
  ( The arithmetic operators have the same branch, and the ieee
    arm **recomputes** rather than answering a fixed INF -- so a
    subtraction of two infinities is a NaN where a subtraction of
    one is an infinity. A zero divisor is tested with DBL_EPSILON
    and raises DIV_ZERO either way. )
  "inf + 1"    inf 1.0 + r
  "inf - inf"  inf inf - r
  "inf * -1"   inf -1.0 * r
  "inf * 0"    inf 0.0 * r
  "1 / 0.0"    1.0 0.0 / r
  "-1 / 0.0"   -1.0 0.0 / r
  "0 / 0.0"    0.0 0.0 / r

  ( The family members with no ieee branch of their own: an
    infinity comes straight back from CEIL and FLOOR, FABS turns
    it positive, and ATAN answers Pi/2 and says nothing at all. )
  "ceil inf"   inf ceil r
  "floor -inf" 0.0 inf - floor r
  "fabs -inf"  0.0 inf - fabs r
  "atan inf"   inf atan r

  ( MODF splits its answer: the integer half is the argument and
    the fractional half is the bounded one. )
  "modf inf"   inf modf two
  "modf 2.5"   2.5 modf two

  ( POW's near-zero base short-circuits to zero before anything
    else is looked at, and a negative base with a fractional
    exponent is imaginary. )
  "0^2"        0.0 2.0 pow r
  ( ...and "near-zero" is DBL_EPSILON, not zero: a base this
    small short-circuits where the arithmetic would not. )
  "1e-300^-1"  "1.0e-300" strtof -1.0 pow r

  ( STRTOF's own idea of a float is ifloat, which demands a
    decimal point -- so "1e-300" is not one, and a string that is
    neither an ifloat nor a plain integer answers 0.0 and raises
    the NAN flag. )
  "strtof 1e-300" "1e-300" strtof r
  "strtof 1.5"    "1.5" strtof r
  "strtof 7"      "7" strtof r
  "strtof 1.5sp"  "1.5 " strtof r
  "strtof sp1.5"  " 1.5" strtof r
  "strtof .5"     ".5" strtof r
  "strtof 1."     "1." strtof r
  "strtof inf"    "inf" strtof r
  "strtof 0x10"   "0x10" strtof r
  "strtof 1.5e3"  "1.5e3" strtof r
  "strtof e3sp"   "1.5e3 " strtof r
  "0.5^-8"     0.5 -8.0 pow r
  "-8^0.5"     -8.0 0.5 pow r
  "-8^2"       -8.0 2.0 pow r

  ( The coordinate conversions answer three zeros and the NAN
    flag. )
  "xyz bad"    inf 1.0 1.0 xyz_to_polar pop two
  "polar bad"  inf 1.0 1.0 polar_to_xyz pop two

  ( And the two spellings, where C and Go disagree about what a
    value that is not a number is called. )
  "ftostr nan"  0.0 0.0 / ftostr me @ swap notify
  clear
  "ftostrc 100" 100.0 ftostrc me @ swap notify
  "ftostr 100"  100.0 ftostr me @ swap notify
  "ftostrc inf" inf ftostrc me @ swap notify
;
: errs[ -- ]
  ( IS_SET?, SET_ERROR and CLEAR_ERROR all take a flag by NAME as
    well as by number, and the last two push a result -- 1 when
    the flag was resolved and 0 when it was not. )
  clear
  "NAN" set_error intostr me @ swap notify
  "nonsense" set_error intostr me @ swap notify
  3 set_error intostr me @ swap notify
  99 set_error intostr me @ swap notify
  "set" flags strcat me @ swap notify
  "NAN" clear_error intostr me @ swap notify
  "cleared" flags strcat me @ swap notify
  1 is_set? intostr me @ swap notify
  "FBOUNDS" is_set? intostr me @ swap notify
  "nonsense" is_set? intostr me @ swap notify
  ( ERROR_NAME and ERROR_STR take either too, and answer the
    empty string for a flag they cannot resolve. )
  2 error_name me @ swap notify
  "[" 2 error_str strcat "]" strcat me @ swap notify
  "[" "nonsense" error_str strcat "]" strcat me @ swap notify
  "[" 99 error_name strcat "]" strcat me @ swap notify
  ( ...and ERROR_NAME takes an integer only, where ERROR_STR
    beside it takes either. )
  0 try "IMAGINARY" error_name pop catch me @ swap notify endcatch
  clear
;
: main
  "-- default --" me @ swap notify
  all rest
  "ieee_bounds_handling" "n" setsysparm
  "-- off --" me @ swap notify
  all rest
  "ieee_bounds_handling" "y" setsysparm
  "-- flags --" me @ swap notify
  errs
;`

var ieeeScript = Script{
	// SETSYSPARM is wizard-level, so the program has to be.
	"@set test.muf=wizard",
	"@set test.muf=3",
	"test",
}

// TestIeeeBoundsMatchesFuzzball compares both settings.
func TestIeeeBoundsMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ieeeSource)
	if err != nil {
		t.Fatal(err)
	}
	script := ieeeScript
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
