# Scenes Phase 3: Membership Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the participant model, membership operations (join/leave/invite/kick/transfer-ownership), and the ops event journal to the `core-scenes` plugin, replacing Phase 2's owner-only ABAC policies with member-based policies and retrofitting Phase 1+2 lifecycle handlers to emit ops events.

**Architecture:** Two new tables in the plugin's Postgres schema (`scene_participants`, `scene_ops_events`), seven new store methods (transactional, race-safe), five new gRPC RPCs and command handlers, resolver extension to expose `participants` and `invitees` as STRING_LIST attributes, and a clean replacement of the plugin manifest's policy set.

**Tech Stack:** Go 1.23+, pgx/v5, gopher-lua (host), gRPC, protovalidate, oops (errors), slog (structured logging), OpenTelemetry, testify, ginkgo (integration), testcontainers-go (Postgres in tests).

**Spec:** [`docs/superpowers/specs/2026-04-07-scenes-phase-3-membership-design.md`](../specs/2026-04-07-scenes-phase-3-membership-design.md)
**Bead:** holomush-5rh.12

---

## File Structure

### Files to create

| Path | Responsibility |
|------|----------------|
| `plugins/core-scenes/migrations/000003_scene_participants_and_ops_events.up.sql` | Forward migration: create both new tables + indexes |
| `plugins/core-scenes/migrations/000003_scene_participants_and_ops_events.down.sql` | Reverse migration: drop tables in reverse order |
| `plugins/core-scenes/participants.go` | New file: `ParticipantRole` enum, `ParticipantRow` type, `ParticipantOpResult` enum |
| `plugins/core-scenes/ops_events.go` | New file: `OpsEventKind` enum, `recordOpsEventTx` helper, kind validation |

### Files to modify

| Path | Modification |
|------|--------------|
| `api/proto/holomush/scene/v1/scene.proto` | Add `KickFromScene` and `TransferOwnership` RPCs + their request/response messages |
| `plugins/core-scenes/store.go` | Add `CreateWithOwner`, `GetWithMembership`, `AddParticipant`, `RemoveParticipant`, `InviteParticipant`, `KickParticipant`, `TransferOwnership`, `ListParticipants`, `GetParticipant`, `classifyJoinMiss`, `classifyTransferMiss`. Retrofit `End`, `Pause`, `Resume`, `Update` to be transactional and emit ops events. |
| `plugins/core-scenes/service.go` | Extend `sceneStorer` interface, retrofit `CreateScene` to call `CreateWithOwner`, add `JoinScene`, `LeaveScene`, `InviteToScene`, `KickFromScene`, `TransferOwnership` handlers |
| `plugins/core-scenes/resolver.go` | Add `participants` + `invitees` to `GetSchema`, switch `ResolveResource` to call `GetWithMembership` |
| `plugins/core-scenes/commands.go` | Add `handleJoin`, `handleLeave`, `handleInvite`, `handleKick`, `handleTransfer`; extend dispatcher's known-subcommands |
| `plugins/core-scenes/plugin.yaml` | Replace Phase 2 policies with Phase 3 policies (clean break) |
| `plugins/core-scenes/metrics.go` | Add 6 new metric stubs |
| `plugins/core-scenes/types.go` | Add `OpsEventKind` and re-export of new enums |
| `plugins/core-scenes/store_integration_test.go` | Add ~14 store integration tests + helper functions |
| `plugins/core-scenes/service_test.go` | Extend `fakeStore` with new methods, add ~10 service unit tests |
| `plugins/core-scenes/resolver_test.go` | Add ~3 resolver tests |

### Files NOT touched

- `plugins/core-scenes/main.go` — no plugin lifecycle changes
- `plugins/core-scenes/observability.go` — startSpan/recordError already exist
- `plugins/core-scenes/lifecycle.go` — state-machine helpers don't change

---

## Task List

The plan has 21 tasks. Each task is a single coherent commit. Order matters: later tasks depend on artifacts from earlier ones.

| # | Task | Depends on |
|---|------|------------|
| 1 | Proto: add `KickFromScene` + `TransferOwnership` messages and RPCs | — |
| 2 | Migration 000003 + new types files (`participants.go`, `ops_events.go`) | 1 |
| 3 | Integration test helpers for participants and ops events | 2 |
| 4 | `recordOpsEventTx` helper + integration test | 2, 3 |
| 5 | `CreateWithOwner` store method + retrofit `CreateScene` handler | 4 |
| 6 | `GetWithMembership` store method | 5 |
| 7 | Resolver extension: expose `participants` + `invitees` | 6 |
| 8 | `JoinScene`: `AddParticipant` + `classifyJoinMiss` + service handler | 5, 6 |
| 9 | `LeaveScene`: `RemoveParticipant` + service handler with owner pre-check | 5 |
| 10 | `InviteToScene`: `InviteParticipant` + service handler | 5 |
| 11 | `KickFromScene`: `KickParticipant` + service handler | 5 |
| 12 | `TransferOwnership`: store + `classifyTransferMiss` + service handler | 5 |
| 13 | `ListParticipants` + `GetParticipant` store methods | 5 |
| 14 | Phase 1+2 retrofit: emit ops events from `End`/`Pause`/`Resume`/`Update` | 4 |
| 15 | Command handlers: `handleJoin`/`Leave`/`Invite`/`Kick`/`Transfer` | 8–12 |
| 16 | ABAC policy replacement in `plugin.yaml` | — (independent, but tested by 21) |
| 17 | Metric stubs in `metrics.go` | — (independent) |
| 18 | Integration test lockdown suite (the spec's ~9 ABAC swap tests) | 8–17 |
| 19 | Boundary and invariant tests (paused-state ops, denorm consistency, ops event count, PK uniqueness) | 8–14 |
| 20 | E2E binary plugin tests in `test/integration/plugin/binary_plugin_test.go` | 1–17 |
| 21 | `task pr-prep` verification + final commit | 18, 19, 20 |

---

## Task 1: Proto additions for KickFromScene and TransferOwnership

**Files:**

- Modify: `api/proto/holomush/scene/v1/scene.proto`
- Generated (regenerated by `task proto:gen`): `pkg/proto/holomush/scene/v1/scene.pb.go`, `pkg/proto/holomush/scene/v1/scene_grpc.pb.go`

- [ ] **Step 1: Inspect current proto state**

Run: `head -30 api/proto/holomush/scene/v1/scene.proto`
Expected: shows `service SceneService { ... }` with `JoinScene`, `LeaveScene`, `InviteToScene` already declared but no `KickFromScene` or `TransferOwnership`.

- [ ] **Step 2: Add the two new RPCs to the service block**

Edit `api/proto/holomush/scene/v1/scene.proto`. Find the `rpc InviteToScene` line and add two new RPCs immediately after it (before `rpc CastPublishVote`):

```proto
  rpc InviteToScene(InviteToSceneRequest) returns (InviteToSceneResponse);
  rpc KickFromScene(KickFromSceneRequest) returns (KickFromSceneResponse);
  rpc TransferOwnership(TransferOwnershipRequest) returns (TransferOwnershipResponse);
  rpc CastPublishVote(CastPublishVoteRequest) returns (CastPublishVoteResponse);
```

- [ ] **Step 3: Add the request/response messages**

Append after the `InviteToSceneResponse {}` message and before `message CastPublishVoteRequest`:

```proto
message KickFromSceneRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string scene_id = 2 [(buf.validate.field).string.min_len = 1];
  string target_character_id = 3 [(buf.validate.field).string.min_len = 1];
}

message KickFromSceneResponse {}

message TransferOwnershipRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string scene_id = 2 [(buf.validate.field).string.min_len = 1];
  string new_owner_character_id = 3 [(buf.validate.field).string.min_len = 1];
}

message TransferOwnershipResponse {}
```

- [ ] **Step 4: Regenerate proto bindings**

Run: `task proto:gen`
Expected: success, no errors. Check `pkg/proto/holomush/scene/v1/scene_grpc.pb.go` now contains `KickFromScene` and `TransferOwnership` server method declarations.

- [ ] **Step 5: Verify build still passes**

Run: `task build`
Expected: success. The plugin's `SceneServiceImpl` embeds `UnimplementedSceneServiceServer` so unimplemented RPCs do not break compilation — they will return `Unimplemented` at runtime if called before Tasks 8/12 land.

- [ ] **Step 6: Commit**

Run via jj (per `references/vcs-preamble.md`):

```bash
jj --no-pager commit -m "feat(scenes): add KickFromScene and TransferOwnership proto RPCs

Adds the two membership RPCs that Phase 3 will implement. The plugin's
SceneServiceImpl embeds UnimplementedSceneServiceServer, so these RPCs
return Unimplemented until the handlers land in later tasks.

Refs: holomush-5rh.12"
```

---

## Task 2: Migration 000003 + new type files

**Files:**

- Create: `plugins/core-scenes/migrations/000003_scene_participants_and_ops_events.up.sql`
- Create: `plugins/core-scenes/migrations/000003_scene_participants_and_ops_events.down.sql`
- Create: `plugins/core-scenes/participants.go`
- Create: `plugins/core-scenes/ops_events.go`

- [ ] **Step 1: Create the up migration**

Create `plugins/core-scenes/migrations/000003_scene_participants_and_ops_events.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 3 schema: scene_participants (membership snapshot) and
-- scene_ops_events (append-only ops journal).
--
-- See docs/superpowers/specs/2026-04-07-scenes-phase-3-membership-design.md
-- for the full design rationale, especially decisions P3.D3 (separate audit
-- table) and P3.D4 (ops vs content separation).

CREATE TABLE IF NOT EXISTS scene_participants (
    scene_id     TEXT        NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    character_id TEXT        NOT NULL,
    role         TEXT        NOT NULL CHECK (role IN ('owner', 'member', 'invited')),
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scene_id, character_id)
);

CREATE INDEX IF NOT EXISTS idx_participants_scene_role
    ON scene_participants(scene_id, role);
CREATE INDEX IF NOT EXISTS idx_participants_character
    ON scene_participants(character_id);

CREATE TABLE IF NOT EXISTS scene_ops_events (
    id          TEXT        PRIMARY KEY,
    scene_id    TEXT        NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    kind        TEXT        NOT NULL CHECK (kind ~ '^[a-z]+\.[a-z_]+$'),
    actor_id    TEXT        NOT NULL,
    target_id   TEXT,
    payload     JSONB       NOT NULL DEFAULT '{}',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_scene_ops_events_scene
    ON scene_ops_events(scene_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_scene_ops_events_target
    ON scene_ops_events(target_id, occurred_at DESC) WHERE target_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_scene_ops_events_kind
    ON scene_ops_events(scene_id, kind, occurred_at DESC);
```

- [ ] **Step 2: Create the down migration**

Create `plugins/core-scenes/migrations/000003_scene_participants_and_ops_events.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP TABLE IF EXISTS scene_ops_events;
DROP TABLE IF EXISTS scene_participants;
```

- [ ] **Step 3: Create the participants type file**

Create `plugins/core-scenes/participants.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "time"

// ParticipantRole represents a character's relationship to a scene.
//
// Per design decision P3.D1, `invited` is a transient role that exists only
// on private scenes. An invitation is a row that grants the holder permission
// to join (and to read scene metadata in a later phase). Calling JoinScene on
// an invited scene atomically promotes the row to `member`. There is no
// `invited` row on open scenes.
type ParticipantRole string

const (
	ParticipantRoleOwner   ParticipantRole = "owner"
	ParticipantRoleMember  ParticipantRole = "member"
	ParticipantRoleInvited ParticipantRole = "invited"
)

// IsValid reports whether r is a recognized participant role.
func (r ParticipantRole) IsValid() bool {
	switch r {
	case ParticipantRoleOwner, ParticipantRoleMember, ParticipantRoleInvited:
		return true
	}
	return false
}

// ParticipantRow is the persistence-layer representation of a row in
// scene_participants. The shape matches the table column-for-column.
type ParticipantRow struct {
	SceneID     string
	CharacterID string
	Role        string
	JoinedAt    time.Time
}

// ParticipantOpResult captures the outcome of an AddParticipant call. The
// service handler uses this to decide whether to emit a membership.join
// ops event (only OpInserted and OpPromoted should emit; OpNoChange must
// not, to keep retries from polluting the audit log).
type ParticipantOpResult int

const (
	// OpInserted indicates a fresh row was added to scene_participants.
	OpInserted ParticipantOpResult = iota
	// OpPromoted indicates an existing row was flipped from invited to member.
	OpPromoted
	// OpNoChange indicates the caller was already a member or owner; the
	// upsert was a no-op.
	OpNoChange
)
```

- [ ] **Step 4: Create the ops events type file**

Create `plugins/core-scenes/ops_events.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// OpsEventKind enumerates the recognised ops event kinds. The dotted naming
// convention is also enforced by the database CHECK constraint on
// scene_ops_events.kind. The Go-side narrow API in recordOpsEventTx prevents
// typos and ad-hoc kinds in handlers.
type OpsEventKind string

const (
	OpsKindMembershipInvite        OpsEventKind = "membership.invite"
	OpsKindMembershipJoin          OpsEventKind = "membership.join"
	OpsKindMembershipLeave         OpsEventKind = "membership.leave"
	OpsKindMembershipKick          OpsEventKind = "membership.kick"
	OpsKindMembershipOwnershipXfer OpsEventKind = "membership.ownership_transferred"
	OpsKindLifecycleCreated        OpsEventKind = "lifecycle.created"
	OpsKindLifecycleEnded          OpsEventKind = "lifecycle.ended"
	OpsKindLifecyclePaused         OpsEventKind = "lifecycle.paused"
	OpsKindLifecycleResumed        OpsEventKind = "lifecycle.resumed"
	OpsKindSettingsUpdated         OpsEventKind = "settings.updated"
)

// IsValid reports whether k is one of the declared OpsEventKind constants.
func (k OpsEventKind) IsValid() bool {
	switch k {
	case OpsKindMembershipInvite, OpsKindMembershipJoin, OpsKindMembershipLeave,
		OpsKindMembershipKick, OpsKindMembershipOwnershipXfer,
		OpsKindLifecycleCreated, OpsKindLifecycleEnded,
		OpsKindLifecyclePaused, OpsKindLifecycleResumed,
		OpsKindSettingsUpdated:
		return true
	}
	return false
}

// recordOpsEventTx inserts a scene_ops_events row inside an existing
// transaction. The kind MUST be one of the OpsEventKind constants — the
// helper rejects unknown kinds with SCENE_OPS_EVENT_INVALID_KIND so typos
// surface as errors instead of silently writing junk.
//
// targetID may be empty for kinds that affect the whole scene (lifecycle.*,
// settings.*); pass "" for those.
//
// payload is marshalled to JSONB. Pass nil or an empty map for kinds that
// don't need extra context.
func recordOpsEventTx(ctx context.Context, tx pgx.Tx, sceneID string, kind OpsEventKind, actorID, targetID string, payload map[string]any) error {
	if !kind.IsValid() {
		return oops.Code("SCENE_OPS_EVENT_INVALID_KIND").
			With("kind", string(kind)).
			Errorf("unknown ops event kind")
	}

	id, err := newOpsEventID()
	if err != nil {
		return oops.Code("SCENE_OPS_EVENT_ID_GEN_FAILED").Wrap(err)
	}

	if payload == nil {
		payload = map[string]any{}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return oops.Code("SCENE_OPS_EVENT_PAYLOAD_MARSHAL_FAILED").
			With("kind", string(kind)).
			Wrap(err)
	}

	var targetParam any
	if targetID == "" {
		targetParam = nil
	} else {
		targetParam = targetID
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO scene_ops_events (id, scene_id, kind, actor_id, target_id, payload)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		id, sceneID, string(kind), actorID, targetParam, payloadJSON,
	)
	if err != nil {
		return oops.Code("SCENE_OPS_EVENT_INSERT_FAILED").
			With("scene_id", sceneID).
			With("kind", string(kind)).
			Wrap(err)
	}
	return nil
}

// newOpsEventID generates a ULID using crypto/rand. Mirrors newSceneID in
// service.go — math/rand is forbidden everywhere per CLAUDE.md.
func newOpsEventID() (string, error) {
	ms := ulid.Timestamp(time.Now())
	id, err := ulid.New(ms, rand.Reader)
	if err != nil {
		return "", err
	}
	return "ope-" + id.String(), nil
}
```

- [ ] **Step 5: Verify build passes**

Run: `task build`
Expected: success. The new types compile but are not yet used by anything.

- [ ] **Step 6: Run lint**

Run: `task lint`
Expected: success. New files have SPDX headers and follow naming conventions.

- [ ] **Step 7: Commit**

```bash
jj --no-pager commit -m "feat(scenes): add Phase 3 migration and type foundations

Adds migration 000003 (scene_participants + scene_ops_events tables with
indexes and constraints) and the Go-side type files for ParticipantRole,
ParticipantRow, ParticipantOpResult, OpsEventKind, and the recordOpsEventTx
helper that all subsequent store methods will use.

Refs: holomush-5rh.12"
```

---

## Task 3: Integration test helpers

**Files:**

- Modify: `plugins/core-scenes/store_integration_test.go` (add helpers near top of file)

- [ ] **Step 1: Add the helper functions**

Append these helpers to `store_integration_test.go` after the `newTestStore` function:

```go
// mustCreateScene inserts a minimal scene row directly via the store's
// Phase 1 Create method. Used by Phase 3 tests that need a pre-existing
// scene but don't care about the participant/ops event side effects of
// CreateWithOwner. Once Task 5 lands, prefer mustCreateSceneWithOwner.
func mustCreateScene(t *testing.T, store *SceneStore, sceneID, ownerID, visibility string) *SceneRow {
	t.Helper()
	row := &SceneRow{
		ID:              sceneID,
		Title:           "Test Scene " + sceneID,
		OwnerID:         ownerID,
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      visibility,
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	require.NoError(t, store.Create(context.Background(), row))
	return row
}

// assertParticipantRowExists asserts that a row exists in scene_participants
// for the given (sceneID, characterID) pair with the expected role. Queries
// the database directly rather than going through any store method, so the
// assertion is independent of the methods under test.
func assertParticipantRowExists(t *testing.T, store *SceneStore, sceneID, characterID, expectedRole string) {
	t.Helper()
	var role string
	err := store.pool.QueryRow(context.Background(),
		`SELECT role FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
		sceneID, characterID,
	).Scan(&role)
	require.NoError(t, err, "expected participant row for (%s, %s) but query failed", sceneID, characterID)
	assert.Equal(t, expectedRole, role)
}

