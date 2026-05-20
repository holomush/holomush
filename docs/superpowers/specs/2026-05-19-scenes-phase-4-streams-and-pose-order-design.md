# Scenes Phase 4 — Event streams + pose order

## Status

**Draft** — pending `design-reviewer` adversarial review.

**Bead:** `holomush-5rh.13` — Scenes Phase 4: Event streams + pose order.

**Authors:**

- Sean Brandt
- Claude (collaborator, via `dev-flow:brainstorming`)

**Date:** 2026-05-19

**Workspace:** `5rh-13-phase4-brainstorm`

## Overview

Phase 4 turns `core-scenes` from "membership and lifecycle only" (Phase 3) into "membership, lifecycle, AND content emission + pose-order computation" while staying within the substrate-contract boundaries.

Four moving parts:

1. **Plugin-owned content emission surface.** `scene pose / say / emit / ooc` subcommands on the existing `scene` top-level command. Each subcommand emits a typed event (`scene_pose` / `scene_say` / `scene_emit` / `scene_ooc`) to subject `events.<game_id>.scene.<scene_id>.ic` (or `.ooc` for the OOC verb), with the corresponding `crypto.emits` sensitivity. The existing emit path through `internal/plugin/event_emitter.go::Emit` carries these — the same gate that already enforces core-communication's emits.

2. **`crypto.emits` matrix + `EmitTypeRegistrar` adoption.** 8 entries in `plugins/core-scenes/plugin.yaml::crypto.emits`. `scenePlugin` implements `pluginsdk.EmitTypeRegistrar`, registering all 8 types in its `EmitRegistry()`. The substrate INV-S5 validator at `internal/plugin/manager.go::loadPlugin` will fail-closed on any drift.

3. **Pose-order computation + GetPoseOrder RPC.** Computation reads maintained metadata on `scene_participants` (`last_pose_at`, `last_pose_seq`) and `scenes` (`total_pose_count`). All 4 modes (`strict`, `3pr`, `5pr`, `free`) supported with O(N participants) reads — no event-history scan. Read path gated by an INV-S9 plugin-code participant check before any computation runs (no ABAC consultation).

4. **Notice events.** `scene_join_ic` / `scene_leave_ic` (auto-emitted by `JoinScene` / `LeaveScene` / `KickFromScene` RPCs so participants see arrivals/departures in their IC stream). `scene_pose_order_changed_ic` (auto-emitted by `UpdateScene` when `pose_order_mode` is in the update mask, alongside the existing Phase 3 `settings.updated` ops event). `scene_idle_nudge` (event type registered; background-trigger implementation deferred to a follow-up bead per §13).

## Supersedes / extends

- **Extends** [`docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md`](2026-04-06-scenes-and-rp-design-v2.md) sections 3 and 4 (Event streams + Pose order). Product semantics (D1–D10) unchanged.
- **Binds to** [`docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md`](2026-05-16-social-spaces-substrate-contract.md). Phase 4 adds the first plugin-owned scene subjects; populates `crypto.emits`; adopts `EmitTypeRegistrar`.
- **Binds to** [`docs/superpowers/specs/2026-05-17-inv-s5-mechanism-design.md`](2026-05-17-inv-s5-mechanism-design.md). `scenePlugin` implements the binary-plugin `EmitTypeRegistrar` interface; substrate set-equality validator enforces.
- **Binds to** [`docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md`](2026-05-17-history-scope-privacy-design.md) §3 scene-stream entry. Scene IC/OOC streams inherit the per-membership temporal floor (`MAX(focusMembership.JoinedAt, [if IsGuest] GuestCharacterCreatedAt)`) and the I-17 hardcoded membership gate. Scene privacy is absolute (no ABAC override).
- **Binds to** [`docs/superpowers/specs/2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md`](2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md) — plugin audit table shape (DEK columns) is the persistence target for `scene_log` rows.

## RFC2119 keywords

The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are used per RFC2119.

## Invariants

13 numbered invariants. Each is paired with a cited test (see §12.1 for the coverage matrix). The meta-test (INV-P4-13) enforces coverage of every numbered INV-P4-* by spec.

| # | Invariant | Enforcement |
|---|-----------|-------------|
| INV-P4-1 | All Phase 4 plugin-owned scene events MUST emit to NATS dot-style subjects (`events.<game_id>.scene.<scene_id>.<facet>`). Legacy colon-style `scene:<id>:*` MUST NOT appear in any pub/sub topic context within `plugins/core-scenes/` or scene-aware substrate code (`internal/grpc/stream_access.go`, `internal/grpc/query_stream_history.go`, `internal/grpc/scope_floor.go`). | Meta-test + per-emit unit test |
| INV-P4-2 | The 8 scene event types (`scene_pose`, `scene_say`, `scene_emit`, `scene_ooc`, `scene_join_ic`, `scene_leave_ic`, `scene_pose_order_changed_ic`, `scene_idle_nudge`) MUST be declared in `plugins/core-scenes/plugin.yaml::crypto.emits` AND MUST be registered via `EmitTypeRegistrar.RegisterEmitTypes`. The two sets MUST be set-equal. | Manifest-parse unit test + substrate INV-S5 enforcement |
| INV-P4-3 | Sensitivity classification MUST be: `scene_pose`/`scene_say`/`scene_emit`/`scene_ooc` are `always`; `scene_join_ic`/`scene_leave_ic`/`scene_pose_order_changed_ic`/`scene_idle_nudge` are `never`. No `may`-classified events in Phase 4. | Manifest-parse unit test |
| INV-P4-4 | `GetPoseOrder` MUST gate non-participant callers via a direct `scene_participants` membership check BEFORE any computation runs. The ABAC engine MUST NOT be consulted for this gate. | Unit test asserting `PermissionDenied` for non-participants + meta-test asserting no `engine.Evaluate` call in `GetPoseOrder` body |
| INV-P4-5 | `AttributeResolverService.ResolveResource` MUST NOT expose pose-order data (`last_pose_at`, `last_pose_seq`, `total_pose_count`) as a scene attribute. Pose-order data is reachable exclusively via the gated `GetPoseOrder` RPC. | Unit test on resolver output + meta-test rg-asserting no pose-metadata columns in attribute-construction code |
| INV-P4-6 | Non-participants in the same physical location MUST NOT receive scene IC events. Closes audit-finding `holomush-ac50`. | Integration test (Ginkgo) — two sessions same location, one participant one not, scene emit, assert isolation |
| INV-P4-7 | Pose-order computation MUST produce correct results for each of the 4 modes (`strict`, `3pr`, `5pr`, `free`). Per-mode test matrix covers empty / single / multi participants; none-posed / all-eligible / some-cooldown; out-of-turn pose adjustment (strict); join-pose-leave-rejoin sequences. | Table-driven pure-function unit tests on `poseorder.Compute()` |
| INV-P4-8 | Maintained pose-order metadata (`scenes.total_pose_count`, `scene_participants.last_pose_at`, `scene_participants.last_pose_seq`) MUST be a function of `scene_log` `scene_pose` rows. The documented recovery SQL MUST produce identical metadata values when run after arbitrary pose history. | Integration test — emit N poses, snapshot metadata, run recovery SQL, assert byte-identical |
| INV-P4-9 | Late-joining participants MUST see only IC events from `scene_participants.joined_at` forward when reading via `QueryStreamHistory`. Pose-order computation remains scene-global; display via `GetPoseOrder` is unaffected by caller's `joined_at`. Currently a live silent regression — `scope_floor.go:21-28` returns `time.Time{}` for all production callers; Phase 4's dot-style migration fixes it (§3.3). | Integration test — A poses N times, B joins late, B's `QueryStreamHistory` returns events `>= B.joined_at`; B's `GetPoseOrder` reflects all N. Test MUST include pre-migration baseline assertion to pin the bug-fix moment per §3.3 |
| INV-P4-10 | `scene_pose` audit-row insertion AND pose-metadata update MUST be transactional. Either both commit or both roll back. | Fault-injection unit test on audit handler |
| INV-P4-11 | `scene pose` / `scene say` / `scene emit` / `scene ooc` subcommands MUST require the actor to be a participant of the target scene. (Inherits Phase 3 `write-scene-as-participant` ABAC policy via command-capability pre-flight.) | Integration test — non-participant attempts each subcommand, assert `PermissionDenied` |
| INV-P4-12 | `scene update` with `pose_order_mode` in `update_mask` MUST require the actor to be the scene owner. (Inherits Phase 3 `update-own-scene` ABAC policy.) | Integration test — non-owner participant attempts the update, assert `PermissionDenied` |
| INV-P4-13 (meta) | Every numbered INV-P4-* MUST have at least one cited test file referenced in this spec. Coverage matrix in §12.1 MUST exist. | Meta-test parsing this spec; same shape as iwzt T15 / 5b2j T15 |

