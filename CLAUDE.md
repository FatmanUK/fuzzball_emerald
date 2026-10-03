# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A from-scratch Go reimplementation of [Fuzzball MUCK](https://www.fuzzball.org/)
7, aiming for behaviour compatibility — existing MUF programs, MPI descriptions
and `.db` worlds should work unchanged — with four deliberate departures: Go
instead of C, TLS-only networking, Postgres instead of flat-file dumps, and a
rootless Podman container instead of autotools.

**There is exactly one plan file**, in `~/.claude/plans/`, and it is the live
one. The three that got the project here — M0–M8, the dispatcher and the
command gap, and movement through the propqueues — are all executed and have
been **deleted**, because three completed plans sitting beside a live one are
three things to mistake for current work. What they established is in this
file and in `docs/upstream-coverage.md`; `BOOTSTRAP.md` §2 is the narrative of
what has landed, and the plan file is what is next.

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
make check            # vet + formatting + test
make fmt              # gofmt, then rewrap comments to 70 columns
make pod-import pod-run   # build image, import starter world, serve in a container
make connect          # openssl s_client to the running server
make pod-logs         # follow the container
make pod-stop         # stop it

make golden-build     # build Fuzzball 7 from the C, once
make golden           # diff this server against it

make build-config     # build ./fbeconfig, the web configurator
make config           # run it against the local database

./fbemerald help-seed          # write the built-in help into the database
./fbemerald help-seed -force   # ...replacing topics edited in-game

make width-check                      # no new line over 70 columns
make width-check RANGE=HEAD~1..HEAD   # ...in a commit range
```

## Go source is 70 columns, counting a tab as 8

`make fmt` enforces it, and `make check` verifies it. **gofmt does not
wrap anything**, so the limit needs a second pass: `tools/reflow` does
what a line-wrapper cannot.

- It reflows comment **paragraphs**, not lines. Wrapping each line on
  its own leaves orphans like `// the ones that`.
- It moves an over-long one-line function body onto its own line, which
  gives what you would have written by hand.
- It splits a composite literal a field per line, and gofmt aligns the
  colons.
- It breaks a long condition after `&&` or `||`, packing operands
  greedily rather than one per line.

It deliberately leaves alone anything carrying its own layout: indented
examples inside comments, lists, tables, `//go:` directives, and raw
string literals — several of those hold MUF source for the golden
fixtures, where re-wrapping would change the program under test.

**Do not add golines.** It was tried and rejected: at this width it
inflates the tree by 16.7% and produces 1,506 "lonely argument" lines —
a single short token on a line of its own — where this codebase has two.
It is also inconsistent, exploding a 73-column line while leaving a
96-column one beside it.

About **2,900 lines remain over 70**, nearly all string literals. Error
messages are upstream's exact wording that programs match on, and
breaking them across a `+` makes them much harder to grep for; that cost
is not worth paying. The limit is therefore enforced on *new* code —
`make width-check` — rather than by a mass rewrite. Write new code to
fit; do not reformat an old line just because it is long.

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
the macro table, the help corpora and any new gripes. All are held apart from
the object graph — source because saving a program rewrites its text without
touching any field on the object, the rest because they belong to no object at
all. Gripes are the one thing a snapshot **appends** rather than replaces,
because a gripe log is append-only and there is no deletion for a whole-table
rewrite to make visible. `World.SetSource` is the
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
- `internal/help` — the built-in help texts, seeded into the database
- `internal/web` — the optional configurator, a separate binary
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

**Every MPI evaluation declares three variables**, `how`, `cmd` and `arg`
(`do_parse_mesg_2`, `msgparse.c:1919`). All three must exist or referring to
one is an MPI *error* rather than an empty string. `{&how}` is the caller
context and `{muf}` reads it back to build the COMMAND variable it hands a
program; `{&cmd}` and `{&arg}` read empty inside a message property, which was
measured against the oracle rather than derived and is pinned by the golden
case.

**`mpi.Env.Type` is upstream's `mesgtyp`**: what triggered an evaluation, as
against `Blessed`, which is what it is allowed to do. Blessing is kept out of
it deliberately — it is a permission, and `{revoke}` drops it while leaving
the provenance alone. Only four places upstream read anything but the blessed
bit, and the one with teeth is `{tell}`/`{otell}`: from a **listener** they
refuse unless the object carrying the message is a room, so a thing lying in a
room cannot message whoever spoke near it. `PARSEPROP` and `PARSEPROPEX`
already carried a `private` argument that had nowhere to go until this
existed.

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

## The help system

`internal/game/help.go` is `src/help.c`. Upstream reads its texts out of files
under the game directory; Emerald has no game directory, so a corpus is rows in
Postgres (`internal/store`.`HelpTopic`) and a topic is a row. That is also what
makes them editable from inside the game, with `@help`, and what the
configurator reads.

There are nine corpora, named in `internal/world/help.go`. Four are upstream's
**index files** — `help`, `news`, `man`, `mpi` — where a topic is matched
against its `|`-separated aliases **exactly**. One is upstream's **directory**,
`info`, which **prefix-matches** and whose bare form lists its topics in
twenty-column fields rather than showing a header. The rest — `motd`,
`credits`, `welcome`, `connect` — hold a single text, stored as one topic with
no name so there is only ever one mechanism to load, write and edit.

**A topic with no name is the header**: the block before the first `~`, which
is what a bare `help` shows. It is indexed under the empty string like any
other name. Skipping that indexing is what made `SetHelpText` append a second
header rather than replace the first.

Two things upstream does that look wrong and are not:

- **`help <topic>=<segment>` ignores the segment.** `index_file` never sees
  one; only `show_subfile` does, and that is the per-file form. So `info` cuts
  a text to a line range and `help` does not.
- **A blank line prints as two spaces.** Both `index_file` and
  `spit_file_segment` do it, and the golden case compares it.

**The command is `mpi`, not `mpihelp`** — only the `@tune` parameters are named
`file_mpihelp*`. `mpihelp` is registered as an alias anyway.

**`@credits` is in `exactOnlyCommands`.** Without that, `@cr` and `@cre` become
ambiguous, and `lookupAtCommand` answers an ambiguous prefix with nothing — so
adding the command naively stops `@create` from being abbreviated.

All fourteen `file_*` parameters that name a help text are inert: they read and
write so `SYSPARM` still works, and nothing acts on them. The `file_log_*`
family is a separate question and is left alone.

### Seeding

`internal/help` holds the built-in texts, embedded with `//go:embed`, written
fresh for Emerald rather than taken from upstream's `.raw` files. The original
reason was that Emerald declared no licence; it is GPL-3.0 now, so vendoring
them would be *permitted* — and is still the wrong thing, because upstream's
help documents about fifty commands this server does not have. Write fresh
content, not an import that has to be pruned.

`help.Apply` fills an empty corpus outright, and otherwise refreshes only
topics nobody has edited, which it tells apart by `Modified` being zero.
`World.ReplaceHelp` is the seeding path and keeps that field; `SetHelpTopic` is
the editing path and stamps it. The seeded release is recorded in `meta` under
`help_seed_version`. `fbemerald help-seed [-force]` re-runs it by hand.

### The welcome banner

