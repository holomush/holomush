<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Scene Character Name Resolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render character display names (not raw ULIDs) on the three web scene surfaces — roster, pose-order strip, and pose author.

**Architecture:** Host-side resolution. The scene **facade** (`GetSceneForViewer`, `internal/grpc`) resolves roster names via the existing in-process `characterNameResolver` after the scene gate has already authorized the roster — mirroring `ListFocusPresence`. The **pose author** rides the display name the host command dispatcher already puts on every `CommandRequest` (`CharacterName`), which `handleEmit` stamps into the IC payload; the gateway already reads it. No new host capability, no new ABAC, no client changes.

**Tech Stack:** Go, ConnectRPC/gRPC, testify + mockery (unit), Ginkgo/Gomega + testcontainers (integration). Design spec: `docs/superpowers/specs/2026-06-20-scene-character-name-resolution-design.md`.

---

## File Structure

| File | Responsibility | Change |
|------|----------------|--------|
| `internal/grpc/sceneaccess_service.go` | Scene facade — add resolver field, `WithCharacterNameResolver` method, roster resolution in `GetSceneForViewer` | Modify |
| `cmd/holomush/sub_grpc.go` | Production wiring — attach the resolver to the facade | Modify |
| `internal/grpc/sceneaccess_service_test.go` | Facade unit tests (fake resolver) | Modify |
| `plugins/core-scenes/commands.go` | `handleEmit` — add `character_name` to the IC payload | Modify |
| `plugins/core-scenes/commands_emit_test.go` | Plugin emit unit test | Modify |
| `internal/web/translate_test.go` | Gateway regression-lock: scene IC `character_name` → `GameEvent.actor` | Modify |
| `test/integration/scenes/scene_name_resolution_test.go` | End-to-end: cross-location roster names + pose author | Create |

---

### Task 1: Inject `characterNameResolver` into the scene facade

Pure plumbing — adds the dependency with no behavior change yet. Mirrors the existing post-construction `WithSceneDEKAdder` method (`sceneaccess_service.go`) and the `characterNameResolver` already wired into `CoreServer` (`sub_grpc.go:526`).

**Files:**

- Modify: `internal/grpc/sceneaccess_service.go` (struct `SceneAccessServer:47-63`; add method)
- Modify: `cmd/holomush/sub_grpc.go:581-597` (construction site)
- Test: `internal/grpc/sceneaccess_service_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/grpc/sceneaccess_service_test.go`:

```go
func TestWithCharacterNameResolverSetsTheField(t *testing.T) {
	srv := &SceneAccessServer{}
	r := &fakeNameResolver{}
	srv.WithCharacterNameResolver(r)
	assert.Same(t, r, srv.characterNameResolver)
}
```

Also add the fake (used here and in Task 2), near the top of the test file:

```go
// fakeNameResolver is a hand-rolled double for the unexported
// characterNameResolver interface (mockery does not generate mocks for
// unexported interfaces).
type fakeNameResolver struct {
	names map[ulid.ULID]string
	err   error
}

func (f *fakeNameResolver) Names(_ context.Context, ids []ulid.ULID) (map[ulid.ULID]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[ulid.ULID]string, len(ids))
	for _, id := range ids {
		if n, ok := f.names[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}
```

Ensure the test file imports `"github.com/oklog/ulid/v2"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestWithCharacterNameResolverSetsTheField ./internal/grpc/`
Expected: FAIL — `srv.characterNameResolver` undefined and `WithCharacterNameResolver` undefined (compile error).

- [ ] **Step 3: Add the field and method**

In `internal/grpc/sceneaccess_service.go`, add a field to the `SceneAccessServer` struct (after `dekAdder`):

```go
	// characterNameResolver resolves participant/observer display names by ID
	// for GetSceneForViewer roster enrichment. Optional: nil leaves rosters
	// with raw ULIDs (best-effort). Mirrors CoreServer's resolver (5b2j).
	characterNameResolver characterNameResolver
```

Add the method next to `WithSceneDEKAdder`:

