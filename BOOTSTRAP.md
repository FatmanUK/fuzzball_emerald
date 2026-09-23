# BOOTSTRAP.md — Fuzzball Emerald

Generated to allow a fresh agent (or a competing LLM) to resume this project
from scratch after total loss of session state. If this file and the repo
disagree, the repo wins — this is a snapshot, not a source of truth.

## 1. Current Goal

Build a from-scratch **Go** reimplementation of **Fuzzball MUCK 7** that is
behaviour-compatible with the original — existing MUF programs, MPI
descriptions, and `.db` worlds must work unchanged — while deliberately
replacing four things that have aged worst in the C original:

| Fuzzball 7 (C) | Fuzzball Emerald (Go) |
|---|---|
| Manual memory management | Go, memory-safe |
| Plaintext + optional SSL | **TLS only** (raw TLS + WebSocket-over-TLS) |
| Flat-file dump, world freezes to save | **Postgres**, continuous write-behind |
| autotools + hand-rolled Dockerfile | Rootless **Podman** container |

Milestones **M0 through M8 are complete**, and so is the primitive and MPI
surface bar a handful of deliberate deferrals: 397 of 417 primitives (nine of
the twenty reported missing are a counting artefact — the compiler dispatches
them as pseudo-ops rather than registering them) and all 140 MPI functions.

What is genuinely unported: the MUF single-step debugger
(`DEBUGGER_BREAK`/`DEBUG_LINE`/`DEBUG_ON`/`DEBUG_OFF`), `SMTP_SEND`, and
`PARSEPROPEX`. See §2 for why each was left.

## 2. Next Three Steps

`internal/boolexp` (lock parsing and evaluation) is built, and lock work is
now finished end to end: `TESTLOCK`, `LOCKED?`, `GETLOCKSTR`, `SETLOCKSTR`,
`PARSELOCK`, `UNPARSELOCK`, `PRETTYLOCK`, `ARRAY_FILTER_LOCK`; the
`@lock`/`@flock`/`@chlock`/`@conlock`/`@linklock`/`@ownlock`/`@readlock`
commands (plus `@force_lock`/`@chown_lock` aliases); and exit traversal
(`useExit` in `internal/game/move.go`) now actually calls `could_doit`
instead of moving a player through any exit unconditionally, which it did
not before. See `internal/game/boolexp.go`, `internal/game/lock_cmd.go`, and
the plan at `~/.claude/plans/rippling-roaming-backus.md` for what's next.

