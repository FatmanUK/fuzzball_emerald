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
| [`mpihelp.html`](https://fuzzball-muck.github.io/fuzzball/mpihelp.html) | **All 140 functions, and the documented limits check out.** Two gaps found later: `{force}`'s unblessed path, and error reporting that does not walk back out. |
| [`mufman.html`](https://fuzzball-muck.github.io/fuzzball/mufman.html) | **Nothing missing that a program can reach.** Every primitive, every compiler directive. Six conditionals are deliberately false. |
| [`muckhelp.html`](https://fuzzball-muck.github.io/fuzzball/muckhelp.html) | **9 of 109 dispatched names have no handler**, and five of those are deliberate. |

Two questions answered below need no work: Emerald is **partly
crash-only, deliberately**, and **does not conform to 12-factor,
correctly**.

---

## `mpihelp.html` — all 140 functions, two gaps inside them

All 140 `mfn_*` functions are implemented and golden-verified, and
that count is asserted by `TestFunctionCoverage` rather than typed
here — see "The primitive count is now self-maintaining" below, which
covers MPI as well. Two things inside them are not implemented, both
found later by golden cases written for something else and both
recorded below under "MPI's {force} is half implemented". The
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

**Two caveats, both found by auditing the claim rather than trusting
it.** `NEWPROGRAM` and `CHECKARGS` were both registered stubs that
aborted when a program reached them. A stub is *registered*, so the
412-of-417 count never moved and nothing contradicted the claim. Both
are now implemented and no stub is left; see "The primitive count is
now self-maintaining" below for what stops the next one hiding.

`CHECKARGS` is worth a note of its own, because the reason it stayed
a stub longest was wrong. It was thought not to be golden-reachable;
it is. Its aborts are *catchable* — `do_abort_interp`
(`interp.c:2844`) puts the message in `fr->errorstr` when a try frame
is open rather than ending the program — so one program can put a
hundred signatures through `0 try ... catch ... endcatch` and print
what each said, which makes the compiled C the arbiter for every
message. `internal/golden/checkargs_test.go` is 240-odd comparisons
on that route.

Two things it turned up that a reading of the C had missed. **Every
per-argument refusal carries a position suffix**, `ABORT_CHECKARGS`
(`p_stack.c:1124`) appending " (top)" or " (top-N)", which is the
most visible thing about the primitive. And **the lowercase dbref
characters are far wider than they look**: the outer test is `ref >=
db_top || ref < HOME`, so `#-1` and `#-2` survive it, and each
lowercase case then asks its type question only of a *non-negative*
ref — so `p` accepts `#-1` and `#-2` as readily as a real player.
`r` and `R` are wider still, because `Typeof(HOME)` is `TYPE_ROOM`
(`db.h:423`) and the explicit HOME refusal the other five carry is
simply missing from the room case.

Two golden suites turned out to be testing nothing, for the same
reason in both cases: `force_test.go` and `connects_test.go` raised
the mucker level of `test`, the *exit*, where the program is
`test.muf`. Their programs aborted on the first privileged primitive
with "Wizbit only primitive." on both servers, so the transcripts
matched and the cases passed while FORCE, FORCEDBY, FORCEDBY_ARRAY,
DESCRHOST and DESCRUSER were never compared. Both are fixed and all
five now agree. **A golden case that aborts identically on both
servers passes while testing nothing** — worth checking, when a case
exists to exercise a primitive, that the primitive ran.

**Every primitive** Fuzzball 7 defines is implemented: 412 of the 417
names, the other five being compiler internals (`" FOR"`,
`" FOREACH"` and the rest) that no program can name. Nine more are
dispatched by the interpreter rather than registered in the primitive
map, because the compiler emits them as instructions — `JMP`, `READ`,
`SLEEP`, `CALL`, `EXECUTE`, `EXIT`, `EVENT_WAITFOR`, `CATCH`,
`CATCH_DETAILED` — and `registry.go`'s `dispatched` table names them
so a survey counts them.

### The primitive count is now self-maintaining

The figures above are asserted by a test rather than typed here, and
until recently they were not — which is how two of them came to be
wrong while every document repeated them.

**A stub was invisible to every count.** A stub is *registered* like
anything else, so `len(prims)` counted it; `NEWPROGRAM` and
`CHECKARGS` both sat in the table aborting "not implemented yet"
while this file, the README and `CLAUDE.md` all said every primitive
was implemented. **And the test that should have caught it asserted
nothing** — `TestPrimitiveCoverage` reported the numbers with
`t.Logf` and passed whatever they were. The same was true of
`internal/mpi`'s `TestFunctionCoverage`. A claim nobody could check
is worse than a known gap, because it stops anyone looking.

Three changes close it:

- **`registerStub(name, reason)`** is the only way to add a stub. It
  records the name and the reason in a `stubs` map *and* installs the
  abort in the same call, so the two cannot come apart. It exists in
  both `internal/muf` and `internal/mpi`.
- **`Implemented()` subtracts the stubs** and **`Stubs()` lists them
  with their reasons**, so a stub is counted as the gap it is. The
  map `Stubs()` returns is a copy, so a caller cannot quietly empty
  the real one.
- **Both coverage tests assert.** `TestPrimitiveCoverage` fails if
  anything is missing, if anything is a stub — naming it and its
  reason — or if `Implemented()` is not the number of nameable
  primitives. `TestFunctionCoverage` does the same against
  `Count()`. Each checks the *arithmetic* rather than a typed
  number, so implementing a primitive that a submodule bump
  introduced needs no edit to the test.

This is the primitives' equivalent of what has always made the
command count trustworthy: `dispatch_table.go` *is* the command
surface, and a name with no handler says so when typed.

**A submodule bump is expected to break these tests**, and should.
Moving to a new upstream release can add names to the generated
table, and a new name with no implementation is exactly what should
not pass quietly.

`registerStub` is currently unused in both packages, because there
are no stubs left — so the bookkeeping is covered by tests that add
a stub to the map and take it away again, rather than by a real one.
The four failure paths were each confirmed by mutation: making a
primitive a stub, deleting its registration, and the same two for an
MPI function.

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

**One divergence inside the primitives is deliberate and was not
recorded here until now.** Go's `regexp` is RE2, which has no
backreferences and no lookaround, so a pattern using either **fails to
compile** rather than matching something else — `compileRegex`,
`internal/muf/prim_string2.go:319`. Letting it fail is the safer of
the two: a pattern that silently means something different is worse
than one that refuses. A world using `\1` inside a `REG*` pattern is
what this breaks, and vendoring a PCRE-compatible engine is the fix if
one ever does. The question was raised in the original milestone plan
as a risk, settled in that code comment, and never made it into this
audit; it is here now because that plan file has been deleted.

---

## `muckhelp.html` — 9 of 109 names have no handler

`internal/game/dispatch_table.go` is every name Fuzzball 7 dispatches:
**109 rows, 100 with handlers, 9 without.** The count is exact rather
than estimated, because the table *is* the command surface and a name
with no handler says so when typed.

The nine, and why:

    @memory  @usage  @reconfiguressl  @tops  @teledump   deliberate
    @armageddon  @restart                                lifecycle
    @mcpedit  @mcpprogram                                MCP editor

`@sweep` has landed, which took the LISTENER flag with it: nothing
maintained the flag before, so the command could not have told the
truth. See CLAUDE.md for why the flag is set in one place, cleared in
none, and derived from the properties at load.

`@teledump` joins the deliberate list: it base64-encodes the flat-file
dump over the connection, and this server has no dump file to send.

`@armageddon` and `@restart` are implementable but touch process
lifecycle and deployment rather than the game — armageddon exits
*without* writing, which write-behind makes a deliberate choice rather
than a free one, and restart needs a supervisor to restart into. They
say "not yet", which is accurate.

This is a different shape of gap from the one this document opened
with, twice over. It began as **verbs, not engine** — about forty
commands whose properties, locks and primitives already worked. Those
all landed, and the characterisation flipped: what remained needed
machinery the server did not have, `enter_room` and the containment
rules, program registration, the compiler conditionals, the
propqueues.

**That machinery is built now.** What is left is not a shape at all
but four names and a handful of recorded divergences, each listed
below.

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

### Wizard

    @memory  @usage

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

`choose_thing` itself is ported now, all four tie-breaks and the coin
toss. Two things this document used to say about it were wrong. It
decides an **exact**-match tie and nothing else — a partial match
overwrites `last_match` and is reported as ambiguous instead — and
`check_keys` is passed by `do_move` and `do_get`, **not** by
`do_drop`: all three of its call sites are `move.c:747`, `:835` and
`:850`, and the last two are both inside `do_get`.

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
reach a trie with four different tie-breaks; and adding the forty
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

All three of `do_look_at`'s recorded gaps have now landed:
`@teleport`'s wording, the LOOK propqueue, and **look traps** — see
below.

### The permission refusals, half fixed

Upstream's `match_controlled` refuses with "Permission denied. (You
don't control what was matched)". Emerald said "Permission denied."
everywhere, which was right nowhere.

**Five commands are fixed**: `@name`, `@describe`, `@set`, `@unlock`
and the whole `@lock` family really do route through
`match_controlled` upstream, and now say what it says.
`matchControlled` in `internal/game/build.go` is that function.

**Four were not, and the reason is behavioural rather than textual.**
None of them goes through `match_controlled` upstream; each matches
for itself and applies its own rule:

- `@unlink` also accepts `controls_link`, so upstream lets the
  destination's owner unlink an exit where Emerald refuses them.
- `@link` — **done.** It lets a builder who controls nothing *seize*
  an unlinked exit, because `do_link` tests `controls` only when the
  exit already points somewhere (`create.c:161`). See "What `@link`
  still diverges on" below for two things in it that are not
  permission refusals and are not fixed.
- `@recycle` — **done.** It is **stricter** than `controls`:
  upstream demands actual ownership per type, even of a wizard, with
  a different sentence for each of room, thing, exit and program. So
  this server used to recycle objects upstream refuses — the one
  place it did *more* than upstream rather than less.
- `@teleport` — **done.** Its test depends on the destination, so it
  cannot happen at match time at all, and what it asks differs per
  victim type. `internal/game/teleport.go` is `do_teleport`, with the
  three refusals as unit tests because the oracle's player controls
  everything in the fixture.

All four are now ported, and `resolveControlled` is gone with them.

### `ObjExists` is not `OkObj`, and one word hid two branches

`absolute_name` (`match.c:333`) guards on **`ObjExists`**
(`db.h:440`), which is only `d >= 0 && d < db_top` — so a dbref may
name a **garbage** object. `World.Valid` is **`OkObj`**
(`db.h:462`), which is `ObjExists` *and* not garbage, and
`Matcher.Absolute` was using it.

So no recycled object could be named by number, and two branches
written to handle exactly that were unreachable: `do_recycle`'s
"That's already garbage!", and the `<recyclable>` description
`@examine` shows for something still pointed at. Found because
`@recycle`'s own garbage branch could not be tested after its
permission rules were ported.

`internal/match/garbage_test.go` pins it, including that a ref past
the end of the database still matches nothing.

### The unconditional `match_player`

Found while porting the three above, and it is a *matching* fault
rather than a permission one, so it is recorded separately.

`match_everything` (`match.c:712`) ends with

```c
if (Wizard(OWNER(md->match_from)) || Wizard(md->match_who))
    match_player(md);
```

so a player search happens only for a wizard. Four of Emerald's
matches added it outright: `matchControlled`, `resolveControlled`,
`@chown`'s own and `examine`'s own. A **mortal** could therefore name
any player in the game as a target, and the control test refused them
— so the symptom was the wrong *message*, "Permission denied. (You
don't control what was matched)" where upstream, having matched
nothing at all, says "I don't understand 'X'." Programs match on both.
It reached `@name`, `@describe`, `@set`, `@unlock`, the whole `@lock`
family, `@link`, `@unlink`, `@recycle`, `@chown` and `examine`.

**No golden case can see it.** The oracle drives `#1`, a wizard, for
whom `match_everything` adds `match_player` anyway, so both servers
agree whichever way it is written. `internal/game/matcher_test.go`
covers it, including the half that must keep working: a wizard still
reaches a remote player.

**Two more call sites are wrong and are not fixed here**, because
their matchers are structurally different rather than merely too
wide, and each needs its own work:

- **MUF `MATCH`** (`internal/game/muf.go`'s `mufHost.Match`).
  `prim_match` (`p_db.c:765`) does not call `match_everything` at
  all: it is `match_all_exits`, `match_neighbor`, `match_possession`,
  `match_me`, `match_here`, `match_home` and `match_nil` — or
  `match_registered` alone when the name begins with `$` — and adds
  `match_absolute` *and* `match_player` only when
  `Wizard(ProgUID) || mlev >= 4`. So Emerald's version both includes
  searches upstream excludes (registered and absolute, always) and
  omits two it has (home and nil). This belongs with the recorded
  `RMATCH`-on-`Matcher.Inside` work, which is the same kind of
  rebuild.
- **MPI's matcher** (`internal/game/mpi.go`'s `mpiHost.Match`), which
  has the same unconditional `Player()` and several callers whose own
  C was not checked.

### `@link` was wrong in five ways, and one hid the others

The walkthrough's first script found it. `btut2` registers a room and
then links an exit to `$name`, and that answered "I don't understand
'$ai'." — so the transcription paid for itself on its first page.
Pulling that thread found four more, each hidden behind the one in
front of it.

**The matcher was hand-built where upstream calls
`match_everything`.** `parse_linkable_dest` (`db.c:1971`) is
`init_match(NOTYPE)` plus `match_everything`, `match_home` and
`match_nil`. What stood here omitted `match_registered` — the
reported symptom — and `match_all_exits`, took `match_player`
unconditionally where `match_everything` gates it on wizardry, and
asked for a preferred type that upstream does not pass.
`Matcher.Everything` already *was* `match_everything`; the chain
beside it was the bug.

**That hid a missing loop check.** With no `match_all_exits` an exit
could not be named as a destination, so `@link` could not build a
ring and nothing noticed that nothing checked for one.
`exit_loop_check` (`predicates.c:289`) is a full recursive walk, and
upstream runs it in all three places an exit's destination is set,
each with its own wording: `_link_exit` (`db.c:2117`) says
"Destination X would create a loop, ignored.", MUF `SETLINK`
(`p_db.c:1805`) says "Link would cause a loop.", and `SETLINKS_ARRAY`
(`p_db.c:3931`) says "Destination would create loop."
`CLAUDE.md` twice said upstream had no such check and that
`maxMetalinkDepth` was therefore a deliberate divergence. **Both
claims were wrong** and are corrected; the depth bound stays as a
backstop, because Emerald is handed dumps it did not write.

**`can_link_to` was `can_teleport_to` under another name.** Upstream
keeps the two apart with a comment saying the rules could diverge
(`predicates.c:89` and `:117`); this had collapsed them and kept the
wrong one. So there was no type argument and therefore none of the
four type rules — a thing's home could be a program, a room's dropto
could be an exit, a player could be homed to a thing — HOME and NIL
were not special-cased, and **`@linklock` was never consulted on the
link path it is named for**: written by its command, shown by
`examine` as "Link_OK Key", and read only by `can_teleport_to`.
`Linkable` (`db.h:576`) was also inverted: a **room or thing** is
linkable when ABODE is set and anything else when LINK_OK is, where
the old test asked LINK_OK first and then ABODE for everything but a
thing — right for neither.

**`parse_linkable_dest`'s own `can_link` refusal was missing**, "You
can't link that." It is unreachable today, because only the exit path
reaches that function and `linkExit` has already made the same test;
it is upstream's line and sits where upstream has it.

**And the three operations were sharing one matcher when upstream
gives each its own.** `do_link` calls `parse_linkable_dest` from the
**exit** branch only; the home branch (`create.c:238`) and the dropto
branch (`:269`) write their lists out inline, and the two differ from
each other as well as from `match_everything`. The home branch has no
`match_home`, so a thing's home cannot be set to HOME; the dropto
branch has one, so a dropto can be, and has neither `match_me` nor
`match_here`, so the room being linked cannot be named. Sharing one
function made all of that wrong at once.

One more thing fell out of the mutation pass rather than the reading:
`resolveLinkTarget` returned HOME and NIL **early**, before the three
checks, which made `can_link_to`'s own HOME and NIL cases dead code —
a mutation deleting the NIL case survived until the short-circuit
went.

Thirteen mutations, all thirteen caught; two need the unit tests,
because `controls` short-circuits the flag test and the link lock for
anyone the oracle can be.

**Still divergent: `@link` cannot build a multi-destination exit.**
`_link_exit` (`db.c:2040`) splits the destination string on `;` up to
`MAX_LINKS`, validates each, and returns how many were linked.
Emerald resolves one destination and writes a one-element list.
`trigger()` already *traverses* a list, so the gap is only in
creating one, and the symptom is that `@link exit=roomA;roomB`
silently links the first alone. A world with metalink fan-out cannot
be built from inside the game.

One invented message was removed rather than recorded: `@link` with
an empty destination said "Link it to what?", which appears nowhere
in upstream. It shadowed two real answers — an exit is charged for,
transferred, and told "No destinations linked.", because
`_link_exit`'s loop never runs and so never matches anything, while
every other type reaches the matcher and gets
`noisy_match_result`'s.

### MUF `SETLINK` and `SETLINKS_ARRAY` have almost none of their rules

Noticed while tracing `exit_loop_check`'s three call sites.
`prim_setlink` (`p_db.c:1780`) runs `prog_can_link_to`, a mucker-or-
ownership permission test, "Exit is already linked.", the loop check,
and then a per-type switch — a player's home, a thing's home with
`parent_loop_check`, a room's dropto. `internal/muf/prim_db.go:1374`
validates that the destination exists and calls `h.SetLinks`. The
same is true of `SETLINKS_ARRAY` against `p_db.c:3903`, which also
enforces "Only one player, room, or program destination allowed."
Both are reachable at mucker 3, so a program can do what `@link`
refuses. Not fixed here: it is a primitive apiece, not a branch of
the command this commit was about.

### MPI read every number wrongly, in four separate ways

One of these was recorded in advance; the other three were found by
putting a non-numeric argument through every function that takes a
number and letting the oracle answer.

**1. The invented abort — fixed.** `atoiArg` refused anything
unparseable with "Non-numeric argument." That string exists nowhere
in Fuzzball: `grep -rc "Non-numeric" fuzzball/src/` finds nothing,
and the complete list of upstream's MPI aborts contains no
numeric-parse error of any kind. Every `mfn_*` reads its numbers with
a bare `atoi`, which takes digits until the first character that is
not one and yields **zero** for anything else — so `{add:abc,3}` is
3 and `{add:12abc,1}` is 13. MPI is evaluated over descriptions and
succeed/fail messages, which routinely receive whatever a player
typed, so the invented abort turned silent upstream behaviour into a
visible error across about thirty functions.

Where upstream does refuse, it refuses on **range** after parsing:
"Out of range time argument.", "Too many dice!", "Fieldwidth too
big.", "Invalid process ID.", "Time period too short.", "Delaying
more than a year in MPI is just silly."

**2. The justification functions — fixed, and one was a crash.**
`{left}`, `{right}` and `{center}` all take `Min: 1`, so the
one-argument form is legal — and `pad` indexed `args[1]`
unconditionally, so `{left:hi}` **panicked**. Upstream falls back to
the descriptor's reported width and then to 78 (`mfuns.c:3833`),
which needed a `DescrWidth(descr)` on the MPI host: upstream reads
`d->detected_width` off the descriptor the evaluation is running for,
where Emerald's existing `Width(obj)` resolves a player's
least-idle connection instead. Two refusals were missing with it —
`{left:hi,9999999}` really did build a ten-million-character string
where upstream aborts "Fieldwidth too big." above `BUFFER_LEN - 1`,
and an explicitly empty pad string was silently treated as a space
where upstream aborts "Null pad string."

**3. `{max}`, `{min}` and the four ordering tests do not use `atoi`
at all — not fixed.** They use `msg_compare`, which compares
numerically only when **both** arguments are numbers and as strings
otherwise. And `{max}`/`{min}` return the argument *text* rather
than a number, taking exactly two arguments rather than folding over
many. Measured against the oracle: `{max:abc,2}` is `abc`,
`{min:abc,2}` is `2`, `{gt:abc,1}` is `1` and `{lt:abc,1}` is `0`.
Emerald answers `2`, `0`, `0` and `1`. `{eq}` and `{ne}` already
have the string-fallback shape and are right; the other six need it.

**4. `{inc}` and `{dec}` are variable operations — fixed.**
`mfn_inc` (`mfuns.c:2279`) reads a **variable name**, aborts "No
such variable currently defined." when there is none, adds an
optional amount, and **writes the result back** to the variable.
Emerald implemented both as arithmetic on the argument, so
`{inc:abc}` answered `1` and `{inc:5}` answered `6` — neither of
which upstream can produce, since naming a bound variable is the
only way to reach the arithmetic at all. Upstream's doc comment for
`mfn_dec` says "The variable is not updated."; the code updates it
(`:2329`) and the oracle agrees with the code.

**6. A rebinding shadows upstream and overwrote here — fixed.**
Found by a probe written for item 4 and kept because it measured
something real.

Upstream's `varv` is a **stack**: `new_mvar` (`msgparse.c:871`)
appends without looking for the name first, `get_mvar` searches
**backwards** so it finds the newest, and `free_top_mvar` pops one.
So binding a name that is already bound **shadows** it and the outer
value comes back. `Env.SetVar` searched for an existing name and
*overwrote* it, and `PopVar` then removed whatever was last — so the
outer binding was destroyed rather than restored.
`{with:n,1,{with:n,2,{&n}}{&n}}` was an "Unrecognized variable."
error where upstream answers `21`.

The cause was one function doing two jobs. `Env.BindVar` now pushes,
which is what `{with}` and the looping functions need, and
`Env.AssignVar` writes to the **innermost** binding and reports
whether there was one, which is what `{set}`, `{inc}` and `{dec}`
need — upstream writes through the pointer `get_mvar` hands back, so
it pushes nothing. `AssignVar` searches newest-first to match
`get_mvar`; `SetVar` searched oldest-first, so reading and writing
disagreed the moment two bindings shared a name.

The split is exactly **ten binding sites** — `{with}`, `{for}`,
`{foreach}`, `{parse}`, `{filter}`, `{fold}` (two), `{lsort}` (two)
and `{commas}` — which is also how many occurrences `mfuns2.c` has
of each of the two refusals, and that agreement is what confirmed the
partition. Both refusals were missing: Emerald answered an invented
"Variable limit exceeded." and never checked a name's length at all.
They are "Too many variables already defined." and "Variable name
too long.", each reported by the function that asked.

The three ambient variables every evaluation declares — `how`, `cmd`
and `arg` (`do_parse_mesg_2`, `:1940`) — are `new_mvar` pushes too,
so the game-side callers bind rather than assign.

**7. `{with}` returns only its last body — fixed.** Found while
fixing item 6. `mfn_with` (`mfuns2.c:1039`) parses each argument
after the first two into **one reused buffer** and returns the
pointer afterwards, so every body but the last is evaluated for its
side effects and discarded: `{with:n,5,a,b,c}` is `c`, where this
server answered `abc`. `{for}` already had the right shape for the
same reason — its `out.Reset()` — so the two disagreed about one
question.

**Two things measured and deliberately left alone.**

"Too many variables already defined." is **unreachable by nesting**:
`MPI_MAX_VARIABLES` is 32 and the recursion limit is 26, so thirty-
three nested `{with}` calls hit "Recursion limit exceeded." first.
The refusal is implemented and correct, but no golden case can reach
it that way.

That probe also showed upstream's MPI error reporting **walking back
out**, naming each enclosing function — a line of `{WITH} (arg 2)`
per level. That is the already-recorded gap under "MPI error
reporting does not walk back out", and this is the first measurement
of it.

All seven are done. The golden case
(`internal/golden/atoi_test.go`) deliberately omits them and says so,
so that it tests the fix it belongs to rather than passing over a
different bug.

### MPI's {force} is implemented, and the force lock is now read

Found by the propqueue golden case, which tried to use `{force}` to
drive the recursion limit.

`mfn_force` (`mfuns2.c:2753`) has an **unblessed path** Emerald did
not have. It refused every unblessed `{force}` with "Permission
Denied."; upstream refuses only when `allow_zombies` is off, and with
it on an unblessed force proceeds and is then subject to six refusals
of its own, four of which apply to a THING alone:

- DARK — "Cannot force a dark puppet."
- the owner flagged ZOMBIE — "Permission denied."
- a no-puppets room, which is ZOMBIE on the **room** — "Cannot force
  a Puppet in a no-puppets room."
- a first word that is a player's name — "Cannot force a thing named
  after a player."
- no XFORCIBLE — "Permission denied: forced object not @set
  Xforcible."
- the force lock — "Permission denied: Object not force-locked to
  trigger."

So a world that allows zombies had a whole mechanism — puppets forced
from their own descriptions — that could not run here at all. Two of
those read "Permission denied." with a lower-case d where the
`allow_zombies` refusal has a capital D; that is upstream's and the
golden case pins it.

Two more sit **outside** the blessed test and so refuse a blessed
force too: God, and `force_level > max_force_level - 1`. An unblessed
force never reaches the God one, because XFORCIBLE refuses a player
first. And the command string is a **list** split on carriage
returns, with the name-after-a-player test repeated per command for
anything that is not a player — its message carries a "[2]".

**The force lock was evaluated nowhere.** `@/flk` could be set with
`@flock` or `@force_lock` and was shown by `examine`, but neither
`{force}` nor `@force` ever read it, so a lock whose whole purpose is
to say who may force a puppet protected nothing. Upstream reads it in
both (`mfuns2.c:2820` and `wiz.c:563`), and an unset lock is **false**
— `test_lock_false_default` (`boolexp.c:906`) returns 0 rather than
passing. The wording differs between the two callers: `@force` says
"force-locked to **you**" and `{force}` says "to **trigger**".

`@force` was missing a second refusal with it: a no-puppet zone,
"Sorry, but that's in a no-puppet zone." (`wiz.c:567`).

**Two smaller `do_force` divergences are recorded and not fixed.**
Its `allow_zombies` gate is `!Wizard(player) || Typeof(player) !=
TYPE_PLAYER`, so upstream refuses a *puppet* forcing even when its
owner is a wizard, where Emerald tests the wizard bit alone. And its
last three refusals ask `Wizard(OWNER(player))` where the three above
them ask `Wizard(player)` — the same for a player, who owns
themselves, and different for a puppet. Both only bite when the
forcer is not a player, which needs its own thought.

### A nested MPI failure is several lines, not one

Found by the `{force}` work above, which is why the two landed
together.

The innermost `ABORT_MPI` notifies its own message, and then **every
enclosing function that was pre-parsing an argument** notifies
`{NAME} (arg N)` as the NULL propagates out (`msgparse.c:1637`). So
`{null:{force:me,look}}` prints the `{FORCE}` refusal *and*
` {NULL} (arg 1)`, and a two-deep nest prints two frames. The
argument's **position** is reported, not just the function, so
`{midstr:abc,{force:me,look}}` ends ` {MIDSTR} (arg 2)`.

Emerald printed the first line and stopped, so anything that failed
inside a nest gave no indication of where it had failed from.

Two details. The frames carry **no colon**, unlike the message line.
And a function that does not pre-parse its arguments adds no frame at
all — `{lit:{force:me,look}}` prints nothing whatever, because the
inner call never runs.

Upstream notifies each line as it unwinds. Emerald reports a failure
once, from `Eval`, so the chain is carried on `mpi.Error.Trail`
instead — which also keeps the lines in upstream's order, where
notifying during the unwind would print the innermost message last.

### The instruction limits, and a mucker level fixed at compile time

`instr_slice`, `max_instr_count` and `max_ml4_preempt_count` were
read nowhere: every `Run` passed an empty `muf.Limits{}`, so the
compiled-in defaults applied and all three parameters were inert.
Wiring them up meant fixing the rules they drive, because **none was
applied the way `interp_loop` applies it**.

- **Preempt mode never yields.** The instructions run since the
  resume are capped instead: at mucker 4 by `max_ml4_preempt_count`
  when it is non-zero, otherwise by `max_instr_count`, aborting
  "Maximum preempt instruction count exceeded" (`interp.c:1731`,
  `:1743`). A **zero** `max_ml4_preempt_count` disables the check
  rather than meaning a default — upstream resets the counter
  (`:1735`). And a program flagged **BUILDER** counts as preempt
  whatever its mode says (`:1728`), which is what "B" means on a
  program.
- **Foreground and background yield in two parts**: the frame must
  have run `instr_slice * 4` instructions *and* `instr_slice` since
  the resume (`:1753`). So a short program never yields at all,
  where a single budget made it yield at the slice.
- **The total ceiling applies only below mucker 3**, at
  `max_instr_count` for level 1 and **four times** that for level 2
  (`:1876`), aborting "Maximum total instruction count exceeded."
  At level 3 and above there is none. This server capped every
  program at one unconditional figure and aborted with an invented
  message.

**A command or an exit runs FOREGROUND**, which `move.c:685` passes
and which this server left at the zero value — PREEMPT. Every other
launch site already said which mode it wanted; the command path was
the one that did not, and silence meant the wrong answer. It is the
difference between a program that yields and one that does not.

**`Frame.MLevel()` is the compile-time level, and upstream's is
per-run.** `mlev = ProgMLevel(program)` is read inside `interp_loop`
(`interp.c:1706`) and **re-read whenever execution enters another
program** (`:2180`, `:2300`, `:2324`, `:2575`), so it is a live
value. Emerald computes `min(program, owner)` once at compile time,
bakes it into `Program.MLevel`, and caches the compile by ref — and
`@set` does not invalidate it. So a program's mucker bits take
effect only on its **first** run, and a frame executing a library's
code uses the *caller's* level rather than the library's.

That affects **every mucker gate in the interpreter**, not just the
ceiling above, and it needs its own work: a runtime level on the
frame, separate from the compile-time one the compiler legitimately
wants for `$ifcancall`. `internal/golden/instrlimit_test.go` works
around it by giving each level a program of its own, compiled cold.

Two things that cannot be compared are left alone. The
**nested-interp loop counters** — `max_nested_interp_loop_count` and
`max_ml4_nested_interp_loop_count` — need a count of INTERP nesting
that Emerald does not keep, so those two parameters stay inert. And
the **instruction a MUF error report names** is masked in that case:
upstream renders the current instruction with `insttotext`, so an
abort on a jump reads "IF->line4", where this server's report names
only primitives and leaves it empty. The two compilers emit
different code for the same source, which is the same reason the
debug trace is compared by source line rather than instruction for
instruction.

### "home" is a direction, and four creators skipped ok_object_name

`home` was in the command table, which is consulted **after** exit
matching, where `can_move` (`predicates.c:509`) answers yes for it
outright when `enable_home` is set -- so upstream makes an exit of
that name unreachable and this server let the exit win. The
precedence was the other way round.

`do_move`'s branch (`move.c:730`) is also more than a move. It
announces "X goes home." to the room, says "There's no place like
home..." **three times**, adds "You wake up back home, without your
possessions.", and then `send_home` (`move.c:1262`) sends the
player's **contents** home *first* -- upstream's own comment
explaining the order, so they see their possessions on arrival. This
server printed one line and left the inventory alone: a quiet
teleport where upstream is a small ceremony with a cost. It also
invented "You have no home to go to." and, when `enable_home` was
clear, "That command is disabled." Upstream has neither; with the
parameter clear the word is ordinary and falls through to exit
matching.

**Two further gaps came out of writing the case for it.**

**`ok_object_name` was missing from four creators.** It lives inside
`create_action`, `create_room`, `create_thing` and `create_program`
(`db.c:216`, `:314`, `:357`, `:257`) rather than in the commands that
call them -- the same shape `create_program` already had here, which
is why `@program` had the check and `@open`, `@dig`, `@create` and
`@action` did not. So a world could make an exit called "home",
"me", "here" or "nil" that nothing could ever refer to, because the
matcher claims all four before it looks at anything. Each creator has
its own refusal: "You cannot use that name for an exit or action.",
"...for a room.", "...for a thing.", "...for a program."

`nameForbidden` was also folding with `strings.ToLower` where
`CLAUDE.md` requires `internal/ascii` -- upstream's `strcasecmp`
folds only A-Z.

**`cmdGo` invented both of its failure messages.** A name that
matches nothing is **`noisy_match_result`'s** to report, and
`do_move` returns silently once it has spoken (`move.c:751`). "You
can't go that way." belongs to a different case entirely -- an exit
that was *found* and then failed `could_doit` -- so it was the wrong
answer for a name that matched nothing, and "I don't know which way
you mean." for an ambiguous one is upstream's nowhere.

### Two preferred types are still unset

`choose_thing`'s type preference is wired at the thirteen call sites
that can reach it. Two of upstream's cannot be wired as things stand:

- **`RMATCH`** (`p_db.c:888`, `TYPE_THING`) is not a `Matcher` in
  Emerald at all — `internal/muf/prim_db.go` walks the container's
  contents and exits itself with a case-folded name comparison, so
  there is no tie to break and no preference to express. Worth
  rebuilding on `Matcher.Inside`, which is already `match_rmatch`.
- **The `connect` action** (`interface.c:1084`, `TYPE_EXIT`) is looked
  up at login, and Emerald reaches it through `can_move` rather than
  its own match.

Neither is observable without two exact matches of one name in one
container, which is why they are recorded rather than chased.

### Boarding a vehicle is covered now

`trigger()` boards a thing when the exit is *inside* it and it is a
VEHICLE -- `dest == LOCATION(exit)` -- so making one needs `@action`,
which attaches an exit to a named object. `@open` always attaches to
the room, so while `@action` was its alias no boarding exit could
exist at all and the code path was unreachable.

`internal/golden/board_test.go` covers it, and the code turned out
correct -- the first uncovered path this session that was. Disabling
the boarding test changes thirteen transcript lines, so the case does
reach it.

**The vehicle had to be dropped first.** `@create` leaves it in the
player's inventory, and entering something you are carrying is a
loop: both servers answer "That would cause a paradox." and the case
compared clean while boarding nothing. That is the third time this
session a case has passed over `@create`'s placement -- the others
were the no-puppets-room run in `force_mpi_test.go` and an earlier
`@recycle` probe. **Worth a habit: after writing a case, read one
transcript rather than only comparing two.**

Two of `trigger()`'s guards are still unreached and cannot be from a
player: "a VEHICLE may not enter a VEHICLE" and "a non-wizard THING
may not enter a ZOMBIE room" both test the object being *moved*, and
a player is neither. They need a thing traversing an exit -- one that
fetches it into a vehicle -- which is its own case.

Naming an object from *inside* it does not work either: `@contents
bus` and `@teleport bus=tram` both answer "I don't understand 'bus'."
on both servers, which measures the matcher rather than boarding.

### The MUF error messages: one divergence, and one that was not

Found by probing four deliberate program failures through both
servers while writing the ANSI case, and **re-measured since** --
which corrected the record.

- **`+` was recorded as a divergence and is not one.** This document
  said upstream names the instruction `++` and says "Invalid
  datatype." Driving `1 "a" +` through both servers, caught and
  uncaught, gives byte-identical output either way: the instruction
  is `+` and the message is "Invalid argument type.", which is what
  this server already said. The entry was wrong in both halves.
- **`SETNAME` -- fixed, and it was worse than an ordering.** The
  recorded complaint was that it checks permission before the
  argument type. It did, but the permission it checked was a
  **mucker-4 floor that does not exist**: `prim_setname`'s rule is
  `(mlev < 4) && !permissions(ProgUID, ref)` -- a wizard *or*
  whoever has permissions on the object -- so a mortal program could
  not rename an object it owned, and the refusal said "Permission
  denied.  Requires Wizbit." where upstream says a bare "Permission
  denied."

  The false floor came from a **second**, bare `if (mlev < 4)`
  nested inside `if (Typeof(ref) == TYPE_PLAYER)`. The mucker-level
  generator's `conditions()` yields that inner test without its
  enclosing one, so it read as unconditional. `CLAUDE.md` already
  warns the extractor is approximate; this is the first case of the
  **enclosing-guard** shape rather than the same-condition one it
  already skips, and `gen_mlev.py` has a `CONDITIONAL_FLOOR` set for
  it now.

  The inner check is still live, for exactly one case the outer test
  lets a mortal through: a program renaming its **own owner**, where
  `permissions()` answers true because `thing == player`.

  `permissions()` (`interp.c:2706`) is **not** `controls()` -- no
  wizard escape at all, and false for any player but the asker. It
  is ported as `Frame.permissions` because several primitives pair
  it with `mlev < 4` to mean "a wizard or the owner".
- A third has been fixed: the stack underflow aborts said "stack
  underflow" where upstream says "Stack underflow."

**Worth re-checking the rest of the mucker table for the same
shape.** `SETNAME` was found by chasing a message; nothing surveys
the 34 gated names against their enclosing guards, so another false
floor would look exactly like a deliberate one.

`STRCAT`'s "Non-string argument." agrees exactly, which is why the
ANSI case uses it to produce an error report.

### Look traps, and a function whose name is a lie

`do_look_at`'s second branch (`look.c:369-439`) is the `_details`
propdir, and this server did not have it. Two things came with it.

**`look` takes two arguments and this server passed one.** Upstream's
`do_look_at(descr, player, arg1, arg2)` means
`look <thing>=<detail>` is a syntax Emerald simply did not accept.
`arg1` is trimmed both ends and `arg2` is **left-trimmed only** —
`remove_ending_whitespace` against `skip_whitespace_var`
(`game.c:701-709`) — and the empty-or-`here` test is made on `arg1`
alone, so `look =foo` shows the room and ignores the detail.

**Which object is searched, and for which word, depends on how the
branch was reached.** Nothing matched, so the *room*'s details are
searched for what was typed; or something matched and a detail was
given, so *that object*'s details are searched for the detail. An
object of the same name therefore beats a trap outright — the match
is tried first and only a failed one reaches the details — which is
the shape of upstream's own `@TODO` at `look.c:380`, flagged there as
"kind of ... technically wrong maybe" and reproduced rather than
improved.

Only a **string**-valued property runs, through `exec_or_notify` with
`(@detail)` as the caller context and the property's own blessing —
so `@bless` on a trap makes its MPI wizardly like any other message
property. A non-string trap falls through to the no-match messages,
so a dbref-valued detail reads as if it were not there at all. The
walk is in nextprop order and stops at the **second** match rather
than choosing between them.

**`exit_prefix` (`fbstrings.c:120`) is not a prefix test.** That is
the single most misleading name in that file, and `look.c:411` is its
only caller. It walks the `;`-separated aliases of the property name
and wants the typed word to equal one of them **whole**, folded: so
`look feh` matches a trap called `feh` or `feh;foo`, and `look fe`
matches neither.

Its whitespace rules fall out of where the C happens to leave its
cursor and are not what anybody would write deliberately — so
`detailMatches` is a direct port of the pointer walk rather than a
split-and-compare, and the thirty-one expectations in
`internal/game/exitprefix_test.go` were produced by **compiling
`exit_prefix` and running it**, the way the ANSI filters and
`env_distance` had to be. Three of them are worth stating:

- whitespace *after* an alias is skipped, so `"feh ;foo"` matches
  `feh`; whitespace *before* one is skipped only when a delimiter has
  just been consumed, so `"feh; foo"` matches `foo` but `" feh"`
  matches nothing at all;
- a trailing space in what the player typed defeats the match, which
  in practice never happens because `arg1` arrives trimmed; and
- an empty alias matches an empty typed string, so `"a;;b"` does and
  `"feh;"` does not — the outer loop stops at the end of the name and
  so never reaches a trailing empty alias.

`internal/match`'s `matchAlias` is deliberately not reused. It splits
an argument off at a space and compares against the whole typed line,
because an exit may take one; sharing a single function between the
two would make one of them wrong, for the same reason the two ANSI
filters are kept apart.

**An ambiguous name reports upstream's empty quotes.** `look.c:438`
passes the *detail* to `match_msg_ambiguous` rather than the name, so
an ambiguous name with no detail says
`I don't know which '' you mean!` Reproduced, because a program
matching on the line sees it — and reachable from a transcript where
an ambiguous *object* is not, since `choose_thing` tosses a coin for
two exact matches while two half-matching names are reported as
ambiguous deterministically.

`internal/golden/looktrap_test.go` is thirty-odd steps over the lot;
five mutations, all caught. One of its probes was wrong at first and
the transcript said so: two traps called `dup1` and `dup2` are not
ambiguous, because the match is exact — two can only collide through
a **shared alias**, which is what makes that branch reachable.

### `prim_moveto` is a type switch, not a move

`internal/muf`'s MOVETO was `h.MoveTo(victim, dest)` -- the raw store
move, which refuses only self-containment -- with a mucker floor of 3
in the generated table standing in for everything else. The floor was
not real: its `if ((mlev < 3))` at `p_db.c:234` opens a block of
extra mortal-only restrictions rather than refusing. So **every M1
and M2 program was refused outright, and every M3 and M4 one got an
unvalidated move**, and the primitive had no test of any kind.

Missing: the whole type switch; `enter_room` for a player, so no
announcement, no autolook and no arrive propqueue; `parent_loop_check`;
exit re-sourcing with its priority reset; room reparenting;
`secure_thing_movement`; the "Bad destination." validations; and
**thirteen** mlev-conditional refusals, none of which is a floor.

Four Host methods were added for it -- `EnterRoom`,
`ParentLoopCheck`, `CanTeleportTo` and `LastUsed` -- and the fifth
turned out not to be needed: **`World.MoveTo` already is
`unset_source` plus `set_source` for an exit**, because `chainHead`
picks the Exits list rather than Contents. `SetMLevel(victim, 0)` goes
with that branch: an exit's mucker bits are its *priority*, so
re-pointing one resets how hard it competes, the same reset `@unlink`
reports as "Action priority Level reset to 0."

`ts_lastuseobject` (`fbtime.c:70`) is **not** `ts_useobject`: it sets
the timestamp and leaves the use count alone, and walks up a room's
parents. Upstream's own comment calls which is used where "a little
arbitrary". `World.Used` was the only one this server had, so
`World.LastUsed` is new -- bounded, where upstream's recursion is not,
since a cycle in a damaged parent chain would take the server down.

Three structural details are upstream's and are reproduced as
written:

- **Two fall-throughs are load-bearing.** A PLAYER falls into the
  THING case for the loop check and the mortal-only block, then
  leaves through `enter_room` before the PROGRAM case; a THING falls
  into the PROGRAM case for the matchroom rule and the move itself.
- **The matchroom rule assigns twice without an `else`**, so the
  *victim's location* wins when both it and the destination are
  controlled.
- **The vehicle and zombie refusals read oddly.**
  `(FLAGS(dest) & VEHICLE) && Typeof(dest) != TYPE_THING` can only
  hold for a **room** flagged VEHICLE, which is how upstream spells
  "a vehicle room".

`internal/golden/moveto_test.go` runs sixteen probes at mucker 1 and
again at 3. Nine mutations; six were caught at once and **three
survived**, each because the probe did not set up the shape it
claimed to test:

- the vehicle clause needed a vehicle thing moved into *another
  vehicle thing*, where the refusal does not apply;
- the matchroom rule needed the victim's location and the
  destination to be **different** rooms, both controlled, differing
  in JUMP_OK, with the victim owned by somebody else;
- and `can_teleport_to` is **not reachable from any transcript**,
  because it goes through `controls()` and the oracle drives a wizard,
  who passes it for everything. `permissions()` is pure ownership with
  no wizard escape (`interp.c:2706`), which is why the other
  mortal-only refusals *are* visible to the oracle while this one is
  not. `internal/game/movetoroom_test.go` runs a mucker-1 program
  owned by a mortal instead.

The strongest single probe is the player move: at mucker 1 Bob's
vault is refused with "Destination not JUMP_OK." and the hall
produces `Hall(#11R)` -- the autolook, so `enter_room` ran -- and at
mucker 3 both succeed, with the contents listing matching byte for
byte.

### `@set` is two commands, and its property form had no rules

The MUCK Manual's looktrap examples use
`@set here=_details/sign;plaque:...` rather than `@propset`, which is
how this came up at all: it is the form nobody had checked.
`do_set`'s property branch (`set.c:763-842`) is most of a second
command inside `@set`, and `cmdSet` had almost none of it.

**The one that mattered: no restricted-property guard.**
`propRestricted` (`internal/game/propset.go:32`) refuses a system
property to everybody and a hidden or see-only one — a path with a
segment starting `@` or `~` — to anybody who is not a wizard.
`@propset` has consulted it since it was written and `@set` never
did, so **`@set` wrote what `@propset` refused**. The system half
refuses a wizard too and so is oracle-visible; the other half needs
a mortal and is unit-tested.

**`@set <obj>=:clear` was missing entirely.** It removes every
property, and removes *less* for a non-wizard: `remove_property_list`
with `allp == 0` leaves the `@` and `~` properties and the whole
`_/` propdir alone, because those are not the asker's to remove. The
replies differ to match — "All properties removed." against "All
user-owned properties removed." — and anything but `clear` after the
bare colon is refused by quoting the syntax.

**`^N` is the only way to make an integer property from the command
line**, and look traps care, because a non-string detail does not
run. `^-7` works; `^abc` is not a number and falls through to being
the literal string `^abc`.

**Three invented messages are gone.** `do_set` has **no usage
message at all** — it matches, checks God's property, and an empty
flag arrives at "You must specify a flag to set." So a missing `=`
and an empty value give the same answer, where this server had
"Usage: @set ...", "Set what?" and "Set which property?". And
"Property cleared." is "Property removed."

**The two name trims, and their order, are the surprising part.**
Upstream right-trims whitespace *first* and only then strips a
trailing `/` — so a name ending `b  /` meets the whitespace loop,
which sees the `/` and stops at once, and **the two spaces
survive**. The property really is called `_test/b  ` and `_test/b`
does not exist. Doing the two in the other order gives `_test/b`,
a different property, and the golden case pins both; a transcript
reading each name back is what settled it, because the first version
of that probe looked for the wrong one and read empty.

The `/` strip is **unobservable on its own**: `props.split` drops
empty segments, so `_test/b  /` resolves to the same node as
`_test/b  `. A mutation removing just that line survives, correctly,
and the line is kept because it is upstream's step and because it is
what makes the order matter.

**One claimed divergence was not one.** I counted a seventh — that
`do_set` checks `strict_god_priv` itself (`set.c:752`) and answers
"Only God may touch God's property.", a wording upstream uses nowhere
else, where `wiz.c:429` and `:469` say "God's stuff". The guard is
**unreachable**, in upstream as much as here: `controls()` already
contains the same condition, and `do_set` calls `match_controlled`
before its own check, so every case it would catch has already been
refused with `match_controlled`'s wording. The observable behaviour
was correct before the guard was added. The line is kept because it
is upstream's and because `controls()` could change;
`TestSetPropGodGuardIsUnreachable` pins the fact rather than the
hope, so a change that makes it reachable is noticed.

Seven mutations, six caught, one a correct survivor — the `/` strip
above.

### ...and its flag form had no permission model at all

The other half of `@set`, and the worse half. `unable_to_set_flag`
(`set.c:537`) is a force-level guard, two mucker-bit rules that
interpolate the level into their message, and a per-flag, per-type
switch covering ABODE, GUEST, YIELD, OVERT, ZOMBIE, VEHICLE, DARK,
QUELL, BUILDER, WIZARD and XFORCIBLE. This server had
`wizardOnlyFlags`, a **six-entry map** of flag to "needs a wizard",
and `cmdSet` put every mucker level behind a blanket
`requireWizard`.

The answer depends on the object's **type** as much as on the flag,
on whether the flag is being set or cleared, on three `@tune`
parameters, and on whether a `@force` is running — none of which a
map can express. What was wrong:

**`@set me=!W` worked.** The only wizard in a world could strip its
own bit, with nothing to put it back; upstream answers "You cannot
make yourself mortal." Disabling the port makes the rest of the
golden script collapse into "Only builders are allowed to
@create.", "You are not allowed to @tune." and "Permission denied:
forced object not @set Xforcible.", which is what that one line
costs.

**YIELD, ABODE, ZOMBIE, VEHICLE and DARK were unguarded.** So the
restriction a wizard applies by setting ZOMBIE or VEHICLE *on a
player* — "this player may not use puppets" — did nothing at all,
`exit_darking` and `thing_darking` had no reader anywhere in the
server, and a mortal could make a program AUTOSTART.

**Three were guarded too tightly**, a divergence the other way.
XFORCIBLE is restricted on an **exit** and nowhere else, so a mortal
may make their own thing or program forcible; BUILDER on a program
is BOUND and asks `mlev < 2` rather than wizardry; and QUELL is a
God rule, not a wizard one — a mortal may set it on their own
things, and a plain wizard may *not* quell a colleague.

**`wiz_vehicles`, `exit_darking` and `thing_darking` gained their
only reader**, three of the 69 `@tune` parameters read nowhere.

**The force guard had no equivalent.** WIZARD and the mucker bits
may never be forced; XFORCIBLE may be forced on an exit and nowhere
else, which is exactly the type the switch refuses to a mortal —
the two guards are complementary rather than inconsistent.

**The generic refusal is "Permission denied. (restricted flag)"**,
where this server said "Permission denied."

Three smaller things in `cmdSet` itself. **`!` is read twice and the
two readings disagree**: upstream takes `negated` from the *first
character alone* and then skips every leading `!` and space to find
the name, so `!!W` **clears** the wizard bit — `has_flag`'s
"!!x = x" rule is a different function. **A bare `!` is an empty
flag name**, and this server read it as a mucker level, because the
empty string is a prefix of "mucker": `@set me=!` answered "Mucker
level reset." instead of "You must specify a flag to set."
**INTERACTIVE was missing from the flag table** (`db.c:2379` has
it), so `@set x=interactive` answered "I don't recognize that
flag." And the **guest guard** was absent: a guest may `@set` a
property, since the check sits after the property branch returns,
and exactly one flag — its own GUEST bit, and only while it is also
a wizard, which is how a world lets a guest stop being one.

**A quelled wizard is a mortal** for every `Wizard(OWNER(player))`
test in the function, which is what makes nearly all of it
comparable from the oracle's single God seat: the golden script
quells `#1` halfway through and the rest of the ladder runs as a
mortal. Quelling does *not* reach ABODE, which asks `TrueWizard`,
QUELL, which asks `God`, or BUILDER-on-a-program, which asks
`MLevel` — and the script checks those three stay permitted rather
than assuming it.

**Three probes in the first draft of that script tested nothing**,
all three passing because both servers agreed on a refusal that
arrived earlier:

- every `@force me=...` answered "You cannot force God to do
  anything." (`wiz.c:553`), so the force guard was never entered. A
  **thing** is a valid victim and a wizard needs neither XFORCIBLE
  nor a flock to force one; a ZOMBIE thing relays what it is told
  back to its owner, which is the only way the forced command's
  output is visible;
- behind that, `strict_god_priv` refused the forced thing with
  "Only God may touch God's property.", because God owns every
  object in the fixture;
- and `@set car=!V` from *inside* the car answered "I don't
  understand 'car'.", because the victim search is
  `match_everything`, which has no stage for the searcher's own
  location. Naming it `here` is what reaches "That vehicle still
  has players in it!"

Sixteen mutations, all sixteen caught; five of them need the unit
tests, because the rule they break is one a God-only transcript
cannot see.

### Two routes past `controls()` that this server does not read

Found while deciding whether the mucker rules'
`OWNER(player) != OWNER(thing)` clause can fire. `controls()`
(`db.c:1822`) has three ways to control something you do not own: a
wizard, `tp_realms_control`'s walk up the environment for a
W-flagged room you own, and an **ownership lock** —
`MESGPROP_OWNLOCK`, tested false-by-default. `Server.controls`
(`look.go:422`) reads only the first.

`realms_control` defaults off and is one of the 69 unread `@tune`
parameters, so nothing diverges today. The **ownlock does diverge**:
`@ownlock` writes `@/olk`, `examine` displays it as "Ownership
Key", and nothing in the server consults it — the same shape as
`_/oecho`. A world that hands out an ownlock finds it inert. The
immediate consequence for `@set` is that the ownership clause in
both mucker rules cannot fire here while upstream can reach it, and
the refusal a player sees is `match_controlled`'s wording instead;
`TestSetFlagMuckerOwnershipClauseIsUnreachable` pins that so the
change is noticed when `controls` is fixed.

### A trailing space on a second argument is lost

Also found by a probe, and wider than the command that found it.
Upstream answers `@set widget=kill_ok ` with "I don't recognize that
flag.": `string_prefix` (`fbstrings.c`) cannot match past the space,
and nothing right-trims `arg2` — `game.c:706` right-trims `arg1`
only, after the `=` split, and `process_command` does not touch the
line it is given.

This server trims the **whole line** at intake
(`command.go:153`), so the second argument of every `=`-taking
command loses its trailing whitespace. For most of the forty-odd
such commands it is invisible; for `@set`'s flag form it changes the
answer, and for `@propset` — where a value may deliberately begin or
end with a space — it changes what is stored. Fixing it means
left-trimming at intake and right-trimming each `arg1` at the split,
then auditing every command that reads `ctx.arg`, so it is recorded
here rather than folded into the `@set` work. There is deliberately
no golden probe for it: one would be a case that cannot pass.

### MPI ran on a brace and on nothing else

§2.2.2's looktrap is `` @set here = _details/sign;...:type `look
mailboxes' `` and the two servers disagreed about the **backtick**:
upstream consumed it, this server printed it. Not a looktrap bug —
it reproduces on a plain `@desc`.

`do_parse_mesg` (`msgparse.c:1400`) runs its scanner over every
message property whether or not the text contains a call, and the
scanner does three things besides evaluating one. A backtick toggles
literal mode and is consumed (`MFUN_LITCHAR`, `mpi.h:31`); `\r`
becomes a carriage return and `\[` becomes the escape character,
which is how a world writes ANSI into a description; and any other
`\x` passes through as `x`, which is the only way to write a
literal brace or backtick. **`evalMPIAs` short-circuited on "does
the text contain a `{`?"**, which was invented here — so every
description without a call in it was printed raw and all three
behaviours were missing.

### MPI could not resolve `this`

The same script's `{name:this}` answered "Match failed." `Host.Match`
was `match_everything` plus an unconditional player search, and
`mesg_dbref_raw` (`msgparse.c:667`) is a different function:

- **four keyword names first** — `this`, `me`, `here`, `home` —
  and upstream's own comment says matching `this` is unique to MPI.
  `me` and `here` happened to work through `match_everything`;
  `this` was not handled at all, so every `{...:this}` failed. `home`
  is reproduced and is **dead**: it yields HOME, which the
  function's own closing `OkObj` check rejects, so `{name:home}` is
  "Match failed." upstream too;
- then a **five-stage** search — absolute, all exits, neighbour,
  possession, registered — with no `match_me` or `match_here`,
  because the keywords have answered those, and `match_absolute`
  without the wizard gate `match_everything` puts on it;
- then, if that found nothing, **the same search again around the
  object carrying the message** (`init_match_remote`), with
  `match_player` in front. So a description on an object elsewhere
  can name what is near *it*. `Matcher.Around` is that constructor
  and had been written with no caller at all.

Not ported, and recorded instead: the three wrappers around
`mesg_dbref_raw` have **different permission rules and a second
failure value**. `mesg_dbref` (`:742`) applies `mesg_read_perms`,
`mesg_dbref_strict` (`:775`) demands the blessed bit or common
ownership, and both answer `PERMDENIED` where the raw form answers
`UNKNOWN` — two outcomes with two messages, where `Env.resolve` has
one path and no permission test.

Five mutations, all five caught. The first attempt at one of them
did not compile, so it tested nothing until it was rewritten without
the import it had removed.

### `examine` printed the stored lock, not the rendered one

§2.3's locks. A lock is **stored** unparsed with
`unparse_boolexp`'s fullname argument *off*, so the property holds
`#1&!#1`; `displayprop` renders a key by re-parsing it and
unparsing it with that argument *on*, which gives
`One(#1PWM3)&!One(#1PWM3)`. `lockText` already did exactly that, for
the property listing — `examine`'s own seven-key block called
`lockString` and printed the raw stored string, so every key in
every `examine` showed bare dbrefs.

### `compatible_priorities` had no reader, and exit priority was wrong

§2.3.3. `match_exits` (`match.c:588`) **promotes a
default-priority exit from 1 to 2** when `compatible_priorities` is
set, which it is by default — one of the 69 `@tune` parameters
nothing in this server consulted.

Without the promotion an exit hanging on a THING beats a plain exit
on the room: both are `PLevel` 1, and a strictly higher level
overwrites an earlier stage's match while an equal one does not. With
it both reach 2, the tie goes to the stage that searched first, and
the room's exit wins — which is the legacy behaviour the parameter is
named for. The manual's own example is a global `bank` against a
local one, and the two servers picked differently.

The promotion is **withheld** from an exit on a thing whose owner
does not control where the searcher is standing, so somebody else's
puppet cannot outrank the room you are in. No transcript can reach
that branch, because the oracle's player owns every object in its
world, so it is a unit test — as is the parameter being off, and a
mucker bit making the level explicit so the promotion never applies.

Porting it moved `controls` and `ownerOf` into `internal/world` as
`World.Controls` and `World.OwnerOf`: the test is
`controls(OWNER(exit), LOCATION(match_from))`, `internal/match`
cannot import `internal/game`, and `Server.controls` never used its
receiver. The two names in `internal/game` now delegate.

Five mutations across the two, all five caught.

### `{&cmd}` and `{&arg}` were empty everywhere

§4.2's multi-action is one action carrying several names that answers
differently for each, through `@fail` set to `{exec:{&cmd}}` — and it
did nothing at all here. `do_parse_mesg_2` (`msgparse.c:1957`) fills
MPI's `cmd` and `arg` variables from the globals `match_cmdname` and
`match_args` (`match.c:28`), which are **cleared by `init_match`**
(`:800`) and filled in by `match_exits` when an exit's alias matches.
So they are live from the exit match until the next match of any
kind, which in practice means the exit's own message properties and
nothing else: a description reads them empty, because `do_look_at`
matches first, and the oracle confirms that directly.

Nothing passed them, so every `{&cmd}` and `{&arg}` read empty.
`mesgArgs` is the pair, threaded through `canDoit`,
`execOrNotifyProp`, `execOrNotify` and `parseOProp` rather than kept
on the Server, so each of the twenty-odd call sites says out loud
whether the pair is live — a field would leak the typed verb into
the arrival description after a move, which upstream's clear is
exactly what prevents.

### `{name}` did not truncate an exit's alias list

The same script. `mfn_name` (`mfuns2.c:638`) cuts an **exit's** name
at the first `;`, so a multi-alias action reports only the name it is
known by; `mfn_fullname` (`:694`) is the same function with that one
line removed, and upstream's comment on it says so and asks for the
shared helper this now has. Its abort still says "NAME", which is
part of the copy/paste and is kept.

**Upstream's three sentinel branches are dead code.** Both functions
test for NOTHING, AMBIGUOUS and HOME and answer `#NOTHING#`,
`#AMBIGUOUS#` and `#HOME#` — but `mesg_dbref_raw` ends with
`if (!OkObj(obj)) obj = UNKNOWN;` and `OkObj` requires `d >= 0`
(`db.h:440`), so all three have already become UNKNOWN and "Match
failed." is the only answer any of them can give. `FULLNAME` used to
return `#NOTHING#`, which is the one answer upstream cannot produce.

### The puppet relay fired twice, and `unparse` forgot whose eyes

§4.3. `@force $pup = :jumps!` arrived twice here, once as the room's
own line and once prefixed `Squiggy> `. `notify_nolisten`
(`interface.c:4712`) relays to the owner only when the message is
**private** *or* the puppet is somewhere other than its owner: a
puppet standing beside its owner relays nothing public, because the
owner has already heard the line. The comment here asserted the
condition was always satisfied, and it is not — room speech is
public all the way down, `notify_except` passing `isprivate` 0.

Upstream also guards the `@pecho` evaluation with
`notify_nolisten_level`, taking the prefix as empty while a relay is
already running. Without it a `@pecho` that notifies anything
recurses, and on one goroutine that is the whole server;
`Server.relayDepth` is that guard.

And `z look` showed bare names where upstream showed dbrefs and
flags. `unparse_object`'s first line is `player = OWNER(player)`,
commented "Handle ZOMBIE case" (`db.c:2232`): the test is made on
whoever **owns** the viewer, so a puppet sees what its owner sees.

**Three of `unparse_object`'s clauses are still missing**, and are
left because porting them means a lock evaluation and so making
`unparse` a method at fifty-odd call sites: a **STICKY viewer** sees
only names whatever else is true; `can_see_flags` is
`can_teleport_to` rather than the wizardry-or-ownership test here;
and a non-player target also shows its flags to anyone who
`controls_link`s it, or when it is CHOWN_OK.

### `_/oecho` was written, displayed, and read nowhere

§4.4, and the one the plan expected to need a second seat. It does
not: the driver sits **inside** the car while the car speaks in the
room, so `drive :vroom vrooOOOOmms!` is an exterior line delivered to
an interior viewer, and one transcript sees it.

`notify_listeners`'s vehicle branch (`interface.c:4902`) prefixes
what happens outside a vehicle and delivers it to everything inside,
defaulting to `"Outside>"` — so the gap showed even in a world that
had never set the property. `@oecho` wrote `_/oecho`, `examine`
displayed it, and nothing read it, which is the shape `@ownlock`
still has.

Five conditions, each excluding a way of listening in from a parked
car: the vehicle must not be DARK unless a wizard owns it; the line
must be public; the speaker must be where the vehicle is; and a
vehicle inside another vehicle relays nothing unless a wizard owns
it.

### Three probes that tested nothing, and one that still does

The walkthrough's own scripts, before their transcripts were read.
`@link three=one` matched the fixture's **player**, who is called
"One", so the depth-two metalink chain was never built. `@link
test=nil` answered "That exit is already linked." `drop $vette`
answered "I don't understand '$vette'." — `do_drop`'s matcher has no
`match_registered`, which is upstream's and is kept as a probe with
the reason written down — and because the drop failed, nothing after
it boarded: the `@idescribe` landed on Room Zero and `leave` said
"You can't go that way."

And `drive :vroom` needed a `@link` the manual does not give. An
**unlinked** exit cannot partial-match — `match_exits` allows one
only when the exit runs a program or is NIL-linked, which is
`exitprog` (`match.c:551`) — so the line reached nothing and both
servers answered "Huh?"

Eleven mutations across the five findings, all eleven caught. Two
needed rewriting first: one did not compile, and one had an anchor
that occurred twice.

### The six `_sys` values on `#0`

`SYSTEM_PROPDIR_PROTECT2` is `_sys` (`include/game.h:70`), and upstream
keeps six values there that this server wrote **none** of. The one
that was visible is `do_uptime` (`look.c:994`), which reads
`_sys/startuptime` back rather than consulting a clock — so `uptime`
agreed with upstream while a program asking `#0` got nothing, which
`cmdUptime`'s own comment already said.

| property | C | when |
|---|---|---|
| `_sys/startuptime` | `game.c:487` | the world goroutine starts |
| `_sys/maxpennies` | `game.c:488` | ditto, from `max_pennies` |
| `_sys/dumpinterval` | `game.c:489` | ditto, from `dump_interval` |
| `_sys/max_connects` | `game.c:490`, `interface.c:4585` | zero, then the peak |
| `_sys/lastdumptime` | `events.c:99` | per dump |
| `_sys/shutdowntime` | `interface.c:4600` | at shutdown |

They are **integers** — `add_property` is called with a NULL string
and a value — so a program reads them with `getpropval`, and the
golden case checks `getpropstr` comes back empty to pin that.

**`Engine.Run` is the hook**, not a loader and not a caller. An Engine
is what a *server* has: `internal/store`'s loader, the importer, the
configurator and `fbemerald tune` all build a world without one, and
"startup time" means nothing to them. Writing it there also means a
new server entry point cannot forget it.

Three needed a decision rather than a port, and each is recorded in
the code:

- **`dumpinterval` names an inert parameter.** The `dump_*` family is
  a declared divergence, since persistence is write-behind rather
  than a dump cycle. The value is written anyway — a program asking
  is entitled to an answer — and it describes nothing.
- **`lastdumptime`'s analogue is a flush**, and it is stamped only
  when the flush has something to carry. Stamping it unconditionally
  would make the snapshot never empty, so an **idle world would write
  to Postgres on every tick for ever**, just to record that it had
  written. `World.HasPending` is that test, and it mirrors
  `Snapshot.Empty` rather than `DirtyCount`, because a snapshot can
  be non-empty on a `@tune` change alone.
- **`shutdowntime` has to be written before the final flush** takes
  its snapshot, or it never reaches the database. `@armageddon`
  deliberately does not come this way at all — it exits without
  dumping, so upstream leaves the property at whatever the last clean
  shutdown wrote.

**`max_connects` written as zero at boot is a no-op, and faithfully
so.** `add_prop_nofetch` (`property.c:285`) is

```c
if (strval && *strval) { ... } else if (value) { ... }
```

so a NULL string with a zero value takes neither branch and the
property is *removed* rather than created. `props.Value.IsEmpty`
treats a zero `Int` the same way, so this server arrives at the same
place by its own rule — an incidental confirmation that the two agree
about empty properties. The call is kept because it is upstream's
line; what it does is nothing until a connection raises the mark.
Upstream raises the mark from its descriptor sweep, where the count
can only have grown since the last pass; the one moment it can grow
here is a login finishing.

Three of the six are exactly comparable and are compared
(`internal/golden/sysprops_test.go`): `maxpennies`, `dumpinterval`
and `max_connects`. `startuptime` cannot be — two servers boot
seconds apart — so it is compared as a *plausibility*, non-zero and
within a day of the clock, which is enough to catch the bug that
mattered: the property not being there at all. The other two are
unit-tested, including that an idle flush does **not** restamp
`lastdumptime` and that `shutdowntime` reaches the persister rather
than only memory.

One thing the unit tests found about the harness rather than the
server: `World.New` starts with no `#0`, and `SetProp` on an object
that is not there does nothing at all — so the first version of every
assertion was reading a missing property and measuring the harness.
A real world always has `#0`, which is why `withRoomZero` is a test
helper and not a guard in `WriteBootProps`.

### The mucker table surveyed: nine false floors and one missing

`internal/muf/mlev_gen.go` is generated from the `mlev <` checks in
the C and is the **only** gate: `prim.go` refuses before the
implementation runs. `gen_mlev.py` was approximate by its own
admission, and SETNAME's false floor had been found by chasing a
message rather than by looking. All thirty-four entries have now been
checked against their guards: **25 genuine, 9 false, 1 missing.**

**The generator is fixed structurally rather than by name.** It used
to carry a `CONDITIONAL_FLOOR` set holding SETNAME and an
escape-hatch token list that had grown `control_process(` after KILL
came out wrong. Two rules replace both:

- **the guarded statement must be a bare `abort_interp(...)`** -- any
  other body means the `mlev <` test selects a *scope*, not a
  refusal; and
- **the `if` must sit at brace depth 0** of the function body -- a
  deeper one is reachable only under a condition the extractor never
  sees, which is exactly how SETNAME got through.

Run against all thirty-four plus SETNAME, the two together flag
precisely the nine findings and SETNAME, with **zero false positives
among the 25 genuine floors**. Both are load-bearing: SETNAME has a
bare abort body and is caught only by the depth rule, while SETOWN
and ADDPENNIES are at depth 0 and caught only by the body rule. A
third rule, **splitting a condition on its top-level `||`**, recovers
the missing floor. Comments and string literals are blanked before
any of this, because `abort_interp("Permission Denied (mlev < ...)")`
unbalances a paren scan and a comment mentioning `permissions()`
would exempt a floor that is real.

Two accounting checks confirm nothing is dropped silently: 67
functions carry an unconditional floor, 26 reach the table and 41 are
excluded for having their own abort wording -- and
`CUSTOM_ABORT_MESSAGE` holds exactly 41 names, **every one of them
used**. The generator now also **fails loudly** on an `mlev <` whose
right-hand side it does not recognise, which is how three tunable
floors had gone missing.

**Five of the nine were not guards at all.** `NOTIFY`,
`NOTIFY_NOLISTEN`, `NOTIFY_EXCLUDE`, `OTELL` and `ARRAY_NOTIFY` were
recorded at 2, taken from

```c
if (tp_force_mlev1_name_notify && mlev < 2 && player != target)
    prefix_message(buf, msg, NAME(player), BUFFER_LEN, 1);
else
    strcpyn(buf, sizeof(buf), msg);
```

where there is **no `abort_interp` anywhere on the path** -- the
branch picks a message prefix. So **a mucker-1 program could not
produce output at all**, which is the most basic thing a MUF program
does. `force_mlev1_name_notify` defaults to true and was read
nowhere, so the behaviour the floor displaced had never existed
either; `mlev1Prefix` is it, over `prefixMessage`, which was already
`prefix_message` with `SuppressIfPresent` set. Only NOTIFY and
NOTIFY_NOLISTEN carry the "not when notifying yourself" exemption.

**Four opened a block of extra mortal-only restrictions.** `SETOWN`,
`ADDPENNIES` and `MOVEPENNIES` were recorded at 4 from an
`if (mlev < 4)` whose body is further `if`s, and `MOVETO` at 3 the
same way. Their real gates are three @tune parameters -- 
`addpennies_muf_mlev` and `movepennies_muf_mlev` default to 2,
`pennies_muf_mlev` to 1 -- and **this server read none of them**, so
`PENNIES` had no gate at all while the other two had an invented one.
A `map[string]int` cannot express a runtime gate, so these are inline
checks, following `USERLOG`'s precedent down to upstream's oddly
literal and inconsistently capitalised wording.

All four had **no argument validation, no permission rules and no
range checks whatsoever** -- the invented floor was standing in for
every one of them. SETOWN gained its four distinct refusals, the
CHOWN_OK and @chlock tests, and the room-and-thing location rules;
the two pennies primitives gained their overflow, ceiling and
negative tests. **None of the four had a single test, golden or
unit, anywhere in the tree.**

**`RECYCLE` is the one wrongly-allowed finding**, and the direction
that matters. `p_db.c:2200` is
`(mlev < 3) || ((mlev < 4) && !permissions(ProgUID, result))`, two
independent disjuncts; the skip regex saw `permissions(` and
discarded the whole condition, so RECYCLE was in no table and had no
level check anywhere. **A mucker-1 program could recycle objects
upstream refuses it.** The floor is back and the second disjunct is
inline.

**Its other refusals are now ported, and one of them was
catastrophic.** This entry previously said they were "still missing
and are a separate gap", which under-rated what was missing: with
none of them, a **mucker-4 program could `#0 recycle`** and turn the
global environment into garbage, or recycle a player. The primitive
went straight to `World.Recycle`, which guards only nil-and-garbage
and which even removes a player from the name index on the way.
`@recycle`, the command, refuses both — so the command was safe and
the primitive was not.

Four of the five are reproduced in upstream's order (`p_db.c:2206`):
`#0`, a player, anything a dbref `@tune` parameter names, and the
running program. The `@tune` scan is shared with `do_recycle` as
`tuneNamesObject` but **the wording is not** — the command says "That
object cannot currently be @recycled." and the primitive "Cannot
currently recycle that object.", two spellings of one guard.

Every one of the six dbref parameters defaults to `#0` or `#1`, which
the first two refusals reach first — the same shadowing `do_recycle`
has — so reaching that branch at all needs a parameter pointed
somewhere else first.

**"Cannot recycle active program." is not reproducible.** Upstream
walks `fr->caller`, whose entries are the program dbrefs execution
has passed through (`interp.c:690-692`); Emerald's `f.calls` holds
`{pc, scopeBase}` — return addresses within one program, with no
program refs on it. The running-program check covers what it would
for any run not nested through INTERP, because upstream's own stack
holds the running program at index 1; what is lost is a program
recycling one further out in an INTERP chain. Same cause as the
sticky+haven+nested `ProgUID` case.

`unset_source` on an exit is incidentally covered: `World.Recycle`
calls `removeFromChain`, which is what it does.

It runs at **mucker 4** in `internal/golden/recycleprim_test.go`,
because below that the `(mlev < 4) && !permissions(...)` clause
refuses anything the program does not own and masks the whole set.
Four mutations, all caught.

**`MOVETO` was ungated by porting it** -- `HELD_FLOOR` held it for
one commit and is gone. See below.

**No golden case could have caught any of this, and the reason is
worth keeping.** A fixture compiles at mucker 3, so **nothing in the
oracle suite had ever run a program below it**. Raising a program's
level is documented; lowering it is `@set <prog>=1`, where upstream
clears both mucker bits before applying, so it is an assignment
rather than an or. `internal/golden/mlevfloor_test.go` runs fifteen
probes at mucker 1 and again at 3, through the catchable-abort route.
Six mutations, all caught -- and two of the first verdicts were
false: one mutation failed `vet` with unreachable code, and one hit a
**non-unique anchor** and silently mutated a different primitive.
That is the same family as "a mutation that fails to compile tests
nothing": the anchor has to be checked for uniqueness, not just for
presence.

**The compile-time mucker level is fixed**, which the instruction-limit
work above had already recorded and left. `Frame.MLevel()` now reads
`Host.ProgMLevel` live, so `@set` takes effect on a program's next
run and a nested frame uses its own program's level. Invalidating the
compile cache instead would not have done: the *owner's* level caps
the program's, so changing one player's bits would mean finding every
program they own. The dangerous direction is the one tested --
`internal/game/mlevlive_test.go` demotes a mucker-4 program and
checks it stops acting like one, where before it went on at its old
level until something happened to recompile it.

**Seven abort messages among the 25 genuine floors are still
wrong**, and the generic-message design is what is failing: 7 of 25
is not an exception list. `ENTRANCES_ARRAY` and `PART_PMATCH` want
"Permission denied.  Requires Mucker Level 3." -- wording already in
`CUSTOM_ABORT_MESSAGE` for `NEXTENTRANCE`; `NOTIFY_SECURE` wants
"Mucker level 3 primitive.", already there for
`ARRAY_NOTIFY_SECURE`; and `NEWEXIT`, `QUEUE`, `FORK` and
`PROGRAM_SETLINES` each want their own. The generator should extract
the message rather than the level alone.

**Two upstream oddities found in passing and reproduced as written.**
`MOVEPENNIES` tests both object arguments with
`Typeof(x) != TYPE_PLAYER || Typeof(x) == TYPE_THING`, whose second
disjunct cannot hold when the first does not -- so it takes *players
only* despite every one of its messages saying "player or thing",
which in turn makes its own `mlev < 4 && Typeof == THING` check
unreachable and its `Typeof(ref) == TYPE_PLAYER` guard always true.
And `ARRAY_NOTIFY` iterates **lines outer, targets inner**, where
this server iterates targets outer: each target sees its own lines in
order either way, so only the interleaving across targets differs and
no transcript with one player can see it. Left alone.

### @tune: two things still collapsed

`do_tune` is ported, and two details of its permission model are not,
both invisible to the oracle because its player is `#1`:

- **`TUNE_MLEV` — fixed, and it was hiding a leak.** The recorded
  complaint was that God gets 255 rather than 4 and that a plain
  wizard could therefore read what upstream reserves to `#1`. Both
  halves were true, and the count was wrong in the recording: it is
  not "the fourteen `file_*` parameters" but **36 gated on writing**
  and **10 gated on reading** — `max_force_level`, `strict_god_priv`
  and the whole smtp family, `smtp_password` included. There are
  also **six** call sites, not the three this entry assumed:
  `do_tune` (`tune.c:674`), `SYSPARM` (`p_misc.c:1217`),
  `SETSYSPARM` (`:1322`), `SYSPARM_ARRAY` (`:1413`), MPI's
  `{sysparm}` (`mfuns.c:4141`) and the MCP simpleedit handler
  (`mcppkgs.c:388`).

  **The leak was `{sysparm}`.** `mufHost.TuneGet` is
  `tune_get_parmstring` *minus its own mlev gate* — its doc comment
  said so — and `{sysparm}` called it with no level at all. Since
  `mfn_sysparm` reads at the **triggering** player, any mortal could
  put `{sysparm:smtp_password}` in their own description, look at
  themselves, and read any of the 55 parameters gated at wizard
  level or above. `TuneGetParm` is the gated form now;
  `TuneGet` stays, ungated, for the parameters upstream keeps in C
  globals — `tp_gender_prop` (`fbstrings.c:307`) and the
  server-policy values MPI's `tuneBool` and `tuneInt` ask for.
  Gating those would have invented a refusal upstream does not make.

  `GodOnly` turned out **not** to be unread after all: the
  configurator renders a "god" tag from it (`internal/web/tune.go`).
  It is a method on `Param` now, derived from `WriteMLev`, so it
  cannot drift from the levels it describes.

  **One more divergence came out of reading `SYSPARM_ARRAY`.**
  `tune_parms_array` filters with `equalstr`, which is `smatch`
  (`tune.c:267`), where `TuneList` used `strings.EqualFold` — the
  wrong comparison, and the function `CLAUDE.md` forbids, since
  upstream folds only A–Z. So `"file_*"` matched **nothing** here and
  twenty-six entries upstream. `tuneDisplay` had it right all along,
  which is why `@tune file_*` worked and `SYSPARM_ARRAY` did not.

  Most of this is oracle-visible after all, contrary to the note
  above: `SYSPARM_ARRAY` reports each parameter's levels verbatim and
  the numbers do not depend on who asks, so
  `internal/golden/tunemlev_test.go` compares them directly — 255
  against 4, and 26 against 0. The two halves that genuinely need
  the asker to be a mortal are unit-tested in
  `internal/game/tunemlev_test.go`, against `strict_god_priv`
  rather than `smtp_password`, because the latter's default is empty
  and an empty answer cannot tell a refusal from a blank.
- **`SETSYSPARM` — fixed, and it was using the wrong setter.** The
  recorded complaint was that it could not tell bad syntax from a
  bad value: `tune_setparm` has six result codes and
  `muf.Host.TuneSet` returned `(bool, error)`, so the primitive
  always said "Bad parameter value. (2)".

  The larger half was that it never called `tune_setparm` at all. It
  did the lookup and the permission test by hand and then wrote
  through **`World.SetTune`** — the *loader's* setter, which is the
  lax one. So a program could put values in the database that
  `@tune` itself refuses: `"true"` for a boolean (upstream reads only
  the first character, so `"yellow"` is true and `"true"` is a
  syntax error), `"12abc"` for an integer, a bare `3600` for a
  timespan. It goes through `tune.Set.SetParm` now, which is
  `tune_setparm`, and reports its code.

  One detail was got wrong first and the oracle corrected it: the
  dbref resolver was given no searcher, on the reasoning that a MUF
  caller has none. `tune_setparm` takes `player`, so it has one —
  the program's **caller** — and `"here"` resolves for `SETSYSPARM`
  exactly as it does for `@tune`.

  Two of the six codes are still uncovered by the golden case:
  `TUNESET_BADVAL` and `TUNESET_DENIED`. Both are reachable, but not
  from a mucker-4 program driving a wizard.

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
