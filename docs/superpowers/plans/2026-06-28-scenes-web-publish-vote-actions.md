<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Scenes Web Publish-Vote Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the scene publish loop (Start / Cast-vote / Withdraw + a live tally panel) to the web portal, with publish status modeled as a scene-read attribute, leaving the already-complete telnet surface untouched.

**Architecture:** Vertical web-exposure slice over a complete backend. New `SceneInfo` fields carry the *active* publish-attempt pointer (affordance gating + cold-start discovery); the participant-gated tally is read via a facade/BFF passthrough of `GetPublishedScene`; the three write verbs are passthroughs; the panel updates live off the existing `scene_publish_*` IC stream. **No ABAC engine on any publish path** (INV-SCENE-33) — the facade mirrors `EndScene` *minus* the gate; the SceneAccessServer holds no policy engine at all.

**Tech Stack:** Go (protobuf/buf, connect-go), SvelteKit 5 + connect-es v2 (`@bufbuild/protobuf` v2.12), Playwright E2E. No new external libraries.

**Spec:** [docs/superpowers/specs/2026-06-28-scenes-web-publish-vote-actions-design.md](../specs/2026-06-28-scenes-web-publish-vote-actions-design.md)

**Reviewers:** `abac-reviewer` REQUIRED (engine-exclusion + observer visibility of pointer/per-voter ballots); `crypto-reviewer` NOT required.

**Red-build window:** Connect's `WebServiceHandler` interface has no `Unimplemented` embed and `*Handler` implements it directly (`internal/web/handler.go:139` `var _ webv1connect.WebServiceHandler = (*Handler)(nil)`). After Task 1 adds the four `Web*` RPCs to the proto, `task build` stays RED until Task 5 lands the BFF methods. Tasks 1–4 are gated by their own checks (`task lint:proto`, package tests), **not** `task build`. This mirrors the prior three slices — do not treat the red build as a Task 1–4 failure.

---

## File Structure

| File | Responsibility | Task |
| --- | --- | --- |
| `api/proto/holomush/scene/v1/scene.proto` | `SceneInfo` += `active_publish_attempt_id`, `publish_status` | 1 |
| `api/proto/holomush/sceneaccess/v1/sceneaccess.proto` | facade publish RPCs + messages | 1 |
| `api/proto/holomush/web/v1/web.proto` | BFF publish RPCs + messages | 1 |
| `plugins/core-scenes/service.go` | `GetScene` populates the two new fields | 2 |
| `internal/grpc/sceneaccess_service.go` | facade: 3 writes + `GetPublishedScene` (no engine) | 3 |
| `internal/web/handler.go` | `SceneAccessClient` iface += 4 methods | 4 |
| `cmd/holomush/deps.go`, `deps_test.go` | `GRPCClient` narrowing iface + `mockGRPCClient` += 4 | 4 |
| `internal/web/scene_handlers.go` | BFF: 4 `Web*` handlers | 5 |
| `web/src/lib/scenes/client.ts` | 4 client wrappers | 6 |
| `web/src/lib/scenes/publishFlow.ts` (new) | flow actions + `publishStateBySceneId` reducer | 6 |
| `web/src/lib/scenes/workspaceStore.svelte.ts`, `types.ts` | thread new fields; route publish events | 6 |
| `web/src/lib/components/scenes/ScenePublishPanel.svelte` (new) | layout-C panel | 7 |
| `web/src/lib/components/scenes/SceneContextRail.svelte` | mount the panel | 7 |
| `web/e2e/scenes-publish.spec.ts` (new) | telnet-free E2E | 8 |

---

## Task 1: Proto — SceneInfo fields + facade & BFF publish RPCs

**Files:**

- Modify: `api/proto/holomush/scene/v1/scene.proto:225-268` (SceneInfo)
- Modify: `api/proto/holomush/sceneaccess/v1/sceneaccess.proto`
- Modify: `api/proto/holomush/web/v1/web.proto`
- Generated (commit): `pkg/proto/**/*.pb.go`, `pkg/proto/**/*connect*.go`, `web/src/lib/connect/**/*_pb.ts`

- [ ] **Step 1: Add the two SceneInfo fields**

In `scene.proto`, inside `message SceneInfo { ... }`, after `int64 last_activity_ms = 15;` add:

```proto
  // The in-flight publication attempt's ID, or empty when no attempt is
  // active. Populated by GetScene from activeAttemptID (commands.go) over the
  // scene's published_scenes rows. Active-only: reflects an attempt in
  // COLLECTING or COOLOFF and clears once the attempt resolves. The portal
  // uses it to gate the publish panel's Start-vs-in-progress affordance and to
  // key the participant-gated tally read (GetPublishedScene). Carries NO tally
  // (SceneInfo is broadly readable; INV-SCENE-60/61).
  string active_publish_attempt_id = 16;
  // The active attempt's state-machine phase ("COLLECTING" or "COOLOFF"), or
  // empty when no attempt is active. Non-sensitive phase signal; the counts
  // stay behind GetPublishedScene's participant gate.
  string publish_status = 17;
```

- [ ] **Step 2: Add facade publish RPCs**

In `sceneaccess.proto`, add to the `SceneAccessService` service block (after `LeaveScene`):

```proto
  // StartScenePublish starts a publication vote on an ended scene. Participant-
  // gated inside the plugin (INV-SCENE-33: no ABAC engine on this path); the
  // facade resolves session/character and dispatches without an engine call.
  rpc StartScenePublish(StartScenePublishRequest) returns (StartScenePublishResponse);
  // CastPublishSceneVote casts or changes the caller's Yes/No vote on the
  // active attempt. Frozen-roster participant gate enforced in the plugin store.
  rpc CastPublishSceneVote(CastPublishSceneVoteRequest) returns (CastPublishSceneVoteResponse);
  // WithdrawScenePublish aborts the active attempt (scene owner only; gate in
  // the plugin handler).
  rpc WithdrawScenePublish(WithdrawScenePublishRequest) returns (WithdrawScenePublishResponse);
  // GetPublishedScene reads the active attempt's status + vote tally for a
  // participant cold-start snapshot. Participant-gated (INV-SCENE-60); the
  // facade passes the plugin's status; the response is trimmed (no frozen
  // content — that is GetPublicSceneArchive's job).
  rpc GetPublishedScene(GetPublishedSceneRequest) returns (GetPublishedSceneResponse);
```

And the messages (place after `LeaveSceneResponse`):

```proto
// StartScenePublishRequest carries the resolved session + acting character and
// the scene to publish.
message StartScenePublishRequest {
  // Web session id (telemetry/logging correlation).
  string session_id = 1;
  // Opaque player-session bearer token, validated by resolveAndGate.
  string player_session_token = 2;
  // The acting character; ownership verified by ownedCharacter.
  string character_id = 3;
  // The scene to start a publish vote on; MUST be ended.
  string scene_id = 4;
}

// StartScenePublishResponse returns the new attempt's id + ordinal.
message StartScenePublishResponse {
  // The publication attempt id (published_scenes.id).
  string published_scene_id = 1;
  // The attempt's 1-based ordinal within the scene's budget.
  int32 attempt_number = 2;
}

// CastPublishSceneVoteRequest carries the attempt id + the Yes/No choice.
message CastPublishSceneVoteRequest {
  string session_id = 1;
  string player_session_token = 2;
  string character_id = 3;
  // The publication attempt to vote on.
  string published_scene_id = 4;
  // The vote: true = yes, false = no.
  bool vote = 5;
}

// CastPublishSceneVoteResponse reports whether this changed an existing vote.
message CastPublishSceneVoteResponse {
  // True when the caller had already voted and this flipped the choice.
  bool is_change = 1;
}

// WithdrawScenePublishRequest aborts the attempt (owner-gated in the plugin).
message WithdrawScenePublishRequest {
  string session_id = 1;
  string player_session_token = 2;
  string character_id = 3;
  string published_scene_id = 4;
}

// WithdrawScenePublishResponse is empty.
message WithdrawScenePublishResponse {}

// GetPublishedSceneRequest reads the attempt's status + tally (participant-gated).
message GetPublishedSceneRequest {
  string session_id = 1;
  string player_session_token = 2;
  string character_id = 3;
  string published_scene_id = 4;
}

// GetPublishedSceneResponse is the trimmed participant view of an attempt:
// status + tally, no frozen content (that is GetPublicSceneArchive).
message GetPublishedSceneResponse {
  // The attempt id.
  string id = 1;
  // The scene the attempt belongs to.
  string scene_id = 2;
  // The attempt's 1-based ordinal.
  int32 attempt_number = 3;
  // State-machine status: COLLECTING / COOLOFF / PUBLISHED / ATTEMPT_FAILED.
  string status = 4;
  // Failure cause when status is ATTEMPT_FAILED; empty otherwise.
  string failure_reason = 5;
  // The current yes/no/pending tally.
  holomush.scene.v1.PublishedSceneVoteSummary vote_summary = 6;
}
```

