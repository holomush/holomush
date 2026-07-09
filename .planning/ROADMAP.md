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
- [ ] **Phase 2: Scenes Lineage Completion** - Notifications and telnet polish for the shipped Scenes subsystem (templates descoped to backlog)
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
**Plans**: 3/7 plans executed

Plans:
**Wave 1**

- [x] 02-01-PLAN.md — Telnet activity nudge + shared `[>GAME: …]` primitive + INV-SCENE-70 (SCENEFWD-02)
- [x] 02-02-PLAN.md — Scene notify-prefs store + migration (mute + notify pref + `mode` digest seam) (SCENEFWD-02)

**Wave 2** *(blocked on Wave 1)*

- [x] 02-03-PLAN.md — Plugin mute RPCs + telnet `scene mute`/`unmute` command + participant DSL policy (SCENEFWD-02)

**Wave 3** *(blocked on Wave 2)*

- [ ] 02-04-PLAN.md — Core mute-suppression at badge downgrade (dependency-inverted SceneMuteChecker, TTL cache) (SCENEFWD-02)
- [ ] 02-05-PLAN.md — Web mute/prefs typed 4-layer slice (WebMuteScene → facade → BFF → notifyFlow.ts) (SCENEFWD-02)
- [ ] 02-06-PLAN.md — Idle-timeout lifecycle (active→paused sweep) + optional idle nudge (OFF) + INV-SCENE-71 (SCENEFWD-02)

**Wave 4** *(blocked on Wave 3)*

- [ ] 02-07-PLAN.md — Telnet edge cases: mixed-render branch + reconnect focus restore + multi-char no-leak (SCENEFWD-03)

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

**Plans**: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 (no hard dependency order between them — Channels, Scenes lineage
completion, and platform hardening can proceed in parallel if desired)

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Channels Subsystem | 10/10 | Complete    | 2026-07-09 |
| 2. Scenes Lineage Completion | 3/7 | In Progress|  |
| 3. Platform Hardening & Deployment Scaling | 0/TBD | Not started | - |

## Deferred (Not in This Roadmap)

See `REQUIREMENTS.md` "v2 Requirements" for full detail:

- **Forums integration** (Epic 11, `holomush-djj`) — no design exists yet
- **Discord/Slack bridging + OAuth linking** (Epic 12) — depends on Phase 1 (Channels) + an unbuilt OAuth substrate
- **Non-scene web-portal surfaces** (world/building editing, admin UI) — directional `theme:web-portals` goal,
  not yet backed by a SPEC; route through `/gsd-spec-phase` before roadmapping
