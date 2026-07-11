---
gsd_state_version: 1.0
milestone: v1.0
milestone_name: milestone
current_phase: 999.1
current_phase_name: BACKLOG
status: "Phase 3 shipped — PR #4782"
stopped_at: Phase 3 context gathered
last_updated: "2026-07-11T02:20:51.421Z"
last_activity: 2026-07-10
progress:
  total_phases: 3
  completed_phases: 3
  total_plans: 26
  completed_plans: 26
  percent: 100
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-07)

**Core value:** Players can play HoloMUSH end-to-end (create characters, communicate, roleplay in scenes)
through either telnet or the web client, with every access-control decision default-deny and every plugin
trusted identically.
**Current focus:** Phase 03 — platform-hardening-deployment-scaling

## Current Position

Phase: 999.1 — Web Client Portal completion (BACKLOG)
Plan: Not started
Status: Phase 3 shipped — PR #4782
Last activity: 2026-07-10
narratives) synthesized into PROJECT.md/REQUIREMENTS.md/ROADMAP.md, grounded against a prior
`/gsd-map-codebase` static analysis and live `bd`/codebase verification of shipped vs. forward scope.

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity:**

- Total plans completed: 26
- Average duration: N/A (no plans executed yet under this GSD roadmap)
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 01 | 10 | - | - |
| 02 | 7 | - | - |
| 03 | 9 | - | - |

**Recent Trend:**

- Last 5 plans: N/A
- Trend: N/A

*Updated after each plan completion*
| Phase 01 P01 | 11 | 2 tasks | 7 files |
| Phase 01 P02 | 95min | 3 tasks | 24 files |
| Phase 01-channels-subsystem P03 | 40min | 3 tasks | 11 files |
| Phase 01 P04 | 40min | 2 tasks | 5 files |
| Phase 01 P05 | 55min | 2 tasks | 4 files |
| Phase 01 P06 | 55min | 2 tasks | 9 files |
| Phase 01 P05b | 70min | 2 tasks | 7 files |
| Phase 01 P07 | 75min | 2 tasks | 9 files |
| Phase 01 P08 | 55min | 2 tasks | 6 files |
| Phase 01 P09 | 150min | 3 tasks | 11 files |
| Phase 02 P01 | 20m | 3 tasks | 6 files |
| Phase 02 P02 | ~15m | 2 tasks | 4 files |
| Phase 02 P03 | 20m | 4 tasks | 11 files |
| Phase 02 P06 | ~40m | 4 tasks | 11 files |
| Phase 02 P04 | ~35m | 3 tasks | 5 files |
| Phase 02 P05 | 55m | 3 tasks | 28 files |
| Phase 02 P07 | ~35m | 3 tasks | 5 files |
| Phase 03 P01 | ~35m | 2 tasks | 4 files |
| Phase 03 P02 | 20m | 2 tasks | 5 files |
| Phase 03 P03 | 40m | 2 tasks | 4 files |
| Phase 03 P05 | ~70m | 3 tasks | 5 files |
| Phase 03 P07 | 50m | 3 tasks | 9 files |
| Phase 03 P09 | 20min | 2 tasks | 1 files |

## Accumulated Context

### Decisions

Full decision log lives in PROJECT.md "Key Decisions". Recent decisions affecting current work:

- **Ingest resolution**: scenes are plugin-owned (`core-scenes`), superseding the 2026-01-22
  `world-model-design.md` locations-table scene section (INGEST-CONFLICTS.md WARNING 1)

- **Ingest resolution**: web structural scene writes use typed RPCs (proto→facade→BFF), superseding E9.5's
  command-path-only decision for structural writes (INGEST-CONFLICTS.md WARNING 2)

