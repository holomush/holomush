# Roadmap: HoloMUSH

## Overview

HoloMUSH is a mature, actively-developed platform — the event-sourced core, ABAC access control, dual-protocol
(telnet + web) gateways, two-tier plugin host, and the flagship Scenes/RP subsystem are shipped and running
(full context: `milestones/v0.11-ROADMAP.md` "Shipped Foundation"). The v0.11 milestone (Channels, Scenes
lineage completion, platform hardening) shipped 2026-07-11.

The v0.12 milestone (Foundation Hardening, v0.12 Phases 4–9) shipped 2026-07-28 — the event-model decision and
its integrity fixes, operational hardening and CI assurance gates, architecture decomposition, and the
code-health sweep. Full detail: `milestones/v0.12-ROADMAP.md`.

**Active milestone — v0.13 Web Portal: Identity & Admin Foundations (Phases 1–6):** give web players a
complete character identity surface — creation, management, and public profiles with privacy — and stand up
the admin portal shell that gives character administration a home, with both designed to absorb the deferred
portal surfaces without rework. The milestone **opens by producing the portal SPEC** that PROJECT.md's
Out-of-Scope entry named as its precondition, rather than waiving it. Requirements: `REQUIREMENTS.md`
(50 REQ-IDs across PORTAL / IDENT / PROFILE / ADMIN / EXT). Research basis: `research/SUMMARY.md`.
Remaining not-yet-scheduled scope stays in `## Backlog` — promote entries with `/gsd-review-backlog`.

## Milestones

> **Phase-numbering convention (changed at v0.13).** Phase numbers **restart at 1 for every milestone as of
> v0.13** — a phase id is unique *within its milestone*, not across project history. **v0.11 and v0.12 used
> continuous global numbering** (v0.11 Phases 1–3, v0.12 Phases 4–9) and are **not** renumbered: they shipped
> under those numbers, and the archives, PRs, and `milestones/v0.1x-phases/` directories all reference them.
> Wherever an archived phase is named below, its milestone is stated explicitly (e.g. "v0.12 Phase 4"); a
> bare "Phase N" always means the **active milestone**. The `999.x` backlog entries are parking-lot ids, not
> milestone phases — their numbering is independent of both schemes.

- ✅ **v0.11 Social Spaces & Platform Hardening** — v0.11 Phases 1–3 (shipped 2026-07-11) — [archive](milestones/v0.11-ROADMAP.md) · [audit](milestones/v0.11-MILESTONE-AUDIT.md)
- ✅ **v0.12 Foundation Hardening** — v0.12 Phases 4–9 (shipped 2026-07-28) — [archive](milestones/v0.12-ROADMAP.md) · [audit](milestones/v0.12-MILESTONE-AUDIT.md)
- ✅ **v0.13 Web Portal: Identity & Admin Foundations** — Phases 1–6 (shipped 2026-08-16) — [archive](milestones/v0.13-ROADMAP.md) · [audit](milestones/v0.13-MILESTONE-AUDIT.md)

## Phases

<details>
<summary>✅ v0.11 Social Spaces & Platform Hardening (v0.11 Phases 1–3) — SHIPPED 2026-07-11</summary>

> **Numbering:** these are **v0.11** phase numbers under the retired continuous global scheme. They are
> *not* the active milestone's Phases 1–6 and are deliberately left un-renumbered.

- [x] v0.11 Phase 1: Channels Subsystem (10/10 plans) — `core-channels` as the social-spaces substrate's second consumer — completed 2026-07-09
- [x] v0.11 Phase 2: Scenes Lineage Completion (7/7 plans) — notifications + telnet polish (templates descoped to backlog) — completed 2026-07-09
- [x] v0.11 Phase 3: Platform Hardening & Deployment Scaling (9/9 plans) — external/clustered NATS, multi-node crypto invalidation, audit DLQ — completed 2026-07-10

Full phase details, requirements mapping, and success criteria: [milestones/v0.11-ROADMAP.md](milestones/v0.11-ROADMAP.md).
Phase execution artifacts: `milestones/v0.11-phases/`.

</details>

<details>
<summary>✅ v0.12 Foundation Hardening (v0.12 Phases 4–9) — SHIPPED 2026-07-28</summary>

> **Numbering:** these are **v0.12** phase numbers under the retired continuous global scheme. They are
> *not* the active milestone's Phases 1–6 and are deliberately left un-renumbered.

