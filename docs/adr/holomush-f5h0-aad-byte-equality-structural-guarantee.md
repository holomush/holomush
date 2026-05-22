<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Strengthen AAD Byte-Equality to a Structural Type Guarantee

**Date:** 2026-05-22
**Status:** Accepted
**Decision:** holomush-f5h0
**Deciders:** HoloMUSH Contributors
**Related:** holomush-gfo6, holomush-absb, holomush-rbw6
**Supersedes:** INV-P7-16 (in spirit; the original invariant text is retained in the Phase 7 crypto spec for historical continuity, with a forward reference to INV-TS-5)

## Context

The Phase 7 event-payload-crypto spec at `docs/superpowers/specs/2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md` introduced **INV-P7-16**: the `AuditRow → *eventbusv1.Event` adapter MUST produce a value whose AAD reconstruction is byte-equal to the AAD computed at encrypt time. Failure would manifest as every sensitive plugin-stored event failing AEAD tag-check on decrypt — indistinguishable from a deliberate downgrade attack.

The original invariant held under a discipline: the publisher at `internal/eventbus/publisher.go:202` truncated `event.Timestamp` to microseconds *before* AAD construction. PostgreSQL `TIMESTAMPTZ` columns (the audit row's storage) also have µs resolution. Both sides saw the same µs-truncated value, so AAD bytes matched.

This discipline was fragile in two ways:

1. **Code-review enforced.** Any new event-producing path that skipped the publisher (test fixtures, direct DB inserts in scripts) silently broke byte-equality.
2. **Test-modeled.** The integration test at `internal/eventbus/history/plugin_aad_reconstruction_test.go` carried four `Truncate(time.Microsecond)` calls modeling what the publisher and the PG column did at runtime — the test was a proxy for the invariant, not a direct expression of it.

The nanosecond-timestamps migration (`holomush-gfo6`) creates the opportunity to make this structural rather than disciplined.

## Decision

INV-P7-16 is superseded by **INV-TS-5** (declared in the spec at `docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md` §5):

> AAD round-trip from publish → DB persist → audit read → AAD reconstruction MUST be byte-equal at full nanosecond resolution.

The mechanism is structural, not disciplined:

1. The publisher at `internal/eventbus/publisher.go:202` MUST NOT truncate `event.Timestamp` before AAD construction or envelope marshal (INV-TS-4).
2. PostgreSQL audit columns (`events_audit.timestamp`, `plugin_core_scenes.scene_log.timestamp`, etc.) are `BIGINT` epoch nanoseconds, carrying full ns precision with no intermediate truncation (INV-TS-1 + `holomush-absb`).
3. The AAD encoding at `internal/eventbus/crypto/aad/aad.go:74` was already ns-native (`event.Timestamp.AsTime().UnixNano()` serialized as 8-byte BE int64). No change needed at this layer.

INV-P7-16's original wording is retained in the Phase 7 spec for historical continuity. Future contributors reading either spec see INV-TS-5 as the live form via the forward reference.

## Alternatives Considered

### A — Keep INV-P7-16's "truncate-at-publisher" discipline (rejected)

Status quo: publisher calls `event.Timestamp.Truncate(time.Microsecond)` before AAD construction so the ns precision matches the `TIMESTAMPTZ` µs-precision audit column on the round-trip. Rejected because (a) the contract is invisible to static checks (any path forgetting the truncate produces silent AAD tag mismatches surfaced only at decrypt time, far from the offending commit), and (b) the truncation has to be replicated everywhere a new event-producing path is added — multiplying the burden as the codebase grows.

### B — Add a runtime AAD-byte-equality assertion in audit-read (rejected)

Wrap `internal/eventbus/audit/projection.go` decrypt with an explicit "AAD constructed at decrypt matches AAD stored at encrypt" tripwire, escalating to a fatal log if they diverge. Rejected because the assertion fires AFTER the regression has shipped — useful as a backstop but does nothing to prevent the bug from reaching production. Structural enforcement at the column layer (INV-TS-1) eliminates the divergence at its source, making the runtime tripwire unnecessary.

### C — Chosen: column-layer enforcement via BIGINT epoch nanoseconds (INV-TS-1)

`events_audit.timestamp` (and all other persistent-time columns) become `BIGINT` storing ns since epoch. The publisher emits ns-precision events; the column stores ns-precision; the AAD reconstruction reads ns-precision. Byte-equality is mechanical, not disciplined. See `holomush-absb` for the BIGINT-over-`timestamp9` choice that made this option viable.

## Rationale

1. **Discipline is fragile.** The truncation contract had to be internalized by every contributor writing an event-producing path. There was no static check; violation was silent until audit-read decryption surfaced a tag mismatch — far from the offending commit. The cost of catching such a regression in production is disproportionate to the cost of structural enforcement.

2. **The AAD encoding was already ns-native.** `aad.go:74` serializes `UnixNano()`. The only reason byte-equality required truncation discipline was the column-resolution mismatch (PG µs vs. AAD ns). Removing that mismatch (via `BIGINT`-ns columns) makes the discipline obsolete.

3. **Adjacent invariant lineage.** This is not a new architectural decision drawn from outside Phase 7 — it is an explicit strengthening of a documented invariant from a finalized spec. The architectural lineage is traceable in the spec history.

4. **Atomicity is required.** Phase 1 of the implementation plan (`docs/superpowers/plans/2026-05-22-nanosecond-timestamps.md`) MUST land the publisher truncation deletion, the host crypto-domain migrations (`events_audit`, `crypto_keys`), and the plugin-side migration (`plugins/core-scenes/migrations/`) in a single PR. A partial Phase 1 leaves the invariant in a broken intermediate state where one tree is ns-precision and the other is still µs-truncated.

## Consequences

**Positive:**

- Future event-producing paths get byte-equal AAD for free, without documentation lookup.
- The Phase 7 AAD reconstruction test removes its four modeling-truncation `Truncate(time.Microsecond)` calls and becomes an honest end-to-end round-trip at full ns resolution.
- A new test (`TestRoundTripPreservesAADWithSubMicrosecondNanos`) asserts the sub-µs nanosecond component survives the publish → DB → read → AAD-reconstruct round-trip — a property that was structurally impossible to assert before.

**Negative:**

- Phase 1 must land atomically (publisher + host migration + plugin migration in one PR). A non-atomic ship breaks the invariant in the intermediate state.
- Plugin migrations (`plugins/core-scenes/migrations/`) and host migrations (`internal/store/migrations/`) become coupled by invariant — they ship together not because of a code dependency but because the type-system guarantee requires both to be present.

**Neutral:**

- INV-P7-16 in the Phase 7 spec is superseded (not deleted). The Phase 7 spec document retains the original wording with a forward reference to INV-TS-5. Phase 7's existing tests and lint guards continue to function; they assert a *stronger* property than they originally claimed (byte-equality at ns resolution rather than at µs).

## Implementation

Phase 1 of `holomush-gfo6` lands all atomic-required changes in one PR:

1. Delete `truncatedTimestamp := event.Timestamp.Truncate(time.Microsecond)` from `internal/eventbus/publisher.go:202`.
2. Delete the floor-comparison truncate at `internal/grpc/server.go:1100` (the related INV-TS-6).
3. Migrate `events_audit` and `crypto_keys` host columns to `BIGINT`-ns.
4. Migrate `plugins/core-scenes/migrations/` columns to `BIGINT`-ns.
5. Remove the four `Truncate(time.Microsecond)` calls from `internal/eventbus/history/plugin_aad_reconstruction_test.go` modeling publisher-side and PG-side truncation; assert byte-equality at ns precision.
6. Delete the regression-locking test `TestDispatchDeliveryForwardsEventTruncatedWithinSameMicrosecondAsFloor` at `internal/grpc/subscribe_loop_test.go:326` (its premise is invalidated by the truncate deletions).
7. Remove the `time.Sleep(50 * time.Millisecond)` tie-prevention hack at `test/integration/privacy/privacy_test.go:141`.

See implementation plan Phase 1 (Tasks 2 through 15) for the full step list.

## References

- Phase 7 spec: `docs/superpowers/specs/2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md` (INV-P7-16, the superseded invariant)
- ns-timestamps spec: `docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md` §5 (INV-TS-4 / INV-TS-5)
- AAD encoding: `internal/eventbus/crypto/aad/aad.go:62-117`
- Publisher truncate site (Phase 1 deletion target): `internal/eventbus/publisher.go:202`
- AAD reconstruction test (Phase 1 strengthening target): `internal/eventbus/history/plugin_aad_reconstruction_test.go`
- Floor truncate site (Phase 1 deletion target): `internal/grpc/server.go:1100`
- Sleep tie-prevention hack (Phase 1 removal target): `test/integration/privacy/privacy_test.go:141`
