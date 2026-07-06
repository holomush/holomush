<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Scenes Phase 3: Membership Design

**Date:** 2026-04-07
**Bead:** holomush-5rh.12
**Parent spec:** [2026-04-06-scenes-and-rp-design-v2.md](2026-04-06-scenes-and-rp-design-v2.md) (sections 1.3, 5.4, 6.2, 7.2)
**Prior phases:** Phase 1 (foundation, PR #196), Phase 2 (lifecycle state machine, PR #200)

## Overview

Phase 3 of the scenes rework introduces the participant model and the membership operations
(`scene join`, `scene leave`, `scene invite`, `scene kick`, `scene transfer-ownership`). It also
introduces an append-only operations event journal that captures all membership transitions
plus the lifecycle and settings transitions emitted by Phase 1+2 handlers (retrofitted in this
phase). The Phase 2 placeholder ABAC policies that limited reads and resumes to the scene owner
are replaced with member-based policies, fulfilling the v2 spec section 5.4 phase 3 plan.

This document elaborates the v2 spec for the implementation of Phase 3 only. All product
semantics defined in the v2 spec remain authoritative; this document records the implementation
decisions and the additions to the v2 spec that were made during brainstorming.

## RFC2119 Keywords

The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are used per RFC2119.

## Scope

### In scope

- `scene_participants` table (migration 000003)
- `scene_ops_events` table (same migration) — append-only ops journal for membership, lifecycle,
  and settings transitions
- `ParticipantRole` and `OpsEventKind` enums
- Store methods: `CreateWithOwner`, `GetWithMembership`, `AddParticipant`, `RemoveParticipant`,
  `InviteParticipant`, `KickParticipant`, `TransferOwnership`, `ListParticipants`,
  `GetParticipant`
- gRPC RPCs: `JoinScene`, `LeaveScene`, `InviteToScene`, `KickFromScene`, `TransferOwnership`
- Proto additions: `KickFromSceneRequest`/`Response`, `TransferOwnershipRequest`/`Response`
- Resolver extensions: `participants` and `invitees` STRING_LIST attributes
- ABAC policy replacement: clean swap of owner-only policies for member-based policies
- Command handlers: `scene join`, `scene leave`, `scene invite`, `scene kick`, `scene transfer`
- Phase 1+2 handler retrofits: emit `lifecycle.*` and `settings.updated` ops events from
  existing `CreateScene` / `EndScene` / `PauseScene` / `ResumeScene` / `UpdateScene` handlers;
  `CreateScene` becomes transactional and inserts the owner participant row
- Metric stubs for new membership and ops events
- Test coverage matching the Phase 1+2 ACE-naming convention with positive and negative paths

### Out of scope (deferred to later phases)

- `scene_journal` table for IC/OOC content events (Phase 4)
- Pose order computation (Phase 4)
- Idle timeout reaper (Phase 4)
- Notification stream emissions to kicked or transferred-to recipients (Phase 5)
- Focus auto-switch on terminal join (Phase 5)
- Read-side `ListMembershipEvents` RPC and `scene history` UI (Phase 6+)
- Admin scene-delete and content retention purge operations (Phase 6+)
- `GetSceneRequest`/`Response` gaining a participants list — the `ParticipantInfo` proto
  already exists; populating it is a small follow-up but not load-bearing for Phase 3

## Design Decisions

These decisions were made during brainstorming on 2026-04-07 and override or extend the v2 spec
where they conflict.

| # | Decision | Rationale |
|---|----------|-----------|
| P3.D1 | `invited` is a transient role that exists only on private scenes. Open-scene joins create `member` rows directly; private-scene joins promote `invited` to `member`. | Simpler than universal invite/accept; matches v2 spec section 5.4 split between open-join and private-join policies. |
| P3.D2 | Kick is a hard delete (`DELETE WHERE role <> 'owner'`), with operation-level audit captured via `scene_ops_events`. The `kick` action removes both `member` and `invited` rows (i.e., `scene kick` also withdraws pending invitations). | Avoids the soft-delete privacy footgun (one missed `WHERE removed_at IS NULL` = leaked membership). The audit record lives in a separate, durable table rather than relying on ephemeral observability tooling. |
| P3.D3 | Audit lives in a dedicated `scene_ops_events` table (not in observability tooling, not in a unified IC/OOC events table). | Loki retention is 30 days, traces are sampled, metrics aggregate away identity. Audit needs durable storage. Separation from future content events keeps lifecycle boundaries (purge, GDPR right-to-be-forgotten, full delete) structurally enforced by table identity rather than `WHERE` clause discipline. |
| P3.D4 | `scene_ops_events` captures membership ops (Phase 3) AND lifecycle/settings ops (Phase 1+2 retrofit). It does NOT capture IC/OOC content events — those land in a separate `scene_journal` table in Phase 4. | The two categories have different retention semantics. Ops events are small-volume, never purged. Content events are large-volume, retention-policy-eligible. Different lifecycles → different tables. |
| P3.D5 | `JoinScene` is idempotent on identity match and atomically promotes `invited → member`. Implemented as a single SELECT-WHERE-guarded UPSERT with a `(xmax = 0)` insert/update discriminator. The store method returns a `ParticipantOpResult` enum (`OpInserted`, `OpPromoted`, `OpNoChange`) that drives whether the handler emits a `membership.join` ops event. | Race-safe by construction (one statement, no SELECT-then-INSERT). Network retries become no-ops. Same-character concurrent joins are no-ops. Different-character concurrent joins don't conflict. Retry semantics are transparent: `OpNoChange` produces no spurious ops event so the audit log isn't polluted by retries. |
| P3.D6 | `CreateScene` MUST be transactional and MUST insert the owner participant row in the same transaction as the `scenes` row insert and the `lifecycle.created` ops event. The Phase 1 `Create` store method is replaced by `CreateWithOwner`. | Phase 3's ABAC policies use `principal.id in resource.scene.participants` for read/write/resume. An owner without a participant row would lose access to their own scene under the new policies. The "create + insert owner row + emit ops event" trio is one atomic act. |
| P3.D7 | Owners cannot leave their own scene. `LeaveScene` rejects with `FailedPrecondition` and an actionable hint message ("scene owners cannot leave; use `scene end` to terminate the scene or transfer ownership first"). The store method also defends with a `WHERE role <> 'owner'` filter. | Avoids the "ownerless scene" state and the implicit-ownership-transfer surprise. The escape hatch is `scene end` or the new `scene transfer-ownership` command. |
| P3.D8 | `TransferOwnership` is in Phase 3 scope. Target MUST be an existing `member`. No recipient consent step (consent already happened at join). Previous owner becomes a `member`. State-gated to `active` or `paused`. Implemented as a 3-statement transaction (`scene_participants` demotion + `scene_participants` promotion + `scenes.owner_id` denorm update). | Without transfer, the owner-leave UX is "you're stuck, end your scene." That contradicts the spirit of giving owners a graceful exit. Members-only restriction prevents transfer + invite + auto-join from happening in one magic command. |
| P3.D9 | The resolver materializes membership lists via a single SQL query (`GetWithMembership`) with two `array_agg` subselects (`participants` and `invitees`). No caching layer in Phase 3. | Single round trip per ABAC eval keeps the hot path fast at expected QPS. Subselects use the indexed `scene_participants(scene_id, role)` index. Caching is deferred until profiling shows the resolver is a bottleneck — at which point PR #188's policy cache infrastructure provides the slot. |
| P3.D10 | The `scene_ops_events.kind` column uses dotted namespacing (`<category>.<verb>`) and a format-only CHECK constraint (`kind ~ '^[a-z]+\.[a-z_]+$'`). Adding new kinds within an existing category does NOT require a migration. The Go API enforces valid kinds via narrow `OpsEventKind` constants. | Format CHECK catches typos at the DB level. Future Phase 4/6 expansions add new kinds without schema migrations. Go-side enforcement prevents typo propagation in handlers. |
| P3.D11 | The Phase 2 `resume-own-scene` policy is replaced by `resume-scene-as-participant` with no transitional period. Plugin policies are loaded atomically when the plugin process boots; there is no rolling deployment of policies independent of the plugin binary. | No version skew is possible. Clean break, single PR. |
| P3.D12 | `JoinScene` returns `NotFound` (distinct from `PermissionDenied`) when the scene does not exist. ULIDs are large enough that blind enumeration is computationally infeasible, so distinguishing "not found" from "no permission" does not constitute a meaningful information leak. The privacy property at stake in v2 spec section 5.5 is content access, not existence confirmation. | Better diagnostics for legit clients hitting bugs. The structured log records the precise reason regardless of which gRPC status the client sees. |

## Schema Additions

Migration `000003_scene_participants_and_ops_events.up.sql`. The migration MUST be idempotent
per the project migration guidelines.

```sql
-- Membership snapshot. Composite primary key on (scene_id, character_id) ensures a character
-- has at most one row per scene and gives the upsert path a clean conflict target.
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

-- Append-only ops journal. Captures membership transitions, lifecycle transitions, and
-- settings updates. Does NOT capture IC/OOC content (Phase 4 builds scene_journal for that
-- with a different retention model).
--
-- See docs/superpowers/specs/2026-04-07-scenes-phase-3-membership-design.md decision P3.D3
-- for the full rationale on why this is separate from observability tooling and from the
-- future content events table.
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

`ON DELETE CASCADE` on the `scene_id` foreign keys is intentional: when a future admin
delete operation removes a scene row (Phase 6+), the participants and ops events for that
scene MUST be removed as part of the same delete operation. The retention purge operation
for Phase 4's `scene_journal` content events will need to cascade similarly.

A matching `000003_scene_participants_and_ops_events.down.sql` migration MUST be provided
that drops both tables in reverse order. Per project conventions, every migration needs a
reversible pair.

### Ops event payload conventions

`scene_ops_events.payload` is JSONB to absorb kind-specific shapes without sparse columns
or schema sprawl. The conventions below are enforced by the Go-side `recordOpsEventTx`
helper, not by SQL constraints.

| Kind | `target_id` | `payload` |
|------|-------------|-----------|
| `membership.invite` | invitee character_id | `{}` |
| `membership.join` | self (character_id of joiner) | `{"visibility": "open"\|"private", "from_invited": false\|true}` |
| `membership.leave` | self | `{"prior_role": "member"}` |
| `membership.kick` | kicked character_id | `{"prior_role": "member"\|"invited"}` |
| `membership.ownership_transferred` | new owner character_id | `{"from": "<old_owner_id>"}` |
| `lifecycle.created` | NULL | `{"visibility": "open"\|"private", "from_template": false\|true}` |
| `lifecycle.ended` | NULL | `{"prior_state": "active"\|"paused"}` |
| `lifecycle.paused` | NULL | `{}` |
| `lifecycle.resumed` | NULL | `{}` |
| `settings.updated` | NULL | `{"paths": ["title", "tags", ...]}` (mask path *names* only — never values) |

`settings.updated` MUST contain only the field names that were updated, never the new values.
This keeps the ops table structurally free of any title, description, tag, or other content,
maintaining the boundary between the ops table (operational metadata) and the future
`scene_journal` table (content).

## Type Additions

`plugins/core-scenes/types.go` gains the participant role and ops event kind enums.

```go
// ParticipantRole represents a character's relationship to a scene.
//
// Per design decision P3.D1, `invited` is a transient role that exists only on private
// scenes. An invitation is a row that grants the holder permission to join (and to read
// scene metadata, in a later phase). Calling JoinScene on an invited scene atomically
// promotes the row to `member`. There is no `invited` row on open scenes.
type ParticipantRole string

const (
    ParticipantRoleOwner   ParticipantRole = "owner"
    ParticipantRoleMember  ParticipantRole = "member"
    ParticipantRoleInvited ParticipantRole = "invited"
)

func (r ParticipantRole) IsValid() bool {
    switch r {
    case ParticipantRoleOwner, ParticipantRoleMember, ParticipantRoleInvited:
        return true
    }
    return false
}

// OpsEventKind enumerates the recognised ops event kinds. The dotted naming convention is
// also enforced by the database CHECK constraint on scene_ops_events.kind.
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

func (k OpsEventKind) IsValid() bool { /* switch on the constants above */ }
```

`ParticipantRow` (the persistence-layer representation of a `scene_participants` row) lives
alongside `SceneRow` in `store.go`.

## Store API

The store API gains the methods listed below. All membership-mutating methods are
transactional and emit a corresponding `scene_ops_events` row inside the same transaction
as the `scene_participants` mutation. If either statement fails the entire operation rolls
back; the participant table and the ops table cannot get out of sync.

```go
// CreateWithOwner replaces the Phase 1 Create method. Inserts the scene row, the owner's
// participant row (role='owner'), and a lifecycle.created ops event in a single transaction.
func (s *SceneStore) CreateWithOwner(ctx context.Context, row *SceneRow) error

// GetWithMembership returns the scene row plus its participants and invitees lists. Used by
// the resolver to materialise ABAC attributes in a single round trip.
func (s *SceneStore) GetWithMembership(ctx context.Context, sceneID string) (*SceneRow, []string, []string, error)

// AddParticipant attempts to add `characterID` to `sceneID`. Idempotent on identity match.
// Atomically promotes invited→member. Returns OpInserted, OpPromoted, or OpNoChange via the
// ParticipantOpResult enum so the caller can decide whether to emit a membership.join event.
func (s *SceneStore) AddParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, ParticipantOpResult, error)

