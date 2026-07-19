# HoloMUSH

## What This Is

HoloMUSH is a modern MUSH (multi-user shared hallucination) platform: an
event-sourced Go core exposing a dual-protocol surface (telnet + web PWA),
a two-tier plugin host (Lua + binary), default-deny ABAC access control, and
PostgreSQL-backed durable audit over an embedded NATS JetStream event bus.
This is a **mature, actively-developed brownfield codebase** — most of the
architecture described below is already shipped and running, not proposed.
The flagship social feature is scenes/RP (`core-scenes` plugin,
`theme:social-spaces`), which forced and validated most of the platform's
substrate (JetStream cutover, payload crypto activation, focus/presence
model).

## Core Value

Players can play HoloMUSH end-to-end — create characters, communicate, and
roleplay in scenes — through either telnet or the web client, with every
access-control decision default-deny and every plugin (Lua or binary)
trusted identically by the host.

## Current Milestone: v0.12 Foundation Hardening

**Goal:** Make the freshly-shipped v0.11 foundation durable — resolve the
event-sourcing-vs-CRUD world-model gap (ADR + version guards + dual-write fix),
eliminate the top operational failure modes surfaced by the 2026-07-11 L7
architecture review, decompose the CoreServer/plugin-manager god objects, and
raise test/coverage/code health.

**Target features:**