`internal/game/welcome.go` is `welcome_user`. Three sources in upstream's
order: the `welcome` **property list on #0** (`welcome#` for the count, then
`welcome#/1`...), then the `welcome` corpus — where upstream reads
`file_welcome_screen` — then the compiled-in default. MPI runs over whichever
won when `do_mpi_parsing` and `do_welcome_parsing` are both set, as
`welcome_mpi_who`/`welcome_mpi_what`, with the descriptor number in `{&cmd}`
and the hostname in `{&arg}`.

**Pre-login `help` is a different text from the banner** — the `connect`
corpus, upstream's `file_connection_help`. An earlier version re-sent the
banner, which is the one thing somebody typing `help` at a login screen has
already read. **The motd is shown on a successful connect**, not only on
demand.

## Movement

`internal/game/enter.go` is `move.c`'s `enter_room` and `moveto`, plus the two
loop checks they depend on.

**`moveObject` redirects where `World.MoveTo` refuses.** The store returns an
error when a move would put something inside itself; upstream walks a per-type
ladder instead — a player to their home, a thing to its home then its owner's
home then `player_start`, a room to `#0`, a program to its owner. Nothing in
upstream's movement path can fail, which is why none of its callers check, and
`enter_room` relies on that.

**`parentLoopCheck` is two walks, not one.** `location_loop_check` follows
locations; `parent_loop_check` runs that *and* a `getparent` walk, which for a
VEHICLE thing follows its home rather than its location. `getparent` itself is
a tortoise-and-hare that collapses a detected cycle to `#0`, reproduced rather
than simplified because which answer it gives decides whether a move is
refused.

**A move is announced under five conditions, not none.** `quiet_moves` unset,
neither the room nor the mover DARK, the mover not a plain THING — a ZOMBIE or
a VEHICLE does announce itself, which is what keeps a puppet-heavy world
readable — and the exit not DARK. Emerald announced every move
unconditionally, and none of `quiet_moves`, `autolook_cmd`, `penny_rate` or
`secure_thing_movement` was read anywhere.

**The order inside `enter_room` is load-bearing.** The destination is resolved
and loop-checked *before* the self-loop test, so a redirected move that lands
where the player already is prints nothing. `do_move` prints `@drop` and
`@odrop` *before* calling `enter_room`, so "you have arrived somewhere" is read
before "has arrived" and before the look. And the autolook happens before the
penny check, which upstream comments on: the arrive propqueue is deliberately
last so a message from it is not lost in the spam.

**The autolook runs a command, not a function.** `autolook_cmd` is looked up as
an *exit* first, so a world can replace what a player sees on arriving — and
can therefore write a loop, which is what `donelook` and "Look aborted because
of look action loop." are for.

**`@link` is three operations wearing one name**, and says so differently for
each: an exit is "Linked to X." (or "Linked to HOME." — unparsing `HOME` gives
`*HOME*`, which is the lock spelling, not this one), a thing or player is
"Home set.", a room is "Dropto set.".

**An unlinked exit is not special-cased.** The destination count is
`could_doit`'s own first check, so an unlinked exit gets the same default every
other failure gets — "You can't go that way." An earlier version answered
"That exit doesn't go anywhere.", which was invented here.

## Exit traversal

`internal/game/move.go`'s `trigger` is `move.c:482`. An exit has a **list** of
destinations and every one of them fires; what each does depends on its type,
and an earlier version treated everything that was not a room, a program or NIL
as a plain move.

- **A room** is walked into, subject to three guards that all say "You can't go
  that way.": a non-wizard THING may not enter a ZOMBIE room, a VEHICLE may not
  enter a VEHICLE, and a guest may not pass a GUEST room or exit.
- **A thing** is *boarded* when the exit is inside it and it is a VEHICLE —
  `dest == LOCATION(exit)`, which is what `@action` makes reachable: it
  attaches an exit to a named object, so `@action board=<vehicle>` followed by
  `@link board=<vehicle>` is a boarding exit, where `@open` could only ever put
  one on the room. Otherwise the thing is **fetched**: to the exit's
  own location, or to its location's location when the exit hangs on a thing,
  so an exit on a bag brings something to the room rather than into the bag.
  A non-STICKY exit that fetched something then sends the exit's home object
  home.
- **A player** is jumped to, if they are JUMP_OK — "That player does not wish
  to be disturbed." otherwise. Linking to one at all needs
  `teleport_to_player`, which nothing read before.
- **An exit** is a metalink: `trigger` calls itself with `pflag` off, so the
  inner exit's rooms and players are skipped and the player cannot be moved
  twice.
- **A program** is run REGUID, and refused for a guest if either the program or
  the exit is GUEST.

**"Done." is what an exit says when nothing it pointed at counted as a
success**, which includes an exit with no destinations reached through a
metalink.

**The metalink depth bound is a deliberate divergence.** Upstream recurses
through one with nothing to stop it, so an exit linked to itself — or a ring
of two — exhausts the C stack and takes the server down, and `@link` does not
test for it. Emerald refuses past `maxMetalinkDepth` with "Exit aborted because
of metalink loop.", which is the one answer that is not a crash. The bound
counts metalinks *only*: a trigger reached through the autolook is a different
recursion with its own counter, and counting both would make eight ordinary
moves report a metalink loop.

**`HOME` as a destination is resolved per player**, and a home that is a THING
is refused with "That would be an undefined operation." rather than entered.

**Every refusal in `parse_linkable_dest` names its object.** "I don't
understand 'X'." for a failed match — `noisy_match_result`, like every other
command — "You can't link to players.  Destination X ignored." (two spaces),
and "You can't link to X."

## get and drop

`internal/game/getdrop.go` is `do_get` and `do_drop`, and the five spellings
of the two: **`take` is another get; `put`, `throw` and `hand` are other
drops.** Upstream dispatches them to the same two functions, and its own
comment says the three differ only in their help files.

**The second argument is most of what they do.** With two arguments, `get`
takes the *first* as the container — `get bag=key` — and `drop` takes it as
the destination. Emerald's own versions had no second argument at all, which
is why the three aliases could not be aliases.

**A container's `@conlock` defaults to false**, so an unopened container is
shut. That ordering is load-bearing and not what the messages suggest: the
conlock is tested *before* the "You can't steal stuff from players." check, so
taking from a player who has set no conlock is refused as an unopenable
container and never reaches the steal message.

**`get` from a container and from the floor differ in more than one message.**
From a container it runs `could_doit` and reports "You can't get that."; from
the floor it runs `can_doit`, which means the `@succ` and `@osucc` show.

