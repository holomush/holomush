<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Focus-Delta Coordinator Unification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Relocate per-connection focus-delta delivery from the binary plugin-host RPC handler into `focus.Coordinator`, so both the binary production path and the Lua runtime deliver live scene-stream deltas through one common substrate seam.

**Architecture:** `focus.Coordinator.{AutoFocusOnJoin,SetConnectionFocus}` already mutate `Connection.FocusKey` and already drive the *session-wide* `StreamSender`. This plan gives the coordinator a sibling `ConnectionSender` + `gameID`, has it compute `ComputeFocusManagedStreams → StreamDeltas → SendToConnection` itself, deletes the duplicate driving (and the now-dead `ConnectionSender` seam) from the binary host, and wires the coordinator's `ConnectionSender` in production (fixing the P0 binary prod gap) and the harness via one anti-drift helper. A full test-Lua-plugin integration test locks runtime symmetry.

**Tech Stack:** Go, `oklog/ulid`, `samber/oops`, `log/slog`; gopher-lua plugins; Ginkgo/testify; `internal/testsupport/integrationtest` harness (embedded NATS + Postgres testcontainer).

**Spec:** [`docs/superpowers/specs/2026-05-28-focus-delta-coordinator-unification-design.md`](../specs/2026-05-28-focus-delta-coordinator-unification-design.md) (design-review READY). **Bead:** holomush-66228 (P0).

---

## File Structure

| File | Responsibility | Change |
| ---- | -------------- | ------ |
| `internal/grpc/focus/coordinator.go` | Coordinator struct + options | Add `connectionSender ConnectionSender` + `gameID string` fields; add `WithConnectionSender` + `WithGameID` options |
| `internal/grpc/focus/focus_delta.go` (NEW) | The single per-connection delta driver | `(*defaultCoordinator).driveFocusDeltas` helper |
| `internal/grpc/focus/auto_focus_on_join.go` | AutoFocusOnJoin coordinator method | Call `driveFocusDeltas` (old = grid/nil) before return |
| `internal/grpc/focus/set_connection_focus.go` | SetConnectionFocus coordinator method | Call `driveFocusDeltas` (old = `result.OldFocusKey`) before return |
| `internal/grpc/focus/focus_delta_test.go` (NEW) | Coordinator delta + boundary unit tests | Whole file |
| `internal/plugin/goplugin/host_service.go` | Binary focus RPC handlers | Delete the two delta-driving blocks (`:253-270`, `:312-335`); handlers become delegate + wire-marshal |
| `internal/plugin/goplugin/host.go` | Binary host | Delete `connectionSender` field + `WithConnectionSender` + `SetConnectionSender` + `ConnectionSender()` (keep `gameID`/`GameID()`) |
| `internal/plugin/goplugin/host_service_test.go` | Binary handler tests | Retire `stubConnectionSender`/`newTestServerWithConnSender`; convert delta tests to delegation; keep wire-marshal tests |
| `internal/plugin/setup/subsystem.go` | Plugin subsystem wiring | Delete `PluginSubsystemConfig.ConnectionSender` (`:121`) + wiring block (`:299-307`); KEEP `hostfunc.WithStreamRegistry` (`:200-203`) |
| `internal/grpc/focus_wiring.go` (NEW) | Single anti-drift assembly point | `FocusStreamCoordinatorOptions(reg) []focus.CoordinatorOption` |
| `internal/grpc/focus_wiring_test.go` (NEW) | INV-FS-5 unit test | Whole file |
| `cmd/holomush/sub_grpc.go` | Prod coordinator wiring | Replace `WithStreamSender` block with `FocusStreamCoordinatorOptions` + `WithGameID` |
| `cmd/holomush/core.go` | Prod plugin config | Remove `ConnectionSender` from `PluginSubsystemConfig` literal (field deleted) |
| `internal/testsupport/integrationtest/harness.go` | Harness coordinator wiring | Drop the binary-host `ConnectionSender` chain (local var + `pluginDeps{...}` literal); add `FocusStreamCoordinatorOptions(streamRegistry)` + `WithGameID(bus.Bus.GameID())` to `NewCoordinator`; thread `extraPluginDirs` into `pluginDeps` |
| `internal/testsupport/integrationtest/plugins.go` | Harness plugin staging | Drop `pluginDeps.connectionSender` field + `ConnectionSender: d.connectionSender` from the `PluginSubsystemConfig` literal in `startPlugins`; stage `extraPluginDirs` via `copyTree` after `assemblePluginsDir` |
| `internal/testsupport/integrationtest/options.go` (NEW) | Harness options | Add `WithExtraPluginDir(dir string)` + `extraPluginDirs []string` on `startConfig` |
| `internal/plugin/lua/focus_ops_adapter.go` | Lua focus adapter | Comment-only corrections (no logic change) |
| `internal/plugin/lua/focus_ops_adapter_test.go` (NEW) | Lua parity unit test | Whole file |
| `test/meta/focus_delta_gate_test.go` (NEW) | INV-FS-1 + INV-FS-4 meta-tests | Whole file |
| `test/integration/scenes/testdata/lua/focus_join/{plugin.yaml,plugin.lua}` (NEW) | Test-only Lua plugin fixture | Whole files |
| `test/integration/scenes/lua_focus_parity_test.go` (NEW) | Full-fidelity Lua integration test | Whole file |
| `site/src/content/docs/contributing/how-to/integration-tests.md` | Contributor docs | Update `WithFocusDelivery` + `FocusStreamCoordinatorOptions` + Lua fixture |

---

## Phase 1: Coordinator drives per-connection deltas

### Task 1: AutoFocusOnJoin drives deltas via the coordinator

**Files:**

- Create: `internal/grpc/focus/focus_delta.go`
- Create: `internal/grpc/focus/focus_delta_test.go`
- Modify: `internal/grpc/focus/coordinator.go` (struct + options)
- Modify: `internal/grpc/focus/auto_focus_on_join.go` (call helper before return)

- [ ] **Step 1: Write the failing test**

