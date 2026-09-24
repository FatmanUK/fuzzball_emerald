# Fuzzball Emerald

[![Test](https://github.com/FatmanUK/fuzzball_emerald/actions/workflows/test.yml/badge.svg)](https://github.com/FatmanUK/fuzzball_emerald/actions/workflows/test.yml)
[![Build](https://github.com/FatmanUK/fuzzball_emerald/actions/workflows/build.yml/badge.svg)](https://github.com/FatmanUK/fuzzball_emerald/actions/workflows/build.yml)
[![Push](https://github.com/FatmanUK/fuzzball_emerald/actions/workflows/push.yml/badge.svg)](https://github.com/FatmanUK/fuzzball_emerald/actions/workflows/push.yml)

A MUCK server in Go, behaviour-compatible with [Fuzzball 7][fb] — existing MUF
programs, MPI descriptions and `.db` worlds are meant to work unchanged — with
four deliberate departures:

| Fuzzball 7 | Emerald |
|---|---|
| Manual memory management across ~110k lines of C | Go |
| Plaintext listener, optional SSL | **TLS only** — raw TLS and WebSocket-over-TLS |
| Flat-file dump; the world freezes for every save | **Postgres**, written continuously in the background |
| autotools and a hand-rolled Dockerfile | A single static binary in a rootless **Podman** container |

The Fuzzball C sources this is ported from are vendored as a submodule at
`fuzzball/`, as read-only reference:

```bash
git submodule update --init fuzzball
```

## Status

All nine milestones are done. The server imports a legacy world, accepts real
MUCK clients over TLS and WebSocket, runs MUF and evaluates MPI, and supports
look, movement, speech, building and admin commands.

Programs can suspend themselves on `READ`, `SLEEP`, `EVENT_WAITFOR` and
timers, and `@ps` and `@kill` manage what is waiting. The MUF editor works, so
programs can be written on the server rather than only imported: `@program`
makes one and opens it, `@edit` reopens it, `@list` prints it. The wizard
commands are in — `@force`, `@toad`, `@boot`, `@stats`, `@pcreate` — along
with `@sanity`, `@sanfix` and `@sanchange` for a damaged database. MCP 2.1 and
MCP-GUI are negotiated with clients that speak them, so a program can put a
dialog on a client that can show one. `examine` reports what Fuzzball's does,
in the same shape, including the property-listing form.

Every MUF primitive and every MPI function is implemented: 412 of the 417
primitive names, the other five being compiler internals no program can name,
and all 140 MPI functions.

One thing is deliberately not ported: `DEBUGGER_BREAK`'s interactive prompt.
The instruction tracer behind it is — a program flagged `DARK` prints a line
per instruction — but there is no prompt to step from, so a break turns
tracing on rather than suspending the program.

**None of that is taken on trust.** Every primitive, every MPI function and
every command above is checked against a real Fuzzball 7 line for line, not
against a reading of its source — see "Checking against real Fuzzball" below.
That harness has repeatedly found this server wrong where careful reading of
the C had said otherwise.

## Building

There is a Makefile; `make` on its own lists the targets.

```bash
make build
```

## Quick start

```bash
make pod-import pod-run
```

That builds the image, starts Postgres, imports the starter world, and runs the
server in a container with the uid mapping described below. Import a different
world with `make pod-import DUMP=path/to/world.db` — the `muf/` directory
beside it is picked up the same way. Then:

```bash
make connect
```

and `connect One potrzebie`. `make pod-logs` follows the server's output and
`make pod-stop` shuts it down.

To run on the host instead, `make import run`.

## Connecting

Once a world is imported and the server is running, connect with any client
that speaks TLS:

```bash
openssl s_client -quiet -connect localhost:4202
```

Then `connect <name> <password>`. Browser clients use the WebSocket listener on
`:4203` at `/muck`; both terminate into the same session, so everything behaves
identically.

Two command-matching rules are inherited deliberately and will look odd
otherwise:

- `QUIT` and `WHO` are **case-sensitive**. Lowercase `quit` falls through to
  exit matching, which is why the starter world ships a `quit` exit that tells
  you to use capitals.
- **Exits beat built-in commands.** A world that defines its own `look` or
  `@view` exit wins. A wizard can prefix a line with `!` to skip exit matching
  and reach the built-in.

## Running locally

Generate a self-signed certificate — there is no cleartext fallback, so the
server will not start without one:

```bash
mkdir -p deploy/tls && openssl req -x509 -newkey rsa:4096 -nodes -days 365 -subj /CN=localhost -keyout deploy/tls/key.pem -out deploy/tls/cert.pem
```

Then bring up Postgres and the server:

```bash
podman-compose -f deploy/compose.yaml up
```

The image runs as the unprivileged `nonroot` user (uid 65532), which under
rootless Podman cannot read a private key owned by your account with the usual
`0600` permissions. Map your uid onto the container's:

```bash
podman run --userns=keep-id:uid=65532,gid=65532 -v ./deploy/tls:/etc/fbemerald/tls:ro,z ...
```

The alternative — loosening the key's permissions — is worse.

## Configuration

Pre-database settings come from the environment, because a TLS-only server
cannot read its own listener configuration out of a database it has not opened
yet. This is why Fuzzball's `ssl_*` `@tune` parameters have no equivalent.

| Variable | Default | Meaning |
|---|---|---|
| `FBE_DATABASE_URL` | — | Postgres connection string (required) |
| `FBE_TLS_CERT_FILE` | — | Certificate path (required) |
| `FBE_TLS_KEY_FILE` | — | Private key path (required) |
| `FBE_TLS_CIPHER_POLICY` | `modern` | `modern` (TLS 1.3) or `compat` (adds TLS 1.2) |
| `FBE_LINE_ADDR` | `:4202` | TLS listener for MUCK clients |
| `FBE_WSS_ADDR` | `:4203` | WebSocket-over-TLS listener |
| `FBE_FLUSH_INTERVAL` | `1s` | Bounds how much a crash can lose |
| `FBE_MAX_CONNECTIONS` | `1024` | Concurrent connections in total; `0` disables |
| `FBE_MAX_PER_HOST` | `16` | Concurrent connections from one address; `0` disables |
| `FBE_CONNECT_RATE` | `30` | New connections one address may open per window; `0` disables |
| `FBE_CONNECT_WINDOW` | `1m` | The window `FBE_CONNECT_RATE` counts over |
| `FBE_PPROF_ADDR` | — | Serve `net/http/pprof` here; must be a loopback address |
| `FBE_WEB_ADDR` | `127.0.0.1:4204` | Where the optional configurator listens |
| `FBE_WEB_TLS_CERT_FILE` | `FBE_TLS_CERT_FILE` | The configurator's certificate |
| `FBE_WEB_TLS_KEY_FILE` | `FBE_TLS_KEY_FILE` | The configurator's key |
| `FBE_WEB_SESSION_TTL` | `2h` | How long a configurator login lasts |

Everything else is an `@tune` parameter, as upstream. Inspect the table with:

```bash
fbemerald tune
```

### Limits

Two layers sit in front of the game, and they answer different threats.

The connection limits above are refused at **accept time**, before the TLS
handshake and before the world goroutine hears about the connection. They
exist because Fuzzball's own limits all sit *after* authentication, which is
too late to help against a peer that never authenticates. They are
environment settings rather than `@tune` parameters for the same reason TLS
is: a server under a flood has to keep refusing while the database is
unreachable. The per-host defaults are generous for a real player with
several clients and stingy for a script — raise `FBE_MAX_PER_HOST` if your
players share an address behind NAT.

Once someone is connected, Fuzzball's own spam limiter applies, and it is an
`@tune` matter: `command_burst_size` commands in hand, `commands_per_time`
more every `command_time_msec`. Spending the allowance neither disconnects
anyone nor loses what they typed — the connection simply waits. A player in
the MUF editor or answering a `READ` is refilled eight times as fast, since
typing program text is not the traffic the limiter is for.

`playermax`, `playermax_limit` and the two messages beside them cap how many
players may be *logged in*, and are `@tune` parameters as upstream. A true
wizard is exempt, so an admin can always get in to deal with whatever filled
the server up.

### Profiling

`FBE_PPROF_ADDR` serves the standard Go profiling endpoints. It is refused
unless it binds to loopback — the handlers hand out goroutine stacks and heap
contents, so reach them through an SSH tunnel rather than exposing the port:

```bash
ssh -N -L 6060:127.0.0.1:6060 your-server
```

```bash
go tool pprof http://127.0.0.1:6060/debug/pprof/profile?seconds=30
```

### The configurator

`fbeconfig` is an optional web interface over the same database and the same
`FBE_*` variables: a status page, a `@tune` editor, a manual editor, player
management and an object inspector. It is a separate binary and a separate
image, so a deployment that does not want one does not ship it.

```bash
make config                        # run it against the local database
make pod-config-build pod-config-run   # ...or in a container
```

```bash
podman-compose -f deploy/compose.yaml --profile admin up
```

Sign in with a wizard's name and MUCK password — there is no separate account
store. Two things are worth knowing:

- **It is read-only while the server is running.** That is enforced on every
  request that changes something, not just by leaving the inputs out, and it is
  decided by a Postgres advisory lock the server holds for its lifetime. Stop
  the server to edit anything.
- **It bypasses the game**, so its own log is the only record of what was done.
  A password changed here leaves no trace in the MUCK's logs.

`FBE_WEB_ADDR` defaults to loopback. Binding it wider is allowed and warned
about: anyone who can reach it and knows a wizard's password has the world, so
put it behind a tunnel or a reverse proxy rather than on a public interface.

## Importing a legacy world

```bash
fbemerald import path/to/starterdb.db
```

Fuzzball splits a world across three places: the `.db` dump, a `muf/` directory
of program sources named `<dbref>.m`, and a `macros` file inside it. A dump on
its own carries no code, so the importer looks for `muf/` beside the dump and
one level up from a `data/` directory; `-muf-dir` overrides it.

`-dry-run` reads and reports without writing. Importing into a database that
already holds a world is refused unless you pass `-force`.

Only the `Foxen9` format is read, which is what Fuzzball 7 writes. Converting
older dumps is upstream's job and its own binary does it.

## Passwords

Emerald hashes with Argon2id. It verifies both formats Fuzzball wrote — bare
base64 MD5, and the newer PBKDF2-HMAC-SHA512 `$1$salt$hex` — so imported
players can still log in, and upgrades them in place on the first successful
login.

Two deliberate differences:

- Fuzzball accepts **any** password for a player whose stored password is
  empty. Emerald refuses the login instead. The importer lists any such players
  so you can set passwords on them.
- Fuzzball compares only the leading bytes of a stored hash, so a truncated
  hash matches. Emerald compares the whole value, in constant time.

## How persistence works

The in-memory object graph is authoritative. One goroutine owns it and runs
every mutation, because Fuzzball is single-threaded and MUF depends on that:
primitives mutate the graph non-atomically and multitasking is cooperative.
Connections and the database talk to that goroutine over channels.

Changed objects are marked dirty, snapshotted as deep copies on the world
goroutine, and written to Postgres in one transaction per flush. Saving
therefore never touches a live object and never pauses the game, which is what
replaces Fuzzball's dump cycle. `@dump` becomes a forced flush that returns
immediately.

The guarantee is that at any instant, every change older than
`FBE_FLUSH_INTERVAL` is already durable. A crash loses at most that window,
rather than up to `dump_interval`.

Containment chains are stored as Fuzzball keeps them, because MUF can observe
their order, and each object also records its own location. That redundancy is
the recovery path: on load the chains are checked against what the objects
claim, and any that disagree are rebuilt rather than silently orphaning
everything past the break.

## Logs

Fuzzball wrote a dozen files under `logs/`, chosen by the `file_log_*`
parameters. Emerald writes one structured stream to stderr and tags each
record with the channel the old server would have used, so you can split it
back apart with whatever you already run. `FBE_LOG_FORMAT=json` makes that
mechanical.

| Channel | What it carries |
|---|---|
| `status` | Server lifecycle: boot, listeners, flushes, `@tune` changes |
| `security` | Authentication, password changes, privileged commands |
| `command` | Player commands |
| `program` | Compiles and edits |
| `muferror` | MUF runtime errors |
| `muf` | MUF diagnostics, written by `USERLOG` |
| `gripe` | Player gripes |
| `sanity` | Database consistency |

`security` is the one channel with no Fuzzball ancestor. Upstream scattered
these records through its status log, where a failed login sat between a flush
report and a compile warning; collecting them gives you something to alert on:

```bash
fbemerald serve 2>&1 | jq -c 'select(.channel == "security")'
```

What lands there is anything an intruder would have to do, or anything that
changes who may do what — connections and failed logins, `@password` and
`SETPASSWORD`, `@toad`, `@boot`, `@force`, `@shutdown`, the `@san*` family,
and every refusal of a wizard command.

## Backing up and restoring

The world lives in Postgres, so it backs up the way any other database does —
there is no dump file to copy, and no need to stop the game to take one.
`pg_dump` runs against a consistent snapshot, so a backup taken while players
are connected is a coherent world rather than a torn one.

```bash
pg_dump --format=custom --file=world-$(date +%F).dump \
  "postgres://fbemerald@localhost:55432/fbemerald"
```

Restoring goes into an **empty** database. `--clean` against a live one would
drop the world out from under a running server, so stop it first:

```bash
createdb -h localhost -p 55432 -U fbemerald fbemerald_restored
pg_restore --dbname="postgres://fbemerald@localhost:55432/fbemerald_restored" \
  world-2026-01-01.dump
```

Then point the server at it with `FBE_DATABASE_URL` and start it. The schema
is migrated automatically on boot, so a dump taken from an older build
restores into a newer one without a separate step.

Two things are worth knowing when planning a schedule. A crash loses at most
`FBE_FLUSH_INTERVAL` of play, so a backup is about recovering from a mistake —
a bad `@sanfix`, a toading nobody meant — rather than from a crash. And
because the in-memory graph is authoritative, a backup restored under a
*running* server would be ignored until it restarts: always stop, restore,
then start.

## Shutting down

`@shutdown` and `SIGTERM` take the same path. Everyone connected is told
`## The server is shutting down. ##` first, then the world goroutine drains
whatever work is queued and makes a final flush, with a 30-second budget for
the write. Nothing already accepted is lost.

The container is configured with a 30-second stop grace period to match. A
second signal aborts immediately, so an operator is never stuck waiting on a
shutdown that has wedged.

## Checking against real Fuzzball

The MUF port is verified against Fuzzball 7 itself rather than against reading
of its source. `make golden-build` compiles the C server from the `mother`
branch into a container; `make golden` then runs the same MUF through both and
diffs what they print.

```bash
make golden-build   # once, takes a few minutes
make golden
```

## Testing

```bash
go test -race ./...
```

The `internal/store` tests need a Postgres to talk to and skip without one:

```bash
podman run -d --name fbe-pg -e POSTGRES_USER=fbemerald -e POSTGRES_PASSWORD=fbemerald -e POSTGRES_DB=fbemerald -p 55432:5432 docker.io/library/postgres:17-alpine
```

```bash
make test
```

Store tests run against a database of their own (`fbemerald_test`), never the
one holding your world, and each test isolates itself further into a scratch
schema. The schema is set in the connection string rather than with `SET`,
because GORM pools connections and a `SET` reaches only one of them — every
other query would silently land in `public`. Each test asserts its isolation
before doing anything, so a regression there fails loudly instead of quietly
writing to a real database.

## Upstream coverage

[`docs/upstream-coverage.md`](docs/upstream-coverage.md) audits Emerald against
Fuzzball 7's three manuals. In short: **MPI is complete**, **MUF is complete**
— every primitive and every compiler directive — and **40 of about 112
player commands are missing**, mostly the verbs that set message and lock
properties whose engine already works.

It also answers two architecture questions: Emerald is partly crash-only by
design, and deliberately does not conform to 12-factor.

## Compatibility notes

`@tune` parameter names are a runtime API, not labels — MUF looks them up by
string via `SYSPARM` — so they are preserved verbatim. The exceptions:

- **Dropped** (replaced by the environment variables above): `ssl_cert_file`,
  `ssl_key_file`, `ssl_keyfile_passwd`, `ssl_min_protocol_version`,
  `ssl_cipher_preference_list`, `ssl_auto_reload_certs`,
  `server_cipher_preference`, and `starttls_allow` — every listener is already
  TLS, so there is nothing to start.
- **Renamed**: `smtp_ssl_type` → `smtp_tls_mode`. The old name still resolves.
- **Inert**: the `dump_*` parameters and `diskbase_propvals` still read and
  write, so MUF that consults them keeps working, but nothing acts on them.
  `fbemerald tune` marks them.

`DESCRSECURE?` now always returns 1, and the insecure branch of `NOTIFY_SECURE`
and `ARRAY_NOTIFY_SECURE` is unreachable.

[fb]: https://www.fuzzball.org/
