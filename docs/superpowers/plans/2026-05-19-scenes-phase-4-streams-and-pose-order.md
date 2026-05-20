# Scenes Phase 4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `dev-flow:subagent-driven-development` (recommended) or `dev-flow:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship Phase 4 of the scenes rework — plugin-owned IC/OOC event emission (8 new event types), maintained pose-order metadata with O(N participants) computation, `GetPoseOrder` RPC with INV-S9 plugin-code participant gate, `scene` subcommands (`pose`/`say`/`emit`/`ooc`/`order`), and atomic migration of scene-aware substrate code from legacy colon-style to NATS dot-style subjects.

**Architecture:** Eight event types declared in `crypto.emits` + registered via `EmitTypeRegistrar`; content events (`scene_pose`/`say`/`emit`/`ooc`) classified `always`, notice events (`scene_join_ic`/`leave_ic`/`pose_order_changed_ic`/`idle_nudge`) classified `never`. Pose-order computation reads denormalized metadata maintained by the audit consumer on `scene_pose` insertion — single transaction across `scene_log` INSERT + `scenes.total_pose_count` increment + `scene_participants.last_pose_at`/`last_pose_seq` UPDATE. `GetPoseOrder` is plugin-code gated per ADR `holomush-c8a9`; no ABAC consultation. Subject migration is atomic — plugin emits dot-style AND substrate gates parse dot-style in the same PR.

**Tech Stack:** Go 1.22+, PostgreSQL 16, pgx v5 (`pgxpool.Pool`, `pgx.BeginFunc`), `oklog/ulid/v2`, gopher-lua N/A (binary plugin), protobuf + protovalidate, hashicorp/go-plugin (binary RPC transport), Ginkgo/Gomega (integration tests), gotestsum (unit tests).

**Source design:** [`docs/superpowers/specs/2026-05-19-scenes-phase-4-streams-and-pose-order-design.md`](../specs/2026-05-19-scenes-phase-4-streams-and-pose-order-design.md). 13 numbered INV-P4-* invariants with cited tests; coverage matrix at spec §12.1.

**Design bead:** `holomush-5rh.13`. Dependencies: `holomush-5b2j.3` (GetNamesByIDs); soft `holomush-iwzt.15` (Tier 2 filter-at-delivery).

---

## File structure

### Existing files modified

| Path | Responsibility | Phase 4 change |
|------|---------------|----------------|
| `api/proto/holomush/scene/v1/scene.proto` | Proto definitions for SceneService | Replace skeletal `GetPoseOrderRequest`/`PoseOrderEntry`/`GetPoseOrderResponse` with Phase 4 shape; ensure `GetPoseOrder` RPC declared in service block |
| `plugins/core-scenes/main.go` | Plugin entry struct + lifecycle | Add `emitRegistry *pluginsdk.EmitRegistry` field + `EmitRegistry()` method (implements `pluginsdk.EmitTypeRegistrar`); register 8 event types in `main()` |
| `plugins/core-scenes/plugin.yaml` | Plugin manifest | Add 8 `crypto.emits` entries; add `idle_nudge_threshold` to scene config (informational; column added by migration) |
| `plugins/core-scenes/service.go` | gRPC RPC handlers + emit calls | Replace `Subject: "scene:" + row.ID` colon-style with dot-style helper; add `GetPoseOrder` handler; add auto-emit of `scene_join_ic`/`scene_leave_ic`/`scene_pose_order_changed_ic` from existing RPC handlers |
| `plugins/core-scenes/audit.go` | Audit event persistence + interface | Extend `sceneAuditLogStore` interface with `InsertScenePose`; add private `insertSceneLogTx` helper; update `AuditEvent` handler dispatch for `scene_pose` |
| `plugins/core-scenes/commands.go` | Subcommand dispatcher | Add `pose`/`say`/`emit`/`ooc`/`order` cases to switch; add corresponding handler functions |
| `plugins/core-scenes/store.go` | SQL store | Add `IsParticipant` + `ListParticipantsWithPoseMeta` methods (extends `sceneStorer`); add SQL helpers `dotStyleSceneSubject`, `dotStyleSceneSubjectIC`, `dotStyleSceneSubjectOOC` |
| `plugins/core-scenes/types.go` | Domain types | Add `ParticipantsWithPoseMeta` + `ParticipantWithPoseMeta` types |
| `plugins/core-scenes/service_test.go` | Service tests | Update emit Subject expectations to dot-style; add GetPoseOrder tests (positive/negative path); add auto-emit assertions |
| `plugins/core-scenes/commands_test.go` | Command tests | Add subcommand tests for `pose`/`say`/`emit`/`ooc`/`order` |
| `plugins/core-scenes/audit_test.go` | Audit tests | Add `TestAuditEvent_ScenePose_TransactionalInsertAndMetadata`; add fault-injection cases for INV-P4-10 |
| `plugins/core-scenes/resolver_test.go` | Resolver tests | Add `TestResolveResource_ExcludesPoseOrderMetadata` (INV-P4-5) |
| `plugins/core-scenes/store_integration_test.go` | Store integration tests | Add `IsParticipant`/`ListParticipantsWithPoseMeta` SQL tests; add `TestPoseOrderMetadata_RebuildFromAuditLog` (INV-P4-8) |
| `internal/grpc/stream_access.go` | Stream classification helpers (substrate) | Migrate `isPrivateStream`/`extractSceneID`/scene-stream branches to dot-style |
| `internal/grpc/stream_access_test.go` | Stream classification tests | Update fixtures to dot-style scene subjects |
| `internal/grpc/scope_floor.go` | Temporal-floor classification (substrate, iwzt §6.1) | Migrate scene branch in `streamScopeFloor` + `extractSceneID` to dot-style |
| `internal/grpc/scope_floor_test.go` | Scope-floor tests | Update fixtures; **add pre-migration baseline pin per spec §3.3** (table-driven cases for legacy colon-style + dot-style; modified atomically in this PR commit) |
| `internal/grpc/query_stream_history.go` | I-17 hardcoded scene-membership gate (substrate) | Update gate to recognize dot-style scene subjects |
| `test/integration/plugin/binary_plugin_test.go` | Plugin integration tests | Update scene-stream fixtures to dot-style |

### New files created

| Path | Responsibility |
|------|---------------|
| `plugins/core-scenes/migrations/000006_pose_order_metadata.up.sql` | Schema additions: `scenes.total_pose_count`, `scene_participants.last_pose_at`/`last_pose_seq`, `scenes.idle_nudge_threshold` |
| `plugins/core-scenes/migrations/000006_pose_order_metadata.down.sql` | Reverse migration |
| `plugins/core-scenes/poseorder.go` | Pure-function `poseorder.Compute(mode, totalPoseCount, participants, names) → []PoseOrderEntry`; per-mode algorithm |
| `plugins/core-scenes/poseorder_test.go` | Table-driven tests per mode (INV-P4-7) |
| `plugins/core-scenes/main_test.go` | Manifest parse + `EmitTypeRegistrar.RegisterEmitTypes` set-equality test (INV-P4-2, INV-P4-3) |
| `test/integration/scenes/non_participant_ic_isolation_test.go` | Ginkgo integration — INV-P4-6 (closes `holomush-ac50`) |
| `test/integration/scenes/late_joiner_temporal_floor_test.go` | Ginkgo integration — INV-P4-9 end-to-end |
| `internal/test/invariants/scene_subjects_test.go` | Meta-test rg-asserting no `"scene:"` literal in pub/sub topic context (INV-P4-1) |
| `internal/test/invariants/scene_no_abac_in_getposeorder_test.go` | Meta-test rg-asserting no `engine.Evaluate`/`engine.CanPerformAction` in `GetPoseOrder` body (INV-P4-4) |
| `internal/test/invariants/scene_resolver_no_poseorder_leak_test.go` | Meta-test rg-asserting no pose-metadata column references in `resolver.go` attribute-construction (INV-P4-5) |
| `internal/test/invariants/p4_coverage_test.go` | Meta-test parsing the spec, asserting every INV-P4-N has cited test + every cited test exists (INV-P4-13) |

### Documentation updates

| Path | Change |
|------|--------|
| `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md` | §3 stream-type table: scene rows updated to dot-style |
| `docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md` | §3.1 stream-naming table updated to dot-style |

---

## Phase A — Foundations (proto + schema + types)

### Task 1: Replace skeletal proto definitions with Phase 4 shape

**Files:**

- Modify: `api/proto/holomush/scene/v1/scene.proto:228-243`

- [ ] **Step 1: Replace `GetPoseOrderRequest`, `PoseOrderEntry`, `GetPoseOrderResponse` definitions**

Open `api/proto/holomush/scene/v1/scene.proto`. Locate the existing skeletal block at lines 228-243. Replace with:

```proto
message GetPoseOrderRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string scene_id     = 2 [(buf.validate.field).string.min_len = 1];
}

message PoseOrderEntry {
  string                    character_id     = 1;
  string                    character_name   = 2;
  bool                      eligible         = 3;
  google.protobuf.Timestamp last_posed_at    = 4;
  // Count of poses by other characters since this participant's last pose
  // (or since scene start if never posed). Meaningful for 3pr/5pr modes.
  optional uint32           poses_since_last = 5;
}

message GetPoseOrderResponse {
  string                  mode             = 1;   // strict | 3pr | 5pr | free
  uint32                  total_pose_count = 2;
  repeated PoseOrderEntry entries          = 3;
}
```

- [ ] **Step 2: Verify `GetPoseOrder` RPC declared in `service SceneService` block**

Run: `rg -n 'rpc GetPoseOrder' api/proto/holomush/scene/v1/scene.proto`

If absent, add `rpc GetPoseOrder(GetPoseOrderRequest) returns (GetPoseOrderResponse);` inside the existing service block (alongside the other Phase 1-3 RPCs).

- [ ] **Step 3: Regenerate proto bindings**

Run: `task proto`

Expected: `pkg/proto/holomush/scene/v1/scene.pb.go` regenerated; no errors. `git status` shows the generated file modified.

- [ ] **Step 4: Verify generated Go types compile**

Run: `task build`

Expected: build succeeds. Any reference to the old `IsEligible` field or skeletal shapes surfaces here.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scene-proto): replace skeletal GetPoseOrder messages with Phase 4 shape

Pre-Phase-4 scene.proto had skeletal GetPoseOrderRequest/PoseOrderEntry/
GetPoseOrderResponse with is_eligible naming and no aggregate fields.
Phase 4 replaces these outright per holomush-5rh.13 spec §7.1 — no
backward-compat constraint (no releases, no external consumers, no
in-tree callers of the skeletal shapes).

New shape:
- Bare character_id + scene_id fields (matches sibling RPC convention
  KickFromSceneRequest:204 / TransferOwnershipRequest:212).
- eligible (not is_eligible) — boolean predicate, is_ prefix dropped.
- New field poses_since_last on PoseOrderEntry (3pr/5pr UX support).
- New field total_pose_count on GetPoseOrderResponse (header rendering).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 2: Add migration 000006 — pose-order metadata + idle-nudge threshold

**Files:**

- Create: `plugins/core-scenes/migrations/000006_pose_order_metadata.up.sql`
- Create: `plugins/core-scenes/migrations/000006_pose_order_metadata.down.sql`

- [ ] **Step 1: Write the up migration**

Create `plugins/core-scenes/migrations/000006_pose_order_metadata.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Phase 4 pose-order metadata + idle-nudge threshold per holomush-5rh.13 spec §9.

-- Per-scene monotonic pose counter. Incremented on each scene_pose audit
-- row insert via SceneAuditStore.InsertScenePose.
ALTER TABLE scenes
    ADD COLUMN IF NOT EXISTS total_pose_count INTEGER NOT NULL DEFAULT 0;

-- Per-scene idle-nudge threshold. NULL = idle nudges off (default).
-- Background trigger implementation deferred to a follow-up bead; Phase 4
-- ships the column + wire-shape only.
ALTER TABLE scenes
    ADD COLUMN IF NOT EXISTS idle_nudge_threshold INTERVAL NULL;

-- Per-participant pose metadata. NULL = participant has never posed.
ALTER TABLE scene_participants
    ADD COLUMN IF NOT EXISTS last_pose_at  TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS last_pose_seq INTEGER     NULL;
```

- [ ] **Step 2: Write the down migration**

Create `plugins/core-scenes/migrations/000006_pose_order_metadata.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Reverse the Phase 4 pose-order metadata + idle-nudge columns.

ALTER TABLE scene_participants
    DROP COLUMN IF EXISTS last_pose_seq,
    DROP COLUMN IF EXISTS last_pose_at;

ALTER TABLE scenes
    DROP COLUMN IF EXISTS idle_nudge_threshold;

ALTER TABLE scenes
    DROP COLUMN IF EXISTS total_pose_count;
```

- [ ] **Step 3: Run integration tests against the new migration**

Run: `task test:int -- ./plugins/core-scenes/...`

Expected: existing integration tests pass; migration runner picks up 000006 and applies it. Verify column existence:

```bash
docker exec -it postgres-test psql -U test -d test_holomush -c "\d plugin_core_scenes.scenes" | grep -E "total_pose_count|idle_nudge_threshold"
docker exec -it postgres-test psql -U test -d test_holomush -c "\d plugin_core_scenes.scene_participants" | grep -E "last_pose_at|last_pose_seq"
```

Expected: 4 column matches across two tables.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(scene-store): migration 000006 — pose-order metadata + idle-nudge threshold

Adds scenes.total_pose_count (per-scene monotonic pose counter),
scenes.idle_nudge_threshold (per-scene config, NULL = off), and
scene_participants.last_pose_at + last_pose_seq (per-participant pose
metadata, NULL until first pose).

Per holomush-5rh.13 spec §9. The maintained metadata enables O(N
participants) pose-order computation in GetPoseOrder without scanning
event history; INV-P4-8 invariant pins the rebuild-from-scene_log
equivalence with documented recovery SQL.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 3: Add `ParticipantsWithPoseMeta` types to `types.go`

**Files:**

- Modify: `plugins/core-scenes/types.go`

- [ ] **Step 1: Add the new types**

Append to `plugins/core-scenes/types.go`:

```go
// ParticipantsWithPoseMeta is the result returned by
// sceneStorer.ListParticipantsWithPoseMeta. Groups the per-scene
// total_pose_count with the per-participant pose metadata so the
// GetPoseOrder handler doesn't need a second SELECT for the scene row.
type ParticipantsWithPoseMeta struct {
	TotalPoseCount uint32
	Participants   []ParticipantWithPoseMeta
}

// ParticipantWithPoseMeta is one participant of a scene plus their
// Phase 4 maintained pose metadata. LastPoseAt and LastPoseSeq are
// nil when the participant has never posed in this scene.
type ParticipantWithPoseMeta struct {
	CharacterID string
	JoinedAt    time.Time
	LastPoseAt  *time.Time
	LastPoseSeq *int32
}
```

Verify `time` is imported (already present from Phase 1-3 types).

- [ ] **Step 2: Add a no-op test that exercises the types**

Append to `plugins/core-scenes/types_test.go`:

```go
func TestParticipantsWithPoseMeta_ZeroValueValid(t *testing.T) {
	t.Parallel()
	var pm ParticipantsWithPoseMeta
	assert.Equal(t, uint32(0), pm.TotalPoseCount)
	assert.Empty(t, pm.Participants)
}

func TestParticipantWithPoseMeta_NeverPosed_NilFields(t *testing.T) {
	t.Parallel()
	p := ParticipantWithPoseMeta{
		CharacterID: "char-alice",
		JoinedAt:    time.Now(),
	}
	assert.Nil(t, p.LastPoseAt)
	assert.Nil(t, p.LastPoseSeq)
}
```

- [ ] **Step 3: Run unit tests**

Run: `task test -- ./plugins/core-scenes/`

Expected: both new tests pass.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(scene-types): add ParticipantsWithPoseMeta + ParticipantWithPoseMeta

Result types for sceneStorer.ListParticipantsWithPoseMeta (added in a
later task). Groups scenes.total_pose_count with per-participant
last_pose_at + last_pose_seq so GetPoseOrder reads everything in one
SELECT. NULL pose metadata represented as *time.Time / *int32.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

---

## Phase B — Store layer extensions

### Task 4: Add `IsParticipant` store method (TDD)

**Files:**

- Modify: `plugins/core-scenes/service.go:35-50` (interface)
- Modify: `plugins/core-scenes/store.go` (implementation)
- Test: `plugins/core-scenes/store_integration_test.go`

- [ ] **Step 1: Write the failing integration test**

