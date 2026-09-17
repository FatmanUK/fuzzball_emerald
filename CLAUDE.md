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

## The C reference lives in git, not on disk

This is the single most useful thing to know. The working branch `vmother`
contains only Go. The ~110k lines of Fuzzball C that everything is ported from
sit on the `mother` branch and are read with `git show`:

```bash
git show origin/mother:src/interp.c
git show origin/mother:include/db.h
git ls-tree -r --name-only origin/mother | grep -E '^(src|include)/'
```

`origin/upstream_master` and `origin/development` are other upstream snapshots.
These branches are read-only reference and must never be merged into `vmother`.

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

## Traps

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
a C Fuzzball built from `origin/mother`, and diffing — as the thing to build
*before* the primitive grind, or M5 and M6 become unverifiable.
