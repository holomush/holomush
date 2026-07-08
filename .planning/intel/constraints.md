# Constraints (from SPECs)

48 SPEC-classified documents, all `confidence: high`, none `locked`, none carrying a
per-doc `precedence` override (default `ADR > SPEC > PRD > DOC` applies uniformly across
this set). Grouped by domain for navigability; chronological order preserved within each
group. Every entry cites `source:` for traceability.

---

## 1. Foundational architecture

### HoloMUSH Architecture Design (ARCHIVED)

- **source:** `docs/plans/archive/2026-01-17-holomush-architecture-design.md`
- **type:** nfr (historical framing)
- **status:** Archived — superseded by `docs/roadmap.md` and later specs. The classifier
  flagged this explicitly: "historical context only, not current constraints." Original
  framing proposed a WASM plugin system; this was abandoned in favor of the Lua +
  go-plugin two-tier model one day later (see next entry). Retained for provenance only —
  do NOT treat as an active constraint.
- **scope:** Go core, event-oriented architecture, telnet adapter, web SvelteKit PWA, WASM
  plugin system (superseded), session persistence, spec-driven/TDD workflow.

### HoloMUSH Roadmap Design

- **source:** `docs/plans/2026-01-18-holomush-roadmap-design.md`
- **type:** nfr
- **constraint:** Defines the development roadmap as iterative epics with phase gates.
  Key architecture decisions locked in at this stage: plugin runtime (two-tier
  Lua/go-plugin), password hashing (argon2id), command prefixes, and ABAC access control
  as the authorization model (replacing static roles).
- **scope:** roadmap, epics, phase gates, plugin architecture, Lua runtime, go-plugin,
  access control, ABAC, event-oriented architecture, PWA web client, telnet protocol.

### Plugin System Design

- **source:** `docs/specs/2026-01-18-plugin-system-design.md`
- **type:** api-contract
- **constraint:** Defines the two-tier plugin system: Lua (gopher-lua) for lightweight
  scripts, go-plugin (hashicorp/go-plugin, process-isolated) for complex extensions.
  Establishes `PluginManager`, `CapabilityEnforcer`, `PluginHost`, `HostFunctions` as the
  core abstractions, plus OTel tracing requirements for observability. This is the
  foundational plugin contract that the entire `plugin-capability-*` epic (2026-06)
  later decomposes/refines without contradicting.
- **scope:** plugin system, Lua scripts, go-plugin, PluginManager, CapabilityEnforcer,
  PluginHost, HostFunctions, capability model, OTel tracing.

### World Model Architecture Design

- **source:** `docs/specs/2026-01-22-world-model-design.md`
- **type:** schema
- **constraint:** Defines HoloMUSH's hybrid world model: persistent locations + temporary
  "scene rooms," a PostgreSQL schema using recursive CTEs (AGE-compatible), bidirectional
  exits, object containment, and event-driven state changes with ABAC integration. At
  this point in the corpus, "scenes" are modeled as `locations` table rows
  (`type='scene'`) owned by a core `internal/world.SceneRepository`, with a
  `scene_participants` table FK'd to `locations(id)`.
  **See `INGEST-CONFLICTS.md` WARNING 1** — this model is architecturally superseded by
  the later plugin-owned scenes model (Scenes v2 onward) but no doc in this corpus names
  this spec directly as superseded; flagged for user confirmation.
- **scope:** world model, locations, exits, objects, scenes (as location-type rows),
  PostgreSQL schema, recursive CTEs, event-driven world state, ABAC world permissions,
  `internal/world` package.

### Auth & Identity Architecture Design

- **source:** `docs/specs/2026-01-25-auth-identity-design.md`
- **type:** schema
- **constraint:** Specifies argon2id password auth, separate telnet/web auth flows,
  player-character separation, DB-backed web sessions with signed tokens, and
  progressive rate limiting.
- **scope:** authentication, player accounts, characters, web sessions, password
  hashing, rate limiting, token design, database schema.

### Commands & Behaviors Architecture

- **source:** `docs/specs/2026-02-02-commands-behaviors-design.md`
- **type:** api-contract
- **constraint:** Specifies the command system: input parsing, alias resolution, unified
  Go/Lua command dispatch, capability-gated ABAC security at dispatch, and help-system
  integration. This is the dispatcher later extended (not contradicted) by
  `2026-07-05-focus-routed-scene-input-design.md`'s `focus_redirects` mechanism.
- **scope:** command registry, command dispatch, alias resolution, capability gating,
  event emission, help system, Lua plugin commands, protocol-agnostic parser.

### Server Configuration File System Design

- **source:** `docs/specs/2026-03-17-config-file-system-design.md`
- **type:** nfr
- **constraint:** YAML config file support via koanf; two-layer precedence (config file <
  CLI flags); single sectioned `config.yaml` at a well-known XDG path. Explicit
  non-goals: no env-var mapping, no hot-reload, no `init` command, no remote config
  sources, no secret management.