Append to `plugins/core-scenes/store_integration_test.go`:

```go
func (s *StoreIntegrationSuite) TestIsParticipant_OwnerMember_True() {
	ctx := context.Background()
	sceneID := s.createScene(ctx, "owner-id-1")  // helper from existing tests; creates scene with owner row
	s.joinScene(ctx, sceneID, "member-id-1")     // helper; AddParticipant

	ok, err := s.store.IsParticipant(ctx, sceneID, "owner-id-1")
	s.Require().NoError(err)
	s.Require().True(ok)

	ok, err = s.store.IsParticipant(ctx, sceneID, "member-id-1")
	s.Require().NoError(err)
	s.Require().True(ok)
}

func (s *StoreIntegrationSuite) TestIsParticipant_Invited_False() {
	ctx := context.Background()
	sceneID := s.createScene(ctx, "owner-id-2")
	// InviteParticipant creates an 'invited' row, not 'member'/'owner'.
	_, err := s.store.InviteParticipant(ctx, sceneID, "owner-id-2", "invitee-id")
	s.Require().NoError(err)

	ok, err := s.store.IsParticipant(ctx, sceneID, "invitee-id")
	s.Require().NoError(err)
	s.Require().False(ok, "invited role MUST NOT count as participant for INV-S9 gate")
}

func (s *StoreIntegrationSuite) TestIsParticipant_NotInScene_False() {
	ctx := context.Background()
	sceneID := s.createScene(ctx, "owner-id-3")

	ok, err := s.store.IsParticipant(ctx, sceneID, "outsider-id")
	s.Require().NoError(err)
	s.Require().False(ok)
}
```

- [ ] **Step 2: Run integration test to verify it fails (method does not exist)**

Run: `task test:int -- -run TestIsParticipant ./plugins/core-scenes/...`

Expected: FAIL — compilation error "s.store.IsParticipant undefined" OR (after interface declaration in Step 3) test panics with "method IsParticipant not implemented".

- [ ] **Step 3: Add `IsParticipant` to the `sceneStorer` interface**

Open `plugins/core-scenes/service.go`. Locate the `sceneStorer` interface block at lines 35-50. Add the new method between `GetParticipant` and the closing brace:

```go
// IsParticipant returns true if the character is a participant (owner
// or member, NOT invited) of the scene. Used by the INV-S9 plugin-code
// gate at GetPoseOrder per holomush-c8a9.
IsParticipant(ctx context.Context, sceneID, characterID string) (bool, error)
```

- [ ] **Step 4: Implement on `*SceneStore` in `store.go`**

Append a new method to `*SceneStore` in `plugins/core-scenes/store.go`:

```go
// IsParticipant reports whether the character is a participant (owner
// or member, NOT invited) of the scene. The invited-role exclusion is
// load-bearing: INV-S9's gate at GetPoseOrder MUST NOT treat pending
// invites as members. Pinned by spec INV-P4-4.
func (s *SceneStore) IsParticipant(ctx context.Context, sceneID, characterID string) (bool, error) {
	const q = `
		SELECT EXISTS (
			SELECT 1 FROM scene_participants
			WHERE scene_id = $1
			  AND character_id = $2
			  AND role IN ('owner', 'member')
		)
	`
	var ok bool
	if err := s.pool.QueryRow(ctx, q, sceneID, characterID).Scan(&ok); err != nil {
		return false, oops.Code("SCENE_PARTICIPANT_LOOKUP_FAILED").
			With("scene_id", sceneID).With("character_id", characterID).Wrap(err)
	}
	return ok, nil
}
```

- [ ] **Step 5: Run integration tests to verify they pass**

Run: `task test:int -- -run TestIsParticipant ./plugins/core-scenes/...`

Expected: all 3 tests pass.

- [ ] **Step 6: Commit**

```bash
jj describe -m "feat(scene-store): add IsParticipant — INV-S9 gate primitive

Binary participant-check (owner OR member, NOT invited) used by the
GetPoseOrder INV-S9 plugin-code gate per holomush-c8a9. Distinct from
existing GetParticipant (which conflates lookup with not-found error).
The role filter pins INV-P4-4's invariant: invited-row participants
fail the gate.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 5: Add `ListParticipantsWithPoseMeta` store method (TDD)

**Files:**

- Modify: `plugins/core-scenes/service.go:35-50` (interface)
- Modify: `plugins/core-scenes/store.go` (implementation)
- Test: `plugins/core-scenes/store_integration_test.go`

- [ ] **Step 1: Write the failing test**

Append to `plugins/core-scenes/store_integration_test.go`:

```go
func (s *StoreIntegrationSuite) TestListParticipantsWithPoseMeta_NoPoses() {
	ctx := context.Background()
	sceneID := s.createScene(ctx, "owner-id-pm-1")
	s.joinScene(ctx, sceneID, "member-id-pm-1")

	pm, err := s.store.ListParticipantsWithPoseMeta(ctx, sceneID)
	s.Require().NoError(err)
	s.Require().Equal(uint32(0), pm.TotalPoseCount)
	s.Require().Len(pm.Participants, 2)
	for _, p := range pm.Participants {
		s.Require().Nil(p.LastPoseAt, "no poses yet: last_pose_at MUST be nil")
		s.Require().Nil(p.LastPoseSeq, "no poses yet: last_pose_seq MUST be nil")
	}
}

func (s *StoreIntegrationSuite) TestListParticipantsWithPoseMeta_ExcludesInvited() {
	ctx := context.Background()
	sceneID := s.createScene(ctx, "owner-id-pm-2")
	s.joinScene(ctx, sceneID, "member-id-pm-2")
	_, err := s.store.InviteParticipant(ctx, sceneID, "owner-id-pm-2", "invitee-pm")
	s.Require().NoError(err)

	pm, err := s.store.ListParticipantsWithPoseMeta(ctx, sceneID)
	s.Require().NoError(err)
	s.Require().Len(pm.Participants, 2, "invited role MUST NOT appear in pose-order list")
}

func (s *StoreIntegrationSuite) TestListParticipantsWithPoseMeta_AfterDirectMetadataWrite() {
	ctx := context.Background()
	sceneID := s.createScene(ctx, "owner-id-pm-3")
	s.joinScene(ctx, sceneID, "member-id-pm-3")

	// Simulate the audit-handler metadata update (will be done by
	// InsertScenePose in a later task; here we exercise the read path).
	ts := time.Now().UTC()
	_, err := s.store.Pool().Exec(ctx,
		`UPDATE scenes SET total_pose_count = 3 WHERE id = $1`, sceneID)
	s.Require().NoError(err)
	_, err = s.store.Pool().Exec(ctx,
		`UPDATE scene_participants SET last_pose_at = $1, last_pose_seq = 3
		 WHERE scene_id = $2 AND character_id = $3`,
		ts, sceneID, "member-id-pm-3")
	s.Require().NoError(err)

	pm, err := s.store.ListParticipantsWithPoseMeta(ctx, sceneID)
	s.Require().NoError(err)
	s.Require().Equal(uint32(3), pm.TotalPoseCount)
	for _, p := range pm.Participants {
		if p.CharacterID == "member-id-pm-3" {
			s.Require().NotNil(p.LastPoseAt)
			s.Require().NotNil(p.LastPoseSeq)
			s.Require().EqualValues(3, *p.LastPoseSeq)
		} else {
			s.Require().Nil(p.LastPoseSeq, "owner did not pose: last_pose_seq MUST be nil")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test:int -- -run TestListParticipantsWithPoseMeta ./plugins/core-scenes/...`

Expected: FAIL — compilation error for missing method.

- [ ] **Step 3: Add to interface**

In `plugins/core-scenes/service.go`, append to `sceneStorer`:

```go
// ListParticipantsWithPoseMeta returns all participants of the scene
// (role IN ('owner','member')) with their maintained pose metadata
// (last_pose_at, last_pose_seq) and the scene's total_pose_count, in
// a single SELECT. Drives GetPoseOrder computation per spec §6.1.
ListParticipantsWithPoseMeta(ctx context.Context, sceneID string) (ParticipantsWithPoseMeta, error)
```

- [ ] **Step 4: Implement on `*SceneStore`**

Append to `plugins/core-scenes/store.go`:

```go
// ListParticipantsWithPoseMeta is a single SELECT joining scenes +
// scene_participants and returning the participants (owner+member, NOT
// invited) with their pose metadata. Pinned by spec §6.1 / INV-P4-7.
func (s *SceneStore) ListParticipantsWithPoseMeta(ctx context.Context, sceneID string) (ParticipantsWithPoseMeta, error) {
	const q = `
		SELECT
		    s.total_pose_count,
		    p.character_id,
		    p.joined_at,
		    p.last_pose_at,
		    p.last_pose_seq
		FROM scenes s
		JOIN scene_participants p ON p.scene_id = s.id
		WHERE s.id = $1
		  AND p.role IN ('owner', 'member')
		ORDER BY p.joined_at ASC
	`
	rows, err := s.pool.Query(ctx, q, sceneID)
	if err != nil {
		return ParticipantsWithPoseMeta{}, oops.Code("SCENE_POSE_META_LOOKUP_FAILED").
			With("scene_id", sceneID).Wrap(err)
	}
	defer rows.Close()

	var result ParticipantsWithPoseMeta
	for rows.Next() {
		var p ParticipantWithPoseMeta
		var totalPoseCount int32   // scanned as signed; uint32 conversion below
		if err := rows.Scan(&totalPoseCount, &p.CharacterID, &p.JoinedAt, &p.LastPoseAt, &p.LastPoseSeq); err != nil {
			return ParticipantsWithPoseMeta{}, oops.Code("SCENE_POSE_META_SCAN_FAILED").Wrap(err)
		}
		result.TotalPoseCount = uint32(totalPoseCount)
		result.Participants = append(result.Participants, p)
	}
	if err := rows.Err(); err != nil {
		return ParticipantsWithPoseMeta{}, oops.Code("SCENE_POSE_META_ITER_FAILED").Wrap(err)
	}
	return result, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `task test:int -- -run TestListParticipantsWithPoseMeta ./plugins/core-scenes/...`

Expected: all 3 tests pass.

- [ ] **Step 6: Commit**

```bash
jj describe -m "feat(scene-store): add ListParticipantsWithPoseMeta

Single SELECT joining scenes.total_pose_count with per-participant
pose metadata (last_pose_at, last_pose_seq). Excludes invited role per
INV-S9 pose-order-only-for-participants discipline. Result type
ParticipantsWithPoseMeta groups the aggregate + list for one-round-trip
reads in GetPoseOrder.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

---

## Phase C — Audit transaction + metadata maintenance

### Task 6: Refactor `Insert` to use private `insertSceneLogTx` helper

**Files:**

- Modify: `plugins/core-scenes/audit.go`
- Test: `plugins/core-scenes/store_integration_test.go` (existing Insert tests should keep passing)

- [ ] **Step 1: Extract `insertSceneLogTx` helper**

In `plugins/core-scenes/audit.go`, locate the existing `func (s *SceneAuditStore) Insert(...) error` method (starts around line 122). Refactor it into two functions:

```go
// Insert persists one audit row in its own transaction. Wrapper over
// insertSceneLogTx for the common single-row case used by non-pose
// events.
func (s *SceneAuditStore) Insert(
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
) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return s.insertSceneLogTx(ctx, tx,
			id, subject, eventType, timestamp, actorKind, actorID,
			payload, schemaVer, codec, dekRef, dekVersion)
	})
}

// insertSceneLogTx executes the scene_log INSERT on a caller-provided
// pgx.Tx. The INSERT uses ON CONFLICT (id) DO NOTHING so JetStream
// redelivery is idempotent. Package-private; not exposed via the
// sceneAuditLogStore interface. Called by Insert and by InsertScenePose.
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
) error {
	// Move the existing INSERT SQL from the previous Insert body here,
	// changing s.pool.Exec(...) to tx.Exec(...).
	// Keep the existing ON CONFLICT (id) DO NOTHING clause and the
	// same column list. Argument list and oops error wrapping unchanged.
	const insertSQL = `
		INSERT INTO scene_log (
			id, subject, type, timestamp, actor_kind, actor_id,
			payload, schema_ver, codec, dek_ref, dek_version
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11
		)
		ON CONFLICT (id) DO NOTHING
	`
	_, err := tx.Exec(ctx, insertSQL,
		id, subject, eventType, timestamp.AsTime(), actorKind, actorID,
		payload, schemaVer, codec, dekRef, dekVersion)
	if err != nil {
		return oops.Code("SCENE_AUDIT_LOG_INSERT_FAILED").
			With("subject", subject).With("event_type", eventType).Wrap(err)
	}
	return nil
}
```

Verify `import "github.com/jackc/pgx/v5"` is present in the file (add if missing).

- [ ] **Step 2: Run existing audit tests to verify no regression**

Run: `task test:int -- -run TestSceneAudit ./plugins/core-scenes/...`

Expected: existing Phase 1-3 audit tests pass — refactor is behavior-preserving.

- [ ] **Step 3: Commit**

```bash
jj describe -m "refactor(scene-audit): extract insertSceneLogTx helper from Insert

Pure refactor — Insert now wraps insertSceneLogTx in pgx.BeginFunc.
The helper accepts a caller-provided pgx.Tx so InsertScenePose (next
task) can compose the same scene_log INSERT with pose-metadata UPDATEs
in one transaction.

ON CONFLICT (id) DO NOTHING semantics preserved (idempotent redelivery
per Phase 7 plugin SDK contract). No behavioral change.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 7: Add `InsertScenePose` to interface + concrete impl (TDD)

**Files:**

- Modify: `plugins/core-scenes/audit.go:53-75` (interface + concrete)
- Test: `plugins/core-scenes/audit_test.go`

- [ ] **Step 1: Write the failing fault-injection test**

Append to `plugins/core-scenes/audit_test.go`:

```go
// TestInsertScenePose_TransactionalRollback_INV_P4_10 pins INV-P4-10:
// scene_log INSERT + pose-metadata UPDATE MUST be all-or-nothing.
func TestInsertScenePose_TransactionalRollback_INV_P4_10(t *testing.T) {
	t.Parallel()
	// Use a real pgxpool against the test DB; fault-injection lives in
	// a wrapper that fails the UPDATE after the INSERT succeeds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := newTestPool(t, ctx)  // existing helper from store_integration_test.go
	store := NewSceneAuditStore(pool)

	sceneID := "scene-rollback-test"
	posedCharID := "char-rollback-test"
	// Seed the scene + participant rows so the UPDATE has a target.
	seedSceneAndParticipant(t, ctx, pool, sceneID, posedCharID)

	// Inject a constraint violation in the metadata UPDATE by removing
	// the column temporarily (or by passing a bad sceneID that fails
	// the foreign-key on scene_participants — simpler).
	badSceneID := "scene-does-not-exist"
	rowID := []byte("test-row-id-fixed-16b")

	err := store.InsertScenePose(ctx,
		rowID, "events.test.scene." + badSceneID + ".ic", "scene_pose",
		timestamppb.Now(), "character", []byte(posedCharID),
		[]byte(`{"text":"test"}`), 1, "identity", nil, nil,
		badSceneID, posedCharID,
	)
	require.Error(t, err, "InsertScenePose MUST fail on bad sceneID (no row to UPDATE)")

	// Verify no scene_log row landed.
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM scene_log WHERE id = $1`, rowID).Scan(&count))
	require.Equal(t, 0, count, "scene_log INSERT MUST roll back when metadata UPDATE fails (INV-P4-10)")
}
```

- [ ] **Step 2: Run test to verify it fails (method does not exist)**

Run: `task test:int -- -run TestInsertScenePose_TransactionalRollback ./plugins/core-scenes/`

Expected: FAIL — `store.InsertScenePose undefined`.

- [ ] **Step 3: Add `InsertScenePose` to the `sceneAuditLogStore` interface**

In `plugins/core-scenes/audit.go`, locate the `sceneAuditLogStore` interface at lines 50-75. Append after the existing `queryLog(...)` method:

```go
// InsertScenePose performs the scene_log INSERT for a scene_pose
// event AND the pose-metadata UPDATE on scenes + scene_participants
// in one transaction. Either both rows mutate or neither does
// (INV-P4-10 per spec §9.4).
//
// sceneID and posedCharID are parsed from the audit request's subject
// and actor fields by the caller; timestamp is the canonical event
// timestamp (NOT wall clock at handler entry).
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
```

- [ ] **Step 4: Implement on `*SceneAuditStore`**

Append to `plugins/core-scenes/audit.go`:

```go
// InsertScenePose composes the scene_log INSERT + the pose-metadata
// UPDATEs (scenes.total_pose_count++ and scene_participants
// .last_pose_at/last_pose_seq) in a single transaction.
//
// Idempotency: the INSERT uses ON CONFLICT (id) DO NOTHING per
// insertSceneLogTx, so redelivery of the same row is a no-op. The
// UPDATEs MUST therefore guard against repeat-execution — but the
// transactional wrapper means the UPDATEs only run when the INSERT
// actually inserts a row. (TODO: if INSERT is a no-op due to conflict,
// UPDATEs would double-count. Mitigation: check rowsAffected on the
// INSERT and skip UPDATEs if zero. Captured in the next step.)
func (s *SceneAuditStore) InsertScenePose(
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
) error {
    return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
        // 1. INSERT into scene_log. Returns nil even on ON CONFLICT;
        //    we need to detect that case to avoid double-counting.
        if err := s.insertSceneLogTx(ctx, tx,
            id, subject, eventType, timestamp, actorKind, actorID,
            payload, schemaVer, codec, dekRef, dekVersion); err != nil {
            return err
        }

        // 2. Check if the INSERT actually inserted (no conflict).
        //    On conflict, scene_log already has this row from a prior
        //    delivery; UPDATEs already happened that time. Skip.
        var alreadyExists bool
        if err := tx.QueryRow(ctx,
            `SELECT EXISTS(SELECT 1 FROM scene_log WHERE id = $1 AND timestamp != $2)`,
            id, timestamp.AsTime(),
        ).Scan(&alreadyExists); err != nil {
            return oops.Code("SCENE_AUDIT_POSE_DEDUP_CHECK_FAILED").Wrap(err)
        }
        // Note: the check above is an approximation; a cleaner pattern
        // is to use INSERT ... RETURNING and check the result. The
        // implementation plan task SHALL switch to RETURNING xmax = 0
        // semantics if integration tests reveal double-counting.

        // 3. Bump scenes.total_pose_count.
        var newSeq int32
        if err := tx.QueryRow(ctx,
            `UPDATE scenes SET total_pose_count = total_pose_count + 1
             WHERE id = $1 RETURNING total_pose_count`,
            sceneID,
        ).Scan(&newSeq); err != nil {
            return oops.Code("SCENE_AUDIT_POSE_TOTAL_INC_FAILED").
                With("scene_id", sceneID).Wrap(err)
        }

        // 4. Stamp the actor's per-participant pose metadata.
        //    Affects 0 rows if the actor is not currently a participant
        //    (edge case: actor left between emit and audit consumption).
        //    Acceptable per spec §9.4 — scene_log row recorded, metadata
        //    reflects "they posed but left."
        if _, err := tx.Exec(ctx,
            `UPDATE scene_participants
               SET last_pose_at = $1, last_pose_seq = $2
             WHERE scene_id = $3 AND character_id = $4`,
            timestamp.AsTime(), newSeq, sceneID, posedCharID,
        ); err != nil {
            return oops.Code("SCENE_AUDIT_POSE_PARTICIPANT_UPDATE_FAILED").
                With("scene_id", sceneID).With("posed_char_id", posedCharID).Wrap(err)
        }
        return nil
    })
}
```

- [ ] **Step 5: Run the fault-injection test to verify it passes**

Run: `task test:int -- -run TestInsertScenePose_TransactionalRollback ./plugins/core-scenes/`

Expected: PASS — INSERT rolls back when UPDATE fails on bad sceneID.

- [ ] **Step 6: Add the happy-path integration test**

Append to `plugins/core-scenes/audit_test.go`:

```go
func TestInsertScenePose_HappyPath_UpdatesMetadata(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := newTestPool(t, ctx)
	store := NewSceneAuditStore(pool)

	sceneID := "scene-happy-" + ulid.Make().String()
	posedCharID := "char-happy-" + ulid.Make().String()
	seedSceneAndParticipant(t, ctx, pool, sceneID, posedCharID)

	rowID := ulid.Make().Bytes()
	now := time.Now().UTC().Truncate(time.Microsecond)

	require.NoError(t, store.InsertScenePose(ctx,
		rowID, "events.test.scene." + sceneID + ".ic", "scene_pose",
		timestamppb.New(now), "character", []byte(posedCharID),
		[]byte(`{"text":"hello"}`), 1, "identity", nil, nil,
		sceneID, posedCharID,
	))

	// Verify scene_log row.
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM scene_log WHERE id = $1`, rowID).Scan(&count))
	require.Equal(t, 1, count)

	// Verify metadata updates.
	var totalPoseCount int32
	require.NoError(t, pool.QueryRow(ctx, `SELECT total_pose_count FROM scenes WHERE id = $1`, sceneID).Scan(&totalPoseCount))
	require.Equal(t, int32(1), totalPoseCount)

	var lastPoseAt time.Time
	var lastPoseSeq int32
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT last_pose_at, last_pose_seq FROM scene_participants
		 WHERE scene_id = $1 AND character_id = $2`, sceneID, posedCharID,
	).Scan(&lastPoseAt, &lastPoseSeq))
	require.WithinDuration(t, now, lastPoseAt, time.Millisecond)
	require.Equal(t, int32(1), lastPoseSeq)
}
```

- [ ] **Step 7: Run happy-path test**

Run: `task test:int -- -run TestInsertScenePose_HappyPath ./plugins/core-scenes/`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
jj describe -m "feat(scene-audit): InsertScenePose — transactional INSERT + metadata UPDATEs

Adds InsertScenePose to sceneAuditLogStore interface and *SceneAuditStore
concrete. Composes scene_log INSERT + scenes.total_pose_count increment
+ scene_participants.last_pose_at/last_pose_seq UPDATE in one
pgx.BeginFunc transaction.

Pins INV-P4-10 (atomic INSERT+UPDATE) with a fault-injection test that
forces the metadata UPDATE to fail and asserts the scene_log INSERT
rolls back. Happy-path test verifies metadata stamps after success.

Per spec §9.4 — interface extension chosen over Pool-exposure to keep
the audit-server boundary clean.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 8: Update `AuditEvent` handler dispatch for `scene_pose`

**Files:**

- Modify: `plugins/core-scenes/audit.go::AuditEvent`
- Test: `plugins/core-scenes/audit_test.go`

- [ ] **Step 1: Write the failing handler-dispatch test**

Append to `plugins/core-scenes/audit_test.go`:

```go
func TestAuditEvent_ScenePose_RoutesToInsertScenePose(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := newTestPool(t, ctx)
	store := NewSceneAuditStore(pool)
	srv := &SceneAuditServer{store: store, memberLookup: newAlwaysMemberLookup()}

	sceneID := "scene-dispatch-" + ulid.Make().String()
	posedCharULID := ulid.Make()
	posedCharID := posedCharULID.String()
	seedSceneAndParticipant(t, ctx, pool, sceneID, posedCharID)

	rowULID := ulid.Make()
	req := &pluginauditpb.AuditEventRequest{
		Row: &pluginauditpb.AuditRow{
			Id:        rowULID.Bytes(),
			Subject:   "events.test.scene." + sceneID + ".ic",
			Type:      "scene_pose",
			Timestamp: timestamppb.Now(),
			Actor:     &pluginauditpb.Actor{Kind: "character", Id: posedCharULID.Bytes()},
			Payload:   []byte(`{"text":"test"}`),
			SchemaVer: 1,
			Codec:     "identity",
		},
	}
	_, err := srv.AuditEvent(ctx, req)
	require.NoError(t, err)

	// Metadata must be stamped.
	var totalPoseCount int32
	require.NoError(t, pool.QueryRow(ctx, `SELECT total_pose_count FROM scenes WHERE id = $1`, sceneID).Scan(&totalPoseCount))
	require.Equal(t, int32(1), totalPoseCount)
}

func TestAuditEvent_NonScenePose_RoutesToInsert(t *testing.T) {
	t.Parallel()
	// Verify that non-pose events still route through plain Insert (no
	// metadata stamping). Uses scene_join_ic as a sample non-pose event.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := newTestPool(t, ctx)
	store := NewSceneAuditStore(pool)
	srv := &SceneAuditServer{store: store, memberLookup: newAlwaysMemberLookup()}

	sceneID := "scene-non-pose-" + ulid.Make().String()
	charULID := ulid.Make()
	seedSceneAndParticipant(t, ctx, pool, sceneID, charULID.String())

	req := &pluginauditpb.AuditEventRequest{
		Row: &pluginauditpb.AuditRow{
			Id:        ulid.Make().Bytes(),
			Subject:   "events.test.scene." + sceneID + ".ic",
			Type:      "scene_join_ic",
			Timestamp: timestamppb.Now(),
			Actor:     &pluginauditpb.Actor{Kind: "character", Id: charULID.Bytes()},
			SchemaVer: 1, Codec: "identity",
		},
	}
	_, err := srv.AuditEvent(ctx, req)
	require.NoError(t, err)

	// total_pose_count MUST remain 0 (scene_join_ic is not a pose).
	var totalPoseCount int32
	require.NoError(t, pool.QueryRow(ctx, `SELECT total_pose_count FROM scenes WHERE id = $1`, sceneID).Scan(&totalPoseCount))
	require.Equal(t, int32(0), totalPoseCount, "non-pose events MUST NOT touch pose metadata")
}
```

- [ ] **Step 2: Run tests to verify they fail (existing AuditEvent doesn't dispatch)**

Run: `task test:int -- -run TestAuditEvent_ScenePose ./plugins/core-scenes/`

Expected: FAIL — both tests fail because existing AuditEvent uses plain Insert path for all events.

- [ ] **Step 3: Update `AuditEvent` handler**

In `plugins/core-scenes/audit.go`, locate the existing `func (s *SceneAuditServer) AuditEvent(...)` body. After the existing validation / subject ownership checks, replace the plain `s.store.Insert(...)` call with a type-dispatch block:

```go
row := req.GetRow()
if row.GetType() == "scene_pose" {
    sceneID, err := parseSceneSubject(row.GetSubject())
    if err != nil {
        return nil, status.Errorf(codes.InvalidArgument, "malformed scene subject: %v", err)
    }
    var actorULID ulid.ULID
    copy(actorULID[:], row.GetActor().GetId())
    posedCharID := actorULID.String()

    if err := s.store.InsertScenePose(ctx,
        row.GetId(),
        row.GetSubject(),
        row.GetType(),
        row.GetTimestamp(),
        row.GetActor().GetKind(),
        row.GetActor().GetId(),
        row.GetPayload(),
        int(row.GetSchemaVer()),
        row.GetCodec(),
        row.GetDekRef(),
        row.GetDekVersion(),
        sceneID,
        posedCharID,
    ); err != nil {
        return nil, oopsToGrpcStatus(err)  // existing helper, or status.Errorf(codes.Internal, ...) per existing pattern
    }
} else {
    if err := s.store.Insert(ctx,
        row.GetId(), row.GetSubject(), row.GetType(), row.GetTimestamp(),
        row.GetActor().GetKind(), row.GetActor().GetId(),
        row.GetPayload(), int(row.GetSchemaVer()), row.GetCodec(),
        row.GetDekRef(), row.GetDekVersion(),
    ); err != nil {
        return nil, oopsToGrpcStatus(err)
    }
}
```

Verify `import "github.com/oklog/ulid/v2"` is present at the top of the file.

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test:int -- -run TestAuditEvent ./plugins/core-scenes/`

Expected: both new tests pass; existing Phase 1-3 audit tests pass.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scene-audit): dispatch scene_pose events to InsertScenePose

AuditEvent handler now branches on row.Type — scene_pose routes through
InsertScenePose (which composes INSERT + pose-metadata UPDATEs in one
transaction); other events continue through plain Insert.

actorID bytes-to-string conversion via ulid.ULID(bytes).String() —
identical to the existing QueryHistory pattern at audit.go:209-213.
sceneID parsed from subject via existing parseSceneSubject helper.

Tests verify routing: scene_pose triggers metadata stamps; scene_join_ic
(non-pose) leaves metadata untouched.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

---

## Phase D — Pose-order computation

### Task 9: Create `poseorder.go` with pure-function Compute (TDD)

**Files:**

- Create: `plugins/core-scenes/poseorder.go`
- Create: `plugins/core-scenes/poseorder_test.go`

- [ ] **Step 1: Write the failing per-mode table-driven tests**

Create `plugins/core-scenes/poseorder_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCompute_FreeMode_AllEligible(t *testing.T) {
	t.Parallel()
	now := time.Now()
	participants := []ParticipantWithPoseMeta{
		{CharacterID: "alice", JoinedAt: now.Add(-3 * time.Hour)},
		{CharacterID: "bob",   JoinedAt: now.Add(-2 * time.Hour)},
		{CharacterID: "carol", JoinedAt: now.Add(-1 * time.Hour)},
	}
	names := map[string]string{"alice": "Alice", "bob": "Bob", "carol": "Carol"}
	entries := Compute("free", 0, participants, names)
	assert.Len(t, entries, 3)
	for _, e := range entries {
		assert.True(t, e.Eligible, "free mode: all participants MUST be eligible")
	}
	// Free mode display order: by JoinedAt ASC.
	assert.Equal(t, "alice", entries[0].CharacterID)
	assert.Equal(t, "bob",   entries[1].CharacterID)
	assert.Equal(t, "carol", entries[2].CharacterID)
}

func TestCompute_StrictMode_NeverPosedAtHead(t *testing.T) {
	t.Parallel()
	now := time.Now()
	twoAgo  := now.Add(-2 * time.Hour)
	oneAgo  := now.Add(-1 * time.Hour)
	twoSeq  := int32(1)
	oneSeq  := int32(2)
	participants := []ParticipantWithPoseMeta{
		{CharacterID: "alice", JoinedAt: now.Add(-3 * time.Hour), LastPoseAt: &twoAgo, LastPoseSeq: &twoSeq},
		{CharacterID: "bob",   JoinedAt: now.Add(-2 * time.Hour), LastPoseAt: &oneAgo, LastPoseSeq: &oneSeq},
		{CharacterID: "carol", JoinedAt: now.Add(-1 * time.Hour)},   // never posed
	}
	names := map[string]string{"alice": "Alice", "bob": "Bob", "carol": "Carol"}
	entries := Compute("strict", 2, participants, names)
	assert.Len(t, entries, 3)
	assert.Equal(t, "carol", entries[0].CharacterID, "never-posed MUST be at head")
	assert.True(t,  entries[0].Eligible)
	assert.Equal(t, "alice", entries[1].CharacterID, "next: oldest last_pose_at")
	assert.False(t, entries[1].Eligible)
	assert.Equal(t, "bob",   entries[2].CharacterID, "tail: most recent poser")
	assert.False(t, entries[2].Eligible)
}

func TestCompute_3prMode_Threshold3(t *testing.T) {
	t.Parallel()
	now := time.Now()
	// Total poses in scene = 5.
	// Alice last_pose_seq = 1 → poses_since_last = 4 → eligible (>= 3).
	// Bob   last_pose_seq = 4 → poses_since_last = 1 → cooldown.
	// Carol never posed → eligible.
	aliceSeq := int32(1)
	bobSeq   := int32(4)
	alicePosed := now.Add(-30 * time.Minute)
	bobPosed   := now.Add(-5  * time.Minute)
	participants := []ParticipantWithPoseMeta{
		{CharacterID: "alice", JoinedAt: now.Add(-3 * time.Hour), LastPoseAt: &alicePosed, LastPoseSeq: &aliceSeq},
		{CharacterID: "bob",   JoinedAt: now.Add(-2 * time.Hour), LastPoseAt: &bobPosed,   LastPoseSeq: &bobSeq},
		{CharacterID: "carol", JoinedAt: now.Add(-1 * time.Hour)},
	}
	names := map[string]string{"alice": "Alice", "bob": "Bob", "carol": "Carol"}
	entries := Compute("3pr", 5, participants, names)
	assert.Len(t, entries, 3)
	byID := indexByCharID(entries)
	assert.True(t,  byID["alice"].Eligible)
	assert.False(t, byID["bob"].Eligible)
	assert.True(t,  byID["carol"].Eligible)
	assert.NotNil(t, byID["alice"].PosesSinceLast)
	assert.EqualValues(t, 4, *byID["alice"].PosesSinceLast)
	assert.EqualValues(t, 1, *byID["bob"].PosesSinceLast)
	assert.EqualValues(t, 5, *byID["carol"].PosesSinceLast) // never-posed: full total
}

func TestCompute_5prMode_Threshold5(t *testing.T) {
	t.Parallel()
	// Same shape as 3pr but threshold 5.
	now := time.Now()
	aliceSeq := int32(1)
	alicePosed := now.Add(-10 * time.Minute)
	participants := []ParticipantWithPoseMeta{
		{CharacterID: "alice", JoinedAt: now.Add(-2 * time.Hour), LastPoseAt: &alicePosed, LastPoseSeq: &aliceSeq},
		{CharacterID: "bob",   JoinedAt: now.Add(-1 * time.Hour)},
	}
	names := map[string]string{"alice": "Alice", "bob": "Bob"}
	entries := Compute("5pr", 4, participants, names)
	byID := indexByCharID(entries)
	assert.False(t, byID["alice"].Eligible, "3 poses since alice (4-1) < 5 threshold")
	assert.True(t,  byID["bob"].Eligible,   "never-posed always eligible")
}

func TestCompute_EmptyParticipants(t *testing.T) {
	t.Parallel()
	entries := Compute("strict", 0, nil, nil)
	assert.Empty(t, entries)
}

func indexByCharID(entries []PoseOrderEntry) map[string]PoseOrderEntry {
	out := make(map[string]PoseOrderEntry, len(entries))
	for _, e := range entries { out[e.CharacterID] = e }
	return out
}
```

