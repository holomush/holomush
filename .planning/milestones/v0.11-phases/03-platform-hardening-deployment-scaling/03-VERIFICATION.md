---
phase: 03-platform-hardening-deployment-scaling
verified: 2026-07-10T19:05:00Z
status: passed
score: 5/5 requirements verified (33/33 plan truths verified)
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: none
  previous_score: n/a
---

# Phase 3: Platform Hardening & Deployment Scaling — Verification Report

**Phase Goal:** HoloMUSH can be deployed as a horizontally-scaled, multi-node cluster with a durable audit pipeline, closing the single-node ceiling flagged in `.planning/codebase/CONCERNS.md`. Operator-facing hardening — NO player-visible features.
**Verified:** 2026-07-10T19:05:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

Goal-backward result: a real operator, given this branch, can (1) deploy the event bus against external NATS; (2) enforce single-principal scoping proven from both sides; (3) trust multi-node crypto invalidation over per-replica connections; (4) rely on a never-drop durable audit DLQ with a replay CLI; (5) follow a complete external-NATS runbook. Every CLUSTER-NN requirement is backed by substantive, wired, and (where behavior-dependent) genuinely-asserted integration-tagged code — not stubs or SUMMARY claims.

### Observable Truths (by requirement)

| # | Requirement / Truth | Status | Evidence |
|---|---------------------|--------|----------|
| 1 | **CLUSTER-01** `eventbus.Config` external mode + `Validate()` fail-closed | ✓ VERIFIED | `internal/eventbus/config.go:21` (ModeExternal), `:69-82` (URL/Credentials/TLS/DLQ keys), `:172-179` (Validate rejects external+empty-URL, `EVENTBUS_CONFIG_INVALID`), `:149-166` (Defaults → embedded default) |
| 2 | CLUSTER-01 `dialExternal`/`connectExternal` + fail-closed boot, no embedded fallback | ✓ VERIFIED | `internal/eventbus/natsdial.go:39-61` (creds+TLS dial, `EVENTBUS_EXTERNAL_CONNECT_FAILED`), `internal/eventbus/subsystem.go:133-135` (mode switch), `:208-219` (connectExternal, s.server stays nil), `:225-227` (exporter embedded-only), `:260` (provision opt-out verify-or-fail) |
| 3 | CLUSTER-01 Validate wired at boot | ✓ VERIFIED | `cmd/holomush/core.go:136-141` (Load event_bus → Defaults → Validate → refuse) |
| 4 | **CLUSTER-02** `deploy/nats` templates grant events.>/audit.>/internal.>/_INBOX.> only | ✓ VERIFIED | `deploy/nats/holomush-server.account.conf:42,45` (exact allow-list), `:56-67` (non-server negative fixture) |
| 5 | CLUSTER-02 `verify-scoping.sh` denies non-server pub+sub on all three prefixes | ✓ VERIFIED | `deploy/nats/verify-scoping.sh:90-124` (assert_publish/subscribe_denied over events/audit/internal), exit-code decided |
| 6 | CLUSTER-02 `VerifyAccountScoping` wired at boot (external only), over-scope → refuse | ✓ VERIFIED | `internal/eventbus/scopecheck.go:46-118` (fail-closed self-check), `cmd/holomush/core.go:474-479` (external-mode gate → `EVENTBUS_SCOPE_CHECK_FAILED`) |
| 7 | CLUSTER-02 Case A/B integration proof (both halves, no runbook fallback) | ✓ VERIFIED | `test/integration/eventbus_external/scopecheck_test.go:127-171` — Case A over-scoped refused; Case B (HARD) loads SHIPPED account.conf, server cred PASSES, non-server DENIED on all three prefixes |
| 8 | **CLUSTER-03** per-replica conns in invalidation test (closes shared-conn gap) | ✓ VERIFIED | `test/integration/crypto/cache_invalidation_test.go:4` (integration tag), `:43` (`h.Members[i].Conn` per-replica), `internal/cluster/clustertest/external.go:24-53` (ExternalMember own conn, NewExternal) |
| 9 | CLUSTER-03 N-of-N acks + cache eviction + cluster_id filtering | ✓ VERIFIED | cache_invalidation_test.go:84-107 (INV-CLUSTER-1 3-replica N-of-N), :110-198 (INV-CLUSTER-2/9 eviction), cluster_test.go:92-105,171-197 (INV-CLUSTER-4 filtering) |
| 10 | CLUSTER-03 hung-replica probe-pill, N-1 completion (D-08) | ✓ VERIFIED | cache_invalidation_test.go:204-241 (closes member 2 conn → probe-pill → rekey completes, LiveCount→2) |
| 11 | CLUSTER-03 INV-CLUSTER-1/2/4/9 bound, INV-CLUSTER-8 pending w/ #4777, no fabricated bindings | ✓ VERIFIED | `docs/architecture/invariants.yaml` (1/2/4/9 bound w/ genuine asserted_by; 8 pending), issue #4777 OPEN, meta-test `TestBoundInvariantsAreGenuinelyAsserted` + 16 others PASS |
| 12 | CLUSTER-03 INV-EVENTBUS-29/30 registered + bound | ✓ VERIFIED | invariants.yaml:2620,2628 bound to external_boot_test.go / dlq_capture+neverdrop tests; `// Verifies:` present on real test funcs |
| 13 | **CLUSTER-04** DLQ never-drop: Capture→Term, Nak on DLQ-publish-fail | ✓ VERIFIED | `internal/eventbus/audit/projection.go:255-274` (NumDelivered≥MaxDeliver → Capture → Term; Capture err → Nak), `dlq.go:148-162` (counter only on success) |
| 14 | CLUSTER-04 bounded DLQ retention + Prometheus counter | ✓ VERIFIED | `dlq.go:116-138` (EnsureStream MaxAge/MaxBytes -1 sentinel), `holomush_audit_dlq_messages_total` via DLQMessagesTotal; wired `core.go:554-558` |
| 15 | CLUSTER-04 `holomush audit dlq {list,show,replay}` CLI, idempotent replay | ✓ VERIFIED | `cmd/holomush/cmd_audit.go:32-57` (group), `:313` (ReplayDLQ reuses persist path), registered `cmd/holomush/root.go:44` |
| 16 | **CLUSTER-05** full-lifecycle runbook (7 steps) + cutover data stance | ✓ VERIFIED | `site/.../external-nats-deployment.md` Steps 1–7 (provision→creds→configure→cutover→verify-scoping→DLQ→rollback), lines 19,155-171 explicit fresh-EVENTS + durable-Postgres stance |
| 17 | CLUSTER-05 references real tool names; deferred options noted-not-implemented | ✓ VERIFIED | runbook cites verify-scoping.sh, `holomush audit dlq {list,show,replay}`, compose.cluster.yaml; :283-295 sandbox + read-only-operator noted as follow-ups |

