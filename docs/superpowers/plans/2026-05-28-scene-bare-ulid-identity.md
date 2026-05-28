<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 HoloMUSH Contributors
-->

# Scene Bare-ULID Identity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the `scene-` id prefix so scenes mint bare ULIDs like every other entity, restoring `scene log`, scene join/subscription, and the privacy temporal floor for real scenes.

**Architecture:** `newSceneID()` is the single production site that mints the prefix. Changing it to return a bare ULID makes the stored id, the subject token, `FocusKey.TargetID`, and `FocusMembership.TargetID` byte-identical end-to-end, so the three host parse boundaries (`streamToFocusKey`, `streamScopeFloor`, `protoToFocusKey`) work unmodified. The remaining work is realigning test helpers that manually re-added the prefix and adding regression coverage that drives a real `CreateScene` scene through the host paths the test harness previously masked.

**Tech Stack:** Go, `github.com/oklog/ulid/v2`, Ginkgo/Gomega integration tests (`//go:build integration`), the `internal/testsupport/integrationtest` harness (Postgres testcontainer + embedded NATS + real `CoreServer`).

**Spec:** `docs/superpowers/specs/2026-05-28-scene-bare-ulid-identity-design.md`
**Bead:** holomush-y5inx

---

## File map

| File | Responsibility | Tasks |
| --- | --- | --- |
| `plugins/core-scenes/service.go` | `newSceneID()` minter — the root change | 1 |
| `plugins/core-scenes/service_test.go` | unit test for `newSceneID`; fix the `HasPrefix` assertion at :602 | 1 |
| `internal/testsupport/integrationtest/session.go` | `CreateScene` helper — drop the masking `TrimPrefix` | 2 |
| `internal/testsupport/integrationtest/crypto.go` | `EmitSceneICContent` — drop the `"scene-"+` re-add; update comments | 2 |
| `test/integration/scenes/publish_e2e_test.go` | drop the `"scene-"+` reconstruction at :70 | 3 |
| `test/integration/plugin/binary_plugin_test.go` | flip the `HavePrefix("scene-")` assertion at :900 | 3 |
| `test/integration/scenes/real_scene_history_readable_test.go` | NEW — INV-Y5INX-2 (`scene log` host read) | 4 |
| `test/integration/scenes/real_scene_join_subscription_test.go` | NEW — INV-Y5INX-3 (join opens subscription) | 5 |
| `test/integration/scenes/publish_history_scope_e2e_test.go` | NEW — INV-Y5INX-4 / E9 (holomush-5rh.20.42) | 6 |
| `test/integration/scenes/scene_id_bare_guard_test.go` | NEW — INV-Y5INX-1/5 behavioral guard | 7 |

**Reviewers:** `abac-reviewer` + `crypto-reviewer` MUST gate before push (the change touches the scene access-gate parse path and the AAD subject shape).

---

### Task 1: `newSceneID()` mints a bare ULID

**Files:**

- Modify: `plugins/core-scenes/service.go:1113`
- Modify: `plugins/core-scenes/service_test.go` (add unit test; fix the `:602` assertion)

Both files are `package main`. `newSceneID` is unexported, so the unit test calls it directly.

- [ ] **Step 1: Write the failing unit test**

Append to `plugins/core-scenes/service_test.go`:

```go
func TestNewSceneIDReturnsBareULIDWithoutPrefix(t *testing.T) {
	id, err := newSceneID()
	require.NoError(t, err)
	assert.False(t, strings.HasPrefix(id, "scene-"),
		"scene id must be a bare ULID, not a scene- prefixed string (holomush-y5inx)")
	parsed, perr := ulid.Parse(id)
	require.NoError(t, perr, "scene id must parse as a bare ULID")
	assert.Equal(t, id, parsed.String(), "round-trip: stored id equals its ULID string form")
}
```

If `ulid` is not already imported in `service_test.go`, add `"github.com/oklog/ulid/v2"` to its import block.

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test -- -run TestNewSceneIDReturnsBareULIDWithoutPrefix ./plugins/core-scenes/`
Expected: FAIL — `newSceneID()` currently returns `"scene-"+ULID`, so `HasPrefix` is true and `ulid.Parse` errors with `ErrDataSize`.

- [ ] **Step 3: Make the minter return a bare ULID**

In `plugins/core-scenes/service.go`, change the return at line 1113:

```go
// before
	return "scene-" + id.String(), nil