**`drop` says which of three things happened.** Into a room, "Dropped." (or the
thing's own `@drop`), plus the room's `@drop`, plus an `@odrop` from each — and
the *room's* `@odrop` is prefixed with the **thing's** name, not the player's.
Into a thing, "Put away." Into a player, a pair of lines naming both sides.
Only the room case has messages at all.

**A room's drop-to is immediate unless the room is STICKY.** Dropping into such
a room puts the thing straight through the drop-to; only a STICKY room holds
things until everybody leaves, which is `maybe_dropto`'s job.

**`leave` and `disembark` are one command**, and each of its refusals is its
own sentence. Boarding a vehicle needs an exit **inside** it — `trigger()`
requires `dest == LOCATION(exit)` — which only `@action` can make, since
`@open` always attaches to the room. Nothing exercises the boarding path yet;
see `docs/upstream-coverage.md`.

**`@link` and `@unlink` are each several operations wearing one name**, and
say so differently for each type. `@link`: "Linked to X." for an exit (or
"Linked to HOME." — unparsing `HOME` gives `*HOME*`, the lock spelling),
"Home set." for a thing or player, "Dropto set." for a room. `@unlink`:
"Unlinked." plus a `link_cost` refund and, if the exit had mucker bits,
"Action priority Level reset to 0."; "Dropto removed."; "Thing's home reset to
owner."; "Player's home reset to default player start room."

## @teleport

`internal/game/teleport.go` is `wiz.c:102`'s `do_teleport`, which is four
commands wearing one name. A **player** is walked in through `enter_room`, so
the move announces itself and the autolook runs; a **thing** or a **program**
is simply put there; a **room** is *reparented*, and says "Parent of X set to
Y." where everything else says "X teleported to Y." This server used to answer
"Teleported." for all four.

**Its permission test is not `match_controlled`, and cannot be.** The rule
depends on the *destination*, so it has to run after both matches, and what it
asks differs per victim type — a player needs control of the victim, the
destination, the victim's location and, when the destination is a thing, the
thing's location; a thing or a program needs the destination controlled **or**
`can_teleport_to`, and the victim **or** its location controlled; a room needs
the victim controlled and the destination linkable, and `#0` is refused
outright. Each refusal names the test it failed, at length, and programs match
on the whole line.

**The destination match is narrower than the victim match**, and neither is
`match_everything`. The victim search has no exits and takes `match_player`
unconditionally; the destination search drops `match_neighbor` as well, and
adds both back only for a wizard. So a mortal cannot name something lying in
the room as a destination, which reads like an oversight and is reproduced —
and a room made by `@dig` can be named by nothing but its dbref or a
registration, because `@dig` leaves it detached.

**A non-STICKY room with a drop-to swallows a thing teleported into it**, and
the confirmation names the drop-to rather than the room that was asked for.

**`HOME` is resolved per victim type**, by `teleportHome` — not by
`fallbackHome`, which is `moveto`'s ladder. They differ for a player, whom
upstream sends to their *owner's* home when their own would make a loop; a
player owns themselves, so it is the same place, and the rung is reproduced
because the C has it.

`can_teleport_to` (`predicates.c:89`) is `canTeleportTo`, and `can_see_flags`
is a call to it — upstream keeps the two separate with a comment saying the
rules could diverge, and that separation is kept rather than collapsed.

## @tune

`internal/game/admin.go`'s `cmdTune` is `tune.c:669`'s `do_tune`, and it is
three commands rather than the two Emerald had.

**A bare `@tune` is the listing.** `@tune <pattern>` narrows it with
`equalstr`, which is `smatch` — so wildcards work and nothing else does, and
`@tune penny` shows one parameter where `@tune penn` shows none. Emerald kept
the listing behind a `#list` subcommand upstream has never had and answered a
bare `@tune` with a usage message, so a program reading a listing back could
not have found it. `@tune info [pattern]` is the third form, adding each
parameter's group and label; its argument is space-separated because it shares
`arg1` with the pattern.

**A set is recognised by the *line* containing an `=`**, not by the value
being non-empty — upstream tests `match_args`, the whole typed line. So
`@tune muckname=` is a set that fails as a bad value, never a request to read
the parameter back. A name prefixed with `%` is the other set: reset to the
compiled-in default.

**`tune_setparm` is stricter than `Param.Parse`, and visibly so.** It is
`tune.Set.SetParm`, kept apart from `SetString` because the loader reads back
values this server wrote itself:

- A **boolean** reads only its *first character*. `y`, `Y` or `1` is true, `n`,
  `N` or `0` is false, anything else is a syntax error — so `yes` works,
  `yellow` also works, and `true` does not.
- An **integer** is `number()`: optional sign then digits, nothing else.
- A **timespan** is `tune_timespan_seconds`, which is **not** `ParseTimespan`.
  It wants `<days>d <h>:<mm>:<ss>` or a run of unit suffixes like `1d12h`, and
  **refuses a bare count of seconds** — a total of zero is an error, so `3600`
  and `4:00:00` both fail.
- A **dbref** is *matched*, through `match_absolute`, `match_registered`,
  `match_player`, `match_me`, `match_here` and nothing else — so a room cannot
  be named by standing in it unless `here` is typed. A failed match is bad
  syntax and a wrong object type is a bad value.

The order of the checks is observable: the write permission first, then the
reset, then the empty-value test — so resetting a non-nullable string with no
value succeeds where setting it would not.

**Each reply is upstream's**, and there are six: "Parameter set." or
"Parameter reset to default." followed by the parameter rendered as a listing
would render it, or one of "Unknown parameter.", "Bad parameter syntax.", "Bad
parameter value." and "Permission denied." A listing ends "*done*" always, and
says "No matching parameters." first when nothing matched — which is how a
program knows it has the lot.

**`[inactive]` is upstream's marker for a parameter whose feature was compiled
out**, `MOD_ENABLED` against the `compile_options` string. `tune.Param.Active`
is that test, and `tune.modules` is Emerald's own list: MCP and nothing else,
since DISKBASE is replaced by Postgres, MEMPROF is malloc profiling and
RESOLVER is upstream's separate resolver process. Emerald's **inert**
parameters — the `dump_*` family and the `file_*` names — are deliberately
*not* marked, because upstream does not mark its equivalents; `fbemerald tune`
and the configurator are where that is said.

**A `@tune` cannot be forced**, which is the only permission check `do_tune`
makes for itself and is not about who is asking.

Two things are not reproduced, both invisible to the oracle because its player
is `#1`. `TUNE_MLEV` gives God 255 rather than 4, and `GOD_PRIV` — which
upstream defines by default — puts the fourteen `file_*` parameters beyond a
plain wizard; the generator collapsed `MLEV_GOD` to `MLEV_WIZARD` and recorded
`GodOnly` beside it, and nothing reads that field yet. And `SETSYSPARM` reports
"Bad parameter value." where upstream distinguishes it from "Bad parameter
syntax."

## Actions, clones and blessing

`internal/game/action.go` is `@action`, `@attach`, `@clone`, `@relink`, `@bless`
and `@unbless`.

**`@action` was an alias for `@open`, which is worse than missing.** Upstream's
`do_action` attaches the exit to a **named object** rather than to the room, so
a world using it got exits in the wrong place and no complaint. It also does
not link, which is the other half of what separates it from `@open`.

**`@relink` checks the new target before breaking the old link.** That is the
whole point of the command — `@unlink` then `@link` leaves an exit pointing
nowhere when the second half fails — and it is why its checks duplicate
`@link`'s rather than calling into it. None of its refusals is worded the same
as `@link`'s for the same condition, and `_do_unlink` runs *quietly* in the
middle, so the only line between the checks and the link is "Attempting to
relink...".

**`@clone`'s cost is the original's value**, floored at `object_cost`, via
`OBJECT_GETCOST` — so cloning something valuable costs what it is worth. A
wizard's clone copies hidden properties and nobody else's does.

**Only `@bless` has the usage guard.** `@unbless` goes straight through with an
empty pattern, matches nothing and reports zero. Its count line says
"unblessed" where `@bless`'s says "blessed". And upstream also blesses
**directories**, which Emerald does not — recorded in
`docs/upstream-coverage.md`, because making it agree needs a flag that can live
on a valueless node.

**The `=<regname>` argument is the last argument of six commands** — `@open`,
`@dig`, `@create`, `@program`, `@action`, `@clone` — and it registers on the
**player**, not on `#0`, through `register_object`'s own messages.

**`@dig`'s default parent is the nearest ABODE room above the digger's**, and
`default_room_parent` only when there is none. Going straight to the parameter
puts a room dug inside somebody's realm at the top of the world instead.

## Registration and arbitrary properties

`internal/game/propset.go` is `@register` (`set.c:1077`) and `@propset`
(`set.c:929`) — the two commands that write a property the *player* names
rather than one a verb implies, which is why both carry a restriction check the
message setters do not need.

**Nothing wrote `_reg/` before.** `$include` and `Matcher.Registered` read a
propdir only the MUF editor's own `q` ever filled in.

**`@register`'s argument shape is genuinely awkward**, and the prefix test is
on the whole argument rather than its first word: upstream writes
`string_prefix(arg1, "#me")`, which asks whether the argument *starts with*
`#me`. Testing it the other way round makes an empty argument match every
prefix, which made `@register =wid` behave as `@register #me =wid`. With no `=`
at all it lists rather than sets.

**A registration is stored as a ref**, and read back as any of four forms,
because real databases contain all of them.

**`@propset`'s type may be abbreviated to any prefix**, and an empty type is a
string — so `@propset me=:foo:bar` is the short form. Only the type and path
are trimmed; a value may begin or end with a space.

**`parse_boolexp` has its own message for a name it cannot resolve** — "I
don't see X here." — and upstream shows it *before* the caller's "I don't
understand that lock." `boolexp.ParseError.Notify` is that message; anything
parsing a lock from player input has to report it.

**A lock property is displayed with names.** It is *stored* unparsed with
fullname off — "#1" — and `displayprop` renders it with `unparse_boolexp`'s
fullname argument set, so reading one back means re-parsing it.

## The four checkflags searches

`@find`, `@owned`, `@contents` and `@entrances` (`internal/game/find.go`) are
one mechanism with four sources: the same flag expression, the same
`checkflags` filter, the same `display_objinfo` rendering, the same two
closing lines.

**The expression language lives in `internal/muf`**, as `FlagCheck`,
`ParseFlagCheck` and `Matches`. It was written there first because
`ARRAY_FILTER_FLAGS` and `FINDNEXT` needed it, and nothing about it is MUF's —
so it is exported rather than ported twice.

**The display mode comes after a *second* `=`.** The first separates the name
from the flags, and `init_checkflags` splits what is left again: `@find
wid=owners` reads "owners" as six flag letters and finds nothing, where `@find
wid==owners` asks for the owners column. And `locations` is tested before
`links`, so `=l` is locations and `=li` is links.

**`=size` cannot be reproduced** and renders as the plain mode. It reports
`size_object`'s byte count; the golden case masks the column, for the reason
`examine`'s "Memory used" line is masked.

`@find` charges `lookup_cost`, which nothing in this server read before, and
has **no result cap** — an invented limit of 200 used to make a large world
answer with part of the truth and report a count that looked right.

## The gripe log

`gripe` files a complaint, and upstream appends it to the file named by
`file_log_gripes`. Emerald has no game directory, so complaints are **rows in
Postgres** — the same answer the help texts got, loaded at boot and written
behind like everything else. Three things about that are load-bearing:

- **A snapshot carries new gripes, not the whole list.** It is the only part of
  a snapshot that is added rather than replaced, because a gripe log is
  append-only: nothing edits or deletes a complaint, so there is nothing for a
  whole-table rewrite to make visible.
- **`gripe` shows only the most recent `world.GripeLimit`**, which keeps the
  reading in memory and stops a world that has been up for years flooding
  somebody's client. The whole log is still in the table, and the
  configurator's `/gripes` page is where it is read.
- **The rendering is upstream's log line**, `vlog2file`'s timestamp and
  `log_gripe`'s format — with the dbrefs carrying **no** `#`, which is how
  every log line upstream spells a ref. The golden case compares it.

`@restrict` is `wizonly_mode`, and it is a field on `Server` rather than a
`@tune` parameter because upstream's is a runtime global: persisting it would
make a maintenance window survive a restart. Its "on" and "off" are compared
**case-sensitively**, so `@restrict ON` reports rather than sets; that reads
like an oversight and is reproduced. Upstream sets the same flag from two
places Emerald has no equivalent of — a `-wizonly` command-line flag, and a
sanity violation found at boot.

## Listeners and @sweep

`internal/world/listen.go` maintains the LISTENER flag and
`internal/game/sweep.go` is `look.c:1961`'s `do_sweep`. They are tranche
three's first half: the flag has to exist before the propqueues can read it,
and `@sweep` only *reports*, so it ships on the flag alone.

**The flag is set in one place and cleared in none.** `set_property`
(`property.c:101`) turns LISTENER on when a property path starts with
`_listen`, `~listen` or `~olisten`, and nothing in a non-DISKBASE build ever
turns it off — so deleting the last `_listen` leaves a thing flagged for the
life of the process. That is reproduced, not improved, because every reader
tests the flag **and** the property: `World.IsListener` is that pair, and it is
what makes the staleness invisible.

**The flag is derived, not stored.** It is in `ref.DumpMask`, so a load strips
it, and `World.RecomputeListeners` puts it back once every property is in —
from the store, from the importer, and from `@sanfix`. That is upstream's
DISKBASE `skipproperties` (`diskprop.c:239`), the one place it clears the flag.

**The test that sets it and the test that reads it disagree**, and both are
upstream's. Setting is `string_prefix`, so `_listenup` makes a listener;
reading is an exact `get_property`, so `@sweep` will never report that object.
Only a **root-level** property counts either way: `foo/_listen` is just a
property.

**`@sweep` is weaker than it sounds**, and upstream's own comment says so: a
DARK *player* in a lit room is reported like anybody else, only a DARK *room*
hides a sleeping one, and nothing a level deeper than the room's contents is
looked at. A sleeping zombie that does not also listen is assembled into a line
and then thrown away, which is why a puppet whose owner is offline does not
appear. The "Listening rooms down the environment:" header prints even when the
walk finds nothing, because upstream's flag is tested at the top of a loop that
always runs once.

**The four trapped commands are not checked the same way.** `page`, `whisper`
and `say` are prefix tests — an exit named `p` traps `page` — while `pose` is
tried exactly as `pose`, then `pos`, then `po`, stopping at the first that
matches. An **unlinked** exit traps nothing, because `exit_matches_name`
requires a destination.

## The propqueues

`internal/game/propqueue.go` is `timequeue.c`'s `propqueue` (`:1912`),
`envpropqueue` (`:2068`) and `listenqueue` (`:2240`): the mechanism by which a
**property is a hook**. A world writes `_arrive`, `_depart`, `_connect`,
`_disconnect`, `_lookq`, `_listen`, `~listen`, `~olisten` or one of the "o"
halves, and the server runs whatever it names.

This is the only work in the port that changes how an existing world
*behaves* rather than what it says. The starter world already carries two
`_arrive` hooks and six `_connect` hooks that had never fired.

**A property names a program four ways, and one of them is not a program.** A
leading `&` means the rest is MPI; `#123` or a bare number is a dbref; `$name`
is a registration looked up on the **object carrying the property**, not on the
player. Anything else names nothing and the queue does nothing — *silently*,
which is why a typo in an `_arrive` is so hard to notice. A `Ref`-typed
property is taken directly, which is `get_property_dbref` being tried first.

**The recursion counter is shared across every queue type.** One
`propq_level`, capped at eight, so a `_depart` that triggers an `_arrive` that
triggers a `_depart` runs out of depth rather than looping. It is checked
*after* the program is resolved, so "Propqueue stopped to prevent infinite
loop." appears for a property that would otherwise have run.

**`mt` is private, not public.** Upstream's parameter is documented as "this is
a public message" and the code does the opposite: set, MPI runs `ISPRIVATE` and
the output goes to the triggering player; clear, MPI runs `ISPUBLIC` and the
output is pronoun-substituted, prefixed `>> ` and broadcast to every *player*
in the room but the trigger. Every non-"o" queue passes it set.

**The call sites do not have one shape.** `_depart` and `_odepart` are **four**
calls — the mover's own properties, then the old room's environment chain, for
each of the pair. `_arrive`, `_oarrive`, `_connect` and the rest are
environment walks from the **mover**, so `getparent` takes them through the
room and upwards in one pass. `_lookq` walks from whatever was looked at, and
its argument is that object's dbref written `#123` where every other queue
passes a word. And the `_connect` pair fires on **every** connection where the
"has connected" line and the `connect` action fire only on the first, which
upstream's own comment calls odd.

**`listenqueue` is propqueue's near-twin with four differences that all
matter.** It is gated on the LISTENER flag (or the owner being ZOMBIE). Its
value may be **conditional** — `Message=&MPI` runs the right-hand side only
when the left `equalstr`-matches the text, and the `=` may be backslash-escaped
— which no other propqueue has. It **queues** rather than runs, so a listener
answers after the line that woke it: a MUF listener on the next tick and an MPI
listener a second later, which is `add_muf_queue_event`'s zero delay against
`add_mpi_event`'s one. And **MPI is opt-in**: `_listen` is called with it off,
so a mortal cannot make a listener evaluate MPI at all.

`Server.deferred` is the slice those queued events live in, drained by `Tick`
— the part of upstream's timequeue that neither the process table nor
`mpiEvents` already covers.

**`notify_listeners` fires from `notifyRoomFrom`**, which is `notify_except`:
the queues run on the room, then up the environment chain, then on every object
in it, *before* a word is delivered to anybody. The speaker is passed
explicitly because a listener can see it — the room a listening program is
told about is the **speaker's** location, not the room being notified, and the
ignore filter is applied between the two. `{otell}` is the one caller that
passes no speaker, because `mpiHost` is built without one at a dozen sites.

**Upstream's environment walk here is inconsistent with itself and is
reproduced**: the first step upwards is `LOCATION(room)` and every step after
it is `getparent`, so a VEHICLE room's chain is followed differently on the
first hop than on the rest.

A program launched from a propqueue runs **now**, in BACKGROUND mode and
HARDUID, with COMMAND fixed at "Queued event." — note the lower-case "event",
where the timequeue's own version says "Queued Event." — and the queue's name
as the pushed argument. A listener's says "(_Listen)" instead.

## A recycled dbref comes back

`World.Create` hands out a garbage dbref before allocating a fresh one, which
is `new_object` (`db.c:147`). Emerald always allocated past its ceiling, so
every dbref in every message after a `@recycle` was one higher than upstream's
and a long-lived world's numbering drifted from the C's for good.

Three details. The list is **LIFO** — the last thing recycled is the first
reused — and it is **rebuilt from the graph** on load, by an ascending walk
that leaves the *highest* garbage ref at the head (`db.c:1222`), so a world's
first reuse after a restart is not the same ref as its first reuse before one.
And a **player never takes a recycled ref**: upstream passes `isplayer` and
skips the list, so a name that was once somebody else's cannot come back
attached to their old number.

Garbage keeps one property, the description `<recyclable>` (`move.c:1314`),
which is what `@examine` shows when something still points at it.

**`@recycle`'s `@tune` guard runs before its per-type refusals**, and that
ordering is most of what anybody sees: `#0` is `default_room_parent`'s value
and `#1` is `toad_default_recipient`'s, so neither `@recycle here` nor
`@recycle me` ever reaches "Room #0 contains everything" or "You can't recycle
a player!" Its *permission* rules are still `resolveControlled`'s and still
diverge — see that function's doc comment.

## ANSI

`internal/ansi` is `queue_ansi`'s two filters (`interface.c:673`) and
`Descriptor.Send` is where they run. Emerald ran neither: a world that sent
colour sent it to every client including those that had asked for none, and a
malformed sequence went out as written.

**There are two filters and they are different functions, not one with a
flag.** `Strip` is `strip_ansi` and removes colour outright. `Sanitize` is
`strip_bad_ansi` and keeps it, passing only SGR sequences, completing one left
unterminated, and appending a reset so a line cannot leak colour into the next.
They disagree about four of the twelve shapes `internal/ansi`'s tests cover,
and those expectations were produced by **compiling the two C functions and
running them**, not by reading them — several are not what the code looks like
it does. The reset is appended *unconditionally*, so text already ending in one
gets two; a sequence truncated at the end of the input gets its `m` and then
**loses** the reset, because upstream writes the input's own NUL into the middle
of its buffer; and `ESC[31X` keeps the `X` in both filters.

**Neither is `internal/muf`'s `ansiPattern`.** That regexp is `ANSI_STRIP`'s
golden-tested contract and is narrower than either: it insists on a `[` and a
terminating letter, so it leaves `ESC[31` and `ESCX` alone where `Strip` removes
both, and it swallows the `X` of `ESC[31X` where `Strip` keeps it. Sharing one
implementation would make one of them wrong.

**The gate is a callback the game installs, read live.** `Descriptor.AllowANSI`
is nil by default, which means strip — the safe answer for a connection the
game has not adopted. `Server.allowANSI` is what fills it in, and it reads the
world on every line rather than caching, so `@set me=C` takes effect on the
next one. The world pointer is *captured* rather than looked up, because an
Engine owns exactly one for its lifetime and the alternative would be an
accessor handing the world to whoever asked.

**Before login the answer is not a flag but two `@tune` parameters**, and both
have to be on: `do_mpi_parsing` and `do_welcome_parsing`. A world that has
turned MPI off gets no colour on its banner either. That reads like an accident
— the banner is the only pre-login text a world writes, so the parameters
deciding whether MPI runs over it also decide whether its colour survives — and
it is upstream's.

**Filtering happens before the MCP quoting, not after**, which is `queue_ansi`'s
own order: a sequence stripped out of a line cannot then be what makes the line
look like an out-of-band message. `sendRaw` is unfiltered, and so is upstream's
`queue_immediate_raw` — MCP messages are not text.

**Almost every line of a MUF error report is coloured**, and this is the half
of the work that was hiding. The header is bold red on black
(`interp.c:1427`), the program-and-message line is bold (`:1436`), and the
backtrace's three lines are bold yellow on black with the program, line,
procedure name and each argument *name* picked out in bold (`debugger.c:406`,
`:320`, `:483`, `:495`). Only the source line under each level carries none.
Emerald emitted all of it plain, which matched every golden transcript because
the harness's player has no COLOR — the oracle was stripping colour Emerald was
not producing, two wrongs that cancelled until one was fixed.

**Three primitives strip ANSI from a name before matching** — `MATCH`,
`PMATCH` and `RMATCH` (`p_db.c:765`, `:812`, `:886`) — and they are the only
primitives that do. A program handed a coloured name can still resolve it,
because the escapes are not part of what anything is called. It is `strip_ansi`
there, not `ANSI_STRIP`'s narrower pattern.

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

**COMMAND and the pushed argument are two different strings**, upstream's
`match_cmdname` and `match_args` — the verb and the rest of the line
(`game.c:695`, `interp.c:688` and `:738`). `SetReserved` takes one string and
uses it for both, so every launch site that needs them to differ overwrites
the pushed value afterwards. Each site chooses COMMAND differently and none
of them agrees with the others: a command or exit gets the typed verb (as
*typed*, not the exit's own spelling — `match_exits` copies out of
`md->match_name`), a message property gets the caller context `(@Desc)`,
`{muf}` gets `<&how>(MPI)`, INTERP **inherits the caller's** because
`prim_interp` never touches `match_cmdname`, and QUEUE gets "Queued Event.".

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

**`ProgUID` is `find_uid`, and all four branches are ported.** Which identity a
program acts with depends on `muf.Frame.Perms` — upstream's `fr->perms`, chosen
by whoever *starts* the program and never by the program itself — together with
the program's own STICKY and HAVEN flags. `HardUID` is the common case, not the
exception: upstream passes it for a lock, for a property that runs a program,
for MPI's `{muf}` and for everything the timequeue fires. Only a command or an
exit runs `RegUID`. A launch site that sets nothing is claiming `RegUID`, so
say it out loud.

The one case not reproduced is sticky+haven+wizard-owned+nested, which recurses
into the *calling program's* identity. `Frame` has no caller-program stack —
`f.calls` is return addresses within one program — and upstream returns the
program's owner whenever that stack is shallow, which is what this does always.

**`FORK` drops `Perms` and keeps `Trig`.** `prim_fork` callocs the child and
copies fields one at a time; `trig` is among them and `perms` is not, so a
HARDUID program's child runs REGUID. It reads like an oversight in the C, it is
observable, and it is reproduced.

**The six compiler conditionals are answered through callbacks**, not by giving
the compiler a world. `Options.ObjVersion` reads `_version` or `_lib-version`
off a named object for `$ifver`/`$iflibver`, and `Options.CanCall` answers
`$ifcancall` — the same shape `Options.Include` already used for `$include` and
`$iflib`. With either nil the directive is treated as false and a note says so,
which is what the compiler's own tests rely on.

Three details are load-bearing. A version comparison is "is the **wanted**
version at most the one the object has", both sides parsed as floats with
anything unparseable reading as 0.0; an object with no version property reads
as "0.0" rather than failing. Failing to *resolve* the object is a compile
**error**, unlike `$iflib`. And `$ifcancall` is **not** `CANCALL?`'s test even
though they read alike: the primitive (`p_misc.c:1274`) weighs the target
program's own mucker level and the running frame's effective one, while the
directive (`compile.c:3837`) weighs both *owners'* levels, because at compile
time there is no frame to have a level. Sharing one function between them would
make one of the two wrong.

**`compileProgram` has a recursion guard, and needs one.** `$ifcancall`
compiles the program it is asking about, so two libraries that each check the
other would compile each other for ever — on the world goroutine, which means
the whole server. A cycle fails the inner compile instead, which makes the
condition false.

**Some compiler directives write properties, and the compiler cannot.**
`$author`, `$note`, `$version`, `$lib-version`, `$doccmd`, `$pubdef` and
`$libdef` all set a property on the program object, but `internal/muf/compiler`
is deliberately given no world. They are collected on `Result.Props` and applied
by `Server.compileSource`, so a caller that uses `Compile` rather than
`CompileResult` silently drops them — which is how `$libdef` came to compile
without exporting anything. `$pubdef` and `$libdef` write under `_defs/`, the
same propdir `$include` reads back, so dropping them breaks libraries rather
than just their documentation.

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
on them. Three are still wrong and are recorded in
`docs/upstream-coverage.md`: `+` reports itself as `++` with "Invalid
datatype.", `SETNAME` checks its argument's type before the permission rather
than after, and a fourth was "stack underflow" against upstream's "Stack
underflow." — that one is fixed.

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

**A command's argument is two strings, not one.** `ctx.arg` is upstream's
`arg1`, trimmed at both ends; `ctx.rest` is `full_command`, the line after the
verb with exactly *one* character skipped. The commands that print back what
was typed read `rest` — `say`, `pose`, `@wall`, `gripe` — so a leading space
survives. Neither `do_say` nor `do_pose` has an emptiness guard, so a bare
`say` really does produce `You say, ""`; and `do_pose` omits its space before
any of **four** separator characters (`'`, space, comma, hyphen), not just an
apostrophe. The golden case compares all of it.

**An unrecognised command says what `huh_mesg` says**, not a fixed string. A
world changes it with `@tune`, and the default is upstream's "Huh?  (Type
"help" for help.)".