Create `internal/grpc/focus/focus_delta_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
)

// connDelta is one captured SendToConnection call.
type connDelta struct {
	sessionID    string
	connectionID ulid.ULID
	stream       string
	add          bool
}

// captureConnSender is a focus.ConnectionSender test double. errOn maps a
// stream name to an error the sender returns for that stream (boundary tests).
type captureConnSender struct {
	calls []connDelta
	errOn map[string]error
}

func (s *captureConnSender) SendToConnection(sessionID string, connectionID ulid.ULID, stream string, add bool) error {
	s.calls = append(s.calls, connDelta{sessionID, connectionID, stream, add})
	if s.errOn != nil {
		if err, ok := s.errOn[stream]; ok {
			return err
		}
	}
	return nil
}

func (s *captureConnSender) adds() []string {
	var out []string
	for _, c := range s.calls {
		if c.add {
			out = append(out, c.stream)
		}
	}
	return out
}

func (s *captureConnSender) removes() []string {
	var out []string
	for _, c := range s.calls {
		if !c.add {
			out = append(out, c.stream)
		}
	}
	return out
}

func TestAutoFocusOnJoinDrivesPerConnectionDeltas(t *testing.T) {
	charID := ulid.Make()
	sceneID := ulid.Make()
	locID := ulid.Make()
	termConnID := ulid.Make()

	sessions := map[string]*session.Info{
		"sess-1": {
			ID:          "sess-1",
			CharacterID: charID,
			LocationID:  locID,
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
			},
		},
	}
	coord, _ := newTestCoordinator(t, sessions)
	cs := &captureConnSender{}
	coord.connectionSender = cs
	coord.gameID = "main"

	require.NoError(t, coord.sessionStore.AddConnection(context.Background(), &session.Connection{
		ID: termConnID, SessionID: "sess-1", ClientType: "terminal",
	}))

	resp, err := coord.AutoFocusOnJoin(context.Background(), charID, sceneID)
	require.NoError(t, err)
	require.Len(t, resp.FocusedConnectionIDs, 1)

	scene := sceneID.String()
	loc := locID.String()
	for _, c := range cs.calls {
		assert.Equal(t, "sess-1", c.sessionID)
		assert.Equal(t, termConnID, c.connectionID)
	}
	assert.ElementsMatch(t, []string{
		"events.main.scene." + scene + ".ic",
		"events.main.scene." + scene + ".ooc",
	}, cs.adds(), "grid→scene MUST add scene IC + OOC")
	assert.ElementsMatch(t, []string{"location:" + loc}, cs.removes(), "grid→scene MUST remove the location stream")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestAutoFocusOnJoinDrivesPerConnectionDeltas ./internal/grpc/focus/`
Expected: FAIL — compile error `coord.connectionSender undefined` / `coord.gameID undefined`.

- [ ] **Step 3: Add coordinator fields + options**

In `internal/grpc/focus/coordinator.go`, add to the `defaultCoordinator` struct (after the `streamSender StreamSender` field):

```go
	connectionSender  ConnectionSender
```

and add a `gameID` field to the struct (after `policies`):

```go
	gameID            string
```

Add two options after `WithStreamSender`:

```go
// WithConnectionSender sets the per-Connection stream sender used to deliver
// focus-driven subscription deltas (INV-FS-1: the coordinator is the sole
// driver). A nil sender disables per-connection delta delivery (best-effort).
func WithConnectionSender(sender ConnectionSender) CoordinatorOption {
	return func(c *defaultCoordinator) { c.connectionSender = sender }
}

// WithGameID sets the game ID used to compute focus-managed scene stream
// names (events.<gameID>.scene.<id>.{ic,ooc}). Empty defaults to "main" at
// the call site of ComputeFocusManagedStreams' caller.
func WithGameID(gameID string) CoordinatorOption {
	return func(c *defaultCoordinator) { c.gameID = gameID }
}
```

- [ ] **Step 4: Create the delta-driver helper**

Create `internal/grpc/focus/focus_delta.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"log/slog"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/session"
)

// driveFocusDeltas computes the per-connection subscription delta from
// oldFK→newFK and delivers it to each connection via the coordinator's
// ConnectionSender. This is the single common-path driver for BOTH plugin
// runtimes (INV-FS-1): the binary RPC handler and the Lua hostfunc adapter
// both reach it through AutoFocusOnJoin / SetConnectionFocus.
//
// Best-effort and non-fatal (INV-FS-8): a delivery failure is logged but never
// fails the focus mutation, and one connection's failure does not abort the
// rest. A nil connectionSender (e.g. tests / deployments without a registry)
// skips delivery — preserving the holomush-y5inx.9 nil-skip behavior, now
// uniformly for both runtimes.
func (c *defaultCoordinator) driveFocusDeltas(
	ctx context.Context,
	sessionID string,
	charLocationID ulid.ULID,
	oldFK, newFK *session.FocusKey,
	conns []ulid.ULID,
) {
	if c.connectionSender == nil || sessionID == "" || len(conns) == 0 {
		return
	}
	gameID := c.gameID
	if gameID == "" {
		gameID = "main"
	}
	oldStreams := ComputeFocusManagedStreams(oldFK, charLocationID, gameID)
	newStreams := ComputeFocusManagedStreams(newFK, charLocationID, gameID)
	adds, removes := StreamDeltas(oldStreams, newStreams)
	for _, connID := range conns {
		for _, stream := range adds {
			if err := c.connectionSender.SendToConnection(sessionID, connID, stream, true); err != nil {
				slog.WarnContext(ctx, "focus delta add delivery failed",
					"session_id", sessionID, "connection_id", connID.String(),
					"stream", stream, "error", err)
			}
		}
		for _, stream := range removes {
			if err := c.connectionSender.SendToConnection(sessionID, connID, stream, false); err != nil {
				slog.WarnContext(ctx, "focus delta remove delivery failed",
					"session_id", sessionID, "connection_id", connID.String(),
					"stream", stream, "error", err)
			}
		}
	}
}
```

- [ ] **Step 5: Call the helper from AutoFocusOnJoin**

In `internal/grpc/focus/auto_focus_on_join.go`, immediately before the final `return resp, nil` (after the connection-mutation loop), insert:

```go
	// INV-FS-1: drive per-connection subscription deltas at the common path.
	// Focused conns were on grid before this call (INV-P5-11 skips already-focused
	// conns), so the old stream set is the grid/location set (nil FocusKey).
	sceneFk := &session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}
	c.driveFocusDeltas(ctx, resp.SessionID, resp.CharLocationID, nil, sceneFk, resp.FocusedConnectionIDs)
```