```go
// WithCharacterNameResolver attaches the roster name resolver. Call after
// construction; when set, GetSceneForViewer overwrites ParticipantInfo
// CharacterName with the resolved display name (ULID fallback on a miss).
func (s *SceneAccessServer) WithCharacterNameResolver(r characterNameResolver) {
	s.characterNameResolver = r
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestWithCharacterNameResolverSetsTheField ./internal/grpc/`
Expected: PASS

- [ ] **Step 5: Wire it in production**

In `cmd/holomush/sub_grpc.go`, add the call **unconditionally**, after the `if s.cfg.RekeyManager != nil { saSrv.WithSceneDEKAdder(...) }` block (lines 594-596) and **before** `sceneAccessSrv = saSrv` (line 597). It MUST NOT go inside the `RekeyManager` guard — name resolution is independent of crypto and must run in KEK-less deployments:

```go
		if s.cfg.RekeyManager != nil {
			saSrv.WithSceneDEKAdder(s.cfg.RekeyManager)
		}
		saSrv.WithCharacterNameResolver(holoGRPC.NewRepoCharacterNameResolver(charRepo))
		sceneAccessSrv = saSrv
```

`charRepo` (the `world.CharacterRepository` from `worldpostgres.NewCharacterRepository(pool)`, `sub_grpc.go:312`) is already in scope — it is the same repo passed to the CoreServer resolver at line 526.

- [ ] **Step 6: Build + commit**

Run: `task build`
Expected: success.
Commit using VCS-appropriate commands per `references/vcs-preamble.md` (message: `feat(scenes): inject character name resolver into scene facade (holomush-5rh.25)`).

---

### Task 2: Resolve roster names in `GetSceneForViewer`

After the plugin returns the (already gated) roster, overwrite each `CharacterName` with the resolved display name, keeping the ULID on a miss. Resolves participants **and** observers (INV-SCENE-61).

**Files:**

- Modify: `internal/grpc/sceneaccess_service.go:180-203` (`GetSceneForViewer`; add a `resolveRosterNames` helper)
- Test: `internal/grpc/sceneaccess_service_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/grpc/sceneaccess_service_test.go`. This mirrors the mock setup in `TestSceneAccessOverridesClientSuppliedCharacterWithOwnedAlt` (same file, ~line 162) for the gate (`buildSATestPS`, `buildSASessionRepo`, `playerRepo.GetByID`, `charRepo.ListByPlayer`); it adds a `GetScene` expectation returning a ULID-stubbed roster and asserts resolution.

```go
func TestGetSceneForViewerResolvesRosterNames(t *testing.T) {
	ctx := context.Background()

	playerID := idgen.New()
	viewer := &world.Character{ID: idgen.New(), PlayerID: playerID, Name: "Viewer"}
	ps := buildSATestPS(t, playerID)

	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).
		Return(&auth.Player{ID: playerID, IsGuest: false}, nil).Maybe()
	psRepo := buildSASessionRepo(t, ps)
	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{viewer}, nil).Maybe()
	sessStore := sessionmocks.NewMockStore(t)
	mgr := &stubPluginManager{}

	ownerID := idgen.New()   // resolves
	memberID := idgen.New()  // resolves
	missingID := idgen.New() // NOT in the resolver map → ULID fallback
	observerID := idgen.New()

	sceneMock := scenemocks.NewMockSceneServiceClient(t)
	sceneMock.EXPECT().GetScene(mock.Anything, mock.Anything).Return(&scenev1.GetSceneResponse{
		Scene: &scenev1.SceneInfo{
			Id: "sc-1",
			Participants: []*scenev1.ParticipantInfo{
				{CharacterId: ownerID.String(), CharacterName: ownerID.String(), Role: "owner"},
				{CharacterId: missingID.String(), CharacterName: missingID.String(), Role: "member"},
			},
			Observers: []*scenev1.ParticipantInfo{
				{CharacterId: observerID.String(), CharacterName: observerID.String(), Role: "observer"},
			},
		},
	}, nil).Maybe()

	srv := newTestSceneAccessServer(t, psRepo, playerRepo, charRepo, sessStore, &stubFocusCoordinator{}, sceneMock, mgr)
	srv.WithCharacterNameResolver(&fakeNameResolver{names: map[ulid.ULID]string{
		ownerID:    "Owner One",
		memberID:   "Member One",
		observerID: "Observer One",
		// missingID intentionally absent
	}})

	resp, err := srv.GetSceneForViewer(ctx, &sceneaccessv1.GetSceneForViewerRequest{
		SessionId:          "ignored",
		PlayerSessionToken: testSAToken,
		CharacterId:        viewer.ID.String(),
		SceneId:            "sc-1",
	})
	require.NoError(t, err)

	parts := resp.GetScene().GetParticipants()
	require.Len(t, parts, 2)
	assert.Equal(t, "Owner One", parts[0].GetCharacterName())
	assert.Equal(t, missingID.String(), parts[1].GetCharacterName(), "unresolved id keeps the ULID")
	obs := resp.GetScene().GetObservers()
	require.Len(t, obs, 1)
	assert.Equal(t, "Observer One", obs[0].GetCharacterName())
}
```