> `sceneaccess.proto` already imports `holomush/scene/v1/scene.proto` (its `EndSceneResponse` references `holomush.scene.v1.SceneInfo`), so `PublishedSceneVoteSummary` resolves without a new import. Confirm the import line is present; if not, add `import "holomush/scene/v1/scene.proto";`.

- [ ] **Step 3: Add BFF publish RPCs**

In `web.proto`, add to the `WebService` block (after `WebEndScene` / the other scene RPCs):

```proto
  // WebStartScenePublish proxies StartScenePublish to the facade.
  rpc WebStartScenePublish(WebStartScenePublishRequest) returns (WebStartScenePublishResponse);
  // WebCastPublishSceneVote proxies CastPublishSceneVote to the facade.
  rpc WebCastPublishSceneVote(WebCastPublishSceneVoteRequest) returns (WebCastPublishSceneVoteResponse);
  // WebWithdrawScenePublish proxies WithdrawScenePublish to the facade.
  rpc WebWithdrawScenePublish(WebWithdrawScenePublishRequest) returns (WebWithdrawScenePublishResponse);
  // WebGetPublishedScene proxies GetPublishedScene (cold-start tally snapshot).
  rpc WebGetPublishedScene(WebGetPublishedSceneRequest) returns (WebGetPublishedSceneResponse);
```

And the messages (the BFF token is injected from the cookie header, so requests carry no token field — mirror `WebEndSceneRequest`):

```proto
// WebStartScenePublishRequest starts a publish vote from the web portal.
message WebStartScenePublishRequest {
  string session_id = 1;
  string character_id = 2;
  string scene_id = 3;
}

// WebStartScenePublishResponse mirrors the facade response.
message WebStartScenePublishResponse {
  string published_scene_id = 1;
  int32 attempt_number = 2;
}

// WebCastPublishSceneVoteRequest casts/changes a vote from the web portal.
message WebCastPublishSceneVoteRequest {
  string session_id = 1;
  string character_id = 2;
  string published_scene_id = 3;
  bool vote = 4;
}

// WebCastPublishSceneVoteResponse mirrors the facade response.
message WebCastPublishSceneVoteResponse {
  bool is_change = 1;
}

// WebWithdrawScenePublishRequest withdraws an attempt from the web portal.
message WebWithdrawScenePublishRequest {
  string session_id = 1;
  string character_id = 2;
  string published_scene_id = 3;
}

// WebWithdrawScenePublishResponse is empty.
message WebWithdrawScenePublishResponse {}

// WebGetPublishedSceneRequest reads the cold-start tally snapshot.
message WebGetPublishedSceneRequest {
  string session_id = 1;
  string character_id = 2;
  string published_scene_id = 3;
}

// WebGetPublishedSceneResponse is the trimmed status + tally view.
message WebGetPublishedSceneResponse {
  string id = 1;
  string scene_id = 2;
  int32 attempt_number = 3;
  string status = 4;
  string failure_reason = 5;
  holomush.scene.v1.PublishedSceneVoteSummary vote_summary = 6;
}
```

> `web.proto` already imports `holomush/scene/v1/scene.proto` (`WebEndSceneResponse` uses `SceneInfo`), so `PublishedSceneVoteSummary` resolves. Confirm the import.

- [ ] **Step 4: Regenerate + lint proto**

Run: `task proto && task web:generate`
Then: `task lint:proto`
Expected: green. New stubs appear under `pkg/proto/holomush/sceneaccess/v1/`, `pkg/proto/holomush/web/v1/`, and `web/src/lib/connect/holomush/{web,scene}/v1/`. `task build` will be RED (expected — see Red-build window above).

- [ ] **Step 5: Commit**

```bash
jj commit -m "proto(scenes): SceneInfo publish pointer + facade/BFF publish RPCs (holomush-5rh.24)

SceneInfo += active_publish_attempt_id/publish_status; SceneAccessService +
WebService += Start/Cast/Withdraw publish + GetPublishedScene (trimmed
status+tally). Regenerated stubs. Build red until BFF lands (Task 5).

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: GetScene populates the publish-attempt pointer

**Files:**

- Modify: `plugins/core-scenes/service.go:436` (`GetScene` — insert before the final `return`; `rowToProto` at `:1876` is **unchanged**, the new fields are set on `resp` inside `GetScene`)
- Test: `plugins/core-scenes/service_test.go`

- [ ] **Step 1: Write the failing test**

Append to `service_test.go`:

```go
func TestSceneServiceGetScenePopulatesActivePublishPointer(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         "scene-pub",
		Title:      "Ended Scene",
		OwnerID:    "char-alice",
		State:      string(SceneStateEnded),
		Visibility: string(SceneVisibilityOpen),
	}))
	// One active (COLLECTING) attempt for the scene.
	store.publishedScenes = map[string]*PublishedScene{
		"att-1": {ID: "att-1", SceneID: "scene-pub", AttemptNumber: 1, Status: StatusCollecting},
	}
	svc := newTestService(t, store)

	resp, err := svc.GetScene(context.Background(), &scenev1.GetSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-pub",
	})
	require.NoError(t, err)
	assert.Equal(t, "att-1", resp.GetScene().GetActivePublishAttemptId())
	assert.Equal(t, "COLLECTING", resp.GetScene().GetPublishStatus())
}

func TestSceneServiceGetSceneLeavesPublishPointerEmptyWhenNoActiveAttempt(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         "scene-noatt",
		Title:      "No Attempt",
		OwnerID:    "char-alice",
		State:      string(SceneStateEnded),
		Visibility: string(SceneVisibilityOpen),
	}))
	// A resolved (terminal) attempt must NOT surface as active.
	store.publishedScenes = map[string]*PublishedScene{
		"att-x": {ID: "att-x", SceneID: "scene-noatt", AttemptNumber: 1, Status: StatusPublished},
	}
	svc := newTestService(t, store)

	resp, err := svc.GetScene(context.Background(), &scenev1.GetSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-noatt",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.GetScene().GetActivePublishAttemptId())
	assert.Empty(t, resp.GetScene().GetPublishStatus())
}
```

> `activeAttemptID` (`commands.go:389`) returns the first non-terminal attempt; `PublishedSceneStatus.IsTerminal()` is true for `PUBLISHED`/`ATTEMPT_FAILED`. The `fakeStore.ListSceneAttempts` (`service_test.go:142`) already returns the configured `publishedScenes` filtered by `scene_id`, sorted by `attempt_number`.

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestSceneServiceGetScenePopulatesActivePublishPointer ./plugins/core-scenes/`
Expected: FAIL — `GetActivePublishAttemptId` undefined OR empty (field unpopulated).

- [ ] **Step 3: Populate the fields in GetScene**

In `service.go` `GetScene`, after the observers-roster loop and before `return &scenev1.GetSceneResponse{Scene: resp}, nil`, insert:

