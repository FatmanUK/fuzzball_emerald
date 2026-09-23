# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A from-scratch Go reimplementation of [Fuzzball MUCK](https://www.fuzzball.org/)
7, aiming for behaviour compatibility — existing MUF programs, MPI descriptions
and `.db` worlds should work unchanged — with four deliberate departures: Go
instead of C, TLS-only networking, Postgres instead of flat-file dumps, and a
rootless Podman container instead of autotools.

The implementation plan, including the milestone breakdown, is at
`~/.claude/plans/i-want-to-create-hashed-moore.md`.

## The C reference is a submodule

This is the single most useful thing to know. The repository itself contains
only Go; the ~110k lines of Fuzzball C that everything is ported from are the
upstream project, vendored as a submodule at `fuzzball/` and pinned to a
release tag:

```bash
git submodule update --init fuzzball   # if fuzzball/ is empty
sed -n '1,80p' fuzzball/src/interp.c
grep -rn "PRIM_STRCMP" fuzzball/src/
git -C fuzzball describe --tags        # which upstream this is
```

It is read-only reference. Nothing in `fuzzball/` is ever edited, and the
submodule is moved to a new upstream release deliberately, not incidentally —
bumping it can change what the generators emit and what the golden oracle
compares against. The four code generators and `make golden-build` all read it
from this path.

**Check the C before implementing anything that claims to match upstream.**
Several behaviours in this codebase look like bugs until you read the original,
and several plausible-looking assumptions turned out to be wrong when checked.

## Commands

```bash
make                  # list every target
make build            # build ./fbemerald
make test             # go test -race ./... against a scratch database
make check            # vet + test
make pod-import pod-run   # build image, import starter world, serve in a container
make connect          # openssl s_client to the running server
make pod-logs         # follow the container
make pod-stop         # stop it

make golden-build     # build Fuzzball 7 from the C, once
make golden           # diff this server against it
```

Running a single test needs the database URL only for `internal/store`, which
skips without it:

```bash
go test -run TestRoundTrip -v ./internal/store/
FBE_TEST_DATABASE_URL="postgres://fbemerald:fbemerald@localhost:55432/fbemerald_test?sslmode=disable" \
  go test -run TestRoundTrip -v ./internal/store/
```

Other packages need nothing:

```bash
go test -run TestExitPriorityBeatsProximity -v ./internal/match/
```

The starter world's `#1` password is `potrzebie`, documented in the upstream
README, so it is a fixture rather than a secret.

## Architecture

### One goroutine owns the world

Fuzzball is single-threaded and MUF depends on it: primitives mutate the object
graph non-atomically and multitasking is cooperative, yielding at
instruction-count slices. `internal/world.Engine` keeps exactly one goroutine
that owns every object; connections and the persister talk to it over channels.

Everything in `internal/game` runs on that goroutine. Transports call
`Server.Connect`, `Server.Input` and `Server.Disconnect` from their own
goroutines, and those only enqueue work via `Engine.Go`. Anything slow — DNS,
Postgres, TLS handshakes — must happen off the world goroutine and post results
back. `Engine.Do` waits for completion and must never be called from inside
another `Do`.

A handler that panics is contained by the engine, logged with a stack trace, and
reported to the player. That reporting matters: an early silent recovery made a
correct password produce no output at all.

### Persistence is write-behind, and the in-memory graph is authoritative

Reads never touch Postgres after boot. Mutations mark objects dirty; every
`FBE_FLUSH_INTERVAL` the world goroutine deep-copies the dirty set into a
`Snapshot` and hands it to `internal/store`, which writes it in one transaction.
Saving therefore never touches a live object and never pauses the game, which is
what replaces Fuzzball's dump cycle. `@dump` is a forced flush.

A snapshot carries more than objects: program source changed by the editor,
and the macro table. Both are held apart from the object graph — source because
saving a program rewrites its text without touching any field on the object,
macros because they belong to no object at all. `World.SetSource` is the
loading path and marks nothing; `World.SaveSource` is the editor's and marks
the source for writing.

Containment chains are stored as upstream keeps them, because MUF can observe
their order, with each object's `Location` as a redundant cross-check.
`World.RepairChains` rebuilds any chain that disagrees, comparing *membership*
rather than order — a healthy chain is in insertion order, so comparing
sequences would rewrite every container on every boot.