// RemoveParticipant deletes the participant row for `characterID` in `sceneID`. The DELETE
// has a `WHERE role <> 'owner'` filter for defense-in-depth: the service layer rejects
// owner-leave first, but the store-layer filter prevents accidental owner removal if the
// service-layer check is ever bypassed. Returns the removed row.
func (s *SceneStore) RemoveParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, error)

// InviteParticipant inserts a participant row with role='invited'. Idempotent on identity
// match (re-inviting an already-invited character is a no-op). Rejected for already-member
// targets and for owner targets (you can't invite the owner — they're already in).
func (s *SceneStore) InviteParticipant(ctx context.Context, sceneID, inviterID, targetID string) (*ParticipantRow, error)

// KickParticipant deletes the target's participant row. The DELETE filter is
// `WHERE role <> 'owner'` so the owner cannot be kicked even by themselves. Removes both
// `member` and `invited` rows (i.e., kick also withdraws pending invitations).
func (s *SceneStore) KickParticipant(ctx context.Context, sceneID, kickerID, targetID string) (*ParticipantRow, error)

// TransferOwnership performs the 3-statement transactional ownership swap:
//   1. Demote current owner (UPDATE scene_participants role member WHERE current owner)
//   2. Promote target (UPDATE scene_participants role owner WHERE current member)
//   3. Update denormalised owner_id (UPDATE scenes.owner_id)
// All three statements MUST succeed (non-empty RETURNING). Rolls back otherwise.
func (s *SceneStore) TransferOwnership(ctx context.Context, sceneID, currentOwnerID, newOwnerID string) error

