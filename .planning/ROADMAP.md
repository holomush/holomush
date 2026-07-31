# Roadmap: HoloMUSH

## Overview

HoloMUSH is a mature, actively-developed platform — the event-sourced core, ABAC access control, dual-protocol
(telnet + web) gateways, two-tier plugin host, and the flagship Scenes/RP subsystem are shipped and running
(full context: `milestones/v0.11-ROADMAP.md` "Shipped Foundation"). The v0.11 milestone (Channels, Scenes
lineage completion, platform hardening) shipped 2026-07-11.

The v0.12 milestone (Foundation Hardening, Phases 4–9) shipped 2026-07-28 — the event-model decision and
its integrity fixes, operational hardening and CI assurance gates, architecture decomposition, and the
code-health sweep. Full detail: `milestones/v0.12-ROADMAP.md`.

**No milestone is currently active.** Define the next one with `/gsd-new-milestone`, which also produces a
fresh `REQUIREMENTS.md` (the v0.12 one is archived at `milestones/v0.12-REQUIREMENTS.md`). Not-yet-scheduled
scope stays in `## Backlog` — promote entries with `/gsd-review-backlog`.

## Milestones

- ✅ **v0.11 Social Spaces & Platform Hardening** — Phases 1–3 (shipped 2026-07-11) — [archive](milestones/v0.11-ROADMAP.md) · [audit](milestones/v0.11-MILESTONE-AUDIT.md)
- ✅ **v0.12 Foundation Hardening** — Phases 4–9 (shipped 2026-07-28) — [archive](milestones/v0.12-ROADMAP.md) · [audit](milestones/v0.12-MILESTONE-AUDIT.md)
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

<details>
<summary>✅ v0.12 Foundation Hardening (Phases 4–9) — SHIPPED 2026-07-28</summary>

- [x] Phase 4: World-Model Resilience Investigation & Decision (F1) (4/4 plans) — resilience/concurrency pass + the event-sourcing-vs-CRUD ADR (decision gate) — completed 2026-07-11
- [x] Phase 5: World-Model Integrity Fixes (M2/M12) (16/16 plans) — version guard, transactional outbox, event-sourcing doc correction — completed 2026-07-13
- [x] Phase 6: Operational Hardening & Assurance Gates (5/5 plans) — `events_audit` retention, nats CVE + vuln-scan gate, DLQ bridge, coverage reconciliation — completed 2026-07-15
- [x] Phase 7: Event-Model & Bootstrap Decomposition (11/11 plans) — `core.Event`/`eventbus.Event` collapse, bootstrap→`lifecycle.Orchestrator`, gateway-boundary imports — completed 2026-07-18
- [x] Phase 8: God-Object Decomposition (9/9 plans) — CoreServer + plugin/manager decomposition (behavior-preserving) — completed 2026-07-19
- [x] Phase 9: Test-Quality & Code-Health Sweep (21/21 plans) — coverage-chain repair, ABAC fail-open fix, session-lifecycle matrix — completed 2026-07-27

Full phase details, requirements mapping, and success criteria: [milestones/v0.12-ROADMAP.md](milestones/v0.12-ROADMAP.md).
Phase execution artifacts: `milestones/v0.12-phases/`. Completion audit: [milestones/v0.12-MILESTONE-AUDIT.md](milestones/v0.12-MILESTONE-AUDIT.md).

**Closed as `override_closeout`:** Phase 9 verified `gaps_found` (3/4) because QUAL-02/03/05 were
deliberately deferred with tracking (#4860, #4861, #4875, #4876, #4792); all other phases verified
`passed`. Carried forward: #4880 (CLAUDE.md event-construction rule defect), #4881 (no ruleset↔CI
reconciliation), #4882/#4883 (unquarantined flakes).

</details>

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Channels Subsystem | v0.11 | 10/10 | Complete | 2026-07-09 |
| 2. Scenes Lineage Completion | v0.11 | 7/7 | Complete | 2026-07-09 |
| 3. Platform Hardening & Deployment Scaling | v0.11 | 9/9 | Complete | 2026-07-10 |
| 4. World-Model Resilience Investigation & Decision (F1) | v0.12 | 4/4 | Complete    | 2026-07-11 |
| 5. World-Model Integrity Fixes (M2/M12) | v0.12 | 16/16 | Complete    | 2026-07-13 |
| 6. Operational Hardening & Assurance Gates | v0.12 | 5/5 | Complete    | 2026-07-15 |
| 7. Event-Model & Bootstrap Decomposition | v0.12 | 11/11 | Complete    | 2026-07-18 |
| 8. God-Object Decomposition | v0.12 | 9/9 | Complete   | 2026-07-19 |
| 9. Test-Quality & Code-Health Sweep | v0.12 | 21/21 [^p9] | Complete | 2026-07-27 |