**`look` does not list exits.** Upstream's `look_room` gives the name, the
description and the contents and stops; a world that wants an "obvious exits"
line supplies it from its own programs, as the starter world does. An earlier
version of this printed one, which made every look diverge.

**A message property beginning `@` names a MUF program.** `_/de`, `_/sc`,
`_/fl` and `_/dr` are run rather than printed when their value starts with
`@` — `@123 args` or `@$registered args` — which is `exec_or_notify`
(`property.c:2454`), ported in `internal/game/exec.go`. The rest of the line
is MPI-evaluated and becomes the program's *stack argument*, while COMMAND
gets the caller context, `(@Desc)` or `(@Succ)`; they are upstream's
`match_args` and `match_cmdname`, two different strings that `SetReserved`
makes one, so a launch site has to overwrite the pushed value. When the `@`
names nothing runnable the rest of the line prints **unparsed**, and the
nothing-special message stands in when there is no rest. The `o` messages
cannot do any of this: they go through `parse_oprop`, which is public MPI,
pronoun substitution and `prefix_message` — which suppresses a prefix the
line already carries, and omits the space before a pose separator.

**`look` is a different shape per type, and only a room shows a name line.**
`do_look_at` (`look.c:265`) switches: a room goes through `look_room` — name,
description, `can_doit`'s success messages, then `Contents:` — and everything
else through `look_simple`, which prints the description alone. The headings
differ too (`Carrying:` for a player, `Contains:` for a thing, suppressed
entirely for a HAVEN thing), as do the three permission refusals, each naming
the test it failed. A room with **no** description prints nothing, where
anything else gets the nothing-special message. `look.c`'s own `can_see` is
also not `controls`: a program shows only to whoever controls it or if it is
a VEHICLE, exits and rooms are never listed, and a STICKY player sees nothing
extra in the dark. Look traps — the `_details` propdir — are not ported, but
the LOOK propqueue is: `_lookq` runs after everything else a look does.