// ListParticipants returns all participants for a scene, ordered by joined_at ASC.
func (s *SceneStore) ListParticipants(ctx context.Context, sceneID string) ([]ParticipantRow, error)

// GetParticipant returns a single participant row, or SCENE_PARTICIPANT_NOT_FOUND.
func (s *SceneStore) GetParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, error)
```

`ParticipantOpResult` is the return enum that drives the handler's emit-or-not decision:

```go
type ParticipantOpResult int
const (
    OpInserted  ParticipantOpResult = iota // Fresh row added
    OpPromoted                              // Existing invited row flipped to member
    OpNoChange                              // Caller was already a member
)
```

The retry semantics fall out of this directly: a network-flaky client that retries a join
gets `OpInserted` on the first call and `OpNoChange` on the retry. The handler emits exactly
one `membership.join` ops event regardless. There is no need for client-side idempotency
keys for this operation.

### `AddParticipant` SQL

The full upsert with eligibility precheck:

```sql
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
RETURNING *, (xmax = 0) AS was_inserted;
```

If the SELECT-WHERE returns zero rows the INSERT inserts nothing, RETURNING is empty, and
the store calls `classifyJoinMiss` to figure out which precondition failed. If a row exists
but wasn't a fresh INSERT (`xmax > 0`), the result is either `OpPromoted` (if the existing
role was `invited`) or `OpNoChange` (if the existing role was `member` or `owner`).

### Diagnostic classification helpers

The store includes two helpers that mirror the existing `classifyTransitionMiss` from
Phase 2. They issue a single diagnostic SELECT in the error path only — the happy path
remains a single round trip.

```go
// classifyJoinMiss is called when AddParticipant's RETURNING was empty. Returns one of
// SCENE_NOT_FOUND, SCENE_TRANSITION_FORBIDDEN (for ended/archived scenes), or
// SCENE_JOIN_NOT_INVITED (for private scenes without an invitation). The handler maps
// these to NotFound, FailedPrecondition, and PermissionDenied respectively.
func (s *SceneStore) classifyJoinMiss(ctx context.Context, sceneID, characterID string, span trace.Span) error