- [x] v0.12 Phase 4: World-Model Resilience Investigation & Decision (F1) (4/4 plans) — resilience/concurrency pass + the event-sourcing-vs-CRUD ADR (decision gate) — completed 2026-07-11
- [x] v0.12 Phase 5: World-Model Integrity Fixes (M2/M12) (16/16 plans) — version guard, transactional outbox, event-sourcing doc correction — completed 2026-07-13
- [x] v0.12 Phase 6: Operational Hardening & Assurance Gates (5/5 plans) — `events_audit` retention, nats CVE + vuln-scan gate, DLQ bridge, coverage reconciliation — completed 2026-07-15
- [x] v0.12 Phase 7: Event-Model & Bootstrap Decomposition (11/11 plans) — `core.Event`/`eventbus.Event` collapse, bootstrap→`lifecycle.Orchestrator`, gateway-boundary imports — completed 2026-07-18
- [x] v0.12 Phase 8: God-Object Decomposition (9/9 plans) — CoreServer + plugin/manager decomposition (behavior-preserving) — completed 2026-07-19
- [x] v0.12 Phase 9: Test-Quality & Code-Health Sweep (21/21 plans) — coverage-chain repair, ABAC fail-open fix, session-lifecycle matrix — completed 2026-07-27

Full phase details, requirements mapping, and success criteria: [milestones/v0.12-ROADMAP.md](milestones/v0.12-ROADMAP.md).
Phase execution artifacts: `milestones/v0.12-phases/`. Completion audit: [milestones/v0.12-MILESTONE-AUDIT.md](milestones/v0.12-MILESTONE-AUDIT.md).

**Closed as `override_closeout`:** v0.12 Phase 9 verified `gaps_found` (3/4) because QUAL-02/03/05 were
deliberately deferred with tracking (#4860, #4861, #4875, #4876, #4792); all other phases verified
`passed`. Carried forward: #4880 (CLAUDE.md event-construction rule defect), #4881 (no ruleset↔CI
reconciliation), #4882/#4883 (unquarantined flakes).

</details>

<details>
<summary>✅ v0.13 Web Portal: Identity & Admin Foundations (Phases 1–6, plus four inserted) — SHIPPED 2026-08-16</summary>

> **Numbering:** v0.13 is the first milestone under the restarted per-milestone scheme — its phases are
> numbered from 1. Four phases were **inserted** mid-milestone (01.1, 02.1, 02.2, 06.1) and carry decimal
> ids; all ten are listed below and all ten are archived.

- [x] Phase 1: Portal SPEC (6/6 plans) — settle every shape decision whose cost explodes after code exists — completed 2026-08-01
- [x] Phase 01.1: Migration framework — adopt goose for Go migrations (7/7 plans) — 88 files collapsed to 44 single-file migrations — completed 2026-08-06
- [x] Phase 2: ABAC & Schema Vocabulary (13/13 plans) — admin-section + public-profile policy, name normalization + unique index, lifecycle column — completed 2026-08-06
- [x] Phase 02.1: World Caller Model (3/3 plans) — `world.Service` takes a typed caller value instead of a bare subject string (INV-WORLD-8) — completed 2026-08-09
- [x] Phase 02.2: Background Job Authorization Model (5/5 plans) — `job:<name>` principals, capability class, per-execution instance scoping (INV-ACCESS-13) — completed 2026-08-09
- [x] Phase 3: World Character Commands (6/6 plans) — soft retire/unretire + the retirement reactor (`RenameCharacter` moved to 999.20) — completed 2026-08-10
- [x] Phase 4: Shared Facade Helpers & `CharacterAccessService` (9/9 plans) — one guest/ownership gate; privacy enforced by absence — completed 2026-08-11
- [x] Phase 5: Character Identity UI & Public Profiles (8/8 plans) — creation identity card, multi-alt management, public profile page, per-field visibility — completed 2026-08-13
- [x] Phase 6: Admin Portal Shell & Character Administration (4/4 plans) — ABAC-gated `/admin`, character administration, six sections denied-after-gate — completed 2026-08-14
- [x] Phase 06.1: Admin Portal Web Surface (10/10 plans) — shadcn-svelte components, the single-row edit surface, breakpoint census — completed 2026-08-15

Full phase details, requirements mapping, and success criteria: [milestones/v0.13-ROADMAP.md](milestones/v0.13-ROADMAP.md).
Phase execution artifacts: `milestones/v0.13-phases/`. Completion audit: [milestones/v0.13-MILESTONE-AUDIT.md](milestones/v0.13-MILESTONE-AUDIT.md).