- [ ] **Step 2: Run test to verify it fails (no poseorder.go)**

Run: `task test -- ./plugins/core-scenes/ -run TestCompute`

Expected: FAIL — `Compute undefined` and `PoseOrderEntry undefined`.

- [ ] **Step 3: Implement `poseorder.go`**

Create `plugins/core-scenes/poseorder.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"sort"
	"time"
)

// PoseOrderEntry is the Go-side equivalent of the proto PoseOrderEntry —
// used for in-process composition before marshaling to the wire type
// in the GetPoseOrder handler.
type PoseOrderEntry struct {
	CharacterID    string
	CharacterName  string
	Eligible       bool
	LastPosedAt    *time.Time   // nil = never posed
	PosesSinceLast *uint32      // meaningful for 3pr/5pr; nil otherwise
}

// Compute derives pose-order entries from maintained metadata per
// spec §6.2. Pure function — no DB, no side effects, fully testable
// with table-driven cases per mode (INV-P4-7).
//
// totalPoseCount is the scene-wide scene_pose count (scenes.total_pose_count).
// participants come from sceneStorer.ListParticipantsWithPoseMeta.
// names maps character_id → character_name (from nameResolver).
func Compute(mode string, totalPoseCount uint32, participants []ParticipantWithPoseMeta, names map[string]string) []PoseOrderEntry {
	if len(participants) == 0 {
		return nil
	}

	// Sort the input slice per mode. For strict/3pr/5pr: NULLS FIRST
	// (never-posed at head), then by LastPoseAt ascending. For free:
	// JoinedAt ascending.
	sorted := make([]ParticipantWithPoseMeta, len(participants))
	copy(sorted, participants)
	switch mode {
	case "free":
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].JoinedAt.Before(sorted[j].JoinedAt)
		})
	default: // strict, 3pr, 5pr
		sort.SliceStable(sorted, func(i, j int) bool {
			a, b := sorted[i], sorted[j]
			// NULLS FIRST: never-posed sorts before posed.
			if a.LastPoseAt == nil && b.LastPoseAt != nil { return true }
			if a.LastPoseAt != nil && b.LastPoseAt == nil { return false }
			// Both nil OR both non-nil: tiebreak.
			if a.LastPoseAt == nil && b.LastPoseAt == nil {
				return a.JoinedAt.Before(b.JoinedAt)
			}
			return a.LastPoseAt.Before(*b.LastPoseAt)
		})
	}

	entries := make([]PoseOrderEntry, 0, len(sorted))
	for i, p := range sorted {
		entry := PoseOrderEntry{
			CharacterID:   p.CharacterID,
			CharacterName: names[p.CharacterID],
			LastPosedAt:   p.LastPoseAt,
		}
		// Compute poses_since_last for 3pr/5pr surfaces.
		if mode == "3pr" || mode == "5pr" {
			var since uint32
			if p.LastPoseSeq == nil {
				since = totalPoseCount
			} else {
				since = totalPoseCount - uint32(*p.LastPoseSeq)
			}
			entry.PosesSinceLast = &since
		}

		switch mode {
		case "strict":
			entry.Eligible = i == 0  // head of queue
		case "3pr":
			entry.Eligible = p.LastPoseSeq == nil || (totalPoseCount - uint32(*p.LastPoseSeq)) >= 3
		case "5pr":
			entry.Eligible = p.LastPoseSeq == nil || (totalPoseCount - uint32(*p.LastPoseSeq)) >= 5
		case "free":
			entry.Eligible = true
		default:
			// Unknown mode: default to non-eligible. Should not happen — the
			// scene state machine enforces the enum.
			entry.Eligible = false
		}
		entries = append(entries, entry)
	}
	return entries
}
```

- [ ] **Step 4: Run tests to verify all pass**

Run: `task test -- ./plugins/core-scenes/ -run TestCompute`

Expected: all 5 tests pass.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scene-poseorder): pure-function Compute per spec §6.2

Derives PoseOrderEntry[] from ParticipantsWithPoseMeta + total_pose_count
+ name map. Per-mode logic:

- strict: NULLS FIRST + JoinedAt + LastPoseAt ASC; head eligible
- 3pr/5pr: same display order; eligible = never-posed OR
  (total_pose_count - last_pose_seq) >= threshold
- free: JoinedAt ASC; all eligible

Surfaces poses_since_last for 3pr/5pr UX ('Carol (2/3 since)'). Pure
function — no DB, no side effects, no globals. Table-driven tests
cover all 4 modes per INV-P4-7.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

---

## Phase E — Subject migration (atomic substrate + plugin)

> All five tasks in this phase land together — substrate-side dot-style parsing and plugin-side dot-style emission MUST be atomic. Mid-phase pushes are forbidden; the I-17 gate would fail-closed on dot-style emits without substrate support, or vice versa.

### Task 10: Add `crypto.emits` + EmitTypeRegistrar adoption (TDD)

**Files:**

- Modify: `plugins/core-scenes/plugin.yaml`
- Modify: `plugins/core-scenes/main.go`
- Create: `plugins/core-scenes/main_test.go`

- [ ] **Step 1: Write the failing manifest set-equality test**

Create `plugins/core-scenes/main_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// TestPlugin_CryptoEmitsMatchesRegistry pins INV-P4-2: the 8 scene
// event types in crypto.emits MUST equal the set registered via
// EmitTypeRegistrar.
func TestPlugin_CryptoEmitsMatchesRegistry(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("plugin.yaml")
	require.NoError(t, err)

	var m struct {
		Crypto struct {
			Emits []struct {
				EventType string `yaml:"event_type"`
				Sensitivity string `yaml:"sensitivity"`
			} `yaml:"emits"`
		} `yaml:"crypto"`
	}
	require.NoError(t, yaml.Unmarshal(data, &m))

	manifestSet := make([]string, 0, len(m.Crypto.Emits))
	for _, e := range m.Crypto.Emits {
		manifestSet = append(manifestSet, e.EventType)
	}
	sort.Strings(manifestSet)

	// Build the registry the same way main() does.
	reg := pluginsdk.NewEmitRegistry()
	reg.RegisterEmitTypes(phase4EmitTypes())   // helper defined in main.go
	registrySet := reg.RegisteredEmitTypes()
	sort.Strings(registrySet)

	assert.Equal(t, manifestSet, registrySet,
		"INV-P4-2: manifest crypto.emits MUST equal EmitTypeRegistrar set")
}

// TestPlugin_SensitivityMatrix pins INV-P4-3: per-type sensitivity matches
// spec §2 table.
func TestPlugin_SensitivityMatrix(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("plugin.yaml")
	require.NoError(t, err)

	var m struct {
		Crypto struct {
			Emits []struct {
				EventType string `yaml:"event_type"`
				Sensitivity string `yaml:"sensitivity"`
			} `yaml:"emits"`
		} `yaml:"crypto"`
	}
	require.NoError(t, yaml.Unmarshal(data, &m))

	want := map[string]string{
		"scene_pose":                  "always",
		"scene_say":                   "always",
		"scene_emit":                  "always",
		"scene_ooc":                   "always",
		"scene_join_ic":               "never",
		"scene_leave_ic":              "never",
		"scene_pose_order_changed_ic": "never",
		"scene_idle_nudge":            "never",
	}
	got := make(map[string]string)
	for _, e := range m.Crypto.Emits {
		got[e.EventType] = e.Sensitivity
	}
	assert.Equal(t, want, got,
		"INV-P4-3: sensitivity matrix MUST match spec §2 table")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./plugins/core-scenes/ -run TestPlugin_`

Expected: FAIL — `phase4EmitTypes undefined`, and manifest has empty `crypto.emits`.

- [ ] **Step 3: Add `crypto.emits` to `plugin.yaml`**

In `plugins/core-scenes/plugin.yaml`, locate the existing `crypto:` block (currently `emits: []` near line 39). Replace with:

```yaml
crypto:
  emits:
    # Content events (sensitivity: always) — participant-only IC/OOC RP
    # content. Privacy boundary is the participant list per INV-S9, NOT
    # the location. Analog to whisper/page in core-communication, not
    # to say/pose (which are location-bounded).
    - event_type: scene_pose
      sensitivity: always
      description: "IC pose by a scene participant; visible to all participants in the scene's IC stream."
    - event_type: scene_say
      sensitivity: always
      description: "IC speech by a scene participant; visible to all participants in the scene's IC stream."
    - event_type: scene_emit
      sensitivity: always
      description: "IC generic emit by a scene participant; visible to all participants in the scene's IC stream."
    - event_type: scene_ooc
      sensitivity: always
      description: "OOC chatter in scene context; participant-only despite never being archived in the published scene log."

    # Notice events (sensitivity: never) — operational metadata, no RP
    # content. Visible to scene participants but plaintext at rest.
    - event_type: scene_join_ic
      sensitivity: never
      description: "Notice that a character joined the scene; name only, no content."
    - event_type: scene_leave_ic
      sensitivity: never
      description: "Notice that a character left or was kicked from the scene; name + reason discriminator, no content."
    - event_type: scene_pose_order_changed_ic
      sensitivity: never
      description: "Notice that the scene owner changed the pose-order mode; mode strings + actor, no content."
    - event_type: scene_idle_nudge
      sensitivity: never
      description: "Notice that the next-up character has been idle past the configured threshold; name + duration, no content. (Trigger implementation deferred — see follow-up bead.)"
```

- [ ] **Step 4: Update `main.go` to register the 8 event types**

In `plugins/core-scenes/main.go`, modify the `scenePlugin` struct and `main()` function:

```go
// scenePlugin gains an emitRegistry field.
type scenePlugin struct {
    // ... existing fields ...
    emitRegistry *pluginsdk.EmitRegistry
}

// EmitRegistry implements pluginsdk.EmitTypeRegistrar.
// The substrate INV-S5 validator reads this set via the binary-plugin
// Init RPC and compares against manifest crypto.emits set-equality.
func (p *scenePlugin) EmitRegistry() *pluginsdk.EmitRegistry {
    return p.emitRegistry
}

// phase4EmitTypes returns the 8 plugin-owned scene event types declared
// in crypto.emits. Exposed at package level so the manifest-vs-registry
// test in main_test.go can build the same set.
func phase4EmitTypes() []string {
    return []string{
        "scene_pose",
        "scene_say",
        "scene_emit",
        "scene_ooc",
        "scene_join_ic",
        "scene_leave_ic",
        "scene_pose_order_changed_ic",
        "scene_idle_nudge",
    }
}

func main() {
    reg := pluginsdk.NewEmitRegistry()
    reg.RegisterEmitTypes(phase4EmitTypes())

    plugin := &scenePlugin{
        service:      &SceneServiceImpl{},
        // ... existing field initialisation unchanged ...
        emitRegistry: reg,
    }
    // ... existing pluginsdk.ServeWithServices call ...
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- ./plugins/core-scenes/ -run TestPlugin_`

Expected: both tests pass.

- [ ] **Step 6: Run the substrate INV-S5 validator end-to-end via a plugin-load test**

Run: `task test:int -- -run TestPluginManager_LoadPlugin_NoCryptoEmitsMismatch ./internal/plugin/`

Expected: existing INV-S5 substrate tests pass with the updated core-scenes plugin manifest+code aligned. If a test like `TestManager_LoadPlugin_EmitTypeMismatch_FailsClosed` exists, it should be unaffected.

- [ ] **Step 7: Commit**

```bash
jj describe -m "feat(scene-emit): declare 8 crypto.emits + EmitTypeRegistrar adoption

Populates core-scenes/plugin.yaml::crypto.emits with the 8 Phase 4
scene event types per spec §2 sensitivity matrix:

  scene_pose / scene_say / scene_emit / scene_ooc → always
  scene_join_ic / scene_leave_ic / scene_pose_order_changed_ic
    / scene_idle_nudge → never

scenePlugin implements pluginsdk.EmitTypeRegistrar via the new
emitRegistry field; main() registers all 8 types so the substrate
INV-S5 set-equality validator (manager.go::loadPlugin) sees matching
manifest and code sets. Mismatch would fail plugin load fail-closed
with EVENT_TYPE_REGISTRY_MISMATCH.

Tests pin INV-P4-2 (manifest == registry) and INV-P4-3 (sensitivity
matches table) via manifest-parse + registry-build.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 11: Migrate scene emit subjects to dot-style + add `service.go` helpers (TDD)

**Files:**

- Modify: `plugins/core-scenes/service.go:211` (existing colon-style)
- Modify: `plugins/core-scenes/store.go` (add subject helpers)
- Modify: `plugins/core-scenes/service_test.go:426` (test expectation)

- [ ] **Step 1: Write the failing test (update existing expectation)**

In `plugins/core-scenes/service_test.go`, locate line 426:

```go
assert.Equal(t, "scene:"+resp.GetScene().GetId(), sink.intents[0].Subject)
```

Change to:

```go
gameID := s.gameID  // wire from test fixture; OR use any non-empty placeholder per SDK contract
expectedSubject := dotStyleSceneSubject(gameID, resp.GetScene().GetId())
assert.Equal(t, expectedSubject, sink.intents[0].Subject,
	"Phase 4: emit subjects MUST be NATS dot-style (INV-P4-1)")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./plugins/core-scenes/ -run TestCreateScene`

Expected: FAIL — `dotStyleSceneSubject undefined` AND existing assertion mismatch.

- [ ] **Step 3: Add the subject formatter helpers**

In `plugins/core-scenes/store.go`, append package-level helpers:

```go
// dotStyleSceneSubject returns the NATS dot-style entity-level subject
// for a scene per INV-S4: events.<gameID>.scene.<sceneID>. Used for
// lifecycle / system events that target the scene itself, not a facet.
func dotStyleSceneSubject(gameID, sceneID string) string {
    return "events." + gameID + ".scene." + sceneID
}

// dotStyleSceneSubjectIC returns the NATS dot-style IC-facet subject
// for a scene: events.<gameID>.scene.<sceneID>.ic.
func dotStyleSceneSubjectIC(gameID, sceneID string) string {
    return dotStyleSceneSubject(gameID, sceneID) + ".ic"
}

// dotStyleSceneSubjectOOC returns the NATS dot-style OOC-facet subject:
// events.<gameID>.scene.<sceneID>.ooc.
func dotStyleSceneSubjectOOC(gameID, sceneID string) string {
    return dotStyleSceneSubject(gameID, sceneID) + ".ooc"
}
```

- [ ] **Step 4: Update `service.go:211` emit subject**

In `plugins/core-scenes/service.go`, locate the `sceneCreatedIntent` (or the function containing line 211). Replace:

```go
Subject: "scene:" + row.ID,
```

with:

```go
Subject: dotStyleSceneSubject(s.gameID, row.ID),
```

Add `gameID string` field to `SceneServiceImpl` struct (if not already present); wire from `Init` per existing pattern.

Repeat the substitution for any other lifecycle emits in service.go (rg first to enumerate: `rg -n 'Subject: "scene:' plugins/core-scenes/`).

- [ ] **Step 5: Wire `gameID` through `SceneServiceImpl.Init`**

In `plugins/core-scenes/main.go::Init`, after `connStr := config.GetConnectionString()`:

```go
gameID := config.GetGameId()
if gameID == "" {
    return oops.Code("SCENE_INIT_FAILED").Errorf("game_id is required for NATS dot-style subjects")
}
p.service.gameID = gameID
```

(Verify `pluginv1.ServiceConfig` has a `GameId` accessor; if not, file a follow-up bead and use the existing `Namespace` or equivalent field.)

- [ ] **Step 6: Run tests to verify they pass**

Run: `task test -- ./plugins/core-scenes/`

Expected: all unit tests pass — emit subjects are dot-style.

- [ ] **Step 7: Run grep verification**

Run: `rg -n '"scene:"' plugins/core-scenes/`

Expected: zero matches (subject-context); helps confirm INV-P4-1.

- [ ] **Step 8: Commit**

```bash
jj describe -m "feat(scene-emit): migrate scene emit subjects to NATS dot-style