(If the response variable is named `r` rather than `resp` in this file, use `r`. `session` is already imported here.)

- [ ] **Step 6: Run test to verify it passes**

Run: `task test -- -run TestAutoFocusOnJoinDrivesPerConnectionDeltas ./internal/grpc/focus/`
Expected: PASS.

- [ ] **Step 7: Lint + commit**

Run: `task lint:go`
Commit using VCS-appropriate commands per `references/vcs-preamble.md`:
`feat(focus): coordinator drives per-connection deltas on AutoFocusOnJoin (holomush-66228)`

### Task 2: SetConnectionFocus drives deltas (using OldFocusKey)

**Files:**

- Modify: `internal/grpc/focus/set_connection_focus.go:111` (before `return result, nil`)
- Modify: `internal/grpc/focus/focus_delta_test.go` (add test)

- [ ] **Step 1: Write the failing test**

Append to `internal/grpc/focus/focus_delta_test.go`:

```go
func TestSetConnectionFocusDrivesPerConnectionDeltas(t *testing.T) {
	charID := ulid.Make()
	sceneA := ulid.Make()
	sceneB := ulid.Make()
	locID := ulid.Make()
	connID := ulid.Make()

	fkA := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneA}
	sessions := map[string]*session.Info{
		"sess-1": {
			ID:          "sess-1",
			CharacterID: charID,
			LocationID:  locID,
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneA, JoinedAt: time.Now()},
				{Kind: session.FocusKindScene, TargetID: sceneB, JoinedAt: time.Now()},
			},
		},
	}
	coord, _ := newTestCoordinator(t, sessions)
	cs := &captureConnSender{}
	coord.connectionSender = cs
	coord.gameID = "main"

	require.NoError(t, coord.sessionStore.AddConnection(context.Background(), &session.Connection{
		ID: connID, SessionID: "sess-1", ClientType: "terminal", FocusKey: &fkA,
	}))

	// scene A → scene B: remove A's IC/OOC, add B's IC/OOC.
	fkB := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneB}
	_, err := coord.SetConnectionFocus(context.Background(), connID, &fkB, false)
	require.NoError(t, err)

	a := sceneA.String()
	b := sceneB.String()
	assert.ElementsMatch(t, []string{
		"events.main.scene." + b + ".ic", "events.main.scene." + b + ".ooc",
	}, cs.adds(), "scene→scene MUST add the new scene's streams")
	assert.ElementsMatch(t, []string{
		"events.main.scene." + a + ".ic", "events.main.scene." + a + ".ooc",
	}, cs.removes(), "scene→scene MUST remove the old scene's streams (from OldFocusKey)")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestSetConnectionFocusDrivesPerConnectionDeltas ./internal/grpc/focus/`