// assertParticipantRowAbsent asserts that no row exists in scene_participants
// for the given (sceneID, characterID) pair. Used to verify deletes.
func assertParticipantRowAbsent(t *testing.T, store *SceneStore, sceneID, characterID string) {
	t.Helper()
	var role string
	err := store.pool.QueryRow(context.Background(),
		`SELECT role FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
		sceneID, characterID,
	).Scan(&role)
	assert.ErrorIs(t, err, pgx.ErrNoRows, "expected participant row for (%s, %s) to be absent", sceneID, characterID)
}

// assertOpsEventRecorded asserts that exactly one row exists in
// scene_ops_events for the given scene with the given kind. Returns the
// payload JSON for the caller to inspect kind-specific fields.
func assertOpsEventRecorded(t *testing.T, store *SceneStore, sceneID string, kind OpsEventKind, expectedActor, expectedTarget string) map[string]any {
	t.Helper()
	var (
		actor   string
		target  *string
		payload []byte
	)
	err := store.pool.QueryRow(context.Background(), `
		SELECT actor_id, target_id, payload FROM scene_ops_events
		WHERE scene_id = $1 AND kind = $2
		ORDER BY occurred_at DESC LIMIT 1`,
		sceneID, string(kind),
	).Scan(&actor, &target, &payload)
	require.NoError(t, err, "expected ops event %s for scene %s but query failed", kind, sceneID)
	assert.Equal(t, expectedActor, actor)
	if expectedTarget == "" {
		assert.Nil(t, target)
	} else {
		require.NotNil(t, target)
		assert.Equal(t, expectedTarget, *target)
	}
	var p map[string]any
	require.NoError(t, json.Unmarshal(payload, &p))
	return p
}

// countOpsEvents returns the number of scene_ops_events rows for a scene,
// optionally filtered by kind. Pass an empty string for kind to count all.
func countOpsEvents(t *testing.T, store *SceneStore, sceneID string, kind OpsEventKind) int {
	t.Helper()
	var n int
	var err error
	if kind == "" {
		err = store.pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM scene_ops_events WHERE scene_id = $1`,
			sceneID,
		).Scan(&n)
	} else {
		err = store.pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM scene_ops_events WHERE scene_id = $1 AND kind = $2`,
			sceneID, string(kind),
		).Scan(&n)
	}
	require.NoError(t, err)
	return n
}
```

- [ ] **Step 2: Add the new imports**

The helpers use `encoding/json` and `pgx` (for `pgx.ErrNoRows`). Edit the import block at the top of `store_integration_test.go`:

```go
import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/test/testutil"
)
```

- [ ] **Step 3: Verify the integration test file compiles**

Run: `go build -tags=integration ./plugins/core-scenes/...`
Expected: success. The helpers compile but are not yet used.

- [ ] **Step 4: Verify lint passes**

Run: `task lint`
Expected: success.

- [ ] **Step 5: Commit**

```bash
jj --no-pager commit -m "test(scenes): add Phase 3 integration test helpers

Adds mustCreateScene, assertParticipantRowExists/Absent,
assertOpsEventRecorded, and countOpsEvents helpers used by every
subsequent Phase 3 store integration test.

Refs: holomush-5rh.12"
```

---

## Task 4: `recordOpsEventTx` integration test

**Files:**

- Modify: `plugins/core-scenes/store_integration_test.go` (add tests for the helper)

- [ ] **Step 1: Write the test for valid kind**

Append to `store_integration_test.go`:

```go
func TestRecordOpsEventTxWritesRowWithExpectedKindAndPayload(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	scene := mustCreateScene(t, store, "scene-ope-1", "char-alice", string(SceneVisibilityOpen))

	tx, err := store.pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	err = recordOpsEventTx(ctx, tx, scene.ID, OpsKindMembershipJoin, "char-alice", "char-alice",
		map[string]any{"visibility": "open", "from_invited": false})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	payload := assertOpsEventRecorded(t, store, scene.ID, OpsKindMembershipJoin, "char-alice", "char-alice")
	assert.Equal(t, "open", payload["visibility"])
	assert.Equal(t, false, payload["from_invited"])
}
```

- [ ] **Step 2: Run the test**

Run: `task test:int -- -run TestRecordOpsEventTxWritesRowWithExpectedKindAndPayload ./plugins/core-scenes/`
Expected: PASS. The implementation already exists from Task 2; this confirms it works against a real database.

- [ ] **Step 3: Write the test for unknown kind rejection**

Append:

```go
func TestRecordOpsEventTxRejectsUnknownKind(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	mustCreateScene(t, store, "scene-ope-2", "char-alice", string(SceneVisibilityOpen))

	tx, err := store.pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	err = recordOpsEventTx(ctx, tx, "scene-ope-2", OpsEventKind("bogus.kind"), "char-alice", "", nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_OPS_EVENT_INVALID_KIND")
}
```

- [ ] **Step 4: Run the test**

Run: `task test:int -- -run TestRecordOpsEventTxRejectsUnknownKind ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 5: Write the test for nil payload defaulting to {}**

Append:

```go
func TestRecordOpsEventTxAcceptsNilPayloadAsEmptyObject(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	mustCreateScene(t, store, "scene-ope-3", "char-alice", string(SceneVisibilityOpen))

	tx, err := store.pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	err = recordOpsEventTx(ctx, tx, "scene-ope-3", OpsKindLifecyclePaused, "char-alice", "", nil)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	payload := assertOpsEventRecorded(t, store, "scene-ope-3", OpsKindLifecyclePaused, "char-alice", "")
	assert.Empty(t, payload)
}
```

- [ ] **Step 6: Run all three tests**

Run: `task test:int -- -run TestRecordOpsEventTx ./plugins/core-scenes/`
Expected: 3 tests pass.

- [ ] **Step 7: Commit**

```bash
jj --no-pager commit -m "test(scenes): integration tests for recordOpsEventTx helper

Locks in: valid kind insertion + payload roundtrip, unknown kind
rejection, nil payload defaulting to empty JSONB object.

Refs: holomush-5rh.12"
```

---

## Task 5: `CreateWithOwner` store method + retrofit `CreateScene` handler

**Files:**

- Modify: `plugins/core-scenes/store.go` (add `CreateWithOwner`, leave `Create` in place for now — Task 14 will delete it after retrofits land)
- Modify: `plugins/core-scenes/service.go` (`CreateScene` calls `CreateWithOwner`; `sceneStorer` interface gains the method)
- Modify: `plugins/core-scenes/service_test.go` (`fakeStore` gains the method)
- Modify: `plugins/core-scenes/store_integration_test.go` (new tests + new helper)

- [ ] **Step 1: Write the failing integration test for CreateWithOwner**

Append to `store_integration_test.go`:

```go
func TestCreateWithOwnerInsertsSceneAndOwnerParticipantAndOpsEvent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	row := &SceneRow{
		ID:              "scene-cwo-1",
		Title:           "Owned scene",
		OwnerID:         "char-alice",
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityPrivate),
		ContentWarnings: []string{},
		Tags:            []string{},
	}

	err := store.CreateWithOwner(ctx, row)
	require.NoError(t, err)

	// 1. Scene row exists
	got, err := store.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, row.OwnerID, got.OwnerID)

	// 2. Owner participant row exists with role='owner'
	assertParticipantRowExists(t, store, row.ID, row.OwnerID, "owner")

	// 3. lifecycle.created ops event recorded
	payload := assertOpsEventRecorded(t, store, row.ID, OpsKindLifecycleCreated, row.OwnerID, "")
	assert.Equal(t, "private", payload["visibility"])
	assert.Equal(t, false, payload["from_template"])
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test:int -- -run TestCreateWithOwnerInsertsSceneAndOwnerParticipantAndOpsEvent ./plugins/core-scenes/`
Expected: FAIL with `store.CreateWithOwner undefined`.

- [ ] **Step 3: Implement `CreateWithOwner` in store.go**

Add to `plugins/core-scenes/store.go` (immediately after the existing `Create` method):

```go
// CreateWithOwner is the transactional Phase 3 replacement for Create.
//
// All-or-nothing: inserts the scene row, the owner's participant row
// (role='owner'), and a lifecycle.created ops event in a single transaction.
// If any step fails, none of the rows persist.
//
// Per design decision P3.D6, this exists because Phase 3's ABAC policies
// use `principal.id in resource.scene.participants`. An owner without a
// participant row would lose access to their own scene under the new
// policies. The "create + insert owner row + emit ops event" trio MUST
// be atomic.
func (s *SceneStore) CreateWithOwner(ctx context.Context, row *SceneRow) error {
	ctx, span := startSpan(ctx, "scene.store.create_with_owner",
		attribute.String("scene_id", row.ID),
	)
	defer span.End()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return oops.Code("SCENE_CREATE_FAILED").With("scene_id", row.ID).Wrap(err)
	}
	defer tx.Rollback(ctx) // no-op after Commit

	// 1. Insert the scene row.
	_, err = tx.Exec(ctx, `
		INSERT INTO scenes (
			id, title, description, location_id, owner_id, state, pose_order,
			visibility, idle_timeout_secs, template_id, content_warnings, tags
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12
		)`,
		row.ID, row.Title, row.Description, row.LocationID, row.OwnerID,
		row.State, row.PoseOrder, row.Visibility, row.IdleTimeoutSecs,
		row.TemplateID, row.ContentWarnings, row.Tags,
	)
	if err != nil {
		recordError(span, err)
		return oops.Code("SCENE_CREATE_FAILED").With("scene_id", row.ID).Wrap(err)
	}

	// 2. Insert the owner participant row.
	_, err = tx.Exec(ctx, `
		INSERT INTO scene_participants (scene_id, character_id, role, joined_at)
		VALUES ($1, $2, 'owner', NOW())`,
		row.ID, row.OwnerID,
	)
	if err != nil {
		recordError(span, err)
		return oops.Code("SCENE_CREATE_OWNER_PARTICIPANT_FAILED").
			With("scene_id", row.ID).
			With("owner_id", row.OwnerID).
			Wrap(err)
	}

	// 3. Record the lifecycle.created ops event.
	payload := map[string]any{
		"visibility":    row.Visibility,
		"from_template": row.TemplateID != nil,
	}
	if err := recordOpsEventTx(ctx, tx, row.ID, OpsKindLifecycleCreated, row.OwnerID, "", payload); err != nil {
		recordError(span, err)
		return oops.Code("SCENE_CREATE_OPS_EVENT_FAILED").
			With("scene_id", row.ID).
			Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return oops.Code("SCENE_CREATE_FAILED").With("scene_id", row.ID).Wrap(err)
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `task test:int -- -run TestCreateWithOwnerInsertsSceneAndOwnerParticipantAndOpsEvent ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 5: Write the failing test for transactional rollback**

Append to `store_integration_test.go`:

```go
func TestCreateWithOwnerRollsBackWhenSceneIDIsDuplicate(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	row := &SceneRow{
		ID:              "scene-cwo-dup",
		Title:           "First",
		OwnerID:         "char-alice",
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	// Second insert with same ID — must fail and leave scene_participants /
	// scene_ops_events untouched (no orphan rows from a partial transaction).
	rowDup := *row
	rowDup.OwnerID = "char-bob" // different owner attempt
	err := store.CreateWithOwner(ctx, &rowDup)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_CREATE_FAILED")

	// char-bob must NOT have a participant row for this scene.
	assertParticipantRowAbsent(t, store, row.ID, "char-bob")

	// Exactly one lifecycle.created event for this scene (the first call).
	assert.Equal(t, 1, countOpsEvents(t, store, row.ID, OpsKindLifecycleCreated))
}
```

- [ ] **Step 6: Run the test**

Run: `task test:int -- -run TestCreateWithOwnerRollsBackWhenSceneIDIsDuplicate ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 7: Add `CreateWithOwner` to the `sceneStorer` interface**

Edit `plugins/core-scenes/service.go`. Find the `sceneStorer` interface and add the method:

```go
type sceneStorer interface {
	Create(ctx context.Context, row *SceneRow) error
	CreateWithOwner(ctx context.Context, row *SceneRow) error
	Get(ctx context.Context, id string) (*SceneRow, error)
	End(ctx context.Context, id string) (*SceneRow, error)
	Pause(ctx context.Context, id string) (*SceneRow, error)
	Resume(ctx context.Context, id string) (*SceneRow, error)
	Update(ctx context.Context, id string, update *SceneUpdate) (*SceneRow, error)
}
```

- [ ] **Step 8: Add `CreateWithOwner` to fakeStore**

Edit `plugins/core-scenes/service_test.go`. Add a `createWithOwnerErr` field to `fakeStore` and a method:

```go
type fakeStore struct {
	scenes              map[string]*SceneRow
	createErr           error
	createWithOwnerErr  error
	getErr              error
}

func (f *fakeStore) CreateWithOwner(ctx context.Context, row *SceneRow) error {
	if f.createWithOwnerErr != nil {
		return f.createWithOwnerErr
	}
	// fakeStore is in-memory and has no participants/ops_events tables to
	// populate. The service-layer test only cares that the call succeeded.
	return f.Create(ctx, row)
}
```

- [ ] **Step 9: Retrofit `CreateScene` handler to call `CreateWithOwner`**

Edit `plugins/core-scenes/service.go`. In `CreateScene`, change `s.store.Create(ctx, row)` to `s.store.CreateWithOwner(ctx, row)`:

```go
	if err := s.store.CreateWithOwner(ctx, row); err != nil {
		recordError(span, err)
		slog.WarnContext(ctx, "scene.service.create_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", id,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to create scene: %v", err)
	}
```

- [ ] **Step 10: Run the existing service test suite to confirm no regressions**

Run: `task test -- ./plugins/core-scenes/`
Expected: all existing service tests pass. The handler now calls `CreateWithOwner` (which the fakeStore aliases to `Create`).

- [ ] **Step 11: Run integration tests**

Run: `task test:int -- -run TestCreateWithOwner ./plugins/core-scenes/`
Expected: 2 tests pass.

- [ ] **Step 12: Lint**

Run: `task lint`
Expected: success.

- [ ] **Step 13: Commit**

```bash
jj --no-pager commit -m "feat(scenes): add CreateWithOwner transactional store method

Phase 1's Create remains for backward compat with existing tests; the
service handler is retrofitted to call CreateWithOwner so the owner
participant row and lifecycle.created ops event land atomically with
the scene row.

Closes a latent gap that Phase 3's member-based ABAC policies would
otherwise expose: an owner without a participant row would lose read
access to their own scene under the new policies.

Refs: holomush-5rh.12"
```

---

## Task 6: `GetWithMembership` store method

**Files:**

- Modify: `plugins/core-scenes/store.go`
- Modify: `plugins/core-scenes/store_integration_test.go`

- [ ] **Step 1: Write the failing test**

Append to `store_integration_test.go`:

```go
func TestGetWithMembershipReturnsParticipantsAndInviteesLists(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Create a scene with the owner.
	row := &SceneRow{
		ID: "scene-gwm-1", Title: "T", OwnerID: "char-alice",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityPrivate),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	// Manually insert a member and an invitee row to test the partition.
	_, err := store.pool.Exec(ctx,
		`INSERT INTO scene_participants (scene_id, character_id, role) VALUES ($1, 'char-bob', 'member')`,
		row.ID)
	require.NoError(t, err)
	_, err = store.pool.Exec(ctx,
		`INSERT INTO scene_participants (scene_id, character_id, role) VALUES ($1, 'char-carol', 'invited')`,
		row.ID)
	require.NoError(t, err)

	got, participants, invitees, err := store.GetWithMembership(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, "char-alice", got.OwnerID)
	assert.ElementsMatch(t, []string{"char-alice", "char-bob"}, participants)
	assert.ElementsMatch(t, []string{"char-carol"}, invitees)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test:int -- -run TestGetWithMembershipReturnsParticipantsAndInviteesLists ./plugins/core-scenes/`
Expected: FAIL with `store.GetWithMembership undefined`.

- [ ] **Step 3: Implement `GetWithMembership` in store.go**

Add to `plugins/core-scenes/store.go` (after the `Get` method):

```go
// GetWithMembership returns the scene row plus its participants and invitees
// lists in a single SQL round trip. Used by the resolver to materialise ABAC
// attributes without two separate queries.
//
// participants contains all character IDs where role IN ('owner', 'member').
// invitees contains all character IDs where role = 'invited'.
//
// Per design decision P3.D9, this uses two array_agg subselects on the
// indexed scene_participants(scene_id, role) index. No caching layer in
// Phase 3.
func (s *SceneStore) GetWithMembership(ctx context.Context, id string) (*SceneRow, []string, []string, error) {
	ctx, span := startSpan(ctx, "scene.store.get_with_membership",
		attribute.String("scene_id", id),
	)
	defer span.End()

	row := &SceneRow{}
	var participants, invitees []string
	err := s.pool.QueryRow(ctx, `
		SELECT
			s.id, s.title, s.description, s.location_id, s.owner_id,
			s.state, s.pose_order, s.visibility, s.idle_timeout_secs,
			s.template_id, s.content_warnings, s.tags,
			s.created_at, s.ended_at, s.archived_at,
			COALESCE(
				(SELECT array_agg(character_id) FROM scene_participants
				 WHERE scene_id = s.id AND role IN ('owner', 'member')),
				'{}'::TEXT[]
			) AS participants,
			COALESCE(
				(SELECT array_agg(character_id) FROM scene_participants
				 WHERE scene_id = s.id AND role = 'invited'),
				'{}'::TEXT[]
			) AS invitees
		FROM scenes s
		WHERE s.id = $1`,
		id,
	).Scan(
		&row.ID, &row.Title, &row.Description, &row.LocationID, &row.OwnerID,
		&row.State, &row.PoseOrder, &row.Visibility, &row.IdleTimeoutSecs,
		&row.TemplateID, &row.ContentWarnings, &row.Tags,
		&row.CreatedAt, &row.EndedAt, &row.ArchivedAt,
		&participants, &invitees,
	)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Wrap(err)
		}
		return nil, nil, nil, oops.Code("SCENE_GET_FAILED").With("scene_id", id).Wrap(err)
	}
	return row, participants, invitees, nil
}
```

- [ ] **Step 4: Run the test**

Run: `task test:int -- -run TestGetWithMembershipReturnsParticipantsAndInviteesLists ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 5: Write the failing test for empty membership lists**

Append:

```go
func TestGetWithMembershipReturnsEmptyListsWhenSceneHasNoParticipants(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Use Phase 1 Create to skip the auto-owner-row insertion of CreateWithOwner.
	mustCreateScene(t, store, "scene-gwm-empty", "char-alice", string(SceneVisibilityOpen))

	row, participants, invitees, err := store.GetWithMembership(ctx, "scene-gwm-empty")
	require.NoError(t, err)
	assert.Equal(t, "char-alice", row.OwnerID)
	assert.Empty(t, participants)
	assert.Empty(t, invitees)
}
```

- [ ] **Step 6: Run the test**

Run: `task test:int -- -run TestGetWithMembershipReturnsEmptyListsWhenSceneHasNoParticipants ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 7: Write the failing test for not-found**

Append:

```go
func TestGetWithMembershipReturnsNotFoundForMissingScene(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, _, _, err := store.GetWithMembership(ctx, "scene-gwm-missing")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
}
```

- [ ] **Step 8: Run all GetWithMembership tests**

Run: `task test:int -- -run TestGetWithMembership ./plugins/core-scenes/`
Expected: 3 tests pass.

- [ ] **Step 9: Add to sceneStorer interface and fakeStore**

Edit `plugins/core-scenes/service.go`, add to the interface:

```go
type sceneStorer interface {
	Create(ctx context.Context, row *SceneRow) error
	CreateWithOwner(ctx context.Context, row *SceneRow) error
	Get(ctx context.Context, id string) (*SceneRow, error)
	GetWithMembership(ctx context.Context, id string) (*SceneRow, []string, []string, error)
	End(ctx context.Context, id string) (*SceneRow, error)
	Pause(ctx context.Context, id string) (*SceneRow, error)
	Resume(ctx context.Context, id string) (*SceneRow, error)
	Update(ctx context.Context, id string, update *SceneUpdate) (*SceneRow, error)
}
```

Edit `plugins/core-scenes/service_test.go`, add to fakeStore:

```go
func (f *fakeStore) GetWithMembership(ctx context.Context, id string) (*SceneRow, []string, []string, error) {
	row, err := f.Get(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	// fakeStore has no participants table — return owner-only participants
	// and empty invitees, matching the post-CreateWithOwner reality.
	return row, []string{row.OwnerID}, nil, nil
}
```

- [ ] **Step 10: Run unit tests to confirm fakeStore compiles and works**

Run: `task test -- ./plugins/core-scenes/`
Expected: existing tests pass.

- [ ] **Step 11: Commit**

```bash
jj --no-pager commit -m "feat(scenes): add GetWithMembership store method

Single-query materialisation of scene row + participants list + invitees
list using array_agg subselects on the indexed (scene_id, role) index.
Used by the resolver in Task 7 to expose ABAC attributes without two
round trips.

Refs: holomush-5rh.12"
```

---

## Task 7: Resolver extension — `participants` and `invitees` attributes

**Files:**

- Modify: `plugins/core-scenes/resolver.go`
- Modify: `plugins/core-scenes/resolver_test.go`

- [ ] **Step 1: Write the failing test for the schema extension**

Edit `plugins/core-scenes/resolver_test.go` and append:

```go
func TestGetSchemaIncludesParticipantsAndInviteesAttributes(t *testing.T) {
	resolver := NewSceneResolver(newFakeStore())

	resp, err := resolver.GetSchema(context.Background(), &pluginv1.GetSchemaRequest{})
	require.NoError(t, err)

	sceneSchema, ok := resp.GetResourceTypes()["scene"]
	require.True(t, ok, "scene resource type missing from schema")

	attrs := sceneSchema.GetAttributes()
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST, attrs["participants"])
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST, attrs["invitees"])
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test -- -run TestGetSchemaIncludesParticipantsAndInviteesAttributes ./plugins/core-scenes/`
Expected: FAIL — current schema only has the 5 Phase 1 attributes.

- [ ] **Step 3: Update `GetSchema` in resolver.go**

Edit `plugins/core-scenes/resolver.go`. In `GetSchema`, add the two new attributes:

```go
return &pluginv1.GetSchemaResponse{
    ResourceTypes: map[string]*pluginv1.ResourceTypeSchema{
        resourceTypeScene: {
            Attributes: map[string]pluginv1.AttributeType{
                "id":           pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
                "owner":        pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
                "state":        pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
                "visibility":   pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
                "location":     pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
                "participants": pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST,
                "invitees":     pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST,
            },
        },
    },
}, nil
```

- [ ] **Step 4: Run the test**

Run: `task test -- -run TestGetSchemaIncludesParticipantsAndInviteesAttributes ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 5: Write the failing test for ResolveResource returning lists**

Append to `resolver_test.go`:

```go
func TestResolveResourceReturnsParticipantsAndInviteesLists(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-r-1"] = &SceneRow{
		ID:         "scene-r-1",
		Title:      "T",
		OwnerID:    "char-alice",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityPrivate),
	}
	resolver := NewSceneResolver(store)

	resp, err := resolver.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: "scene",
		ResourceId:   "scene-r-1",
	})
	require.NoError(t, err)

	participantsAttr := resp.GetAttributes()["participants"]
	require.NotNil(t, participantsAttr)
	require.NotNil(t, participantsAttr.GetStringListValue())
	assert.ElementsMatch(t, []string{"char-alice"}, participantsAttr.GetStringListValue().GetValues())

	inviteesAttr := resp.GetAttributes()["invitees"]
	require.NotNil(t, inviteesAttr)
	require.NotNil(t, inviteesAttr.GetStringListValue())
	assert.Empty(t, inviteesAttr.GetStringListValue().GetValues())
}
```

- [ ] **Step 6: Run the test to verify it fails**

Run: `task test -- -run TestResolveResourceReturnsParticipantsAndInviteesLists ./plugins/core-scenes/`
Expected: FAIL — current ResolveResource calls `r.store.Get` (no membership lists).

- [ ] **Step 7: Update `ResolveResource` to call `GetWithMembership`**

Edit `plugins/core-scenes/resolver.go`. Replace the existing `ResolveResource` body:

```go
func (r *SceneResolver) ResolveResource(ctx context.Context, req *pluginv1.ResolveResourceRequest) (*pluginv1.ResolveResourceResponse, error) {
	ctx, span := startSpan(ctx, "scene.resolver.resolve_resource",
		attribute.String("resource_type", req.GetResourceType()),
		attribute.String("resource_id", req.GetResourceId()),
	)
	defer span.End()

	if req.GetResourceType() != resourceTypeScene {
		err := status.Errorf(codes.InvalidArgument,
			"core-scenes only resolves resource type %q, got %q",
			resourceTypeScene, req.GetResourceType())
		recordError(span, err)
		return nil, err
	}

	row, participants, invitees, err := r.store.GetWithMembership(ctx, req.GetResourceId())
	if err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "SCENE_NOT_FOUND" {
			return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetResourceId())
		}
		return nil, status.Errorf(codes.Internal, "failed to resolve scene: %v", err)
	}

	location := ""
	if row.LocationID != nil {
		location = *row.LocationID
	}

	return &pluginv1.ResolveResourceResponse{
		Attributes: map[string]*pluginv1.AttributeValue{
			"id":         {Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.ID}},
			"owner":      {Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.OwnerID}},
			"state":      {Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.State}},
			"visibility": {Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.Visibility}},
			"location":   {Kind: &pluginv1.AttributeValue_StringValue{StringValue: location}},
			"participants": {Kind: &pluginv1.AttributeValue_StringListValue{
				StringListValue: &pluginv1.StringList{Values: participants},
			}},
			"invitees": {Kind: &pluginv1.AttributeValue_StringListValue{
				StringListValue: &pluginv1.StringList{Values: invitees},
			}},
		},
	}, nil
}
```

The `sceneStorer` interface inside `resolver.go` is the same one defined in `service.go` — Task 6 already added `GetWithMembership` to it.

- [ ] **Step 8: Run all resolver tests**

Run: `task test -- -run TestResolve ./plugins/core-scenes/ && task test -- -run TestGetSchema ./plugins/core-scenes/`
Expected: all pass, including the existing Phase 1+2 tests.

- [ ] **Step 9: Commit**

```bash
jj --no-pager commit -m "feat(scenes): expose participants and invitees in resolver