- **Event-model decision & symptom fixes** — investigate + ADR (build real
  event sourcing vs. formally adopt CRUD-with-version-guards); correct the false
  event-sourcing docs (root `CLAUDE.md`, `contributing/explanation/architecture.md`,
  public site `index.mdx`); add version guards for last-write-wins (#4798);
  address dual-write non-atomicity (events emit AFTER db commit) — F1 #4784.
  **Decision gate — sequenced early; the model-collapse and last-write-wins
  fixes depend on the ADR's outcome.**
- **Operational hardening** — gateway OOM/survival (F2 #4785), events_audit
  unbounded-growth retention (F4 #4786), audit DLQ hardening (F3 #4787), NATS
  CVE bump (F8 #4790), resilience investigation (#4791).
- **Architecture decomposition (999.9)** — decompose CoreServer + plugin/manager
  god objects, migrate bootstrap to `lifecycle.Orchestrator`, collapse the
  parallel `core.Event`/`eventbus.Event` models, fix gateway-boundary import
  violations (epics `holomush-1bft`/`dj95`/`wm0fi`/`yvdm`).
- **Code health & test quality (999.10)** — coverage backfill (F7 #4804),
  weak/skeleton-test remediation, ACE naming violations, de-slop/humanization,
  session-lifecycle test matrix, security-polish batch (epics
  `holomush-ec22`/`89o9`).

**Deferred (explicitly out of this milestone):** Ops & DR resilience (999.13 —
backup/restore, object-storage DB sync, remote KMS/Vault, Tailscale admin);
feature-shaped Highs F5 no-movement (#4788) and F6 PWA/offline (#4803).

## Requirements

### Validated

<!-- Shipped and confirmed running. Full detail with source citations: milestones/v0.11-REQUIREMENTS.md "Shipped Foundation". -->

- ✓ Event-sourced Go core (immutable ordered events, JetStream-owned ordering, ULID identity) — foundational
- ✓ Two-tier plugin runtime (Lua + binary) with enforced trust/capability symmetry — foundational
- ✓ Dual-protocol gateways (telnet + web ConnectRPC), protocol-translation only — foundational
- ✓ Cedar-aligned default-deny ABAC (AccessPolicyEngine, policy DSL, attribute providers) — access control
- ✓ Auth/identity (argon2id), cross-protocol session persistence, derived session liveness — auth & sessions
- ✓ Scenes & RP subsystem (Epic 9) — plugin-owned `core-scenes`: membership, focus model, content streams,
  publish-vote privacy pipeline, scene board + content warnings, web workspace (create/manage/publish-vote),
  focus-routed conversational input — all shipped through 2026-07-05
- ✓ JetStream event bus + sensitive-payload crypto (DEK/KEK, mandatory-KEK-to-boot) + canonical wire/content
  conventions + central invariant registry — event substrate
- ✓ Plugin-capability-architecture epic (`holomush-eykuh`) — capability-scoped `host.v1` services,
  least-privilege manifest gates, fail-closed-at-load enforcement — SHIPPED (P3 polish tail tracked in `bd`,
  not in this roadmap)
- ✓ Unified web portal shell (`(authed)` layout) + shared `CommLine` rendering seam
- ✓ Channels subsystem (`theme:social-spaces` Epic 10) — plugin-owned `core-channels`: persistent named
  location-independent channels, membership-gated ABAC (resource-side `resource.channel.members`), EventBus
  emit + durable plaintext history, telnet command surface + `=name` shorthand, live delivery
  (`QuerySessionStreams` + `stream.subscription`), whole-system census + E2E; validates INV-S7 (N=2
  second-consumer rule). CHAN-01..05 shipped 2026-07-09 (Phase 1)
- ✓ Scenes lineage completion (`theme:social-spaces` Phase 2) — scene-activity notifications on telnet
  (throttled content-free `[>GAME: …]` nudge, INV-SCENE-70) + web mute/notify-prefs 4-layer slice,
  plugin-owned notify-prefs store, participant-gated mute RPCs + core fail-open badge suppression,
  idle-timeout active→paused lifecycle (INV-SCENE-71), and telnet edge-case hardening (mixed focused/skipped
  render, reconnect focus restore, multi-character no-leak). SCENEFWD-02/03 shipped 2026-07-09 (Phase 2);
  templates (SCENEFWD-01) descoped to backlog (`holomush-x4n1r`)
- ✓ Platform hardening & deployment scaling (`theme:social-spaces` Phase 3) — external/clustered NATS mode
  (`eventbus: mode: external` + fail-closed boot + provision opt-out; embedded stays the zero-config default),
  single-principal account scoping (`deploy/nats` templates + `verify-scoping.sh` + boot self-check),
  multi-node crypto-invalidation verification (per-replica conns, N-of-N + hung-replica probe-pill; binds
  INV-CLUSTER-1/2/4/9, INV-CLUSTER-8 pending w/ coverage issue), audit dead-letter queue + `holomush audit dlq`
  replay CLI (INV-EVENTBUS-29/30 never-drop/fail-closed), and the external-NATS operator runbook. CLUSTER-01..05
  shipped 2026-07-10 (Phase 3); closes the single-node ceiling

- ✓ World-model resilience investigation & decision (v0.12 Phase 4) — two-replica resilience harness
  (external-NATS + shared-DB seams, gated `test/integration/resilience/` suite; OPS-05 #4791), M12
  last-write-wins **reproduced deterministically** + M2 dual-write window **characterized** + unwired-emitter
  production finding (`f1-resilience-verdict.md`), and the MODEL-01 ADR **accepted** (#4784,
  `holomush-i4784`): Option B — CRUD-canonical + optimistic version guard + ordered atomic outbox feed, in
  the shape unanimously ratified by a three-model panel (`model-01-consensus-onepager.md` is normative).
  Phase 5 implements MODEL-03 (version guard) + MODEL-04 (transactional outbox). Shipped 2026-07-11 (Phase 4)
- ✓ Event-model collapse & bootstrap unification (v0.12 Phase 7, ARCH-03/04/05) — single Event representation
  (`core.Event`/`NewEvent`/`EventAppender` deleted repo-wide; `eventbus.Event` is the sole type), process
  bootstrap unified on `lifecycle.Orchestrator` (`Subsystem.Start` split into `Prepare`/`Activate`, two-sweep
  barrier across all 17 production subsystems, zero eager pre-starts), and the gateway-boundary import rule
  holds with zero violations (`internal/web`/`internal/telnet` transitive closures clear of every domain
  package; INV-EVENTBUS-1 bound). 11/11 plans, 10/10 must-haves independently verified. Validated in Phase 7:
  Event-Model & Bootstrap Decomposition, shipped 2026-07-18

### Active

<!-- Current GSD roadmap scope — milestone v0.12 Foundation Hardening. Detailed REQ-IDs + phase mapping: REQUIREMENTS.md / ROADMAP.md. -->

- [ ] Event-model direction decided (ADR: event sourcing vs. CRUD-with-guards), false event-sourcing docs corrected, last-write-wins version guards added, and dual-write non-atomicity addressed (F1 #4784, #4798)
- [ ] Top operational failure modes reduced — gateway survival (#4785), events_audit retention (#4786), audit DLQ hardening (#4787), NATS CVE (#4790), resilience investigation (#4791)
- [ ] CoreServer + plugin/manager god objects decomposed (999.9; Phase 8 — bootstrap/event-model/gateway-boundary portions of this item shipped in Phase 7, see Validated above)
- [ ] Test/coverage/code health raised — coverage backfill (#4804), weak/skeleton tests remediated, ACE naming, de-slop, session-lifecycle matrix (999.10)

### Out of Scope

- **Forums integration** (Epic 11, `holomush-djj`) — no design exists yet; the former Epic 9 sub-item (E9.6)
  was explicitly lifted out 2026-07-03 pending a Forums epic design. Revisit once `holomush-djj` has a spec.
- **Discord/Slack bridging + OAuth linking** (Epic 12) — depends on Channels (Active, above) shipping first,
  plus an OAuth substrate that does not yet exist. Not phase-able until both prerequisites land.
- **Non-scene web-portal surfaces** (world/building editing, admin UI) — `theme:web-portals`'s "web ⊇ telnet"
  principle is directional strategy, not a bound invariant; most non-scene surfaces remain telnet/CLI-only.
  Needs its own spec (`/gsd-spec-phase`) before it can be roadmapped — not fabricated here for lack of a
  source SPEC.
- **Locations-table scene model** (`docs/specs/2026-01-22-world-model-design.md` scene section) — superseded
  by the plugin-owned `core-scenes` model (see Key Decisions). Historical only; do not resurrect.
- **Command-path-only structural scene writes** (E9.5 decision D4, "no new write RPCs") — superseded by the
  typed-RPC decision for structural writes (see Key Decisions). Conversational verbs (pose/say/ooc/emit) still
  correctly use the command path; only *structural* writes (create/end/invite/kick/transfer) moved to RPCs.
- **WASM plugin system** — abandoned one day after the archived 2026-01-17 proposal in favor of the
  Lua + go-plugin two-tier model. No corpus document since has revisited it.

## Context

HoloMUSH's `.planning/` directory is a **complementary** planning surface layered on an existing, mature
project-management stack:

- GitHub Issues (`gh issue -R holomush/holomush`) is the live issue tracker — bugs, follow-ups, labels
  (beads/`bd` was retired 2026-07-09; the export + id mapping live in `.planning/archive/beads/`).
- `docs/roadmap.md` carries strategic theme narratives (`theme:social-spaces`, `theme:plugin-capability-architecture`,
  `theme:web-portals`) as a complement to `theme:*` issue labels.
- `docs/architecture/invariants.yaml`/`.md` is the canonical registry of 334+ named system invariants
  (`INV-<SCOPE>-N`), each `binding: pending` or `binding: bound` to a test.

This GSD roadmap does not replace any of the above — it derives forward-looking phases from the same source
material (48 ingested SPECs + the invariant registry + roadmap theme narratives) and should be read alongside
open GitHub issues / `docs/roadmap.md` for live status, not in place of them.

**Shipped v0.11 (2026-07-11):** Channels subsystem (`core-channels`, second substrate consumer), scenes
lineage completion (notifications + telnet polish), and platform hardening (external/clustered NATS,
multi-node crypto invalidation, audit DLQ + replay CLI) — ~42k lines across PRs #4595/#4782 in 5 days.
Closes the single-node deployment ceiling formerly flagged in CONCERNS.md.

**Ingest provenance:** this PROJECT.md was generated from a 50-document curated ingest (48 SPEC + 2 DOC,
zero ADR/PRD in the batch — see `.planning/intel/SYNTHESIS.md`) plus a prior `/gsd-map-codebase` static
analysis (`.planning/codebase/*.md`). Two SPEC-vs-SPEC conflicts were flagged by the ingest and resolved by
explicit user confirmation before this document was written — both are captured as Key Decisions below and
detailed in `.planning/INGEST-CONFLICTS.md`.

**Known systemic risk** (from `.planning/codebase/CONCERNS.md`): 259 of 334 registered invariants are
`binding: pending` (no test yet proves them), concentrated in `INV-CRYPTO` (103) and `INV-SCENE` (60). This is
a tracked, tolerated ratchet (epic `holomush-hz0v4`), not a roadmap blocker — but any phase touching crypto or
scenes should bind relevant invariants as part of its own definition of done rather than adding to the pile.

## Constraints

- **Tech stack**: Go 1.26.5 core/plugins; SvelteKit 2.69/Svelte 5 web PWA; PostgreSQL 18; NATS JetStream —
  embedded (zero-config default) or external/clustered (`eventbus: mode: external`, shipped Phase 3) — see
  `.planning/codebase/STACK.md`.
- **Build/process**: `task` is the mandatory entry point for build/test/lint/fmt (never raw `go`/lint
  commands); TDD required; spec-driven development with RFC2119 keywords; pre-push adversarial review gates
  (design/plan/code/crypto/abac reviewers) per root `CLAUDE.md`.
- **Deployment scaling**: the event bus runs embedded (single-node default) OR against external/clustered
  NATS JetStream for horizontal multi-node scaling (shipped Phase 3, `holomush-s5ts`; see the external-NATS
  operator runbook under `site/src/content/docs/operating/how-to/`).
- **Gateway boundary**: `internal/web/` and `internal/telnet/` are protocol-translation only — no direct DB
  or domain-service access (`.claude/rules/gateway-boundary.md`).
- **Plugin runtime symmetry**: any new host-side trust/gate/manifest check must apply identically to Lua and
  binary plugins — asymmetry is permitted only when it is purely a transport difference reaching the same
  policy chokepoint (`.claude/rules/plugin-runtime-symmetry.md`).

## Key Decisions

<!-- Durable architectural decisions that constrain all future work on this project. -->

### Locked Architectural Decisions

1. **Plugin runtime symmetry (MUST).** Any host-side trust check, validation, or feature MUST apply
   identically to Lua and binary plugins. The shared chokepoint is
   `internal/plugin/event_emitter.go::Emit`. Asymmetry is permitted only when both runtimes reach the *same*
   policy/trust outcome through different transports (e.g., Lua's `world.query` host-capability vs. binary's
   `WorldService` — same ABAC chokepoint, different wire path). Ref: `.claude/rules/plugin-runtime-symmetry.md`.

2. **Default-deny ABAC (MUST).** Every subject/action/resource triple is evaluated explicitly through the
   Cedar-aligned `AccessPolicyEngine`; there is no implicit allow. Engine failures return `(false, err)`,
   never a permissive decision on infra error. Ref: `docs/specs/abac/00-overview.md`,
   `internal/access/policy/types/types.go`.

3. **Event-sourcing / JetStream ordering ownership (MUST).** Actions produce immutable ordered events; state
   derives from replay/projection, never from mutable authoritative tables alone. Ordering is owned
   exclusively by JetStream's per-stream `uint64` sequence. `core.Event.ID` (ULID, via `core.NewULID()`) is an
   identity/dedup key ONLY — never an ordering key. Ref:
   `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md`.

4. **Scenes are plugin-owned (MUST) — supersedes the locations-table model.** Scenes live entirely inside the
   `core-scenes` binary plugin (own Postgres schema, gRPC `SceneService`, plugin-self-enforced ABAC), NOT as
   `locations`-table rows with a `type='scene'` discriminator. This **resolves ingest WARNING 1**:
   `docs/specs/2026-01-22-world-model-design.md`'s scene section is historical/superseded — corroborated by
   68 `INV-SCENE-*` registry invariants and the social-spaces substrate contract's INV-S6 (per-plugin Postgres
   schema isolation). Ref: `docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md`.

5. **Web structural writes use typed RPCs, not the command path (MUST) — supersedes command-path-only.**
   Structural mutations (create/end/invite/kick/transfer — anything driven by a GUI button/form) MUST go
   through a typed RPC on the BFF facade, never `sendCommand`/`HandleCommand`. The command path is reserved
   for conversational verbs (pose/say/ooc/join) typed by a human or CLI. This **resolves ingest WARNING 2**:
   `docs/superpowers/specs/2026-06-19-web-create-scene-design.md` explicitly supersedes E9.5's
   (`web-portal-scenes-design.md`) D4 "no new write RPCs" decision for structural writes — corroborated by
   `docs/roadmap.md`'s `theme:web-portals` narrative and ADR `holomush-v4qmu`. Ref:
   `.claude/rules/gateway-boundary.md` § "Structural writes use typed RPCs, not the command path".

### Key Decisions Log

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Plugin runtime symmetry | Prevents privilege-gradient bugs between Lua/binary plugin runtimes | ✓ Good — enforced at `event_emitter.go::Emit` |
| Default-deny ABAC (Cedar-aligned DSL) | No implicit allow; fail-closed on infra error | ✓ Good — `internal/access/` |
| Event-sourcing, JetStream-owned ordering, ULID = identity only | Ordering correctness must not depend on ID lexicographic drift | ✓ Good — `internal/eventbus/` |
| Scenes are plugin-owned (`core-scenes`), not `locations` rows | 68 INV-SCENE-* invariants + INV-S6 per-plugin schema isolation assume plugin ownership | ✓ Good — supersedes 2026-01-22 world-model-design's scene section (historical) |
| Web structural writes use typed RPCs, not the command path | GUI-driven mutations must not route through the human/CLI text-command parser (ADR `holomush-v4qmu`) | ✓ Good — supersedes E9.5 D4; conversational verbs still use the command path |
| External/clustered NATS — embedded default, external mode shipped Phase 3 | Built & verified: external dial + fail-closed boot, single-principal account scoping, multi-node crypto invalidation (INV-CLUSTER-1/2/4/9), audit DLQ + replay CLI | ✅ Built in Phase 3 (2026-07-10) — epic `holomush-s5ts` |
| Plugins self-enforce ABAC per RPC (channels adopts the INV-SCENE-65 pattern) | Service-level authz can't be bypassed by new callers (command layer, BFF, future surfaces); uniform NotFound hides denied/hidden resources | ✓ Good — `core-channels` channelService, all 12 RPCs (v0.11) |
| Plugin-owned audit has no DLQ capture (host-audit-only) | DLQ scope deliberately limited to host `events_audit` projection in Phase 3; plugin consumers rely on AckWait+MaxDeliver | — Pending — revisit via issue #4776 before treating plugin audit as never-drop |
| GSD milestone labels track cog-computed semver; GSD never mints v* tags | cog + release.yaml own the v* tag namespace; a GSD tag would corrupt cog's latest-tag version derivation | ✓ Good — `git.create_tag: false`; milestone relabeled v1.0→v0.11 (PR #4783) |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---

*Last updated: 2026-07-18 — Phase 7 (Event-Model & Bootstrap Decomposition) complete: 11/11 plans executed, ARCH-03/04/05 validated against the codebase. Next: `/gsd-discuss-phase 8` / `/gsd-plan-phase 8` (God-Object Decomposition — CoreServer + plugin/manager). The remaining unchecked Active items span Phase 5/6/8 work; this note was last refreshed for Phase 7 only — Phase 5/6 completion status here may be stale relative to `.planning/ROADMAP.md`, which remains the source of truth.*
