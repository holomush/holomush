---
title: "External NATS Deployment"
---

This runbook walks the full lifecycle of running the HoloMUSH event bus against
an **external** NATS cluster instead of the default embedded in-process server:
provision the cluster and its single-principal account, mint credentials,
configure and cut over HoloMUSH, prove the subject scoping, operate the audit
dead-letter queue (DLQ), and roll back to embedded if needed.

Embedded NATS stays the zero-config default — external mode is a deliberate
opt-in for horizontally-scaled multi-node deployments. If you run a single core
process, you do not need this guide; see
[Deploying HoloMUSH](/operating/how-to/deploy/deployment/).

:::caution[Read the cutover data stance first]
The external cluster does **not** inherit the embedded server's event history.
On cutover the PostgreSQL `events_audit` table is the durable audit record and
survives unchanged; the JetStream `EVENTS` stream **starts fresh** on the
external cluster. There is no stream migration. Read the "Cut over from
embedded" step below before you switch any production node.
:::

## Prerequisites

| Requirement                     | Notes                                                                   |
| ------------------------------- | ----------------------------------------------------------------------- |
| A reachable NATS cluster        | JetStream enabled, file storage. `nats:2-alpine` is the tested image.   |
| The `nats` CLI                  | [natscli](https://github.com/nats-io/natscli) — for provisioning and DLQ inspection. |
| `nsc` (recommended)             | [nsc](https://github.com/nats-io/nsc) — decentralized JWT/NKey auth.    |
| The `deploy/nats/` assets       | Account templates, `verify-scoping.sh`, and the compose overlay ship in the repo. |
| A working embedded deployment   | Cut over from a known-good node; do not first-boot straight into external mode. |

The working reference for every step below is `compose.cluster.yaml` (the D-14
multi-replica overlay) together with the fragments under `deploy/nats/`.

## Step 1 — Provision the external NATS cluster and account

Game-topic NATS subjects are **single-principal** by design: only the
`holomush-server` account may publish or subscribe on `events.>`, `audit.>`,
`internal.>`, and its request-reply inboxes (`_INBOX.>`). Enforcement lives at
the NATS account layer — HoloMUSH runs no app-level ACLs.

The account grant list is exactly:

| Prefix       | Why                                                                        |
| ------------ | -------------------------------------------------------------------------- |
| `events.>`   | The `EVENTS` JetStream stream and all game events (`events.<game_id>.…`).  |
| `audit.>`    | Host-owned audit subjects.                                                 |
| `internal.>` | Cluster heartbeats, cache-invalidation, and the audit DLQ (`internal.<game_id>.audit.dlq.>`). |
| `_INBOX.>`   | Request-reply reply inboxes (the crypto cache-invalidation N-of-N acks).   |

Nothing else is granted. Two options ship, with identical permission
allow-lists — full detail lives in `deploy/nats/README.md`.

### Option A — static accounts (simple deploys, CI proof)

Replace the placeholder passwords in `deploy/nats/holomush-server.account.conf`,
then run the server against it:

```bash
nats-server -c deploy/nats/holomush-server.account.conf -js -sd /var/lib/nats
```

### Option B — decentralized JWT auth with nsc (recommended for production)

```bash
# 1. Operator + system account (one-time per cluster).
nsc add operator --generate-signing-key --sys --name HoloMUSH

# 2. The single application account.
nsc add account HOLOMUSH_SERVER
nsc edit account HOLOMUSH_SERVER --js-mem-storage -1 --js-disk-storage -1

# 3. The server user, scoped to the game-topic prefixes + reply inboxes.
nsc add user --account HOLOMUSH_SERVER holomush-server \
  --allow-pub 'events.>,audit.>,internal.>,_INBOX.>' \
  --allow-sub 'events.>,audit.>,internal.>,_INBOX.>'
```

The compose overlay's reference NATS service loads a JetStream +
single-principal config from `deploy/nats/cluster-server.conf` and stores its
data under `/data` (file storage, backend network only, no host port).

## Step 2 — Mint the credentials file

HoloMUSH authenticates with a NATS `.creds` file (JWT/NKey). Generate it for the
`holomush-server` user and push the account JWTs to the cluster's resolver:

```bash
# Mint the .creds file HoloMUSH loads via event_bus.credentials.
nsc generate creds --account HOLOMUSH_SERVER --name holomush-server \
  > holomush-server.creds

# Push the operator/account JWTs to the cluster's account resolver.
nsc push --all
```

Deliver `holomush-server.creds` to each core replica as a mounted secret (for
example `/run/secrets/holomush-server.creds`). For dev clusters a
`user:password` embedded in the URL also works — nats.go accepts it directly —
but production should use the `.creds` file plus TLS.

## Step 3 — Configure HoloMUSH for external mode

The event bus is configured under the `event_bus:` section of the HoloMUSH
config file. External mode requires a non-empty `url` — `mode: external` with no
URL fails closed at config-validation time.

```yaml
event_bus:
  mode: external
  url: nats://nats.internal:4222
  credentials: /run/secrets/holomush-server.creds
  tls:
    ca: /run/secrets/ca.pem
    cert: /run/secrets/tls.pem
    key: /run/secrets/tls.key
  provision: true
  dlq:
    max_age: 720h
    max_bytes: 1073741824
```

| Key           | Meaning                                                                                      |
| ------------- | -------------------------------------------------------------------------------------------- |
| `mode`        | `embedded` (default) or `external`. `external` requires `url`.                                |
| `url`         | NATS server/cluster URL. Required in external mode.                                           |
| `credentials` | Path to the NATS `.creds` file. Empty means no creds-file auth (dev may carry `user:pass` in the URL). |
| `tls`         | Optional mTLS / private-CA block (`ca`, `cert`, `key` are filesystem paths).                  |
| `provision`   | `true` (default) lets the server idempotently create/update JetStream streams. See below.     |
| `dlq.max_age` | Audit DLQ retention cap. Defaults to ~30 days (`720h`) when unset.                            |
| `dlq.max_bytes` | Audit DLQ size cap in bytes. `0` means age-capped only.                                     |

**Provisioning seam (`provision`).** With `provision: true` the server
idempotently declares the `EVENTS` stream on boot (a concurrent declare from a
second replica is safe — `CreateOrUpdateStream` is idempotent). For a
locked-down cluster whose server account lacks `$JS.API` stream-admin
permissions, set `provision: false`, pre-declare the stream out of band, and the
server then **verifies existence and fails closed on mismatch** instead of
creating.

The working example is `deploy/nats/cluster-config.yaml`, mounted into both core
replicas by `compose.cluster.yaml`:

```bash
docker compose -f compose.prod.yaml -f compose.cluster.yaml up -d
```

That overlay adds a standalone NATS JetStream service and a second core replica,
switches both replicas from embedded to external mode against the in-stack
`nats` service, and is the same substrate the multi-process cluster smoke
(`scripts/smoke/cluster-smoke.sh`) drives.

## Step 4 — Cut over from embedded

:::danger[Cutover changes what event history exists]
The external `EVENTS` stream starts empty. HoloMUSH does not migrate the
embedded server's JetStream data.

- **Durable record survives.** The PostgreSQL `events_audit` table is the
  forever-archive of every published event and is untouched by the cutover.
  Audit history, DLQ replay targets, and the crypto audit trail all read from
  Postgres, so they carry across unchanged.
- **The live stream starts fresh.** Recent-event replay served from JetStream
  (`HistoryReader` before it falls back to Postgres) begins from the cutover
  point on the external cluster. Older history transparently falls back to
  `events_audit`.

Do not expect the external cluster to "inherit" the embedded stream — plan the
cutover as a fresh JetStream domain backed by the same durable Postgres audit.
:::

Cutover procedure:

1. Provision the cluster and account (Steps 1–2) and stage the `event_bus:`
   config (Step 3) without restarting yet.
2. Stop the embedded-mode core process.
3. Start the core process with `mode: external`. If the external NATS is
   unreachable at boot the server **refuses to start** with a clear error — the
   orchestrator (compose restart policy / k8s) owns retry. Once connected,
   nats.go's built-in reconnect handles transient drops.
4. On first external boot with `provision: true`, the server declares the
   `EVENTS` stream. Confirm it exists:

   ```bash
   nats --creds holomush-server.creds -s nats://nats.internal:4222 stream ls
   ```

## Step 5 — Verify the subject scoping

Two complementary checks prove single-principal scoping from both sides:

1. **Boot-time self-check (internal).** On external-mode boot the server runs
   `eventbus.VerifyAccountScoping` and **refuses to start** if its own account
   can publish or subscribe outside the granted prefixes — it aborts with
   `EVENTBUS_ACCOUNT_OVERSCOPED`. No operator action is needed; an over-scoped
   server account never reaches a running state.

2. **External verification (outside).** Run `deploy/nats/verify-scoping.sh` with
   a **non-server** credential to prove every other principal is locked out. It
   asserts that publish and subscribe are DENIED on a probe subject under each of
   `events.>`, `audit.>`, and `internal.>`, and exits non-zero if any of those
   operations is wrongly permitted:

   ```bash
   NATS_URL=nats://nats.internal:4222 \
   NATS_CREDS=/path/to/nonserver.creds \
     ./deploy/nats/verify-scoping.sh
   ```

   With the static template, `holomush-verify` is the built-in non-server
   identity:

   ```bash
   NATS_URL=nats://127.0.0.1:4222 \
   NATS_USER=holomush-verify NATS_PASSWORD=holomush-verify-CHANGEME \
     ./deploy/nats/verify-scoping.sh
   ```

Exit `0` means every publish and subscribe on the three game-topic prefixes was
denied. A non-zero exit names the operation that was wrongly permitted — fix the
account grants before continuing.

## Step 6 — Operate the audit dead-letter queue

Audit messages that exhaust redelivery (usually because Postgres is down) are
captured to the bounded `EVENTS_AUDIT_DLQ` JetStream stream instead of being
dropped. Its failure domain is deliberately independent of Postgres. Dead
letters you can't replay are just nicer-looking data loss — so once the outage
is fixed you replay them back into `events_audit`.

The `holomush audit dlq` commands dial the external cluster (they read `url`,
`credentials`, and `tls` from the `event_bus` config section) and write directly
to the `events_audit` table (via `DATABASE_URL`). They do not use the admin UNIX
socket.

**Monitor.** Alert on the Prometheus counter
`holomush_audit_dlq_messages_total`, which increments once per captured dead
letter. It rises long before the DLQ's age/size caps would drop anything.

**Inspect.**

```bash
# Stream-level summary: message count, bytes, age of the DLQ.
holomush audit dlq list --config /etc/holomush/config.yaml

# A single dead letter's headers and metadata by its Nats-Msg-Id.
holomush audit dlq show <nats-msg-id> --config /etc/holomush/config.yaml
```

**Replay** once the underlying outage is fixed. Replay re-drives dead letters
through the same idempotent write path the live projection uses, so re-running
is safe:

```bash
# Replay everything.
holomush audit dlq replay --all --config /etc/holomush/config.yaml

# Replay a single dead letter.
holomush audit dlq replay --msg-id <nats-msg-id> --config /etc/holomush/config.yaml

# Cap how many dead letters one pass scans.
holomush audit dlq replay --all --limit 500 --config /etc/holomush/config.yaml
```

## Step 7 — Roll back to embedded

If you need to return to the embedded in-process server, set the mode back and
restart:

```yaml
event_bus:
  mode: embedded
```

The same data stance applies in reverse: PostgreSQL `events_audit` remains the
durable record and carries across the rollback unchanged, while the embedded
`EVENTS` stream starts fresh in-process. Drain the external DLQ first (Step 6) —
`holomush audit dlq replay` targets the external cluster, so replay any
outstanding dead letters before you disconnect from it.

## Future options

The following are intentionally **not** part of this phase and are documented
here only as follow-ups:

- **Read-only operator account.** A distinct `holomush-operator-read` account
  (`subscribe: events.>` only, for monitoring/debugging) is a future option
  tracked under `holomush-s5ts`. It is not shipped: audit-table reads remain the
  localhost UNIX admin-socket path, not a NATS subscribe.
- **Sandbox migration.** Migrating the project's own sandbox
  (`game.holomush.dev`) to external NATS is a deferred operational task to run
  once this runbook has settled. Until then the sandbox continues on embedded
  mode — see [Sandbox Operations](/operating/how-to/sandbox/sandbox-operations/).

## Related

- [Database Management](/operating/how-to/database/) — the `events_audit`
  durable store DLQ replay writes to.
- [Sandbox Operations](/operating/how-to/sandbox/sandbox-operations/) — the
  project sandbox's day-to-day operations.