### Package roles

- `internal/ref` — dbrefs, object types, the flag word
- `internal/props` — per-object property trees
- `internal/world` — objects, chains, dirty tracking, the engine
- `internal/store` — GORM models and the persister
- `internal/importer` — legacy `.db` dumps, `muf/*.m` sources, the macro table
- `internal/match` — resolving typed names to objects
- `internal/session` — descriptors, the connection hub, telnet negotiation
- `internal/game` — login, command dispatch, the commands
- `internal/transport/{tlsline,wss}` — the two listeners, terminating into one
  descriptor abstraction so session logic is written once

## The golden-output harness

`internal/golden` is the oracle. It builds a database holding a MUF snippet,
drives the same script through Fuzzball 7 built from the `fuzzball`
submodule and
through this server, and diffs the transcripts. The C runs in a container over
TCP; this server runs in-process.

**Use it for every primitive ported in M5.** Reading the C and reasoning about
it is guesswork; the harness answers directly. Its first run found three real
divergences in an afternoon's work, all of which had passed unit tests written
from the same C.

Adding a case is a `Case{Source: ...}` in `golden_test.go`. The snippet becomes
`test.muf`, reachable through an exit named `test`, and the harness compares
what each server prints.

Each command is bounded by a marker pose, so the harness knows when a command
has finished rather than guessing. **A session that holds the input line
cannot use the marker**: the MUF editor reads `!pose EMERALDDONE` as the
editor command `x`, and a program waiting on a `READ` eats it outright.
`RunOracleQuiet` drives the C server without markers, waiting for silence
instead; it is slower, so it is used only where the marker cannot be
(`editor_test.go`). `READ` has no such workaround, because the *reply* is what
the program consumes, and so is covered by unit tests in `internal/game`.

The C server writes into its game directory — a dump, and the macro table — so
it is given a copy of the fixture. Without that, a case defining a macro leaves
it behind for this server to import, and the two servers stop running the same
world.

## MPI

`internal/mpi` evaluates the macro language inside property values. It is
generated from `mfun_list` in `include/mfun.h`, which carries each function's
arity and the flags that decide how its arguments are handled before the
implementation runs.

Two things about it are easy to get wrong. An MPI failure is **reported to the
player and yields empty text** — it does not abort the MUF program or command
that was reading the property, so `Eval` is what callers want and `Parse` is
the inner form. And `{and}`, `{or}` and `{if}` do **not** pre-evaluate their
arguments, which is what the `Parse` flag in the table controls; evaluating a
branch they will not use would be visible, because MPI has side effects.

## Processes

`internal/game/proc.go` holds suspended programs. A program that hits `READ`,
`SLEEP` or `EVENT_WAITFOR` reports why through `muf.Frame.Block`, and the
scheduler files it under that. The engine's tick, which runs at the flush
interval, wakes sleepers.

A line typed while a program is reading goes to **that program**, not the
command parser — `@Q` is how a player escapes one. This is worth remembering
when writing tests: a reading program eats whatever comes next, including a
test harness's own marker.

A program runs at the **lower** of its own mucker level and its owner's, which
is `find_mlev`. A wizard with no mucker bits has level 0, so programs it owns
are capped there.

## The MUF editor

`internal/game/edit.go` is `src/edit.c`. A session holds the program's text in
`Server.editors`, keyed by player, and the program is flagged `INTERNAL` for as
long as one is open, which is what stops two people editing it at once. Nothing
is written until `q`; `x` discards.

The parse is the part worth knowing. **Arguments come first and the command
last**, and only the *first character of the last word* is looked at: `3 5 d`
deletes lines 3 to 5, `1 n` turns line numbers on, and `look` is the list
command because it starts with `l`. Anything unrecognised is "Illegal editor
command." — including `WHO`, which the descriptor layer hands to the editor
rather than answering itself.

Several behaviours look like bugs and are not. Entering the editor prints
"Line not available for display.", because the current line starts at zero and
the walk runs off the buffer. A bare `def` is the *delete* command, because
upstream only recognises `def` while parsing a second word. `u` never compiles
anything, so it says there is nothing to disassemble until `c` has run. The
current line lives on the program rather than the session, so reopening an
editor resumes where the last one left off (`Server.editLine`, not persisted).