## 1. Substrate inheritance

What Phase 4 BINDS to without modifying:

| Substrate primitive | Phase 4 use |
|---------------------|-------------|
| Emit path through `internal/plugin/event_emitter.go::Emit` | Each scene event type flows through this gate. Existing manifest enforcement (`crypto.emits`, `actor_kinds_claimable`) applies uniformly to Phase 4 emits. |
| INV-S5 set-equality validator at `internal/plugin/manager.go::loadPlugin` | Fires on `core-scenes` load; fails closed if manifest and code drift. |
| INV-S9 plugin-code privacy boundary (ADR `holomush-c8a9`) | `GetPoseOrder` adopts this pattern; `GetSceneLog` (Phase 6, `holomush-cb4x`) will mirror it. |
| I-17 hardcoded scene-membership gate (`internal/grpc/query_stream_history.go:157-178`) | Migrated to dot-style subject classification by Phase 4; protects scene IC/OOC stream subscription / history reads. |
| iwzt §3 scene-stream entry + I-PRIV-1..8 invariants | Phase 4 IC/OOC streams inherit the per-membership temporal floor (`MAX(focusMembership.JoinedAt, [if IsGuest] GuestCharacterCreatedAt)`) and filter-at-delivery. |
| `audit` declaration `subjects: ["events.*.scene.>"]` in current `plugin.yaml` | Already wildcard; no manifest change needed. Phase 4's IC + OOC events route to `scene_log`. |
| `history_scope: scene` in current `plugin.yaml` | Already declared per ADR `holomush-jhl5`. No Phase 4 work. |
| Substrate filter-at-delivery (ADR `holomush-ghpx`) | Phase 4 scene IC/OOC subjects covered by the existing two-tier privacy enforcement. |

What Phase 4 does NOT do (out of scope; §16):

- Focus-routing for plain `pose` / `say` / `emit` / `ooc` — Phase 5.
- Scene log content read path — Phase 6 + `holomush-cb4x`.
- Cross-scene notifications stream — Phase 10.
- Web client chat view — Phase 9.

## 2. Event vocabulary + crypto.emits matrix

8 plugin-owned scene event types. The privacy boundary for scene content is the participant list (INV-S9 / iwzt §3 "scene privacy is absolute"), so content-carrying events classify as `sensitivity: always` and notice events as `sensitivity: never`.

| Event type | Stream | Sensitivity | Trigger | Payload shape |
|------------|--------|-------------|---------|---------------|
| `scene_pose` | `.ic` | `always` | `scene pose <text>` subcommand | `{actor_id, scene_id, text}` |
| `scene_say` | `.ic` | `always` | `scene say <text>` subcommand | `{actor_id, scene_id, text}` |
| `scene_emit` | `.ic` | `always` | `scene emit <text>` subcommand | `{actor_id, scene_id, text}` |
| `scene_ooc` | `.ooc` | `always` | `scene ooc <text>` subcommand | `{actor_id, scene_id, text}` |
| `scene_join_ic` | `.ic` | `never` | Auto-emitted by `JoinScene` RPC on `OpInserted` or `OpPromoted` results (skipped on `OpNoChange` per Phase 3 D5 retry-idempotency) | `{actor_id, actor_name, scene_id, from_role}` |
| `scene_leave_ic` | `.ic` | `never` | Auto-emitted by `LeaveScene` and `KickFromScene` RPCs on successful remove | `{actor_id, actor_name, scene_id, reason, removed_by?}` where `reason in {"left","kicked"}`, `removed_by` populated for kicks |
| `scene_pose_order_changed_ic` | `.ic` | `never` | Auto-emitted by `UpdateScene` when `update_mask` contains `pose_order_mode` AND the new value differs from current | `{actor_id, actor_name, scene_id, old_mode, new_mode}` |
| `scene_idle_nudge` | `.ooc` | `never` | **Background-trigger implementation deferred (§13).** Wire shape lands in Phase 4; trigger lands in follow-up bead. | `{target_id, target_name, scene_id, idle_duration_seconds, eligible_since_seq}` |

**Sensitivity rationale.** The 4 `always` types carry RP content; INV-S9 makes the participant list the privacy boundary (analog to `whisper`/`page` in core-communication, NOT to `say` whose privacy boundary is the location). The 4 `never` types carry only metadata (character IDs, names, mode strings, durations) — analogous to core-communication's `whisper_notice`. The `mjy3` footgun (sensitivity: never blocks future encrypted emits) is acknowledged: notice events are committed to plaintext for the lifetime of the spec; if a future requirement demands encryption, a sensitivity change is a deliberate manifest migration.

**OOC sensitivity: always (not may).** Considered classifying `scene_ooc` as `may` (caller flexibility per ephemeral OOC). Rejected because: (a) v2 §3.1 makes OOC participant-only (privacy boundary applies regardless of archival status); (b) iwzt §3 explicitly covers `scene:<id>:ooc` under "scene privacy is absolute"; (c) Phase 7 has a known fence gap for `may`-classified types (manifest-set downgrade fence only covers `always`). `always` for OOC has overhead (encryption on ephemeral chatter) but the substrate guarantee strength dominates.

## 3. Subject naming + substrate migration

### 3.1 Subject naming (no legacy translation)

| Stream | Subject |
|--------|---------|
| IC | `events.<game_id>.scene.<scene_id>.ic` |
| OOC | `events.<game_id>.scene.<scene_id>.ooc` |
| Existing lifecycle (CreateScene, EndScene, etc., from Phase 1-3) | `events.<game_id>.scene.<scene_id>` (no facet — entity-level subject; migrates from current colon-style `scene:<id>`) |

Per the substrate contract §1.1 and INV-S4, NATS dot-style is the only form for new code. Phase 4 commits to **no translation layer** for scene subjects: any colon-style scene-subject usage in core-scenes or scene-aware substrate code MUST be migrated in this PR.

### 3.2 Substrate migration scope

Phase 4 ships scene-subject migration in a single coherent PR alongside plugin emission. The non-scene colon-style sweep (location, character, notifications) is tracked by `holomush-rops` (P1 top-level, separate effort).

| File | Lines (approx) | Change |
|------|----------------|--------|
| `plugins/core-scenes/service.go` | `:211` | Replace `Subject: "scene:" + row.ID` with NATS dot-style via a shared `subjectFormatter` helper that takes `gameID`, `sceneID`, optional `facet` |
| `plugins/core-scenes/service_test.go` | `:426` | Update test expectations to dot-style |
| `internal/grpc/stream_access.go` | `:25, :52, :77` | `isPrivateStream` / `extractSceneID` / scene-stream helpers parse the NATS dot-style scene-subject form |
| `internal/grpc/query_stream_history.go` | `:39, :157-178` | I-17 hardcoded scene-membership gate matches dot-style scene subjects |
| `internal/grpc/scope_floor.go` | `:22, :47, :116` | `streamScopeFloor` classifies dot-style scene subjects for per-membership floor. **Live silent regression fix — see §3.3.** |
| `internal/grpc/stream_access_test.go`, `internal/grpc/scope_floor_test.go` | many | Update fixtures to dot-style |
| `test/integration/plugin/binary_plugin_test.go` | `:502, :512` | Update integration scene-stream fixtures (ABAC `Resource` strings — distinct concern — unchanged) |
| `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md` | §3 | Update scene-stream rows to dot-style |
| `docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md` | §3.1 | Update stream-naming table to dot-style |
| `internal/eventbus/subjectxlate/` | `subjectxlate.go` | Verified at spec time: no scene-specific path exists today (generic translator). No removal needed for scene migration. Full layer fate decided in `holomush-rops` audit. |

