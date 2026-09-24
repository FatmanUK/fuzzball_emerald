# BOOTSTRAP.md — Fuzzball Emerald

Written so a fresh agent — or a competing LLM — can resume this project after
total loss of session state. If this file and the repo disagree, **the repo
wins**: this is a snapshot, not a source of truth.

Read `CLAUDE.md` first. It is the working contract and is loaded into every
session; this file is the orientation around it.

## 1. Current Goal

A from-scratch **Go** reimplementation of **Fuzzball MUCK 7**, behaviour-
compatible with the original — existing MUF programs, MPI descriptions and
`.db` worlds must work unchanged — while deliberately replacing four things
that have aged worst in the C:

| Fuzzball 7 (C) | Fuzzball Emerald (Go) |
|---|---|
| Manual memory management | Go, memory-safe |
| Plaintext + optional SSL | **TLS only** (raw TLS + WebSocket-over-TLS) |
| Flat-file dump, world freezes to save | **Postgres**, continuous write-behind |
| autotools + hand-rolled Dockerfile | Rootless **Podman** container |

### Where it stands

- **M0–M8 are complete.** The server imports the starter world, accepts real
  MUCK clients over TLS and WebSocket, runs MUF and MPI, and speaks MCP 2.1
  and MCP-GUI.
- **The language surface is complete.** 412 of 417 primitive names — the
  other five are compiler internals (`" FOR"` and friends) no program can
  name — all 140 MPI functions, and every compiler directive.
- **The help system is in**: `help`, `news`, `man`, `mpi`, `info`, `motd`,
  `@credits`, with the texts in Postgres and a configurable welcome banner.
- **The configurator is in**: `cmd/fbeconfig`, an optional web interface over
  the same database, read-only while the server runs.
- **What is not done is commands.** About 40 of Fuzzball's ~112 player
  commands are missing. See `docs/upstream-coverage.md`, which is the audit
  and the authority on this.

Two behaviours are deliberately not ported: `DEBUGGER_BREAK`'s interactive
prompt, and the MPI tracer behind `{debug}`/`{debugif}`. Both would mean
taking over a connection's input, which nothing else in this server does.

## 2. Next Three Steps

The plans at `~/.claude/plans/i-want-to-create-hashed-moore.md` (M0–M8) and
`~/.claude/plans/rippling-roaming-backus.md` (the help system, the
configurator, the audit) are **both fully executed**. There is no open plan.
What follows is what the audit left on the table, in the order it is worth
doing.

1. **The message and lock property commands.** `@fail`, `@ofail`,
   `@success`, `@osuccess`, `@drop`, `@odrop`, `@idescribe`, `@oecho`,
   `@pecho`, `@propset`. These are the conspicuous half of the command gap
   and the cheapest to close: every one sets a property `examine` already
   prints and MUF already reads, so the work is the verb, not the engine.
   `internal/game/build.go`'s `cmdDescribe` is the shape to copy.

2. **The remaining commands**, in decreasing order of how often anyone will
   miss them: `@owned`, `@contents`, `@entrances`, `@sweep`, `@register`,
   `@relink`, `@clone`, `@attach`, `@trace`; then the wizard set (`@bless`,
   `@unbless`, `@examine`, `@memory`, `@uncompile`, `@usage`, `@doing`,
   `@wall`, `@restrict`); then the basics (`put`, `give`, `score`, `gripe`,
   `uptime`, `leave`, `disembark`, `hand`). `throw`, `goto` and `read` are
   upstream's alternate spellings of `drop`, `go` and `look` — dispatch
   entries rather than one-line aliases, because upstream prefix-matches
   bare commands and Emerald does not.

3. **The six compiler conditionals that are always false** — `$ifver`,
   `$ifnver`, `$iflibver`, `$ifnlibver`, `$ifcancall`, `$ifncancall`
   (`internal/muf/compiler/directive.go`). They need a live database the
   compiler is not given, but `CANCALL?` and the program cache both exist
   now, so the compiler can probably be handed what it needs through
   `Options`. Deferred deliberately, not forgotten.

Smaller things, each self-contained:

- **`@action` still aliases `@open`**, which is not upstream's behaviour:
  `do_action` attaches an exit to a *named object* rather than to the room,
  and says so in its own words.