GetSchema declares two new STRING_LIST attributes; ResolveResource calls
GetWithMembership for a single-round-trip materialisation. Phase 3 ABAC
policies will use these for member-based read/write/resume rules.

Refs: holomush-5rh.12"
```

---

## Task 8: `JoinScene` — `AddParticipant` + `classifyJoinMiss` + service handler

**Files:**

- Modify: `plugins/core-scenes/store.go`
- Modify: `plugins/core-scenes/service.go`
- Modify: `plugins/core-scenes/store_integration_test.go`
- Modify: `plugins/core-scenes/service_test.go`

- [ ] **Step 1: Write the failing integration test for fresh open join**

Append to `store_integration_test.go`:

```go
func TestAddParticipantInsertsFreshMemberRowForOpenScene(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	row := &SceneRow{
		ID: "scene-ap-1", Title: "T", OwnerID: "char-alice",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	got, result, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)
	assert.Equal(t, OpInserted, result)
	assert.Equal(t, "char-bob", got.CharacterID)
	assert.Equal(t, "member", got.Role)
	assertParticipantRowExists(t, store, row.ID, "char-bob", "member")
	assertOpsEventRecorded(t, store, row.ID, OpsKindMembershipJoin, "char-bob", "char-bob")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test:int -- -run TestAddParticipantInsertsFreshMemberRowForOpenScene ./plugins/core-scenes/`
Expected: FAIL with `store.AddParticipant undefined`.

- [ ] **Step 3: Implement `AddParticipant` and `classifyJoinMiss` in store.go**

Add to `plugins/core-scenes/store.go`:

```go
// AddParticipant attempts to add characterID to sceneID. The operation is
// idempotent on identity match (calling it twice for the same character is
// a no-op) and atomically promotes invited→member.
//
// Returns:
//   - OpInserted: a fresh member row was created
//   - OpPromoted: an existing invited row was flipped to member
//   - OpNoChange: the caller was already a member or owner
//
// The single SELECT-WHERE-guarded UPSERT enforces all join eligibility
// checks at the SQL layer:
//   - Scene must exist
//   - Scene must be in active or paused state
//   - Either the scene is open OR there's an invited row for this character
//
// If the eligibility check fails, RETURNING is empty and we issue a
// diagnostic SELECT (classifyJoinMiss) to figure out the precise reason.
func (s *SceneStore) AddParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, ParticipantOpResult, error) {
	ctx, span := startSpan(ctx, "scene.store.add_participant",
		attribute.String("scene_id", sceneID),
		attribute.String("character_id", characterID),
	)
	defer span.End()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return nil, OpNoChange, oops.Code("SCENE_JOIN_FAILED").
			With("scene_id", sceneID).With("character_id", characterID).Wrap(err)
	}
	defer tx.Rollback(ctx)

	row := &ParticipantRow{}
	var wasInserted bool
	err = tx.QueryRow(ctx, `
		INSERT INTO scene_participants (scene_id, character_id, role, joined_at)
		SELECT $1, $2, 'member', NOW()
		FROM scenes
		WHERE id = $1
		  AND state IN ('active', 'paused')
		  AND (
		    visibility = 'open'
		    OR EXISTS (
		      SELECT 1 FROM scene_participants
		      WHERE scene_id = $1 AND character_id = $2 AND role = 'invited'
		    )
		  )
		ON CONFLICT (scene_id, character_id) DO UPDATE
		  SET role = CASE WHEN scene_participants.role = 'invited' THEN 'member' ELSE scene_participants.role END,
		      joined_at = CASE WHEN scene_participants.role = 'invited' THEN NOW() ELSE scene_participants.joined_at END
		RETURNING scene_id, character_id, role, joined_at, (xmax = 0) AS was_inserted`,
		sceneID, characterID,
	).Scan(&row.SceneID, &row.CharacterID, &row.Role, &row.JoinedAt, &wasInserted)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Eligibility check failed. Classify the precise reason.
			return nil, OpNoChange, s.classifyJoinMiss(ctx, sceneID, characterID, span)
		}
		recordError(span, err)
		return nil, OpNoChange, oops.Code("SCENE_JOIN_FAILED").
			With("scene_id", sceneID).With("character_id", characterID).Wrap(err)
	}

	// Determine the result. wasInserted=true → OpInserted. Otherwise either
	// promoted (the existing row was 'invited' and is now 'member') or no
	// change (the existing row was already 'member' or 'owner').
	var result ParticipantOpResult
	if wasInserted {
		result = OpInserted
	} else {
		// We need to figure out if this was a promotion or no-op. The post-
		// update row's role is 'member' in both cases, so we can't tell from
		// the row itself. The trick: if the row was JUST promoted (in this
		// txn), the join succeeded for a reason — it must have been the
		// invited path. If it was no-change, the role was already member or
		// owner before the txn started.
		//
		// We use a marker query within the same transaction: check if there's
		// an audit signal that this character was previously invited. The
		// cleanest way is to look at the joined_at — for OpPromoted, the
		// CASE branch updated joined_at to NOW(). We can compare against
		// the txn start time. But pgx doesn't give us txn start cheaply.
		//
		// Simpler: do a pre-check before the upsert. We're already in a txn,
		// so the SELECT is consistent.
		//
		// However, pre-check defeats the purpose of the single-statement
		// race-safe upsert. Better: use the joined_at-vs-now heuristic.
		// joined_at = NOW() means the CASE branch fired (promotion).
		// Since this query just executed, joined_at within the last second
		// indicates promotion.
		//
		// Even simpler and exact: query the row's joined_at and compare to
		// the time we started — but again we'd need that timestamp.
		//
		// The cleanest approach is to issue ONE additional query: SELECT
		// joined_at from the row we just touched and compare to the
		// current statement_timestamp(). If they're equal (within the same
		// transaction), it was a promotion. Otherwise, no-change.
		var promoted bool
		err = tx.QueryRow(ctx, `
			SELECT joined_at >= statement_timestamp() - interval '1 second'
			FROM scene_participants
			WHERE scene_id = $1 AND character_id = $2`,
			sceneID, characterID,
		).Scan(&promoted)
		if err != nil {
			recordError(span, err)
			return nil, OpNoChange, oops.Code("SCENE_JOIN_CLASSIFY_FAILED").
				With("scene_id", sceneID).With("character_id", characterID).Wrap(err)
		}
		if promoted {
			result = OpPromoted
		} else {
			result = OpNoChange
		}
	}

	// Emit ops event ONLY for OpInserted and OpPromoted; OpNoChange must
	// not pollute the audit log with retry events.
	if result != OpNoChange {
		// Determine visibility for the payload by reading the scene row.
		var visibility string
		err = tx.QueryRow(ctx, `SELECT visibility FROM scenes WHERE id = $1`, sceneID).Scan(&visibility)
		if err != nil {
			recordError(span, err)
			return nil, OpNoChange, oops.Code("SCENE_JOIN_OPS_EVENT_FAILED").Wrap(err)
		}
		payload := map[string]any{
			"visibility":   visibility,
			"from_invited": result == OpPromoted,
		}
		if err := recordOpsEventTx(ctx, tx, sceneID, OpsKindMembershipJoin, characterID, characterID, payload); err != nil {
			recordError(span, err)
			return nil, OpNoChange, oops.Code("SCENE_JOIN_OPS_EVENT_FAILED").Wrap(err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return nil, OpNoChange, oops.Code("SCENE_JOIN_FAILED").Wrap(err)
	}
	return row, result, nil
}

// classifyJoinMiss issues one diagnostic SELECT to figure out which
// precondition failed when AddParticipant's RETURNING was empty. Pays the
// extra round trip ONLY in the error path; the happy path is single-statement.
//
// Returns one of:
//   - SCENE_NOT_FOUND
//   - SCENE_TRANSITION_FORBIDDEN (with current_state in context)
//   - SCENE_JOIN_NOT_INVITED (private scene, no invitation)
func (s *SceneStore) classifyJoinMiss(ctx context.Context, sceneID, characterID string, span trace.Span) error {
	var (
		state      string
		visibility string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT state, visibility FROM scenes WHERE id = $1`,
		sceneID,
	).Scan(&state, &visibility)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.Code("SCENE_NOT_FOUND").
				With("scene_id", sceneID).
				With("op", "join").
				Wrap(err)
		}
		return oops.Code("SCENE_JOIN_CLASSIFY_FAILED").
			With("scene_id", sceneID).
			With("op", "join").
			Wrap(err)
	}

	if state != "active" && state != "paused" {
		return oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", sceneID).
			With("op", "join").
			With("current_state", state).
			Errorf("scene in state %q cannot be joined", state)
	}

	// State is OK. The remaining reason is private scene without invitation.
	return oops.Code("SCENE_JOIN_NOT_INVITED").
		With("scene_id", sceneID).
		With("character_id", characterID).
		With("visibility", visibility).
		Errorf("character not invited to private scene")
}
```

- [ ] **Step 4: Run the first test**

Run: `task test:int -- -run TestAddParticipantInsertsFreshMemberRowForOpenScene ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 5: Write the failing test for invited→member promotion**

Append:

```go
func TestAddParticipantPromotesInvitedRowToMemberOnPrivateScene(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	row := &SceneRow{
		ID: "scene-ap-promote", Title: "T", OwnerID: "char-alice",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityPrivate),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	// Pre-insert an invitation for char-bob.
	_, err := store.pool.Exec(ctx,
		`INSERT INTO scene_participants (scene_id, character_id, role) VALUES ($1, 'char-bob', 'invited')`,
		row.ID)
	require.NoError(t, err)

	got, result, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)
	assert.Equal(t, OpPromoted, result)
	assert.Equal(t, "member", got.Role)
	assertParticipantRowExists(t, store, row.ID, "char-bob", "member")

	payload := assertOpsEventRecorded(t, store, row.ID, OpsKindMembershipJoin, "char-bob", "char-bob")
	assert.Equal(t, "private", payload["visibility"])
	assert.Equal(t, true, payload["from_invited"])
}
```

- [ ] **Step 6: Run the test**

Run: `task test:int -- -run TestAddParticipantPromotesInvitedRowToMemberOnPrivateScene ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 7: Write the failing test for OpNoChange retry**

Append:

```go
func TestAddParticipantReturnsOpNoChangeForExistingMember(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	row := &SceneRow{
		ID: "scene-ap-noop", Title: "T", OwnerID: "char-alice",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	// First join — OpInserted.
	_, result1, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)
	assert.Equal(t, OpInserted, result1)

	// Second join (retry) — OpNoChange, no new ops event.
	_, result2, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)
	assert.Equal(t, OpNoChange, result2)

	// Exactly one membership.join event for this scene.
	assert.Equal(t, 1, countOpsEvents(t, store, row.ID, OpsKindMembershipJoin))
}
```

- [ ] **Step 8: Run the test**

Run: `task test:int -- -run TestAddParticipantReturnsOpNoChangeForExistingMember ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 9: Write tests for the three error paths**

Append:

```go
func TestAddParticipantRejectsPrivateSceneWithoutInvitation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	row := &SceneRow{
		ID: "scene-ap-priv", Title: "T", OwnerID: "char-alice",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityPrivate),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_JOIN_NOT_INVITED")
	errutil.AssertErrorContext(t, err, "scene_id", row.ID)
	errutil.AssertErrorContext(t, err, "character_id", "char-bob")
}

func TestAddParticipantRejectsEndedScene(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	row := &SceneRow{
		ID: "scene-ap-ended", Title: "T", OwnerID: "char-alice",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, err := store.End(ctx, row.ID)
	require.NoError(t, err)

	_, _, err = store.AddParticipant(ctx, row.ID, "char-bob")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_TRANSITION_FORBIDDEN")
	errutil.AssertErrorContext(t, err, "current_state", "ended")
}

func TestAddParticipantReturnsNotFoundForMissingScene(t *testing.T) {
	store := newTestStore(t)
	_, _, err := store.AddParticipant(context.Background(), "scene-nope", "char-bob")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
}
```

- [ ] **Step 10: Run all AddParticipant tests**

Run: `task test:int -- -run TestAddParticipant ./plugins/core-scenes/`
Expected: 6 tests pass.

- [ ] **Step 11: Add `AddParticipant` to sceneStorer interface and fakeStore**

Edit `plugins/core-scenes/service.go`:

```go
type sceneStorer interface {
	Create(ctx context.Context, row *SceneRow) error
	CreateWithOwner(ctx context.Context, row *SceneRow) error
	Get(ctx context.Context, id string) (*SceneRow, error)
	GetWithMembership(ctx context.Context, id string) (*SceneRow, []string, []string, error)
	End(ctx context.Context, id string) (*SceneRow, error)
	Pause(ctx context.Context, id string) (*SceneRow, error)
	Resume(ctx context.Context, id string) (*SceneRow, error)
	Update(ctx context.Context, id string, update *SceneUpdate) (*SceneRow, error)
	AddParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, ParticipantOpResult, error)
}
```

Edit `plugins/core-scenes/service_test.go` — extend fakeStore with a participants map and AddParticipant:

```go
type fakeStore struct {
	scenes              map[string]*SceneRow
	participants        map[string]map[string]string // sceneID → characterID → role
	createErr           error
	createWithOwnerErr  error
	getErr              error
	addParticipantErr   error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		scenes:       make(map[string]*SceneRow),
		participants: make(map[string]map[string]string),
	}
}

func (f *fakeStore) AddParticipant(_ context.Context, sceneID, characterID string) (*ParticipantRow, ParticipantOpResult, error) {
	if f.addParticipantErr != nil {
		return nil, OpNoChange, f.addParticipantErr
	}
	scene, ok := f.scenes[sceneID]
	if !ok {
		return nil, OpNoChange, oops.Code("SCENE_NOT_FOUND").With("scene_id", sceneID).Errorf("not found")
	}
	if scene.State != string(SceneStateActive) && scene.State != string(SceneStatePaused) {
		return nil, OpNoChange, oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", sceneID).With("current_state", scene.State).Errorf("cannot join")
	}
	if f.participants[sceneID] == nil {
		f.participants[sceneID] = make(map[string]string)
	}
	existing, exists := f.participants[sceneID][characterID]
	if exists {
		if existing == "invited" {
			f.participants[sceneID][characterID] = "member"
			return &ParticipantRow{SceneID: sceneID, CharacterID: characterID, Role: "member"}, OpPromoted, nil
		}
		return &ParticipantRow{SceneID: sceneID, CharacterID: characterID, Role: existing}, OpNoChange, nil
	}
	if scene.Visibility == string(SceneVisibilityPrivate) {
		return nil, OpNoChange, oops.Code("SCENE_JOIN_NOT_INVITED").
			With("scene_id", sceneID).With("character_id", characterID).Errorf("not invited")
	}
	f.participants[sceneID][characterID] = "member"
	return &ParticipantRow{SceneID: sceneID, CharacterID: characterID, Role: "member"}, OpInserted, nil
}
```

You also need to update the existing fakeStore methods (`Create`, `CreateWithOwner`) to NOT auto-insert participant rows — the AddParticipant tests need a known starting state. But `CreateWithOwner` SHOULD insert the owner row to match the real store. Update:

```go
func (f *fakeStore) CreateWithOwner(ctx context.Context, row *SceneRow) error {
	if f.createWithOwnerErr != nil {
		return f.createWithOwnerErr
	}
	if err := f.Create(ctx, row); err != nil {
		return err
	}
	if f.participants[row.ID] == nil {
		f.participants[row.ID] = make(map[string]string)
	}
	f.participants[row.ID][row.OwnerID] = "owner"
	return nil
}
```

Also update `GetWithMembership` to read from the new participants map:

```go
func (f *fakeStore) GetWithMembership(ctx context.Context, id string) (*SceneRow, []string, []string, error) {
	row, err := f.Get(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	var participants, invitees []string
	for cid, role := range f.participants[id] {
		switch role {
		case "owner", "member":
			participants = append(participants, cid)
		case "invited":
			invitees = append(invitees, cid)
		}
	}
	return row, participants, invitees, nil
}
```

- [ ] **Step 12: Write the failing service-layer test for JoinScene success**

Append to `service_test.go`:

```go
func TestSceneServiceJoinSceneInsertsMemberAndReturnsSuccess(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-js-1", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	svc := NewSceneServiceImpl(store)

	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		CharacterId: "char-bob",
		SceneId:     "scene-js-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "member", store.participants["scene-js-1"]["char-bob"])
}
```

- [ ] **Step 13: Run the test to verify it fails**

Run: `task test -- -run TestSceneServiceJoinSceneInsertsMemberAndReturnsSuccess ./plugins/core-scenes/`
Expected: FAIL with `svc.JoinScene undefined`.

- [ ] **Step 14: Implement `JoinScene` in service.go**

Add to `plugins/core-scenes/service.go`:

```go
// JoinScene attempts to add the calling character to a scene. The store
// method handles all eligibility checks (open vs private, state, etc.).
//
// Per design decision P3.D5, the operation is idempotent: same-character
// retries return success without polluting the audit log with extra
// membership.join events. The store's ParticipantOpResult enum drives
// the emit-or-not decision inside the store transaction.
func (s *SceneServiceImpl) JoinScene(ctx context.Context, req *scenev1.JoinSceneRequest) (*scenev1.JoinSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.join_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	_, _, err := s.store.AddParticipant(ctx, req.GetSceneId(), req.GetCharacterId())
	if err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) {
			switch oe.Code() {
			case "SCENE_NOT_FOUND":
				return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
			case "SCENE_TRANSITION_FORBIDDEN":
				return nil, status.Errorf(codes.FailedPrecondition,
					"scene cannot be joined in its current state: %v", err)
			case "SCENE_JOIN_NOT_INVITED":
				return nil, status.Errorf(codes.PermissionDenied,
					"character not invited to private scene")
			}
		}
		slog.WarnContext(ctx, "scene.service.join_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to join scene: %v", err)
	}

	slog.InfoContext(ctx, "scene.service.join_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
	)

	return &scenev1.JoinSceneResponse{}, nil
}
```

- [ ] **Step 15: Run the test**

Run: `task test -- -run TestSceneServiceJoinSceneInsertsMemberAndReturnsSuccess ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 16: Write tests for the three error mappings**