**`inventory` ends with `score`.** `do_inventory` finishes by calling
`do_score`, so the money line is part of the command — including when there
is nothing to list, which is why "You aren't carrying anything." is not the
end of it. Emerald returned early there and printed no money line at all.

**Command resolution is upstream's dispatcher, ported.**
`internal/game/dispatch_table.go` is every command Fuzzball 7 dispatches, in
the order its nested switch visits them; `dispatch.go` resolves by taking the
first entry that accepts the typed word. The table is derived from
`game.c`'s trie, so `min` is how deep the switch commits before a name is
reachable — which is why `@co` reaches nothing (`game.c:829` demands
`command[3] == 'n'`, then a fifth character).

Emerald used to take any unique prefix over its own map, which is a *different
algorithm* and diverged both ways. Four tie-breaks are not prefix matching at
all: `strcmp` (so `@SHUTDOWN` is not `@shutdown`), `strcasecmp`,
`strlen(command) < 7` (which splits `@chown` from `@chown_lock`), and one node
with `string_prefix`'s arguments reversed (so `@unb` is the shortest
`@unbless`). One command, `move`, has no `Matched()` at all and accepts
trailing text — `movex` runs it.

**Names Emerald does not implement stay in the table**, with no handler, and
say so when typed. Dropping one silently widens every abbreviation it
constrained: that is how `@chown` came to mean `@chown_lock`. `register`
panics on a name the table lacks, so a typo is a startup failure rather than a
dead command.