- **`resolveControlled`'s permission wording.** Upstream's `match_controlled`
  says "Permission denied. (You don't control what was matched)"; Emerald
  says "Permission denied." Some upstream commands reach the same point with
  their own wording, so this needs a command-by-command comparison rather
  than one edit.
- **ANSI output over a live connection.** Real Fuzzball suppressed
  `TEXTATTR`'s escape codes where Emerald sent them raw — probably a client
  capability gate that nothing in Emerald's `NOTIFY` path checks. Not
  specific to `TEXTATTR`.
- **`ProgUID`/`find_uid` is approximated.** The dominant `REGUID` path is
  covered; `STICKY`/`HAVEN`/`SETUID`/`HARDUID` are not, since they need a
  `fr->perms` caller-stack concept nothing here has built.
- **The repository declares no licence.** This is why the help content was
  written fresh rather than taken from upstream's GPLv3 `.raw` files. Worth
  settling deliberately rather than by accident.

### How it got here

Seven phases, all complete. `git log` has the detail and every commit message
explains its own findings; `docs/upstream-coverage.md` is the current-state
audit. In brief: M0–M4 built the object model, persistence, transports and
the MUF compiler and VM; M5–M7 ported the primitives, MPI, the process queue,
the editor and MCP; M8 was hardening and deployment. Then the five-task plan:
an import bug, a 70-column reformat, the compiler directives, the help
system, the configurator, and the audit.

## 2a. Findings worth keeping

Non-obvious things learned by reading the C, each of which cost real time to
establish. Every one is pinned by a test; this is the index, not the record.

**Upstream behaviours that look like bugs and are not:**

- **`FIRSTDESCR`/`LASTDESCR` are asymmetric.** `#-1 firstdescr` answers the
  globally *oldest* connection, but `<player> firstdescr` answers that
  player's *newest*. `prim_firstdescr` reads `darr[dcount-1]`.
- **`help topic=3-7` ignores its segment.** `index_file` never sees one; only
  `show_subfile` does. `info` is the one corpus whose segment works.
- **`index_file` and `spit_file_segment` render a blank line as two spaces.**
- **Upstream builds its MUF debug trace backwards** — `debug_inst` writes the
  instruction and then *prepends* the stack, so the stack reads bottom-to-top
  and comes first in the line.
- **`COPYPLAYER` gives the copy twice the source's pennies**, because
  `copy_properties_onto` replaces the destination's property tree and the
  value arithmetic then reads back what it just copied.
- **`rnd()` feeds MD5 only 8 of its 16 digest bytes** (`sizeof(digest)` on a
  `uint32*`). Reproducing that is what makes a recorded seed replay.
- **`DESCRUSER` is not an identity** — `d->username` holds the client's
  ephemeral TCP source port, parsed back out of a logging string.
- **`FMTTIME` parses rather than formats.** It is `strptime`.
- **`LOCKED?`'s type check is written `!= TYPE_PLAYER && == TYPE_THING`**,
  which rejects a THING against its own doc comment. Reproduced deliberately.

**Divergences between upstream's doc comments and its code**, found often
enough to be a pattern — never trust the comment: `tune_parms_array` and
`muf_event_exists` claim smatch patterns and do exact matches;
`STATS_ARRAY`'s comment reverses the order it actually builds; `COPYOBJ`'s
claims a clone's value resets.

**Wording is a compatibility surface.** Programs match on error strings. Most
mlev refusals use the dispatcher's generic text, but `p_connects.c` gives
*every* floor its own — three distinct level-3 variants and two level-4 —
and `FORCE`, `FORCEDBY`, `FORCEDBY_ARRAY`, `GETPIDS`, `PARSEPROP` and
`SMTP_SEND` each have their own too. `gen_mlev.py`'s `CUSTOM_ABORT_MESSAGE`
set is how a primitive opts out of the generated table to check inline.

**The mlev generator's extraction is approximate and has been wrong twice.**
It records only *unconditional* floors; a check qualified by ownership
(`control_process(...)`, `fr->pid ==`) is an escape hatch, not a floor, and
each of those had to be taught to the regex after it silently mis-gated a
primitive. A primitive behaving oddly at low mucker level is worth checking
against the C by hand.

**A missing command is not always a silent gap.** `@chown` was absent while
being a *prefix* of `@chown_lock`, so `lookupAtCommand` resolved it there and
a wizard transferring ownership silently set a lock. When adding a command,
check what its name is currently a prefix of.

