# Scenes Phase 6 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `dev-flow:subagent-driven-development` (recommended) or `dev-flow:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship Phase 6 of the scenes rework — publish vote state machine, publication artifact distinct from the existing audit scene_log, two-pair RPC surface (participant INV-S9-gated and public status-gated), snapshot pipeline, three renderer formats, and the `scene log` / `scene log export` commands folded in from `holomush-cb4x`.

**Architecture:** New `published_scenes` table + `published_scene_votes` roster table; nine new RPCs split across two structurally separate handler groups so INV-S9 cannot regress via refactor; package-level metric stubs matching the existing Phase 2 pattern; vote events emitted as `sensitivity:never` IC stream notices joining the `scene_join_ic` family.

**Tech Stack:** Go 1.24+, PostgreSQL via pgx/v5, JetStream via NATS, gRPC via hashicorp/go-plugin, Ginkgo/Gomega for integration + E2E, testify for unit, testcontainers-go for PG, mockery for unit mocks.

**Spec:** [`docs/superpowers/specs/2026-05-23-scenes-phase-6-logs-vote-privacy-design.md`](../specs/2026-05-23-scenes-phase-6-logs-vote-privacy-design.md)

**Beads:** Implementation tracker `holomush-5rh.15`; design tracker `holomush-5rh.20`; folds in `holomush-cb4x`.

---

## File Structure

### New files (`plugins/core-scenes/`)

| Path | Purpose |
| ---- | ------- |
| `migrations/000008_scene_publication.up.sql` + `.down.sql` | `published_scenes` + `published_scene_votes` tables + indexes |
| `migrations/000009_scene_max_publish_attempts.up.sql` + `.down.sql` | `scenes.max_publish_attempts` column |
| `publish_types.go` | Domain types (`PublishedScene`, `PublishedSceneStatus`, `PublishFailureReason`, `PublishedSceneVote`, `Entry`, `EntryKind`) |
| `publish_store.go` | Store methods for `published_scenes` + `published_scene_votes` + `ReadSceneLogForSnapshot` |
| `publish_state.go` | State machine helpers (transition guards, vote tally, resolution check) |
| `publish_service.go` | RPC handlers for the 9 new RPCs |
| `publish_snapshot.go` | Snapshot pipeline (COOLOFF → PUBLISHED) |
| `publish_render.go` | Markdown / plain-text / jsonl renderers + entry shape |
| `publish_events.go` | Event emission helpers for the 6 new IC notice types |
| `publish_scheduler.go` | Vote-window timeout + cool-off expiry ticker integration |
| `publish_types_test.go` | Unit tests for types |
| `publish_state_test.go` | State machine unit tests |
| `publish_vote_tally_test.go` | Tally unit tests |
| `publish_roster_test.go` | Roster freeze unit tests |
| `publish_render_test.go` | Renderer unit tests |
| `publish_state_integration_test.go` | Lifecycle integration tests |
| `publish_snapshot_integration_test.go` | Snapshot atomicity + decrypt tests |
| `publish_resolver_integration_test.go` | INV-P6-7 resolver no-leak meta test |
| `publish_event_emission_integration_test.go` | Event emission integration tests |
| `service_publish_gate_test.go` | INV-S9 call-stack tripwire tests |
| `service_public_archive_test.go` | INV-P6-8 opacity tests |
| `service_privacy_block_test.go` | INV-P6-9 triple-signal tests |
| `commands_resolve_test.go` | resolveSceneRef unit tests |

### Modified files

| Path | Change |
| ---- | ------ |
| `api/proto/holomush/scene/v1/scene.proto` | Add 9 RPC methods + new request/response messages |
| `pkg/proto/holomush/scene/v1/*.pb.go` | Regenerated from `task proto` |
| `plugins/core-scenes/plugin.yaml` | Add 6 `crypto.emits` entries (sensitivity:never) + 3 ABAC policies + new command help blocks |
| `plugins/core-scenes/commands.go` | Add `scene publish *` + `scene log *` subcommand handlers + `resolveSceneRef` helper |
| `plugins/core-scenes/metrics.go` | Add 7 package-level no-op stub functions per spec §13.1 |
| `plugins/core-scenes/main.go` | Wire `publish_scheduler` into plugin lifecycle |
| `plugins/core-scenes/service.go` | Add public RPCs to `SceneServiceImpl` (delegating to `publish_service.go`) |
| `plugins/core-scenes/resolver.go` | UNCHANGED (INV-P6-7 forbids adding content attrs); resolver_test.go gets regression-lock meta test |

### Cross-package new test files

| Path | Purpose |
| ---- | ------- |
| `test/integration/scenes/publish_e2e_test.go` | Full-stack E2E happy + sad paths |
| `test/integration/scenes/publish_history_scope_e2e_test.go` | iwzt floor interaction with publish events |
| `test/meta/scenes_phase6_invariants_test.go` | INV-P6-1..10 coverage enumeration |

---

## Phase A — Schema, Types, Store, State Machine

Phase A produces the foundational substrate. By end of Phase A, the migrations apply cleanly, domain types compile, the store can CRUD `published_scenes` + `published_scene_votes`, and the state machine helpers exist with unit-test coverage. No RPCs or commands yet.

**Spec coverage:** §3 Domain Model, §4 State Machine, §3.3 Migration, §3.4 Configuration.

### Task A1: Migration 000008 — `published_scenes` + `published_scene_votes` tables

**Files:**

- Create: `plugins/core-scenes/migrations/000008_scene_publication.up.sql`
- Create: `plugins/core-scenes/migrations/000008_scene_publication.down.sql`

**Spec ref:** §3.3

- [ ] **Step 1: Write the up migration**

Content of `000008_scene_publication.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 6 (holomush-5rh.15): publication artifact distinct from the
-- audit scene_log table. See spec section 3.3.

CREATE TABLE IF NOT EXISTS published_scenes (
    id                     TEXT        PRIMARY KEY,
    scene_id               TEXT        NOT NULL,
    attempt_number         INTEGER     NOT NULL,
    status                 TEXT        NOT NULL CHECK (status IN
                              ('COLLECTING','COOLOFF','PUBLISHED','ATTEMPT_FAILED')),
    initiated_by           TEXT        NOT NULL,
    initiated_at           TIMESTAMPTZ NOT NULL,
    cooloff_started_at     TIMESTAMPTZ,
    resolved_at            TIMESTAMPTZ,
    vote_window            INTERVAL    NOT NULL,
    cooloff_window         INTERVAL    NOT NULL,
    max_attempts_snapshot  INTEGER     NOT NULL,
    content_entries        JSONB,
    title_snapshot         TEXT,
    participants_snapshot  JSONB,
    published_at           TIMESTAMPTZ,
    failure_reason         TEXT        CHECK (failure_reason IS NULL OR failure_reason IN
                              ('ANY_NO','TIMEOUT','WITHDRAWN',
                               'SNAPSHOT_DECRYPT_FAILED','SNAPSHOT_RENDER_FAILED',
                               'COOLOFF_INVARIANT_BROKEN'))
);

CREATE UNIQUE INDEX IF NOT EXISTS published_scenes_one_active_per_scene
    ON published_scenes(scene_id) WHERE status IN ('COLLECTING','COOLOFF');
CREATE UNIQUE INDEX IF NOT EXISTS published_scenes_one_published_per_scene
    ON published_scenes(scene_id) WHERE status = 'PUBLISHED';
CREATE UNIQUE INDEX IF NOT EXISTS published_scenes_attempt_unique
    ON published_scenes(scene_id, attempt_number);
CREATE INDEX IF NOT EXISTS published_scenes_scene_status
    ON published_scenes(scene_id, status);

CREATE TABLE IF NOT EXISTS published_scene_votes (
    published_scene_id  TEXT        NOT NULL,
    character_id        TEXT        NOT NULL,
    vote                BOOLEAN,
    voted_at            TIMESTAMPTZ,
    last_changed_at     TIMESTAMPTZ,
    PRIMARY KEY (published_scene_id, character_id)
);

CREATE INDEX IF NOT EXISTS published_scene_votes_pending
    ON published_scene_votes(published_scene_id) WHERE vote IS NULL;
```

- [ ] **Step 2: Write the down migration**

Content of `000008_scene_publication.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS published_scene_votes_pending;
DROP TABLE IF EXISTS published_scene_votes;

DROP INDEX IF EXISTS published_scenes_scene_status;
DROP INDEX IF EXISTS published_scenes_attempt_unique;
DROP INDEX IF EXISTS published_scenes_one_published_per_scene;
DROP INDEX IF EXISTS published_scenes_one_active_per_scene;
DROP TABLE IF EXISTS published_scenes;
```

- [ ] **Step 3: Run migration lint**

Run: `task lint:migrations`
Expected: PASS (logic-free SQL only; idempotent IF NOT EXISTS).

- [ ] **Step 4: Run plugin migration smoke test**

Run: `task test:int -- ./plugins/core-scenes/ -run TestStoreOpensWithMigrations`
Expected: PASS — `NewSceneStore` opens against a fresh DB and applies all migrations including 000008. (This test pre-exists in `store_integration_test.go`; it will pick up the new migration automatically.)

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scene-migrations): add 000008 published_scenes + published_scene_votes

Phase 6 schema for publication artifact distinct from audit scene_log
(holomush-5rh.15). See spec section 3.3."
```

### Task A2: Migration 000009 — `scenes.max_publish_attempts` column

**Files:**

- Create: `plugins/core-scenes/migrations/000009_scene_max_publish_attempts.up.sql`
- Create: `plugins/core-scenes/migrations/000009_scene_max_publish_attempts.down.sql`

**Spec ref:** §3.4

- [ ] **Step 1: Write the up migration**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 6: per-scene configurable max publish attempts. Default 3;
-- admin can bump via ExtendScenePublishVoteAttempts RPC. See spec §3.4.

ALTER TABLE scenes
    ADD COLUMN IF NOT EXISTS max_publish_attempts INTEGER NOT NULL DEFAULT 3;
```

- [ ] **Step 2: Write the down migration**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE scenes DROP COLUMN IF EXISTS max_publish_attempts;
```

- [ ] **Step 3: Verify migration applies**

Run: `task test:int -- ./plugins/core-scenes/ -run TestStoreOpensWithMigrations`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(scene-migrations): add 000009 scenes.max_publish_attempts

Per-scene retry budget for publish vote attempts (holomush-5rh.15)."
```

### Task A3: Proto contract — extend `holomush.scene.v1.SceneService`

**Files:**

- Modify: `api/proto/holomush/scene/v1/scene.proto`

**Spec ref:** §5

- [ ] **Step 1: Add the 9 new RPC declarations**

Append to the `service SceneService` block in `scene.proto`:

```protobuf
  // Phase 6 publication RPCs. See spec section 5.
  rpc StartScenePublish(StartScenePublishRequest) returns (StartScenePublishResponse);
  rpc CastPublishSceneVote(CastPublishSceneVoteRequest) returns (CastPublishSceneVoteResponse);
  rpc WithdrawScenePublish(WithdrawScenePublishRequest) returns (WithdrawScenePublishResponse);
  rpc GetPublishedScene(GetPublishedSceneRequest) returns (GetPublishedSceneResponse);
  rpc DownloadPublishedScene(DownloadPublishedSceneRequest) returns (DownloadPublishedSceneResponse);
  rpc ListScenePublishAttempts(ListScenePublishAttemptsRequest) returns (ListScenePublishAttemptsResponse);
  rpc GetPublicSceneArchive(GetPublicSceneArchiveRequest) returns (GetPublicSceneArchiveResponse);
  rpc DownloadPublicSceneArchive(DownloadPublicSceneArchiveRequest) returns (DownloadPublicSceneArchiveResponse);
  rpc ExtendScenePublishVoteAttempts(ExtendScenePublishVoteAttemptsRequest) returns (ExtendScenePublishVoteAttemptsResponse);
```

- [ ] **Step 2: Add request/response messages**

Append new messages to `scene.proto` (full message definitions; spec §5 enumerates field requirements):

```protobuf
message StartScenePublishRequest {
  string scene_id = 1;
}
message StartScenePublishResponse {
  string published_scene_id = 1;
  int32 attempt_number = 2;
}

message CastPublishSceneVoteRequest {
  string published_scene_id = 1;
  bool vote = 2;
}
message CastPublishSceneVoteResponse {
  bool is_change = 1;
}

message WithdrawScenePublishRequest {
  string published_scene_id = 1;
}
message WithdrawScenePublishResponse {}

message PublishedSceneEntry {
  string speaker = 1;
  string kind = 2;     // "pose" | "say" | "emit"
  string content = 3;
}

message PublishedSceneVoteSummary {
  int32 yes = 1;
  int32 no = 2;
  int32 pending = 3;
}

message GetPublishedSceneRequest {
  string published_scene_id = 1;
}
message GetPublishedSceneResponse {
  string id = 1;
  string scene_id = 2;
  int32 attempt_number = 3;
  string status = 4;
  string failure_reason = 5;       // empty unless ATTEMPT_FAILED
  PublishedSceneVoteSummary tally = 6;
  repeated PublishedSceneEntry content_entries = 7;  // populated only when PUBLISHED
  string title_snapshot = 8;
  repeated string participants_snapshot = 9;
  int64 initiated_at_unix_ns = 10;
  int64 cooloff_started_at_unix_ns = 11;
  int64 resolved_at_unix_ns = 12;
  int64 published_at_unix_ns = 13;
}

message DownloadPublishedSceneRequest {
  string published_scene_id = 1;
  string format = 2;   // "markdown" | "plain_text" | "jsonl"
}
message DownloadPublishedSceneResponse {
  bytes content = 1;
  string mime_type = 2;
}

message ListScenePublishAttemptsRequest {
  string scene_id = 1;
}
message ListScenePublishAttemptsResponse {
  repeated PublishedSceneSummary attempts = 1;
}
message PublishedSceneSummary {
  string id = 1;
  int32 attempt_number = 2;
  string status = 3;
  string failure_reason = 4;
  int64 initiated_at_unix_ns = 5;
  int64 resolved_at_unix_ns = 6;
}

message GetPublicSceneArchiveRequest {
  string published_scene_id = 1;
}
message GetPublicSceneArchiveResponse {
  string id = 1;
  string title_snapshot = 2;
  repeated string participants_snapshot = 3;
  repeated PublishedSceneEntry content_entries = 4;
  int64 published_at_unix_ns = 5;
}

message DownloadPublicSceneArchiveRequest {
  string published_scene_id = 1;
  string format = 2;
}
message DownloadPublicSceneArchiveResponse {
  bytes content = 1;
  string mime_type = 2;
}

message ExtendScenePublishVoteAttemptsRequest {
  string scene_id = 1;
  int32 additional = 2;
}
message ExtendScenePublishVoteAttemptsResponse {
  int32 new_max = 1;
}
```

- [ ] **Step 3: Regenerate Go bindings**

Run: `task proto`
Expected: `pkg/proto/holomush/scene/v1/scene.pb.go` + `scene_grpc.pb.go` regenerated cleanly.

- [ ] **Step 4: Verify compilation**

Run: `task build`
Expected: PASS — generated bindings compile; existing handlers may need stub implementations to satisfy the interface (added in Phase B; for now add `panic("not implemented in phase A")` stubs to `service.go` per Step 5).

- [ ] **Step 5: Add not-yet-implemented stubs to `SceneServiceImpl`**

In `plugins/core-scenes/service.go`, append after the existing handlers (line 273 region):

```go
// Phase 6 RPC stubs — implemented in Phase B (publish_service.go).
// Present here only so the generated server interface compiles.

func (s *SceneServiceImpl) StartScenePublish(ctx context.Context, req *scenev1.StartScenePublishRequest) (*scenev1.StartScenePublishResponse, error) {
    return nil, status.Error(codes.Unimplemented, "not yet implemented")
}
// Repeat for each of the other 8 RPCs — same shape.
```

- [ ] **Step 6: Run vet**

Run: `task lint`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
jj describe -m "feat(scene-proto): add 9 Phase 6 publication RPCs + messages