Append to `service_test.go`:

```go
func TestSceneServiceJoinSceneMapsNotInvitedToPermissionDenied(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-js-priv", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityPrivate),
	}))
	svc := NewSceneServiceImpl(store)

	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		CharacterId: "char-bob", SceneId: "scene-js-priv",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

func TestSceneServiceJoinSceneMapsNotFoundToNotFound(t *testing.T) {
	svc := NewSceneServiceImpl(newFakeStore())
	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		CharacterId: "char-bob", SceneId: "scene-missing",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServiceJoinSceneMapsTransitionForbiddenToFailedPrecondition(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-js-ended", OwnerID: "char-alice",
		State: string(SceneStateEnded), Visibility: string(SceneVisibilityOpen),
	}))
	svc := NewSceneServiceImpl(store)
	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		CharacterId: "char-bob", SceneId: "scene-js-ended",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}
```

- [ ] **Step 17: Run all JoinScene service tests**

Run: `task test -- -run TestSceneServiceJoinScene ./plugins/core-scenes/`
Expected: 4 tests pass.

- [ ] **Step 18: Run all tests (unit + integration) for the package**

Run: `task test -- ./plugins/core-scenes/ && task test:int -- ./plugins/core-scenes/`
Expected: all pass.

- [ ] **Step 19: Lint**

Run: `task lint`
Expected: success.

- [ ] **Step 20: Commit**

```bash
jj --no-pager commit -m "feat(scenes): JoinScene RPC with idempotent race-safe upsert

Adds AddParticipant store method (single SELECT-WHERE-guarded UPSERT
with role hierarchy CASE), classifyJoinMiss diagnostic helper, and
JoinScene service handler with proper status code mapping
(NotFound/FailedPrecondition/PermissionDenied/Internal).

The (xmax = 0) discriminator + per-row joined_at-vs-statement-timestamp
check produces the OpInserted/OpPromoted/OpNoChange enum that drives
ops event emission — retries return OpNoChange and emit no event,
keeping the audit log clean.

Refs: holomush-5rh.12"
```

---

## Task 9: `LeaveScene` — `RemoveParticipant` + service handler with owner pre-check

**Files:**

- Modify: `plugins/core-scenes/store.go`, `service.go`, `store_integration_test.go`, `service_test.go`

- [ ] **Step 1: Write the failing integration test**

Append to `store_integration_test.go`:

```go
func TestRemoveParticipantDeletesMemberRowAndEmitsOpsEvent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	row := &SceneRow{
		ID: "scene-rp-1", Title: "T", OwnerID: "char-alice",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)

	got, err := store.RemoveParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)
	assert.Equal(t, "member", got.Role)
	assertParticipantRowAbsent(t, store, row.ID, "char-bob")

	payload := assertOpsEventRecorded(t, store, row.ID, OpsKindMembershipLeave, "char-bob", "char-bob")
	assert.Equal(t, "member", payload["prior_role"])
}

func TestRemoveParticipantRefusesToRemoveOwner(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-rp-owner", OwnerID: "char-alice",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen), Title: "T",
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	_, err := store.RemoveParticipant(ctx, row.ID, "char-alice")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_OWNER_CANNOT_LEAVE")
	assertParticipantRowExists(t, store, row.ID, "char-alice", "owner")
}

func TestRemoveParticipantReturnsNotFoundForMissingParticipant(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-rp-missing", OwnerID: "char-alice",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen), Title: "T",
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	_, err := store.RemoveParticipant(ctx, row.ID, "char-ghost")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_PARTICIPANT_NOT_FOUND")
}
```

- [ ] **Step 2: Implement `RemoveParticipant` in store.go**

Add to `plugins/core-scenes/store.go`:

```go
// RemoveParticipant deletes the participant row for characterID in sceneID.
//
// The DELETE has a `WHERE role <> 'owner'` filter for defense-in-depth: the
// service layer rejects owner-leave first, but this prevents accidental
// owner removal if the service-layer check is ever bypassed (direct store
// call, future bug, etc.).
//
// Returns the removed row via RETURNING. Distinguishes "owner cannot leave"
// from "participant not found" via a follow-up SELECT in the error path.
func (s *SceneStore) RemoveParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, error) {
	ctx, span := startSpan(ctx, "scene.store.remove_participant",
		attribute.String("scene_id", sceneID),
		attribute.String("character_id", characterID),
	)
	defer span.End()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_LEAVE_FAILED").
			With("scene_id", sceneID).With("character_id", characterID).Wrap(err)
	}
	defer tx.Rollback(ctx)

	row := &ParticipantRow{}
	err = tx.QueryRow(ctx, `
		DELETE FROM scene_participants
		WHERE scene_id = $1 AND character_id = $2 AND role <> 'owner'
		RETURNING scene_id, character_id, role, joined_at`,
		sceneID, characterID,
	).Scan(&row.SceneID, &row.CharacterID, &row.Role, &row.JoinedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Either the row doesn't exist or it was the owner. Distinguish.
			var existingRole string
			err2 := tx.QueryRow(ctx, `
				SELECT role FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
				sceneID, characterID,
			).Scan(&existingRole)
			if err2 != nil {
				if errors.Is(err2, pgx.ErrNoRows) {
					return nil, oops.Code("SCENE_PARTICIPANT_NOT_FOUND").
						With("scene_id", sceneID).
						With("character_id", characterID).
						Wrap(err)
				}
				recordError(span, err2)
				return nil, oops.Code("SCENE_LEAVE_CLASSIFY_FAILED").
					With("scene_id", sceneID).Wrap(err2)
			}
			if existingRole == "owner" {
				return nil, oops.Code("SCENE_OWNER_CANNOT_LEAVE").
					With("scene_id", sceneID).
					With("character_id", characterID).
					Errorf("scene owners cannot leave; use scene end or transfer ownership")
			}
			// Shouldn't happen — DELETE matched no row but SELECT did?
			return nil, oops.Code("SCENE_LEAVE_FAILED").Errorf("unexpected state")
		}
		recordError(span, err)
		return nil, oops.Code("SCENE_LEAVE_FAILED").Wrap(err)
	}

	payload := map[string]any{"prior_role": row.Role}
	if err := recordOpsEventTx(ctx, tx, sceneID, OpsKindMembershipLeave, characterID, characterID, payload); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_LEAVE_OPS_EVENT_FAILED").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_LEAVE_FAILED").Wrap(err)
	}
	return row, nil
}
```

- [ ] **Step 3: Run the tests**

Run: `task test:int -- -run TestRemoveParticipant ./plugins/core-scenes/`
Expected: 3 tests pass.

- [ ] **Step 4: Add to sceneStorer interface and fakeStore**

Edit `service.go` interface — add `RemoveParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, error)`.

Edit `service_test.go` fakeStore:

```go
func (f *fakeStore) RemoveParticipant(_ context.Context, sceneID, characterID string) (*ParticipantRow, error) {
	role, exists := f.participants[sceneID][characterID]
	if !exists {
		return nil, oops.Code("SCENE_PARTICIPANT_NOT_FOUND").
			With("scene_id", sceneID).With("character_id", characterID).Errorf("not found")
	}
	if role == "owner" {
		return nil, oops.Code("SCENE_OWNER_CANNOT_LEAVE").
			With("scene_id", sceneID).With("character_id", characterID).Errorf("owners cannot leave")
	}
	delete(f.participants[sceneID], characterID)
	return &ParticipantRow{SceneID: sceneID, CharacterID: characterID, Role: role}, nil
}
```

- [ ] **Step 5: Write the failing service-layer test for owner-leave rejection**

Append to `service_test.go`:

```go
func TestSceneServiceLeaveSceneRejectsOwnerWithFailedPrecondition(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-ls-owner", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	svc := NewSceneServiceImpl(store)

	_, err := svc.LeaveScene(context.Background(), &scenev1.LeaveSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-ls-owner",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), "owners cannot leave")
}

func TestSceneServiceLeaveSceneRemovesMember(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-ls-1", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	_, _, err := store.AddParticipant(context.Background(), "scene-ls-1", "char-bob")
	require.NoError(t, err)
	svc := NewSceneServiceImpl(store)

	_, err = svc.LeaveScene(context.Background(), &scenev1.LeaveSceneRequest{
		CharacterId: "char-bob",
		SceneId:     "scene-ls-1",
	})
	require.NoError(t, err)
	_, exists := store.participants["scene-ls-1"]["char-bob"]
	assert.False(t, exists)
}
```

- [ ] **Step 6: Implement `LeaveScene` in service.go**

Add to `plugins/core-scenes/service.go`:

```go
// LeaveScene removes the calling character from a scene. Per design decision
// P3.D7, scene owners cannot leave their own scene — they must use scene end
// or transfer ownership first. The service-layer pre-check returns
// FailedPrecondition with an actionable hint message.
//
// The store's RemoveParticipant ALSO has a `WHERE role <> 'owner'` filter
// for defense-in-depth.
func (s *SceneServiceImpl) LeaveScene(ctx context.Context, req *scenev1.LeaveSceneRequest) (*scenev1.LeaveSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.leave_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	// Service-layer owner-leave pre-check. Reads the scene first so we can
	// give the user a helpful message before hitting the store's defensive
	// WHERE filter (which would return SCENE_OWNER_CANNOT_LEAVE — same
	// outcome but the error path is uglier).
	sceneRow, err := s.store.Get(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "SCENE_NOT_FOUND" {
			return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
		}
		return nil, status.Errorf(codes.Internal, "failed to load scene: %v", err)
	}
	if sceneRow.OwnerID == req.GetCharacterId() {
		err := status.Errorf(codes.FailedPrecondition,
			"scene owners cannot leave; use `scene end` to terminate the scene or transfer ownership first")
		recordError(span, err)
		return nil, err
	}

	if _, err := s.store.RemoveParticipant(ctx, req.GetSceneId(), req.GetCharacterId()); err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) {
			switch oe.Code() {
			case "SCENE_PARTICIPANT_NOT_FOUND":
				return nil, status.Errorf(codes.NotFound, "character not in scene")
			case "SCENE_OWNER_CANNOT_LEAVE":
				// Defense-in-depth path — should never trigger after the
				// service-layer pre-check above, but mapped for completeness.
				return nil, status.Errorf(codes.FailedPrecondition,
					"scene owners cannot leave")
			}
		}
		slog.WarnContext(ctx, "scene.service.leave_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to leave scene: %v", err)
	}

	slog.InfoContext(ctx, "scene.service.leave_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
	)

	return &scenev1.LeaveSceneResponse{}, nil
}
```

- [ ] **Step 7: Run all tests**

Run: `task test -- -run TestSceneServiceLeaveScene ./plugins/core-scenes/ && task test:int -- -run TestRemoveParticipant ./plugins/core-scenes/`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
jj --no-pager commit -m "feat(scenes): LeaveScene RPC with owner-cannot-leave guard

Adds RemoveParticipant store method with WHERE role <> 'owner' defense
filter and the LeaveScene service handler with a service-layer pre-check
that returns FailedPrecondition with an actionable hint message
('use scene end or transfer ownership first').

Refs: holomush-5rh.12"
```

---

## Task 10: `InviteToScene` — `InviteParticipant` + service handler

**Files:**

- Modify: `plugins/core-scenes/store.go`, `service.go`, `store_integration_test.go`, `service_test.go`

- [ ] **Step 1: Write failing integration tests**

Append to `store_integration_test.go`:

```go
func TestInviteParticipantInsertsInvitedRowAndEmitsOpsEvent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-inv-1", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityPrivate),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	got, err := store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
	require.NoError(t, err)
	assert.Equal(t, "invited", got.Role)
	assertParticipantRowExists(t, store, row.ID, "char-bob", "invited")
	assertOpsEventRecorded(t, store, row.ID, OpsKindMembershipInvite, "char-alice", "char-bob")
}

func TestInviteParticipantIsIdempotentForExistingInvitee(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-inv-2", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityPrivate),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	_, err := store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
	require.NoError(t, err)
	// Second invite — no error, no second event.
	_, err = store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
	require.NoError(t, err)
	assert.Equal(t, 1, countOpsEvents(t, store, row.ID, OpsKindMembershipInvite))
}

func TestInviteParticipantRejectsExistingMember(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-inv-3", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)

	_, err = store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_INVITE_TARGET_ALREADY_MEMBER")
}
```

- [ ] **Step 2: Implement `InviteParticipant` in store.go**

```go
// InviteParticipant inserts a participant row with role='invited'. Idempotent
// on identity match (re-inviting an already-invited character is a no-op,
// no second ops event). Rejected for existing members or owners with
// SCENE_INVITE_TARGET_ALREADY_MEMBER.
func (s *SceneStore) InviteParticipant(ctx context.Context, sceneID, inviterID, targetID string) (*ParticipantRow, error) {
	ctx, span := startSpan(ctx, "scene.store.invite_participant",
		attribute.String("scene_id", sceneID),
		attribute.String("inviter_id", inviterID),
		attribute.String("target_id", targetID),
	)
	defer span.End()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_INVITE_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx)

	// Check existing role for target — distinguish "already invited" (no-op),
	// "already member/owner" (error), and "not present" (insert).
	var existingRole string
	err = tx.QueryRow(ctx,
		`SELECT role FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
		sceneID, targetID,
	).Scan(&existingRole)

	switch {
	case err == nil:
		if existingRole == "invited" {
			// Idempotent no-op — return the existing row, no new ops event.
			row := &ParticipantRow{SceneID: sceneID, CharacterID: targetID, Role: "invited"}
			return row, tx.Commit(ctx)
		}
		// Already member or owner — reject.
		return nil, oops.Code("SCENE_INVITE_TARGET_ALREADY_MEMBER").
			With("scene_id", sceneID).
			With("target_id", targetID).
			With("current_role", existingRole).
			Errorf("character is already a %s", existingRole)
	case errors.Is(err, pgx.ErrNoRows):
		// Not present — fall through to insert.
	default:
		recordError(span, err)
		return nil, oops.Code("SCENE_INVITE_FAILED").Wrap(err)
	}

	row := &ParticipantRow{}
	err = tx.QueryRow(ctx, `
		INSERT INTO scene_participants (scene_id, character_id, role, joined_at)
		VALUES ($1, $2, 'invited', NOW())
		RETURNING scene_id, character_id, role, joined_at`,
		sceneID, targetID,
	).Scan(&row.SceneID, &row.CharacterID, &row.Role, &row.JoinedAt)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_INVITE_FAILED").
			With("scene_id", sceneID).With("target_id", targetID).Wrap(err)
	}

	if err := recordOpsEventTx(ctx, tx, sceneID, OpsKindMembershipInvite, inviterID, targetID, nil); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_INVITE_OPS_EVENT_FAILED").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_INVITE_FAILED").Wrap(err)
	}
	return row, nil
}
```

- [ ] **Step 3: Run integration tests**

Run: `task test:int -- -run TestInviteParticipant ./plugins/core-scenes/`
Expected: 3 tests pass.

- [ ] **Step 4: Add to interface and fakeStore**

Edit `service.go` interface — add `InviteParticipant(ctx context.Context, sceneID, inviterID, targetID string) (*ParticipantRow, error)`.

Edit `service_test.go` fakeStore:

```go
func (f *fakeStore) InviteParticipant(_ context.Context, sceneID, inviterID, targetID string) (*ParticipantRow, error) {
	if f.participants[sceneID] == nil {
		f.participants[sceneID] = make(map[string]string)
	}
	if existing, ok := f.participants[sceneID][targetID]; ok {
		if existing == "invited" {
			return &ParticipantRow{SceneID: sceneID, CharacterID: targetID, Role: "invited"}, nil
		}
		return nil, oops.Code("SCENE_INVITE_TARGET_ALREADY_MEMBER").
			With("scene_id", sceneID).With("target_id", targetID).Errorf("already %s", existing)
	}
	f.participants[sceneID][targetID] = "invited"
	return &ParticipantRow{SceneID: sceneID, CharacterID: targetID, Role: "invited"}, nil
}
```

- [ ] **Step 5: Write the failing service test**

Append to `service_test.go`:

```go
func TestSceneServiceInviteToSceneCallsStore(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-its-1", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityPrivate),
	}))
	svc := NewSceneServiceImpl(store)

	_, err := svc.InviteToScene(context.Background(), &scenev1.InviteToSceneRequest{
		CharacterId:       "char-alice",
		SceneId:           "scene-its-1",
		TargetCharacterId: "char-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, "invited", store.participants["scene-its-1"]["char-bob"])
}
```

- [ ] **Step 6: Implement `InviteToScene` service handler**

Add to `plugins/core-scenes/service.go`:

```go
// InviteToScene adds an 'invited' participant row for the target character.
// ABAC enforces owner-only invite at the dispatcher layer.
func (s *SceneServiceImpl) InviteToScene(ctx context.Context, req *scenev1.InviteToSceneRequest) (*scenev1.InviteToSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.invite_to_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
		attribute.String("target_id", req.GetTargetCharacterId()),
	)
	defer span.End()

	if _, err := s.store.InviteParticipant(ctx, req.GetSceneId(), req.GetCharacterId(), req.GetTargetCharacterId()); err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "SCENE_INVITE_TARGET_ALREADY_MEMBER" {
			return nil, status.Errorf(codes.AlreadyExists, "character is already a member of this scene")
		}
		slog.WarnContext(ctx, "scene.service.invite_to_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"target_id", req.GetTargetCharacterId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to invite: %v", err)
	}

	slog.InfoContext(ctx, "scene.service.invite_to_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
		"target_id", req.GetTargetCharacterId(),
	)
	return &scenev1.InviteToSceneResponse{}, nil
}
```

- [ ] **Step 7: Run service test**

Run: `task test -- -run TestSceneServiceInviteToScene ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
jj --no-pager commit -m "feat(scenes): InviteToScene RPC with idempotent invitation insertion

Adds InviteParticipant store method (insert with idempotent re-invite,
reject already-member targets) and the InviteToScene service handler.