// after
	return id.String(), nil
```

Then fix the now-inverted assertion in `plugins/core-scenes/service_test.go:602` (inside `TestSceneServiceCreateScenePersistsTitleAndOwnerWhenRequestIsValid`):

```go
// before
	assert.True(t, strings.HasPrefix(resp.GetScene().GetId(), "scene-"))
// after
	assert.False(t, strings.HasPrefix(resp.GetScene().GetId(), "scene-"),
		"scene id is a bare ULID (holomush-y5inx)")
	_, idErr := ulid.Parse(resp.GetScene().GetId())
	assert.NoError(t, idErr, "scene id parses as a bare ULID")
```

- [ ] **Step 4: Run the package unit tests to verify they pass**

Run: `task test -- ./plugins/core-scenes/`
Expected: PASS (both the new test and the fixed `:602` assertion).

- [ ] **Step 5: Lint**

Run: `task lint:go`
Expected: clean (no new findings).

- [ ] **Step 6: Commit**

```text
fix(scenes): mint bare ULID scene ids, drop scene- prefix (holomush-y5inx)
```

---

### Task 2: Realign the integration harness to bare scene ids

The harness manually compensated for the prefix in two places. After Task 1 both must stop re-adding/stripping it, or the crypto ingest path will mismatch the now-bare stored row.

**Files:**

- Modify: `internal/testsupport/integrationtest/session.go:462-477` (`CreateScene`)
- Modify: `internal/testsupport/integrationtest/crypto.go:180-190` (`EmitSceneICContent` + doc comment)

Both are `package integrationtest`.

- [ ] **Step 1: Drop the masking strip in `CreateScene`**

In `internal/testsupport/integrationtest/session.go`, replace the body that strips the prefix (lines 470-476):

```go
// before
	require.NoError(s.server.t, err, "integrationtest.Session.CreateScene")
	// core-scenes stamps scene IDs as "scene-"+ULID (plugins/core-scenes/service.go:1113);
	// strip the prefix before parsing the underlying ULID.
	raw := strings.TrimPrefix(resp.GetScene().GetId(), "scene-")
	id, err := ulid.Parse(raw)
	require.NoError(s.server.t, err, "integrationtest.Session.CreateScene: parse scene id")
	return id
// after
	require.NoError(s.server.t, err, "integrationtest.Session.CreateScene")
	// core-scenes mints bare ULID scene ids (plugins/core-scenes/service.go:1113,
	// holomush-y5inx). The returned id parses directly — no prefix to strip.
	id, err := ulid.Parse(resp.GetScene().GetId())
	require.NoError(s.server.t, err, "integrationtest.Session.CreateScene: parse scene id")
	return id