// classifyTransferMiss is called when TransferOwnership's any of the three UPDATE statements
// failed. Returns one of SCENE_NOT_FOUND, SCENE_TRANSITION_FORBIDDEN, SCENE_NOT_OWNER (the
// caller is not the current owner), SCENE_TRANSFER_TARGET_NOT_MEMBER (target isn't a member).
func (s *SceneStore) classifyTransferMiss(ctx context.Context, sceneID, currentOwnerID, newOwnerID string, span trace.Span) error
```

### Ops event recording helper

```go
// recordOpsEventTx inserts a scene_ops_events row inside an existing transaction. The kind
// MUST be one of the OpsEventKind constants — the helper does NOT accept arbitrary strings,
// preventing typos and ad-hoc kinds in handlers. The payload is marshalled to JSONB.
//
// Used by every transactional store method. Never called from a handler directly.
func recordOpsEventTx(ctx context.Context, tx pgx.Tx, sceneID string, kind OpsEventKind, actorID, targetID string, payload map[string]any) error
```

### Failure mode summary for `JoinScene`

| Scenario | Store error | gRPC status | Retryable? |
|----------|-------------|-------------|------------|
| Two different characters race for same open scene | none — both succeed | OK | N/A |
| Same character races itself | none — second is `OpNoChange` | OK | N/A |
| Already a member, calls join again | none — `OpNoChange` | OK | N/A |
| Invited character calls join (private scene) | none — `OpPromoted` | OK | N/A |
| Scene doesn't exist | `SCENE_NOT_FOUND` | NotFound | No |
| Scene is ended/archived | `SCENE_TRANSITION_FORBIDDEN` w/ `current_state` | FailedPrecondition | No |
| Private scene, no invite row | `SCENE_JOIN_NOT_INVITED` | PermissionDenied | No |
| Postgres deadlock | `SCENE_JOIN_FAILED` (wraps pgx error) | Aborted | Yes |
| Postgres connection lost | `SCENE_JOIN_FAILED` (wraps pgx error) | Unavailable | Yes |
| `protovalidate` rejects empty IDs | n/a (rejected before handler) | InvalidArgument | No |

## Service Layer

`plugins/core-scenes/service.go` gains five new gRPC handlers. The `sceneStorer` interface in
that file is extended with the new store methods.

### Handler list

| RPC | Handler | OTel span | Notes |
|-----|---------|-----------|-------|
| `JoinScene` | `JoinScene` | `scene.service.join_scene` | Calls `store.AddParticipant`, emits `membership.join` ops event when result is `OpInserted` or `OpPromoted` (NOT for `OpNoChange`). |
| `LeaveScene` | `LeaveScene` | `scene.service.leave_scene` | Service-layer pre-check rejects owner-leave with `FailedPrecondition` and the hint message. Otherwise calls `store.RemoveParticipant`. |
| `InviteToScene` | `InviteToScene` | `scene.service.invite_to_scene` | Calls `store.InviteParticipant`. |
| `KickFromScene` | `KickFromScene` | `scene.service.kick_from_scene` | Calls `store.KickParticipant`. |
| `TransferOwnership` | `TransferOwnership` | `scene.service.transfer_ownership` | Calls `store.TransferOwnership`. |

All handlers follow the Phase 1+2 logging convention: `slog.InfoContext` on success,
`slog.WarnContext` on store errors, with the relevant character/scene/target IDs in the
context fields. All handlers record errors on their span before returning.

### Owner-leave service-layer pre-check

```go
// service.go LeaveScene
func (s *SceneServiceImpl) LeaveScene(ctx context.Context, req *scenev1.LeaveSceneRequest) (*scenev1.LeaveSceneResponse, error) {
    ctx, span := startSpan(ctx, "scene.service.leave_scene", ...)
    defer span.End()

    // Pre-check: owner cannot leave their own scene. Defense-in-depth: the store also has
    // a `WHERE role <> 'owner'` filter so this can't be bypassed by direct store calls.
    sceneRow, err := s.store.Get(ctx, req.GetSceneId())
    if err != nil {
        // Map SCENE_NOT_FOUND, etc.
    }
    if sceneRow.OwnerID == req.GetCharacterId() {
        return nil, status.Errorf(codes.FailedPrecondition,
            "scene owners cannot leave; use `scene end` to terminate the scene or transfer ownership first")
    }

    _, err = s.store.RemoveParticipant(ctx, req.GetSceneId(), req.GetCharacterId())
    // ...standard error mapping and ops event handled in store
}
```

### Phase 1+2 handler retrofits

The following Phase 1+2 handlers are modified to record their corresponding ops event in the
same transaction as the existing scene mutation. This requires each handler's store method to
become transactional if it isn't already.

| Handler | New ops event recorded | Payload |
|---------|------------------------|---------|
| `CreateScene` | `lifecycle.created` | `{"visibility": ..., "from_template": ...}` (also inserts owner participant row, see P3.D6) |
| `EndScene` | `lifecycle.ended` | `{"prior_state": ...}` (the prior_state comes from the SCENE_TRANSITION_FORBIDDEN context if the WHERE clause matched a 'paused' scene, otherwise 'active') |
| `PauseScene` | `lifecycle.paused` | `{}` |
| `ResumeScene` | `lifecycle.resumed` | `{}` |
| `UpdateScene` | `settings.updated` | `{"paths": [<mask path names>]}` |

The Phase 1+2 store methods (`Create`, `End`, `Pause`, `Resume`, `Update`) become
transactional wrappers around the original SQL plus the `recordOpsEventTx` call.

## Proto Additions

`api/proto/holomush/scene/v1/scene.proto` already declares `JoinScene`, `LeaveScene`, and
`InviteToScene` (Phase 3 was anticipated). Phase 3 adds two RPCs that are not yet present:

```proto
rpc KickFromScene(KickFromSceneRequest) returns (KickFromSceneResponse);
rpc TransferOwnership(TransferOwnershipRequest) returns (TransferOwnershipResponse);