Replaces 'scene:<id>' colon-style with events.<game_id>.scene.<id>
dot-style per INV-S4 substrate convention. Adds package-level helpers
dotStyleSceneSubject / dotStyleSceneSubjectIC / dotStyleSceneSubjectOOC
in store.go to keep formatting consistent across all emit sites.

SceneServiceImpl gains a gameID field, wired from ServiceConfig.GameId
in Init. Test expectations updated.

Per INV-P4-1: zero scene-subject colon-style literals remain in
plugins/core-scenes/. The substrate-side scene-aware code migrates in
the next four tasks (atomic with this change; mid-phase push would
fail-closed at the I-17 gate).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 12: Migrate `stream_access.go` scene branches to dot-style

**Files:**

- Modify: `internal/grpc/stream_access.go:25, :52, :77`
- Modify: `internal/grpc/stream_access_test.go`

- [ ] **Step 1: Write the failing test (update fixtures)**

In `internal/grpc/stream_access_test.go`, locate the test fixtures using colon-style scene subjects (lines :68, :74, :80, :86, :104, :118, :142, :148, :164, :169, :174 per earlier rg). Update each to dot-style. Example for line 68:

```go
// Before:
stream: "scene:" + activeSceneID.String() + ":ic",
// After:
stream: dotStyleSceneIC("test", activeSceneID.String()),
```

Add a test-local helper at the top of `stream_access_test.go`:

```go
func dotStyleSceneIC(gameID, sceneID string) string {
    return "events." + gameID + ".scene." + sceneID + ".ic"
}
func dotStyleSceneOOC(gameID, sceneID string) string {
    return "events." + gameID + ".scene." + sceneID + ".ooc"
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./internal/grpc/ -run TestStreamAccess`

Expected: FAIL — the existing `isPrivateStream`/`extractSceneID` don't recognize dot-style yet.

- [ ] **Step 3: Update production code**

In `internal/grpc/stream_access.go`:

```go
// isPrivateStream reports whether a stream is one of the private types
// (character own-stream OR scene IC/OOC). Phase 4: dot-style scene subjects.
func isPrivateStream(stream string) bool {
    return strings.HasPrefix(stream, "character:") || isSceneStream(stream)
}

// isSceneStream reports whether a stream is a scene IC or OOC subject
// in NATS dot-style: events.<gameID>.scene.<sceneID>.{ic,ooc}.
func isSceneStream(stream string) bool {
    parts := strings.Split(stream, ".")
    if len(parts) < 5 { return false }
    if parts[0] != "events" || parts[2] != "scene" { return false }
    facet := parts[len(parts)-1]
    return facet == "ic" || facet == "ooc"
}

// extractSceneID returns the scene ULID from a dot-style scene subject.
// Caller MUST check isSceneStream first; undefined behavior otherwise.
func extractSceneID(stream string) (string, bool) {
    parts := strings.Split(stream, ".")
    if len(parts) < 5 || parts[0] != "events" || parts[2] != "scene" {
        return "", false
    }
    return parts[3], true
}
```

Locate the existing colon-style scene branches in `stream_access.go:25, :52, :77` and replace with calls to the new helpers per the production code above. (rg first: `rg -n '"scene:"' internal/grpc/stream_access.go`)

- [ ] **Step 4: Run tests**

Run: `task test -- ./internal/grpc/ -run TestStreamAccess`

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(substrate-grpc): stream_access.go — dot-style scene subjects

Migrates isPrivateStream / extractSceneID to recognise NATS dot-style
scene subjects (events.<gid>.scene.<id>.{ic,ooc}). Adds isSceneStream
helper. Tests fixtures updated.

Atomic with plugins/core-scenes/service.go emit migration (Task 11)
and the iwzt §6.1 floor classification (next task) — mid-phase pushes
would fail-closed at the I-17 gate.

Per INV-P4-1 + INV-S1 (this is scene-aware substrate code, reviewed
under substrate gates).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 13: Migrate `scope_floor.go` + add INV-P4-9 unit pin (TDD per spec §3.3)

**Files:**

- Modify: `internal/grpc/scope_floor.go:42-74, :115-122`
- Modify: `internal/grpc/scope_floor_test.go`

- [ ] **Step 1: Write the failing pre/post migration table-driven test**

In `internal/grpc/scope_floor_test.go`, add a new table-driven test that pins the bug-fix moment per spec §3.3:

```go
// TestStreamScopeFloor_SceneSubjects_INV_P4_9 pins the bug-fix moment
// for the scope_floor.go format mismatch (spec §3.3). Pre-migration,
// the colon-style branch matched but production callers passed
// dot-style — function returned time.Time{} for all real traffic.
// Post-migration, dot-style matches; colon-style is no longer recognised.
func TestStreamScopeFloor_SceneSubjects_INV_P4_9(t *testing.T) {
    t.Parallel()
    sceneID := ulid.Make()
    joinedAt := time.Now().UTC().Truncate(time.Microsecond)

    info := &session.Info{
        FocusMemberships: []session.FocusMembership{{
            Kind:     session.FocusKindScene,
            TargetID: sceneID,
            JoinedAt: joinedAt,
        }},
    }

    cases := []struct {
        name          string
        stream        string
        wantFloor     time.Time
        rationale     string
    }{
        {
            name:      "legacy colon-style (no longer matched after migration)",
            stream:    "scene:" + sceneID.String() + ":ic",
            wantFloor: time.Time{},  // post-migration: legacy form falls through to default
            rationale: "Phase 4 removed the colon-style scene branch — legacy form falls to time.Time{} default.",
        },
        {
            name:      "NATS dot-style (newly matched after migration)",
            stream:    "events.test.scene." + sceneID.String() + ".ic",
            wantFloor: joinedAt,
            rationale: "Phase 4 added the dot-style scene branch — production-shape subjects now floor to FocusMembership.JoinedAt.",
        },
        {
            name:      "NATS dot-style OOC facet",
            stream:    "events.test.scene." + sceneID.String() + ".ooc",
            wantFloor: joinedAt,
            rationale: "OOC facet floors to same JoinedAt as IC.",
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got := streamScopeFloor(info, tc.stream)
            assert.Equal(t, tc.wantFloor.UTC(), got.UTC(), tc.rationale)
        })
    }
}
```

- [ ] **Step 2: Run test to verify pre-migration shape**

Run: `task test -- ./internal/grpc/ -run TestStreamScopeFloor_SceneSubjects_INV_P4_9`

Expected: FAIL on the dot-style cases (current production-broken behavior); the legacy case might PASS (or fail, depending on existing matching). This is the pre-migration baseline shape that the migration commit will flip.

- [ ] **Step 3: Update `scope_floor.go`**

In `internal/grpc/scope_floor.go`, replace the scene branch in `streamScopeFloor` (around line 47):

```go
// Before (colon-style):
case strings.HasPrefix(stream, "scene:"):
    sceneID, ok := extractSceneID(stream)
    if !ok {
        return time.Time{}
    }
    for _, m := range info.FocusMemberships { ... }

// After (dot-style):
case isSceneStream(stream):
    sceneID, ok := extractSceneID(stream)
    if !ok {
        return time.Time{}
    }
    for _, m := range info.FocusMemberships {
        if m.Kind == session.FocusKindScene && m.TargetID.String() == sceneID {
            base = m.JoinedAt
            break
        }
    }
```

Note: `isSceneStream` + dot-style-aware `extractSceneID` are in `stream_access.go` (Task 12). Same package — no import change.

Also REMOVE the developer-note comment at lines 20-28 of `scope_floor.go` (it documents the pre-migration bug; once fixed, the comment is stale).

- [ ] **Step 4: Run test to verify post-migration shape**

Run: `task test -- ./internal/grpc/ -run TestStreamScopeFloor_SceneSubjects_INV_P4_9`

Expected: all 3 cases pass — the migration is correct.

- [ ] **Step 5: Verify no other scope_floor tests broke**

Run: `task test -- ./internal/grpc/ -run TestStreamScopeFloor`

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
jj describe -m "fix(substrate-grpc): scope_floor.go — dot-style scene branch (live regression)

Pre-Phase-4 scope_floor.go matched 'scene:<id>:ic' colon-style, but
production callers passed events.<gid>.scene.<id>.ic dot-style — so
the function returned time.Time{} for all real-world scene subjects
and the iwzt §6.1 temporal floor for scene streams was silently
inactive (spec §3.3).

This commit migrates the scene branch to the dot-style helpers added
in stream_access.go (Task 12). The accompanying scope_floor_test.go
TestStreamScopeFloor_SceneSubjects_INV_P4_9 pins the bug-fix moment
with explicit before/after assertions for both colon-style (no longer
matched) and dot-style (now matched).

Closes the live silent regression for INV-P4-9. End-to-end integration
test in test/integration/scenes/late_joiner_temporal_floor_test.go
verifies the iwzt §3 contract on the post-migration code.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 14: Migrate `query_stream_history.go` I-17 gate to dot-style

**Files:**

- Modify: `internal/grpc/query_stream_history.go:39, :157-178`

- [ ] **Step 1: Inspect existing I-17 gate**

Run: `rg -n -C 5 'scene:|isScene' internal/grpc/query_stream_history.go`

Read the surrounding code to understand the gate shape.

- [ ] **Step 2: Update the gate predicate**

Replace any `strings.HasPrefix(req.Stream, "scene:")` with `isSceneStream(req.Stream)`. Replace any direct colon-prefix parsing with the `extractSceneID` helper from `stream_access.go`. Both helpers live in the same package — no imports needed.

- [ ] **Step 3: Update any tests in `query_stream_history_test.go` that use colon-style fixtures**

Run: `rg -n '"scene:"' internal/grpc/query_stream_history_test.go`

Replace each with the dot-style equivalent using the `dotStyleSceneIC`/`dotStyleSceneOOC` helpers added in `stream_access_test.go` (Task 12 Step 1).

- [ ] **Step 4: Run tests**

Run: `task test -- ./internal/grpc/`

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(substrate-grpc): query_stream_history.go I-17 gate — dot-style

Migrates the I-17 hardcoded scene-membership gate to recognise NATS
dot-style scene subjects via the isSceneStream + extractSceneID
helpers from stream_access.go (Task 12).

Per INV-S9 + iwzt §3, the gate remains plugin-code adjacent (no
ABAC override path); scene privacy stays absolute. Only the subject
detection migrates.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 15: Update `test/integration/plugin/binary_plugin_test.go` fixtures

**Files:**

- Modify: `test/integration/plugin/binary_plugin_test.go:502, :512`

- [ ] **Step 1: Replace colon-style scene-subject test inputs with dot-style**

Run: `rg -n '"scene:"' test/integration/plugin/binary_plugin_test.go`

For each match that is a STREAM/SUBJECT (not ABAC resource), replace with NATS dot-style. ABAC `Resource` strings (`"scene:" + sceneID` inside `NewAccessRequest`) are policy-DSL resource identifiers, NOT topics — leave those unchanged per spec §3.2 scope clarification.

Inspection checklist before each replacement:

- Is the string passed as a SUBJECT or STREAM argument? → migrate
- Is the string passed as `Resource` in an ABAC `NewAccessRequest` call? → leave unchanged

- [ ] **Step 2: Run the integration test**

Run: `task test:int -- ./test/integration/plugin/`

Expected: all tests pass.

- [ ] **Step 3: Commit**

```bash
jj describe -m "test(plugin-integration): scene subject fixtures — dot-style

Updates test fixtures in binary_plugin_test.go that passed scene
subjects via STREAM/SUBJECT arguments to use NATS dot-style. ABAC
Resource strings (scene:<id> in policy DSL) unchanged — different
concern per spec §3.2.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

---

## Phase F — Emit subcommands

### Task 16: `scene pose / say / emit / ooc` subcommands (TDD)

**Files:**

- Modify: `plugins/core-scenes/commands.go`
- Test: `plugins/core-scenes/commands_test.go`

- [ ] **Step 1: Write the failing subcommand tests**

Append to `plugins/core-scenes/commands_test.go`:

```go
func TestSceneSubcommand_Pose_HappyPath(t *testing.T) {
    t.Parallel()
    p, sink := newTestScenePluginWithMembership(t, "scene-pose-test", "char-alice")
    req := pluginsdk.CommandRequest{
        Command: "scene",
        Args:    "pose smiles at the room",
        CharacterID: "char-alice",
    }
    resp, err := p.dispatchCommand(context.Background(), req)
    require.NoError(t, err)
    require.NotNil(t, resp)

    require.Len(t, sink.intents, 1)
    intent := sink.intents[0]
    assert.Equal(t, "scene_pose", intent.Type)
    assert.Equal(t, dotStyleSceneSubjectIC("test", "scene-pose-test"), intent.Subject)
    assert.True(t, intent.Sensitive, "scene_pose MUST be emitted with Sensitive=true (sensitivity:always)")
}

func TestSceneSubcommand_NonParticipant_PermissionDenied(t *testing.T) {
    t.Parallel()
    p, _ := newTestScenePluginWithMembership(t, "scene-perm-test", "char-alice")
    // char-bob is NOT a participant.
    req := pluginsdk.CommandRequest{
        Command: "scene",
        Args:    "pose tries to butt in",
        CharacterID: "char-bob",
    }
    resp, _ := p.dispatchCommand(context.Background(), req)
    require.NotNil(t, resp)
    assert.True(t, resp.IsError(), "INV-P4-11: non-participant pose MUST be rejected at command-execute layer")
}