**The permission macros live on the table**, not in the handlers, because
upstream applies them at the dispatch site — including for commands this
server has not implemented. `internal/game/build.go`'s `requireWizard` and
friends are now shadowed for anything dispatched.

**`Matcher.Exits` is `match_all_exits`, and three of its five stages were
missing.** The room, then **actions on things the searcher is carrying**, then
**actions on things in the room**, then the searcher's own, and only then the
environment chain. Without the two object-action stages an action attached to a
thing — exactly what `@action` makes — could not be reached at all. A searcher
inside a THING is in a vehicle, so the walk continues from that vehicle's
*home*; the walk is bounded at 88 levels; and a YIELD room blocks everything
behind it but an OVERT one.

**`choose_thing` decides an *exact*-match tie and nothing else**, which is the
thing to know before reading `internal/match/choose.go`. Upstream calls it from
two places — `match_contents` (`match.c:471`), for two objects in one
container with the same name, and `match_exits` (`:636`), for two exits at the
same priority with the same longest alias — and both are resolving
`exact_match`. A **partial** match never arrives: it overwrites `last_match`
and bumps the count, so two half-matching names are reported as ambiguous
rather than chosen between. Emerald used to take the later of two exact
matches, which is a fifth answer upstream never gives.

Its four tie-breaks run in order: either being NOTHING; a **preferred type**,
which is whatever the command's own `init_match` asked for; **`check_keys`**,
which prefers an object the searcher can actually use and is set at exactly
three call sites — `do_move`'s direction and *both* of `do_get`'s matches, not
`drop`'s; and **environment distance**. Then it **tosses a coin**
(`match.c:175`), which is reproduced rather than settled: a stable answer here
would invent a behaviour programs could come to rely on. **No golden case can
pin a name that matches two objects**, which is why the clone suite clones each
thing once and why all five tie-breaks are unit-tested instead.