message KickFromSceneRequest {
    string character_id        = 1 [(buf.validate.field).string.min_len = 1];
    string scene_id            = 2 [(buf.validate.field).string.min_len = 1];
    string target_character_id = 3 [(buf.validate.field).string.min_len = 1];
}
message KickFromSceneResponse {}

message TransferOwnershipRequest {
    string character_id            = 1 [(buf.validate.field).string.min_len = 1];
    string scene_id                = 2 [(buf.validate.field).string.min_len = 1];
    string new_owner_character_id  = 3 [(buf.validate.field).string.min_len = 1];
}
message TransferOwnershipResponse {}
```

The existing `JoinSceneResponse` and `LeaveSceneResponse` empty messages are kept as-is.
`InviteToSceneRequest` is unchanged. Generated code lives in `pkg/proto/holomush/scene/v1/`
and is regenerated via the existing `task proto:gen` workflow.

## Resolver

`plugins/core-scenes/resolver.go` `GetSchema` gains two STRING_LIST attributes:

```go
"participants": pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST,
"invitees":     pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST,
```

`ResolveResource` swaps `r.store.Get(sceneID)` for `r.store.GetWithMembership(sceneID)` and
returns the two new attributes alongside the existing five:

```go
"participants": {Kind: &pluginv1.AttributeValue_StringListValue{
    StringListValue: &pluginv1.StringList{Values: participants},
}},
"invitees": {Kind: &pluginv1.AttributeValue_StringListValue{
    StringListValue: &pluginv1.StringList{Values: invitees},
}},
```

The `participants` list contains all `character_id`s where role IN (`'owner'`, `'member'`).
The owner is always present in `participants` because `CreateWithOwner` inserts the owner
row at scene creation time. The owner check via `==` (existing `read-own-scene`-style
policies) and the member check via `in` (new `read-scene-as-participant`-style policies)
both succeed for the owner.

The `invitees` list contains only `character_id`s where role = `'invited'`.

## ABAC Policies

Clean replacement of the Phase 1+2 policies with the Phase 3 policy set. The full policy
section in `plugins/core-scenes/plugin.yaml` after Phase 3 lands:

```yaml
policies:
  # ─── Unchanged from Phase 1+2 ─────────────────────────────────────────────────────────
  - name: execute-scene-commands
    dsl: >-
      permit(principal is character, action in ["execute"], resource is command)
      when { resource.command.name in ["scene", "scenes"] };

  - name: end-own-scene
    dsl: >-
      permit(principal is character, action in ["end"], resource is scene)
      when { resource.scene.owner == principal.id
        && resource.scene.state in ["active", "paused"] };

  - name: pause-own-scene
    dsl: >-
      permit(principal is character, action in ["pause"], resource is scene)
      when { resource.scene.owner == principal.id
        && resource.scene.state == "active" };

  - name: update-own-scene
    dsl: >-
      permit(principal is character, action in ["update"], resource is scene)
      when { resource.scene.owner == principal.id
        && resource.scene.state in ["active", "paused"] };

  # ─── DELETED in Phase 3 ───────────────────────────────────────────────────────────────
  # - name: read-own-scene    →  replaced by read-scene-as-participant
  # - name: resume-own-scene  →  replaced by resume-scene-as-participant
  # The Phase 3 NOTE comment that flagged the resume-own-scene swap is removed along with
  # the policy itself.

  # ─── REPLACED — was owner-only, now any participant ───────────────────────────────────
  # Revised 2026-07-06 (holomush-sjtlz, ADR holomush-gcr2k): after the vestigial
  # unconditional host seed:player-scene-read was retired (read twin of
  # holomush-8m01u; migration 000048), this policy became the sole participant
  # grant for the `scene info` read gate, and two SIBLING policies were added
  # alongside it in plugin.yaml — read-scene-as-invitee (invitees may inspect
  # before accepting) and read-open-scene (visibility=open metadata is public,
  # coherent with spectate-open-scene). Net contract: INV-SCENE-68.
  - name: read-scene-as-participant
    dsl: >-
      permit(principal is character, action in ["read"], resource is scene)
      when { principal.id in resource.scene.participants };

  - name: resume-scene-as-participant
    dsl: >-
      permit(principal is character, action in ["resume"], resource is scene)
      when { principal.id in resource.scene.participants
        && resource.scene.state == "paused" };

  # ─── NEW in Phase 3 ───────────────────────────────────────────────────────────────────
  - name: write-scene-as-participant
    dsl: >-
      permit(principal is character, action in ["write"], resource is scene)
      when { principal.id in resource.scene.participants
        && resource.scene.state in ["active", "paused"] };

  - name: join-open-scene
    dsl: >-
      permit(principal is character, action in ["join"], resource is scene)
      when { resource.scene.visibility == "open"
        && resource.scene.state in ["active", "paused"] };

  - name: join-private-scene-as-invitee
    dsl: >-
      permit(principal is character, action in ["join"], resource is scene)
      when { resource.scene.visibility == "private"
        && principal.id in resource.scene.invitees
        && resource.scene.state in ["active", "paused"] };

  - name: leave-scene
    dsl: >-
      permit(principal is character, action in ["leave"], resource is scene)
      when { principal.id in resource.scene.participants };
    # Owner-cannot-leave is enforced in plugin code (service-layer pre-check + store-layer
    # WHERE filter), not via ABAC. ABAC says "members can leave"; the state-machine guard
    # rejects the owner case with FailedPrecondition + actionable hint message.

  - name: invite-to-scene
    dsl: >-
      permit(principal is character, action in ["invite"], resource is scene)
      when { resource.scene.owner == principal.id
        && resource.scene.state in ["active", "paused"] };

  - name: kick-from-scene
    dsl: >-
      permit(principal is character, action in ["kick"], resource is scene)
      when { resource.scene.owner == principal.id
        && resource.scene.state in ["active", "paused"] };

  - name: transfer-ownership
    dsl: >-
      permit(principal is character, action in ["transfer-ownership"], resource is scene)
      when { resource.scene.owner == principal.id
        && resource.scene.state in ["active", "paused"] };