`h` is the one deliberate divergence: upstream reads `data/edit-help.txt` and
this has no game directory, so the summary is built in.

## MCP

`internal/mcp` is MCP 2.1, the out-of-band protocol clients use to exchange
structured data alongside text. Every descriptor has a frame; a client that
never negotiates leaves it disabled and every line passes through untouched.

The **authentication key** is the security surface. Every message after the
opening one carries a key the server issued, which is what stops text a world
prints from being obeyed as a message by somebody's client. Outgoing text that
would look like a message is quoted on the way out for the same reason —
`Descriptor.Send` does that, and `sendRaw` is the unquoted path messages
themselves take.

Unlike the rest of the server, **a frame is reached from two goroutines**: a
connection is read on its transport's and written from the world's. It has a
mutex, and the lock is released before a package handler is called — holding it
across a callback turns a handler that replies into a deadlock, which is what
the first version did.

Package names are hierarchical, so a message name resolves to the *longest*
registered package it starts with: `org-fuzzball-gui-ctrl-value` belongs to
`org-fuzzball-gui`, not `org-fuzzball`. Packages are announced in reverse
registration order, because upstream builds its list by prepending.

The package list lives on the `session.Hub`, not on a descriptor, because
`MCP_REGISTER` adds to it at runtime and upstream offers such a package to
everyone who connects afterwards. `Server.installMCPHandlers` fills in the
handling, which cannot be declared with the packages: a descriptor needs the
list the moment it is created, before the server exists.

**MCP-GUI** draws dialogs. A dialog's control values live in
`mcp.Dialogs`, not in the program that opened it, because the client may
change them while that program is suspended. The MCP and GUI primitives are
gated by the `mcp_muf_mlev` parameter — a floor the generated mucker table
cannot express, so `checkMCPPerm` applies it — with upstream's exemption for a
program run by its own owner. `GUI_CTRL_COMMAND`, `GUI_AVAILABLE` and
`MCP_SUPPORTS` are not gated, which is also upstream's.

## Traps

**A MUF program starts with one value on its stack**: the command's argument,
which `interp()` pushes before the program runs. `depth` counts it. And unset
variables read as integer `0`, not `#-1`.

**Privileged primitives are gated by mucker level.** `internal/muf/mlev_gen.go`
is generated from the `mlev <` checks in the C; without it a level-1 program
could read passwords and change ownership. The table records only
*unconditional* floors: a check written `(mlev < 4) && !permissions(...)` means
"a wizard **or** the owner", not a floor, and treating it as one would refuse
the owner. The extractor is approximate — it skips conditions mentioning
permissions, ownership, flags or types — so a primitive that behaves oddly at
low mucker level is worth checking against the C.

**A program runs at its own mucker level**, bounded by its owner's — not at its
owner's level.

**TRY takes a count off the stack**: how many items the guarded block
consumes. Catching unwinds to exactly the depth below them, so the idiom is
`0 try ... catch ... endcatch`, not a bare `try`.

**A failing program prints a fixed block**, not a line: a header, the program
and line, then a backtrace with the failing source under each level, ending
`*done*`. `muf.Report` builds it and `Render` formats it. The backtrace shows
only to someone who controls the program, and the header differs for a
non-owner. Upstream leaves the argument list's opening parenthesis unclosed;
that is reproduced, because transcripts are compared against it.

**Error messages are upstream's wording**, not descriptions: "Invalid argument
type.", "Non-string argument.", "Variable number out of range." Programs match
on them.

**MUF does not abort on integer division by zero.** The result is `0` and an
error flag the program reads with `is_set?`. Aborting ends programs that
upstream runs to completion.

**`ref.Ref`'s zero value is `#0`, the global environment — not `NOTHING`.** Any
struct holding refs must initialise them explicitly. This has already caused one
real bug: the importer left `Exits` at the zero value for object types whose
dumps do not store it, silently attaching programs to the world root.

**`@tune` parameter names are a runtime API, not labels.** MUF's `SYSPARM`,
`SETSYSPARM` and `SYSPARM_ARRAY` look them up by string, so renaming one breaks
third-party code with no compile error. `internal/tune/params_gen.go` is
generated from `include/tunelist.h` — edit
`internal/tune/internal/gen/gen_params.py` and run `make generate`, never the
generated file. Reading an unknown parameter panics by design, so a typo in a
name is a runtime failure.

