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
| [`muckhelp.html`](https://fuzzball-muck.github.io/fuzzball/muckhelp.html) | **10 of 109 dispatched names have no handler**, and five of those are deliberate. |

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

**The six conditionals now work.** `$ifver`, `$ifnver`, `$iflibver`,
`$ifnlibver`, `$ifcancall` and `$ifncancall` used to be recognised and
treated as false, because they need a live database the compiler is
deliberately denied.

They are answered through two callbacks instead, the shape `$include`
already used: `Options.ObjVersion` reads the version property off a
named object, and `Options.CanCall` applies upstream's four-part
public-function test. The compiler still has no world.

Three things were settled by doing it:

- **A version comparison is "is the wanted version at most the one the
  object has"**, both sides parsed as floats with anything unparseable
  reading as 0.0. An object with no version property reads as "0.0"
  rather than failing, so `$ifver $lib 1.0` is *false* against a
  library that never declared one.
- **Failing to resolve the object is a compile error**, not a false
  condition — the one place these differ from `$iflib`.
- **`$ifcancall` is not `CANCALL?`'s test.** The primitive weighs the
  target program's own mucker level and the running frame's effective
  one; the directive weighs both *owners'* levels, because at compile
  time there is no frame. They read alike and sharing one function
  between them would make one wrong.

`$ifcancall` compiles the program it asks about, so `compileProgram`
gained a **recursion guard**: two libraries that each check the other
would otherwise compile each other for ever, on the world goroutine.

---

## `muckhelp.html` — 10 of 109 names have no handler

`internal/game/dispatch_table.go` is every name Fuzzball 7 dispatches:
**109 rows, 99 with handlers, 10 without.** The count is exact rather
than estimated, because the table *is* the command surface and a name
with no handler says so when typed.

The ten, and why:

    @memory  @usage  @reconfiguressl  @tops  @teledump   deliberate
    @armageddon  @restart                                lifecycle
    @mcpedit  @mcpprogram                                MCP editor
    @sweep                                               LISTENER flag

`@teledump` joins the deliberate list: it base64-encodes the flat-file
dump over the connection, and this server has no dump file to send.

`@armageddon` and `@restart` are implementable but touch process
lifecycle and deployment rather than the game — armageddon exits
*without* writing, which write-behind makes a deliberate choice rather
than a free one, and restart needs a supervisor to restart into. They
say "not yet", which is accurate.

This is a different shape of gap from the one this document opened
with. It used to be **verbs, not engine** — about forty commands whose
properties, locks and primitives already worked. Those have all landed.

That characterisation has now flipped. It used to be **commands, not
engine** — the properties, locks and primitives behind most of the
gap already worked and only the verb was missing. Those verbs have
landed. What remains needs machinery this server does not have:
`enter_room` and the containment rules, program registration, the
compiler conditionals, and the propqueues.

The count is now exact rather than estimated, because
`internal/game/dispatch_table.go` is every name upstream dispatches
and a name with no handler says so when typed.

### Message and lock properties

Nothing left in this group.

Ten of this group have landed: `@fail`, `@ofail`, `@success`,
`@osuccess`, `@drop`, `@odrop`, `@idescribe`, `@oecho`, `@pecho` and
`@doing` are now rows in `internal/game/mesg_cmd.go`, driven by one
port of `set_standard_property` — `set_standard_lock`'s exact twin,
down to "no `=` means report it". `@describe` was absorbed into the
same table, which is how it came to say "Object Description set."
rather than "Description set.".

One thing that family does cannot be reproduced. Reporting a property
that is **not set** prints "`(null)`" upstream: `GETMESG` hands the
NULL from `get_property_class` straight to a `%s`, and glibc renders
it that way. It is undefined behaviour with a stable-looking output
rather than a chosen wording, and a Go server has no null pointer to
print — so Emerald prints nothing after the colon, and the golden
case masks the line rather than dropping it, as `examine`'s "Memory
used" is masked.

`@propset` has landed too. It is not a message setter but a general
property writer, with six types and its own syntax — and it is one of
only two commands that write a property the *player* names, which is
why it carries a restriction check the message setters do not need.

### Building and ownership

    @sweep

### Wizard

    @debug  @examine  @memory  @usage

`@armageddon`, `@restart` and `@teledump` are also absent. `@reconfiguressl` is **deliberately** absent:
TLS is configured from the environment, because a TLS-only server
cannot read its listener configuration out of a database it has not
opened.

### MUF

    @mcpedit  @mcpprogram

The MCP machinery they need is implemented; only the editor commands
that drive it over MCP are missing.

### Basics

    put  give  disembark  leave  hand  throw  goto  read

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

### One thing @bless does that is recorded rather than reproduced

`blessprops_wildcard` also blesses **directories**, which `first_prop`
walks and Emerald's `props.Tree` does not report because a directory
carries no value of its own.

The effect is confined to the count `@bless` reports and to the marker
`examine` prints: `Prop_Blessed` reads a path's own flags, and a
blessed directory does not bless its children. Making it agree means a
flag that can live on a valueless node, which touches `internal/props`,
`examine` and the store together — and would have to persist, since
upstream's dumps carry propdir flags. Its own step.

### What cannot be compared at all

Two things in the matcher are genuinely non-deterministic upstream, so
no golden case can pin them:

- **`choose_thing`'s last resort is a coin toss** (`match.c:175`). Two
  objects with the same name, at the same environment distance, with
  no type preference to separate them, resolve *randomly*. Any script
  that names one of a pair would diverge half the time.
- **The penny find** is `RANDOM() % penny_rate`, which is why the
  movement suite sets that parameter to zero and the payout is a unit
  test.

`choose_thing` itself is only partly ported: Emerald's tie-break is an
exit's priority level and the longest matching alias, where upstream
also weighs a preferred type, whether an object is locked against the
searcher (`check_keys`), and environment distance. That matters for
`get` and `drop`, which pass `check_keys` so that a locked container
loses to an unlocked one.

### The four checkflags searches

`@find`, `@owned`, `@contents` and `@entrances` are one mechanism with
four sources: the same flag expression, the same `checkflags` filter,
the same `display_objinfo` rendering and the same two closing lines.

The expression language was **already ported** before any of them —
`internal/muf`'s `FlagCheck`, written for `ARRAY_FILTER_FLAGS` and
`FINDNEXT`. Nothing about it is MUF's, so it is exported rather than
written twice. What was genuinely missing is `display_objinfo`
(`look.c:1476`) and its six modes, which `parseFlagCheck` had
deliberately dropped.

Two things about the syntax are easy to get wrong and are pinned by
the golden case:

- **The display mode comes after a *second* `=`.** The first separates
  the name from the flags, and `init_checkflags` splits what is left
  again — so `@find wid=owners` reads "owners" as six flag letters and
  finds nothing, where `@find wid==owners` asks for the owners column.
- **`locations` is tested before `links`**, so `=l` is locations and
  `=li` is links.

`=size` is the one mode that cannot be reproduced: it reports
`size_object`'s byte count, and this server's objects are laid out
nothing like the C's. It is recognised and renders as the plain mode,
the same judgement as `examine`'s masked "Memory used" line, and the
golden case masks the column rather than dropping the case.

`@find` itself was further from upstream than "ignores the flags". It
now wraps the pattern in `*…*` and matches with `smatch`, so a
substring works and so do the wildcards a player writes; it charges
`lookup_cost`, which nothing in this server had ever read; and its
invented cap of 200 results is gone — a large world used to answer
with part of the truth and report a count that looked right.

### Where the gripe log lives

Upstream appends a complaint to the file named by `file_log_gripes`,
and a bare `gripe` from a wizard spits that whole file back. Emerald
has no game directory, so this needed deciding, and the answer is the
one the help texts got: **rows in Postgres**, loaded at boot and
written behind like everything else.

That keeps the two architectural rules intact. Nothing in the running
server reads the database after boot — the recent complaints are in
memory from the load, and a new one is appended there and carried out
by the next flush. And the write-behind snapshot carries *new* gripes
rather than the whole list, which is the only place it does that: a
gripe log is append-only, so there is no deletion for a whole-table
rewrite to make visible.

Two consequences worth knowing:

- **`gripe` shows only the most recent `world.GripeLimit`.** Upstream's
  file grows without bound and hands a wizard the lot; on a world that
  has been up for years that is a flood. The whole log is still in the
  table.
- **The configurator has a `/gripes` page**, which is where the full
  history is read, paged. It is read-only for the reason the object
  inspector is: nothing there should be able to rewrite what somebody
  reported.

The rendering is upstream's log line, `vlog2file`'s timestamp and
`log_gripe`'s format — including the dbrefs carrying **no** `#`, which
is how every log line upstream spells a ref and which the golden case
compares.

### Abbreviations

**Emerald's abbreviation rule used not to be upstream's**, and the two
disagreed before any new command was added. `lookupAtCommand` took any
unique prefix over the whole table and answered nothing when a prefix
was ambiguous. Upstream's `process_command` is a hand-written
character trie with a different tie-break at each node —
`string_prefix`, `strcmp` (case-sensitive), `strcasecmp`,
`strlen(command) < 7`, and one node with the `string_prefix`
arguments reversed.

The divergences that made the case:

| Typed | Emerald, before | Fuzzball 7 |
|---|---|---|
| `@to` | `@toad` | Huh — `@toad` is `strcmp` (`game.c:1458`) |
| `@co` | `@conlock` | Huh — `game.c:829` requires `command[3] == 'n'` |
| `e` | Huh | `examine` — `Matched("examine")`, `game.c:1587` |
| `i` | `inventory` | `inventory` (agreed by luck) |

Two things settled the question. Adjusting the resolver could not
reach a trie with four different tie-breaks; and adding the ~40
missing verbs to the old resolver would have *lost* seven working
abbreviations — `@a @b @e @re @pr @co @ow` — by making them
ambiguous. So the trie was ported instead, as
`internal/game/dispatch_table.go`, and
`internal/golden/dispatch_test.go` drives every interesting
abbreviation through both servers.

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