```

## Commands

`plugins/core-scenes/commands.go` `dispatchCommand` gains five new subcommand handlers. The
"known subcommands" usage string in the error case updates accordingly.

| Command syntax | Handler | Calls |
|----------------|---------|-------|
| `scene join <scene-id>` | `handleJoin` | `s.service.JoinScene` |
| `scene leave <scene-id>` | `handleLeave` | `s.service.LeaveScene` |
| `scene invite <scene-id> <character>` | `handleInvite` | `s.service.InviteToScene` |
| `scene kick <scene-id> <character>` | `handleKick` | `s.service.KickFromScene` |
| `scene transfer <scene-id> <character>` | `handleTransfer` | `s.service.TransferOwnership` |

Each handler follows the existing pattern: parse arguments, return `Errorf` on bad usage,
call the gRPC service method, return a human-readable success message. The success messages
are intentionally brief and stable so terminal scripting against them is feasible.

## Metrics

`plugins/core-scenes/metrics.go` gains six new stub functions following the Phase 1+2 zero-cost
no-op pattern. Each documents the eventual Prometheus metric name and label set.

```go
// metricSceneParticipantJoined counts successful joins. Labels: visibility (open|private),
// from_invited (true|false). Metric: scene_participants_joined_total{visibility, from_invited}.
func metricSceneParticipantJoined(visibility, fromInvited string)