**INV-S1 note.** The substrate-side files touched (`internal/grpc/*`) are scene-aware substrate code. This change is reviewed under substrate review gates (`code-reviewer`, plus `abac-reviewer` since `query_stream_history.go` touches access-control adjacent gating) alongside the plugin gates. The change is atomic — plugin emits dot-style AND substrate gates parse dot-style in the same PR; otherwise the I-17 gate fails closed on Phase 4 emissions.

**ABAC resource IDs unaffected.** The Cedar-style policies reference resources as `<resource_type>:<id>` strings (e.g., `scene:01ABC...` in policy DSL). This is a policy-DSL serialization artifact, NOT a pub/sub topic. Phase 4 does not touch ABAC resource ID serialization. The narrow scope is "no colon-style pub/sub topics"; the broader sweep (also handled by `rops` if/when desired) does not extend to ABAC resource IDs.

### 3.3 `scope_floor.go` — Phase 4 closes a live silent regression

`internal/grpc/scope_floor.go:21-28` carries a developer note:

> "`streamScopeFloor` currently inspects legacy stream-name prefixes (`location:`, `scene:`), but production callers pass NATS subjects (`events.<gid>.location.X`), so the loop returns `time.Time{}` for every real-world subject today. Until that format mismatch is closed (tracked as a separate follow-up bead), the aggregate floor is effectively zero — preserving the pre-iwzt `DeliverAllPolicy` behavior."

This means the iwzt §6.1 temporal floor for scene streams is **not just untested** before Phase 4 — it is **actively non-functional for all real traffic**. The function is wired up, but its prefix match never fires. INV-P4-9 ("late-joining participants MUST see only IC events from `joined_at` forward") is therefore not a future invariant being introduced; it is a live invariant currently being violated by silent fallthrough to "no floor applied."

Phase 4's migration of `scope_floor.go` (developer note at lines 21-28) to dot-style is therefore **a correctness fix for a currently silent failure**, not merely format hygiene.

**Test mechanism — pinning the bug-fix moment.** A naïve "pre-migration baseline" cannot live in an integration test that runs against post-migration code. The pinning approach is a **unit test in `internal/grpc/scope_floor_test.go`** with two table-driven cases that get MODIFIED in the migration commit:

| Case | Subject form | Pre-migration assertion | Post-migration assertion |
|------|--------------|-------------------------|--------------------------|
| Legacy colon-style | `scene:01ABC:ic` | Returns `JoinedAt` (current matching branch) | Returns `time.Time{}` (legacy form no longer matched — migration removes the `strings.HasPrefix(stream, "scene:")` branch entirely) |
| NATS dot-style | `events.holomush.scene.01ABC.ic` | Returns `time.Time{}` (current bug — no prefix match) | Returns `JoinedAt` (new dot-style branch matches) |

The migration commit changes both assertions atomically. The git diff of `scope_floor_test.go` between pre- and post-migration commits is the audit artifact that pins the bug-fix moment. INV-P4-9's integration test (Ginkgo, `test/integration/scenes/late_joiner_temporal_floor_test.go`) then verifies the end-to-end behavior on the post-migration code — late-joiner sees only events from their `joined_at` forward. Both the unit-level pin AND the integration-level assertion are required by INV-P4-9; the unit pin makes the regression-fix moment auditable, the integration pin makes the end-to-end contract testable.

## 4. `EmitTypeRegistrar` adoption

Per the INV-S5 mechanism design §1.2, binary plugins with non-empty `crypto.emits` MUST implement `pluginsdk.EmitTypeRegistrar`. `scenePlugin` adopts:

```go
// plugins/core-scenes/main.go

import pluginsdk "github.com/holomush/holomush/pkg/plugin"

type scenePlugin struct {
    // ... existing fields ...
    emitRegistry *pluginsdk.EmitRegistry
}

// EmitTypeRegistrar interface.
func (p *scenePlugin) EmitRegistry() *pluginsdk.EmitRegistry {
    return p.emitRegistry
}

func main() {
    reg := pluginsdk.NewEmitRegistry()
    reg.RegisterEmitTypes([]string{
        "scene_pose",
        "scene_say",
        "scene_emit",
        "scene_ooc",
        "scene_join_ic",
        "scene_leave_ic",
        "scene_pose_order_changed_ic",
        "scene_idle_nudge",
    })

    plugin := &scenePlugin{
        // ... existing field initialisation ...
        emitRegistry: reg,
    }
    // ... existing pluginsdk.ServeWithServices call ...
}
```

The SDK adapter at `pkg/plugin/sdk.go::pluginServerAdapter.Init` detects the `EmitTypeRegistrar` interface via type assertion and populates `InitResponse.RegisteredEmitTypes` from the registry. Substrate validator compares this set against the manifest's `crypto.emits[].event_type` set; any drift fails plugin load with `EVENT_TYPE_REGISTRY_MISMATCH` (INV-P4-2 / INV-S5).

`scene_idle_nudge` is registered even though Phase 4 does not call `Emit` on it (background trigger deferred). INV-S5 set-equality requires declared AND registered to match; both sets contain the type. The sensitivity-fence (`internal/plugin/sensitivity_fence.go`) only fires on actual `Emit` calls — zero call sites means no fence interaction in Phase 4.

## 5. Emit subcommands

Phase 4 adds four content-emit subcommands to the existing `scene` top-level command. Until Phase 5 ships focus-aware command routing, these are the explicit emit surface; plain `pose` / `say` / `emit` / `ooc` continue to route through core-communication to location streams.

| Subcommand | Emits | Subject facet |
|------------|-------|---------------|
| `scene pose <text>` | `scene_pose` | `.ic` |
| `scene say <text>` | `scene_say` | `.ic` |
| `scene emit <text>` | `scene_emit` | `.ic` |
| `scene ooc <text>` | `scene_ooc` | `.ooc` |

### 5.1 Authorization

The existing `scene` command capability declaration (`action: write, resource: scene, scope: local`) carries forward unchanged. The Layer-1 command-execute gate is the existing `execute-scene-commands` policy; the Layer-2 capability pre-flight on `write` translates to the Phase 3 `write-scene-as-participant` DSL policy (`principal.id in resource.scene.participants AND resource.scene.state in ["active", "paused"]`).

INV-P4-11 pins this binding: non-participants attempting any of the 4 emit subcommands MUST fail with `PermissionDenied` at the command-execute layer.

### 5.2 Handler shape

Each subcommand handler:

1. Resolves the target scene (current focus context if available; else explicit `scene_id` argument; else error).
2. Calls the in-process emit path via `pluginsdk.EventSink.Emit` with:
   - `Subject`: dot-style scene subject (`.ic` or `.ooc` facet per verb)
   - `Type`: the corresponding scene event type string
   - `Payload`: JSON-encoded `{actor_id, scene_id, text}`
   - `Sensitive`: `true` (matches `sensitivity: always` manifest declaration)
3. Returns a plain-text confirmation (e.g., `"You pose: <text>"`) to the command output.

Until Phase 5 ships focus context, the target scene MAY be inferred from a single-membership session (if the caller is in exactly one active scene). On ambiguity (multiple active memberships), the handler returns an `InvalidArgument` with an actionable hint message. The single-membership inference is convenience UX, not load-bearing — explicit `scene <id> pose <text>` (or equivalent) syntax MAY be added in Phase 5 if needed.

### 5.3 Emit path through substrate

The emit path is uniform with all other plugin emits:

```text
scene pose handler
  → pluginsdk.EventSink.Emit(EmitIntent{...})           [plugin-side SDK]
    → goplugin gRPC → internal/plugin/event_emitter.go::Emit
      → manifest gate (crypto.emits sensitivity check + actor_kinds_claimable)
      → AuditGuard / crypto envelope (sensitivity=always → DEK lookup + encrypt)
      → JetStream publish to events.<game>.scene.<id>.ic
      → audit projection ack-and-skip (plugin-owned subject)
      → PluginAuditService.AuditEvent (plugin-side handler)
        → scene_log INSERT + (if scene_pose) pose-metadata UPDATE
          [§9 transactional update; INV-P4-10]
```

No new substrate surface introduced.

## 6. Pose-order computation