**Closed as `override_closeout`.** All ten phases verified `passed` and all 51 in-scope requirements are
satisfied — IDENT-03 was deliberately removed from the milestone on 2026-08-06 and moved to backlog 999.20.
Ten open artifacts were acknowledged and deferred at close (see `STATE.md` → Deferred Items). Nyquist
coverage is complete: 5 phases compliant, 5 partial with named causes (#4993, #4994, #4971, #4982, and
phase 01's planning-process finding). Carried forward: #4990/#4991/#4992 (integration orphans), #4996
(agent-facing pipe hazard), #4974/#4966 (traceability writer defects).

</details>

## Phase Details

**Dependency spine.** The milestone opens on the **SPEC** (Phase 1) because eight of the fourteen
catalogued pitfalls are shape decisions — message shape, audience matrix, lifecycle vocabulary,
`expected_version` placement — whose cost explodes once code exists, and because PROJECT.md's Out-of-Scope
entry for non-scene portal surfaces named exactly this SPEC as its precondition. **Vocabulary before
surfaces** (Phase 2): privacy and admin-section policy must exist before anything reads or gates on them,
and the **character lifecycle column** and **normalized-name unique index** are each load-bearing for two
later phases, so they land once here rather than being rediscovered per consumer. **Domain before facade
before UI** (Phases 3 → 4 → 5) mirrors the shipped scenes path exactly. **Admin goes last** (Phase 6)
because it consumes the most and because `internal/web/` contains **zero `RoleAdmin` references today** —
a net-new trust boundary with no existing test suite that would notice if it were wrong.

Phase 3 is *planning*-parallelizable with Phase 2, and its `Retire` needs Phase 2's lifecycle column.
(The former `Rename` ordering constraint — MUST NOT land before Phase 2's unique index, since it adds a
writer to a live check-then-insert race — moves with `Rename` to Phase 999.20; Phase 2's index has since
shipped, so it is satisfied either way.)

**Scheduling note (not a dependency):** `WebCheckSessionResponse.roles` (ADMIN-08) can be pulled into
Phase 4's proto work to avoid a second `web.proto` regeneration cycle in Phase 6. Likewise, Phase 4
should land the full SPEC-defined profile field set — including the PROFILE-06..09 fields that Phase 5
verifies — in one regeneration pass.

**Research posture.** Phases 3 and 6 carry `--research-phase` flags (below); Phase 2 needs a narrow
data audit only. Phases 1, 4, and 5 have well-understood patterns and should skip research — Phase 1 is
synthesis of `research/SUMMARY.md` rather than new research, Phase 4 is a verbatim copy of a fully-traced
shipped path (`internal/grpc/sceneaccess_service.go`), and Phase 5 is established shadcn/runes patterns
plus the existing `web/src/lib/scenes/createFlow.ts` idiom.

**PORTAL-10 is binding on every phase.** The six verification-integrity rules (census with set equality;
paired positive controls on every denial test; assertions against marshaled response bytes; gates
demonstrated RED against the pre-fix state; wire-level assertion of every opacity and authorization contract; invariant-scope
discipline) are SPEC content, not a capability — but every phase plan below carries them as acceptance
criteria. v0.12's audit catalogued 17 instances of *"a verification that cannot fail"*, and research found
that the natural test for nearly every privacy and authorization property in this milestone **passes while
the property is false**.

## Progress

> **Reading this table.** Phase numbers **restart at 1 per milestone as of v0.13**; v0.11 and v0.12 used the
> retired continuous global numbering and are **not** renumbered. The `Phase` column is therefore only
> unique *within* a milestone — always read it together with the `Milestone` column (rows `1.`–`9.` under
> v0.11/v0.12 are archived phases; rows `1.`–`6.` under v0.13 are the active milestone).

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Channels Subsystem | v0.11 | 6/6 | In Progress|  |
| 2. Scenes Lineage Completion | v0.11 | 13/13 | In Progress|  |
| 3. Platform Hardening & Deployment Scaling | v0.11 | 6/6 | Complete    | 2026-08-10 |
| 4. World-Model Resilience Investigation & Decision (F1) | v0.12 | 9/9 | In Progress|  |
| 5. World-Model Integrity Fixes (M2/M12) | v0.12 | 7/8 | In Progress|  |
| 6. Operational Hardening & Assurance Gates | v0.12 | 4/4 | Complete    | 2026-08-14 |
| 7. Event-Model & Bootstrap Decomposition | v0.12 | 11/11 | Complete    | 2026-07-18 |
| 8. God-Object Decomposition | v0.12 | 9/9 | Complete   | 2026-07-19 |
| 9. Test-Quality & Code-Health Sweep | v0.12 | 21/21 [^p9] | Complete | 2026-07-27 |
| 1. Portal SPEC | v0.13 | 0/TBD | Not started | - |
| 2. ABAC & Schema Vocabulary | v0.13 | 0/TBD | Not started | - |
| 3. World Character Commands | v0.13 | 0/TBD | Not started | - |
| 4. Shared Facade Helpers & CharacterAccessService | v0.13 | 9/9 | Complete    | 2026-08-11 |
| 5. Character Identity UI & Public Profiles | v0.13 | 8/8 | Complete    | 2026-08-13 |
| 6. Admin Portal Shell & Character Administration | v0.13 | 0/TBD | Not started | - |

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
>   other children were already separately filed (#4643, #4728) or shipped (`.19` → v0.11 Phase 2).
> - **999.2 was 8 items and is actually 1.** Seven were Epic 10's *pre-implementation* breakdown
>   and shipped in v0.11 Phase 1; only full-text search remains. See its entry.
> - **999.11's per-scope counts had drifted** and buried CRYPTO (102 pending, 40% of the total)
>   as a "long tail". Recounted in its entry.
>
> **Generalized lesson — beads-migrated item counts are an UPPER bound, not a residual.** Epics
> were migrated as whole clusters *including* their build-the-schema/build-the-commands children,
> so an epic that was subsequently implemented still lists them as pending. The count is exactly
> the number that inflates when sizing a milestone. Re-verify against the tree before promoting
> any 999.x cluster sourced from the beads migration.

> **v0.13 spine (selected 2026-07-31, PARTIALLY PROMOTED 2026-07-31):** **999.1 Web Client Portal
> completion** was the only player-facing cluster with substantial *verified* unbuilt scope (6
> concrete items: offline/PWA #4803, wiki/help, character profiles, character creation UI, admin
> portal, DM web surface). `/gsd-new-milestone` promoted **two and a half of the six** into milestone
> v0.13 (Phases 1–6): character creation/management UI (`qve.15`, non-roster), public character
> profiles (`qve.9`, no avatars), and the **shell only** of the admin portal (`qve.10`) — taken so
> character administration has a home. **Still in the backlog:** offline/PWA (`qve.7`), wiki
> (`qve.8`), web DMs (`qve.17`), and the six non-character admin sections (which overlap 999.8).
> See the 999.1 entry below for the residual.

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

### Phase 999.1: Web Client Portal completion (BACKLOG — PARTIALLY PROMOTED to v0.13)

**Goal:** Round out the web portal beyond scenes: offline support, wiki/help pages, character profiles + creation/management UI, admin portal, and a web surface for 1:1 direct messages.
**Source:** beads migration — 7 item(s) incl. epic(s) `holomush-qve`; member list in TRIAGE.md
**Related issues:** arch-review F6 PWA/offline #4803 (overlaps the offline-support + web-surface goals).
**Promoted to v0.13 (2026-07-31):** `qve.15` character creation + management UI (non-roster scope — roster integration stays deferred to 999.6), `qve.9` public character profiles (per-field privacy; avatars deferred to 999.16), and `qve.10` admin portal **shell + character administration only**.
**Residual (still backlog):** `qve.7` offline support / PWA (#4803), `qve.8` wiki portal, `qve.17` web surface for 1:1 direct messages, and the six non-character admin sections — stats, player management, moderation, audit viewer, config editor, plugin management — which overlap 999.8 and ship in v0.13 as *registered, role-gated, `NOT_IMPLEMENTED`-after-the-gate* stubs so wiring one later replaces a handler body rather than adding a check.
**Requirements:** v0.13 portion → IDENT-*/PROFILE-*/ADMIN-*/EXT-* (see `REQUIREMENTS.md`); residual TBD
**Plans:** 0 plans (residual)

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.2: Channels — remaining scope (BACKLOG)

**Goal:** Full-text search over channel message history. **This is the only member of the
original 8-item cluster that v0.11 Phase 1 did not deliver.**
**Scope reconciled 2026-07-31** (the entry's own "verify against what v0.11 Phase 1 delivered"
caution, finally acted on). The other 7 beads members were Epic 10's *pre-implementation*
breakdown and shipped in v0.11 Phase 1 (10/10 plans, 2026-07-09), verified against the tree:
`.3` schema → `migrations/000001_channels.up.sql`; `.4` join/leave/list/say →
`commands.go:63-85`; `.5` channel types → `types.go`; `.6` moderation mute/ban/ops →
`mute`/`ban`/`kick`/`transfer` subcommands + `moderate-own-channel` policy; `.7` history +
replay-on-join → `history` subcommand; `.2` implementation plan → consumed by v0.11 Phase 1.
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

### Phase 999.8: Admin Web UI & Config (BACKLOG — substrate consumed by v0.13)

**Goal:** Operator tools: /admin route, server stats, player management, config surface (epics holomush-g4pb + holomush-7nub; overlaps the web-portal admin page — consolidate at design time).
**Source:** beads migration — 3 item(s) incl. epic(s) `holomush-g4pb`; member list in TRIAGE.md
**Consumed by v0.13 (2026-07-31):** the `/admin` route, its ABAC trust boundary (`admin_section:` resource + `seed:admin-section-access`, which covers every future section at zero policy cost), the section registry, and the shared authorization helper. The "consolidate at design time" note this entry carried is now discharged — v0.13's Phase 1 SPEC is that design point.
**Residual (still backlog):** the six section *bodies* — server stats, player management, moderation, audit log viewer, config editor, plugin management. v0.13 ships each **registered, role-gated, returning `NOT_IMPLEMENTED` after the gate**, so implementing one later replaces a handler body rather than standing up a surface. Audit *emission* (with before-values) also lands in v0.13, so the deferred audit viewer inherits real history rather than launching empty.
**Requirements:** TBD (residual)
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

### Phase 999.20: Character Rename & the Approval Dimension (BACKLOG)

**Goal:** Give the character identity model an **approval dimension**, then land
`world.Service.RenameCharacter` on top of it — renaming permitted only while a character
is not yet approved for play, with the core name immutable afterwards.
**Source:** removed from v0.13 Phase 3 on 2026-08-06 during `/gsd-discuss-phase 3`; full
rationale and evidence in `.planning/phases/03-world-character-commands/03-CONTEXT.md`
(D-44, plus the deferred-ideas section).
**Related:** Phase 999.6 (Character Rostering & Transfer) — rostering is defined in
REQUIREMENTS v2 as "a distinct transition *out of* retired, which is why retire must not
release the name", so the approval axis and the rostering transition must be designed
together, not separately.
**Requirements:** IDENT-03 (moved out of v0.13); the rename half of IDENT-10.
**Plans:** 0 plans

Design inputs this item MUST carry:

- **The approval dimension does not exist.** `characters.status` is `active|retired|idle`
  only (`000054_character_identity_and_lifecycle.sql`). "Rename only before approval" has
  no state to read; the honest substitute — "has this character ever emitted a comm
  payload" — means querying the audit log to authorize a write.

- **Two naming frictions.** `rostered` collides with 999.6's reserved meaning, and
  `retired` is a lifecycle value 01-SPEC §4.4 deliberately keeps distinct from `purge` —
  do not merge the approval axis into the lifecycle axis. Separately, a `session_status`
  column on `characters` would **duplicate** an existing one: session status already lives
  on the session row (`internal/session/session.go:21`).

- **Blast radius.** Bound `INV-WORLD-5` (closed vocabulary + exhaustive-switch), 01-SPEC
  §4.4, PORTAL-04, character creation's initial state, and an approving actor (an admin
  surface, so it interacts with Phase 6).

- **The substrate is already built.** `CharacterRepository.Rename`
  (`internal/world/postgres/character_repo.go:212`) is version-guarded, runs `guardSkeleton`
  with self-exclusion, and writes its outbox envelope inside its own transaction. Its doc
  comment carries the rule: it MUST NOT be routed through `worldMutator.mutate()` (two
  envelopes for one rename). No `character_renamed` taxonomy kind exists yet — the shipped
  operator CLI emits rename as `character_updated`; reconcile rather than duplicate.

- **`INV-WORLD-6` must be resolved here.** Its text claims a name becomes claimable again
  only via a tombstone-emitting hard delete over exactly two paths, but rename frees the
  old `normalized_name` — and the shipped operator CLI already does so in production. Its
  binding test never exercises rename. Options recorded in 03-CONTEXT.md: narrow it to
  lifecycle transitions, widen the enumeration, or add a former-names reservation table
  (which closes identity inheritance while still permitting rename).

- **Read `01-SPEC.md` §5 first** — the name-capture surface inventory, six frozen sites vs
  five live ones, is what makes any rename argument checkable.

- **UI consequences.** Sketch 004's `Rename…` affordance and sketch 009's "names are
  reserved, not permanent" copy both assume rename ships; both are false for v0.13 and
  become true again only with this item.

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)