**The preferred type is narrower than it sounds.** An exit above the current
match level overwrites `exact_match` outright (`match.c:632`) without
consulting `choose_thing`, so a thing and an exit of one name are decided by
the exit's priority whatever type was asked for. What the preference really
decides is two objects *in one container* — a thing and a program both called
"wand" in your hands.

**`env_distance` (`db.c:2327`) is not what its name suggests**, and the numbers
in its test came from compiling the C and running it rather than from reading
it. It measures to the target's **parent**, so a thing in the room you are
standing in is at distance zero. And when the searcher is not under that parent
at all, the walk runs off the top of the world and counts hops to `#0` — so
from a sibling branch the answer grows with how deep the *searcher* is, and
from `#0` itself it is 1 because the first hop reaches nothing. Two unrelated
objects therefore compare by where the searcher stands.

**`Matcher.Everything` is `match_everything`, and two searches were missing
from it.** `Registered` means a `$name` resolves for every command that takes
an object; without it `look $wid` and `@describe $wid=...` found nothing a
program had registered. `Player` is added when the searcher or its owner is a
wizard, so a wizard can name somebody elsewhere. But **`look` is deliberately
narrower**: `do_look_at` builds its own list without `match_registered`, so
`look $thing` really does fail, and a failed look says `match_msg_nomatch`'s
"I don't understand 'X'." rather than "I don't see that here."

**`Matcher.Absolute` has no permission test**, and an invented one used to make
`look #5` fail for anybody who did not own `#5`. `absolute_name`
(`match.c:333`) checks that the reference parses and that the object exists,
and nothing else: **anybody may name any object by number**, and what stops
them acting on it is the command's own check — `matchControlled`'s refusal,
`examine`'s limited view for a non-owner, `@teleport`'s per-type tests. The
guard in the matcher duplicated some of those and contradicted the rest, and
hid which rule had actually refused.

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

**Locks are evaluated.** `internal/boolexp` is a direct port of `boolexp.c`
— parse, evaluate, unparse — and `lockPasses` (`internal/game/examine.go`)
really runs it. A stored lock holds its *unparsed string*, not a cached tree,
so it is re-parsed on each use through the disk-loader path (`dbload=true`,
trusting `#123` refs, no name matching); that is valid because `Unparse` with
`fullname=false` is the only thing that ever writes one. Name matching happens
once, when a lock is set from player input.

## The liveness lease

`internal/store/lease.go` answers one question: is a server running against
this world? Nothing in the database said so before, and two servers sharing one
world would each hold an authoritative in-memory graph and write over each
other every flush interval. `cmdServe` takes the lease straight after `Migrate`
and **refuses to start** if it is held.

It is a Postgres **session-level advisory lock**, chosen because it is the only
marker that is still correct after a crash: a process that dies drops it with
its connection, so there is nothing to time out and nothing to clean up.

Two things about it are easy to get wrong.

**The connection has to be pinned outside GORM's pool** — `db.DB()` then
`sqlDB.Conn(ctx)`. A lock taken through the pool is released the moment that
connection goes back, silently, leaving a running server looking offline.
`SetMaxOpenConns` is 9 rather than 8 because the lease keeps one for good.

**`Release` must unlock explicitly.** Closing an `*sql.Conn` hands the session
back to the pool *alive*, so the lock would outlive the lease and travel to
whoever drew that connection next. Only the process actually dying drops it by
itself — which is what `TestLeaseClearsOnACrash` simulates, with
`pg_terminate_backend` rather than a `Close`.