**`procQueue` holds the currently-running foreground process**, unlike
upstream's `tqhead`. Every count and every wildcard match over it has to
allow for that — `processLimitOK` runs one high, and `GETPIDS` had to exclude
the caller's own pid explicitly.

## 3. Project State

### 3.1 Key Logic

- **Single world goroutine** (`internal/world.Engine`) owns every object.
  Everything in `internal/game` runs on it; transports enqueue work with
  `Engine.Go`, and `Engine.Do` waits and must never be called from inside
  another `Do`. This exists because MUF semantics assume single-threaded,
  non-atomic mutation and cooperative multitasking. It is the most
  load-bearing architectural fact in the codebase, and it is why the server
  refuses to run twice against one world.
- **Write-behind persistence**: reads never touch Postgres after boot.
  Mutations mark objects dirty; every `FBE_FLUSH_INTERVAL` the world
  goroutine deep-copies the dirty set into a `Snapshot` and hands it to
  `internal/store` for one transaction. `@dump` is a forced flush, not a
  freeze. A snapshot also carries changed program source, the macro table,
  and any changed help corpus — all of which live apart from the object
  graph.
- **The liveness lease** (`internal/store/lease.go`) is a Postgres
  session-level advisory lock the server holds for its lifetime. It is what
  stops two servers sharing a world and what tells the configurator whether
  it may write. It must be held on a connection **pinned outside GORM's
  pool**, and `Release` must unlock explicitly — closing an `*sql.Conn`
  returns the session to the pool alive, lock and all.
- **Containment chains** are stored in Fuzzball's insertion order because MUF
  can observe it, with each object's `Location` as a redundant cross-check.
  `World.RepairChains` rebuilds any chain that disagrees, comparing
  *membership* rather than order.
- **The golden harness** (`internal/golden`) is the oracle for correctness,
  not the C source. It builds a database, drives the same script through a
  real Fuzzball 7 in a Podman container and through this server in-process,
  and diffs the transcripts. It has found real divergences on nearly every
  milestone that unit tests written from the same C had missed. Two driving
  modes: marker-bounded (`RunOracle`, default) and quiet-bounded
  (`RunOracleQuiet`, for sessions that would eat the marker — the editor, a
  `READ`).
- **A program runs at the *lower* of its own mucker level and its owner's**
  (`find_mlev`), so a wizard with no mucker bits caps every program it owns
  at level 0.
- **The MCP frame is the one piece of state reached from two goroutines.** It
  has a mutex, released before calling a package handler — holding it across
  a callback deadlocks a handler that replies to its own message.

### 3.2 Architecture

```
cmd/fbemerald/          — the server: serve, import, migrate, tune,
                            help-seed, version
cmd/fbeconfig/          — the optional web configurator
internal/ref/           — dbrefs (Ref), object types, the flag word
internal/props/         — per-object property trees (case-insensitive, blessed)
internal/ascii/         — ASCII-only case folding, SMatch, AlphanumCompare
internal/ansi/          — the attribute tags TEXTATTR and {attr} share
internal/timefmt/       — strftime and strptime
internal/world/         — Object, World, Engine, Snapshot, RepairChains,
                            help corpora, sanity checking (Check/Fix)
internal/store/         — GORM models, persister, loader, the liveness lease
internal/importer/      — Foxen9 .db parser, muf/*.m loader, macro table
internal/tune/          — 161 @tune params (params_gen.go, generated)
internal/match/         — name resolution: exits, aliases, environment walk,
                            $registered names, priority
internal/session/       — Descriptor, Hub, telnet codec, spam quota
internal/mcp/           — MCP 2.1: framing, negotiation, GUI dialogs
internal/muf/           — instruction set, VM, 412 primitives, the debugger
internal/muf/compiler/  — the MUF compiler (lexer + compile.c port)
internal/mpi/           — MPI parser + all 140 mfn_* functions
internal/boolexp/       — lock expressions, boolexp.c ported directly
internal/help/          — the built-in help texts, embedded and seeded
internal/game/          — login, command dispatch, the commands, MUF/MPI
                            hosts, process queue, editor, examine, help
internal/web/           — the configurator's handlers and templates
internal/admit/         — accept-time connection limits
internal/golden/        — the differential test harness (THE oracle)
internal/transport/     — tlsline (raw TLS) and wss (WebSocket)
internal/password/      — Argon2id + legacy MD5/PBKDF2 verify-and-upgrade
internal/config/        — pre-database settings from the environment
internal/logging/       — structured slog wrappers, including the audit channel
tools/reflow/           — the 70-column formatter gofmt will not do
docs/                   — upstream-coverage.md, the audit
fuzzball/               — git submodule, Fuzzball v7.2.1 (READ-ONLY C
                            reference; never edit, never merge into vmother)
deploy/                 — Containerfile (two targets), compose.yaml, the
                            golden oracle's own Containerfile
```

