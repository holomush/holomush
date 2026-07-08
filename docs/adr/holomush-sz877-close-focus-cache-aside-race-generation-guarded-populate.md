<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-sz877; do not edit manually; use `/adr update holomush-sz877` -->

# Close the focus cache-aside race with a generation-guarded populate

**Date:** 2026-07-08
**Status:** Accepted
**Decision:** holomush-sz877
**Deciders:** Sean Brandt

## Context

The focus hot-path cache (holomush-wm0fi) reads through a plain, non-locking GetConnection SELECT (session_store.go:992). A cache-aside reader can therefore miss, read the pre-write value, and populate the cache AFTER a concurrent eviction fired, leaving a stale entry that survives until the connection's next focus change. This is the classic cache-aside repopulation hazard and directly threatens G2 / INV-SCENE-69's no-stale-serve guarantee. It was the central finding of design-review round 1.

## Decision

The cache's populate path captures a generation counter before its store read (beginLoad) and commits the read result only if the generation is unchanged at commit time (commitLoad), rather than relying on eviction ordering alone. Eviction (Evict) bumps the generation and runs after a successful UpdateSessionConnection.

## Rationale

- Eviction ordering alone (evict-before or evict-after the write) does not close the race: a reader that started before the write can still commit a stale value after the eviction fires.
- The generation guard closes the race deterministically without locking the DB read, and is testable in isolation under the race detector; it is the mechanism that underwrites INV-SCENE-69.

## Alternatives Considered

- **Evict-ordering alone (rejected):** simple, no extra bookkeeping, but leaves a window where a stale read populates the cache after an eviction — it does not close the race.
- **Generation-guarded populate (chosen):** beginLoad captures the generation; commitLoad stores only if unchanged; Evict bumps it. Provably closes the race; adds an epoch/counter and a beginLoad/commitLoad method pair.

## Consequences

- Positive: no stale focus kind is ever cached for, and served to, a live connection after its focus has been committed (INV-SCENE-69).
- Negative: adds bookkeeping (a generation counter) and a dedicated concurrency test.
- Neutral: per-key epoch vs a single global counter is left to the plan; both close the race identically, differing only in false-invalidation rate under eviction churn. The size cap (LRU vs FIFO) is a memory backstop, not a correctness mechanism.
