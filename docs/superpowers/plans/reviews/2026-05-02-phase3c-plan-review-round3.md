<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Phase 3c Plan Review — Round 3

**Plan:** `docs/superpowers/plans/2026-05-02-event-payload-crypto-phase3c-cache-invalidation.md`
**Spec:** `docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3c-cache-invalidation.md`
**Round:** 3 (focused re-verification of round-2 NOT READY)
**Reviewer:** plan-reviewer subagent
**Date:** 2026-05-02

## Summary

Round-3 verifies the four declared edits responding to round-2's blocking
finding (INV-55 + INV-59 binding gap) and two non-blocking residuals. INV-59
is now annotated on its own line and binds via the meta-test regex. INV-55
has a new unit test `TestPillReceivedOnPoisonSubjectInvokesPillTrigger` in
`internal/cluster/probe_pill_test.go` annotated `// Verifies: INV-55`. T5's
Modify-list line numbers were updated to `manager.go:144, 237` (matching
the file-structure table and repo state). T9's dead-code block was left
intentionally unchanged per round-2 disposition.

## Per-finding verdict

### Round-2 BLOCKING — INV-55 + INV-59 lack `// Verifies: INV-N` annotations

**INV-59 binding (plan line 5091-5093):**

```text
// Verifies: INV-59
// Verifies: INV-12 (read-immediacy substrate)
func TestParticipantsChangedPropagatesViaInvalidation(t *testing.T) {
```

Regex `//\s*Verifies:\s*INV-(\d+)` with `FindAllSubmatch` captures both `59`
and `12` from these two lines. Verified by simulating the regex against the
actual annotation strings — produces `59` and `12` as separate captures.
INV-59 binding present and on its own line as claimed.

**INV-55 binding (plan lines 2483-2531):**

New test `TestPillReceivedOnPoisonSubjectInvokesPillTrigger` exists in
`internal/cluster/probe_pill_test.go` (created by T4) with `// Verifies:
INV-55` annotation at line 2483 directly preceding the test function. The
test:

- Constructs a `PoisonPayload` with `Reason = PillReasonMissedInvalidationAck`
  and a synthetic source `MemberID("01HSOURCE_INV55")`.
- Marshals via `cluster.MarshalPoison`.
- Publishes on `cluster.SubjectPoison("test-game", h.Members[0].MemberID)`.
- Reads `h.Members[0].PillEvents` channel and asserts both `Reason` and
  `SourceID` match.

This exercises the wire path (NATS publish → Registry's `handlePoison`
subscriber → `Pill.Trigger`) with a `TestPill` substitute, satisfying the
observable-behavior side of INV-55. The grounding doc Decision 7 deferral
to a subprocess harness for `ProductionPill os.Exit(125)` is preserved at
T15.X-class follow-up scope; the meta-test does not require process-exit
verification, only `// Verifies: INV-55` binding.

**Harness dependencies — all defined within the plan:**

| Symbol | Defined at plan line |
|---|---|
| `clustertest.New(t, clusterID, n)` | 1689-1698 (T1 step 6) |
| `cluster.PoisonPayload` | 867-875 |
| `cluster.MarshalPoison` | 977-984 |
| `cluster.SubjectPoison` | 909-913 |
| `h.Members[i].PillEvents` | 1721-1726 (HarnessMember struct) |
| `cluster.MemberID` | 208-214 |
| `cluster.PillReasonMissedInvalidationAck` | 284 |
| `h.Embedded.Conn` | 1714-1716 (Harness.Embedded *eventbustest.Embedded) |

**T14 meta-test coverage of INV-53..60 after round-3 edits:**

| INV | Annotation location | Captured by regex |
|---|---|---|
| INV-53 | T12 (line 4877) | yes → `53` |
| INV-54 | T12 (line 4943) | yes → `54` |
| INV-55 | T4 (line 2483) | yes → `55` (new in round-3) |
| INV-56 | T12 (line 5127) | yes → `56` |
| INV-57 | T12 (line 4959) | yes → `57` |
| INV-58 | lint rule existence | yes → `phase3cLintRuleExists()` stat-check |
| INV-59 | T12 (line 5091) | yes → `59` (split in round-3) |
| INV-60 | T12 (line 4982) | yes → `60` |

Verdict: **CLOSED.**

### Round-2 NB#1 — T5 line-number drift (`manager.go:138, 200` vs file-structure `144, 237`)

**Modify-list at plan line 2629:**

```text
- Modify: `internal/eventbus/crypto/dek/manager.go:144, 237` (callsite updates; executors `rg "m.cache.Put"` to confirm)
```