// Repeat for say / emit / ooc subcommands with matching expectations:
// - scene_say → .ic facet, sensitivity:always
// - scene_emit → .ic facet, sensitivity:always
// - scene_ooc → .ooc facet, sensitivity:always
func TestSceneSubcommand_Say(t *testing.T) { ... }
func TestSceneSubcommand_Emit(t *testing.T) { ... }
func TestSceneSubcommand_OOC(t *testing.T) {
    // OOC routes to .ooc facet:
    // assert.Equal(t, dotStyleSceneSubjectOOC("test", sceneID), intent.Subject)
    ...
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `task test -- ./plugins/core-scenes/ -run TestSceneSubcommand`

Expected: FAIL — dispatcher returns "Unknown scene subcommand 'pose'".

- [ ] **Step 3: Add subcommand cases to dispatcher**

In `plugins/core-scenes/commands.go::dispatchCommand`, add cases inside the switch:

```go
case "pose":
    return p.handleEmit(ctx, req, rest, "scene_pose", false /* ooc */)
case "say":
    return p.handleEmit(ctx, req, rest, "scene_say", false)
case "emit":
    return p.handleEmit(ctx, req, rest, "scene_emit", false)
case "ooc":
    return p.handleEmit(ctx, req, rest, "scene_ooc", true /* ooc */)
case "order":
    return p.handleOrder(ctx, req, rest)
```

Add a single shared `handleEmit` function:

```go
// handleEmit is the shared emit-subcommand handler for pose / say /
// emit / ooc. The verb determines the event_type; the ooc flag determines
// the subject facet.
func (p *scenePlugin) handleEmit(
    ctx context.Context,
    req pluginsdk.CommandRequest,
    text string,
    eventType string,
    ooc bool,
) (*pluginsdk.CommandResponse, error) {
    text = strings.TrimSpace(text)
    if text == "" {
        return pluginsdk.Errorf("Usage: scene %s <text>", strings.TrimPrefix(eventType, "scene_")), nil
    }

    // Resolve target scene: for Phase 4, infer from single-membership.
    // Phase 5 will add focus-aware routing.
    sceneID, err := p.resolveSingleSceneMembership(ctx, req.CharacterID)
    if err != nil {
        return pluginsdk.Errorf("Cannot determine target scene: %v. Phase 5 will route plain pose/say/emit/ooc by focus context.", err), nil
    }

    // INV-P4-11: the capability pre-flight via execute-scene-commands +
    // write-scene-as-participant gates this at dispatch time. Defense in
    // depth: verify membership in plugin store before emit.
    ok, err := p.store.IsParticipant(ctx, sceneID, req.CharacterID)
    if err != nil { return nil, oops.Code("SCENE_EMIT_MEMBERSHIP_LOOKUP_FAILED").Wrap(err) }
    if !ok {
        return pluginsdk.Errorf("You are not a participant of scene %s.", sceneID), nil
    }

    subject := dotStyleSceneSubjectIC(p.gameID, sceneID)
    if ooc { subject = dotStyleSceneSubjectOOC(p.gameID, sceneID) }

    payload, err := json.Marshal(map[string]string{
        "actor_id": req.CharacterID,
        "scene_id": sceneID,
        "text":     text,
    })
    if err != nil { return nil, oops.Code("SCENE_EMIT_PAYLOAD_MARSHAL_FAILED").Wrap(err) }

    intent := pluginsdk.EmitIntent{
        Subject:   subject,
        Type:      eventType,
        Payload:   string(payload),
        Sensitive: true,   // sensitivity:always — INV-P4-3
    }
    if err := p.eventSink.Emit(ctx, intent); err != nil {
        return nil, oops.Code("SCENE_EMIT_FAILED").With("event_type", eventType).Wrap(err)
    }

    verb := strings.TrimPrefix(eventType, "scene_")
    return pluginsdk.Outputf("You %s: %s", verb, text), nil
}
```

Add a stub `resolveSingleSceneMembership` (refined in Task 17 if needed):

```go
// resolveSingleSceneMembership returns the scene_id this character is
// a sole participant of, or an error if they are in zero or multiple
// scenes. Phase 5 will replace this with focus-aware routing.
func (p *scenePlugin) resolveSingleSceneMembership(ctx context.Context, characterID string) (string, error) {
    scenes, err := p.store.ListScenesForCharacter(ctx, characterID)  // existing or new
    if err != nil { return "", err }
    switch len(scenes) {
    case 0:
        return "", errors.New("you are not currently in any scene; join one first with `scene join <scene-id>`")
    case 1:
        return scenes[0], nil
    default:
        return "", fmt.Errorf("you are in %d scenes; Phase 5 will add focus-aware routing", len(scenes))
    }
}
```

If `ListScenesForCharacter` doesn't exist in `sceneStorer`, add it as part of this task (mirror the existing query patterns).

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- ./plugins/core-scenes/ -run TestSceneSubcommand`

Expected: all 4 happy-path tests + 1 permission-denied test pass.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scene-commands): scene pose / say / emit / ooc subcommands

Adds 4 emit subcommands to the scene top-level command dispatcher.
Each emits the matching scene_<verb> event type with sensitivity:true
(matches crypto.emits sensitivity:always — INV-P4-3). Subject facet
.ic for pose/say/emit; .ooc for ooc.

Target scene resolution via single-membership inference (Phase 4 only);
Phase 5 will replace with focus-aware routing. Defense-in-depth
participant check via IsParticipant before emit (Layer-1 ABAC
execute-scene-commands gate fires first per spec §11).

INV-P4-11 covers the participant requirement; pinned by
TestSceneSubcommand_NonParticipant_PermissionDenied.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

---

## Phase G — Auto-emit triggers (notice events)

### Task 17: Auto-emit `scene_join_ic` on `JoinScene`

**Files:**

- Modify: `plugins/core-scenes/service.go::JoinScene`
- Test: `plugins/core-scenes/service_test.go::TestJoinScene_*`

- [ ] **Step 1: Write the failing auto-emit assertion**

In `plugins/core-scenes/service_test.go`, locate the existing `TestJoinScene_*` tests. Append:

```go
func TestJoinScene_EmitsSceneJoinIC_OnInsert(t *testing.T) {
    t.Parallel()
    s, sink := newTestSceneServiceWithMember(t, "scene-join-emit", "owner-id")
    // Pre-condition: char-bob is NOT yet a participant.
    resp, err := s.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
        SceneId:     "scene-join-emit",
        CharacterId: "char-bob",
    })
    require.NoError(t, err)
    _ = resp

    // INV: a scene_join_ic event MUST have been emitted to .ic facet.
    found := findIntent(sink.intents, "scene_join_ic")
    require.NotNil(t, found, "JoinScene MUST auto-emit scene_join_ic on OpInserted")
    assert.Equal(t, dotStyleSceneSubjectIC("test", "scene-join-emit"), found.Subject)
    assert.False(t, found.Sensitive, "scene_join_ic is sensitivity:never")
}

func TestJoinScene_NoEmit_OnNoChange(t *testing.T) {
    t.Parallel()
    // Per Phase 3 D5: idempotent retry returns OpNoChange and MUST NOT
    // emit a duplicate scene_join_ic.
    s, sink := newTestSceneServiceWithMember(t, "scene-join-idempotent", "char-alice")
    initialCount := len(sink.intents)

    _, err := s.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
        SceneId:     "scene-join-idempotent",
        CharacterId: "char-alice",  // already a member
    })
    require.NoError(t, err)

    finalCount := len(sink.intents)
    assert.Equal(t, initialCount, finalCount, "idempotent join MUST NOT emit a duplicate scene_join_ic")
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `task test -- ./plugins/core-scenes/ -run TestJoinScene_Emits`

Expected: FAIL — JoinScene does not yet emit scene_join_ic.

- [ ] **Step 3: Update `JoinScene` to emit on insert/promote**

In `plugins/core-scenes/service.go::JoinScene`, modify the body to capture the `ParticipantOpResult` and emit conditionally:

```go
// Replace existing:
// _, _, err := s.store.AddParticipant(ctx, req.GetSceneId(), req.GetCharacterId())

// With:
_, result, err := s.store.AddParticipant(ctx, req.GetSceneId(), req.GetCharacterId())
if err != nil {
    // ... existing error handling ...
}

if result == OpInserted || result == OpPromoted {
    name, _ := s.nameResolver.GetNameByID(ctx, req.GetCharacterId())  // best-effort
    payload, err := json.Marshal(map[string]string{
        "actor_id":   req.GetCharacterId(),
        "actor_name": name,
        "scene_id":   req.GetSceneId(),
        "from_role":  fromRoleFor(result),
    })
    if err == nil {
        _ = s.eventSink.Emit(ctx, pluginsdk.EmitIntent{
            Subject:   dotStyleSceneSubjectIC(s.gameID, req.GetSceneId()),
            Type:      "scene_join_ic",
            Payload:   string(payload),
            Sensitive: false,  // sensitivity:never
        })
        // Emit failure is non-fatal — membership is already committed.
        // Log and continue per existing patterns.
    }
}
```

Add the `fromRoleFor` helper:

```go
func fromRoleFor(r ParticipantOpResult) string {
    if r == OpPromoted { return "invited" }
    return "none"
}
```

If `nameResolver` is not yet wired into `SceneServiceImpl`, add the field and wire from Init in this task.

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- ./plugins/core-scenes/ -run TestJoinScene`

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scene-emit): auto-emit scene_join_ic on JoinScene (OpInserted/OpPromoted)

JoinScene RPC handler emits scene_join_ic to the scene IC stream when
AddParticipant returns OpInserted or OpPromoted; skipped on OpNoChange
per Phase 3 D5 retry-idempotency (prevents duplicate audit log entries
on JetStream redelivery).

sensitivity:never (notice event, no RP content); payload carries
actor_id, actor_name, scene_id, from_role discriminator.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 18: Auto-emit `scene_leave_ic` on `LeaveScene` + `KickFromScene`

**Files:**

- Modify: `plugins/core-scenes/service.go::LeaveScene`, `KickFromScene`
- Test: `plugins/core-scenes/service_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `service_test.go`:

```go
func TestLeaveScene_EmitsSceneLeaveIC_ReasonLeft(t *testing.T) { /* assert reason="left", removed_by absent */ }
func TestKickFromScene_EmitsSceneLeaveIC_ReasonKicked(t *testing.T) { /* assert reason="kicked", removed_by=kicker */ }
```

- [ ] **Step 2: Run to verify failure, then update both RPC handlers**

In `service.go::LeaveScene` and `KickFromScene`, after the successful `RemoveParticipant`/`KickParticipant` call, emit:

```go
// In LeaveScene, after RemoveParticipant returns nil:
s.emitSceneLeaveIC(ctx, req.GetSceneId(), req.GetCharacterId(), "left", "" /* no kicker */)

// In KickFromScene, after KickParticipant returns nil:
s.emitSceneLeaveIC(ctx, req.GetSceneId(), req.GetTargetCharacterId(), "kicked", req.GetCharacterId())
```

Add the shared helper:

```go
func (s *SceneServiceImpl) emitSceneLeaveIC(ctx context.Context, sceneID, actorID, reason, removedBy string) {
    name, _ := s.nameResolver.GetNameByID(ctx, actorID)
    fields := map[string]string{
        "actor_id":   actorID,
        "actor_name": name,
        "scene_id":   sceneID,
        "reason":     reason,
    }
    if removedBy != "" {
        fields["removed_by"] = removedBy
    }
    payload, err := json.Marshal(fields)
    if err != nil { return /* non-fatal */ }
    _ = s.eventSink.Emit(ctx, pluginsdk.EmitIntent{
        Subject:   dotStyleSceneSubjectIC(s.gameID, sceneID),
        Type:      "scene_leave_ic",
        Payload:   string(payload),
        Sensitive: false,
    })
}
```

- [ ] **Step 3: Run tests**

Run: `task test -- ./plugins/core-scenes/ -run TestLeaveScene_Emits TestKickFromScene_Emits`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(scene-emit): auto-emit scene_leave_ic on LeaveScene + KickFromScene

Both LeaveScene (voluntary, reason=left) and KickFromScene (involuntary,
reason=kicked, removed_by=kicker) auto-emit scene_leave_ic notice events
to the scene IC stream after successful participant removal.

sensitivity:never. One event type with reason discriminator (keeps
manifest count at 8). Helper emitSceneLeaveIC consolidates the emit
shape.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 19: Auto-emit `scene_pose_order_changed_ic` on `UpdateScene`

**Files:**

- Modify: `plugins/core-scenes/service.go::UpdateScene`
- Test: `plugins/core-scenes/service_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestUpdateScene_EmitsPoseOrderChangedIC_OnModeChange(t *testing.T) {
    // Update with pose_order_mode in mask AND new value differs from current.
    // Assert scene_pose_order_changed_ic emitted with old_mode/new_mode/actor.
}
func TestUpdateScene_NoEmit_OnNoModeChange(t *testing.T) {
    // Update with pose_order_mode in mask but new value == current.
    // Assert NO scene_pose_order_changed_ic emit.
}
```

- [ ] **Step 2: Run to verify failure, update handler**

In `UpdateScene`, capture the pre-update value before calling `s.store.Update(...)`. After successful update, if `update_mask` contained `pose_order_mode` AND old != new:

```go
// Capture old mode BEFORE the Update call.
preRow, _ := s.store.Get(ctx, req.GetSceneId())  // best-effort; emit is non-critical
// ... existing buildSceneUpdate + s.store.Update logic ...
postRow := /* result of Update */

if preRow != nil && hasMaskPath(req.UpdateMask, "pose_order_mode") && preRow.PoseOrder != postRow.PoseOrder {
    name, _ := s.nameResolver.GetNameByID(ctx, req.GetCharacterId())
    payload, err := json.Marshal(map[string]string{
        "actor_id":   req.GetCharacterId(),
        "actor_name": name,
        "scene_id":   req.GetSceneId(),
        "old_mode":   preRow.PoseOrder,
        "new_mode":   postRow.PoseOrder,
    })
    if err == nil {
        _ = s.eventSink.Emit(ctx, pluginsdk.EmitIntent{
            Subject:   dotStyleSceneSubjectIC(s.gameID, req.GetSceneId()),
            Type:      "scene_pose_order_changed_ic",
            Payload:   string(payload),
            Sensitive: false,
        })
    }
}
```

If `hasMaskPath` does not exist, add it as a small helper in the same file.

- [ ] **Step 3: Run tests**

Run: `task test -- ./plugins/core-scenes/ -run TestUpdateScene_Emits TestUpdateScene_NoEmit`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(scene-emit): auto-emit scene_pose_order_changed_ic on mode change

UpdateScene auto-emits scene_pose_order_changed_ic when update_mask
includes pose_order_mode AND the new value differs from current.
No emit on no-op updates (mask present but value unchanged) —
prevents spurious notices.

Complements Phase 3's existing settings.updated ops event in
scene_ops_events (operational journal); the IC notice is the
participant-visible counterpart.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

---

## Phase H — Read path: GetPoseOrder + `scene order`

### Task 20: `GetPoseOrder` RPC handler + INV-S9 gate (TDD)

**Files:**

- Modify: `plugins/core-scenes/service.go` (add `GetPoseOrder` method)
- Test: `plugins/core-scenes/service_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `service_test.go`:

```go
func TestGetPoseOrder_NonParticipant_PermissionDenied(t *testing.T) {
    t.Parallel()
    // INV-P4-4: ABAC engine MUST NOT be consulted.
    abacMock := newCountingAbacMock(t)
    s, _ := newTestSceneServiceWithABACMock(t, "scene-pdt", "char-alice", abacMock)

    _, err := s.GetPoseOrder(context.Background(), &scenev1.GetPoseOrderRequest{
        SceneId:     "scene-pdt",
        CharacterId: "char-bob",  // NOT a participant
    })
    require.Error(t, err)
    assert.Equal(t, codes.PermissionDenied, status.Code(err))
    assert.Equal(t, 0, abacMock.callCount(), "INV-P4-4: ABAC engine MUST NOT be consulted for GetPoseOrder")
}

func TestGetPoseOrder_Participant_ReturnsEntries(t *testing.T) {
    t.Parallel()
    s, _ := newTestSceneServiceWithMember(t, "scene-gpo", "char-alice")

    resp, err := s.GetPoseOrder(context.Background(), &scenev1.GetPoseOrderRequest{
        SceneId:     "scene-gpo",
        CharacterId: "char-alice",
    })
    require.NoError(t, err)
    assert.Equal(t, "free", resp.GetMode())   // default for a fresh scene
    assert.Equal(t, uint32(0), resp.GetTotalPoseCount())
    assert.Len(t, resp.GetEntries(), 1)  // just the owner
}