> If `GetSceneForViewer`'s `beginDispatch` requires additional mock expectations to reach the `GetScene` call, copy them from the nearest existing happy-path facade test in this file (the gate scaffolding is shared). The assertions above are the behavior under test.

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestGetSceneForViewerResolvesRosterNames ./internal/grpc/`
Expected: FAIL — names are still the ULIDs (`parts[0].GetCharacterName()` == ownerID string, not `"Owner One"`).

- [ ] **Step 3: Implement resolution**

In `internal/grpc/sceneaccess_service.go`, change the tail of `GetSceneForViewer` (currently `return &sceneaccessv1.GetSceneForViewerResponse{Scene: resp.GetScene()}, nil`) to resolve first:

```go
	scene := resp.GetScene()
	s.resolveRosterNames(ctx, scene)
	return &sceneaccessv1.GetSceneForViewerResponse{Scene: scene}, nil
```

Add the helper (same file). It is best-effort: a nil resolver or a resolver error leaves the ULIDs in place and never fails the RPC (mirrors `ListFocusPresence` graceful degradation, but keeps the ULID rather than skipping).

```go
// resolveRosterNames overwrites participant + observer CharacterName fields with
// resolved display names, in place. Best-effort: nil resolver, parse failure, a
// resolver error, or a missing id all leave the raw ULID. The scene gate already
// authorized this roster (plugin GetScene privacy gate), so name resolution is
// downstream display — no per-character ABAC (mirrors ListFocusPresence).
func (s *SceneAccessServer) resolveRosterNames(ctx context.Context, scene *scenev1.SceneInfo) {
	if s.characterNameResolver == nil || scene == nil {
		return
	}
	roster := append(append([]*scenev1.ParticipantInfo{}, scene.GetParticipants()...), scene.GetObservers()...)
	if len(roster) == 0 {
		return
	}
	ids := make([]ulid.ULID, 0, len(roster))
	for _, p := range roster {
		id, err := ulid.Parse(p.GetCharacterId())
		if err != nil {
			continue // leave unparseable ids as-is
		}
		ids = append(ids, id)
	}
	names, err := s.characterNameResolver.Names(ctx, ids)
	if err != nil {
		// Log via slog.ErrorContext, as ListFocusPresence does at
		// list_focus_presence.go:161-162. DELIBERATE DIVERGENCE: presence
		// HARD-FAILS on a resolver error (returns INTERNAL, line 164); the
		// scene roster instead DEGRADES — keep the ULIDs and return the scene,
		// never failing GetSceneForViewer on a name miss (spec G3, best-effort).
		slog.ErrorContext(ctx, "scene roster name resolution failed", "error", err, "scene_id", scene.GetId())
		return // keep ULIDs
	}
	for _, p := range roster {
		id, err := ulid.Parse(p.GetCharacterId())
		if err != nil {
			continue
		}
		if n, ok := names[id]; ok && n != "" {
			p.CharacterName = n
		}
	}
}
```

Confirm imports in `sceneaccess_service.go` include `"github.com/oklog/ulid/v2"` and `"log/slog"` (already used in the file, e.g. `slog.ErrorContext` at the `WatchScene`/`GetSceneForViewer` error paths); add `ulid` if missing.

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestGetSceneForViewerResolvesRosterNames ./internal/grpc/`
Expected: PASS

- [ ] **Step 5: Run the package + commit**