Proto contract for StartScenePublish, CastPublishSceneVote,
WithdrawScenePublish, GetPublishedScene, DownloadPublishedScene,
ListScenePublishAttempts, GetPublicSceneArchive,
DownloadPublicSceneArchive, ExtendScenePublishVoteAttempts.
Stub implementations panic with Unimplemented; real handlers land in
Phase B (holomush-5rh.15 spec section 5)."
```

### Task A4: Domain types (`publish_types.go`)

**Files:**

- Create: `plugins/core-scenes/publish_types.go`
- Create: `plugins/core-scenes/publish_types_test.go`

**Spec ref:** §3.1, §3.2

- [ ] **Step 1: Write failing tests in `publish_types_test.go`**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestPublishedSceneStatusIsValid(t *testing.T) {
    t.Parallel()
    cases := []struct {
        name  string
        s     PublishedSceneStatus
        valid bool
    }{
        {"COLLECTING valid", StatusCollecting, true},
        {"COOLOFF valid", StatusCoolOff, true},
        {"PUBLISHED valid", StatusPublished, true},
        {"ATTEMPT_FAILED valid", StatusAttemptFailed, true},
        {"empty invalid", PublishedSceneStatus(""), false},
        {"junk invalid", PublishedSceneStatus("WAT"), false},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            assert.Equal(t, tc.valid, tc.s.IsValid())
        })
    }
}

func TestPublishFailureReasonIsValid(t *testing.T) {
    t.Parallel()
    cases := []struct {
        name  string
        r     PublishFailureReason
        valid bool
    }{
        {"ANY_NO valid", FailureAnyNo, true},
        {"TIMEOUT valid", FailureTimeout, true},
        {"WITHDRAWN valid", FailureWithdrawn, true},
        {"SNAPSHOT_DECRYPT_FAILED valid", FailureSnapshotDecryptFailed, true},
        {"SNAPSHOT_RENDER_FAILED valid", FailureSnapshotRenderFailed, true},
        {"COOLOFF_INVARIANT_BROKEN valid", FailureCoolOffInvariantBroken, true},
        {"empty invalid", PublishFailureReason(""), false},
        {"junk invalid", PublishFailureReason("WAT"), false},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            assert.Equal(t, tc.valid, tc.r.IsValid())
        })
    }
}

func TestEntryKindIsValid(t *testing.T) {
    t.Parallel()
    assert.True(t, EntryKindPose.IsValid())
    assert.True(t, EntryKindSay.IsValid())
    assert.True(t, EntryKindEmit.IsValid())
    assert.False(t, EntryKind("ooc").IsValid(), "ooc MUST be excluded from publication content per spec §12")
    assert.False(t, EntryKind("").IsValid())
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `task test -- -run "TestPublishedSceneStatusIsValid|TestPublishFailureReasonIsValid|TestEntryKindIsValid" ./plugins/core-scenes/`
Expected: FAIL (`undefined: PublishedSceneStatus`, etc.).

- [ ] **Step 3: Implement `publish_types.go`**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "time"

    "github.com/holomush/holomush/internal/pgnanos"
)

// PublishedSceneStatus is the publish-attempt state machine status per
// spec §4. See the transition table at §4.1 for legal transitions.
type PublishedSceneStatus string

const (
    StatusCollecting    PublishedSceneStatus = "COLLECTING"
    StatusCoolOff       PublishedSceneStatus = "COOLOFF"
    StatusPublished     PublishedSceneStatus = "PUBLISHED"
    StatusAttemptFailed PublishedSceneStatus = "ATTEMPT_FAILED"
)

func (s PublishedSceneStatus) IsValid() bool {
    switch s {
    case StatusCollecting, StatusCoolOff, StatusPublished, StatusAttemptFailed:
        return true
    }
    return false
}

func (s PublishedSceneStatus) IsTerminal() bool {
    return s == StatusPublished || s == StatusAttemptFailed
}

// PublishFailureReason explains why an attempt reached ATTEMPT_FAILED.
// Set ONLY on terminal ATTEMPT_FAILED rows. See spec §4.1 and §11.4.
type PublishFailureReason string

const (
    FailureAnyNo                  PublishFailureReason = "ANY_NO"
    FailureTimeout                PublishFailureReason = "TIMEOUT"
    FailureWithdrawn              PublishFailureReason = "WITHDRAWN"
    FailureSnapshotDecryptFailed  PublishFailureReason = "SNAPSHOT_DECRYPT_FAILED"
    FailureSnapshotRenderFailed   PublishFailureReason = "SNAPSHOT_RENDER_FAILED"
    FailureCoolOffInvariantBroken PublishFailureReason = "COOLOFF_INVARIANT_BROKEN"
)

func (r PublishFailureReason) IsValid() bool {
    switch r {
    case FailureAnyNo, FailureTimeout, FailureWithdrawn,
        FailureSnapshotDecryptFailed, FailureSnapshotRenderFailed,
        FailureCoolOffInvariantBroken:
        return true
    }
    return false
}

// EntryKind discriminates the three IC content kinds that survive into
// a published scene. OOC and ops events are EXCLUDED — see spec §12 and
// ADR holomush-sb3n.
type EntryKind string

const (
    EntryKindPose EntryKind = "pose"
    EntryKindSay  EntryKind = "say"
    EntryKindEmit EntryKind = "emit"
)

func (k EntryKind) IsValid() bool {
    switch k {
    case EntryKindPose, EntryKindSay, EntryKindEmit:
        return true
    }
    return false
}

// Entry is one row in the published_scenes.content_entries JSONB array.
// Frozen at PUBLISHED transition; immutable thereafter.
type Entry struct {
    Speaker string    `json:"speaker"`
    Kind    EntryKind `json:"kind"`
    Content string    `json:"content"`
}

// PublishedScene is the in-memory representation of a published_scenes row.
type PublishedScene struct {
    ID                  string
    SceneID             string
    AttemptNumber       int
    Status              PublishedSceneStatus
    InitiatedBy         string
    InitiatedAt         pgnanos.Time
    CoolOffStartedAt    *pgnanos.Time
    ResolvedAt          *pgnanos.Time
    VoteWindow          time.Duration
    CoolOffWindow       time.Duration
    MaxAttemptsSnapshot int
    ContentEntries      []Entry        // nil unless PUBLISHED
    TitleSnapshot       *string
    ParticipantsSnapshot []string
    PublishedAt         *pgnanos.Time
    FailureReason       *PublishFailureReason
}

// PublishedSceneVote is one row in published_scene_votes — one voter's
// state on one attempt. Roster is frozen at attempt creation.
type PublishedSceneVote struct {
    PublishedSceneID string
    CharacterID      string
    Vote             *bool          // nil = pending; *true / *false = cast
    VotedAt          *pgnanos.Time  // first-cast timestamp
    LastChangedAt    *pgnanos.Time  // updated on every cast
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `task test -- -run "TestPublishedSceneStatusIsValid|TestPublishFailureReasonIsValid|TestEntryKindIsValid" ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scene-types): add Phase 6 publish domain types

PublishedSceneStatus + IsValid/IsTerminal, PublishFailureReason +
IsValid, EntryKind + IsValid, Entry, PublishedScene, PublishedSceneVote.
Spec section 3.1, 3.2 (holomush-5rh.15)."
```

### Task A5: Store layer — attempt + roster lifecycle

**Files:**

- Create: `plugins/core-scenes/publish_store.go`
- Test: `plugins/core-scenes/store_integration_test.go` (extend existing file)

**Spec ref:** §3.3, §4

- [ ] **Step 1: Write failing integration tests**

Append to `plugins/core-scenes/store_integration_test.go`:

```go
var _ = Describe("Publish store — attempt + roster lifecycle", func() {
    It("creates a published_scenes row with COLLECTING status and frozen roster", func() {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        store := newTestStore()
        sceneID := newULID()
        ownerID := newULID()
        memberID := newULID()
        invitedID := newULID()
        mustCreateScene(store, sceneID, ownerID, string(SceneVisibilityOpen))
        mustAddParticipant(store, sceneID, memberID, "member")
        mustAddParticipant(store, sceneID, invitedID, "invited")

        pub, err := store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
            SceneID:        sceneID,
            AttemptNumber:  1,
            InitiatedBy:    ownerID,
            VoteWindow:     7 * 24 * time.Hour,
            CoolOffWindow:  30 * time.Minute,
            MaxAttempts:    3,
        })
        Expect(err).NotTo(HaveOccurred())
        Expect(pub.Status).To(Equal(StatusCollecting))
        Expect(pub.AttemptNumber).To(Equal(1))

        voters, err := store.ListPublishVoters(ctx, pub.ID)
        Expect(err).NotTo(HaveOccurred())
        Expect(voters).To(HaveLen(2), "roster must include owner+member, NOT invited (INV-P6-1)")
        voterIDs := make(map[string]bool, len(voters))
        for _, v := range voters {
            voterIDs[v.CharacterID] = true
            Expect(v.Vote).To(BeNil(), "fresh roster row MUST start with vote=nil (pending)")
        }
        Expect(voterIDs[ownerID]).To(BeTrue())
        Expect(voterIDs[memberID]).To(BeTrue())
        Expect(voterIDs[invitedID]).To(BeFalse(), "invited role MUST be excluded — INV-P6-1")
    })

    It("rejects a duplicate active attempt for the same scene", func() {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        store := newTestStore()
        sceneID := newULID()
        ownerID := newULID()
        mustCreateScene(store, sceneID, ownerID, string(SceneVisibilityOpen))

        _, err := store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
            SceneID: sceneID, AttemptNumber: 1, InitiatedBy: ownerID,
            VoteWindow: 7 * 24 * time.Hour, CoolOffWindow: 30 * time.Minute, MaxAttempts: 3,
        })
        Expect(err).NotTo(HaveOccurred())

        _, err = store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
            SceneID: sceneID, AttemptNumber: 2, InitiatedBy: ownerID,
            VoteWindow: 7 * 24 * time.Hour, CoolOffWindow: 30 * time.Minute, MaxAttempts: 3,
        })
        Expect(err).To(HaveOccurred())
        Expect(errutil.MatchCode(err, "SCENE_PUBLISH_ALREADY_ACTIVE")).To(BeTrue())
    })
})
```

(`mustAddParticipant` is a helper similar to existing `mustCreateScene`; if not present, add a small helper inline in the test file.)

- [ ] **Step 2: Run, verify FAIL**

Run: `task test:int -- -ginkgo.focus="Publish store" ./plugins/core-scenes/`
Expected: FAIL (`undefined: CreatePublishAttempt`, etc.).

- [ ] **Step 3: Implement `publish_store.go` — `CreatePublishAttempt` + `ListPublishVoters`**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"
    "errors"
    "time"

    "github.com/holomush/holomush/internal/pgnanos"
    "github.com/holomush/holomush/pkg/errutil"
    "github.com/holomush/holomush/pkg/oops"
    "github.com/jackc/pgx/v5"
)

type CreatePublishAttemptInput struct {
    SceneID       string
    AttemptNumber int
    InitiatedBy   string
    VoteWindow    time.Duration
    CoolOffWindow time.Duration
    MaxAttempts   int
}

// CreatePublishAttempt creates a published_scenes row in COLLECTING
// status and seeds published_scene_votes from the scene's owner+member
// participants in a single transaction. The unique partial index
// published_scenes_one_active_per_scene enforces "at most one active
// attempt per scene". See spec §3.3, §4.1.
func (s *SceneStore) CreatePublishAttempt(ctx context.Context, in CreatePublishAttemptInput) (*PublishedScene, error) {
    ctx, span := startSpan(ctx, "scene.store.create_publish_attempt",
        attribute.String("scene_id", in.SceneID))
    defer span.End()

    id := newULID()
    initiatedAt := time.Now()

    tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
    if err != nil {
        return nil, oops.Code("SCENE_PUBLISH_TX_BEGIN_FAILED").Wrap(err)
    }
    defer func() { _ = tx.Rollback(ctx) }()

    // Insert the attempt; unique index catches concurrent / duplicate active.
    _, err = tx.Exec(ctx, `
        INSERT INTO published_scenes (
            id, scene_id, attempt_number, status, initiated_by, initiated_at,
            vote_window, cooloff_window, max_attempts_snapshot
        ) VALUES ($1, $2, $3, 'COLLECTING', $4, $5, $6, $7, $8)
    `, id, in.SceneID, in.AttemptNumber, in.InitiatedBy, initiatedAt,
        in.VoteWindow, in.CoolOffWindow, in.MaxAttempts)
    if err != nil {
        if isUniqueViolation(err, "published_scenes_one_active_per_scene") {
            return nil, oops.Code("SCENE_PUBLISH_ALREADY_ACTIVE").
                With("scene_id", in.SceneID).Wrap(err)
        }
        if isUniqueViolation(err, "published_scenes_one_published_per_scene") {
            return nil, oops.Code("SCENE_PUBLISH_ALREADY_PUBLISHED").
                With("scene_id", in.SceneID).Wrap(err)
        }
        if isUniqueViolation(err, "published_scenes_attempt_unique") {
            return nil, oops.Code("SCENE_PUBLISH_ATTEMPT_NUMBER_TAKEN").
                With("scene_id", in.SceneID).
                With("attempt_number", in.AttemptNumber).Wrap(err)
        }
        return nil, oops.Code("SCENE_PUBLISH_CREATE_FAILED").Wrap(err)
    }

    // Seed roster from owner+member participants (NOT invited — INV-P6-1).
    _, err = tx.Exec(ctx, `
        INSERT INTO published_scene_votes (published_scene_id, character_id)
        SELECT $1, character_id FROM scene_participants
        WHERE scene_id = $2 AND role IN ('owner', 'member')
    `, id, in.SceneID)
    if err != nil {
        return nil, oops.Code("SCENE_PUBLISH_SEED_ROSTER_FAILED").Wrap(err)
    }

    // Verify roster non-empty — fail closed.
    var rosterSize int
    if err := tx.QueryRow(ctx,
        `SELECT count(*) FROM published_scene_votes WHERE published_scene_id = $1`,
        id).Scan(&rosterSize); err != nil {
        return nil, oops.Code("SCENE_PUBLISH_ROSTER_CHECK_FAILED").Wrap(err)
    }
    if rosterSize == 0 {
        return nil, oops.Code("SCENE_PUBLISH_NO_ELIGIBLE_VOTERS").
            With("scene_id", in.SceneID).
            Errorf("scene has no owner/member participants")
    }

    if err := tx.Commit(ctx); err != nil {
        return nil, oops.Code("SCENE_PUBLISH_COMMIT_FAILED").Wrap(err)
    }

    return &PublishedScene{
        ID:                  id,
        SceneID:             in.SceneID,
        AttemptNumber:       in.AttemptNumber,
        Status:              StatusCollecting,
        InitiatedBy:         in.InitiatedBy,
        InitiatedAt:         pgnanos.From(initiatedAt),
        VoteWindow:          in.VoteWindow,
        CoolOffWindow:       in.CoolOffWindow,
        MaxAttemptsSnapshot: in.MaxAttempts,
    }, nil
}

// ListPublishVoters returns all voter rows for an attempt, including
// pending (vote=nil) rows. Used by the resolution check and observability.
func (s *SceneStore) ListPublishVoters(ctx context.Context, publishedSceneID string) ([]PublishedSceneVote, error) {
    rows, err := s.pool.Query(ctx, `
        SELECT published_scene_id, character_id, vote, voted_at, last_changed_at
        FROM published_scene_votes
        WHERE published_scene_id = $1
        ORDER BY character_id
    `, publishedSceneID)
    if err != nil {
        return nil, oops.Code("SCENE_PUBLISH_LIST_VOTERS_FAILED").Wrap(err)
    }
    defer rows.Close()
    var out []PublishedSceneVote
    for rows.Next() {
        var v PublishedSceneVote
        var votedAt, lastChangedAt *time.Time
        if err := rows.Scan(&v.PublishedSceneID, &v.CharacterID, &v.Vote, &votedAt, &lastChangedAt); err != nil {
            return nil, oops.Code("SCENE_PUBLISH_LIST_VOTERS_SCAN_FAILED").Wrap(err)
        }
        if votedAt != nil { t := pgnanos.From(*votedAt); v.VotedAt = &t }
        if lastChangedAt != nil { t := pgnanos.From(*lastChangedAt); v.LastChangedAt = &t }
        out = append(out, v)
    }
    if err := rows.Err(); err != nil {
        return nil, oops.Code("SCENE_PUBLISH_LIST_VOTERS_ITER_FAILED").Wrap(err)
    }
    return out, nil
}

// isUniqueViolation detects pgx unique-constraint errors on a named index.
// Helper; refactor into shared store_helpers.go if used elsewhere later.
func isUniqueViolation(err error, indexName string) bool {
    var pgErr *pgconn.PgError
    if !errors.As(err, &pgErr) {
        return false
    }
    return pgErr.Code == "23505" && pgErr.ConstraintName == indexName
}
```

(Import additions: `"github.com/jackc/pgx/v5/pgconn"` for `PgError`.)

- [ ] **Step 4: Run integration tests, verify PASS**

Run: `task test:int -- -ginkgo.focus="Publish store" ./plugins/core-scenes/`
Expected: PASS (both `It` blocks).

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scene-store): CreatePublishAttempt + ListPublishVoters