```

If `strings` becomes unused in `session.go` after this edit, remove it from the import block (let `task lint:go` / the compiler tell you).

- [ ] **Step 2: Drop the `"scene-"+` re-add in `EmitSceneICContent`**

In `internal/testsupport/integrationtest/crypto.go`, the doc comment (lines 178-186) and body (lines 188-190) assume the stored id is prefixed. Replace:

```go
// before
// sceneID is the BARE ULID returned by Session.CreateScene; core-scenes
// PERSISTS scene rows under the "scene-"+ULID form (newSceneID,
// service.go:1113) and its InsertScenePose keys off that stored id via
// parseSceneSubject(subject)[3] → UPDATE scenes WHERE id=<token> (audit.go:629,
// :233). So the subject's scene-id token MUST be "scene-"+sceneID — NOT the
// plan's literal sceneID.String() — for the UPDATE…RETURNING to find the row.
// This mirrors production's own subject builder dotStyleSceneSubjectIC, which
// is fed the stored "scene-"+ULID id (store.go:1479).
func (s *Server) EmitSceneICContent(ctx context.Context, plugin string, sceneID, actorID ulid.ULID, eventType, payloadJSON string) EmittedEvent {
	return s.emitPluginEventForScene(ctx, plugin,
		"scene-"+sceneID.String(), actorID, eventType, payloadJSON, true)
}
// after
// sceneID is the BARE ULID returned by Session.CreateScene. core-scenes
// persists scene rows under that bare id (newSceneID, service.go:1113,
// holomush-y5inx) and its InsertScenePose keys off it via
// parseSceneSubject(subject)[3] → UPDATE scenes WHERE id=<token>. The subject's
// scene-id token is the bare sceneID — identical to production's own subject
// builder dotStyleSceneSubjectIC, which is fed the stored bare id.
func (s *Server) EmitSceneICContent(ctx context.Context, plugin string, sceneID, actorID ulid.ULID, eventType, payloadJSON string) EmittedEvent {
	return s.emitPluginEventForScene(ctx, plugin,
		sceneID.String(), actorID, eventType, payloadJSON, true)
}
```

Also update the trailing comment on `emitPluginEventForScene` (around lines 203-205) that reads `EmitSceneICContent passes "scene-"+ULID (matching CreateScene's stored id)` → `EmitSceneICContent passes the bare ULID (matching CreateScene's stored id)`.

- [ ] **Step 3: Run the existing scene + crypto integration suites to verify they still pass on bare ids**

Run: `task test:int -- ./test/integration/scenes/...`
Expected: PASS. The existing `seed_encrypted_ic_validation_test.go` and `publish_e2e_test.go` exercise `EmitSceneICContent` + ingest; they confirm the bare subject token now matches the bare stored row. (Requires Docker.)

> Note: `publish_e2e_test.go` still reconstructs `"scene-"+sceneID.String()` at line 70 — it is fixed in Task 3. If Task 3 has not run yet, that test may fail at the join/command step; that is expected and resolved by Task 3. Run Task 2 and Task 3 together before relying on a green `scenes` suite.

- [ ] **Step 4: Commit**

```text
test(scenes): realign integration harness to bare scene ids (holomush-y5inx)
```

---

### Task 3: Fix the remaining required test fixtures

Two test sites assert or reconstruct the prefix as a contract and break after Task 1.

**Files:**

- Modify: `test/integration/scenes/publish_e2e_test.go:65-83`
- Modify: `test/integration/plugin/binary_plugin_test.go:900`

- [ ] **Step 1: Drop the prefix reconstruction in `publish_e2e_test.go`**

Replace lines 65-70:

```go
// before
		// CreateScene returns the BARE ULID (prefix stripped by the harness),
		// but core-scenes stores the id as "scene-<ULID>" and the command/RPC
		// resolvers match the stored full form verbatim (handleEnd/handleJoin/
		// handleInvite pass the token straight through; resolveSceneRef strips
		// only the leading '#'). So reconstruct the stored form for refs.
		sceneID := alice.CreateScene(ctx, loc)
		fullSceneID := "scene-" + sceneID.String()
// after
		// CreateScene returns the bare ULID, which is exactly the stored id
		// (holomush-y5inx). Command/RPC resolvers match the stored form verbatim
		// (handleEnd/handleJoin/handleInvite pass the token straight through;
		// resolveSceneRef strips only the leading '#'), so the bare id is the ref.
		sceneID := alice.CreateScene(ctx, loc)
		sceneRef := sceneID.String()
```

Then replace every later use of `fullSceneID` in this `It` block with `sceneRef` (the `scene invite`, `scene join`, `scene end`, `scene publish #…`, `scene publish vote …`, and the `ListScenePublishAttempts` `SceneId:` field at the call sites around lines 77-110). Also update the comment at lines 80-82 that says `EmitSceneICContent … re-adds the "scene-" prefix internally` → `EmitSceneICContent takes the bare ULID and builds the bare subject`.

- [ ] **Step 2: Flip the `HavePrefix` assertion in `binary_plugin_test.go`**

`makePrivateScene` (lines 886-894) returns `createResp.GetScene().GetId()` — the raw RPC id, now bare. Replace line 900:

```go
// before
				Expect(sceneID).To(HavePrefix("scene-"))
// after
				Expect(sceneID).NotTo(HavePrefix("scene-"),
					"scene id is a bare ULID (holomush-y5inx)")
				_, parseErr := ulid.Parse(sceneID)
				Expect(parseErr).NotTo(HaveOccurred(), "scene id parses as a bare ULID")
```

If `github.com/oklog/ulid/v2` is not imported in `binary_plugin_test.go`, add it. The rest of that lifecycle test uses `sceneID` directly in commands and DB queries, which match the bare stored id unchanged.

- [ ] **Step 3: Run both affected suites**