Refs: holomush-5rh.12"
```

---

## Task 11: `KickFromScene` — `KickParticipant` + service handler

**Files:** `store.go`, `service.go`, `store_integration_test.go`, `service_test.go`

- [ ] **Step 1: Write failing integration tests**

Append to `store_integration_test.go`:

```go
func TestKickParticipantRemovesMemberRowAndEmitsOpsEvent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-kp-1", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)

	got, err := store.KickParticipant(ctx, row.ID, "char-alice", "char-bob")
	require.NoError(t, err)
	assert.Equal(t, "member", got.Role)
	assertParticipantRowAbsent(t, store, row.ID, "char-bob")

	payload := assertOpsEventRecorded(t, store, row.ID, OpsKindMembershipKick, "char-alice", "char-bob")
	assert.Equal(t, "member", payload["prior_role"])
}

func TestKickParticipantRemovesInvitedRowAndPayloadReflectsPriorRole(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-kp-inv", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityPrivate),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, err := store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
	require.NoError(t, err)

	got, err := store.KickParticipant(ctx, row.ID, "char-alice", "char-bob")
	require.NoError(t, err)
	assert.Equal(t, "invited", got.Role)
	assertParticipantRowAbsent(t, store, row.ID, "char-bob")

	payload := assertOpsEventRecorded(t, store, row.ID, OpsKindMembershipKick, "char-alice", "char-bob")
	assert.Equal(t, "invited", payload["prior_role"])
}

func TestKickParticipantRefusesToKickOwner(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-kp-owner", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	_, err := store.KickParticipant(ctx, row.ID, "char-alice", "char-alice")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_KICK_FORBIDDEN")
	assertParticipantRowExists(t, store, row.ID, "char-alice", "owner")
}
```

- [ ] **Step 2: Implement `KickParticipant` in store.go**

```go
// KickParticipant deletes the target's participant row. The DELETE filter is
// `WHERE role <> 'owner'` so the owner cannot be kicked even by themselves.
// Removes both 'member' and 'invited' rows in a single statement (i.e., kick
// also withdraws pending invitations).
//
// Returns SCENE_KICK_FORBIDDEN if the target is the owner; SCENE_PARTICIPANT_NOT_FOUND
// if the target isn't in the scene at all.
func (s *SceneStore) KickParticipant(ctx context.Context, sceneID, kickerID, targetID string) (*ParticipantRow, error) {
	ctx, span := startSpan(ctx, "scene.store.kick_participant",
		attribute.String("scene_id", sceneID),
		attribute.String("kicker_id", kickerID),
		attribute.String("target_id", targetID),
	)
	defer span.End()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_KICK_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx)

	row := &ParticipantRow{}
	err = tx.QueryRow(ctx, `
		DELETE FROM scene_participants
		WHERE scene_id = $1 AND character_id = $2 AND role <> 'owner'
		RETURNING scene_id, character_id, role, joined_at`,
		sceneID, targetID,
	).Scan(&row.SceneID, &row.CharacterID, &row.Role, &row.JoinedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Distinguish "owner" from "not present".
			var existing string
			err2 := tx.QueryRow(ctx,
				`SELECT role FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
				sceneID, targetID,
			).Scan(&existing)
			if err2 != nil {
				if errors.Is(err2, pgx.ErrNoRows) {
					return nil, oops.Code("SCENE_PARTICIPANT_NOT_FOUND").
						With("scene_id", sceneID).With("target_id", targetID).Wrap(err)
				}
				recordError(span, err2)
				return nil, oops.Code("SCENE_KICK_CLASSIFY_FAILED").Wrap(err2)
			}
			if existing == "owner" {
				return nil, oops.Code("SCENE_KICK_FORBIDDEN").
					With("scene_id", sceneID).
					With("target_id", targetID).
					Errorf("scene owner cannot be kicked")
			}
			return nil, oops.Code("SCENE_KICK_FAILED").Errorf("unexpected state")
		}
		recordError(span, err)
		return nil, oops.Code("SCENE_KICK_FAILED").Wrap(err)
	}

	payload := map[string]any{"prior_role": row.Role}
	if err := recordOpsEventTx(ctx, tx, sceneID, OpsKindMembershipKick, kickerID, targetID, payload); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_KICK_OPS_EVENT_FAILED").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_KICK_FAILED").Wrap(err)
	}
	return row, nil
}
```

- [ ] **Step 3: Run integration tests**

Run: `task test:int -- -run TestKickParticipant ./plugins/core-scenes/`
Expected: 3 tests pass.

- [ ] **Step 4: Add to interface and fakeStore**

Edit `service.go` interface — add `KickParticipant(ctx context.Context, sceneID, kickerID, targetID string) (*ParticipantRow, error)`.

Edit fakeStore in `service_test.go`:

```go
func (f *fakeStore) KickParticipant(_ context.Context, sceneID, kickerID, targetID string) (*ParticipantRow, error) {
	role, exists := f.participants[sceneID][targetID]
	if !exists {
		return nil, oops.Code("SCENE_PARTICIPANT_NOT_FOUND").
			With("scene_id", sceneID).With("target_id", targetID).Errorf("not found")
	}
	if role == "owner" {
		return nil, oops.Code("SCENE_KICK_FORBIDDEN").
			With("scene_id", sceneID).With("target_id", targetID).Errorf("cannot kick owner")
	}
	delete(f.participants[sceneID], targetID)
	return &ParticipantRow{SceneID: sceneID, CharacterID: targetID, Role: role}, nil
}
```

- [ ] **Step 5: Write failing service test**

Append to `service_test.go`:

```go
func TestSceneServiceKickFromSceneRemovesMember(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-kfs-1", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	_, _, err := store.AddParticipant(context.Background(), "scene-kfs-1", "char-bob")
	require.NoError(t, err)
	svc := NewSceneServiceImpl(store)

	_, err = svc.KickFromScene(context.Background(), &scenev1.KickFromSceneRequest{
		CharacterId:       "char-alice",
		SceneId:           "scene-kfs-1",
		TargetCharacterId: "char-bob",
	})
	require.NoError(t, err)
	_, exists := store.participants["scene-kfs-1"]["char-bob"]
	assert.False(t, exists)
}

func TestSceneServiceKickFromSceneRejectsKickingOwner(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-kfs-owner", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	svc := NewSceneServiceImpl(store)

	_, err := svc.KickFromScene(context.Background(), &scenev1.KickFromSceneRequest{
		CharacterId:       "char-alice",
		SceneId:           "scene-kfs-owner",
		TargetCharacterId: "char-alice",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}
```

- [ ] **Step 6: Implement `KickFromScene` service handler**

Add to `plugins/core-scenes/service.go`:

```go
// KickFromScene removes a target character from a scene. ABAC enforces
// owner-only kick at the dispatcher layer. The store's WHERE filter is
// the defense-in-depth layer that prevents owner removal.
func (s *SceneServiceImpl) KickFromScene(ctx context.Context, req *scenev1.KickFromSceneRequest) (*scenev1.KickFromSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.kick_from_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
		attribute.String("target_id", req.GetTargetCharacterId()),
	)
	defer span.End()

	if _, err := s.store.KickParticipant(ctx, req.GetSceneId(), req.GetCharacterId(), req.GetTargetCharacterId()); err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) {
			switch oe.Code() {
			case "SCENE_PARTICIPANT_NOT_FOUND":
				return nil, status.Errorf(codes.NotFound, "target not in scene")
			case "SCENE_KICK_FORBIDDEN":
				return nil, status.Errorf(codes.FailedPrecondition,
					"scene owner cannot be kicked")
			}
		}
		slog.WarnContext(ctx, "scene.service.kick_from_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"target_id", req.GetTargetCharacterId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to kick: %v", err)
	}

	slog.InfoContext(ctx, "scene.service.kick_from_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
		"target_id", req.GetTargetCharacterId(),
	)
	return &scenev1.KickFromSceneResponse{}, nil
}
```

- [ ] **Step 7: Run tests**

Run: `task test -- -run TestSceneServiceKick ./plugins/core-scenes/ && task test:int -- -run TestKickParticipant ./plugins/core-scenes/`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
jj --no-pager commit -m "feat(scenes): KickFromScene RPC removes member or invited row

Hard delete with WHERE role <> 'owner' filter. Kick removes both member
and invited rows (kick also withdraws pending invitations). The
prior_role payload field tells observers which case applied.

Refs: holomush-5rh.12"
```

---

## Task 12: `TransferOwnership` — store + `classifyTransferMiss` + service handler

**Files:** `store.go`, `service.go`, `store_integration_test.go`, `service_test.go`

- [ ] **Step 1: Write failing integration tests**

Append to `store_integration_test.go`:

```go
func TestTransferOwnershipUpdatesParticipantsAndScenesRowAtomically(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-to-1", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)

	err = store.TransferOwnership(ctx, row.ID, "char-alice", "char-bob")
	require.NoError(t, err)

	// Previous owner is now a member.
	assertParticipantRowExists(t, store, row.ID, "char-alice", "member")
	// New owner.
	assertParticipantRowExists(t, store, row.ID, "char-bob", "owner")
	// Denormalised scenes.owner_id updated.
	got, err := store.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, "char-bob", got.OwnerID)

	payload := assertOpsEventRecorded(t, store, row.ID, OpsKindMembershipOwnershipXfer, "char-alice", "char-bob")
	assert.Equal(t, "char-alice", payload["from"])
}

func TestTransferOwnershipRejectsNonMemberTarget(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-to-nm", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	err := store.TransferOwnership(ctx, row.ID, "char-alice", "char-bob")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_TRANSFER_TARGET_NOT_MEMBER")
}

func TestTransferOwnershipRejectsNonOwnerCaller(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-to-no", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)
	_, _, err = store.AddParticipant(ctx, row.ID, "char-carol")
	require.NoError(t, err)

	// char-bob (not owner) tries to transfer ownership of the scene to char-carol.
	err = store.TransferOwnership(ctx, row.ID, "char-bob", "char-carol")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_NOT_OWNER")
}

func TestTransferOwnershipIsNoOpWhenTargetEqualsCurrentOwner(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-to-self", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	err := store.TransferOwnership(ctx, row.ID, "char-alice", "char-alice")
	require.NoError(t, err) // idempotent no-op
	assertParticipantRowExists(t, store, row.ID, "char-alice", "owner")
	// No transfer ops event emitted.
	assert.Equal(t, 0, countOpsEvents(t, store, row.ID, OpsKindMembershipOwnershipXfer))
}
```

- [ ] **Step 2: Implement `TransferOwnership` and `classifyTransferMiss` in store.go**

```go
// TransferOwnership performs a 3-statement transactional ownership swap:
//   1. Demote current owner: UPDATE scene_participants SET role='member' WHERE owner
//   2. Promote target: UPDATE scene_participants SET role='owner' WHERE member
//   3. Update denormalised owner_id: UPDATE scenes SET owner_id = $new
//
// All three statements MUST succeed (non-empty RETURNING). Rolls back otherwise.
// Idempotent when newOwnerID == currentOwnerID (returns nil without changes).
func (s *SceneStore) TransferOwnership(ctx context.Context, sceneID, currentOwnerID, newOwnerID string) error {
	ctx, span := startSpan(ctx, "scene.store.transfer_ownership",
		attribute.String("scene_id", sceneID),
		attribute.String("current_owner", currentOwnerID),
		attribute.String("new_owner", newOwnerID),
	)
	defer span.End()

	if currentOwnerID == newOwnerID {
		return nil // idempotent no-op
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return oops.Code("SCENE_TRANSFER_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx)

	// Statement 1: demote current owner.
	var demotedID string
	err = tx.QueryRow(ctx, `
		UPDATE scene_participants SET role = 'member'
		WHERE scene_id = $1 AND character_id = $2 AND role = 'owner'
		RETURNING character_id`,
		sceneID, currentOwnerID,
	).Scan(&demotedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.classifyTransferMiss(ctx, sceneID, currentOwnerID, newOwnerID, span)
		}
		recordError(span, err)
		return oops.Code("SCENE_TRANSFER_FAILED").Wrap(err)
	}

	// Statement 2: promote target (must currently be a member, not invited).
	var promotedID string
	err = tx.QueryRow(ctx, `
		UPDATE scene_participants SET role = 'owner'
		WHERE scene_id = $1 AND character_id = $2 AND role = 'member'
		RETURNING character_id`,
		sceneID, newOwnerID,
	).Scan(&promotedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.classifyTransferMiss(ctx, sceneID, currentOwnerID, newOwnerID, span)
		}
		recordError(span, err)
		return oops.Code("SCENE_TRANSFER_FAILED").Wrap(err)
	}

	// Statement 3: update denormalised owner_id, gated on state.
	var sceneIDOut string
	err = tx.QueryRow(ctx, `
		UPDATE scenes SET owner_id = $1
		WHERE id = $2 AND owner_id = $3 AND state IN ('active', 'paused')
		RETURNING id`,
		newOwnerID, sceneID, currentOwnerID,
	).Scan(&sceneIDOut)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.classifyTransferMiss(ctx, sceneID, currentOwnerID, newOwnerID, span)
		}
		recordError(span, err)
		return oops.Code("SCENE_TRANSFER_FAILED").Wrap(err)
	}

	payload := map[string]any{"from": currentOwnerID}
	if err := recordOpsEventTx(ctx, tx, sceneID, OpsKindMembershipOwnershipXfer, currentOwnerID, newOwnerID, payload); err != nil {
		recordError(span, err)
		return oops.Code("SCENE_TRANSFER_OPS_EVENT_FAILED").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return oops.Code("SCENE_TRANSFER_FAILED").Wrap(err)
	}
	return nil
}