- **scope:** config file loading, koanf, CLI flags precedence, XDG config path,
  `config.yaml` sections, operator documentation, epic `holomush-eagr`.

---

## 2. ABAC / Access control

### Full ABAC Architecture Design — Overview

- **source:** `docs/specs/abac/00-overview.md`
- **type:** protocol
- **constraint:** Defines the full ABAC architecture replacing static roles with a
  Cedar-inspired policy DSL: `AccessPolicyEngine`, policy DSL, attributes, properties,
  locks, seed policies, audit logging, policy CRUD admin commands. Foundational contract
  the entire access-control corpus builds on.
- **scope:** ABAC engine, AccessPolicyEngine, policy DSL, attributes, properties, locks,
  seed policies, audit logging, policy CRUD admin commands.

### Full ABAC Architecture Design — Index

- **source:** `docs/specs/2026-02-05-full-abac-design.md`
- **type:** protocol
- **constraint:** Index/overview splitting the Full ABAC spec into nine standalone
  sections (core types, policy DSL, property model, resolution, storage/audit,
  layers/commands, migration, testing). Not itself a technical constraint — a
  navigational index over `docs/specs/abac/*.md` (only `00-overview.md` of that set was
  independently classified in this batch).
- **scope:** ABAC, AccessPolicyEngine, Policy DSL, Property Model, Attribute Providers,
  Audit Log, Lock Syntax, Seed Policies, Plugin Capability Migration.

---

## 3. Web client & session persistence

### Web Client Adapter & SvelteKit Scaffold Design

- **source:** `docs/specs/2026-03-18-web-client-adapter-design.md`
- **type:** api-contract
- **constraint:** Specifies the ConnectRPC HTTP adapter in the gateway process, a
  web-facing protobuf `WebService`, and a minimal embedded SvelteKit scaffold proving the
  browser-to-core pipeline end-to-end. Sub-spec 1 of 3 for Epic 8 (Web Client) —
  scaffolding/adapter only, explicitly excludes terminal/chat UX and portal pages. This
  is the origin of the gateway-boundary invariant (protocol translation only, no direct
  service access) later codified in `.claude/rules/gateway-boundary.md`.
- **scope:** ConnectRPC gateway adapter, WebService protobuf, SvelteKit scaffold,
  `go:embed` static assets, `--web-dir` flag, Playwright E2E test, `CharacterRef` core
  struct, gateway boundary.

### Server-Side Session Persistence Design

- **source:** `docs/specs/2026-03-19-session-persistence-design.md`
- **type:** schema
- **constraint:** Sub-spec 2a of Epic 8: Postgres-backed `SessionStore` interface for
  durable, cross-protocol (telnet/web) sessions; gap-free event replay on reconnect;
  server-side command history; per-role TTL/history limits resolved via ABAC. This is
  the base session model later extended (not contradicted) by the derived-liveness layer
  in `2026-05-30-session-liveness-and-gateway-survival-design.md` (see INFO note in
  `INGEST-CONFLICTS.md`).
- **scope:** SessionStore interface, Postgres session persistence, session
  detach/reattach TTL, event replay on reconnect, command history, cross-protocol
  session continuity, ABAC-resolved per-role limits, proto RPCs for session
  listing/reattachment/history.

---

## 4. Channels

### Channels Architecture Design

- **source:** `docs/specs/2026-04-03-channels-architecture.md`
- **type:** schema
- **constraint:** Data model, storage, ABAC authorization, event-stream integration, and
  command surface for persistent named channels, independent of the spatial world model
  (consistent with the later social-spaces substrate-vs-use boundary — see
  `2026-05-16-social-spaces-substrate-contract.md`). Includes Discord/Slack bridging and
  faction channels.
- **scope:** channels, event store, ABAC policies, verb registry, channel membership,
  channel history, Discord/Slack bridging, faction channels.

---

## 5. Scenes / roleplay subsystem (Epic 9, `holomush-5rh`)

This is the largest coherent sub-corpus (15 specs). It runs as a single incremental
epic: v2 base design → membership → adoption → phase 4-8 → identity fix → crypto
activation → name resolution → web slices. No two docs in this sub-series contradict
each other on overlapping scope; each explicitly builds on/references its predecessor
(confirmed acyclic by cross-ref graph traversal). Presented chronologically.

### Scenes & RP Architecture Design (v2)

- **source:** `docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md`
- **type:** api-contract
- **constraint:** Re-architects the scenes/RP subsystem as a plugin-owned binary plugin
  (`core-scenes`) — domain model, ABAC enforcement, gRPC `SceneService`, plugin
  architecture, observability. Supersedes an earlier v1 server-centric design (not in
  this ingest batch). Establishes scenes as living OUTSIDE `internal/world`/`locations`
  entirely, in the plugin's own schema — in tension with the earlier
  `2026-01-22-world-model-design.md` locations-table scene model (flagged as WARNING 1).