Run: `task test:int -- ./test/integration/scenes/... ./test/integration/plugin/...`
Expected: PASS. (Requires Docker; `task plugin:build-all` runs automatically under `task test:int`.)

- [ ] **Step 4: Commit**

```text
test(scenes): drop scene- prefix from publish + binary-plugin fixtures (holomush-y5inx)
```

---

### Task 4: Regression — `scene log` history is readable on a real scene (INV-Y5INX-2)

Proves the host `QueryStreamHistory` path no longer returns `INVALID_ARGUMENT` for a `CreateScene`-minted scene.

**Files:**

- Create: `test/integration/scenes/real_scene_history_readable_test.go`

- [ ] **Step 1: Write the regression test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// INV-Y5INX-2: a participant can read the IC history of a real CreateScene
// scene via the host QueryStreamHistory path (the `scene log` route). Before
// holomush-y5inx the stored "scene-<ULID>" id failed ulid.Parse in
// streamToFocusKey → INVALID_ARGUMENT, so `scene log` was broken for every
// real scene.
var _ = Describe("INV-Y5INX-2: real scene history is readable via the host", func() {
	It("returns IC content for a CreateScene scene instead of INVALID_ARGUMENT", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		ts := integrationtest.Start(suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
		)
		defer ts.Stop()

		alice := ts.ConnectAuthed(ctx, "Alice")
		loc := ts.NewLocation(ctx)
		sceneID := alice.CreateScene(ctx, loc)
		sceneStream := "events." + ts.GameID() + ".scene." + sceneID.String() + ".ic"

		// Emit one sensitive IC pose into the scene (sets up a real scene_log row).
		ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			alice.CharacterID, "scene_pose", `{"text":"the manor is quiet tonight"}`)

		// Read it back through the host history path the `scene log` command
		// uses. The load-bearing assertions are (1) no error — pre-fix this
		// returned INVALID_ARGUMENT — and (2) the emitted pose is present.
		Eventually(func(g Gomega) {
			evs, err := alice.QueryStreamHistory(ctx, sceneStream)
			g.Expect(err).NotTo(HaveOccurred(),
				"INV-Y5INX-2: host QueryStreamHistory MUST NOT reject a bare scene subject")
			g.Expect(evs).NotTo(BeEmpty(), "scene log must contain the emitted pose")
		}).WithTimeout(10 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())
	})
})
```

`QueryStreamHistory` returns `([]*corev1.EventFrame, error)` (`session.go:517`); the slice non-emptiness is the assertion — no element-type shim needed. Mirrors the sibling call at `privacy_test.go:629`.

- [ ] **Step 2: Run it to confirm it passes (post-Task-1)**

Run: `task test:int -- -run TestScenes ./test/integration/scenes/` (Ginkgo entrypoint is `suite_test.go`)
Expected: PASS. (To see the pre-fix RED, temporarily revert Task 1's one-line change: the read fails with `INVALID_ARGUMENT`.)

- [ ] **Step 3: Commit**

```text
test(scenes): regression — real scene history readable via host (holomush-y5inx)
```

---

### Task 5: Regression — real `scene join` opens a focus subscription (INV-Y5INX-3)

The store-shortcut `JoinScene` helper bypasses `protoToFocusKey`, so it cannot prove the bug is fixed. This test drives the real `scene join` command and asserts **delivery** (an emitted pose reaches the joiner's live stream) — independent of the command-success response, which the host may report `Success=true` regardless (bead D4/E8 note).

**Files:**

- Create: `test/integration/scenes/real_scene_join_subscription_test.go`

- [ ] **Step 1: Write the regression test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// INV-Y5INX-3: joining a real scene via the `scene join` command opens a focus
// subscription, so a subsequently-emitted IC pose is delivered to the joiner's
// live stream. Before holomush-y5inx the prefixed "scene-<ULID>" join key
// failed ulid.Parse in protoToFocusKey, the subscription never opened, and the
// joiner received nothing (WaitForEvent would time out).
var _ = Describe("INV-Y5INX-3: real scene join opens a subscription", func() {
	It("delivers a post-join IC pose to a command-joined participant", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		ts := integrationtest.Start(suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
		)
		defer ts.Stop()

		alice := ts.ConnectAuthed(ctx, "Alice")
		bob := ts.ConnectAuthed(ctx, "Bob")
		loc := ts.NewLocation(ctx)
		sceneID := alice.CreateScene(ctx, loc)

		// Bob joins through the REAL command path (JoinScene helper bypasses
		// protoToFocusKey; the command does not). The bare id is the stored ref.
		Expect(bob.SendCommand(ctx, "scene join "+sceneID.String())).To(Succeed())

		// Alice emits an IC pose AFTER bob's join.
		ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			alice.CharacterID, "scene_pose", `{"text":"bob, you made it"}`)

		// If the subscription opened, bob's live stream delivers the pose.
		// WaitForEvent t.Fatalf's on timeout, so reaching a non-nil return is
		// the positive assertion.
		got := bob.WaitForEvent(ctx, "scene_pose")
		Expect(got).NotTo(BeNil(),
			"INV-Y5INX-3: command-joined participant MUST receive scene IC events")
	})
})
```