// classifyTransferMiss diagnoses why TransferOwnership's UPDATE chain failed.
// Issues a single SELECT to figure out the precise reason.
func (s *SceneStore) classifyTransferMiss(ctx context.Context, sceneID, currentOwnerID, newOwnerID string, span trace.Span) error {
	// Check the scene exists and its state.
	var (
		state          string
		actualOwnerID  string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT state, owner_id FROM scenes WHERE id = $1`,
		sceneID,
	).Scan(&state, &actualOwnerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.Code("SCENE_NOT_FOUND").With("scene_id", sceneID).Wrap(err)
		}
		recordError(span, err)
		return oops.Code("SCENE_TRANSFER_CLASSIFY_FAILED").Wrap(err)
	}
	if state != "active" && state != "paused" {
		return oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", sceneID).With("op", "transfer-ownership").
			With("current_state", state).
			Errorf("scene in state %q cannot have ownership transferred", state)
	}
	if actualOwnerID != currentOwnerID {
		return oops.Code("SCENE_NOT_OWNER").
			With("scene_id", sceneID).
			With("caller", currentOwnerID).
			With("actual_owner", actualOwnerID).
			Errorf("caller is not the current owner")
	}
	// State is OK and caller IS the owner; the failure must be the target.
	var targetRole string
	err = s.pool.QueryRow(ctx,
		`SELECT role FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
		sceneID, newOwnerID,
	).Scan(&targetRole)
	if err != nil || targetRole != "member" {
		return oops.Code("SCENE_TRANSFER_TARGET_NOT_MEMBER").
			With("scene_id", sceneID).
			With("target_id", newOwnerID).
			Errorf("transfer target must be an existing member")
	}
	return oops.Code("SCENE_TRANSFER_FAILED").Errorf("unexpected classify state")
}
```

- [ ] **Step 3: Run integration tests**

Run: `task test:int -- -run TestTransferOwnership ./plugins/core-scenes/`
Expected: 4 tests pass.

- [ ] **Step 4: Add to interface and fakeStore**

Edit `service.go` interface — add `TransferOwnership(ctx context.Context, sceneID, currentOwnerID, newOwnerID string) error`.

Edit fakeStore in `service_test.go`:

```go
func (f *fakeStore) TransferOwnership(_ context.Context, sceneID, currentOwnerID, newOwnerID string) error {
	if currentOwnerID == newOwnerID {
		return nil
	}
	scene, ok := f.scenes[sceneID]
	if !ok {
		return oops.Code("SCENE_NOT_FOUND").With("scene_id", sceneID).Errorf("not found")
	}
	if scene.OwnerID != currentOwnerID {
		return oops.Code("SCENE_NOT_OWNER").With("scene_id", sceneID).Errorf("not owner")
	}
	if scene.State != string(SceneStateActive) && scene.State != string(SceneStatePaused) {
		return oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", sceneID).With("current_state", scene.State).Errorf("wrong state")
	}
	if f.participants[sceneID][newOwnerID] != "member" {
		return oops.Code("SCENE_TRANSFER_TARGET_NOT_MEMBER").
			With("scene_id", sceneID).With("target_id", newOwnerID).Errorf("not member")
	}
	f.participants[sceneID][currentOwnerID] = "member"
	f.participants[sceneID][newOwnerID] = "owner"
	scene.OwnerID = newOwnerID
	return nil
}
```

- [ ] **Step 5: Write failing service tests**

Append to `service_test.go`:

```go
func TestSceneServiceTransferOwnershipUpdatesOwner(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-tos-1", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	_, _, err := store.AddParticipant(context.Background(), "scene-tos-1", "char-bob")
	require.NoError(t, err)
	svc := NewSceneServiceImpl(store)

	_, err = svc.TransferOwnership(context.Background(), &scenev1.TransferOwnershipRequest{
		CharacterId:           "char-alice",
		SceneId:               "scene-tos-1",
		NewOwnerCharacterId:   "char-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, "char-bob", store.scenes["scene-tos-1"].OwnerID)
}

func TestSceneServiceTransferOwnershipRejectsNonMemberTargetWithFailedPrecondition(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-tos-nm", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	svc := NewSceneServiceImpl(store)

	_, err := svc.TransferOwnership(context.Background(), &scenev1.TransferOwnershipRequest{
		CharacterId:         "char-alice",
		SceneId:             "scene-tos-nm",
		NewOwnerCharacterId: "char-bob",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}
```

- [ ] **Step 6: Implement `TransferOwnership` service handler**

Add to `plugins/core-scenes/service.go`:

```go
// TransferOwnership reassigns ownership of a scene from the calling character
// to a target member. ABAC enforces owner-only transfer at the dispatcher.
// Per design decision P3.D8, the target MUST be an existing member; the
// previous owner becomes a member.
func (s *SceneServiceImpl) TransferOwnership(ctx context.Context, req *scenev1.TransferOwnershipRequest) (*scenev1.TransferOwnershipResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.transfer_ownership",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
		attribute.String("new_owner", req.GetNewOwnerCharacterId()),
	)
	defer span.End()

	if err := s.store.TransferOwnership(ctx, req.GetSceneId(), req.GetCharacterId(), req.GetNewOwnerCharacterId()); err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) {
			switch oe.Code() {
			case "SCENE_NOT_FOUND":
				return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
			case "SCENE_NOT_OWNER":
				return nil, status.Errorf(codes.PermissionDenied,
					"only the scene owner can transfer ownership")
			case "SCENE_TRANSITION_FORBIDDEN":
				return nil, status.Errorf(codes.FailedPrecondition,
					"scene cannot have ownership transferred in its current state")
			case "SCENE_TRANSFER_TARGET_NOT_MEMBER":
				return nil, status.Errorf(codes.FailedPrecondition,
					"transfer target must be an existing member of the scene")
			}
		}
		slog.WarnContext(ctx, "scene.service.transfer_ownership store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"new_owner", req.GetNewOwnerCharacterId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to transfer ownership: %v", err)
	}

	slog.InfoContext(ctx, "scene.service.transfer_ownership ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
		"new_owner", req.GetNewOwnerCharacterId(),
	)
	return &scenev1.TransferOwnershipResponse{}, nil
}
```

- [ ] **Step 7: Run all tests**

Run: `task test -- ./plugins/core-scenes/ && task test:int -- -run TestTransferOwnership ./plugins/core-scenes/`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
jj --no-pager commit -m "feat(scenes): TransferOwnership RPC with 3-statement transactional swap

Demote current owner → promote target → update denormalised owner_id,
all in one transaction. Idempotent when target == current owner.
classifyTransferMiss diagnoses precise failure reasons (not found,
not owner, wrong state, target not member).

Refs: holomush-5rh.12"
```

---

## Task 13: `ListParticipants` + `GetParticipant` store methods

**Files:** `store.go`, `store_integration_test.go`, `service.go` (interface), `service_test.go` (fakeStore)

These are read-only convenience methods used by future commands like `scene info`. No service handler in Phase 3 — they exist so the integration tests have a clean way to assert state and so the store API is complete.

- [ ] **Step 1: Write failing integration tests**

Append to `store_integration_test.go`:

```go
func TestListParticipantsReturnsAllRolesOrderedByJoinedAt(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-lp-1", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)

	got, err := store.ListParticipants(ctx, row.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "char-alice", got[0].CharacterID) // joined first via CreateWithOwner
	assert.Equal(t, "owner", got[0].Role)
	assert.Equal(t, "char-bob", got[1].CharacterID)
	assert.Equal(t, "member", got[1].Role)
}

func TestGetParticipantReturnsRowWhenPresent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-gp-1", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	got, err := store.GetParticipant(ctx, row.ID, "char-alice")
	require.NoError(t, err)
	assert.Equal(t, "owner", got.Role)
}

func TestGetParticipantReturnsNotFoundForMissingParticipant(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-gp-missing", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	_, err := store.GetParticipant(ctx, row.ID, "char-ghost")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_PARTICIPANT_NOT_FOUND")
}
```

- [ ] **Step 2: Implement both methods in store.go**

```go
// ListParticipants returns all participants for a scene, ordered by joined_at
// ASC (so the owner appears first since CreateWithOwner inserts them at scene
// creation).
func (s *SceneStore) ListParticipants(ctx context.Context, sceneID string) ([]ParticipantRow, error) {
	ctx, span := startSpan(ctx, "scene.store.list_participants",
		attribute.String("scene_id", sceneID),
	)
	defer span.End()

	rows, err := s.pool.Query(ctx, `
		SELECT scene_id, character_id, role, joined_at
		FROM scene_participants
		WHERE scene_id = $1
		ORDER BY joined_at ASC`,
		sceneID,
	)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_LIST_PARTICIPANTS_FAILED").
			With("scene_id", sceneID).Wrap(err)
	}
	defer rows.Close()

	var out []ParticipantRow
	for rows.Next() {
		var p ParticipantRow
		if err := rows.Scan(&p.SceneID, &p.CharacterID, &p.Role, &p.JoinedAt); err != nil {
			recordError(span, err)
			return nil, oops.Code("SCENE_LIST_PARTICIPANTS_FAILED").Wrap(err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_LIST_PARTICIPANTS_FAILED").Wrap(err)
	}
	return out, nil
}

// GetParticipant returns a single participant row, or SCENE_PARTICIPANT_NOT_FOUND.
func (s *SceneStore) GetParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, error) {
	ctx, span := startSpan(ctx, "scene.store.get_participant",
		attribute.String("scene_id", sceneID),
		attribute.String("character_id", characterID),
	)
	defer span.End()

	p := &ParticipantRow{}
	err := s.pool.QueryRow(ctx, `
		SELECT scene_id, character_id, role, joined_at
		FROM scene_participants
		WHERE scene_id = $1 AND character_id = $2`,
		sceneID, characterID,
	).Scan(&p.SceneID, &p.CharacterID, &p.Role, &p.JoinedAt)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.Code("SCENE_PARTICIPANT_NOT_FOUND").
				With("scene_id", sceneID).
				With("character_id", characterID).Wrap(err)
		}
		return nil, oops.Code("SCENE_GET_PARTICIPANT_FAILED").
			With("scene_id", sceneID).
			With("character_id", characterID).Wrap(err)
	}
	return p, nil
}
```

- [ ] **Step 3: Run integration tests**

Run: `task test:int -- -run "TestListParticipants|TestGetParticipant" ./plugins/core-scenes/`
Expected: 3 tests pass.

- [ ] **Step 4: Add to interface and fakeStore (minimal)**

Edit `service.go` interface — add both methods.

Edit fakeStore:

```go
func (f *fakeStore) ListParticipants(_ context.Context, sceneID string) ([]ParticipantRow, error) {
	var out []ParticipantRow
	for cid, role := range f.participants[sceneID] {
		out = append(out, ParticipantRow{SceneID: sceneID, CharacterID: cid, Role: role})
	}
	return out, nil
}

func (f *fakeStore) GetParticipant(_ context.Context, sceneID, characterID string) (*ParticipantRow, error) {
	role, ok := f.participants[sceneID][characterID]
	if !ok {
		return nil, oops.Code("SCENE_PARTICIPANT_NOT_FOUND").
			With("scene_id", sceneID).With("character_id", characterID).Errorf("not found")
	}
	return &ParticipantRow{SceneID: sceneID, CharacterID: characterID, Role: role}, nil
}
```

- [ ] **Step 5: Verify build**

Run: `task build && task test -- ./plugins/core-scenes/`
Expected: success.

- [ ] **Step 6: Commit**

```bash
jj --no-pager commit -m "feat(scenes): ListParticipants and GetParticipant store methods

Read-only convenience methods for completeness of the store API. No
service handlers in Phase 3 — exposed via the sceneStorer interface for
future commands like scene info to consume.

Refs: holomush-5rh.12"
```

---

## Task 14: Phase 1+2 retrofit — emit lifecycle/settings ops events

**Files:** `plugins/core-scenes/store.go`, `plugins/core-scenes/store_integration_test.go`

The Phase 1+2 store methods (`End`, `Pause`, `Resume`, `Update`) need to become transactional and emit their corresponding ops events. Phase 1's `Create` is already replaced by `CreateWithOwner` in Task 5.

- [ ] **Step 1: Write failing integration tests for ops event emission**

Append to `store_integration_test.go`:

```go
func TestEndEmitsLifecycleEndedOpsEventInSameTransaction(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-end-ope", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	_, err := store.End(ctx, row.ID)
	require.NoError(t, err)

	payload := assertOpsEventRecorded(t, store, row.ID, OpsKindLifecycleEnded, row.OwnerID, "")
	assert.Equal(t, "active", payload["prior_state"])
}

func TestPauseEmitsLifecyclePausedOpsEvent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-pause-ope", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	_, err := store.Pause(ctx, row.ID)
	require.NoError(t, err)
	assertOpsEventRecorded(t, store, row.ID, OpsKindLifecyclePaused, row.OwnerID, "")
}

func TestResumeEmitsLifecycleResumedOpsEvent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-resume-ope", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, err := store.Pause(ctx, row.ID)
	require.NoError(t, err)

	_, err = store.Resume(ctx, row.ID)
	require.NoError(t, err)
	assertOpsEventRecorded(t, store, row.ID, OpsKindLifecycleResumed, row.OwnerID, "")
}

func TestUpdateEmitsSettingsUpdatedOpsEventWithMaskPaths(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-upd-ope", OwnerID: "char-alice", Title: "Old",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	newTitle := "New"
	_, err := store.Update(ctx, row.ID, &SceneUpdate{Title: &newTitle})
	require.NoError(t, err)

	payload := assertOpsEventRecorded(t, store, row.ID, OpsKindSettingsUpdated, row.OwnerID, "")
	paths, ok := payload["paths"].([]any)
	require.True(t, ok)
	assert.Contains(t, paths, "title")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test:int -- -run "TestEndEmits|TestPauseEmits|TestResumeEmits|TestUpdateEmits" ./plugins/core-scenes/`
Expected: all 4 fail because the existing Phase 2 methods don't emit ops events.

- [ ] **Step 3: Refactor `End` to be transactional and emit the ops event**

Edit `plugins/core-scenes/store.go`. Replace the `End` method body with:

```go
func (s *SceneStore) End(ctx context.Context, id string) (*SceneRow, error) {
	ctx, span := startSpan(ctx, "scene.store.end",
		attribute.String("scene_id", id),
	)
	defer span.End()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_END_FAILED").With("scene_id", id).Wrap(err)
	}
	defer tx.Rollback(ctx)

	// Capture prior_state in the same SELECT-via-RETURNING by reading the
	// state before the update via a subquery on the original row.
	row := &SceneRow{}
	var priorState string
	err = tx.QueryRow(ctx, `
		WITH prior AS (
			SELECT state FROM scenes WHERE id = $1
		)
		UPDATE scenes
		SET state = 'ended', ended_at = NOW()
		WHERE id = $1 AND state IN ('active', 'paused')
		RETURNING `+sceneSelectColumns+`, (SELECT state FROM prior)`,
		id,
	).Scan(
		&row.ID, &row.Title, &row.Description, &row.LocationID, &row.OwnerID,
		&row.State, &row.PoseOrder, &row.Visibility, &row.IdleTimeoutSecs,
		&row.TemplateID, &row.ContentWarnings, &row.Tags,
		&row.CreatedAt, &row.EndedAt, &row.ArchivedAt,
		&priorState,
	)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, s.classifyTransitionMiss(ctx, id, span, "end")
		}
		return nil, oops.Code("SCENE_END_FAILED").With("scene_id", id).Wrap(err)
	}

	payload := map[string]any{"prior_state": priorState}
	if err := recordOpsEventTx(ctx, tx, id, OpsKindLifecycleEnded, row.OwnerID, "", payload); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_END_OPS_EVENT_FAILED").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_END_FAILED").Wrap(err)
	}
	return row, nil
}
```

- [ ] **Step 4: Refactor `Pause` similarly**

Replace the `Pause` body in `store.go`:

```go
func (s *SceneStore) Pause(ctx context.Context, id string) (*SceneRow, error) {
	ctx, span := startSpan(ctx, "scene.store.pause",
		attribute.String("scene_id", id),
	)
	defer span.End()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_PAUSE_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx)

	row := &SceneRow{}
	err = scanSceneRow(tx.QueryRow(ctx, `
		UPDATE scenes
		SET state = 'paused'
		WHERE id = $1 AND state = 'active'
		RETURNING `+sceneSelectColumns,
		id,
	), row)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, s.classifyTransitionMiss(ctx, id, span, "pause")
		}
		return nil, oops.Code("SCENE_PAUSE_FAILED").Wrap(err)
	}

	if err := recordOpsEventTx(ctx, tx, id, OpsKindLifecyclePaused, row.OwnerID, "", nil); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_PAUSE_OPS_EVENT_FAILED").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_PAUSE_FAILED").Wrap(err)
	}
	return row, nil
}
```

- [ ] **Step 5: Refactor `Resume` similarly**

Replace the `Resume` body in `store.go`:

```go
func (s *SceneStore) Resume(ctx context.Context, id string) (*SceneRow, error) {
	ctx, span := startSpan(ctx, "scene.store.resume",
		attribute.String("scene_id", id),
	)
	defer span.End()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_RESUME_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx)

	row := &SceneRow{}
	err = scanSceneRow(tx.QueryRow(ctx, `
		UPDATE scenes
		SET state = 'active'
		WHERE id = $1 AND state = 'paused'
		RETURNING `+sceneSelectColumns,
		id,
	), row)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, s.classifyTransitionMiss(ctx, id, span, "resume")
		}
		return nil, oops.Code("SCENE_RESUME_FAILED").Wrap(err)
	}

	if err := recordOpsEventTx(ctx, tx, id, OpsKindLifecycleResumed, row.OwnerID, "", nil); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_RESUME_OPS_EVENT_FAILED").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_RESUME_FAILED").Wrap(err)
	}
	return row, nil
}
```

- [ ] **Step 6: Refactor `Update` to wrap in transaction and emit settings.updated**

Replace the existing `Update` method. The tricky part is that `Update` already has dynamic SQL — keep that logic, but wrap it in `tx.Begin/Commit` and call `recordOpsEventTx` after the UPDATE succeeds. The mask paths come from the `SceneUpdate` struct's set fields.

```go
func (s *SceneStore) Update(ctx context.Context, id string, update *SceneUpdate) (*SceneRow, error) {
	ctx, span := startSpan(ctx, "scene.store.update",
		attribute.String("scene_id", id),
	)
	defer span.End()

	if update == nil || !update.HasChanges() {
		return s.Get(ctx, id)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_UPDATE_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx)

	// Build SET clause and capture mask paths for the ops event payload.
	var (
		setParts []string
		args     []any
		paths    []string
		argIdx   = 1
	)
	addSet := func(col string, value any) {
		setParts = append(setParts, fmt.Sprintf("%s = $%d", col, argIdx))
		args = append(args, value)
		paths = append(paths, col)
		argIdx++
	}
	if update.Title != nil {
		addSet("title", *update.Title)
	}
	if update.Description != nil {
		addSet("description", *update.Description)
	}
	if update.Visibility != nil {
		addSet("visibility", *update.Visibility)
	}
	if update.PoseOrder != nil {
		addSet("pose_order", *update.PoseOrder)
	}
	if update.LocationID != nil {
		if *update.LocationID == "" {
			setParts = append(setParts, fmt.Sprintf("location_id = $%d", argIdx))
			args = append(args, nil)
			paths = append(paths, "location_id")
			argIdx++
		} else {
			addSet("location_id", *update.LocationID)
		}
	}
	if update.UpdateContentWarnings {
		addSet("content_warnings", update.ContentWarnings)
	}
	if update.UpdateTags {
		addSet("tags", update.Tags)
	}

	args = append(args, id)
	sceneIDIdx := argIdx

	query := fmt.Sprintf(
		`UPDATE scenes
         SET %s
         WHERE id = $%d AND state IN ('active', 'paused')
         RETURNING %s`,
		strings.Join(setParts, ", "),
		sceneIDIdx,
		sceneSelectColumns,
	)

	row := &SceneRow{}
	err = scanSceneRow(tx.QueryRow(ctx, query, args...), row)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, s.classifyTransitionMiss(ctx, id, span, "update")
		}
		return nil, oops.Code("SCENE_UPDATE_FAILED").With("scene_id", id).Wrap(err)
	}

	payload := map[string]any{"paths": paths}
	if err := recordOpsEventTx(ctx, tx, id, OpsKindSettingsUpdated, row.OwnerID, "", payload); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_UPDATE_OPS_EVENT_FAILED").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_UPDATE_FAILED").Wrap(err)
	}
	return row, nil
}
```

- [ ] **Step 7: Run the new tests**

Run: `task test:int -- -run "TestEndEmits|TestPauseEmits|TestResumeEmits|TestUpdateEmits" ./plugins/core-scenes/`
Expected: 4 tests pass.

- [ ] **Step 8: Run the existing Phase 2 lifecycle tests to confirm no regressions**

Run: `task test:int -- -run "TestSceneStoreEnd|TestSceneStorePause|TestSceneStoreResume|TestSceneStoreUpdate" ./plugins/core-scenes/`
Expected: existing tests still pass.

- [ ] **Step 9: Run the full integration suite**

Run: `task test:int -- ./plugins/core-scenes/`
Expected: all integration tests pass.

- [ ] **Step 10: Run unit tests**

Run: `task test -- ./plugins/core-scenes/`
Expected: pass.

- [ ] **Step 11: Commit**

```bash
jj --no-pager commit -m "feat(scenes): emit lifecycle and settings ops events from Phase 1+2 handlers

End/Pause/Resume/Update become transactional and emit lifecycle.ended,
lifecycle.paused, lifecycle.resumed, and settings.updated ops events
respectively. The ops timeline is now complete from day one — no
'audit gap' between when the table exists and when these handlers are
retrofitted.

Refs: holomush-5rh.12"
```

---

## Task 15: Command handlers — `handleJoin`/`Leave`/`Invite`/`Kick`/`Transfer`

**Files:** `plugins/core-scenes/commands.go`, `plugins/core-scenes/commands_test.go`

- [ ] **Step 1: Update the dispatcher to recognise the new subcommands**

Edit `plugins/core-scenes/commands.go`. In `dispatchCommand`, extend the switch and the help message:

```go
func (p *scenePlugin) dispatchCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	ctx, span := startSpan(ctx, "scene.command.dispatch",
		attribute.String("subject_id", req.CharacterID),
	)
	defer span.End()

	sub, rest := splitSubcommand(req.Args)
	span.SetAttributes(attribute.String("subcommand", sub))

	if sub == "" {
		return pluginsdk.Errorf("Usage: scene <subcommand> [args]\nKnown subcommands: create, info, end, pause, resume, set, join, leave, invite, kick, transfer"), nil
	}

	switch sub {
	case "create":
		return p.handleCreate(ctx, req, rest)
	case "info":
		return p.handleInfo(ctx, req, rest)
	case "end":
		return p.handleEnd(ctx, req, rest)
	case "pause":
		return p.handlePause(ctx, req, rest)
	case "resume":
		return p.handleResume(ctx, req, rest)
	case "set":
		return p.handleSet(ctx, req, rest)
	case "join":
		return p.handleJoin(ctx, req, rest)
	case "leave":
		return p.handleLeave(ctx, req, rest)
	case "invite":
		return p.handleInvite(ctx, req, rest)
	case "kick":
		return p.handleKick(ctx, req, rest)
	case "transfer":
		return p.handleTransfer(ctx, req, rest)
	default:
		return pluginsdk.Errorf("Unknown scene subcommand %q. Known subcommands: create, info, end, pause, resume, set, join, leave, invite, kick, transfer.", sub), nil
	}
}
```

- [ ] **Step 2: Implement the five handler functions**

Append to `plugins/core-scenes/commands.go`:

```go
// handleJoin parses "scene join <scene-id>" and calls JoinScene.
func (p *scenePlugin) handleJoin(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID := strings.TrimSpace(args)
	if sceneID == "" {
		return pluginsdk.Errorf("Usage: scene join <scene id>"), nil
	}

	_, err := p.service.JoinScene(ctx, &scenev1.JoinSceneRequest{
		CharacterId: req.CharacterID,
		SceneId:     sceneID,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to join scene: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Joined scene %s.", sceneID),
	}, nil
}

// handleLeave parses "scene leave <scene-id>" and calls LeaveScene.
func (p *scenePlugin) handleLeave(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID := strings.TrimSpace(args)
	if sceneID == "" {
		return pluginsdk.Errorf("Usage: scene leave <scene id>"), nil
	}

	_, err := p.service.LeaveScene(ctx, &scenev1.LeaveSceneRequest{
		CharacterId: req.CharacterID,
		SceneId:     sceneID,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to leave scene: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Left scene %s.", sceneID),
	}, nil
}

// handleInvite parses "scene invite <scene-id> <character>".
func (p *scenePlugin) handleInvite(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID, rest := splitSubcommand(args)
	target := strings.TrimSpace(rest)
	if sceneID == "" || target == "" {
		return pluginsdk.Errorf("Usage: scene invite <scene id> <character>"), nil
	}

	_, err := p.service.InviteToScene(ctx, &scenev1.InviteToSceneRequest{
		CharacterId:       req.CharacterID,
		SceneId:           sceneID,
		TargetCharacterId: target,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to invite: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Invited %s to scene %s.", target, sceneID),
	}, nil
}

// handleKick parses "scene kick <scene-id> <character>".
func (p *scenePlugin) handleKick(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID, rest := splitSubcommand(args)
	target := strings.TrimSpace(rest)
	if sceneID == "" || target == "" {
		return pluginsdk.Errorf("Usage: scene kick <scene id> <character>"), nil
	}

	_, err := p.service.KickFromScene(ctx, &scenev1.KickFromSceneRequest{
		CharacterId:       req.CharacterID,
		SceneId:           sceneID,
		TargetCharacterId: target,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to kick: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Removed %s from scene %s.", target, sceneID),
	}, nil
}

// handleTransfer parses "scene transfer <scene-id> <character>".
func (p *scenePlugin) handleTransfer(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID, rest := splitSubcommand(args)
	target := strings.TrimSpace(rest)
	if sceneID == "" || target == "" {
		return pluginsdk.Errorf("Usage: scene transfer <scene id> <character>"), nil
	}

	_, err := p.service.TransferOwnership(ctx, &scenev1.TransferOwnershipRequest{
		CharacterId:         req.CharacterID,
		SceneId:             sceneID,
		NewOwnerCharacterId: target,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to transfer ownership: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Transferred ownership of scene %s to %s.", sceneID, target),
	}, nil
}
```

- [ ] **Step 3: Add unit tests for command parsing**

Append to `plugins/core-scenes/commands_test.go`:

```go
func TestSceneCommandJoinForwardsToServiceWithCorrectSceneID(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-cmd-j", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	plugin := &scenePlugin{service: NewSceneServiceImpl(store)}

	resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "join scene-cmd-j",
		CharacterID: "char-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "Joined scene scene-cmd-j")
}

func TestSceneCommandLeaveRejectsMissingSceneID(t *testing.T) {
	plugin := &scenePlugin{service: NewSceneServiceImpl(newFakeStore())}

	resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "leave", CharacterID: "char-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "Usage: scene leave")
}

func TestSceneCommandInviteParsesSceneIDAndTarget(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-cmd-i", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityPrivate),
	}))
	plugin := &scenePlugin{service: NewSceneServiceImpl(store)}

	resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "invite scene-cmd-i char-bob", CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Equal(t, "invited", store.participants["scene-cmd-i"]["char-bob"])
}

func TestSceneCommandTransferRejectsMissingTarget(t *testing.T) {
	plugin := &scenePlugin{service: NewSceneServiceImpl(newFakeStore())}

	resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "transfer scene-x", CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "Usage: scene transfer")
}
```

- [ ] **Step 4: Run command tests**

Run: `task test -- -run TestSceneCommand ./plugins/core-scenes/`
Expected: existing + new tests pass.

- [ ] **Step 5: Commit**

```bash
jj --no-pager commit -m "feat(scenes): scene join/leave/invite/kick/transfer command handlers

Adds the five new subcommands to the dispatcher with parsing and
error-message conventions matching Phase 2's existing handlers.

Refs: holomush-5rh.12"
```

---

## Task 16: ABAC policy replacement in `plugin.yaml`

**Files:** `plugins/core-scenes/plugin.yaml`

- [ ] **Step 1: Replace the policies section in plugin.yaml**

Edit `plugins/core-scenes/plugin.yaml`. Replace the entire `policies:` block with:

```yaml
policies:
  # ─── Layer 1: command execution gate (unchanged from Phase 1) ─────────
  - name: execute-scene-commands
    dsl: >-
      permit(principal is character, action in ["execute"], resource is command) when { resource.command.name in ["scene",
      "scenes"] };

  # ─── Owner-only operations (unchanged from Phase 2) ───────────────────
  - name: end-own-scene
    dsl: >-
      permit(principal is character, action in ["end"], resource is scene) when { resource.scene.owner == principal.id &&
      resource.scene.state in ["active", "paused"] };
  - name: pause-own-scene
    dsl: >-
      permit(principal is character, action in ["pause"], resource is scene) when { resource.scene.owner == principal.id
      && resource.scene.state == "active" };
  - name: update-own-scene
    dsl: >-
      permit(principal is character, action in ["update"], resource is scene) when { resource.scene.owner == principal.id
      && resource.scene.state in ["active", "paused"] };

  # ─── Member-based read/write/resume (Phase 3 replacement) ─────────────
  - name: read-scene-as-participant
    dsl: >-
      permit(principal is character, action in ["read"], resource is scene) when { principal.id in resource.scene.participants
      };
  - name: write-scene-as-participant
    dsl: >-
      permit(principal is character, action in ["write"], resource is scene) when { principal.id in resource.scene.participants
      && resource.scene.state in ["active", "paused"] };
  - name: resume-scene-as-participant
    dsl: >-
      permit(principal is character, action in ["resume"], resource is scene) when { principal.id in resource.scene.participants
      && resource.scene.state == "paused" };

  # ─── Membership operations (Phase 3 new) ──────────────────────────────
  - name: join-open-scene
    dsl: >-
      permit(principal is character, action in ["join"], resource is scene) when { resource.scene.visibility == "open" &&
      resource.scene.state in ["active", "paused"] };
  - name: join-private-scene-as-invitee
    dsl: >-
      permit(principal is character, action in ["join"], resource is scene) when { resource.scene.visibility == "private"
      && principal.id in resource.scene.invitees && resource.scene.state in ["active", "paused"] };
  - name: leave-scene
    dsl: >-
      permit(principal is character, action in ["leave"], resource is scene) when { principal.id in resource.scene.participants
      };
  - name: invite-to-scene
    dsl: >-
      permit(principal is character, action in ["invite"], resource is scene) when { resource.scene.owner == principal.id
      && resource.scene.state in ["active", "paused"] };
  - name: kick-from-scene
    dsl: >-
      permit(principal is character, action in ["kick"], resource is scene) when { resource.scene.owner == principal.id
      && resource.scene.state in ["active", "paused"] };
  - name: transfer-ownership
    dsl: >-
      permit(principal is character, action in ["transfer-ownership"], resource is scene) when { resource.scene.owner ==
      principal.id && resource.scene.state in ["active", "paused"] };
```

The Phase 2 `read-own-scene` and `resume-own-scene` policies are removed entirely along with the load-bearing PHASE 3 NOTE comment that flagged the swap.

- [ ] **Step 2: Run the plugin manifest validation tests if they exist**

Run: `task test -- -run TestPluginManifest ./internal/plugin/...`
Expected: pass. The manifest schema validates that all referenced attributes exist; the resolver schema (Task 7) already declares `participants` and `invitees`.

- [ ] **Step 3: Lint**

Run: `task lint`
Expected: success.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "feat(scenes): replace Phase 2 policies with Phase 3 member-based ABAC

Clean break — no transitional period since plugin policies are loaded
atomically when the plugin process boots. Deletes read-own-scene and
resume-own-scene; adds 8 new member-based policies covering read, write,
resume, join (open + private), leave, invite, kick, and transfer-ownership.
Owner-only policies for end/pause/update remain unchanged.

Refs: holomush-5rh.12"
```

---

## Task 17: Metric stub additions

**Files:** `plugins/core-scenes/metrics.go`

- [ ] **Step 1: Append the new metric stubs**

Edit `plugins/core-scenes/metrics.go`. Append after the existing stubs:

```go
// metricSceneParticipantJoined counts successful joins. Labels: visibility,
// from_invited. Metric: scene_participants_joined_total{visibility, from_invited}.
func metricSceneParticipantJoined(visibility, fromInvited string) {
	_ = visibility
	_ = fromInvited
}

// metricSceneParticipantLeft counts successful leaves. Labels: visibility.
// Metric: scene_participants_left_total{visibility}.
func metricSceneParticipantLeft(visibility string) {
	_ = visibility
}

// metricSceneParticipantKicked counts successful kicks. Labels: visibility,
// prior_role. Metric: scene_participants_kicked_total{visibility, prior_role}.
func metricSceneParticipantKicked(visibility, priorRole string) {
	_ = visibility
	_ = priorRole
}

// metricSceneParticipantInvited counts successful invitations. Labels: visibility.
// Metric: scene_participants_invited_total{visibility}.
func metricSceneParticipantInvited(visibility string) {
	_ = visibility
}

// metricSceneOwnershipTransferred counts ownership transfers. Labels: visibility.
// Metric: scene_ownership_transfers_total{visibility}.
func metricSceneOwnershipTransferred(visibility string) {
	_ = visibility
}

// metricSceneOpsEventRecorded counts every ops event by kind. Catch-all for
// observability of the ops timeline. Metric: scene_ops_events_total{kind}.
func metricSceneOpsEventRecorded(kind string) {
	_ = kind
}
```

- [ ] **Step 2: Verify build**

Run: `task build`
Expected: success.

- [ ] **Step 3: Commit**

```bash
jj --no-pager commit -m "feat(scenes): add Phase 3 metric stubs for membership operations

Six new no-op metric functions (joined, left, kicked, invited,
ownership-transferred, ops-event-recorded). Stub-only per the Phase 1+2
binary-plugin-metrics convention; the actual Prometheus wiring lands
when the plugin metrics infrastructure ships.

Refs: holomush-5rh.12"
```

---

## Task 18: Integration test lockdown suite

**Files:** `plugins/core-scenes/store_integration_test.go`

These are the high-level "spec acceptance" tests from the design doc. They exercise the full ABAC + service + store stack to lock in invariants the lower-level tests don't cover individually.

- [ ] **Step 1: Write the owner-can-read-via-participant test**

Append to `store_integration_test.go`:

```go
func TestOwnerCanReadOwnSceneViaParticipantPolicy(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-locks-1", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	// CreateWithOwner must have inserted the owner participant row.
	// This is the regression-locking assertion: an owner without a
	// participant row would lose access under Phase 3's member-based
	// read policy.
	_, participants, _, err := store.GetWithMembership(ctx, row.ID)
	require.NoError(t, err)
	assert.Contains(t, participants, "char-alice", "owner must be in participants list")
}
```

- [ ] **Step 2: Run the test**

Run: `task test:int -- -run TestOwnerCanReadOwnSceneViaParticipantPolicy ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 3: Write member-can-resume test**

Append:

```go
func TestMemberCanResumePausedScene(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-locks-resume", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)
	_, err = store.Pause(ctx, row.ID)
	require.NoError(t, err)

	// char-bob is in the participants list — the resume-scene-as-participant
	// policy should permit them. Verify by reading the resolver attributes.
	_, participants, _, err := store.GetWithMembership(ctx, row.ID)
	require.NoError(t, err)
	assert.Contains(t, participants, "char-bob",
		"member must be in participants list for resume-scene-as-participant policy")
}
```

- [ ] **Step 4: Write the kicked-character-immediately-cannot-read test**

Append:

```go
func TestKickedCharacterImmediatelyDisappearsFromParticipants(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-locks-kick", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)

	// Verify char-bob is in participants pre-kick.
	_, before, _, err := store.GetWithMembership(ctx, row.ID)
	require.NoError(t, err)
	assert.Contains(t, before, "char-bob")

	// Kick char-bob.
	_, err = store.KickParticipant(ctx, row.ID, "char-alice", "char-bob")
	require.NoError(t, err)

	// IMMEDIATELY (no cache, so no TTL) char-bob is gone.
	_, after, _, err := store.GetWithMembership(ctx, row.ID)
	require.NoError(t, err)
	assert.NotContains(t, after, "char-bob",
		"kicked character must immediately disappear from participants list")
}
```

- [ ] **Step 5: Write the invitee-can-join-private + non-invitee-cannot tests**

Append:

```go
func TestInviteeCanJoinPrivateScene(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-locks-pj", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityPrivate),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, err := store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
	require.NoError(t, err)

	got, result, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)
	assert.Equal(t, OpPromoted, result)
	assert.Equal(t, "member", got.Role)
}

func TestNonInviteeCannotJoinPrivateScene(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-locks-pj-no", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityPrivate),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_JOIN_NOT_INVITED")
}
```

- [ ] **Step 6: Write the owner-cannot-leave + transfer-then-leave test**

Append:

```go
func TestOwnerCannotLeaveOwnScene(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-locks-ol", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	_, err := store.RemoveParticipant(ctx, row.ID, "char-alice")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_OWNER_CANNOT_LEAVE")
}

func TestOwnerCanTransferToMemberAndPreviousOwnerBecomesMember(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-locks-xfer", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)

	require.NoError(t, store.TransferOwnership(ctx, row.ID, "char-alice", "char-bob"))

	// Verify all three changes landed in one transaction:
	assertParticipantRowExists(t, store, row.ID, "char-alice", "member") // demoted
	assertParticipantRowExists(t, store, row.ID, "char-bob", "owner")    // promoted
	got, err := store.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, "char-bob", got.OwnerID) // denorm updated

	// Now char-alice (no longer owner) CAN leave.
	_, err = store.RemoveParticipant(ctx, row.ID, "char-alice")
	require.NoError(t, err)
	assertParticipantRowAbsent(t, store, row.ID, "char-alice")
}
```

- [ ] **Step 7: Run the lockdown suite**

Run: `task test:int -- -run "TestOwnerCanRead|TestMemberCanResume|TestKickedCharacter|TestInviteeCanJoin|TestNonInviteeCannot|TestOwnerCannotLeave|TestOwnerCanTransfer" ./plugins/core-scenes/`
Expected: 7 tests pass.

- [ ] **Step 8: Run the FULL integration suite to make sure nothing else broke**

Run: `task test:int -- ./plugins/core-scenes/`
Expected: all tests pass (Phase 1 + Phase 2 + all Phase 3 tasks).

- [ ] **Step 9: Run the FULL unit test suite**

Run: `task test -- ./plugins/core-scenes/`
Expected: all tests pass.

- [ ] **Step 10: Commit**

```bash
jj --no-pager commit -m "test(scenes): integration lockdown suite for Phase 3 ABAC swap

Locks in the spec acceptance criteria as direct integration tests:
owner-in-participants regression guard, member resume eligibility,
immediate kick visibility, private join with/without invite,
owner-cannot-leave, transfer-then-leave flow.

Refs: holomush-5rh.12"
```

---

## Task 19: Boundary and invariant tests

**Files:** `plugins/core-scenes/store_integration_test.go`

These fill gaps the lockdown suite doesn't cover: state-machine boundary cases (paused-state operations) and invariants that must hold across the system (denorm consistency, ops event counts, primary key uniqueness).

- [ ] **Step 1: Write boundary tests for paused-state operations**

Append to `store_integration_test.go`:

```go
func TestAddParticipantWorksOnPausedScene(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-bnd-pj", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, err := store.Pause(ctx, row.ID)
	require.NoError(t, err)

	// Joining a PAUSED scene must succeed (state IN ('active', 'paused')).
	got, result, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)
	assert.Equal(t, OpInserted, result)
	assert.Equal(t, "member", got.Role)
}

func TestKickParticipantWorksOnPausedScene(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-bnd-pk", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)
	_, err = store.Pause(ctx, row.ID)
	require.NoError(t, err)

	// Kicking from a PAUSED scene must succeed (no state filter on kick;
	// scene state is irrelevant to the membership operation itself).
	_, err = store.KickParticipant(ctx, row.ID, "char-alice", "char-bob")
	require.NoError(t, err)
	assertParticipantRowAbsent(t, store, row.ID, "char-bob")
}

func TestTransferOwnershipWorksOnPausedScene(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-bnd-pt", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))
	_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)
	_, err = store.Pause(ctx, row.ID)
	require.NoError(t, err)

	// Transfer must work in paused state (paused scenes can have admin
	// operations including ownership transfer).
	require.NoError(t, store.TransferOwnership(ctx, row.ID, "char-alice", "char-bob"))
	assertParticipantRowExists(t, store, row.ID, "char-bob", "owner")
	got, err := store.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, "char-bob", got.OwnerID)
	assert.Equal(t, "paused", got.State, "transfer must not affect scene state")
}

func TestInviteParticipantRejectsOwnerTarget(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-bnd-io", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityPrivate),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	// Inviting the owner must reject — they're already a participant with
	// the highest role. The existing-role check in InviteParticipant catches
	// this via the SCENE_INVITE_TARGET_ALREADY_MEMBER code path.
	_, err := store.InviteParticipant(ctx, row.ID, "char-alice", "char-alice")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_INVITE_TARGET_ALREADY_MEMBER")
	errutil.AssertErrorContext(t, err, "current_role", "owner")
}
```

- [ ] **Step 2: Run boundary tests**

Run: `task test:int -- -run "TestAddParticipantWorksOnPausedScene|TestKickParticipantWorksOnPausedScene|TestTransferOwnershipWorksOnPausedScene|TestInviteParticipantRejectsOwnerTarget" ./plugins/core-scenes/`
Expected: 4 tests pass.

- [ ] **Step 3: Write invariant test for denorm consistency**

Append:

```go
func TestSceneOwnerIDDenormAlwaysMatchesParticipantOwnerRow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Helper that asserts the invariant for a given scene at the current moment.
	assertInvariant := func(sceneID string) {
		t.Helper()
		var (
			denormOwnerID    string
			participantOwner string
		)
		require.NoError(t, store.pool.QueryRow(ctx,
			`SELECT owner_id FROM scenes WHERE id = $1`, sceneID,
		).Scan(&denormOwnerID))
		require.NoError(t, store.pool.QueryRow(ctx,
			`SELECT character_id FROM scene_participants WHERE scene_id = $1 AND role = 'owner'`,
			sceneID,
		).Scan(&participantOwner))
		assert.Equal(t, denormOwnerID, participantOwner,
			"scenes.owner_id must always match the participant row with role='owner'")
	}

	row := &SceneRow{
		ID: "scene-inv-denorm", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}

	// Initial state after CreateWithOwner.
	require.NoError(t, store.CreateWithOwner(ctx, row))
	assertInvariant(row.ID)

	// After adding a regular member — denorm unchanged.
	_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)
	assertInvariant(row.ID)

	// After ownership transfer — denorm MUST update.
	require.NoError(t, store.TransferOwnership(ctx, row.ID, "char-alice", "char-bob"))
	assertInvariant(row.ID)

	// After old owner (now member) leaves — denorm unchanged.
	_, err = store.RemoveParticipant(ctx, row.ID, "char-alice")
	require.NoError(t, err)
	assertInvariant(row.ID)
}
```

- [ ] **Step 4: Write invariant test for ops event count**

Append:

```go
func TestEachMembershipMutationProducesExactlyOneOpsEvent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	row := &SceneRow{
		ID: "scene-inv-count", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityPrivate),
		ContentWarnings: []string{}, Tags: []string{},
	}

	// CreateWithOwner produces 1 ops event: lifecycle.created.
	require.NoError(t, store.CreateWithOwner(ctx, row))
	assert.Equal(t, 1, countOpsEvents(t, store, row.ID, ""), "after create")

	// Invite produces 1 event: membership.invite.
	_, err := store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
	require.NoError(t, err)
	assert.Equal(t, 2, countOpsEvents(t, store, row.ID, ""), "after invite")

	// Re-invite is idempotent: NO new event.
	_, err = store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
	require.NoError(t, err)
	assert.Equal(t, 2, countOpsEvents(t, store, row.ID, ""), "after redundant invite")

	// Join (promotes invited→member) produces 1 event: membership.join.
	_, _, err = store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)
	assert.Equal(t, 3, countOpsEvents(t, store, row.ID, ""), "after join")

	// Retry of join (OpNoChange) produces NO new event.
	_, _, err = store.AddParticipant(ctx, row.ID, "char-bob")
	require.NoError(t, err)
	assert.Equal(t, 3, countOpsEvents(t, store, row.ID, ""), "after redundant join")

	// Kick produces 1 event: membership.kick.
	_, err = store.KickParticipant(ctx, row.ID, "char-alice", "char-bob")
	require.NoError(t, err)
	assert.Equal(t, 4, countOpsEvents(t, store, row.ID, ""), "after kick")

	// Pause + Resume each produce 1 event.
	_, err = store.Pause(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, 5, countOpsEvents(t, store, row.ID, ""), "after pause")
	_, err = store.Resume(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, 6, countOpsEvents(t, store, row.ID, ""), "after resume")

	// End produces 1 event.
	_, err = store.End(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, 7, countOpsEvents(t, store, row.ID, ""), "after end")
}
```

- [ ] **Step 5: Write invariant test for primary key uniqueness**

Append:

```go
func TestParticipantPrimaryKeyPreventsDoubleInsertion(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	row := &SceneRow{
		ID: "scene-inv-pk", OwnerID: "char-alice", Title: "T",
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen),
		ContentWarnings: []string{}, Tags: []string{},
	}
	require.NoError(t, store.CreateWithOwner(ctx, row))

	// Direct insert of a duplicate row MUST fail at the PK constraint.
	// This proves the schema, not the store API, prevents duplicates.
	_, err := store.pool.Exec(ctx,
		`INSERT INTO scene_participants (scene_id, character_id, role) VALUES ($1, $2, 'member')`,
		row.ID, "char-alice", // char-alice is already the owner row
	)
	require.Error(t, err, "duplicate (scene_id, character_id) must violate PK")

	// Verify exactly one row exists for char-alice.
	var count int
	require.NoError(t, store.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
		row.ID, "char-alice",
	).Scan(&count))
	assert.Equal(t, 1, count)
}
```

- [ ] **Step 6: Write invariant test for ops event immutability**

Append:

```go
func TestOpsEventsCannotBeUpdatedOrDeletedByApplicationCode(t *testing.T) {
	// This is a meta-test: assert that the codebase contains no UPDATE or
	// DELETE statements against scene_ops_events. Append-only is enforced by
	// convention (the recordOpsEventTx helper is the only writer); this test
	// catches accidental future violations via grep.
	//
	// The check uses os.ReadFile + strings.Contains rather than running an
	// external grep so it works in any test environment.
	files := []string{
		"store.go",
		"ops_events.go",
		"service.go",
		"resolver.go",
		"commands.go",
		"main.go",
	}
	for _, fname := range files {
		data, err := os.ReadFile(fname)
		if err != nil {
			continue // file doesn't exist in this layout
		}
		content := string(data)
		assert.NotContains(t, content, "UPDATE scene_ops_events",
			"%s contains UPDATE on scene_ops_events — events must be immutable", fname)
		assert.NotContains(t, content, "DELETE FROM scene_ops_events",
			"%s contains DELETE on scene_ops_events — events must be immutable", fname)
	}
}
```

You'll need to add `"os"` to the imports of `store_integration_test.go` if it isn't already there.

- [ ] **Step 7: Run all invariant tests**

Run: `task test:int -- -run "TestSceneOwnerIDDenorm|TestEachMembershipMutation|TestParticipantPrimaryKey|TestOpsEventsCannotBe" ./plugins/core-scenes/`
Expected: 4 tests pass.

- [ ] **Step 8: Run the full integration suite to confirm nothing else broke**

Run: `task test:int -- ./plugins/core-scenes/`
Expected: all tests pass.

- [ ] **Step 9: Commit**

```bash
jj --no-pager commit -m "test(scenes): boundary and invariant tests for Phase 3

Boundary tests: paused-state join/kick/transfer (paused scenes can still
have membership ops); invite-owner rejection.

Invariant tests:
- scenes.owner_id always matches the participant row with role='owner'
  (cross-table consistency, defends against TransferOwnership txn bugs)
- Each membership mutation produces exactly one ops event; idempotent
  retries produce zero (audit log integrity)
- Primary key prevents double-insertion of (scene_id, character_id)
- scene_ops_events table is never UPDATE'd or DELETE'd by application
  code (append-only invariant via static check)

Refs: holomush-5rh.12"
```

---

## Task 20: E2E binary plugin tests in `test/integration/plugin/binary_plugin_test.go`

**Files:** `test/integration/plugin/binary_plugin_test.go`

This task adds Phase 3 membership coverage to the existing binary plugin E2E suite. These tests build the actual `core-scenes` binary, start it via go-plugin (real subprocess boundary), make gRPC calls through the host's proxy (real wire marshalling), and validate DB state via direct SQL.

This catches problems the in-process integration tests miss: plugin manifest validation, gRPC marshalling at the plugin boundary, schema isolation, host-plugin context propagation.

- [ ] **Step 1: Inspect the existing binary_plugin_test.go structure**

Run: `wc -l test/integration/plugin/binary_plugin_test.go`
Expected: a substantial file (multiple `Describe` blocks for Phase 1+2 scene operations).

Read the existing file with the Read tool to understand the patterns it uses for:
- Setting up the host with the core-scenes plugin
- Getting a SceneServiceClient
- Acquiring a pgxpool to the plugin's schema
- Cleaning up between tests

The new Phase 3 tests reuse this same harness — no new setup code is needed.

- [ ] **Step 2: Add a "Phase 3 Membership" Describe block**

Append to `test/integration/plugin/binary_plugin_test.go`, just before the final closing brace of the file (after the existing Describe blocks):

```go
var _ = Describe("Phase 3 Membership", func() {
	var (
		ctx       context.Context
		cancel    context.CancelFunc
		container testcontainers.Container
		connStr   string
		host      *plugins.Host
		client    scenev1.SceneServiceClient
		pool      *pgxpool.Pool
	)

	BeforeEach(func() {
		// Reuse the same setup pattern as the existing Describe blocks.
		// Skip if the binary isn't built; check + setup match the existing
		// BeforeEach in this file.
		pluginDir, binaryPath := coreScenesBinaryPath()
		if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
			Skip(fmt.Sprintf("core-scenes binary not found at %s — run task plugin:build-all first", binaryPath))
		}

		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)

		var err error
		container, connStr, err = testutil.StartPostgresContainer(ctx)
		Expect(err).NotTo(HaveOccurred())

		host, client, pool, err = setupHostWithCoreScenes(ctx, pluginDir, connStr)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if host != nil {
			_ = host.Shutdown(ctx)
		}
		if pool != nil {
			pool.Close()
		}
		if container != nil {
			termCtx, termCancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = container.Terminate(termCtx)
			termCancel()
		}
		cancel()
	})

	Describe("Full membership lifecycle over the binary plugin boundary", func() {
		It("supports create→invite→join→kick→reinvite→join→transfer→leave", func() {
			// 1. Create a private scene as char-alice.
			createResp, err := client.CreateScene(ctx, &scenev1.CreateSceneRequest{
				CharacterId: "char-alice",
				Title:       "E2E Test Scene",
				Visibility:  "private",
			})
			Expect(err).NotTo(HaveOccurred())
			sceneID := createResp.GetScene().GetId()
			Expect(sceneID).To(HavePrefix("scene-"))

			// DB validation: owner participant row inserted by CreateWithOwner.
			var ownerRole string
			Expect(pool.QueryRow(ctx,
				`SELECT role FROM plugin_core_scenes.scene_participants WHERE scene_id = $1 AND character_id = $2`,
				sceneID, "char-alice",
			).Scan(&ownerRole)).To(Succeed())
			Expect(ownerRole).To(Equal("owner"))

			// DB validation: lifecycle.created ops event recorded.
			var createdEventCount int
			Expect(pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM plugin_core_scenes.scene_ops_events WHERE scene_id = $1 AND kind = 'lifecycle.created'`,
				sceneID,
			).Scan(&createdEventCount)).To(Succeed())
			Expect(createdEventCount).To(Equal(1))

			// 2. Invite char-bob to the private scene.
			_, err = client.InviteToScene(ctx, &scenev1.InviteToSceneRequest{
				CharacterId:       "char-alice",
				SceneId:           sceneID,
				TargetCharacterId: "char-bob",
			})
			Expect(err).NotTo(HaveOccurred())

			// DB validation: invited row exists.
			var bobRole string
			Expect(pool.QueryRow(ctx,
				`SELECT role FROM plugin_core_scenes.scene_participants WHERE scene_id = $1 AND character_id = $2`,
				sceneID, "char-bob",
			).Scan(&bobRole)).To(Succeed())
			Expect(bobRole).To(Equal("invited"))

			// 3. char-bob joins (promotes invited→member).
			_, err = client.JoinScene(ctx, &scenev1.JoinSceneRequest{
				CharacterId: "char-bob",
				SceneId:     sceneID,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(pool.QueryRow(ctx,
				`SELECT role FROM plugin_core_scenes.scene_participants WHERE scene_id = $1 AND character_id = $2`,
				sceneID, "char-bob",
			).Scan(&bobRole)).To(Succeed())
			Expect(bobRole).To(Equal("member"))

			// DB validation: membership.join ops event with from_invited=true.
			var joinPayload []byte
			Expect(pool.QueryRow(ctx,
				`SELECT payload FROM plugin_core_scenes.scene_ops_events
				 WHERE scene_id = $1 AND kind = 'membership.join' AND target_id = $2`,
				sceneID, "char-bob",
			).Scan(&joinPayload)).To(Succeed())
			Expect(string(joinPayload)).To(ContainSubstring(`"from_invited": true`))

			// 4. char-alice (owner) kicks char-bob.
			_, err = client.KickFromScene(ctx, &scenev1.KickFromSceneRequest{
				CharacterId:       "char-alice",
				SceneId:           sceneID,
				TargetCharacterId: "char-bob",
			})
			Expect(err).NotTo(HaveOccurred())

			// DB validation: char-bob row gone.
			err = pool.QueryRow(ctx,
				`SELECT role FROM plugin_core_scenes.scene_participants WHERE scene_id = $1 AND character_id = $2`,
				sceneID, "char-bob",
			).Scan(&bobRole)
			Expect(err).To(MatchError(ContainSubstring("no rows")))

			// 5. Re-invite + re-join.
			_, err = client.InviteToScene(ctx, &scenev1.InviteToSceneRequest{
				CharacterId: "char-alice", SceneId: sceneID, TargetCharacterId: "char-bob",
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = client.JoinScene(ctx, &scenev1.JoinSceneRequest{
				CharacterId: "char-bob", SceneId: sceneID,
			})
			Expect(err).NotTo(HaveOccurred())

			// 6. char-alice transfers ownership to char-bob.
			_, err = client.TransferOwnership(ctx, &scenev1.TransferOwnershipRequest{
				CharacterId:         "char-alice",
				SceneId:             sceneID,
				NewOwnerCharacterId: "char-bob",
			})
			Expect(err).NotTo(HaveOccurred())

			// DB validation: char-bob is now owner, char-alice is member,
			// scenes.owner_id is denormalised correctly.
			Expect(pool.QueryRow(ctx,
				`SELECT role FROM plugin_core_scenes.scene_participants WHERE scene_id = $1 AND character_id = $2`,
				sceneID, "char-bob",
			).Scan(&bobRole)).To(Succeed())
			Expect(bobRole).To(Equal("owner"))

			var aliceRole string
			Expect(pool.QueryRow(ctx,
				`SELECT role FROM plugin_core_scenes.scene_participants WHERE scene_id = $1 AND character_id = $2`,
				sceneID, "char-alice",
			).Scan(&aliceRole)).To(Succeed())
			Expect(aliceRole).To(Equal("member"))

			var denormOwner string
			Expect(pool.QueryRow(ctx,
				`SELECT owner_id FROM plugin_core_scenes.scenes WHERE id = $1`,
				sceneID,
			).Scan(&denormOwner)).To(Succeed())
			Expect(denormOwner).To(Equal("char-bob"))

			// 7. char-alice (now member) can leave.
			_, err = client.LeaveScene(ctx, &scenev1.LeaveSceneRequest{
				CharacterId: "char-alice",
				SceneId:     sceneID,
			})
			Expect(err).NotTo(HaveOccurred())

			err = pool.QueryRow(ctx,
				`SELECT role FROM plugin_core_scenes.scene_participants WHERE scene_id = $1 AND character_id = $2`,
				sceneID, "char-alice",
			).Scan(&aliceRole)
			Expect(err).To(MatchError(ContainSubstring("no rows")))
		})

		It("rejects owner leave with FailedPrecondition over the wire", func() {
			createResp, err := client.CreateScene(ctx, &scenev1.CreateSceneRequest{
				CharacterId: "char-alice",
				Title:       "Owner-leave test",
				Visibility:  "open",
			})
			Expect(err).NotTo(HaveOccurred())
			sceneID := createResp.GetScene().GetId()

			_, err = client.LeaveScene(ctx, &scenev1.LeaveSceneRequest{
				CharacterId: "char-alice",
				SceneId:     sceneID,
			})
			Expect(err).To(HaveOccurred())
			st, ok := status.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(codes.FailedPrecondition))
			Expect(st.Message()).To(ContainSubstring("owners cannot leave"))
		})

		It("rejects join to a private scene without invitation with PermissionDenied", func() {
			createResp, err := client.CreateScene(ctx, &scenev1.CreateSceneRequest{
				CharacterId: "char-alice",
				Title:       "Private join test",
				Visibility:  "private",
			})
			Expect(err).NotTo(HaveOccurred())
			sceneID := createResp.GetScene().GetId()

			_, err = client.JoinScene(ctx, &scenev1.JoinSceneRequest{
				CharacterId: "char-bob",
				SceneId:     sceneID,
			})
			Expect(err).To(HaveOccurred())
			st, ok := status.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(codes.PermissionDenied))
		})

		It("rejects transfer to a non-member with FailedPrecondition", func() {
			createResp, err := client.CreateScene(ctx, &scenev1.CreateSceneRequest{
				CharacterId: "char-alice",
				Title:       "Transfer test",
				Visibility:  "open",
			})
			Expect(err).NotTo(HaveOccurred())
			sceneID := createResp.GetScene().GetId()

			_, err = client.TransferOwnership(ctx, &scenev1.TransferOwnershipRequest{
				CharacterId:         "char-alice",
				SceneId:             sceneID,
				NewOwnerCharacterId: "char-bob",
			})
			Expect(err).To(HaveOccurred())
			st, ok := status.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(codes.FailedPrecondition))
		})
	})
})
```

The `setupHostWithCoreScenes` helper already exists in the file from Phase 1+2 work. If the existing helper signature differs from what's used above, adapt the test code to match — the helper returns whatever the existing tests use for `host`, `client`, `pool`. Read the existing Describe blocks to see the exact pattern.

- [ ] **Step 3: Build the plugin binary**

Run: `task plugin:build-all`
Expected: success. Outputs `build/plugins/core-scenes/<os>-<arch>/core-scenes`.

- [ ] **Step 4: Run the new Phase 3 binary plugin tests**

Run: `task test:int -- -run "Phase 3 Membership" ./test/integration/plugin/...`
Expected: 4 specs pass (the full lifecycle + 3 error-mapping specs).

If specs fail with `setupHostWithCoreScenes undefined` or similar, read the existing binary_plugin_test.go to find the actual helper name and adapt.

- [ ] **Step 5: Run the full binary plugin suite to confirm no regressions**

Run: `task test:int -- ./test/integration/plugin/...`
Expected: all specs pass (Phase 1+2 + Phase 3).

- [ ] **Step 6: Commit**

```bash
jj --no-pager commit -m "test(scenes): E2E binary plugin tests for Phase 3 membership