Four **code generators** (`make generate`) read the `fuzzball/` submodule and
must never be hand-edited in their output:

- `internal/tune/internal/gen/gen_params.py` → `internal/tune/params_gen.go`
- `internal/muf/internal/gen/gen_prims.py` → `internal/muf/prims_gen.go`,
  `defs_gen.go`
- `internal/muf/internal/gen/gen_mlev.py` → `internal/muf/mlev_gen.go`
  (approximate — see §2a)
- `internal/mpi/internal/gen/gen_funcs.py` → `internal/mpi/funcs_gen.go`

### 3.3 Decisions

Departures from the plan or from naive C-to-Go translation, recorded so they
are not "fixed" back by accident.

**Structural:**

- **The C reference is a submodule** at `fuzzball/`, pinned to v7.2.1 rather
  than a moving branch. All four generators and `make golden-build` read it.
- **`@sanity`/`@sanfix`/`@sanchange` check the in-memory graph, not
  Postgres**, though the plan said otherwise: the graph is authoritative and
  the store is a write-behind copy, so a check against the copy could pass
  while the live world was broken. The repair is also simpler than
  upstream's, because `Location` is stored redundantly.
- **Help texts live in Postgres**, not a game directory, which is why all
  fourteen `file_*` parameters naming one are inert. The content is Emerald's
  own: upstream's `.raw` files are GPLv3, this repo declares no licence, and
  their help documents ~50 commands that do not exist here.
- **The configurator reads the database directly** rather than loading a
  world, because the server may be running and holding the authoritative
  graph in its own memory.

**Traps in the Go:**

- **`ref.Ref`'s zero value is `#0`** (global environment), not `#-1`. This
  bit the importer once, silently attaching programs to the world root.
- **`@tune` parameter names are a runtime string API.** `SYSPARM` looks them
  up by string with no compile-time check.
- **Case folding is ASCII-only** (`internal/ascii`), never
  `strings.EqualFold`: upstream's `strcasecmp` folds only A–Z, so `Ä` and
  `ä` are distinct player and property names.
- **`World.Add` deliberately does not mark dirty** — it is `store.Load`'s
  path. `MarkAllDirty` is the importer's, and it has to run before any early
  return, which is what `273a170` fixed.
- **Some compiler directives write properties and the compiler cannot.**
  `$author`, `$pubdef`, `$libdef` and their kind are collected on
  `Result.Props`; a caller using `Compile` rather than `CompileResult`
  silently drops them, which is how `$libdef` came to export nothing.

**Compatibility choices:**

- **TLS only** — no cleartext, no STARTTLS. The `ssl_*` and `starttls_allow`
  parameters are gone, replaced by `FBE_TLS_*` environment variables, because
  a TLS-only server cannot read its listener configuration from a database it
  has not opened.
- **Argon2id passwords**, with legacy MD5/PBKDF2 verified and upgraded in
  place. An empty stored password is refused rather than accepted, and the
  comparison is constant-time over the whole value rather than the leading
  bytes.
- **New code says TLS, never SSL** — except where it would break a surface:
  `@tune` names, `DESCRSECURE?`, `NOTIFY_SECURE` and `ARRAY_NOTIFY_SECURE`
  keep their names and change meaning.
- **The `dump_*` family and `diskbase_propvals` are inert**, still readable
  and settable so MUF that consults them works.
- **Building costs money** — `@create`/`@dig`/`@open` charge, linking charges
  again, and a created thing is endowed `(cost-5)/5`.
- **Two upstream bugs are reproduced** because transcripts are compared
  against them: the unclosed parenthesis in MUF error backtraces, and
  `@sanchange` reporting an "exits" change as a "next" change. **Two are
  not**: `unparse_flags`'s NUL read for garbage objects, and `@sanchange`'s
  echo of an uninitialised stack variable — undefined behaviour is not a
  contract.