Updated to `144, 237` as claimed. Matches file-structure table (line 46)
and repo reality (`rg -n m.cache.Put internal/eventbus/crypto/dek/manager.go`
returns `144` and `237`).

**Residual drift INSIDE T5 step bodies:**

The Modify-list was updated, but the in-step prose still references the
OLD line numbers:

- Line 2886: "Edit `internal/eventbus/crypto/dek/manager.go` lines 138, 200"
- Line 2891: "Before (manager.go:137-138):"
- Line 2905: "Before (manager.go:198-200):"

This is the same defect class round-2 noted (NB#2 in round-1 review): a
fix applied to the Modify-list but not propagated through the in-step
references. However, plan line 73 contains a "Line-number disclaimer"
that explicitly tells executors line numbers are convenience and to `rg`
at execution time. With the disclaimer in place, the inconsistency does
not block execution — executors will grep `m.cache.Put` and find both
callsites at the actual current lines.

Verdict: **PARTIALLY CLOSED.** The user's specific claim ("T5's Modify-list
at plan line ~2629 was changed from `manager.go:138, 200` to `manager.go:144,
237`") is verified true. The broader drift remains in the step body but is
mitigated by the existing line-number disclaimer. **Non-blocking.**

### Round-2 NB#2 — T9 dead-code block

Plan lines 4992-4995 (the `(INV-55 is exercised under e2e with
ProductionPill in a subprocess harness; deferred to a follow-up if not
feasible in T12 timeline. The TestPill substitute in
cluster/probe_pill_test.go covers the observable behavior on the test
side.)` block) intentionally left unchanged per round-2 disposition.

Verdict: **CLOSED (no change required).**

## Regressions introduced by round-3 edits

None detected. Searched for collateral effects:

- INV-29 binding annotations at lines 5042 and 5078 still present and bind
  to their tests.
- Other INV-N annotations (53, 54, 56, 57, 60) at T12 lines 4877, 4943,
  5127, 4959, 4982 untouched.
- T1 step 6 harness definition (line 1689) and T4 step 4 harness extension
  (line 2534-2562) consistent — `PublishSyntheticHeartbeat` and
  `AwaitMemberPresent` referenced by both `TestPillRateLimitBlocks...` and
  `TestPillReceivedOnPoisonSubject...` exist in the same place.

## Verification evidence

- Read plan: `docs/superpowers/plans/2026-05-02-event-payload-crypto-phase3c-cache-invalidation.md` (5693 lines).
- Read sections: 5020-5099 (INV-29/INV-59 annotations), 2360-2562 (T4 INV-55 test + harness extension), 2620-2918 (T5 callsite updates), 4980-5018 (T12 dead-code residual), 5320-5468 (T14 meta-test).
- Searched: `rg -n "Verifies:\s*INV-" docs/.../plan.md` — captured all 14 annotation lines.
- Searched: `rg -n "manager\.go:" docs/.../plan.md` — confirmed Modify-list updated, in-step prose still on old numbers (mitigated by disclaimer at line 73).
- Searched: `rg -n "PoisonPayload|MarshalPoison|SubjectPoison|MemberID|PillEvents|PillReasonMissedInvalidationAck|h\.Embedded\.Conn|clustertest\.New"` — confirmed all harness deps for the new INV-55 test are defined within the plan (T1, T2, T4 step 6).
- Repo-reality check: `rg -n "m\.cache\.Put" internal/eventbus/crypto/dek/manager.go` — confirmed lines 144 and 237 (matching the updated Modify-list).
- Regex simulation: piped each captured `// Verifies: INV-N` line through `rg -o '//\s*Verifies:\s*INV-(\d+)' -r '$1'` — produced 55, 59, 12, 29, 29, 56, 53, 54, 57, 60 (all distinct INV numbers including 55 and 59 are captured).

## Verdict

- [x] **READY** — round-2 blocking finding (INV-55 + INV-59 binding gap) is
      closed. INV-59 split annotation captures via `FindAllSubmatch`. INV-55
      has a new unit test bound via `// Verifies: INV-55` and all harness
      dependencies are defined within the plan. INV-53..60 each have a meta-
      test binding (INV-58 via lint rule file-existence). T5 Modify-list line
      numbers updated to match repo reality. T9 dead-code block unchanged
      per round-2 disposition. Non-blocking residual: line-number drift
      inside T5 step body remains but is mitigated by the existing line-
      number disclaimer at plan line 73.
- [ ] NOT READY

Execution may proceed.

## Persisted report

Repo-relative path: `docs/superpowers/plans/reviews/2026-05-02-phase3c-plan-review-round3.md`