- **scope:** scenes, roleplay, core-scenes plugin, SceneService gRPC, ABAC enforcement,
  plugin architecture, IC/OOC event streams, focus model, Epic 9.

### Scenes Phase 3: Membership Design

- **source:** `docs/superpowers/specs/2026-04-07-scenes-phase-3-membership-design.md`
- **type:** api-contract
- **constraint:** Participant model, membership RPCs/store methods
  (Join/Leave/Invite/Kick/TransferOwnership), an append-only `scene_ops_events` audit
  journal, and ABAC policy replacement from owner-only to member-based access.
  `CreateWithOwner` replaces the Phase 1 `Create` store method — owner-row insertion,
  scene-row insertion, and the `lifecycle.created` ops event are one atomic transaction.
- **scope:** scene_participants table, scene_ops_events table, ParticipantRole enum,
  OpsEventKind enum, membership RPCs, ABAC member-based policies, scene commands,
  CreateWithOwner store method, resolver participants/invitees attributes.

### B10 — core-scenes Plugin Adoption Design

- **source:** `docs/superpowers/specs/2026-04-16-b10-core-scenes-adoption-design.md`
- **type:** api-contract
- **constraint:** Scopes a `FocusClient` SDK facade for binary plugins and
  core-scenes command-path wiring (join/leave/end/switch) integrating DB writes with
  focus RPCs. Explicitly excludes `session_id` proto changes, multi-session fan-out,
  auto-join, and Lua adoption from this slice.
- **scope:** core-scenes plugin, FocusClient SDK facade, focus substrate, scene commands
  (join/leave/end/switch), plugin host focus RPCs, SceneService.

### Scenes Phase 4 — Event streams + pose order

- **source:** `docs/superpowers/specs/2026-05-19-scenes-phase-4-streams-and-pose-order-design.md`
- **type:** api-contract
- **constraint:** Plugin-owned content emission (pose/say/emit/ooc), `crypto.emits` +
  `EmitTypeRegistrar` adoption, pose-order computation with a `GetPoseOrder` RPC, and
  IC/OOC notice events. Binds INV-P4-1..13 (now `INV-SCENE-1..13` in the registry — see
  `context.md`). Non-participants in the same physical location MUST NOT receive scene
  IC events (INV-SCENE-6, closing audit-finding `holomush-ac50`).
- **scope:** core-scenes plugin, event streams, pose order computation, GetPoseOrder RPC,
  crypto.emits manifest, EmitTypeRegistrar, scene IC/OOC subjects, INV-P4-1..13.

### Scenes Phase 5: Focus Model + Multi-Connection Visibility

- **source:** `docs/superpowers/specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md`
- **type:** api-contract
- **constraint:** Per-connection focus tracking and multi-connection visibility;
  extends `PluginHostService` with three new focus RPCs; adds scene focus/grid/list
  subcommands; wires focus changes to JetStream subscription churn. Focus-managed state
  mutations MUST apply atomically under a single `Store`-lock (INV-P5-1, INV-P5-7,
  INV-P5-20 in registry terms).
- **scope:** scenes, focus model, PluginHostService, session.Connection,
  SessionStreamRegistry, subscription_router, core-scenes plugin, session store
  (MemStore/Postgres), JetStream subscriptions.

### Scenes Phase 6: Logs, Publish Vote, and Hard Privacy Boundary

- **source:** `docs/superpowers/specs/2026-05-23-scenes-phase-6-logs-vote-privacy-design.md`
- **type:** api-contract
- **constraint:** Phase 6 publication artifact: a vote state machine, new gRPC RPCs and
  telnet/web commands, IC-stream event emission, a COOLOFF→PUBLISHED atomic snapshot
  pipeline, observability signals, and scene log replay/export commands, all gated by a
  **hard participant-only privacy boundary** (INV-SCENE-33/60: ABAC MUST NOT be consulted
  for scene-log/publication reads — the participant-only check is plugin-code-enforced
  and MUST run before any DB query).
- **scope:** scenes, published_scenes table, publish vote state machine, scene_log audit
  table, gRPC RPCs, telnet/web commands, IC-stream events, privacy boundary (INV-S9),
  snapshot pipeline, observability.

### Scene Bare-ULID Identity — Design