- **A lock property holds its unparsed string, not a cached tree.**
  `boolexp.Parse` always takes the disk-loader path when reading one back,
  which is valid because `Unparse` is the only thing that writes one. Name
  matching happens once, when a lock is set from player input.
- **`Descriptor.Send` auto-quotes text that looks like an MCP message**, so a
  player cannot drive another player's client by typing the prefix.

### 3.4 Other

- **Fixture password**: the starter world's `#1` is `potrzebie`, documented in
  the upstream README — a fixture, not a secret.
- **Go source is 70 columns, tab counted as 8.** `make fmt` enforces it with
  `tools/reflow`; `make width-check` holds new code to it. **Do not add
  golines** — it was tried, measured and rejected; `CLAUDE.md` has the
  numbers. About 2,900 existing lines are still long, nearly all string
  literals, and are deliberately left alone.
- **God-only commands** (`@sanity`, `@sanfix`, `@sanchange`) are the only
  `@`-commands that cannot be abbreviated, because upstream compares them
  with `strcmp`. They are also refused from inside a `@force`. `@credits` is
  exact-only for a different reason: otherwise `@cre` becomes ambiguous and
  `@create` stops abbreviating.
- **Command precedence is load-bearing** and the starter world depends on it:
  `QUIT`/`WHO` are compared case-sensitively before anything else, and exits
  are matched before *any* built-in including `@`-commands. A wizard's `!`
  prefix skips exit matching.
- **Store tests must never share a database with a real world.** The scratch
  schema goes in the connection string, not a `SET search_path`: GORM pools
  connections, so the SET reaches one of them and every other query lands in
  `public`. This destroyed a real imported world once.
- **GPG signing times out regularly** (a gnome3 pinentry issue, not a code
  problem). The fix is always to retry the identical `git commit` once the
  user has unlocked the key. Never use `--no-gpg-sign`.

## 4. Dependency Map

**External Go modules** (`go.mod`):

- `golang.org/x/crypto` — Argon2id
- `gorm.io/gorm` + `gorm.io/driver/postgres` (+ transitive `jackc/pgx`,
  `pgpassfile`, `pgservicefile`, `puddle`) — persistence
- `github.com/coder/websocket` — the WSS transport
- transitive only: `golang.org/x/sync`, `golang.org/x/sys`,
  `golang.org/x/text`, `jinzhu/inflection`, `jinzhu/now`

The configurator adds nothing: it is `net/http` and `html/template`.

**External non-Go dependency**: Podman (rootless), for the deployment
containers and for the golden oracle built from `fuzzball/`.

**Internal package shape** (arrows = "depends on"):

Direct imports, taken from `go list` rather than written by hand:

```
cmd/fbemerald  → game, help, world, store, importer, admit, config,
                   transport/*, tune, logging, ref
cmd/fbeconfig  → web, store, config, logging
web            → store, world, help, tune, password, props, ref
game           → world, session, mcp, muf, muf/compiler, mpi, boolexp,
                   match, password, tune, props, ref, ascii, logging
help           → world, ascii
muf            → boolexp, match, ansi, timefmt, props, ref, ascii
muf/compiler   → muf, ref, ascii
match          → world, props, ref, ascii
world          → tune, props, ref, ascii
store          → world, props, ref, ascii
importer       → world, password, tune, props, ref
session        → mcp, ref
mpi            → ansi, timefmt, ascii      (no ref, no world)
boolexp        → props, ref, ascii         (pure)
mcp            → ascii                     (pure protocol)
admit          → nothing
golden         → game, help, importer, session, world   (test-only)
```

Two of these are worth noticing. **`game` does not import `store`** — the
engine reaches persistence through the `world.Persister` interface, and
`cmd/fbemerald` is what wires the two together. And **`mpi` depends on
neither `ref` nor `world`**: it reaches the database entirely through its own
`Host` interface, which is why it can be tested without one.

`fuzzball/` is a leaf that only the generator scripts and `make golden-build`
read. No Go package imports it.

## 5. Version Log

Most recent first. `git log` has the full history back to M0, and every
message explains its own findings.

