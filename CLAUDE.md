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

**Case-insensitive comparison is `strcasecmp`, not Unicode.** Use
`internal/ascii`, never `strings.EqualFold` or `strings.ToLower`. Upstream folds
only A–Z, so `Ä` and `ä` are distinct property and player names.

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

M0–M3 are done: the server imports the starter world, accepts real MUCK clients
over TLS and WebSocket, and supports look, movement, speech, building and basic
admin commands.

MUF and MPI are not implemented. Exits that run programs say so rather than
working, and descriptions render as stored rather than evaluated. M4 is the MUF
compiler and VM, M5 the remaining primitives, M6 MPI. The plan flags the
golden-output harness — running a scripted session against both this server and
a C Fuzzball built from the submodule, and diffing — as the thing to build
*before* the primitive grind, or M5 and M6 become unverifiable.