Run: `task test -- ./internal/grpc/`
Expected: PASS (no regressions).
Commit (message: `feat(scenes): resolve roster display names in GetSceneForViewer (holomush-5rh.25)`).

---

### Task 3: Stamp the pose author name at emit (`handleEmit`)

`handleEmit` already holds the author's display name as `req.CharacterName` (the dispatcher sets it at `internal/command/dispatcher.go:310`). Add it to the IC payload; the gateway's `translateEvent` already reads `character_name` (`internal/web/translate.go:82`).

**Files:**

- Modify: `plugins/core-scenes/commands.go:1310-1314` (`handleEmit` payload marshal)
- Test: `plugins/core-scenes/commands_emit_test.go`
- Test: `internal/web/translate_test.go`

- [ ] **Step 1: Write the failing plugin test**

Add to `plugins/core-scenes/commands_emit_test.go` (mirrors `TestSceneSubcommand_Pose_EmitsSceneEventOnICFacet`):

```go
func TestSceneSubcommand_Pose_IncludesAuthorCharacterName(t *testing.T) {
	t.Parallel()
	p, sink := newTestPluginWithMember(t, "scene-author-test")

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:       "scene",
		Args:          "pose smiles",
		CharacterID:   "char-alice",
		CharacterName: "Alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)

	found := findIntentByType(sink.intents, "core-scenes:scene_pose")
	require.NotNil(t, found)
	assert.Contains(t, found.Payload, `"character_name":"Alice"`)
}

func TestSceneSubcommand_Pose_OmitsCharacterNameWhenEmpty(t *testing.T) {
	t.Parallel()
	p, sink := newTestPluginWithMember(t, "scene-noauthor-test")

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "pose smiles",
		CharacterID: "char-alice",
		// CharacterName intentionally empty
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)

	found := findIntentByType(sink.intents, "core-scenes:scene_pose")
	require.NotNil(t, found)
	assert.NotContains(t, found.Payload, "character_name")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run 'TestSceneSubcommand_Pose_IncludesAuthorCharacterName' ./plugins/core-scenes/`
Expected: FAIL — payload has no `character_name`.

- [ ] **Step 3: Implement the payload change**

In `plugins/core-scenes/commands.go`, replace the `payload, err := json.Marshal(map[string]string{...})` block at `handleEmit` (lines 1310-1314) with a conditional build:

```go
	payloadMap := map[string]string{
		"actor_id": req.CharacterID,
		"scene_id": sceneID,
		"text":     text,
	}
	// Stamp the author display name when the dispatcher provided one
	// (internal/command/dispatcher.go:310). Empty → omit; the gateway falls
	// back to actor_id exactly as before. Rides the encrypted IC payload
	// (crypto.emits) — no more sensitive than the pose text it labels.
	if req.CharacterName != "" {
		payloadMap["character_name"] = req.CharacterName
	}
	payload, err := json.Marshal(payloadMap)
```

`handleEmit` is the **shared** content-emit handler for all four IC verbs (`scene_pose` / `scene_say` / `scene_ooc` / `scene_emit`), so this single change carries `character_name` for OOC too (spec §8 R3) — `PoseCard` renders the OOC author from `actorName` at `PoseCard.svelte:64`.

- [ ] **Step 4: Run plugin tests to verify they pass**

Run: `task test -- -run 'TestSceneSubcommand_Pose' ./plugins/core-scenes/`
Expected: PASS (both new tests + the existing pose tests).

- [ ] **Step 5: Add the gateway regression-lock test**

`translateEvent` already extracts `actor` from `character_name`; this test pins the scene-IC end of the contract. Add to `internal/web/translate_test.go`. **Note:** `core-scenes:scene_pose` is NOT in the file's `testRenderings` map (line ~42), so `withRendering(ev)` would yield a nil `Rendering` and `translateEvent` drops nil-Rendering frames (INV-EVENTBUS-6) — the test must **inline** `Rendering` directly (the pattern at `translate_test.go:~387`) and use `newTestHandler(t)` (the file idiom):