- **Codebase verification**: confirmed via `rg`/`bd` that Scenes/RP (Epic 9, all 17 specs through
  focus-routed-input) is fully shipped; Channels/Forums/Discord plugins do not exist in-tree; `eventkit`/
  `groupkit` SDKs are not yet extracted (consistent with INV-S7's N=2 deferral)

- [Phase ?]: 01-01: ChannelService proto mirrors SceneService; identity by ID not payload name (D-08); plaintext no crypto.emits (D-04)
- [Phase ?]: 01-02: ONE shared pluginauthz fence (AuthorizePluginStreamContribution) enforced at BOTH session-establishment merge and mid-session stream.subscription — relative-only, forbidden system/audit/crypto, owned-emit-domain; in-handler control, not read-only seed forbids (R2-B/R3-A)
- [Phase ?]: 01-02: LIVE_ONLY served end-to-end (AddStreamWithMode accepts ReplayModeLiveOnly); no-history-flood is structural via SetFilters start-policy preservation
- [Phase ?]: 01-04: channel membership resolved RESOURCE-side (resource.channel.members) — D-03 Landmine-1; plugin proto has no subject RPC
- [Phase ?]: 01-04: Layer-1 execute-channel-commands gate deferred to the channel command's plan (01-05/01-07) — policy validator rejects a policy targeting an undeclared command
- [Phase ?]: 01-06: PluginAuditService is NOT in a plugin's provides (per-plugin reachability via PluginAuditClient + audit: block); a duplicate declaration collides with core-scenes (DUPLICATE_SERVICE_PROVIDER)
- [Phase ?]: 01-06: channel history membership-gated at auth step-1 for every channel type incl. public (INV-CHANNEL-1); joined_at floor + scrollback cap (D-07); channel_log plaintext no crypto.emits (D-04)
- [Phase ?]: 01-08: guest auto-join served by unioning ListDefaultChannels into QuerySessionStreams (resource-side, no membership-row write, D-01)
- [Phase ?]: 01-08: mid-session live-subscribe failure logged not propagated — degrades to next session-establishment delivery (holomush-l6std), never silently dropped
- [Phase ?]: Telnet scene-activity nudge debounce = 45s; reusable [>GAME] gamenotice primitive; INV-SCENE-70 bound (telnet privacy parity).
- [Phase ?]: Scene notify prefs stored in one plugin table: NULL scene_id = per-character global pref (muted=NOT enabled), non-NULL = per-scene mute; mode column is the D-05 digest seam defaulting realtime.
- [Phase 02]: 02-06: idle sweep transitions active→paused past effective threshold (explicit game-default param into a pool-only store — store never reads config; per-scene idle_timeout_secs overrides via COALESCE); idle nudge OFF by default, emitted via EventSink.Emit and rendered on telnet through gamenotice.Idle; idle math is epoch-nanos (plan's *1000/ms shorthand was a unit bug); INV-SCENE-71 bound.
- [Phase 02]: 02-04: mute/notify-pref suppression at the SCENE_ACTIVITY badge downgrade via a dependency-inverted SceneMuteChecker (interface in server.go, concrete wired at sub_grpc.go); order global-notify-off then per-scene-muted then deliver; per-character 45s TTL cache, loader off-lock; fail-OPEN on nil/error (preferences, not access control); loader dials plugin SceneService with host-vouched actor+ownerPlayerID via BeginServiceDispatch.
- [Phase ?]: 02-05: Web mute/notify shipped as a 4-layer typed slice (proto->facade->BFF->client); facade stamps CharacterId from the verified owned character so the plugin guard passes; never the command path (gateway-boundary).
- [Phase ?]: 02-05: Tasks 1+2 merged into one commit — monolithic proto regen couples the WebServiceHandler interface, so facade + BFF impls land together (Plan 03 precedent).
- [Phase ?]: Provision uses *bool + IsProvision() (mirrors CryptoConfig) so provision:false survives Defaults() — D-03 opt-out seam
- [Phase 03]: CLUSTER-03 multi-node proof runs each replica on its own *nats.Conn to one external NATS testcontainer via new clustertest.ExternalHarness (D-05a); shared embedded conn removed
- [Phase 03]: Invariant registry consolidated in one change (D-07): INV-CLUSTER-1 bound; INV-EVENTBUS-29/30 minted+bound; INV-CLUSTER-8 left pending with coverage issue #4777; no fabricated bindings

### Pending Todos

None yet.

### Blockers/Concerns

- Forums (Epic 11, `holomush-djj`) has no design yet — blocks any Forums-integration forward work
- Discord integration (Epic 12) depends on Phase 1 (Channels) shipping plus an OAuth substrate not yet built
- 259/334 registered invariants are `binding: pending` (concentrated in INV-CRYPTO and INV-SCENE) — tracked
  epic `holomush-hz0v4`, not a blocker, but phases touching crypto/scenes should bind relevant invariants as
  part of their own definition of done

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260709-sqg | Fix holomush-9hygy — convert core-channels migrations TIMESTAMPTZ→BIGINT epoch-ns (lint:no-timestamptz ship blocker) | 2026-07-10 | 1284ba341 | [260709-sqg-…](./quick/260709-sqg-fix-bead-holomush-9hygy-convert-core-cha/) |

## Deferred Items

Items acknowledged and carried forward from the ingest, not part of this roadmap:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| Social-spaces | Forums integration (Epic 11) | No design yet | Ingest 2026-07-07 |
| Social-spaces | Discord/Slack bridging + OAuth linking (Epic 12) | Blocked on Channels + OAuth substrate | Ingest 2026-07-07 |
| Web portal | Non-scene web surfaces (building/world editing, admin UI) | Directional theme goal, not yet spec'd | Ingest 2026-07-07 |

## Session Continuity

Last session: 2026-07-10T22:43:10.188Z
prior `/gsd-map-codebase` run; PROJECT.md, REQUIREMENTS.md, ROADMAP.md, STATE.md written and awaiting user
review/approval.
Stopped at: Phase 3 context gathered
Hardening & Deployment Scaling); awaiting user approval before `/gsd-plan-phase 1`.
Resume file: None
