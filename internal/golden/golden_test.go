package golden

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Case is one differential test: a MUF program, and what to type at it.
type Case struct {
	// Name identifies the case and names the exit that runs it, so it must
	// be a single word a player could type.
	Name string
	// Source is the program's MUF.
	Source string
	// Then is typed after the exit, one line per entry. A program that
	// reads input needs it; everything else leaves it empty.
	Then []string
	// Pause is how long to wait before reading the case's output, for a
	// program that suspends itself and resumes later. Most cases finish
	// within the command and leave it zero.
	Pause time.Duration
}

// requireOracle skips unless the C server has been built.
func requireOracle(t *testing.T) {
	t.Helper()
	if !OracleAvailable() {
		t.Skip("the oracle image is not built; run 'make golden-build'")
	}
}

// tell is the idiom the cases use to report a value, so a program's output is
// one line per result.
const tellPrelude = `: t[ x -- ] me @ x @ intostr notify ;
: ts[ s -- ] me @ s @ notify ;
`

// cases are run together against one server each.
//
// Starting a container costs about two seconds, so a case per container made
// the suite's runtime grow with the number of cases. Putting every program in
// one database turns that into a single cost per run.
var cases = []Case{
	{
		Name: "arithmetic",
		Source: tellPrelude + `: main
  2 3 + t
  10 3 - t
  6 7 * t
  20 4 / t
  17 5 % t
  -7 2 / t
  -7 2 % t
  0 5 - t
;`,
	},
	{
		Name: "comparisons",
		Source: tellPrelude + `: main
  1 2 < t
  2 1 < t
  2 2 = t
  2 2 >= t
  1 not t
  0 not t
  1 0 and t
  1 0 or t
  1 1 xor t
;`,
	},
	{
		Name: "strings",
		Source: tellPrelude + `: main
  "foo" "bar" strcat ts
  "hello" strlen t
  "hello" toupper ts
  "HELLO" tolower ts
  "  padded  " strip ts
  "hello" 2 3 midstr ts
  "hello" "ll" instr t
  "hello" "zz" instr t
  "abc" "abd" stringcmp t
;`,
	},
	{
		Name: "stack",
		Source: tellPrelude + `: main
  1 2 swap t t
  5 dup t t
  1 2 over t t t
  1 2 3 rot t t t
  1 2 nip t
  1 2 3 depth t pop pop pop
;`,
	},
	{
		Name: "control",
		Source: tellPrelude + `: main
  1 if 111 t else 222 t then
  0 if 111 t else 222 t then
  var i 0 i !
  begin i @ 1 + i ! i @ 3 >= until i @ t
  var sum 0 sum !
  1 5 1 for sum @ + sum ! repeat sum @ t
;`,
	},
	{
		Name: "procedures",
		Source: tellPrelude + `: double[ int:n -- int:r ] n @ 2 * ;
: add[ int:a int:b -- int:r ] a @ b @ + ;
: main
  21 double t
  2 3 add t
  1 double double double t
;`,
	},
	{
		Name: "arrays",
		Source: tellPrelude + `: main
  { 1 2 3 }list array_count t
  { 10 20 30 }list 1 array_getitem t
  { "a" "b" "c" }list "," array_join ts
;`,
	},
	{
		Name: "fmtstring",
		Source: tellPrelude + `: main
  "plain" "%s" fmtstring ts
  42 "%i" fmtstring ts
  "x" "%5s|" fmtstring ts
  "x" "%-5s|" fmtstring ts
  7 "%5i|" fmtstring ts
  1 2 "%i and %i" fmtstring ts
  "100%% sure" ts
;`,
	},
	{
		Name: "strcut",
		Source: tellPrelude + `: main
  "hello world" 5 strcut ts ts
  "hello" 0 strcut ts ts
  "hello" 99 strcut ts ts
  "abcdef" "abcxyz" 3 strncmp t
  "abcdef" "abcxyz" 4 strncmp t
  "one two one" "one" "X" subst ts
;`,
	},
	{
		Name: "smatch",
		Source: tellPrelude + `: main
  "hello" "h*" smatch t
  "hello" "*o" smatch t
  "hello" "h?llo" smatch t
  "hello" "goodbye" smatch t
  "HELLO" "hello" smatch t
  "hello" "{hello|goodbye}" smatch t
;`,
	},
	{
		Name: "objects",
		Source: tellPrelude + `: main
  me @ name ts
  me @ location intostr ts
  me @ owner intostr ts
  me @ player? t
  me @ room? t
  loc @ room? t
  me @ "wizard" flag? t
  me @ "dark" flag? t
  me @ me @ controls t
;`,
	},
	{
		Name: "properties",
		Source: tellPrelude + `: main
  me @ "test/str" "a value" setprop
  me @ "test/str" getpropstr ts
  me @ "test/num" "" 42 addprop
  me @ "test/num" getpropval t
  me @ "test/str" remove_prop
  me @ "test/str" getpropstr ts
  me @ "test" propdir? t
;`,
	},
	{
		Name: "rotate",
		Source: tellPrelude + `: main
  1 2 3 3 rotate t t t
  1 2 3 -3 rotate t t t
  1 2 3 2 rotate t t t
;`,
	},
	{
		Name: "floats",
		Source: tellPrelude + `: main
  2.5 ftostr ts
  4.0 sqrt ftostr ts
  2.0 10.0 pow ftostr ts
  -3.5 fabs ftostr ts
  3.7 floor ftostr ts
  3.2 ceil ftostr ts
  1.0 exp ftostr ts
  100.0 log10 ftostr ts
  "2.5" strtof ftostr ts
  7.0 2.0 fmod ftostr ts
;`,
	},
	{
		Name: "array_ops",
		Source: tellPrelude + `: main
  { 3 1 2 }list 0 array_sort array_vals t t t
  { 1 2 3 4 5 }list 1 3 array_getrange array_count t
  { 1 2 3 }list array_reverse array_vals t t t
  { "a" 1 "b" 2 }dict array_count t
  { 1 2 3 }list 1 array_delitem array_count t
;`,
	},
	{
		Name: "timesplit",
		Source: tellPrelude + `: main
  0 timesplit
  t t t t t t t t
  "%Y-%m-%d" 0 timefmt ts
  "%H:%M:%S" 0 timefmt ts
;`,
	},
	{
		Name: "error_format",
		Source: `: main
  "not a number" 2 +
;`,
	},
	{
		Name: "nested_error",
		Source: `: inner
  "bad" 2 +
;
: outer
  inner
;
: main
  outer
;`,
	},
	{
		Name: "error_with_args",
		Source: `: boom[ str:what int:n -- ]
  what @ n @ +
;
: main
  "text" 7 boom
;`,
	},
	{
		Name: "caught",
		Source: tellPrelude + `: main
  0 try "bad" 2 + catch ts endcatch
  "still here" ts
;`,
	},
	{
		Name: "operators",
		Source: tellPrelude + `: main
  12 10 & t
  12 10 | t
  12 10 ^ t
  1 4 << t
  5 ++ t
  5 -- t
  1 2 != t
  2 2 != t
  6 2 bitshift t
  6 -1 bitshift t
;`,
	},
	{
		Name: "envprop",
		Source: tellPrelude + `: main
  loc @ "test/env" "from the room" setprop
  me @ "test/env" envpropstr ts ts
  me @ "test/missing" envpropstr ts ts
;`,
	},
	{
		Name: "reflists",
		Source: tellPrelude + `: main
  me @ "test/list" #1 reflist_add
  me @ "test/list" #0 reflist_add
  me @ "test/list" #1 reflist_find t
  me @ "test/list" #0 reflist_find t
  me @ "test/list" #3 reflist_find t
  me @ "test/list" getpropstr ts
  me @ "test/list" #1 reflist_del
  me @ "test/list" getpropstr ts
;`,
	},
	{
		Name: "unparse",
		Source: tellPrelude + `: main
  me @ unparseobj ts
  loc @ unparseobj ts
  me @ pennies t
;`,
	},
	{
		Name: "set_ops",
		Source: tellPrelude + `: main
  { 1 2 3 }list { 3 4 5 }list 2 array_nunion array_count t
  { 1 2 3 }list { 3 4 5 }list 2 array_nintersect array_count t
  { 1 2 3 }list { 3 4 5 }list 2 array_ndiff array_count t
  { 1 2 3 }list { 3 4 5 }list 2 array_nintersect array_vals t
;`,
	},
	{
		Name: "array_search",
		Source: tellPrelude + `: main
  { 10 20 30 20 }list 20 array_findval array_count t
  { 10 20 30 }list 99 array_findval array_count t
  { 1 2 3 4 }list 2 array_cut array_count t
  { 1 2 3 }list { 1 2 3 }list array_compare t
  { 1 2 }list { 1 2 3 }list array_compare t
;`,
	},
	{
		Name: "nested",
		Source: tellPrelude + `: main
  { }dict { "a" "b" }list 42 array_nested_set
  { "a" "b" }list array_nested_get t
;`,
	},
	{
		Name: "ctoi",
		Source: tellPrelude + `: main
  "A" ctoi t
  "" ctoi t
  65 itoc ts
  10 itoc ts
  "hello" md5hash ts
;`,
	},
	{
		Name: "error_flags",
		Source: tellPrelude + `: main
  error_num t
  "DIV_ZERO" error_bit t
  "NOSUCH" error_bit t
  0 error_name ts
  1 error_name ts
;`,
	},
	{
		Name: "power",
		Source: tellPrelude + `: main
  2.0 10.0 ** ftostr ts
  3.0 4.0 0.0 dist3d ftostr ts
  1.0 2.0 3.0 4.0 6.0 8.0 diff3 ftostr ts ftostr ts ftostr ts
;`,
	},
	{
		Name: "regex",
		Source: tellPrelude + `: main
  "hello world" "o w" "O W" 0 regsub ts
  "hello world" "o" "0" 2 regsub ts
  "Hello" "hello" "X" 1 regsub ts
  "a1b2c3" "[0-9]" 0 regsplit array_count t
;`,
	},
	{
		Name:   "divide_by_zero",
		Source: `: main 1 0 / me @ swap intostr notify ;`,
	},
	{
		Name: "creation",
		Source: tellPrelude + `: main
  me @ "a test widget" newobject
  dup name ts
  dup owner me @ = t
  dup location me @ = t
  recycle
;`,
	},
	{
		// NEWOBJECT rejects a room as the parent, which is an upstream
		// bug its own message contradicts. Reproduced, so a program
		// behaves the same on both.
		Name: "creation_in_a_room",
		Source: tellPrelude + `: main
  0 try loc @ "in a room" newobject recycle "created" ts catch ts endcatch
;`,
	},
	{
		Name: "links",
		Source: tellPrelude + `: main
  loc @ "linktest" newexit
  dup me @ setlink
  dup getlink me @ = t
  recycle
;`,
	},
	{
		Name: "mlevel",
		Source: tellPrelude + `: main
  me @ mlevel t
  #-1 mlevel t
;`,
	},
	{
		Name: "entrances",
		Source: tellPrelude + `: main
  loc @ entrances_array array_count t
  me @ entrances_array array_count t
;`,
	},
	{
		Name: "passwords",
		Source: tellPrelude + `: main
  me @ "potrzebie" checkpassword t
  me @ "wrong" checkpassword t
;`,
	},
	{
		// MPI is evaluated when a description is read, so the program
		// stores one and then looks at itself.
		Name: "mpi_text",
		Source: tellPrelude + `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop ts
;
: main
  "plain text" show
  "{null:ignored}" show
  "{toupper:shout}" show
  "{strip:  spaced  }|" show
  "{strlen:hello}" show
  "{subst:one two one,one,X}" show
;`,
	},
	{
		Name: "mpi_arithmetic",
		Source: tellPrelude + `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop ts
;
: main
  "{add:2,3}" show
  "{subt:10,3}" show
  "{mult:6,7}" show
  "{div:20,4}" show
  "{div:1,0}" show
  "{mod:17,5}" show
  "{abs:-5}" show
  "{max:3,9,2}" show
  "{min:3,9,2}" show
;`,
	},
	{
		Name: "mpi_logic",
		Source: tellPrelude + `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop ts
;
: main
  "{if:1,yes,no}" show
  "{if:0,yes,no}" show
  "{if:,yes,no}" show
  "{not:1}" show
  "{not:0}" show
  "{eq:2,2}" show
  "{gt:3,2}" show
  "{and:1,1}" show
  "{and:1,0}" show
  "{or:0,1}" show
;`,
	},
	{
		Name: "mpi_literal",
		Source: tellPrelude + `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop ts
;
: main
  "{{not a call}" show
  "{lit:{add:1,2}}" show
;`,
	},
	{
		Name: "mpi_objects",
		Source: tellPrelude + `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop ts
;
: main
  "{name:me}" show
  "{name:here}" show
  "{owner:me}" show
;`,
	},
	// READ is deliberately not tested here. The harness marks the end of a
	// command's output by sending a pose and reading until it appears, and
	// a program waiting on a READ consumes that marker as its input. There
	// is no marker a READ would not eat, so READ is covered by a unit test
	// in internal/game instead.
	{
		// Sleeping suspends the program and resumes it later.
		Name: "muf_sleep",
		Source: tellPrelude + `: main
  "before" ts
  1 sleep
  "after" ts
;`,
		Pause: 2 * time.Second,
	},
	{
		// TESTLOCK, GETLOCKSTR/SETLOCKSTR, PARSELOCK/UNPARSELOCK/PRETTYLOCK
		// and ARRAY_FILTER_LOCK, all against the wizard's own dbref (#1,
		// always present in the fixture) so the case needs no dbref only
		// known after a @create.
		Name: "locks",
		Source: tellPrelude + `: main
  #1 "#1" setlockstr t
  #1 getlockstr ts
  #1 "#1" parselock testlock t
  "#1" parselock unparselock ts
  "#1" parselock prettylock ts
  { #1 #0 }list "#1" parselock array_filter_lock array_count t
  #1 "" setlockstr t
  #1 getlockstr ts
  "" parselock unparselock ts
;`,
	},
	{
		Name: "proc",
		Source: tellPrelude + `: foo ;
public foo
: main
  pid t
  pid ispid? t
  0 ispid? t
  -1 ispid? t
  force_level t
  0 try #1 instances catch ts endcatch
  supplicant intostr ts
  prog "foo" cancall? t
  prog "FOO" cancall? t
  prog "bar" cancall? t
  0 try #1 "foo" cancall? t catch ts endcatch
  999999 kill t
  prog getpids array_count t
  #99999 getpids array_count t
  #-1 getpids array_count 1 >= t
  pid getpidinfo array_count t
  pid getpidinfo "PID" [] t
  pid getpidinfo "TYPE" [] ts
  pid getpidinfo "SUBTYPE" [] ts
  pid getpidinfo "CALLED_DATA" [] ts
  pid getpidinfo "MLEVEL" [] t
  pid getpidinfo "CALLED_PROG" [] intostr ts
  pid getpidinfo "TRIG" [] intostr ts
  pid getpidinfo "PLAYER" [] intostr ts
  999999 getpidinfo array_count t
  0 try pid kill catch "caught" ts endcatch
  "after" ts
;`,
	},
	{
		// FORK: the child runs independently of the parent, on its own copy
		// of every variable — mutating "label" in the child must not be
		// visible to the parent, which already reported its own value by
		// the time the child gets to run.
		Name: "fork",
		Source: tellPrelude + `: main
  "shared" var! label
  fork if
    "parent:" label @ strcat ts
  else
    label @ "changed" strcat label !
    "child:" label @ strcat ts
  then
;`,
		Pause: time.Second,
	},
	{
		// QUEUE: the fired instance is a fresh frame, not a copy of the
		// queuer's — its own COMMAND is "Queued Event.", never the queuer's,
		// and its initial stack argument is the string QUEUE was given, a
		// different string upstream, both pushed from a plain interp() call
		// with no relation to whatever the queuer's own COMMAND/args were.
		//
		// Also exercises GETPIDINFO's other-pid branch, before the queued
		// process fires: SUBTYPE "QUEUE" and CALLED_DATA the arg QUEUE was
		// given, both only knowable from the queuer's side since the fired
		// copy's own frame reports neither about itself.
		Name: "queue",
		Source: tellPrelude + `: main
  command @ "Queued Event." strcmp not if
    "fired:" command @ strcat ts
    ts
  else
    0 prog "queuearg" queue
    dup t
    dup getpidinfo "SUBTYPE" [] ts
    getpidinfo "CALLED_DATA" [] ts
  then
;`,
		Pause: time.Second,
	},
	{
		// WATCHPID on a pid that names no live process queues the
		// PROC.EXIT event immediately, on the caller's own frame — so the
		// very next EVENT_WAITFOR call, filtering on that same event, is
		// served without ever blocking.
		Name: "watchpid",
		Source: tellPrelude + `: main
  999999 watchpid
  { "PROC.EXIT.999999" }list event_waitfor
  ts
  intostr ts
;`,
	},
	{
		// Phase 3: descriptor/connection introspection (src/p_connects.c).
		// Only one connection is live for the whole run, so every check is
		// either shape-only (counts, round-trips through SETWIDTH/
		// SETHEIGHT) or an argument-validation abort, whose wording is
		// exactly what golden exists to catch — DESCRIDLE, DESCRTIME,
		// DESCRBUFSIZE, DESCRHOST and DESCRUSER's own successful-path
		// values are environment-dependent (wall-clock elapsed time, an
		// OS-assigned buffer size, a container's own view of the peer
		// address) and deliberately not compared here.
		Name: "connects",
		Source: tellPrelude + `: main
  descr me @ descrdbref = t
  online array_count t
  online_array array_count t
  descrcount t
  me @ descriptors array_count t
  me @ descr_array array_count t
  descr nextdescr t
  #-1 firstdescr descr = t
  #-1 lastdescr descr = t
  me @ firstdescr descr = t
  me @ lastdescr descr = t
  me @ descrleastidle descr = t
  me @ descrmostidle descr = t
  descr descrflush
  descr 132 setwidth
  descr width t
  descr 43 setheight
  descr height t
  0 try "x" descridle catch ts endcatch
  0 try 999999 descridle catch ts endcatch
  0 try "x" descrtime catch ts endcatch
  0 try 999999 descrtime catch ts endcatch
  0 try "x" descrbufsize catch ts endcatch
  0 try 999999 descrbufsize catch ts endcatch
  0 try "x" descrsecure? catch ts endcatch
  0 try "x" descrdbref catch ts endcatch
  0 try descr "x" descrnotify catch ts endcatch
  0 try 999999 "hi" descrnotify catch ts endcatch
  0 try "x" nextdescr catch ts endcatch
  0 try 5 firstdescr catch ts endcatch
  0 try 5 lastdescr catch ts endcatch
  0 try "x" 5 setwidth catch ts endcatch
  0 try descr 70000 setwidth catch ts endcatch
  0 try "x" 5 setheight catch ts endcatch
  0 try descr 70000 setheight catch ts endcatch
  0 try "x" descrflush catch ts endcatch
;`,
	},
	{
		// Phase 4: a sample of the more tractable p_strings.c/p_misc.c/
		// p_array.c/p_props.c/p_db.c primitives, run at the harness's own
		// mlevel 3 — the mlevel-4 ones (SETSYSPARM, BLESSPROP/UNBLESSPROP,
		// PARSEMPIBLESSED, COMPILE, UNCOMPILE) are covered by their own
		// dedicated wizard-mlevel fixture instead, the same way FORCE's own
		// family needed one.
		Name: "phase4",
		Source: tellPrelude + `: main
  "#5" stod intostr ts
  "nonsense" stod intostr ts
  "'s test" pose-separator? t
  "xtest" pose-separator? t
  "hello" "key" strencrypt "key" strdecrypt ts
  "hi" "bold,red" textattr strlen 0 > t
  { { 3 "c" }list { 1 "a" }list { 2 "b" }list }list 0 0 array_sort_indexed
  dup 0 [] 0 [] t
  dup 1 [] 0 [] t
  2 [] 0 [] t
  { 1 2 3 }list { 9 8 }list 1 array_insertrange array_count t
  me @ "_test/a" 1 setprop
  me @ "_test" { "a" 1 }dict array_put_propvals
  me @ "_test/a" getpropval t
  me @ array_get_ignorelist array_count t
  { "a" 1 me @ }list array_interpret ts
  me @ "{name}" "" 0 parsempi ts
  me @ "_test/a" blessed? t
  "good" prop-name-ok? t
  "bad:name" prop-name-ok? t
  { me @ }list "_test/a" "1" array_filter_prop array_count t
  prog compiled? 0 >= t
  prog 1 1 program_getlines array_count 0 >= t
  0 try #5 stats catch ts endcatch
;`,
	},
	{
		// Phase 4's remainder: the flag-match expression both
		// ARRAY_FILTER_FLAGS and FINDNEXT take, the rewritten shared
		// sprintf behind FMTSTRING and ARRAY_FMTSTRINGS, the seeded
		// generator GETSEED/SETSEED expose, FMTTIME's arbitrary format and
		// INTERP's nested run. Each is mlevel 3 or below; COPYOBJ,
		// NEWPLAYER, COPYPLAYER, TOADPLAYER, PNAME_HISTORY,
		// PROGRAM_SETLINES and DUMP are all mlevel 4 and covered by unit
		// tests instead, for the same reason as the case above.
		Name: "phase4b",
		Source: tellPrelude + `: main
  ( the flag language, positive and negated )
  { me @ }list "P" array_filter_flags array_count t
  { me @ }list "!P" array_filter_flags array_count t
  { me @ }list "R" array_filter_flags array_count t
  { me @ #0 }list "R" array_filter_flags array_count t
  0 try { 1 2 }list "P" array_filter_flags catch ts endcatch
  0 try { me @ }list "" array_filter_flags catch ts endcatch

  ( findnext walks the db in ref order )
  #-1 me @ "" "" findnext me @ = t
  me @ me @ "" "" findnext t
  #-1 me @ "nosuchname" "" findnext t

  ( the shared sprintf: width, justification, padding and the verbs )
  42 "%i" fmtstring ts
  42 "[%5i]" fmtstring ts
  42 "[%-5i]" fmtstring ts
  42 "[%05i]" fmtstring ts
  42 "[%+i]" fmtstring ts
  "abc" "[%5s]" fmtstring ts
  "abc" "[%-5s]" fmtstring ts
  "abcdef" "[%.2s]" fmtstring ts
  me @ "%d" fmtstring ts
  me @ "%D" fmtstring ts
  1.5 "%f" fmtstring ts
  1.5 "[%.2f]" fmtstring ts
  3 "%~" fmtstring ts
  "hi" "%~" fmtstring ts
  1.0 "%?" fmtstring ts
  "100%% done" fmtstring ts
  5 "abc" "[%*s]" fmtstring ts
  0 try "abc" "%i" fmtstring catch ts endcatch
  0 try 3 "%s" fmtstring catch ts endcatch

  ( array_fmtstrings names its fields instead of popping them )
  { { "name" "Rusty" "n" 3 }dict }list "%[name]s has %[n]i" array_fmtstrings
  dup 0 [] ts array_count t
  { { "name" "Rusty" }dict }list "%[missing]s|%[gone]i" array_fmtstrings 0 [] ts
  0 try { { "a" 1 }dict }list "%s" array_fmtstrings catch ts endcatch

  ( the seeded generator replays from a recorded seed )
  "AAAABBBBCCCCDDDDAAAABBBBCCCCDDDD" setseed
  getseed ts
  srand intostr ts
  srand intostr ts
  getseed ts
  "AAAABBBBCCCCDDDDAAAABBBBCCCCDDDD" setseed
  srand intostr ts
  "short" setseed
  getseed ts
  srand intostr ts

  ( fmttime parses under a caller-supplied format )
  "12:00:00 01/01/2000" "%T%t%D" fmttime t
  "2000-01-01" "%Y-%m-%d" fmttime t
  0 try "nonsense" "%Y-%m-%d" fmttime catch ts endcatch
  0 try "2000" "" fmttime catch ts endcatch

  ( interp runs another program and takes back its top value )
  0 try #-1 me @ "" interp catch ts endcatch
  0 try prog #-1 "" interp catch ts endcatch
  0 try prog me @ 3 interp catch ts endcatch
;`,
	},
	{
		// Timers and cross-process events. A timer with a zero delay is
		// already due when EVENT_WAITFOR asks for it, so this finishes in
		// one step and needs no Pause — the delayed case is covered by a
		// unit test instead, since the two servers' tick intervals differ.
		Name: "timers",
		Source: tellPrelude + `: main
  0 try 0 "tick" timer_start catch ts endcatch
  ( event_waitfor leaves the data below the event's name )
  { "TIMER.tick" }list event_waitfor
  ts pop
  ( a timer cancelled before it fires delivers nothing )
  0 "gone" timer_start
  "gone" timer_stop
  "TIMER.gone" event_exists t
  ( event_send to our own pid lands on our own queue )
  pid "hello" "payload" event_send
  "USER.hello" event_exists t
  { "USER.hello" }list event_waitfor
  ts
  dup "data" [] ts
  dup "caller_pid" [] pid = t
  "player" [] me @ = t
  ( a pid that names nothing is silently ignored )
  999999 "nowhere" 1 event_send
  0 try 0 3 timer_start catch ts endcatch
  0 try "x" "y" timer_start catch ts endcatch
  0 try 3 timer_stop catch ts endcatch
  0 try pid 3 "d" event_send catch ts endcatch
  0 try "x" "e" "d" event_send catch ts endcatch
;`,
	},
	{
		// Phase 5: the MPI list functions and the looping ones built on
		// them. A carriage return inside a result would split the line the
		// harness compares, so a list is inspected with {count}, {lmember}
		// and {sublist} rather than printed whole.
		Name: "mpi_lists",
		Source: tellPrelude + `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop ts
;
: main
  ( build and measure )
  "{count:{mklist:a,b,c}}" show
  "{count:}" show
  "{count:{mklist:a,b,c},b}" show

  ( slicing, forwards, backwards and out of range )
  "{sublist:{mklist:a,b,c,d},2}" show
  "{count:{sublist:{mklist:a,b,c,d},2,3}}" show
  "{sublist:{mklist:a,b,c,d},-1}" show
  "{sublist:{mklist:a,b,c,d},2,99}" show
  "{sublist:{mklist:a,b,c},0}" show
  "{sublist:{mklist:a,b,c}}" show

  ( set operations, and the case-sensitivity split between them )
  "{count:{lunique:{mklist:a,b,a,A}}}" show
  "{count:{lcommon:{mklist:a,b,c},{mklist:b,c,d}}}" show
  "{sublist:{lcommon:{mklist:a,b,c},{mklist:b,c,d}},1}" show
  "{count:{lunion:{mklist:a,b},{mklist:B,c}}}" show
  "{count:{lremove:{mklist:a,b,c},{mklist:b}}}" show
  "{count:{lremove:{mklist:a,B,c},{mklist:b}}}" show
  "{lmember:{mklist:a,b,c},b}" show
  "{lmember:{mklist:a,b,c},z}" show

  ( sorting, default and with a comparison body )
  "{sublist:{lsort:{mklist:item10,item2,item1}},1}" show
  "{sublist:{lsort:{mklist:item10,item2,item1}},3}" show
  "{sublist:{lsort:{mklist:b,a,c}},1}" show
  "{sublist:{lsort:{mklist:1,3,2},a,b,{gt:{&a},{&b}}},1}" show

  ( the looping functions: each yields only its last iteration )
  "{for:i,1,5,1,{&i}}" show
  "{for:i,5,1,-1,{&i}}" show
  "{foreach:x,{mklist:a,b,c},{&x}}" show
  "{count:{filter:x,{mklist:1,0,2},{&x}}}" show
  "{fold:acc,x,{mklist:1,2,3},{add:{&acc},{&x}}}" show
  "{fold:acc,x,{mklist:7},{add:{&acc},{&x}}}" show

  ( variables: does a {set} inside a nested body reach a {with} binding? )
  "{with:n,0,{&n}}" show
  "{with:n,0,{set:n,5}{&n}}" show
  "{with:n,0,{if:1,{set:n,5}}{&n}}" show

  ( eval, and the macros built on it )
  "{eval:{lit:{add:1,2}}}" show
  "{func:double,n,{add:{&n},{&n}}}{double:21}" show
  "{func:greet,a,b,{&a}-{&b}}{greet:x,y}" show

  ( errors )
  "{lsort:a,b}" show
  "{count:{mklist:a,b},}" show
  "{nosuchfunction:x}" show
;`,
	},
	{
		// MPI property functions. {prop} searches outwards through the
		// environment and {prop!} does not, which is what the room-set
		// property here distinguishes.
		Name: "mpi_props",
		Source: tellPrelude + `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop ts
;
: main
  me @ location "_envtest" "fromroom" setprop
  me @ "_own" "mine" setprop
  "{prop:_own}" show
  "{prop!:_own}" show
  "{prop:_envtest}" show
  "{prop!:_envtest}" show
  "{prop:_nosuch}" show

  me @ "_ind" "_own" setprop
  "{index:_ind}" show
  "{index!:_ind}" show
  "{index:_nosuch}" show

  me @ "_dir/a" "1" setprop
  "{propdir:_dir}" show
  "{propdir:_own}" show

  me @ "_gone" "here" setprop
  "{prop!:_gone}" show
  "{delprop:_gone}" show
  "{prop!:_gone}" show

  ( property lists: a count and numbered items )
  me @ "_stuff#" "3" setprop
  me @ "_stuff#/1" "one" setprop
  me @ "_stuff#/2" "two" setprop
  me @ "_stuff#/3" "three" setprop
  "{count:{list:_stuff}}" show
  "{parse:x,{list:_stuff},{&x},,+}" show
  "{concat:_stuff}" show
  "{select:2,_stuff}" show
  "{select:9,_stuff}" show
  "{count:{list:_nosuchlist}}" show

  ( a list with no count property is measured by walking it )
  me @ "_walk#/1" "a" setprop
  me @ "_walk#/2" "b" setprop
  "{count:{list:_walk}}" show

  ( lexec runs a property list as MPI )
  me @ "_code#/1" "{add:1," setprop
  me @ "_code#/2" "2}" setprop
  "{lexec:_code}" show
  me @ "_one" "{add:3,4}" setprop
  "{exec:_one}" show
  "{exec!:_one}" show
;`,
	},
	{
		// PARSEPROPEX: MPI with a MUF dictionary in scope, handed back
		// with whatever the MPI left in each variable.
		Name: "parsepropex",
		Source: tellPrelude + `: main
  me @ "_px" "{&greeting}, {&name}!{set:name,changed}" setprop
  me @ "_px" { "greeting" "Hello" "name" "world" }dict 0 parsepropex
  ts
  dup "name" [] ts
  "greeting" [] ts

  ( a property that is not set evaluates to nothing and changes nothing )
  me @ "_nosuchprop" { "a" "1" }dict 0 parsepropex
  ts
  "a" [] ts

  ( every value type becomes the text an MPI variable holds )
  me @ "_types" "{&i}/{&d}/{&s}" setprop
  me @ "_types" { "i" 42 "d" me @ "s" "txt" }dict 0 parsepropex
  ts pop

  ( argument checking )
  0 try me @ "_px" { 1 2 }dict 0 parsepropex catch ts endcatch
  0 try me @ "_px" { "a" "b" }list 0 parsepropex catch ts endcatch
  0 try me @ "_px" { "a" "b" }dict 2 parsepropex catch ts endcatch
  0 try #-1 "_px" { "a" "b" }dict 0 parsepropex catch ts endcatch
  0 try me @ 3 { "a" "b" }dict 0 parsepropex catch ts endcatch
;`,
	},

	{
		// MPI object introspection. Anything environment-dependent — a
		// dbref number, a connection's idle time, the clock — is compared
		// only for shape, the same rule the connects case follows.
		Name: "mpi_objects2",
		Source: tellPrelude + `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop ts
;
: main
  "{type:me}" show
  "{type:here}" show
  "{type:#-1}" show
  "{fullname:me}" show
  "{flag?:me,player}" show
  "{flag?:me,dark}" show
  "{flag?:me,!dark}" show
  "{controls:me,me}" show
  "{controls:here,me}" show
  "{holds:me,here}" show
  "{holds:here,me}" show
  "{contains:me,here}" show
  "{contains:here,me}" show
  "{nearby:me,me}" show
  "{dbeq:me,me}" show
  "{dbeq:me,here}" show
  "{money:me}" show
  "{money:here}" show
  "{count:{contents:here}}" show
  "{count:{contents:here,Player}}" show
  "{count:{exits:me}}" show
  "{count:{links:me}}" show
  "{locked:me,here}" show
  "{usecount:me}" show

  ( errors and edge cases )
  "{contents:here,Exit}" show
  "{contents:here,nonsense}" show
  "{online}" show

  ( strings, numbers and time: all deterministic )
  "{smatch:hello,h*}" show
  "{smatch:hello,goodbye}" show
  "{xor:1,0}" show
  "{xor:1,1}" show
  "{dist:3,4}" show
  "{dist:0,0,3,4}" show
  "{dice:1,0}" show
  "{dice:0}" show
  "{timestr:90}" show
  "{timestr:90061}" show
  "{stimestr:90}" show
  "{stimestr:45}" show
  "{stimestr:90061}" show
  "{ltimestr:90}" show
  "{ltimestr:0}" show
  "{convtime:12:00:00 01/01/2000}" show
  "{ftime:%Y-%m-%d,,946684800}" show
  "{ftime:%H:%M:%S,,946684800}" show
  "{default:,fallback}" show
  "{default:given,fallback}" show
  "{commas:{mklist:a,b,c}}" show
  "{commas:{mklist:a}}" show
  "{commas:{mklist:a,b}, or }" show
  "{escape:plain text}" show
  "{with:n,7,{v:n}}" show
  "{muckname}" show
  "{sysparm:penny}" show
  "{pronouns:%s likes %p stuff.}" show
  "{attr:bold,red,hi}" strlen 0 > t
  "{attr:nosuchtag,hi}" show
  "{dist:1}" show
;`,
	},
}

