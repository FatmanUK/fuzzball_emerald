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

The Fuzzball C sources this is ported from live on the `mother` branch and are
read-only reference:

```bash
git show origin/mother:src/interp.c
```

## Status

Early. M0 (skeleton, `@tune` table, container) and M1 (object model,
properties, world goroutine, Postgres persistence) are done. There is no
network listener yet, so the server currently boots, loads the world and waits
for a signal.

## Building

```bash
go build ./cmd/fbemerald
```

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

## Testing

```bash
go test -race ./...
```

The `internal/store` tests need a Postgres to talk to and skip without one:

```bash
podman run -d --name fbe-pg -e POSTGRES_USER=fbemerald -e POSTGRES_PASSWORD=fbemerald -e POSTGRES_DB=fbemerald -p 55432:5432 docker.io/library/postgres:17-alpine
```

```bash
FBE_TEST_DATABASE_URL="postgres://fbemerald:fbemerald@localhost:55432/fbemerald?sslmode=disable" go test -race ./...
```

Each test runs in its own schema, so they do not interfere.

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