Adds 'Phase 3 Membership' Describe block to binary_plugin_test.go that
exercises the full membership lifecycle (create→invite→join→kick→
reinvite→join→transfer→leave) over the actual go-plugin subprocess
boundary, with direct DB verification of scene_participants and
scene_ops_events at every step.

Plus three error-mapping specs that verify the gRPC status codes
travel correctly across the wire: owner-cannot-leave (FailedPrecondition),
non-invitee-private-join (PermissionDenied), transfer-non-member
(FailedPrecondition).

Closes the gap where store-layer integration tests didn't exercise the
plugin manifest, gRPC marshalling, or schema isolation.

Refs: holomush-5rh.12"
```

---

## Task 21: `task pr-prep` verification + final commit

**Files:** none (verification only)

This task runs the full CI mirror locally and addresses anything it surfaces. Per `feedback_pr_prep_must_run`, the full `task pr-prep` MUST run; subset checks are not acceptable.

- [ ] **Step 1: Run `task pr-prep`**

Run: `task pr-prep`
Expected: all jobs pass — lint, format, schema, license, unit tests, integration tests, E2E tests. This may take 10–15 minutes; do NOT abort early.

If any job fails:
- **Lint:** Fix the issue. Do NOT add lint suppressions without justification. If a fix isn't straightforward, ask for guidance.
- **Format:** Run `task fmt` and re-run pr-prep.
- **License:** Run `task license:add` to add SPDX headers to any new files missing them.
- **Schema:** A migration was forgotten or misnamed. Recheck Task 2.
- **Unit / integration / E2E:** Investigate the failure with the systematic-debugging skill before patching.

- [ ] **Step 2: Verify the bead is still claimed and update with completion notes**

Run: `bd show holomush-5rh.12`
Expected: status=in_progress.

Update with a brief completion note:

```bash
bd update holomush-5rh.12 --notes "Phase 3 implementation complete. PR pending. All 21 tasks landed via TDD; task pr-prep green; full integration suite passes including E2E binary plugin tests. Spec at docs/superpowers/specs/2026-04-07-scenes-phase-3-membership-design.md, plan at docs/superpowers/plans/2026-04-07-scenes-phase-3-membership.md."
```

- [ ] **Step 3: Push the branch and create the PR**

Per the project's PR workflow with the SSL workaround for the holomush GitHub setup:

```bash
jj --no-pager bookmark set scenes-phase-3-membership -r @-
GIT_SSL_NO_VERIFY=1 jj --no-pager git push -b scenes-phase-3-membership

