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

## Current State: v0.13 Web Portal — Identity & Admin Foundations shipped (2026-08-16)

**No active milestone.** The next one is defined through `/gsd-new-milestone`, which also writes a fresh
`REQUIREMENTS.md` (v0.13's is archived at `milestones/v0.13-REQUIREMENTS.md`).

v0.13 gave the web client a complete character identity surface and stood up the admin portal shell. Ten
phases, 71 plans, 135 tasks. The organising idea is that **privacy is enforced by absence rather than by
filtering**: three audiences get three proto messages and three projection functions, so a field a viewer
may not see is not omitted from the response — it does not exist on the message at all, and a set-equality
census over every character-returning RPC makes adding a fourth path a compile-or-RED event.

On top of that substrate: public profiles readable by a logged-out visitor (the first route served outside
`(authed)`), per-field viewer-tier masking proven against marshaled bytes, an ABAC-gated `/admin` with a
seven-section registry where the six unbuilt sections are registered and denied *after* the gate, and a
background-job authorization model (`job:<name>` principals with per-execution instance scoping) that
closed a class of unauthenticated write the world service previously had no vocabulary for.

**Closed as `override_closeout`.** All ten phases verified `passed`; all 51 in-scope requirements
satisfied. IDENT-03 (`RenameCharacter`) was deliberately removed from the milestone on 2026-08-06 and
moved to backlog 999.20 — it cannot be specified until the character identity model gains an approval
dimension, which is milestone-scale work. Nyquist coverage is complete: 5 phases compliant, 5 partial
with named causes. Ten open artifacts were acknowledged and deferred (see `STATE.md`).

Carried forward: #4990/#4991/#4992 (integration orphans — #4991 is the sharp one, a deferral note naming
a job identity that does not exist, so a policy written to it fails closed and silently), #4993/#4994
(Nyquist partials), #4996 (agent-facing pipe hazard in `.planning/` commands), and #4974/#4966 (the
traceability writer defects that left the archived REQUIREMENTS table reading `Pending`).

<details>
<summary>v0.12 Foundation Hardening — shipped 2026-07-28</summary>


**Active milestone: v0.13 Web Portal — Identity & Admin Foundations** (started 2026-07-31; see
`## Current Milestone` below).

v0.12 made the v0.11 foundation durable. The world-model gap is closed by decision and by code: ADR
`holomush-i4784` chose CRUD-canonical world state with optimistic concurrency and a transactional outbox,
and Phase 5 implemented it — version-predicated CAS across all four world repos, outbox intent written in
the same transaction as the state change, and the post-commit emit path deleted. The top operational
failure modes from the 2026-07-11 L7 review are closed (`events_audit` retention, nats CVE + a required
`Vuln` supply-chain gate, DLQ `game_id` bridge). `core.Event` is gone and all 17 subsystems start through
`lifecycle.Orchestrator`. `CoreServer` and `plugin/manager` are decomposed behind a mutation-proven
regrowth ratchet.

**What did not fully land, and should shape the next milestone:**