Expected: FAIL — `cs.calls` empty (coordinator's SetConnectionFocus does not drive deltas yet).

- [ ] **Step 3: Call the helper from SetConnectionFocus**

In `internal/grpc/focus/set_connection_focus.go`, replace the final `return result, nil` (line 111) with:

```go
	// INV-FS-1: drive the per-connection subscription delta at the common path.
	// Old streams derive from the pre-mutation FocusKey (result.OldFocusKey;
	// nil = grid), new streams from focusKey (the requested target; nil = grid).
	c.driveFocusDeltas(ctx, result.SessionID, result.CharLocationID, result.OldFocusKey, focusKey, []ulid.ULID{connectionID})

	return result, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestSetConnectionFocusDrivesPerConnectionDeltas ./internal/grpc/focus/`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

Run: `task lint:go`
Commit: `feat(focus): coordinator drives per-connection deltas on SetConnectionFocus (holomush-66228)`

### Task 3: Boundary + error-logging coverage

**Files:**

- Modify: `internal/grpc/focus/focus_delta_test.go` (add boundary tests)

- [ ] **Step 1: Write the boundary tests**

Append to `internal/grpc/focus/focus_delta_test.go`:

```go
func TestAutoFocusOnJoinNilSenderIsNoOp(t *testing.T) {
	charID := ulid.Make()
	sceneID := ulid.Make()
	locID := ulid.Make()
	connID := ulid.Make()
	sessions := map[string]*session.Info{
		"sess-1": {
			ID: "sess-1", CharacterID: charID, LocationID: locID,
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
			},
		},
	}
	coord, _ := newTestCoordinator(t, sessions) // connectionSender stays nil
	require.NoError(t, coord.sessionStore.AddConnection(context.Background(), &session.Connection{
		ID: connID, SessionID: "sess-1", ClientType: "terminal",
	}))
	// Must not panic; focus mutation still succeeds.
	resp, err := coord.AutoFocusOnJoin(context.Background(), charID, sceneID)
	require.NoError(t, err)
	assert.Len(t, resp.FocusedConnectionIDs, 1)
}

func TestDriveFocusDeltasContinuesPastSendError(t *testing.T) {
	charID := ulid.Make()
	sceneID := ulid.Make()
	locID := ulid.Make()
	connID := ulid.Make()
	sessions := map[string]*session.Info{
		"sess-1": {
			ID: "sess-1", CharacterID: charID, LocationID: locID,
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
			},
		},
	}
	coord, _ := newTestCoordinator(t, sessions)
	scene := sceneID.String()
	// Fail the IC add; the OOC add and the location remove must still be attempted.
	cs := &captureConnSender{errOn: map[string]error{
		"events.main.scene." + scene + ".ic": errors.New("CONNECTION_NOT_REGISTERED"),
	}}
	coord.connectionSender = cs
	coord.gameID = "main"
	require.NoError(t, coord.sessionStore.AddConnection(context.Background(), &session.Connection{
		ID: connID, SessionID: "sess-1", ClientType: "terminal",
	}))

	resp, err := coord.AutoFocusOnJoin(context.Background(), charID, sceneID)
	require.NoError(t, err, "delivery failure MUST NOT fail the focus mutation")
	require.Len(t, resp.FocusedConnectionIDs, 1)
	// All three deltas attempted despite the IC failure.
	assert.Len(t, cs.calls, 3, "one failing send MUST NOT abort the remaining sends")
}

func TestAutoFocusOnJoinSessionNotFoundDrivesNoDeltas(t *testing.T) {
	coord, _ := newTestCoordinator(t, map[string]*session.Info{})
	cs := &captureConnSender{}
	coord.connectionSender = cs
	coord.gameID = "main"
	resp, err := coord.AutoFocusOnJoin(context.Background(), ulid.Make(), ulid.Make())
	require.NoError(t, err)
	assert.Equal(t, uint32(0), resp.TotalConnectionCount)
	assert.Empty(t, cs.calls, "no session → no deltas")
}
```

- [ ] **Step 2: Run + verify pass**

Run: `task test -- -run 'TestAutoFocusOnJoinNilSenderIsNoOp|TestDriveFocusDeltasContinuesPastSendError|TestAutoFocusOnJoinSessionNotFoundDrivesNoDeltas' ./internal/grpc/focus/`
Expected: PASS (logic from Tasks 1-2 already satisfies these).

- [ ] **Step 3: Coverage check**

Run: `task test:cover -- ./internal/grpc/focus/`
Expected: focus package ≥80% (target 90%); `focus_delta.go` branches covered.

- [ ] **Step 4: Commit**

Commit: `test(focus): boundary + error-logging coverage for driveFocusDeltas (holomush-66228)`

---

## Phase 2: Remove the binary-host ConnectionSender seam

### Task 4: Delete the binary handler delta loops

**Files:**

- Modify: `internal/plugin/goplugin/host_service.go` (delete `:253-270` and `:312-335`)
- Modify: `internal/plugin/goplugin/host_service_test.go` (convert delta tests to delegation)

- [ ] **Step 1: Rewrite the binary handler delta tests as delegation tests**

In `internal/plugin/goplugin/host_service_test.go`, replace `TestAutoFocusOnJoin_DrivesSubscriptionDeltas` and `TestSetConnectionFocus_DrivesSubscriptionDeltas` with delegation assertions that the handler returns the coordinator's wire response (drop the `stubConnectionSender` / `newTestServerWithConnSender` usage). Example for AutoFocusOnJoin:

```go
func TestAutoFocusOnJoin_DelegatesToCoordinator(t *testing.T) {
	connID := ulid.Make()
	fc := &stubCoordinator{
		autoFocusResult: focus.AutoFocusOnJoinResponse{
			SessionID:            "sess-1",
			FocusedConnectionIDs: []ulid.ULID{connID},
			TotalConnectionCount: 1,
		},
	}
	srv := newTestServer(fc, nil) // newTestServer(fc focus.Coordinator, hr plugins.HistoryReader); nil HistoryReader
	charID := ulid.Make()
	charIDBuf := charID.Bytes()
	resp, err := srv.AutoFocusOnJoin(context.Background(), &pluginv1.PluginHostServiceAutoFocusOnJoinRequest{
		CharacterId: charIDBuf[:],
		SceneId:     ulid.Make().Bytes(),
	})
	require.NoError(t, err)
	assert.Equal(t, uint32(1), resp.GetTotalConnectionCount())
	require.Len(t, resp.GetFocusedConnectionIds(), 1)
}
```

(`newTestServer(fc, nil)` is the existing 2-arg constructor — `(focus.Coordinator, plugins.HistoryReader)` — at `internal/plugin/goplugin/host_service_test.go`; it does not take a ConnectionSender, which is exactly the point. Keep the existing wire-marshal tests for `FocusedConnectionIds`/`SkippedConnectionIds`/`FailedConnectionIds`/reason mapping unchanged.)

- [ ] **Step 2: Run to verify failure**

Run: `task test -- ./internal/plugin/goplugin/`
Expected: FAIL/compile — `stubConnectionSender` now unused, or delegation test still sees deltas driven.

- [ ] **Step 3: Delete the delta blocks**

In `internal/plugin/goplugin/host_service.go`:

- Delete the SetConnectionFocus delta block (the `cs := s.host.ConnectionSender()` ... `SendToConnection` loop, lines `253-270`), leaving the `result, err := fc.SetConnectionFocus(...)` call and the `resp` echo.
- Delete the AutoFocusOnJoin delta block (lines `312-335`, the `cs := s.host.ConnectionSender()` ... loop), leaving `r, err := fc.AutoFocusOnJoin(...)` and the wire-response marshal.
- Remove the now-unused `focus` import if nothing else in the file references it (run `task lint:go` to confirm).

- [ ] **Step 4: Remove the stub double**

Delete `stubConnectionSender`, `connSendCall`, and `newTestServerWithConnSender` from `host_service_test.go` if no longer referenced.

- [ ] **Step 5: Run + commit**

Run: `task test -- ./internal/plugin/goplugin/` → PASS. Then `task lint:go`.
Commit: `refactor(plugin): remove binary-host focus-delta driving; delegate to coordinator (holomush-66228)`

### Task 5: Delete the binary-host ConnectionSender API + subsystem wiring

**Files:**

- Modify: `internal/plugin/goplugin/host.go` (delete field + 3 methods)
- Modify: `internal/plugin/setup/subsystem.go` (delete field + wiring; keep StreamRegistry)
- Modify: `cmd/holomush/core.go` (no `ConnectionSender` to remove — confirm)

- [ ] **Step 1: Delete from `host.go`**

Remove: the `connectionSender focus.ConnectionSender` field; `WithConnectionSender`; `SetConnectionSender`; and the `ConnectionSender()` accessor. KEEP `gameID` and `GameID()`.

- [ ] **Step 2: Delete from `subsystem.go`**

Remove `PluginSubsystemConfig.ConnectionSender` (field at `:121`) and the wiring block at `:299-307`:

```go
	if s.cfg.ConnectionSender != nil {
		hostOpts = append(hostOpts, goplugin.WithConnectionSender(s.cfg.ConnectionSender))
	}
```

KEEP the `hostfunc.WithStreamRegistry(...)` wiring (`:200-203`) — the Lua hostfunc session-stream contribution still needs the registry (INV-FS-7).

- [ ] **Step 3: Remove the harness ConnectionSender chain (compile-critical ripple)**

The integration harness also feeds the binary host a `ConnectionSender`, so the struct-field deletion ripples there — `plugins.go` will not compile otherwise:

- `internal/testsupport/integrationtest/plugins.go`: delete the `connectionSender focus.ConnectionSender` field from the `pluginDeps` struct, and delete `ConnectionSender: d.connectionSender,` from the `pluginsetup.PluginSubsystemConfig{...}` literal in `startPlugins`.
- `internal/testsupport/integrationtest/harness.go`: delete `connectionSender: connectionSender,` from the `pluginDeps{...}` literal passed to `startPlugins`, and delete the `connectionSender := holoGRPC.NewConnectionSenderAdapter(streamRegistry)` local declaration. Keep `streamRegistry` (still feeds the coordinator + `CoreServer`'s `WithStreamRegistry`). The coordinator regains a `ConnectionSender` in Task 8.

`cmd/holomush/core.go` never set `ConnectionSender` (it only sets `StreamRegistry:` — confirmed `core.go:418`), so it needs no change.

- [ ] **Step 4: Build + test (incl. the integration build tag)**

Run: `task build` then `task test -- ./internal/plugin/...` → PASS.
Then verify the integration-tagged harness compiles — `plugins.go`/`harness.go` carry `//go:build integration`, which `task build`/`task test` do NOT compile (per repo memory `lint_go_misses_integration_tag`): `task test:int -- ./test/integration/scenes/...` (Docker). Because Task 5's harness edit (removing the binary-host ConnectionSender) and Task 8's coordinator rewire are compile-coupled, land Tasks 5 and 8 together when running the integration tier locally — or expect the integration build to be red on the Task 5 commit alone until Task 8.
Expected: `rg -n 'ConnectionSender' cmd/holomush/ internal/plugin/` → zero matches.

- [ ] **Step 5: Commit**

Commit: `refactor(plugin): delete binary-host ConnectionSender seam (holomush-66228)`

---

## Phase 3: Unify wiring + meta-tests + Lua parity unit

### Task 6: FocusStreamCoordinatorOptions helper (anti-drift)

**Files:**

- Create: `internal/grpc/focus_wiring.go`
- Create: `internal/grpc/focus_wiring_test.go`

- [ ] **Step 1: Write the failing test (INV-FS-5)**

Create `internal/grpc/focus_wiring_test.go`. The test asserts the helper yields exactly two options AND that both registry-derived adapters route to the **same** registry — without constructing a `focus.Coordinator` (the in-memory session memstore was removed from this codebase, so a real coordinator needs Docker; this test is store-free):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// INV-FS-5: the StreamSender and ConnectionSender produced for one coordinator
// MUST target the same SessionStreamRegistry. We prove it by routing through
// both registry-derived adapters and asserting both reach the one registry.
func TestFocusStreamCoordinatorOptionsShareOneRegistry(t *testing.T) {
	reg := NewSessionStreamRegistry()
	opts := FocusStreamCoordinatorOptions(reg)
	require.Len(t, opts, 2, "helper yields exactly StreamSender + ConnectionSender options")

	sessionID := "sess-x"
	connID := ulid.Make()

	connCh := make(chan sessionStreamUpdate, 1)
	reg.RegisterConnection(sessionID, connID, connCh)
	require.NoError(t, NewConnectionSenderAdapter(reg).SendToConnection(sessionID, connID, "events.main.scene.s.ic", true))
	assert.Len(t, connCh, 1, "ConnectionSender adapter MUST reach the same registry")

	sessCh := make(chan sessionStreamUpdate, 1)
	reg.Register(sessionID, sessCh)
	require.NoError(t, NewStreamSenderAdapter(reg).Send(sessionID, "location:x", true, focus.ReplayModeFromCursor))
	assert.Len(t, sessCh, 1, "StreamSender adapter MUST reach the same registry")
}
```

> Implementer note: confirm the exact in-package names against `internal/grpc/stream_registry.go` — `sessionStreamUpdate`, `Register`/`RegisterConnection` signatures, `StreamSenderAdapter.Send`'s signature, and the `focus.ReplayModeFromCursor` constant (use a `FromCursor` mode so `Send` does not hit the INV-FS-6 replay-mode rejection). Add the `focus` import if `ReplayModeFromCursor` is referenced. The decisive property is "both adapters built from `reg` reach `reg`."

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run TestFocusStreamCoordinatorOptionsShareOneRegistry ./internal/grpc/`
Expected: FAIL — `FocusStreamCoordinatorOptions` undefined.

- [ ] **Step 3: Create the helper**

Create `internal/grpc/focus_wiring.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import "github.com/holomush/holomush/internal/grpc/focus"

// FocusStreamCoordinatorOptions is the SINGLE assembly point for the two
// registry-derived focus.Coordinator senders (INV-FS-4): the session-wide
// StreamSender and the per-Connection ConnectionSender, both backed by the
// same SessionStreamRegistry. Production (cmd/holomush) and the integration
// harness MUST build their coordinator stream wiring through this helper rather
// than hand-rolling NewStreamSenderAdapter + NewConnectionSenderAdapter, so the
// harness is a faithful production mirror by construction.
func FocusStreamCoordinatorOptions(reg *SessionStreamRegistry) []focus.CoordinatorOption {
	return []focus.CoordinatorOption{
		focus.WithStreamSender(NewStreamSenderAdapter(reg)),
		focus.WithConnectionSender(NewConnectionSenderAdapter(reg)),
	}
}
```

- [ ] **Step 4: Run + commit**

Run: `task test -- -run TestFocusStreamCoordinatorOptionsShareOneRegistry ./internal/grpc/` → PASS. `task lint:go`.
Commit: `feat(grpc): FocusStreamCoordinatorOptions single-assembly helper (holomush-66228)`

### Task 7: Wire the coordinator's ConnectionSender in production

**Files:**

- Modify: `cmd/holomush/sub_grpc.go:444-450` (focusCoordOpts)

- [ ] **Step 1: Replace the WithStreamSender block**

In `cmd/holomush/sub_grpc.go`, replace:

```go
	if s.cfg.StreamRegistry != nil {
		focusCoordOpts = append(
			focusCoordOpts,
			holoFocus.WithStreamSender(holoGRPC.NewStreamSenderAdapter(s.cfg.StreamRegistry)),
		)
	}
```

with:

```go
	focusCoordOpts = append(focusCoordOpts, holoFocus.WithGameID(s.cfg.EventBus.GameID))
	if s.cfg.StreamRegistry != nil {
		focusCoordOpts = append(focusCoordOpts, holoGRPC.FocusStreamCoordinatorOptions(s.cfg.StreamRegistry)...)
	}
```

- [ ] **Step 2: Build + confirm no ConnectionSender references remain**

Run: `task build` then `rg -n 'ConnectionSender' cmd/holomush/`
Expected: build OK; zero `ConnectionSender` matches in `cmd/holomush/`.

- [ ] **Step 3: Commit**

Commit: `fix(focus): wire coordinator ConnectionSender in production — fixes binary scene-join live delivery (holomush-66228)`

### Task 8: Rewire the integration harness

**Files:**

- Modify: `internal/testsupport/integrationtest/harness.go` (NewCoordinator opts + drop binary-host ConnectionSender)

- [ ] **Step 1: Update the harness coordinator build**

In `harness.go`, the `WithFocusDelivery` block builds the coordinator via a `focus.NewCoordinator(...)` call whose last option is `focus.WithStreamSender(...)` (it passes `holoGRPC.NewStreamSenderAdapter(streamRegistry)`, possibly via a `streamSender` local). Refactor to a slice so the helper can be spread, copying the other options **verbatim** from the current call and replacing only the `WithStreamSender(...)` option:

```go
		coordOpts := []focus.CoordinatorOption{
			// Copy the existing non-stream options verbatim from the current
			// NewCoordinator(...) call (WithSessionStore / WithKindPolicy /
			// WithGameSettings / WithPlayerPreferences / WithStreamContributor).
			focus.WithGameID(bus.Bus.GameID()), // SAME game id startPlugins is given (harness passes gameID: bus.Bus.GameID()), so delta stream names match emitted subjects
		}
		coordOpts = append(coordOpts, holoGRPC.FocusStreamCoordinatorOptions(streamRegistry)...)
		focusCoord, focusErr = focus.NewCoordinator(coordOpts...)
```

`bus` is already in scope here (the `pluginDeps{... gameID: bus.Bus.GameID()}` literal proves it). If a `streamSender :=` local existed only for the old `WithStreamSender` option, delete it — `FocusStreamCoordinatorOptions` builds the StreamSender internally. (The binary-host `connectionSender` chain was already removed in Task 5 Step 3.)

- [ ] **Step 2: Run the binary scene integration regression net (INV-FS-2)**

Run: `task test:int -- ./test/integration/scenes/...`
Expected: PASS — `real_scene_join_subscription_test.go`, `auto_focus_on_join_terminal_only_test.go`, `multi_connection_visibility_test.go` all green via the coordinator-driven path. (Requires Docker + `task plugin:build-all`, which `task test:int` runs.)

- [ ] **Step 3: Commit**

Commit: `test(harness): drive focus deltas via coordinator; drop binary-host ConnectionSender wiring (holomush-66228)`

### Task 9: Lua parity unit test + comment corrections

**Files:**

- Create: `internal/plugin/lua/focus_ops_adapter_test.go`
- Modify: `internal/plugin/lua/focus_ops_adapter.go` (comments only)

- [ ] **Step 1: Write the Lua adapter delegation test**

Create `internal/plugin/lua/focus_ops_adapter_test.go`. The adapter is a thin delegator; the delta-firing itself is proven by Task 1 (coordinator unit) and Task 13 (full gopher-lua integration). This unit test proves the Lua adapter forwards to the coordinator and translates the response — using an interface-embedding stub `focus.Coordinator` (no session store, so no Docker):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/grpc/focus"
)

// stubCoordinator embeds the interface and overrides only AutoFocusOnJoin, so
// it satisfies focus.Coordinator without implementing all ~10 methods.
type stubCoordinator struct {
	focus.Coordinator
	gotChar, gotScene ulid.ULID
	resp              focus.AutoFocusOnJoinResponse
}

func (s *stubCoordinator) AutoFocusOnJoin(_ context.Context, charID, sceneID ulid.ULID) (focus.AutoFocusOnJoinResponse, error) {
	s.gotChar, s.gotScene = charID, sceneID
	return s.resp, nil
}

func TestLuaAdapterAutoFocusOnJoinDelegatesAndTranslates(t *testing.T) {
	charID := ulid.Make()
	sceneID := ulid.Make()
	focusedConn := ulid.Make()
	stub := &stubCoordinator{resp: focus.AutoFocusOnJoinResponse{
		FocusedConnectionIDs: []ulid.ULID{focusedConn},
		TotalConnectionCount: 1,
	}}
	adapter := &coordinatorFocusOpsAdapter{c: stub}

	focused, skipped, failed, total, err := adapter.AutoFocusOnJoin(context.Background(), charID, sceneID)
	require.NoError(t, err)
	assert.Equal(t, charID, stub.gotChar, "adapter MUST forward characterID to the coordinator")
	assert.Equal(t, sceneID, stub.gotScene, "adapter MUST forward sceneID to the coordinator")
	assert.Equal(t, []ulid.ULID{focusedConn}, focused)
	assert.Empty(t, skipped)
	assert.Empty(t, failed)
	assert.Equal(t, uint32(1), total)
}
```

> Implementer note: confirm `coordinatorFocusOpsAdapter`'s field name (`c`) and the `AutoFocusOnJoin` tuple return order against `internal/plugin/lua/focus_ops_adapter.go`. The interface-embedding stub pattern requires `focus.Coordinator` to be an interface (confirmed — `internal/grpc/focus/coordinator.go:44`).

- [ ] **Step 2: Run to verify it passes**

Run: `task test -- -run TestLuaAdapterAutoFocusOnJoinDelegatesAndTranslates ./internal/plugin/lua/`
Expected: PASS (the adapter already delegates; this locks the forward + translation).

- [ ] **Step 3: Correct the stale comments**

In `internal/plugin/lua/focus_ops_adapter.go`:

- Lines 44-49 (`SetConnectionFocus` doc): remove the claim "the Lua hostfunc path does not need stream deltas (Lua plugins react to focus events via JetStream, not via the RPC return value)." Replace with: "Per-connection subscription deltas are driven inside `focus.Coordinator` (INV-FS-1), so the adapter needs only to delegate; the dropped `oldFocusKey` return value is not needed here."
- Lines 55-58 (`AutoFocusOnJoin` doc): keep the "does not need the full struct" tuple-translation note, but remove any implication that Lua skips delta delivery.

- [ ] **Step 4: Commit**

Commit: `test(lua): parity lock — Lua focus adapter drives coordinator deltas (holomush-66228)`

### Task 10: Invariant meta-tests

**Files:**

- Create: `test/meta/focus_delta_gate_test.go`

- [ ] **Step 1: Write the meta-tests**

Create `test/meta/focus_delta_gate_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test's CWD to the module root (go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func nonTestGoFilesContaining(t *testing.T, root, needle string) []string {
	t.Helper()
	var hits []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(b), needle) {
			rel, _ := filepath.Rel(root, path)
			hits = append(hits, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

// INV-FS-1: per-connection focus-delta delivery is driven ONLY inside
// internal/grpc/focus. The interface decl + the registry impl/adapter in
// internal/grpc are the only other legitimate occurrences.
func TestSendToConnectionConfinedToFocusAndRegistry(t *testing.T) {
	root := repoRoot(t)
	allowed := map[string]bool{
		"internal/grpc/stream_registry.go":            true, // impl + ConnectionSenderAdapter
		"internal/grpc/focus/subscription_router.go":  true, // ConnectionSender interface decl
		"internal/grpc/focus/focus_delta.go":          true, // the sole driver (the gate)
	}
	for _, f := range nonTestGoFilesContaining(t, root, "SendToConnection(") {
		if !allowed[f] {
			t.Errorf("INV-FS-1 violation: SendToConnection( used outside the focus gate: %s", f)
		}
	}
}

// INV-FS-4: the registry-derived adapter pair is assembled ONLY in the
// FocusStreamCoordinatorOptions helper (constructors are defined in
// stream_registry.go).
func TestFocusAdapterPairAssembledOnlyInHelper(t *testing.T) {
	root := repoRoot(t)
	allowed := map[string]bool{
		"internal/grpc/stream_registry.go": true, // constructor definitions
		"internal/grpc/focus_wiring.go":    true, // the single assembly point
	}
	for _, needle := range []string{"NewConnectionSenderAdapter(", "NewStreamSenderAdapter("} {
		for _, f := range nonTestGoFilesContaining(t, root, needle) {
			if !allowed[f] {
				t.Errorf("INV-FS-4 violation: %s used outside FocusStreamCoordinatorOptions: %s", needle, f)
			}
		}
	}
}
```

- [ ] **Step 2: Run + verify pass**

Run: `task test -- ./test/meta/`
Expected: PASS (Tasks 4-8 removed all out-of-gate occurrences).

- [ ] **Step 3: Commit**

Commit: `test(meta): lock focus-delta gate-on-common-path (INV-FS-1, INV-FS-4) (holomush-66228)`

---

## Phase 4: Full test-Lua-plugin integration

### Task 11: Harness `WithExtraPluginDir` option

**Files:**

- Modify: `internal/testsupport/integrationtest/options.go` (add option)
- Modify: `internal/testsupport/integrationtest/harness.go` (stage the extra dir before plugin subsystem start)

> Grounding note: plugin staging lives in `plugins.go::startPlugins`: it calls `assemblePluginsDir(pluginsDst, repoPluginsSrcDir(), buildDir)` then the subsystem `LoadAll`-scans `pluginsDst`. `copyTree(src, dst)` is the existing recursive copy primitive (overlay semantics). There is no out-of-tree-plugin hook today. This task adds one by copying extra dirs into `pluginsDst` after `assemblePluginsDir`.

- [ ] **Step 1: Add the option (new `options.go`)**

Create `internal/testsupport/integrationtest/options.go` (the option type is `StartOption` and the config struct is `startConfig`, both already used in `plugins.go`):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

// WithExtraPluginDir stages an additional plugin directory (e.g. a test-only
// Lua fixture under test/integration/.../testdata/lua/<name>) into the plugin
// load path so the real plugin subsystem loads it alongside the in-tree
// plugins. Used by focus runtime-symmetry tests that need a Lua plugin which
// calls the auto_focus_on_join hostfunc. dir is resolved relative to the test's
// package directory (Go runs tests with CWD = package dir).
func WithExtraPluginDir(dir string) StartOption {
	return func(c *startConfig) { c.extraPluginDirs = append(c.extraPluginDirs, dir) }
}
```

Add `extraPluginDirs []string` to the `startConfig` struct (in `harness.go`, where `withPlugins`/`pluginConfigOverrides` live).

- [ ] **Step 2: Thread + stage the extra dirs**

1. Add an `extraPluginDirs []string` field to the `pluginDeps` struct in `plugins.go` (next to `pluginConfigOverrides`).
2. In `harness.go`'s `startPlugins(t, ctx, pluginDeps{...})` literal, pass `extraPluginDirs: cfg.extraPluginDirs,`.
3. In `plugins.go::startPlugins`, after the existing `assemblePluginsDir(pluginsDst, repoPluginsSrcDir(), buildDir)` call, stage each extra dir into `pluginsDst` using the existing `copyTree` primitive:

```go
	for _, extra := range d.extraPluginDirs {
		abs, err := filepath.Abs(extra)
		require.NoError(t, err, "startPlugins: resolve extra plugin dir")
		dstSub := filepath.Join(pluginsDst, filepath.Base(abs))
		require.NoError(t, copyTree(abs, dstSub), "startPlugins: stage extra plugin dir")
	}
```

The extra plugin is then discovered by the same `Manager.LoadAll` scan and shares the harness coordinator/registry wiring (the loaded plugin's `auto_focus_on_join` hostfunc reaches the harness's `focusCoord`, since `ConfigureFocusDeps` wires the coordinator into every loaded plugin host).

- [ ] **Step 3: Build the harness package**

Run: `task test:int -- -run TestNothing ./test/integration/scenes/` (compiles the harness + suite without running specs)
Expected: compiles clean.

- [ ] **Step 4: Commit**

Commit: `test(harness): WithExtraPluginDir for out-of-tree test plugins (holomush-66228)`

### Task 12: Test-only Lua plugin fixture

**Files:**

- Create: `test/integration/scenes/testdata/lua/focus_join/plugin.yaml`
- Create: `test/integration/scenes/testdata/lua/focus_join/plugin.lua`

- [ ] **Step 1: Write the manifest**

Create `test/integration/scenes/testdata/lua/focus_join/plugin.yaml`:

```yaml
# SPDX-License-Identifier: Apache-2.0
name: test-focus-join
version: 1.0.0
type: lua
resource_types: []
requires: []
provides: []
commands:
  - name: luafocusjoin
    capabilities:
      - { action: write, resource: scene, scope: self }
```

> Implementer note: validate against `schemas/plugin.schema.json` via `task lint`. If the schema requires additional fields (e.g. `description`), add them. The capability shape mirrors `plugins/core-scenes` focus commands — confirm the exact `{action,resource,scope}` the dispatcher expects so the harness's allow-all ABAC admits the command.

- [ ] **Step 2: Write the Lua handler**

Create `test/integration/scenes/testdata/lua/focus_join/plugin.lua`:

```lua
-- test-focus-join: registers a command that auto-focuses the issuing
-- character's connections onto a scene, exercising the real gopher-lua VM
-- path into holomush.auto_focus_on_join (focus runtime-symmetry test).
-- Command form: luafocusjoin <character_id_ulid> <scene_id_ulid>
holomush.register_command("luafocusjoin", function(ctx, args)
  local char_id, scene_id = string.match(args, "(%S+)%s+(%S+)")
  holomush.auto_focus_on_join(char_id, scene_id)
  return "focused"
end)
```

> Implementer note: confirm the exact Lua hostfunc names + argument encoding against `internal/plugin/hostfunc/stdlib_focus.go` (the `auto_focus_on_join` registration) and the command-registration hostfunc used by in-tree Lua plugins (`plugins/core-*/main.lua`). Adjust `register_command`/`auto_focus_on_join` call shapes to match the real API; the REQUIRED behavior is "a Lua command calls auto_focus_on_join with the joining character + scene."

- [ ] **Step 3: Commit**

Commit: `test(scenes): test-only Lua focus-join plugin fixture (holomush-66228)`

### Task 13: Lua parity integration test (full gopher-lua path)

**Files:**

- Create: `test/integration/scenes/lua_focus_parity_test.go`

- [ ] **Step 1: Write the integration test**

Create `test/integration/scenes/lua_focus_parity_test.go` (build-tagged `//go:build integration`). Model the seed/Subscribe/assert shape on the existing `auto_focus_on_join_terminal_only_test.go` (binary equivalent). The test:

1. `integrationtest.Start(t, integrationtest.WithFocusDelivery(), integrationtest.WithExtraPluginDir("testdata/lua/focus_join"))`.
2. Seed an actor/character at a location, an active session with a terminal connection, and a `FocusMembership` for the target scene (so `AutoFocusOnJoin` does not fail `membership_absent`) — reuse the same seed helpers the binary scene tests use.
3. Open a Subscribe stream for the terminal connection.
4. Run the Lua command `luafocusjoin <char_id> <scene_id>` through the harness command path.
5. Assert the connection's live subscription gained the scene IC stream — e.g. publish/await a scene IC event and assert it is delivered to that connection, exactly as the binary `auto_focus_on_join_terminal_only_test.go` asserts.

> Implementer note: copy the Subscribe + seed + await helpers verbatim from `auto_focus_on_join_terminal_only_test.go` (and `real_scene_join_subscription_test.go`) — those are the canonical harness helpers for "a connection receives a live scene pose after focus." The ONLY differences from the binary test are: (a) the focus is triggered by running the Lua command rather than calling the binary `AutoFocusOnJoin` RPC, and (b) `WithExtraPluginDir` loads the fixture. This is INV-FS-3's full-fidelity lock: same assertion, Lua-driven.

- [ ] **Step 2: Run the integration test**

Run: `task test:int -- ./test/integration/scenes/...`
Expected: PASS (binary regression net + the new Lua parity test).

- [ ] **Step 3: Commit**

Commit: `test(scenes): full gopher-lua focus-delta parity integration test (holomush-66228)`

---

## Phase 5: Documentation

### Task 14: Update contributor docs + code comments

**Files:**

- Modify: `site/src/content/docs/contributing/how-to/integration-tests.md`
- Modify: `internal/plugin/setup/subsystem.go` (doc-comment block `:104-121`)

- [ ] **Step 1: Update integration-tests.md**

In the `WithFocusDelivery` section, state that the harness builds the coordinator's focus-delivery senders via `holoGRPC.FocusStreamCoordinatorOptions` (the same helper production uses — a faithful mirror), and document `WithExtraPluginDir` + the `testdata/lua/focus_join` fixture as the mechanism for the Lua runtime-symmetry test.

- [ ] **Step 2: Update the subsystem doc-comment**

In `internal/plugin/setup/subsystem.go`, update the `:104-121` doc-comment block to state that per-connection focus deltas are driven inside `focus.Coordinator` for both runtimes (the plugin host no longer carries a `ConnectionSender`); the plugin host still receives the `StreamRegistry` for the Lua hostfunc session-stream contribution.

- [ ] **Step 3: Markdown lint + commit**

Run: `rumdl check site/src/content/docs/contributing/how-to/integration-tests.md` (use the CI-pinned rumdl for docs-heavy edits per repo memory).
Commit: `docs(scenes): focus-delta coordinator unification — integration-tests + subsystem comments (holomush-66228)`

---

## Post-implementation checklist

- [ ] `task lint:go` clean (no widened `.golangci.yaml`; line-scoped `//nolint` only).
- [ ] `task test:cover -- ./internal/grpc/focus/ ./internal/grpc/ ./internal/plugin/goplugin/ ./internal/plugin/lua/` — each ≥80%.
- [ ] `task test:int -- ./test/integration/scenes/...` green (Docker; binary regression net + Lua parity).
- [ ] `task test -- ./test/meta/` green (INV-FS-1, INV-FS-4).
- [ ] `rg -n 'ConnectionSender' cmd/holomush/ internal/plugin/` shows zero binary-host references.
- [ ] `task pr-prep` green.
- [ ] ADR captured (auto-fire `capture-adrs` after plan-review READY): the coordinator-as-delta-driver decision, superseding the nki4-era RPC-handler placement.
- [ ] Spec + plan + ADR landed on `main` (docs-first PR) before subagent-driven implementation dispatch.
<!-- adr-capture: sha256=443f9a19cb40f9c8; session=cli; ts=2026-05-29T11:08:13Z; adrs=holomush-jfw0k -->