> Verify the `EmitSceneICContent` `eventType` ("scene_pose") matches the `Type` that `WaitForEvent` sees on the delivered frame. If the host translates the wire type (e.g. a `core-scenes:`-namespaced verb), align the `WaitForEvent` argument to the delivered `EventFrame.Type` — check the type a sibling test (`non_participant_ic_isolation_test.go`) asserts on delivered scene poses and use the same string.

- [ ] **Step 2: Run it to confirm it passes (post-Task-1)**

Run: `task test:int -- ./test/integration/scenes/`
Expected: PASS. (Pre-fix RED: revert Task 1 and `WaitForEvent` times out → `t.Fatalf`.)

- [ ] **Step 3: Commit**

```text
test(scenes): regression — real scene join opens subscription (holomush-y5inx)
```

---

### Task 6: E9 — late-joiner temporal floor on publish events (INV-Y5INX-4, unblocks holomush-5rh.20.42)

Mirrors the `privacy_test.go` I-PRIV-2 scene floor, but on a real `CreateScene` scene with a publish-typed IC event. The late joiner must not see a `scene_publish_started` event emitted before their join; the floor is keyed on `FocusMembership.JoinedAt`. Uses the `JoinScene` store-shortcut (which sets a precise `JoinedAt`) — correct here because this test exercises the **floor**, not the subscription wiring.

**Files:**

- Create: `test/integration/scenes/publish_history_scope_e2e_test.go`

