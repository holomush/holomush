<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase 3c Implementation Plan — Adversarial Plan Review (Round 2)

**Plan reviewed:** `docs/superpowers/plans/2026-05-02-event-payload-crypto-phase3c-cache-invalidation.md`
**Spec:** `docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3c-grounding.md`
**Round-1 review:** `docs/superpowers/plans/reviews/2026-05-02-phase3c-plan-review.md`
**Date:** 2026-05-02
**Reviewer:** plan-reviewer agent (round 2 — focused re-review)

## Summary

Round-2 re-review verifies the three round-1 findings against the current
plan text and runs a regression sweep. Findings 1, 2, 3 are CLOSED. The
regression sweep surfaces ONE new blocking issue: the T14 meta-test will
fail on first execution because INV-55 and INV-59 lack the `// Verifies:
INV-N` annotation form that the meta-test regex captures. Both invariants
are mentioned in test files, but only via prose — never as a leading
`// Verifies: INV-55` or `// Verifies: INV-59` comment that the regex
`//\s*Verifies:\s*INV-(\d+)` would capture.

## Per-finding verification

### Finding 1 — INV-53 enforcement gap → CLOSED

**Round-2 evidence (verified in plan):**

- T1 metrics struct: `DuplicateMemberIDMetrics` with constructor at plan
  lines 633–648 (`internal/cluster/metrics.go` section). Counter is
  `cluster_duplicate_member_id_total` with label `member_id` (bounded
  cardinality — one entry per ULID ever observed in cluster).
- T2 Deps struct: plan line 1130 — `DuplicateMemberID *DuplicateMemberIDMetrics`.
- T2 `handleAlive` detection logic: plan lines 1507–1527.
  - Reads under `r.mu.Lock()` (line 1507).
  - Guard: `if present && !existing.StartedAt.IsZero() && !p.StartedAt.Equal(existing.StartedAt)` (line 1516).
  - Releases lock with `r.mu.Unlock()` (line 1517).
  - Logs `slog.Error("CLUSTER_MEMBER_DUPLICATE_ID; rejecting duplicate heartbeat", ...)` (lines 1518–1522).
  - Increments metric with nil guard: `if r.deps.DuplicateMemberID != nil { ... }` (lines 1523–1525).
  - Early return on line 1526 — no registry update; first-seen identity preserved.
- T12 test at plan lines 4827–4891
  (`TestRegistryRejectsDuplicateMemberIDFromDifferentStartedAt`):
  - Publishes heartbeat#1 with `StartedAt = t1`, `HolomushVersion = "first"`
    on `cluster.SubjectAlive("test-game", target)` (lines 4836–4845).
  - Awaits via `h.AwaitMemberPresent` (line 4846).
  - Samples initial state and asserts first-seen StartedAt = t1 (lines 4849–4855).
  - Publishes heartbeat#2 with `StartedAt = t1.Add(10s)`,
    `HolomushVersion = "duplicate"` (lines 4860–4870).
  - Asserts post-duplicate `after.StartedAt.Equal(t1)` (line 4879) AND
    `after.HolomushVersion == "first"` (line 4882) — proving the duplicate
    was rejected and first-seen identity preserved.

**Verdict:** Detection logic actually rejects the duplicate (returns early
before updating the registry), the test exercises the rejection path
(asserts StartedAt + HolomushVersion are NOT updated to the duplicate's
values), and metric label cardinality is bounded to `member_id` only.
INV-53 enforcement is wired end-to-end. **CLOSED.**

### Finding 2 — Line-number drift → CLOSED (with one residual citation drift)

**Round-2 evidence:**

- File structure table at plan line 46 reads: `internal/eventbus/crypto/dek/manager.go:144, 237`
- Disclaimer at plan lines 73 reads:
  > "Line numbers are indicative; executors MUST `rg` for the actual
  > current line at execution time and adjust the edit target accordingly."
- Disk verification:

  ```text
  $ rg -n "m.cache.Put" internal/eventbus/crypto/dek/manager.go
  144: m.cache.Put(CacheKey{KeyID: keyID, Version: 1}, material)
  237: m.cache.Put(CacheKey{KeyID: keyID, Version: r.Version}, material)
  ```

  Plan and disk agree.