[^p9]: All 21 plans executed, but plan 09-21 produced no SUMMARY (it performed the phase's only
unconditional push and opened PR #4874). Hence "20/21" appears in some records and "21/21" in others —
neither is exactly right: *executed* and *documented* diverge by one. Its five must_haves were re-derived
live and are recorded in `milestones/v0.12-phases/09-.../09-VERIFICATION.md`.

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
the strategic frame. Reviewed 2026-07-28 (`/gsd-review-backlog`): 18 entries kept, **999.9 removed** (see below).
Re-reviewed 2026-07-31 (`/gsd-review-backlog`): **17 entries kept, 999.3 removed** (see below).

> **2026-07-31 review — scope reconciliation.** This pass verified cluster contents against the
> tree rather than re-reading goal prose, and three entries were wrong:
>
> - **999.3 is REMOVED.** Its only real content was scene templates (SCENEFWD-01, descoped
>   2026-07-08). Filed standalone as **#4897** and dropped from the backlog as someday/maybe —
>   a one-item cluster, the same disposition 999.9's last item got via #4886. `holomush-5rh`'s
>   other children were already separately filed (#4643, #4728) or shipped (`.19` → Phase 2).
> - **999.2 was 8 items and is actually 1.** Seven were Epic 10's *pre-implementation* breakdown
>   and shipped in Phase 1; only full-text search remains. See its entry.
> - **999.11's per-scope counts had drifted** and buried CRYPTO (102 pending, 40% of the total)
>   as a "long tail". Recounted in its entry.
>
> **Generalized lesson — beads-migrated item counts are an UPPER bound, not a residual.** Epics
> were migrated as whole clusters *including* their build-the-schema/build-the-commands children,
> so an epic that was subsequently implemented still lists them as pending. The count is exactly
> the number that inflates when sizing a milestone. Re-verify against the tree before promoting
> any 999.x cluster sourced from the beads migration.

> **v0.13 candidate spine (selected 2026-07-31, not yet promoted):** **999.1 Web Client Portal
> completion** — the only player-facing cluster with substantial *verified* unbuilt scope (6
> concrete items: offline/PWA #4803, wiki/help, character profiles, character creation UI, admin
> portal, DM web surface). Promotion deferred to `/gsd-new-milestone`, which assigns real phase
> numbers; promoting now would mint them into a milestone that does not exist
> (`STATE.md: status: milestone_complete`).

> **v0.12 outcome (shipped 2026-07-28):** milestone **v0.12 Foundation Hardening** (Phases 4–9, archived at
> `milestones/v0.12-ROADMAP.md`) pulled the bulk of **999.9** (architecture decomposition → ARCH-01..05) and
> **999.10** (code health & test quality → QUAL-01..05), and addressed the arch-review operational Highs
> (→ OPS-01..05) plus the F1 event-model decision (→ MODEL-01..04).
>
> **999.9 is now REMOVED — exhausted.** v0.12 delivered five of its six named goals; the sixth
> (focus-redirect hot-path cache) was split out to #4886 rather than keeping a one-item cluster alive. That
> issue is explicit that the path is **unprofiled**, and that closing it unmeasured is an acceptable outcome
> — a cache on an unmeasured path trades invalidation surface for an unmeasured gain.
>
> **999.10 is kept and is now the larger residual of the two.** QUAL-02/03/05 shipped only partially
> (#4860, #4861, #4792), and the repo-wide coverage gate QUAL-01 aimed at **does not exist** — no codecov
> context is required on `main` (#4875, #4876). Detail in `milestones/v0.12-REQUIREMENTS.md`.
>
> **999.13's arch-review overlap also closed** (#4785/#4786/#4787/#4790, all shipped as OPS-01..04); its
> DR/backup/KMS core is untouched and the cluster stays.

### Phase 999.1: Web Client Portal completion (BACKLOG)

**Goal:** Round out the web portal beyond scenes: offline support, wiki/help pages, character profiles + creation/management UI, admin portal, and a web surface for 1:1 direct messages.
**Source:** beads migration — 7 item(s) incl. epic(s) `holomush-qve`; member list in TRIAGE.md
**Related issues:** arch-review F6 PWA/offline #4803 (overlaps the offline-support + web-surface goals).
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.2: Channels — remaining scope (BACKLOG)

**Goal:** Full-text search over channel message history. **This is the only member of the
original 8-item cluster that Phase 1 did not deliver.**
**Scope reconciled 2026-07-31** (the entry's own "verify against what Phase 1 delivered"
caution, finally acted on). The other 7 beads members were Epic 10's *pre-implementation*
breakdown and shipped in Phase 1 (10/10 plans, 2026-07-09), verified against the tree:
`.3` schema → `migrations/000001_channels.up.sql`; `.4` join/leave/list/say →
`commands.go:63-85`; `.5` channel types → `types.go`; `.6` moderation mute/ban/ops →
`mute`/`ban`/`kick`/`transfer` subcommands + `moderate-own-channel` policy; `.7` history +
replay-on-join → `history` subcommand; `.2` implementation plan → consumed by Phase 1.
Only `.8` (full-text search) remains: no `tsvector`, `GIN`, or `pg_trgm` exists in
`plugins/core-channels/migrations/` (earlier greps for "search" matched `search_path`, the
Postgres schema setting — a false positive worth not repeating).
**Sizing caution:** beads epics were migrated as whole clusters *including* their
pre-implementation task breakdowns, so an implemented epic still lists "build the schema"
children. Item counts on any 999.x cluster sourced from beads migration are an UPPER bound,
not a residual — re-verify against the tree before sizing a milestone off them.
**Source:** beads migration — 8 item(s) incl. epic(s) `holomush-0sc`; member list in TRIAGE.md
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

### Phase 999.10: Code health & test-quality program (BACKLOG)

**Goal:** Codebase humanization/de-slop, ACE naming violations, weak/skeleton tests, security polish batch, coverage backfill on Phase-1.5 infra packages, session-lifecycle test matrix.
**Source:** beads migration — 8 item(s) incl. epic(s) `holomush-ec22`, `holomush-89o9`; member list in TRIAGE.md
**Related issues:** arch-review F7 coverage #4804 (overlaps the coverage-backfill goal).
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.11: Invariant registry backfill program (BACKLOG)

**Goal:** Bind pending INV-* registry entries per scope, migrate INV-DOCS/INV-BRANDING scopes, reclassify entries that fail the invariant bar (epic holomush-hz0v4).
**Scope (recounted 2026-07-31 from `docs/architecture/invariants.yaml` — 243 pending, 103 bound, 346 total):**
CRYPTO 102 · SCENE 60 · PLUGIN 29 · EVENTBUS 19 · ACCESS 8 · STORE 8 · TELEMETRY 8 ·
COMMAND 3 · SESSION 3 · CLUSTER 1 · COMM 1 · PRESENCE 1. CHANNEL/PRIVACY/WORLD are fully bound.
(Sum checks: 102+60+29+19+8+8+8+3+3+1+1+1 = 243.) INV-BRANDING and INV-DOCS are *declared*
scopes carrying `status: pending` with ZERO numbered entries — they contribute nothing to the
count above; migrating them is separate work from binding entries, hence both goals.
**CRYPTO is 42% of the remaining work** and is the correct entry point, not a long-tail
afterthought — an earlier version of this entry led with SCENE and buried crypto, and also
carried stale PLUGIN/EVENTBUS counts (39/28) that had since been partially backfilled.
Recount before scoping; these numbers move under normal test work. **Anchor the pattern**
(`^\s+binding:\s*pending`) — an unanchored count also matches 11 YAML comments that discuss
pending bindings in prose, which is how this entry first shipped "254".
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
**Related issues:** none open. The arch-review overlap (F2 gateway OOM #4785, F3 DLQ #4787, F4 events_audit unbounded #4786, F8 nats CVE #4790) all shipped in v0.12 as OPS-01..04 and is closed — this cluster's remaining scope is the DR/backup/object-storage/Tailscale/KMS core, which is untouched.
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
