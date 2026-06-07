<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Web Portal: Scenes — Player Workspace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the player-scoped scenes workspace (browse, watch via observer auto-join, live participate across alts, export) on the web portal, per `docs/superpowers/specs/2026-06-07-web-portal-scenes-design.md` (bead `holomush-5rh.8`).

**Architecture:** Scenes are a separate web surface that rides the Phase 5 focus system — workspace `comms_hub` connections set per-connection focus; writes go through the existing command path on per-alt sessions; watching = observer-role auto-join (INV-SCENE-60 untouched); live log reads ride `WebQueryStreamHistory` (host decrypts; FocusMembership gate); badges are `CONTROL_SIGNAL_SCENE_ACTIVITY` downgrades + `ListMyScenes` snapshot catch-up. A new core-side `SceneAccessService` facade owns identity resolution (session→character, guest deny) so the gateway stays a dumb translator.

**Tech Stack:** Go (core + core-scenes plugin), protobuf/ConnectRPC (`task proto`, `task web:generate`), PostgreSQL plugin migrations, SvelteKit + Svelte 5 runes + bits-ui/shadcn-svelte (`$lib/components/ui/`), Playwright.

**Spec:** `docs/superpowers/specs/2026-06-07-web-portal-scenes-design.md` — read it first. Registry invariants: INV-SCENE-61..64 (pending). Key conventions that WILL bite you: proto doc comments are mandatory and name-echo-gated (`task lint:proto`); `slog.*Context` only; `oops` error codes; ACE test names; never `core.Event{}` literals; `task` runner only (never raw `go test`).

**Verification commands used throughout:**

- `task test -- ./<pkg>/` — unit tests for a package
- `task test:int` — integration tests (Docker required)
- `task proto` — regenerate Go from proto; `task web:generate` — regenerate TS
- `task lint` / `task lint:proto` — linters
- Commit after every task: `jj commit -m "<type>(<scope>): <msg> (holomush-5rh.8)"`

---

## File Structure

| Area | Files |
| --- | --- |
| Plugin migration | `plugins/core-scenes/migrations/000008_participants_observer_role.{up,down}.sql` (new) |
| Plugin Go | `plugins/core-scenes/participants.go`, `store.go`, `service.go`, `commands.go`, `publish_render.go` (modify); `export.go` (new) |
| Plugin manifest | `plugins/core-scenes/plugin.yaml` (spectate policy) |
| Scene proto | `api/proto/holomush/scene/v1/scene.proto` (WatchScene, ExportSceneLog, ListCharacterScenes, ListPublishedScenes, SceneInfo.observers) |
| Host-service proto | `api/proto/holomush/plugin/v1/plugin.proto` (GetConnectionFocus) + `internal/plugin/goplugin/host_service.go`, `internal/plugin/hostfunc/stdlib_focus.go`, `pkg/plugin/focus_client.go` |
| Core proto | `api/proto/holomush/core/v1/core.proto` (SelectCharacterRequest.client_type, CONTROL_SIGNAL_SCENE_ACTIVITY, ControlFrame.scene_id) |
| Facade | `api/proto/holomush/sceneaccess/v1/sceneaccess.proto` (new), `internal/grpc/sceneaccess_service.go` (new) + `cmd/holomush/sub_grpc.go` (register) |
| Subscribe path | `internal/grpc/server.go` (filter union + activity downgrade), `internal/grpc/auth_handlers.go` (quiet select) |
| Presence | `internal/store/session_store.go` (grid-presence filter — `PostgresSessionStore` is the sole `session.Store` implementation) |
| ABAC | `internal/access/policy/attribute/player.go` (is_guest) |
| Web proto/gateway | `api/proto/holomush/web/v1/web.proto`, `internal/web/handler.go`, `internal/web/scene_handlers.go` (new), `cmd/holomush/gateway.go` |
| Frontend | `web/src/routes/(authed)/scenes/**` (new), `web/src/lib/scenes/**` (new), `web/src/lib/components/scenes/**` (new) |
| E2E | `web/e2e/scenes.spec.ts` (extend — file already exists; append the E9.5 describe block per Task 19) |
| Registry | `docs/architecture/invariants.yaml` (+ `go run ./cmd/inv-render`) |

---

## Phase 1: Scene plugin substrate

### Task 1: Observer participant role (migration + Go constant)

**Files:**

- Create: `plugins/core-scenes/migrations/000008_participants_observer_role.up.sql`, `.down.sql`
- Modify: `plugins/core-scenes/participants.go` (~line 22–29)
- Test: `plugins/core-scenes/participants_test.go` (or `types_test.go` — colocate with existing `ParticipantRole` tests; find them: `rg -n "IsValid" plugins/core-scenes/*_test.go`)

- [ ] **Step 1: Confirm the next migration number** — `ls plugins/core-scenes/migrations/` and use the next free prefix (expected `000008`). Adjust filenames below if taken.

- [ ] **Step 2: Write the failing test**

```go
func TestParticipantRoleIsValidAcceptsObserver(t *testing.T) {
	assert.True(t, ParticipantRoleObserver.IsValid())
	assert.Equal(t, ParticipantRole("observer"), ParticipantRoleObserver)
}
```

- [ ] **Step 3: Run it** — `task test -- -run TestParticipantRoleIsValidAcceptsObserver ./plugins/core-scenes/` — Expected: FAIL (`ParticipantRoleObserver` undefined).

- [ ] **Step 4: Add the constant + `IsValid` case** in `participants.go`, matching the existing `ParticipantRoleOwner`/`ParticipantRoleMember`/`ParticipantRoleInvited` naming (participants.go:17–21):

```go
// ParticipantRoleObserver marks a watching, non-acting participant (E9.5
// observer auto-join, INV-SCENE-61): present in the roster, excluded from
// the emit path, pose order, and publish votes.
const ParticipantRoleObserver ParticipantRole = "observer"
```

and add `ParticipantRoleObserver` to the `IsValid()` switch alongside the three existing constants. Use `ParticipantRoleObserver` consistently in Tasks 2–4 wherever the observer role is compared.

- [ ] **Step 5: Write the migration.** `000008_participants_observer_role.up.sql` (mirror the SPDX header style of `000003_scene_participants_and_ops_events.up.sql`):

```sql
-- Widen the participants role constraint to admit the E9.5 observer role
-- (spec 2026-06-07-web-portal-scenes-design.md D6, INV-SCENE-61).
ALTER TABLE scene_participants
    DROP CONSTRAINT IF EXISTS scene_participants_role_check;
ALTER TABLE scene_participants
    ADD CONSTRAINT scene_participants_role_check
    CHECK (role IN ('owner', 'member', 'invited', 'observer'));
```

`.down.sql` (must delete observer rows before re-tightening — a down on live data otherwise fails):

```sql
DELETE FROM scene_participants WHERE role = 'observer';
ALTER TABLE scene_participants
    DROP CONSTRAINT IF EXISTS scene_participants_role_check;
ALTER TABLE scene_participants
    ADD CONSTRAINT scene_participants_role_check
    CHECK (role IN ('owner', 'member', 'invited'));
```

Verify the constraint name matches what `000003` produced: `rg -n "role" plugins/core-scenes/migrations/000003_scene_participants_and_ops_events.up.sql` — if the CHECK is inline/unnamed, PostgreSQL auto-names it `scene_participants_role_check`; if `000003` names it explicitly, use that name.

- [ ] **Step 6: Run** `task test -- -run TestParticipantRoleIsValidAcceptsObserver ./plugins/core-scenes/` (PASS) and `task test:int` (migration up exercised against a fresh DB).

- [ ] **Step 7: Commit** — `jj commit -m "feat(core-scenes): admit observer participant role (holomush-5rh.8)"`

### Task 2: Store — AddObserver, role upgrade on join, observers in SceneInfo

**Files:**

