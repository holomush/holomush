# HoloMUSH external-NATS account scoping (CLUSTER-02)

This directory ships the single-principal subject-scoping assets for running the
HoloMUSH event bus against an **external** NATS cluster (`event_bus.mode:
external`). Game-topic NATS subjects are single-principal by design
(phase3d-grounding Decision 4): only the `holomush-server` account connects on
`events.>`, `audit.>`, and `internal.>`. Enforcement lives at the NATS account
layer — the server never runs app-level ACLs; it only self-verifies at boot that
its own account is not over-scoped.

| File | Purpose |
| --- | --- |
| `holomush-server.account.conf` | NATS account template granting `holomush-server` publish+subscribe on exactly `events.>`, `audit.>`, `internal.>`, `_INBOX.>` and nothing else. Loadable directly by `nats-server -c`, or translated to nsc/JWT (below). |
| `verify-scoping.sh` | Connects with a **non-server** credential and asserts publish+subscribe are DENIED on all three game-topic prefixes. Exits non-zero if any is permitted. |

## Grant list

The `holomush-server` account is granted publish **and** subscribe on exactly:

| Prefix | Why |
| --- | --- |
| `events.>` | The EVENTS JetStream stream and all game events (`events.<game_id>.…`). |
| `audit.>` | Host-owned audit subjects. |
| `internal.>` | Cluster heartbeats, cache-invalidation, and the audit DLQ (`internal.<game_id>.audit.dlq.>`, D-12). |
| `_INBOX.>` | Request-reply **reply inboxes**. The crypto cache-invalidation coordinator uses `NewRespInbox` (`internal/eventbus/crypto/invalidation/coordinator.go`); without this grant the N-of-N acks cannot return. |

Nothing else is granted. Any principal that is not `holomush-server` is denied on
the three game-topic prefixes.

## Option 1 — static accounts (simple deploys, CI proof)

Replace the placeholder passwords in `holomush-server.account.conf`, then:

```sh
nats-server -c holomush-server.account.conf -js -sd /var/lib/nats
```

Point HoloMUSH at it with `event_bus.mode: external`, `event_bus.url`, and a
credential for the `holomush-server` user.

## Option 2 — decentralized JWT auth with nsc (recommended for production)

The permission allow-lists are identical to Option 1; only the auth mechanism
differs. Using [`nsc`](https://github.com/nats-io/nsc):

```sh
# 1. Operator + system account (one-time per cluster).
nsc add operator --generate-signing-key --sys --name HoloMUSH

# 2. The single application account.
nsc add account HOLOMUSH_SERVER

# 3. Scope the account's default user permissions to exactly the four prefixes.
nsc edit account HOLOMUSH_SERVER \
  --js-mem-storage -1 --js-disk-storage -1

# 4. The server user, scoped to the game-topic prefixes + reply inboxes.
nsc add user --account HOLOMUSH_SERVER holomush-server \
  --allow-pub 'events.>,audit.>,internal.>,_INBOX.>' \
  --allow-sub 'events.>,audit.>,internal.>,_INBOX.>'

# 5. Mint the .creds file HoloMUSH loads via event_bus.credentials.
nsc generate creds --account HOLOMUSH_SERVER --name holomush-server \
  > holomush-server.creds

# 6. Push the operator/account JWTs to the cluster's account resolver
#    (or export a resolver.conf and load it with nats-server -c).
nsc push --all
```

Configure HoloMUSH:

```yaml
event_bus:
  mode: external
  url: nats://nats.internal:4222
  credentials: /run/secrets/holomush-server.creds
  tls: { ca: /run/secrets/ca.pem, cert: /run/secrets/tls.pem, key: /run/secrets/tls.key }
```

## Verifying the scoping

Two complementary checks prove single-principal from both sides (D-13):

1. **Boot-time self-check (internal).** On external-mode boot the server runs
   `eventbus.VerifyAccountScoping` and REFUSES to start if its own account can
   publish/subscribe outside the granted prefixes (fail-closed). No action
   needed — an over-scoped account aborts boot with `EVENTBUS_ACCOUNT_OVERSCOPED`.

2. **External verification (outside).** Run `verify-scoping.sh` with a
   **non-server** credential to confirm other principals are locked out:

   ```sh
   NATS_URL=nats://nats.internal:4222 \
   NATS_CREDS=/path/to/nonserver.creds \
     ./verify-scoping.sh
   ```

   With the static template, `holomush-verify` is the built-in non-server
   identity:

   ```sh
   NATS_URL=nats://127.0.0.1:4222 \
   NATS_USER=holomush-verify NATS_PASSWORD=holomush-verify-CHANGEME \
     ./verify-scoping.sh
   ```

   Exit 0 means every publish and subscribe on `events.>`/`audit.>`/`internal.>`
   was denied. A non-zero exit lists which operation was wrongly permitted.

## Deferred: read-only operator account

A distinct read-only operator account (`holomush-operator-read`, `subscribe:
events.>` only, for monitoring/debugging) is a **future option** tracked under
`holomush-s5ts` — intentionally NOT shipped in this phase. Audit-table reads
remain the localhost UNIX admin-socket path, not a NATS subscribe.