The lock is scoped to the **schema**, not the database, because a schema is
what holds a world and the store tests each make their own. `LeaseHeld` reads
`pg_locks` rather than taking the lock and letting go, so a probe cannot make a
server starting at the same moment fail for no reason.

## The configurator

`cmd/fbeconfig` and `internal/web` are an optional second binary: a small
administrative interface over the same Postgres database and the same `FBE_*`
variables. Stdlib only — `net/http` and `html/template`, with the pages
embedded.

**It reads the database directly rather than loading a world.** It has to: the
server may be running and holding the authoritative graph in its own memory, so
a world loaded here would be a copy that went stale the moment it was taken.
`internal/store/admin.go` holds those queries.

**Whether the server is running decides what it may do**, and that is enforced
in exactly one place — `readOnlyGuard`, on every request that is not a GET.
Leaving the inputs out of the templates is the cosmetic half; a form posted from
a page loaded before the server started, or a request made by hand, has to be
refused at the middleware or not at all. A lease that cannot be *read* is
treated as held, because not knowing is not a reason to write. `/login` and
`/logout` are the exceptions: refusing a sign-in while a server runs would make
the interface useless for the case it exists to report on.

**Authentication is the world's own wizards** — the same name and password they
connect to the MUCK with, read out of `objects` and checked with
`internal/password`. There is no separate account store, because anyone who can
be trusted here is already a wizard. Two details matter:

- **A legacy password is not rehashed on login here**, which the MUCK does.
  Writing is exactly what this process may be forbidden to do, and a login that
  sometimes writes is worse than one that never does.
- **A wrong name, a wrong password and a mortal all give the same message.**
  Distinguishing them would turn the login page into a way to enumerate
  wizards. The log records which it was; the page does not.

Names are matched with `lower()` in SQL to narrow and `ascii.EqualFold` in Go to
confirm, so the locale Postgres runs under cannot fold two distinct player names
together.

### The pages

`/tune` shows every parameter that exists, grouped as `tune.Groups` has them,
not only the ones somebody has set — names are a runtime API that MUF reads by
string. A value is put through the parameter's **own** `Parse` and written back
through its `Format`, so what lands in the database is canonical and anything
that would not load is refused here rather than at the next boot.

`/help` edits a corpus as **one text in upstream's index format** rather than a
topic at a time: that is the format the seed files are written in and the one a
wizard already knows, and it makes reordering or deleting a topic an edit
rather than a sequence of operations. Everything saved there is stamped as
edited, so a later release's seeding leaves it alone.

`/players` does two things — set a password, and toggle WIZARD, BUILDER and
QUELL. There is no old-password field, because this is the interface somebody
reaches for when the old one is lost; what guards it is that only a wizard is
here and the server has to be stopped. Nothing else about a player is writable:
the rest of the flag word means different things by type, and **the flag word
carries the type**, which is why `TestPlayersTogglesFlags` checks the type bits
survive a whole-word write.

`/gripes` is the whole complaint log, paged, and is **read-only** for the same
reason `/objects` is: nothing here should be able to rewrite what somebody
reported. The game itself shows only the most recent `world.GripeLimit`, so
this is where the history is.

`/objects` is **read-only even when the world is free**. Changing an object
means threading containment chains, checking that an owner exists, and applying
flag rules that depend on the type — rules that live in the game. What the
inspector is for is looking at a world the server will not boot on, which is
why it flags a reference to an object that is not there rather than rendering a
blank.

### Packaging

`deploy/Containerfile` has two targets sharing one build stage: `server` (the
default) and `config`. They are separate images rather than two commands on
one, so a deployment that does not want an administrative web interface does
not have one sitting in the image it runs. `compose.yaml` puts `fbeconfig`
under an `admin` profile and publishes it to **loopback on the host** — inside
the container it necessarily binds every interface, since that is the only way
a port can be published at all.

`ValidateWeb` is separate from `Validate` rather than an extension of it:
`Validate` hard-requires the MUCK listener's TLS material and at least one MUCK
listener, and the configurator has neither. Its TLS material falls back to the
MUCK listener's pair and is required either way. `FBE_WEB_ADDR` defaults to
loopback and binding it wider is a logged warning, not a refusal — unlike
`FBE_PPROF_ADDR`, which is refused outright.

## Sanity checking

`@sanity` reports inconsistency, `@sanfix` repairs it, `@sanchange` edits one
reference by hand, and **`@examine` prints one object's raw fields** —
`do_examine_sanity`, which is not `examine`. That one renders an object for a
player; this prints the chain fields a repair would act on, plus everything in
the database pointing at it, which is what makes it useful on a world that will
not boot. Its names are unparsed with **no viewer**, so every dbref shows
whatever the flags say, and it has **no "here" default**: an empty argument
goes to the matcher and fails.

`@debug` is here too. Its only upstream option is "display propcache" and only
under DISKBASE, which Emerald does not have — so every argument reaches
"Unrecognized option.", which is exactly what upstream compiled without
DISKBASE does. It is implemented rather than declined because that *is* its
behaviour.

All four of the @san family are God-only, refused from inside a `@force`, and
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
- **A self-linked exit is refused rather than fatal.** Upstream recurses
  through a metalink with nothing to stop it and exhausts its stack; see
  "Exit traversal".
- **A regexp using a backreference or lookaround fails to compile.** Go's
  `regexp` is RE2, which has neither, and `compileRegex`
  (`internal/muf/prim_string2.go:319`) lets the compile fail rather than
  quietly matching something else — the safer of the two, since a pattern
  that silently means something different is worse than one that refuses.
  The MUF `REG*` primitives are the whole surface. A world using `\1` inside
  a pattern is the one thing this breaks, and vendoring a PCRE engine is the
  fix if one ever does.

## Upstream coverage

`docs/upstream-coverage.md` audits this against Fuzzball 7's three manuals and
answers the crash-only and 12-factor questions. MPI and MUF are complete, and
the command surface very nearly is: **109 dispatched names, 100 with handlers,
9 without**, five of those nine declined rather than missing. The count is
exact rather than estimated, because `internal/game/dispatch_table.go` *is* the
command surface — it carries every name upstream dispatches, and a name with no
handler says so when typed. To re-derive it, iterate `commandTable` against
`handlers` and `declined`.

**A missing command is not always a silent gap.** `@chown` was absent while
being a *prefix* of `@chown_lock`, so `lookupAtCommand` resolved it there and a
wizard transferring ownership silently set a lock instead. When adding a
command, check what its name is currently a prefix of.

## Status

M0–M8 are done, and so are the four tranches that followed them: the
dispatcher and the command gap, movement and containment and registration, the
propqueues, and ANSI gating. `BOOTSTRAP.md` §2 is the record.

The server imports the starter world, accepts real MUCK clients over TLS and
WebSocket, runs MUF and evaluates MPI, and supports look, movement, speech,
building and admin commands. A property is a hook: `_arrive`, `_depart`,
`_connect`, `_disconnect`, `_lookq` and the three listen propqueues all fire. Programs can suspend themselves on `READ`, `SLEEP`
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