Phase 6 store primitive for attempt creation + roster freeze in a single
transaction. Roster excludes invited rows per INV-P6-1 (spec section 3.3,
4.1). Fails closed on empty roster with SCENE_PUBLISH_NO_ELIGIBLE_VOTERS."
```

### Task A6: Store layer — vote upsert + tally

**Files:**

- Modify: `plugins/core-scenes/publish_store.go`
- Test: `plugins/core-scenes/store_integration_test.go` (extend)

**Spec ref:** §4.2, §4.3

- [ ] **Step 1: Write failing tests**

Append to `store_integration_test.go`:

```go
var _ = Describe("Publish store — vote operations", func() {
    It("upserts a vote and returns is_change=false on first cast", func() {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        store := newTestStore()
        pub, voter := setupAttemptWithOneVoter(ctx, store)

        result, err := store.CastVote(ctx, pub.ID, voter, true)
        Expect(err).NotTo(HaveOccurred())
        Expect(result.IsChange).To(BeFalse(), "first cast is not a change")
        Expect(result.Vote).To(Equal(true))
    })

    It("flips a vote and returns is_change=true on change", func() {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        store := newTestStore()
        pub, voter := setupAttemptWithOneVoter(ctx, store)
        _, err := store.CastVote(ctx, pub.ID, voter, true)
        Expect(err).NotTo(HaveOccurred())
        result, err := store.CastVote(ctx, pub.ID, voter, false)
        Expect(err).NotTo(HaveOccurred())
        Expect(result.IsChange).To(BeTrue())
        Expect(result.Vote).To(Equal(false))
    })

    It("re-affirms same value with is_change=false", func() {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        store := newTestStore()
        pub, voter := setupAttemptWithOneVoter(ctx, store)
        _, err := store.CastVote(ctx, pub.ID, voter, true)
        Expect(err).NotTo(HaveOccurred())
        result, err := store.CastVote(ctx, pub.ID, voter, true)
        Expect(err).NotTo(HaveOccurred())
        Expect(result.IsChange).To(BeFalse())
    })

    It("rejects vote from non-roster member", func() {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        store := newTestStore()
        pub, _ := setupAttemptWithOneVoter(ctx, store)

        nonRosterID := newULID()
        _, err := store.CastVote(ctx, pub.ID, nonRosterID, true)
        Expect(errutil.MatchCode(err, "SCENE_PUBLISH_NOT_A_VOTER")).To(BeTrue())
    })

    It("TallyVotes returns correct yes/no/pending counts", func() {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        store := newTestStore()
        pub := setupAttemptWith3Voters(ctx, store)
        voters, _ := store.ListPublishVoters(ctx, pub.ID)
        _, _ = store.CastVote(ctx, pub.ID, voters[0].CharacterID, true)
        _, _ = store.CastVote(ctx, pub.ID, voters[1].CharacterID, false)
        // voters[2] pending

        tally, err := store.TallyVotes(ctx, pub.ID)
        Expect(err).NotTo(HaveOccurred())
        Expect(tally.Yes).To(Equal(1))
        Expect(tally.No).To(Equal(1))
        Expect(tally.Pending).To(Equal(1))
    })
})
```

(Helpers `setupAttemptWithOneVoter` and `setupAttemptWith3Voters` go in the same file, near other helpers.)

- [ ] **Step 2: Run, verify FAIL**

Run: `task test:int -- -ginkgo.focus="Publish store — vote operations" ./plugins/core-scenes/`
Expected: FAIL (`undefined: CastVote`, `undefined: TallyVotes`).

- [ ] **Step 3: Implement `CastVote` + `TallyVotes`**

Append to `publish_store.go`:

```go
type CastVoteResult struct {
    Vote     bool
    IsChange bool
}

// CastVote upserts a vote. The voter MUST already be on the roster
// (published_scene_votes row exists with the character_id). Returns
// IsChange=true if the value differs from a prior cast on the same row.
func (s *SceneStore) CastVote(ctx context.Context, publishedSceneID, characterID string, vote bool) (*CastVoteResult, error) {
    ctx, span := startSpan(ctx, "scene.store.cast_vote",
        attribute.String("published_scene_id", publishedSceneID),
        attribute.String("character_id", characterID))
    defer span.End()

    var prior *bool
    row := s.pool.QueryRow(ctx,
        `SELECT vote FROM published_scene_votes
         WHERE published_scene_id = $1 AND character_id = $2`,
        publishedSceneID, characterID)
    if err := row.Scan(&prior); err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, oops.Code("SCENE_PUBLISH_NOT_A_VOTER").
                With("published_scene_id", publishedSceneID).
                With("character_id", characterID).Wrap(err)
        }
        return nil, oops.Code("SCENE_PUBLISH_CAST_LOOKUP_FAILED").Wrap(err)
    }

    now := time.Now()
    isChange := prior == nil || *prior != vote

    _, err := s.pool.Exec(ctx, `
        UPDATE published_scene_votes
        SET vote = $1,
            voted_at = COALESCE(voted_at, $2),
            last_changed_at = $2
        WHERE published_scene_id = $3 AND character_id = $4
    `, vote, now, publishedSceneID, characterID)
    if err != nil {
        return nil, oops.Code("SCENE_PUBLISH_CAST_UPDATE_FAILED").Wrap(err)
    }
    return &CastVoteResult{Vote: vote, IsChange: isChange}, nil
}

type VoteTally struct {
    Yes     int
    No      int
    Pending int
}

// TallyVotes counts yes/no/pending across all voters for an attempt.
func (s *SceneStore) TallyVotes(ctx context.Context, publishedSceneID string) (*VoteTally, error) {
    var t VoteTally
    row := s.pool.QueryRow(ctx, `
        SELECT
            count(*) FILTER (WHERE vote = true)  AS yes,
            count(*) FILTER (WHERE vote = false) AS no,
            count(*) FILTER (WHERE vote IS NULL) AS pending
        FROM published_scene_votes WHERE published_scene_id = $1
    `, publishedSceneID)
    if err := row.Scan(&t.Yes, &t.No, &t.Pending); err != nil {
        return nil, oops.Code("SCENE_PUBLISH_TALLY_FAILED").Wrap(err)
    }
    return &t, nil
}
```

- [ ] **Step 4: Run, verify PASS**

Run: `task test:int -- -ginkgo.focus="Publish store — vote operations" ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scene-store): CastVote + TallyVotes for Phase 6 publish

CastVote upserts onto roster row (SCENE_PUBLISH_NOT_A_VOTER if absent),
preserves voted_at on first cast, advances last_changed_at on every cast.
TallyVotes returns yes/no/pending counts in a single query."
```

### Task A7: Store layer — status read/transitions + SELECT FOR UPDATE

**Files:**

- Modify: `plugins/core-scenes/publish_store.go`
- Test: `plugins/core-scenes/store_integration_test.go` (extend)

**Spec ref:** §4.1 (transitions), §11 (SELECT FOR UPDATE for snapshot)

- [ ] **Step 1: Write failing tests covering: GetPublishedSceneHeader, GetPublishedSceneContent, TransitionStatus, LockForSnapshot, CountAttempts**

```go
var _ = Describe("Publish store — status + transitions", func() {
    It("GetPublishedSceneHeader returns row without content_entries", func() { ... })
    It("GetPublishedSceneHeader returns nil for nonexistent id", func() { ... })
    It("GetPublishedSceneContent returns nil entries for non-PUBLISHED row", func() { ... })
    It("TransitionStatus(COLLECTING → COOLOFF) sets cooloff_started_at", func() { ... })
    It("TransitionStatus(COOLOFF → COLLECTING) clears cooloff_started_at", func() { ... })
    It("TransitionStatus to ATTEMPT_FAILED sets resolved_at + failure_reason", func() { ... })
    It("TransitionStatus rejects illegal transitions with SCENE_PUBLISH_INVALID_TRANSITION", func() { ... })
    It("LockForSnapshot blocks concurrent calls (idempotent on second call)", func() { ... })
    It("CountAttempts returns total + active + published counts per scene", func() { ... })
})
```

(Full test bodies follow the patterns in Tasks A5 + A6 — use `Eventually` for the concurrency test.)

- [ ] **Step 2: Run, verify FAIL**

Run: `task test:int -- -ginkgo.focus="Publish store — status" ./plugins/core-scenes/`
Expected: FAIL.

- [ ] **Step 3: Implement the methods**

```go
// GetPublishedSceneHeader returns the row WITHOUT content_entries —
// callers that need content call GetPublishedSceneContent separately.
// This split is load-bearing for INV-P6-5 (the participant gate runs
// between the header read and the content read).
func (s *SceneStore) GetPublishedSceneHeader(ctx context.Context, id string) (*PublishedScene, error) {
    row := s.pool.QueryRow(ctx, `
        SELECT id, scene_id, attempt_number, status, initiated_by, initiated_at,
               cooloff_started_at, resolved_at, vote_window, cooloff_window,
               max_attempts_snapshot, title_snapshot, participants_snapshot,
               published_at, failure_reason
        FROM published_scenes WHERE id = $1
    `, id)
    // Scan into PublishedScene; return nil if pgx.ErrNoRows. Pattern follows
    // existing SceneStore.Get at store.go (e.g., the SELECT pattern at line ~500).
    var pub PublishedScene
    // ... (full scan — fill in nullable fields with pointer-to-types)
    return &pub, nil
}

// GetPublishedSceneContent returns content_entries + frozen participants.
// MUST only be called AFTER the participant gate has approved the caller
// for participant-gated RPCs.
func (s *SceneStore) GetPublishedSceneContent(ctx context.Context, id string) ([]Entry, error) {
    var raw []byte
    if err := s.pool.QueryRow(ctx,
        `SELECT content_entries FROM published_scenes WHERE id = $1`, id,
    ).Scan(&raw); err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, nil
        }
        return nil, oops.Code("SCENE_PUBLISH_CONTENT_READ_FAILED").Wrap(err)
    }
    if len(raw) == 0 { return nil, nil }
    var entries []Entry
    if err := json.Unmarshal(raw, &entries); err != nil {
        return nil, oops.Code("SCENE_PUBLISH_CONTENT_DECODE_FAILED").Wrap(err)
    }
    return entries, nil
}

// TransitionStatus mutates status with side effects per spec §4.1.
type TransitionInput struct {
    To            PublishedSceneStatus
    FailureReason *PublishFailureReason  // required when To == ATTEMPT_FAILED
    SetCoolOffAt  *time.Time             // set when entering COOLOFF
    ClearCoolOff  bool                   // true when leaving COOLOFF (flip-back)
    Resolved      bool                   // sets resolved_at
}
func (s *SceneStore) TransitionStatus(ctx context.Context, id string, in TransitionInput) error {
    // Single UPDATE with CASE/COALESCE to mutate just the relevant fields.
    // Validate legality via a precondition CHECK in the WHERE clause
    // (e.g., only COLLECTING→COOLOFF when current status='COLLECTING').
    // Return SCENE_PUBLISH_INVALID_TRANSITION on zero rows updated.
    // Full impl follows the pattern from store.go's MoveCharacter at line ~700.
    return nil // placeholder; see Step 3 final impl
}

// LockForSnapshot does SELECT FOR UPDATE on the published_scenes row and
// re-validates status == COOLOFF. Used by the snapshot pipeline (spec §11.3).
func (s *SceneStore) LockForSnapshot(ctx context.Context, tx pgx.Tx, id string) (*PublishedScene, error) {
    var pub PublishedScene
    // SELECT ... FOR UPDATE; scan; verify status; return.
    return &pub, nil // placeholder
}

// CountAttempts returns (total, active, published) counts for a scene.
// Used by StartScenePublish preconditions (attempt_count < max_attempts).
func (s *SceneStore) CountAttempts(ctx context.Context, sceneID string) (AttemptCounts, error) {
    var c AttemptCounts
    // Single query with FILTER aggregates.
    return c, nil // placeholder
}
type AttemptCounts struct {
    Total     int
    Active    int  // status IN (COLLECTING, COOLOFF)
    Published int  // status == PUBLISHED
}
```

Flesh out each placeholder with full SQL bodies following the patterns at `store.go:325-354` (`IsMember`) and `store.go:759-825` (`MoveCharacter` — for transition validation with WHERE clause).

- [ ] **Step 4: Run, verify PASS**

Run: `task test:int -- -ginkgo.focus="Publish store — status" ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scene-store): publish status + transitions + lock

GetPublishedSceneHeader / GetPublishedSceneContent split is load-bearing
for INV-P6-5: the participant gate runs between these two store calls.
TransitionStatus uses precondition WHERE clauses for legal-transition
validation. LockForSnapshot wraps SELECT FOR UPDATE for the snapshot
pipeline (spec section 11.3)."
```

### Task A8: State machine + vote tally + roster freeze unit tests

**Files:**

- Create: `plugins/core-scenes/publish_state.go`
- Create: `plugins/core-scenes/publish_state_test.go`
- Create: `plugins/core-scenes/publish_vote_tally_test.go`
- Create: `plugins/core-scenes/publish_roster_test.go`

**Spec ref:** §4, §15.1 unit tier

- [ ] **Step 1: Write `publish_state_test.go` failing tests**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestPublishStateMachine_TransitionTable(t *testing.T) {
    t.Parallel()
    cases := []struct {
        name        string
        from        PublishedSceneStatus
        trigger     PublishTrigger
        wantTo      PublishedSceneStatus
        wantReject  bool
    }{
        {"COLLECTING + all-yes → COOLOFF", StatusCollecting, TriggerAllYes, StatusCoolOff, false},
        {"COLLECTING + any-no-after-voted → ATTEMPT_FAILED", StatusCollecting, TriggerAllVotedAnyNo, StatusAttemptFailed, false},
        {"COLLECTING + timeout → ATTEMPT_FAILED", StatusCollecting, TriggerTimeout, StatusAttemptFailed, false},
        {"COLLECTING + withdraw → ATTEMPT_FAILED", StatusCollecting, TriggerWithdraw, StatusAttemptFailed, false},
        {"COOLOFF + window-elapsed → PUBLISHED", StatusCoolOff, TriggerCoolOffElapsed, StatusPublished, false},
        {"COOLOFF + flip-no → COLLECTING", StatusCoolOff, TriggerFlipNo, StatusCollecting, false},
        {"COOLOFF + withdraw → ATTEMPT_FAILED", StatusCoolOff, TriggerWithdraw, StatusAttemptFailed, false},
        {"PUBLISHED + anything rejects", StatusPublished, TriggerAllYes, "", true},
        {"ATTEMPT_FAILED + anything rejects", StatusAttemptFailed, TriggerAllYes, "", true},
        {"COLLECTING + cool-off-elapsed rejects (illegal)", StatusCollecting, TriggerCoolOffElapsed, "", true},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            to, ok := NextStatus(tc.from, tc.trigger)
            if tc.wantReject {
                assert.False(t, ok)
            } else {
                assert.True(t, ok)
                assert.Equal(t, tc.wantTo, to)
            }
        })
    }
}
```

- [ ] **Step 2: Run, verify FAIL**

Run: `task test -- -run TestPublishStateMachine ./plugins/core-scenes/`
Expected: FAIL (`undefined: PublishTrigger`, `undefined: NextStatus`).

- [ ] **Step 3: Implement `publish_state.go`**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

// PublishTrigger names a state-machine event. The legal transition is
// (from, trigger) → to per spec §4.1.
type PublishTrigger string

const (
    TriggerAllYes         PublishTrigger = "all_yes"
    TriggerAllVotedAnyNo  PublishTrigger = "all_voted_any_no"
    TriggerTimeout        PublishTrigger = "timeout"
    TriggerWithdraw       PublishTrigger = "withdraw"
    TriggerCoolOffElapsed PublishTrigger = "cooloff_elapsed"
    TriggerFlipNo         PublishTrigger = "flip_no"
    TriggerSnapshotFailed PublishTrigger = "snapshot_failed"
)

// NextStatus returns the next status for (from, trigger) per spec §4.1,
// or (_, false) if the transition is illegal. Pure function for unit
// testability; the database layer applies the transition via
// SceneStore.TransitionStatus.
func NextStatus(from PublishedSceneStatus, t PublishTrigger) (PublishedSceneStatus, bool) {
    switch from {
    case StatusCollecting:
        switch t {
        case TriggerAllYes: return StatusCoolOff, true
        case TriggerAllVotedAnyNo, TriggerTimeout, TriggerWithdraw:
            return StatusAttemptFailed, true
        }
    case StatusCoolOff:
        switch t {
        case TriggerCoolOffElapsed: return StatusPublished, true
        case TriggerFlipNo:         return StatusCollecting, true
        case TriggerWithdraw, TriggerSnapshotFailed:
            return StatusAttemptFailed, true
        }
    }
    return "", false
}

// FailureReasonForTrigger maps a terminal-causing trigger to its failure_reason.
func FailureReasonForTrigger(t PublishTrigger) (PublishFailureReason, bool) {
    switch t {
    case TriggerAllVotedAnyNo: return FailureAnyNo, true
    case TriggerTimeout:       return FailureTimeout, true
    case TriggerWithdraw:      return FailureWithdrawn, true
    }
    return "", false
}

// ResolveFromTally returns the trigger to apply based on a tally tally
// during COLLECTING. Returns ("", false) if no resolution applies yet.
func ResolveFromTally(t VoteTally) (PublishTrigger, bool) {
    if t.Pending > 0 {
        return "", false
    }
    if t.No > 0 {
        return TriggerAllVotedAnyNo, true
    }
    return TriggerAllYes, true
}
```

- [ ] **Step 4: Run, verify PASS**

Run: `task test -- -run TestPublishStateMachine ./plugins/core-scenes/`
Expected: PASS (10 subtests).

- [ ] **Step 5: Write `publish_vote_tally_test.go`**

```go
package main

