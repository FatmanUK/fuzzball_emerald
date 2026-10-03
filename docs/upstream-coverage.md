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

One thing `do_look_at` does is still missing, and it is recorded
rather than hidden: look traps, the `_details` propdir consulted when
the match finds nothing. The other two have landed — `@teleport`'s
wording, and the LOOK propqueue, which is tranche three.

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

### What `@link` still diverges on

Two things, both found while porting its permission rules and
neither of them a permission refusal, so neither was folded into that
commit:

- **`resolveLinkTarget`'s matcher is not `parse_linkable_dest`'s.**
  Upstream is `init_match(NOTYPE)` plus `match_everything`,
  `match_home` and `match_nil` (`db.c:1975`). Emerald hand-rolls a
  list that omits `Exits()` and `Registered()`, asks for
  `PreferType(TypeRoom)` where upstream passes `NOTYPE`, and carries
  another unconditional `Player()` of the kind recorded above. It is
  also missing `parse_linkable_dest`'s own `can_link(player, exit)`
  refusal, "You can't link that."
- **`@link` cannot build a multi-destination exit.** `_link_exit`
  (`db.c:2040`) splits the destination string on `;` up to
  `MAX_LINKS`, validates each, and returns how many were linked.
  Emerald resolves one destination and writes a one-element list.
  `trigger()` already *traverses* a list — that landed with exit
  traversal — so the gap is only in creating one, and the symptom is
  that `@link exit=roomA;roomB` silently links the first alone. A
  world with metalink fan-out cannot be built from inside the game.

One invented message was removed rather than recorded: `@link` with
an empty destination said "Link it to what?", which appears nowhere
in upstream. It shadowed two real answers — an exit is charged for,
transferred, and told "No destinations linked.", because
`_link_exit`'s loop never runs and so never matches anything, while
every other type reaches the matcher and gets
`noisy_match_result`'s.

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

**6. A rebinding shadows upstream and overwrites here — not
fixed.** Found by a probe written for item 4 and kept because it
measured something real.

Upstream's `{with}` pushes a new variable with `new_mvar` and pops it
with `free_top_mvar`, so binding a name that is already bound
**shadows** it and the outer value comes back afterwards. Emerald's
`Env.SetVar` searches for an existing name and *overwrites* it
instead of appending, and `PopVar` then removes whatever is last —
so the outer binding is destroyed rather than restored.

Measured against the oracle: `{with:n,1,{with:n,2,{&n}}{&n}}` is
`21` upstream, and here the inner `{with}`'s pop leaves `n` undefined
so the outer `{&n}` is an "Unrecognized variable." error.
`{with:n,1,{with:n,2,{inc:n}}{&n}}` is `31` upstream and empty here.

The fix is to separate the two operations upstream keeps apart:
`{with}` and the looping functions **bind** (push), while `{set}`,
`{inc}` and `{dec}` **assign** to an existing binding in place.
`Env.SetVar` currently does both and so can do neither correctly. It
affects every binding construct — `{with}`, `{for}`, `{foreach}`,
`{parse}`, `{filter}`, `{fold}` and `{lsort}` — and needs its own
commit. `internal/golden/varscope_test.go` holds the measurement.

Also worth noting: `Env.Var` searches newest-first (as `get_mvar`
does) while `SetVar` searches oldest-first, so the two disagree the
moment a duplicate name exists. That is the same defect seen from
the other side.

**5. `{midstr}` takes two positions, not a position and a length —
fixed.** `mfn_midstr` (`mfuns2.c:2897`) clamps both arguments as
1-based positions, lets a negative one index from the end
(`pos += len + 1`), and **walks backwards when the second is lower
than the first**, returning the span reversed. So
`{midstr:hello,2,4}` is `ell`, `{midstr:hello,4,2}` is `lle`, and
`{midstr:hello,-2,-1}` is `lo`; this server answered `ello`, `lo`
and the empty string. Reading the second number as a length made
every three-argument call wrong, and neither the negative form nor
the reversal existed at all.

