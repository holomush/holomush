<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Scenes Phase 6: Logs, Publish Vote, and Hard Privacy Boundary

**Status:** Draft v1 (2026-05-23)
**Bead (impl):** `holomush-5rh.15`
**Bead (design):** `holomush-5rh.20`
**Folds in:** `holomush-cb4x` (scene log replay + export commands + renderers)
**Predecessors:** `holomush-5rh.14` Phase 5 (focus model), shipped 2026-05-21 (PR #4191)
**Parent design (v2):** [scenes-and-rp-design-v2.md](2026-04-06-scenes-and-rp-design-v2.md) §1.5, §5.5–§5.8, §6.4, §6.6
**Substrate contract:** [substrate-contract](2026-05-16-social-spaces-substrate-contract.md) — INV-S9 (privacy boundary is participant list, plugin-code enforced)
**History scope privacy:** [history-scope-privacy-design.md](2026-05-17-history-scope-privacy-design.md) — §3 "scene privacy is absolute"

## RFC2119 Keywords

The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are used per RFC2119.

## 1. Overview

Phase 6 ships the publication artifact for scenes — a player-controlled mechanism for turning a participant-only roleplay scene into a public, immutable archive. The substrate that gates participant-only reads of the audit history (`scene_log` table, INV-S9 plugin-code participant gate) already shipped in PR #267 (`audit.go:493-555`). Phase 6 adds:

1. A separate **publication artifact** (`published_scenes` table) distinct from the audit `scene_log` table.
2. A **vote state machine** with three terminal-or-transient states (`COLLECTING`, `COOLOFF`, `PUBLISHED`, `ATTEMPT_FAILED`) and a retry surface up to a configurable per-scene max.
3. New **gRPC RPCs** split across two surfaces — participant-gated (INV-S9 plugin-code check) and public (status-gated; no participant check).
4. New **telnet/web commands** under `scene publish` and `scene log` parents, with focus-implicit and explicit `#<scene_id>` argument resolution.
5. **IC-stream event emission** for publish-vote lifecycle, declared at `sensitivity: never` in the plugin manifest crypto.emits.
6. A **COOLOFF → PUBLISHED snapshot pipeline** that reads, decrypts, filters, and renders IC events into a structured JSONB content column atomically.
7. **Observability triple-signal** for hard-privacy-boundary blocks (WARN log + metric + span error; no IC event side channel).
8. **`scene log` replay + `scene log export`** commands and three-format renderers (markdown, plain text, jsonl) — folded in from `holomush-cb4x`.

## 2. Divergences from the v2 Design and Bead Acceptance Criteria

Phase 6 intentionally departs from several specifics in the v2 design and the `holomush-5rh.15` bead acceptance criteria. Each divergence is listed with its rationale; the design-reviewer is expected to evaluate the divergence as intentional, not a defect.

| Source text | Phase 6 spec | Rationale |
| ----------- | ------------ | --------- |
| v2 §5.6 "no mechanism to re-vote or change a vote after casting" | Votes are changeable until attempt resolution | Q4 (2026-05-23 brainstorm). Forgiving UX; resolution remains binary; voted_at + last_changed_at preserve auditability |
| v2 §5.6 "scene end triggers publish vote" automatically | Explicit `scene publish` command starts every attempt including the first | Q5. Separates ending from publication intent; symmetric retry handling |
| v2 §5.6 "IF any vote no OR vote timeout: log stays participant-only permanently" | Per-attempt failure, not per-scene. Default 3 attempts per scene; configurable; admin-extendable | Q5. Real-world async play benefits from a retry surface; admin extend adjusts retry budget but does NOT bypass the unanimous-yes requirement |
| v2 §1.2 "ended → archived after publish vote resolves or times out" | Scene transitions to `archived` ONLY on PUBLISHED. Attempts-exhausted scenes stay `ended` indefinitely | Q5. Preserves a future "resume from ended" capability without committing to it in Phase 6 |
| Bead "Re-voting and admin overrides are structurally impossible" | Re-voting allowed within attempt. Admin overrides for attempt count allowed; NOT for vote outcome | Q4 + Q5. Privacy contract is "admin cannot bypass unanimous-yes"; admin can adjust retry budget |
| v2 §1.3 `OriginLocationID` on Participant | Dropped | Q6. `Character.LocationID` is canonical; no operational consumer |
| v2 §1.3 `PublishVote *bool` on Participant | Moved to `published_scene_votes` table | Q3. Clean roster snapshot at attempt start; doesn't pollute participant rows with vote-window state |
| v2 §1.5 "Scene Log (Published)" naming | Renamed to `PublishedScene` / `published_scenes` everywhere | Q2. `scene_log` is taken by the audit history table (PR #267); the publication artifact needs an unambiguous name |
| Bead "scene_logs table (migration 000003)" | Migration `000008_scene_publication.up.sql` (next available number; `scene_log` already exists at 000004) | Plan reflects shipped state; the audit table predates this design |

## 3. Domain Model

### 3.1 PublishedScene

A `PublishedScene` represents one publication attempt for a scene. Multiple `PublishedScene` rows per scene are allowed up to `max_attempts`; at most one PUBLISHED row is allowed per scene (one-and-done).

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| ID | ULID | Yes | Primary identifier |
| SceneID | ULID | Yes | Logical reference to `scenes(id)` (enforced in code, not via DB FK) |
| AttemptNumber | int | Yes | 1-indexed; unique within a scene |
| Status | PublishedSceneStatus | Yes | One of: `COLLECTING`, `COOLOFF`, `PUBLISHED`, `ATTEMPT_FAILED` |
| InitiatedBy | ULID | Yes | Character ID who ran `scene publish` |
| InitiatedAt | time.Time | Yes | Attempt-creation timestamp |
| CoolOffStartedAt | \*time.Time | No | Set on `COLLECTING → COOLOFF` transition; cleared on flip-back |
| ResolvedAt | \*time.Time | No | Set on terminal status only |
| VoteWindow | time.Duration | Yes | Frozen at attempt start; default 7 days, configurable |
| CoolOffWindow | time.Duration | Yes | Frozen at attempt start; default 30 minutes, configurable |
| MaxAttemptsSnapshot | int | Yes | Frozen at attempt start for audit clarity |
| ContentEntries | \*\[\]Entry | No | Set ONLY on PUBLISHED transition. Each entry: `{speaker, kind, content}` where `kind ∈ {pose, say, emit}` |
| TitleSnapshot | \*string | No | Set on PUBLISHED; snapshot of `scenes.title` at publish time |
| ParticipantsSnapshot | \*\[\]Participant | No | Set on PUBLISHED; frozen list of character names visible to public |
| PublishedAt | \*time.Time | No | Set on PUBLISHED only |
| FailureReason | \*PublishFailureReason | No | Set on ATTEMPT_FAILED only. One of: `ANY_NO`, `TIMEOUT`, `WITHDRAWN`, `SNAPSHOT_DECRYPT_FAILED`, `SNAPSHOT_RENDER_FAILED`, `COOLOFF_INVARIANT_BROKEN` |

### 3.2 PublishedSceneVote

One row per eligible voter, per attempt. Roster is frozen at attempt creation.

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| PublishedSceneID | ULID | Yes | Logical reference to `published_scenes(id)` |
| CharacterID | ULID | Yes | The voter |
| Vote | \*bool | No | `nil` = pending; `true` = yes; `false` = no |
| VotedAt | \*time.Time | No | First-cast timestamp |
| LastChangedAt | \*time.Time | No | Updated on every cast (including no-op same-value re-cast) |

Composite primary key `(PublishedSceneID, CharacterID)`.

### 3.3 Migration

New migration `000008_scene_publication.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

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

No foreign-key constraints. Referential integrity for `scene_id`, `initiated_by`, `published_scene_id`, and `character_id` is enforced in the Go service layer. This matches the project convention for new tables (per user direction 2026-05-23); the existing `scene_participants → scenes` FK is an outlier.

A paired `000008_scene_publication.down.sql` drops both tables and their indexes. Migrations follow the discipline at `site/docs/contributing/database-migrations.md` (idempotent, logic-free, plain SQL).

### 3.4 Configuration: max_attempts per scene

`scenes.max_publish_attempts` is an INTEGER column that defaults to a game-wide value (default 3). Added in a paired migration `000009_scene_max_publish_attempts.up.sql`:

```sql
ALTER TABLE scenes
    ADD COLUMN IF NOT EXISTS max_publish_attempts INTEGER NOT NULL DEFAULT 3;
```

`ExtendScenePublishVoteAttempts` mutates this column.

## 4. State Machine

The authoritative transition specification is the table at §4.1. The diagram below is a navigational aid.

```text
       StartScenePublish
              │
              ▼
       ┌─────────────┐  all roster voted yes  ┌─────────┐
       │ COLLECTING  │ ─────────────────────► │ COOLOFF │
       │             │ ◄───── vote flip ───── │         │
       │             │     yes → no (COOLOFF) │         │
       └─────────────┘                        └────┬────┘
              │                                    │
              │ all voted, any no                  │ cooloff window
              │ OR vote_window timeout             │ elapsed (all-yes)
              │ OR owner withdraw                  │
              │                                    ▼
              │                              ┌───────────┐
              │                              │ PUBLISHED │  (terminal)
              │                              └───────────┘
              ▼
       ┌────────────────┐
       │ ATTEMPT_FAILED │  (terminal for this attempt)
       └────────────────┘
              ▲
              │
              │ owner withdraw from COOLOFF
              │ OR snapshot transaction failure during COOLOFF→PUBLISHED
              │
       (also reached from COOLOFF, see arrows above)
```

A scene MAY enter a new COLLECTING attempt after ATTEMPT_FAILED (up to `max_publish_attempts`). PUBLISHED is terminal for the scene — no further attempts allowed (unique index `published_scenes_one_published_per_scene`).

### 4.1 Transitions

| From | Trigger | To | Side effects |
| ---- | ------- | -- | ------------ |
| (none) | StartScenePublish (scene.state=ended, no active attempt, attempt_count < max, no PUBLISHED row) | COLLECTING | Insert published_scenes row; insert published_scene_votes rows for roster; emit `scene_publish_started` |
| COLLECTING | All roster votes yes | COOLOFF | Set `cooloff_started_at`; emit `scene_publish_cooloff_started` |
| COLLECTING | All roster voted, any no | ATTEMPT_FAILED | Set `resolved_at`, `failure_reason = ANY_NO`; emit `scene_publish_resolved` |
| COLLECTING | `vote_window` elapsed with any pending voter | ATTEMPT_FAILED | Set `resolved_at`, `failure_reason = TIMEOUT`; emit `scene_publish_resolved` |
| COLLECTING | Owner withdraws | ATTEMPT_FAILED | Set `resolved_at`, `failure_reason = WITHDRAWN`; emit `scene_publish_withdrawn` AND `scene_publish_resolved` |
| COOLOFF | `cooloff_window` elapsed with all-yes intact | PUBLISHED | Run snapshot pipeline (§7); set `content_entries`, `title_snapshot`, `participants_snapshot`, `published_at`, `resolved_at`; update `scenes.state = 'archived'`; emit `scene_publish_resolved{outcome=PUBLISHED}` |
| COOLOFF | Any voter changes vote to no | COLLECTING | Clear `cooloff_started_at`; emit `scene_publish_vote_cast{is_change=true, vote=false}`. NO `scene_publish_resolved`. |
| COOLOFF | Owner withdraws | ATTEMPT_FAILED | Same as COLLECTING-owner-withdraw |
| COOLOFF | Snapshot transaction fails | ATTEMPT_FAILED | Set `failure_reason` to `SNAPSHOT_DECRYPT_FAILED` or `SNAPSHOT_RENDER_FAILED`; emit `scene_publish_resolved{outcome=ATTEMPT_FAILED, reason=<failure>}` |

### 4.2 Voting semantics

Votes are CHANGEABLE during COLLECTING. A second cast updates `vote` and `last_changed_at`; if the value changes, `scene_publish_vote_cast{is_change=true}` fires. If the value is the same, the event still fires with `is_change=false` for audit clarity (and so renderers can show "re-affirmed").

During COOLOFF, a `yes → no` change demotes the attempt back to COLLECTING. A `no → yes` change is a no-op in COOLOFF because there are no `no` voters by precondition — but the predicate is asserted defensively (state machine rejects no-yes transitions during COOLOFF as `SCENE_PUBLISH_INVALID_STATE`).

### 4.3 Resolution check

Every `CastPublishSceneVote` triggers the resolution check at the end of the same transaction:

```text
votes := tally(published_scene_id)
if votes.pending == 0:
    if votes.no == 0:
        transition to COOLOFF (if currently COLLECTING)
    else:
        transition to ATTEMPT_FAILED with reason=ANY_NO
elif current_status == COOLOFF and votes.no > 0:
    transition to COLLECTING (clear cooloff_started_at)
```

The timeout and cool-off-elapsed transitions are driven by a separate scheduler (the existing plugin's lifecycle ticker, extended to scan `published_scenes` for `vote_window`/`cooloff_window` boundaries).

## 5. gRPC RPC Surface

Proto contract: `holomush.scene.v1.SceneService` (extended). New methods:

| RPC | Caller | Gate | Description |
| --- | ------ | ---- | ----------- |
| `StartScenePublish(scene_id)` | owner+member | ABAC `publish` + plugin-code preconditions | Creates a new attempt → COLLECTING |
| `CastPublishSceneVote(published_scene_id, vote bool)` | roster voter | Plugin-code: caller in `published_scene_votes` for this attempt | Upserts vote; triggers resolution check |
| `WithdrawScenePublish(published_scene_id)` | scene owner | ABAC `withdraw_publish` (owner only) | Transitions COLLECTING/COOLOFF → ATTEMPT_FAILED |
| `GetPublishedScene(published_scene_id)` | participant | Plugin-code `IsParticipant` (INV-S9) | Returns full state including content if PUBLISHED |
| `DownloadPublishedScene(published_scene_id, format)` | participant | Plugin-code `IsParticipant` (INV-S9) | Returns rendered bytes; only when status==PUBLISHED |
| `ListScenePublishAttempts(scene_id)` | participant | Plugin-code `IsParticipant` (INV-S9) | Audit list of all attempts for this scene |
| `GetPublicSceneArchive(published_scene_id)` | anyone | Code: status == PUBLISHED OR opaque NOT_FOUND | Public-safe view |
| `DownloadPublicSceneArchive(published_scene_id, format)` | anyone | Code: same status gate | Rendered bytes; PUBLISHED only |
| `ExtendScenePublishVoteAttempts(scene_id, additional int)` | admin role | ABAC `extend_publish_attempts` (admin only) | Bumps `scenes.max_publish_attempts` |

### 5.1 Two-pair RPC architecture

The participant-gated pair (`GetPublishedScene`, `DownloadPublishedScene`, `ListScenePublishAttempts`) and the public pair (`GetPublicSceneArchive`, `DownloadPublicSceneArchive`) are deliberately separate handlers that do NOT share a code path beyond the underlying store reads. This separation is the structural guarantee that:

- The participant-gated path cannot accidentally become public via a future refactor.
- The public path cannot accidentally read private content because it only loads content_entries when status == PUBLISHED.

The proto contract reflects this: separate RPC names, separate request/response messages with different field sets (the public response omits per-voter information and the active vote-progress state).

### 5.2 Error code surface

All new errors use the `oops.Code(...)` convention. Codes prefixed `SCENE_PUBLISH_`:

| Code | Wire status | When |
| ---- | ----------- | ---- |
| `SCENE_PUBLISH_INVALID_STATE` | FailedPrecondition | StartScenePublish when scene.state != ended; CastVote when attempt is terminal; etc. |
| `SCENE_PUBLISH_ALREADY_ACTIVE` | FailedPrecondition | StartScenePublish when an active attempt exists for the scene |
| `SCENE_PUBLISH_ALREADY_PUBLISHED` | FailedPrecondition | StartScenePublish when a PUBLISHED row exists for the scene |
| `SCENE_PUBLISH_ATTEMPTS_EXHAUSTED` | FailedPrecondition | StartScenePublish when attempt_count == max_attempts |
| `SCENE_PUBLISH_NO_ELIGIBLE_VOTERS` | FailedPrecondition | StartScenePublish when roster would be empty (only invited rows on the scene) |
| `SCENE_PUBLISH_NOT_A_VOTER` | PermissionDenied | CastVote when caller is not in `published_scene_votes` |
| `SCENE_PUBLISH_INVALID_TRANSITION` | Internal | Defensive: state machine rejects an impossible transition |
| `SCENE_PUBLISH_NOT_OWNER` | PermissionDenied | WithdrawScenePublish from non-owner |
| `SCENE_PRIVACY_BOUNDARY_BLOCK` | PermissionDenied | INV-S9 gate denies participant-only read (also emits the triple-signal per §10) |
| `SCENE_PUBLISH_FORMAT_UNSUPPORTED` | InvalidArgument | Download with format outside {markdown, plain_text, jsonl} |
| `SCENE_PUBLISH_NO_FOCUSED_SCENE` | InvalidArgument | Telnet command with no arg and no focused scene |
| `SCENE_PUBLISH_REF_INVALID` | InvalidArgument | Telnet command with malformed `#<id>` |
| `SCENE_PUBLISH_NOT_PARTICIPANT` | PermissionDenied | Explicit `#<id>` references a scene the caller is not in |

The public RPC pair (`GetPublicSceneArchive`, `DownloadPublicSceneArchive`) returns a single uniform `NOT_FOUND` for all of: nonexistent ID, COLLECTING, COOLOFF, ATTEMPT_FAILED. The error wire shape is identical to "this archive does not exist" — non-participants MUST NOT be able to infer that an attempt is in progress or has failed (INV-P6-8).

Error opacity discipline follows `.claude/rules/grpc-errors.md`. The wire-level `Status.message` is generic; structured `denial_reason` is an internal slog attribute only.

## 6. Commands and Focus Resolution

### 6.1 Command surface

| Command | RPC | Description |
| ------- | --- | ----------- |
| `scene publish` | StartScenePublish | Start a new publish-vote attempt on the focused scene |
| `scene publish #<id>` | StartScenePublish | Same, explicit scene ID |
| `scene publish withdraw` | WithdrawScenePublish | Owner withdraws active attempt on focused scene |
| `scene publish status` | GetPublishedScene | Show vote progress for focused scene's active attempt |
| `scene publish download <format>` | DownloadPublishedScene | Download PUBLISHED artifact in chosen format |
| `scene publish vote yes` | CastPublishSceneVote(true) | Cast/change vote on focused scene's active attempt |
| `scene publish vote no` | CastPublishSceneVote(false) | Cast/change vote |
| `scene publish vote extend <count>` | ExtendScenePublishVoteAttempts | Admin extends max_publish_attempts (ABAC-gated, not name-gated) |
| `scene log` | (existing audit `QueryHistory`) | Replay IC audit history for focused scene |
| `scene log #<id>` | (same) | Same, explicit scene ID |
| `scene log export <format>` | (new local renderer) | Render audit history to chosen format |

All commands accept an optional `#<scene_id>` argument that overrides the connection's focused scene. Resolution rule:

```text
no arg            → use connection.focused_scene
                    error SCENE_PUBLISH_NO_FOCUSED_SCENE if connection has no scene focus
#<scene_id>       → explicit scene reference
                    error SCENE_PUBLISH_REF_INVALID for malformed input
                    error SCENE_PUBLISH_NOT_PARTICIPANT if caller is not a member
```

A `resolveSceneRef(ctx, conn, args) (sceneID, error)` helper in `plugins/core-scenes/commands.go` is the single dispatch point. All `scene publish *` and `scene log *` subcommands route through it.

### 6.2 Cross-scene vote ergonomics

A roster voter MAY be a member of multiple scenes simultaneously. The `scene_publish_started` event emits on the scene IC stream; non-focused roster members receive it as a notification on `notifications:<character_id>` (per v2 §3.3). The notification body includes the `scene_id` and `attempt_id` so the player can fire `scene publish vote yes #<scene_id>` without first switching focus.

A pending-vote dashboard view (showing all scenes with open attempts where the caller is a roster member) is OUT OF SCOPE for Phase 6 — that surface is a Phase 9 (web chat view) concern.

## 7. Event Emission

Six new event types are added to `plugins/core-scenes/plugin.yaml` under `crypto.emits`. All are `sensitivity: never` (operational metadata, no IC content):

| Event type | When | Payload fields |
| ---------- | ---- | -------------- |
| `scene_publish_started` | StartScenePublish success | `attempt_id`, `attempt_number`, `initiated_by`, `vote_window_seconds`, `cooloff_window_seconds`, `roster_character_ids` |
| `scene_publish_vote_cast` | CastPublishSceneVote success | `attempt_id`, `character_id`, `vote`, `is_change` |
| `scene_publish_cooloff_started` | COLLECTING → COOLOFF | `attempt_id`, `cooloff_ends_at` |
| `scene_publish_resolved` | Any terminal transition (PUBLISHED OR ATTEMPT_FAILED) | `attempt_id`, `outcome`, `reason`, `tally_yes`, `tally_no`, `tally_pending` |
| `scene_publish_withdrawn` | WithdrawScenePublish success | `attempt_id`, `withdrawn_by`. Fires AND `scene_publish_resolved` fires so renderers can distinguish |
| `scene_publish_vote_attempts_extended` | ExtendScenePublishVoteAttempts success | `scene_id`, `additional`, `new_max`, `admin_id` |

All events are emitted on the scene IC stream (`events.<game_id>.scene.<scene_id>.ic`) using the existing plugin emit path (`internal/plugin/event_emitter.go::Emit`). The crypto-reviewer gate fires when the manifest changes; these are additive `never` declarations and do not change payload encryption rules. Visible to focused participants inline; surfaces to non-focused members via the per-character notification stream.

The manifest update will join the existing `scene_join_ic` / `scene_leave_ic` / `scene_pose_order_changed_ic` / `scene_idle_nudge` family of notice events.

## 8. ABAC Policies

Three new policies added to `plugins/core-scenes/plugin.yaml` under `policies`:

```yaml
- name: start-publish-as-participant
  dsl: >-
    permit(principal is character, action in ["publish"], resource is scene)
    when { principal.id in resource.scene.participants
           && resource.scene.state == "ended" };

- name: withdraw-publish-as-owner
  dsl: >-
    permit(principal is character, action in ["withdraw_publish"], resource is scene)
    when { resource.scene.owner == principal.id };

- name: admin-extend-publish-attempts
  dsl: >-
    permit(principal is character, action in ["extend_publish_attempts"], resource is scene)
    when { "admin" in principal.character.roles };
```

Notably absent: there is no `read-publication-as-participant` ABAC policy. `GetPublishedScene` / `DownloadPublishedScene` / `ListScenePublishAttempts` are gated **in plugin code** via `IsParticipant`, not ABAC, per INV-S9. The reviewer for this spec MUST verify no future PR adds such a policy.

The plugin's resolver schema is unchanged. The `scene.participants` attribute (per the existing Phase 3 work) is what the `start-publish-as-participant` policy reads. No new resolver attributes are required.

## 9. INV-S9 Enforcement Contract

This section is the load-bearing privacy work for Phase 6. INV-S9 (substrate-contract §4.1) requires the scene-log hard privacy boundary to be plugin-code enforced, NOT ABAC-engine enforced. Phase 6 extends the same pattern to the publication artifact participant-gated RPCs.

### 9.1 Gate placement

The participant gate is the third step of every participant-gated handler, before any read of `published_scenes.content_entries` or `published_scene_votes`:

```go
// service_publish.go (sketch)
func (s *SceneServiceImpl) GetPublishedScene(ctx context.Context, req *scenev1.GetPublishedSceneRequest) (*scenev1.GetPublishedSceneResponse, error) {
    // Step 1: caller validation.
    callerID, err := validateCharacterCaller(ctx)
    if err != nil {
        return nil, err
    }

    // Step 2: load attempt header (status, scene_id, etc. — NOT content_entries).
    pub, err := s.store.GetPublishedSceneHeader(ctx, req.GetId())
    if err != nil {
        return nil, mapStoreErr(err)
    }
    if pub == nil {
        return nil, status.Error(codes.NotFound, "publication not found")
    }

    // Step 3: INV-S9 plugin-code gate. NO ABAC engine consultation.
    ok, err := s.store.IsParticipant(ctx, pub.SceneID, callerID)
    if err != nil {
        return nil, internalErr(err)
    }
    if !ok {
        s.emitPrivacyBoundaryBlock(ctx, "GetPublishedScene", pub.SceneID, callerID, "not_participant")
        return nil, oops.Code("SCENE_PRIVACY_BOUNDARY_BLOCK").
            Errorf("scene not accessible")
    }

    // Step 4: load content if PUBLISHED.
    var entries []Entry
    if pub.Status == StatusPublished {
        entries, err = s.store.GetPublishedSceneContent(ctx, req.GetId())
        if err != nil {
            return nil, internalErr(err)
        }
    }
    return assembleParticipantResponse(pub, entries), nil
}
```

`GetPublishedSceneHeader` and `GetPublishedSceneContent` are deliberately separate store methods. The header read is needed to know the `scene_id` for the participant check; the content read is gated by `IsParticipant` returning true. This split is what makes the call-stack assertion meaningful (§9.4).

The same pattern applies to `DownloadPublishedScene` and `ListScenePublishAttempts`.

### 9.2 Public surface separation

`GetPublicSceneArchive` is a structurally separate handler:

```go
func (s *SceneServiceImpl) GetPublicSceneArchive(ctx context.Context, req *scenev1.GetPublicSceneArchiveRequest) (*scenev1.GetPublicSceneArchiveResponse, error) {
    // No participant check. No ABAC. Status-gated only.
    pub, err := s.store.GetPublishedSceneHeader(ctx, req.GetId())
    if err != nil {
        return nil, mapStoreErr(err)
    }
    if pub == nil || pub.Status != StatusPublished {
        return nil, status.Error(codes.NotFound, "scene archive not found")
    }
    entries, err := s.store.GetPublishedSceneContent(ctx, req.GetId())
    if err != nil {
        return nil, internalErr(err)
    }
    return assemblePublicResponse(pub, entries), nil
}
```

The two handlers MUST NOT share helpers beyond the read-only store methods. Specifically:

- There is no `getPublishedSceneCore(ctx, id, gatePolicy)` that both call with different gate flags. A flag-driven gate is a regression risk; flag flips during refactor turn a participant-only RPC public.
- The response assemblers (`assembleParticipantResponse` vs `assemblePublicResponse`) are distinct functions returning different proto messages.

### 9.2.1 INV-S9 predicate equivalence: `IsMember` and `IsParticipant`

The existing audit-history path (`audit.go::QueryHistory`, lines 493–555) uses `s.memberLookup.IsMember(ctx, sceneID, characterID)`. The new publication-RPC handlers use `s.store.IsParticipant(ctx, sceneID, characterID)` (introduced for `GetPoseOrder` per ADR `holomush-nt2d`). Both predicates implement the same INV-S9 rule: return `true` only for `scene_participants` rows with `role = 'owner'` or `role = 'member'`; rows with `role = 'invited'` return `false`. The difference is naming only — both are valid gate primitives for INV-S9 paths. A future cleanup MAY consolidate to a single name; the spec does not mandate the consolidation in Phase 6.

### 9.3 AttributeResolverService

`plugins/core-scenes/resolver.go` currently returns these attributes for the `scene` type: `id`, `owner`, `state`, `visibility`, `location_id`, `tags`, `warnings`, `participants`, `invitees`. Phase 6 makes no additions and MUST NOT add any of: `content`, `content_entries`, `poses`, `says`, `emits`, `ooc`, `log`, `entries`, `publication`. A meta-test (§11) regression-locks this.

### 9.4 Test contract for INV-S9 (call-stack assertion)

The plugin-code gate ordering is asserted by a tripwire mock store:

```go
// service_publish_gate_test.go
type contentTripwireStore struct {
    headerStore
    contentReadCalls atomic.Int32
}

func (s *contentTripwireStore) GetPublishedSceneContent(_ context.Context, _ string) ([]Entry, error) {
    s.contentReadCalls.Add(1)
    return nil, nil
}

func TestGetPublishedSceneDeniesNonParticipantWithoutHittingContentStore(t *testing.T) {
    store := &contentTripwireStore{...}
    // Configure: pub exists, status=PUBLISHED. Caller is NOT a participant.
    srv := &SceneServiceImpl{store: store, /* no ABAC engine */}
    _, err := srv.GetPublishedScene(ctx, req)
    require.Error(t, err)
    require.Equal(t, "SCENE_PRIVACY_BOUNDARY_BLOCK", oops.AsOops(err).Code())
    require.Equal(t, int32(0), store.contentReadCalls.Load(),
        "INV-S9 violation: content store hit before participant gate denied")
}
```

This test parallels `audit_test.go:229 TestQueryHistoryDeniesNonMemberWithoutHittingLogStore`, which pins the same ordering for the audit-table read path.

## 10. Hard Privacy Boundary Block — Triple Signal

When the INV-S9 gate denies a non-participant access to a participant-gated RPC, the handler MUST emit a triple-signal observability artifact:

```go
func (s *SceneServiceImpl) emitPrivacyBoundaryBlock(ctx context.Context, op, sceneID, callerID, reason string) {
    // 1. WARN structured log.
    slog.WarnContext(ctx, "scene privacy boundary block",
        "operation", op,
        "scene_id", sceneID,
        "caller_id", callerID,
        "denial_reason", reason,
        "code", "SCENE_PRIVACY_BOUNDARY_BLOCK")

    // 2. Counter metric (no-op stub — see §13.1).
    metricScenePublishPrivacyBlock(op, reason)

    // 3. Span error attribute.
    span := trace.SpanFromContext(ctx)
    span.SetStatus(otelcodes.Error, "denied")
    span.SetAttributes(attribute.String("deny.reason", reason))
}
```

The metric call follows the existing package-level no-op stub pattern in `plugins/core-scenes/metrics.go` (see §13.1). Binary-plugin Prometheus infrastructure does not exist yet (per v2 spec §11, recorded as the substrate gap); Phase 6 adds stub functions named per §13.1, each accepting the labeled arguments and discarding them. Every privacy block call-site is wired now; when the host's binary-plugin metrics path lands, only `metrics.go` changes.

The handler MUST NOT emit an IC stream event for the block. Doing so creates a side channel — non-participants who attempt access leave a visible trace on the scene's own stream. Internal observability only (INV-P6-9).

## 11. Snapshot Pipeline (COOLOFF → PUBLISHED)

When the cool-off timer fires for an attempt where COOLOFF status holds, the snapshot pipeline runs:

```go
// publish_snapshot.go (sketch)
func (s *publishService) runSnapshot(ctx context.Context, publishedSceneID string) error {
    ctx, span := tracer.Start(ctx, "scene.publish.snapshot")
    defer span.End()

    return s.txRunner.RunInTx(ctx, func(tx pgx.Tx) error {
        // Step 1: SELECT FOR UPDATE; re-validate invariants.
        pub, err := s.store.LockPublishedSceneForSnapshot(ctx, tx, publishedSceneID)
        if err != nil {
            return err
        }
        if pub.Status != StatusCoolOff {
            return nil // idempotent no-op
        }
        if !allYesVotes(pub.Votes) {
            return s.failAttempt(ctx, tx, pub, FailureCoolOffInvariantBroken)
        }

        // Step 2: read IC events from scene_log, filtered to content kinds.
        rows, err := s.store.ReadSceneLogForSnapshot(ctx, tx, pub.SceneID)
        if err != nil {
            return err
        }

        // Step 3: decrypt sensitive payloads via the plugin's existing DEK access.
        decoded, err := s.codec.DecodeBatch(ctx, rows)
        if err != nil {
            return s.failAttempt(ctx, tx, pub, FailureSnapshotDecryptFailed)
        }

        // Step 4: render to structured entries.
        entries, err := renderEntries(decoded)
        if err != nil {
            return s.failAttempt(ctx, tx, pub, FailureSnapshotRenderFailed)
        }

        // Step 5: read scene metadata for snapshots.
        scene, err := s.store.GetScene(ctx, tx, pub.SceneID)
        if err != nil {
            return err
        }
        participants := frozenParticipantsAtPublishTime(scene)

        // Step 6: atomic UPDATE.
        if err := s.store.MarkPublished(ctx, tx, pub.ID, MarkPublishedInput{
            ContentEntries:       entries,
            TitleSnapshot:        scene.Title,
            ParticipantsSnapshot: participants,
            PublishedAt:          time.Now(),
        }); err != nil {
            return err
        }

        // Step 7: archive the scene.
        if err := s.store.SetSceneState(ctx, tx, pub.SceneID, SceneStateArchived); err != nil {
            return err
        }
        return nil
    })
    // Step 8 (post-tx): emit scene_publish_resolved{outcome=PUBLISHED}.
}
```

### 11.1 Source of truth for IC events

The snapshot reads directly from `plugin_core_scenes.scene_log` (the per-plugin audit table). It does NOT call `PluginAuditService.QueryHistory` — that's a gRPC streaming RPC intended for participant reads. The snapshot is an internal plugin operation that benefits from a direct SQL read.

The filter is `WHERE subject = events.<game_id>.scene.<scene_id>.ic AND type IN ('scene_pose','scene_say','scene_emit')`. OOC, ops events (join/leave/kick), pose-order-changed, idle-nudge, and the new publish-lifecycle events are all excluded. Order is `ORDER BY id ASC` (ULID time-ordered).

### 11.2 Decryption

`scene_pose`, `scene_say`, `scene_emit` are `sensitivity: always` per ADR `holomush-sb3n`. Payloads are per-event-DEK encrypted with AAD bound to event ID + subject. The plugin already holds DEK access for its own events (Phase 7 crypto work). The snapshot reuses the existing decrypt path (no new crypto surface).

The crypto-reviewer MUST evaluate this section for any inadvertent AAD or fence violation. Specifically, bulk-decrypt-at-snapshot reuses existing primitives but is a new caller; the reviewer should confirm:

- AAD is constructed identically to the runtime decrypt path.
- The downgrade fence (INV-P7-7) is unchanged — no payload-encrypted event is rendered without successful decrypt.
- DEK access does not cross plugin boundaries (per the role-isolation invariants).

### 11.3 Atomicity

Steps 1–7 happen in a single Postgres transaction. The `SELECT FOR UPDATE` row lock on `published_scenes` ensures concurrent timer fires are serialized; the status re-check in Step 1 makes subsequent fires a no-op. The atomic state change covers: status → PUBLISHED, content_entries set, scenes.state → archived. There is no observable intermediate state where the publication exists in PUBLISHED status without content.

### 11.4 Failure modes

| Failure | Action |
| ------- | ------ |
| Status changed under us (vote-flip during cool-off) | No-op; SELECT-FOR-UPDATE found COLLECTING; transaction commits without further mutation |
| All-yes invariant violated (votes count mismatch) | Transition to ATTEMPT_FAILED with `failure_reason = COOLOFF_INVARIANT_BROKEN` |
| Decrypt error | Transition to ATTEMPT_FAILED with `failure_reason = SNAPSHOT_DECRYPT_FAILED` |
| Render error (encoding, panic-recovery) | Transition to ATTEMPT_FAILED with `failure_reason = SNAPSHOT_RENDER_FAILED` |
| DB error | Bubble up; timer retries on next tick |

All failure transitions emit `scene_publish_resolved` with the appropriate `outcome` and `reason`.

## 12. Renderers

Three formats ship in Phase 6:

| Format | Description |
| ------ | ----------- |
| `markdown` | Canonical human-readable form. Each entry rendered as `**<speaker>** <verb-form>` where verb-form is `<content>` (pose), `says, "<content>"` (say), or `<content>` (emit). Pose-order is implicit in document order. Markdown special characters in user content are escaped. |
| `plain_text` | No markdown syntax. Each entry rendered as `<speaker> <verb-form>`. Suitable for terminal output, copy-paste, email |
| `jsonl` | One entry per line, each line a JSON object `{"speaker":...,"kind":...,"content":...}`. Stable key ordering for diff-friendliness |

All three are derived at request time from the JSONB `content_entries` column. The same renderer code path serves both `DownloadPublishedScene` (participant) and `DownloadPublicSceneArchive` (public).

For `scene log export` (audit history, NOT publication), the same three renderers operate on the live IC event stream rather than the snapshotted entries. The entry shape is identical (`{speaker, kind, content}`); the source differs.

## 13. Observability

### 13.1 Metrics

Added to `plugins/core-scenes/metrics.go` as **package-level no-op stub functions**, following the established Phase 2 pattern (see `metricSceneCreated`, `metricSceneStateTransition`, etc., at `metrics.go:24-121`). Each stub accepts its labeled arguments and discards them. The file's header comment already documents the wire-up plan: when the binary-plugin metrics infrastructure lands (separate effort, tracked as the v2 spec §11 substrate gap), only this file changes — every call-site is already in place.

| Stub function | Spec metric (when infra lands) | Type | Labels |
| ------------- | ------------------------------ | ---- | ------ |
| `metricScenePublishAttemptResolved(outcome, reason string)` | `scene_publish_attempts_total` | counter | `outcome`, `reason` |
| `metricScenePublishVoteCast(vote, isChange string)` | `scene_publish_votes_cast_total` | counter | `vote`, `is_change` |
| `metricScenePublishVoteWindowDuration(outcome string, durationSeconds float64)` | `scene_publish_vote_window_duration_seconds` | histogram | `outcome` |
| `metricScenePublishCoolOffWindowDuration(outcome string, durationSeconds float64)` | `scene_publish_cooloff_window_duration_seconds` | histogram | `outcome` |
| `metricScenePublishSnapshotDuration(result string, durationSeconds float64)` | `scene_publish_snapshot_duration_seconds` | histogram | `result` |
| `metricScenePublishPrivacyBlock(operation, denialReason string)` | `scene_publish_privacy_blocks_total` | counter | `operation`, `denial_reason` |
| `metricScenePublishActiveAttempts(delta int)` | `scene_publish_active_attempts` | gauge | (none) |

Unit tests at this stage assert call-count via a thin shim (interface-typed function variable swappable in tests) — see §15.1 `TestPrivacyBoundaryBlock_IncrementsMetric`. When the real metrics infrastructure lands, the shim is replaced with the Prometheus registration; the call-count assertions transition to Prometheus testutil assertions in the same test names.

### 13.2 Spans

OpenTelemetry spans at every boundary, following the v2 spec §10.1 convention:

- `scene.service.start_scene_publish`
- `scene.service.cast_publish_scene_vote`
- `scene.service.withdraw_scene_publish`
- `scene.service.get_published_scene`
- `scene.service.download_published_scene`
- `scene.service.get_public_scene_archive`
- `scene.publish.resolve`
- `scene.publish.snapshot`
- `scene.publish.privacy_block`

All spans carry `scene_id` and `attempt_id` where applicable. Parent context propagates from the host's plugin gRPC client (per v2 §10.1).

### 13.3 Structured logs

Every state-machine transition logs at INFO with `attempt_id`, `scene_id`, `from_status`, `to_status`, `reason`. Every privacy boundary block logs at WARN per §10. Every snapshot result logs at INFO (success) or ERROR (failure) with `attempt_id`, `failure_reason`, wrapped pgx error code.

## 14. Invariants (RFC2119)

- **INV-P6-1 (MUST):** Publication-vote rosters SHALL be frozen at attempt creation and immutable for the attempt's lifetime. Owner+member roles only; invited rows SHALL be excluded.
- **INV-P6-2 (MUST):** A vote MAY be cast or changed any number of times during COLLECTING. Once an attempt enters COOLOFF, votes MAY be changed only by voting no (which SHALL transition the attempt back to COLLECTING).
- **INV-P6-3 (MUST):** Only the scene owner SHALL withdraw an active attempt. Participants opposed to publication MUST express their position via voting no, not via withdraw.
- **INV-P6-4 (MUST):** A scene SHALL transition to `archived` ONLY on PUBLISHED. ATTEMPT_FAILED transitions SHALL NOT advance scene state. Attempts-exhausted scenes SHALL stay `ended` indefinitely.
- **INV-P6-5 (MUST):** The IsParticipant gate at `GetPublishedScene`, `DownloadPublishedScene`, and `ListScenePublishAttempts` SHALL execute before any database query against `published_scenes.content_entries` or `published_scene_votes`. Verified by call-stack tripwire test.
- **INV-P6-6 (MUST NOT):** The ABAC engine SHALL NOT be called during participant-gated publication RPC handlers. INV-S9 forbids ABAC from the participant-only read path.
- **INV-P6-7 (MUST NOT):** `AttributeResolverService.ResolveResource` SHALL NOT return scene content (poses, says, emits, OOC, publication content_entries) under any attribute name. Verified by a regression-lock meta-test.
- **INV-P6-8 (MUST):** `GetPublicSceneArchive` and `DownloadPublicSceneArchive` SHALL return opaque `NOT_FOUND` for any non-PUBLISHED publication. The error wire shape SHALL be identical for nonexistent, COLLECTING, COOLOFF, and ATTEMPT_FAILED states.
- **INV-P6-9 (MUST):** Hard-privacy-boundary blocks SHALL emit a WARN-level structured log AND increment `scene_publish_privacy_blocks_total` AND mark the OTel span error with `deny.reason`. NO IC stream event SHALL be emitted (side-channel prevention).
- **INV-P6-10 (MUST):** Snapshot at COOLOFF → PUBLISHED SHALL be atomic. The SELECT FOR UPDATE + content build + UPDATE + scene state change SHALL happen in one transaction. Snapshot failures SHALL transition to ATTEMPT_FAILED with a specific `failure_reason` and SHALL NOT leave the publication in a partial state.

## 15. Test Plan

The Phase 6 surface is privacy-critical. Coverage discipline applies across four tiers; every invariant in §14 SHALL have at least one test asserting it by ID.

### 15.1 Tier 1: Unit tests

Located in `plugins/core-scenes/*_test.go`. Standard Go testing with testify; ACE naming per `.claude/rules/testing.md`; table-driven where natural. Mocks via mockery; ABAC stubs from `policytest.GrantEngine` / `AllowAllEngine` / `DenyAllEngine`.

**State machine** (`publish_state_test.go`):

- `TestPublishStateMachine_TransitionTable` — every legal transition AND every illegal transition rejects with `SCENE_PUBLISH_INVALID_TRANSITION`
- `TestPublishStateMachine_RejectsBackwardFromTerminal` — PUBLISHED and ATTEMPT_FAILED reject any outbound transition
- `TestPublishStateMachine_ResolutionTriggers` — all-yes → COOLOFF; any-no-after-all-voted → ATTEMPT_FAILED; timeout-with-pending → ATTEMPT_FAILED; owner-withdraw from either active state → ATTEMPT_FAILED
- `TestPublishStateMachine_CoolOffFlipBack` — flip yes → no during COOLOFF returns to COLLECTING; previous yes votes preserved

**Vote tally** (`publish_vote_tally_test.go`):

- `TestVoteTally_CountsYesNoPending` — correct counts for mixed roster
- `TestVoteTally_UnanimousYesRequiresZeroPendingZeroNo` — unanimous only when `(yes=N, no=0, pending=0)`
- `TestVoteTally_AllVotedNotUnanimous` — `pending=0 AND no>0` triggers ATTEMPT_FAILED

**Roster freezing** (`publish_roster_test.go`):

- `TestFreezeRoster_OwnerAndMembersOnly` — invited rows excluded
- `TestFreezeRoster_EmptyExcludedRoles` — invited-only scene produces empty roster → `SCENE_PUBLISH_NO_ELIGIBLE_VOTERS`
- `TestFreezeRoster_SingleParticipantScene` — owner-alone scene: roster = [owner]; single yes → COOLOFF
- `TestFreezeRoster_PostFreezeImmutable` — adding members AFTER attempt start does NOT change in-flight roster

**Renderers** (`publish_render_test.go`):

- `TestRenderMarkdown_FromEntries` — well-formed output, speaker bolded, kind-appropriate verb form
- `TestRenderMarkdown_EmptyEntries` — sentinel "no content" markdown, not empty string
- `TestRenderMarkdown_EscapesMarkdownSyntax` — user content with `*_[]` escaped
- `TestRenderMarkdown_PreservesUnicodeAndEmoji` — UTF-8 safe including supplementary plane
- `TestRenderPlainText_StripsAllSyntax` — output matches expected golden file
- `TestRenderJSONL_OneEntryPerLine` — each line valid JSON; round-trips byte-equal
- `TestRenderJSONL_StableKeyOrder` — speaker/kind/content key order deterministic

**Focus resolution** (`commands_resolve_test.go`):

- `TestResolveSceneRef_FocusedNoArg` — uses connection.focused_scene; no DB call
- `TestResolveSceneRef_ExplicitHashArg` — parses `#<ulid>`, validates membership
- `TestResolveSceneRef_NoFocusNoArg` — returns `SCENE_PUBLISH_NO_FOCUSED_SCENE`
- `TestResolveSceneRef_ExplicitMalformed` — returns `SCENE_PUBLISH_REF_INVALID`
- `TestResolveSceneRef_ExplicitNonMember` — returns `SCENE_PUBLISH_NOT_PARTICIPANT`

**INV-S9 gate ordering** (`service_publish_gate_test.go`) — INV-P6-5/6:

- `TestGetPublishedSceneDeniesNonParticipantWithoutHittingContentStore` — content-read tripwire NOT hit on deny
- `TestDownloadPublishedSceneDeniesNonParticipantWithoutHittingContentStore`
- `TestListScenePublishAttemptsDeniesNonParticipantWithoutHittingStore`
- `TestParticipantRPCsDoNotConsultABACEngine` — recording ABAC engine has zero calls (INV-P6-6)
- `TestPublicArchiveRPCsDoNotConsultParticipantStore` — separate code path verified

**Public RPC opacity** (`service_public_archive_test.go`) — INV-P6-8:

- `TestGetPublicSceneArchive_OpacityTable` — subtests for nonexistent, COLLECTING, COOLOFF, ATTEMPT_FAILED all return identical wire-shape NOT_FOUND
- `TestGetPublicSceneArchive_PublishedReturnsContent` — PUBLISHED returns full content + frozen participants + title + published_at
- `TestDownloadPublicSceneArchive_OpacityTable` — same multi-status opacity check

**Resolver no-leak** (`resolver_test.go`, extended) — INV-P6-7:

- `TestResolverNeverExposesContentByForbiddenAttributeName` — enumerate ResolveResource attrs; assert no key matches `^(content|content_entries|poses?|says?|emits?|ooc|log|entries|publication)$`
- `TestResolverNeverExposesPublicationStatus` — no publish-attempt status returned in the ABAC attribute surface

**Privacy block triple-signal** (`service_privacy_block_test.go`) — INV-P6-9:

- `TestPrivacyBoundaryBlock_EmitsWARNLog` — slog capture: WARN level with full attribute set
- `TestPrivacyBoundaryBlock_IncrementsMetric` — `scene_publish_privacy_blocks_total{operation, denial_reason}` increments by 1
- `TestPrivacyBoundaryBlock_DoesNotEmitICEvent` — mock event emitter; ZERO publish events fire on deny path
- `TestPrivacyBoundaryBlock_SpanMarkedError` — OTel test exporter: span has `Status.Code == codes.Error`, attribute `deny.reason` set

### 15.2 Tier 2: Plugin-local integration tests

Located in `plugins/core-scenes/*_integration_test.go`. Ginkgo/Gomega with `//go:build integration`. Uses `sharedPG` from `core_scenes_suite_test.go`.

`publish_state_integration_test.go`:

```go
var _ = Describe("Scene publication lifecycle", func() {
    Context("Happy path: unanimous yes → PUBLISHED", func() {
        It("transitions COLLECTING → COOLOFF when all roster votes yes", ...)
        It("transitions COOLOFF → PUBLISHED when cool-off window elapses", ...)
        It("writes content_entries atomically with status change", ...)
        It("updates parent scene.state to archived in same transaction", ...)
        It("emits scene_publish_resolved with outcome=PUBLISHED", ...)
    })

    Context("Sad path: any-no after all-voted → ATTEMPT_FAILED", func() {
        It("transitions to ATTEMPT_FAILED with reason=ANY_NO", ...)
        It("leaves parent scene in state=ended (NOT archived)", ...)
        It("allows starting a new attempt with attempt_number+1", ...)
    })

    Context("Sad path: timeout with pending → ATTEMPT_FAILED", func() {
        It("transitions to ATTEMPT_FAILED with reason=TIMEOUT", ...)
        It("counts pending voters as effective no", ...)
    })

    Context("Sad path: owner withdraw → ATTEMPT_FAILED", func() {
        It("from COLLECTING: transitions with reason=WITHDRAWN", ...)
        It("from COOLOFF: transitions with reason=WITHDRAWN", ...)
        It("rejects withdraw from non-owner participant", ...)
    })

    Context("Cool-off flip back", func() {
        It("returns to COLLECTING when a voter flips yes → no", ...)
        It("preserves other voters' yes votes across the flip", ...)
        It("can re-enter COOLOFF if the flipped voter changes back to yes", ...)
    })

    Context("Retry semantics", func() {
        It("allows up to max_publish_attempts (default 3) attempts per scene", ...)
        It("rejects StartScenePublish on attempt_count == max", ...)
        It("admin extend bumps max_publish_attempts; subsequent StartScenePublish succeeds", ...)
        It("rejects non-admin extend with permission_denied", ...)
        It("rejects new attempt while a non-terminal attempt exists", ...)
        It("rejects new attempt after a PUBLISHED row exists", ...)
    })

    Context("DB constraint enforcement", func() {
        It("unique index published_scenes_one_active_per_scene rejects duplicate", ...)
        It("unique index published_scenes_one_published_per_scene rejects duplicate", ...)
        It("CHECK constraint rejects invalid status enum values", ...)
    })
})
```

`publish_snapshot_integration_test.go` — INV-P6-10 atomicity:

- `It("snapshots pose/say/emit events only, excluding OOC and ops")` — mixed insert; only three content kinds appear in entries
- `It("decrypts sensitive payloads via plugin's DEK access")` — sensitivity:always payloads → plaintext entries
- `It("transitions to ATTEMPT_FAILED with SNAPSHOT_DECRYPT_FAILED on key miss")` — corrupted DEK; failure path
- `It("is idempotent — second snapshot call on same attempt is a no-op")` — concurrent fire; second SELECT-FOR-UPDATE blocked
- `It("preserves chronological order via ULID id ASC")` — out-of-order insert; ordered output
- `It("freezes participants_snapshot at PUBLISHED time")` — late-join member appears in participants_snapshot

`publish_resolver_integration_test.go` — INV-P6-7:

- `It("resolver returns identical attribute key set for any scene state")` — Resolve scene at active/paused/ended/archived plus with PUBLISHED row; key set unchanged

`publish_event_emission_integration_test.go`:

- `It("emits scene_publish_started on attempt start")`
- `It("emits scene_publish_vote_cast with is_change=true on re-vote")`
- `It("emits scene_publish_cooloff_started on COLLECTING → COOLOFF")`
- `It("emits scene_publish_resolved with appropriate reason")`
- `It("emits scene_publish_withdrawn alongside scene_publish_resolved on owner withdraw")`
- `It("emits scene_publish_vote_attempts_extended on admin extend")`

### 15.3 Tier 3: End-to-end / cross-plugin integration

Located in `test/integration/scenes/`. Ginkgo/Gomega using `internal/testsupport/integrationtest` — real CoreServer, testcontainer Postgres, embedded NATS, full ABAC engine, plugin process. MUST NOT use `eventbustest` (full stack required).

`publish_e2e_test.go`:

```go
var _ = Describe("Scene publication E2E", func() {
    Context("Telnet command flow", func() {
        It("Alice ends scene; runs 'scene publish'; first attempt created", ...)
        It("each roster member receives notification on per-character stream", ...)
        It("non-focused roster member can 'scene publish vote yes #id' and vote registers", ...)
        It("'scene publish status' shows tally without revealing individual votes pre-resolution", ...)
        It("after all yes + cool-off elapse, scene transitions to PUBLISHED and archived", ...)
    })

    Context("Privacy gate end-to-end", func() {
        It("non-participant Charlie's GetPublishedScene returns PermissionDenied", ...)
        It("Charlie's denial emits WARN log + metric + error span (observable)", ...)
        It("Charlie's GetPublicSceneArchive while COLLECTING returns NOT_FOUND", ...)
        It("Charlie's GetPublicSceneArchive after PUBLISHED returns content", ...)
        It("Charlie cannot use AttributeResolverService to read content", ...)
    })

    Context("Notifications", func() {
        It("members not focused on the scene receive scene_publish_started notification", ...)
        It("notification carries scene_id + attempt_id for explicit voting", ...)
    })

    Context("Retry and admin extend", func() {
        It("3 failed attempts blocks a 4th unless admin extends", ...)
        It("admin extend on a scene with 3 failed allows StartScenePublish to succeed", ...)
        It("non-admin extend returns PermissionDenied at ABAC gate", ...)
    })

    Context("Concurrency", func() {
        It("two voters casting simultaneously each succeed; tally is correct", ...)
        It("two simultaneous StartScenePublish: exactly one succeeds", ...)
        It("vote-flip race with cool-off-timer has a well-defined outcome", ...)
    })

    Context("cb4x: scene log replay + export", func() {
        It("'scene log' replays IC history paginated, participant-only", ...)
        It("'scene log export markdown' produces well-formed markdown", ...)
        It("'scene log export jsonl' produces newline-delimited JSON", ...)
        It("non-participant 'scene log #id' returns access-denied", ...)
    })
})
```

`publish_history_scope_e2e_test.go` — interaction with the history-scope-privacy floor:

- `It("a participant who joined the scene AFTER an attempt's scene_publish_started cannot see that started event")` — per iwzt §3 scene-join-floor: focus membership joined_at floor blocks pre-membership events
- `It("a participant who joined before sees all attempt events")` — positive control

### 15.4 Tier 4: Meta-tests

Located in `test/meta/`:

- `TestPhase6InvariantsHaveTestCoverage` — enumerate INV-P6-1..10; assert ≥1 test in tiers 1-3 cites each by ID
- `TestSceneResolverAttributeAllowlist` — live introspection of `AttributeResolverService.GetSchema` for the `scene` type; assert key set ⊆ allowlist (regression lock for any future attribute addition)

### 15.5 Coverage targets

| Package | Target |
| ------- | ------ |
| `plugins/core-scenes/` (Phase 6 additions) | ≥ 85% (privacy-critical surface) |
| `plugins/core-scenes/publish_*.go` (new files) | ≥ 90% |
| Renderer functions | 100% |

### 15.6 Test runners

```bash
task test                                          # All unit tests (Tier 1)
task test -- ./plugins/core-scenes/...             # Plugin-scoped unit
task test:int                                      # Integration (Tier 2 + 3)
task test:int -- ./plugins/core-scenes/...         # Plugin-scoped integration
task test:int -- ./test/integration/scenes/...     # E2E
task test:cover                                    # Coverage report
task pr-prep                                       # Mirrors CI before push
```

## 16. Phasing (Informative)

The implementation plan (produced by `writing-plans` as the next stage) is expected to slice Phase 6 into roughly five sub-deliverables. This section is informative; `plan-reviewer` and `plan-to-beads` are the authoritative slicing pass.

| Slice | Surface |
| ----- | ------- |
| 6a | Schema (migration 000008 + 000009) + domain types + store layer + unit tests for state machine |
| 6b | Participant RPCs + telnet commands (`scene publish [withdraw\|status]`, `scene publish vote yes/no`) + ABAC policies + INV-S9 gate + unit & integration tests |
| 6c | Public RPCs + snapshot pipeline + renderers + opacity tests + crypto-reviewer pass |
| 6d | Event emission (crypto.emits update; crypto-reviewer gate) + observability + privacy-block triple-signal + remaining integration tests |
| 6e | `scene publish vote extend` admin RPC + `scene log` / `scene log export` (cb4x absorption) + meta-tests + final E2E |

Each slice ships as a discrete bead under the parent epic `holomush-5rh`, with `bd dep add` edges expressing the sequencing. The slice topology is provisional pending `plan-reviewer`.

## 17. Out of Scope

- **Scene board with content warnings** (Phase 8, `holomush-5rh.17`) — public discovery surface for published scenes is deferred to the board work.
- **Web chat view** (Phase 9, `holomush-5rh.18`) — including a pending-vote dashboard for cross-scene voters.
- **"Resume from ended"** capability — preserved by the §4 state-machine choice (archive only on PUBLISHED) but not built in Phase 6.
- **Forum view** of published scenes — out of scope per v2 spec; covered by the future Forum epic.
- **Per-participant publication revocation** — once PUBLISHED, the artifact is immutable and cannot be withdrawn. Future revocation surface is not designed.
- **AttributeResolverService schema for publication attributes** — INV-P6-7 forbids; resolver is intentionally not extended.
- **Multi-format publication storage** (HTML pre-rendering, PDF) — Phase 6 derives all formats from the single JSONB content_entries column; pre-rendered formats are out of scope.

## 18. Open Questions

None at design-doc finalization. All three brainstorm items identified in the `holomush-5rh.15` bead notes were resolved during the 2026-05-23 brainstorm session (see `holomush-5rh.20` notes):

1. **Publication-artifact rename** → `PublishedScene` / `published_scenes` everywhere
2. **OriginLocationID / PublishVote reinstate** → both removed from the participant model (OriginLocationID dropped; PublishVote lives in dedicated `published_scene_votes` table)
3. **INV-S9 hard privacy boundary preservation** → resolved as the implementation contract specified in §9-§10

## 19. Related Work

- [Substrate-contract design](2026-05-16-social-spaces-substrate-contract.md) — INV-S9 boundary contract for scene reads
- [Scenes v2 design](2026-04-06-scenes-and-rp-design-v2.md) — Phase 6 binds to §1.5, §5.5–§5.8, §6.4, §6.6 with the divergences in §2 above
- [History scope privacy design](2026-05-17-history-scope-privacy-design.md) — §3 "scene privacy is absolute" applies to scene IC stream events including publish-vote lifecycle events
- [Scenes Phase 4 design](2026-05-19-scenes-phase-4-streams-and-pose-order-design.md) — IsParticipant predicate (§7.3) is the primitive used by §9 gates
- [Scenes Phase 5 design](2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md) — connection focus + multi-connection visibility; non-focused-member notifications path
- ADR [holomush-c8a9](../../adr/holomush-c8a9-scene-privacy-plugin-code-enforcement.md) — plugin-code enforcement of scene-log reads
- ADR [holomush-nt2d](../../adr/holomush-nt2d-participant-gate-pattern-generalized.md) — generalizing the participant-gate pattern across scene RPCs
- ADR [holomush-sb3n](../../adr/holomush-sb3n-scene-content-sensitivity-always.md) — scene content events classified as sensitivity:always (drives §11.2 decrypt path)
- `.claude/rules/event-conventions.md` — subject naming, event identity vs ordering
- `.claude/rules/grpc-errors.md` — error opacity and code-translation discipline
- `.claude/rules/plugin-manifest.md` — manifest update conventions
- `.claude/rules/plugin-runtime-symmetry.md` — host RPC parity (informative; Phase 6 does not add host RPCs)
- `.claude/rules/testing.md` — testing posture and Ginkgo discipline

<!-- adr-capture: sha256=4b723875810e0492; session=cli; ts=2026-05-23T23:20:33Z; adrs=qd3r5,jrefa,e3xlx,39a5f,c4jee -->