```go
	// Surface the active publish attempt (COLLECTING/COOLOFF) so the web portal
	// can gate the publish panel. Active-only; tally stays behind the
	// participant-gated GetPublishedScene (INV-SCENE-60/61). Best-effort: a
	// lookup failure must not fail the scene read.
	if attempts, attErr := s.store.ListSceneAttempts(ctx, row.ID); attErr == nil {
		if attID, ok := activeAttemptID(attempts); ok {
			resp.ActivePublishAttemptId = attID
			for i := range attempts {
				if attempts[i].ID == attID {
					resp.PublishStatus = string(attempts[i].Status)
					break
				}
			}
		}
	} else {
		slog.WarnContext(ctx, "scene.service.get_scene publish-attempt lookup failed",
			"scene_id", row.ID, "error", attErr)
	}
```

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- -run 'TestSceneServiceGetScene(Populates|Leaves)' ./plugins/core-scenes/`
Expected: PASS (both).

- [ ] **Step 5: Format + commit**

```bash
task fmt
jj commit -m "feat(scenes): GetScene surfaces active publish-attempt pointer (holomush-5rh.24)

Populates SceneInfo.active_publish_attempt_id/publish_status from
activeAttemptID; best-effort, never fails the read. Tally stays behind
GetPublishedScene (INV-SCENE-60/61).

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: Facade publish methods (no engine)

**Files:**

- Modify: `internal/grpc/sceneaccess_service.go` (add 4 methods after `LeaveScene`)
- Test: `internal/grpc/sceneaccess_service_test.go`

- [ ] **Step 1: Write the failing test**

Append to `sceneaccess_service_test.go`, using the existing `EndScene` facade-test harness verbatim: the mockery `scenemocks.NewMockSceneServiceClient(t)`, the `testSAToken` constant, `buildSATestPS` / `buildSASessionRepo`, and the 8-arg `newTestSceneAccessServer(t, sessionRepo, playerRepo, charRepo, sessStore, coord, sceneClient, mgr)` constructor (`sceneaccess_service_test.go:164`):

```go
func TestSceneAccessStartScenePublishDispatchesWithoutEngine(t *testing.T) {
	ctx := context.Background()
	playerID := idgen.New()
	char := &world.Character{ID: idgen.New(), PlayerID: playerID, Name: "Alice"}
	ps := buildSATestPS(t, playerID)

	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).Return(&auth.Player{ID: playerID, IsGuest: false}, nil).Maybe()
	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*world.Character{char}, nil).Maybe()

	sceneMock := scenemocks.NewMockSceneServiceClient(t)
	sceneMock.EXPECT().StartScenePublish(mock.Anything, mock.MatchedBy(func(r *scenev1.StartScenePublishRequest) bool {
		return r.GetCallerCharacterId() == char.ID.String() && r.GetSceneId() == "scene-1"
	})).Return(&scenev1.StartScenePublishResponse{PublishedSceneId: "att-9", AttemptNumber: 1}, nil).Once()

	srv := newTestSceneAccessServer(t, buildSASessionRepo(t, ps), playerRepo, charRepo,
		sessionmocks.NewMockStore(t), &stubFocusCoordinator{}, sceneMock, &stubPluginManager{})

	resp, err := srv.StartScenePublish(ctx, &sceneaccessv1.StartScenePublishRequest{
		PlayerSessionToken: testSAToken, CharacterId: char.ID.String(), SceneId: "scene-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "att-9", resp.GetPublishedSceneId())
}

func TestSceneAccessGetPublishedSceneTrimsToStatusAndTally(t *testing.T) {
	ctx := context.Background()
	playerID := idgen.New()
	char := &world.Character{ID: idgen.New(), PlayerID: playerID, Name: "Alice"}
	ps := buildSATestPS(t, playerID)

	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).Return(&auth.Player{ID: playerID, IsGuest: false}, nil).Maybe()
	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*world.Character{char}, nil).Maybe()

	sceneMock := scenemocks.NewMockSceneServiceClient(t)
	sceneMock.EXPECT().GetPublishedScene(mock.Anything, mock.Anything).Return(&scenev1.GetPublishedSceneResponse{
		Id: "att-9", SceneId: "scene-1", AttemptNumber: 1, Status: "COLLECTING",
		Tally: &scenev1.PublishedSceneVoteSummary{Yes: 2, No: 0, Pending: 3},
	}, nil).Once()

	srv := newTestSceneAccessServer(t, buildSASessionRepo(t, ps), playerRepo, charRepo,
		sessionmocks.NewMockStore(t), &stubFocusCoordinator{}, sceneMock, &stubPluginManager{})

	resp, err := srv.GetPublishedScene(ctx, &sceneaccessv1.GetPublishedSceneRequest{
		PlayerSessionToken: testSAToken, CharacterId: char.ID.String(), PublishedSceneId: "att-9",
	})
	require.NoError(t, err)
	assert.Equal(t, "COLLECTING", resp.GetStatus())
	assert.EqualValues(t, 2, resp.GetVoteSummary().GetYes())
	assert.EqualValues(t, 3, resp.GetVoteSummary().GetPending())
}
```

