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
		Name: "queue",
		Source: tellPrelude + `: main
  command @ "Queued Event." strcmp not if
    "fired:" command @ strcat ts
    ts
  else
    0 prog "queuearg" queue t
  then
;`,
		Pause: time.Second,
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