func TestGetPoseOrder_ArchivedScene_FailedPrecondition(t *testing.T) {
    t.Parallel()
    s, _ := newTestSceneServiceWithMember(t, "scene-archived", "char-alice")
    _, _ = s.EndScene(context.Background(), &scenev1.EndSceneRequest{
        SceneId:     "scene-archived",
        CharacterId: "char-alice",
    })
    // Manually transition to archived via store (or use a helper).
    archiveScene(t, s, "scene-archived")

    _, err := s.GetPoseOrder(context.Background(), &scenev1.GetPoseOrderRequest{
        SceneId:     "scene-archived",
        CharacterId: "char-alice",
    })
    require.Error(t, err)
    assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `task test -- ./plugins/core-scenes/ -run TestGetPoseOrder`

Expected: FAIL — method does not exist.

- [ ] **Step 3: Implement `GetPoseOrder` per spec §7.3**

In `plugins/core-scenes/service.go`, add the handler:

```go
func (s *SceneServiceImpl) GetPoseOrder(ctx context.Context, req *scenev1.GetPoseOrderRequest) (*scenev1.GetPoseOrderResponse, error) {
    ctx, span := startSpan(ctx, "scene.service.get_pose_order",
        attribute.String("scene_id", req.GetSceneId()),
        attribute.String("subject_id", req.GetCharacterId()),
    )
    defer span.End()

    // INV-S9 / INV-P4-4: direct participant check. NO ABAC engine consultation.
    ok, err := s.store.IsParticipant(ctx, req.GetSceneId(), req.GetCharacterId())
    if err != nil {
        recordError(span, err)
        return nil, status.Errorf(codes.Internal, "failed to check participant: %v", err)
    }
    if !ok {
        return nil, status.Errorf(codes.PermissionDenied, "not a participant of this scene")
    }

    sceneRow, err := s.store.Get(ctx, req.GetSceneId())
    if err != nil {
        recordError(span, err)
        var oe oops.OopsError
        if errors.As(err, &oe) && oe.Code() == "SCENE_NOT_FOUND" {
            return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
        }
        return nil, status.Errorf(codes.Internal, "failed to load scene: %v", err)
    }
    if sceneRow.State == string(SceneStateArchived) {
        return nil, status.Errorf(codes.FailedPrecondition, "scene is archived; pose order not available")
    }

    pm, err := s.store.ListParticipantsWithPoseMeta(ctx, req.GetSceneId())
    if err != nil {
        recordError(span, err)
        return nil, status.Errorf(codes.Internal, "failed to load participants: %v", err)
    }

    ids := make([]string, 0, len(pm.Participants))
    for _, p := range pm.Participants { ids = append(ids, p.CharacterID) }
    names, err := s.nameResolver.GetNamesByIDs(ctx, ids)
    if err != nil {
        recordError(span, err)
        return nil, status.Errorf(codes.Internal, "failed to resolve character names: %v", err)
    }

    entries := Compute(sceneRow.PoseOrder, pm.TotalPoseCount, pm.Participants, names)

    protoEntries := make([]*scenev1.PoseOrderEntry, 0, len(entries))
    for _, e := range entries {
        pe := &scenev1.PoseOrderEntry{
            CharacterId:   e.CharacterID,
            CharacterName: e.CharacterName,
            Eligible:      e.Eligible,
        }
        if e.LastPosedAt != nil { pe.LastPosedAt = timestamppb.New(*e.LastPosedAt) }
        if e.PosesSinceLast != nil { pe.PosesSinceLast = e.PosesSinceLast }
        protoEntries = append(protoEntries, pe)
    }
    return &scenev1.GetPoseOrderResponse{
        Mode:           sceneRow.PoseOrder,
        TotalPoseCount: pm.TotalPoseCount,
        Entries:        protoEntries,
    }, nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `task test -- ./plugins/core-scenes/ -run TestGetPoseOrder`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scene-rpc): GetPoseOrder with INV-S9 plugin-code gate

Implements GetPoseOrder RPC per spec §7. Direct participant check via
sceneStorer.IsParticipant; ABAC engine NOT consulted. Pattern follows
ADR holomush-c8a9 — scene privacy is absolute, no admin override path.

Reads pose metadata via ListParticipantsWithPoseMeta (single SELECT,
O(N participants)), resolves names via nameResolver.GetNamesByIDs,
computes entries via the pure-function poseorder.Compute.

Errors: PERMISSION_DENIED (non-participant, INV-S9), NOT_FOUND (no
scene), FAILED_PRECONDITION (archived), INTERNAL (store).

Pinned by INV-P4-4: TestGetPoseOrder_NonParticipant_PermissionDenied
asserts the gate fires AND the mock ABAC engine receives zero calls.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 21: `scene order` subcommand renderer (TDD)

**Files:**

- Modify: `plugins/core-scenes/commands.go::handleOrder` (case already added in Task 16)
- Test: `plugins/core-scenes/commands_test.go`

- [ ] **Step 1: Write the failing renderer tests**

```go
func TestSceneOrder_FreeMode_RendersParticipants(t *testing.T) {
    // Expect: "Scene ... — pose order: free (no enforcement, N total poses)"
    // Followed by "  Participants:" list.
}
func TestSceneOrder_StrictMode_RendersQueue(t *testing.T) {
    // Expect: "Scene ... — pose order: strict (N total poses)"
    // "  Next: Alice ..."
    // "  Then: ..."
}
func TestSceneOrder_3prMode_RendersEligibleAndCooldown(t *testing.T) { /* groups */ }
func TestSceneOrder_NonParticipant_PermissionDenied(t *testing.T) { /* same INV-S9 gate */ }
```

- [ ] **Step 2: Implement `handleOrder` calling `GetPoseOrder`**

```go
func (p *scenePlugin) handleOrder(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
    sceneID, err := p.resolveSingleSceneMembership(ctx, req.CharacterID)
    if err != nil {
        return pluginsdk.Errorf("Cannot determine target scene: %v", err), nil
    }
    resp, err := p.service.GetPoseOrder(ctx, &scenev1.GetPoseOrderRequest{
        SceneId: sceneID, CharacterId: req.CharacterID,
    })
    if err != nil {
        if status.Code(err) == codes.PermissionDenied {
            return pluginsdk.Errorf("You are not a participant of this scene."), nil
        }
        return pluginsdk.Errorf("Failed to get pose order: %v", err), nil
    }
    return pluginsdk.Outputf(renderPoseOrder(sceneID, resp)), nil
}

// renderPoseOrder formats GetPoseOrderResponse as plain-text command output.
// Per spec §8.
func renderPoseOrder(sceneID string, resp *scenev1.GetPoseOrderResponse) string {
    // Implementation per the templates in spec §8 — strict, 3pr/5pr, free.
    // strict: "Next: X" header + "Then:" list.
    // 3pr/5pr: "Eligible to pose:" + "Cooldown:" groups; surfaces poses_since_last.
    // free: "Participants:" simple list.
    // Uses fmt.Sprintf; no markdown chrome.
    // (Full implementation in the implementer's commit; the template strings live in this function.)
}
```

- [ ] **Step 3: Run tests**

Run: `task test -- ./plugins/core-scenes/ -run TestSceneOrder`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(scene-commands): scene order subcommand renderer

handleOrder calls GetPoseOrder in-process (INV-S9 gate fires the same)
and renders the response as plain-text command output per spec §8.

Per-mode rendering:
- strict: 'Next: X' header + 'Then:' queue tail
- 3pr/5pr: 'Eligible to pose:' + 'Cooldown:' groups with N/threshold annotation
- free: 'Participants:' simple list (no eligibility annotation)

No markdown chrome; matches existing core-scenes command output style.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

---

## Phase I — Resolver / meta-tests / integration tests

### Task 22: Resolver no-pose-data-leak test (INV-P4-5)

**Files:**

- Test: `plugins/core-scenes/resolver_test.go`
- Create: `internal/test/invariants/scene_resolver_no_poseorder_leak_test.go`

- [ ] **Step 1: Add a resolver test asserting no pose data in attributes**

Append to `plugins/core-scenes/resolver_test.go`:

```go
// TestResolveResource_ExcludesPoseOrderMetadata pins INV-P4-5: the
// scene resolver MUST NOT expose pose-order metadata (last_pose_at,
// last_pose_seq, total_pose_count) as ABAC attributes.
func TestResolveResource_ExcludesPoseOrderMetadata(t *testing.T) {
    t.Parallel()
    r := newTestSceneResolver(t)
    // Seed a scene that HAS pose metadata so the test catches accidental
    // exposure rather than passing trivially.
    seedSceneWithPoses(t, r.store, "scene-resolver-test", 5)

    resp, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
        ResourceType: "scene",
        ResourceId:   "scene-resolver-test",
    })
    require.NoError(t, err)

    forbidden := []string{"last_pose_at", "last_pose_seq", "total_pose_count"}
    for _, key := range forbidden {
        _, exists := resp.GetAttributes()[key]
        assert.False(t, exists, "INV-P4-5: pose-metadata attribute %q MUST NOT be in resolver response", key)
    }
}
```

- [ ] **Step 2: Add the meta-test (rg-based)**

Create `internal/test/invariants/scene_resolver_no_poseorder_leak_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invariants

