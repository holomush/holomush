---
phase: 03-platform-hardening-deployment-scaling
plan: 02
subsystem: testsupport / eventbus-external-nats
tags: [testing, nats, jetstream, testcontainers, cluster, CLUSTER-03]
status: complete
requires:
  - testcontainers-go (already in go.mod)
  - nats.go / jetstream (already in go.mod)
provides:
  - "internal/testsupport/natstest.StartNATS / NATSEnv.Conn / NATSEnv.URL / NATSEnv.Terminate"
  - "external-NATS integration test tier (D-06 rule amendment)"
affects:
  - "downstream external-mode proofs: CLUSTER-01 external connect, CLUSTER-02 scoping self-check, CLUSTER-03 multi-node invalidation, CLUSTER-04 DLQ"
tech-stack:
  added: []
  patterns:
    - "generic testcontainer (no NATS go.mod module) mirroring test/testutil/postgres.go"
    - "per-replica independent *nats.Conn handed via NATSEnv.Conn(t) + t.Cleanup"
key-files:
  created:
    - internal/testsupport/natstest/nats.go
    - internal/testsupport/natstest/nats_test.go
  modified:
    - CLAUDE.md
    - .claude/rules/testing.md
    - .golangci.yaml
decisions:
  - "Single external NATS node (not a NATS cluster) — invariants bind on HoloMUSH replicas, not NATS replication (OQ-4)"
  - "Generic testcontainers.GenericContainer running nats:2-alpine -js — no NATS testcontainer module added to go.mod"
  - "natstest added to depguard deny-list at eventbustest/coretest parity (threat T-03-03 mitigation)"
metrics:
  duration: ~20m
  tasks: 2
  files: 5
  completed: 2026-07-10
---

# Phase 3 Plan 2: External-NATS Integration Substrate Summary

Ship a testcontainer harness that boots one real NATS JetStream node and hands each caller an independent `*nats.Conn`, and amend the test-tier rule (D-06) so external-mode-specific behavior must be verified against a real broker instead of the shared embedded harness.

## What Was Built

**Task 1 — `internal/testsupport/natstest`** (`7602f9b7b`): `StartNATS(ctx)` runs a generic `testcontainers.GenericContainer` (`nats:2-alpine`, `-js -sd /data`, wait on port 4222 + "Server is ready"), resolves the mapped port into a dialable `URL`, and retries+reclaims half-started containers exactly like `test/testutil/postgres.go`. `(*NATSEnv).Conn(t)` dials a NEW connection per call and registers `t.Cleanup(conn.Close)` — closing the CLUSTER-03 gap where the crypto multi-member test shares one in-process conn. The integration smoke test proves JetStream `AccountInfo` succeeds and that two `Conn(t)` calls yield distinct objects with distinct broker client IDs (`GetClientID`). The helper is build-tag-free (importable by any integration test); the smoke test carries `//go:build integration`.

**Task 2 — D-06 rule amendment** (`cb7e8e68c`): CLAUDE.md and `.claude/rules/testing.md` now state embedded NATS is correct at every tier EXCEPT external-mode-specific behavior (external dial/fail-closed boot, single-principal scoping, multi-node per-replica invalidation, DLQ against a real broker), which MUST use `internal/testsupport/natstest`. Added an external-NATS Test-Tiers row and a `**/natstest/**/*.go` frontmatter path so the rule auto-loads there. Kept depguard parity: production code MUST NOT import natstest (wording in both docs + a new deny entry in `.golangci.yaml`).

## Verification

- `task test:int -- -run TestNatstest ./internal/testsupport/natstest/` — 2 tests green (5.99s, Docker).
- `task lint` — EXIT=0 (new package + depguard config clean).
- `task lint:docs-symmetry` — AGENTS.md → CLAUDE.md symlink intact.
- `task fmt` output committed; go.mod/go.sum unchanged (no NATS testcontainer module added).

## Deviations from Plan

**1. [Rule 2 — threat mitigation] Added natstest to `.golangci.yaml` depguard deny-list**
- **Found during:** Task 2. The rule prose says "production code MUST NOT import natstest"; the enforcing mechanism is depguard, which lists eventbustest/coretest/quarantinetest but not natstest.
- **Fix:** Added a deny entry mirroring the siblings, satisfying threat T-03-03's `mitigate` disposition (depguard parity so production imports fail lint). The plan's Task 2 action explicitly calls for this parity ("mirror the eventbustest/coretest wording").
- **Files modified:** `.golangci.yaml`
- **Commit:** `cb7e8e68c`

Otherwise the plan executed as written.

## Known Stubs

None. The harness provides transport only (no stream provisioning) — this is intentional per the plan: callers provision what they need over their own connections.

## Self-Check: PASSED

- FOUND: internal/testsupport/natstest/nats.go
- FOUND: internal/testsupport/natstest/nats_test.go
- FOUND commit: 7602f9b7b (feat 03-02 harness)
- FOUND commit: cb7e8e68c (docs 03-02 D-06 amendment)