**Score:** 5/5 requirements verified (all 33 plan-frontmatter truths across 9 plans verified; 0 present-behavior-unverified)

### Required Artifacts

| Artifact | Status | Details |
|----------|--------|---------|
| `internal/eventbus/config.go` (+_test) | ✓ VERIFIED | ModeExternal, TLSConfig/DLQConfig, Validate, Defaults; reconciled to ModeExternal/URL/Credentials (no ModeCluster leftovers) |
| `internal/eventbus/natsdial.go` | ✓ VERIFIED | Dial/dialExternal, creds+TLS, coded fail-closed error |
| `internal/eventbus/scopecheck.go` (+_test) | ✓ VERIFIED | VerifyAccountScoping fail-closed over-scope self-check |
| `internal/testsupport/natstest/nats.go` | ✓ VERIFIED | GenericContainer nats:2-alpine (no go.mod module), per-caller conns; depguard-denied in prod (`.golangci.yaml:158`) |
| `internal/cluster/clustertest/external.go` | ✓ VERIFIED | ExternalHarness/ExternalMember with independent per-replica conns |
| `internal/eventbus/audit/dlq.go` | ✓ VERIFIED | dlqPublisher{js,cfg,counter}, EnsureStream, Capture never-increments-on-fail |
| `internal/eventbus/audit/replay.go` + `cmd/holomush/cmd_audit.go` | ✓ VERIFIED | ReplayDLQ + cobra dlq group |
| `deploy/nats/{account.conf,README.md,verify-scoping.sh,cluster-config.yaml}` | ✓ VERIFIED | account template, nsc walkthrough, denial script, external config fragment |
| `compose.cluster.yaml` + `scripts/smoke/cluster-smoke.sh` | ✓ VERIFIED | nats service + core2 `--skip-seed-migrations`, smoke asserts 2 members by count + EXIT-trap teardown |
| `test/integration/{crypto,cluster,eventbus_external}/*` | ✓ VERIFIED | all `//go:build integration`, genuine assertions |
| `docs/architecture/invariants.{yaml,md}` | ✓ VERIFIED | bindings + regenerated md (no drift via `go run ./cmd/inv-render`) |
| `site/.../external-nats-deployment.md` | ✓ VERIFIED | full 7-step runbook |

### Key Link Verification

