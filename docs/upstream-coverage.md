# What Fuzzball 7 documents that Emerald does not

An audit against the three upstream manuals, plus the two
architecture questions that prompted it. Everything here was checked
against the code rather than against the documents alone; where the
published page and upstream's own source disagree, the source wins,
because that is what the golden harness compares against.

Written against Emerald at the commit that adds this file.

---

## The short version

| Document | State |
|---|---|
| [`mpihelp.html`](https://fuzzball-muck.github.io/fuzzball/mpihelp.html) | **Nothing missing.** All 140 functions, and the documented limits check out. |
| [`mufman.html`](https://fuzzball-muck.github.io/fuzzball/mufman.html) | **Nothing missing that a program can reach.** Every primitive, every compiler directive. Six conditionals are deliberately false. |
| [`muckhelp.html`](https://fuzzball-muck.github.io/fuzzball/muckhelp.html) | **40 commands missing** of about 112. The engine behind most of them exists; the commands do not. |

Two questions answered below need no work: Emerald is **partly
crash-only, deliberately**, and **does not conform to 12-factor,
correctly**.

---

## `mpihelp.html` — nothing missing

All 140 `mfn_*` functions are implemented and golden-verified. The
documented limits were checked against both servers:

| Limit | The page says | Upstream's source says | Emerald |
|---|---|---|---|
| Recursion | 26 | `MPI_RECURSION_LIMIT` 26 | `recursionLimit` 26 |
| Variables | 32 | 32 | `maxVariables` 32 |
| Name length | — | `MAX_MFUN_NAME_LEN` 16 | `maxMPINameLen` 16 |
| Loop iterations | **256** | `MAX_MFUN_LIST_LEN` **512** | `maxListLen` **512** |

The published page's 256 is stale: upstream's own header says 512 and
so does its behaviour. Emerald follows the source. This is the
documentation being wrong, not a divergence, and it should not be
"fixed" later.

---

## `mufman.html` — the primitives and the directives are complete

**Every primitive** Fuzzball 7 defines is implemented: 412 of the 417
names, the other five being compiler internals (`" FOR"`,
`" FOREACH"` and the rest) that no program can name. Nine more are
dispatched by the interpreter rather than registered in the primitive
map, because the compiler emits them as instructions — `JMP`, `READ`,
`SLEEP`, `CALL`, `EXECUTE`, `EXIT`, `EVENT_WAITFOR`, `CATCH`,
`CATCH_DETAILED` — and `registry.go`'s `dispatched` table names them
so a survey counts them.

**Every compiler directive** now works. `$PRAGMA`, `$ENTRYPOINT` and
`$LANGUAGE` were missing until recently, and their absence was not a
degradation: they hit the unrecognised-directive branch, so a program
using one did not compile at all.

Six conditionals are **recognised and treated as false**, which is a
deliberate limitation rather than an omission:

    $ifver  $ifnver  $iflibver  $ifnlibver  $ifcancall  $ifncancall

They need a live database the compiler is not given. A program using
one takes its `$else` branch and the compiler says so in a note. This
is worth revisiting: `CANCALL?` and the program cache both exist now,
and the compiler could probably be handed what it needs.

---

## `muckhelp.html` — 40 commands missing

Upstream dispatches about 112 commands; Emerald has 72 names covering
72 of them, and 40 are absent. What is missing is **commands, not
engine**: the properties, locks and primitives behind most of these
already work, and what is not there is the verb that sets them.

### Message and lock properties

The most conspicuous group, and the cheapest to close. Every one of
these sets a property that `examine` already prints and that MUF
already reads:

    @fail  @ofail  @success  @osuccess  @drop  @odrop
    @idescribe  @oecho  @pecho  @propset

### Building and ownership

    @attach  @clone  @contents  @entrances  @owned  @register
    @relink  @sweep  @trace

### Wizard

    @bless  @unbless  @debug  @examine  @memory  @uncompile
    @usage  @doing

`@armageddon`, `@restart`, `@restrict`, `@teledump`, `@tops` and
`@wall` are also absent. `@reconfiguressl` is **deliberately** absent:
TLS is configured from the environment, because a TLS-only server
cannot read its listener configuration out of a database it has not
opened.

### MUF

    @mcpedit  @mcpprogram

The MCP machinery they need is implemented; only the editor commands
that drive it over MCP are missing.

### Basics

    put  give  score  gripe  disembark  leave  hand  uptime
    throw  goto  read

`throw`, `goto` and `read` are upstream's alternate spellings of
`drop`, `go` and `look`. They are not one-line aliases, though: they
are dispatch entries, because upstream prefix-matches bare commands
and Emerald's table does not — see "Abbreviations" below.

An earlier version of this list included `goal`. **There is no such
command in Fuzzball 7** — no `do_goal`, no dispatch entry, nothing in
the shipped help. It was an error in the list, not a gap in Emerald.

### Deliberately absent

`@memory` and `@usage` report C allocator and `getrusage` internals —
`mallinfo()` fields and sixteen `rusage` counters. Go has no
equivalent with the same shape, and inventing one would report
numbers that look like upstream's and mean something different. Same
judgement as `examine`'s "Memory used" line, which is masked in the
golden case for exactly that reason. Upstream itself guards both
behind `NO_MEMORY_COMMAND` / `NO_USAGE_COMMAND`.

`@reconfiguressl` is absent for the older reason: TLS is configured
from the environment, because a TLS-only server cannot read its
listener configuration out of a database it has not opened.

`@tops` reports per-program profiling totals, and this server does
not profile programs — the same reason `examine` reports a
cumulative runtime of zero.

All four still **resolve**: they are rows in the dispatch table with
no handler, so the abbreviations around them stay upstream's, and
typing one says which of the four reasons applies. The list lives in
`declined` in `internal/game/dispatch.go`, and a test checks every
name in it is a real command and is not secretly implemented.

### Abbreviations

**Emerald's abbreviation rule is not upstream's**, and the two already
disagree before any new command is added. `lookupAtCommand` takes any
unique prefix over the whole table and answers nothing when a prefix
is ambiguous. Upstream's `process_command` is a hand-written character
trie with a different tie-break at each node — `string_prefix`,
`strcmp` (case-sensitive), `strcasecmp`, `strlen(command) < 7`, and
one node with the `string_prefix` arguments reversed.

Verified divergences today:

| Typed | Emerald | Fuzzball 7 |
|---|---|---|
| `@to` | `@toad` | Huh — `@toad` is `strcmp` (`game.c:1458`) |
| `@co` | `@conlock` | Huh — `game.c:829` requires `command[3] == 'n'` |
| `e` | Huh | `examine` — `Matched("examine")`, `game.c:1587` |
| `i` | `inventory` | `inventory` (agrees by luck) |

Bare commands are prefix-matched upstream and exact-matched here,
which is the `e` case. This is being fixed by porting the dispatch
table rather than by adjusting the resolver.

### What the audit found by accident

Two things turned up that were not gaps but faults, and both were
fixed rather than recorded:

- **`@chown` silently set a chown lock.** Emerald had no `@chown`, but
  `@chown` is a prefix of `@chown_lock`, and `lookupAtCommand`
  resolved it there. A wizard transferring ownership would have been
  told the lock was set and believed the transfer had happened. The
  command is now implemented and golden-verified.
- **`resolveControlled` reported a failed match in the wrong words.**
  It said "I don't see that here." where upstream's
  `noisy_match_result` says "I don't understand 'X'." — which every
  command reaching it goes through, and which programs match on. That
  affected `@lock` and its whole family, `@link`, `@unlink`, `@name`,
  `@describe`, `@set`, `@teleport` and `@recycle`.

`@unlock` was added at the same time, since it is two lines and its
absence was being papered over by the help text.

Two more turned up while porting the message machinery, and both were
live rather than latent:

- **A description beginning `@` printed instead of running.** That
  spelling names a MUF program upstream — `exec_or_notify`,
  `property.c:2454` — and Emerald evaluated MPI over it and sent the
  text, so a world using the idiom showed `@$lib-desc` to whoever
  looked. It reached `look`, exit traversal and the fail/succeed
  messages, every caller of what used to be `mesgProp`.
- **Every `look` at a thing or a player printed a name line.**
  Upstream gives one only from `look_room`; `look_simple` prints the
  description alone. The contents heading was wrong for the same
  reason — `Contents:` everywhere, where upstream heads a player's
  `Carrying:` and a thing's `Contains:` — and a HAVEN thing showed
  contents it should have kept. `internal/golden/look_test.go` now
  pins the shape; before it, nothing in the harness looked at
  anything but a room.

Three things `do_look_at` does are still missing, and each is
recorded rather than hidden: look traps (the `_details` propdir,
consulted when the match finds nothing), the LOOK propqueue, and
`@teleport`'s own wording, which reports what moved where instead of
"Teleported." — that one belongs with `do_teleport`, whose control
rules diverge structurally anyway.

### The permission refusals, half fixed

Upstream's `match_controlled` refuses with "Permission denied. (You
don't control what was matched)". Emerald said "Permission denied."
everywhere, which was right nowhere.

**Five commands are fixed**: `@name`, `@describe`, `@set`, `@unlock`
and the whole `@lock` family really do route through
`match_controlled` upstream, and now say what it says.
`matchControlled` in `internal/game/build.go` is that function.

**Four are not, and the reason is behavioural rather than textual.**
None of them goes through `match_controlled` upstream; each matches
for itself and applies its own rule:

- `@unlink` also accepts `controls_link`, so upstream lets the
  destination's owner unlink an exit where Emerald refuses them.
- `@link` lets a builder who controls nothing *seize* an unlinked
  exit, paying `link_cost` plus `exit_cost`; Emerald refuses before
  that path can run.
- `@teleport` defers its control test until the destination is known
  and varies it by victim type; Emerald tests the victim up front.
- `@recycle` is **stricter** than `controls`: upstream requires
  actual ownership of a room or thing even of a wizard, so Emerald
  currently lets a wizard recycle objects upstream refuses.

Those four stay on `resolveControlled`, which keeps the old message
rather than pretending to be upstream's. Each needs its own commit.

---

## Is Emerald crash-only?

**Partly, and deliberately not fully.**

In favour:

- The in-memory graph is rebuilt from Postgres on every boot. There
  is no other source of truth and no separate recovery path.
- A crash loses at most `FBE_FLUSH_INTERVAL`, because persistence is
  write-behind rather than a dump cycle.
- `World.RepairChains` is recovery logic that runs on **every** load,
  not only after a crash — so the repair path is exercised constantly
  rather than being the code that only runs when things are already
  bad.
- The liveness lease is an advisory lock held by a connection, which
  means a killed server leaves nothing to clean up. There is no stale
  lock file and no timeout to tune.

Against:

- There is a distinct graceful-shutdown path that drains the queue
  and flushes, so stopping and crashing are not the same operation.
- The engine **contains panics** rather than crashing: a handler that
  panics is logged, reported to the player, and the world carries on.
  That is the opposite of crash-only's "crash rather than limp", and
  it is the right trade here — one bad command should not take a
  world down.

---

## Does Emerald conform to 12-factor?

**No, and correctly so.** The interesting answer is not the score but
which violations are essential.

Conforming: codebase, dependencies, backing services, build/release/
run, port binding, dev/prod parity, logs as an event stream to stderr,
and admin processes (`import`, `migrate`, `tune`, `help-seed`).

Violated:

- **VI (processes) and VIII (concurrency)**, at the root. One
  goroutine owns the world in memory; the server cannot be scaled out
  or run twice, and now refuses to be. This is *required* by MUF
  semantics — primitives mutate the object graph non-atomically and
  multitasking is cooperative, yielding at instruction-count slices —
  so conforming here would mean not being a MUCK.
- **III (config)**: `@tune` parameters live in the database rather
  than the environment. Also deliberate: they are a runtime API that
  MUF reads by name through `SYSPARM`, not deployment configuration.
  The settings that really are deployment configuration — listeners,
  TLS, the database URL, the limits — *are* environment variables,
  precisely because a TLS-only server cannot read them from a
  database it has not opened.
- Minor: TLS material is supplied as file paths rather than values.

VI and VIII are essential. The rest are not violations worth closing.