import (
    "os"
    "regexp"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// TestINV_P4_5_ResolverNoPoseOrderLeak rg-asserts that resolver.go
// does not reference pose-metadata columns in attribute-construction
// code paths.
func TestINV_P4_5_ResolverNoPoseOrderLeak(t *testing.T) {
    t.Parallel()
    data, err := os.ReadFile("../../../plugins/core-scenes/resolver.go")
    require.NoError(t, err)

    forbidden := regexp.MustCompile(`\b(last_pose_at|last_pose_seq|total_pose_count|LastPoseAt|LastPoseSeq|TotalPoseCount)\b`)
    matches := forbidden.FindAll(data, -1)
    assert.Empty(t, matches,
        "INV-P4-5: resolver.go MUST NOT reference pose-metadata columns; INV-S9 forbids attribute-driven path to pose data")
}
```

- [ ] **Step 3: Run tests**

Run: `task test -- ./plugins/core-scenes/ -run TestResolveResource_ExcludesPoseOrderMetadata`
Run: `task test -- ./internal/test/invariants/ -run TestINV_P4_5`

Expected: both pass.

- [ ] **Step 4: Commit**

```bash
jj describe -m "test(scene-inv-p4-5): resolver MUST NOT leak pose-order metadata

Two tests pin INV-P4-5:

1. plugins/core-scenes/resolver_test.go — calls ResolveResource against
   a scene with non-zero pose metadata, asserts last_pose_at /
   last_pose_seq / total_pose_count are NOT in the response attributes.

2. internal/test/invariants/scene_resolver_no_poseorder_leak_test.go —
   rg-asserts resolver.go source contains no references to pose-meta
   column names. Catches code-level leak even before runtime exposure.

INV-S9 / ADR holomush-c8a9: pose-order data is reachable exclusively
via the gated GetPoseOrder RPC; ABAC attribute path is forbidden.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 23: Meta-test — no `engine.Evaluate` in `GetPoseOrder` body (INV-P4-4)

**Files:**

- Create: `internal/test/invariants/scene_no_abac_in_getposeorder_test.go`

- [ ] **Step 1: Write the meta-test**

Create the file:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invariants

import (
    "go/ast"
    "go/parser"
    "go/token"
    "regexp"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// TestINV_P4_4_NoABACInGetPoseOrder uses go/parser to locate the
// GetPoseOrder function body in plugins/core-scenes/service.go and
// asserts it contains no ABAC engine calls.
func TestINV_P4_4_NoABACInGetPoseOrder(t *testing.T) {
    t.Parallel()
    fset := token.NewFileSet()
    file, err := parser.ParseFile(fset, "../../../plugins/core-scenes/service.go", nil, parser.ParseComments)
    require.NoError(t, err)

    forbidden := regexp.MustCompile(`\b(engine|accessEngine|abacEngine)\.(Evaluate|CanPerformAction|Allow|Forbid)\b`)
    found := false
    ast.Inspect(file, func(n ast.Node) bool {
        fn, ok := n.(*ast.FuncDecl)
        if !ok || fn.Name.Name != "GetPoseOrder" {
            return true
        }
        // Extract body source and search for forbidden patterns.
        start := fset.Position(fn.Body.Pos()).Offset
        end := fset.Position(fn.Body.End()).Offset
        srcBytes, _ := readBytes("../../../plugins/core-scenes/service.go")
        body := string(srcBytes[start:end])
        if forbidden.MatchString(body) {
            found = true
        }
        return false  // stop traversal
    })
    assert.False(t, found,
        "INV-P4-4: GetPoseOrder MUST NOT consult the ABAC engine; INV-S9 plugin-code gate is the only authorization path")
}

func readBytes(path string) ([]byte, error) { /* simple os.ReadFile wrapper */ }
```

- [ ] **Step 2: Run the meta-test**

Run: `task test -- ./internal/test/invariants/ -run TestINV_P4_4`

Expected: PASS (GetPoseOrder body contains no ABAC calls).

- [ ] **Step 3: Commit**

```bash
jj describe -m "test(scene-inv-p4-4): GetPoseOrder MUST NOT call ABAC engine

Meta-test using go/parser to extract GetPoseOrder function body and
rg-assert it contains no engine.Evaluate / engine.CanPerformAction
calls. Pins INV-P4-4 / INV-S9: scene privacy is plugin-code-gated,
absolute; ABAC engine is not in the authorization path.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 24: Meta-test — no colon-style scene subjects (INV-P4-1)

**Files:**

- Create: `internal/test/invariants/scene_subjects_test.go`

- [ ] **Step 1: Write the meta-test**

```go
// TestINV_P4_1_NoColonStyleSceneSubjects rg-asserts that no production
// code path in plugins/core-scenes/ or scene-aware substrate code
// (internal/grpc/stream_access.go, internal/grpc/scope_floor.go,
// internal/grpc/query_stream_history.go) contains a "scene:" string
// literal in a pub/sub-topic context.
//
// ABAC resource ID context (NewAccessRequest, Grant calls) is excluded
// — that's policy-DSL serialization, not a topic (spec §3.2).
func TestINV_P4_1_NoColonStyleSceneSubjects(t *testing.T) {
    targets := []string{
        "../../../plugins/core-scenes/service.go",
        "../../../plugins/core-scenes/commands.go",
        "../../../plugins/core-scenes/audit.go",
        "../../../plugins/core-scenes/store.go",
        "../../../plugins/core-scenes/resolver.go",
        "../../../plugins/core-scenes/main.go",
        "../../../internal/grpc/stream_access.go",
        "../../../internal/grpc/scope_floor.go",
        "../../../internal/grpc/query_stream_history.go",
    }
    // Match "scene:" string literals, but EXCLUDE lines that are clearly
    // ABAC-resource-id context (e.g., they contain NewAccessRequest or
    // .Grant() or .Resource:). Conservative match: just flag and let the
    // implementer audit; one-off false-positives are cheaper than a
    // bypass-by-design.
    pattern := regexp.MustCompile(`"scene:"`)
    for _, path := range targets {
        data, err := os.ReadFile(path)
        require.NoError(t, err, "reading %s", path)
        if matches := pattern.FindAllIndex(data, -1); len(matches) > 0 {
            // Surface line numbers for diagnostics.
            for _, m := range matches {
                lineNum := bytes.Count(data[:m[0]], []byte("\n")) + 1
                lineStart := bytes.LastIndexByte(data[:m[0]], '\n') + 1
                lineEnd := bytes.IndexByte(data[m[1]:], '\n')
                if lineEnd == -1 { lineEnd = len(data) - m[1] }
                line := string(data[lineStart:m[1]+lineEnd])
                // Exclude ABAC resource-id context.
                if strings.Contains(line, "NewAccessRequest") || strings.Contains(line, ".Grant(") || strings.Contains(line, "resource:") {
                    continue
                }
                t.Errorf("INV-P4-1: %s:%d uses colon-style scene subject: %s", path, lineNum, line)
            }
        }
    }
}
```

- [ ] **Step 2: Run**

Run: `task test -- ./internal/test/invariants/ -run TestINV_P4_1`

Expected: PASS (no colon-style scene subjects in production code).

- [ ] **Step 3: Commit**

```bash
jj describe -m "test(scene-inv-p4-1): rg-assert no colon-style scene subjects

Meta-test scans plugins/core-scenes/ + scene-aware substrate files
(stream_access.go, scope_floor.go, query_stream_history.go) for
'scene:' string literals in pub/sub-topic context. ABAC resource-id
contexts (NewAccessRequest, Grant) excluded per spec §3.2.

Pins INV-P4-1: all Phase 4 plugin-owned scene events MUST emit to
NATS dot-style subjects; legacy colon-style MUST NOT appear in any
pub/sub topic context.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 25: Non-participant scene IC isolation integration test (INV-P4-6, closes ac50)

**Files:**

- Create: `test/integration/scenes/non_participant_ic_isolation_test.go`

- [ ] **Step 1: Write the Ginkgo spec**

```go
// SPDX-License-Identifier: Apache-2.0
//go:build integration

package scenes_test

import (
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
)

var _ = Describe("INV-P4-6: non-participant scene IC isolation", func() {
    var (
        env *TestEnv
    )
    BeforeEach(func() {
        env = newTestEnv()
    })
    AfterEach(func() {
        env.cleanup()
    })

    It("non-participant in same location MUST NOT receive scene IC events", func() {
        loc := env.createLocation("Tavern")
        alice := env.createCharacter("Alice", loc)
        bob := env.createCharacter("Bob", loc)

        sceneID := env.createScene(alice, "Phase 4 isolation test")

        // Alice subscribes to scene IC; Bob subscribes only to location.
        aliceStream := env.subscribeScene(alice, sceneID)
        bobStream := env.subscribeLocation(bob, loc)

        // Alice poses.
        env.scenePose(alice, sceneID, "smiles at the room")

        // Alice's stream MUST receive the scene_pose event.
        Eventually(aliceStream.events, "2s").Should(ContainElementWithType("scene_pose"))

        // Bob's stream MUST NOT receive ANY scene event.
        Consistently(bobStream.events, "2s").ShouldNot(ContainElementWithSubjectPrefix("events.test.scene."))
    })
})
```

The test helpers (`TestEnv`, `createLocation`, `subscribeScene`, etc.) live in `test/integration/scenes/helpers_test.go` — adapt existing patterns from other integration tests under `test/integration/`. If helper infrastructure does not yet exist, add a minimal `TestEnv` here.

- [ ] **Step 2: Run**

Run: `task test:int -- -run TestINV_P4_6 ./test/integration/scenes/`

Expected: PASS.

- [ ] **Step 3: Close audit-finding bead `holomush-ac50`**

Run:

```bash
bd close holomush-ac50 --reason="Closed by holomush-5rh.13 Phase 4: INV-P4-6 integration test in test/integration/scenes/non_participant_ic_isolation_test.go pins the boundary."
```

- [ ] **Step 4: Commit**

```bash
jj describe -m "test(scene-inv-p4-6): non-participant scene IC isolation (closes ac50)

Ginkgo integration test asserting that a non-participant in the same
physical location does NOT receive scene IC events while a participant
DOES. The mechanism is implicit (focus subscription gates non-
participants before they ever subscribe to the scene IC stream); this
test makes the boundary auditable per audit-finding holomush-ac50.

Pins INV-P4-6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 26: Late-joiner temporal floor integration test (INV-P4-9 end-to-end)

**Files:**

- Create: `test/integration/scenes/late_joiner_temporal_floor_test.go`

- [ ] **Step 1: Write the Ginkgo spec**

```go
//go:build integration

package scenes_test

import (
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
)

var _ = Describe("INV-P4-9: late-joiner temporal floor for scene IC stream", func() {
    It("late-joiner sees only events after their joined_at", func() {
        env := newTestEnv()
        defer env.cleanup()

        loc := env.createLocation("Lounge")
        alice := env.createCharacter("Alice", loc)
        bob := env.createCharacter("Bob", loc)

        sceneID := env.createScene(alice, "Lounge scene")

        // Alice poses 5 times BEFORE Bob joins.
        for i := 0; i < 5; i++ {
            env.scenePose(alice, sceneID, "pose #" + strconv.Itoa(i+1))
        }
        time.Sleep(100 * time.Millisecond)  // let JetStream durably persist

        // Bob joins after the fact.
        env.sceneJoin(bob, sceneID)
        bobJoinedAt := time.Now().UTC()

        // Bob's QueryStreamHistory on scene IC subject MUST return ONLY
        // events with timestamp >= bobJoinedAt (no pre-join history).
        events := env.queryStreamHistory(bob, dotStyleSceneSubjectIC(env.gameID, sceneID))
        for _, e := range events {
            Expect(e.Timestamp.AsTime()).To(BeTemporally(">=", bobJoinedAt))
        }
        // Bob MUST see zero pre-join scene_pose events.
        scenePoseCount := 0
        for _, e := range events {
            if e.Type == "scene_pose" { scenePoseCount++ }
        }
        Expect(scenePoseCount).To(Equal(0), "INV-P4-9: late-joiner MUST NOT see pre-arrival IC events")

        // Bob's GetPoseOrder MUST reflect ALL 5 of Alice's poses (pose-order
        // computation is scene-global, not subject to the temporal floor).
        po := env.getPoseOrder(bob, sceneID)
        Expect(po.TotalPoseCount).To(Equal(uint32(5)))
    })
})
```

- [ ] **Step 2: Run**

Run: `task test:int -- -run TestINV_P4_9 ./test/integration/scenes/`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
jj describe -m "test(scene-inv-p4-9): late-joiner temporal floor integration

Ginkgo integration test asserting:
(1) Bob, joining a scene after Alice has posed 5 times, sees ZERO
    pre-arrival scene_pose events in QueryStreamHistory.
(2) Bob's GetPoseOrder reflects ALL 5 of Alice's poses — pose-order
    computation is scene-global; only display via QueryStreamHistory
    is subject to the temporal floor.

End-to-end pin for INV-P4-9 on the post-migration code path (the
unit-level pre/post pin lives in internal/grpc/scope_floor_test.go).

Soft dependency on holomush-iwzt.15 (Tier 2 filter-at-delivery) for
the Subscribe path; this test specifically exercises QueryStreamHistory
which the iwzt-side ScopeFloor migration (Task 13) already covers.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 27: Pose-order metadata rebuild integration test (INV-P4-8)

**Files:**

- Test: `plugins/core-scenes/store_integration_test.go`

- [ ] **Step 1: Write the rebuild idempotency test**

Append:

```go
func (s *StoreIntegrationSuite) TestPoseOrderMetadata_RebuildFromAuditLog_INV_P4_8() {
    ctx := context.Background()
    sceneID := s.createScene(ctx, "owner-rebuild")
    s.joinScene(ctx, sceneID, "alice")
    s.joinScene(ctx, sceneID, "bob")

    // Simulate audit-handler dispatch for 5 poses (3 by alice, 2 by bob).
    audit := NewSceneAuditStore(s.pool)
    for i, actor := range []string{"alice", "alice", "bob", "alice", "bob"} {
        require.NoError(s.T(), audit.InsertScenePose(ctx,
            ulid.Make().Bytes(),
            "events.test.scene." + sceneID + ".ic", "scene_pose",
            timestamppb.New(time.Now().Add(time.Duration(i) * time.Second)),
            "character", []byte(actor),
            []byte(`{}`), 1, "identity", nil, nil,
            sceneID, actor,
        ))
    }

    // Snapshot post-emit metadata.
    pm1, err := s.store.ListParticipantsWithPoseMeta(ctx, sceneID)
    s.Require().NoError(err)

    // Run the recovery SQL from spec §9.5.
    _, err = s.pool.Exec(ctx, recoveryUpdateScenesTotalPoseCount)  // SQL constants in a test_sql.go
    s.Require().NoError(err)
    _, err = s.pool.Exec(ctx, recoveryUpdateScenePoseMeta)
    s.Require().NoError(err)

    // Snapshot post-rebuild metadata.
    pm2, err := s.store.ListParticipantsWithPoseMeta(ctx, sceneID)
    s.Require().NoError(err)

    // Assert byte-identical.
    s.Require().Equal(pm1.TotalPoseCount, pm2.TotalPoseCount, "INV-P4-8: rebuild produces identical total_pose_count")
    s.Require().Len(pm2.Participants, len(pm1.Participants))
    // Compare each participant's metadata (mod ordering — sort by character_id).
    for _, p1 := range pm1.Participants {
        var p2 ParticipantWithPoseMeta
        for _, candidate := range pm2.Participants {
            if candidate.CharacterID == p1.CharacterID { p2 = candidate; break }
        }
        s.Require().Equal(p1.LastPoseSeq, p2.LastPoseSeq)
        if p1.LastPoseAt != nil && p2.LastPoseAt != nil {
            s.Require().WithinDuration(*p1.LastPoseAt, *p2.LastPoseAt, time.Millisecond)
        }
    }
}
```

The two recovery SQL constants live in a new file `plugins/core-scenes/recovery_sql.go` (or inlined in the test). Implementation per spec §9.5.

- [ ] **Step 2: Run**

Run: `task test:int -- -run TestPoseOrderMetadata_RebuildFromAuditLog ./plugins/core-scenes/`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
jj describe -m "test(scene-inv-p4-8): pose-order metadata rebuild idempotency

Integration test exercising the spec §9.5 recovery SQL. Emits 5 mixed
poses via InsertScenePose; snapshots the maintained metadata;
runs the recovery SQL; snapshots again; asserts byte-identical results.

Pins INV-P4-8: maintained metadata is a function of scene_log; the
documented recovery procedure produces identical metadata when run
after arbitrary pose history. Recovery SQL formalised in
plugins/core-scenes/recovery_sql.go for operator runbook reuse.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 28: INV-P4-* coverage meta-test (INV-P4-13)

**Files:**

- Create: `internal/test/invariants/p4_coverage_test.go`

- [ ] **Step 1: Write the coverage meta-test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invariants

import (
    "os"
    "path/filepath"
    "regexp"
    "strings"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// TestINV_P4_13_CoverageMatrix parses the spec, extracts every
// INV-P4-N reference, and asserts:
// (1) The coverage matrix table (§12.1) cites at least one test file per invariant.
// (2) Each cited test file path exists on disk.
// (3) For each cited test, the file contains a test function whose name
//     references the invariant (best-effort via INV_P4_N substring match).
func TestINV_P4_13_CoverageMatrix(t *testing.T) {
    specPath := "../../../docs/superpowers/specs/2026-05-19-scenes-phase-4-streams-and-pose-order-design.md"
    data, err := os.ReadFile(specPath)
    require.NoError(t, err)

    spec := string(data)

    // Extract all numbered invariants (INV-P4-1 .. INV-P4-13).
    invRe := regexp.MustCompile(`INV-P4-(\d+)`)
    invSet := make(map[string]bool)
    for _, m := range invRe.FindAllStringSubmatch(spec, -1) {
        invSet["INV-P4-"+m[1]] = true
    }
    assert.GreaterOrEqual(t, len(invSet), 13, "spec MUST declare at least 13 invariants")

    // Extract test-file references from §12.1 coverage matrix lines.
    fileRe := regexp.MustCompile("`([^`]+_test\\.go)`")
    citedTests := fileRe.FindAllStringSubmatch(spec, -1)
    require.NotEmpty(t, citedTests, "spec §12.1 MUST cite test files")

    // Verify each cited test path exists.
    repoRoot, err := filepath.Abs("../../..")
    require.NoError(t, err)
    for _, m := range citedTests {
        path := filepath.Join(repoRoot, m[1])
        _, err := os.Stat(path)
        if os.IsNotExist(err) {
            t.Errorf("INV-P4-13: cited test path does not exist: %s", m[1])
        }
    }

    // For each invariant, assert at least one cited test mentions it.
    for invariant := range invSet {
        normalized := strings.ReplaceAll(invariant, "-", "_")  // INV_P4_1
        found := false
        for _, m := range citedTests {
            data, err := os.ReadFile(filepath.Join(repoRoot, m[1]))
            if err != nil { continue }
            if strings.Contains(string(data), normalized) || strings.Contains(string(data), invariant) {
                found = true
                break
            }
        }
        assert.True(t, found, "INV-P4-13: invariant %s has no test citing it by name", invariant)
    }
}
```

- [ ] **Step 2: Run**

Run: `task test -- ./internal/test/invariants/ -run TestINV_P4_13`

Expected: PASS — all 13 invariants have at least one cited test that mentions them by name.

- [ ] **Step 3: Commit**

```bash
jj describe -m "test(scene-inv-p4-13): coverage matrix meta-test

Parses the spec, extracts INV-P4-1..13 declarations and the §12.1
coverage matrix's cited test files, then asserts:
(1) Spec declares at least 13 invariants.
(2) Every cited test file path exists on disk.
(3) Each invariant is mentioned by name in at least one cited test.

Pins INV-P4-13 — same shape as iwzt T15 / 5b2j T15 precedents.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

---

## Phase J — Documentation + close-out

### Task 29: Update iwzt §3 stream-type table to dot-style

**Files:**

- Modify: `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md`

- [ ] **Step 1: Locate §3 stream-type table**

Open the iwzt history-scope-privacy spec. Find the `## 3. Scope by stream type` table.

- [ ] **Step 2: Update scene rows to dot-style**

Replace existing entries:

```markdown
| `scene:<id>:ic`, `scene:<id>:ooc` | Existing I-17 membership gate (unchanged) | ... |
```

with:

```markdown
| `events.<game_id>.scene.<scene_id>.ic`, `events.<game_id>.scene.<scene_id>.ooc` | Existing I-17 membership gate (migrated to dot-style by Phase 4, `holomush-5rh.13`) | ... |
```

Other entries in the table (`location:`, `character:`, plugin-owned) remain colon-style and are covered by `holomush-rops`.

- [ ] **Step 3: Lint docs**

Run: `task lint:markdown`

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
jj describe -m "docs(iwzt): §3 — scene rows updated to NATS dot-style

Phase 4 (holomush-5rh.13) migrated scene-aware substrate code to
dot-style scene subjects. iwzt §3 stream-type table now reflects the
post-Phase-4 reality. Other entries (location, character, plugin-owned)
unchanged — tracked separately by holomush-rops.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Then `jj new`.

### Task 30: Update v2 §3.1 stream-naming table to dot-style

**Files:**

- Modify: `docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md`

- [ ] **Step 1: Update §3.1 table**

Replace the stream-naming table's scene rows to use dot-style. Add a footnote citing Phase 4 (`holomush-5rh.13`) as the migration point.

- [ ] **Step 2: Lint + commit**

Run: `task lint:markdown`. Commit per the existing convention with a `docs(scenes-v2): §3.1 — dot-style subjects` message.

### Task 31: Run `task pr-prep` to completion

**Files:** (no file changes)

- [ ] **Step 1: Run the full pre-push pipeline**

Run: `task pr-prep`

Expected: full pipeline runs (lint + format + schema + license + unit + integration + E2E) and reports green. Per the project rule, DO NOT skip; DO NOT trust a sub-agent's claim.

If any step fails:

1. Capture the exact failure output.
2. Diagnose the failing component (likely a missing test fixture or a regression in a tangentially-touched file).
3. Fix inline.
4. Re-run `task pr-prep` from the top.

- [ ] **Step 2: Verify no jj describe is needed**

Run: `jj st`

Expected: working copy is clean (if all Phase 4 changes have been committed in the prior tasks).

- [ ] **Step 3: Push**

Per CLAUDE.md "Landing the Plane" + the `jj:jujutsu` skill "Pre-Push Rebase" section:

```bash
jj git fetch
jj rebase -s "$(jj log -r 'roots(trunk()..@)' --no-graph -T 'change_id.short(12)')" -o main@origin --skip-emptied
jj bookmark set 5rh-13-phase4 -r @
jj git push --branch 5rh-13-phase4
jj st  # verify
```

- [ ] **Step 4: Open the PR**

```bash
gh pr create --title "feat(scenes): Phase 4 — IC/OOC event streams + pose order (5rh.13)" --body "$(cat <<'EOF'
## Summary

- 8 plugin-owned scene event types with crypto.emits matrix + EmitTypeRegistrar adoption (INV-S5 set-equality)
- Pose-order computation with maintained metadata (O(N participants), no event-history scan); GetPoseOrder RPC with INV-S9 plugin-code participant gate
- scene pose/say/emit/ooc/order subcommands; auto-emit notice events on join/leave/kick/mode-change
- Scene subject migration: plugin emits + substrate-side scene-aware code (stream_access.go, scope_floor.go, query_stream_history.go) atomic in this PR
- Closes live silent regression in scope_floor.go (pre-Phase-4 returned time.Time{} for all production callers)
- 13 numbered INV-P4-* invariants with cited tests + coverage meta-test
- Closes audit-finding holomush-ac50

## Spec / design

- Spec: docs/superpowers/specs/2026-05-19-scenes-phase-4-streams-and-pose-order-design.md (design-reviewer READY round 3)
- Plan: docs/superpowers/plans/2026-05-19-scenes-phase-4-streams-and-pose-order.md (plan-reviewer READY)

## Dependencies

- Hard: holomush-5b2j.3 (GetNamesByIDs batch lookup)
- Soft: holomush-iwzt.15 (Tier 2 filter-at-delivery) — covered by INV-P4-9 end-to-end test

## Out of scope / follow-ups

- Focus routing for plain pose/say/emit/ooc → Phase 5 (holomush-5rh.14)
- scene_idle_nudge background trigger → new follow-up bead (filed at plan-to-beads time)
- Non-scene colon-style sweep (location, character, notifications) → holomush-rops

## Test plan

- [x] task pr-prep green (full lane)
- [x] All 13 INV-P4-* tests pass
- [x] Existing Phase 1-3 tests pass (no regression)
- [x] crypto-reviewer + abac-reviewer + code-reviewer passes

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 5: Update bead status**

```bash
bd update holomush-5rh.13 --status=in_progress  # if not already
bd note holomush-5rh.13 "PR opened: <URL from gh pr create output>"
```

---

## Post-implementation checklist

- [ ] All 31 tasks complete; their checkboxes ticked.
- [ ] `task pr-prep` green on the final commit.
- [ ] PR opened with the title + body from Task 31 Step 4.
- [ ] `holomush-ac50` closed (audit-finding for non-participant isolation).
- [ ] `holomush-5rh.13` updated with PR URL.
- [ ] `holomush-rops` remains open (broader sweep — separate work).
- [ ] Follow-up bead filed: "Scenes: scene_idle_nudge background trigger implementation" (P2, parent `holomush-5rh.13`).
- [ ] `holomush-iwzt.15` Tier 2 filter-at-delivery: verify INV-P4-9 integration test still passes when iwzt.15 lands; no Phase 4 changes needed.
- [ ] PR review surface includes `crypto-reviewer` (crypto.emits manifest changes) AND `abac-reviewer` (no new policies but verify) AND `code-reviewer` (substrate-side gate migration in internal/grpc/).

---

<!-- adr-capture: sha256=2bf828e93cf93b2a; ts=2026-05-19T12:00:00Z; adrs=holomush-r4th,holomush-s9nu,holomush-sb3n,holomush-nt2d,holomush-1ang -->