### 6.1 Maintained metadata

Per §9 below, pose-order computation reads denormalized metadata maintained by the audit-event handler on `scene_pose` insertion. Computation is O(N participants) per call, regardless of scene history depth.

### 6.2 Per-mode algorithm

`poseorder.Compute(mode, totalPoseCount, participantsWithMeta, names) → ([]PoseOrderEntry, totalPoseCount)`. Pure function in `plugins/core-scenes/poseorder.go`. Pinned by INV-P4-7.

| Mode | Display order | Eligibility |
|------|---------------|-------------|
| `strict` | `(last_pose_at NULLS FIRST, joined_at ASC)` — never-posed at head, then oldest-posed | First entry: `eligible=true`; rest: `eligible=false` |
| `3pr` | Same as strict (stable display) | `eligible = last_pose_seq IS NULL OR (total_pose_count - last_pose_seq) >= 3` |
| `5pr` | Same as strict | `eligible = last_pose_seq IS NULL OR (total_pose_count - last_pose_seq) >= 5` |
| `free` | `joined_at ASC` | All `eligible=true` |

The `poses_since_last` field on `PoseOrderEntry` surfaces `total_pose_count - COALESCE(last_pose_seq, 0)` for client UX ("Carol (2/3 since)" rendering). Meaningful for 3pr/5pr only; nil otherwise.

**Strict mode out-of-turn semantics.** v2 §4.1: "Linear order based on join sequence. Posing moves you to the end. Out-of-turn poses adjust the order." The maintained-metadata approach captures this exactly: every pose updates the actor's `last_pose_at` to NOW(), pushing them to the tail of the `(last_pose_at NULLS FIRST, joined_at ASC)` ordering. No special-case logic for in-turn vs out-of-turn.

### 6.3 Naming resolution

Character name resolution adopts the `characterNameResolver` interface introduced by `holomush-5b2j` (presence-snapshot work). The default impl wraps `world.CharacterRepository.GetNamesByIDs`. Phase 4 depends on `holomush-5b2j.3` (the `GetNamesByIDs` bead) — see §17 for the dependency edge.

## 7. GetPoseOrder RPC contract

### 7.1 Proto — replaces existing skeletal definitions

`api/proto/holomush/scene/v1/scene.proto:228-243` currently contains a skeletal definition (`GetPoseOrderRequest`, `PoseOrderEntry`, `GetPoseOrderResponse`) from earlier scaffolding. Phase 4 **replaces** these definitions outright — no backward-compatibility constraint (no releases, no external consumers, no in-tree callers of the skeletal shapes), so the right forward-thinking shape lands directly without a deprecation path.

**Final proto (replaces lines 228-243 of `scene.proto`):**

```proto
message GetPoseOrderRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];   // caller; INV-S9 gate
  string scene_id     = 2 [(buf.validate.field).string.min_len = 1];
}

message PoseOrderEntry {
  string                    character_id     = 1;
  string                    character_name   = 2;
  bool                      eligible         = 3;
  google.protobuf.Timestamp last_posed_at    = 4;
  // Count of poses by other characters since this participant's last pose
  // (or since scene start if never posed). Meaningful for 3pr/5pr; for
  // strict it surfaces queue depth; for free it is always zero.
  optional uint32           poses_since_last = 5;
}

message GetPoseOrderResponse {
  string                 mode             = 1;   // strict | 3pr | 5pr | free
  uint32                 total_pose_count = 2;   // drives header text in `scene order`
  repeated PoseOrderEntry entries         = 3;
}
```

**Shape rationale:**

