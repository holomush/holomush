# `legacy_id` Elimination Cutover (holomush-w9ml)

One-time deploy step for `holomush-w9ml`. Run **once**, **before** bringing up
the binary that includes the `Actor.legacy_id` proto-field removal.

## What it does

1. `TRUNCATE events_audit` — removes legacy plugin-actor events whose envelope
   blobs reference the now-defunct `Actor.legacy_id` field. Migration 000018
   also truncates on first run; this command is idempotent with respect to that
   migration.
2. JetStream `PurgeStream("EVENTS")` — removes pre-cutover encrypted
   plugin-actor events from the stream. Their AAD bytes were sealed against the
   pre-w9ml proto shape; post-cutover code computes a different AAD, so AEAD
   verification fails. Purging avoids the failure mode.

## Why this is necessary

The `holomush-w9ml` epic removes `Actor.legacy_id` from the proto definition.
Events written before the cutover were encrypted with AAD derived from the full
proto shape (including `legacy_id`). After the field is removed, re-computing
the AAD produces a different byte sequence, which causes AEAD verification to
fail on every pre-cutover event. There is no safe migration path for these
events; they must be discarded.

## Prerequisites

- HoloMUSH server **stopped** (embedded NATS must not be running; the cutover
  command connects to an external NATS instance)
- External NATS server with JetStream enabled, accessible at `NATS_URL`
- PostgreSQL accessible at `DATABASE_URL`
- `task` installed

## Run

```bash
DATABASE_URL=postgres://user:pass@host/dbname \
NATS_URL=nats://localhost:4222 \
task migrate:plugin-actors-cutover
```

Override the stream name if your deployment uses a non-default name:

```bash
DATABASE_URL=... NATS_URL=... NATS_STREAM_NAME=EVENTS task migrate:plugin-actors-cutover
```

The default stream name is `EVENTS` (the canonical project stream).

## Expected output

```text
time=... level=INFO msg="events_audit truncated"
time=... level=INFO msg="jetstream stream purged" stream=EVENTS
```

A non-zero exit code indicates failure. Check the error message for details.

## Failure modes

| Failure | State after exit | Recovery |
|---|---|---|
| `DATABASE_URL` not set | Nothing done | Set env var and re-run |
| `NATS_URL` not set | Nothing done | Set env var and re-run |
| PG connect failed | Nothing done | Fix connection and re-run |
| `TRUNCATE events_audit` failed | Nothing done | Fix PG access and re-run |
| NATS connect failed | PG already truncated | Fix NATS connection and re-run (TRUNCATE is idempotent) |
| Stream lookup failed | PG already truncated | Ensure NATS JetStream is enabled and stream exists; re-run |
| Stream purge failed | PG already truncated | Fix NATS and re-run |

All operations are idempotent: re-running after partial failure is safe.

## Post-cutover

1. Stop the external NATS server used for this cutover.
2. Deploy the new HoloMUSH binary (the one with `legacy_id` removed).
3. The bootstrap orphan check passes on first start — no legacy plugin-actor
   events remain in the audit log.
4. Embedded NATS starts fresh with an empty `EVENTS` stream.