| From | To | Via | Status |
|------|----|-----|--------|
| core.go event_bus load | Config.Validate → boot refuse | `core.go:136-141` | ✓ WIRED |
| config.Mode | connectExternal / connectEmbedded | `subsystem.go:133-135` | ✓ WIRED |
| external-mode boot | VerifyAccountScoping(conn) → refuse | `core.go:474-479` | ✓ WIRED |
| event_bus.dlq config | audit.Config.DLQ → dlqPublisher.EnsureStream | `core.go:554-558`, `projection.go:128` | ✓ WIRED |
| projection persist-error | NumDelivered gate → dlq.Capture → Term/Nak | `projection.go:255-274` | ✓ WIRED |
| NewRootCmd | AddCommand(NewAuditCmd()) → dlq replay | `root.go:44`, `cmd_audit.go:313` | ✓ WIRED |
| natstest URL | per-replica nats.Connect → Coordinator/Registry | `external.go`, `cache_invalidation_test.go:43` | ✓ WIRED |
| `// Verifies:` annotations | invariants.yaml asserted_by/binding | meta-test PASS | ✓ WIRED |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| No fabricated bindings / genuine assertions | `task test -- -run 'TestBoundInvariantsAreGenuinelyAsserted\|TestProvenanceGuard\|TestRegistryBindingChecks\|TestEveryRegistryInvariantHasBinding' ./test/meta/` | 17 tests PASS (9.3s) | ✓ PASS |
| invariants.md regenerated (no drift) | `go run ./cmd/inv-render` + `git diff --stat` | empty diff | ✓ PASS |
| Coverage issue for pending INV-CLUSTER-8 | `gh issue view 4777` | OPEN "coverage gap: INV-CLUSTER-8 unbound" | ✓ PASS |
| Multi-node rotation / DLQ end-to-end | `task test:int` (Docker required) | executed as the CI-required `Integration Test` check (green on PR #4782); not run in the local verifier pass | ✓ PASS (CI) |

### Requirements Coverage

| Requirement | Source Plans | Status | Evidence |
|-------------|--------------|--------|----------|
| CLUSTER-01 | 03-01, 03-03 | ✓ SATISFIED | config external mode + Validate + dialExternal + fail-closed boot |
| CLUSTER-02 | 03-06 | ✓ SATISFIED | deploy/nats + verify-scoping.sh + VerifyAccountScoping at boot + Case A/B |
| CLUSTER-03 | 03-02, 03-05, 03-08 | ✓ SATISFIED | per-replica conns + N-of-N + probe-pill + bindings + #4777 |
| CLUSTER-04 | 03-04, 03-07 | ✓ SATISFIED | never-drop DLQ + replay CLI + counter + bounded retention |
| CLUSTER-05 | 03-09 | ✓ SATISFIED | full-lifecycle runbook + cutover stance + real tool names |

All CLUSTER-01..05 map to Phase 3 in REQUIREMENTS.md:263-267; none orphaned.

### Context Decision Honored (spot-checks)

- **D-13 both halves:** external `verify-scoping.sh` AND boot-time `VerifyAccountScoping` both shipped and wired; Case B proves both sides in one CI-backed test. ✓
- **D-09 never-drop:** `projection.go:255-274` Capture-then-Term, Nak-on-DLQ-fail — nothing dropped. ✓
- **D-15 sandbox deferred-not-implemented:** runbook `:283-295` notes sandbox migration + read-only operator account as future follow-ups only; no live-migration step. ✓
- **D-07 no fabricated bindings:** INV-CLUSTER-8 left pending w/ #4777; meta-test `TestBoundInvariantsAreGenuinelyAsserted` passes. ✓

### Anti-Patterns Found

None blocking. No debt markers (TBD/FIXME/XXX) in phase-modified files; no stubs; no unwired artifacts.

### Notes (INFO — not gaps)

- Plan 03-04 / 03-07 frontmatter listed the DLQ tests at `test/integration/audit/dlq_capture_test.go` / `dlq_replay_test.go`; the actual tests live in-package at `internal/eventbus/audit/dlq_capture_integration_test.go`, `dlq_neverdrop_integration_test.go`, and `dlq_replay_integration_test.go` (all `//go:build integration`, genuinely asserting INV-EVENTBUS-30). Behavior fully verified; only the plan's stated path differs. No functional gap.

### Gaps Summary

No gaps. Every must-have across all 9 plans is real, substantive, wired, and — for behavior-dependent truths — backed by genuine integration-tagged assertions whose registry bindings are proven authentic by the passing unit-tier meta-tests. Requirement traceability is complete (CLUSTER-01..05, 0 orphaned). The only item not run in the local verifier pass is the Docker-gated `task test:int` suite, whose assertion bodies were read and confirmed genuine and whose invariant bindings the meta-test validates locally — and which subsequently ran green as the CI-required `Integration Test` check on PR #4782.

---

_Verified: 2026-07-10T19:05:00Z_
_Verifier: Claude (gsd-verifier)_