- Modify: `plugins/core-scenes/store.go` (AddParticipant ~line 693; scene-load queries ~line 285–300), `plugins/core-scenes/types.go` (SceneInfo conversion), `api/proto/holomush/scene/v1/scene.proto` (SceneInfo)
- Test: `plugins/core-scenes/store_integration_test.go`

- [ ] **Step 1: Proto — additive `observers` field on `SceneInfo`** (after the existing `participants = 13`; confirm next free field number by reading the message):

```proto
  // The watching (role=observer) participants, listed separately from the
  // acting roster. Populated by the same scene-load queries that build
  // `participants`; excluded from pose order and publish votes
  // (INV-SCENE-61). See store.go scene-load queries.
  repeated ParticipantInfo observers = 14;
```

Run `task proto` then `task lint:proto` (doc comment is mandatory and must not echo the field name).

- [ ] **Step 2: Failing integration test** in `store_integration_test.go` (mirror the existing AddParticipant test setup):

```go
func TestAddObserverInsertsObserverRowOnOpenActiveScene(t *testing.T) { /* Ginkgo or table style — match file convention:
   create open active scene; AddObserver(ctx, sceneID, watcherID);
   expect role=="observer"; expect GetParticipant returns the row.
   Then: AddObserver on a visibility=private scene → ObserverSceneNotOpen.
   Then: AddObserver on an ended scene → ObserverSceneNotActive.
   Then: AddObserver for an existing member → ObserverAlreadyParticipant (row unchanged).
   Then: scene load returns the watcher under Observers, NOT Participants. */ }
```

Write it as real assertions per the file's existing style (these files use Ginkgo specs — `Describe("AddObserver", ...)`).

- [ ] **Step 3: Implement `AddObserver`** in `store.go`, mirroring `AddParticipant`'s span/oops/tx conventions:

```go
// ObserverAddResult classifies AddObserver outcomes.
type ObserverAddResult int

const (
	ObserverAdded ObserverAddResult = iota
	ObserverAlreadyParticipant
	ObserverSceneNotFound
	ObserverSceneNotOpen
	ObserverSceneNotActive
)

// AddObserver inserts a role=observer participant row for an OPEN,
// active/paused scene. The visibility/state checks are plugin-code-enforced
// inside the transaction (INV-SCENE-61: they run regardless of any ABAC
// outcome and re-check under lock). Idempotent: an existing row of any role
// is returned unchanged as ObserverAlreadyParticipant.
func (s *SceneStore) AddObserver(ctx context.Context, sceneID, characterID string) (*ParticipantRow, ObserverAddResult, error) {
	// tx begin per store convention; SELECT visibility, state FROM scenes WHERE id=$1 FOR SHARE
	// → no row: ObserverSceneNotFound
	// → visibility != "open": ObserverSceneNotOpen
	// → state NOT IN ("active","paused"): ObserverSceneNotActive
	// SELECT existing participant row → return ObserverAlreadyParticipant
	// INSERT INTO scene_participants (scene_id, character_id, role, joined_at)
	//   VALUES ($1,$2,'observer',now-equivalent) per AddParticipant's INSERT shape
	// commit; return row, ObserverAdded
}
```

Write the body by mirroring `AddParticipant` (same tx helper, same `oops.Code` style, same span attrs); the SQL gates above are the contract.

- [ ] **Step 4: Role upgrade on join.** In `AddParticipant`, where it currently classifies an existing row: when the existing row's role is `observer`, UPDATE it to `member` and return a new result value `ParticipantUpgraded` (add to `ParticipantOpResult`). Add to the Step 2 test: watcher then `AddParticipant` → role becomes `member`, result `ParticipantUpgraded`.

- [ ] **Step 5: Observers in scene load.** In the scene-load query (`store.go:285–300`) add a third array:

```sql
COALESCE(
    (SELECT array_agg(character_id) FROM scene_participants
     WHERE scene_id = s.id AND role = 'observer'),
    '{}'::TEXT[]
) AS observers
```

Thread it through the row scan → `SceneRow` → `SceneInfo.Observers` conversion in `types.go` (mirror how `participants`/`invitees` flow). Do NOT touch the `participants` array's `role IN ('owner','member')` filter — the `write-scene-as-participant` policy and the resolver depend on it.

- [ ] **Step 6: Run** `task test:int` — Expected: new specs PASS, no regressions.

- [ ] **Step 7: Commit** — `jj commit -m "feat(core-scenes): AddObserver store path + observer→member upgrade + SceneInfo.observers (holomush-5rh.8)"`

### Task 3: WatchScene RPC + spectate policy (INV-SCENE-61 gate ordering)

**Files:**

- Modify: `api/proto/holomush/scene/v1/scene.proto`, `plugins/core-scenes/service.go`, `plugins/core-scenes/plugin.yaml` (policies), `plugins/core-scenes/main.go` (focus-client wiring into service if not already present — check `rg -n "focusClient" plugins/core-scenes/main.go service.go`)
- Test: `plugins/core-scenes/service_watch_test.go` (new)

