---
phase: 03-platform-hardening-deployment-scaling
plan: 09
subsystem: infra
tags: [nats, jetstream, operator-docs, runbook, dlq, account-scoping, starlight]

# Dependency graph
requires:
  - phase: 03-01
    provides: event_bus config section (mode/url/credentials/tls/provision/dlq keys)
  - phase: 03-06
    provides: deploy/nats account templates + verify-scoping.sh + boot self-check
  - phase: 03-07
    provides: holomush audit dlq list/show/replay CLI + holomush_audit_dlq_messages_total
  - phase: 03-08
    provides: compose.cluster.yaml overlay + deploy/nats/cluster-config.yaml
provides:
  - "External-NATS deployment runbook (CLUSTER-05) covering the full 7-step lifecycle"
  - "Documented cutover data stance (Postgres durable, EVENTS stream fresh)"
affects: [operator-onboarding, sandbox-migration]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Operator how-to grounded verbatim in shipped config keys/CLI/scripts (no invented flags)"

key-files:
  created:
    - site/src/content/docs/operating/how-to/external-nats-deployment.md
  modified: []

key-decisions:
  - "Single doc commit: both plan tasks (write + accuracy/build pass) produce one atomic artifact"
  - "Replaced a fragile in-page anchor cross-link with plain-text reference to satisfy rumdl MD051"

patterns-established:
  - "Runbook cross-checks every command against the real shipped artifact before it is documented"

requirements-completed: [CLUSTER-05]

coverage:
  - id: D1
    description: "Full-lifecycle external-NATS operator runbook: provision NATS + holomush-server account -> mint .creds -> configure event_bus external -> cut over from embedded -> verify scoping -> DLQ list/show/replay -> rollback to embedded"
    requirement: "CLUSTER-05"
    verification:
      - kind: automated
        ref: "task docs:build (page rendered at site/dist/operating/how-to/external-nats-deployment/index.html)"
        status: pass
      - kind: automated
        ref: "task lint:markdown (rumdl clean, 84 site files)"
        status: pass
    human_judgment: true
    rationale: "Operator-facing prose accuracy (guidance correctness, tone, completeness) is editorial and needs a human read; the build/lint gates only prove it renders and lints clean."
  - id: D2
    description: "Explicit cutover data stance documented: Postgres events_audit is the durable record; the EVENTS JetStream stream starts fresh on the external cluster (no migration)"
    requirement: "CLUSTER-05"
    verification:
      - kind: other
        ref: "rg 'events_audit|EVENTS stream starts fresh|audit dlq replay|verify-scoping.sh|compose.cluster.yaml' external-nats-deployment.md"
        status: pass
    human_judgment: true
    rationale: "Whether the stance is stated clearly enough to prevent operator data-loss surprise (threat T-03-24) is a judgment call."

# Metrics
duration: 20min
completed: 2026-07-10
status: complete
---

# Phase 3 Plan 09: External NATS Deployment Runbook Summary

**CLUSTER-05 operator runbook for the full external-NATS lifecycle — provision, cut over, verify scoping, operate the audit DLQ, roll back — with an explicit Postgres-durable / EVENTS-fresh cutover data stance, grounded in the shipped config keys, CLI, and deploy assets.**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-07-10
- **Tasks:** 2 (one atomic doc artifact)
- **Files modified:** 1 created

## Accomplishments