- [ ] **Step 1: Write the E9 test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// INV-Y5INX-4 / E9 (holomush-5rh.20.42): the history-scope temporal floor
// excludes a publish event emitted before a late participant joined a REAL
// scene. The early participant (joined first) sees it; the late participant
// does not. Floors at FocusMembership.JoinedAt via streamScopeFloor.
var _ = Describe("INV-Y5INX-4 / E9: publish-event history-scope floor", func() {
	It("hides a pre-join scene_publish_started from a late joiner of a real scene", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		ts := integrationtest.Start(suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
		)
		defer ts.Stop()

		owner := ts.ConnectAuthed(ctx, "Owner")
		loc := ts.NewLocation(ctx)
		sceneID := owner.CreateScene(ctx, loc)
		sceneStream := "events." + ts.GameID() + ".scene." + sceneID.String() + ".ic"

		// Early joiner is a member before any event is emitted.
		early := ts.ConnectAuthed(ctx, "Early")
		early.JoinScene(ctx, sceneID)

		// Emit the publish-started event BEFORE the late joiner joins.
		ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			owner.CharacterID, "scene_publish_started", `{"attempt":"first"}`)

		// Late joiner joins AFTER the publish-started event.
		late := ts.ConnectAuthed(ctx, "Late")
		lateJoinedAt := late.JoinScene(ctx, sceneID)

		// Emit a second event AFTER the late join so the late joiner has at
		// least one visible event (proves the floor is a floor, not a blanket deny).
		ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			owner.CharacterID, "scene_pose", `{"text":"after late joined"}`)

		// Early joiner sees BOTH events (joined before either).
		Eventually(func(g Gomega) {
			evs, err := early.QueryStreamHistory(ctx, sceneStream)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(len(evs)).To(BeNumerically(">=", 2),
				"early joiner must see the pre-join publish event and the later pose")
		}).WithTimeout(10 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())

		// Late joiner sees ONLY post-join events; every returned event's
		// timestamp is >= their JoinedAt, and the pre-join publish event is gone.
		Eventually(func(g Gomega) {
			evs, err := late.QueryStreamHistory(ctx, sceneStream)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(evs).NotTo(BeEmpty(), "late joiner must see the post-join pose")
			for _, e := range evs {
				g.Expect(e.GetTimestamp().AsTime()).To(BeTemporally(">=", lateJoinedAt),
					"INV-Y5INX-4: pre-join publish event leaked past the floor")
			}
		}).WithTimeout(10 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())
	})
})
```

> The `e.GetTimestamp().AsTime()` accessor mirrors `privacy_test.go:631-633`. If the returned frame type differs, copy the exact timestamp accessor that file uses on its `QueryStreamHistory` results.

- [ ] **Step 2: Run it**

Run: `task test:int -- ./test/integration/scenes/`
Expected: PASS.

- [ ] **Step 3: Close the E9 blocker linkage**

This test satisfies holomush-5rh.20.42 (E9). Note it in the bead: `bd note holomush-5rh.20.42 "Satisfied by test/integration/scenes/publish_history_scope_e2e_test.go (INV-Y5INX-4); unblocked by holomush-y5inx bare-ULID fix."` Do not close 5rh.20.42 here — leave that to its own review/close flow.

- [ ] **Step 4: Commit**

```text
test(scenes): E9 publish-event history-scope floor on real scene (holomush-y5inx)
```

---

### Task 7: Behavioral guard — `CreateScene` RPC returns a bare ULID (INV-Y5INX-1 / INV-Y5INX-5)

A permanent regression guard pinning the production mint output, so a future change cannot silently reintroduce a type-tag prefix.

**Files:**

- Create: `test/integration/scenes/scene_id_bare_guard_test.go`

- [ ] **Step 1: Write the guard test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// INV-Y5INX-1 / INV-Y5INX-5: the production CreateScene RPC mints a bare ULID
// scene id — no "scene-" (or any) prefix. This guards against reintroducing a
// type-tag prefix on entity ids (the bare-ULID identity convention).
var _ = Describe("INV-Y5INX-1/5: CreateScene mints a bare ULID", func() {
	It("returns an id with no scene- prefix that parses as a ULID", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		ts := integrationtest.Start(suiteT, integrationtest.WithInTreePlugins())
		defer ts.Stop()

		alice := ts.ConnectAuthed(ctx, "Alice")
		loc := ts.NewLocation(ctx)

		// Call the RPC directly (not the helper) so we assert on the raw id the
		// server returns, not the harness-parsed value. Mirrors session.go:464.
		resp, err := ts.SceneServiceClient().CreateScene(ctx, &scenev1.CreateSceneRequest{
			CharacterId: alice.CharacterID.String(),
			Title:       "guard scene",
			LocationId:  loc.String(),
			Visibility:  "open",
		})
		Expect(err).NotTo(HaveOccurred())
		rawID := resp.GetScene().GetId()

		Expect(strings.HasPrefix(rawID, "scene-")).To(BeFalse(),
			"INV-Y5INX-1/5: scene id MUST be a bare ULID, not scene- prefixed")
		_, perr := ulid.Parse(rawID)
		Expect(perr).NotTo(HaveOccurred(), "scene id MUST parse as a bare ULID")
	})
})
```

The `scenev1` import alias (`pkg/proto/holomush/scene/v1`) matches `session.go:23`; `SceneServiceClient()` is `harness.go:476`.

- [ ] **Step 2: Run it**

Run: `task test:int -- ./test/integration/scenes/`
Expected: PASS.

- [ ] **Step 3: Commit**

```text
test(scenes): guard — CreateScene mints bare ULID (holomush-y5inx)
```

---

## Final verification (before push)

- [ ] `task test -- ./plugins/core-scenes/` — unit tests green.
- [ ] `task test:int -- ./test/integration/scenes/... ./test/integration/plugin/...` — integration green (Docker).
- [ ] `task lint:go` — clean.
- [ ] `task pr-prep` (fast lane) — green. Note: this diff touches integration surface (Ginkgo specs + shared `integrationtest` helpers), so also run `task pr-prep:full` per CLAUDE.md, since `Integration Test` / `E2E Test` are required CI checks.
- [ ] `abac-reviewer` + `crypto-reviewer` run and return READY before push.

<!-- adr-capture: sha256=296ff30d3fb4b1e8; session=cli; ts=2026-05-28T19:52:10Z; adrs=holomush-vy0rt -->