- [ ] **Step 1: Proto.** Add to `SceneService` (doc comments grounded in the handler you're about to write):

```proto
  // WatchScene auto-joins the requesting character into an OPEN scene as a
  // role=observer participant and registers the focus membership for the
  // supplied session, so focus/Subscribe/history gates admit the watcher.
  // Gate order is fail-closed per INV-SCENE-61: the plugin-code
  // visibility==open and state checks run BEFORE the ABAC spectate action is
  // evaluated; non-open scenes are rejected without consulting ABAC.
  // See service.go::WatchScene.
  rpc WatchScene(WatchSceneRequest) returns (WatchSceneResponse);
```

```proto
// WatchSceneRequest identifies the watcher, target scene, and the watcher's
// game session (host-supplied; the session receives the FocusMembership).
message WatchSceneRequest {
  // The watching character's ID; required. Trusted host-supplied identity
  // per the SceneService caller contract (service.go's ABAC note).
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  // The scene to watch; required. Must be visibility=open and active/paused.
  string scene_id = 2 [(buf.validate.field).string.min_len = 1];
  // The watcher's game session ULID; required — JoinFocus registers the
  // scene FocusMembership on this session.
  string session_id = 3 [(buf.validate.field).string.min_len = 1];
}

// WatchSceneResponse confirms the observer row.
message WatchSceneResponse {
  // The resulting participant entry (role=observer, or the pre-existing row
  // when the character was already a participant of any role).
  ParticipantInfo participant = 1;
}
```

`task proto && task lint:proto`.

- [ ] **Step 2: Failing tests** (`service_watch_test.go`, ACE names, use the existing service-test harness in `service_test.go` / `testhelpers_test.go` for store + evaluator wiring):

```go
// Verifies: INV-SCENE-61
func TestWatchSceneRejectsNonOpenSceneBeforeConsultingABAC(t *testing.T) {
	// evaluator := policytest.NewErrorEngine(errors.New("ABAC MUST NOT BE REACHED"))
	// private scene → WatchScene → expect SCENE_NOT_WATCHABLE error code,
	// and the error engine was never invoked (its error never surfaces).
}

func TestWatchSceneDeniedWhenSpectatePolicyDenies(t *testing.T) {
	// open scene + policytest.DenyAllEngine() → PermissionDenied
}

func TestWatchSceneAddsObserverAndJoinsFocusWhenPermitted(t *testing.T) {
	// open scene + AllowAllEngine → observer row returned; fake FocusClient
	// records JoinFocus(sessionID, FocusKey{Scene, sceneID}).
}
```

Run: `task test -- -run TestWatchScene ./plugins/core-scenes/` — Expected: FAIL (method undefined).

- [ ] **Step 3: Implement `WatchScene`** on `SceneServiceImpl` (service.go), following the file's span/validation conventions:

```go
func (s *SceneServiceImpl) WatchScene(ctx context.Context, req *scenev1.WatchSceneRequest) (*scenev1.WatchSceneResponse, error) {
	// 1. span + protovalidate per file convention.
	// 2. Load scene; CODE GATES FIRST (INV-SCENE-61):
	//    visibility != "open"            → oops.Code("SCENE_NOT_WATCHABLE")
	//    state not in {"active","paused"} → oops.Code("SCENE_NOT_WATCHABLE")
	// 3. ABAC: s.evaluator.Evaluate(ctx, "spectate", "scene:"+req.GetSceneId())
	//    — fail-closed on nil evaluator (mirror handleEmit's nil check);
	//    !dec.Allowed → PermissionDenied.
	// 4. store.AddObserver (re-checks gates in-tx; map result codes).
	// 5. s.focusClient.JoinFocus(ctx, req.GetSessionId(),
	//        pluginsdk.FocusKey{Kind: pluginsdk.FocusKindScene, TargetID: sceneID})
	//    — JoinFocus is idempotent for existing members.
	// 6. Return ParticipantInfo.
}
```

`SceneServiceImpl` needs `evaluator` and `focusClient` fields if not already present — check `rg -n "evaluator|focusClient" plugins/core-scenes/service.go plugins/core-scenes/main.go` and wire them from the plugin struct the same way `SetSnapshotDecryptor` wiring works (a setter called from `main.go` after host clients are available).

- [ ] **Step 4: Spectate policy** in `plugin.yaml` `policies:` (defense-in-depth behind the code gate):

```yaml
  - name: spectate-open-scene
    dsl: >-
      permit(principal is character, action in ["spectate"], resource is scene) when { resource.scene.visibility == "open" };
```

- [ ] **Step 5: Run** `task test -- -run TestWatchScene ./plugins/core-scenes/` (PASS), then `task test -- ./plugins/core-scenes/` and `task lint`.

- [ ] **Step 6: Commit** — `jj commit -m "feat(core-scenes): WatchScene observer auto-join with code-before-ABAC gate (holomush-5rh.8)"`

### Task 4: Role-gate proofs — observer excluded from emit, pose order, publish votes

No new behavior is *expected* for emit and votes (structural exclusion via the role-filtered `participants` array and the `CreatePublishAttempt` roster filter at `publish_store.go:84–87`); this task **pins** those exclusions with tests and closes any gap found in pose order.

**Files:**

- Test: `plugins/core-scenes/resolver_test.go`, `plugins/core-scenes/poseorder_test.go`, `plugins/core-scenes/publish_vote_tally_test.go` (extend, matching each file's style)
- Possibly modify: `plugins/core-scenes/poseorder.go` (only if its roster query is not role-filtered)

- [ ] **Step 1: Resolver exclusion test** — in `resolver_test.go`: scene with one member + one observer → resolved `resource.scene.participants` attribute contains the member only. (This is what makes `write-scene-as-participant` deny observers.)

- [ ] **Step 2: Pose-order exclusion test** — in `poseorder_test.go`: add an observer to a scene with a fixed pose order → observer never appears in `GetPoseOrder`. Run it. If it FAILS, find the roster source (`rg -n "scene_participants" plugins/core-scenes/poseorder.go plugins/core-scenes/store.go`) and add `AND role IN ('owner','member')` to that query; re-run to PASS. If it passes immediately, the existing filter covers it — keep the test as the pin.

- [ ] **Step 3: Vote exclusion test** — in `publish_vote_tally_test.go` (or the matching integration file): observer on a scene with an active publish attempt → `CastPublishSceneVote` returns `SCENE_PUBLISH_NOT_A_VOTER`; tally `eligible` count excludes the observer.

- [ ] **Step 4: Emit denial integration test** — in `test/integration/scenes/` (Ginkgo, `//go:build integration`): with the real policy set loaded, an observer issuing `scene pose hi` gets the "not permitted to write" denial; a member succeeds. Use the existing scenes integration suite's scene/character setup helpers (see `test/integration/scenes/publish_e2e_test.go` for the harness idiom).

- [ ] **Step 5: Run** `task test -- ./plugins/core-scenes/` and `task test:int`. Commit — `jj commit -m "test(core-scenes): pin observer exclusions (emit, pose order, publish votes) (holomush-5rh.8)"`

### Task 5: ListCharacterScenes + ListPublishedScenes RPCs

**Files:**

- Modify: `api/proto/holomush/scene/v1/scene.proto`, `plugins/core-scenes/service.go`, `plugins/core-scenes/store.go`, `plugins/core-scenes/publish_store.go`
- Test: `plugins/core-scenes/service_test.go`, `plugins/core-scenes/store_integration_test.go`

- [ ] **Step 1: Proto.**

```proto
  // ListCharacterScenes returns every non-archived scene the character has a
  // participant row in (any role, including observer), with the character's
  // role and per-scene activity metadata for workspace badges. Serves the
  // web workspace's "my scenes" list; the host facade fans this out across a
  // player's owned characters. See service.go::ListCharacterScenes.
  rpc ListCharacterScenes(ListCharacterScenesRequest) returns (ListCharacterScenesResponse);

  // ListPublishedScenes pages through PUBLISHED scene archives (public-safe
  // fields only, same status gate as GetPublicSceneArchive / INV-SCENE-35),
  // newest first, with optional tag filtering. Powers the archive browse
  // page. See publish_service.go::ListPublishedScenes.
  rpc ListPublishedScenes(ListPublishedScenesRequest) returns (ListPublishedScenesResponse);
```

Messages:

```proto
message ListCharacterScenesRequest {
  // The character whose participations to list; required (host-trusted).
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
}
message CharacterSceneInfo {
  // The scene's full projection (includes observers).
  SceneInfo scene = 1;
  // This character's participant role in the scene (owner/member/observer).
  string role = 2;
  // Epoch-ms timestamp of the newest scene_log row on the scene's IC
  // subject; 0 when the log is empty.
  int64 last_activity_ms = 3;
  // Total scene_log rows on the IC subject (workspace activity panel).
  int64 entry_count = 4;
}
message ListCharacterScenesResponse {
  // The character's scenes, most recently active first.
  repeated CharacterSceneInfo scenes = 1;
}
message ListPublishedScenesRequest {
  // Page size; 0 means server default, capped at 200 (mirrors ListScenes).
  int32 limit = 1 [(buf.validate.field).int32 = { gte: 0, lte: 200 }];
  // Leading results to skip.
  int32 offset = 2 [(buf.validate.field).int32.gte = 0];
  // Restrict to archives whose scene carries all of these tags.
  repeated string tags = 3;
}
message ListPublishedScenesResponse {
  // Public-safe published-archive summaries, newest first. Reuses the
  // public-archive projection message GetPublicSceneArchive returns — read
  // that message's name in scene.proto and reference it here.
  repeated PublicSceneArchive archives = 1;
}
```

**Note:** confirm the actual public-archive message name with `rg -n "message.*PublicSceneArchive\|GetPublicSceneArchiveResponse" api/proto/holomush/scene/v1/scene.proto` and reuse it (do not invent a parallel shape). `task proto && task lint:proto`.

- [ ] **Step 2: Failing store tests** — `store_integration_test.go`: seed two scenes (one with a member row, one observer row) + log rows; `ListCharacterScenes(charID)` returns both with correct roles, `last_activity_ms` ordering, counts. `publish_store` test: only `status=PUBLISHED` attempts appear; tag filter works.

- [ ] **Step 3: Store queries.** `ListCharacterScenes` (store.go) — the subject is `events.<gameID>.scene.<id>.ic`; the store already knows how to build scene subjects (`rg -n "dotStyleSceneSubject" plugins/core-scenes/`); reuse that helper:

```sql
SELECT s.<scene columns>, p.role,
  COALESCE((SELECT EXTRACT(EPOCH FROM max(l.timestamp))*1000 FROM scene_log l WHERE l.subject = $2 || s.id || '.ic'), 0)::BIGINT AS last_activity_ms,
  COALESCE((SELECT count(*) FROM scene_log l WHERE l.subject = $2 || s.id || '.ic'), 0) AS entry_count
FROM scenes s JOIN scene_participants p ON p.scene_id = s.id
WHERE p.character_id = $1 AND s.archived_at IS NULL
ORDER BY last_activity_ms DESC
```

(`$2` = `events.<gameID>.scene.` prefix, passed in from the service which owns `gameID`.) `ListPublishedScenes` mirrors the existing published-scene load in `publish_store.go` with `status='PUBLISHED' ORDER BY published_at DESC LIMIT/OFFSET` + tag predicate on the scene row.

- [ ] **Step 4: Service handlers** — thin: validate, call store, convert (mirror `ListScenes`'s shape in `service.go`). Run tests → PASS. `task test:int`.

- [ ] **Step 5: Commit** — `jj commit -m "feat(core-scenes): ListCharacterScenes + ListPublishedScenes RPCs (holomush-5rh.8)"`

### Task 6: ExportSceneLog RPC (decrypt seam + renderers)

**Files:**

- Modify: `api/proto/holomush/scene/v1/scene.proto`
- Create: `plugins/core-scenes/export.go`
- Test: `plugins/core-scenes/export_test.go`, plus an integration spec in `plugins/core-scenes/publish_snapshot_integration_test.go`'s style

- [ ] **Step 1: Read the existing render/decrypt pipeline** — `mcp__probe__extract_code` on `plugins/core-scenes/publish_render.go` and `publish_snapshot.go` (`snapshotDecryptor`, `DecryptOwnAuditRows`, `snapshotDecryptBatch=500`, and the markdown/plain/jsonl renderer functions used by `DownloadPublishedScene`). The export path MUST reuse those renderer functions and the decrypt seam — do not duplicate rendering logic.

- [ ] **Step 2: Proto.**

```proto
  // ExportSceneLog renders a scene's IC log to a downloadable document for a
  // participant of ANY role (observers may export what they may read;
  // INV-SCENE-60's participant gate is plugin-code-enforced — non-participants
  // fail before ABAC, which is never consulted here). Decryption flows
  // through the host-mediated snapshot decrypt seam; supported formats are
  // "markdown" and "jsonl". See export.go::ExportSceneLog.
  rpc ExportSceneLog(ExportSceneLogRequest) returns (ExportSceneLogResponse);

message ExportSceneLogRequest {
  // The exporting character; required; must hold a participant row (any role).
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  // The scene to export; required. Works for active, paused, and ended scenes.
  string scene_id = 2 [(buf.validate.field).string.min_len = 1];
  // The render format; required: "markdown" or "jsonl".
  string format = 3 [(buf.validate.field).string.min_len = 1];
}
message ExportSceneLogResponse {
  // The rendered document bytes.
  bytes content = 1;
  // The content's MIME type (text/markdown or application/jsonl) — mirrors
  // DownloadPublishedScene's MIME vocabulary.
  string mime_type = 2;
  // Suggested download filename (slugified title + extension).
  string filename = 3;
}
```

`task proto && task lint:proto`.

- [ ] **Step 3: Failing tests** — `export_test.go`: (a) non-participant → `SCENE_EXPORT_NOT_PARTICIPANT` error code, decryptor never invoked (use a recording fake); (b) observer → allowed; (c) `format="html"` → `SCENE_EXPORT_BAD_FORMAT`; (d) happy path renders via a fake decryptor returning two plaintext rows, jsonl output has 2 lines.

- [ ] **Step 4: Implement** `export.go::(*SceneServiceImpl) ExportSceneLog`:

```go
// 1. validate; 2. store.GetParticipant(sceneID, characterID) — no row →
//    oops.Code("SCENE_EXPORT_NOT_PARTICIPANT") (code gate; no ABAC);
// 3. read scene_log rows for the IC subject (reuse/derive from
//    ReadSceneLogForSnapshot's query — live read, ORDER BY id);
// 4. decrypt in snapshotDecryptBatch chunks via the same snapshotDecryptor
//    seam the publish snapshot uses;
// 5. render via the SAME renderer functions DownloadPublishedScene uses
//    (resolved in Step 1) for "markdown"/"jsonl";
// 6. filename: slug(title) + ".md"/".jsonl".
```

- [ ] **Step 5: Run** `task test -- -run TestExportSceneLog ./plugins/core-scenes/` (PASS), `task test:int`, `task lint`. Commit — `jj commit -m "feat(core-scenes): ExportSceneLog via snapshot decrypt seam (holomush-5rh.8)"`

### Task 7: Focus-aware emit routing (GetConnectionFocus host RPC, INV-S3 parity)

**Files:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto`, `internal/plugin/goplugin/host_service.go`, `internal/plugin/hostfunc/stdlib_focus.go`, `pkg/plugin/focus_client.go` (+ its goplugin client impl — find with `rg -ln "JoinFocus" pkg/plugin/ internal/plugin/`), `plugins/core-scenes/commands.go` (handleEmit, ~line 1246)
- Test: `internal/plugin/hostfunc/stdlib_focus_test.go`, `plugins/core-scenes/commands_emit_test.go`

- [ ] **Step 1: Proto** (mirror `SetConnectionFocus`'s shapes at plugin.proto:725):

```proto
  // GetConnectionFocus returns the named connection's current per-connection
  // focus, or absent when the connection is grid-focused (FocusKey nil) or
  // unknown. Read-only counterpart of SetConnectionFocus; lets plugins route
  // connection-scoped operations (e.g. scene pose) to the focused target.
  // See goplugin/host_service.go::GetConnectionFocus.
  rpc GetConnectionFocus(PluginHostServiceGetConnectionFocusRequest) returns (PluginHostServiceGetConnectionFocusResponse);

message PluginHostServiceGetConnectionFocusRequest {
  // ULID bytes of the connection whose focus is being read.
  bytes connection_id = 1;
}
message PluginHostServiceGetConnectionFocusResponse {
  // The connection's focus; absent for grid focus or unknown connection.
  optional FocusKey focus_key = 1;
}
```

- [ ] **Step 2: Host impl** in `host_service.go` next to `SetConnectionFocus`: parse ULID → `sessionStore.GetConnection(ctx, connID)` (exists: `internal/session/session.go:432`) → map `conn.FocusKey` to proto (absent on nil / not-found; not-found is NOT an error — absent focus, logged at debug).

- [ ] **Step 3: SDK + Lua parity (INV-S3).** Add `GetConnectionFocus(ctx context.Context, connectionID string) (*FocusKey, error)` to `pkg/plugin/focus_client.go`'s `FocusClient` interface + the goplugin client impl; add the `get_connection_focus(connection_id)` Lua hostfunc in `stdlib_focus.go` (string-ULID convention per Phase 5 D6, mirroring `parseFocusKey`). Test both: hostfunc test returns kind/target table for a focused conn, nil for grid.

- [ ] **Step 4: Routing in `handleEmit`** — replace the single-membership block (commands.go ~1246, the "Phase 5 will replace this with focus-aware routing" TODO):

```go
	sceneID := ""
	if req.ConnectionID != "" && p.focusClient != nil {
		if fk, fkErr := p.focusClient.GetConnectionFocus(ctx, req.ConnectionID); fkErr == nil &&
			fk != nil && fk.Kind == pluginsdk.FocusKindScene {
			sceneID = fk.TargetID // FocusKey.TargetID is a plain string in the SDK (pkg/plugin/focus_client.go)
		}
	}
	if sceneID == "" {
		var userErr string
		var internalErr error
		sceneID, userErr, internalErr = p.resolveSingleSceneMembership(ctx, req.CharacterID)
		// existing error handling unchanged
	}
```

Delete the stale "Phase 5 will replace this" TODO comment.

- [ ] **Step 5: Tests** — `commands_emit_test.go`: character in TWO scenes + connection focused on scene B → pose lands in B (fake focus client); unfocused connection + single membership → unchanged fallback; unfocused + two memberships → existing ambiguity error preserved.

- [ ] **Step 6: Run** `task test -- ./plugins/core-scenes/ ./internal/plugin/...` and `task test:int` (refactor touched shared plugin SDK). Commit — `jj commit -m "feat(plugin): GetConnectionFocus host RPC + focus-aware scene emit routing (holomush-5rh.8)"`

---

## Phase 2: Host substrate

### Task 8: Quiet select — `SelectCharacterRequest.client_type`

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto` (SelectCharacterRequest, line ~734), `internal/grpc/auth_handlers.go` (fresh-create branch after line ~340), `api/proto/holomush/web/v1/web.proto` (WebSelectCharacterRequest), `internal/web/auth_handlers.go` (passthrough)
- Test: `internal/grpc/auth_handlers_test.go` (match existing SelectCharacter tests)

- [ ] **Step 1: Proto.** Add to `SelectCharacterRequest`:

```proto
  // client_type declares the surface establishing the session
  // (terminal/comms_hub/telnet — the session_connections vocabulary). When
  // "comms_hub", a FRESH session creation skips the grid arrive emission:
  // scenes-workspace sessions must not announce the character on the grid
  // (spec 2026-06-07 §V2). Empty preserves the legacy behavior (arrive).
  // Reattach paths never re-emit arrive regardless of this field.
  string client_type = 3;
```

Mirror the field (same comment, adapted) onto `WebSelectCharacterRequest` in web.proto. `task proto && task lint:proto`.

- [ ] **Step 2: Failing test** — find the arrive-emission assertion in the existing fresh-session tests (`rg -n "arrive" internal/grpc/auth_handlers_test.go internal/grpc/server_test.go`); add:

```go
func TestSelectCharacterSkipsArriveForCommsHubFreshSession(t *testing.T) {
	// fresh session path with ClientType: "comms_hub" → engine/event sink
	// records NO arrive event; session still created.
}

func TestSelectCharacterStillEmitsArriveByDefault(t *testing.T) { /* empty client_type → arrive recorded */ }
```

- [ ] **Step 3: Implement.** In the FRESH-create branch of `SelectCharacter` (auth_handlers.go), locate the arrive/`HandleConnect` emission (`rg -n "HandleConnect\|Arrive" internal/grpc/auth_handlers.go`) and gate it:

```go
	if req.GetClientType() != "comms_hub" {
		// existing arrive emission, unchanged
	}
```

Web passthrough: `WebSelectCharacter` forwards `req.Msg.GetClientType()` into the core request.

- [ ] **Step 4: Run** `task test -- ./internal/grpc/` (needs Docker — sessiontest). PASS. Commit — `jj commit -m "feat(core): comms_hub SelectCharacter skips grid arrive (holomush-5rh.8)"`

### Task 9: Grid-presence filter on `ListActiveByLocation`

**Files:**

- Modify: `internal/store/session_store.go` (`ListActiveByLocation` — `session.Store` has exactly ONE implementation, `PostgresSessionStore`; the in-memory store was removed per `docs/superpowers/specs/2026-05-23-remove-session-memstore-design.md`)
- Test: `internal/store/session_store_integration_test.go` (or the package's non-tagged Store tests — `sessiontest`-backed tests run under plain `task test` with Docker)

- [ ] **Step 1: Failing test:** a session whose only connection has `client_type='comms_hub'` does NOT appear in `ListActiveByLocation` for its location; the same session appears once a `terminal` connection is added. Use `sessiontest.NewStoreWithPool` per the testing rules (Docker required).

- [ ] **Step 2: Implement.** Add to the `ListActiveByLocation` query:

```sql
AND EXISTS (
    SELECT 1 FROM session_connections c
    WHERE c.session_id = sessions.id
      AND c.client_type IN ('terminal', 'telnet')
)
```

Keep the rest of the query untouched.

- [ ] **Step 3: Check ripple** — consumers are `internal/grpc/list_focus_presence.go`, `internal/grpc/location_follow.go`, and `internal/session/reaper.go` (`rg -rn "ListActiveByLocation" internal/ | rg -v _test`). The reaper and location-follow paths must be inspected: if either relies on comms_hub-only sessions being listed (e.g. idle reaping), move the filter into the presence call sites instead of the store query — decide by reading both call sites and record the choice in the commit message. Run `task test -- ./internal/session/ ./internal/store/` + `task test:int`. Commit — `jj commit -m "feat(session): grid presence counts only terminal/telnet-connected sessions (holomush-5rh.8)"`

### Task 10: `scene_activity` badges — control-frame downgrade (INV-SCENE-62)

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto` (ControlSignal + ControlFrame, ~line 554), `api/proto/holomush/web/v1/web.proto` (the web-side `ControlFrame` mirror needs a matching `scene_id` field — without it the badge payload dies at the gateway), `internal/grpc/server.go` (Subscribe filter assembly + `runSubscribeLoop` state + `dispatchDelivery`), `internal/web/handler.go` — the control-frame branch within `forwardFrame` (function starts at handler.go:519; the branch at ~543–560 maps only `Signal`/`Message`/`AttachMomentMs` today — add `SceneId`)
- Test: `internal/grpc/subscribe_server_test.go` + a Ginkgo spec in `test/integration/scenes/` + a `forwardFrame` unit case in `internal/web/handler_test.go` asserting the scene_id round-trips

- [ ] **Step 1: Proto.** Read the `ControlSignal` enum (`rg -n "enum ControlSignal" -A 20 api/proto/holomush/core/v1/core.proto`) and append the next value:

```proto
  // CONTROL_SIGNAL_SCENE_ACTIVITY notifies the client that a scene it is a
  // member of received an event while this connection was NOT focused on it.
  // Carries scene_id only — never event content (the payload may be
  // encrypted; the ping requires no decryption). Drives workspace unread
  // badges; lossy by design (clients re-sync via ListMyScenes snapshots).
  CONTROL_SIGNAL_SCENE_ACTIVITY = <next>;
```

Add to `ControlFrame`:

```proto
  // scene_id identifies the scene that produced a SCENE_ACTIVITY signal; the
  // bare scene ULID (not a subject). Set ONLY on
  // CONTROL_SIGNAL_SCENE_ACTIVITY; clients reading other signals MUST ignore it.
  string scene_id = 4;
```

`task proto && task lint:proto && task web:generate`.

- [ ] **Step 2: Filter union.** In the Subscribe handler where the consumer `filterSet`/`filterSubjects` are assembled from the focus `RestorePlan`, union in every scene subject from `info.FocusMemberships` (both `.ic` and `.ooc` facets — reuse the subject-building helper `streamToFocusKey`'s inverse; check `internal/grpc/focus/` for an existing `FocusKey→subjects` helper before writing one). Membership ⊆ participants, so non-members' consumers never carry these subjects — that IS the INV-SCENE-62 guarantee.

- [ ] **Step 3: Track connection focus in the loop.** `runSubscribeLoop` already applies focus-driven ctrl updates (`applyFilterCtrl`). Extend the loop state with `currentFocus *session.FocusKey`, initialized from the connection's `FocusKey` at Subscribe start (`sessionStore.GetConnection`) and updated whenever a ctrl message carries a focus change (extend the ctrl payload struct if it doesn't already carry the new focus — read `applyFilterCtrl` first).

- [ ] **Step 4: Downgrade in `dispatchDelivery`.** Before the existing send path (and before any payload work):

```go
	// E9.5 badge downgrade (INV-SCENE-62): a scene event for a connection
	// not focused on that scene becomes a content-free activity ping.
	if sid, ok := extractSceneID(event.Subject.String()); ok {
		focusedOn := currentFocus != nil &&
			currentFocus.Kind == session.FocusKindScene &&
			currentFocus.TargetID.String() == sid
		if !focusedOn {
			frame := &corev1.SubscribeResponse{Frame: &corev1.SubscribeResponse_Control{
				Control: &corev1.ControlFrame{
					Signal:  corev1.ControlSignal_CONTROL_SIGNAL_SCENE_ACTIVITY,
					SceneId: sid,
				},
			}}
			if sendErr := stream.Send(frame); sendErr != nil { /* nack path, mirror below */ }
			// ack and return — never fall through to the event send
		}
	}
```

(`extractSceneID` already exists in `internal/grpc/stream_access.go` — reuse it; adjust the snippet to `dispatchDelivery`'s actual parameter names and ack/nack idioms, which you must mirror exactly.)

- [ ] **Step 5: Tests.**

```go
// Verifies: INV-SCENE-62
// integration (WithInTreePlugins + WithFocusDelivery): char member of scenes
// A+B, connection focused on A; emit in B → connection receives
// CONTROL_SIGNAL_SCENE_ACTIVITY{scene_id: B}, and NO EventFrame for B;
// emit in A → normal EventFrame; a NON-member's connection receives nothing
// for either.
```

Plus a unit-level test on the downgrade branch in `subscribe_server_test.go`. Run `task test -- ./internal/grpc/`, `task test:int`. Commit — `jj commit -m "feat(core): scene_activity control-frame badges for non-focused member connections (holomush-5rh.8)"`

### Task 11: SceneAccessService facade (identity, guest gate — INV-SCENE-63/64)

**Files:**

- Create: `api/proto/holomush/sceneaccess/v1/sceneaccess.proto`, `internal/grpc/sceneaccess_service.go`, `internal/grpc/sceneaccess_service_test.go`
- Modify: `cmd/holomush/sub_grpc.go` (registration), `buf.yaml`/`buf.gen.yaml` only if new proto dirs need listing (check how `content/v1` is included — likely automatic)

- [ ] **Step 1: Proto** — new package `holomush.sceneaccess.v1`, service `SceneAccessService`, RPCs: `ListScenesForViewer`, `GetSceneForViewer`, `ListMyScenes`, `WatchScene`, `ExportScene`, `SetSceneFocus`, `ListPublishedScenes`, `GetPublicSceneArchive`, `DownloadPublicSceneArchive`. Every request carries:

```proto
  // session_id + player_session_token authenticate the calling player; the
  // facade resolves the acting character SERVER-SIDE and ignores/overrides
  // any client-supplied identity (INV-SCENE-63).
  string session_id = 1;
  string player_session_token = 2;
```

plus per-RPC fields (`character_id` meaning "act as this alt of mine — ownership verified", `scene_id`, `format`, `connection_id`+optional `scene_id` for SetSceneFocus, limit/offset/tags for lists). Responses re-export the scene.proto messages (import them) — no parallel shapes. Full doc comments; `task proto && task lint:proto`.

- [ ] **Step 2: Failing tests** (`sceneaccess_service_test.go`, mockery mocks for the SceneService client — add `scenev1.SceneServiceClient` to `.mockery.yaml` and run `mockery`):

```go
// Verifies: INV-SCENE-63
func TestSceneAccessOverridesClientSuppliedCharacterWithOwnedAlt(t *testing.T) {
	// player owns chars A,B; request names character C (not owned) → NotFound
	// "character does not belong to this player"; request naming B → the
	// downstream SceneService call receives B (assert on the mock).
}

// Verifies: INV-SCENE-64
func TestSceneAccessDeniesGuestPlayersEverywhere(t *testing.T) {
	// table over every RPC: IsGuest player → PermissionDenied
	// "guests cannot access scenes"; downstream client never invoked.
}

func TestWatchSceneRequiresExistingAltSession(t *testing.T) {
	// FindByCharacter → SESSION_NOT_FOUND → FailedPrecondition telling the
	// client to select the character first.
}
```

- [ ] **Step 3: Implement `SceneAccessServer`.** Constructor takes: the player-session resolver (refactor `CoreServer.resolvePlayerSession` (auth_handlers.go:182) into an unexported **package-level function** in `internal/grpc` — `SceneAccessServer` lives in the same package, so both call it directly; behavior identical, the method becomes a one-line delegate), `playerRepo` (IsGuest), `charRepo.ListByPlayer` (ownership), `sessionStore` (FindByCharacter, GetConnection), `coordinator focus.Coordinator` (SetSceneFocus), and a `scenev1.SceneServiceClient`. Per-RPC skeleton:

```go
// 1. ps := resolvePlayerSession(token) → Unauthenticated on failure
// 2. player := playerRepo.GetByID(ps.PlayerID); player.IsGuest →
//        status.Error(codes.PermissionDenied, "guests cannot access scenes")
// 3. char := ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
//        → NotFound when not owned (never pass client id through unverified)
// 4. delegate to sceneClient.<RPC> with the VERIFIED character_id
//        (and session_id from FindByCharacter where the RPC needs it)
// 5. errors: log internally, return generic codes — never leak inner error
//        text (grpc-errors rule); pass through downstream status codes with
//        line-scoped //nolint:wrapcheck where appropriate.
```

`SetSceneFocus`: verify the connection belongs to one of the player's characters' sessions (`GetConnection` → its session → session.CharacterID owned), then `coordinator.SetConnectionFocus(connID, focusKeyOrNil)`.

- [ ] **Step 4: Obtain the SceneService client.** In `sub_grpc.go`, after plugins load, resolve via the plugin service registry exactly the way `GRPCServiceProxy` reaches plugin services (see `serviceRegistry := s.cfg.Plugins.ServiceRegistry()` at sub_grpc.go:234 and the harness precedent `internal/testsupport/integrationtest/harness.go` which builds a `SceneService` client from the registry's `svc.Conn`). Register: `sceneaccessv1.RegisterSceneAccessServiceServer(s.grpcServer, sceneAccessServer)`. Guard nil (scenes plugin absent → RPCs return Unimplemented).

- [ ] **Step 5: Run** `task test -- ./internal/grpc/`, `task lint`, `task test:int`. Commit — `jj commit -m "feat(core): SceneAccessService facade with identity override + guest gate (holomush-5rh.8)"`

---

## Phase 3: Gateway

### Task 12: WebService scene RPCs + gateway passthrough + TS regen

**Files:**

- Modify: `api/proto/holomush/web/v1/web.proto`, `internal/web/handler.go` (~line 95–125: client interfaces/options), `cmd/holomush/gateway.go` (~line 308), `internal/grpc/client.go` (or wherever `holoGRPC.NewClient`'s returned type lives — find with `rg -n "func NewClient" internal/grpc/`)
- Create: `internal/web/scene_handlers.go`, `internal/web/scene_handlers_test.go`

- [ ] **Step 1: web.proto.** Add `WebListScenes`, `WebGetScene`, `WebListMyScenes`, `WebWatchScene`, `WebExportScene`, `WebSetSceneFocus`, `WebListPublishedScenes`, `WebGetPublicSceneArchive`, `WebDownloadPublicSceneArchive`. Requests carry `session_id` only (the token rides the `headerInjectSessionToken` header exactly like `WebListFocusPresence` — internal/web/handler.go:733); responses re-export sceneaccess/scene messages. Doc comments per rule. `task proto && task lint:proto && task web:generate`.

- [ ] **Step 2: SceneAccessClient seam.** In `handler.go`, add alongside `ContentClient`:

```go
// SceneAccessClient is the gRPC interface used by Handler to reach the
// core scene-access facade (one method per Web* scene RPC, all
// sceneaccessv1 types).
type SceneAccessClient interface {
	ListScenesForViewer(ctx context.Context, req *sceneaccessv1.ListScenesForViewerRequest) (*sceneaccessv1.ListScenesForViewerResponse, error)
	// ... one per RPC from Task 11
}

// WithSceneAccessClient mirrors WithContentClient (handler.go:119).
func WithSceneAccessClient(c SceneAccessClient) HandlerOption { return func(h *Handler) { h.sceneAccess = c } }
```

Extend the `holoGRPC` client wrapper with the sceneaccess methods the same way it exposes content methods, and wire `gateway.go:308`: `web.NewHandler(grpcClient, web.WithContentClient(grpcClient), web.WithSceneAccessClient(grpcClient))`.

- [ ] **Step 3: Handlers** (`scene_handlers.go`) — pure translation, modeled char-for-char on `WebListFocusPresence`: read token header → build sceneaccess request (session_id + token + op fields) → call → translate → pass through status errors with line-scoped `//nolint:wrapcheck`. NO logic.

- [ ] **Step 4: Tests** (`scene_handlers_test.go`): mockery mock of `SceneAccessClient`; per handler assert token header forwarding + field mapping + error passthrough opacity (mirror the existing web handler test idiom).

- [ ] **Step 5: Run** `task test -- ./internal/web/`, `task lint`, `cd web && pnpm check` (regenerated TS compiles). Commit — `jj commit -m "feat(web): scene Web* RPCs proxying the scene-access facade (holomush-5rh.8)"`

---

## Phase 4: ABAC

### Task 13: `is_guest` player attribute (omit-don't-sentinel)

**Files:**

- Modify: `internal/access/policy/attribute/player.go`, `cmd/holomush/sub_grpc.go` (provider construction site — `rg -n "NewPlayerAttributeProvider" cmd/ internal/`)
- Test: `internal/access/policy/attribute/player_test.go`

- [ ] **Step 1: Failing tests** (ACE, mirror the character provider's witness tests):

```go
func TestPlayerProviderEmitsIsGuestWitnessWhenLookupConfigured(t *testing.T) {
	// guest player → attrs["is_guest"]==true, attrs["has_is_guest"]==true
	// registered player → attrs["is_guest"]==false, attrs["has_is_guest"]==true
}
func TestPlayerProviderOmitsIsGuestWhenLookupAbsentOrFails(t *testing.T) {
	// no lookup configured / lookup error → "is_guest" key ABSENT,
	// "has_is_guest"==false (witness always present — ADR holomush-ti1b)
}
```

- [ ] **Step 2: Implement.** Add an optional lookup seam:

```go
// PlayerKindLookup resolves whether a player is an ephemeral guest.
type PlayerKindLookup interface {
	GetByID(ctx context.Context, id ulid.ULID) (*auth.Player, error)
}

// WithPlayerKindLookup supplies the lookup; without it the provider omits
// is_guest (has_is_guest=false) per the omit-don't-sentinel rule.
```

(If importing `internal/auth` from the attribute package creates a cycle, define a narrow local `func(ctx, playerID string) (isGuest bool, err error)` lookup instead — check imports first.) Emit per ADR holomush-ti1b: value key omitted on unresolved, witness always present. Register both keys in `Schema()`.

- [ ] **Step 3: Wire** the lookup at the provider construction site using the auth player repo already in scope there. Run `task test -- ./internal/access/...`, `task lint`. Commit — `jj commit -m "feat(access): is_guest player attribute with has_ witness (holomush-5rh.8)"`

---

## Phase 5: Frontend

> Reference implementations: stream loop — `web/src/routes/(authed)/terminal/+page.svelte:331` (`for await (const response of client.streamEvents(...))`); client construction — `web/src/routes/(authed)/characters/+page.svelte` (`createClient(WebService, transport)`); UI primitives — `web/src/lib/components/ui/` (bits-ui via shadcn-svelte; add new ones with the shadcn-svelte CLI, never a new UI lib). Approved layouts: spec §5.4 (board = rich list rows; workspace = 3-pane; archive = read-only chrome).

### Task 14: Routes scaffold + non-guest guard + nav entry

**Files:**

- Create: `web/src/routes/(authed)/scenes/+layout.ts`, `+layout.svelte`, `+page.svelte` (workspace shell), `browse/+page.svelte`, `archive/+page.svelte`, `[id]/+page.svelte` (placeholder shells rendering a heading each)
- Modify: the sidebar/icon-rail nav component (`rg -ln "terminal" web/src/lib/components/sidebar/`) to add the Scenes entry

- [ ] **Step 1: Guard** in `scenes/+layout.ts`:

```ts
import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';
import { redirect } from '@sveltejs/kit';

export const ssr = false;
export const load = async () => {
	const client = createClient(WebService, transport);
	const session = await client.webCheckSession({}); // throws → (authed) layout redirects to /login
	if (session.isGuest) throw redirect(302, '/terminal'); // INV-SCENE-64 UX guard (server gate is the facade)
	return { playerId: session.playerId, characters: session.characters };
};
```

- [ ] **Step 2: Route shells + nav icon** (lucide `Drama` or `Clapperboard` to match the rail's icon style). **Step 3:** `cd web && pnpm check` passes; manual smoke via `task dev` optional. Commit — `jj commit -m "feat(web-client): scenes routes scaffold + non-guest guard (holomush-5rh.8)"`

### Task 15: Workspace data layer (alt sessions, streams, badges)

**Files:**

- Create: `web/src/lib/scenes/types.ts`, `web/src/lib/scenes/client.ts`, `web/src/lib/scenes/altSessions.svelte.ts`, `web/src/lib/scenes/workspaceStore.svelte.ts`
- Test: `web/src/lib/scenes/workspaceStore.test.ts` (vitest — `cd web && pnpm test:unit`; runner confirmed in web/package.json)

- [ ] **Step 1: `types.ts`** — `WorkspaceScene { sceneId, title, locationId, state, tags, role, asCharacterId, asCharacterName, lastActivityMs, entryCount, unread }`, `LogEntry { id, kind: 'pose'|'say'|'ooc'|'system', actorId, actorName, text, timestampMs, contentWarning? }`, parsing helpers `eventFrameToLogEntry` (map the EventFrame's qualified type — e.g. `core-scenes:scene_pose` — and JSON payload `{actor_id, scene_id, text}` per `handleEmit`'s marshal shape).

- [ ] **Step 2: `client.ts`** — one `createClient(WebService, transport)` and typed wrappers: `listMyScenes(sessionId)`, `listScenes(...)`, `watchScene(...)`, `exportScene(...)`, `setSceneFocus(sessionId, connectionId, sceneId|null)`, `sendSceneCommand(sessionId, cmd)` (via `sendCommand`).

- [ ] **Step 3: `altSessions.svelte.ts`** — per-alt session manager:

```ts
// ensureSession(characterId): webSelectCharacter({ characterId, clientType: 'comms_hub' })
//   → cache sessionId per character (reattach-safe server-side).
// openStream(sessionId): for-await over client.streamEvents({ sessionId }) —
//   mirror the terminal page's loop incl. STREAM_OPENED → capture connectionId,
//   REPLAY_COMPLETE boundary, reconnect-with-backoff, and STREAM_CLOSED → re-auth.
// Dispatch frames: EventFrame → workspaceStore.ingestEvent(sessionId, frame);
//   ControlFrame SCENE_ACTIVITY → workspaceStore.bumpUnread(frame.sceneId).
```

- [ ] **Step 4: `workspaceStore.svelte.ts`** (runes) — state: `myScenes`, `watching`, `selectedSceneId`, `logsBySceneId`, `unreadBySceneId`; actions: `refresh()` (ListMyScenes snapshot → seed badges/activity), `select(sceneId)` (ensure alt session+stream **and that the `STREAM_OPENED` control frame's `connection_id` has been captured — `setSceneFocus` cannot be called before it arrives** → `setSceneFocus(sessionId, connectionId, sceneId)` → clear unread → backfill via `webQueryStreamHistory` on `events.<game>.scene.<id>.ic` if the replay tail left a gap), `ingestEvent` (append `LogEntry` to the focused scene; ignore non-scene frames), `bumpUnread` (skip when `sceneId === selectedSceneId` — dedup rule from spec D7).

- [ ] **Step 5:** `pnpm check` + unit tests if runner exists. Commit — `jj commit -m "feat(web-client): scenes workspace data layer (alt sessions, focus, badges) (holomush-5rh.8)"`

### Task 16: Workspace UI (3-pane, pose cards, composer)

**Files:**

- Create: `web/src/lib/components/scenes/SceneListItem.svelte`, `PoseCard.svelte`, `OocLine.svelte`, `PoseOrderStrip.svelte`, `SceneComposer.svelte`, `SceneContextRail.svelte`
- Modify: `web/src/routes/(authed)/scenes/+page.svelte`

- [ ] **Step 1: Layout** per spec §5.4: left pane (My scenes + Watching lists, unread `ui/badge`, "as <character>" line), center (scrollable `ui/scroll-area` log + pose-order strip + composer), right rail (Scene/Roster incl. observers/Activity panels using the existing rail panel idiom). Grid: `grid-cols-[260px_1fr_300px]` desktop.

- [ ] **Step 2: `PoseCard`** — author banner (avatar initial, cyan name, timestamp) + body text; `OocLine` dim italic; system entries muted. **Step 3: `SceneComposer`** — `ui/` textarea + acting-character chip + buttons Pose/Say/OOC mapping to `scene pose|say|ooc <text>` via `sendSceneCommand` on the scene's alt session; disabled with a primary **Join scene** button when `role === 'observer'` (join sends `scene join #<id>`, then store refreshes role). Drafts: `localStorage` keyed `scene-draft-<sceneId>`.

- [ ] **Step 4: Live region** — the log container carries `role="log" aria-live="polite" aria-label="scene log"`. **Step 5:** `pnpm check`; manual smoke with `task dev` (two browser tabs: terminal char + workspace alt; pose from workspace appears in both). Commit — `jj commit -m "feat(web-client): scenes workspace UI (holomush-5rh.8)"`

### Task 17: Board, archive, ended-scene page, export downloads

**Files:**

- Modify: `web/src/routes/(authed)/scenes/browse/+page.svelte`, `archive/+page.svelte`, `[id]/+page.svelte`
- Create: `web/src/lib/scenes/download.ts`, `web/src/lib/components/scenes/SceneBoardRow.svelte`, `TagFilter.svelte`

- [ ] **Step 1: Board** — `webListScenes({ limit, offset, tags })` → rich list rows (status dot, title, location, tag chips via `ui/badge`, "N here", relative last activity) + toolbar (search input filters client-side on title; tag chips drive the server `tags` filter) + per-row **Watch** (open scenes → `webWatchScene` then jump to workspace) and **Join** (sends `scene join`) actions.

- [ ] **Step 2: Archive** — `webListPublishedScenes` rows → `[id]` page; **`[id]` page**: `webGetScene`; if published → `webDownloadPublicSceneArchive(format='jsonl')`, else participant → `webExportScene(format='jsonl')`; parse jsonl lines into `LogEntry[]` and render with the same `PoseCard` components; read-only chrome (no composer); export buttons (md + jsonl).

- [ ] **Step 3: `download.ts`**

```ts
export function downloadBlob(content: Uint8Array | string, mime: string, filename: string) {
	const blob = new Blob([content], { type: mime });
	const url = URL.createObjectURL(blob);
	const a = Object.assign(document.createElement('a'), { href: url, download: filename });
	a.click();
	URL.revokeObjectURL(url);
}
```

- [ ] **Step 4:** `pnpm check`; commit — `jj commit -m "feat(web-client): scene board, archive, export downloads (holomush-5rh.8)"`

### Task 18: Mobile + accessibility pass

**Files:** modify the Task 14–17 components.

- [ ] **Step 1: Mobile** — below `md:` the scene list collapses into a `ui/sheet` (left), the context rail into a `ui/sheet` (right); the log+composer is the page. Triggers in a slim header bar. **Step 2: A11y** — landmarks (`nav`/`main`/`aside`), keyboard: scene list arrow-key navigable (roving tabindex), composer ⌘/Ctrl+Enter submits, focus moves to the log on scene switch; all interactive elements labeled. **Step 3:** `pnpm check` + Playwright snapshot smoke on a 390px viewport (folded into Task 19's spec file). Commit — `jj commit -m "feat(web-client): scenes mobile + a11y pass (holomush-5rh.8)"`

---

## Phase 6: End-to-end verification & registry

### Task 19: Playwright E2E suite

**Files:**

- Modify: `web/e2e/scenes.spec.ts` — it ALREADY EXISTS with "Scene lifecycle (Phase 2)" and "Scene focus routing (Phase 5)" describes; append a new `test.describe('Scenes workspace (E9.5)')` block. Reuse its existing login/scene-seeding helpers and the `web/e2e/helpers/` directory — do NOT invent a new fixture pattern.

- [ ] **Step 1: Scenarios** (each a `test(...)`):

1. registered player → `/scenes/browse` lists a seeded open scene; tag filter narrows.
2. watch flow: Watch → workspace shows the scene under Watching, log visible, composer replaced by Join.
3. participate: Join → composer enables → pose → pose card appears (live, no reload).
4. terminal isolation: with a terminal session open in the same context, workspace focus changes never alter the terminal page's stream (assert terminal still receives its location events).
5. export: ended seeded scene → md + jsonl downloads (assert `Download` events + filenames).
6. guest: guest login → `/scenes` redirects to `/terminal`.
7. mobile: 390×844 viewport → sheets open/close; log readable.
8. a11y: if an axe helper exists in `web/e2e` deps use it on `/scenes`; otherwise assert the structural roles (`role=log`, `aria-live`, landmark count) — note which path was taken in the spec file comment.

- [ ] **Step 2: Quarantine check** — none of these may land flaky; if one is, fix or drop it (never mark quarantine in the same PR per the quarantine rules). Run `task test:e2e`. Commit — `jj commit -m "test(e2e): scenes workspace suite (holomush-5rh.8)"`

### Task 20: Invariant bindings + registry flip + final gates

**Files:**

- Modify: `docs/architecture/invariants.yaml`, the binding test files from Tasks 3/10/11
- Generated: `docs/architecture/invariants.md` (via `go run ./cmd/inv-render`)

- [ ] **Step 1:** Confirm the `// Verifies:` annotations sit immediately above the genuinely-asserting tests: INV-SCENE-61 → `TestWatchSceneRejectsNonOpenSceneBeforeConsultingABAC` (+ the observer-exclusion pins from Task 4 as additional sites), INV-SCENE-62 → the Task 10 integration spec, INV-SCENE-63 → `TestSceneAccessOverridesClientSuppliedCharacterWithOwnedAlt`, INV-SCENE-64 → `TestSceneAccessDeniesGuestPlayersEverywhere`. Do NOT annotate tests that merely touch the code.

- [ ] **Step 2:** Flip each registry entry to `binding: bound` with `asserted_by:` listing the test files; `go run ./cmd/inv-render`; run `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted|TestGeneratedDocIsCurrent' ./test/meta/`.

- [ ] **Step 3: Full gates** — `task test`, `task test:int`, `task pr-prep` (sole command; exit code is the verdict). The diff touches int/E2E surface → run `task pr-prep:full` locally once before the PR.

- [ ] **Step 4: Commit** — `jj commit -m "docs(invariants): bind INV-SCENE-61..64 (holomush-5rh.8)"`

---

## Out of scope (follow-up beads, filed at materialization)

Persisted read markers · telnet `scene watch` command (uses Task 3's `WatchScene` + Task 7's focus plumbing) · composer preview/rich text · published-archive full-text search.
<!-- adr-capture: sha256=e73ebf1036cf856c; session=66ca5652; ts=2026-06-07T20:32:25Z; adrs=holomush-wf4zj,holomush-zukuh,holomush-pc3bg,holomush-b0365,holomush-0qnnr -->
