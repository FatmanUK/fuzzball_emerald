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

Milestones **M0 through M7 are complete**. What remains is filling out the
primitive/MPI surface and M8 (hardening + deploy). See §2.

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

Still to come from the Phase 2 plan, in order: `FORCE`/`FORCEDBY`/
`FORCEDBY_ARRAY` (share permission logic with `internal/game/wiz.go`'s
`@force`), `GETPIDS`/`GETPIDINFO`, and last `WATCHPID` (needs a still-missing
generic event-delivery mechanism built alongside it).

1. **Port more MUF primitives.** 94 of 417 are still unimplemented (see
   `go test -run TestPrimitiveCoverage -v ./internal/muf/` for the exact
   count and which ones). Each must be checked against the real C server via
   the golden harness (`internal/golden`), not just read from source — this
   has repeatedly caught real divergences that unit tests missed, including
   two in the lock work: `PARSELOCK` on `""` must produce no message at all
   (a null vs. merely-empty `PROG_STRING` distinction the C makes and Go
   cannot represent directly — see `mufHost.ParseLock`), and a match
   failure's own message during `_set_lock` is never gated by its `silent`
   flag, only `_set_lock`'s own messages are (see `boolexp.ParseError.Notify`
   and `Server.setLock`). `FORCE`/`FORK`/`QUEUE` (process and multitasking,
   `src/p_misc.c`) are the largest remaining chunk, now underway — see
   Phase 2 of the plan above.
2. **Port more MPI functions.** ~89 of 140 `mfn_*` functions from
   `src/mfuns.c`/`src/mfuns2.c` are still missing, mostly the list functions.
3. **Start M8**: rate limiting/connection caps and `pprof` behind a
   localhost-only port are genuinely missing. Structured audit logging is
   partial (`internal/logging` has no security-specific channel yet).
   Graceful drain and a README divergences section are likely already
   adequate — `cmd/fbemerald` already wires `SIGTERM` to a cancellable
   context, and `README.md`'s "Compatibility notes" section already covers
   divergences — verify before assuming either needs new work.

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
internal/muf/           — instruction set, VM/interpreter, ~318 primitives
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
    `FORCE_LEVEL`, `INSTANCES`, `SUPPLICANT`, `CANCALL?`, `KILL`), the
    `"fork"` case (parent/child independence) and the `"queue"` case
    (COMMAND vs. stack argument)
- Primitive coverage: **318 of 417** implemented
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