**`@action` is an alias for `@open`, and upstream's is not.** `do_action`
attaches an exit to a named object rather than to the room, and says so in its
own words. Until it is ported the alias differs in where the exit lands.

**Case-insensitive comparison is `strcasecmp`, not Unicode.** Use
`internal/ascii`, never `strings.EqualFold` or `strings.ToLower`. Upstream folds
only A–Z, so `Ä` and `ä` are distinct property and player names.

**An unrecognised command says what `huh_mesg` says**, not a fixed string. A
world changes it with `@tune`, and the default is upstream's "Huh?  (Type
"help" for help.)".

**`look` does not list exits.** Upstream's `look_room` gives the name, the
description and the contents and stops; a world that wants an "obvious exits"
line supplies it from its own programs, as the starter world does. An earlier
version of this printed one, which made every look diverge.

**Command precedence is load-bearing.** `QUIT` and `WHO` are compared
case-sensitively before anything else, and exits are matched before built-in
commands including `@`-commands. The starter world depends on both: it ships a
lowercase `quit` exit that says "did you mean QUIT in capitals", which only
works because lowercase falls through, and it defines exits named `@view`,
`@tel` and `@stats`. A wizard's `!` prefix skips exit matching.

**Store tests must not share a database with a real world.** The scratch schema
goes in the connection string, because GORM pools connections and `SET
search_path` reaches only one of them — every other query lands in `public`.
This destroyed a locally imported world before it was fixed, so each test now
asserts its isolation before doing anything.

## examine

`internal/game/examine.go` is `do_examine`. It prints a heading that differs by
type, a flags line, every message and lock that is set, three timestamps, a use
count, the contents and then a type-specific tail. `examine <obj>=<pattern>`
lists properties instead — `/` for the root, `**` recursively — with paths
shown from the root.

Two of its lines cannot agree with upstream and are masked in the golden case
rather than dropped: **Memory used** counts this server's own memory, which is
laid out differently, and **Cumulative runtime** is zero because Emerald does
not profile programs. Both keep their line and position.

`examine` reports whether a program is compiled **from the cache**, never by
compiling it — compiling to find out would make the answer always yes.

**Building costs money.** `@create`, `@dig` and `@open` charge `object_cost`,
`room_cost` and `exit_cost`, linking charges again, and a created thing is
endowed with `(cost-5)/5`. That is where an object's value comes from, and why
a fresh `@create` shows `Value: 1`. Wizards pay for nothing.

**Locks are stored, not evaluated.** `internal/boolexp` is planned and not
built, so `lockPasses` treats any lock that is actually set as failing —
failing closed, because erring the other way would hand out access a lock was
put there to refuse. Only the unset case, which is nearly every case, is
answered properly.

## Sanity checking

`@sanity` reports inconsistency, `@sanfix` repairs it, `@sanchange` edits one
reference by hand. All three are God-only, refused from inside a `@force`, and
are the only `@`-commands that cannot be abbreviated — upstream compares them
with `strcmp`, and "@san" should not be enough to run something that can
rewrite ownership.

These check the **in-memory** graph, not Postgres, though the plan said
otherwise. The in-memory graph is what the game runs on and the store is a
write-behind copy of it, so a check against the copy would pass while the
running world was broken.

The repair is also simpler than upstream's. Upstream must cut a damaged chain
and then hunt for whatever fell out of it, because the chain is its only record
of where things are; Emerald stores each object's location too, so `Fix`
corrects every object's fields first and rebuilds the chains from the locations
afterwards.

## Deliberate divergences from Fuzzball

Worth knowing before "fixing" something that looks wrong:

- **TLS only.** No cleartext listener, no STARTTLS. The eight `ssl_*` and
  `starttls_allow` parameters are gone, replaced by `FBE_TLS_*` environment
  variables, because a TLS-only server cannot read its listener configuration
  from a database it has not opened. `smtp_ssl_type` is renamed `smtp_tls_mode`,
  with the old name still resolving.
- **Passwords.** Argon2id, with both legacy formats verified and upgraded in
  place on first login. Fuzzball accepts *any* password when the stored one is
  empty; Emerald refuses. Fuzzball compares only the leading bytes of a stored
  hash; Emerald compares the whole value in constant time.