Phase 2 of that plan (process/multitasking primitives) is underway. A design
pass mapped the whole cluster onto the existing `internal/game/proc.go`
scheduler (see the plan file for the full breakdown — PID numbering,
per-primitive design, and an implementation order). The first, lowest-risk
slice has landed: `Frame.PID`/`Frame.Supplicant` fields, and the primitives
`PID`, `ISPID?`, `FORCE_LEVEL`, `INSTANCES`, `SUPPLICANT` (`internal/muf/prim_proc.go`,
`internal/game/proc_host.go`). `CANCALL?` has landed too — it turned out to
need no process-queue work at all, just the compiler's already-existing
`Program.Publics` table exposed through a new `Host.CanCall` (see
`internal/muf/prim_proc.go` and `internal/game/proc_host.go`'s `linkable`
helper). `KILL` has landed too: `Host.ControlsProcess`/`Host.KillPID` port
upstream's `control_process`/`dequeue_process`, and killing a program's own
pid is a genuinely new interpreter case — `errSilentAbort` in
`internal/muf/value.go`, upstream's `ERROR_DIE_NOW`, an abort that skips even
an open `TRY` and produces no error report, unlike every other one. Its own
`@kill` command (`internal/game/admin.go`'s `cmdKill`) was deliberately left
alone rather than reworked to share `ControlsProcess`: upstream's `@kill` is
`do_kill_process`, a materially richer command (kills by player name, by
program dbref, or "all", not just by pid) that Emerald's `cmdKill` does not
attempt yet — porting that is its own task, not a refactor incidental to the
`KILL` primitive. The engine scheduling-granularity fix Phase 2 needed before
`FORK` has landed too: `Engine.OnEachOp` (`internal/world/engine.go`) runs a
callback after every applied operation, not just once per flush interval —
`cmd/fbemerald/commands.go` and `internal/golden/emerald.go` both wire it to
the same `Server.Tick` `OnTick` already uses, so a freshly-runnable process
gets its first instruction slice within the same op that created it rather
than waiting up to `FBE_FLUSH_INTERVAL`. It is deliberately not wired into
the test harness (`internal/game/game_test.go`'s `newHarness`): existing
sleep/process tests (`TestSleepingProgramResumes` and others) depend on
nothing resuming a suspended program except an explicit `h.s.Tick(w)` call,
and auto-draining there would make their assertions race their own setup.

`FORK` has landed too, the architecturally hard one. `Frame.fork()`
(`internal/muf/fork.go`) is a pure, independently-tested deep-copy of a
frame's whole state — stack, vars, lvars, scopes, calls, fors, trys — using
the same `deepCopy` helper `DEEP_COPY` already relies on, since Go's arrays
are ordinary shared `*Array` pointers between `Value`s until something
decouples them, unlike a plain stack `DUP`. `Host.Fork`
(`internal/game/proc_host.go`) registers the copy as an ordinary background
`process`; no special-cased scheduling was needed beyond `OnEachOp`, since a
freshly-added `procRunnable` process is already exactly what `Tick` drains.
`processLimitOK` ports `add_event`'s `max_process_limit`/`max_plyr_processes`
gate, with one documented, deliberate divergence: unlike upstream's
`tqhead`, `procQueue` also holds the process currently running in the
foreground — the one making the check — so both counts run one process
higher here than upstream's equivalent count would for the same state.

Porting `FORK` also surfaced a real bug in `internal/muf/internal/gen/gen_mlev.py`,
fixed this session: its exemption regex recognised `controls(` but not
`control_process(`, so `KILL`'s own `mlev < 3 && !control_process(...)` — a
conditional check with an ownership escape hatch, not a flat floor — had
been wrongly generated as an unconditional `"KILL": 3` in `mlev_gen.go`.
That silently blocked a mlev-1 player from killing their own suspended
program with the wrong, generic message, before `KILL`'s own ownership-aware
check ever ran. Regenerating after the fix removed the false entry;
`TestKillBelowMlevelThreeStillWorksForTheProcessesOwnPlayer`
(`internal/game/proc_host_test.go`) pins the corrected behaviour.

`QUEUE` has landed too, unlike `FORK` building its frame eagerly rather than
copying one: `Host.Queue` (`internal/game/proc_host.go`) compiles the target
program and constructs a fresh `*muf.Frame` at `QUEUE`-call time — the one
documented gap against upstream's own lazy `interp()`-at-fire-time is that an
edit to the program between queuing and firing is not picked up, since the
process already holds a compiled `*muf.Program`. The new frame's `COMMAND`
reserved variable ("Queued Event.") and its initial stack argument (whatever
string `QUEUE` was given) are genuinely different strings upstream, unlike a
command-driven program where `SetReserved`'s own convention makes them the
same one — `Host.Queue` calls `SetReserved` for the `COMMAND` value and then
overwrites the pushed stack value directly. It shares `processLimitOK` with
`FORK`, returning `0` on the same limit failure rather than `FORK`'s `-1`.

`FORCE`/`FORCEDBY`/`FORCEDBY_ARRAY` have landed too, the last of Phase 2's
process-cluster primitives before `GETPIDS`/`GETPIDINFO`/`WATCHPID`. `FORCE`
needs none of `@force` (`cmdForce`)'s own ownership-escaping checks — mlev 4
already means the calling program has full wizard authority — so it shares
only the mechanical half, `Server.force` (`internal/game/wiz.go`, refactored
to take a descriptor instead of a `*ctx` so `mufHost.Force` can call it too).
`Server.forcelist` is upstream's own single global `objnode` stack — both
`cmdForce` and `mufHost.Force` push onto and pop from the same one, which is
what lets `FORCEDBY`/`FORCEDBY_ARRAY` see who forced a program regardless of
whether `@force` or `FORCE` did it. Porting this surfaced another real
wording divergence via golden, alongside `KILL`'s from last session: `FORCE`,
`FORCEDBY` and `FORCEDBY_ARRAY` all abort mlev<4 with upstream's own "Wizbit
only primitive.", not the generic "Permission denied.  Requires Wizbit."
most other level-4 primitives get — verified only for `p_db.c`'s primitives
so far, so a level-4 primitive from another module is worth checking by hand
before assuming the generic wording is right. `gen_mlev.py`'s new
`CUSTOM_ABORT_MESSAGE` set excludes these three from the generated table so
each can check its own mlev inline with the correct string instead.

`GETPIDS` has landed too. Its own `mlev < 3` is unconditional but, like
`FORCE`'s family, has non-generic wording ("Permission denied.  Requires
Mucker Level 3.") — added to `CUSTOM_ABORT_MESSAGE`. Porting it surfaced a
real behavioural gap this time, not just wording: `Host.GetPIDs`
(`internal/game/proc_host.go`) has to exclude the calling frame's own pid
from every match, because `procQueue` holds the currently-running foreground
process (the same divergence `processLimitOK` already documents) while
upstream's timequeue never does — a naive port made `GETPIDS`'s `#-1`
"match everything" wildcard include the caller's own pid, which golden
caught upstream does not do. `GETPIDS`'s own `prim_getpids` only ever adds
the caller's pid back via one explicit final step — "if the argument is
exactly the calling program's own ref" — never through the wildcard, which
`internal/muf/prim_proc.go`'s `GETPIDS` now reproduces exactly rather than
folding into `Host.GetPIDs` itself.

`GETPIDINFO` has landed too. Its own `mlev < 3` is conditional — "unless the
pid is the caller's own" — so, like `KILL`, it stays out of the generated
table (`gen_mlev.py`'s exemption regex now also recognises `fr->pid`
comparisons as an ownership-style escape hatch, the same class of gap
`control_process` was) and is hand-checked in `internal/muf/prim_proc.go`.
The self branch reads straight off the live `*Frame`; the other-pid branch
reads a new `Host.PIDInfo` (`internal/game/proc_host.go`), this port's
equivalent of `get_pidinfo`. Some fields are deliberately reproduced upstream
quirks, not gaps: self's `CALLED_DATA` is always `""` and `NEXTRUN` always
`0`, and other-pid's `MLEVEL` is always `0` — all three hardcoded in
upstream's own C, the last its own documented `TODO`. `CPU` is always `0.0`
in both branches, the same "Emerald does not profile programs" divergence
`examine`'s "Cumulative runtime" line already documents. `TYPE` is always
`"MUF"`: unlike upstream's separate MUF/MPI timequeue and MUF-event queue,
`procQueue` holds only `muf.Frame` processes, an `EVENT_WAITFOR`-blocked one
included, so there is no second queue an other-pid lookup ever falls back to.
A new `process.calledData` field (`internal/game/proc.go`) — "READ",
"SLEEPING", "EVENT_WAITFOR", "FOREGROUND", "BACKGROUND", or a `QUEUE`'s own
arg string — backs `CALLED_DATA` for both primitives; a new `Frame.Started`
backs `STARTED`.

`WATCHPID` has landed last, closing out Phase 2. It needed the "still-missing
generic event-delivery mechanism" the plan flagged as its own task: upstream's
`fr->events` queue is now `Frame.PendingEvents`
(`internal/muf/frame.go`), with `AddEvent`/`popEvent` mirroring
`muf_event_add`/`muf_event_pop(_specific)`, and `EVENT_WAITFOR`
(`internal/muf/prim.go`) now checks for an already-queued matching event
before blocking — Emerald has no periodic scan loop equivalent to upstream's
own `muf_event_process`, so a pre-queued event (WATCHPID's own "target
already dead" case) is served synchronously instead of waiting for one.
Delivery to an *already-blocked* process runs through a new
`Server.deliverEvent` (`internal/game/proc.go`), which resumes it immediately
if its filter matches, and a new `Server.finishProcess` — upstream's
`watchpid_process`, folded into the one place every process-ending path now
funnels through (`step`'s `Done` case, `failProcess`, `killProcessesFor/Of`,
`abortForeground`, the `@kill` command, and `Host.KillPID`) — notifies every
`process.waiters` entry with `PROC.EXIT.<pid>` and unwinds `waitees`
bookkeeping on both sides. One upstream quirk not reproduced: its own
WATCHPID dedup check compares the wrong field (a stored caller pid against
the *target* pid being searched for, so it only matches by coincidence);
`Host.WatchPID` (`internal/game/proc_host.go`) dedups correctly instead,
matching what upstream's own comment says it meant to do.

Phase 2 of the remaining-work plan is now complete.

Phase 3 (`src/p_connects.c`'s 16 descriptor/connection introspection
primitives — `DESCRBOOT`, `DESCRBUFSIZE`, `DESCRFLUSH`, `DESCRHOST`,
`DESCRIDLE`, `DESCRLEASTIDLE`, `DESCRMOSTIDLE`, `DESCRNOTIFY`, `DESCRTIME`,
`DESCRUSER`, `DESCR_SETUSER`, `FIRSTDESCR`, `LASTDESCR`, `NEXTDESCR`,
`SETHEIGHT`, `SETWIDTH`) is now also complete, in `internal/muf/prim_connects.go`
and a new `internal/game/conn_host.go`. It turned out less mechanical than
the plan expected: **every mlev floor in this C file has its own wording**,
never the dispatcher's generic "Permission denied."/"Permission denied.
Requires Wizbit." — three distinct level-3 variants alone ("Mucker level 3
primitive.", "Requires Mucker Level 3.", "Requires Mucker Level 3 or
better.") plus two level-4 variants ("Primitive is a wizbit only command.",
"Requires Wizbit."). `gen_mlev.py`'s `CUSTOM_ABORT_MESSAGE` grew all 16 new
primitives plus four already-shipped ones this phase found using the wrong,
generic wording since an earlier milestone — `ONLINE`, `ONLINE_ARRAY`,
`DESCRDBREF` and `DESCRSECURE?` — fixed alongside everything new rather than
left inconsistent in the same file.

Real behavioural findings, not just wording, caught by reading the C before
porting (per `CLAUDE.md`'s own mandate):

- **`FIRSTDESCR`/`LASTDESCR`'s player-scoped branch is the opposite way
  around from their own global form.** `#-1 firstdescr`/`#-1 lastdescr`
  answer the globally oldest/newest connection (upstream's own
  `pfirstdescr`/`plastdescr`), but `<player> firstdescr` answers that
  player's *newest* connection and `<player> lastdescr` their *oldest* —
  `prim_firstdescr` reads `darr[dcount-1]`, `prim_lastdescr` reads
  `darr[0]`. Checked against the C twice before trusting it, and pinned by
  `TestFirstLastDescrPlayerScopedAsymmetry`
  (`internal/game/conn_host_test.go`).
- **`DESCRFLUSH` is genuinely stack-neutral.** `prim_descrflush` computes a
  result from `pdescrflush` but never pushes it — consumes its descriptor
  argument and returns nothing, not even on error. Pinned by
  `TestDescrFlushIsStackNeutral`.
- **`DESCRUSER`'s "username" is not an identity.** `pdescruser`/`d->username`
  is the client's own ephemeral TCP source port, parsed out of the
  `"host(port)"` string `addrout` builds for logging — a naming accident from
  when the field really did hold ident-protocol data. Emerald's transports
  discard the remote port on accept, so a live connection always answers
  `""` here; see `Host.DescrUser`'s own doc comment.
- **`DESCRBUFSIZE` has no real equivalent to report.** Upstream subtracts a
  connection's queued OS socket-buffer bytes from `tp_max_output`; Emerald's
  output channel (`internal/session.Descriptor`) has no byte-budget of its
  own, only a message-count depth, so this reports the tune parameter's own
  ceiling untouched rather than fabricating a number.
- **`SETWIDTH`'s invalid-descriptor message has no trailing period** —
  `"Invalid descriptor number (2)"` — unlike almost every other one in this
  file, which do end `. (1)`. Preserved exactly; pinned by
  `TestSetHeightReportsInvalidDescriptorWithoutAPeriod`.

`DESCR_SETUSER` (`Host.SetUser`, `internal/game/conn_host.go`) needed a new
`session.Hub.Unbind`, since upstream's own `pset_user` leaves `d->player`/
`d->connected` in an ambiguous half-state when told to set a descriptor to
`NOTHING` (the `if (who != NOTHING)` branch that would update them is simply
skipped) — read as unfinished state management rather than a deliberate
no-op, so Emerald instead returns the descriptor to a clean pre-login state
in that case. `DESCRBOOT` (`Host.DescrBoot`) mirrors `Server.Disconnect`'s
own body directly rather than calling it, since `Disconnect` re-enters the
world goroutine through `Engine.Go` for a transport reporting an
already-closed connection — not for ending one from inside a handler that is
already running on that goroutine.

Golden-verified: a new `"connects"` case in `golden_test.go` (single
connection — shape and argument-validation wording only, since
`DESCRIDLE`/`DESCRTIME`/`DESCRBUFSIZE`'s own successful-path values are
wall-clock- or OS-buffer-dependent) plus a new dedicated `connects_test.go`
for `DESCRHOST`/`DESCRUSER`, which need mlevel 4 the way `FORCE`'s own
dedicated fixture did.

Phase 4 landed most of what it scoped, but not all of it — see below for
exactly what and why. Confirmed first, per the plan's own note: `CALL`,
`CATCH`, `CATCH_DETAILED`, `EVENT_WAITFOR`, `EXIT`, `JMP`, `READ` and
`SLEEP` are coverage-test false positives, dispatched as compiler
pseudo-ops in `internal/muf/prim.go`'s own `primitive()` switch rather than
registered in the `prims` map — and so, it turns out, is `EXECUTE`, which
the plan's own note missed. `INTERP`, despite the name similarity, is a
real, separate, still-missing primitive (a synchronous sub-invocation of
another program) — see below.

**Landed** (35 primitives, `go test -run TestPrimitiveCoverage` now reads
380/417): `p_strings.c`'s `STOD`, `OTELL`, `PRONOUN_SUB`, `STRENCRYPT`,
`STRDECRYPT`, `TEXTATTR`, `POSE-SEPARATOR?`; `p_misc.c`'s `SYSPARM`,
`SETSYSPARM`, `SYSPARM_ARRAY`, `EVENT_COUNT`, `EVENT_EXISTS`,
`EXT-NAME-OK?`, `READ_WANTS_BLANKS`, `READ_WANTS_NO_BLANKS`, `IGNORING?`,
`IGNORE_ADD`, `IGNORE_DEL`, `CONVTIME`, `STATS`, `STATS_ARRAY`, `USERLOG`;
`p_array.c`'s `ARRAY_INSERTRANGE`, `ARRAY_SORT_INDEXED`,
`ARRAY_PUT_PROPVALS`, `ARRAY_GET_IGNORELIST`, `ARRAY_INTERPRET`,
`ARRAY_NOTIFY_SECURE`; `p_props.c`'s `BLESSPROP`, `UNBLESSPROP`,
`BLESSED?`, `PROP-NAME-OK?`, `PARSEMPI`, `PARSEMPIBLESSED`,
`ARRAY_FILTER_PROP`; `p_db.c`'s `COMPILE`, `COMPILED?`, `UNCOMPILE`,
`PROGRAM_GETLINES`, `NEXTENTRANCE`.

Real findings, not just wording, golden caught along the way:

- **`FIRSTDESCR`/`LASTDESCR`'s player-scoped/global asymmetry** was already
  documented in Phase 3; this phase found the same *class* of upstream
  doc-comment-vs-code mismatch repeatedly enough to be worth naming as a
  pattern: `tune_parms_array`'s and `muf_event_exists`'s own "smatch
  pattern" doc comments, both actually exact `equalstr` matches;
  `STATS_ARRAY`'s doc comment ordering ("players, programs") the reverse of
  what the code actually builds ("program, player"); `COPYOBJ`'s doc
  comment claiming a clone's value resets to 0 when the code clamps and
  copies it instead. None of these were assumed from the comment — each was
  checked against the code once read, the same discipline that caught
  `GETPIDS`'s wildcard gap last phase.
- **A test bug, not an implementation bug, on the first pass of two unit
  tests** for the new `pronounSub`/`strEncrypt`/`strDecrypt` helpers: an
  expected capitalization result written backwards against upstream's own
  `isupper(prn[1])` rule, and a round-trip case using control-byte input
  outside the cipher's own documented "visible ASCII" range. Both were the
  test's own expectation being wrong, caught by writing the test and
  reading the C again to check the failure rather than "fixing" working
  code to match a guess.
- **Golden caught two real primitive bugs before this session considered
  them done**: `ARRAY_INSERTRANGE` and `ARRAY_SORT_INDEXED` used generic
  stack-helper error messages (`"Argument not an array."`) instead of
  upstream's own argument-numbered wording (`"Argument not an array. (3)"`)
  — using `f.popArray()`/`f.popInt()` where upstream's own message needed a
  number those helpers don't carry.

Phase 4's remainder then landed too, taking coverage to **397 of 417**. Of
the 20 the coverage test still reports, nine are the pseudo-op false
positives named above; six are genuinely unported (below).

**Also landed** (17 further primitives): `INTERP` (`p_stack.c`);
`GETSEED`/`SETSEED` (`p_math.c`); `FMTTIME`, `TIMER_START`, `TIMER_STOP`,
`EVENT_SEND` (`p_misc.c`); `ARRAY_FILTER_FLAGS` (`p_array.c`); `FINDNEXT`,
`NEWPLAYER`, `COPYPLAYER`, `TOADPLAYER`, `PNAME_HISTORY`, `COPYOBJ`,
`PROGRAM_SETLINES`, `DUMP` (`p_db.c`); `ARRAY_FMTSTRINGS` (`p_strings.c`).

What each needed, and what it found:

- **`checkflags`/`init_checkflags`** is now `internal/muf/checkflags.go`,
  which both `ARRAY_FILTER_FLAGS` and `FINDNEXT` compile their flag string
  with. `@find` could use it too, unchanged, if it is ever built.
  Upstream's two size tests (`~` and `^`) are parsed and then ignored: they
  compare against `size_object`, and Emerald's objects are laid out nothing
  like the C's — the same reason `examine`'s own "Memory used" line is
  masked in its golden case.
- **The seeded generator** turned out to be exactly reproducible, quirk
  included. Upstream's `rnd()` hashes `sizeof(digest)` bytes where `digest`
  is a `uint32*`, so it feeds MD5 the first **8** bytes of its 16-byte
  buffer rather than all of it. Reproducing that is what makes a recorded
  seed replay the same sequence on both servers, which the golden case now
  checks by printing actual `SRAND` values rather than just comparing them
  to each other.
- **`FMTSTRING` was quietly wrong**, and porting `ARRAY_FMTSTRINGS` is what
  surfaced it. The two are one grammar with two ways of reaching an
  argument, so they now share `internal/muf/fmtstring.go` — and the old
  implementation had `%d` and `%D` backwards (upstream's `%d` is a dbref as
  `#123`, `%D` is its *name*; Emerald had `%d` as an integer and `%D` as a
  dbref), and supported none of `|` centring, `+`/space sign padding, `0`
  padding, `*` dynamic widths, `~`, `?`, `l`, or `\t` tab stops. The
  starter world's own `25.m` uses `%d` and `%D` upstream's way, so this was
  a live bug against shipped content, not a hypothetical one.
- **`FMTTIME`** parses rather than formats, its name notwithstanding — it
  is `time_string_to_seconds`, C's `strptime` under a caller-supplied
  format. `internal/muf/strptime.go` is that parser, and `CONVTIME` now
  goes through it too instead of keeping its own one-format copy.
- **Timers** fit the existing scheduler without new machinery: a
  `process.timers` list, a pass in `Server.Tick`, and delivery through the
  `deliverEvent` path `WATCHPID` already built. `EVENT_SEND` needed only a
  way to reach another live process's frame.
- **`COPYPLAYER` reproduces two upstream results that look like mistakes.**
  `copy_properties_onto` *replaces* the destination's property tree rather
  than merging into it, so the new player loses the `created_as` and
  starting pennies `create_player` had just given them; the value
  arithmetic that follows then reads back the value it just copied, so a
  copy ends up with twice the source's pennies rather than the source's
  plus its own. Both are reproduced, because a program written against the
  real server sees them.
- **`PNAME_HISTORY` needed the history to exist first.** Nothing recorded
  it — `World.Rename` now does, upstream's `change_player_name`
  bookkeeping, expiring entries past `pname_history_threshold`.
- **The mlev generator missed `prim_dump` entirely**, because its function
  regex anchored on a line starting with `prim_`, and `prim_dump` is the
  one primitive in `p_*.c` whose `void` sits on the same line. Widened;
  `DUMP` is now gated like every other wizard primitive.

Genuinely deferred, not attempted, each needing more than a primitive port
on its own:

- **The MUF single-step debugger** — `DEBUGGER_BREAK`, `DEBUG_LINE`,
  `DEBUG_ON`, `DEBUG_OFF` (`p_misc.c`) — unimplemented in Emerald entirely,
  not a gap specific to these four.
- **`SMTP_SEND`** (`p_misc.c`) — a real SMTP client.
- **`PARSEPROPEX`** (`p_props.c`) — converts a whole caller-supplied
  dictionary into MPI variables and hands one back; materially more
  plumbing than `PARSEMPI`, which is ported. Natural to pick up alongside
  the MPI work rather than before it.

One divergence found but not fixed, outside this phase's own scope: piping
`TEXTATTR`'s ANSI-coded output to a live connection showed real Fuzzball
suppressing the escape codes entirely while Emerald sent them raw — likely
upstream gates color on whatever capability a client negotiated, which nothing
in Emerald's own `NOTIFY` path currently checks either. Not specific to
`TEXTATTR`; worth its own look before assuming any primitive's ANSI output is
byte-for-byte comparable over a real connection.

## Phase 5 — MPI, finished

**All 140 MPI functions are implemented**, from 51 when the phase started.
`internal/mpi/coverage_test.go` tracks it, the way `internal/muf`'s own
coverage test tracks primitives; it was written first, before any porting,
and is what made the gap legible.

The work split four ways: the list functions (`internal/mpi/list.go` plus
`impl_list.go`), the property ones (`impl_prop.go`), the looping and
evaluating ones (`impl_control.go`, with `macro.go` for what `{func}`
defines), and the object, time and world ones (`impl_object.go`,
`impl_misc.go`, with `internal/game/mpi_host.go` behind them).

Golden found five real bugs, four of them pre-existing rather than newly
written:

- **`{prop}` never walked the environment.** It is `safegetprop` upstream,
  not the strict form, so a description reading a property set on the room
  found nothing. `{prop!}` is the form that looks only at the object named,
  and it is the one Emerald had.
- **`{set}` returned the empty string** instead of the value it assigned, so
  `{set:n,5}{&n}` read `5` where upstream reads `55`.
- **`FLAG?` matched flag names the wrong way, in MUF as well as MPI.**
  Upstream's `str_to_flag` matches a *prefix* of a full name against a fixed
  ordered list — so `m` is MUCKER and `d` is DARK — rather than looking up
  exact names and hand-picked single letters, which is what
  `internal/ref/flagnames.go` used to do. It also knows only flags: a type
  name or mucker level is not one, so `{flag?:me,player}` is false even for
  a player, where Emerald said true. Rewritten as a real port of
  `has_flag`/`str_to_flag`, which both languages now share.
- **`PRONOUN_SUB` produced nothing for an object with no gender set**, where
  upstream substitutes the object's *name* — `%s` becomes "Igor", `%p`
  becomes "Igor's". `%n` was missing outright. Both were part of the
  "deliberately not reproduced" list from an earlier phase, and both turned
  out to be the common case rather than an edge one.
- **`{ltimestr}` counted in four units and said "0 seconds" for nothing.**
  Upstream's `timestr_long` counts in seven, down to years, and renders a
  zero duration as the empty string.

Three shared pieces came out of the overlap rather than being duplicated:
`internal/timefmt` (strftime and strptime, previously private to
`internal/muf`), `internal/ansi` (the attribute tags TEXTATTR and `{attr}`
both take), and `ascii.AlphanumCompare` (upstream's `alphanum_compare`,
which `{lsort}` needs and whose zero-backtracking quirks are checked against
values taken from the C rather than from what the ordering ought to be).

Deliberately simplified, and worth knowing before trusting either:

- **`{debug}` and `{debugif}` evaluate plainly.** Upstream runs its MPI
  tracer for them, printing each call and its result; Emerald has no tracer,
  so the text they produce is right and the diagnostics are absent.
- **Match permissions are not layered.** Upstream has three matchers —
  `mesg_dbref`, `mesg_dbref_local` and `mesg_dbref_raw` — differing in what
  an unblessed message may resolve. Emerald has one, so a few functions will
  resolve an object upstream would refuse.

The next steps from here are:

## Phase 6 — M8, finished

**M0 through M8 are complete.** What each item turned out to need:

- **Connection limits** are `internal/admit`, refusing at accept time — before
  the TLS handshake, before a descriptor exists, before the world goroutine
  hears about it. A total cap, a per-host cap and a per-host connection rate,
  all `FBE_*` environment settings rather than `@tune` parameters for the same
  reason TLS is: a server under a flood has to keep refusing while the
  database is unreachable. Upstream has no equivalent at all — every Fuzzball
  limit sits after authentication, which is too late against a peer that never
  authenticates. The one trap worth knowing is in the rate limiter: a refused
  attempt must *not* be recorded, or a peer that keeps retrying holds its own
  window open and stays shut out forever. `TestRefusalDoesNotExtendTheWindow`
  pins that.
- **The command spam limiter turned out to be upstream's, already specified.**
  `command_burst_size`, `commands_per_time` and `command_time_msec` were
  already in the generated `@tune` table, unused. `session.Quota` is
  `d->quota`, refilled by `Server.refillQuotas` on the tick, exactly
  `update_quotas`. The architectural point: spending the allowance must block
  the **transport's** goroutine, not the world's — `Server.Input` waits before
  enqueuing, so one noisy connection is held up and nobody else is. Nothing is
  dropped, which is what upstream achieves by leaving the line on its input
  queue. MCP messages are exempt, and a player in the editor or on a `READ` is
  refilled eight times as fast (upstream's `INTERACTIVE` case).
- **`playermax`/`playermax_limit` were likewise already in the table** and
  unused. Checked after the password verifies, exempting a true wizard, with
  the warning shown at the welcome screen — all upstream's. The count is of
  connections that *finished logging in*, which is `con_players_curr`, not of
  sockets and not of distinct players; a test pins that, because it is the
  kind of thing that looks like an off-by-one later.
- **`pprof`** is `FBE_PPROF_ADDR`, and `Config.Validate` refuses anything that
  is not loopback. A bare `:6060` binds every interface, which is exactly the
  mistake worth failing at startup rather than shipping — the handlers hand
  out goroutine stacks and heap contents.
- **The audit trail** is `logging.Security`, the one channel with no
  `file_log_*` ancestor. Upstream scattered these through its status log,
  where a failed login sat between a flush report and a compile warning.
  Authentication, password changes, and every privileged command or refusal of
  one now land there.
- **Graceful drain was half-adequate, as the plan guessed — but only half.**
  The world side was fine: `Engine.shutdown` already drained the op queue and
  made a final flush with a 30-second budget. What was missing is that nobody
  connected was *told*. Fixing it needed a real change of shape: the world's
  context is no longer derived from the signal context, because deriving it
  cancels both at once and races the farewell against the drain that makes
  sending impossible. A signal now announces, then stops, in that order.
  Verified by hand against a running server with connections held open.
- **Backup and restore** were genuinely undocumented; `README.md` now covers
  `pg_dump`/`pg_restore`, and the two things that decide a schedule: a crash
  loses at most `FBE_FLUSH_INTERVAL` so backups are for mistakes rather than
  crashes, and a backup restored under a running server is ignored until it
  restarts, because the in-memory graph is authoritative.
- **The README divergences section** was already done, as the plan said.

Everything here was checked against a running server as well as by test:
pprof reachable on loopback and not on the host's own address, the per-host
cap refusing before the handshake, and `SIGTERM` delivering
`## The server is shutting down. ##` to held connections before exiting
cleanly.

## 3. Project State

### 3.1 Key Logic

- **Single world goroutine** (`internal/world.Engine`) owns every object.
  Everything in `internal/game` runs on it. Connections/transports enqueue
  work via `Engine.Go`; `Engine.Do` waits for completion and must never be
  called from inside another `Do`. This exists because Fuzzball's MUF
  semantics assume single-threaded, non-atomic mutation and cooperative
  multitasking — this is the single most load-bearing architectural fact in
  the codebase.
- **Write-behind persistence**: reads never touch Postgres after boot.
  Mutations mark objects dirty; every `FBE_FLUSH_INTERVAL` the world
  goroutine deep-copies the dirty set into a `Snapshot` and hands it to
  `internal/store` for one transaction. `@dump` is just a forced flush, not
  a freeze. A `Snapshot` also carries changed program source (from the MUF
  editor) and the whole macro table when either changed — these live apart
  from the object graph.
- **Containment chains** (contents/exits linked lists) are stored in
  Fuzzball's original insertion order (MUF can observe it), with each
  object's `Location` field as a redundant cross-check that lets
  `World.RepairChains` rebuild any chain that disagrees — comparing
  *membership*, not order, since reordering a healthy chain every boot would
  itself be a bug.
- **The golden-output harness** (`internal/golden`) is the actual oracle for
  correctness, not the C source. It builds a database holding a MUF snippet
  or command script, runs it through a real Fuzzball 7 (built from the
  `fuzzball/` submodule, run in a Podman container over TCP) and through
  this server (in-process), and diffs the transcripts line-for-line. This
  has found real divergences on nearly every milestone that unit tests
  written from reading the same C source had missed. Two driving modes:
  marker-bounded (`RunOracle`, fast, default) and quiet-bounded
  (`RunOracleQuiet`, slower, used only when a marker pose would itself be
  consumed as input — e.g. inside the MUF editor, or a `READ`).
- **A program runs at the *lower* of its own mucker level and its owner's**
  (`find_mlev` semantics) — a wizard with no mucker bits caps every program
  it owns at level 0. This was independently re-derived and then verified
  against the C, and it matched.
- **MCP frame is the one piece of state reached from two goroutines** (a
  connection's own, and the world's) — it has its own mutex, and the lock
  is released before calling a package handler, because holding it across a
  callback deadlocks a handler that tries to reply to its own message (this
  was a real bug caught by a test).

### 3.2 Architecture

```
cmd/fbemerald/          — the binary: serve, import, migrate, hashpw subcommands
internal/ref/           — dbrefs (Ref), object types, the flag word
internal/props/         — per-object property trees (case-insensitive, blessed)
internal/ascii/         — ASCII-only case folding + SMatch (MUCK wildcard match)
internal/world/         — Object, World, Engine (the world goroutine), Snapshot,
                            RepairChains, sanity checking (Check/Fix)
internal/store/         — GORM models, persister, loader (Postgres)
internal/importer/      — Foxen9 .db parser, muf/*.m loader, macro table
internal/tune/          — ~161 @tune params (params_gen.go, generated)
internal/match/         — name resolution: exits, aliases, environment walk,
                            $registered names, priority
internal/session/       — Descriptor, Hub, telnet codec, MCP frame attachment
internal/mcp/           — MCP 2.1 protocol: framing, negotiation, GUI dialogs
internal/muf/           — instruction set, VM/interpreter, ~380 primitives
internal/muf/compiler/  — the MUF compiler (lexer + compile.c port)
internal/mpi/           — MPI parser + ~51 mfn_* functions (generated table)
internal/boolexp/       — lock expressions: parse_boolexp/eval_boolexp/
                            unparse_boolexp, boolexp.c ported directly
internal/game/          — login, command dispatch, all @-commands, MUF/MPI
                            hosts, process queue, MUF editor, examine, sanity
                            commands, MCP host wiring
internal/golden/        — the differential test harness (THE oracle)
internal/transport/     — tlsline (raw TLS) and wss (WebSocket) listeners
internal/password/      — Argon2id + legacy MD5/PBKDF2 verify-and-upgrade
internal/logging/       — structured slog wrappers
fuzzball/                — git submodule, pinned to Fuzzball v7.2.1 (READ-ONLY
                            C reference; never edit, never merge into vmother)
deploy/                  — Containerfile, compose.yaml, golden oracle Containerfile
```

Four **code generators** (run via `go generate`, or `make generate`) read
the `fuzzball/` submodule directly and must never be hand-edited in their
generated output:
- `internal/tune/internal/gen/gen_params.py` → `internal/tune/params_gen.go`
- `internal/muf/internal/gen/gen_prims.py` → `internal/muf/prims_gen.go`,
  `defs_gen.go`
- `internal/muf/internal/gen/gen_mlev.py` → `internal/muf/mlev_gen.go`
  (mucker-level floors — approximate extraction, see §3.3)
- `internal/mpi/internal/gen/gen_funcs.py` → `internal/mpi/funcs_gen.go`

### 3.3 Decisions

Departures from the plan or from naive C-to-Go translation, made
deliberately and recorded so they aren't "fixed" back by accident:

- **The C reference moved from a git branch (`origin/mother`) to a
  submodule** at `fuzzball/`, pinned to a release tag (v7.2.1) rather than a
  moving branch head. All four generators and `make golden-build` read from
  this path. Regenerating after the move produced byte-identical output to
  what the branch held.
- **`@sanity`/`@sanfix`/`@sanchange` check the in-memory graph, not
  Postgres**, though the original plan said "Postgres-side checks". The
  in-memory graph is authoritative; Postgres is a write-behind copy, so a
  check against the copy could pass while the live world was broken.
- **The sanity repair algorithm is simpler than upstream's** because
  Emerald stores each object's `Location` redundantly (see §3.1): `Fix`
  corrects every object's own fields first, then rebuilds chains from
  locations — upstream must cut a damaged chain and hunt for what fell out
  of it, because the chain is its *only* record of placement.
- **`ref.Ref`'s zero value is `#0`** (global environment), not `NOTHING`
  (`#-1`). This bit the importer once already (silently attached programs
  to the world root) — any new struct holding a `Ref` must initialise it
  explicitly.
- **`@tune` parameter names are a runtime string API**, not just labels —
  `SYSPARM`/`SETSYSPARM` look them up by string at runtime with no
  compile-time check, so renaming one silently breaks third-party MUF. Only
  the generator script may be edited, never `params_gen.go` directly.
- **`internal/muf/mlev_gen.go`'s extraction is approximate**: it only
  records *unconditional* mucker-level floors from the C's `mlev < N`
  checks. A check qualified by ownership/permissions/flags/type (e.g.
  `(mlev < 4) && !permissions(...)`, meaning "wizard OR owner") is correctly
  *not* treated as a floor — treating it as one would refuse the owner. Any
  primitive behaving oddly at low mucker level should be checked against
  the C by hand.
- **MCP/MCP-GUI's mucker floor (`mcp_muf_mlev`) is a `@tune` param, not a
  constant**, so it can't go in the generated mlev table — it's applied by
  hand in `checkMCPPerm`, replicating upstream's exemption for a program
  run by its own owner, and upstream's *non*-gating of
  `GUI_CTRL_COMMAND`/`GUI_AVAILABLE`/`MCP_SUPPORTS`.
- **Case folding is ASCII-only** (`internal/ascii`), never
  `strings.EqualFold`/`ToLower` — upstream's `strcasecmp` folds only A–Z, so
  `Ä`/`ä` are distinct player/property names. `smatch`/`SMatch` (MUCK
  wildcard matching) lives in `internal/ascii` too, not `internal/muf`,
  since `examine`, MPI, and MUF all need it.
- **`@action` currently aliases `@open`**, which is *not* upstream's
  behaviour (`do_action` attaches to a named object, not the room) — a
  known, flagged, temporary shortcut pending a real port.
- **Building costs money now** (`@create`/`@dig`/`@open` charge
  `object_cost`/`room_cost`/`exit_cost`; linking charges again; a created
  thing is endowed `(cost-5)/5`) — this was missing until the `examine`
  format-matching pass surfaced it as the reason `Value:` never matched.
- **Deliberate divergences kept from the plan**: TLS-only (no cleartext, no
  STARTTLS — `ssl_*`/`starttls_allow` tune params removed, replaced by
  `FBE_TLS_*` env vars); Argon2id passwords (legacy MD5/PBKDF2
  verified-then-upgraded in place; empty stored password is refused, not
  accepted; hash comparison is constant-time, not leading-bytes-only); the
  `dump_*`/`diskbase_propvals` tune params are inert (read/write but do
  nothing); new Go code says "TLS" never "SSL" **except** where it would
  break a compatibility surface (`@tune` names, `DESCRSECURE?`,
  `NOTIFY_SECURE`, `ARRAY_NOTIFY_SECURE` keep their names, change meaning).
- **Two upstream bugs are intentionally reproduced** because they're
  program-visible and transcripts are compared against them: the
  unclosed-parenthesis in MUF error backtraces, and `@sanchange`'s report
  of an "exits" field change as a "next" field change (upstream copy-paste
  slip). **One upstream bug is intentionally *not* reproduced**:
  `unparse_flags`'s NUL-terminator read for garbage objects, since it
  produces corrupt output no program could sanely depend on. Also *not*
  reproduced: `@sanchange`'s report of a malformed dbref, which echoes
  whatever was left in an uninitialized C stack variable from the previous
  command — undefined behaviour, not a contract.
- **`Descriptor.Send` auto-quotes text that would look like an MCP
  message** (`#$#...`) so a player can't drive another player's client by
  typing the prefix in speech; `sendRaw` is the unquoted path MCP messages
  themselves use.
- **A lock property holds its unparsed boolean expression string, not a
  cached compiled tree**, unlike upstream, which parses once at set-time and
  caches the result. `internal/boolexp.Parse` always takes the disk-loader
  path (`dbload=true`, trusting `#123` dbrefs, no name matching) to re-parse
  a stored lock at each use, which is valid because the stored form is
  always already in that dbref form — `Unparse` with `fullname=false`
  produces it, and that is the only thing that ever writes a lock property.
  Name matching (`dbload=false`) happens once, when a lock is *set* from
  player input (`@lock`, `SETLOCKSTR`, `PARSELOCK`).
- **`ProgUID`/`find_uid` is only approximated** (`progUID` in
  `internal/muf/prim_lock.go`): the dominant `REGUID` path (a program below
  mucker level 2 runs as its own owner; at or above, as whoever is running
  it) is covered, but `STICKY`/`HAVEN`/`SETUID`/`HARDUID` are not, since
  those need a `fr->perms`/caller-stack concept nothing in this codebase has
  built yet. Anything that reads `progUID` should be treated as
  approximately right, not exactly.
- **One upstream bug is reproduced in `LOCKED?`**: its player/thing argument
  check is written `!= TYPE_PLAYER && == TYPE_THING` in Fuzzball 7.2.1,
  which rejects a `THING` rather than allowing it as the primitive's own doc
  comment claims. Kept deliberately, like the other reproduced bugs above.

### 3.4 Other

- **Fixture password**: the starter world's `#1` is `potrzebie`, from the
  upstream README — a fixture, not a secret.
- **God-only commands** (`@sanity`, `@sanfix`, `@sanchange`) are the *only*
  `@`-commands that cannot be reached by abbreviation — upstream compares
  them with `strcmp` specifically because they can rewrite ownership.
  They're also refused from inside a `@force`.
- **Command precedence is load-bearing** and depends on the starter world:
  `QUIT`/`WHO` are checked case-sensitively before anything else; exits are
  matched before *any* built-in including `@`-commands (the starter world
  ships a lowercase `quit` exit whose whole job is telling players to
  capitalize it, and defines `@view`/`@tel`/`@stats` exits that would
  otherwise be shadowed). A wizard's `!` prefix skips exit matching.
- **Store tests must never share a database with a real world** — the
  scratch schema must be embedded in the connection string itself (not set
  via `SET search_path`), because GORM's connection pool means the SET
  reaches only one pooled connection and every other query silently lands
  in `public`. This destroyed the user's real imported world once, before
  the fix. A separate `fbemerald_test` database is now used, with a
  16-probe isolation assertion at the top of every store test.
- **A GPG signing timeout occurred at least twice** during this session
  (pinentry issue, not a code problem) — commits were retried after the
  user re-triggered signing successfully.

## 4. Dependency Map

**External Go modules** (see `go.mod`):
- `golang.org/x/crypto` — Argon2id
- `gorm.io/gorm` + `gorm.io/driver/postgres` (+ transitive `jackc/pgx`,
  `pgpassfile`, `pgservicefile`, `puddle`) — the persistence layer
- `github.com/coder/websocket` — the WSS transport
- (transitive only) `golang.org/x/sync`, `golang.org/x/sys`,
  `golang.org/x/text`, `jinzhu/inflection`, `jinzhu/now`

**External non-Go dependency**: Podman (rootless), for both the deployment
container (`deploy/Containerfile`) and the golden-harness oracle container
(built from `fuzzball/` via `deploy/golden/Containerfile.fbmuck`).

**Internal package dependency shape** (high level, arrows = "depends on"):

```
cmd/fbemerald → game → { world, store, session, mcp, muf, mpi, boolexp, importer, tune }
game → muf/compiler, muf, mpi, boolexp, match, ascii, props, ref
muf → ascii, ref, props, boolexp   (muf/compiler → muf; boolexp for TypeLock's payload)
mpi → ascii, ref
boolexp → ref, props   (pure; no world/game dependency, like mpi)
world → ref, props, tune, ascii
store → world, ref, props   (GORM/Postgres)
importer → world, ref, props, store
session → mcp, ref
mcp → ascii   (no world/game dependency — pure protocol)
match → world, ref, ascii, props
golden → game, world, importer, store   (test-only; drives a real oracle container)
```

`fuzzball/` (the submodule) is a leaf that only the generator scripts and
the golden harness's `make golden-build` step read — no Go package imports
it.

## 5. Version Log

Most recent commits first (see `git log` for full history back to `M0`):

| Commit | Summary |
|---|---|
| `29b99ed` | Match `examine`'s output to Fuzzball's exactly (heading by type, flags line, locks/messages, timestamps, property-listing form); surfaced and fixed: examine was compiling programs just to check compiledness, building was free (now charges `object_cost`/`room_cost`/`exit_cost`), `@dig`'s parent-argument failure handling was silently swallowed. Moved `smatch`→`ascii.SMatch`. |
| `70c1bd8` | MCP-GUI package + the 14 MCP/GUI MUF primitives (finishes M7). Dialog state lives in the dialog registry, not the opening program, since the client can mutate it while the program is suspended. `mcp_muf_mlev` permission gate. |
| `e6b6763` | MCP 2.1 framing: negotiation, authentication key, message framing/parsing, quoting of in-band text that looks like a message. Frame mutex + release-before-callback bug fix. |
| `f0f4603` | `@sanity`/`@sanfix`/`@sanchange` — in-memory (not Postgres) consistency checking and repair. |
| `418cd39` | `@force`, `@toad`, `@boot`, `@stats`, `@pcreate`. |
| `347d47b` | The MUF editor (`@program`/`@edit`, insert mode, `def`, line listing/deletion). |
| `04d6aeb` | Switched the C reference from `origin/mother` branch to the `fuzzball/` submodule for all generators + golden harness. |
| `870c71e` | Added Fuzzball v7.2.1 as a git submodule at `fuzzball/`. |
| `3293468` | The process queue — `READ`/`SLEEP`/`EVENT_WAITFOR` suspension, `@ps`/`@kill`. |
| `9f921af` | M6: MPI evaluated in descriptions and exit messages. |
| `c4d2b3c`…`90f68f3` | M5: MUF primitives ported in batches (192→217→268→286→301), each checked against the golden oracle; mucker-level gating added. |
| `5b95704` | Golden-output harness built; MUF wired into the server. |
| `bcd8df7`, `e3b13d9` | M4: MUF lexer, compiler, interpreter. |
| `07d9919` | M3: TLS + WebSocket transports, login, basic commands. |
| `b1cd2dd` | M2: Foxen9 legacy `.db` importer, password verification. |
| `fe60598` | M1: object model, property trees, world goroutine, Postgres persistence. |

Working branch: `vmother` (deliberately started empty). Main/PR-target
branch: `mother` (holds the C reference on other checkouts of this repo —
**never merge `vmother` into it**, and never merge `fuzzball/`-adjacent
branches like `origin/upstream_master`/`origin/development` into `vmother`
either; they're read-only reference).

## 6. Testing Status

**All green as of the lock work** (`internal/boolexp`, the `TESTLOCK`
through `ARRAY_FILTER_LOCK` primitives, the `@lock` family, and exit-lock
enforcement — see `git log` for the exact commits):
- `go vet ./...` — clean
- `gofmt -l .` (excluding `fuzzball/`) — clean
- `make test` (`go test -race ./...` against a scratch Postgres schema) —
  clean, no data races
- `make golden` (the full differential suite against a live Fuzzball 7
  container) — all suites passing, including:
  - `TestAgainstFuzzball` (the general MUF/MPI primitive corpus)
  - `TestEditorMatchesFuzzball` (~110-command MUF editor session,
    quiet-driven)
  - `TestExamineMatchesFuzzball` (all 5 object types + property listing)
  - `TestMCPNegotiationMatchesFuzzball`
  - `TestSanityMatchesFuzzball`
  - `TestWizardCommandsMatchFuzzball`
  - `TestLockCommandsMatchFuzzball` (the `@lock` family, an exit whose
    `@lock` actually gates it)
  - the `"proc"` case in `TestAgainstFuzzball` (`PID`, `ISPID?`,
    `FORCE_LEVEL`, `INSTANCES`, `SUPPLICANT`, `CANCALL?`, `KILL`,
    `GETPIDS`), the `"fork"` case (parent/child independence) and the
    `"queue"` case (COMMAND vs. stack argument)
  - `TestForceMatchesFuzzball` (`FORCE`/`FORCEDBY`/`FORCEDBY_ARRAY`, a
    self-forcing program)
- Primitive coverage: **397 of 417** implemented
- MPI coverage: **140 of 140** implemented
  (`go test -run TestPrimitiveCoverage -v ./internal/muf/`)
- MPI coverage: **~51 of 140** functions (no dedicated coverage test exists
  for this yet — worth adding one analogous to `TestPrimitiveCoverage`)

**Known test-infrastructure quirks** (not bugs, just easy to trip over):
- `internal/store` tests need `FBE_TEST_DATABASE_URL` set or they skip
  silently — see the exact env var and value in CLAUDE.md's Commands
  section.
- The golden harness needs `make golden-build` run once (builds a Fuzzball
  7 Podman image from the `fuzzball/` submodule) before `make golden` will
  do anything but fail with a clear "oracle not built" message.
- A reading program in the MUF editor / on `READ` will eat a golden
  harness's own marker line if driven with the default marker-based runner
  — those cases use `RunOracleQuiet` instead (see CLAUDE.md's "golden-output
  harness" section for the exact mechanism and why).
