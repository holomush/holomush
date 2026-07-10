# Roadmap: HoloMUSH

## Overview

HoloMUSH is a mature, actively-developed platform — the event-sourced core, ABAC access control, dual-protocol
(telnet + web) gateways, two-tier plugin host, and the flagship Scenes/RP subsystem (Epic 9, all 17 ingested
specs through the 2026-07-05 focus-routed-input design) are already shipped and running. This roadmap covers
only the genuine **forward** work identified from the 48-SPEC ingest, the invariant registry, and
`docs/roadmap.md`'s active theme narratives: standing up Channels as the social-spaces substrate's second
consumer, closing out the remaining Scenes lineage items (notifications and telnet polish; templates deferred to backlog), and
hardening the platform for real multi-node/production deployment. Forums and Discord integration remain
explicitly deferred pending their own designs (see REQUIREMENTS.md v2).

## Shipped Foundation (Context — Not Executable Roadmap Phases)

The following is already built and running. It is NOT re-planned here; each item traces to a source SPEC in
`REQUIREMENTS.md`'s "Shipped Foundation" section and to `docs/roadmap.md`'s "Completed themes". Downstream
tools should treat this as historical context, not phase-parseable roadmap content.

- **Foundational architecture** — event-sourced Go core, two-tier Lua/binary plugin runtime with enforced
  symmetry, dual-protocol (telnet + web) gateways, command dispatcher with two-layer ABAC, config system.

- **Access control** — Cedar-aligned default-deny ABAC (`AccessPolicyEngine`, policy DSL, attribute
  providers).

- **Auth, identity & sessions** — argon2id auth, cross-protocol Postgres session persistence, derived session
  liveness, current-state presence snapshot.

- **Scenes & RP subsystem (Epic 9)** — plugin-owned `core-scenes`: membership, focus model, content streams,
  publish-vote privacy pipeline, scene board + content warnings, bare-ULID identity, full web workspace
  (create/manage/publish-vote), focus-routed conversational input. This is the social-spaces substrate's
  reference implementation and forced the JetStream cutover + crypto production activation.

- **Event bus, crypto & wire conventions** — NATS JetStream event bus (JetStream-owned ordering, ULID
  identity-only), sensitive-payload DEK/KEK encryption (KEK mandatory to boot), canonical wire/content-payload
  conventions, central invariant registry.

- **Plugin-capability-architecture** (epic `holomush-eykuh`) — SHIPPED; capability-scoped `host.v1` services,
  least-privilege manifest gates, fail-closed-at-load enforcement. Kept active only for a `bd`-tracked P3
  polish tail, not part of this roadmap.

- **Web portal shell** — unified `(authed)` layout, shared `CommLine` rendering seam across terminal and
  scenes workspace.

## Phases

**Phase Numbering:**

- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

- [x] **Phase 1: Channels Subsystem** - Stand up `core-channels` as the social-spaces substrate's second consumer (completed 2026-07-09)
- [x] **Phase 2: Scenes Lineage Completion** - Notifications and telnet polish for the shipped Scenes subsystem (templates descoped to backlog) (completed 2026-07-09)
- [ ] **Phase 3: Platform Hardening & Deployment Scaling** - External/clustered NATS, multi-node crypto invalidation, audit durability

## Phase Details

### Phase 1: Channels Subsystem

**Goal**: Players can communicate via persistent named channels, independent of physical location, with the
same substrate guarantees (EventBus, ABAC, audit) already proven by Scenes.
**Depends on**: Nothing new — consumes the already-shipped EventBus/ABAC/plugin-host substrate
**Requirements**: CHAN-01, CHAN-02, CHAN-03, CHAN-04, CHAN-05
**Success Criteria** (what must be TRUE):

1. Player can join, leave, and list persistent named channels independent of the spatial world model
2. Player can post to and read history from channels they are a member of, gated by ABAC channel-membership
   policies (default-deny, consistent with every other subsystem)

3. Channel events flow through the shared EventBus substrate with the same JetStream/audit guarantees as
   scenes

4. Faction-restricted channels enforce membership-based access distinct from open channels
5. `core-channels` validates the `eventkit`/`groupkit` SDK extraction pattern as the substrate's second
   consumer (INV-S7, N=2 rule) — extraction itself is a follow-on, not a blocking criterion of this phase