- **source:** `docs/superpowers/specs/2026-05-28-scene-bare-ulid-identity-design.md`
- **type:** api-contract (bugfix)
- **constraint:** Removes the undocumented `"scene-"` prefix from scene entity IDs
  (introduced silently in PR #200) so scenes mint bare ULIDs like every other world
  entity. Fixes three broken host boundaries (`streamToFocusKey`/`extractSceneID`, scene
  join/subscribe, privacy temporal floor) that failed to parse the prefixed ID. Binds
  INV-Y5INX-1..5 (`INV-SCENE-46..50`) — no production code path may strip a `scene-`
  prefix anywhere (the masking strip that hid the bug lived only in the test harness).
- **scope:** scene identifiers, ULID generation, event subject naming, ABAC resource-type
  prefix, scene join/subscribe, scene log, privacy temporal floor, core-scenes plugin.

### Scenes Phase 8 — Scene Board + Content Warnings

- **source:** `docs/superpowers/specs/2026-05-29-scenes-phase-8-board-content-warnings-design.md`
- **type:** api-contract
- **constraint:** Implements the `ListScenes` RPC and a browsable/filterable scene board,
  plus a game-overridable content-warning taxonomy with persistent player/character block
  preferences. Introduces a plugin-partitioned settings substrate on the host so
  content-warning preferences stay domain-ignorant at the plugin/host boundary. Every
  board row MUST display its content-warning labels regardless of active filters
  (INV-SCENE-56); resolution is safety-accumulating union across GAME/PLAYER/CHARACTER
  scope, not first-match-wins (INV-SCENE-58).
- **scope:** scene board, ListScenes RPC, content warnings, content-warning taxonomy,
  settings substrate, core-scenes plugin, player preferences, PluginHostService, scenes
  command.

### Scene DEK Genesis-on-First-Focus — Design

- **source:** `docs/superpowers/specs/2026-06-09-scene-dek-genesis-on-focus-design.md`
- **type:** api-contract (bugfix)
- **constraint:** Diagnoses and fixes a chicken-and-egg gap where `SetSceneFocus` fails
  on a fresh scene because the scene DEK is not yet genesised. Specifies that the focus
  path itself must trigger DEK genesis, seeded with the focusing reader as a participant
  (INV-CRYPTO-121 in the registry).
- **scope:** SetSceneFocus, dek.Manager, scene DEK genesis, AuthGuard,
  publisher.go initialParticipantsForContext, sceneaccess_service, event-payload crypto
  architecture.

### Sensitive-Event Crypto Activation — Design

- **source:** `docs/superpowers/specs/2026-06-09-sensitive-event-crypto-activation-design.md`
- **type:** nfr (security)
- **constraint:** Makes **KEK presence the single activation gate** for sensitive-event
  crypto across publish/subscribe (INV-CRYPTO-117/118 in registry); confirms character
  binding-row preconditions via tests; makes a provisioned KEK **mandatory to boot**
  (INV-CRYPTO-119) while keeping provisioning frictionless. Corrects an intermediate
  design (not in this ingest batch) that had made KEK-less deployment a legitimate silent
  unencrypted mode — this corpus's own `2026-04-25-event-payload-crypto-design.md` never
  asserted that KEK-less claim, so there is no in-corpus contradiction on this point.
- **scope:** sensitive-event crypto, KEK (key-encryption-key), DEK manager, AuthGuard,
  EventBus publisher/subscriber, character bindings, boot/provisioning flow.

### Scene Character Name Resolution — Design

- **source:** `docs/superpowers/specs/2026-06-20-scene-character-name-resolution-design.md`
- **type:** api-contract
- **constraint:** Host-side design resolving character ULIDs to display names in three
  web scene surfaces (roster, pose-order, pose author) via a non-ABAC name resolver,
  mirroring the `ListFocusPresence` pattern. No client or telnet changes required.
- **scope:** scene roster, pose-order strip, pose author, GetSceneForViewer,
  characterNameResolver, world.CharacterRepository, core-scenes plugin, gateway
  translateEvent, GameEvent.actor.

### Web Create-Scene — Design

- **source:** `docs/superpowers/specs/2026-06-19-web-create-scene-design.md`
- **type:** api-contract
- **constraint:** Restores web-based scene creation via a **typed RPC path** (proto →
  facade → BFF → client → UI). States explicitly that this **supersedes E9.5's
  command-path-only decision for structural writes** (E9.5 = `web-portal-scenes-design`,
  06-07, D4). Records the window-per-scene UI disposition. **See `INGEST-CONFLICTS.md`
  WARNING 2** — flagged for user confirmation because the superseded doc's own text is
  not marked superseded, even though `docs/roadmap.md`'s `theme:web-portals` section
  corroborates this correction narrative.
- **scope:** web create-scene affordance, typed RPC, SceneService.CreateScene, BFF
  facade, Scenes Portal, command path vs structural writes, window-per-scene UI.

### Scenes Web — Lifecycle & Management Actions Design

- **source:** `docs/superpowers/specs/2026-06-24-scenes-web-management-actions-design.md`
- **type:** api-contract
- **constraint:** Exposes scene lifecycle/management verbs (end/pause/resume,
  invite/kick/transfer, publish voting) to the web client via a typed BFF facade, closing
  an authorization gap: `SceneService` mutating RPC handlers MUST self-enforce ABAC per
  verb (INV-SCENE-65) rather than relying on the telnet command wrapper to gate first —
  this holds for both the telnet command path and the web `SceneAccessService` facade.
- **scope:** SceneService, SceneAccessService facade, WebService, scene lifecycle verbs,
  scene membership verbs, scene publish verbs, gateway-boundary invariant, INV-SCENE-33,
  INV-SCENE-63, INV-SCENE-64.

### Scenes Web — Publish-Vote Actions (slice 4/4) Design

- **source:** `docs/superpowers/specs/2026-06-28-scenes-web-publish-vote-actions-design.md`
- **type:** api-contract
- **constraint:** Exposes scene publish/vote/withdraw backend capabilities through
  facade, BFF, and web-client layers, plus a reactive publish panel UI — final slice of
  epic `holomush-5rh.24`.
- **scope:** scene publish, vote, withdraw, SceneAccessService facade, WebService BFF,
  web client scenes lib, SceneContextRail panel, scene_publish_* events, Playwright E2E.

### Publish-vote web: live event delivery design

- **source:** `docs/superpowers/specs/2026-06-29-publish-vote-web-live-event-delivery-design.md`
- **type:** api-contract
- **constraint:** Specifies refetch-on-event (Approach B) rather than client-side tally
  reconstruction, per ADR `holomush-o8gx8`'s snapshot-read + live-event split. Defines
  observer-vs-participant UX visibility rules; no host/proto/manifest changes required.
- **scope:** publish-vote web slice, scene_publish_* events, GetPublishedScene RPC,
  GetScene active-attempt pointer, workspaceStore.ingestEvent, translate.go event
  translation, observer vs participant UX.

### Publish-Vote Web — Interactive Controls Design

- **source:** `docs/superpowers/specs/2026-06-30-publish-vote-web-interactive-controls-design.md`
- **type:** api-contract
- **constraint:** Web write-path integration for publish-vote (start/cast/withdraw
  actions, store state, UI controls) over existing typed BFF RPCs, respecting
  no-caller-vote-data and no-payload-events backend constraints.
- **scope:** publish-vote feature, publishFlow.ts, publishStore, ScenePublishPanel,
  SceneContextRail, typed BFF RPC wrappers, scene lifecycle actions.

### Focus-Routed Scene Input — Design

- **source:** `docs/superpowers/specs/2026-07-05-focus-routed-scene-input-design.md`
- **type:** api-contract
- **constraint:** Top-level ambient conversational verbs (pose/say/ooc/emit, incl. sigil
  aliases) from a scene-focused connection MUST route to the focused scene's IC/OOC
  stream instead of the grid location — across telnet, web terminal, and web portal —
  while keeping the core dispatcher plugin-agnostic (uses the generic `focus_redirects`
  manifest mechanism, consumed by `command.WithFocusRedirects`; core owns no
  verb/focus vocabulary). A focus-read infra error during dispatch fails CLOSED
  (INV-SCENE-67) rather than leaking to the plaintext location stream.
- **scope:** core-communication dispatcher, core-scenes plugin, Connection.FocusKey,
  SceneComposer (web), sigil aliases, ABAC write-scene-as-participant gate,
  focus_redirects manifest mechanism.

---

## 6. Social-spaces substrate & presence

### theme:social-spaces — Substrate Contract Design

- **source:** `docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md`
- **type:** protocol
- **constraint:** Defines the substrate contract for the social-spaces theme (Scenes,
  Channels, Forums, Discord): what the EventBus/crypto/focus/plugin-host substrate
  provides, the substrate-vs-use boundary, `eventkit`/`groupkit` SDKs (named but
  deliberately unbuilt pending a second consumer), and validates the prior Scenes v2
  design against the current substrate. Codifies INV-S1–INV-S10 boundary invariants
  (substrate stays domain-free, Go+Lua runtime parity, per-plugin Postgres schema
  isolation, ABAC stays out of scene-log read path, emit-type set-equality).
- **scope:** theme:social-spaces, Scenes, Channels, Forums, Discord bridge, JetStream
  event bus, crypto envelope, focus coordinator, plugin host RPC contract,
  pkg/plugin/eventkit, pkg/plugin/groupkit.

### Presence snapshot — current-state RPC, not event replay

- **source:** `docs/superpowers/specs/2026-05-19-presence-snapshot-design.md`
- **type:** api-contract
- **constraint:** Presence MUST be populated via a **current-state RPC query** at
  Subscribe open, NOT event replay. Defines presence semantics, single
  server-decided focus context, LOCATION-only resolver scope for this bead (scene
  resolver deferred), and explicit exemption from the I-PRIV-1 (now INV-PRIVACY-1)
  temporal floor (current-state queries are timeless, INV-PRESENCE-2).
- **scope:** presence, session store, Subscribe RPC, PresenceEntry/PresenceState proto,
  location resolver, scene resolver (deferred), I-PRIV-1 privacy invariant.

### Session Liveness & Gateway Survival

- **source:** `docs/superpowers/specs/2026-05-30-session-liveness-and-gateway-survival-design.md`
- **type:** nfr
- **constraint:** Redesigns session liveness so `active`/`grid_present` presence state
  derives from a **decaying, actively-refreshed signal** rather than stored cooperative-
  transition intent (`active` was previously "stored intent," never reconciled against
  transport reality — root cause of orphaned-active-session bugs). Defines gateway-held
  connection leases so a core restart does not drop or ghost live clients. Extends,
  rather than contradicts, the base `SessionStore` model from
  `2026-03-19-session-persistence-design.md`.
- **scope:** session liveness, gateway survival, connection leases, derived presence,
  grid_present, reconnect resilience, session reaper, guest reattach TTL, web and telnet
  transports.

---

## 7. Event bus, crypto, and wire conventions

### JetStream Event Log + PostgreSQL Audit Projection — Design

- **source:** `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md`
- **type:** protocol
- **constraint:** Replaces the PostgreSQL LISTEN/NOTIFY event log and delivery transport
  with NATS JetStream, relegating PostgreSQL to identity/projection/forever-archive
  audit. `EventStore.Append`/`SubscribeSession` are replaced by `EventBus.Publish`/
  `OpenSession`. Ordering is owned by JetStream's per-stream `uint64` sequence; ULIDs are
  identity/dedup keys only, never an ordering key. Foundational contract for every later
  event-domain spec in this corpus.
- **scope:** EventBus, JetStream, PostgreSQL audit projection, EventWriter,
  internal/core/event.go, event delivery transport, plugin boundary rules, ULID ordering.

### Event Payload Cryptography — Design

- **source:** `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`
- **type:** protocol
- **constraint:** Encrypts sensitive event payloads (whisper/DM/private-scene content)
  at rest and in transit on the JetStream/PostgreSQL event bus, preserving operator
  visibility into subjects/actors/timestamps while denying plaintext content access,
  without requiring a KMS. Defines the DEK/KEK model, `DEKManager`, crypto_keys schema
  (rotation via `superseded_by`), and dozens of `INV-CRYPTO-*`/legacy `INV-*` invariants
  now catalogued in the central registry (see `context.md`). This is the largest single
  SPEC in the corpus (2566 lines) and the anchor for the entire crypto sub-domain
  (DEK genesis, sensitive-event activation, rekey, admin read-stream, dual-control).
- **scope:** event payload encryption, JetStream event log, PostgreSQL audit projection,
  codec layer, ABAC access control, plugin system (binary/Lua), key management
  (passphrase/keyring/Vault), player-character handoff content protection.

### Central Invariant Registry Design

- **source:** `docs/superpowers/specs/2026-05-31-invariant-registry-design.md`
- **type:** nfr (meta / process)
- **constraint:** Establishes the single canonical registry
  (`docs/architecture/invariants.yaml`/`.md`) unifying 30+ scattered invariant naming
  schemes into one `INV-<SCOPE>-N` convention, a scope taxonomy, legacy-alias mapping,
  and a drift-protection meta-test. This is the meta-spec governing how every other
  spec's invariants (INV-P4-*, I-PRIV-*, INV-CRYPTO-*, etc.) get catalogued — see
  `context.md` for the live registry content.
- **scope:** invariant registry, docs/architecture/invariants.yaml,
  docs/architecture/invariants.md, INV-<SCOPE>-N naming scheme, scope index/taxonomy,
  drift-protection meta-test, legacy invariant ID aliases.

### Event-Type Wire Convention: Canonicalize to `<plugin>:<verb>`

- **source:** `docs/superpowers/specs/2026-06-06-event-type-wire-convention-design.md`
- **type:** protocol
- **constraint:** Canonicalizes the wire event-type identity to plugin-qualified
  `<owning-plugin>:<verb>` across all plugins, fixing silent mismatches in rendering,
  crypto, and verb registries. Establishes the qualified-wire/bare-crypto-registry split
  now codified in `.claude/rules/event-conventions.md`.
- **scope:** event bus, wire event-type convention, plugin manifest verbs,
  RenderingPublisher, crypto.emits, core-communication plugin, core-scenes plugin, verb
  registry.

### Communication Content Contract — Design

- **source:** `docs/superpowers/specs/2026-07-03-communication-content-contract-design.md`
- **type:** protocol
- **constraint:** Defines one canonical content-body payload contract
  (`CommunicationContent`) for conversational-content emitters (say/pose/ooc/emit/page/
  whisper/pemit), enforced symmetrically for Lua and binary plugins at a single host
  chokepoint — collapsing divergent per-consumer dialects. This is the most recent
  SPEC in the corpus and the current source of truth for cross-plugin content shape.
- **scope:** communication content contract, core-communication plugin (Lua),
  core-scenes plugin (Go), web client CommLine model, BFF genericPayload translation,
  plugin-runtime-symmetry, conversational content verbs, plugin SDK builder ergonomics.

---

## 8. Plugin capability & least-privilege epic (`holomush-eykuh`, 6 sub-specs)

Sequential, self-referencing sub-spec series (each cites its predecessor's bead number)
implementing a "do-it-right" redesign of plugin capability declaration and consumption,
per `docs/roadmap.md`'s `theme:plugin-capability-architecture` (shipped epic). No
cross-contradictions detected; presented in delivery order.

### Plugin Host `Evaluate` — per-action ABAC for plugin commands

- **source:** `docs/superpowers/specs/2026-05-25-plugin-host-evaluate-design.md`
- **type:** api-contract
- **constraint:** New plugin-host `Evaluate` RPC/host-capability enabling per-subcommand
  and principal-attribute ABAC checks for plugin commands, replacing coarse
  command/capability gates that never evaluated resource-instance or admin-role
  policies. Unblocks admin-gating of the scene publish-vote extend action.
- **scope:** plugin host, ABAC, Evaluate RPC, core-scenes plugin, plugin.yaml
  capabilities, AttributeResolver, scene publish vote extend.

### Plugin Runtime Config: Manifest Schema + Opaque Host Passthrough

- **source:** `docs/superpowers/specs/2026-05-26-plugin-runtime-config-design.md`
- **type:** api-contract
- **constraint:** Generic plugin runtime config primitive: typed config schema in
  `plugin.yaml`, host-opaque merge of manifest defaults with server overrides, identical
  typed delivery to binary and Lua plugins via SDK helpers. Binds INV-PLUGIN-1..6
  (host MUST NOT interpret plugin config-key meaning; override wins; parity enforced at
  merge layer, not per-runtime).
- **scope:** plugin.yaml manifest schema, config primitive, binary plugin SDK
  (DecodeConfig), Lua plugin SDK (holomush.config), core-scenes plugin, host config
  merge/delivery.

### Plugin Capability & Dependency Foundation — Design

- **source:** `docs/superpowers/specs/2026-06-11-plugin-capability-dependency-foundation-design.md`
- **type:** api-contract
- **constraint:** Foundation sub-spec defining a unified manifest vocabulary and
  dependency resolver for plugin host-capability and inter-plugin-service declarations,
  establishing runtime-parity, least-privilege, and fail-fast invariants
  (INV-PLUGIN-41 through 45) that later sub-specs implement. Triggered by boot bug
  `holomush-oeb4d` (a phantom `requires` silently disabled DAG load-order validation).
- **scope:** plugin manifest vocabulary, dependency resolver, capability declarations,
  Lua/binary runtime parity, fail-fast/fail-closed policy, invariant registry
  INV-PLUGIN-41..45, core-aliases boot bug holomush-oeb4d.

### Plugin Host Capability Decomposition — Design

- **source:** `docs/superpowers/specs/2026-06-11-plugin-host-capability-decomposition-design.md`
- **type:** api-contract
- **constraint:** Decomposes the monolithic `PluginHostService` god-service (25 RPCs)
  into capability-scoped proto contracts under a new `holomush.plugin.host.v1`
  namespace, defining the controlled capability vocabulary and a complete rehoming map
  for all 23 RPCs. Defers enforcement, Lua transport wiring, and manifest migration to
  later sub-specs.
- **scope:** PluginHostService, capability vocabulary, proto contracts, plugin manifest
  requires/capability declarations, binary plugin SDK, Lua hostfunc surface,
  holomush.plugin.host.v1 namespace.

### Lua Parity Layer — Host-Brokered Consumption Design

- **source:** `docs/superpowers/specs/2026-06-12-lua-parity-host-brokered-consumption-design.md`
- **type:** api-contract
- **constraint:** Single host-brokered mechanism for Lua plugins to consume host
  capabilities and plugin services via the same `host.v1` gRPC contracts and
  `BrokerProxy` binary plugins use, replacing divergent hand-written Lua shims. Binds
  INV-PLUGIN-44/45/49; closes bug `holomush-eykuh.6`.
- **scope:** Lua plugin runtime, host.v1 capabilities, BrokerProxy, hashicorp/go-plugin
  grpcbroker, hostcap package, plugin identity, Session/Property/World capabilities.

### Plugin Least-Privilege Enforcement & Plugin-Trust Security — Design

- **source:** `docs/superpowers/specs/2026-06-12-plugin-least-privilege-trust-design.md`
- **type:** nfr (security)
- **constraint:** Defines runtime semantics for manifest `access:`/`scope:`
  least-privilege parameters (per `.claude/rules/plugin-manifest.md`), a default-deny
  ABAC decision on plugin capability/service access keyed on host-stamped subjects, a
  host-vouched dispatch-context primitive, and a symmetric fix for the PR #4430
  plugin-trust (subject-spoofing) finding across Lua and binary runtimes. Binds
  INV-PLUGIN-50 through 53.
- **scope:** plugin manifest access/scope parameters, ABAC access gate for
  capability/service access, dispatch-context primitive,
  CommandRegistryService.ListCommands/GetCommandHelp, plugin-trust hardening,
  INV-PLUGIN-50..53.

### Plugin Capability-Declaration Enforcement at Load

- **source:** `docs/superpowers/specs/2026-06-13-plugin-capability-declaration-enforcement-design.md`
- **type:** nfr (security)
- **constraint:** Fail-closed-at-load enforcement so a binary plugin whose code consumes
  an undeclared `host.v1` capability cannot load. Defines INV-PLUGIN-54 (bound) and
  INV-PLUGIN-55 (pending, Lua half deferred to a separate epic sub-spec). Root cause:
  capability injection is code-driven, not manifest-driven, for both runtimes.
- **scope:** plugin capability declarations, plugin manifest requires:, binary plugin
  loader, hostcap interceptor, wholesystem census, Lua hostfunc shim, luabridge,
  INV-PLUGIN-54, INV-PLUGIN-55, core-scenes plugin.

### Sub-spec 5: Plugin-capability atomic cutover + o262d settlement

- **source:** `docs/superpowers/specs/2026-06-14-plugin-capability-atomic-cutover-design.md`
- **type:** nfr (security)
- **constraint:** Atomic cutover removing the host-capability-bridge allowlist,
  retiring legacy unconditional Lua capability injection, making the
  declaration-gated brokered path the SOLE capability-consumption route for both binary
  and Lua plugins. Settles the `o262d` loader-policy bug. This is the final sub-spec of
  the `holomush-eykuh` epic — per `docs/roadmap.md` the epic is now shipped, kept active
  only for a P3 polish tail.
- **scope:** plugin-capability-architecture, hostfunc.Register,
  luabridge.RegisterHostCaps, RegisterPluginService, hostcap servers,
  WithHostCapBridge allowlist, manifest capability declarations, loader policy /
  INV-PLUGIN-43, least-privilege grant resolver, INV-PLUGIN-44, INV-PLUGIN-45.

---

## 9. Web portal / shell / rendering slices

### Unified Authed Workspace Shell — Design

- **source:** `docs/superpowers/specs/2026-06-20-unified-authed-shell-design.md`
- **type:** api-contract
- **constraint:** Specifies a shared `(authed)` SvelteKit layout unifying the terminal
  and scenes web-client chrome via a persistent rail, footer, and section registry.
- **scope:** web/(authed) layout, Rail navigation, TopBar, ShellFooter, CommandPalette,
  section registry, mobile drawer nav, terminal and scenes routes.

### Web Portal: Scenes — Player Workspace (E9.5)

- **source:** `docs/superpowers/specs/2026-06-07-web-portal-scenes-design.md`
- **type:** api-contract
- **constraint:** Web player workspace for scenes: browsing/watching/contributing, alt
  handling, live delivery via the Phase 5 focus system, BFF read/write RPCs, unread
  badges, log export, auth boundaries. **D4 decision: writes go through the existing
  command path — "No new write RPCs."** This decision is later revisited by
  `2026-06-19-web-create-scene-design.md` for the create-scene affordance specifically
  (see `INGEST-CONFLICTS.md` WARNING 2); other write verbs in this spec (pose/say/ooc via
  command path) are NOT contested.
- **scope:** web portal, scenes workspace, SceneService, WebService BFF RPCs, focus
  coordinator, ConnectRPC streaming, scene participants/roles, ABAC guest gate, scene log
  export, unread notifications.

### Shared Web Communication Rendering Seam

- **source:** `docs/superpowers/specs/2026-06-25-shared-web-communication-seam-design.md`
- **type:** api-contract
- **constraint:** Single shared rendering primitive (`CommLine` model) so the web
  terminal and scene workspace both render say/pose/ooc/emit phrasing consistently via
  `--mush-*` tokens, replacing `PoseCard`'s diverged implementation. Scoped to rendering
  only — composer sigil/input routing and server-side scene command changes are
  explicitly out of scope (later covered by `2026-07-05-focus-routed-scene-input-design.md`).
- **scope:** web scene workspace, web terminal, PoseCard.svelte,
  CommunicationRenderer.svelte, publish_render.go, CommLine model, core-communication
  event vocabulary, core-scenes scene_* events, mush-* theme tokens.

---

## Summary

| Category | Count |
| --- | --- |
| Foundational architecture | 7 (1 archived/historical) |
| ABAC / access control | 2 |
| Web client & session persistence | 2 |
| Channels | 1 |
| Scenes / RP subsystem (Epic 9) | 17 |
| Social-spaces substrate & presence | 3 |
| Event bus, crypto, wire conventions | 5 |
| Plugin capability & least-privilege epic | 8 |
| Web portal / shell / rendering slices | 3 |
| **Total SPEC entries** | **48** (matches classification count for `type: SPEC`) |
