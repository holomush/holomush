<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase 3c Implementation Plan — Adversarial Plan Review (Round 1)

**Plan reviewed:** `docs/superpowers/plans/2026-05-02-event-payload-crypto-phase3c-cache-invalidation.md`
**Spec:** `docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3c-grounding.md` (READY per design-reviewer round 2)
**Date:** 2026-05-02
**Reviewer:** plan-reviewer agent (partial output captured below; agent cut off before persisting its own report)

## Note on this report

The plan-reviewer adversarial agent produced a partial review and cut off before persisting its formal report at the canonical path. This document captures the findings emitted in the partial output, along with the orchestrator's verification of each, so the round-2 plan edits have a documented basis.

## Findings emitted by plan-reviewer

### Finding 1 [Severity: High] — INV-53 enforcement gap

**Location:** Plan T12 line ~4801 (`TestRegistryRejectsDuplicateMemberID`); spec INV-53 in §New invariants.

**Spec INV-53:** "Every member of a `cluster.Registry` MUST have a unique `MemberID`; concurrent registration with a colliding MemberID MUST be rejected with `CLUSTER_MEMBER_DUPLICATE_ID`."

**Plan T12 test body says (verbatim):**

> "INV-53: duplicate construction with same SelfIDForTest is allowed by NewSubsystem (it doesn't know about peers); the registry rejects the duplicate at heartbeat-receive time. Verify the duplicate's heartbeat doesn't double-count in LiveMembers... For Phase 3c we ship the documentary statement of INV-53; if a future deployment wires a leader-coordinated UUID issuance, the test extends to assert the constructor rejection."

**Issue:** The test is a "non-test test" — it explicitly defers actual rejection to a "future deployment." The plan never adds:

1. A `CLUSTER_MEMBER_DUPLICATE_ID` typed error code anywhere in `internal/cluster/`.
2. Logic in `handleAlive` (`heartbeat.go`) to detect a colliding MemberID with mismatched `StartedAt` (indicating a different process re-using the ULID) and reject the duplicate.
3. A test that asserts the rejection actually fires.

**Verdict:** Blocker for round-1 plan acceptance.

**Round-2 plan edits required:**

- T2 (heartbeat.go): on `handleAlive` receive, if `existing.StartedAt != p.StartedAt && !existing.StartedAt.IsZero()`, emit a structured WARN + increment `cluster_duplicate_member_id_total` Prometheus counter, AND skip the registry update (preserve first-seen identity). The duplicate's heartbeat is "rejected" at the receive boundary.
- T2 metrics: add `DuplicateMemberIDTotal *prometheus.CounterVec` to `metrics.go`.
- T12 cluster_test.go INV-53 test: rewrite to publish two heartbeats with the same MemberID but different StartedAt timestamps, then assert the metric incremented and the second heartbeat did NOT update the registry's view of `StartedAt`.

### Finding 2 [Severity: Low] — Line-number drift in plan

**Location:** Plan T5 references `manager.go:138, 200`; actual lines (verified via `rg -n "m.cache.Put" internal/eventbus/crypto/dek/manager.go`) are 144 and 237.

**Issue:** Phase 3b's manager.go additions (the `Participants` stub, the new `partCache` field if landed in 3b, etc.) shifted the existing cache.Put callsites. Off-by-6 and off-by-37.

**Verdict:** Non-blocking — plan executors typically grep for the right line at execution time rather than trusting line numbers. But fixing where cheap is good hygiene.

**Round-2 plan edits required (informational):**

- Update T5 file-paths-with-line-numbers to `manager.go:144, 237`.
- Add a generic note near the file structure table: "Line numbers are indicative; executors MUST `rg` for the actual current line at execution time."

### Finding 3 [Severity: Low] — T9 staged-TDD test always passes

**Location:** Plan T9 `TestCoordinatorRequestInvalidationSucceedsWhenSelfIsOnlyMember` step 4.

**Verbatim:**

```go
if err != nil && !errors.Is(err, invalidation.ErrSelfTimeout) {
    t.Errorf("err = %v; want nil (after T10) or ErrSelfTimeout (before T10)", err)
}
```

**Issue:** Test passes regardless of whether T10 lands. That's not a useful staged TDD — it just always passes.

**Verdict:** Non-blocking but worth tightening so that the test fails meaningfully before T10 lands and asserts nil after.

**Round-2 plan edits required (recommended):**

- Annotate the test with a `t.Skip()` until T10 commit lands; remove the skip in T10 step 1 with a more strict assertion (`require.NoError(t, err)`).

## Findings the orchestrator verified independently

The reviewer's partial output mentioned reviewing T11 ruleguard fixture pattern, T13 master spec edit fidelity, and T14 meta-test substantiveness, but cut off before producing a verdict on those. Spot-checks during round-2 plan editing did not surface additional blocking issues:

- T11 ruleguard fixture pattern matches what the existing `gorules/dek_no_serialize.go` rule does for INV-27.
- T13 enumerates each of the spec's master spec edits.
- T14 walks `_test.go` files for the `// Verifies: INV-N` annotation; all of INV-53..60 (including INV-58 via lint-rule existence) are bound by the time T11+T12 land.

## Verdict

- [ ] READY
- [x] **NOT READY** — Finding 1 (INV-53 enforcement gap) is blocking. Findings 2 + 3 are non-blocking but recommended.

After round-2 edits address Finding 1, the plan is good for `superpowers:subagent-driven-development` or `superpowers:executing-plans`.