- Wrote `site/src/content/docs/operating/how-to/external-nats-deployment.md`, the single end-to-end operator runbook for CLUSTER-05, walking all seven D-16 lifecycle steps in order.
- Documented the cutover data stance explicitly with a danger callout: `events_audit` (Postgres) is the durable record and survives cutover unchanged; the `EVENTS` JetStream stream starts fresh on the external cluster — no stream migration (mitigates T-03-24).
- Grounded every command/key in shipped reality: `event_bus:` config keys (`mode`, `url`, `credentials`, `tls`, `provision`, `dlq.max_age`, `dlq.max_bytes`), `holomush audit dlq {list,show,replay}` with real flags (`--all`, `--msg-id`, `--limit`), `deploy/nats/verify-scoping.sh`, the `EVENTBUS_ACCOUNT_OVERSCOPED` boot self-check, `compose.cluster.yaml`, and the `holomush_audit_dlq_messages_total` metric.
- Marked the read-only operator account and the sandbox (`game.holomush.dev`) migration as deferred future options (D-15), never as in-scope steps.
- Page auto-lists in the operating sidebar (`autogenerate: { directory: 'operating' }`) and cross-links to the database and sandbox-operations how-tos.

## Task Commits

1. **Task 1 + 2: Write the full-lifecycle runbook + accuracy/build pass** - `50b99520e` (docs)

**Plan metadata:** (this commit)

## Files Created/Modified

- `site/src/content/docs/operating/how-to/external-nats-deployment.md` - The CLUSTER-05 external-NATS deployment runbook.

## Decisions Made

- Combined both plan tasks into one commit: Task 1 writes the runbook and Task 2 is an accuracy/build verification pass over the same single file — one atomic artifact, no separate code change to isolate.
- The top-level config section is `event_bus:` (koanf), confirmed against `internal/eventbus/config.go` and `deploy/nats/cluster-config.yaml`. The plan/context prose used `eventbus:` loosely in places; documented the shipped `event_bus:` form.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Installed site docs dependencies before build**

- **Found during:** Task 2 (docs build)
- **Issue:** `task docs:build` failed — `site/node_modules` was absent, so `bunx astro build` tried to fetch `astro@latest` from a temp dir and could not resolve `astro/config`.
- **Fix:** Ran `task docs:setup` (`bun install` in `site/`) to install the pinned site dependencies, then re-ran the build.
- **Files modified:** none tracked (`site/node_modules` is gitignored)
- **Verification:** `task docs:build` completes; page rendered at `site/dist/operating/how-to/external-nats-deployment/index.html`.
- **Committed in:** n/a (environment setup, no tracked change)

**2. [Rule 1 - Bug] Removed a fragile in-page anchor cross-link (rumdl MD051)**

- **Found during:** Task 1 lint (`task fmt` markdown check)
- **Issue:** The intro danger callout linked `[Step 4](#step-4-cut-over-from-embedded)`; the em-dash heading produced a different slug, so rumdl MD051 flagged a non-existent anchor.
- **Fix:** Replaced the link with a plain-text reference ("Read the 'Cut over from embedded' step below").
- **Files modified:** `site/src/content/docs/operating/how-to/external-nats-deployment.md`
- **Verification:** `task lint:markdown` clean on the site tree.
- **Committed in:** `50b99520e`

---

**Total deviations:** 2 auto-fixed (1 blocking env setup, 1 lint bug)
**Impact on plan:** Both necessary to reach a lint-clean, buildable page. No scope creep.

## Issues Encountered

- Ripgrep output for the DLQ Prometheus metric was visually mangled by highlighting; resolved by reading `internal/eventbus/audit/lag_metric.go` directly, confirming the fully-qualified counter is `holomush_audit_dlq_messages_total` (`Namespace: holomush`, `Subsystem: audit`, `Name: dlq_messages_total`).

## User Setup Required

None - documentation only.

## Next Phase Readiness

- CLUSTER-05 is complete; this is the last plan of the phase. Operators have an accurate, buildable runbook for the full external-NATS lifecycle.
- Deferred follow-ups noted in the runbook: sandbox migration to external NATS (D-15) and the read-only operator account.

## Self-Check: PASSED

- FOUND: site/src/content/docs/operating/how-to/external-nats-deployment.md
- FOUND: commit 50b99520e

---
*Phase: 03-platform-hardening-deployment-scaling*
*Completed: 2026-07-10*