import (
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestResolveFromTally(t *testing.T) {
    t.Parallel()
    cases := []struct {
        name    string
        tally   VoteTally
        wantT   PublishTrigger
        wantOK  bool
    }{
        {"all yes unanimous", VoteTally{Yes: 3, No: 0, Pending: 0}, TriggerAllYes, true},
        {"any no after all voted", VoteTally{Yes: 2, No: 1, Pending: 0}, TriggerAllVotedAnyNo, true},
        {"still pending", VoteTally{Yes: 2, No: 0, Pending: 1}, "", false},
        {"mixed with pending", VoteTally{Yes: 1, No: 1, Pending: 1}, "", false},
        {"single yes single voter", VoteTally{Yes: 1, No: 0, Pending: 0}, TriggerAllYes, true},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got, ok := ResolveFromTally(tc.tally)
            assert.Equal(t, tc.wantOK, ok)
            if ok {
                assert.Equal(t, tc.wantT, got)
            }
        })
    }
}
```

- [ ] **Step 6: Run vote-tally tests, verify PASS**

Run: `task test -- -run TestResolveFromTally ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 7: Write `publish_roster_test.go` — pure logic for roster eligibility**

This tests the in-Go predicate `IsEligibleRole`, used by the store + handler layers:

```go
package main

import (
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestIsEligibleVoterRole(t *testing.T) {
    t.Parallel()
    assert.True(t, IsEligibleVoterRole("owner"))
    assert.True(t, IsEligibleVoterRole("member"))
    assert.False(t, IsEligibleVoterRole("invited"))
    assert.False(t, IsEligibleVoterRole(""))
    assert.False(t, IsEligibleVoterRole("admin"))
}
```

Add to `publish_state.go`:

```go
// IsEligibleVoterRole returns true for roles that may be on a publish
// roster per INV-P6-1: owner and member only, NOT invited.
func IsEligibleVoterRole(role string) bool {
    return role == "owner" || role == "member"
}
```

- [ ] **Step 8: Run, verify PASS**

Run: `task test -- -run TestIsEligibleVoterRole ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
jj describe -m "feat(scene-state): Phase 6 state machine + tally + roster predicates

Pure-Go state machine helpers (NextStatus, FailureReasonForTrigger,
ResolveFromTally) and roster eligibility predicate (IsEligibleVoterRole).
Unit-tested without DB. Spec section 4 (holomush-5rh.15)."
```

---

## Phase B — Participant RPCs + INV-S9 Gate + Commands

Phase B implements the participant-gated RPC surface, the resolveSceneRef helper, the `scene publish *` commands, and the INV-S9 plugin-code gate with its load-bearing tripwire test. ABAC policies in `plugin.yaml` updated and verified. By end of Phase B, a participant can run `scene publish` / `scene publish vote yes/no` / `scene publish withdraw` against the focused scene; non-participants are denied via the plugin-code gate with the triple-signal observability.

**Spec coverage:** §5 (participant RPCs only — public surface in C), §6 (commands), §8 (ABAC), §9 (INV-S9 contract), §10 (privacy block triple signal).

### Task B1: `resolveSceneRef` helper + tests

**Files:**

- Create: `plugins/core-scenes/commands_resolve_test.go`
- Modify: `plugins/core-scenes/commands.go` (add helper near top of file)

**Spec ref:** §6.1

**Design note:** The plugin SDK does NOT expose a per-connection "currently focused scene" query — `pluginsdk.FocusClient` (at `pkg/plugin/focus_client.go:27`) has `SetConnectionFocus`, `IsAnyConnFocused`, `JoinFocus`/`LeaveFocus`, but no `GetConnectionFocus`. The existing codebase pattern for "which scene does this command target?" is **single-membership inference** via `p.resolveSingleSceneMembership(ctx, characterID)` (at `commands.go:828` in `handleEmit`, also `commands.go:911` in `handleOrder`): if the character is in exactly one active scene, return its ID; otherwise return an error message that prompts the player to pass `#<scene_id>` explicitly. Phase 6 reuses this pattern instead of inventing a new `FocusedSceneID()` abstraction.

- [ ] **Step 1: Write failing tests**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"
    "testing"

    "github.com/holomush/holomush/pkg/errutil"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// fakeMembershipLookup returns a fixed slice of scenes the caller is in.
type fakeMembershipLookup struct{ scenes []string }

func (f *fakeMembershipLookup) ListScenesForCharacter(_ context.Context, _ string) ([]string, error) {
    return f.scenes, nil
}

func TestResolveSceneRef_SingleMembershipNoArg(t *testing.T) {
    t.Parallel()
    look := &fakeMembershipLookup{scenes: []string{"scene-abc"}}
    sceneID, err := resolveSceneRef(context.Background(), look, "char-1", "")
    require.NoError(t, err)
    assert.Equal(t, "scene-abc", sceneID)
}

func TestResolveSceneRef_ExplicitHashArg(t *testing.T) {
    t.Parallel()
    look := &fakeMembershipLookup{scenes: []string{"scene-default"}}
    sceneID, err := resolveSceneRef(context.Background(), look, "char-1", "#scene-xyz")
    require.NoError(t, err)
    assert.Equal(t, "scene-xyz", sceneID, "explicit arg overrides single-membership")
}

func TestResolveSceneRef_NoMembershipNoArg(t *testing.T) {
    t.Parallel()
    look := &fakeMembershipLookup{scenes: nil}
    _, err := resolveSceneRef(context.Background(), look, "char-1", "")
    require.Error(t, err)
    assert.True(t, errutil.MatchCode(err, "SCENE_PUBLISH_NO_FOCUSED_SCENE"))
}

func TestResolveSceneRef_MultiMembershipNoArgRequiresExplicit(t *testing.T) {
    t.Parallel()
    look := &fakeMembershipLookup{scenes: []string{"scene-a", "scene-b"}}
    _, err := resolveSceneRef(context.Background(), look, "char-1", "")
    require.Error(t, err)
    assert.True(t, errutil.MatchCode(err, "SCENE_PUBLISH_NO_FOCUSED_SCENE"),
        "ambiguous membership requires explicit '#<id>' arg")
}

func TestResolveSceneRef_ExplicitMalformed(t *testing.T) {
    t.Parallel()
    look := &fakeMembershipLookup{scenes: []string{"scene-default"}}
    cases := []string{"notahash", "#", "##abc", "  ", "# scene-with-space"}
    for _, c := range cases {
        t.Run(c, func(t *testing.T) {
            _, err := resolveSceneRef(context.Background(), look, "char-1", c)
            require.Error(t, err)
            assert.True(t, errutil.MatchCode(err, "SCENE_PUBLISH_REF_INVALID"))
        })
    }
}
```

- [ ] **Step 2: Run, verify FAIL**

Run: `task test -- -run TestResolveSceneRef ./plugins/core-scenes/`
Expected: FAIL (`undefined: resolveSceneRef`).

- [ ] **Step 3: Implement `resolveSceneRef` in `commands.go`**

```go
// membershipLookup is the minimal interface resolveSceneRef needs from
// the store. The real *SceneStore satisfies it via ListScenesForCharacter
// (store.go:1284). Tests use fakeMembershipLookup.
type membershipLookup interface {
    ListScenesForCharacter(ctx context.Context, characterID string) ([]string, error)
}

// resolveSceneRef returns the scene ID this command targets. If args
// starts with '#', the rest is treated as an explicit scene ID. Otherwise
// the helper uses single-membership inference: if the caller is in exactly
// one active scene, return its ID; ambiguous (zero or multiple) returns
// SCENE_PUBLISH_NO_FOCUSED_SCENE prompting the player to pass '#<id>'.
//
// This mirrors the existing handleEmit / handleOrder pattern at
// commands.go:828 (resolveSingleSceneMembership). See spec §6.1.
func resolveSceneRef(ctx context.Context, look membershipLookup, characterID, args string) (string, error) {
    args = strings.TrimSpace(args)
    if strings.HasPrefix(args, "#") {
        id := strings.TrimSpace(args[1:])
        if id == "" || strings.ContainsAny(id, " \t\n#") {
            return "", oops.Code("SCENE_PUBLISH_REF_INVALID").
                With("arg", args).Errorf("malformed scene reference")
        }
        return id, nil
    }
    if args != "" {
        return "", oops.Code("SCENE_PUBLISH_REF_INVALID").
            With("arg", args).Errorf("non-hash arg requires '#' prefix")
    }
    scenes, err := look.ListScenesForCharacter(ctx, characterID)
    if err != nil {
        return "", oops.Code("SCENE_PUBLISH_REF_LOOKUP_FAILED").Wrap(err)
    }
    if len(scenes) != 1 {
        return "", oops.Code("SCENE_PUBLISH_NO_FOCUSED_SCENE").
            With("matching_scenes", len(scenes)).
            Errorf("command requires '#<scene_id>' arg (caller is in %d scenes)", len(scenes))
    }
    return scenes[0], nil
}
```

- [ ] **Step 4: Run, verify PASS**

Run: `task test -- -run TestResolveSceneRef ./plugins/core-scenes/`
Expected: PASS (5 subtests).

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scene-commands): resolveSceneRef with single-membership inference

Single dispatch point for 'scene publish *' and 'scene log *' commands.
Implicit-no-arg form uses ListScenesForCharacter for single-membership
inference (mirrors handleEmit/handleOrder at commands.go:828); explicit
'#<id>' form bypasses inference. Errors: SCENE_PUBLISH_NO_FOCUSED_SCENE,
SCENE_PUBLISH_REF_INVALID, SCENE_PUBLISH_REF_LOOKUP_FAILED.
Spec section 6.1 (holomush-5rh.15)."
```

### Task B1.5: Caller validation + error-mapping helpers (`publish_helpers.go`)

**Files:**

- Create: `plugins/core-scenes/publish_helpers.go`
- Create: `plugins/core-scenes/publish_helpers_test.go`

**Spec ref:** §5 (caller validation pattern), §5.2 (error code surface)

