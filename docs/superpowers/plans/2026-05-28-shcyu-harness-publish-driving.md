<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 HoloMUSH Contributors
-->

# shcyu — integrationtest Publish Driving Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the test-tier harness driving layer so a full-stack Ginkgo E2E can drive the core-scenes Phase 6 publish lifecycle to PUBLISHED and read it back, and ship that happy-path E2E (folds in `holomush-5rh.20.39`/E6).

**Architecture:** All additions are behind `//go:build integration` in `internal/testsupport/integrationtest` + `test/integration/scenes` — **zero production code changes**. shcyu composes the two landed prerequisites: `WithPluginConfigOverrides` (sets yzt86's per-plugin config so cool-off + scheduler interval are short) and `WithPluginCrypto` (5iaov; the encrypt + read-back round-trip so the publish snapshot decrypts the IC stream and reaches PUBLISHED). It adds a `SceneService` client accessor, wires `Session.CreateScene`, parameterizes the existing `EmitPluginEvent` by scene/actor (to seed encrypted IC content into a created scene — the command emit path is fence-rejected under crypto, per spec §3.4), and the E2E.

**Tech Stack:** Go 1.24+, gopher-lua/go-plugin substrate, generated `scenev1` gRPC client, Ginkgo/Gomega (the existing `test/integration/scenes` suite), testcontainers Postgres + embedded NATS. Spec: `docs/superpowers/specs/2026-05-27-shcyu-harness-publish-driving-design.md`.

---

## Phase 1: Harness driving layer + happy-path E2E

### Task 1: `WithPluginConfigOverrides` StartOption

**Files:**

- Modify: `internal/testsupport/integrationtest/plugins.go` (add the option + thread into `startPlugins`' `PluginSubsystemConfig`)
- Modify: `internal/testsupport/integrationtest/harness.go` (`startConfig` struct — add the field)
- Test: `internal/testsupport/integrationtest/plugins_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/testsupport/integrationtest/plugins_test.go`:

```go
//go:build integration

func TestWithPluginConfigOverridesThreads(t *testing.T) {
	var c startConfig
	WithPluginConfigOverrides(map[string]map[string]string{
		"core-scenes": {"cooloff_window": "1ms", "scheduler_interval": "20ms"},
	})(&c)
	require.Equal(t, "1ms", c.pluginConfigOverrides["core-scenes"]["cooloff_window"])
	require.Equal(t, "20ms", c.pluginConfigOverrides["core-scenes"]["scheduler_interval"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test:int -- -run TestWithPluginConfigOverridesThreads ./internal/testsupport/integrationtest/`
Expected: FAIL — `WithPluginConfigOverrides` / `startConfig.pluginConfigOverrides` undefined.

- [ ] **Step 3: Add the field + option**

In `harness.go`, add to `startConfig`:

```go
	// pluginConfigOverrides is the per-plugin opaque config override
	// (plugin name → key → value) threaded into PluginSubsystemConfig.
	pluginConfigOverrides map[string]map[string]string
```

In `plugins.go`, add (next to `WithInTreePlugins`):

```go
// WithPluginConfigOverrides sets per-plugin config overrides (plugin name →
// key → value) the harness threads into PluginSubsystemConfig.PluginConfigOverrides
// — the same opaque channel production uses (yzt86). Reusable by any plugin's
// harness tests. Empty/absent → manifest defaults.
func WithPluginConfigOverrides(overrides map[string]map[string]string) StartOption {
	return func(c *startConfig) { c.pluginConfigOverrides = overrides }
}
```

- [ ] **Step 4: Thread it into `startPlugins`**

In `plugins.go` `startPlugins`, where `pluginsetup.PluginSubsystemConfig{...}` is built, add the field:

```go
		PluginConfigOverrides: d.pluginConfigOverrides,
```

and add `pluginConfigOverrides map[string]map[string]string` to the `pluginDeps` struct, populated from `startConfig.pluginConfigOverrides` where `Start` builds `pluginDeps` (mirror how other `startConfig`→`pluginDeps` fields flow).

- [ ] **Step 5: Run test to verify it passes**

Run: `task test:int -- -run TestWithPluginConfigOverridesThreads ./internal/testsupport/integrationtest/`
Expected: PASS.

- [ ] **Step 6: Build + commit**

Run: `task build`. Commit: `test(harness): WithPluginConfigOverrides StartOption (holomush-shcyu)`.

---

### Task 2: `Server.SceneServiceClient()` accessor

**Files:**

- Modify: `internal/testsupport/integrationtest/harness.go` (add the method near `ServiceRegistry()` at `:451`)
- Test: `test/integration/scenes/publish_e2e_test.go` exercises it (Task 5); a focused smoke check here

- [ ] **Step 1: Write the failing test**

Add to `internal/testsupport/integrationtest/harness_test.go` (or a scenes integration test):

```go
//go:build integration

func TestSceneServiceClientResolves(t *testing.T) {
	ts := Start(t, WithInTreePlugins())
	require.NotNil(t, ts.SceneServiceClient())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test:int -- -run TestSceneServiceClientResolves ./internal/testsupport/integrationtest/`
Expected: FAIL — `Server.SceneServiceClient` undefined.

- [ ] **Step 3: Implement the accessor**

In `harness.go`:

```go
// SceneServiceClient returns a SceneService client backed by the loaded
// core-scenes plugin, resolved from the existing plugin ServiceRegistry.
// Test-only; requires WithInTreePlugins (panics otherwise via requirePlugins).
func (s *Server) SceneServiceClient() scenev1.SceneServiceClient {
	s.requirePlugins("SceneServiceClient")
	svc, err := s.ServiceRegistry().Resolve("holomush.scene.v1.SceneService")
	require.NoError(s.t, err, "integrationtest.Server.SceneServiceClient: resolve SceneService")
	require.NotNil(s.t, svc.Conn, "integrationtest.Server.SceneServiceClient: nil conn")
	return scenev1.NewSceneServiceClient(svc.Conn)
}
```

Add the import `scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"` if absent.

- [ ] **Step 4: Run test to verify it passes**

Run: `task test:int -- -run TestSceneServiceClientResolves ./internal/testsupport/integrationtest/`
Expected: PASS (core-scenes registers `holomush.scene.v1.SceneService` via `WithInTreePlugins`).

- [ ] **Step 5: Build + commit**

Run: `task build`. Commit: `test(harness): Server.SceneServiceClient() accessor (holomush-shcyu)`.

---

### Task 3: Wire `Session.CreateScene`

**Files:**

- Modify: `internal/testsupport/integrationtest/session.go` (`:460-462` stub)
- Test: `test/integration/scenes/publish_e2e_test.go` (Task 5); a focused check here

- [ ] **Step 1: Write the failing test**

```go
//go:build integration

func TestSessionCreateSceneReturnsULID(t *testing.T) {
	ts := Start(t, WithInTreePlugins())
	loc := ts.NewLocation(context.Background())
	alice := ts.ConnectAuthed(context.Background(), "Alice") // creates player+character, returns *Session
	sceneID := alice.CreateScene(context.Background(), loc)
	require.NotEqual(t, ulid.ULID{}, sceneID)
}
```

Grounded: `Server.ConnectAuthed(ctx, charName)` (`harness.go:639`) creates the player + character via `world.NewCharacter` and returns a `*Session` whose `CharacterID` is a `ulid.ULID` (`session.go:42`).

- [ ] **Step 2: Run test to verify it fails**

Run: `task test:int -- -run TestSessionCreateSceneReturnsULID ./internal/testsupport/integrationtest/`
Expected: FAIL — current stub calls `t.Fatalf` (`session.go:462`).

- [ ] **Step 3: Replace the stub**

Replace `session.go:460-462`:

```go
// CreateScene creates a scene owned by this session's character via the
// loaded core-scenes SceneService and returns its ULID.
func (s *Session) CreateScene(ctx context.Context, locationID ulid.ULID) ulid.ULID {
	s.server.t.Helper()
	resp, err := s.server.SceneServiceClient().CreateScene(ctx, &scenev1.CreateSceneRequest{
		CharacterId: s.CharacterID.String(), // CharacterID is a ulid.ULID (session.go:42)
		Title:       "test scene",
		LocationId:  locationID.String(),
		Visibility:  "open", // string literal — the SceneVisibility consts live in package main (plugins/core-scenes); confirm core-scenes' accepted value
	})
	require.NoError(s.server.t, err, "integrationtest.Session.CreateScene")
	// CreateSceneResponse wraps *SceneInfo (scene.pb.go:540) — id is resp.GetScene().GetId().
	id, err := ulid.Parse(resp.GetScene().GetId())
	require.NoError(s.server.t, err, "integrationtest.Session.CreateScene: parse scene id")
	return id
}
```

Notes for the implementer: the signature gains `locationID ulid.ULID` (the stub took only `ctx`); update the stub's callers if any (grep `\.CreateScene(`). `s.CharacterID` is grounded (`session.go:42`, a `ulid.ULID`).

- [ ] **Step 4: Run test to verify it passes**

Run: `task test:int -- -run TestSessionCreateSceneReturnsULID ./internal/testsupport/integrationtest/`
Expected: PASS.

- [ ] **Step 5: Build + commit**

Run: `task build`. Commit: `test(harness): wire Session.CreateScene to SceneService (holomush-shcyu)`.

---

### Task 4: Scene-parameterized `EmitSceneICContent` (seed encrypted content into a created scene)

**Why:** The existing `EmitPluginEvent` (`crypto.go:163`) hard-codes `WithPluginCrypto`'s fixed `sceneID`/`actorID` (`crypto.go:180,189`). shcyu's E2E creates its **own** scene and needs encrypted `scene_pose` content in it. The command emit path can't (SDK can't set `Sensitive` → INV-7 fence, spec §3.4), so we add a scene/actor-parameterized variant. A focused validation test proves the parameterized crypto round-trip **before** the full E2E (spec mandate).

**Files:**

- Modify: `internal/testsupport/integrationtest/crypto.go` (extract core; add the variant)
- Test: `test/integration/plugincrypto/roundtrip_test.go` or a scenes test (validation)

- [ ] **Step 1: Write the failing validation test**

In `test/integration/scenes/publish_e2e_test.go` (or a focused crypto test):

```go
// Validates encrypted IC content can be seeded into a CreateScene-created scene
// and read back (the parameterized crypto round-trip) — de-risks Task 5.
It("seeds encrypted scene_pose into a created scene and reads it back", func() {
	loc := ts.NewLocation(ctx)
	alice := ts.ConnectAuthed(ctx, "Alice")
	sceneID := alice.CreateScene(ctx, loc)
	emitted := ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
		alice.CharacterID, "scene_pose", `{"text":"a secret pose"}`) // CharacterID is a ulid.ULID
	Expect(emitted.SubjectStr).To(ContainSubstring(sceneID.String()))
	Expect(ts.WireCodecFor(emitted.SubjectStr)).To(Equal(codec.NameXChaCha20v1)) // encrypted
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test:int -- ./test/integration/scenes/...`
Expected: FAIL — `EmitSceneICContent` undefined.

- [ ] **Step 3: Refactor `EmitPluginEvent` + add the variant**

In `crypto.go`, extract the body of `EmitPluginEvent` into an unexported helper taking explicit `sceneID, actorID ulid.ULID`, and have both the existing method (fixed ids — preserves 5iaov's callers) and a new exported variant call it:

```go
// EmitSceneICContent emits an encrypted (Sensitive=true) scene IC event for an
// ARBITRARY scene + actor — used to seed content into a CreateScene-created
// scene (the command emit path can't set Sensitive; see spec §3.4). Requires
// WithPluginCrypto + WithInTreePlugins. The scene row MUST already exist
// (CreateScene) so core-scenes' InsertScenePose UPDATE … RETURNING resolves.
func (s *Server) EmitSceneICContent(ctx context.Context, plugin string, sceneID, actorID ulid.ULID, eventType, payloadJSON string) EmittedEvent {
	return s.emitPluginEventForScene(ctx, plugin, sceneID, actorID, eventType, payloadJSON, true)
}

// emitPluginEventForScene is the parameterized core (extracted from
// EmitPluginEvent). EmitPluginEvent calls it with s.pluginCrypto.sceneID /
// .actorID; EmitSceneICContent passes caller-supplied ids.
func (s *Server) emitPluginEventForScene(ctx context.Context, plugin string, sceneID, actorID ulid.ULID, eventType, payloadJSON string, sensitive bool) EmittedEvent {
	// ... body of the current EmitPluginEvent, with sceneID/actorID substituted
	// for s.pluginCrypto.sceneID / s.pluginCrypto.actorID at crypto.go:180,189 ...
}
```

Update the existing `EmitPluginEvent` to delegate: `return s.emitPluginEventForScene(ctx, plugin, s.pluginCrypto.sceneID, s.pluginCrypto.actorID, eventType, payloadJSON, sensitive)`.

- [ ] **Step 4: Run to verify it passes**

Run: `task test:int -- ./test/integration/scenes/...` (the validation It) and `task test:int -- ./test/integration/plugincrypto/...` (5iaov's suite still green — the refactor preserved its path).
Expected: PASS both.

- [ ] **Step 5: Build + lint + commit**

Run: `task build` then `task lint:go`. Commit: `test(harness): EmitSceneICContent — seed encrypted IC content into an arbitrary scene (holomush-shcyu)`.

---

### Task 5: Happy-path publish E2E (folds E6 / `holomush-5rh.20.39`)

**Files:**

- Create/extend: `test/integration/scenes/publish_e2e_test.go` (Ginkgo, in the existing scenes suite)

- [ ] **Step 1: Write the E2E spec**

Follow the existing `test/integration/scenes/*_test.go` Ginkgo conventions (shared `suiteT`, `BeforeEach` Start, `ts` handle). The happy-path spec:

```go
It("Alice ends scene, publishes, votes pass, cool-off elapses → PUBLISHED with content", func() {
	loc := ts.NewLocation(ctx)
	alice := ts.ConnectAuthed(ctx, "Alice")
	bob := ts.ConnectAuthed(ctx, "Bob")

	sceneID := alice.CreateScene(ctx, loc)
	// Bob must JOIN, not merely be invited — the vote roster seeds from
	// role IN ('owner','member') (publish_store.go:86); an 'invited' row is
	// excluded (INV-P6-1). Without the join, Bob's vote command errors (not a voter).
	Expect(alice.SendCommand(ctx, "scene invite #"+sceneID.String()+" "+bob.CharacterName)).To(Succeed())
	Expect(bob.SendCommand(ctx, "scene join #"+sceneID.String())).To(Succeed())
	// (confirm the exact invite/join command surface in plugins/core-scenes/commands.go
	// + spec §6.1; handleInvite expects "scene invite <sceneRef> <character>", commands.go:950.)

	// seed encrypted IC content (command path is fence-rejected under crypto, §3.4)
	ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
		alice.CharacterID, "scene_pose", `{"text":"the scene happens"}`) // CharacterID is a ulid.ULID

	// lifecycle stays command-driven (E6's intent): end → start publish → unanimous vote
	Expect(alice.SendCommand(ctx, "scene end #"+sceneID.String())).To(Succeed())
	Expect(alice.SendCommand(ctx, "scene publish #"+sceneID.String())).To(Succeed())
	Expect(alice.SendCommand(ctx, "scene publish vote yes #"+sceneID.String())).To(Succeed())
	Expect(bob.SendCommand(ctx, "scene publish vote yes #"+sceneID.String())).To(Succeed())

	// scheduler (~20ms interval, ~1ms cool-off via WithPluginConfigOverrides) sweeps → PUBLISHED.
	// Await the resolved notice (emitter wired by WithPluginCrypto), bounded.
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	alice.WaitForEvent(waitCtx, "scene_publish_resolved")

	// The read RPCs key off published_scene_id — the ATTEMPT ulid, NOT the scene
	// ulid (scene.pb.go:2213,2697). The lifecycle was command-driven, so recover
	// the attempt id via ListScenePublishAttempts → the PUBLISHED summary's Id
	// (mirrors the plugin's own publishedAttemptID, commands.go:223-225).
	listResp, err := ts.SceneServiceClient().ListScenePublishAttempts(ctx, &scenev1.ListScenePublishAttemptsRequest{
		CallerCharacterId: alice.CharacterID.String(), SceneId: sceneID.String(),
	})
	Expect(err).NotTo(HaveOccurred())
	var publishedSceneID string
	for _, a := range listResp.GetAttempts() { // []*PublishedSceneSummary{Id,AttemptNumber,Status} (scene.pb.go:2610)
		if a.GetStatus() == "PUBLISHED" { // confirm the canonical published status string
			publishedSceneID = a.GetId()
		}
	}
	Expect(publishedSceneID).NotTo(BeEmpty(), "no PUBLISHED attempt found for the scene")

	// participant read returns decrypted content
	pub, err := ts.SceneServiceClient().GetPublishedScene(ctx, &scenev1.GetPublishedSceneRequest{
		CallerCharacterId: alice.CharacterID.String(), PublishedSceneId: publishedSceneID,
	})
	Expect(err).NotTo(HaveOccurred())
	// content_entries is "populated only when PUBLISHED" (scene.pb.go:2270), so
	// non-empty content is the grounded PUBLISHED + decryption-succeeded signal.
	Expect(pub.GetContentEntries()).NotTo(BeEmpty())

	// public archive returns content once PUBLISHED
	arch, err := ts.SceneServiceClient().GetPublicSceneArchive(ctx, &scenev1.GetPublicSceneArchiveRequest{
		PublishedSceneId: publishedSceneID,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(arch.GetContentEntries()).NotTo(BeEmpty())
})
```

The `BeforeEach` MUST `Start(suiteT, WithInTreePlugins(), WithPluginCrypto(), WithPluginConfigOverrides(map[string]map[string]string{"core-scenes": {"cooloff_window": "1ms", "scheduler_interval": "20ms"}}))`.

Implementer notes (grounded shapes — confirm only the flagged unknowns): read RPCs take `PublishedSceneId` (attempt ulid), recovered via `ListScenePublishAttempts` → `PublishedSceneSummary{Id,Status}` (`scene.pb.go:2610`); `ConnectAuthed(ctx, charName)` creates the char and returns `*Session` (`harness.go:639`). Still to confirm at impl: the exact `scene join` command token + whether `handleInvite` strips the `#` ref prefix (it does NOT route through `resolveSceneRef`, `commands.go:950`); the canonical published-status string (`"PUBLISHED"`). Use `#<sceneID>` ref form (spec §6.1).

- [ ] **Step 2: Run the E2E**

Run: `task test:int -- ./test/integration/scenes/...`
Expected: PASS — the scene reaches PUBLISHED and both RPCs return decrypted content.

- [ ] **Step 3: Run the full suite + commit**

Run: `task test:int -- ./test/integration/scenes/... ./test/integration/plugincrypto/...` (no regressions).
Commit: `test(scenes): E2E happy-path publish flow (holomush-shcyu, closes 5rh.20.39)`.

---

## Final verification

- [ ] `task test:int -- ./internal/testsupport/integrationtest/... ./test/integration/scenes/... ./test/integration/plugincrypto/...` — all green (Docker required).
- [ ] `task lint` + `task fmt` clean.
- [ ] Confirm **zero production code changes** — `jj diff --stat` shows only `internal/testsupport/integrationtest/` + `test/integration/` paths (all `//go:build integration`).
- [ ] `bd close holomush-5rh.20.39` (E6, folded in) with a reference to the E2E; note the now-unblocked variant beads (`.40`/`.41`/`.42`/`.30`).
<!-- adr-capture: sha256=e2dbd127464278ee; ts=2026-05-28T12:43:47Z; adrs= -->