// metricSceneParticipantLeft counts successful leaves. Labels: visibility.
// Metric: scene_participants_left_total{visibility}.
func metricSceneParticipantLeft(visibility string)

// metricSceneParticipantKicked counts successful kicks. Labels: visibility, prior_role
// (member|invited). Metric: scene_participants_kicked_total{visibility, prior_role}.
func metricSceneParticipantKicked(visibility, priorRole string)

// metricSceneParticipantInvited counts successful invitations. Labels: visibility.
// Metric: scene_participants_invited_total{visibility}.
func metricSceneParticipantInvited(visibility string)

// metricSceneOwnershipTransferred counts ownership transfers. Labels: visibility.
// Metric: scene_ownership_transfers_total{visibility}.
func metricSceneOwnershipTransferred(visibility string)

// metricSceneOpsEventRecorded counts every ops event by kind. Labels: kind. Catch-all for
// observability of the ops timeline. Metric: scene_ops_events_total{kind}.
func metricSceneOpsEventRecorded(kind string)
```

## Test Plan

Phase 3 adds approximately 30 new tests across four test files. All tests follow the ACE
naming convention (Action, Condition, Expectation) per project guidelines.

### Store unit tests (`store_test.go` extensions, ~14 tests)

| Test | Locks |
|------|-------|
| `TestCreateWithOwnerInsertsSceneAndOwnerParticipantAndOpsEvent` | All three rows inserted in one transaction |
| `TestCreateWithOwnerRollsBackWhenParticipantInsertFails` | Transactional integrity |
| `TestAddParticipantInsertsFreshMemberRowForOpenScene` | Happy path open join |
| `TestAddParticipantPromotesInvitedRowToMemberOnPrivateScene` | invited→member promotion |
| `TestAddParticipantReturnsOpNoChangeForExistingMember` | Idempotent retry |
| `TestAddParticipantRejectsPrivateSceneWithoutInvitation` | Returns SCENE_JOIN_NOT_INVITED |
| `TestAddParticipantRejectsEndedScene` | Returns SCENE_TRANSITION_FORBIDDEN |
| `TestRemoveParticipantRefusesToRemoveOwner` | Defense-in-depth WHERE filter |
| `TestKickParticipantRemovesMemberAndInvitedRowsButNotOwner` | Kick semantics |
| `TestTransferOwnershipUpdatesParticipantsAndScenesRowAtomically` | Three-statement transaction |
| `TestTransferOwnershipRejectsNonMemberTarget` | classifyTransferMiss path |
| `TestTransferOwnershipIsNoOpWhenTargetEqualsCurrentOwner` | Idempotency |
| `TestRecordOpsEventTxWritesRowWithExpectedKindAndPayload` | Ops event recording helper |
| `TestRecordOpsEventTxRejectsKindThatViolatesFormatConstraint` | DB-level CHECK enforcement |

### Service unit tests (`service_test.go` extensions, ~10 tests)

| Test | Locks |
|------|-------|
| `TestJoinSceneEmitsMembershipJoinOpsEventOnFreshInsertion` | Handler emits exactly one event |
| `TestJoinSceneDoesNotEmitOpsEventOnNoChangeRetry` | Retries don't pollute audit |
| `TestJoinSceneMapsNotInvitedToPermissionDenied` | gRPC status mapping |
| `TestLeaveSceneRejectsOwnerWithFailedPrecondition` | Service-layer pre-check + hint message |
| `TestInviteToSceneCallsStoreAndEmitsOpsEvent` | Happy path |
| `TestKickFromSceneRemovesMemberAndEmitsOpsEvent` | Happy path |
| `TestKickFromSceneRemovesInvitedRowAndPayloadReflectsPriorRole` | Withdrawing invitation |
| `TestTransferOwnershipUpdatesScenesOwnerIDAndEmitsOpsEvent` | Happy path |
| `TestTransferOwnershipRejectsNonMemberTargetWithFailedPrecondition` | Error mapping |
| `TestCreateSceneEmitsLifecycleCreatedOpsEventInSameTransaction` | Phase 1 retrofit |

### Resolver tests (`resolver_test.go` extensions, ~3 tests)

| Test | Locks |
|------|-------|
| `TestGetSchemaIncludesParticipantsAndInviteesAttributes` | Schema declares STRING_LIST types |
| `TestResolveResourceReturnsParticipantsAndInviteesLists` | Single-query materialization |
| `TestResolveResourceReturnsEmptyListsForSceneWithoutParticipants` | Defensive against new scenes |

### Integration tests (`store_integration_test.go` extensions, ~9 tests)

These hit a real Postgres via testcontainers and exercise the full ABAC + service + store
stack. They use direct schema-qualified DB verification (not just service-layer assertions)
to lock in the schema invariants.

| Test | Locks |
|------|-------|
| `TestOwnerCanReadOwnSceneViaParticipantPolicy` | Owner is in participants → new read policy permits. Catches "we forgot to insert the owner row" regression. |
| `TestOwnerCanResumePausedSceneViaParticipantPolicy` | Owner is in participants → new resume policy permits |
| `TestMemberCanResumePausedScene` | The actual D6 async-safety property |
| `TestNonMemberCannotReadScene` | Privacy-by-default |
| `TestKickedCharacterImmediatelyCannotRead` | No cache → immediate effect after kick |
| `TestInviteeCanJoinPrivateScene` | Private join via invitee path |
| `TestNonInviteeCannotJoinPrivateScene` | Private join without invite is denied |
| `TestOwnerCannotLeaveOwnScene` | Owner-leave returns FailedPrecondition with hint |
| `TestOwnerCanTransferToMember` | Transfer succeeds, denorm owner_id updated, all in one tx |

### Test infrastructure helpers

A small set of test helpers reduces boilerplate:

```go
// store_test.go test helper
func mustCreateSceneWithOwner(t *testing.T, store *SceneStore, ownerID string) *SceneRow