**Design note:** Existing scene handlers do caller validation inline (`audit.go:499-524` checks Kind == CHARACTER, 16-byte ID length, non-zero ULID). Phase 6 handlers need the same predicate at the top of every RPC, so we extract it to a helper to keep the INV-S9 gate ordering legible. The Phase 6 request messages (added in Task A3) MUST include a `caller_character_id` string field — amend the Task A3 proto messages to add `string caller_character_id = N` to `StartScenePublishRequest`, `CastPublishSceneVoteRequest`, `WithdrawScenePublishRequest`, `GetPublishedSceneRequest`, `DownloadPublishedSceneRequest`, `ListScenePublishAttemptsRequest`, `ExtendScenePublishVoteAttemptsRequest`. (The public RPCs `GetPublicSceneArchive` / `DownloadPublicSceneArchive` do NOT carry a caller field — they're unauthenticated.) The plugin command handlers populate `caller_character_id` from `pluginsdk.CommandRequest.CharacterID`.

- [ ] **Step 1: Write failing tests**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"
    "testing"

    "github.com/holomush/holomush/pkg/errutil"
    "github.com/holomush/holomush/pkg/oops"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

func TestParseCallerCharacterID_HappyPath(t *testing.T) {
    t.Parallel()
    id, err := parseCallerCharacterID("01HK0000000000000000000001")
    require.NoError(t, err)
    assert.Equal(t, "01HK0000000000000000000001", id)
}

func TestParseCallerCharacterID_Empty(t *testing.T) {
    t.Parallel()
    _, err := parseCallerCharacterID("")
    require.Error(t, err)
    assert.True(t, errutil.MatchCode(err, "SCENE_PUBLISH_CALLER_REQUIRED"))
}

func TestParseCallerCharacterID_Malformed(t *testing.T) {
    t.Parallel()
    _, err := parseCallerCharacterID("not-a-ulid")
    require.Error(t, err)
    assert.True(t, errutil.MatchCode(err, "SCENE_PUBLISH_CALLER_MALFORMED"))
}

func TestMapStoreErr_OopsCodePropagates(t *testing.T) {
    t.Parallel()
    err := oops.Code("SCENE_PUBLISH_ALREADY_ACTIVE").Errorf("active")
    mapped := mapStoreErr(err)
    assert.True(t, errutil.MatchCode(mapped, "SCENE_PUBLISH_ALREADY_ACTIVE"))
    assert.Equal(t, codes.FailedPrecondition, status.Code(mapped))
}

func TestMapStoreErr_BareErrorBecomesInternal(t *testing.T) {
    t.Parallel()
    mapped := mapStoreErr(context.Canceled)
    assert.Equal(t, codes.Internal, status.Code(mapped))
    // Wire-level message MUST be generic per .claude/rules/grpc-errors.md.
    assert.Equal(t, "internal error", status.Convert(mapped).Message())
}
```

- [ ] **Step 2: Run, verify FAIL**

Run: `task test -- -run "TestParseCallerCharacterID|TestMapStoreErr" ./plugins/core-scenes/`
Expected: FAIL.

- [ ] **Step 3: Implement `publish_helpers.go`**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "log/slog"

    "github.com/holomush/holomush/pkg/oops"
    "github.com/oklog/ulid/v2"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

// parseCallerCharacterID validates a string ULID from a Phase 6 request's
// caller_character_id field. Mirrors the inline pattern at audit.go:511-524
// (Kind=CHARACTER check is enforced upstream at the host plugin gateway;
// service handlers receive the ULID-string in the request body).
func parseCallerCharacterID(s string) (string, error) {
    if s == "" {
        return "", oops.Code("SCENE_PUBLISH_CALLER_REQUIRED").
            Errorf("caller_character_id is required")
    }
    parsed, err := ulid.ParseStrict(s)
    if err != nil {
        return "", oops.Code("SCENE_PUBLISH_CALLER_MALFORMED").
            With("caller_character_id", s).Wrap(err)
    }
    if parsed == (ulid.ULID{}) {
        return "", oops.Code("SCENE_PUBLISH_CALLER_MALFORMED").
            Errorf("caller_character_id is zero ULID")
    }
    return parsed.String(), nil
}

// mapStoreErr translates store-layer errors into gRPC status codes,
// preserving oops codes for the application-error category and mapping
// bare errors to Internal with a generic wire-level message (per
// .claude/rules/grpc-errors.md "Never leak inner errors past trust
// boundaries"). The full error is logged at the call site via
// internalErr; this function only shapes the wire-visible response.
func mapStoreErr(err error) error {
    if err == nil {
        return nil
    }
    code := oops.AsOops(err).Code()
    switch code {
    case "SCENE_PUBLISH_ALREADY_ACTIVE",
        "SCENE_PUBLISH_ALREADY_PUBLISHED",
        "SCENE_PUBLISH_ATTEMPTS_EXHAUSTED",
        "SCENE_PUBLISH_NO_ELIGIBLE_VOTERS",
        "SCENE_PUBLISH_INVALID_STATE",
        "SCENE_PUBLISH_INVALID_TRANSITION":
        return status.Error(codes.FailedPrecondition, code)
    case "SCENE_PUBLISH_NOT_A_VOTER",
        "SCENE_PUBLISH_NOT_OWNER",
        "SCENE_PRIVACY_BOUNDARY_BLOCK":
        return status.Error(codes.PermissionDenied, code)
    case "SCENE_PUBLISH_CALLER_REQUIRED",
        "SCENE_PUBLISH_CALLER_MALFORMED",
        "SCENE_PUBLISH_FORMAT_UNSUPPORTED",
        "SCENE_PUBLISH_REF_INVALID",
        "SCENE_PUBLISH_NO_FOCUSED_SCENE":
        return status.Error(codes.InvalidArgument, code)
    }
    return status.Error(codes.Internal, "internal error")
}

// internalErr returns a generic Internal status for the wire and logs
// the inner error to slog via the package-level logger. Wire-level
// opacity per .claude/rules/grpc-errors.md "Never leak inner errors
// past trust boundaries". Single-arg signature keeps handler call
// sites terse: `return nil, internalErr(err)`.
func internalErr(err error) error {
    slog.Error("scene publish internal error", "err", err.Error())
    return status.Error(codes.Internal, "internal error")
}
```

- [ ] **Step 4: Run, verify PASS**

Run: `task test -- -run "TestParseCallerCharacterID|TestMapStoreErr" ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 5: Add `SceneServiceImpl` fields `cfg` + `events` + their defaults**

Phase B handlers reference `s.cfg.DefaultVoteWindow`, `s.cfg.DefaultCoolOffWindow`, and `s.events.emit*`. Introduce both here so Phase B compiles. The cfg struct lives in `publish_helpers.go`; the events interface and no-op default live there too (the real emitter ships in Phase D Task D2).

In `plugins/core-scenes/publish_helpers.go`, add:

```go
import "time"

// SceneServiceConfig carries Phase 6 defaults. Game-wide values set at
// plugin init time (main.go); per-scene overrides are picked up at
// StartScenePublish time from the scene row's per-scene columns.
type SceneServiceConfig struct {
    DefaultVoteWindow    time.Duration
    DefaultCoolOffWindow time.Duration
}

// DefaultSceneServiceConfig matches the spec §6/§4 defaults: 7-day vote
// window, 30-minute cool-off window.
func DefaultSceneServiceConfig() SceneServiceConfig {
    return SceneServiceConfig{
        DefaultVoteWindow:    7 * 24 * time.Hour,
        DefaultCoolOffWindow: 30 * time.Minute,
    }
}

// publishEventer is the interface SceneServiceImpl uses to fire Phase 6
// scene_publish_* IC notice events. Phase B handlers call its methods on
// every state transition; the real implementation (publishEventEmitter)
// lands in Phase D Task D2. The noopPublishEventer default below absorbs
// every call during Phase B so handler tests pass without touching the
// event substrate.
type publishEventer interface {
    emitPublishStarted(ctx context.Context, pub *PublishedScene) error
    emitVoteCast(ctx context.Context, attemptID, characterID string, result *CastVoteResult) error
    emitCoolOffStarted(ctx context.Context, attemptID string, window time.Duration) error
    emitResolved(ctx context.Context, attemptID string, finalStatus PublishedSceneStatus, reason *PublishFailureReason, tally *VoteTally) error
    emitWithdrawn(ctx context.Context, attemptID, withdrawnBy string) error
    emitAttemptsExtended(ctx context.Context, sceneID, adminID string, additional, newMax int) error
}

// noopPublishEventer absorbs every emit. Used as the SceneServiceImpl
// default until Phase D wires the real emitter via SetEventSink-equivalent.
type noopPublishEventer struct{}

func (noopPublishEventer) emitPublishStarted(context.Context, *PublishedScene) error { return nil }
func (noopPublishEventer) emitVoteCast(context.Context, string, string, *CastVoteResult) error { return nil }
func (noopPublishEventer) emitCoolOffStarted(context.Context, string, time.Duration) error { return nil }
func (noopPublishEventer) emitResolved(context.Context, string, PublishedSceneStatus, *PublishFailureReason, *VoteTally) error { return nil }
func (noopPublishEventer) emitWithdrawn(context.Context, string, string) error { return nil }
func (noopPublishEventer) emitAttemptsExtended(context.Context, string, string, int, int) error { return nil }
```

In `plugins/core-scenes/service.go`, extend the `SceneServiceImpl` struct (add lines after the existing fields) and the constructor:

```go
type SceneServiceImpl struct {
    store      sceneStorer
    // ... existing fields (eventSink, focusClient, gameID, etc.) ...
    cfg        SceneServiceConfig   // NEW (Phase 6)
    events     publishEventer       // NEW (Phase 6)
}

func NewSceneServiceImpl(store sceneStorer) *SceneServiceImpl {
    return &SceneServiceImpl{
        store:  store,
        cfg:    DefaultSceneServiceConfig(),
        events: noopPublishEventer{},  // Phase D's SetPublishEventer replaces this
    }
}
```

(If existing `NewSceneServiceImpl` already takes more params, preserve them; just add the two-field default-init at the end of the struct literal.)

- [ ] **Step 6: Run, verify PASS (compile-only)**

Run: `task build`
Expected: PASS — `SceneServiceImpl` now has `cfg` + `events` with sensible defaults so Phase B handlers compile.

- [ ] **Step 7: Commit**

```bash
jj describe -m "feat(scene-publish): caller validation + error mapping + service config

publish_helpers.go:
- parseCallerCharacterID validates ULID-string from caller_character_id
  field (mirrors audit.go:511-524 inline pattern)
- mapStoreErr translates oops codes to gRPC status per grpc-errors.md
- internalErr (single-arg) logs + returns generic Internal for wire opacity
- SceneServiceConfig + DefaultSceneServiceConfig hold Phase 6 defaults
  (7-day vote window, 30-min cool-off)
- publishEventer interface + noopPublishEventer default; Phase D wires
  the real publishEventEmitter

service.go: SceneServiceImpl gains cfg + events fields; NewSceneServiceImpl
seeds DefaultSceneServiceConfig and noopPublishEventer.

NOTE for Phase 6 readers: Task A3 proto messages MUST include 'string
caller_character_id = N' in each authenticated request."
```

### Task B2: `StartScenePublish` handler + ABAC policy

**Files:**

- Create: `plugins/core-scenes/publish_service.go`
- Modify: `plugins/core-scenes/service.go` (replace the Phase A stub)
- Modify: `plugins/core-scenes/plugin.yaml` (add `start-publish-as-participant` policy)
- Create: `plugins/core-scenes/publish_service_test.go`

**Spec ref:** §5 StartScenePublish, §8 ABAC

- [ ] **Step 1: Write failing test**

```go
func TestStartScenePublish_HappyPath(t *testing.T) {
    t.Parallel()
    store := newFakeStoreWithEndedScene("scene-1", "owner-1", []string{"owner-1", "member-1"}, []string{"invited-1"})
    svc := NewSceneServiceImpl(store)

    ctx := callerContext("owner-1")
    resp, err := svc.StartScenePublish(ctx, &scenev1.StartScenePublishRequest{SceneId: "scene-1"})

    require.NoError(t, err)
    assert.NotEmpty(t, resp.PublishedSceneId)
    assert.Equal(t, int32(1), resp.AttemptNumber)
    // Fake store should have one COLLECTING attempt with a 2-voter roster (NOT 3).
    pubs := store.publishAttempts()
    require.Len(t, pubs, 1)
    assert.Equal(t, StatusCollecting, pubs[0].Status)
    assert.Len(t, store.publishVoters(pubs[0].ID), 2, "invited excluded — INV-P6-1")
}

func TestStartScenePublish_RejectsActiveScene(t *testing.T) {
    t.Parallel()
    store := newFakeStoreWithActiveScene("scene-1", "owner-1")
    svc := NewSceneServiceImpl(store)
    _, err := svc.StartScenePublish(callerContext("owner-1"), &scenev1.StartScenePublishRequest{SceneId: "scene-1"})
    require.Error(t, err)
    assert.True(t, errutil.MatchCode(err, "SCENE_PUBLISH_INVALID_STATE"))
}

func TestStartScenePublish_RejectsAttemptsExhausted(t *testing.T) {
    t.Parallel()
    store := newFakeStoreWithExhaustedAttempts("scene-1", "owner-1", 3)
    svc := NewSceneServiceImpl(store)
    _, err := svc.StartScenePublish(callerContext("owner-1"), &scenev1.StartScenePublishRequest{SceneId: "scene-1"})
    require.Error(t, err)
    assert.True(t, errutil.MatchCode(err, "SCENE_PUBLISH_ATTEMPTS_EXHAUSTED"))
}
```

(Fake store factories `newFakeStoreWithEndedScene` etc. live in `publish_service_test.go`; pattern follows existing `service_test.go::fakeStore`.)

- [ ] **Step 2: Run, verify FAIL**

Run: `task test -- -run TestStartScenePublish ./plugins/core-scenes/`
Expected: FAIL.

- [ ] **Step 3: Implement handler in `publish_service.go`**

```go
package main

import (
    "context"

    scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
    "github.com/holomush/holomush/pkg/oops"
    "go.opentelemetry.io/otel/attribute"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

func (s *SceneServiceImpl) StartScenePublish(ctx context.Context, req *scenev1.StartScenePublishRequest) (*scenev1.StartScenePublishResponse, error) {
    ctx, span := startSpan(ctx, "scene.service.start_scene_publish",
        attribute.String("scene_id", req.GetSceneId()))
    defer span.End()

    callerID, err := parseCallerCharacterID(req.GetCallerCharacterId())
    if err != nil {
        return nil, err
    }

    scene, err := s.store.GetScene(ctx, req.GetSceneId())
    if err != nil {
        return nil, mapStoreErr(err)
    }
    if scene == nil {
        return nil, status.Error(codes.NotFound, "scene not found")
    }
    if scene.State != string(SceneStateEnded) {
        return nil, oops.Code("SCENE_PUBLISH_INVALID_STATE").
            With("scene_id", req.GetSceneId()).
            With("current_state", scene.State).
            Errorf("scene must be in 'ended' state to publish")
    }

    counts, err := s.store.CountAttempts(ctx, req.GetSceneId())
    if err != nil {
        return nil, mapStoreErr(err)
    }
    if counts.Published > 0 {
        return nil, oops.Code("SCENE_PUBLISH_ALREADY_PUBLISHED").
            With("scene_id", req.GetSceneId()).Errorf("scene already has a published archive")
    }
    if counts.Active > 0 {
        return nil, oops.Code("SCENE_PUBLISH_ALREADY_ACTIVE").
            With("scene_id", req.GetSceneId()).Errorf("scene already has an active attempt")
    }
    if counts.Total >= scene.MaxPublishAttempts {
        return nil, oops.Code("SCENE_PUBLISH_ATTEMPTS_EXHAUSTED").
            With("scene_id", req.GetSceneId()).
            With("max_attempts", scene.MaxPublishAttempts).
            Errorf("scene has exhausted its publish-attempt budget")
    }

    pub, err := s.store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
        SceneID:       req.GetSceneId(),
        AttemptNumber: counts.Total + 1,
        InitiatedBy:   callerID,
        VoteWindow:    s.cfg.DefaultVoteWindow,
        CoolOffWindow: s.cfg.DefaultCoolOffWindow,
        MaxAttempts:   scene.MaxPublishAttempts,
    })
    if err != nil {
        return nil, mapStoreErr(err)
    }

    metricScenePublishAttemptResolved("started", "")
    if emitErr := s.events.emitPublishStarted(ctx, pub); emitErr != nil {
        slog.WarnContext(ctx, "publish-started emit failed", "err", emitErr)
    }

    return &scenev1.StartScenePublishResponse{
        PublishedSceneId: pub.ID,
        AttemptNumber:    int32(pub.AttemptNumber),
    }, nil
}
```

(`s.cfg` and `s.events` are declared on `SceneServiceImpl` and wired in Task B1.5 Step 5 — `cfg` gets `DefaultSceneServiceConfig()` (7d/30m defaults); `events` gets `noopPublishEventer{}`. Phase D Task D2 replaces the noop emitter with the real `publishEventEmitter` via `SetPublishEventer`.)

- [ ] **Step 4: Add ABAC policy to `plugin.yaml`**

In `plugins/core-scenes/plugin.yaml` under `policies:` (append):

```yaml
- name: start-publish-as-participant
  dsl: >-
    permit(principal is character, action in ["publish"], resource is scene)
    when { principal.id in resource.scene.participants
           && resource.scene.state == "ended" };
```

- [ ] **Step 5: Run, verify PASS**

Run: `task test -- -run TestStartScenePublish ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
jj describe -m "feat(scene-publish): StartScenePublish RPC + ABAC policy

Creates a new publish-vote attempt with frozen roster (owner+member,
INV-P6-1). Pre-conditions check scene state, prior publication,
active attempt, and max-attempts budget. Emits scene_publish_started
via the event emitter (Phase D wires the real emit). Spec section 5,
8 (holomush-5rh.15)."
```

### Task B3: `CastPublishSceneVote` handler + resolution check

**Files:**

- Modify: `plugins/core-scenes/publish_service.go`
- Modify: `plugins/core-scenes/publish_service_test.go`

**Spec ref:** §5 CastPublishSceneVote, §4.3 resolution check

- [ ] **Step 1: Write failing tests**

```go
func TestCastPublishSceneVote_FirstYesIsNotAChange(t *testing.T) { ... }
func TestCastPublishSceneVote_FlipYesToNoIsAChange(t *testing.T) { ... }
func TestCastPublishSceneVote_NonRosterMemberRejected(t *testing.T) { ... }
func TestCastPublishSceneVote_TriggersCoolOffOnAllYes(t *testing.T) {
    // 2-voter roster; vote 1 yes; vote 2 yes; assert store transitions to COOLOFF
    ...
}
func TestCastPublishSceneVote_TriggersAttemptFailedOnAnyNoAfterAllVoted(t *testing.T) { ... }
func TestCastPublishSceneVote_FlipFromYesToNoDuringCoolOffReturnsToCollecting(t *testing.T) { ... }
```

- [ ] **Step 2: Run, verify FAIL**

- [ ] **Step 3: Implement**

```go
func (s *SceneServiceImpl) CastPublishSceneVote(ctx context.Context, req *scenev1.CastPublishSceneVoteRequest) (*scenev1.CastPublishSceneVoteResponse, error) {
    ctx, span := startSpan(ctx, "scene.service.cast_publish_scene_vote",
        attribute.String("published_scene_id", req.GetPublishedSceneId()))
    defer span.End()

    callerID, err := parseCallerCharacterID(req.GetCallerCharacterId())
    if err != nil { return nil, err }

    pub, err := s.store.GetPublishedSceneHeader(ctx, req.GetPublishedSceneId())
    if err != nil { return nil, mapStoreErr(err) }
    if pub == nil {
        return nil, status.Error(codes.NotFound, "publication attempt not found")
    }
    if pub.Status.IsTerminal() {
        return nil, oops.Code("SCENE_PUBLISH_INVALID_STATE").
            With("status", string(pub.Status)).
            Errorf("vote on terminal attempt rejected")
    }

    result, err := s.store.CastVote(ctx, pub.ID, callerID, req.GetVote())
    if err != nil { return nil, mapStoreErr(err) }

    // Run resolution check; transition status if appropriate.
    if err := s.applyResolution(ctx, pub); err != nil {
        slog.WarnContext(ctx, "resolution check failed", "err", err, "attempt_id", pub.ID)
        // Resolution failures don't fail the vote — the next cast/tick retries.
    }

    metricScenePublishVoteCast(boolLabel(result.Vote), boolLabel(result.IsChange))
    if emitErr := s.events.emitVoteCast(ctx, pub.ID, callerID, result); emitErr != nil {
        slog.WarnContext(ctx, "vote-cast emit failed", "err", emitErr)
    }

    return &scenev1.CastPublishSceneVoteResponse{IsChange: result.IsChange}, nil
}

// applyResolution checks vote tally and transitions status if applicable.
// See spec §4.3.
func (s *SceneServiceImpl) applyResolution(ctx context.Context, pub *PublishedScene) error {
    tally, err := s.store.TallyVotes(ctx, pub.ID)
    if err != nil { return err }
    switch pub.Status {
    case StatusCollecting:
        trig, ok := ResolveFromTally(*tally)
        if !ok { return nil }
        return s.applyTrigger(ctx, pub.ID, trig)
    case StatusCoolOff:
        if tally.No > 0 {
            return s.applyTrigger(ctx, pub.ID, TriggerFlipNo)
        }
    }
    return nil
}

// applyTrigger drives the DB state transition for a publish-vote attempt.
// Reads the current row, computes the next status via the pure NextStatus
// helper (publish_state.go), and applies the side effects (TransitionStatus
// store call, metric counter, event emit). Event emission is best-effort
// in Phase B — Phase D's publishEventEmitter wires the real Emit; Phase B
// uses a no-op emitter so the state-machine tests pass without depending
// on Phase D's event substrate.
func (s *SceneServiceImpl) applyTrigger(ctx context.Context, attemptID string, t PublishTrigger) error {
    pub, err := s.store.GetPublishedSceneHeader(ctx, attemptID)
    if err != nil {
        return err
    }
    if pub == nil {
        return oops.Code("SCENE_PUBLISH_ATTEMPT_NOT_FOUND").
            With("attempt_id", attemptID).Errorf("attempt vanished")
    }
    next, ok := NextStatus(pub.Status, t)
    if !ok {
        return oops.Code("SCENE_PUBLISH_INVALID_TRANSITION").
            With("from", string(pub.Status)).
            With("trigger", string(t)).
            Errorf("illegal transition")
    }

    in := TransitionInput{To: next, Resolved: next.IsTerminal()}
    if next == StatusAttemptFailed {
        reason, hasReason := FailureReasonForTrigger(t)
        if !hasReason {
            // Snapshot/cooloff-invariant triggers are not in FailureReasonForTrigger;
            // they're applied via failAttempt from publish_snapshot.go directly.
            return oops.Code("SCENE_PUBLISH_INVALID_TRANSITION").
                With("trigger", string(t)).Errorf("no failure_reason mapping")
        }
        in.FailureReason = &reason
    }
    if next == StatusCoolOff {
        now := time.Now()
        in.SetCoolOffAt = &now
    }
    if next == StatusCollecting && pub.Status == StatusCoolOff {
        in.ClearCoolOff = true
    }

    if err := s.store.TransitionStatus(ctx, attemptID, in); err != nil {
        return err
    }

    // Metrics + observability — emit is best-effort; failure logs but does
    // not roll back the transition (DB state is the source of truth).
    if next.IsTerminal() {
        reasonLabel := ""
        if in.FailureReason != nil {
            reasonLabel = string(*in.FailureReason)
        } else if next == StatusPublished {
            reasonLabel = "all_yes"
        }
        metricScenePublishAttemptResolved(string(next), reasonLabel)
    }
    if next == StatusCoolOff {
        if emitErr := s.events.emitCoolOffStarted(ctx, attemptID, pub.CoolOffWindow); emitErr != nil {
            slog.WarnContext(ctx, "cooloff-started emit failed", "err", emitErr, "attempt_id", attemptID)
        }
    }
    if next.IsTerminal() {
        tally, _ := s.store.TallyVotes(ctx, attemptID)
        if emitErr := s.events.emitResolved(ctx, attemptID, next, in.FailureReason, tally); emitErr != nil {
            slog.WarnContext(ctx, "resolved emit failed", "err", emitErr, "attempt_id", attemptID)
        }
    }
    return nil
}
```

- [ ] **Step 4: Run, verify PASS**

Run: `task test -- -run TestCastPublishSceneVote ./plugins/core-scenes/`
Expected: PASS — state-machine transitions exercise `applyTrigger` against a fake store; the no-op `publishEventEmitter` (set in `NewSceneServiceImpl` default) absorbs every emit call so tests pass before Phase D.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scene-publish): CastPublishSceneVote + applyTrigger state machine

Vote upsert with is_change tracking, triggers resolution check that
transitions COLLECTING→COOLOFF on all-yes or COLLECTING→ATTEMPT_FAILED
on any-no-after-all-voted. COOLOFF→COLLECTING flip-back on any post-
cooloff no. applyTrigger reads current status, computes next via
NextStatus (pure helper), applies TransitionInput to store, fires
metric stub + emit (no-op in Phase B; Phase D wires real emitter).
Spec section 4.3, 5 (holomush-5rh.15)."
```