- **Coverage is measured but not enforced.** Phase 9 repaired an E2E coverage upload that had been landing
  empty for ~4 months (project now 79.11%), but no codecov context is a required check on `main` —
  `codecov/project` posts on no ref (#4875) and requiring `codecov/patch` would deadlock docs-only PRs
  (#4876). QUAL-01 reconciled the *documentation* to reality; the gate itself does not exist.
- **QUAL-02/03/05 shipped partially** and are tracked (#4860, #4861, #4792). Backlog cluster 999.10 is now
  the larger residual of the two clusters v0.12 drew from.
- **Two structural gaps found by the closing audit:** `CLAUDE.md`'s event-construction rule would break
  outbox dedup if followed (#4880), and nothing reconciles required CI contexts against what CI can
  actually emit (#4881) — the gap that silently blocked every docs-only PR until #4879.

Full detail: `milestones/v0.12-ROADMAP.md` · `milestones/v0.12-REQUIREMENTS.md` ·
`milestones/v0.12-MILESTONE-AUDIT.md`.

<details>
<summary>v0.12 original milestone goal and target features (as written at kickoff)</summary>

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

</details>

</details>

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

<!-- Current GSD roadmap scope — milestone v0.13 Web Portal: Identity & Admin Foundations. Detailed REQ-IDs + phase mapping: REQUIREMENTS.md / ROADMAP.md. -->

- [ ] Web-portal SPEC produced — admin IA with named slots for deferred sections, character data model, per-field privacy model, media-ready profile schema, and the new RPC surface (satisfies the Out-of-Scope precondition below)
- [ ] Character creation + management usable from the web — creation flow, rename/description/retire, backed by new core/world mutation RPCs (999.1 / `holomush-qve.15`, non-roster scope)
- [ ] Public character profiles + sheets with per-field privacy and a public/private toggle; schema accommodates 1 primary + 10 gallery images without later migration (`holomush-qve.9`)
- [ ] Admin portal shell at `/admin`, `RoleAdmin`-gated, housing character administration and carrying declared empty room for stats / player management / moderation / audit / config / plugin sections (`holomush-qve.10` subset)

<!-- v0.12 residuals are NOT active roadmap scope; they are issue-tracked: #4860, #4861, #4792, #4875, #4876, #4880, #4881, #4882, #4883. -->


### Out of Scope

- **Forums integration** (Epic 11, `holomush-djj`) — no design exists yet; the former Epic 9 sub-item (E9.6)
  was explicitly lifted out 2026-07-03 pending a Forums epic design. Revisit once `holomush-djj` has a spec.
- **Discord/Slack bridging + OAuth linking** (Epic 12) — depends on Channels (Active, above) shipping first,
  plus an OAuth substrate that does not yet exist. Not phase-able until both prerequisites land.
- **Non-scene web-portal surfaces** — *partially reversed 2026-07-31 for v0.13.* The original entry deferred
  every non-scene surface because none had a source SPEC ("needs its own spec (`/gsd-spec-phase`) before it
  can be roadmapped — not fabricated here"). v0.13 satisfies that precondition rather than waiving it: its
  opening phase **produces** the portal SPEC, and only then builds character identity (creation/management,
  profiles) and the admin shell + character administration. Still out of scope and still SPEC-less:
  **world/building editing**, the remaining admin sections (stats, player management, moderation, audit
  viewer, config editor, plugin management — 999.8), the wiki portal (`qve.8`), offline/PWA (`qve.7`), and
  web DMs (`qve.17`). `theme:web-portals`'s "web ⊇ telnet" principle remains directional strategy, not a
  bound invariant.
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
| Background jobs act under a first-class `job:` principal, never a system bypass | A job needs an identity that ABAC can reason about — liveness-gated (a dead job resolves to `(nil, nil)` and denies), capability-classed, and instance-scoped by the provenance of its triggering event (D-55). A typed `world.JobCaller` carries the provenance so no caller can assert arbitrary action keys. | ✓ Good — Phase 02.2 (2026-08-09); AUTHZ-02, INV-ACCESS-13 bound. Mechanism shipped with a fixture job only; first production consumers land in Phase 3 (D-52) |
| An undeclared `action.*` attribute key is a hard boot failure, with no runtime override (D-67) | The alternative is worse: an undeclared key silently evaluates false, so a typo'd policy default-denies forever with no signal. Failing loudly at `cache.Reload` makes the error operator-actionable (it names the policy and the key). Deliberately not softenable — no env kill switch, no seed-vs-DB conditional. | ⚠️ Shipped with a known operator cost — Phase 02.2 (2026-08-09). Upgrades of any deployment carrying operator- or plugin-authored policy rows MUST run the pre-flight query first (`contributing/explanation/background-job-authorization.md`); accepted as AR-02.2-04 |
| Character ownership is policy text, never a Go predicate (D-39/D-40) | `retire`/`unretire` are distinct ABAC actions checked at the `world.Service` chokepoint with no ownership condition compiled into Go, so the authorization surface can be re-scoped by editing policy alone. v0.13 ships them admin-only (user decision 2026-08-07) via the pre-existing bare-action admin seed — no new human-principal grant. | ✓ Good — Phase 3 (2026-08-10); `internal/world/service.go:942,:1042`. Domain capability only: `RetireCharacter` has zero non-test callers until the Phase 6 admin surface (ADMIN-05) lands |
| The first production `job:` consumers are instance-scoped by their triggering event (D-54) | A bare `permit(principal is job, …)` is a blanket grant to every present and future job. Binding BOTH `action.job.trigger_event_type` AND `action.job.trigger_subject == resource.id` makes the blanket the ceiling rather than the target: the retirement reactor may touch only the character its own `character_retired` event names. | ✓ Good — Phase 3 (2026-08-10); `seed:job-retirement-instance-scoped` (`internal/access/policy/seed.go:563`), pinned by exact-DSL assertion. Realizes 02.2's mechanism with its first real consumer |
| `characters.last_active_at` is a lagging column written by a fenced background flusher | Writing activity inline on every event would put a hot write on the world path. A KV-buffered flusher batching into a bare `UPDATE` keeps it off the outbox entirely (no envelope, no version bump) at the cost of up to one flush interval of staleness — which also blunts it as a presence oracle. | ✓ Good — Phase 3 (2026-08-10); sanctioned as INV-WORLD-4's fourth writer. Staleness accepted as AR-03-03; any surface exposing it is a Phase 5 privacy decision |
| Profile privacy is enforced by ABSENCE AT THE DESCRIPTOR, never by clearing fields (D-72/D-77) | An unauthorized field that is populated then cleared is one refactor away from leaking, and proto3 cannot distinguish absent from empty after unmarshal — so a test asserting "field is empty" proves nothing. Instead each audience gets its OWN message type carrying only what that audience may see: `PublicCharacter` has no `player_id`, no `location_id`, no visibility hint. The property is then testable at the marshaled BYTES, with a paired positive control. | ✓ Good — Phase 4 (2026-08-11); `characteraccess.proto:99-120`, projection `characteraccess_projection.go:79-94`, byte assertions `characteraccess_profile_test.go:581,591`. The phase's tracer bullet found a real anonymous ID-existence oracle before the architecture was built on top of it |
| The owner audience builds NO viewer principal, because `entity_properties.owner` is a scalar (D-27) | An "all alts" visibility rule collapses to "owner is that player's only character" once you notice the column is a scalar. The grid side is character-keyed, but a web-side viewer principal on the owner path would disclose that two characters share a player. So `ListMyCharacters`/`GetMyCharacter` construct `access.CharacterSubject` only, and `GetCharacterProfile` has NO self-branch — one code path for every viewer. | ✓ Good — Phase 4 (2026-08-11); `characteraccess_owner.go:140`, two-alt regression + both controls `viewer_alt_linkage_test.go:126,152,162`; INV-ACCESS-15 bound. The test asserts the recorded `world.Caller` BY VALUE, so a viewer principal leaking onto that path fails rather than passing silently |
| A compile fence beats a lint rule when the property is "this layer cannot reach that method" (D-79) | Criterion 5 wanted the facade unable to call world-mutation reads it has no business calling. A `gorules` rule plus a meta-test was rejected in favour of narrow interfaces (`characterAccessWorldReader`, `characterAccessProfileVisibility`): the compiler refuses the call outright, with no suppression vocabulary and no second gate that later becomes grounds for relaxing the first. | ✓ Good — Phase 4 (2026-08-11); fence verified by introducing a violation — `s.world.ListByParent` fails `task build` with "has no field or method". `internal/world/fence_test.go:34,39` pins the domain-side twin |
| Character creation is TWO transactions, and the create is authoritative (Q1) | A single transaction spanning the character row and its five `entity_properties` profile rows would make a property write able to fail a name reservation that already succeeded — and a retry then collides with the name it just reserved. So the create commits first and the profile write is separate: on profile failure the RPC returns SUCCESS with the five keys ABSENT, logging the character id and attempted paths. Correct as a server contract, but it left the PLAYER with a silent five-field loss — closed by a `profileIncomplete` notice linking to `/characters/[id]`, WITHOUT reopening the pinned decision. "The decision is settled" and "the consequence is handled" are separate questions. | ✓ Good — Phase 5 (2026-08-12); accepted as AR-05-05, the phase's one maintainer-ruled accepted risk. Checkpoint log `characteraccess_create.go:206-208`. The notice's non-empty filter is load-bearing — comparing unsubmitted keys would fire on every name-only create |
| The section boundary IS the save boundary, which dissolves the two-RPC/one-version problem (D-93) | `UpdateCharacterProfile` and `UpdateCharacterDescription` guard the SAME `characters.version` and no transaction spans them, so a whole-form save is a two-call chain that can half-fail with nothing to tell the player which half. Per-section save makes that split a VISIBLE boundary instead of a hidden hazard: each response returns the fresh version for the next save, and a lost-update conflict costs one section rather than twelve. | ✓ Good — Phase 5 (2026-08-12); verified LIVE in `05-UAT.md` test 3 (two tabs, one row): the failing section alone took the conflict copy and kept its typed text, the other four stayed inert with Saves disabled, and focus landed on the failed section's first field. `characteraccess_write.go:419,508` surface `Aborted` rather than retrying |
| The public profile lives at root-level `/c/[id]`, outside `(authed)` (D-85) | Every other route sits under `(authed)`, whose layout redirects on session failure (`+layout.ts:26-30`) — so no path prefix may span two auth postures. A public profile nested under an authenticating prefix would be one layout change away from silently requiring a session. Keyed on character id, never name. The anonymous chrome is `TopBar`'s existing unconditional `Login`/`Register` pair, which varies with NOTHING — a profile-local sign-in notice would itself be a which-profiles-are-populated oracle. | ✓ Good — Phase 5 (2026-08-12); T-05-02-03 verified at L3 — no sign-in markup in the route or `PublicProfile.svelte`; `TopBar.svelte:142-144` keys only on auth state. `05-UAT.md` confirmed anonymous reads against a zero-cookie context |

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

*Last updated: 2026-08-13 after Phase 5 (character-identity-ui-public-profiles) — IDENT-01, IDENT-05, PROFILE-01, PROFILE-02, PROFILE-06, PROFILE-07, PROFILE-08, PROFILE-09, EXT-05 and EXT-08 complete (8/8 plans); PROFILE-12 partial (its retirement half deferred to Phase 6 as D-91, tracked in #4963). **No Active requirement moved — but for a NEW reason, and one that is a milestone-closeout question rather than a phase gap.** Phases 3 and 4 held these two requirements open because the surfaces did not exist yet. Phase 5 BUILT the surfaces, and three named sub-capabilities were deliberately descoped instead: "creation flow, **rename**/description/**retire**" ships creation and description but not rename (left v0.13 via D-44) and not retire (no player-facing retirement flow exists — `world.Service.RetireCharacter` has zero non-test callers; IDENT-04 defers self-retire and the admin path is Phase 6's ADMIN-05); and "profiles + per-field privacy and a **public/private toggle**" ships the profiles, the per-field privacy and the 1-primary+10-gallery media schema, but §8.12 ships NO visibility controls in v0.13 — threat T-05-04-07 negative-greps the surface for the vocabulary one would need, so the toggle's absence is a PROVEN property rather than an oversight. Marking either requirement Validated would assert delivery of a control the phase has a test proving absent; at milestone close the choice is to amend the requirement text or move the residue to Out of Scope, and that is the maintainer's call. Three decisions added to the Key Decisions Log (the two-transaction create with the create authoritative, the section boundary as the save boundary, the root-level `/c/[id]` outside `(authed)`). Verification: `05-UAT.md` 5/5 human checkpoints PASS, driven live against a docker-compose stack rather than by hand — which is how checkpoint 4 was caught as resting on a FALSE premise: `charRepo.ListByPlayer` has carried `ORDER BY name` since 7ff05af3c (PR #4816, 2026-07-13), so issue #4965 is invalid as written and the same false claim sits in `web/e2e/characters-roster.spec.ts:11-17`, where it is the stated reason that suite declines to assert ordering. Security: `05-SECURITY.md` records 59 threats, 59 closed, `threats_open: 0`, audited at scaled ASVS depth (L3/L2/L1) by three independent auditors — the L1 short-circuit was declined a second consecutive phase, because it closes on executor self-reports and `05-05-SUMMARY.md` carries no `## Threat Flags` section at all. Five findings recorded where the property holds but the register's stated reason was wrong or weaker than advertised (F-1…F-5). Transition also reproduced #4961: `phase.complete` matched the Progress and STATE By-Phase tables by BARE phase number and wrote v0.13 Phase 5's data into v0.12 Phase 5's rows — repaired in the same commit. Prior entry — 2026-08-11 after Phase 4 (shared-facade-helpers-characteraccessservice) — IDENT-02, IDENT-02a, PROFILE-03, PROFILE-04, PROFILE-05, PROFILE-10 and EXT-06 complete (9/9 plans). No Active requirement moved, for the same reason as Phase 3: Phase 4 ships the facade layer (six `CharacterAccessService` RPCs across public/owner/owner-write audiences, two ABAC seed policies, a world-layer profile write, and the retirement of `WebListAllCharacters`), but "character management usable from the web" and "public profiles + sheets" both need the Phase 5 surfaces. Three decisions added to the Key Decisions Log (privacy by absence-at-the-descriptor, the scalar-`owner` audience split, the compile fence over a lint rule). One developer ruling recorded in `04-UAT.md`: the `pronouns` reachability floor rests on a seeded default rather than structural immunity — `name` is projected from the character row and never floor-evaluated, but `pronouns` is a floor-evaluated `entity_properties` row that a deny-overrides `forbid` could raise — ACCEPTED as a v0.13 limitation (AR-04-03; INV-PRIVACY-10 stays `binding: pending`). Phase 4 also found and fixed a real domain gap: `UpdateCharacterDescription` performed no validation at all (#4954), so ROADMAP criterion 4 was genuinely unmet at HEAD. Prior entry — 2026-08-10 after Phase 3 (world-character-commands) — IDENT-04 + IDENT-10 complete; three decisions added to the Key Decisions Log (policy-not-Go character ownership, instance-scoped `job:` grants, the fenced `last_active_at` flusher). No Active requirement moved: Phase 3 delivers the world-layer capability, but "character management usable from the web" needs the Phase 4 facade and the Phase 5/6 surfaces. Two developer rulings recorded in `03-UAT.md`: IDENT-04 is Complete at Phase 3 as a domain capability (the admin-reachable half is ADMIN-05, Phase 6), and the ungated two-replica resilience proof is SCHEDULED-not-accepted (#4953 must close for it to rejoin the gating CI lane; AR-03-04). Prior entry — 2026-08-09 after Phase 02.2 (background-job-authorization-model) — AUTHZ-02 complete; the `job:` principal and the D-67 undeclared-action-key boot gate added to the Key Decisions Log. Prior entry — 2026-07-31 — milestone v0.13 Web Portal: Identity & Admin Foundations started (promoted from backlog 999.1, identity + admin-shell subset; `/gsd-new-milestone`). v0.12 Foundation Hardening shipped and archived 2026-07-28 (6 phases, 66 plans, 15/19 requirements; `override_closeout`). `.planning/ROADMAP.md` remains the source of truth for phase status; `milestones/v0.12-MILESTONE-AUDIT.md` records what shipped, what was deferred, and why.*
