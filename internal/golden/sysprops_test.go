package golden

import (
	"context"
	"testing"
)

// Upstream keeps six values on #0 under the `_sys` propdir —
// `SYSTEM_PROPDIR_PROTECT2` (`include/game.h:70`) — and this server
// wrote none of them. `do_uptime` (`look.c:994`) reads
// `_sys/startuptime` back rather than consulting a clock, so `uptime`
// agreed with upstream while a program asking `#0` got nothing.
//
// Four are written once the database is loaded (`game.c:487-490`),
// one per dump (`events.c:99`) and one at shutdown
// (`interface.c:4600`). They are **integers**: `add_property` is
// called with a NULL string and a value, so a program reads them with
// `getpropval`.
//
// Three are exactly comparable here, because both servers boot the
// same fixture with the same parameters: maxpennies, dumpinterval and
// max_connects. startuptime cannot be — the two servers boot
// seconds apart — so it is compared as a *plausibility*: non-zero,
// and within a day of the server's own clock. That is enough to catch
// the bug that mattered, which was the property not being there at
// all.
//
// `_sys/lastdumptime` and `_sys/shutdowntime` are not probed. The
// first is a flush here rather than a dump cycle and so fires on a
// schedule the oracle does not share; the second is written on the
// way out, after anything a transcript could ask. Both are unit
// tested instead.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const sysPropsSource = `: t[ x -- ] me @ x @ intostr notify ;
: ts[ s -- ] me @ s @ notify ;
: main
  ( maxpennies is max_pennies, which both servers honour, so the
    number is directly comparable. )
  #0 "_sys/maxpennies" getpropval t

  ( dumpinterval is dump_interval. The parameter is inert in this
    server -- persistence is write-behind rather than a dump cycle --
    and the value is written anyway, because a program asking is
    entitled to an answer. )
  #0 "_sys/dumpinterval" getpropval t

  ( max_connects is a high-water mark of connections. One player is
    logged in on each server, so both read 1. )
  #0 "_sys/max_connects" getpropval t

  ( startuptime cannot be compared directly, so what is compared is
    that it is there and sane: non-zero, and within a day of the
    clock. Before this it was absent, which getpropval reads as 0. )
  #0 "_sys/startuptime" getpropval dup 0 > swap
    systime swap - abs 86400 < and
    if "startuptime sane" else "startuptime WRONG" then ts

  ( And it is an integer property rather than a string, which is
    what getpropval wants. A string would read back as 0 above and
    as itself here. )
  #0 "_sys/startuptime" getpropstr "[" swap strcat "]" strcat ts

  ( The propdir exists and holds what was written. Four at boot:
    nothing has flushed or shut down yet. )
  #0 "_sys/" nextprop ts
;`

// sysPropsScript needs nothing built.
var sysPropsScript = Script{"test", "uptime"}

// TestSysPropsMatchFuzzball compares what a program reads off #0, and
// that `uptime` still answers.
func TestSysPropsMatchFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), sysPropsSource)
	if err != nil {
		t.Fatal(err)
	}
	script := sysPropsScript
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
		// uptime names a wall-clock time and an elapsed
		// count, neither of which two servers can agree on;
		// what is compared is the shape of the line.
		if cmd == "uptime" {
			want = maskUptime(want)
			got = maskUptime(got)
		}
		if diffs := Compare(want, got); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}