### Task B4: `WithdrawScenePublish` handler + ABAC policy

**Files:**

- Modify: `plugins/core-scenes/publish_service.go`
- Modify: `plugins/core-scenes/plugin.yaml` (add `withdraw-publish-as-owner` policy)
- Modify: `plugins/core-scenes/publish_service_test.go`

**Spec ref:** §5 WithdrawScenePublish, §8

- [ ] **Step 1: Write failing tests** — happy path (owner), rejection (non-owner), rejection (terminal).
- [ ] **Step 2: Run, verify FAIL**
- [ ] **Step 3: Implement**

```go
func (s *SceneServiceImpl) WithdrawScenePublish(ctx context.Context, req *scenev1.WithdrawScenePublishRequest) (*scenev1.WithdrawScenePublishResponse, error) {
    callerID, err := parseCallerCharacterID(req.GetCallerCharacterId())
    if err != nil { return nil, err }

    pub, err := s.store.GetPublishedSceneHeader(ctx, req.GetPublishedSceneId())
    if err != nil { return nil, mapStoreErr(err) }
    if pub == nil { return nil, status.Error(codes.NotFound, "attempt not found") }
    if pub.Status.IsTerminal() {
        return nil, oops.Code("SCENE_PUBLISH_INVALID_STATE").Errorf("attempt already terminal")
    }
    scene, err := s.store.GetScene(ctx, pub.SceneID)
    if err != nil { return nil, mapStoreErr(err) }
    if scene.OwnerID != callerID {
        return nil, oops.Code("SCENE_PUBLISH_NOT_OWNER").
            With("scene_id", pub.SceneID).Errorf("only the scene owner may withdraw")
    }

    if err := s.applyTrigger(ctx, pub.ID, TriggerWithdraw); err != nil {
        return nil, mapStoreErr(err)
    }
    return &scenev1.WithdrawScenePublishResponse{}, nil
}
```

Add policy to `plugin.yaml`:

```yaml
- name: withdraw-publish-as-owner
  dsl: >-
    permit(principal is character, action in ["withdraw_publish"], resource is scene)
    when { resource.scene.owner == principal.id };
```

- [ ] **Step 4: Run, verify PASS**
- [ ] **Step 5: Commit**

### Task B5: `GetPublishedScene` + **INV-S9 tripwire test (CRITICAL)**

**Files:**

- Modify: `plugins/core-scenes/publish_service.go`
- Create: `plugins/core-scenes/service_publish_gate_test.go`

**Spec ref:** §9.1, §9.4, INV-P6-5, INV-P6-6

- [ ] **Step 1: Write the **load-bearing** tripwire test FIRST**

This is the test that pins INV-S9 ordering for Phase 6. It MUST exist before the handler.

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"
    "sync/atomic"
    "testing"

    "github.com/holomush/holomush/pkg/errutil"
    scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
    "github.com/stretchr/testify/require"
)

// contentTripwireStore embeds a fake header store and tracks any call
// into GetPublishedSceneContent. The test asserts the tripwire is NOT
// hit when a non-participant is denied at the gate — INV-P6-5 ordering.
type contentTripwireStore struct {
    *fakeStore
    contentReadCalls atomic.Int32
}

func (s *contentTripwireStore) GetPublishedSceneContent(_ context.Context, _ string) ([]Entry, error) {
    s.contentReadCalls.Add(1)
    return nil, nil
}

func TestGetPublishedSceneDeniesNonParticipantWithoutHittingContentStore(t *testing.T) {
    t.Parallel()
    base := newFakeStore()
    base.installPublishedAttempt("pub-1", "scene-1", StatusPublished)
    base.installSceneWithRoster("scene-1", "owner-1", []string{"owner-1", "member-1"})
    // Caller "alice" is NOT in scene-1's roster.
    store := &contentTripwireStore{fakeStore: base}
    svc := NewSceneServiceImpl(store)

    _, err := svc.GetPublishedScene(callerContext("alice"), &scenev1.GetPublishedSceneRequest{Id: "pub-1"})

    require.Error(t, err)
    require.True(t, errutil.MatchCode(err, "SCENE_PRIVACY_BOUNDARY_BLOCK"),
        "non-participant must receive SCENE_PRIVACY_BOUNDARY_BLOCK")
    require.Equal(t, int32(0), store.contentReadCalls.Load(),
        "INV-P6-5 violation: content store hit before the participant gate denied")
}

// INV-P6-6 is enforced structurally, not via runtime injection: the
// SceneServiceImpl has no ABAC-engine field, so participant-gated
// handlers physically cannot call the engine. This test is an AST-
// level assertion that publish_service.go imports no policy package
// and references no policy.* symbol. If a future PR adds an engine
// field or import, this test fires immediately.
func TestPublicationServiceFileImportsNoABACPolicyPackage(t *testing.T) {
    t.Parallel()
    src, err := os.ReadFile("publish_service.go")
    require.NoError(t, err)
    fset := token.NewFileSet()
    f, err := parser.ParseFile(fset, "publish_service.go", src, parser.ImportsOnly)
    require.NoError(t, err)
    for _, imp := range f.Imports {
        path := strings.Trim(imp.Path.Value, `"`)
        require.False(t, strings.HasPrefix(path, "github.com/holomush/holomush/internal/access/policy"),
            "INV-P6-6 violation: publish_service.go imports ABAC policy package %s", path)
    }
}

func TestPublicationServiceTypeHasNoABACEngineField(t *testing.T) {
    t.Parallel()
    // Reflection-based field enumeration; fails if any field's type name
    // contains "policy" or "Engine" — those are the ABAC engine signals.
    typ := reflect.TypeOf(SceneServiceImpl{})
    for i := 0; i < typ.NumField(); i++ {
        f := typ.Field(i)
        require.NotContains(t, strings.ToLower(f.Type.String()), "policy.engine",
            "INV-P6-6 violation: field %s carries ABAC engine type %s", f.Name, f.Type.String())
    }
}
```

(Imports needed in the test file: `"go/parser"`, `"go/token"`, `"os"`, `"reflect"`, `"strings"`.)

- [ ] **Step 2: Run, verify FAIL**

Run: `task test -- -run "TestGetPublishedSceneDeniesNonParticipantWithoutHittingContentStore|TestPublicationServiceFileImportsNoABACPolicyPackage|TestPublicationServiceTypeHasNoABACEngineField" ./plugins/core-scenes/`
Expected: FAIL on `TestGetPublishedSceneDeniesNonParticipantWithoutHittingContentStore` (handler not implemented yet); the two structural tests should PASS immediately because `publish_service.go` doesn't exist yet (the file-read test errors on missing file, which is its own pre-condition — handle by creating an empty `publish_service.go` first OR by gating the file-read test to require the file to exist, and rely on the reflection test as the live INV-P6-6 lock).

- [ ] **Step 3: Implement `GetPublishedScene` per spec §9.1**

```go
func (s *SceneServiceImpl) GetPublishedScene(ctx context.Context, req *scenev1.GetPublishedSceneRequest) (*scenev1.GetPublishedSceneResponse, error) {
    ctx, span := startSpan(ctx, "scene.service.get_published_scene",
        attribute.String("published_scene_id", req.GetId()))
    defer span.End()

    callerID, err := parseCallerCharacterID(req.GetCallerCharacterId())
    if err != nil { return nil, err }

    pub, err := s.store.GetPublishedSceneHeader(ctx, req.GetId())
    if err != nil { return nil, mapStoreErr(err) }
    if pub == nil { return nil, status.Error(codes.NotFound, "publication not found") }

    // INV-S9 plugin-code gate. NO ABAC engine consultation.
    ok, err := s.store.IsParticipant(ctx, pub.SceneID, callerID)
    if err != nil { return nil, internalErr(err) }
    if !ok {
        s.emitPrivacyBoundaryBlock(ctx, "GetPublishedScene", pub.SceneID, callerID, "not_participant")
        return nil, oops.Code("SCENE_PRIVACY_BOUNDARY_BLOCK").
            Errorf("scene not accessible")
    }

    var entries []Entry
    if pub.Status == StatusPublished {
        entries, err = s.store.GetPublishedSceneContent(ctx, req.GetId())
        if err != nil { return nil, internalErr(err) }
    }
    tally, err := s.store.TallyVotes(ctx, pub.ID)
    if err != nil { return nil, mapStoreErr(err) }

    return assembleParticipantResponse(pub, entries, tally), nil
}

// emitPrivacyBoundaryBlock is the triple-signal helper. See spec §10.
func (s *SceneServiceImpl) emitPrivacyBoundaryBlock(ctx context.Context, op, sceneID, callerID, reason string) {
    slog.WarnContext(ctx, "scene privacy boundary block",
        "operation", op,
        "scene_id", sceneID,
        "caller_id", callerID,
        "denial_reason", reason,
        "code", "SCENE_PRIVACY_BOUNDARY_BLOCK")
    metricScenePublishPrivacyBlock(op, reason)
    span := trace.SpanFromContext(ctx)
    span.SetStatus(otelcodes.Error, "denied")
    span.SetAttributes(attribute.String("deny.reason", reason))
}
```

(`assembleParticipantResponse` is a small pure function that maps `(*PublishedScene, []Entry, *VoteTally)` → `*scenev1.GetPublishedSceneResponse`.)

- [ ] **Step 4: Run, verify PASS**

Run: `task test -- -run "TestGetPublishedSceneDeniesNonParticipantWithoutHittingContentStore|TestPublicationServiceFileImportsNoABACPolicyPackage|TestPublicationServiceTypeHasNoABACEngineField" ./plugins/core-scenes/`
Expected: PASS — tripwire never fires; no ABAC package imported by publish_service.go; SceneServiceImpl has no field carrying a `policy.Engine` type.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scene-publish): GetPublishedScene + INV-S9 plugin-code gate

Participant-gated read with the load-bearing INV-P6-5 ordering: caller
validation → header read → IsParticipant check → content read only on
gate pass. Tripwire test asserts content store NOT hit when gate denies.
emitPrivacyBoundaryBlock implements the triple-signal (slog WARN, metric
stub, span error). Spec section 9, 10 (holomush-5rh.15)."
```

### Task B6: `DownloadPublishedScene` + INV-S9 gate

**Files:**

- Modify: `plugins/core-scenes/publish_service.go`
- Modify: `plugins/core-scenes/service_publish_gate_test.go`

Same pattern as B5 — write tripwire test for download path; implement handler that runs `IsParticipant` before any content read.

- [ ] **Step 1: Write tripwire test for download**
- [ ] **Step 2: Run, verify FAIL**
- [ ] **Step 3: Implement** (calls into `publish_render` from Phase C; for now stub the render call)
- [ ] **Step 4: Run, verify PASS**
- [ ] **Step 5: Commit**

### Task B7: `ListScenePublishAttempts` + INV-S9 gate

Same pattern as B5.

- [ ] Steps 1–5: as above

### Task B8: `scene publish` command handler

**Files:**

- Modify: `plugins/core-scenes/commands.go`
- Modify: `plugins/core-scenes/commands_test.go`

**Spec ref:** §6.1

- [ ] **Step 1: Write failing test**

```go
func TestSceneCommand_Publish_DispatchesToService(t *testing.T) {
    plugin := newTestPluginWithEndedScene("scene-1", "owner-1")
    resp, err := plugin.handlePublish(callerContext("owner-1"), commandReq("scene-1", ""), "")
    require.NoError(t, err)
    require.NotNil(t, resp)
    // Assert response contains attempt_number and id.
}
```

- [ ] **Step 2: Run, verify FAIL**
- [ ] **Step 3: Implement `handlePublish` in `commands.go`**

```go
func (p *scenePlugin) handlePublish(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
    sceneID, err := resolveSceneRef(ctx, p.service.store, req.CharacterID, args)
    if err != nil {
        return pluginsdk.Errorf("%s", err.Error()), nil
    }
    resp, err := p.service.StartScenePublish(ctx, &scenev1.StartScenePublishRequest{SceneId: sceneID})
    if err != nil {
        return pluginsdk.Errorf("%s", err.Error()), nil
    }
    return pluginsdk.OK(fmt.Sprintf(
        "Publish-vote attempt #%d started for scene %s. Participants will be notified.",
        resp.AttemptNumber, sceneID,
    )), nil
}
```

Wire into the dispatcher (the existing subcommand switch in `commands.go`).

- [ ] **Step 4: Run, verify PASS**
- [ ] **Step 5: Commit**

### Task B9: `scene publish vote yes|no` commands

**Files:**

- Modify: `plugins/core-scenes/commands.go`
- Modify: `plugins/core-scenes/commands_test.go`

Same shape as B8. Dispatch routes `scene publish vote yes` to `CastPublishSceneVote(vote=true)`, `scene publish vote no` to `vote=false`.

- [ ] Steps 1–5: as above

### Task B10: `scene publish withdraw` + `scene publish status` + `scene publish download` commands

**Files:**

- Modify: `plugins/core-scenes/commands.go`
- Modify: `plugins/core-scenes/commands_test.go`

Three small handlers wiring `WithdrawScenePublish`, `GetPublishedScene`, `DownloadPublishedScene` to the SDK.

- [ ] Steps 1–5: as above for each

---

## Phase C — Public Surface + Snapshot Pipeline + Renderers

Phase C ships the public RPC pair (`GetPublicSceneArchive`, `DownloadPublicSceneArchive`), the three renderers (markdown / plain text / jsonl), and the COOLOFF → PUBLISHED snapshot pipeline. By end of Phase C, a successfully resolved vote can transition to PUBLISHED with content_entries populated from the decrypted IC event stream, and public consumers can read the artifact via the public RPC pair.

**Spec coverage:** §5 (public RPCs), §11 (snapshot pipeline), §12 (renderers), INV-P6-8, INV-P6-10.

### Task C1: Markdown renderer + escape tests

**Files:**

- Create: `plugins/core-scenes/publish_render.go`
- Create: `plugins/core-scenes/publish_render_test.go`

**Spec ref:** §12

- [ ] **Step 1: Write failing tests**

```go
func TestRenderMarkdown_FromEntries(t *testing.T) { ... }
func TestRenderMarkdown_EmptyEntries(t *testing.T) { ... }
func TestRenderMarkdown_EscapesMarkdownSyntax(t *testing.T) { ... }
func TestRenderMarkdown_PreservesUnicodeAndEmoji(t *testing.T) { ... }
```

- [ ] **Step 2: Run, verify FAIL**
- [ ] **Step 3: Implement `renderMarkdown([]Entry) string`**

