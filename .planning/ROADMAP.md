# Roadmap: HoloMUSH

## Overview

HoloMUSH is a mature, actively-developed platform — the event-sourced core, ABAC access control, dual-protocol
(telnet + web) gateways, two-tier plugin host, and the flagship Scenes/RP subsystem are shipped and running
(full context: `milestones/v0.11-ROADMAP.md` "Shipped Foundation"). The v0.11 milestone (Channels, Scenes
lineage completion, platform hardening) shipped 2026-07-11. Forward scope now lives in the `## Backlog`
section below — promote entries with `/gsd-review-backlog` when defining the next milestone
(`/gsd-new-milestone`).

## Milestones

- ✅ **v0.11 Social Spaces & Platform Hardening** — Phases 1–3 (shipped 2026-07-11) — [archive](milestones/v0.11-ROADMAP.md) · [audit](milestones/v0.11-MILESTONE-AUDIT.md)
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

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Channels Subsystem | v0.11 | 10/10 | Complete | 2026-07-09 |
| 2. Scenes Lineage Completion | v0.11 | 7/7 | Complete | 2026-07-09 |
| 3. Platform Hardening & Deployment Scaling | v0.11 | 9/9 | Complete | 2026-07-10 |

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
the strategic frame. Reviewed 2026-07-11 (`/gsd-review-backlog`): all 19 entries kept —
none stale; no active milestone, so prioritization is deferred to `/gsd-new-milestone`.

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