| Commit | Summary |
|---|---|
| `4c5b8ed` | The upstream-manual audit (`docs/upstream-coverage.md`), and the two faults it found: `@chown` was missing *and* silently resolving to `@chown_lock`; `resolveControlled` reported failed matches in the wrong words. `@unlock` added alongside. |
| `ffda4c7` | Packaged the configurator — a second Containerfile target, an `admin` compose profile bound to loopback. |
| `998931a` | The configurator's pages: `@tune` editor, manual editor, player management, object inspector. |
| `2676e1c` | The configurator's login and the read-only rule. |
| `6576909` | The liveness lease: refuse to run two servers against one world. |
| `e6c9785` | The integrated help system, the configurable welcome banner, motd-on-connect. |
| `8c1cb8a` | `$pragma`, `$entrypoint`, `$language` — and `$libdef`/`$pubdef` actually writing their properties. |
| `9dba68b` | Held Go source to 70 columns; added `tools/reflow` and `make width-check`. |
| `273a170` | Fixed: a dump with no `muf/` directory imported as an empty world. |
| `f8c6a56` | Closed the last three deferrals: `PARSEPROPEX`, `SMTP_SEND`, the MUF debugger. |
| `dae7780` | M8: connection limits, the spam quota, `playermax`, pprof, the audit channel, graceful drain. |
| `3b500e0`, `f338674` | Phase 5: all 140 MPI functions. |
| `7ef7985` and earlier | Phase 4 and before: the primitive surface, the process queue, MCP, the editor, `examine`, sanity checking. |

Working branch: `vmother` (deliberately started empty). Base branch: `mother`
— **never merge `vmother` into it**, and never merge the `fuzzball/`-adjacent
branches (`origin/upstream_master`, `origin/development`) into `vmother`;
they are read-only reference.

## 6. Testing Status

All green as of `4c5b8ed`:

- `go vet ./...` — clean
- `make fmt-check` — clean (gofmt plus the 70-column reflow)
- `make width-check` — no new line over 70 columns
- `make test` (`go test -race ./...` against a scratch Postgres schema) —
  clean, no data races, across ~610 test functions
- `make golden` — all 11 differential suites against a live Fuzzball 7
  container:

  | Suite | Covers |
  |---|---|
  | `TestAgainstFuzzball` | 57 MUF and MPI cases |
  | `TestEditorMatchesFuzzball` | a 94-command editor session, quiet-driven |
  | `TestExamineMatchesFuzzball` | all five object types plus property listing |
  | `TestHelpMatchesFuzzball` | the whole help system, both servers given the same texts |
  | `TestLockCommandsMatchFuzzball` | the `@lock` family, `@unlock`, an exit whose lock gates it |
  | `TestWizardCommandsMatchFuzzball` | `@stats`, `@boot`, `@force`, `@toad`, `@chown` |
  | `TestForceMatchesFuzzball` | `FORCE`/`FORCEDBY`/`FORCEDBY_ARRAY` |
  | `TestConnectsMatchesFuzzball` | `DESCRHOST`/`DESCRUSER` at mlevel 4 |
  | `TestDebugTraceMatchesFuzzball` | the MUF debugger, by source line |
  | `TestMCPNegotiationMatchesFuzzball` | MCP 2.1 negotiation |
  | `TestSanityMatchesFuzzball` | `@sanity`/`@sanfix` |

- Primitive coverage: **412 of 417, 0 missing**
  (`go test -run TestPrimitiveCoverage -v ./internal/muf/`)
- MPI coverage: **140 of 140, 0 missing**
  (`go test -run TestFunctionCoverage -v ./internal/mpi/`)

**Test-infrastructure quirks**, not bugs, but easy to trip over:

- `internal/store` and `internal/web` tests skip silently without
  `FBE_TEST_DATABASE_URL`. `make test` sets it; a bare `go test` does not.
- `make golden` needs `make golden-build` run once first, which builds a
  Fuzzball 7 Podman image from the submodule. Without it the suite fails with
  a clear "oracle not built" message.
- A reading program — the editor, or a `READ` — eats the marker line the
  default golden runner uses. Those cases use `RunOracleQuiet` instead.
- The golden Emerald server does **not** seed help from `internal/help`; the
  harness loads a small fixed corpus into both servers instead
  (`internal/golden/helpdata.go`), because the two projects do not share
  prose and only the machinery is comparable.
- `internal/web`'s tests create objects in a world whose refs start at 100
  (`scratchWorld`), because a fresh `world.New()` hands out `#0` and would
  overwrite the wizard the test logs in as.