```go
func renderMarkdown(entries []Entry) string {
    if len(entries) == 0 {
        return "_No content was recorded for this scene._\n"
    }
    var b strings.Builder
    for _, e := range entries {
        switch e.Kind {
        case EntryKindPose:
            fmt.Fprintf(&b, "**%s** %s\n\n", escapeMarkdown(e.Speaker), escapeMarkdown(e.Content))
        case EntryKindSay:
            fmt.Fprintf(&b, "**%s** says, \"%s\"\n\n", escapeMarkdown(e.Speaker), escapeMarkdown(e.Content))
        case EntryKindEmit:
            fmt.Fprintf(&b, "_%s_\n\n", escapeMarkdown(e.Content))
        }
    }
    return b.String()
}

func escapeMarkdown(s string) string {
    var b strings.Builder
    for _, r := range s {
        switch r {
        case '*', '_', '[', ']', '`', '\\':
            b.WriteByte('\\')
        }
        b.WriteRune(r)
    }
    return b.String()
}
```

- [ ] **Step 4: Run, verify PASS**
- [ ] **Step 5: Commit**

### Task C2: Plain-text renderer + golden file

Same shape; `renderPlainText([]Entry) string` strips bolding to produce `Alice smiles. Alice says, "Hi." ...`. Golden file in `plugins/core-scenes/testdata/publish_render_plain_text.golden`.

- [ ] Steps 1–5

### Task C3: JSONL renderer + round-trip test

`renderJSONL([]Entry) ([]byte, error)` — emits one JSON object per line. Round-trip test: marshal then split-and-unmarshal should yield equal slice.

- [ ] Steps 1–5

### Task C4: `GetPublicSceneArchive` + opacity tests

**Files:**

- Modify: `plugins/core-scenes/publish_service.go`
- Create: `plugins/core-scenes/service_public_archive_test.go`

**Spec ref:** §5.1, §9.2, INV-P6-8

- [ ] **Step 1: Write failing opacity tests**

```go
func TestGetPublicSceneArchive_OpacityTable(t *testing.T) {
    t.Parallel()
    cases := []struct {
        name   string
        setup  func(*fakeStore)
        argID  string
    }{
        {"nonexistent id", func(*fakeStore) {}, "missing-id"},
        {"COLLECTING attempt", installAttempt("pub-1", StatusCollecting), "pub-1"},
        {"COOLOFF attempt", installAttempt("pub-2", StatusCoolOff), "pub-2"},
        {"ATTEMPT_FAILED attempt", installAttempt("pub-3", StatusAttemptFailed), "pub-3"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            store := newFakeStore()
            tc.setup(store)
            svc := NewSceneServiceImpl(store)
            _, err := svc.GetPublicSceneArchive(context.Background(), &scenev1.GetPublicSceneArchiveRequest{PublishedSceneId: tc.argID})
            require.Error(t, err)
            assert.Equal(t, codes.NotFound, status.Code(err),
                "INV-P6-8: opaque NOT_FOUND for all non-PUBLISHED states")
            assert.Equal(t, "scene archive not found", status.Convert(err).Message(),
                "INV-P6-8: identical wire message across non-PUBLISHED states")
        })
    }
}

func TestGetPublicSceneArchive_PublishedReturnsContent(t *testing.T) {
    store := newFakeStoreWithPublishedScene("pub-1", "scene-1", []Entry{
        {Speaker: "Alice", Kind: EntryKindSay, Content: "Hello."},
    })
    svc := NewSceneServiceImpl(store)
    resp, err := svc.GetPublicSceneArchive(context.Background(), &scenev1.GetPublicSceneArchiveRequest{PublishedSceneId: "pub-1"})
    require.NoError(t, err)
    require.Len(t, resp.ContentEntries, 1)
    assert.Equal(t, "Alice", resp.ContentEntries[0].Speaker)
}
```

- [ ] **Step 2: Run, verify FAIL**
- [ ] **Step 3: Implement** per spec §9.2:

```go
func (s *SceneServiceImpl) GetPublicSceneArchive(ctx context.Context, req *scenev1.GetPublicSceneArchiveRequest) (*scenev1.GetPublicSceneArchiveResponse, error) {
    ctx, span := startSpan(ctx, "scene.service.get_public_scene_archive",
        attribute.String("published_scene_id", req.GetPublishedSceneId()))
    defer span.End()

    pub, err := s.store.GetPublishedSceneHeader(ctx, req.GetPublishedSceneId())
    if err != nil {
        return nil, mapStoreErr(err)
    }
    if pub == nil || pub.Status != StatusPublished {
        return nil, status.Error(codes.NotFound, "scene archive not found")
    }
    entries, err := s.store.GetPublishedSceneContent(ctx, req.GetPublishedSceneId())
    if err != nil { return nil, internalErr(err) }
    return assemblePublicResponse(pub, entries), nil
}
```

- [ ] **Step 4: Run, verify PASS**
- [ ] **Step 5: Commit**

### Task C5: `DownloadPublicSceneArchive` + opacity tests

Same shape as C4, but routes to a renderer based on the `format` field. Add to `service_public_archive_test.go`.

- [ ] Steps 1–5

### Task C6: Snapshot pipeline scaffolding — `ReadSceneLogForSnapshot`

**Files:**

- Modify: `plugins/core-scenes/publish_store.go`
- Modify: `plugins/core-scenes/store_integration_test.go`

**Spec ref:** §11.1

- [ ] **Step 1: Write failing integration test** — insert mixed events (poses + says + emits + OOC + ops) into scene_log; assert `ReadSceneLogForSnapshot` returns only pose/say/emit in chronological order.
- [ ] **Step 2: Run, verify FAIL**
- [ ] **Step 3: Implement**

```go
func (s *SceneStore) ReadSceneLogForSnapshot(ctx context.Context, tx pgx.Tx, sceneID string) ([]LogRow, error) {
    subject := fmt.Sprintf("events.%s.scene.%s.ic", s.gameID, sceneID)
    rows, err := tx.Query(ctx, `
        SELECT id, type, payload, schema_ver, codec, dek_ref, dek_version
        FROM scene_log
        WHERE subject = $1 AND type IN ('scene_pose','scene_say','scene_emit')
        ORDER BY id ASC
    `, subject)
    // ... scan into []LogRow ...
}
```

- [ ] **Step 4: Run, verify PASS**
- [ ] **Step 5: Commit**

### Task C7: Snapshot pipeline — decrypt + render + atomic transition

**Files:**

- Create: `plugins/core-scenes/publish_snapshot.go`
- Create: `plugins/core-scenes/publish_snapshot_integration_test.go`

**Spec ref:** §11.2, §11.3, INV-P6-10

- [ ] **Step 1: Write failing integration test for happy path: PUBLISHED transition with content_entries populated**
- [ ] **Step 2: Run, verify FAIL**
- [ ] **Step 3: Implement `runSnapshot`** per spec §11 pseudocode (in design doc lines 487-553)
- [ ] **Step 4: Write additional failing tests for failure modes** (decrypt fail, render fail, idempotent re-run)
- [ ] **Step 5: Implement failure-mode handling**
- [ ] **Step 6: Run, verify all PASS**
- [ ] **Step 7: Commit**

```bash
jj describe -m "feat(scene-publish): snapshot pipeline COOLOFF → PUBLISHED

Atomic transaction: SELECT FOR UPDATE → re-validate all-yes → read
scene_log rows for pose/say/emit → decrypt sensitive payloads → render
to JSONB entries → UPDATE status PUBLISHED + content + participants
snapshot → archive parent scene. Failure modes transition to ATTEMPT_
FAILED with specific reason. INV-P6-10 atomicity asserted by idempotent
re-fire test. Crypto-reviewer must pass before push."
```

---

## Phase D — Events, Crypto Manifest, Observability

Phase D adds the 6 new IC stream event types (sensitivity:never), wires emission into every state transition, adds the 7 metric stubs, and creates the resolver meta-test enforcing INV-P6-7.

**Spec coverage:** §7 event emission, §10 privacy block triple signal, §13 observability, INV-P6-7, INV-P6-9.

### Task D1: Update `plugin.yaml` crypto.emits with 6 new types

**Files:**

- Modify: `plugins/core-scenes/plugin.yaml`

**Spec ref:** §7

- [ ] **Step 1: Append the 6 new entries under `crypto.emits` in the existing list (after `scene_idle_nudge`)**

```yaml
    - event_type: scene_publish_started
      sensitivity: never
      description: "Notice that a scene publish-vote attempt has started; emit time + roster, no content."
    - event_type: scene_publish_vote_cast
      sensitivity: never
      description: "Notice that a roster member cast or changed a vote; character + vote + is_change, no content."
    - event_type: scene_publish_cooloff_started
      sensitivity: never
      description: "Notice that all roster members have voted yes and the cool-off window has begun; ends_at timestamp, no content."
    - event_type: scene_publish_resolved
      sensitivity: never
      description: "Notice of terminal transition (PUBLISHED or ATTEMPT_FAILED) with outcome + reason + tally summary, no IC content."
    - event_type: scene_publish_withdrawn
      sensitivity: never
      description: "Notice that the scene owner withdrew the active publish attempt; emitted alongside scene_publish_resolved."
    - event_type: scene_publish_vote_attempts_extended
      sensitivity: never
      description: "Notice that an admin extended the per-scene publish-attempts budget; new_max + admin_id, no content."
```

- [ ] **Step 2: Run plugin schema lint**

Run: `task lint` (rumdl + schema validation)
Expected: PASS — manifest schema accepts the additive entries.

- [ ] **Step 3: Run crypto-reviewer (the agent gate)**

This step requires the user to run `/review-crypto` against the manifest diff. The crypto-reviewer agent verifies the additive declarations match repo invariants. Note in the commit body that the gate fired.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(scene-manifest): declare 6 Phase 6 publish notice events

crypto.emits additions: scene_publish_started, scene_publish_vote_cast,
scene_publish_cooloff_started, scene_publish_resolved,
scene_publish_withdrawn, scene_publish_vote_attempts_extended. All
sensitivity:never — operational metadata, no IC content. Crypto-
reviewer: READY (additive never declarations; no payload-encryption
rule changes)."
```

### Task D2: Event emission helpers (`publish_events.go`)

**Files:**

- Create: `plugins/core-scenes/publish_events.go`
- Modify: `plugins/core-scenes/publish_service.go` (wire `s.events` field)

**Spec ref:** §7

**Design note:** Real emit shape from `commands.go:888-894` in `handleEmit`:

```go
intent := pluginsdk.EmitIntent{
    Subject: subject,
    Type:    pluginsdk.EventType(eventType),
    Payload: payloadBytes,
    // Headers, ActorKind, ActorID etc. set per emit context
}
if err := p.service.eventSink.Emit(ctx, intent); err != nil { ... }
```

Phase 6 emitter follows this same shape. The `SceneServiceImpl` already has an `eventSink pluginsdk.EventSink` field (set via `SetEventSink`, mirroring the focusClient wiring from Phase 5 / `commands.go:879`). Phase 6 adds a thin wrapper `publishEventEmitter` that holds an `EventSink` reference and exposes one method per event type with typed signatures so handler call-sites stay readable.

- [ ] **Step 1: Write failing tests**

```go
// publish_events_test.go (unit, against a fake EventSink that records intents)

type recordingEventSink struct {
    intents []pluginsdk.EmitIntent
    err     error
}

func (r *recordingEventSink) Emit(_ context.Context, intent pluginsdk.EmitIntent) error {
    r.intents = append(r.intents, intent)
    return r.err
}

func TestEmitPublishStarted_SetsSubjectTypeAndPayload(t *testing.T) {
    t.Parallel()
    sink := &recordingEventSink{}
    // newPublishEventEmitter needs a store reference to fetch the roster
    // on emitPublishStarted (the handler doesn't pass roster explicitly).
    fakeStore := newFakeStoreWithRoster("pub-1", []string{"char-1", "char-2"})
    em := newPublishEventEmitter(sink, fakeStore, "test-game")
    pub := &PublishedScene{
        ID: "pub-1", SceneID: "scene-1", AttemptNumber: 1,
        InitiatedBy: "char-1",
        VoteWindow: 7 * 24 * time.Hour, CoolOffWindow: 30 * time.Minute,
    }
    err := em.emitPublishStarted(context.Background(), pub)
    require.NoError(t, err)
    require.Len(t, sink.intents, 1)
    intent := sink.intents[0]
    assert.Equal(t, "events.test-game.scene.scene-1.ic", intent.Subject)
    assert.Equal(t, pluginsdk.EventType("scene_publish_started"), intent.Type)
    // Decode payload and assert fields match.
    var ev scenev1.ScenePublishStartedEvent
    require.NoError(t, proto.Unmarshal(intent.Payload, &ev))
    assert.Equal(t, "pub-1", ev.AttemptId)
    assert.Equal(t, int32(1), ev.AttemptNumber)
    assert.Equal(t, "char-1", ev.InitiatedBy)
    assert.ElementsMatch(t, []string{"char-1", "char-2"}, ev.RosterCharacterIds)
}

// Repeat for emitVoteCast, emitCoolOffStarted, emitResolved, emitWithdrawn, emitAttemptsExtended.
```

- [ ] **Step 2: Run, verify FAIL**

Run: `task test -- -run "TestEmitPublish" ./plugins/core-scenes/`
Expected: FAIL — `undefined: newPublishEventEmitter`, missing event proto types.

- [ ] **Step 3: Add event proto messages to `scene.proto` + regenerate**

Append to `scene.proto` (one message per IC notice event):

```protobuf
message ScenePublishStartedEvent {
  string attempt_id = 1;
  int32 attempt_number = 2;
  string initiated_by = 3;
  int64 vote_window_seconds = 4;
  int64 cooloff_window_seconds = 5;
  repeated string roster_character_ids = 6;
}

message ScenePublishVoteCastEvent {
  string attempt_id = 1;
  string character_id = 2;
  bool vote = 3;
  bool is_change = 4;
}

message ScenePublishCoolOffStartedEvent {
  string attempt_id = 1;
  int64 cooloff_ends_at_unix_ns = 2;
}

message ScenePublishResolvedEvent {
  string attempt_id = 1;
  string outcome = 2;        // "PUBLISHED" | "ATTEMPT_FAILED"
  string failure_reason = 3; // empty unless ATTEMPT_FAILED
  int32 tally_yes = 4;
  int32 tally_no = 5;
  int32 tally_pending = 6;
}

message ScenePublishWithdrawnEvent {
  string attempt_id = 1;
  string withdrawn_by = 2;
}

message ScenePublishVoteAttemptsExtendedEvent {
  string scene_id = 1;
  int32 additional = 2;
  int32 new_max = 3;
  string admin_id = 4;
}
```

Run: `task proto`
Expected: regenerated `*.pb.go` files.

- [ ] **Step 4: Implement `publish_events.go`**

The struct must satisfy the `publishEventer` interface declared in Task B1.5 `publish_helpers.go`. All six methods take their inputs verbatim from the handler call sites; the emitter is responsible for fetching auxiliary data (roster) and computing the subject string. It needs a `sceneStorer` reference to load the roster on `emitPublishStarted`.

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"
    "fmt"
    "time"

    pluginsdk "github.com/holomush/holomush/pkg/plugin"
    scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
    "google.golang.org/protobuf/proto"
)

// publishEventEmitter is the production publishEventer (interface defined
// in publish_helpers.go). Wraps EventSink + sceneStorer; the latter so
// emitPublishStarted can load the roster without the handler needing to
// pass it explicitly.
//
// Every event emits on the scene IC subject events.<game_id>.scene.<scene_id>.ic
// with sensitivity:never per crypto.emits manifest entries.
type publishEventEmitter struct {
    sink   pluginsdk.EventSink
    store  sceneStorer
    gameID string
}

func newPublishEventEmitter(sink pluginsdk.EventSink, store sceneStorer, gameID string) *publishEventEmitter {
    return &publishEventEmitter{sink: sink, store: store, gameID: gameID}
}

func (e *publishEventEmitter) emitPublishStarted(ctx context.Context, pub *PublishedScene) error {
    voters, err := e.store.ListPublishVoters(ctx, pub.ID)
    if err != nil {
        return err
    }
    roster := make([]string, 0, len(voters))
    for _, v := range voters {
        roster = append(roster, v.CharacterID)
    }
    payload, err := proto.Marshal(&scenev1.ScenePublishStartedEvent{
        AttemptId:            pub.ID,
        AttemptNumber:        int32(pub.AttemptNumber),
        InitiatedBy:          pub.InitiatedBy,
        VoteWindowSeconds:    int64(pub.VoteWindow.Seconds()),
        CoolOffWindowSeconds: int64(pub.CoolOffWindow.Seconds()),
        RosterCharacterIds:   roster,
    })
    if err != nil {
        return err
    }
    return e.sink.Emit(ctx, pluginsdk.EmitIntent{
        Subject: e.icSubject(pub.SceneID),
        Type:    pluginsdk.EventType("scene_publish_started"),
        Payload: string(payload),  // EmitIntent.Payload is string per pkg/plugin/event.go:120
    })
}

func (e *publishEventEmitter) emitVoteCast(ctx context.Context, attemptID, characterID string, result *CastVoteResult) error {
    // We need the scene_id for the subject. Fetch from the header (cheap;
    // one row by primary key). Alternative: pass scene_id through the
    // handler call chain — rejected because it bloats every call site.
    pub, err := e.store.GetPublishedSceneHeader(ctx, attemptID)
    if err != nil {
        return err
    }
    payload, err := proto.Marshal(&scenev1.ScenePublishVoteCastEvent{
        AttemptId: attemptID, CharacterId: characterID,
        Vote: result.Vote, IsChange: result.IsChange,
    })
    if err != nil {
        return err
    }
    return e.sink.Emit(ctx, pluginsdk.EmitIntent{
        Subject: e.icSubject(pub.SceneID),
        Type:    pluginsdk.EventType("scene_publish_vote_cast"),
        Payload: string(payload),  // EmitIntent.Payload is string per pkg/plugin/event.go:120
    })
}