- **Inert parameters.** The `dump_*` family and `diskbase_propvals` still read
  and write so MUF that consults them works, but nothing acts on them.
  `fbemerald tune` marks them.
- **New code says TLS, never SSL** — but do not apply that rename to `@tune`
  names or primitive names. `DESCRSECURE?`, `NOTIFY_SECURE` and
  `ARRAY_NOTIFY_SECURE` keep their names and change meaning instead.

## Status

M0–M8 are done.

The server imports the starter world, accepts real MUCK clients over TLS and
WebSocket, runs MUF and evaluates MPI, and supports look, movement, speech,
building and admin commands. Programs can suspend themselves on `READ`, `SLEEP`
and `EVENT_WAITFOR`, the MUF editor works, so programs can be written on the
server rather than only imported, and MCP 2.1 and MCP-GUI are negotiated with
clients that speak them.

**Every primitive and every MPI function is implemented**: 412 of the 417
names, the other five being compiler internals (`" FOR"`, `" FOREACH"` and
the rest) that no program can name, and all 140 `mfn_*` functions.

Nine primitives are dispatched by `Frame.primitive` rather than registered in
the `prims` map, because the compiler emits them as instructions — `JMP`,
`READ`, `SLEEP`, `CALL`, `EXECUTE`, `EXIT`, `EVENT_WAITFOR`, `CATCH`,
`CATCH_DETAILED`. `registry.go`'s `dispatched` table names them so a survey
counts them as present; before it existed, every reading of this codebase
reported nine gaps that were not there.

## The MUF debugger

A program flagged `DARK` is being traced: the interpreter prints a line per
instruction to whoever controls it, which is what `DEBUG_ON` and `DEBUG_OFF`
switch. `internal/muf/debug.go` renders those lines, and upstream builds them
*backwards*, which is why the stack comes before the instruction:

    Debug> Pid 7: #58 6 ("", 3) DEBUG_OFF

The stack reads bottom to top, cut to its last eight with a leading `...`.

**The trace cannot be compared instruction for instruction against upstream,
and the golden case does not try.** The two compilers emit different code for
the same source: upstream fuses a variable reference with the `!` or `@` that
follows it, and a procedure address with the call that consumes it, where
Emerald emits each separately. Every *rendering* agrees — source line, stack,
strings cut at thirty characters with a trailing `_`, `SV0:name`,
`INIT FUNC: name (1 arg)`, `EXIT` — so `debugtrace_test.go` compares the
sequence of source lines walked, plus the untraced output either side.

`DEBUGGER_BREAK` is the one piece not ported. Upstream drops the player into
an interactive prompt to step and inspect; that would mean taking over a
connection's input, which nothing else in this server does. It forces tracing
on for the rest of the run instead, so the player sees what they would have
stepped through — but the program is not suspended, which is the difference
that matters.

## Limits

Two layers, answering different threats, and they are configured in different
places for a reason.

`internal/admit` refuses connections at **accept time**, before the TLS
handshake and before the world goroutine hears about them: a total cap, a
per-host cap and a per-host connection rate. Upstream has no equivalent —
Fuzzball's own limits all sit after authentication, which is too late against
a peer that never authenticates. These are `FBE_*` environment settings rather
than `@tune` parameters, for the same reason TLS is: a server under a flood
has to keep refusing while the database is unreachable.

Everything after login is upstream's and lives in `@tune`. The command spam
limiter is `session.Quota`, upstream's `d->quota`: `command_burst_size`
commands in hand, `commands_per_time` more every `command_time_msec`, refilled
by `Server.refillQuotas` on the tick. Spending it neither disconnects anyone
nor drops input — `Server.Input` waits on the transport's own goroutine, which
is where it must happen, since blocking the world goroutine would stall
everyone. A player in the editor or answering a `READ` is refilled eight times
as fast, which is upstream's `INTERACTIVE` case.

`playermax`/`playermax_limit` cap logged-in connections, checked after the
password is verified and exempting a true wizard, both upstream's. The count
is of connections that finished logging in, not of distinct players and not of
sockets — upstream's `con_players_curr`.

**`FBE_PPROF_ADDR` is refused unless it binds to loopback.** The handlers hand
out goroutine stacks and heap contents; `config.isLoopback` rejects a bare
port, which would bind every interface.