```go
func TestTranslateEvent_ScenePoseUsesCharacterNameAsActor(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "core-scenes:scene_pose",
		ActorId: "01HYXCHARALICE0000000000AA",
		Payload: mustMarshal(t, map[string]string{"character_name": "Alice", "text": "smiles"}),
		// Inline (not withRendering): scene_pose is absent from testRenderings.
		// Any non-"state" category takes the generic-payload path that reads
		// character_name.
		Rendering: &corev1.RenderingMetadata{
			Category:      "communication",
			Format:        "action",
			DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
			SourcePlugin:  "core-scenes",
		},
	}
	got := h.translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, "Alice", got.GetActor())
}
```

- [ ] **Step 6: Run the gateway test + commit**

Run: `task test -- -run TestTranslateEvent_ScenePoseUsesCharacterNameAsActor ./internal/web/`
Expected: PASS.
Commit (message: `feat(scenes): stamp pose author display name into IC payload (holomush-5rh.25)`).

> **Review gate:** this change touches the `crypto.emits` IC payload shape — the `crypto-reviewer` agent MUST run before push (per root `CLAUDE.md`). Note it on the bead.

---

### Task 4: Integration test — cross-location roster + pose author

Proves the fix end-to-end through the real stack: two participants in **different locations** still get resolved roster names (the co-location-independence that distinguishes this fix from the rejected ABAC-gated approach), and a posted pose surfaces the author's name.

**Files:**

- Create: `test/integration/scenes/scene_name_resolution_test.go`

- [ ] **Step 1: Write the Ginkgo spec**

Follow the harness usage in the sibling specs `test/integration/scenes/real_scene_join_subscription_test.go` and `test/integration/scenes/scene_command_join_delivery_test.go` for the exact helper API (scene create / member add / location placement / facade `GetSceneForViewer` / posting a `scene pose`). Skeleton:

```go
//go:build integration

package scenes

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

var _ = Describe("Scene character name resolution", func() {
	It("resolves roster display names for participants in different locations", func() {
		ts := integrationtest.Start(suiteT, integrationtest.WithInTreePlugins(), integrationtest.WithPluginCrypto())
		defer ts.Close()

		// GIVEN a scene whose two members are in DIFFERENT locations
		//   (use the harness helpers to create characters at distinct locations,
		//    create a scene, and add both as members — per the sibling specs).
		// WHEN the viewer fetches the scene via the facade GetSceneForViewer
		// THEN every roster ParticipantInfo.CharacterName is a display name,
		//      NOT a 26-char ULID.
		// Assertion shape:
		//   for _, p := range scene.GetParticipants() {
		//       Expect(p.GetCharacterName()).NotTo(MatchRegexp(`^[0-9A-HJKMNP-TV-Z]{26}$`),
		//           "roster name must be a display name, not a ULID")
		//   }
	})

	It("surfaces the pose author display name on the IC stream", func() {
		// GIVEN a member posing in a scene
		// WHEN the IC event is delivered/translated
		// THEN the rendered author is the display name, not the actor ULID.
	})
})
```

Fill the GIVEN/WHEN bodies using the sibling harness helpers; the assertions (display-name-not-ULID) are the behavior under test. The ULID regex `^[0-9A-HJKMNP-TV-Z]{26}$` is the Crockford-base32 ULID shape — asserting a name does NOT match it is the precise "not a ULID" check.

- [ ] **Step 2: Run the integration suite**

Run: `task test:int -- ./test/integration/scenes/...`
Expected: PASS (Docker required; `task plugin:build-all` runs automatically).

- [ ] **Step 3: Commit + close the bug**

Commit (message: `test(scenes): integration coverage for cross-location name resolution (holomush-5rh.25)`).
This task closing-criteria: `holomush-5rh.25` is resolved at merge (do NOT `bd close` before the PR lands, per the close-at-merge convention).

---

## Verification (whole-feature)

- `task pr-prep` green (fast lane: lint, fmt, unit, build).
- `task test:int -- ./test/integration/scenes/...` green (Docker).
- `crypto-reviewer` run for Task 3 (IC payload change).
- Manual web check (optional): roster, pose-order strip, and a fresh pose all show display names.
<!-- adr-capture: sha256=32fa279b73167ad7; session=cli; ts=2026-06-21T00:38:56Z; adrs=holomush-sv1ei -->