func (e *publishEventEmitter) emitCoolOffStarted(ctx context.Context, attemptID string, window time.Duration) error {
    pub, err := e.store.GetPublishedSceneHeader(ctx, attemptID)
    if err != nil {
        return err
    }
    payload, err := proto.Marshal(&scenev1.ScenePublishCoolOffStartedEvent{
        AttemptId:           attemptID,
        CoolOffEndsAtUnixNs: time.Now().Add(window).UnixNano(),
    })
    if err != nil {
        return err
    }
    return e.sink.Emit(ctx, pluginsdk.EmitIntent{
        Subject: e.icSubject(pub.SceneID),
        Type:    pluginsdk.EventType("scene_publish_cooloff_started"),
        Payload: string(payload),  // EmitIntent.Payload is string per pkg/plugin/event.go:120
    })
}

func (e *publishEventEmitter) emitResolved(ctx context.Context, attemptID string, finalStatus PublishedSceneStatus, reason *PublishFailureReason, tally *VoteTally) error {
    pub, err := e.store.GetPublishedSceneHeader(ctx, attemptID)
    if err != nil {
        return err
    }
    reasonStr := ""
    if reason != nil {
        reasonStr = string(*reason)
    }
    var y, n, p int32
    if tally != nil {
        y, n, p = int32(tally.Yes), int32(tally.No), int32(tally.Pending)
    }
    payload, err := proto.Marshal(&scenev1.ScenePublishResolvedEvent{
        AttemptId:     attemptID,
        Outcome:       string(finalStatus),
        FailureReason: reasonStr,
        TallyYes:      y,
        TallyNo:       n,
        TallyPending:  p,
    })
    if err != nil {
        return err
    }
    return e.sink.Emit(ctx, pluginsdk.EmitIntent{
        Subject: e.icSubject(pub.SceneID),
        Type:    pluginsdk.EventType("scene_publish_resolved"),
        Payload: string(payload),  // EmitIntent.Payload is string per pkg/plugin/event.go:120
    })
}

func (e *publishEventEmitter) emitWithdrawn(ctx context.Context, attemptID, withdrawnBy string) error {
    pub, err := e.store.GetPublishedSceneHeader(ctx, attemptID)
    if err != nil {
        return err
    }
    payload, err := proto.Marshal(&scenev1.ScenePublishWithdrawnEvent{
        AttemptId: attemptID, WithdrawnBy: withdrawnBy,
    })
    if err != nil {
        return err
    }
    return e.sink.Emit(ctx, pluginsdk.EmitIntent{
        Subject: e.icSubject(pub.SceneID),
        Type:    pluginsdk.EventType("scene_publish_withdrawn"),
        Payload: string(payload),  // EmitIntent.Payload is string per pkg/plugin/event.go:120
    })
}

func (e *publishEventEmitter) emitAttemptsExtended(ctx context.Context, sceneID, adminID string, additional, newMax int) error {
    payload, err := proto.Marshal(&scenev1.ScenePublishVoteAttemptsExtendedEvent{
        SceneId: sceneID, AdminId: adminID,
        Additional: int32(additional), NewMax: int32(newMax),
    })
    if err != nil {
        return err
    }
    return e.sink.Emit(ctx, pluginsdk.EmitIntent{
        Subject: e.icSubject(sceneID),
        Type:    pluginsdk.EventType("scene_publish_vote_attempts_extended"),
        Payload: string(payload),  // EmitIntent.Payload is string per pkg/plugin/event.go:120
    })
}

func (e *publishEventEmitter) icSubject(sceneID string) string {
    return fmt.Sprintf("events.%s.scene.%s.ic", e.gameID, sceneID)
}
```

Also add a wiring method on `SceneServiceImpl` (in `service.go`) so the plugin's `main.go` lifecycle can replace the noop default with the real emitter once the event sink is available:

```go
// SetPublishEventer replaces the default no-op publish eventer with the
// supplied implementation. Called from main.go after SetEventSink so the
// real publishEventEmitter (which needs an EventSink + store) is wired
// before any RPC handler fires.
func (s *SceneServiceImpl) SetPublishEventer(e publishEventer) {
    s.events = e
}
```

And in `main.go`, wire the real emitter after `SetEventSink`:

```go
plugin.service.SetEventSink(sink)
plugin.service.SetPublishEventer(newPublishEventEmitter(sink, plugin.service.store, plugin.service.gameID))
```

- [ ] **Step 5: Run, verify PASS**

Run: `task test -- -run "TestEmitPublish" ./plugins/core-scenes/`
Expected: PASS (one subtest per emit method).

- [ ] **Step 6: Commit**

```bash
jj describe -m "feat(scene-publish): event emitter for 6 IC notice events

publish_events.go: typed Emit wrappers per IC notice event type
(started/vote_cast/cooloff_started/resolved/withdrawn/extended).
Real impl marshals proto + emits via pluginsdk.EventSink.Emit with
EmitIntent matching commands.go:888-894 shape. noopPublishEventEmitter
is the Phase-B default for tests that don't exercise event substrate.
Proto messages added to scene.proto and regenerated.
Spec section 7 (holomush-5rh.15)."
```

### Task D3: Wire emission into state-machine transitions

**Files:**

- Modify: `plugins/core-scenes/publish_service.go`
- Modify: `plugins/core-scenes/publish_snapshot.go`

Connect `s.events.emit*` calls at every state transition site identified in Phase A `applyTrigger`.

- [ ] Steps 1–5

### Task D4: Event emission integration tests

**Files:**

- Create: `plugins/core-scenes/publish_event_emission_integration_test.go`

Ginkgo specs running against a real eventbus harness (NOT eventbustest — use the integrationtest harness's NATS embed).

- [ ] Steps 1–5

### Task D5: Resolver meta-test — INV-P6-7

**Files:**

- Modify: `plugins/core-scenes/resolver_test.go`

**Spec ref:** §9.3, INV-P6-7

- [ ] **Step 1: Write failing test**

```go
func TestResolverNeverExposesContentByForbiddenAttributeName(t *testing.T) {
    t.Parallel()
    r := newTestResolver()
    schema, err := r.GetSchema(context.Background(), &pluginv1.GetSchemaRequest{})
    require.NoError(t, err)

    sceneType, ok := schemaTypeByName(schema, "scene")
    require.True(t, ok)

    forbidden := regexp.MustCompile(`^(content|content_entries|poses?|says?|emits?|ooc|log|entries|publication)$`)
    for _, attr := range sceneType.Attributes {
        assert.False(t, forbidden.MatchString(attr.Name),
            "INV-P6-7 violation: resolver exposes attribute %q matching forbidden pattern", attr.Name)
    }
}
```

- [ ] **Step 2: Run, verify PASS immediately** (since `resolver.go` is unchanged — this test is a regression lock for future PRs).
- [ ] **Step 3: Commit**

### Task D6: Metric stubs in `metrics.go`

**Files:**

- Modify: `plugins/core-scenes/metrics.go`

**Spec ref:** §13.1

- [ ] **Step 1: Append 7 stubs** matching the existing pattern at `metrics.go:24-121`:

```go
//nolint:unused // stub for the future binary plugin metrics infrastructure (spec section 13); intentionally retained so call sites can be wired in cheaply once that infrastructure lands
func metricScenePublishAttemptResolved(outcome, reason string) {
    _ = outcome
    _ = reason
}

//nolint:unused // stub for the future binary plugin metrics infrastructure (spec section 13)
func metricScenePublishVoteCast(vote, isChange string) {
    _ = vote
    _ = isChange
}

// ... 5 more stubs: VoteWindowDuration, CoolOffWindowDuration, SnapshotDuration, PrivacyBlock, ActiveAttempts
```

- [ ] **Step 2: Run `task lint`** — verify no unused-function lint failures (the `//nolint:unused` directives prevent them).
- [ ] **Step 3: Commit**

### Task D7: Privacy boundary block tests

**Files:**

- Create: `plugins/core-scenes/service_privacy_block_test.go`

Four tests — WARN log emit, metric increment, NO IC event emit (side-channel prevention), span error attribute set. Use a slog test handler + the metric-call shim from D6 + a recording event emitter + an OTel test span recorder.

- [ ] Steps 1–5

---

## Phase E — Admin Extend, cb4x Absorption, Scheduler, E2E

Phase E completes the publication surface: admin `ExtendScenePublishVoteAttempts`, the `scene log` / `scene log export` commands folded from `holomush-cb4x`, the scheduler that drives vote-window timeouts and cool-off expiry, and the cross-package E2E tests in `test/integration/scenes/` and the meta-test in `test/meta/`.

**Spec coverage:** §5 (Extend), §6 (`scene log` commands), §15.3 + §15.4 (E2E + meta).

### Task E1: `ExtendScenePublishVoteAttempts` RPC + ABAC

**Files:**

- Modify: `plugins/core-scenes/publish_service.go`
- Modify: `plugins/core-scenes/plugin.yaml` (add `admin-extend-publish-attempts` policy)
- Modify: `plugins/core-scenes/publish_service_test.go`

**Spec ref:** §5, §8

- [ ] **Step 1: Write failing tests** (admin success, non-admin rejection)
- [ ] **Step 2: Run, verify FAIL**
- [ ] **Step 3: Implement** + add policy
- [ ] **Step 4: Run, verify PASS**
- [ ] **Step 5: Commit**

### Task E2: `scene publish vote extend <count>` command

**Files:**

- Modify: `plugins/core-scenes/commands.go`
- Modify: `plugins/core-scenes/commands_test.go`

Routes to `ExtendScenePublishVoteAttempts`. ABAC gates non-admins; the command name itself is identical for everyone.

- [ ] Steps 1–5

### Task E3: `scene log` command (cb4x absorption)

**Files:**

- Modify: `plugins/core-scenes/commands.go`
- Modify: `plugins/core-scenes/commands_test.go`

Calls the existing `PluginAuditService.QueryHistory` (membership-gated; INV-S9 enforced by `audit.go:493-555`). Paginate output for terminal display.

- [ ] **Step 1: Write failing test** — happy path (participant), denial (non-participant).
- [ ] **Step 2: Run, verify FAIL**
- [ ] **Step 3: Implement `handleLog`**

```go
func (p *scenePlugin) handleLog(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
    sceneID, err := resolveSceneRef(ctx, p.service.store, req.CharacterID, args)
    if err != nil {
        return pluginsdk.Errorf("%s", err.Error()), nil
    }
    rows, err := p.queryHistoryForScene(ctx, sceneID, /* limit */ 50)
    if err != nil {
        return pluginsdk.Errorf("%s", err.Error()), nil
    }
    entries, err := decodeEntriesForReplay(rows)
    if err != nil {
        return pluginsdk.Errorf("%s", err.Error()), nil
    }
    return pluginsdk.OK(renderPlainText(entries)), nil
}
```

(`queryHistoryForScene` calls the plugin's own audit service via the in-process gRPC handle; same auth surface as the audit RPC.)

- [ ] **Step 4: Run, verify PASS**
- [ ] **Step 5: Commit**

### Task E4: `scene log export <format>` command

**Files:**

- Modify: `plugins/core-scenes/commands.go`
- Modify: `plugins/core-scenes/commands_test.go`

Routes to a separate handler that runs the renderer for the chosen format on the same audit-history payload that `scene log` uses.

- [ ] Steps 1–5

### Task E5: Scheduler — vote-window timeout + cool-off expiry tickers

**Files:**

- Create: `plugins/core-scenes/publish_scheduler.go`
- Modify: `plugins/core-scenes/main.go` (wire into lifecycle)
- Create: `plugins/core-scenes/publish_scheduler_integration_test.go`

**Spec ref:** §4.3

- [ ] **Step 1: Write failing integration test** — start an attempt with `vote_window=2s`, leave one voter pending, advance 2s of mock time, assert `ATTEMPT_FAILED` with `reason=TIMEOUT`.
- [ ] **Step 2: Run, verify FAIL**
- [ ] **Step 3: Implement scheduler**

```go
type publishScheduler struct {
    store sceneStorer
    svc   *SceneServiceImpl
    tick  *time.Ticker
}

func (s *publishScheduler) Run(ctx context.Context) {
    defer s.tick.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-s.tick.C:
            if err := s.sweep(ctx); err != nil {
                slog.WarnContext(ctx, "publish scheduler sweep failed", "err", err)
            }
        }
    }
}

func (s *publishScheduler) sweep(ctx context.Context) error {
    // 1. SELECT id FROM published_scenes WHERE status='COLLECTING' AND initiated_at + vote_window <= now()
    //    → apply TriggerTimeout
    // 2. SELECT id FROM published_scenes WHERE status='COOLOFF' AND cooloff_started_at + cooloff_window <= now()
    //    → run snapshot pipeline
    return nil // full impl
}
```

Wire `publishScheduler.Run(ctx)` into the plugin's `main()` startup via a goroutine launched alongside the existing focus consumer.

- [ ] **Step 4: Run, verify PASS**
- [ ] **Step 5: Commit**

### Task E6: E2E happy-path test

**Files:**

- Create: `test/integration/scenes/publish_e2e_test.go`

Ginkgo spec using `internal/testsupport/integrationtest`. Alice creates scene, joins, ends, runs `scene publish`, votes yes, Bob votes yes, cool-off expires, scene transitions to PUBLISHED. Public RPC returns content; participant RPC also returns content.

- [ ] Steps 1–5

### Task E7: E2E privacy gate test

**Files:**

- Modify: `test/integration/scenes/publish_e2e_test.go`

Non-participant Charlie calls `GetPublishedScene` → PermissionDenied with WARN log + metric stub call. Charlie calls `GetPublicSceneArchive` on COLLECTING attempt → NOT_FOUND. Charlie calls same RPC after PUBLISHED → success.

- [ ] Steps 1–5

### Task E8: E2E retry + admin extend test

Same file, new `Context` block. 3 failed attempts; 4th rejected; admin extends; 4th succeeds.

- [ ] Steps 1–5

### Task E9: E2E history-scope-privacy floor test

**Files:**

- Create: `test/integration/scenes/publish_history_scope_e2e_test.go`

Per spec §15.3: a participant who joined the scene AFTER an attempt's `scene_publish_started` cannot see that started event in their history-scope-floor view.

- [ ] Steps 1–5

### Task E10: Meta-test for INV-P6-1..10

**Files:**

- Create: `test/meta/scenes_phase6_invariants_test.go`

Enumerates each invariant ID and asserts at least one test cites it. Pattern: parse all `*_test.go` files for `INV-P6-N` substrings in test bodies / comments; require non-zero for each of 1..10.

- [ ] Steps 1–5

---

## Post-Implementation Checklist

After Task E10 lands:

- [ ] Run `task test` — all unit tests pass with no warnings
- [ ] Run `task test:int` — all integration + E2E tests pass
- [ ] Run `task test:cover` — verify ≥85% coverage on `plugins/core-scenes/` overall; ≥90% on new `publish_*.go` files
- [ ] Run `task lint` — clean (no `//nolint` additions that bypass real issues)
- [ ] Run `task pr-prep` — full pipeline green
- [ ] Run `/review-crypto` final pass — ensures the snapshot decrypt path didn't introduce AAD or fence violations
- [ ] Run `/review-abac` — ensures the 3 new ABAC policies are sound and don't shadow existing rules
- [ ] Run `/review-code` — adversarial code review
- [ ] Update `docs/roadmap.md` — move Phase 6 work from "frontier" to shipped in the social-spaces theme section
- [ ] Close `holomush-5rh.15` with `--reason="Phase 6 shipped in PR #<n>"`
- [ ] Close `holomush-cb4x` with `--reason="Folded into Phase 6 (5rh.15)"`
- [ ] Squash-merge PR
- [ ] Clean up isolated workspace per `.claude/rules/landing-the-plane.md` step 5

## Risk Items

- **Crypto decryption at snapshot time** — Phase 6 introduces a new caller of the per-event-DEK decrypt path. The crypto-reviewer agent MUST pass before push.
- **Scheduler concurrency vs. RPC handlers** — both can fire `applyTrigger` on the same attempt; SELECT FOR UPDATE in `LockForSnapshot` and precondition WHERE clauses in `TransitionStatus` provide serialization, but the race test in §15.3 must be green.
- **JSONB indexing growth** — `content_entries` is unindexed JSONB. Long scenes (10k+ poses) produce large rows; if queries against `content_entries` ever surface as a need, a separate audit follow-up bead is required.

## Out of Scope (Future Work)

- Phase 8 (`5rh.17`) — scene board + content warnings (public discovery surface for `GetPublicSceneArchive`)
- Phase 9 (`5rh.18`) — web chat view + pending-vote dashboard
- "Resume from ended" scene capability — the §4 state-machine choice preserves the option without committing to it
- Multi-format publication storage (HTML, PDF) — derive at request time from JSONB content_entries
- Per-participant publication revocation — once PUBLISHED, immutable

<!-- adr-capture: sha256=35c26bc314cd05a3; session=cli; ts=2026-05-23T23:20:33Z; adrs=qd3r5,jrefa,e3xlx,39a5f,c4jee -->