**Residual issue:** Plan line 2579 (T5 "Modify" listing) still reads
`internal/eventbus/crypto/dek/manager.go:138, 200` — the same stale
citation that round-1 flagged. This is internally inconsistent with
the file-structure table (line 46), but the disclaimer at line 73
explicitly tells executors to rg for actual lines. Non-blocking on
plan acceptance; a tidy-up.

**Verdict:** CLOSED. Round-1's required resolution ("update T5 file-paths-with-line-numbers
to `manager.go:144, 237`" + add disclaimer) is functionally addressed —
the disclaimer is the load-bearing fix; the T5 line-number residual is
covered by the disclaimer's instruction.

### Finding 3 — T9 staged-TDD test always passed → CLOSED

**Round-2 evidence:** Plan lines 4318–4327
(`TestCoordinatorRequestInvalidationSucceedsWhenSelfIsOnlyMember` step 4):

```go
// Staged TDD: this test is SKIPPED until T10 wires the receive side.
// T10's commit removes this skip and asserts require.NoError(t, err)
// (single-member self-ack succeeds via NATS loopback).
if err != nil {
    t.Skipf("T10 receive-side not yet wired; got err = %v. Remove this skip in T10's commit and assert require.NoError.", err)
}
// Once T10 lands, this assertion is the active check:
if err != nil {
    t.Errorf("RequestInvalidation single-member returned %v; want nil (self-ack via NATS loopback)", err)
}
```

The first `if err != nil` skips before T10 lands; once T10 wires
receive-side, err is nil and skip is bypassed. The second `if err != nil`
block is currently dead code (skip returns) — round-1 review explicitly
acknowledged this as "acceptable as documentation of the post-T10
strictness, but worth flagging if the duplication is awkward."

**Verdict:** CLOSED. The test now meaningfully fails (via skip) before
T10 lands. Stylistic note: the dead-code second `if err != nil` is
documentary — T10 step 1 is supposed to remove the skip and tighten the
assertion to `require.NoError`. Acceptable.

## Regression sweep

### Sweep 1 — `DuplicateMemberID` reference consistency

`rg -n "DuplicateMemberID" docs/superpowers/plans/2026-05-02-event-payload-crypto-phase3c-cache-invalidation.md`
returns 11 hits: type definition (T1, line 633), constructor (T1, line 639),
counter field assignment (T1, lines 641–644), Deps struct field (T2, line
1130), handleAlive use (T2, lines 1523–1524), test file
(T12, line 4828), test commentary (line 4887). All references consistent.
The metric is gracefully nil-tolerant in handleAlive (`if r.deps.DuplicateMemberID != nil`)
so the harness need not wire it for the T12 test to assert via the
structured-log signal. **No regression.**

### Sweep 2 — INV-53 test grounded in existing harness helpers

T12's `TestRegistryRejectsDuplicateMemberIDFromDifferentStartedAt` calls:

- `clustertest.New(t, "test-game", 1)` — defined in T2, plan line ~1734.
- `h.AwaitConverged(t, ...)` — defined in T2, plan line 1777.
- `h.Embedded.Conn.Publish(...)` — `Embedded` field is `*eventbustest.Embedded`
  defined in T2 harness at plan line 1715. The disk-side
  `eventbustest.Embedded` type does have a `Conn *nats.Conn` field
  (verified at `internal/eventbus/eventbustest/embedded.go:48–51`).
- `cluster.SubjectAlive("test-game", target)` — defined in T2, plan
  line 877.
- `cluster.MarshalHeartbeat(p)` — defined in T2 codec, used in
  `r.publishHeartbeat` at plan line 1481.
- `h.AwaitMemberPresent(t, 0, target, ...)` — defined in T4, plan
  line 2517.

All harness dependencies for the T12 INV-53 test exist in tasks ≤ T12.
**No regression.**

### Sweep 3 — INV-58 / INV-59 / INV-60 still bound

