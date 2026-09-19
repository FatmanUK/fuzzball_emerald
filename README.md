# Fuzzball Emerald

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

Early, but playable. M0 (skeleton, `@tune` table, container), M1 (object model,
properties, world goroutine, Postgres persistence), M2 (legacy importer) and M3
(transports, login, basic commands) are done: you can import the shipped
starter world, connect a real MUCK client over TLS, walk around, talk and
build.

M4 is done and M5 and M6 are partly done: MUF compiles and runs, and MPI is
evaluated in descriptions and exit messages. Every program the starter world
ships compiles except three that cannot compile anywhere, and the interpreter
executes arithmetic, control flow, procedures, scoped variables, arrays and
try/catch. Programs can now wait: `READ` suspends one until the player types a line,
`SLEEP` until a time passes, and `@ps` and `@kill` manage what is waiting.

The MUF editor works, so programs can be written on the server instead of only
imported: `@program` makes one and opens it, `@edit` reopens it, `@list` prints
it. The wizard commands are in — `@force`, `@toad`, `@boot`, `@stats`,
`@pcreate` — along with `@sanity`, `@sanfix` and `@sanchange` for a damaged
database, and MCP 2.1 and MCP-GUI are negotiated with clients that speak them,
so a program can put a dialog on a client that can show one.

`examine` reports what Fuzzball's does, in the same shape, including the
property-listing form.

Each of those is checked against a real Fuzzball 7 line for line, not just
against a reading of its source.

301 of 417 MUF primitives and 51 of 140 MPI functions are implemented, so a
real program may still stop at one it needs.

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

Everything else is an `@tune` parameter, as upstream. Inspect the table with:

```bash
fbemerald tune
```

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