> The mockery `scenemocks.MockSceneServiceClient` regenerates with the four publish methods automatically after Task 1 (run `mockery` if the `.EXPECT().StartScenePublish` helper is missing — config in `.mockery.yaml`). `stubFocusCoordinator` / `stubPluginManager` / `testSAToken` already exist in this test file (used by every lifecycle facade test). Add owner-denial/guest-denial sub-cases mirroring `TestSceneAccessEndScene`'s table if desired.

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run 'TestSceneAccess(StartScenePublish|GetPublishedScene)' ./internal/grpc/`
Expected: FAIL — methods undefined on `*SceneAccessServer`.

- [ ] **Step 3: Implement the four facade methods**

In `sceneaccess_service.go`, after `LeaveScene`, add (each resolves session + character + dispatch, then calls the plugin client — **no `Evaluate` call**; the `SceneAccessServer` struct has no engine field):

```go
// StartScenePublish starts a publish vote. INV-SCENE-33: no ABAC engine on the
// publish path; the plugin handler self-protects (participant + ended).
func (s *SceneAccessServer) StartScenePublish(ctx context.Context, req *sceneaccessv1.StartScenePublishRequest) (*sceneaccessv1.StartScenePublishResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := s.sceneClient.StartScenePublish(dctx, &scenev1.StartScenePublishRequest{
		CallerCharacterId: char.ID.String(),
		SceneId:           req.GetSceneId(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.StartScenePublishResponse{
		PublishedSceneId: resp.GetPublishedSceneId(),
		AttemptNumber:    resp.GetAttemptNumber(),
	}, nil
}

// CastPublishSceneVote casts/changes the caller's vote. INV-SCENE-33: no engine.
func (s *SceneAccessServer) CastPublishSceneVote(ctx context.Context, req *sceneaccessv1.CastPublishSceneVoteRequest) (*sceneaccessv1.CastPublishSceneVoteResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := s.sceneClient.CastPublishSceneVote(dctx, &scenev1.CastPublishSceneVoteRequest{
		CallerCharacterId: char.ID.String(),
		PublishedSceneId:  req.GetPublishedSceneId(),
		Vote:              req.GetVote(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.CastPublishSceneVoteResponse{IsChange: resp.GetIsChange()}, nil
}

// WithdrawScenePublish aborts the active attempt (owner gate in the plugin).
func (s *SceneAccessServer) WithdrawScenePublish(ctx context.Context, req *sceneaccessv1.WithdrawScenePublishRequest) (*sceneaccessv1.WithdrawScenePublishResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	if _, err := s.sceneClient.WithdrawScenePublish(dctx, &scenev1.WithdrawScenePublishRequest{
		CallerCharacterId: char.ID.String(),
		PublishedSceneId:  req.GetPublishedSceneId(),
	}); err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.WithdrawScenePublishResponse{}, nil
}

// GetPublishedScene reads the active attempt's status + tally (participant-
// gated in the plugin). Trimmed: no frozen content (GetPublicSceneArchive).
func (s *SceneAccessServer) GetPublishedScene(ctx context.Context, req *sceneaccessv1.GetPublishedSceneRequest) (*sceneaccessv1.GetPublishedSceneResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := s.sceneClient.GetPublishedScene(dctx, &scenev1.GetPublishedSceneRequest{
		CallerCharacterId: char.ID.String(),
		PublishedSceneId:  req.GetPublishedSceneId(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.GetPublishedSceneResponse{
		Id:            resp.GetId(),
		SceneId:       resp.GetSceneId(),
		AttemptNumber: resp.GetAttemptNumber(),
		Status:        resp.GetStatus(),
		FailureReason: resp.GetFailureReason(),
		VoteSummary:   resp.GetTally(),
	}, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- -run 'TestSceneAccess(StartScenePublish|GetPublishedScene)' ./internal/grpc/`
Expected: PASS.

- [ ] **Step 5: Format + commit**

```bash
task fmt
jj commit -m "feat(scenes): facade publish methods on SceneAccessServer — no engine (holomush-5rh.24)

Start/Cast/Withdraw + GetPublishedScene (trimmed status+tally) mirror the
EndScene facade shape minus the ABAC engine call (INV-SCENE-33). The server
holds no policy engine; gating stays in the plugin handlers/store.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: Client narrowing interfaces + mock (compile gate)

**Files:**

- Modify: `internal/web/handler.go:108-127` (`SceneAccessClient`)
- Modify: `cmd/holomush/deps.go:146-199` (`GRPCClient`)
- Modify: `cmd/holomush/deps_test.go` (`mockGRPCClient`)

- [ ] **Step 1: Extend `SceneAccessClient`**

In `handler.go`, add to the `SceneAccessClient` interface (after `LeaveScene`):

```go
	StartScenePublish(ctx context.Context, req *sceneaccessv1.StartScenePublishRequest) (*sceneaccessv1.StartScenePublishResponse, error)
	CastPublishSceneVote(ctx context.Context, req *sceneaccessv1.CastPublishSceneVoteRequest) (*sceneaccessv1.CastPublishSceneVoteResponse, error)
	WithdrawScenePublish(ctx context.Context, req *sceneaccessv1.WithdrawScenePublishRequest) (*sceneaccessv1.WithdrawScenePublishResponse, error)
	GetPublishedScene(ctx context.Context, req *sceneaccessv1.GetPublishedSceneRequest) (*sceneaccessv1.GetPublishedSceneResponse, error)
```

- [ ] **Step 2: Extend `GRPCClient`**

In `deps.go`, add the same four method signatures to the `GRPCClient` interface (after `LeaveScene`, before `Close() error`).

- [ ] **Step 3: Extend `mockGRPCClient`**

In `deps_test.go`, add (mirroring the `EndScene` stub at `:218`):

```go
func (m *mockGRPCClient) StartScenePublish(_ context.Context, _ *sceneaccessv1.StartScenePublishRequest) (*sceneaccessv1.StartScenePublishResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) CastPublishSceneVote(_ context.Context, _ *sceneaccessv1.CastPublishSceneVoteRequest) (*sceneaccessv1.CastPublishSceneVoteResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) WithdrawScenePublish(_ context.Context, _ *sceneaccessv1.WithdrawScenePublishRequest) (*sceneaccessv1.WithdrawScenePublishResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) GetPublishedScene(_ context.Context, _ *sceneaccessv1.GetPublishedSceneRequest) (*sceneaccessv1.GetPublishedSceneResponse, error) {
	return nil, nil
}
```

> The concrete connect client to the facade (generated `sceneaccessv1connect` client) already implements these after Task 1's regen, so no other implementor needs editing. The build remains red only on the missing BFF methods (Task 5).

- [ ] **Step 4: Confirm the additions are well-formed (full compile deferred to Task 5)**

`internal/web` does **not** compile standalone yet — it carries
`var _ webv1connect.WebServiceHandler = (*Handler)(nil)` (`handler.go:139`),
which needs the `Web*` handler methods that land in Task 5; `cmd/holomush`
imports `internal/web`, so it is red too. This is the Red-build window — there
is no green package test at this step. Confirm the four interface signatures
match the facade methods and `task fmt` is clean, then rely on Task 5 Step 4's
`task build` to verify the full narrowing compiles.

Run (still red until Task 5): `task build`
Expected: FAIL — `*Handler` missing `WebStartScenePublish`/etc.; closes at Task 5.

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(scenes): narrow publish methods into SceneAccessClient + GRPCClient (holomush-5rh.24)

Adds Start/Cast/Withdraw + GetPublishedScene to the internal/web and
cmd/holomush narrowing interfaces + mockGRPCClient. Generated facade client
satisfies them.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: BFF WebService publish handlers (build green)

**Files:**

- Modify: `internal/web/scene_handlers.go` (add 4 methods)
- Test: `internal/web/scene_handlers_test.go`

- [ ] **Step 1: Write the failing test**

Append to `scene_handlers_test.go`, mirroring `TestWebEndSceneForwardsTokenAndFieldsToFacade:424` verbatim — the `mockSceneAccessClient{}` struct (per-method `xxxReq`/`xxxResp`/`xxxErr` fields, `:33`) and `NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))`:

```go
func TestWebStartScenePublishForwardsTokenAndFieldsToFacade(t *testing.T) {
	sc := &mockSceneAccessClient{
		startScenePublishResp: &sceneaccessv1.StartScenePublishResponse{PublishedSceneId: "att-9", AttemptNumber: 1},
	}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebStartScenePublishRequest{SessionId: "sess-1", CharacterId: "char-1", SceneId: "scene-1"})
	req.Header().Set(headerInjectSessionToken, "tok-abc")

	resp, err := h.WebStartScenePublish(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "att-9", resp.Msg.GetPublishedSceneId())
	require.NotNil(t, sc.startScenePublishReq)
	assert.Equal(t, "tok-abc", sc.startScenePublishReq.GetPlayerSessionToken())
	assert.Equal(t, "scene-1", sc.startScenePublishReq.GetSceneId())
}
```

> Extend the hand-written `mockSceneAccessClient` (`scene_handlers_test.go:33`): add the four `Start/CastPublish/Withdraw/GetPublished` methods (each recording `m.xxxReq = req` and returning `m.xxxResp, m.xxxErr`, mirroring `EndScene` at `:124`) plus the `startScenePublishReq`/`startScenePublishResp`/`startScenePublishErr` (and sibling) fields. This is the same struct the `scene_handlers_logging_test.go` table uses for per-method error pass-through, so add `startScenePublishErr`-style cases there too for the `*PassesStatusErrorThroughAsIs` coverage.

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestWebStartScenePublishForwardsToFacade ./internal/web/`
Expected: FAIL — `WebStartScenePublish` undefined.

- [ ] **Step 3: Implement the four BFF methods**

In `scene_handlers.go`, add (mirroring `WebEndScene:183`):

```go
func (h *Handler) WebStartScenePublish(ctx context.Context, req *connect.Request[webv1.WebStartScenePublishRequest]) (*connect.Response[webv1.WebStartScenePublishResponse], error) {
	slog.DebugContext(ctx, "web: WebStartScenePublish", "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}
	token := req.Header().Get(headerInjectSessionToken)
	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	resp, err := h.sceneAccess.StartScenePublish(rpcCtx, &sceneaccessv1.StartScenePublishRequest{
		SessionId: req.Msg.GetSessionId(), PlayerSessionToken: token,
		CharacterId: req.Msg.GetCharacterId(), SceneId: req.Msg.GetSceneId(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: start scene publish RPC failed", err, "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return connect.NewResponse(&webv1.WebStartScenePublishResponse{
		PublishedSceneId: resp.GetPublishedSceneId(), AttemptNumber: resp.GetAttemptNumber(),
	}), nil
}

func (h *Handler) WebCastPublishSceneVote(ctx context.Context, req *connect.Request[webv1.WebCastPublishSceneVoteRequest]) (*connect.Response[webv1.WebCastPublishSceneVoteResponse], error) {
	slog.DebugContext(ctx, "web: WebCastPublishSceneVote", "session_id", req.Msg.GetSessionId(), "published_scene_id", req.Msg.GetPublishedSceneId())
	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}
	token := req.Header().Get(headerInjectSessionToken)
	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	resp, err := h.sceneAccess.CastPublishSceneVote(rpcCtx, &sceneaccessv1.CastPublishSceneVoteRequest{
		SessionId: req.Msg.GetSessionId(), PlayerSessionToken: token,
		CharacterId: req.Msg.GetCharacterId(), PublishedSceneId: req.Msg.GetPublishedSceneId(), Vote: req.Msg.GetVote(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: cast publish vote RPC failed", err, "session_id", req.Msg.GetSessionId(), "published_scene_id", req.Msg.GetPublishedSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return connect.NewResponse(&webv1.WebCastPublishSceneVoteResponse{IsChange: resp.GetIsChange()}), nil
}

func (h *Handler) WebWithdrawScenePublish(ctx context.Context, req *connect.Request[webv1.WebWithdrawScenePublishRequest]) (*connect.Response[webv1.WebWithdrawScenePublishResponse], error) {
	slog.DebugContext(ctx, "web: WebWithdrawScenePublish", "session_id", req.Msg.GetSessionId(), "published_scene_id", req.Msg.GetPublishedSceneId())
	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}
	token := req.Header().Get(headerInjectSessionToken)
	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	if _, err := h.sceneAccess.WithdrawScenePublish(rpcCtx, &sceneaccessv1.WithdrawScenePublishRequest{
		SessionId: req.Msg.GetSessionId(), PlayerSessionToken: token,
		CharacterId: req.Msg.GetCharacterId(), PublishedSceneId: req.Msg.GetPublishedSceneId(),
	}); err != nil {
		errutil.LogErrorContext(ctx, "web: withdraw scene publish RPC failed", err, "session_id", req.Msg.GetSessionId(), "published_scene_id", req.Msg.GetPublishedSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return connect.NewResponse(&webv1.WebWithdrawScenePublishResponse{}), nil
}

func (h *Handler) WebGetPublishedScene(ctx context.Context, req *connect.Request[webv1.WebGetPublishedSceneRequest]) (*connect.Response[webv1.WebGetPublishedSceneResponse], error) {
	slog.DebugContext(ctx, "web: WebGetPublishedScene", "session_id", req.Msg.GetSessionId(), "published_scene_id", req.Msg.GetPublishedSceneId())
	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}
	token := req.Header().Get(headerInjectSessionToken)
	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	resp, err := h.sceneAccess.GetPublishedScene(rpcCtx, &sceneaccessv1.GetPublishedSceneRequest{
		SessionId: req.Msg.GetSessionId(), PlayerSessionToken: token,
		CharacterId: req.Msg.GetCharacterId(), PublishedSceneId: req.Msg.GetPublishedSceneId(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: get published scene RPC failed", err, "session_id", req.Msg.GetSessionId(), "published_scene_id", req.Msg.GetPublishedSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return connect.NewResponse(&webv1.WebGetPublishedSceneResponse{
		Id: resp.GetId(), SceneId: resp.GetSceneId(), AttemptNumber: resp.GetAttemptNumber(),
		Status: resp.GetStatus(), FailureReason: resp.GetFailureReason(), VoteSummary: resp.GetVoteSummary(),
	}), nil
}
```

- [ ] **Step 4: Run tests + verify build is green again**

Run: `task test -- -run TestWebStartScenePublishForwardsToFacade ./internal/web/`
Expected: PASS.
Run: `task build`
Expected: SUCCESS (the red-build window closes here).

- [ ] **Step 5: Format + commit**

```bash
task fmt
jj commit -m "feat(scenes): BFF WebService publish handlers — build green (holomush-5rh.24)

WebStart/Cast/Withdraw + WebGetPublishedScene proxy to the facade with the
cookie-injected session token. Closes the proto red-build window.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: Web client wrappers + publish flow + live reducer

**Files:**

- Modify: `web/src/lib/scenes/client.ts`
- Create: `web/src/lib/scenes/publishFlow.ts`
- Modify: `web/src/lib/scenes/workspaceStore.svelte.ts`, `web/src/lib/scenes/types.ts`
- Test: `web/src/lib/scenes/publishFlow.test.ts` (new)

- [ ] **Step 1: Add client wrappers**

In `client.ts`, add the import types to the existing `web_pb` import block:

```typescript
	type WebStartScenePublishRequest,
	type WebCastPublishSceneVoteRequest,
	type WebWithdrawScenePublishRequest,
```

Then append the wrappers (mirror `endScene`/`getScene` idiom):

```typescript
/** Starts a publish vote on an ended scene. Returns {publishedSceneId, attemptNumber}. */
export async function startScenePublish(
	sessionId: string,
	opts: Pick<WebStartScenePublishRequest, 'characterId' | 'sceneId'>,
) {
	const res = await client.webStartScenePublish({ sessionId, ...opts });
	return { publishedSceneId: res.publishedSceneId, attemptNumber: res.attemptNumber };
}

/** Casts or changes a Yes/No vote on a publish attempt. Returns isChange. */
export async function castPublishSceneVote(
	sessionId: string,
	opts: Pick<WebCastPublishSceneVoteRequest, 'characterId' | 'publishedSceneId' | 'vote'>,
) {
	const res = await client.webCastPublishSceneVote({ sessionId, ...opts });
	return res.isChange;
}

/** Withdraws (owner) the active publish attempt. */
export async function withdrawScenePublish(
	sessionId: string,
	opts: Pick<WebWithdrawScenePublishRequest, 'characterId' | 'publishedSceneId'>,
) {
	await client.webWithdrawScenePublish({ sessionId, ...opts });
}

/** Reads the cold-start status + tally snapshot for a publish attempt (participant-gated). */
export async function getPublishedScene(
	sessionId: string,
	characterId: string,
	publishedSceneId: string,
) {
	const res = await client.webGetPublishedScene({ sessionId, characterId, publishedSceneId });
	return res; // {id, sceneId, attemptNumber, status, failureReason, voteSummary}
}
```

- [ ] **Step 2: Write the failing reducer test**

Create `web/src/lib/scenes/publishFlow.test.ts`:

```typescript
import { describe, it, expect } from 'vitest';
import { reducePublishEvent, emptyPublishState } from './publishFlow';

describe('reducePublishEvent', () => {
	it('initializes tally on scene_publish_started', () => {
		const next = reducePublishEvent(emptyPublishState(), {
			type: 'core-scenes:scene_publish_started',
			metadata: { attempt_id: 'att-1', attempt_number: '1', roster_character_ids: ['a', 'b', 'c'] },
		} as never);
		expect(next?.attemptId).toBe('att-1');
		expect(next?.status).toBe('COLLECTING');
		expect(next?.pending).toBe(3);
	});

	it('moves a pending voter to yes on scene_publish_vote_cast', () => {
		let s = reducePublishEvent(emptyPublishState(), {
			type: 'core-scenes:scene_publish_started',
			metadata: { attempt_id: 'att-1', roster_character_ids: ['a', 'b'] },
		} as never);
		s = reducePublishEvent(s, {
			type: 'core-scenes:scene_publish_vote_cast',
			metadata: { attempt_id: 'att-1', character_id: 'a', vote: 'true' },
		} as never);
		expect(s?.yes).toBe(1);
		expect(s?.pending).toBe(1);
		expect(s?.ballots['a']).toBe('yes');
	});

	it('marks resolved on scene_publish_resolved', () => {
		let s = reducePublishEvent(emptyPublishState(), {
			type: 'core-scenes:scene_publish_started',
			metadata: { attempt_id: 'att-1', roster_character_ids: ['a'] },
		} as never);
		s = reducePublishEvent(s, {
			type: 'core-scenes:scene_publish_resolved',
			metadata: { attempt_id: 'att-1', outcome: 'PUBLISHED', tally_yes: '1', tally_no: '0', tally_pending: '0' },
		} as never);
		expect(s?.status).toBe('PUBLISHED');
	});
});
```

Run: `cd web && pnpm test:unit -- publishFlow`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement `publishFlow.ts`**

Create `web/src/lib/scenes/publishFlow.ts`:

```typescript
import type { GameEvent } from '$lib/connect/holomush/core/v1/core_pb';
import { ensureSession } from './altSessions.svelte';
import { startScenePublish, castPublishSceneVote, withdrawScenePublish, getPublishedScene } from './client';

export type PublishPhase = 'COLLECTING' | 'COOLOFF' | 'PUBLISHED' | 'ATTEMPT_FAILED';

/** Per-scene publish-attempt state derived from the scene_publish_* IC stream. */
export interface PublishState {
	attemptId: string;
	status: PublishPhase;
	attemptNumber: number;
	yes: number;
	no: number;
	pending: number;
	ballots: Record<string, 'yes' | 'no'>;
	roster: string[];
	failureReason: string;
}

export function emptyPublishState(): PublishState | null {
	return null;
}

function meta(ev: GameEvent): Record<string, string> {
	return (ev.metadata as Record<string, string>) ?? {};
}

/**
 * Folds one scene_publish_* event into the per-scene publish state. Returns the
 * next state (or the prior state unchanged for irrelevant event types). This is
 * the live tally engine: started seeds the roster, vote_cast moves a ballot,
 * cooloff/resolved/withdrawn advance the phase.
 */
export function reducePublishEvent(prev: PublishState | null, ev: GameEvent): PublishState | null {
	const m = meta(ev);
	switch (ev.type) {
		case 'core-scenes:scene_publish_started': {
			const roster = (m['roster_character_ids'] as unknown as string[]) ?? [];
			return {
				attemptId: m['attempt_id'] ?? '',
				status: 'COLLECTING',
				attemptNumber: Number(m['attempt_number'] ?? '1'),
				yes: 0,
				no: 0,
				pending: roster.length,
				ballots: {},
				roster,
				failureReason: '',
			};
		}
		case 'core-scenes:scene_publish_vote_cast': {
			if (!prev) return prev;
			const cid = m['character_id'] ?? '';
			const isYes = m['vote'] === 'true' || m['vote'] === 'yes';
			const ballots = { ...prev.ballots };
			const had = ballots[cid];
			ballots[cid] = isYes ? 'yes' : 'no';
			let { yes, no, pending } = prev;
			if (!had) pending -= 1;
			else if (had === 'yes') yes -= 1;
			else no -= 1;
			if (isYes) yes += 1;
			else no += 1;
			return { ...prev, yes, no, pending, ballots };
		}
		case 'core-scenes:scene_publish_cooloff_started':
			return prev ? { ...prev, status: 'COOLOFF' } : prev;
		case 'core-scenes:scene_publish_resolved':
			return prev ? { ...prev, status: (m['outcome'] as PublishPhase) ?? 'ATTEMPT_FAILED', failureReason: m['failure_reason'] ?? '' } : prev;
		case 'core-scenes:scene_publish_withdrawn':
			return prev ? { ...prev, status: 'ATTEMPT_FAILED', failureReason: 'WITHDRAWN' } : prev;
		default:
			return prev;
	}
}

const PUBLISH_EVENT_PREFIX = 'core-scenes:scene_publish_';

/** True for any scene_publish_* event type. */
export function isPublishEvent(ev: GameEvent): boolean {
	return typeof ev.type === 'string' && ev.type.startsWith(PUBLISH_EVENT_PREFIX);
}

/** Flow actions invoked by the panel. Each resolves the alt session internally
 *  (mirroring lifecycleFlow/membershipFlow — the panel passes NO sessionId). */
export async function startPublishAction(a: { characterId: string; sceneId: string }) {
	const sessionId = await ensureSession(a.characterId);
	return startScenePublish(sessionId, { characterId: a.characterId, sceneId: a.sceneId });
}
export async function castVoteAction(a: { characterId: string; publishedSceneId: string; vote: boolean }) {
	const sessionId = await ensureSession(a.characterId);
	return castPublishSceneVote(sessionId, { characterId: a.characterId, publishedSceneId: a.publishedSceneId, vote: a.vote });
}
export async function withdrawAction(a: { characterId: string; publishedSceneId: string }) {
	const sessionId = await ensureSession(a.characterId);
	return withdrawScenePublish(sessionId, { characterId: a.characterId, publishedSceneId: a.publishedSceneId });
}

/** Maps a WebGetPublishedScene snapshot into PublishState. No per-voter ballots
 *  (the snapshot carries only aggregate counts); subsequent live vote_cast
 *  events fill `ballots`. */
export function snapshotToPublishState(snap: {
	id: string;
	status: string;
	attemptNumber: number;
	failureReason: string;
	voteSummary?: { yes: number; no: number; pending: number };
}): PublishState {
	const t = snap.voteSummary ?? { yes: 0, no: 0, pending: 0 };
	return {
		attemptId: snap.id,
		status: (snap.status as PublishPhase) || 'COLLECTING',
		attemptNumber: snap.attemptNumber,
		yes: t.yes,
		no: t.no,
		pending: t.pending,
		ballots: {},
		roster: [],
		failureReason: snap.failureReason ?? '',
	};
}

/** Cold-start: fetch the participant-gated snapshot for an attempt the client
 *  didn't witness start. Best-effort — a non-participant denial or absent
 *  attempt resolves to null (live events fill the gap). Returns the seeded
 *  state so the caller writes it to the store (keeps publishFlow free of a
 *  workspaceStore import → no import cycle). */
export async function loadPublishSnapshot(a: { characterId: string; publishedSceneId: string }): Promise<PublishState | null> {
	try {
		const sessionId = await ensureSession(a.characterId);
		const snap = await getPublishedScene(sessionId, a.characterId, a.publishedSceneId);
		return snapshotToPublishState(snap);
	} catch {
		return null;
	}
}
```

Run: `cd web && pnpm test:unit -- publishFlow`
Expected: PASS.

- [ ] **Step 4: Route publish events into a store map**

In `workspaceStore.svelte.ts`, add a reactive map + getter and route publish events at the top of `ingestEvent` (before `eventFrameToLogEntry`, which returns null for publish types and would drop them):

```typescript
import { reducePublishEvent, isPublishEvent, type PublishState } from './publishFlow';

let publishStateBySceneId = $state<Record<string, PublishState | null>>({});
```

At the start of `ingestEvent`, after computing `sceneId` is unavailable yet — restructure so publish events route by their own `scene_id` metadata. Insert at the very top of `ingestEvent`:

```typescript
	if (isPublishEvent(ev)) {
		const m = ev.metadata as Record<string, unknown> | undefined;
		const sid =
			typeof m?.['scene_id'] === 'string' && m['scene_id'] ? (m['scene_id'] as string) : null;
		if (sid) {
			publishStateBySceneId = {
				...publishStateBySceneId,
				[sid]: reducePublishEvent(publishStateBySceneId[sid] ?? null, ev),
			};
		}
		return;
	}
```

Add a cold-start seed function (define near `applySceneInfo`) — it must NOT clobber live state already accumulated from events:

```typescript
function seedPublishState(sceneId: string, state: PublishState): void {
	if (publishStateBySceneId[sceneId]) return; // live events win over a stale snapshot
	publishStateBySceneId = { ...publishStateBySceneId, [sceneId]: state };
}
```

Add the getter + `seedPublishState` to the exported store object (after `unreadBySceneId`) and the `export { ... }` list:

```typescript
	get publishStateBySceneId() {
		return publishStateBySceneId;
	},
	seedPublishState,
```

> **Grounding caveat (verify at implementation):** `reducePublishEvent` reads payload fields from `ev.metadata` (`attempt_id`, `scene_id`, `character_id`, `vote`, `roster_character_ids`, `attempt_number`, `outcome`, `failure_reason`). The existing `eventFrameToLogEntry` proves `ev.metadata` is a populated string map (it reads `actor_id`/`text`/`scene_id`), but the exact metadata keys for `scene_publish_*` events depend on how the host translates the plugin event payloads (`plugins/core-scenes/publish_events.go` structs) into `GameEvent.metadata`. Before implementing, confirm the wire metadata keys by inspecting one real `scene_publish_started`/`scene_publish_vote_cast` frame (or the host translation in `internal/plugin`/`internal/eventbus`), and adjust the key strings + the `vote`/`roster_character_ids` decoding (string vs JSON-encoded array) to match. The reducer's value-coercion (`'true'`/`'yes'`, `Number(...)`) is written defensively for string metadata; tighten it once the real shape is confirmed.

- [ ] **Step 5: Thread the new SceneInfo fields into WorkspaceScene**

In `types.ts`, add to the `WorkspaceScene` interface:

```typescript
	activePublishAttemptId?: string;
	publishStatus?: string;
```

In `applySceneInfo` (`workspaceStore.svelte.ts:270`), inside the `apply` loop after `s.locationId = scene.locationId;`:

```typescript
			s.activePublishAttemptId = scene.activePublishAttemptId;
			s.publishStatus = scene.publishStatus;
```

(Also set these wherever `WorkspaceScene` is first built from a `SceneInfo` in `refresh`/`select` — grep for the `sceneId:` mapping in `workspaceStore.svelte.ts` and add the two fields in the same place.)

- [ ] **Step 6: Run web unit tests**

Run: `cd web && pnpm test:unit -- publishFlow workspaceStore`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /Volumes/Code/github.com/holomush/.worktrees/scenes-publish-design
jj commit -m "feat(web): publish client wrappers + flow + live tally reducer (holomush-5rh.24)

client.ts publish wrappers; publishFlow.ts reduces scene_publish_* IC events
into per-scene tally/phase; workspaceStore routes publish events + threads the
new SceneInfo publish pointer into WorkspaceScene.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 7: Publish panel UI (layout C) on the rail

**Files:**

- Create: `web/src/lib/components/scenes/ScenePublishPanel.svelte`
- Modify: `web/src/lib/components/scenes/SceneContextRail.svelte`
- Test: `web/src/lib/components/scenes/ScenePublishPanel.svelte.test.ts` (new)

- [ ] **Step 1: Write the failing component test**

Create `ScenePublishPanel.svelte.test.ts` (raw `svelte` mount + jsdom queries, mirroring `SceneContextRail.svelte.test.ts`):

```typescript
import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount } from 'svelte';
import ScenePublishPanel from './ScenePublishPanel.svelte';
import type { PublishState } from '$lib/scenes/publishFlow';

vi.mock('$lib/scenes/publishFlow', async (orig) => ({
	...(await orig<typeof import('$lib/scenes/publishFlow')>()),
	startPublishAction: vi.fn(),
	castVoteAction: vi.fn(),
	withdrawAction: vi.fn(),
	loadPublishSnapshot: vi.fn(async () => null),
}));

function render(props: Record<string, unknown>): HTMLElement {
	const target = document.createElement('div');
	document.body.appendChild(target);
	mount(ScenePublishPanel, { target, props });
	return target;
}

afterEach(() => document.body.replaceChildren());

const collecting: PublishState = {
	attemptId: 'att-1', status: 'COLLECTING', attemptNumber: 1,
	yes: 2, no: 0, pending: 3, ballots: {}, roster: ['a', 'b', 'c', 'd', 'e'], failureReason: '',
};

describe('ScenePublishPanel', () => {
	it('shows Start when ended with no active attempt and caller is participant', () => {
		const t = render({ sceneState: 'ended', isParticipant: true, publishState: null, characterId: 'a', sceneId: 'sc' });
		expect([...t.querySelectorAll('button')].some((b) => /start publish vote/i.test(b.textContent ?? ''))).toBe(true);
	});

	it('renders Yes/No/Pending tiles during COLLECTING', () => {
		const t = render({ sceneState: 'ended', isParticipant: true, publishState: collecting, characterId: 'a', sceneId: 'sc' });
		expect(t.textContent).toContain('2');
		expect(t.textContent).toContain('3');
		expect([...t.querySelectorAll('button')].some((b) => /vote yes/i.test(b.textContent ?? ''))).toBe(true);
	});

	it('hides vote controls for a non-participant observer', () => {
		const t = render({ sceneState: 'ended', isParticipant: false, publishState: collecting, characterId: 'z', sceneId: 'sc' });
		expect([...t.querySelectorAll('button')].some((b) => /vote yes/i.test(b.textContent ?? ''))).toBe(false);
	});
});
```

Run: `cd web && pnpm test:unit -- ScenePublishPanel`
Expected: FAIL — component not found.

- [ ] **Step 2: Implement `ScenePublishPanel.svelte` (layout C)**

Create `web/src/lib/components/scenes/ScenePublishPanel.svelte`:

```svelte
<script lang="ts">
  import { Button } from '$lib/components/ui/button/index.js';
  import { startPublishAction, castVoteAction, withdrawAction, loadPublishSnapshot, type PublishState } from '$lib/scenes/publishFlow';
  import { workspaceStore } from '$lib/scenes/workspaceStore.svelte';

  let {
    sceneState,
    isParticipant,
    isOwner = false,
    publishState,
    activeAttemptId = '',
    characterId,
    sceneId,
  }: {
    sceneState: string;
    isParticipant: boolean;
    isOwner?: boolean;
    publishState: PublishState | null;
    activeAttemptId?: string;
    characterId: string;
    sceneId: string;
  } = $props();

  let err = $state('');
  let showVoters = $state(false);

  let active = $derived(publishState && (publishState.status === 'COLLECTING' || publishState.status === 'COOLOFF'));
  let myBallot = $derived(publishState?.ballots[characterId]);

  // Cold-start: SceneInfo reports an active attempt but we have no live state
  // (opened the scene mid-vote, missed scene_publish_started). Fetch the
  // participant-gated snapshot once and seed the store; live events take over.
  $effect(() => {
    if (activeAttemptId && !publishState) {
      void loadPublishSnapshot({ characterId, publishedSceneId: activeAttemptId }).then((s) => {
        if (s) workspaceStore.seedPublishState(sceneId, s);
      });
    }
  });

  async function run(fn: () => Promise<unknown>) {
    err = '';
    try { await fn(); } catch (e) { err = e instanceof Error ? e.message : 'Action failed'; }
  }
</script>

<section class="p-4 pb-3" aria-label="Publish vote">
  <h2 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">Publish</h2>

  {#if active && publishState}
    <div class="flex gap-2 mb-3">
      <div class="flex-1 text-center rounded-md border border-border bg-muted/30 py-2">
        <div class="text-lg font-bold text-green-500">{publishState.yes}</div>
        <div class="text-[9px] uppercase tracking-wide text-muted-foreground">Yes</div>
      </div>
      <div class="flex-1 text-center rounded-md border border-border bg-muted/30 py-2">
        <div class="text-lg font-bold text-red-500">{publishState.no}</div>
        <div class="text-[9px] uppercase tracking-wide text-muted-foreground">No</div>
      </div>
      <div class="flex-1 text-center rounded-md border border-border bg-muted/30 py-2">
        <div class="text-lg font-bold text-muted-foreground">{publishState.pending}</div>
        <div class="text-[9px] uppercase tracking-wide text-muted-foreground">Pending</div>
      </div>
    </div>
    <p class="text-[11px] text-muted-foreground mb-2">{publishState.status === 'COOLOFF' ? 'Cooling off before publish…' : `Vote in progress · attempt #${publishState.attemptNumber}`}</p>

    {#if isParticipant && publishState.status === 'COLLECTING'}
      <div class="flex gap-2">
        <Button variant={myBallot === 'yes' ? 'default' : 'outline'} size="sm" class="flex-1 h-7 text-xs"
          onclick={() => run(() => castVoteAction({ characterId, publishedSceneId: publishState.attemptId, vote: true }))}>Vote Yes</Button>
        <Button variant={myBallot === 'no' ? 'default' : 'outline'} size="sm" class="flex-1 h-7 text-xs"
          onclick={() => run(() => castVoteAction({ characterId, publishedSceneId: publishState.attemptId, vote: false }))}>Vote No</Button>
      </div>
    {/if}

    {#if Object.keys(publishState.ballots).length > 0}
      <button class="text-[11px] text-primary mt-2" onclick={() => (showVoters = !showVoters)}>
        {showVoters ? '▴ hide voters' : `▾ show ${Object.keys(publishState.ballots).length} voters`}
      </button>
      {#if showVoters}
        <ul class="mt-1 space-y-0.5">
          {#each Object.entries(publishState.ballots) as [cid, b] (cid)}
            <li class="flex justify-between text-xs"><span class="truncate">{cid}</span><span class={b === 'yes' ? 'text-green-500' : 'text-red-500'}>{b}</span></li>
          {/each}
        </ul>
      {/if}
    {/if}

    {#if isOwner}
      <button class="text-[11px] text-muted-foreground mt-2 block"
        onclick={() => run(() => withdrawAction({ characterId, publishedSceneId: publishState.attemptId }))}>⋯ Withdraw</button>
    {/if}
  {:else if publishState && publishState.status === 'PUBLISHED'}
    <p class="text-xs text-green-500">Published ✓</p>
  {:else if publishState && publishState.status === 'ATTEMPT_FAILED'}
    <p class="text-xs text-muted-foreground">Vote failed{publishState.failureReason ? ` — ${publishState.failureReason}` : ''}.</p>
    {#if isParticipant && sceneState === 'ended'}
      <Button variant="outline" size="sm" class="h-7 text-xs mt-2"
        onclick={() => run(() => startPublishAction({ characterId, sceneId }))}>Start another</Button>
    {/if}
  {:else if isParticipant && sceneState === 'ended'}
    <Button variant="outline" size="sm" class="w-full h-7 text-xs"
      onclick={() => run(() => startPublishAction({ characterId, sceneId }))}>Start publish vote</Button>
  {:else}
    <p class="text-xs text-muted-foreground italic">No active publish vote.</p>
  {/if}

  {#if err}<p class="text-xs text-destructive pt-1">{err}</p>{/if}
</section>
```

Run: `cd web && pnpm test:unit -- ScenePublishPanel`
Expected: PASS.

- [ ] **Step 3: Mount the panel on the rail**

In `SceneContextRail.svelte`, add the import:

```svelte
  import ScenePublishPanel from './ScenePublishPanel.svelte';
  import { workspaceStore } from '$lib/scenes/workspaceStore.svelte';
```

After the Roster `</section>` + its `<Separator />`, add (the panel is shown for ended scenes, or whenever an attempt exists):

```svelte
  {#if scene && (scene.state === 'ended' || workspaceStore.publishStateBySceneId[scene.sceneId])}
    <Separator />
    <ScenePublishPanel
      sceneState={scene.state}
      isParticipant={scene.role === 'owner' || scene.role === 'member'}
      isOwner={scene.ownerId === scene.asCharacterId}
      publishState={workspaceStore.publishStateBySceneId[scene.sceneId] ?? null}
      activeAttemptId={scene.activePublishAttemptId ?? ''}
      characterId={scene.asCharacterId}
      sceneId={scene.sceneId}
    />
  {/if}
```

> The panel takes **no** `sessionId` prop — its flow actions resolve the alt session internally via `ensureSession(characterId)` (mirroring `lifecycleFlow`). `scene.activePublishAttemptId` is the `SceneInfo` field threaded into `WorkspaceScene` in Task 6 Step 5.

- [ ] **Step 4: Run web checks**

Run: `cd web && pnpm test:unit -- ScenePublishPanel SceneContextRail && pnpm check`
Expected: PASS (tests + svelte-check clean).

- [ ] **Step 5: Commit**

```bash
cd /Volumes/Code/github.com/holomush/.worktrees/scenes-publish-design
jj commit -m "feat(web): scene publish panel (layout C) on the context rail (holomush-5rh.24)

Tiles + Yes/No + per-voter expander + owner Withdraw; renders all attempt
states off publishStateBySceneId. Observers see status/tally, no controls.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 8: Telnet-free E2E

**Files:**

- Create: `web/e2e/scenes-publish.spec.ts`

- [ ] **Step 1: Write the E2E spec**

Create `web/e2e/scenes-publish.spec.ts` (prefix `wpv`, mirroring `scenes-lifecycle.spec.ts`). A single-character vote auto-resolves (roster of one → unanimous yes publishes):

```typescript
import { test, expect, db, registerAndEnterTerminal } from './helpers/fixtures';

test.describe('Scene publish vote via web GUI (wpv)', () => {
  test('participant starts a publish vote and casts yes from the web GUI with no telnet', async ({ page }) => {
    await registerAndEnterTerminal(page, 'wpv');

    await page.goto('/scenes');
    await expect(page.locator('[data-testid="scenes-workspace"]')).toBeVisible({ timeout: 15000 });

    // Create + end a scene so it is publishable.
    await page.getByRole('button', { name: /new scene/i }).first().click();
    const titleInput = page.locator('input[name="title"]');
    await expect(titleInput).toBeVisible({ timeout: 10000 });
    const sceneTitle = `WPV Publish ${Date.now()}`;
    await titleInput.fill(sceneTitle);
    await page.getByRole('button', { name: /create scene/i }).click();
    await expect(page.locator('.font-semibold').filter({ hasText: sceneTitle })).toBeVisible({ timeout: 15000 });

    const scene = await db.getSceneByTitle(sceneTitle);
    expect(scene).not.toBeNull();
    const sceneId = scene!.id;

    await page.getByRole('button', { name: /^End$/ }).click();
    await expect.poll(async () => (await db.getSceneById(sceneId))?.state, { timeout: 15000 }).toBe('ended');

    // Start the publish vote from the panel.
    await page.getByRole('button', { name: /start publish vote/i }).click();
    await expect.poll(async () => await db.countPublishAttempts(sceneId), { timeout: 15000 }).toBeGreaterThan(0);

    // The tally tiles + vote buttons appear (live via the IC stream).
    await expect(page.getByRole('button', { name: /vote yes/i })).toBeVisible({ timeout: 15000 });
    await page.getByRole('button', { name: /vote yes/i }).click();

    // Single-roster unanimous yes resolves to PUBLISHED.
    await expect.poll(async () => await db.getLatestPublishStatus(sceneId), { timeout: 20000 }).toBe('PUBLISHED');
  });
});
```

> Add the helpers `db.countPublishAttempts(sceneId)` and `db.getLatestPublishStatus(sceneId)` to `web/e2e/helpers/fixtures.ts` (query `published_scenes` by `scene_id`, `ORDER BY attempt_number DESC LIMIT 1`), mirroring the existing `db.getSceneById` query. If the publish vote-window default makes resolution slow, assert on the tally tiles (`yes = 1`) instead of `PUBLISHED` and leave resolution to the backend's own tests.

- [ ] **Step 2: Run the E2E**

Run: `cd web && pnpm test:e2e -- scenes-publish`
Expected: PASS (requires the Docker e2e stack; `task test:e2e` from repo root brings it up).

- [ ] **Step 3: Commit**

```bash
cd /Volumes/Code/github.com/holomush/.worktrees/scenes-publish-design
jj commit -m "test(scenes): telnet-free E2E for web publish vote (holomush-5rh.24)

Drives create→end→start publish→vote yes→PUBLISHED entirely from the GUI.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Post-Implementation Checklist

- [ ] `task pr-prep` green (fast lane: lint/fmt/unit/build/bats).
- [ ] `task pr-prep:full` (int + E2E) — this slice touches E2E + web; run the full lane.
- [ ] `abac-reviewer` READY — confirm (a) no engine on any publish facade/handler path, (b) the `active_publish_attempt_id` pointer's non-participant visibility on open scenes is acceptable, (c) observer visibility of per-voter ballots (`ScenePublishVoteCastEvent.vote`) surfaced in the panel is intended.
- [ ] Generated stubs committed in the same change as the proto edit (Task 1).
- [ ] No new registry invariants (respects INV-SCENE-33/60/61; no `// Verifies:` owed).
- [ ] `task fmt:check` clean per-bead (gofumpt + rumdl).
<!-- adr-capture: sha256=c163c671e6160bc3; session=cli; ts=2026-06-28T20:07:08Z; adrs=holomush-o8gx8 -->