- INV-58: lint rule binding via `gorules/no_remote_clock_compare.go`
  (T11, plan line 62). Meta-test special-cases this: plan line 5381
  `found[58] = phase3cLintRuleExists(repoRoot)`. **Intact.**
- INV-60: `// Verifies: INV-60` at plan line 4932
  (`TestProbeAndPillRefusesSelfTarget`). **Intact.**
- INV-59: prose-only mentions; the `Verifies:` comment in
  `TestParticipantsChangedPropagatesViaInvalidation` at plan line 5041
  reads `// Verifies: INV-12 (read-immediacy substrate via INV-59)` —
  the regex `//\s*Verifies:\s*INV-(\d+)` captures only `12`. See
  Blocking Finding 4 below.

### Sweep 4 — INV-55 binding

`rg "Verifies: INV-55"` returns zero hits. The integration-test stub at
plan lines 4942–4945 explicitly defers INV-55 to a follow-up:

> `// (INV-55 is exercised under e2e with ProductionPill in a subprocess`
> `// harness; deferred to a follow-up if not feasible in T12 timeline. The`
> `// TestPill substitute in cluster/probe_pill_test.go covers the`
> `// observable behavior on the test side.)`

Yet `phase3cInvariants = []int{53, 54, 55, 56, 57, 58, 59, 60}` (plan
line 5313) still requires INV-55 to be bound. See Blocking Finding 4.

## Blocking findings

### 1. [Severity: High] INV-55 and INV-59 lack `// Verifies: INV-N` bindings; T14 meta-test will fail on first run

- **Location:** Plan T14 step 3 (line 5400 expected output: PASS); offending
  test files referenced by name in T12 (lines ~4940 and ~5041).
- **Evidence:**
  - Meta-test definition (plan lines 5313, 5315):

    ```go
    var phase3cInvariants = []int{53, 54, 55, 56, 57, 58, 59, 60}
    var verifiesRE = regexp.MustCompile(`//\s*Verifies:\s*INV-(\d+)`)
    ```

  - INV-58 has special-case lint-rule binding (plan line 5381).
  - INV-55 has zero `// Verifies: INV-55` annotations in the entire
    plan (verified via `rg "Verifies: INV-55"`).
  - INV-59 only appears as `// Verifies: INV-12 (read-immediacy
    substrate via INV-59)` at plan line 5041. The regex captures
    only `12`; the trailing `INV-59` is not preceded by `Verifies:`
    so does not match.
  - Plan T11/T12 text at lines 4942–4945 explicitly defers INV-55 to
    a follow-up bead, while keeping `55` in `phase3cInvariants`.
- **Issue:** When T14 runs, the meta-test reports
  `Phase 3c invariants without test binding: [55 59]` and `t.Fatalf`s.
  T14 step 3 expected output ("PASS") cannot be achieved with the plan
  as written.
- **Why it blocks:** T14 is the meta-test that closes the spec's
  invariant-binding loop (see grounding doc §"Invariant test binding").
  An executor running this plan top-to-bottom hits a failing required
  test at T14, with no remediation path documented. Spec INV-binding
  guarantee is not met.
- **Required resolution:**
  1. **INV-59 fix (cheap):** change plan line 5041 from
     `// Verifies: INV-12 (read-immediacy substrate via INV-59)` to two
     lines:

     ```go
     // Verifies: INV-59
     // Verifies: INV-12 (read-immediacy substrate)
     ```

     The regex's `FindAllSubmatch` captures both.
  2. **INV-55 fix (one of):**
     - **Option A (preferred):** add a `// Verifies: INV-55`-annotated unit
       test in `internal/cluster/probe_pill_test.go` that asserts the
       observable behavior the deferred-comment promises (TestPill's
       `Trigger` is invoked when a poison message arrives on
       `internal.<cluster_id>.member.poison.<self_id>`). The integration
       harness already supports publishing arbitrary subjects via
       `h.Embedded.Conn.Publish`.
     - **Option B:** drop INV-55 from `phase3cInvariants` and add a
       follow-up bead to bind it once a subprocess harness exists.
       Document the temporary hole in the master spec edit (T13).
       This option weakens the spec's binding guarantee — only do this
       with explicit user approval.