The clamping order is upstream's and is load-bearing: a position of
zero returns the empty string **before** any clamping, and only then
is a position above the length pulled down, a negative one wrapped,
and anything still below one raised to 1.

One thing needed a guard the C does not have. With an empty subject
both positions clamp to 1, and upstream then copies the string's own
NUL terminator — which reads back as the empty string. Indexing
`s[0]` in Go would panic, so `midstr` returns early; the oracle
confirms the answer.

Item 6 needs its own commit. The golden case
(`internal/golden/atoi_test.go`) deliberately omits them and says so,
so that it tests the fix it belongs to rather than passing over a
different bug.

### MPI's {force} is half implemented

Found by the propqueue golden case, which tried to use it to drive the
recursion limit. `mfn_force` (`mfuns2.c:2760`) has an **unblessed
path** Emerald does not: with `allow_zombies` set, an unblessed
`{force}` is allowed to proceed and is then subject to seven refusals
of its own — a dark puppet, an owner flagged ZOMBIE, a no-puppets
room, a thing named after a player, the XFORCIBLE flag, the force
lock, and God. Emerald refuses every unblessed `{force}` outright with
"Permission Denied.", where upstream's wording for the XFORCIBLE case
alone is "Permission denied: forced object not @set Xforcible."

It also found that **MPI reports only the innermost failing
function**. Upstream walks back out, so `{null:{force:...}}` prints
the `{FORCE}` error and then `{NULL} (arg 1)`; Emerald prints the
first line and stops. That is the error *reporting*, not the
evaluation, and it affects every nested MPI failure.

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

### Boarding a vehicle is reachable and uncovered

`trigger()` boards a thing when the exit is *inside* it and it is a
VEHICLE — `dest == LOCATION(exit)` — so making one needs `@action`,
which attaches an exit to a named object. `@open` always attaches to
the room, so while `@action` was its alias no boarding exit could
exist at all and the code path was unreachable.

`@action` is ported, so it is reachable now and simply has no test:
`@action board=<vehicle>` then `@link board=<vehicle>`. Worth a golden
case, which would also exercise `leave`'s three remaining refusals
from the inside rather than as unit tests.

### Three MUF error messages are still not upstream's

Found by probing four deliberate program failures through both
servers while writing the ANSI case. Programs match on these strings,
so each is a real divergence rather than a cosmetic one:

- **`+` reports itself as `+` and says "Invalid argument type."**
  Upstream names the instruction `++` and says "Invalid datatype."
  The doubled name is upstream's own and is not a typo in this
  document.
- **`SETNAME` checks permission before the argument type.** Given a
  non-string name by a mortal, upstream answers "Non-string argument
  (2)" and Emerald answers "Permission denied.  Requires Wizbit." The
  order is what differs, not either message.
- A fourth has been fixed: the stack underflow aborts said "stack
  underflow" where upstream says "Stack underflow."

`STRCAT`'s "Non-string argument." agrees exactly, which is why the
ANSI case uses it to produce an error report.

### @tune: two things still collapsed

`do_tune` is ported, and two details of its permission model are not,
both invisible to the oracle because its player is `#1`:

- **`TUNE_MLEV` gives God 255**, not 4, and `GOD_PRIV` — which
  upstream `#define`s by default — raises the fourteen `file_*`
  parameters to that level. The generator collapsed `MLEV_GOD` to
  `MLEV_WIZARD` and recorded `GodOnly` beside each one; nothing reads
  that field yet, so a plain wizard here can read and write
  parameters upstream reserves for God. Closing it means teaching the
  MUF side `TUNE_MLEV` too, since `SETSYSPARM` and `SYSPARM_ARRAY`
  share the rule.
- **`SETSYSPARM` cannot tell bad syntax from a bad value.** Upstream
  aborts with "Bad parameter syntax. (2)" or "Bad parameter value.
  (2)" from `tune_setparm`'s two codes; `muf.Host.TuneSet` returns a
  plain error, so the primitive always says the second.

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
