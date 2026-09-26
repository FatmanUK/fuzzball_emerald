package golden

import (
	"context"
	"testing"
	"time"
)

// The six compiler conditionals — $ifver, $ifnver, $iflibver,
// $ifnlibver, $ifcancall, $ifncancall — which were recognised and
// treated as false because they need a live database the compiler is
// deliberately denied. They are answered through two callbacks now,
// the way $include already was.
//
// The fixture is three programs: a library that declares versions and
// exports a public function, and two that ask about it. #2 is the
// library, #4 and #6 the askers, since each program is followed by
// its exit.

const condLibrary = `$version 2.5
$lib-version 1.5
$pubdef :
: hello "hello" me @ swap notify ;
public hello
$libdef hello
: main "library" me @ swap notify ;`

// condVersions asks every version question four ways. The comparison
// is "is the wanted version at most the one the object has", so 2.0
// is met by a library at 2.5 and 3.0 is not.
const condVersions = `: main
  $ifver #2 2.0
    "ver 2.0: yes" me @ swap notify
  $else
    "ver 2.0: no" me @ swap notify
  $endif
  $ifver #2 3.0
    "ver 3.0: yes" me @ swap notify
  $else
    "ver 3.0: no" me @ swap notify
  $endif
  $ifnver #2 3.0
    "nver 3.0: yes" me @ swap notify
  $endif
  $iflibver #2 1.0
    "libver 1.0: yes" me @ swap notify
  $else
    "libver 1.0: no" me @ swap notify
  $endif
  $iflibver #2 2.0
    "libver 2.0: yes" me @ swap notify
  $else
    "libver 2.0: no" me @ swap notify
  $endif
  $ifnlibver #2 2.0
    "nlibver 2.0: yes" me @ swap notify
  $endif
  ( an object with no version property reads as 0.0 )
  $ifver me 0.0
    "me 0.0: yes" me @ swap notify
  $else
    "me 0.0: no" me @ swap notify
  $endif
  $ifver me 1.0
    "me 1.0: yes" me @ swap notify
  $else
    "me 1.0: no" me @ swap notify
  $endif
  ( "this" is the program being compiled )
  $ifver this 0.0
    "this 0.0: yes" me @ swap notify
  $endif
;`

// condCanCall asks about a public function that exists, one that does
// not, and an object that is not a program.
const condCanCall = `: main
  $ifcancall #2 hello
    "cancall hello: yes" me @ swap notify
  $else
    "cancall hello: no" me @ swap notify
  $endif
  $ifcancall #2 nosuchfunction
    "cancall nosuch: yes" me @ swap notify
  $else
    "cancall nosuch: no" me @ swap notify
  $endif
  $ifncancall #2 nosuchfunction
    "ncancall nosuch: yes" me @ swap notify
  $endif
  $ifcancall #0 hello
    "cancall on a room: yes" me @ swap notify
  $else
    "cancall on a room: no" me @ swap notify
  $endif
;`

// condScript compiles and runs each asker. @list is not used: the
// interesting output is what the compiled code does, and a
// conditional that took the wrong branch shows up there.
var condScript = Script{
	"lib",
	"versions",
	"cancall",

	// And the compile errors, which are errors rather than false
	// conditions: an object that does not resolve, and a missing
	// second argument.
	"@program badver",
	"i",
	"1 i",
	": main $ifver $nosuchreg 1.0 $endif ;",
	".end",
	"c",
	"q",
	"@program badfn",
	"i",
	"1 i",
	": main $ifcancall #2 $endif ;",
	".end",
	"c",
	"q",
}

// TestConditionalsMatchFuzzball checks the six compiler conditionals
// against the C server.
func TestConditionalsMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteMultiFixture(t.TempDir(), []Program{
		{Name: "lib", Source: condLibrary},
		{Name: "versions", Source: condVersions},
		{Name: "cancall", Source: condCanCall},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The editor holds the input line, so the marker cannot be
	// used and the quiet runner drives both halves.
	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, condScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, condScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range condScript {
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