// store_test.go test helper
func mustAddMember(t *testing.T, store *SceneStore, sceneID, characterID string)

// store_test.go test helper
func assertParticipantRowExists(t *testing.T, db *pgxpool.Pool, sceneID, characterID, expectedRole string)

// store_test.go test helper
func assertOpsEventRecorded(t *testing.T, db *pgxpool.Pool, sceneID string, kind OpsEventKind, expectedActor, expectedTarget string)
```

## Acceptance Criteria

The bead's acceptance criteria, refined and expanded based on the design:

- [ ] Migration 000003 creates `scene_participants` and `scene_ops_events` tables, both
  with their indexes and CHECK constraints; reverse migration drops them
- [ ] `CreateScene` becomes transactional and inserts owner participant row + lifecycle.created
  ops event atomically
- [ ] `JoinScene`, `LeaveScene`, `InviteToScene`, `KickFromScene`, `TransferOwnership` RPCs
  all work end-to-end with race-safe store mutations
- [ ] Open scenes accept any join; private scenes require an invitation
- [ ] Owner-only operations (invite, kick, transfer, end, pause, update) are enforced via
  ABAC
- [ ] Members can read scenes they belong to (replacing owner-only reads)
- [ ] Members can resume paused scenes (replacing owner-only resume — D6 async safety)
- [ ] Owners cannot leave their own scene; the rejection message is actionable
- [ ] Ownership transfer demotes prior owner to member, promotes target from member to owner,
  updates `scenes.owner_id`, all atomically
- [ ] Every membership-mutating operation emits exactly one `scene_ops_events` row (or zero
  for no-op retries)
- [ ] Every Phase 1+2 lifecycle/settings handler emits its corresponding ops event
- [ ] Resolver returns `participants` and `invitees` STRING_LIST attributes in a single query
- [ ] All new Phase 3 ABAC policies are declared in `plugin.yaml`; the Phase 2
  `read-own-scene` and `resume-own-scene` policies are deleted
- [ ] Metrics stubs exist for all new membership and ops events
- [ ] Test coverage matches the test plan above; all tests use ACE naming
- [ ] `task pr-prep` passes with zero failures

## Patterns Inherited from Phase 1+2 (Non-Negotiable)

These patterns are project conventions that Phase 3 MUST inherit without exception:

- `buf.validate.field` annotations on every new request message field, including `min_len = 1`
  on every ID string field
- Race-safe WHERE-clause-guarded store mutations with `RETURNING` (no SELECT-then-UPDATE)
- `errutil.AssertErrorCode` and `errutil.AssertErrorContext` in tests; never use conditional
  `errors.As` for assertions
- Per-operation OTel spans named `scene.<layer>.<operation>` (e.g., `scene.service.join_scene`,
  `scene.store.add_participant`)
- `slog.InfoContext` on success, `slog.WarnContext` on store errors, with subject_id /
  scene_id / target_id in the log fields
- ACE test naming enforced by lint
- Positive AND negative path coverage for every exported function
- `task pr-prep` MUST pass with zero failures before any push
- `task` for all build/test/lint operations; never direct `go test` or `golangci-lint`
- Migrations are idempotent and have matching `.up.sql`/`.down.sql` pairs
- SPDX license headers on all new files
- All randomness via `crypto/rand`, never `math/rand`

## Open Questions and Risks

None at design time. All six brainstorming questions (open vs. private semantics, kick
semantics, owner-leave edge case, race semantics, resolver schema strategy, resume policy
swap) have explicit decisions recorded above with rationale.

The largest risk is scope creep from the Phase 1+2 retrofit work. The design intentionally
includes the lifecycle/settings ops event emission as a Phase 3 deliverable (decision P3.D4
implies it: the table exists, populating it for non-membership ops costs ~5 lines per handler).
If the retrofit work expands beyond expectation during implementation, the fallback is to
ship Phase 3 with membership-only ops events and a follow-up bead for the lifecycle/settings
retrofits — but this creates an "audit gap" in the timeline that the design explicitly tries
to avoid.