## Non-blocking findings

### 1. [Severity: Low] T5 Modify-list residual citation drift

- **Location:** Plan line 2579: `Modify: internal/eventbus/crypto/dek/manager.go:138, 200`
- **Issue:** The file-structure table at line 46 says `manager.go:144, 237`;
  the T5 Modify listing still says `138, 200`. The line-number disclaimer
  at line 73 covers this in practice (executors `rg`), but the
  inconsistency between two parts of the plan is sloppy and re-introduces
  the round-1 finding's surface form.
- **Required resolution (recommended):** change line 2579's
  `manager.go:138, 200` to `manager.go:144, 237` for consistency with
  line 46.

### 2. [Severity: Low] T9 staged-TDD dead-code block

- **Location:** Plan lines 4324–4327.
- **Issue:** Second `if err != nil { t.Errorf(...) }` is unreachable
  because the preceding `if err != nil { t.Skipf(...) }` returns. The
  block is documentary — round-1 reviewer accepted it as such.
- **Required resolution (optional):** replace lines 4324–4327 with a
  single comment:

  ```go
  // Once T10 lands, the skip is removed and replaced with:
  //   require.NoError(t, err) // single-member self-ack succeeds via NATS loopback
  ```

  This eliminates dead code while preserving the documentary intent.

## Verification evidence

- **Read:**
  - `docs/superpowers/plans/2026-05-02-event-payload-crypto-phase3c-cache-invalidation.md` (full file, 5642 lines)
  - `docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3c-grounding.md` (header + invariant table)
  - `docs/superpowers/plans/reviews/2026-05-02-phase3c-plan-review.md` (round-1 review)
  - `internal/eventbus/crypto/dek/manager.go` (lines 144, 237 confirmed)
  - `internal/eventbus/eventbustest/embedded.go` (Conn field confirmed)
- **Searched:**
  - `rg -n "DuplicateMemberID|CLUSTER_MEMBER_DUPLICATE_ID|cluster_duplicate_member_id|TestRegistryRejectsDuplicate|INV-53"` against plan
  - `rg -n "manager\.go:144|manager\.go:200|manager\.go:138|manager\.go:237"` against plan
  - `rg -n "m\.cache\.Put|cache\.Put"` against `internal/eventbus/crypto/dek/manager.go`
  - `rg -n "AwaitMemberPresent|AwaitConverged|PublishSyntheticHeartbeat|SubjectAlive|Embedded\b"` against plan
  - `rg -n "Verifies: INV-5[3-9]|Verifies: INV-60|Verifies: INV-55|Verifies: INV-59"` against plan
  - `rg -n "phase3cInvariants"` against plan
- **Round-1 finding traceability:**
  - Finding 1 (INV-53 enforcement) → CLOSED via T1 metrics struct, T2 Deps + handleAlive logic, T12 test rewrite.
  - Finding 2 (line-number drift) → CLOSED via file-structure table update + disclaimer (T5 Modify-list residual is non-blocking).
  - Finding 3 (T9 always passes) → CLOSED via skip-then-strict pattern.
- **Regression sweep results:**
  - Sweeps 1, 2, 3 (INV-58/60 binding, harness wiring, DuplicateMemberID consistency) → no regression.
  - Sweep 4 (INV-55/59 binding) → blocking regression-adjacent finding (gap may have pre-existed round 1; surfaces blocking on T14 meta-test execution regardless).

## Verdict

- [ ] READY
- [x] **NOT READY** — Blocking Finding 1 (INV-55 / INV-59 missing `// Verifies: INV-N` annotations) breaks T14 meta-test. Round-3 plan edit needed:
  - Change plan line 5041 to add a `// Verifies: INV-59` line.
  - Add a `// Verifies: INV-55`-annotated unit test in T11
    (`internal/cluster/probe_pill_test.go`) **OR** drop 55 from
    `phase3cInvariants` with documented follow-up bead and explicit
    user approval.

After Round-3 closes Blocking Finding 1, the plan is ready for
`superpowers:subagent-driven-development`. The two non-blocking findings
are tidy-ups; their resolution is not required for execution but is
recommended.
