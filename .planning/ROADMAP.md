# Roadmap: HoloMUSH

## Overview

HoloMUSH is a mature, actively-developed platform — the event-sourced core, ABAC access control, dual-protocol
(telnet + web) gateways, two-tier plugin host, and the flagship Scenes/RP subsystem are shipped and running
(full context: `milestones/v0.11-ROADMAP.md` "Shipped Foundation"). The v0.11 milestone (Channels, Scenes
lineage completion, platform hardening) shipped 2026-07-11.

**Active milestone — v0.12 Foundation Hardening (Phases 4–9):** pay down the highest-severity architecture &
operational risks surfaced by the 2026-07-11 L7 architecture review (PR #4807), plus backlog clusters 999.9
(architecture decomposition) and 999.10 (code health & test quality). Internal hardening — no new
player-facing features (requirements in `REQUIREMENTS.md`). Remaining not-yet-scheduled scope stays in
`## Backlog` — promote entries with `/gsd-review-backlog`.

## Milestones

- ✅ **v0.11 Social Spaces & Platform Hardening** — Phases 1–3 (shipped 2026-07-11) — [archive](milestones/v0.11-ROADMAP.md) · [audit](milestones/v0.11-MILESTONE-AUDIT.md)
- 🔨 **v0.12 Foundation Hardening** — Phases 4–9 (active) — event-model decision + fixes, operational hardening, architecture decomposition, code health & test quality
- 📋 **Next milestone** — not yet defined (`/gsd-new-milestone`)

## Phases

<details>
<summary>✅ v0.11 Social Spaces & Platform Hardening (Phases 1–3) — SHIPPED 2026-07-11</summary>

- [x] Phase 1: Channels Subsystem (10/10 plans) — `core-channels` as the social-spaces substrate's second consumer — completed 2026-07-09
- [x] Phase 2: Scenes Lineage Completion (7/7 plans) — notifications + telnet polish (templates descoped to backlog) — completed 2026-07-09
- [x] Phase 3: Platform Hardening & Deployment Scaling (9/9 plans) — external/clustered NATS, multi-node crypto invalidation, audit DLQ — completed 2026-07-10

Full phase details, requirements mapping, and success criteria: [milestones/v0.11-ROADMAP.md](milestones/v0.11-ROADMAP.md).
Phase execution artifacts: `milestones/v0.11-phases/`.

</details>

**v0.12 Foundation Hardening** — phase numbering continues across milestones (v0.11 ended at Phase 3):

- [ ] **Phase 4: Operational Quick Wins & Assurance Gates** — gateway body cap, `events_audit` retention, nats CVE + vuln-scan gate, DLQ bridge, coverage gate
- [ ] **Phase 5: World-Model Resilience Investigation & Decision (F1)** — resilience/concurrency pass + the event-sourcing-vs-CRUD ADR
- [ ] **Phase 6: World-Model Integrity Fixes (M2/M12)** — version guard, dual-write elimination, event-sourcing doc correction
- [ ] **Phase 7: Event-Model & Bootstrap Decomposition** — `core.Event`/`eventbus.Event` collapse, bootstrap→`lifecycle.Orchestrator`, gateway-boundary imports
- [ ] **Phase 8: God-Object Decomposition** — CoreServer + plugin/manager decomposition (behavior-preserving)
- [ ] **Phase 9: Test-Quality & Code-Health Sweep** — coverage backfill, weak-test/ACE remediation, session-lifecycle matrix, code-health batch

## Phase Details

**Dependency spine:** the resilience pass + F1 ADR (Phase 5) gate the world-model fixes (Phase 6) and the
Event-model collapse in Phase 7. Operational quick wins (Phase 4) and god-object decomposition (Phase 8) are
independent; the code-health sweep (Phase 9) depends on the coverage gate landed in Phase 4.

### Phase 4: Operational Quick Wins & Assurance Gates

**Goal**: Close the cheap, high-value operational Highs and stand up the CI assurance gates every later phase relies on.
**Depends on**: — (independent; ship first)
**Requirements**: OPS-01, OPS-02, OPS-03, OPS-04, QUAL-01
**Success Criteria** (what must be TRUE):

1. An unauthenticated oversized request to the public gateway is rejected without OOM (bounded request body + read timeout) — F2 #4785
2. `events_audit` rows past the retention window are pruned by the extended RetentionWorker; the table no longer grows unbounded — F4 #4786
3. A build pinning a known-vulnerable `nats-server` fails the new govulncheck / vuln-scan CI gate; the dependency is ≥ v2.14.3 — F8 #4790
4. The audit-DLQ replay CLI recovers messages for an external-NATS deployment (game_id bridge), proven by a non-tautological test — F3 #4787
5. Coverage policy and CI enforcement agree — the >80% gate blocks merges, or the doc MUST is corrected to the enforced reality — F7 #4804
**Plans**: 0 plans

Plans:

- [ ] TBD (created by /gsd-plan-phase 4)

### Phase 5: World-Model Resilience Investigation & Decision (F1)

**Goal**: Empirically characterize the concurrency/dual-write risk, then decide the world-state model via ADR — the decision gate the rest of the event-model work depends on.
**Depends on**: — (exercises Phase-4 gates but is not blocked by them)
**Requirements**: OPS-05, MODEL-01
**Success Criteria** (what must be TRUE):

1. A reproducible harness exercises concurrent commands + a NATS broker flap + a replica restart + client reconnect under the two-replica deployment — #4791
2. The harness produces a documented, reproducible verdict on whether last-write-wins (M12) actually corrupts world state under concurrency
3. A committed ADR records the world-model decision (build a real projection/outbox **vs.** adopt CRUD-canonical + optimistic-concurrency/transactional-outbox), grounded in F1 (`docs/reviews/arch-review/2026-07-11/verification/f1-eventsourcing-why.md`) and the resilience evidence — #4784
4. The ADR names the concrete mechanism Phase 6 (MODEL-03/MODEL-04) will implement

### Phase 6: World-Model Integrity Fixes (M2 / M12)

**Goal**: Implement the ADR's chosen mechanism to close last-write-wins and dual-write non-atomicity, and correct the event-sourcing docs.
**Depends on**: Phase 5 (the MODEL-01 ADR)
**Requirements**: MODEL-02, MODEL-03, MODEL-04
**Success Criteria** (what must be TRUE):

1. Concurrent writers to the same world entity cannot silently lose an update — a version-guard conflict is detected and surfaced (the Phase-5 harness now passes) — #4798
2. A world mutation and its event emission are atomic or reconciled: a NATS blip after commit cannot lose the notification (closes M2)
3. Every doc site that stated the false "event sourcing / state derives from replay" principle now describes the decided model; no doc claims replay-derived world state the code does not provide
4. The relevant INV-* invariants for the new guard/outbox are bound (not left `pending`)

### Phase 7: Event-Model & Bootstrap Decomposition

**Goal**: Collapse the parallel Event models (coordinated with the ADR) and unify process bootstrap on `lifecycle.Orchestrator`.
**Depends on**: Phase 5/6 (ARCH-04 needs the MODEL-01 outcome); ARCH-03/05 are independent
**Requirements**: ARCH-03, ARCH-04, ARCH-05
**Success Criteria** (what must be TRUE):

1. A single Event representation exists; the `core.Event`/`eventbus.Event` duplication is gone, all callers migrated, with no behavior change
2. Process bootstrap runs through `lifecycle.Orchestrator` with unified start/stop ordering; startup/shutdown behavior is unchanged
3. `internal/web` / `internal/telnet` import only protocol-translation dependencies; the gateway-boundary rule passes with zero violations

### Phase 8: God-Object Decomposition

**Goal**: Decompose the CoreServer and plugin/manager god objects into cohesive, separately-testable units without behavior change.
**Depends on**: — (behavior-preserving; independent of the model work)
**Requirements**: ARCH-01, ARCH-02
**Success Criteria** (what must be TRUE):

1. `CoreServer` is split into cohesive, independently-testable units; the full integration + whole-system suites pass unchanged (behavior-preserving)
2. `plugin/manager` is similarly decomposed; plugin load/lifecycle behavior is unchanged (whole-system plugin census stays green)
3. Size/complexity metrics on the former god objects drop below an agreed threshold; no new gateway-boundary or plugin-runtime-symmetry violations are introduced

### Phase 9: Test-Quality & Code-Health Sweep

**Goal**: Backfill coverage and remediate test/code-health debt to the reconciled bar.
**Depends on**: Phase 4 (QUAL-01 sets the coverage bar)
**Requirements**: QUAL-02, QUAL-03, QUAL-04, QUAL-05
**Success Criteria** (what must be TRUE):

1. Packages under the reconciled coverage bar are backfilled with genuine behavioral tests; the coverage gate passes repo-wide
2. Skeleton/tautological tests are replaced with real assertions; ACE naming violations are gone (naming audit clean)
3. A session-lifecycle test matrix covers the connect / reconnect / multi-character / idle-timeout paths
4. The arch-review Medium cluster (secure-cookie default, ABAC empty-string sentinels, silent audit-emitter drop, DEK read-cache, `sessions.location_id` index) is addressed or explicitly deferred with rationale

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Channels Subsystem | v0.11 | 10/10 | Complete | 2026-07-09 |
| 2. Scenes Lineage Completion | v0.11 | 7/7 | Complete | 2026-07-09 |
| 3. Platform Hardening & Deployment Scaling | v0.11 | 9/9 | Complete | 2026-07-10 |
| 4. Operational Quick Wins & Assurance Gates | v0.12 | 0 | Pending | — |
| 5. World-Model Resilience Investigation & Decision | v0.12 | 0 | Pending | — |
| 6. World-Model Integrity Fixes | v0.12 | 0 | Pending | — |
| 7. Event-Model & Bootstrap Decomposition | v0.12 | 0 | Pending | — |
| 8. God-Object Decomposition | v0.12 | 0 | Pending | — |
| 9. Test-Quality & Code-Health Sweep | v0.12 | 0 | Pending | — |

## Deferred (Not in This Roadmap)

See `milestones/v0.11-REQUIREMENTS.md` "v2 Requirements" for full detail. Deferred strategic
clusters now live as first-class parking-lot entries in the `## Backlog`
section below (Forums → 999.4, Discord → 999.5, non-scene web-portal
surfaces → 999.1/999.8) — route each through `/gsd-spec-phase` before
roadmapping.

## Backlog

Strategic clusters consolidated from the beads → GitHub Issues migration
(2026-07-09). Member-level detail: [`.planning/archive/beads/TRIAGE.md`](archive/beads/TRIAGE.md).
Promote an entry with `/gsd-review-backlog` when ready.

The 2026-07-11 L7 architecture review (PR #4807) filed 23 discrete issues #4784–#4806
(epic E1 #4806) that overlap the foundation clusters below; per-cluster `**Related
issues:**` lines cross-link them. The issues track the discrete work; these clusters carry
the strategic frame. Reviewed 2026-07-11 (`/gsd-review-backlog`): all 19 entries kept — none stale.

> **v0.12 promotion (2026-07-11):** milestone **v0.12 Foundation Hardening** (Phases 4–9, see `## Phase
> Details` above) pulls the bulk of **999.9** (architecture decomposition → ARCH-01..05) and **999.10** (code
> health & test quality → QUAL-01..05) into active scope, and addresses the arch-review operational Highs
> (→ OPS-01..05) plus the F1 event-model decision (→ MODEL-01..04). Both clusters are **kept** here — v0.12
> may not exhaust them, and their residual scope stays parked. Detail in `REQUIREMENTS.md`.

### Phase 999.1: Web Client Portal completion (BACKLOG)

**Goal:** Round out the web portal beyond scenes: offline support, wiki/help pages, character profiles + creation/management UI, admin portal, and a web surface for 1:1 direct messages.
**Source:** beads migration — 7 item(s) incl. epic(s) `holomush-qve`; member list in TRIAGE.md
**Related issues:** arch-review F6 PWA/offline #4803 (overlaps the offline-support + web-surface goals).
**Requirements:** TBD
**Plans:** TBD (promote with /gsd-review-backlog when ready)

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.2: Channels — remaining scope (BACKLOG)

**Goal:** Close the gap between the shipped Phase-1 channels subsystem and the full Epic-10 vision (moderation depth, history replay UX, channel types, message search). Verify each item against what Phase 1 already delivered before planning.
**Source:** beads migration — 8 item(s) incl. epic(s) `holomush-0sc`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.3: Scenes & RP — remaining scope (BACKLOG)

**Goal:** Long-tail scenes work not covered by the shipped lineage: remaining epic scope under holomush-5rh (templates were explicitly descoped to backlog on 2026-07-08).
**Source:** beads migration — 1 item(s) incl. epic(s) `holomush-5rh`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.4: Forums (BACKLOG)

**Goal:** Forum boards/threads/posts with web UI, moderation, notifications, and in-game integration. No design exists yet — needs brainstorm + spec before planning (theme:social-spaces).
**Source:** beads migration — 9 item(s) incl. epic(s) `holomush-djj`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.5: Discord Integration (BACKLOG)

**Goal:** Discord bridge plugin: bot, channel bridging, OAuth account linking, notifications, presence sync. Depends on channels substrate + an unbuilt OAuth substrate (theme:social-spaces).
**Source:** beads migration — 8 item(s) incl. epic(s) `holomush-aqq`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.6: Character Rostering & Transfer (BACKLOG)

**Goal:** Roster characters and transfer them between players (epic holomush-gloh).
**Source:** beads migration — 1 item(s) incl. epic(s) `holomush-gloh`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.7: Inventory & Object Manipulation (BACKLOG)

**Goal:** Inventory and object-interaction model; design task first (epic holomush-ni99).
**Source:** beads migration — 2 item(s) incl. epic(s) `holomush-ni99`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.8: Admin Web UI & Config (BACKLOG)

**Goal:** Operator tools: /admin route, server stats, player management, config surface (epics holomush-g4pb + holomush-7nub; overlaps the web-portal admin page — consolidate at design time).
**Source:** beads migration — 3 item(s) incl. epic(s) `holomush-g4pb`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.9: Architecture decomposition program (BACKLOG)

**Goal:** Repo-audit architecture follow-ups: decompose CoreServer + plugin/manager god objects, migrate bootstrap to lifecycle.Orchestrator, collapse parallel core.Event/eventbus.Event models, fix gateway-boundary imports, focus-redirect hot-path cache.
**Source:** beads migration — 9 item(s) incl. epic(s) `holomush-1bft`, `holomush-dj95`, `holomush-wm0fi`, `holomush-yvdm`; member list in TRIAGE.md
**Related issues:** arch-review F1 event-sourcing-never-built #4784 (event-sourcing-vs-CRUD ADR decision; overlaps the parallel core.Event/eventbus.Event model-collapse goal).
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.10: Code health & test-quality program (BACKLOG)

**Goal:** Codebase humanization/de-slop, ACE naming violations, weak/skeleton tests, security polish batch, coverage backfill on Phase-1.5 infra packages, session-lifecycle test matrix.
**Source:** beads migration — 8 item(s) incl. epic(s) `holomush-ec22`, `holomush-89o9`; member list in TRIAGE.md
**Related issues:** arch-review F7 coverage #4804 (overlaps the coverage-backfill goal).
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.11: Invariant registry backfill program (BACKLOG)

**Goal:** Bind pending INV-* registry entries per scope (SCENE 60, PLUGIN 39, EVENTBUS 28, crypto + long tail), migrate INV-DOCS/INV-BRANDING scopes, reclassify entries that fail the invariant bar (epic holomush-hz0v4).
**Source:** beads migration — 11 item(s) incl. epic(s) `holomush-hz0v4`, `holomush-s6wp`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.12: Observability & vendor-neutral telemetry (BACKLOG)

**Goal:** Vendor-neutral error/telemetry/metrics abstraction at every seam (epic holomush-ionvr), error-event seam design, signal-hygiene so benign conditions stop masquerading as ERROR/WARN.
**Source:** beads migration — 3 item(s) incl. epic(s) `holomush-ionvr`, `holomush-yxfbi`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.13: Ops & deployment resilience (BACKLOG)

**Goal:** Disaster recovery + backup/restore guides, background DB sync to object storage, gateway-survival deploy strategy, Tailscale admin access, remote KMS substrate (VaultTransitProvider + rotation CLIs).
**Source:** beads migration — 6 item(s) incl. epic(s) `holomush-aub5`; member list in TRIAGE.md
**Related issues:** arch-review F2 gateway OOM #4785, F3 DLQ #4787, F4 events_audit unbounded #4786, F8 nats CVE #4790 (overlap the gateway-survival + backup/DR goals).
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.14: Platform & security design seeds (BACKLOG)

**Goal:** Design-needed platform work: load/perf harness + SLOs, feature-flag system, audit-backfill CLI, audit drift detector, KEK fail-closed decision, plugin scene-metadata privacy decision, comm event-type extensibility, plugin hostfunc authorization, ABAC fair-share timeout + debug endpoint.
**Source:** beads migration — 9 item(s); member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.15: Documentation program (BACKLOG)

**Goal:** Comprehensive features/usage/admin/operator/player docs under site/docs, consolidated system-design documentation, session-lifecycle diagram, unified in-game + website help system.
**Source:** beads migration — 4 item(s) incl. epic(s) `holomush-k7qy`, `holomush-rm9g`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.16: Feature wishlist (BACKLOG)

**Goal:** Player/operator-facing capabilities awaiting prioritization: rich text (markdown + emoji), operator-defined color themes, interface-backed content/blob storage, plugin-authoring Claude Code skill.
**Source:** beads migration — 4 item(s); member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.17: iOS Client (stretch) (BACKLOG)

**Goal:** Native iOS client (Epic 13) — stretch goal; depends on stable web/API surface.
**Source:** beads migration — 1 item(s) incl. epic(s) `holomush-5g6`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.18: Release process coherence (BACKLOG)

**Goal:** Review release procedures end-to-end and make them coherent: consider restoring
release-please (or keeping cog — evaluate, don't assume), align the release flow with GSD
practices/idioms (milestone close ↔ release cut, labels tracking cog-computed semver per
PROJECT.md Key Decisions), and produce better release notes than the current
GoReleaser-generated ones. Not necessarily all one tool, but something coherent.
**Source:** captured 2026-07-11 at v0.11 milestone close (milestone-relabel session — the
v1.0/v0.11 label drift and the GSD-tagging/cog collision motivated this review)
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.19: Restore lefthook + speed up the inner loop (BACKLOG)

**Goal:** Now that the repo is back on native git only (jj retired), restore lefthook git
hooks (worktree creation currently warns "no lefthook config found") and look for further
inner-loop speedups. Investigate: reinstate a lefthook config so `task workspace:new`
worktrees auto-install hooks (pre-commit fmt/lint, commit-msg conventional-commit check to
match CI's PR-title gate), and profile the `task pr-prep` fast lane / `task lint` / `task
test` cycle for wins (caching, scoping, parallelism). Aim: tighter edit→check feedback.
**Source:** captured 2026-07-11 at v0.11 milestone close (multiple worktree sessions this
day emitted "No lefthook config" warnings on every commit)
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)