**Plans**: 10/10 plans complete

Plans:
**Wave 1**

- [x] 01-01-PLAN.md — holomush.channel.v1 proto + generated bindings (wave 1)
- [x] 01-02-PLAN.md — Live-delivery substrate: SDK QuerySessionStreams hook + dot-subject acceptance (HIGH-1) + stream.subscription served with real LIVE_ONLY (HIGH-2) + concrete-stream authz guard (HIGH-3) [holomush-l6std] (wave 1)
- [x] 01-03-PLAN.md — Plugin scaffold + schema/migrations + types + store (wave 1)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 01-04-PLAN.md — ChannelResolver (resource-side membership, D-03) + ABAC seed policies (incl. write-channel-as-member, MED-5) (wave 2)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 01-05-PLAN.md — ChannelService create/join/leave/list + admin-gated rate-limited create (wave 3)

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 01-06-PLAN.md — Channel emit (CommunicationContent) + membership-gated audit/history (wave 4)

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 01-05b-PLAN.md — Remaining ChannelService RPCs: post/who/history/invite/mute/ban/kick/transfer (completes the service surface, HIGH-4) (wave 5)

**Wave 6** *(blocked on Wave 5 completion)*

- [x] 01-07-PLAN.md — channel commands + =name shorthand (manifest alias, MED-6) + moderation + retention prune (wave 6)

**Wave 7** *(blocked on Wave 6 completion)*

- [x] 01-08-PLAN.md — Live delivery wiring: QuerySessionStreams impl + mid-session join/leave (wave 7)

**Wave 8** *(blocked on Wave 7 completion)*

- [x] 01-09-PLAN.md — Whole-system census + e2e integration + invariant registration (wave 8)

### Phase 2: Scenes Lineage Completion

**Goal**: The shipped Scenes/RP subsystem reaches the remainder of its designed scope — activity
notifications and telnet edge-case hardening — beyond the reference-implementation core already delivered
through the 2026-07-05 focus-routed-input design. (Scene templates, originally SCENEFWD-01, were descoped to
backlog on 2026-07-08 — bd `holomush-x4n1r`, P4 — as not actively pursued at this time.)
**Depends on**: Nothing new — extends the already-shipped `core-scenes` plugin
**Requirements**: SCENEFWD-02, SCENEFWD-03
**Success Criteria** (what must be TRUE):

1. Player receives a notification when a scene they participate in or are invited to has activity requiring
   their attention — on both web (already shipped) and telnet (via a throttled `[>GAME: …]` nudge line)

2. Telnet scene commands handle previously-identified edge cases (mixed focused/skipped render branches,
   reconnection membership+focus restore, multi-character-per-connection) without silent failure
**Plans**: 6/7 plans executed

Plans:
**Wave 1**

- [x] 02-01-PLAN.md — Telnet activity nudge + shared `[>GAME: …]` primitive + INV-SCENE-70 (SCENEFWD-02)
- [x] 02-02-PLAN.md — Scene notify-prefs store + migration (mute + notify pref + `mode` digest seam) (SCENEFWD-02)

**Wave 2** *(blocked on Wave 1)*

- [x] 02-03-PLAN.md — Plugin mute RPCs + telnet `scene mute`/`unmute` command + participant DSL policy (SCENEFWD-02)

**Wave 3** *(blocked on Wave 2)*

- [x] 02-04-PLAN.md — Core mute-suppression at badge downgrade (dependency-inverted SceneMuteChecker, TTL cache) (SCENEFWD-02)
- [x] 02-05-PLAN.md — Web mute/prefs typed 4-layer slice (WebMuteScene → facade → BFF → notifyFlow.ts) (SCENEFWD-02)
- [x] 02-06-PLAN.md — Idle-timeout lifecycle (active→paused sweep) + optional idle nudge (OFF) + INV-SCENE-71 (SCENEFWD-02)

**Wave 4** *(blocked on Wave 3)*

- [x] 02-07-PLAN.md — Telnet edge cases: mixed-render branch + reconnect focus restore + multi-char no-leak (SCENEFWD-03)

**UI hint**: yes

### Phase 3: Platform Hardening & Deployment Scaling

