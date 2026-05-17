<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# In-Process Filter-at-Delivery as Load-Bearing Privacy Gate; NATS-as-Source-of-Truth for Consumer Config

**Date:** 2026-05-17
**Status:** Accepted
**Decision:** holomush-ghpx
**Deciders:** HoloMUSH Contributors

## Context

The `holomush-iwzt` privacy fix needs to apply a temporal floor to events delivered via Subscribe — not only to `QueryStreamHistory`. Subscribe creates one durable JetStream consumer per session with `DeliverAllPolicy` (`internal/eventbus/subscriber.go:177-186`). JetStream consumers accept exactly one `DeliverPolicy` per consumer, and the policy plus `OptStartTime` / `OptStartSeq` are server-side immutable on an existing consumer (NATS server error 10012: "deliver policy can not be updated"; see [NATS consumer config](https://docs.nats.io/nats-concepts/jetstream/consumers), "Editable" column).

Design exploration considered: per-subject consumer fan-out, adding a sequence-from-time API to `HistoryReader`, filter-at-delivery in the broadcaster, recreating the durable on every reattach. Each has different cost and complexity profiles. Six rounds of adversarial review refined the chosen approach.

## Decision

### Two-tier privacy enforcement

**Tier 1** is JetStream's native `DeliverByStartTimePolicy` with `OptStartTime = minFloor` (computed as the maximum scope-floor across all subscribed subjects at consumer creation) as a **performance hint** that bounds how far back JetStream will deliver.

**Tier 2** is per-subject filter-at-delivery in the Subscribe broadcaster (`internal/grpc/server.go` event dispatch loop) that drops any event with `Timestamp < streamScopeFloor(currentSessionInfo, event.Subject)`. **This is the load-bearing privacy gate.**

### NATS-as-source-of-truth for immutable consumer config

Both `OpenSession` (called on every Subscribe, including transport reattach) and `SetFilters` (called on location-following moves) MUST query `s.js.Consumer(ctx, StreamName, name)` before issuing `CreateOrUpdateConsumer`. On hit, copy `DeliverPolicy` / `OptStartTime` / `OptStartSeq` verbatim from `existing.CachedInfo().Config`; only `FilterSubjects` mutates. On `ErrConsumerNotFound`, compute fresh `minFloor`. On any other lookup error, **fail closed** (return wrapped `EVENTBUS_CONSUMER_LOOKUP_FAILED`; do not invoke `CreateOrUpdateConsumer`).

## Rationale

**Privacy correctness cannot depend on JetStream config immutability.** `OptStartTime` is set once at consumer creation. After session reattach or character move, the floor advances — but JetStream config cannot change. Filter-at-delivery is the only mechanism that can enforce the post-reattach floor on already-delivered events.

**`OptStartTime` is still valuable as a perf hint.** It bounds the total volume JetStream will replay on consumer first-create. Without it, `DeliverAllPolicy` would replay the entire retained stream — a latency disaster for the connect-to-live budget tracked at `holomush-87qu`.

**NATS-source-of-truth avoids cache-coherence bugs.** In-process or DB-row caching of `OptStartTime` would diverge across process restarts, consumer reaper actions, and concurrent OpenSession races. Querying NATS on every call is idempotent and serializable; error 10012 is unreachable under the documented call discipline (one in-flight OpenSession per session ID, enforced by the gRPC Subscribe handler being the sole control-plane owner of the session).

**Fail-closed on transient lookup errors** prevents the brief leak window where a missing `DeliverPolicy` in the new config (zero-value = `DeliverAllPolicy`) would be sent against an existing-but-temporarily-unreachable durable.

## Alternatives Considered

**A. Per-subject consumer fan-out (one durable per subject).** Rejected: multiplies JS state per session; more bookkeeping; more reaper attention; gains precision the filter-at-delivery already provides.

**B. Add `HistoryReader.LowerBoundByTime(subject, t) (uint64, error)` API and seed consumer with `DeliverByStartSequencePolicy`.** Rejected (round 3 finding B-R2.1): `HistoryReader` is consumed by `QueryStreamHistory` only, not Subscribe; the cold-tier seq-from-time lookup is unnecessary on the `QueryStreamHistory` path (existing `HistoryQuery.NotBefore` field suffices); JetStream already provides native `DeliverByStartTimePolicy`.

**C. Cache `OptStartTime` in-process on the `jetStreamSessionStream` instance.** Rejected (round 4 finding B-R4.1): the instance is freshly constructed per `OpenSession` call; the cached value does not survive across reattach.

**D. Cache `OptStartTime` in the `sessions` table.** Rejected: persists a perf hint to the durable store; cache-coherence concerns across process restarts; adds a column for a value already canonical in NATS.

**E (selected): NATS-source-of-truth query-then-copy pattern + filter-at-delivery as load-bearing gate.**

## Consequences

- **MUST** apply the per-subject filter-at-delivery check on every event in the Subscribe dispatch loop (I-PRIV-1).
- **MUST** query `s.js.Consumer(...)` before `CreateOrUpdateConsumer` in both `OpenSession` and `SetFilters` paths (I-PRIV-8).
- **MUST** preserve `DeliverPolicy` / `OptStartTime` / `OptStartSeq` from existing consumer config verbatim; only `FilterSubjects` may mutate (I-PRIV-3, I-PRIV-8).
- **MUST NOT** recreate the durable on `Subscribe.ReattachCAS`; the idempotent OpenSession call is effectively a no-op on the existing consumer (I-PRIV-3).
- **MUST NOT** cache `OptStartTime` in-process or in DB; NATS is the source of truth.
- **MUST** fail closed on lookup errors other than `ErrConsumerNotFound` — return wrapped error, do not invoke `CreateOrUpdateConsumer`.
- Adds one NATS round-trip per OpenSession (typically sub-millisecond). Acceptable per `holomush-87qu` latency context — much faster than the alternative cache-coherence machinery.

## References

- Spec: `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md` §6.2 (full two-tier model), I-PRIV-1, I-PRIV-3, I-PRIV-8
- Bead: `holomush-iwzt`
- Related ADRs: `holomush-wxty` (hard-gate for location streams), `holomush-rc8b` (per-session attach intervals)
- Code: `internal/eventbus/subscriber.go:159-366`, `internal/grpc/server.go:855`, `internal/eventbus/history/hot_jetstream.go:283`
- NATS: [Consumer config — Editable column](https://docs.nats.io/nats-concepts/jetstream/consumers) (DeliverPolicy = No)
- NATS error code: [server error 10012 discussion](https://github.com/nats-io/nats.py/issues/657)