GIT_SSL_NO_VERIFY=1 gh pr create --head scenes-phase-3-membership --title "feat(scenes): Phase 3 — membership, ops events, and ABAC policy swap" --body "$(cat <<'EOF'
## Summary

Phase 3 of Epic 9 (Scenes & RP). Adds the participant model and membership operations to core-scenes, replaces Phase 2 owner-only ABAC policies with member-based policies, and retrofits Phase 1+2 lifecycle handlers to emit ops events.

Refs: holomush-5rh.12

## Spec & Plan

- Spec: docs/superpowers/specs/2026-04-07-scenes-phase-3-membership-design.md
- Plan: docs/superpowers/plans/2026-04-07-scenes-phase-3-membership.md

## Schema

Migration 000003 adds two tables:
- scene_participants — membership snapshot (composite PK, role enum)
- scene_ops_events — append-only ops journal (membership + lifecycle + settings)

Both have ON DELETE CASCADE on scene_id. The ops table uses a CHECK regex
for kind format validation (\`^[a-z]+\.[a-z_]+\$\`), letting future phases
add new kinds without migrations.

## Behavioural changes

- Owner is now always a participant. CreateScene becomes transactional and
  inserts the owner row + lifecycle.created event atomically.
- Read/write/resume policies switch from owner-only to member-based.
- Members can resume paused scenes (D6 async safety).
- Owners cannot leave; \`scene transfer <character>\` is the new escape hatch.
- Kick removes both member and invited rows.
- Join is idempotent and atomically promotes invited→member.

## Test plan

- [x] All unit tests in plugins/core-scenes/ pass
- [x] All integration tests pass (tests/Phase1+2 + ~24 new Phase 3 tests)
- [x] task pr-prep green
- [x] Direct DB verification of scene_participants and scene_ops_events rows
- [x] ABAC swap regression tests (owner can still read/resume own scene)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 4: Verify the PR was created**

Run: `gh pr list --limit 5 --json number,title,state`
Expected: the new PR appears at the top.

- [ ] **Step 5: Final commit (only if any pr-prep fixes were needed)**

If steps in Task 19 required code changes (lint, format, license fixes), commit them:

```bash
jj --no-pager commit -m "chore(scenes): pr-prep cleanup for Phase 3

Lint/format/license fixes surfaced by task pr-prep.

Refs: holomush-5rh.12"
```

If no fixes were needed, no commit is necessary — proceed to push.

---

## Self-Review Checklist

After completing all 21 tasks, verify:

- [ ] Every spec acceptance criterion has a corresponding test (run `grep -r "SCENE_" plugins/core-scenes/store_integration_test.go` and confirm coverage of all error codes introduced)
- [ ] No `TODO`, `TBD`, or `FIXME` markers introduced in production code
- [ ] All new files have SPDX license headers (verified by `task license:check`)
- [ ] All new tests follow ACE naming (no `TestFooSuccess`, `TestBarError` style)
- [ ] No magic strings — `ParticipantRoleMember` constant used instead of `"member"` string literal
- [ ] `crypto/rand` used for ULID generation in `newOpsEventID`, never `math/rand`
- [ ] Every store method that mutates rows uses `tx.Begin/Commit/Rollback` for atomicity with its ops event
- [ ] `recordOpsEventTx` is the ONLY entry point for ops event writes — no raw INSERTs into `scene_ops_events` from handlers
- [ ] `scene_participants` mutations defended by `WHERE role <> 'owner'` where applicable (RemoveParticipant, KickParticipant)
- [ ] Phase 2 `read-own-scene` and `resume-own-scene` policies are deleted (not commented out) from `plugin.yaml`

---

## Type Consistency Check

| Type / function | First defined | Used by |
|---|---|---|
| `ParticipantRole` | Task 2 | All store methods, fakeStore, integration tests |
| `ParticipantRow` | Task 2 | All membership store methods, fakeStore |
| `ParticipantOpResult` (`OpInserted`/`OpPromoted`/`OpNoChange`) | Task 2 | `AddParticipant` return, JoinScene handler |
| `OpsEventKind` constants | Task 2 | `recordOpsEventTx`, all store methods |
| `recordOpsEventTx` | Task 2 | Tasks 5, 8, 9, 10, 11, 12, 14 |
| `CreateWithOwner` | Task 5 | `CreateScene` handler, all integration tests via `mustCreateSceneWithOwner` |
| `GetWithMembership` | Task 6 | `ResolveResource`, lockdown tests |
| `AddParticipant` | Task 8 | `JoinScene`, integration tests |
| `RemoveParticipant` | Task 9 | `LeaveScene`, integration tests |
| `InviteParticipant` | Task 10 | `InviteToScene`, integration tests |
| `KickParticipant` | Task 11 | `KickFromScene`, integration tests |
| `TransferOwnership` (store) | Task 12 | `TransferOwnership` service handler |
| `classifyJoinMiss` | Task 8 | `AddParticipant` error path |
| `classifyTransferMiss` | Task 12 | `TransferOwnership` error path |

All names are consistent across the plan. All functions referenced in later tasks are defined in earlier tasks.