**Goal**: HoloMUSH can be deployed as a horizontally-scaled, multi-node cluster with a durable audit pipeline,
closing the single-node ceiling flagged in `.planning/codebase/CONCERNS.md`.
**Depends on**: Nothing new — extends the already-shipped EventBus/crypto substrate
**Requirements**: CLUSTER-01, CLUSTER-02, CLUSTER-03, CLUSTER-04, CLUSTER-05
**Success Criteria** (what must be TRUE):

1. Operator can deploy HoloMUSH's event bus against external/clustered NATS JetStream instead of only
   embedded in-process mode

2. Server-account subject scoping enforces single-principal publish/subscribe on game-topic subjects
   (`events.>`, `audit.>`, `internal.>`) in external mode

3. The crypto key-invalidation coordinator correctly propagates rotation events across real multi-node
   replicas, not just the embedded single-node path

4. Audit messages that exhaust `MaxDeliver` land in a dead-letter queue instead of being silently dropped
5. Operator has a documented runbook for external-NATS deployment

**Plans**: 8/9 plans executed

Plans:
**Wave 1** *(foundation — no deps)*

- [x] 03-01-PLAN.md — EventBus config reconciliation (ModeExternal/URL/Credentials/TLS/Provision/DLQ) + fail-closed `Validate()` (CLUSTER-01)
- [x] 03-02-PLAN.md — External-NATS testcontainer harness (per-replica conns) + test-tier rule amendment (CLUSTER-03 substrate)

**Wave 2** *(build on config + harness)*

- [x] 03-03-PLAN.md — External mode connect branch + provision opt-out + fail-closed boot (CLUSTER-01)
- [x] 03-04-PLAN.md — Audit DLQ capture helper + projection Term/Nak hook + metric (CLUSTER-04)

**Wave 3** *(verification + operator assets)*

- [x] 03-05-PLAN.md — Multi-node crypto invalidation + hung-replica probe-pill + invariant capstone (CLUSTER-03)
- [x] 03-06-PLAN.md — Single-principal account scoping: deploy/nats templates + verify script + boot self-check (CLUSTER-02)
- [x] 03-07-PLAN.md — DLQ replay CLI (`holomush audit dlq {list,show,replay}`) (CLUSTER-04)
- [x] 03-08-PLAN.md — compose.cluster.yaml overlay + multi-process cluster smoke (CLUSTER-03/05)

**Wave 4** *(capstone)*

- [ ] 03-09-PLAN.md — External-NATS operator runbook (CLUSTER-05)

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 (no hard dependency order between them — Channels, Scenes lineage
completion, and platform hardening can proceed in parallel if desired)

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Channels Subsystem | 10/10 | Complete    | 2026-07-09 |
| 2. Scenes Lineage Completion | 7/7 | Complete    | 2026-07-09 |
| 3. Platform Hardening & Deployment Scaling | 8/9 | In Progress|  |

## Deferred (Not in This Roadmap)

See `REQUIREMENTS.md` "v2 Requirements" for full detail. Deferred strategic
clusters now live as first-class parking-lot entries in the `## Backlog`
section below (Forums → 999.4, Discord → 999.5, non-scene web-portal
surfaces → 999.1/999.8) — route each through `/gsd-spec-phase` before
roadmapping.

## Backlog

Strategic clusters consolidated from the beads → GitHub Issues migration
(2026-07-09). Member-level detail: [`.planning/archive/beads/TRIAGE.md`](archive/beads/TRIAGE.md).
Promote an entry with `/gsd-review-backlog` when ready.

### Phase 999.1: Web Client Portal completion (BACKLOG)

**Goal:** Round out the web portal beyond scenes: offline support, wiki/help pages, character profiles + creation/management UI, admin portal, and a web surface for 1:1 direct messages.
**Source:** beads migration — 7 item(s) incl. epic(s) `holomush-qve`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

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
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.10: Code health & test-quality program (BACKLOG)

**Goal:** Codebase humanization/de-slop, ACE naming violations, weak/skeleton tests, security polish batch, coverage backfill on Phase-1.5 infra packages, session-lifecycle test matrix.
**Source:** beads migration — 8 item(s) incl. epic(s) `holomush-ec22`, `holomush-89o9`; member list in TRIAGE.md
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