- **Bare `character_id` + `scene_id`** matches the scene.proto convention used by `KickFromSceneRequest:204`, `TransferOwnershipRequest:212`, `CastPublishVoteRequest:220`. No `RequestMeta`/`ResponseMeta` wrapper — those are CoreService conventions (per 5b2j's core.proto), not appropriate for plugin-owned RPCs called over go-plugin transport.
- **`eligible`** (not `is_eligible`) for the boolean — the `is_*` prefix is a Java-bean idiom, redundant with the field's type (booleans are already predicates). The replacement is cleaner; no backward-compat means we don't need to live with the legacy prefix.
- **Field numbering** is rearranged for semantic grouping: identity (1, 2), eligibility (3), timing (4), context (5) on entries; header (1), aggregate (2), payload (3) on response.

**SceneService RPC declaration** — verify the existing `service SceneService { ... }` block contains `rpc GetPoseOrder(GetPoseOrderRequest) returns (GetPoseOrderResponse);`. The implementation plan task includes verifying / adding the line.

Per-field validation via `protovalidate` annotations on `scene_id` / `character_id` ensures unmarshal-time rejection of invalid requests.

### 7.2 Error semantics

| Code | Trigger |
|------|---------|
| `INVALID_ARGUMENT` | `scene_id` or `character_id` empty (protovalidate) |
| `NOT_FOUND` | Scene does not exist |
| `PERMISSION_DENIED` | Caller is not a participant (INV-S9 / INV-P4-4) |
| `FAILED_PRECONDITION` | Scene is `archived` (poses cannot occur, order is meaningless) |
| `INTERNAL` | Store error |

### 7.3 INV-S9 plugin-code gate + `sceneStorer` interface extensions

The Phase 4 `GetPoseOrder` handler requires two new methods on the `sceneStorer` interface (`plugins/core-scenes/service.go:35-50`), in addition to the existing methods that Phase 1-3 introduced. These extensions are declared explicitly here so a plan author treats them as load-bearing interface changes, not implementer-discretion details.

**`sceneStorer` interface extensions for Phase 4:**

```go
type sceneStorer interface {
    // ... existing Phase 1-3 methods unchanged ...

    // IsParticipant returns true if the character is a current
    // participant of the scene with role "owner" or "member" (NOT
    // "invited"). Used by the INV-S9 plugin-code gate at GetPoseOrder.
    // Returns (false, nil) if the character is not a participant — no
    // distinction from "not found"; the gate's contract is binary.
    // Underlying SQL: SELECT 1 FROM scene_participants WHERE scene_id=$1
    // AND character_id=$2 AND role IN ('owner','member').
    IsParticipant(ctx context.Context, sceneID, characterID string) (bool, error)

    // ListParticipantsWithPoseMeta returns all participants of the
    // scene with role "owner" or "member" along with their Phase 4
    // maintained pose metadata (last_pose_at, last_pose_seq) and the
    // scene's total_pose_count. Single-query SELECT per §6.1.
    ListParticipantsWithPoseMeta(
        ctx context.Context,
        sceneID string,
    ) (ParticipantsWithPoseMeta, error)
}

// ParticipantsWithPoseMeta groups the single-query result so the
// handler doesn't have to re-fetch the scene row for total_pose_count.
type ParticipantsWithPoseMeta struct {
    // From `scenes`. Driver header for the response.
    TotalPoseCount uint32

    // From `scene_participants` JOIN with maintained metadata columns.
    Participants []ParticipantWithPoseMeta
}

type ParticipantWithPoseMeta struct {
    CharacterID  string
    JoinedAt     time.Time
    LastPoseAt   *time.Time   // nil if never posed
    LastPoseSeq  *int32       // nil if never posed
}
```

**Handler shape:**

```go
func (s *SceneServiceImpl) GetPoseOrder(ctx context.Context, req *scenev1.GetPoseOrderRequest) (*scenev1.GetPoseOrderResponse, error) {
    // INV-S9: direct participant check in plugin store. No ABAC engine consulted.
    ok, err := s.store.IsParticipant(ctx, req.SceneId, req.CharacterId)
    if err != nil {
        return nil, status.Errorf(codes.Internal, "failed to check participant: %v", err)
    }
    if !ok {
        return nil, status.Errorf(codes.PermissionDenied, "not a participant")
    }

    sceneRow, err := s.store.Get(ctx, req.SceneId)
    if err != nil { /* map SCENE_NOT_FOUND → NOT_FOUND, else INTERNAL */ }
    if sceneRow.State == string(SceneStateArchived) {
        return nil, status.Errorf(codes.FailedPrecondition, "scene is archived; pose order not available")
    }

    pm, err := s.store.ListParticipantsWithPoseMeta(ctx, req.SceneId)
    if err != nil { /* INTERNAL */ }

    participantIDs := make([]string, 0, len(pm.Participants))
    for _, p := range pm.Participants { participantIDs = append(participantIDs, p.CharacterID) }
    names, err := s.nameResolver.GetNamesByIDs(ctx, participantIDs)
    if err != nil { /* INTERNAL */ }

    entries := poseorder.Compute(sceneRow.PoseOrder, pm.TotalPoseCount, pm.Participants, names)
    return &scenev1.GetPoseOrderResponse{
        Mode:           sceneRow.PoseOrder,
        TotalPoseCount: pm.TotalPoseCount,
        Entries:        entries,
    }, nil
}
```

The `engine.Evaluate` / `engine.CanPerformAction` ABAC entry points MUST NOT appear in this handler body. Pinned by INV-P4-4 meta-test.

`AttributeResolverService.ResolveResource` MUST NOT expose `last_pose_at`, `last_pose_seq`, or `total_pose_count` as scene attributes (INV-P4-5). The ABAC engine sees the existing Phase 3 scene attribute set (`id`, `owner`, `state`, `visibility`, `location_id`, `tags`, `warnings`, `participants`, `invitees`) — no pose-order leakage path.

**Why `IsParticipant` distinct from existing `GetParticipant`.** The Phase 3 store has `GetParticipant(ctx, sceneID, characterID) (*ParticipantRow, error)` returning `SCENE_PARTICIPANT_NOT_FOUND` on miss. Using it for the INV-S9 gate would conflate "not found" with the gate decision — the handler would need to inspect the oops code, which adds parsing surface and obscures the gate's intent. `IsParticipant` is a binary predicate that names what the gate is checking. The "owner OR member" role filter also matters: `invited` rows MUST NOT pass the gate even though they exist in `scene_participants` — `IsParticipant`'s contract pins this in one place.

## 8. `scene order` command

`scene order` is dispatched by `commands.go::dispatchCommand`. The handler:

1. Resolves the target scene (single-membership inference; explicit `scene_id` argument).
2. Calls `s.service.GetPoseOrder` in-process (no gRPC round-trip; INV-S9 gate still fires).
3. Renders the response as plain text to command output.

**Output format (telnet/terminal):**

```text
Scene "A Decades-Crossed Meeting" — pose order: 3pr (8 total poses)

  Eligible to pose:
    Alice    (last posed 12m ago)
    Bob      (last posed 18m ago)

  Cooldown:
    Carol    (1/3 since last pose)
    Dave     (2/3 since last pose)
```

For `strict` mode, output highlights the queue head as "Next:":

```text
Scene "..." — pose order: strict (12 total poses)

  Next: Alice (joined 1h ago, last posed 5m ago)

  Then:
    Bob      (last posed 8m ago)
    Carol    (last posed 12m ago)
    Dave     (last posed 2h ago)
```

For `free` mode, output lists participants without eligibility annotation:

```text
Scene "..." — pose order: free (no enforcement, 4 total poses)

  Participants:
    Alice    (last posed 5m ago)
    Bob      (last posed 8m ago)
    Carol    (never posed)
    Dave     (joined 2h ago, never posed)
```

Renderer lives in `plugins/core-scenes/commands.go` (or a small `commands_order.go` adjunct). Output is plain text — no markdown chrome — matching existing core-scenes command output style.

## 9. Schema migration 000006: pose-order metadata

### 9.1 Up migration

```sql
-- plugins/core-scenes/migrations/000006_pose_order_metadata.up.sql

-- Per-scene monotonic pose counter.
ALTER TABLE scenes
  ADD COLUMN total_pose_count INTEGER NOT NULL DEFAULT 0;

-- Per-participant pose metadata. NULL = participant has never posed in this scene.
ALTER TABLE scene_participants
  ADD COLUMN last_pose_at  TIMESTAMPTZ NULL,
  ADD COLUMN last_pose_seq INTEGER     NULL;

-- Per-scene idle-nudge threshold (wire shape; trigger deferred per §13).
-- NULL = idle nudges off for this scene (default).
ALTER TABLE scenes
  ADD COLUMN idle_nudge_threshold INTERVAL NULL;
```

### 9.2 Down migration

```sql
-- plugins/core-scenes/migrations/000006_pose_order_metadata.down.sql

ALTER TABLE scenes
  DROP COLUMN IF EXISTS idle_nudge_threshold;

ALTER TABLE scene_participants
  DROP COLUMN IF EXISTS last_pose_seq,
  DROP COLUMN IF EXISTS last_pose_at;

ALTER TABLE scenes
  DROP COLUMN IF EXISTS total_pose_count;
```

### 9.3 Idempotency

Per project migration guidelines (`site/docs/contributing/database-migrations.md`), the migration MUST be idempotent. `ADD COLUMN` is not natively idempotent in standard PostgreSQL syntax; the migration uses `IF NOT EXISTS` clauses where supported, or the runner's existing tracking (`plugin_migrations` table) ensures the migration is applied exactly once. The implementation plan task SHALL verify the runner's contract before finalising syntax.

### 9.4 Update path — audit handler transaction

The audit-event handler in `plugins/core-scenes/audit.go` is extended to update pose metadata when `type = 'scene_pose'` is received. The current `sceneAuditLogStore` interface at `plugins/core-scenes/audit.go:50-75` exposes only `Insert` and `queryLog` — no transaction primitive. Phase 4 extends this interface with a single new method that performs the insert + pose-metadata updates atomically; this preserves the boundary that `SceneAuditServer` holds an interface, not a pool, while making INV-P4-10's transactional guarantee testable and substitutable.

**Interface extension** (`plugins/core-scenes/audit.go`):

```go
type sceneAuditLogStore interface {
    // ... existing Insert(...) and queryLog(...) ...

    // InsertScenePose performs the scene_log INSERT for a scene_pose event
    // AND the pose-metadata UPDATE on scenes + scene_participants in one
    // transaction. Either both rows mutate or neither does (INV-P4-10).
    //
    // sceneID and posedCharID are parsed from the audit request's subject
    // and actor fields by the caller. timestamp is the canonical event
    // timestamp from the audit request (NOT the wall clock at handler
    // entry — preserves audit-log ordering under retry).
    InsertScenePose(
        ctx context.Context,
        id []byte,
        subject, eventType string,
        timestamp *timestamppb.Timestamp,
        actorKind string,
        actorID []byte,
        payload []byte,
        schemaVer int,
        codec string,
        dekRef *int64,
        dekVersion *int32,
        sceneID string,
        posedCharID string,
    ) error
}
```

**Concrete implementation** on `*SceneAuditStore` (already holds `pool *pgxpool.Pool` privately):

```go
func (s *SceneAuditStore) InsertScenePose(
    ctx context.Context,
    /* ... same args as Insert plus sceneID, posedCharID ... */
) error {
    return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
        // 1. The scene_log INSERT (same ON CONFLICT (id) DO NOTHING shape as
        //    existing Insert path; reused via a private tx-bound helper —
        //    see signature below).
        if err := s.insertSceneLogTx(ctx, tx, id, subject, eventType, timestamp,
                actorKind, actorID, payload, schemaVer, codec, dekRef, dekVersion); err != nil {
            return oops.Code("SCENE_AUDIT_POSE_INSERT_FAILED").Wrap(err)
        }

        // 2. Bump the scene's total_pose_count and capture the new value.
        var newSeq int32
        if err := tx.QueryRow(ctx,
            `UPDATE scenes SET total_pose_count = total_pose_count + 1
             WHERE id = $1 RETURNING total_pose_count`,
            sceneID,
        ).Scan(&newSeq); err != nil {
            return oops.Code("SCENE_AUDIT_POSE_META_UPDATE_FAILED").Wrap(err)
        }

        // 3. Stamp the actor's per-participant pose metadata.
        if _, err := tx.Exec(ctx,
            `UPDATE scene_participants
               SET last_pose_at = $1, last_pose_seq = $2
             WHERE scene_id = $3 AND character_id = $4`,
            timestamp.AsTime(), newSeq, sceneID, posedCharID,
        ); err != nil {
            return oops.Code("SCENE_AUDIT_POSE_META_UPDATE_FAILED").Wrap(err)
        }
        return nil
    })
}
```

**Private tx-bound helper signature** (on `*SceneAuditStore`, package-private — exact name an implementer choice; signature pinned here for clarity):

```go
// insertSceneLogTx executes the same scene_log INSERT as the public Insert
// method but on a caller-provided pgx.Tx. The INSERT keeps the existing
// ON CONFLICT (id) DO NOTHING semantics so redelivery is idempotent.
// Internal helper; not exposed via the sceneAuditLogStore interface.
func (s *SceneAuditStore) insertSceneLogTx(
    ctx context.Context,
    tx  pgx.Tx,
    id []byte,
    subject, eventType string,
    timestamp *timestamppb.Timestamp,
    actorKind string,
    actorID []byte,
    payload []byte,
    schemaVer int,
    codec string,
    dekRef *int64,
    dekVersion *int32,
) error
```

The existing public `Insert(ctx, ...)` is rewritten in Phase 4 as a thin wrapper that opens a `pgx.Tx`, calls `insertSceneLogTx`, and commits. This DRY-refactor consolidates the SQL in one place and makes the test surface uniform.

**Handler dispatch** in `SceneAuditServer.AuditEvent`:

```go
func (s *SceneAuditServer) AuditEvent(ctx context.Context, req *pluginauditpb.AuditEventRequest) (*pluginauditpb.AuditEventResponse, error) {
    // ... existing validation, subject ownership check, etc ...

    row := req.GetRow()
    if row.GetType() == "scene_pose" {
        sceneID, err := parseSceneSubject(row.GetSubject())   // existing helper at audit.go:409
        if err != nil { /* INVALID_ARGUMENT: malformed scene subject */ }

        // ActorID arrives as 16-byte ULID nested under row.Actor per the
        // Phase 7 plugin SDK contract (pluginauditpb.AuditRow.Actor.Id is
        // bytes). Plugin stores character IDs as ULID strings throughout
        // (scene_participants.character_id is TEXT). Convert via
        // ulid.ULID + .String() — identical accessor path used by the
        // existing QueryHistory implementation at
        // plugins/core-scenes/audit.go:209-213.
        var actorULID ulid.ULID
        copy(actorULID[:], row.GetActor().GetId())
        posedCharID := actorULID.String()

        if err := s.store.InsertScenePose(ctx,
            /* same arg list as Insert ... */,
            sceneID, posedCharID,
        ); err != nil {
            return nil, /* wrap to gRPC status */
        }
    } else {
        if err := s.store.Insert(ctx, /* args ... */); err != nil {
            return nil, /* wrap */
        }
    }

    return &pluginauditpb.AuditEventResponse{}, nil
}
```

**Why interface-level extension (not Pool-exposure):**

Three alternatives were considered:

| Alternative | Rejected because |
|-------------|------------------|
| Expose `Pool()` on `sceneAuditLogStore` interface | Leaks pool internals into the audit-server interface; test substitution would force fakes to construct pgxpool — much heavier test surface |
| Add a generic `WithTx(ctx, fn func(tx pgx.Tx) error) error` method | Surface area grows beyond Phase 4 needs; future callers might use it for non-scene-pose operations and bypass the Phase 4 invariant. INV-P4-10 names a specific operation; the interface method names it too. |
| Move transactional logic into `SceneAuditServer` itself (holds pool directly) | Breaks the existing interface-based decoupling — `SceneAuditServer` is constructed with a `sceneAuditLogStore` interface field today (`audit.go:96`); restructuring at the field-type level would propagate test churn |

Extending the interface with the specific `InsertScenePose` method names the operation, keeps `SceneAuditServer` constructor unchanged, and lets test fakes substitute deterministic behavior for INV-P4-10 fault injection.

**Edge case — actor not currently a participant.** If `req.Actor` is not a row in `scene_participants` for `sceneID` (e.g., the actor left between emit and audit consumption — possible under JetStream redelivery semantics), step 3's `UPDATE` affects 0 rows. The transaction COMMITS successfully because none of the three statements errored. The `scene_log` row records the pose; pose-order metadata simply reflects "they posed but left." Recovery SQL (§9.5) handles this correctly on a future rebuild.

**Fault-injection test for INV-P4-10.** A test fake substituting `sceneAuditLogStore` makes `InsertScenePose` return an error after the (simulated) scene_log INSERT but before the metadata UPDATE — asserts the entire operation rolls back (no `scene_log` row, no metadata change). Symmetrically, the metadata-UPDATE failure path is exercised.

### 9.5 Recovery procedure (operator runbook)

If metadata drifts from `scene_log` (e.g., manual intervention, migration replay, future bug), the documented recovery rebuilds metadata from canonical event history:

```sql
-- Recompute scenes.total_pose_count from scene_log.
UPDATE scenes s SET total_pose_count = (
    SELECT COUNT(*) FROM scene_log sl
    WHERE sl.subject = 'events.' || $game || '.scene.' || s.id || '.ic'
      AND sl.type = 'scene_pose'
);

-- Recompute per-participant last_pose_at + last_pose_seq.
WITH per_actor_last AS (
    SELECT
        subject,
        actor,
        timestamp AS last_pose_at,
        ROW_NUMBER() OVER (PARTITION BY subject, actor ORDER BY timestamp DESC) AS rn_per_actor,
        -- count of scene_pose rows on this subject up to and including this row
        COUNT(*) FILTER (WHERE type = 'scene_pose') OVER (PARTITION BY subject ORDER BY timestamp) AS seq
    FROM scene_log
    WHERE type = 'scene_pose'
)
UPDATE scene_participants p
SET last_pose_at  = pal.last_pose_at,
    last_pose_seq = pal.seq
FROM per_actor_last pal
WHERE pal.rn_per_actor = 1
  AND pal.actor   = p.character_id
  AND pal.subject = 'events.' || $game || '.scene.' || p.scene_id || '.ic';
```

The recovery is documented for operator use; it is NOT auto-executed at startup. INV-P4-8 pins the property that the recovery produces identical metadata to the maintained path.

## 10. Notice events: emission triggers

### 10.1 `scene_join_ic`

Emitted by `JoinScene` RPC (`plugins/core-scenes/service.go::JoinScene`) after `AddParticipant` returns `OpInserted` or `OpPromoted`. Skipped on `OpNoChange` per Phase 3 D5 retry-idempotency — preserves the audit-log cleanness Phase 3 committed to.

```go
result, err := s.store.AddParticipant(ctx, req.SceneId, req.CharacterId)
if err != nil { /* map errors */ }
if result == OpInserted || result == OpPromoted {
    name, _ := s.nameResolver.GetNameByID(ctx, req.CharacterId)
    s.eventSink.Emit(ctx, EmitIntent{
        Subject:   sceneSubjectIC(s.gameID, req.SceneId),
        Type:      "scene_join_ic",
        Payload:   marshalJSON(map[string]string{
            "actor_id": req.CharacterId,
            "actor_name": name,
            "scene_id": req.SceneId,
            "from_role": fromRoleFor(result),
        }),
        Sensitive: false,  // sensitivity: never
    })
}
```

### 10.2 `scene_leave_ic`

Emitted by `LeaveScene` and `KickFromScene` RPCs on successful remove. One event type, `reason` field discriminates voluntary (`"left"`) vs involuntary (`"kicked"`).

### 10.3 `scene_pose_order_changed_ic`

Emitted by `UpdateScene` handler when `update_mask` contains `pose_order_mode` AND the new value differs from current. The latter check prevents spurious emissions on no-op updates:

```go
if hasMaskPath(req.UpdateMask, "pose_order_mode") && req.GetPoseOrderMode() != currentRow.PoseOrder {
    s.eventSink.Emit(ctx, EmitIntent{...
        Type: "scene_pose_order_changed_ic",
        Payload: marshalJSON(map[string]string{
            "actor_id":   req.CharacterId,
            "actor_name": actorName,
            "scene_id":   req.SceneId,
            "old_mode":   currentRow.PoseOrder,
            "new_mode":   req.GetPoseOrderMode(),
        }),
        Sensitive: false,
    })
}
```

The Phase 3 `settings.updated` ops event (plugin-internal `scene_ops_events` table) continues to fire alongside; the two are complementary (ops journal for audit, IC stream for participant visibility).

### 10.4 `scene_idle_nudge` — wire shape only

Phase 4 declares the event type in `crypto.emits`, registers it in `EmitTypeRegistrar`, adds the `idle_nudge_threshold INTERVAL NULL` column to `scenes` (migration 000006), but does NOT implement the trigger. Background-trigger implementation is deferred to a follow-up bead (§13).

Rationale: the trigger requires either a per-scene polling goroutine or a reactive on-pose hook. Both are non-trivial and orthogonal to the rest of Phase 4. The wire shape (event type + schema column + manifest declaration) is locked in here so the follow-up bead can land as a pure-logic change with no manifest, proto, or migration touches.

## 11. ABAC: no new policies

Phase 4 introduces ZERO new ABAC policies. All authorization gates are inherited:

| Surface | Gate | Source |
|---------|------|--------|
| `scene pose / say / emit / ooc` execute | Layer-1 `execute` on `command:scene` | Phase 3 `execute-scene-commands` policy |
| `scene pose / say / emit / ooc` content emit | Layer-2 capability pre-flight (`write` on `scene`, scope `local`) → DSL `write-scene-as-participant` | Phase 3 `write-scene-as-participant` policy |
| `scene update --pose_order_mode` (emits `scene_pose_order_changed_ic`) | `update` on `scene`, owner-only | Phase 3 `update-own-scene` policy |
| `scene order` (calls `GetPoseOrder`) | INV-S9 plugin-code participant gate | New plugin-code gate in Phase 4, ADR `holomush-c8a9` pattern |
| `GetPoseOrder` RPC | Same INV-S9 gate | Same |
| Subscriber-side receipt of `scene_*_ic` / `scene_*_ooc` events | Substrate I-17 hardcoded scene-membership gate + iwzt §3 temporal floor | Substrate + iwzt §3 |

The `scene` command capability declaration in `plugin.yaml` (`action: write, resource: scene, scope: local`) carries forward unchanged.

Phase 4 ABAC review surface is therefore *confirmation* (existing policies continue to gate correctly under new emit surfaces) rather than new policy authoring.

## 12. Test strategy

### 12.1 Coverage matrix (pins INV-P4-13)

| Invariant | Test type | Location |
|-----------|-----------|----------|
| INV-P4-1 | Meta-test + per-emit unit | `internal/test/invariants/scene_subjects_test.go` (new); `plugins/core-scenes/service_test.go` (updated) |
| INV-P4-2 | Manifest-parse unit + substrate integration | `plugins/core-scenes/main_test.go` (new); existing `internal/plugin/manager_test.go` |
| INV-P4-3 | Manifest-parse unit | `plugins/core-scenes/main_test.go` (new) |
| INV-P4-4 | Unit + meta-test | `plugins/core-scenes/service_test.go::TestGetPoseOrder_NonParticipant_PermissionDenied`; `internal/test/invariants/scene_no_abac_in_getposeorder_test.go` (new, rg-based) |
| INV-P4-5 | Unit + meta-test | `plugins/core-scenes/resolver_test.go::TestResolveResource_ExcludesPoseOrderMetadata`; `internal/test/invariants/scene_resolver_no_poseorder_leak_test.go` (new, rg-based) |
| INV-P4-6 | Ginkgo integration | `test/integration/scenes/non_participant_ic_isolation_test.go` (new) |
| INV-P4-7 | Table-driven pure-function unit | `plugins/core-scenes/poseorder_test.go` (new) |
| INV-P4-8 | Integration | `plugins/core-scenes/store_integration_test.go::TestPoseOrderMetadata_RebuildFromAuditLog` |
| INV-P4-9 | Ginkgo integration | `test/integration/scenes/late_joiner_temporal_floor_test.go` (new) |
| INV-P4-10 | Fault-injection unit | `plugins/core-scenes/audit_test.go::TestAuditEvent_PoseMetadataTransactional` |
| INV-P4-11 | Integration | `plugins/core-scenes/commands_test.go::TestSceneEmitSubcommands_NonParticipant_PermissionDenied` |
| INV-P4-12 | Integration | `plugins/core-scenes/service_test.go::TestUpdateScene_PoseOrderMode_NonOwner_PermissionDenied` |
| INV-P4-13 (meta) | Meta-test parsing this spec | `internal/test/invariants/p4_coverage_test.go` (new) — same shape as iwzt T15 / 5b2j T15 |

### 12.2 Boundary tests ("what must and must not be true")

- **MUST tests**: each invariant has at least one positive-path test asserting the property holds.
- **MUST NOT tests** (negative-path):
  - INV-P4-1: no colon-style string literal in scene-subject positional context.
  - INV-P4-4: no ABAC engine consultation in `GetPoseOrder` body.
  - INV-P4-5: no pose-metadata column references in attribute-construction code.
  - INV-P4-6: no scene IC event delivery to non-participant in same location.
  - INV-P4-9: no pre-joined-at history leak to late joiners.
- **Fault-injection**:
  - INV-P4-2: manifest drift (declared-but-unregistered, registered-but-undeclared) MUST fail plugin load with `EVENT_TYPE_REGISTRY_MISMATCH`.
  - INV-P4-10: simulated `UPDATE` failure inside the audit transaction MUST roll back the `INSERT`.
- **Property tests**:
  - INV-P4-8: rebuild idempotency over arbitrary pose history — generate N poses with arbitrary actor distribution and timing; assert maintained metadata equals SQL-recomputed metadata.

### 12.3 Pre-existing test surfaces leveraged

- Phase 3's `core_scenes_suite_test.go` Ginkgo suite — Phase 4 tests extend existing patterns.
- `audit_test.go` — existing audit handler tests; Phase 4 adds the metadata-transaction cases.
- `resolver_test.go` — existing resolver tests; Phase 4 adds the no-pose-leak case.

## 13. Out of scope

The following are explicitly NOT shipped in Phase 4, with their dispositions:

| Item | Where it lives |
|------|----------------|
| Focus-routing for plain `pose` / `say` / `emit` / `ooc` (so they auto-target the focused scene) | Phase 5 (`holomush-5rh.14`) |
| Content-read paths: `scene log` command + export renderers + scene-log archival | Phase 6 (`holomush-5rh.15`) + existing bead `holomush-cb4x` |
| Cross-scene notifications stream (`notifications:<character_id>` migration + integration) | Phase 10 (`holomush-5rh.19`) |
| Web client chat view (Phase 4 emit shape consumed unchanged) | Phase 9 (`holomush-5rh.18`) |
| Scene templates | Phase 7 (`holomush-5rh.16`) |
| Scene board / discovery | Phase 8 (`holomush-5rh.17`) |
| Background-trigger implementation for `scene_idle_nudge` (wire shape ships in Phase 4) | **New follow-up bead** — filed at `plan-to-beads` materialization |
| Non-scene colon-style subject migration (`location:*`, `character:*`, `notifications:*`) | `holomush-rops` (already filed P1 top-level) |
| Idle-timeout reaper (scene auto-pause on inactivity, v2 §1.1 `IdleTimeout`) | Separate hardening bead if desired; v2 framed as configurable, off-by-default |
| `scene_journal` table introduction / rename of `scene_log` to `scene_publication` | Phase 6 (`5rh.15`) brainstorm decides |
| `OriginLocationID` and `PublishVote` participant fields (deferred from Phase 3) | Phase 6 brainstorm reinstate decision |

## 14. Follow-up beads

To be filed during `plan-to-beads` materialization (alongside the Phase 4 implementation child beads):

1. **`scene_idle_nudge` background trigger implementation** (P2) — implements the per-scene polling goroutine (or reactive on-pose hook) that detects "next-up character has been idle beyond `idle_nudge_threshold`" and emits `scene_idle_nudge` to the OOC stream. Phase 4 ships the wire shape; this bead lands the trigger logic.

Already-filed dependencies / interactions:

- `holomush-rops` (P1, open) — broader colon-style sweep (filed during this brainstorm).
- `holomush-ac50` (P2, open) — non-participant scene IC isolation E2E test. INV-P4-6 is exactly this bead's acceptance; Phase 4 closes it.
- `holomush-cb4x` (P2, open) — Phase 6 scene-log content read path. NOT blocked by Phase 4; Phase 4's INV-S9 pattern for `GetPoseOrder` is the template `cb4x` will mirror for `GetSceneLog`.
- `holomush-5b2j.3` (P1, in-progress) — `GetNamesByIDs` batch lookup on `world.CharacterRepository`. Phase 4 depends on this.
- `holomush-iwzt` (P0, in-progress) — history-scope privacy. Phase 4 binds to iwzt §3 scene-stream entry. Phase 4 has a soft dependency on `iwzt.15` (Tier 2 filter-at-delivery) for INV-P4-9 — if iwzt.15 lands first, INV-P4-9 is testable end-to-end; if Phase 4 ships first, INV-P4-9 is marked PENDING-iwzt.15.

## 15. Dependencies

Phase 4 bead `holomush-5rh.13` `depends-on`:

- `holomush-5b2j.3` — `GetNamesByIDs` batch lookup (name resolution for `GetPoseOrder` / `scene order`).
- `holomush-iwzt.15` — Tier 2 filter-at-delivery (INV-P4-9 enforcement end-to-end). Soft dependency: Phase 4 builds and ships without it; INV-P4-9 test is gated separately.

Already-satisfied dependencies (substrate-contract era):

- `holomush-jg9b.3` — INV-S5 cap + plugin adoptions (closed). `core-scenes` adopts the binary-plugin `EmitTypeRegistrar` per the INV-S5 mechanism design.
- `holomush-oy6e.10` — core-scenes plugin adoption (closed). Phase 4 builds on the focus-aware membership baseline.
- `holomush-z69k` — server-owned replay/resume/focus semantics (closed).

## 16. Areas needing deeper design (after Phase 4 lands)

None blocking. The following are tracked as named follow-ups rather than design gaps:

| Area | Trigger |
|------|---------|
| `scene_idle_nudge` background trigger | After Phase 4 closes (P2 bead filed at `plan-to-beads`) |
| Focus-routing for plain pose/say/emit/ooc | Phase 5 brainstorm (`holomush-5rh.14`) |
| Scene-log content read path + publication artifact rename | Phase 6 brainstorm (`holomush-5rh.15`) + `cb4x` |
| `scene_journal` introduction (if needed beyond `scene_log` for retention separation) | Phase 6 brainstorm |
| Per-scene admin-bypass for staff debugging of pose order | NOT planned — scene privacy is absolute per iwzt §3 / INV-S9. Staff have no Phase-4-side bypass. |

## 17. References

### Within the repository

- [Substrate Contract Spec](2026-05-16-social-spaces-substrate-contract.md) — boundary invariants, INV-S1..S10.
- [INV-S5 Mechanism Design](2026-05-17-inv-s5-mechanism-design.md) — binary-plugin `EmitTypeRegistrar`.
- [Scenes & RP Design v2](2026-04-06-scenes-and-rp-design-v2.md) §3-4 — Event streams + pose order (extended here).
- [Scenes Phase 3 Membership Design](2026-04-07-scenes-phase-3-membership-design.md) — content-vs-ops event split; participant model.
- [History Scope Privacy](2026-05-17-history-scope-privacy-design.md) §3 — scene-stream entry + I-PRIV-1..8.
- [Phase 7 Plugin SDK + Crypto Integration](2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md) — plugin audit table shape; AuditEvent RPC contract.
- [Presence Snapshot](2026-05-19-presence-snapshot-design.md) — `GetNamesByIDs` pattern; future scene-context presence resolver wire shape.
- [`.claude/rules/event-conventions.md`](../../.claude/rules/event-conventions.md) — subject naming convention.
- [`.claude/rules/plugin-runtime-symmetry.md`](../../.claude/rules/plugin-runtime-symmetry.md) — Go + Lua parity (Phase 4 is binary-only, but parity invariant guides any future Lua-side scene work).

### ADRs

- [`holomush-c8a9`](../../adr/holomush-c8a9-scene-privacy-plugin-code-enforcement.md) — Scene privacy plugin-code enforcement (INV-S9 / INV-P4-4 pattern).
- [`holomush-3vsb`](../../adr/holomush-3vsb-manifest-emit-type-startup-validation.md) — INV-S5 startup validation.
- [`holomush-vie9`](../../adr/holomush-vie9-init-rpc-emit-type-communication.md) — Init-RPC for emit-type communication.
- [`holomush-z1e7`](../../adr/holomush-z1e7-strict-plugin-boundary.md) — INV-S1 strict plugin boundary.
- [`holomush-p7w0`](../../adr/holomush-p7w0-split-plugin-sdk-eventkit-groupkit.md) — `eventkit` / `groupkit` SDK split (Phase 4 does not extract; future).
- [`holomush-lrt3`](../../adr/holomush-lrt3-n2-consumer-validation-sdk-extraction.md) — N=2 SDK validation deferral.
- [`holomush-jhl5`](../../adr/holomush-jhl5-plugin-history-scope-opt-in.md) — Plugin manifests opt-in to `history_scope`.
- [`holomush-wxty`](../../adr/holomush-wxty-hard-gate-location-stream-history.md) — Hard-gate location-stream history (sibling pattern to scene I-17 gate).
- [`holomush-ghpx`](../../adr/holomush-ghpx-filter-at-delivery-and-nats-source-of-truth.md) — Filter-at-delivery + NATS-source-of-truth.

### Working precedents cited

- [`plugins/core-communication/plugin.yaml:273-298`](../../../plugins/core-communication/plugin.yaml) — 8-type `crypto.emits` matrix; location-bounded vs participant-bounded sensitivity precedent.
- [`plugins/core-scenes/plugin.yaml`](../../../plugins/core-scenes/plugin.yaml) — current scene manifest (empty `crypto.emits`; Phase 4 populates).
- [`plugins/core-scenes/service.go`](../../../plugins/core-scenes/service.go) — existing service handlers (Phase 1-3); Phase 4 adds `GetPoseOrder` + scene_*_ic emits.
- [`plugins/core-scenes/audit.go`](../../../plugins/core-scenes/audit.go) — existing audit handler; Phase 4 extends with pose-metadata transactional update.
- [`internal/plugin/event_emitter.go::Emit`](../../../internal/plugin/event_emitter.go) — substrate emit gate.
- [`internal/plugin/sensitivity_fence.go:23-48`](../../../internal/plugin/sensitivity_fence.go) — emit-time sensitivity truth table.
- [`internal/grpc/query_stream_history.go:157-178`](../../../internal/grpc/query_stream_history.go) — existing I-17 hardcoded scene-membership gate (migrates to dot-style in Phase 4).
- [`internal/grpc/stream_access.go`](../../../internal/grpc/stream_access.go) — `isPrivateStream` / `extractSceneID` helpers.
- [`internal/grpc/scope_floor.go`](../../../internal/grpc/scope_floor.go) — `streamScopeFloor` (iwzt §6.1 implementation).

## Document history

| Date       | Action                       | Notes                                                                 |
|------------|------------------------------|-----------------------------------------------------------------------|
| 2026-05-19 | Draft authored               | Brainstorming session under bead `holomush-5rh.13`. Grounded against substrate contract + INV-S5 mechanism + iwzt + Phase 7 plugin SDK + ADRs c8a9/wxty/ghpx/jhl5/p7w0/lrt3/z1e7/3vsb/vie9. Filed `holomush-rops` (P1) for the broader colon-style sweep. |
| 2026-05-19 | design-reviewer READY round 3 | 3 rounds total. Round 1 NOT READY (2 critical + 3 important + 3 minor — all patched). Round 2 NOT READY (3 important + 4 minor — all patched). Round 3 READY (3 minor — all patched inline). |
| 2026-05-19 | ADRs captured | 5 ADRs extracted via capture-adrs flow: holomush-r4th (denormalize pose-order metadata), holomush-s9nu (atomic subject migration), holomush-sb3n (sensitivity:always for content), holomush-nt2d (generalized participant gate, supersedes c8a9), holomush-1ang (audit interface extension). |

<!-- adr-capture: sha256=2c0eec2c61bb5c85; ts=2026-05-19T12:00:00Z; adrs=holomush-r4th,holomush-s9nu,holomush-sb3n,holomush-nt2d,holomush-1ang -->