// TestAgainstFuzzball runs every case against the C server and against this
// one, and reports where their transcripts differ.
func TestAgainstFuzzball(t *testing.T) {
	requireOracle(t)

	programs := make([]Program, len(cases))
	script := make(Script, 0, len(cases))
	// steps[i] is how many script entries belong to case i, so its output
	// can be gathered back together.
	steps := make([]int, len(cases))
	// pauses[i] delays reading step i's output, for a program that resumes
	// after suspending itself.
	pauses := map[int]time.Duration{}
	for i, c := range cases {
		programs[i] = Program{Name: c.Name, Source: c.Source}
		script = append(script, c.Name)
		script = append(script, c.Then...)
		steps[i] = 1 + len(c.Then)
		if c.Pause > 0 {
			pauses[len(script)-1] = c.Pause
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Each server gets its own copy, so neither can see what the other left
	// behind in the database.
	oracleFx, err := WriteMultiFixture(t.TempDir(), programs)
	if err != nil {
		t.Fatal(err)
	}
	emeraldFx, err := WriteMultiFixture(t.TempDir(), programs)
	if err != nil {
		t.Fatal(err)
	}

	oracleOut, err := RunOracleSteps(ctx, oracleFx, script, pauses)
	if err != nil {
		t.Fatalf("running the oracle: %v", err)
	}
	emeraldOut, err := RunEmeraldSteps(ctx, emeraldFx, script, pauses)
	if err != nil {
		t.Fatalf("running emerald: %v", err)
	}
	if len(oracleOut) != len(script) || len(emeraldOut) != len(script) {
		t.Fatalf("got %d oracle and %d emerald transcripts for %d steps",
			len(oracleOut), len(emeraldOut), len(script))
	}

	// Each case is reported on its own, so one failure names itself rather
	// than shifting every line after it.
	at := 0
	for i, c := range cases {
		oracle := strings.Join(oracleOut[at:at+steps[i]], "")
		emerald := strings.Join(emeraldOut[at:at+steps[i]], "")
		at += steps[i]

		t.Run(c.Name, func(t *testing.T) {
			if diffs := Compare(oracle, emerald); len(diffs) > 0 {
				t.Errorf("transcripts differ:\n%s\nfuzzball said:\n%s\nemerald said:\n%s",
					Render(diffs), oracle, emerald)
			}
		})
	}
}
