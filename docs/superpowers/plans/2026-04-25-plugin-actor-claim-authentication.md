# Plugin Actor-Claim Authentication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the host-trust gap on plugin `EmitEvent` (binary plugins can forge actor metadata claims) by adding (a) upstream actor-stamping at dispatch boundaries, (b) a manifest-declared opt-in (`actor_kinds_claimable`) enforced symmetrically across Lua and binary runtimes, and (c) a host-issued token mechanism that authenticates the actor input on the binary gRPC `EmitEvent` boundary.

**Architecture:** Three coordinated layers — upstream stamping in `subscriber.go::deliverAsync` and `dispatcher.go::dispatchToPlugin` activates the actor-metadata channel; manifest gate in `event_emitter.go::Emit` enforces operator-declared claim policy universally; token store in `internal/plugin/goplugin/` authenticates the actor input on the binary path so plugins can't forge identity claims. Cause-origin cascade semantic preserved (per `internal/core/event.go:188`); ActorSystem re-anchored at token issuance (plugins never speak as host's system identity); `[system]` rejected at manifest load. Loud-error failure mode throughout — no silent fallback. Pre-1.0 clean break: 3 in-tree plugin manifests migrated same PR.

**Tech Stack:** Go 1.22+, `crypto/rand` (token entropy), `google.golang.org/grpc` + `metadata` (header carrier), `samber/oops` (error wrapping), `samber/lo` (already imported), `gopkg.in/yaml.v3` (manifest), `github.com/stretchr/testify`, `Taskfile.yaml` (CI tasks). Existing patterns from `internal/web/handler.go:381` (line-scoped `//nolint:wrapcheck`) and `Taskfile.yaml:366,386` (lint task pattern).

**Spec:** [`docs/superpowers/specs/2026-04-25-plugin-actor-claim-authentication-design.md`](../specs/2026-04-25-plugin-actor-claim-authentication-design.md)

**Bead:** `holomush-ec22.1`

**Working dir:** `/Users/sean/Code/github.com/holomush/.worktrees/ec22.1` (jj workspace; parent commit = `main@origin` = PR #269 `plzx`).

**VCS:** jj-colocated repo. Cadence per the previous PR's lessons (memory `feedback_jj_empty_wc_before_task`): **new-first, then edit, then describe**. Each task opens with `jj new` Step 0; each task closes with `jj describe` (no trailing `jj new` — the next task starts with its own `jj new`). Tip detection at push time uses `jj log -r 'main@origin..@ & ~empty()' --no-pager -T 'change_id.short() ++ "\n"' | head -1` (memory `writing-plans-tip-detection-jj-log-r-main` — `head -1`, not `tail -1`, because jj defaults to newest-first).

---

## File Structure

| File | Action | Responsibility |
| ---- | ------ | -------------- |
| `internal/plugin/subscriber.go` | Modify (~5 LoC) | Move `core.WithActor(...)` from line 137 to before line 112 (`s.host.DeliverEvent`). Rename ctx variable to `dispatchCtx`. |
| `internal/command/dispatcher.go` | Modify (~5 LoC) | Move `core.WithActor(ctx, ActorCharacter:exec.CharacterID())` from line 370 to before line 310 (`d.pluginDeliverer.DeliverCommand`). Rename ctx variable to `dispatchCtx`. |
| `internal/plugin/manifest.go` | Modify (~30 LoC) | Add `ActorKindsClaimable []string` field, validation rules (must contain plugin; must not contain system; only known kinds; deduplication), and helper `Manifest.DeclaresActorKindClaimable(kind core.ActorKind) bool`. |
| `internal/plugin/event_emitter.go` | Modify (~10 LoC) | Add manifest gate after namespace check at lines 99-111: `if !manifest.DeclaresActorKindClaimable(actor.Kind) { return loud-error }`. |
| `internal/plugin/goplugin/emit_token_store.go` | Create (~150 LoC) | Token store struct + Issue/Lookup/Revoke/Run/Close methods. Background sweeper goroutine. |
| `internal/plugin/goplugin/host.go` | Modify (~30 LoC) | Add `tokenStore *emitTokenStore` field; construction; `Close()` calls `tokenStore.Close()`; `DeliverEvent` and `DeliverCommand` issue tokens (with ActorSystem re-anchor) and defer Revoke. |
| `internal/plugin/goplugin/host_service.go` | Modify (~25 LoC) | `EmitEvent` reads `x-holomush-emit-token` header, looks up in tokenStore, uses stored actor verbatim; ignores `x-holomush-actor-kind` / `x-holomush-actor-id` headers. Loud-error on missing/unknown token. |
| `plugins/core-scenes/plugin.yaml` | Modify (+1 line) | Declare `actor_kinds_claimable: [plugin, character]`. |
| `plugins/core-communication/plugin.yaml` | Modify (+1 line) | Declare `actor_kinds_claimable: [plugin, character]`. |
| `plugins/echo-bot/plugin.yaml` | Modify (+1 line) | Declare `actor_kinds_claimable: [plugin, character]`. |
| `Taskfile.yaml` | Modify (~30 LoC) | Add `lint:plugin-manifests` and `lint:docs-symmetry` task targets. Wire into `lint` aggregate. |
| `cmd/lint-plugin-manifests/main.go` | Create (~70 LoC) | Go program implementing the heuristic from spec criterion 8 (parses `plugins.Manifest` directly; no shell/yq dependency). |
| `AGENTS.md` | Modify (+~25 LoC) | Add "Plugin Runtime Symmetry" subsection delimited by `<!-- BEGIN: plugin-runtime-symmetry -->` and `<!-- END: plugin-runtime-symmetry -->`. |
| `CLAUDE.md` | Modify (+~25 LoC) | Same subsection, byte-identical between the two files (verified by `lint:docs-symmetry`). |
| `site/docs/extending/actor-kinds-claimable.md` | Create (~80 LoC) | Plugin-author-facing doc for the new manifest field. |
| `internal/plugin/subscriber_test.go` | Create (~100 LoC) | Unit tests for §5.8.1 (subscriber upstream stamping). |
| `internal/command/dispatcher_test.go` | Modify (~50 LoC) | Unit tests for §5.8.2 (dispatcher upstream stamping). |
| `internal/plugin/manifest_test.go` | Modify (~80 LoC) | Unit tests for §5.2 (manifest validation). |
| `internal/plugin/event_emitter_test.go` | Modify (~80 LoC) | Unit tests for §5.3 (manifest gate). |
| `internal/plugin/goplugin/emit_token_store_test.go` | Create (~200 LoC) | Unit tests for §5.1 (token store). |
| `internal/plugin/goplugin/host_test.go` | Modify (~100 LoC) | Tests for §5.5 (Host token issuance + cleanup). |
| `internal/plugin/goplugin/host_service_test.go` | Modify (~150 LoC) | Tests for §5.4 (EmitEvent token-based). |
| `test/integration/plugin/actor_authentication_test.go` | Create (~200 LoC) | Integration tests for §5.6 (binary forgery + Lua manifest gate) added as new Describe blocks in the EXISTING Ginkgo suite at `test/integration/plugin/`. Reuses existing Postgres testcontainer + plugin-loading scaffolding. |
| `test/integration/plugin/testdata/forgery_plugin/main.go` + `plugin.yaml` | Create (~80 LoC) | Test-only binary plugin with FORGERY_OVERRIDE_KIND / FORGERY_OVERRIDE_ID / FABRICATE_TOKEN / EMIT_FROM_BACKGROUND env-var modes. Used only by the integration tests. |

---

## Task 1: Activate the actor-metadata channel — subscriber upstream stamping (G7)

**Why first:** This is a refactor that activates the actor-metadata channel at the subscriber boundary. Without it, the manifest gate (Task 5) and token mechanism (Task 8) operate on a channel that's empty in production. TDD: write a unit test asserting `core.ActorFromContext(ctx)` is populated at the `DeliverEvent` boundary; verify it fails (red); apply the refactor; verify it passes (green).

**Files:**
- Modify: `internal/plugin/subscriber.go:104-150` (`deliverAsync` function)
- Create: `internal/plugin/subscriber_test.go`

- [ ] **Step 0: Start a fresh jj change**

```bash
jj new
jj st                       # MUST show clean working copy
jj log -r '@-' --no-pager   # MUST show main@origin (plzx)
```

- [ ] **Step 1: Write the failing test (TDD red)**

Create `internal/plugin/subscriber_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// TestSubscriberStampsCharacterActorBeforeDeliverEvent asserts that the
// PRODUCTION Subscriber populates core.ActorFromContext(ctx) BEFORE
// calling Host.DeliverEvent, so the host's outgoing-metadata injection
// and token issuance see the upstream actor.
//
// This test invokes the production Subscriber.deliverAsync directly (the
// test file is in package plugins, the same package as subscriber.go,
// so it has access to unexported methods).
func TestSubscriberStampsCharacterActorBeforeDeliverEvent(t *testing.T) {
	t.Parallel()

	host := mocks.NewMockHost(t)
	var capturedCtx context.Context
	var capturedMu sync.Mutex
	host.EXPECT().DeliverEvent(mock.Anything, "test-plugin", mock.Anything).
		RunAndReturn(func(ctx context.Context, _ string, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
			capturedMu.Lock()
			capturedCtx = ctx
			capturedMu.Unlock()
			return nil, nil
		})
	emitter := mocks.NewMockEventEmitter(t)
	// EmitPluginEvent may or may not be invoked (zero emits returned), so
	// don't EXPECT it; the strict mock will pass if it's never called.

	sub := NewSubscriber(host, emitter)

	charID := ulid.MustParse("01HX0000000000000000000000")
	event := pluginsdk.Event{
		ID:        ulid.MustParse("01HEVENT00000000000000000C").String(),
		Stream:    "location:01HLOC0000000000000000000",
		Type:      "say",
		Timestamp: 0,
		ActorKind: pluginsdk.ActorCharacter,
		ActorID:   charID.String(),
		Payload:   `{"message":"hi"}`,
	}

	// Invoke production deliverAsync directly. It launches a goroutine; wait
	// for completion via Subscriber.wg (an exported wait helper or by relying
	// on test-only WaitGroup access via the package-internal field).
	sub.deliverAsync(context.Background(), "test-plugin", event)
	sub.Stop() // existing public method at subscriber.go:84; waits on s.wg // see step 1.5 below — adds an in-package test helper

	capturedMu.Lock()
	defer capturedMu.Unlock()
	require.NotNil(t, capturedCtx, "DeliverEvent must have been invoked")
	got, ok := core.ActorFromContext(capturedCtx)
	require.True(t, ok, "core.ActorFromContext MUST return ok=true at DeliverEvent boundary")
	assert.Equal(t, core.ActorCharacter, got.Kind)
	assert.Equal(t, charID.String(), got.ID)
}

// TestSubscriberStampsSystemActorBeforeDeliverEvent — same shape, ActorSystem case.
func TestSubscriberStampsSystemActorBeforeDeliverEvent(t *testing.T) {
	t.Parallel()
	host := mocks.NewMockHost(t)
	var capturedCtx context.Context
	var capturedMu sync.Mutex
	host.EXPECT().DeliverEvent(mock.Anything, "test-plugin", mock.Anything).
		RunAndReturn(func(ctx context.Context, _ string, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
			capturedMu.Lock()
			capturedCtx = ctx
			capturedMu.Unlock()
			return nil, nil
		})
	emitter := mocks.NewMockEventEmitter(t)
	sub := NewSubscriber(host, emitter)

	event := pluginsdk.Event{
		ID:        ulid.MustParse("01HEVENT00000000000000000S").String(),
		Stream:    "system:health",
		Type:      "tick",
		ActorKind: pluginsdk.ActorSystem,
		ActorID:   core.ActorSystemID,
	}
	sub.deliverAsync(context.Background(), "test-plugin", event)
	sub.Stop() // existing public method at subscriber.go:84; waits on s.wg

	capturedMu.Lock()
	defer capturedMu.Unlock()
	got, ok := core.ActorFromContext(capturedCtx)
	require.True(t, ok)
	assert.Equal(t, core.ActorSystem, got.Kind)
	assert.Equal(t, core.ActorSystemID, got.ID)
}
```

**Package + imports for `subscriber_test.go`:**

The test file lives in `package plugins` (NOT `package plugins_test`) so it can call the unexported `deliverAsync`. Because the package name is `plugins`, do NOT also import `github.com/holomush/holomush/internal/plugin` as `plugins` — that would clash. Use unqualified `Subscriber`, `NewSubscriber`, etc. throughout the test body (the calls above already do).

```go
import (
    "context"
    "sync"
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/core"
    "github.com/holomush/holomush/internal/plugin/mocks"
    pluginsdk "github.com/holomush/holomush/pkg/plugin"
)
```

(No new helper required — `Subscriber.Stop()` at `subscriber.go:84` already does `s.wg.Wait()` and is the right hook for tests that drive `deliverAsync` directly.)

- [ ] **Step 2: Run the failing test (TDD red)**

Run: `task test -- -run TestSubscriberStampsCharacterActorBeforeDeliverEvent ./internal/plugin/`

Expected: FAIL — the production `subscriber.go::deliverAsync` passes raw `tctx` (no actor) into `Host.DeliverEvent` at line 112; the post-Deliver `core.WithActor` at line 137 binds only `emitCtx`. So `core.ActorFromContext(capturedCtx)` returns `ok=false`, the `require.True(t, ok)` assertion fails. That's the genuine red — the test exercises the production codepath, not a fixture that does what production should do.

- [ ] **Step 3: Refactor `subscriber.go::deliverAsync` (TDD green)**

In `internal/plugin/subscriber.go`, locate `deliverAsync` (around line 104). Find the line:

```go
emits, err := s.host.DeliverEvent(tctx, pluginName, event)
```

and the post-Deliver actor-stamping (around line 137):

```go
emitCtx := core.WithActor(tctx, actorFromIncomingEvent(event))
```

Replace the structure so the actor is stamped BEFORE the Deliver call and the same ctx flows through to the emit loop:

```go
// Stamp the host-vouched actor on the dispatch ctx BEFORE calling Host.DeliverEvent.
// This activates the actor-metadata channel for the host's outgoing metadata
// injection (host.go:540) and the binary-plugin token issuance (per spec G7).
dispatchCtx := core.WithActor(tctx, actorFromIncomingEvent(event))

emits, err := s.host.DeliverEvent(dispatchCtx, pluginName, event)
// ... existing error handling unchanged, except references to tctx in error
//     paths can stay as tctx (pre-actor) — only the dispatch ctx flows the actor.

// Existing emit loop now uses dispatchCtx (same actor as the Deliver call):
for _, emit := range emits {
    if err := s.emitter.EmitPluginEvent(dispatchCtx, pluginName, emit); err != nil {
        slog.ErrorContext(tctx, "failed to emit plugin event",
            "plugin", pluginName,
            "stream", emit.Stream,
            "error", err)
    }
}
```

The `actorFromIncomingEvent(event)` helper at line 153-166 is unchanged.

- [ ] **Step 4: Run the test to verify it PASSES (TDD green)**

Run: `task test -- -run TestSubscriberStampsCharacterActorBeforeDeliverEvent ./internal/plugin/`

Expected: PASS. Also run `TestSubscriberStampsSystemActorBeforeDeliverEvent` — PASS.

- [ ] **Step 5: Run full `internal/plugin/` test suite for regressions**

Run: `task test -- ./internal/plugin/...`

Expected: all PASS. The change is a pure ctx-mutation move; no observable behavior changes for tests that don't read ctx-actor.

- [ ] **Step 6: Run lint**

Run: `task lint`

Expected: PASS.

- [ ] **Step 7: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): stamp upstream actor on dispatch ctx before subscriber→DeliverEvent (G7 prerequisite)

Activates the actor-metadata channel at the subscriber boundary by
moving core.WithActor(actorFromIncomingEvent(event)) from line 137
(post-DeliverEvent, used only for emit ctx) to before line 112
(pre-DeliverEvent). The same ctx now flows through to both
DeliverEvent AND the post-deliver EmitPluginEvent loop.

Per spec §1.1: today's host.go:539-540 reads core.ActorFromContext(ctx)
to attach outgoing actor metadata to the binary plugin's HandleEvent
call, but this check is dead in production because subscriber.go
passes the unstamped tctx into DeliverEvent. After this change, the
host's outgoing metadata is correctly populated for character-driven
and system-driven dispatches.

This unblocks the manifest gate (next task) and token mechanism (later
task) by ensuring core.ActorFromContext returns ok=true at the
dispatch boundary.

Bead: holomush-ec22.1 (G7)"
```

(Do NOT run `jj new` — the next task starts with its own `jj new` step.)

---

## Task 2: Activate the actor-metadata channel — dispatcher upstream stamping (G7)

**Why second:** Same prerequisite as Task 1, applied at the second dispatch entry point (command dispatch path). TDD red-green at the dispatcher boundary.

**Files:**
- Modify: `internal/command/dispatcher.go::dispatchToPlugin` (~lines 285-381)
- Modify: `internal/command/dispatcher_test.go` (extend with §5.8.2 tests)

- [ ] **Step 0: Start a fresh jj change**

```bash
jj new
jj st
jj log -r '@-' --no-pager   # MUST show Task 1's commit
```

- [ ] **Step 1: Write the failing test (TDD red)**

In `internal/command/dispatcher_test.go`, add:

```go
// capturingDeliverer captures the ctx passed to DeliverCommand /
// EmitPluginEvent so the test can assert on actor context at the
// dispatch boundary.
type capturingDeliverer struct {
    mu             sync.Mutex
    deliverCtx     context.Context
    emitCtxs       []context.Context
}

func (c *capturingDeliverer) DeliverCommand(ctx context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
    c.mu.Lock()
    c.deliverCtx = ctx
    c.mu.Unlock()
    return &pluginsdk.CommandResponse{Status: pluginsdk.CommandOK}, nil
}

func (c *capturingDeliverer) EmitPluginEvent(ctx context.Context, _ string, _ pluginsdk.EmitEvent) error {
    c.mu.Lock()
    c.emitCtxs = append(c.emitCtxs, ctx)
    c.mu.Unlock()
    return nil
}

// TestDispatcherStampsCharacterActorBeforeDeliverCommand asserts the
// dispatcher populates core.ActorFromContext(ctx) BEFORE calling
// pluginDeliverer.DeliverCommand, per spec G7. Uses the existing
// newTestDispatcherWithPlugin + newTestCommandExecution scaffolding
// (dispatcher_test.go:2063, :2104) and exercises the public Dispatch
// API which routes through dispatchToPlugin internally.
func TestDispatcherStampsCharacterActorBeforeDeliverCommand(t *testing.T) {
    t.Parallel()

    cd := &capturingDeliverer{}
    d := newTestDispatcherWithPlugin(t, cd)
    exec := newTestCommandExecution(t)
    expectedCharID := exec.CharacterID().String()

    // Dispatch the registered "plugintest" command (registered by the helper).
    err := d.Dispatch(context.Background(), exec, "plugintest")
    require.NoError(t, err)

    cd.mu.Lock()
    defer cd.mu.Unlock()
    require.NotNil(t, cd.deliverCtx, "DeliverCommand must have been invoked")
    got, ok := core.ActorFromContext(cd.deliverCtx)
    require.True(t, ok, "DeliverCommand MUST receive ctx with actor populated")
    assert.Equal(t, core.ActorCharacter, got.Kind)
    assert.Equal(t, expectedCharID, got.ID)
}
```

**Imports needed in `dispatcher_test.go`** (most are already present; add only what's missing):

- `"sync"` (already present per existing tests)
- `"github.com/holomush/holomush/internal/core"` (verify; add if absent)
- `pluginsdk "github.com/holomush/holomush/pkg/plugin"` (verify; add if absent)

- [ ] **Step 2: Run the failing test (TDD red)**

Run: `task test -- -run TestDispatcherStampsCharacterActorBeforeDeliverCommand ./internal/command/`

Expected: FAIL — current `dispatchToPlugin` (dispatcher.go:286) passes raw `ctx` (no actor) to `DeliverCommand`; the post-DeliverCommand `core.WithActor` at line 370 binds only `emitCtx`. So `core.ActorFromContext(cd.deliverCtx)` returns `ok=false`, the `require.True(t, ok)` assertion fails. This is the genuine red — the test exercises the production Dispatch path.

- [ ] **Step 3: Refactor `dispatcher.go::dispatchToPlugin` (TDD green)**

In `internal/command/dispatcher.go`, locate `dispatchToPlugin` (around line 285). Find:

```go
resp, err := d.pluginDeliverer.DeliverCommand(ctx, entry.PluginName(), cmdReq)
```

and the post-Deliver stamping (around line 370):

```go
emitCtx := core.WithActor(ctx, core.Actor{
    Kind: core.ActorCharacter,
    ID:   exec.CharacterID().String(),
})
```

Move the stamping BEFORE the DeliverCommand call:

```go
// Stamp ActorCharacter on the dispatch ctx BEFORE DeliverCommand. This
// activates the actor-metadata channel for the host's outgoing metadata
// injection (host.go:592) and the binary-plugin token issuance (per spec G7).
dispatchCtx := core.WithActor(ctx, core.Actor{
    Kind: core.ActorCharacter,
    ID:   exec.CharacterID().String(),
})

resp, err := d.pluginDeliverer.DeliverCommand(dispatchCtx, entry.PluginName(), cmdReq)
// ... existing error handling unchanged ...

// The post-Deliver emit loop now uses dispatchCtx (same actor):
for _, evt := range resp.Events {
    if emitErr := d.pluginDeliverer.EmitPluginEvent(dispatchCtx, entry.PluginName(), evt); emitErr != nil {
        return oops.In("dispatcher").
            With("command", entry.Name).
            With("stream", evt.Stream).
            Wrap(emitErr)
    }
}
```

- [ ] **Step 4: Run the test to verify PASS**

Run: `task test -- -run TestDispatcherStampsCharacterActorBeforeDeliverCommand ./internal/command/`

Expected: PASS.

- [ ] **Step 5: Run full `internal/command/` test suite for regressions**

Run: `task test -- ./internal/command/...`

Expected: all PASS.

- [ ] **Step 6: Run lint**

Run: `task lint`

Expected: PASS.

- [ ] **Step 7: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(command): stamp ActorCharacter on dispatch ctx before dispatcher→DeliverCommand (G7 prerequisite)

Activates the actor-metadata channel at the command-dispatcher boundary
by moving core.WithActor(ActorCharacter:exec.CharacterID()) from line
370 (post-DeliverCommand, used only for emit ctx) to before line 310
(pre-DeliverCommand). Same shape as the subscriber.go change in the
preceding commit.

Per spec §1.1 + G7: command-driven plugin invocations now have ctx
populated with the dispatching character's actor BEFORE the host's
DeliverCommand call sees the ctx, enabling correct outgoing-metadata
injection and (in a later task) token issuance with cascade-preserving
stored actor.

Bead: holomush-ec22.1 (G7)"
```

(Do NOT run `jj new`.)

---

## Task 3: Manifest schema — add `ActorKindsClaimable` field with validation

**Why third:** All downstream tasks (manifest gate at Task 5, plugin migrations at Task 4) depend on the field existing on the parsed `Manifest` struct. Tasks 4 and 5 must come AFTER this. TDD: validation rules first (table-driven test), then add the field + validation logic.

**Files:**
- Modify: `internal/plugin/manifest.go::Manifest` struct (~line 70)
- Modify: `internal/plugin/manifest.go` (validation logic in `ParseManifest` or `(*Manifest).Validate()`)
- Modify: `internal/plugin/manifest_test.go`

- [ ] **Step 0: Start a fresh jj change**

```bash
jj new
jj st
jj log -r '@-' --no-pager   # MUST show Task 2's commit
```

- [ ] **Step 1: Write the failing tests (TDD red)**

In `internal/plugin/manifest_test.go`, add a table-driven test:

```go
// TestManifestActorKindsClaimableValidation covers spec §3.2 validation rules.
func TestManifestActorKindsClaimableValidation(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name        string
        yaml        string
        wantErr     bool
        wantErrCode string
        wantNormalized []string // expected post-load value of ActorKindsClaimable
    }{
        {
            name: "absent field defaults to [plugin]",
            yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
            wantNormalized: []string{"plugin"},
        },
        {
            name: "explicit [plugin] loads",
            yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: [plugin]
`,
            wantNormalized: []string{"plugin"},
        },
        {
            name: "[plugin, character] loads",
            yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: [plugin, character]
`,
            wantNormalized: []string{"plugin", "character"},
        },
        {
            name:        "empty list rejected (missing plugin)",
            yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: []
`,
            wantErr:     true,
            wantErrCode: "MANIFEST_ACTOR_KINDS_MISSING_PLUGIN",
        },
        {
            name:        "[character] rejected (missing plugin)",
            yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: [character]
`,
            wantErr:     true,
            wantErrCode: "MANIFEST_ACTOR_KINDS_MISSING_PLUGIN",
        },
        {
            name:        "[plugin, system] rejected (system forbidden)",
            yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: [plugin, system]
`,
            wantErr:     true,
            wantErrCode: "MANIFEST_ACTOR_KIND_SYSTEM_FORBIDDEN",
        },
        {
            name:        "[plugin, frobnicate] rejected (unknown kind)",
            yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: [plugin, frobnicate]
`,
            wantErr:     true,
            wantErrCode: "MANIFEST_ACTOR_KIND_UNKNOWN",
        },
        {
            name: "duplicates silently dedup",
            yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: [plugin, plugin, character]
`,
            wantNormalized: []string{"plugin", "character"},
        },
        {
            name: "malformed string instead of list rejected by yaml unmarshal",
            yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: plugin
`,
            wantErr: true,
            // Note: gopkg.in/yaml.v3 returns its own type-mismatch error
            // before our validation runs. We don't define a project-specific
            // MANIFEST_ACTOR_KINDS_MALFORMED code; the yaml.TypeError surfaces
            // through the existing manifest-load error path. The test asserts
            // err is non-nil and contains "actor_kinds_claimable" in the message.
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            m, err := ParseManifest([]byte(tt.yaml))
            if tt.wantErr {
                require.Error(t, err)
                if tt.wantErrCode != "" {
                    errutil.AssertErrorCode(t, err, tt.wantErrCode)
                }
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.wantNormalized, m.ActorKindsClaimable)
        })
    }
}

// TestManifestDeclaresActorKindClaimable verifies the helper used by the
// manifest gate (Task 5).
func TestManifestDeclaresActorKindClaimable(t *testing.T) {
    t.Parallel()
    m := &Manifest{ActorKindsClaimable: []string{"plugin", "character"}}
    assert.True(t, m.DeclaresActorKindClaimable(core.ActorPlugin))
    assert.True(t, m.DeclaresActorKindClaimable(core.ActorCharacter))
    assert.False(t, m.DeclaresActorKindClaimable(core.ActorSystem))

    m2 := &Manifest{ActorKindsClaimable: []string{"plugin"}}
    assert.True(t, m2.DeclaresActorKindClaimable(core.ActorPlugin))
    assert.False(t, m2.DeclaresActorKindClaimable(core.ActorCharacter))
    assert.False(t, m2.DeclaresActorKindClaimable(core.ActorSystem))
}
```

- [ ] **Step 2: Run the failing tests (TDD red)**

Run: `task test -- -run TestManifestActorKindsClaimableValidation\|TestManifestDeclaresActorKindClaimable ./internal/plugin/`

Expected: FAIL — `Manifest` has no `ActorKindsClaimable` field, no `DeclaresActorKindClaimable` method, no validation.

- [ ] **Step 3: Add the field to `Manifest`**

In `internal/plugin/manifest.go`, in the `Manifest` struct (around line 70), add the field after `Emits`:

```go
    // ActorKindsClaimable declares which Actor.Kind values the plugin may
    // vouch for on emitted events. Default if absent: ["plugin"]. Allowed
    // values: "plugin" (always required), "character". The "system" kind
    // is rejected at load — plugins may never claim the host's system
    // identity. See spec docs/superpowers/specs/2026-04-25-plugin-actor-claim-authentication-design.md §3.2.
    ActorKindsClaimable []string `yaml:"actor_kinds_claimable,omitempty" json:"actor_kinds_claimable,omitempty"`
```

- [ ] **Step 4: Add the validation logic**

Find the existing manifest-validation entry point (likely `ParseManifest` or `(*Manifest).Validate()` — `internal/plugin/manifest.go:291` per the spec). Add a call to validate the new field:

```go
// validateActorKindsClaimable applies spec §3.2 validation rules and
// normalizes the field (default to [plugin], dedup, sort). Returns the
// canonical form on success; oops error on violation.
func validateActorKindsClaimable(in []string) ([]string, error) {
    if len(in) == 0 {
        return []string{"plugin"}, nil
    }
    seen := make(map[string]bool)
    out := make([]string, 0, len(in))
    for _, k := range in {
        switch k {
        case "plugin", "character":
            if !seen[k] {
                seen[k] = true
                out = append(out, k)
            }
        case "system":
            return nil, oops.Code("MANIFEST_ACTOR_KIND_SYSTEM_FORBIDDEN").
                Errorf("actor_kinds_claimable MUST NOT contain %q (host's system identity is not a claimable plugin capability)", k)
        default:
            return nil, oops.Code("MANIFEST_ACTOR_KIND_UNKNOWN").
                With("kind", k).
                Errorf("actor_kinds_claimable contains unknown kind %q (allowed: plugin, character)", k)
        }
    }
    if !seen["plugin"] {
        return nil, oops.Code("MANIFEST_ACTOR_KINDS_MISSING_PLUGIN").
            Errorf("actor_kinds_claimable MUST contain %q (plugins always need to vouch for their own identity)", "plugin")
    }
    return out, nil
}
```

Wire it into the validation flow:

```go
// In ParseManifest or Validate, after other field validations:
normalized, err := validateActorKindsClaimable(m.ActorKindsClaimable)
if err != nil {
    return err
}
m.ActorKindsClaimable = normalized
```

- [ ] **Step 5: Add the `DeclaresActorKindClaimable` helper**

```go
// DeclaresActorKindClaimable returns true if the plugin manifest opts into
// vouching for the given actor kind on emitted events. Used by the manifest
// gate at internal/plugin/event_emitter.go::Emit.
func (m *Manifest) DeclaresActorKindClaimable(kind core.ActorKind) bool {
    var name string
    switch kind {
    case core.ActorPlugin:
        name = "plugin"
    case core.ActorCharacter:
        name = "character"
    case core.ActorSystem:
        return false // architecturally never claimable; manifest validation rejects "system"
    default:
        return false
    }
    for _, k := range m.ActorKindsClaimable {
        if k == name {
            return true
        }
    }
    return false
}
```

- [ ] **Step 6: Run the tests to verify PASS (TDD green)**

Run: `task test -- -run TestManifestActorKindsClaimableValidation\|TestManifestDeclaresActorKindClaimable ./internal/plugin/`

Expected: PASS.

- [ ] **Step 7: Run full plugin-package tests**

Run: `task test -- ./internal/plugin/...`

Expected: all PASS — existing tests are unaffected because the field defaults to `[plugin]` when absent, and no enforcement exists yet (Task 5 adds it).

- [ ] **Step 8: Run lint**

Run: `task lint`

Expected: PASS.

- [ ] **Step 9: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): add Manifest.ActorKindsClaimable field + validation (G2 schema)

Per spec §3.2: new manifest field actor_kinds_claimable with validation
rules:
- Default if absent: [plugin].
- MUST contain plugin (plugins always need their own identity).
- MUST NOT contain system (host's identity, never claimable).
- Only known kinds: plugin, character.
- Duplicates silently deduplicated.

New helper Manifest.DeclaresActorKindClaimable(core.ActorKind) bool
returns true if the plugin opts into vouching for the given kind. Used
by the manifest gate (next task).

No enforcement yet — this commit only adds the schema. Subsequent
commits add the gate (event_emitter.go) + plugin migrations.

Bead: holomush-ec22.1 (G2)"
```

(Do NOT run `jj new`.)

---

## Task 4: Migrate in-tree plugin manifests (3 plugins)

**Why fourth:** Once the manifest gate (Task 5) is in place, any plugin without proper claim breaks. Manifests must be updated FIRST so Task 5 can land green. TDD: not strictly applicable (YAML edits); regression assertion is "Task 5's tests pass."

**Files:**
- Modify: `plugins/core-scenes/plugin.yaml`
- Modify: `plugins/core-communication/plugin.yaml`
- Modify: `plugins/echo-bot/plugin.yaml`

- [ ] **Step 0: Start a fresh jj change**

```bash
jj new
jj st
jj log -r '@-' --no-pager   # MUST show Task 3's commit
```

- [ ] **Step 1: Update `plugins/core-scenes/plugin.yaml`**

Add the line `actor_kinds_claimable: [plugin, character]` to the manifest. Place it adjacent to `emits:` for editorial coherence (both fields concern emit policy):

```yaml
# Before this line: existing fields including emits: [scene]
emits: [scene]
actor_kinds_claimable: [plugin, character]
# After: existing fields
```

- [ ] **Step 2: Update `plugins/core-communication/plugin.yaml`**

Same line, after `emits: [location, character]`:

```yaml
emits: [location, character]
actor_kinds_claimable: [plugin, character]
```

- [ ] **Step 3: Update `plugins/echo-bot/plugin.yaml`**

echo-bot has no top-level `emits:`. Place the new field after `events: [say]`:

```yaml
events:
  - say
actor_kinds_claimable: [plugin, character]
```

- [ ] **Step 4: Verify all three manifests parse**

Run: `task test -- -run TestParseManifest ./internal/plugin/` (or the equivalent test that exercises the parsing path on real plugin YAML)

Expected: PASS. The existing manifest-loading tests should continue to pass; the new field is recognized and stored.

- [ ] **Step 5: Run lint**

Run: `task lint`

Expected: PASS.

- [ ] **Step 6: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugins): declare actor_kinds_claimable: [plugin, character] for character-driven emit plugins

Pre-emptive migration to the new manifest schema (per Task 3) for the
three in-tree plugins that emit during character-driven dispatches:

- plugins/core-scenes/plugin.yaml: scene-lifecycle emits during
  character-driven scene commands.
- plugins/core-communication/plugin.yaml: say/pose/emote/page/whisper/
  ooc/pemit/emit/wall command-handler emits.
- plugins/echo-bot/plugin.yaml: on_event handler returns a say emit
  in response to character-actor say events.

Lands BEFORE the manifest gate (next task) so the gate can enforce
without breaking these plugins.

Bead: holomush-ec22.1 (§3.2 migration scope)"
```

(Do NOT run `jj new`.)

---

## Task 5: Manifest gate — universal enforcement at `event_emitter.go::Emit` (G2)

**Why fifth:** The manifest schema (Task 3) and migrations (Task 4) are in place; the gate can now safely enforce. TDD: write tests asserting the gate fires for a non-claimed kind and passes for a claimed kind, then add the gate.

**Files:**
- Modify: `internal/plugin/event_emitter.go::Emit` (around lines 99-111)
- Modify: `internal/plugin/event_emitter_test.go`

- [ ] **Step 0: Start a fresh jj change**

```bash
jj new
jj st
jj log -r '@-' --no-pager   # MUST show Task 4's commit
```

- [ ] **Step 1: Write the failing tests (TDD red)**

In `internal/plugin/event_emitter_test.go`, add:

```go
// Use the existing scaffolding from event_emitter_test.go:
//   - eventbustest.New(t) → *eventbustest.Embedded (real embedded NATS+JS).
//   - newEmitter(t, bus, lookup, resolve) → *PluginEventEmitter wired to bus.
//   - pluginActorResolver — already-defined ActorResolver that reads ctx-actor.
//
// Tests assert on bus state via fetchAllMessages(t, bus.JS) (also defined
// in the existing file). NO new mock types needed.

// TestEmitManifestGateRejectsCharacterClaimWithoutOptIn covers spec §3.4 + §5.3:
// a plugin manifest that doesn't list "character" MUST loud-error when emit
// ctx carries an ActorCharacter.
func TestEmitManifestGateRejectsCharacterClaimWithoutOptIn(t *testing.T) {
    t.Parallel()
    bus := eventbustest.New(t)
    manifest := &plugins.Manifest{
        Name: "plug-A", Type: plugins.TypeLua, Emits: []string{"location"},
        ActorKindsClaimable: []string{"plugin"}, // no character
    }
    e := newEmitter(t, bus,
        func(string) *plugins.Manifest { return manifest },
        actorFromCtxResolver, // see below — reads core.ActorFromContext
    )
    ctx := core.WithActor(context.Background(), core.Actor{
        Kind: core.ActorCharacter,
        ID:   "01HCHAR0000000000000000000",
    })
    err := e.Emit(ctx, "plug-A", pluginsdk.EmitIntent{
        Subject: "location:01HLOC0000000000000000000",
        Type:    "say",
        Payload: `{"message":"hi"}`,
    })
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "EMIT_ACTOR_KIND_NOT_CLAIMABLE")

    // No message should have been published.
    msgs := fetchAllMessages(t, bus.JS)
    assert.Empty(t, msgs)
}

// TestEmitManifestGateAllowsClaimedKind verifies the gate passes when the
// manifest declares the actor's kind.
func TestEmitManifestGateAllowsClaimedKind(t *testing.T) {
    t.Parallel()
    bus := eventbustest.New(t)
    manifest := &plugins.Manifest{
        Name: "plug-A", Type: plugins.TypeLua, Emits: []string{"location"},
        ActorKindsClaimable: []string{"plugin", "character"},
    }
    e := newEmitter(t, bus,
        func(string) *plugins.Manifest { return manifest },
        actorFromCtxResolver,
    )
    ctx := core.WithActor(context.Background(), core.Actor{
        Kind: core.ActorCharacter,
        ID:   "01HCHAR0000000000000000000",
    })
    err := e.Emit(ctx, "plug-A", pluginsdk.EmitIntent{
        Subject: "location:01HLOC0000000000000000000",
        Type:    "say",
        Payload: `{"message":"hi"}`,
    })
    require.NoError(t, err)
    msgs := fetchAllMessages(t, bus.JS)
    require.Len(t, msgs, 1)
    assert.Equal(t, "character", msgs[0].Header.Get(eventbus.HeaderActorKind))
}

// TestEmitManifestGateAllowsPluginCascade covers cascade preservation:
// plug-A emits during a cascade with ActorPlugin:plug-B in ctx; default
// [plugin] manifest allows plug-A to vouch for the cascade.
func TestEmitManifestGateAllowsPluginCascade(t *testing.T) {
    t.Parallel()
    bus := eventbustest.New(t)
    manifest := &plugins.Manifest{
        Name: "plug-A", Type: plugins.TypeLua, Emits: []string{"location"},
        ActorKindsClaimable: []string{"plugin"},
    }
    e := newEmitter(t, bus,
        func(string) *plugins.Manifest { return manifest },
        actorFromCtxResolver,
    )
    ctx := core.WithActor(context.Background(), core.Actor{
        Kind: core.ActorPlugin,
        ID:   "plug-B", // upstream cascade
    })
    err := e.Emit(ctx, "plug-A", pluginsdk.EmitIntent{
        Subject: "location:01HLOC0000000000000000000",
        Type:    "test",
        Payload: `{}`,
    })
    require.NoError(t, err)
}

// actorFromCtxResolver is a test-only ActorResolver that reads from ctx
// (mirrors the production resolver at internal/plugin/manager.go:1164).
// Inline this helper into event_emitter_test.go alongside the existing
// pluginActorResolver helper.
func actorFromCtxResolver(ctx context.Context, _ string) (core.Actor, error) {
    actor, ok := core.ActorFromContext(ctx)
    if !ok {
        return core.Actor{}, oops.New("plugin event actor missing from context")
    }
    return actor, nil
}
```

**Imports needed in `event_emitter_test.go`** (most already present per existing file; add only):

- `"github.com/samber/oops"` (already present)
- `"github.com/holomush/holomush/internal/eventbus"` (already present, for HeaderActorKind constant)

- [ ] **Step 2: Run the failing tests (TDD red)**

Run: `task test -- -run 'TestEmitManifestGate' ./internal/plugin/`

Expected: FAIL (or partial fail) — `TestEmitManifestGateRejectsCharacterClaimWithoutOptIn` MUST fail (no gate exists, emit succeeds). The other two should already pass.

- [ ] **Step 3: Add the manifest gate to `event_emitter.go::Emit` (TDD green)**

In `internal/plugin/event_emitter.go::Emit`, after the existing namespace check at lines 99-111 (the `declaresEmitNamespace` block) and after `actor` is resolved via `e.resolveActor(ctx, pluginName)` (lines 117-123), add:

```go
// Manifest gate (universal — applies to Lua and binary plugins). Asserts
// the actor's Kind is in the plugin's actor_kinds_claimable list.
// Per spec §3.4 + project invariant on plugin runtime symmetry: this
// gate fires for both Lua and binary plugins because both runtimes
// flow through this Emit codepath.
if !manifest.DeclaresActorKindClaimable(actor.Kind) {
    return oops.Code("EMIT_ACTOR_KIND_NOT_CLAIMABLE").
        With("plugin", pluginName).
        With("subject", subjectRaw).
        With("actor_kind", actor.Kind.String()).
        With("declared_kinds", manifest.ActorKindsClaimable).
        Errorf("plugin manifest does not declare %q as a claimable actor kind", actor.Kind.String())
}
```

The `manifest` variable is already in scope from the namespace check.

- [ ] **Step 4: Run the tests to verify PASS (TDD green)**

Run: `task test -- -run 'TestEmitManifestGate' ./internal/plugin/`

Expected: all three tests PASS.

- [ ] **Step 5: Run full `internal/plugin/` test suite for regressions**

Run: `task test -- ./internal/plugin/...`

Expected: all PASS. The three migrated plugin manifests carry `[plugin, character]`, so any test that exercises a real plugin manifest is unaffected.

- [ ] **Step 6: Run lint**

Run: `task lint`

Expected: PASS.

- [ ] **Step 7: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): manifest gate — enforce actor_kinds_claimable at event_emitter.Emit (G2)

Per spec §3.4: universal manifest gate at internal/plugin/event_emitter.go::Emit
asserts core.ActorFromContext(ctx).Kind is listed in the plugin's
manifest.actor_kinds_claimable. Loud error EMIT_ACTOR_KIND_NOT_CLAIMABLE
when not declared. Applies symmetrically to Lua and binary plugins
(per project invariant on plugin runtime symmetry).

ActorSystem can never reach this gate via a plugin emit because
manifest validation (Task 3) rejects 'system' from the claim list.
Per spec §3.3.4 the binary-plugin path will additionally re-anchor
ActorSystem to ActorPlugin:<self> at token issuance (Task 8) before
the gate sees it.

In-tree plugins migrated in Task 4. After this commit, the universal
gate is live and enforcing.

Bead: holomush-ec22.1 (G2)"
```

(Do NOT run `jj new`.)

---

## Task 6: Token store — new file `emit_token_store.go`

**Why sixth:** Foundation for the binary-plugin token mechanism. Pure isolated unit (no integration with `Host` yet). TDD: write all unit tests for `Issue` / `Lookup` / `Revoke` / sweeper / Close, then implement.

**Files:**
- Create: `internal/plugin/goplugin/emit_token_store.go`
- Create: `internal/plugin/goplugin/emit_token_store_test.go`

- [ ] **Step 0: Start a fresh jj change**

```bash
jj new
jj st
jj log -r '@-' --no-pager   # MUST show Task 5's commit
```

- [ ] **Step 1: Write the failing tests (TDD red)**

Create `internal/plugin/goplugin/emit_token_store_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/holomush/holomush/internal/core"
)

func newStoreForTest(t *testing.T) *emitTokenStore {
	t.Helper()
	return &emitTokenStore{
		items: make(map[string]emitTokenEntry),
		now:   time.Now,
		rand:  rand.Reader,
		ttl:   5 * time.Minute,
		sweep: 30 * time.Second,
	}
}

func TestEmitTokenStoreIssueLookupHappyPath(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	actor := core.Actor{Kind: core.ActorCharacter, ID: "01HCHAR..."}
	tok, err := s.Issue("plug-A", actor)
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	got, ok := s.Lookup("plug-A", tok)
	require.True(t, ok)
	assert.Equal(t, actor, got)
}

func TestEmitTokenStoreLookupWrongPluginNameReturnsFalse(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	actor := core.Actor{Kind: core.ActorCharacter, ID: "01HCHAR..."}
	tok, err := s.Issue("plug-A", actor)
	require.NoError(t, err)

	_, ok := s.Lookup("plug-B", tok)
	assert.False(t, ok)
}

func TestEmitTokenStoreLookupUnknownTokenReturnsFalse(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	_, ok := s.Lookup("plug-A", "not-a-real-token")
	assert.False(t, ok)
}

func TestEmitTokenStoreLookupExpiredEntryReturnsFalse(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := newStoreForTest(t)
	s.now = func() time.Time { return now }
	tok, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"})
	require.NoError(t, err)

	// Advance clock past TTL.
	s.now = func() time.Time { return now.Add(s.ttl + time.Second) }
	_, ok := s.Lookup("plug-A", tok)
	assert.False(t, ok)
}

func TestEmitTokenStoreRevokeRemovesEntry(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	tok, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"})
	require.NoError(t, err)
	s.Revoke(tok)
	_, ok := s.Lookup("plug-A", tok)
	assert.False(t, ok)
}

func TestEmitTokenStoreRevokeIsIdempotent(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	s.Revoke("never-issued")
	tok, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"})
	require.NoError(t, err)
	s.Revoke(tok)
	s.Revoke(tok) // second call must not panic
}

func TestEmitTokenStoreTokenFormat(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	tok, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"})
	require.NoError(t, err)
	assert.Len(t, tok, 22, "token MUST be 22 base64url chars (16 bytes unpadded)")
	decoded, decodeErr := base64.RawURLEncoding.DecodeString(tok)
	require.NoError(t, decodeErr)
	assert.Len(t, decoded, 16)
}

func TestEmitTokenStoreTokenUniqueness(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	const N = 10000
	seen := make(map[string]bool, N)
	for i := 0; i < N; i++ {
		tok, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"})
		require.NoError(t, err)
		require.False(t, seen[tok], "token collision at i=%d", i)
		seen[tok] = true
	}
}

func TestEmitTokenStoreConcurrentIssueLookupSafety(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	const N = 100
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"})
			require.NoError(t, err)
			_, ok := s.Lookup("plug-A", tok)
			require.True(t, ok)
			s.Revoke(tok)
		}()
	}
	wg.Wait()
}

func TestEmitTokenStoreIssueFailsOnRandFailure(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	s.rand = bytes.NewReader(nil) // exhausted reader → io.EOF on Read
	_, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_ISSUE_FAILED")
}

func TestEmitTokenStoreSweeperRemovesExpired(t *testing.T) {
	t.Parallel()
	defer goleak.VerifyNone(t)

	now := time.Now()
	s := newStoreForTest(t)
	s.now = func() time.Time { return now }
	s.sweep = 10 * time.Millisecond
	tok, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	// Advance clock past TTL.
	s.now = func() time.Time { return now.Add(s.ttl + time.Second) }
	// Wait for sweeper to fire.
	require.Eventually(t, func() bool {
		_, ok := s.Lookup("plug-A", tok)
		return !ok
	}, 200*time.Millisecond, 5*time.Millisecond, "sweeper should remove expired entry")
}

func TestEmitTokenStoreCloseStopsSweeper(t *testing.T) {
	t.Parallel()
	defer goleak.VerifyNone(t)

	s := newStoreForTest(t)
	s.sweep = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	require.NoError(t, s.Close())
	// goleak.VerifyNone in defer asserts no goroutines leak.
}
```

(Add `go.uber.org/goleak` import; if not yet in go.mod, this requires `go get go.uber.org/goleak` in Step 4.)

- [ ] **Step 2: Run the failing tests (TDD red)**

Run: `task test -- ./internal/plugin/goplugin/...`

Expected: COMPILATION ERROR (struct + methods don't exist yet). That's the red phase for this kind of pure-creation task.

- [ ] **Step 3: Implement `emit_token_store.go`**

Create `internal/plugin/goplugin/emit_token_store.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"sync"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
)

// emitTokenStore authenticates the actor input on the binary-plugin gRPC
// EmitEvent boundary. The host issues a per-dispatch random token at every
// outgoing call to a binary plugin (Host.DeliverEvent / DeliverCommand),
// stores the host-vouched actor against the token, and the plugin's SDK
// auto-ferries the token back when the plugin emits. The host's EmitEvent
// looks up the token and uses the stored actor verbatim, ignoring any
// kind/id values the plugin's metadata claims.
//
// Per spec §3.3: defense-in-depth pluginName tagging on top of 128-bit
// token entropy guards against cross-plugin token leakage. TTL =
// 60 × DefaultEventTimeout (5 min) is a generous safety margin against
// crash-without-defer paths; the deferred Revoke at the issuance site
// is the happy-path cleanup.
type emitTokenStore struct {
	mu      sync.RWMutex
	items   map[string]emitTokenEntry
	now     func() time.Time
	rand    io.Reader
	ttl     time.Duration
	sweep   time.Duration
	stop    chan struct{}
	stopped bool
}

type emitTokenEntry struct {
	pluginName string
	actor      core.Actor
	expiresAt  time.Time
}

func newEmitTokenStore() *emitTokenStore {
	return &emitTokenStore{
		items: make(map[string]emitTokenEntry),
		now:   time.Now,
		rand:  rand.Reader,
		ttl:   5 * time.Minute, // 60 × DefaultEventTimeout
		sweep: 30 * time.Second,
		stop:  make(chan struct{}),
	}
}

// Issue creates a new token for an outgoing dispatch. Caller MUST defer
// Revoke or the entry will rely on TTL expiry for cleanup.
func (s *emitTokenStore) Issue(pluginName string, actor core.Actor) (string, error) {
	var buf [16]byte
	if _, err := io.ReadFull(s.rand, buf[:]); err != nil {
		return "", oops.Code("EMIT_TOKEN_ISSUE_FAILED").
			With("plugin", pluginName).
			Wrap(err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf[:])
	s.mu.Lock()
	s.items[token] = emitTokenEntry{
		pluginName: pluginName,
		actor:      actor,
		expiresAt:  s.now().Add(s.ttl),
	}
	s.mu.Unlock()
	return token, nil
}

// Lookup retrieves the actor stored for a token. Returns ok=false if the
// token is missing, expired, OR if the stored entry's pluginName does not
// match the caller's. All three failure modes are indistinguishable to
// callers (the security log records the specific reason at the call site).
//
// pluginName tagging is defense-in-depth on top of 128-bit token entropy:
// if a future host bug ever lets plugin A's gRPC client invoke plugin B's
// server, the mismatch trips EMIT_TOKEN_REJECTED rather than allowing
// actor escalation.
func (s *emitTokenStore) Lookup(pluginName, token string) (core.Actor, bool) {
	s.mu.RLock()
	entry, ok := s.items[token]
	s.mu.RUnlock()
	if !ok {
		return core.Actor{}, false
	}
	if entry.pluginName != pluginName {
		return core.Actor{}, false
	}
	if !s.now().Before(entry.expiresAt) {
		return core.Actor{}, false
	}
	return entry.actor, true
}

// Revoke removes a token entry. Idempotent.
func (s *emitTokenStore) Revoke(token string) {
	s.mu.Lock()
	delete(s.items, token)
	s.mu.Unlock()
}

// Run starts the background sweeper goroutine. Terminates when ctx is
// canceled OR Close is called.
func (s *emitTokenStore) Run(ctx context.Context) {
	t := time.NewTicker(s.sweep)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-t.C:
			s.sweepExpired()
		}
	}
}

// Close stops the sweeper goroutine and clears all entries.
func (s *emitTokenStore) Close() error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	close(s.stop)
	s.items = make(map[string]emitTokenEntry)
	s.mu.Unlock()
	return nil
}

func (s *emitTokenStore) sweepExpired() {
	now := s.now()
	s.mu.Lock()
	for tok, entry := range s.items {
		if !now.Before(entry.expiresAt) {
			delete(s.items, tok)
		}
	}
	s.mu.Unlock()
}
```

- [ ] **Step 4: Add `goleak` dep if missing**

Run: `go get go.uber.org/goleak` (if needed)

Then `go mod tidy` to clean up.

- [ ] **Step 5: Run the tests to verify PASS (TDD green)**

Run: `task test -- ./internal/plugin/goplugin/...`

Expected: all `TestEmitTokenStore*` PASS, including goroutine-leak assertions.

- [ ] **Step 6: Run lint**

Run: `task lint`

Expected: PASS.

- [ ] **Step 7: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(goplugin): add emitTokenStore — per-dispatch token authentication for plugin EmitEvent (G1 foundation)

New package-private struct internal/plugin/goplugin/emit_token_store.go.
Implements per-dispatch random tokens (16 bytes from crypto/rand,
base64url-encoded, 22 chars) keyed in a sync map with TTL = 60 ×
DefaultEventTimeout (5 min). Background sweeper GCs expired entries
every 30s.

API:
- Issue(pluginName, actor) (token, err) — host issues at outgoing call.
- Lookup(pluginName, token) (actor, ok) — host looks up at EmitEvent.
- Revoke(token) — happy-path cleanup via defer at issuance site.
- Run(ctx) / Close() — sweeper lifecycle.

pluginName tagging is defense-in-depth on top of 128-bit token entropy
to defeat cross-plugin token leakage. Concurrent-safe; idempotent
Revoke; goroutine-leak-tested.

Not yet wired into Host — that's the next task. Pure unit at this
commit; tests cover happy path, expired lookup, mismatched pluginName,
unknown token, idempotent revoke, sweeper expiry, Close goroutine
cleanup, token format, uniqueness across 10k issuances, concurrent
issue/lookup/revoke safety, and crypto/rand failure mode.

Bead: holomush-ec22.1 (G1)"
```

(Do NOT run `jj new`.)

---

## Task 7: Wire `emitTokenStore` into `Host` (lifecycle)

**Why seventh:** Plumb the store into `Host` construction and `Close`. Pure structural; no behavior change yet (issuance comes in Task 8). TDD: assert `NewHost()` returns a Host with a non-nil `tokenStore`; assert `Close()` invokes `tokenStore.Close()`.

**Files:**
- Modify: `internal/plugin/goplugin/host.go::Host` struct + `NewHost`/`NewHostWithFactory` + `Close`
- Modify: `internal/plugin/goplugin/host_test.go`

- [ ] **Step 0: Start a fresh jj change**

```bash
jj new
jj st
jj log -r '@-' --no-pager   # MUST show Task 6's commit
```

- [ ] **Step 1: Write the failing tests (TDD red)**

In `internal/plugin/goplugin/host_test.go`, append:

```go
// TestNewHostInitializesTokenStore verifies Host construction wires the
// emitTokenStore and starts its sweeper goroutine.
func TestNewHostInitializesTokenStore(t *testing.T) {
    t.Parallel()
    defer goleak.VerifyNone(t)
    h := NewHost()
    require.NotNil(t, h.tokenStore, "Host must construct emitTokenStore")
    require.NoError(t, h.Close(context.Background()))
}

// TestHostCloseClosesTokenStore verifies Host.Close shuts the token
// store down (sweeper goroutine exits, entries cleared).
func TestHostCloseClosesTokenStore(t *testing.T) {
    t.Parallel()
    defer goleak.VerifyNone(t)
    h := NewHost()
    _, err := h.tokenStore.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"})
    require.NoError(t, err)
    require.NoError(t, h.Close(context.Background()))
    // After Close, the token store is reset.
    h.tokenStore.mu.RLock()
    n := len(h.tokenStore.items)
    h.tokenStore.mu.RUnlock()
    assert.Equal(t, 0, n)
}
```

- [ ] **Step 2: Run the failing tests (TDD red)**

Run: `task test -- -run 'TestNewHostInitializesTokenStore|TestHostCloseClosesTokenStore' ./internal/plugin/goplugin/`

Expected: COMPILATION ERROR — `Host` has no `tokenStore` field.

- [ ] **Step 3: Wire `emitTokenStore` into `Host`**

In `internal/plugin/goplugin/host.go`:

```go
type Host struct {
    // ... existing fields ...
    tokenStore *emitTokenStore
}
```

In `NewHostWithFactory`. The sweeper goroutine MUST be tied to a context the Host owns so `Close()` deterministically stops it (per spec §6 risk #2 + plan-reviewer finding B8). Add a host-owned `tokenStoreCtx context.Context` and `tokenStoreCancel context.CancelFunc`:

```go
func NewHostWithFactory(factory ClientFactory, opts ...HostOption) *Host {
    if factory == nil {
        panic("goplugin: factory cannot be nil")
    }
    tokenStoreCtx, tokenStoreCancel := context.WithCancel(context.Background())
    h := &Host{
        clientFactory:    factory,
        plugins:          make(map[string]*loadedPlugin),
        tokenStore:       newEmitTokenStore(),
        tokenStoreCtx:    tokenStoreCtx,
        tokenStoreCancel: tokenStoreCancel,
    }
    for _, opt := range opts {
        opt(h)
    }
    // Start the token-store sweeper. tokenStoreCancel (called from Close)
    // signals the sweeper to exit; tokenStore.Close() is idempotent and
    // also closes the s.stop channel as a belt-and-braces safety.
    go h.tokenStore.Run(h.tokenStoreCtx)
    return h
}
```

In `Host.Close` (find the existing function at `internal/plugin/goplugin/host.go:784`). The existing impl is:

```go
func (h *Host) Close(_ context.Context) error {
    h.mu.Lock()
    defer h.mu.Unlock()

    if h.closed {
        return nil
    }

    for _, p := range h.plugins {
        if p.client != nil {
            p.client.Kill()
        }
        if p.certDir != "" {
            _ = os.RemoveAll(p.certDir) //nolint:errcheck
        }
    }

    h.closed = true
    clear(h.plugins)
    return nil
}
```

Augment it with token-store teardown — keep the existing lock-defer pattern and the `_ context.Context` receiver-arg (sweeper cancellation comes from the host-owned `tokenStoreCancel` field, not the ctx parameter), and call `tokenStoreCancel()` + `tokenStore.Close()` AFTER existing teardown but BEFORE the final `return nil`:

```go
func (h *Host) Close(_ context.Context) error {
    h.mu.Lock()
    defer h.mu.Unlock()

    if h.closed {
        return nil
    }

    for _, p := range h.plugins {
        if p.client != nil {
            p.client.Kill()
        }
        if p.certDir != "" {
            _ = os.RemoveAll(p.certDir) //nolint:errcheck
        }
    }

    h.closed = true
    clear(h.plugins)

    // Token-store teardown (Task 7): cancel sweeper context first so the
    // sweeper goroutine exits on ctx.Done, then close the store. Surfaces
    // tokenStore.Close error since the existing function previously
    // returned nil unconditionally.
    h.tokenStoreCancel()
    if err := h.tokenStore.Close(); err != nil {
        return err
    }
    return nil
}
```

Add the matching fields to the `Host` struct:

```go
type Host struct {
    // ... existing fields ...
    tokenStore       *emitTokenStore
    tokenStoreCtx    context.Context
    tokenStoreCancel context.CancelFunc
}
```

(The existing `Close` returned `nil` unconditionally; surfacing `tokenStore.Close`'s error is a strict superset. If a future patch starts accumulating other errors, wrap with `errors.Join`.)

- [ ] **Step 4: Run the tests to verify PASS**

Run: `task test -- -run 'TestNewHostInitializesTokenStore|TestHostCloseClosesTokenStore' ./internal/plugin/goplugin/`

Expected: PASS, no goroutine leaks.

- [ ] **Step 5: Run full `internal/plugin/goplugin/` tests**

Run: `task test -- ./internal/plugin/goplugin/...`

Expected: all PASS.

- [ ] **Step 6: Run lint**

Run: `task lint`

Expected: PASS.

- [ ] **Step 7: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(goplugin): wire emitTokenStore into Host lifecycle

Host struct gains *emitTokenStore field; NewHostWithFactory constructs
it via newEmitTokenStore() and starts the background sweeper goroutine.
Host.Close() invokes tokenStore.Close() to stop the sweeper and clear
entries.

Pure structural change — no behavior gates yet. Token issuance and
EmitEvent lookup come in subsequent commits.

Bead: holomush-ec22.1 (G1 wiring)"
```

(Do NOT run `jj new`.)

---

## Task 8: Token issuance at `Host.DeliverEvent` and `Host.DeliverCommand`

**Why eighth:** Now that the store is wired in (Task 7), the host can issue tokens at the outgoing-call boundary with the actor re-anchor logic per spec §3.3.4. TDD: assert outgoing metadata contains `x-holomush-emit-token`; assert store has matching entry; assert defer-revoke clears it; assert ActorSystem re-anchored.

**Files:**
- Modify: `internal/plugin/goplugin/host.go::DeliverEvent` (~line 540) + `DeliverCommand` (~line 592)
- Modify: `internal/plugin/goplugin/host_test.go`

- [ ] **Step 0: Start a fresh jj change**

```bash
jj new
jj st
jj log -r '@-' --no-pager   # MUST show Task 7's commit
```

- [ ] **Step 1: Write the failing tests (TDD red)**

In `internal/plugin/goplugin/host_test.go`:

```go
// Use the EXISTING scaffolding from host_test.go:
//   - mockPluginClient + mockGRPCPluginClient at lines 82-141 (mockGRPCPluginClient
//     captures the incoming ctx on HandleEvent via `eventCtx`).
//   - NewHostWithFactory(&mockClientFactory{client: mockClient}) at e.g.
//     :1283 — constructs a real Host backed by a mock plugin process.
//   - Manual host.plugins["plug-A"] = &loadedPlugin{...} insertion bypasses
//     Load() for in-test plugin registration.

// TestDeliverEventIssuesTokenWithCharacterActor verifies the host issues
// a per-dispatch token, attaches it to outgoing metadata, stores the
// upstream actor verbatim, and revokes on call return.
func TestDeliverEventIssuesTokenWithCharacterActor(t *testing.T) {
    t.Parallel()
    grpcClient := &mockGRPCPluginClient{}
    mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
    h := NewHostWithFactory(&mockClientFactory{client: mockClient})
    defer h.Close(context.Background())
    h.plugins["plug-A"] = &loadedPlugin{
        manifest: &plugins.Manifest{Name: "plug-A"},
        plugin:   grpcClient,
    }

    charID := "01HCHAR0000000000000000000"
    ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: charID})
    _, err := h.DeliverEvent(ctx, "plug-A", pluginsdk.Event{Type: "say"})
    require.NoError(t, err)

    // Inspect the OUTGOING metadata that the host attached to the gRPC call.
    // (mockGRPCPluginClient.eventCtx is set in HandleEvent at line 130.)
    md, ok := metadata.FromOutgoingContext(grpcClient.eventCtx)
    require.True(t, ok, "outgoing metadata MUST be present on the call to plugin")
    tokens := md.Get("x-holomush-emit-token")
    require.Len(t, tokens, 1, "x-holomush-emit-token MUST be set exactly once")
    capturedToken := tokens[0]
    require.NotEmpty(t, capturedToken)

    // The entry will already be Revoke'd by defer when DeliverEvent returns.
    // Note: this test cannot inspect the in-flight stored actor without
    // capturing it from inside the mock plugin's HandleEvent. Two follow-up
    // tests below assert in-flight state via a captured-ctx mockGRPCPluginClient.
    _, ok = h.tokenStore.Lookup("plug-A", capturedToken)
    assert.False(t, ok, "deferred Revoke MUST clear the token after DeliverEvent returns")
}

// TestDeliverEventStoresUpstreamCharacterActorVerbatim asserts the token
// store has ActorCharacter:<charID> at the time the plugin's HandleEvent
// is called. Uses a captured-from-handler pattern: the mock plugin client's
// HandleEvent reads the token from incoming metadata (it sees the same
// metadata the host attached as outgoing, since the gRPC mock pipes
// outgoing→incoming for in-process testing) and looks up the store.
func TestDeliverEventStoresUpstreamCharacterActorVerbatim(t *testing.T) {
    t.Parallel()
    grpcClient := &mockGRPCPluginClient{}
    mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
    h := NewHostWithFactory(&mockClientFactory{client: mockClient})
    defer h.Close(context.Background())
    h.plugins["plug-A"] = &loadedPlugin{
        manifest: &plugins.Manifest{Name: "plug-A"},
        plugin:   grpcClient,
    }

    charID := "01HCHAR0000000000000000000"
    ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: charID})
    _, err := h.DeliverEvent(ctx, "plug-A", pluginsdk.Event{Type: "say"})
    require.NoError(t, err)

    // Read the outgoing token; while the call was in-flight the token store
    // had the entry. We can re-issue with the same fake parameters and verify
    // the re-anchor logic by inspecting what the host attached to outgoing
    // metadata via WithOutgoingActorMetadata: the kind/id reflect the stored
    // actor (verbatim ActorCharacter for this case).
    kind, id, ok := pluginsdk.ActorMetadataFromOutgoingContext(grpcClient.eventCtx)
    require.True(t, ok)
    assert.Equal(t, pluginsdk.ActorCharacter, kind)
    assert.Equal(t, charID, id)
}

// TestDeliverEventReanchorsActorSystem verifies spec §3.3.4: when ctx has
// ActorSystem, the host attaches ActorPlugin:<self> as outgoing metadata
// (re-anchored). Plugins can never speak as the host's system identity.
func TestDeliverEventReanchorsActorSystem(t *testing.T) {
    t.Parallel()
    grpcClient := &mockGRPCPluginClient{}
    mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
    h := NewHostWithFactory(&mockClientFactory{client: mockClient})
    defer h.Close(context.Background())
    h.plugins["plug-A"] = &loadedPlugin{
        manifest: &plugins.Manifest{Name: "plug-A"},
        plugin:   grpcClient,
    }

    ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorSystem, ID: core.ActorSystemID})
    _, err := h.DeliverEvent(ctx, "plug-A", pluginsdk.Event{Type: "tick"})
    require.NoError(t, err)

    kind, id, ok := pluginsdk.ActorMetadataFromOutgoingContext(grpcClient.eventCtx)
    require.True(t, ok)
    assert.Equal(t, pluginsdk.ActorPlugin, kind, "ActorSystem MUST be re-anchored to ActorPlugin at issuance")
    assert.Equal(t, "plug-A", id)
}

// TestDeliverEventNoActorFallsBackToPluginIdentity covers the bootstrap
// edge case: no actor in ctx → token store entry has ActorPlugin:<self>.
func TestDeliverEventNoActorFallsBackToPluginIdentity(t *testing.T) {
    t.Parallel()
    grpcClient := &mockGRPCPluginClient{}
    mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
    h := NewHostWithFactory(&mockClientFactory{client: mockClient})
    defer h.Close(context.Background())
    h.plugins["plug-A"] = &loadedPlugin{
        manifest: &plugins.Manifest{Name: "plug-A"},
        plugin:   grpcClient,
    }

    _, err := h.DeliverEvent(context.Background(), "plug-A", pluginsdk.Event{Type: "test"})
    require.NoError(t, err)

    kind, id, ok := pluginsdk.ActorMetadataFromOutgoingContext(grpcClient.eventCtx)
    require.True(t, ok)
    assert.Equal(t, pluginsdk.ActorPlugin, kind)
    assert.Equal(t, "plug-A", id)
}

// TestDeliverEventNoRecoverWrapper (N2 from spec §5.5 future-proofing) — asserts
// Host.DeliverEvent does NOT contain a recover() call that would swallow
// host-side panics and skip the deferred token Revoke. Static-analysis
// approach via go/ast.
func TestDeliverEventNoRecoverWrapper(t *testing.T) {
    t.Parallel()
    fset := token.NewFileSet()
    file, err := parser.ParseFile(fset, "host.go", nil, parser.ParseComments)
    require.NoError(t, err)
    var deliverEventDecl *ast.FuncDecl
    for _, decl := range file.Decls {
        if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "DeliverEvent" {
            deliverEventDecl = fn
            break
        }
    }
    require.NotNil(t, deliverEventDecl, "DeliverEvent function must be in host.go")
    ast.Inspect(deliverEventDecl, func(n ast.Node) bool {
        call, ok := n.(*ast.CallExpr)
        if !ok {
            return true
        }
        ident, ok := call.Fun.(*ast.Ident)
        if !ok {
            return true
        }
        assert.NotEqual(t, "recover", ident.Name, "Host.DeliverEvent MUST NOT contain recover() — would swallow panics and skip deferred token Revoke")
        return true
    })
}
```

**Imports needed in `host_test.go`**:
- `"google.golang.org/grpc/metadata"` (verify against existing imports — the file already imports gRPC metadata at line 1353; confirm)
- `"go/ast"`, `"go/parser"`, `"go/token"` for the recover() static-analysis test (new — Task 8 introduces these)

- [ ] **Step 2: Run the failing tests (TDD red)**

Run: `task test -- -run 'TestDeliverEventIssuesToken|TestDeliverEventReanchorsActorSystem|TestDeliverEventNoActorFallsBackToPluginIdentity' ./internal/plugin/goplugin/`

Expected: FAIL — `DeliverEvent` doesn't issue tokens yet.

- [ ] **Step 3: Add token issuance to `Host.DeliverEvent` (TDD green)**

In `internal/plugin/goplugin/host.go::DeliverEvent`, find the actor-handling block at lines 537-541:

```go
callCtx, cancel := context.WithTimeout(ctx, DefaultEventTimeout)
defer cancel()
if actor, ok := core.ActorFromContext(ctx); ok {
    callCtx = pluginsdk.WithOutgoingActorMetadata(callCtx, coreActorKindToSDK(actor.Kind), actor.ID)
}
```

Replace with the issuance flow:

```go
callCtx, cancel := context.WithTimeout(ctx, DefaultEventTimeout)
defer cancel()

// Compute the stored actor with re-anchor for ActorSystem (per spec §3.3.4).
storedActor := core.Actor{Kind: core.ActorPlugin, ID: name} // default
if upstream, ok := core.ActorFromContext(ctx); ok {
    switch upstream.Kind {
    case core.ActorCharacter, core.ActorPlugin:
        storedActor = upstream // verbatim — cascade preserved
    case core.ActorSystem:
        storedActor = core.Actor{Kind: core.ActorPlugin, ID: name} // re-anchor
    }
}

token, err := h.tokenStore.Issue(name, storedActor)
if err != nil {
    return nil, oops.In("goplugin").With("plugin", name).Wrap(err)
}
defer h.tokenStore.Revoke(token)

callCtx = metadata.AppendToOutgoingContext(callCtx, "x-holomush-emit-token", token)
// Existing actor-kind/-id metadata still attached for plugin-side advisory consumption
// (per pkg/plugin/sdk.go:195's contextWithIncomingActorMetadata reader).
callCtx = pluginsdk.WithOutgoingActorMetadata(callCtx, coreActorKindToSDK(storedActor.Kind), storedActor.ID)
```

Add `metadata "google.golang.org/grpc/metadata"` to the imports if not present.

Apply the SAME pattern to `Host.DeliverCommand` (around line 591):

```go
callCtx, cancel := context.WithTimeout(ctx, DefaultEventTimeout)
defer cancel()

storedActor := core.Actor{Kind: core.ActorPlugin, ID: name}
if upstream, ok := core.ActorFromContext(ctx); ok {
    switch upstream.Kind {
    case core.ActorCharacter, core.ActorPlugin:
        storedActor = upstream
    case core.ActorSystem:
        storedActor = core.Actor{Kind: core.ActorPlugin, ID: name}
    }
}

token, err := h.tokenStore.Issue(name, storedActor)
if err != nil {
    return nil, oops.In("goplugin").With("plugin", name).Wrap(err)
}
defer h.tokenStore.Revoke(token)

callCtx = metadata.AppendToOutgoingContext(callCtx, "x-holomush-emit-token", token)
callCtx = pluginsdk.WithOutgoingActorMetadata(callCtx, coreActorKindToSDK(storedActor.Kind), storedActor.ID)
```

- [ ] **Step 4: Run the tests to verify PASS (TDD green)**

Run: `task test -- -run 'TestDeliverEventIssuesToken|TestDeliverEventReanchorsActorSystem|TestDeliverEventNoActorFallsBackToPluginIdentity' ./internal/plugin/goplugin/`

Expected: PASS.

- [ ] **Step 5: Run all `internal/plugin/goplugin/` tests for regressions**

Run: `task test -- ./internal/plugin/goplugin/...`

Expected: all PASS.

- [ ] **Step 6: Run lint**

Run: `task lint`

Expected: PASS.

- [ ] **Step 7: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(goplugin): issue per-dispatch token on Host.DeliverEvent / DeliverCommand (G1 issuance)

Per spec §3.3.4: every binary-plugin outgoing call now issues a token
via h.tokenStore.Issue(pluginName, storedActor). The stored actor
follows the re-anchor rule:
- ActorCharacter / ActorPlugin: verbatim (cascade preserved).
- ActorSystem: re-anchored to ActorPlugin:<currentPluginName>
  (plugins never speak as host's system identity).
- Absent: ActorPlugin:<currentPluginName> (existing fallback).

Token attached to outgoing gRPC metadata as
x-holomush-emit-token. Existing x-holomush-actor-kind / -actor-id
headers continue to ferry advisory metadata for plugin-side reading
(pkg/plugin/sdk.go:195 / pluginsdk.ActorMetadataFromIncomingContext).

defer h.tokenStore.Revoke(token) clears the entry on call return
(happy path); TTL-based sweeper (Task 6) GCs entries that escape
the defer (e.g., abnormal goroutine termination).

EmitEvent doesn't read the token yet — that's the next task.

Bead: holomush-ec22.1 (G1 issuance)"
```

(Do NOT run `jj new`.)

---

## Task 9: `EmitEvent` — token-based authentication, ignore plugin's metadata claims

**Why ninth:** The store has tokens (Task 8); now `EmitEvent` looks them up instead of trusting the plugin's metadata. This is the load-bearing security change — closes the forgery surface. TDD: forgery override test (plugin substitutes kind/id headers but token is honest → host uses token's stored actor); missing token; unknown token; cross-plugin token leak.

**Files:**
- Modify: `internal/plugin/goplugin/host_service.go::EmitEvent` (lines 39-74)
- Modify: `internal/plugin/goplugin/host_service_test.go`

- [ ] **Step 0: Start a fresh jj change**

```bash
jj new
jj st
jj log -r '@-' --no-pager   # MUST show Task 8's commit
```

- [ ] **Step 1: Write the failing tests (TDD red)**

In `internal/plugin/goplugin/host_service_test.go`:

```go
// Test scaffolding strategy: this test file is in `package goplugin`, so
// it has access to the unexported `pluginHostServiceServer` struct and
// can construct it directly. We pair it with a real Host that has a
// real PluginEventEmitter wired to an embedded JetStream bus
// (eventbustest.New(t)), so emits can be inspected via fetchAllMessages
// (the same pattern as event_emitter_test.go).

// newTestHostWithEmitter constructs a Host with a real eventEmitter
// wired to the given embedded bus. Caller-supplied manifest is the
// lookup target for emitter manifest validation.
func newTestHostWithEmitter(t *testing.T, bus *eventbustest.Embedded, pluginName string, manifest *plugins.Manifest) *Host {
    t.Helper()
    publisher := bus.Bus.Publisher()
    require.NotNil(t, publisher)
    emitter := plugins.NewPluginEventEmitter(publisher,
        func(name string) *plugins.Manifest {
            if name == pluginName {
                return manifest
            }
            return nil
        },
        func(ctx context.Context, _ string) (core.Actor, error) {
            actor, ok := core.ActorFromContext(ctx)
            if !ok {
                return core.Actor{}, oops.New("plugin event actor missing from context")
            }
            return actor, nil
        },
    )
    h := NewHost()
    h.SetEventEmitter(emitter) // public method per host.go (used at host_test.go:1346)
    return h
}

// TestEmitEventUsesStoredActorIgnoringPluginClaim covers the load-bearing
// G1 invariant: a plugin substituting the actor-kind/id headers but
// ferrying a valid token MUST get an event stamped with the host's
// stored actor, NOT the plugin's claim.
func TestEmitEventUsesStoredActorIgnoringPluginClaim(t *testing.T) {
    t.Parallel()
    bus := eventbustest.New(t)
    manifest := &plugins.Manifest{
        Name: "plug-A", Type: plugins.TypeBinary, Emits: []string{"location"},
        ActorKindsClaimable: []string{"plugin", "character"},
    }
    h := newTestHostWithEmitter(t, bus, "plug-A", manifest)
    defer h.Close(context.Background())

    // Construct the server struct directly (we're in package goplugin).
    s := &pluginHostServiceServer{host: h, pluginName: "plug-A"}

    // Host issues a token storing the legitimate dispatching character.
    storedActor := core.Actor{Kind: core.ActorCharacter, ID: "01HCORRECT000000000000000C"}
    token, err := h.tokenStore.Issue("plug-A", storedActor)
    require.NoError(t, err)
    defer h.tokenStore.Revoke(token)

    // Plugin (in test simulation) substitutes a forged actor-kind/id claim
    // but ferries the valid token.
    md := metadata.New(map[string]string{
        "x-holomush-emit-token": token,
        "x-holomush-actor-kind": strconv.Itoa(int(pluginsdk.ActorCharacter)),
        "x-holomush-actor-id":   "01HFORGED000000000000000000",
    })
    ctx := metadata.NewIncomingContext(context.Background(), md)

    _, err = s.EmitEvent(ctx, &pluginv1.PluginHostServiceEmitEventRequest{
        Stream:    "location:01HLOC0000000000000000000",
        EventType: "say",
        Payload:   []byte(`{"message":"hi"}`),
    })
    require.NoError(t, err)

    // Inspect the published event via the embedded bus.
    msgs := fetchAllMessages(t, bus.JS)
    require.Len(t, msgs, 1)
    assert.Equal(t, "character", msgs[0].Header.Get(eventbus.HeaderActorKind))
    assert.Equal(t, "01HCORRECT000000000000000C", msgs[0].Header.Get(eventbus.HeaderActorID),
        "event MUST carry the host-stored actor, not the plugin's claim")
    assert.NotEqual(t, "01HFORGED000000000000000000", msgs[0].Header.Get(eventbus.HeaderActorID))
}

// TestEmitEventMissingTokenFails covers spec §3.3.5 EMIT_TOKEN_MISSING.
func TestEmitEventMissingTokenFails(t *testing.T) {
    t.Parallel()
    bus := eventbustest.New(t)
    manifest := &plugins.Manifest{Name: "plug-A", Type: plugins.TypeBinary, Emits: []string{"location"}, ActorKindsClaimable: []string{"plugin", "character"}}
    h := newTestHostWithEmitter(t, bus, "plug-A", manifest)
    defer h.Close(context.Background())
    s := &pluginHostServiceServer{host: h, pluginName: "plug-A"}
    md := metadata.New(map[string]string{}) // no token
    ctx := metadata.NewIncomingContext(context.Background(), md)
    _, err := s.EmitEvent(ctx, &pluginv1.PluginHostServiceEmitEventRequest{
        Stream: "location:01HLOC0000000000000000000",
        EventType: "say",
        Payload: []byte(`{}`),
    })
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "EMIT_TOKEN_MISSING")
}

// TestEmitEventUnknownTokenFails covers EMIT_TOKEN_REJECTED for an
// unrecognized token.
func TestEmitEventUnknownTokenFails(t *testing.T) {
    t.Parallel()
    bus := eventbustest.New(t)
    manifest := &plugins.Manifest{Name: "plug-A", Type: plugins.TypeBinary, Emits: []string{"location"}, ActorKindsClaimable: []string{"plugin", "character"}}
    h := newTestHostWithEmitter(t, bus, "plug-A", manifest)
    defer h.Close(context.Background())
    s := &pluginHostServiceServer{host: h, pluginName: "plug-A"}
    md := metadata.New(map[string]string{
        "x-holomush-emit-token": "not-a-real-token",
    })
    ctx := metadata.NewIncomingContext(context.Background(), md)
    _, err := s.EmitEvent(ctx, &pluginv1.PluginHostServiceEmitEventRequest{
        Stream: "location:01HLOC0000000000000000000",
        EventType: "test",
        Payload: []byte(`{}`),
    })
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "EMIT_TOKEN_REJECTED")
}

// TestEmitEventCrossPluginTokenLeakFails covers cross-plugin defense:
// plug-A's token used by plug-B's server → reject.
func TestEmitEventCrossPluginTokenLeakFails(t *testing.T) {
    t.Parallel()
    bus := eventbustest.New(t)
    manifestA := &plugins.Manifest{Name: "plug-A", Type: plugins.TypeBinary, Emits: []string{"location"}, ActorKindsClaimable: []string{"plugin", "character"}}
    h := newTestHostWithEmitter(t, bus, "plug-A", manifestA)
    defer h.Close(context.Background())

    // Issue a token for plug-A.
    tok, err := h.tokenStore.Issue("plug-A", core.Actor{Kind: core.ActorCharacter, ID: "01HCHAR0000000000000000000"})
    require.NoError(t, err)

    // Invoke EmitEvent on plug-B's server (different pluginName).
    sB := &pluginHostServiceServer{host: h, pluginName: "plug-B"}
    md := metadata.New(map[string]string{"x-holomush-emit-token": tok})
    ctx := metadata.NewIncomingContext(context.Background(), md)
    _, err = sB.EmitEvent(ctx, &pluginv1.PluginHostServiceEmitEventRequest{
        Stream:    "location:01HLOC0000000000000000000",
        EventType: "test",
        Payload:   []byte(`{}`),
    })
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "EMIT_TOKEN_REJECTED")
}
```

**Imports needed in `host_service_test.go`** — verify against the existing import block (the existing file at `host_service_test.go:1-30` imports most of these). Required additions:

- `"strconv"` (for the ActorCharacter kind serialization in the forgery test)
- `"google.golang.org/grpc/metadata"` (for metadata.New / NewIncomingContext)
- `"github.com/holomush/holomush/internal/eventbus"` (for eventbus.HeaderActorKind / HeaderActorID constants)
- `"github.com/holomush/holomush/internal/eventbus/eventbustest"` (for eventbustest.New)
- `"github.com/samber/oops"` (for oops.New in newTestHostWithEmitter)

The package name of the test file remains `goplugin` (unqualified in-package access to `pluginHostServiceServer` and `Host` fields).

- [ ] **Step 2: Run the failing tests (TDD red)**

Run: `task test -- -run 'TestEmitEventUsesStoredActor|TestEmitEventMissingTokenFails|TestEmitEventUnknownTokenFails|TestEmitEventCrossPluginTokenLeakFails' ./internal/plugin/goplugin/`

Expected: FAIL — current EmitEvent trusts kind/id metadata.

- [ ] **Step 3: Refactor `EmitEvent` to use token lookup (TDD green)**

In `internal/plugin/goplugin/host_service.go::EmitEvent`, replace lines 51-62 (the metadata-trust + fallback block):

```go
emitCtx := ctx
if kind, id, ok := pluginsdk.ActorMetadataFromIncomingContext(ctx); ok {
    emitCtx = core.WithActor(ctx, core.Actor{
        Kind: sdkActorKindToCore(kind),
        ID:   id,
    })
} else {
    emitCtx = core.WithActor(emitCtx, core.Actor{
        Kind: core.ActorPlugin,
        ID:   s.pluginName,
    })
}
```

with the token-based flow:

```go
md, _ := metadata.FromIncomingContext(ctx)
tokens := md.Get("x-holomush-emit-token")
if len(tokens) == 0 || tokens[0] == "" {
    return nil, oops.Code("EMIT_TOKEN_MISSING").
        With("plugin", s.pluginName).
        Errorf("plugin emitted without a host-issued dispatch token")
}

s.host.mu.RLock()
tokenStore := s.host.tokenStore
s.host.mu.RUnlock()

storedActor, ok := tokenStore.Lookup(s.pluginName, tokens[0])
if !ok {
    slog.WarnContext(ctx, "EmitEvent rejected: token not valid for this plugin",
        "plugin", s.pluginName,
        "code", "EMIT_TOKEN_REJECTED",
    )
    return nil, oops.Code("EMIT_TOKEN_REJECTED").
        With("plugin", s.pluginName).
        Errorf("dispatch token is not valid for this plugin")
}

emitCtx := core.WithActor(ctx, storedActor)
```

Add `metadata "google.golang.org/grpc/metadata"` and `"log/slog"` to imports if not present.

- [ ] **Step 4: Run the tests to verify PASS (TDD green)**

Run: `task test -- -run 'TestEmitEventUsesStoredActor|TestEmitEventMissingTokenFails|TestEmitEventUnknownTokenFails|TestEmitEventCrossPluginTokenLeakFails' ./internal/plugin/goplugin/`

Expected: PASS.

- [ ] **Step 5: Run all `internal/plugin/goplugin/` tests for regressions**

Run: `task test -- ./internal/plugin/goplugin/...`

Expected: all PASS.

- [ ] **Step 6: Run all `internal/plugin/` tests (full plugin package)**

Run: `task test -- ./internal/plugin/...`

Expected: all PASS.

- [ ] **Step 7: Run lint**

Run: `task lint`

Expected: PASS.

- [ ] **Step 8: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(goplugin): EmitEvent uses host-issued token, ignores plugin's actor-claim metadata (G1 closure)

Per spec §3.3.5: pluginHostServiceServer.EmitEvent now reads the
x-holomush-emit-token incoming metadata header, looks it up in the
token store keyed by (s.pluginName, token), and uses the host-stored
actor verbatim. The plugin's x-holomush-actor-kind / -actor-id
header values are NO LONGER trusted as identity claims at this
boundary (they remain advisory for plugin-side reading per
pkg/plugin/sdk.go:195).

Failure modes (loud, no silent fallback):
- Missing token: EMIT_TOKEN_MISSING.
- Unknown / expired / cross-plugin token: EMIT_TOKEN_REJECTED
  (logged with security warning).

This closes the load-bearing G1 invariant: a plugin can substitute
the actor-claim headers but cannot fabricate a token (128-bit
crypto/rand entropy + per-dispatch issuance + pluginName tagging),
so the published event is always stamped with the host's vouched
actor.

Bead: holomush-ec22.1 (G1)"
```

(Do NOT run `jj new`.)

---

## Task 10: Integration forgery test (§5.6) — full e2e

**Why tenth:** Full-stack test exercising the gRPC metadata path with a real binary plugin and a real Lua plugin. This is the load-bearing G1 + G2 verification.

**Files:**
- Create: `test/integration/plugin/actor_authentication_test.go` (NEW file in the EXISTING `test/integration/plugin/` Ginkgo suite — `binary_plugin_test.go`, `extensible_actions_test.go`, etc. live here)
- Optional create (only if a forgery-capable test plugin doesn't already exist): `test/integration/plugin/testdata/forgery_plugin/main.go` — a binary plugin built specifically for this test that allows test-controlled header substitution before EmitEvent. Reuse the existing `testdata/` patterns.

**Note:** the plan formerly proposed a new `test/integration/plugin_e2e/` directory with an invented harness. The existing `test/integration/plugin/` Ginkgo suite already loads real binary plugins via the `BinaryPluginSuite` and exercises the gRPC boundary. The forgery scenarios are added there as new `Describe` blocks, riding on the existing Postgres testcontainer + plugin-loading infrastructure.

- [ ] **Step 0: Start a fresh jj change**

```bash
jj new
jj st
jj log -r '@-' --no-pager   # MUST show Task 9's commit
```

- [ ] **Step 1: Inspect the existing `test/integration/plugin/` Ginkgo suite**

Run: `Read internal/plugin/integration tests at test/integration/plugin/binary_plugin_test.go and test/integration/plugin/plugin_suite_test.go`. Identify the existing scaffolding (the file imports `. github.com/onsi/ginkgo/v2` and follows the `BeforeSuite`/`Describe`/`It` pattern). The suite likely already has a Postgres testcontainer and binary-plugin-loading helpers — REUSE these rather than inventing new ones.

- [ ] **Step 2: Write the integration tests**

Create `test/integration/plugin/actor_authentication_test.go` as a new Describe block in the existing suite. Use the existing scaffolding from `binary_plugin_test.go` (PostgreSQL container, plugin process spawning, EventBus subscription) — DO NOT invent a new harness.

Pseudocode shape (adapt to the actual `binary_plugin_test.go` scaffolding):

```go
//go:build integration
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugin_test

import (
    . "github.com/onsi/ginkgo/v2" //nolint:revive
    . "github.com/onsi/gomega"    //nolint:revive
)

var _ = Describe("Plugin actor-claim authentication (ec22.1)", func() {
    // Reuse existing BeforeEach/AfterEach from binary_plugin_test.go that
    // spawns a real binary plugin process. Pass it test-control flags via
    // env vars or proto extensions (see existing scaffolding for the
    // pattern, e.g., test-abac-widget at plugins/test-abac-widget/main.go).

    Describe("Honest dispatch", func() {
        It("publishes events stamped with the dispatching character", func() {
            // Use the existing harness's plugin-load + character-publish
            // helpers. Assert the published event's Actor.ID matches the
            // dispatching character.
        })
    })

    Describe("Forgery override (load-bearing G1)", func() {
        It("publishes events with the host-stored actor regardless of the plugin's claim", func() {
            // Spawn a test plugin with FORGERY_TARGET_KIND / FORGERY_TARGET_ID
            // env vars (the test plugin reads these and substitutes them into
            // x-holomush-actor-kind / x-holomush-actor-id headers via
            // pluginsdk.WithOutgoingActorMetadata before calling EmitEvent;
            // it does NOT touch x-holomush-emit-token).
            // Assert: the published Actor.ID is the dispatching character,
            // NOT the forgery target.
        })
    })

    Describe("Token fabrication", func() {
        It("rejects emits carrying tokens not issued by the host", func() {
            // Spawn a test plugin that overrides x-holomush-emit-token with
            // a fabricated 22-char base64url string. Assert: emit fails
            // with EMIT_TOKEN_REJECTED on the host's gRPC return.
        })
    })

    Describe("Out-of-dispatch emit", func() {
        It("rejects background emits that have no token in ctx", func() {
            // Spawn a test plugin that calls EmitEvent from a goroutine
            // after Init returns (no in-flight host dispatch). Assert:
            // EMIT_TOKEN_MISSING.
        })
    })

    Describe("Lua plugin manifest gate", func() {
        It("publishes events stamped with the dispatching character when manifest opts in", func() {
            // Load echo-bot (already migrated in Task 4 to declare
            // [plugin, character]). Trigger a character-driven say. Assert
            // the published echo emit has Actor.Kind=character.
        })

        It("rejects emit attempts when the manifest does not opt in", func() {
            // Load a test Lua plugin with actor_kinds_claimable: [plugin]
            // (no character). Trigger a character-driven event the plugin
            // subscribes to. Assert the host's emit path returns
            // EMIT_ACTOR_KIND_NOT_CLAIMABLE.
        })
    })
})
```

The exact `BeforeEach` / `AfterEach` plumbing depends on the existing `plugin_suite_test.go`. The implementer's job in this step is to **read the existing suite, identify the helpers, and call them** — not to invent a parallel harness.

- [ ] **Step 3: Build the test-only forgery plugin fixture**

If the existing test plugins (e.g., `test-abac-widget`) don't already support forgery-mode env vars, add a new test plugin under `test/integration/plugin/testdata/forgery_plugin/main.go` that:

```go
// Shape: a minimal binary plugin that, on each Emit, reads two env vars:
//   FORGERY_OVERRIDE_KIND (e.g., "0" for ActorCharacter)
//   FORGERY_OVERRIDE_ID   (e.g., "01HFORGED...")
// and substitutes them into the outgoing actor-kind/-id metadata via
// pluginsdk.WithOutgoingActorMetadata. The token header is left
// untouched (SDK auto-ferry preserves it).
//
// A second mode controlled by FABRICATE_TOKEN replaces the token header
// with the fabricated value via metadata.AppendToOutgoingContext (which
// overwrites for the same key on the second call — verify behavior).
//
// A third mode controlled by EMIT_FROM_BACKGROUND triggers Emit from a
// goroutine after Init returns (no in-flight host dispatch context).
```

Plugin manifest at `test/integration/plugin/testdata/forgery_plugin/plugin.yaml` declares `actor_kinds_claimable: [plugin, character]` and `type: binary`.

- [ ] **Step 4: Run the integration test**

Run: `task test:int -- ./test/integration/plugin/...`

Expected: all scenarios PASS. The forgery-override scenario is the load-bearing assertion: published Actor.ID is the dispatching character, NOT the forgery target.

- [ ] **Step 5: Run lint**

Run: `task lint`

Expected: PASS.

- [ ] **Step 6: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "test(plugin_e2e): integration forgery + Lua manifest-gate suite (spec §5.6 + §5.7)

End-to-end ginkgo specs covering:
- Honest binary dispatch: published event has dispatching character.
- Forgery override (load-bearing G1): plugin substitutes actor
  metadata headers but ferries valid token; published event has
  HOST-STORED actor, not plugin's claim.
- Token fabrication: plugin invents random token; emit fails
  EMIT_TOKEN_REJECTED.
- Out-of-dispatch emit: background goroutine emit with no token in
  ctx; emit fails EMIT_TOKEN_MISSING.
- Lua plugin with [plugin, character] manifest: cascade preserved.
- Lua plugin with [plugin] only: emit fails EMIT_ACTOR_KIND_NOT_CLAIMABLE.

Bead: holomush-ec22.1 (G1 + G2 e2e)"
```

(Do NOT run `jj new`.)

---

## Task 11: Documentation — `AGENTS.md` + `CLAUDE.md` runtime symmetry invariant (G6)

**Why eleventh:** Codifies the "binary and Lua plugins MUST be treated identically" invariant in the two human-facing AI guidance files. Anchored byte-equivalence enforced by a CI check (Task 12).

**Files:**
- Modify: `AGENTS.md`
- Modify: `CLAUDE.md`

- [ ] **Step 0: Start a fresh jj change**

```bash
jj new
jj st
jj log -r '@-' --no-pager   # MUST show Task 10's commit
```

- [ ] **Step 1: Add the symmetry subsection to `AGENTS.md`**

In `AGENTS.md`, locate the "Plugin System (`internal/plugin`)" section (around line 794). After the existing "Three plugin types" table and the manifest schema description, add:

```markdown
### Plugin Runtime Symmetry

<!-- BEGIN: plugin-runtime-symmetry -->

**Project invariant: Binary and Lua plugins MUST be treated identically by the host.**

Any host-side trust check, validation, or feature MUST apply to both binary and Lua plugins. Asymmetric behavior between plugin runtimes is forbidden — it creates a privilege gradient that violates the core plugin-system design.

When designing security or authorization features that touch plugins:

1. Find the **common code path** that handles both runtimes (e.g., `internal/plugin/event_emitter.go::Emit` is the shared emit boundary for both Lua return-value emits and binary gRPC emits).
2. Place the gate at the common path so both runtimes are enforced uniformly.
3. Runtime-specific code (e.g., the gRPC token mechanism for binary plugins, Lua state lifecycle) is acceptable for runtime-specific concerns (e.g., the binary forgery surface that doesn't exist on the Lua path), but MUST NOT differ in policy / trust / manifest-gate dimensions.

Example (this PR — `holomush-ec22.1`): the `actor_kinds_claimable` manifest gate fires at `event_emitter.go::Emit` for both runtimes; the supplemental token-authentication mechanism applies only to the binary gRPC `EmitEvent` boundary because that's where the forgery surface exists. Both runtimes reach the same policy enforcement.

<!-- END: plugin-runtime-symmetry -->
```

- [ ] **Step 2: Add the same subsection to `CLAUDE.md`**

In `CLAUDE.md`, locate the analogous "Plugin System" section. Add the SAME subsection — content between `<!-- BEGIN: plugin-runtime-symmetry -->` and `<!-- END: plugin-runtime-symmetry -->` MUST be byte-identical to the AGENTS.md version. The two files have parallel structures; the subsection placement should match.

- [ ] **Step 3: Manually verify byte-equivalence**

Run:

```bash
diff <(awk '/<!-- BEGIN: plugin-runtime-symmetry -->/,/<!-- END: plugin-runtime-symmetry -->/' AGENTS.md) <(awk '/<!-- BEGIN: plugin-runtime-symmetry -->/,/<!-- END: plugin-runtime-symmetry -->/' CLAUDE.md)
```

Expected: empty output (byte-identical).

- [ ] **Step 4: Run lint**

Run: `task lint`

Expected: PASS (markdown lint should be clean).

- [ ] **Step 5: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "docs(agents,claude): document plugin runtime-symmetry invariant (G6)

New 'Plugin Runtime Symmetry' subsection in AGENTS.md and CLAUDE.md,
delimited by anchored HTML comments for CI byte-equivalence checks
(added in next task).

Codifies the project invariant flagged 2026-04-25: binary and Lua
plugins MUST be treated identically by the host. Any host-side trust
check, validation, or feature MUST apply to both. Asymmetric behavior
is forbidden because it creates a privilege gradient.

Cites this PR's actor_kinds_claimable manifest gate as the
canonical pattern reference.

Bead: holomush-ec22.1 (G6)"
```

(Do NOT run `jj new`.)

---

## Task 12: New CI lint tasks — `lint:plugin-manifests` + `lint:docs-symmetry`

**Why twelfth:** Codifies the regression guards from spec criterion 8 (manifest claim coverage) and criterion 9 (docs byte-equivalence) as `task` targets that run in `task pr-prep`.

**Files:**
- Modify: `Taskfile.yaml`
- Create: `scripts/lint-plugin-manifests.sh`

- [ ] **Step 0: Start a fresh jj change**

```bash
jj new
jj st
jj log -r '@-' --no-pager   # MUST show Task 11's commit
```

- [ ] **Step 1: Create `cmd/lint-plugin-manifests/main.go`**

`yq` is NOT available on CI runners (verified by reviewer: `rg -n 'yq' .github/workflows/` returns zero hits). Use a Go program that parses the existing `plugins.Manifest` schema directly:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// cmd/lint-plugin-manifests verifies that every in-tree plugin.yaml with
// a character-reachable entry point declares "character" in
// actor_kinds_claimable. Spec ec22.1 §3.2 + acceptance criterion 8.
package main

import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/holomush/holomush/internal/plugin"
)

func main() {
    matches, err := filepath.Glob("plugins/*/plugin.yaml")
    if err != nil {
        fmt.Fprintf(os.Stderr, "glob error: %v\n", err)
        os.Exit(2)
    }
    failures := 0
    for _, manifestPath := range matches {
        data, err := os.ReadFile(manifestPath)
        if err != nil {
            fmt.Fprintf(os.Stderr, "%s: read error: %v\n", manifestPath, err)
            failures++
            continue
        }
        m, err := plugins.ParseManifest(data)
        if err != nil {
            fmt.Fprintf(os.Stderr, "%s: parse error: %v\n", manifestPath, err)
            failures++
            continue
        }
        if requiresCharacterClaim(m) && !containsCharacter(m.ActorKindsClaimable) {
            fmt.Fprintf(os.Stderr, "ERROR: plugin %q at %s has a character-reachable entry point but actor_kinds_claimable does not include \"character\".\n",
                m.Name, manifestPath)
            fmt.Fprintf(os.Stderr, "  Add 'actor_kinds_claimable: [plugin, character]' to the manifest.\n")
            failures++
        }
    }
    if failures > 0 {
        fmt.Fprintf(os.Stderr, "\n%d plugin manifest(s) failed the actor_kinds_claimable lint.\n", failures)
        os.Exit(1)
    }
    fmt.Println("All plugin manifests pass actor_kinds_claimable lint.")
}

// requiresCharacterClaim implements the spec §3.2 / criterion 8 heuristic:
// flag plugins where type != setting AND any of:
//   (a) emits: non-empty AND (commands: non-empty OR events: non-empty)
//   (b) events: non-empty (regardless of emits:)
func requiresCharacterClaim(m *plugins.Manifest) bool {
    if m.Type == plugins.TypeSetting {
        return false
    }
    hasEmits := len(m.Emits) > 0
    hasCommands := len(m.Commands) > 0
    hasEvents := len(m.Events) > 0
    if hasEvents {
        return true // clause (b)
    }
    if hasEmits && (hasCommands || hasEvents) {
        return true // clause (a)
    }
    return false
}

func containsCharacter(kinds []string) bool {
    for _, k := range kinds {
        if k == "character" {
            return true
        }
    }
    return false
}
```

- [ ] **Step 2: Add `lint:plugin-manifests` task to `Taskfile.yaml`**

Locate the `lint:` aggregator and the existing `lint:test-helpers` / `lint:access-migration` tasks. Add:

```yaml
lint:plugin-manifests:
  desc: Lint plugin.yaml manifests for actor_kinds_claimable coverage (spec ec22.1).
  cmds:
    - go run ./cmd/lint-plugin-manifests
```

Add it to the `lint:` aggregate `deps:` list. No new CI dependencies needed — `go run` works on stock GitHub Actions runners.

- [ ] **Step 3: Add `lint:docs-symmetry` task**

```yaml
lint:docs-symmetry:
  desc: Verify AGENTS.md and CLAUDE.md plugin-runtime-symmetry subsection is byte-identical.
  cmds:
    - |
      set -euo pipefail
      a=$(awk '/<!-- BEGIN: plugin-runtime-symmetry -->/,/<!-- END: plugin-runtime-symmetry -->/' AGENTS.md)
      c=$(awk '/<!-- BEGIN: plugin-runtime-symmetry -->/,/<!-- END: plugin-runtime-symmetry -->/' CLAUDE.md)
      if [ -z "$a" ]; then echo "ERROR: AGENTS.md missing plugin-runtime-symmetry anchored subsection"; exit 1; fi
      if [ -z "$c" ]; then echo "ERROR: CLAUDE.md missing plugin-runtime-symmetry anchored subsection"; exit 1; fi
      if [ "$a" != "$c" ]; then
        echo "ERROR: plugin-runtime-symmetry subsection diverged between AGENTS.md and CLAUDE.md"
        diff <(echo "$a") <(echo "$c") || true
        exit 1
      fi
      echo "AGENTS.md and CLAUDE.md plugin-runtime-symmetry subsection is byte-identical."
```

Add it to the `lint:` aggregate.

- [ ] **Step 4: Run the new lint tasks**

```bash
task lint:plugin-manifests
task lint:docs-symmetry
```

Expected: both PASS — the three migrated manifests carry `[plugin, character]`; AGENTS.md and CLAUDE.md have byte-identical subsections.

- [ ] **Step 5: Run full lint**

```bash
task lint
```

Expected: PASS, including the two new sub-tasks.

- [ ] **Step 6: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(ci): lint:plugin-manifests + lint:docs-symmetry (acceptance criteria 8 + 9)

Two new lint tasks added to Taskfile.yaml and wired into the lint
aggregate:

- lint:plugin-manifests: enforces spec §3.2 coverage. Flags any
  in-tree plugin where type != setting AND actor_kinds_claimable
  lacks character AND ANY of:
  - emits: non-empty AND (commands: OR events: non-empty)
  - events: non-empty (regardless of emits:)
  Catches all three current in-tree migrations and leaves setting
  plugins + emit-less plugins unflagged. Catches future regressions
  where a contributor adds a new character-reachable entry point
  without updating the manifest.

- lint:docs-symmetry: asserts the plugin-runtime-symmetry subsection
  in AGENTS.md and CLAUDE.md (delimited by HTML-comment anchors) is
  byte-identical. Anchored equivalence is robust against drift in
  the surrounding files.

Bead: holomush-ec22.1 (acceptance criteria 8 + 9)"
```

(Do NOT run `jj new`.)

---

## Task 13: Plugin-author docs — `site/docs/extending/actor-kinds-claimable.md`

**Why thirteenth:** Spec acceptance criterion 11 — operator-facing docs for plugin authors describing the new manifest field and migration guidance.

**Files:**
- Create: `site/docs/extending/actor-kinds-claimable.md`

- [ ] **Step 0: Start a fresh jj change**

```bash
jj new
jj st
jj log -r '@-' --no-pager   # MUST show Task 12's commit
```

- [ ] **Step 1: Create the doc**

Create `site/docs/extending/actor-kinds-claimable.md`:

```markdown
# `actor_kinds_claimable`

The `actor_kinds_claimable` field in your plugin's `plugin.yaml` declares which
actor kinds your plugin is allowed to vouch for on emitted events. It is the
operator-controlled trust boundary for plugin-emitted event identity.

## Schema

```yaml
# Default if absent: [plugin]
actor_kinds_claimable:
  - plugin
  - character
```

Allowed values:

| Value       | Meaning                                                                     |
| ----------- | --------------------------------------------------------------------------- |
| `plugin`    | Plugin can vouch for plugin-actor cascades and its own identity. **Required.** |
| `character` | Plugin can also vouch for character-actor cascades (verb handlers, etc.).      |

`system` is rejected at manifest load — the host's system identity is never
claimable by plugins.

## Validation rules

| Rule | Error code on violation |
| ---- | ----------------------- |
| MUST contain `plugin` | `MANIFEST_ACTOR_KINDS_MISSING_PLUGIN` |
| MUST NOT contain `system` | `MANIFEST_ACTOR_KIND_SYSTEM_FORBIDDEN` |
| MUST only contain known kinds | `MANIFEST_ACTOR_KIND_UNKNOWN` |
| MUST be a list | `MANIFEST_ACTOR_KINDS_MALFORMED` |
| Duplicates: silently deduplicated | n/a |

## When does my plugin need `character`?

Add `character` to `actor_kinds_claimable` if EITHER of:

1. Your plugin declares `commands:` and emits events from those handlers
   (verb-style plugins like `say`, `pose`, `emote`).
2. Your plugin declares `events:` (subscribes to events that may carry
   character actors) AND emits events from `on_event` handlers (cascade
   responders like an echo bot).

If your plugin only emits with its own identity (`ActorPlugin:<your-name>`)
and never preserves cascade actors, leave the field at its default `[plugin]`.

## Loud failures

Plugins that emit during a character-driven dispatch without declaring
`character` will receive `EMIT_ACTOR_KIND_NOT_CLAIMABLE` errors at the host's
emit boundary. The error is loud by design — silent fallback would mask
plugin misconfiguration.

## Cascade preservation

When your plugin handles an event triggered by a character (e.g., a `say`
command), the host stamps `ActorCharacter:<dispatching-character>` on the
emit context. Your plugin's emits will be attributed to that character on
the published event — provided your manifest declares `character`.

This preserves audit-trail integrity: events document who *caused* the
chain, not just who emitted at each link.

## Binary plugin authentication

Binary plugins additionally undergo per-dispatch token authentication on
the gRPC `EmitEvent` boundary. The host issues an opaque token at every
outgoing call and looks it up at emit time; the plugin's claimed actor
metadata (`x-holomush-actor-kind` / `-actor-id` headers) is no longer
trusted as identity. This closes the forgery surface where a binary
plugin could otherwise substitute arbitrary actor IDs.

The token mechanism is invisible to plugin authors using the standard
SDK — the SDK auto-ferries the token across the dispatch round-trip.

## Migration

If you maintain an out-of-tree plugin upgrading from a pre-`ec22.1` HoloMUSH
version: add `actor_kinds_claimable: [plugin, character]` to your manifest if
your plugin emits during character-driven dispatches. The first emit after
upgrade will loud-fail with `EMIT_ACTOR_KIND_NOT_CLAIMABLE` if the field is
missing.
```

- [ ] **Step 2: Verify the doc builds**

Run: `task docs:build` (if the project's docs site builds locally; otherwise skip).

Expected: PASS.

- [ ] **Step 3: Run lint**

Run: `task lint`

Expected: PASS (markdown lint).

- [ ] **Step 4: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "docs(extending): plugin-author guide for actor_kinds_claimable (acceptance criterion 11)

New site/docs/extending/actor-kinds-claimable.md describing:
- Schema + validation rules
- When a plugin needs to opt into 'character'
- Loud failures on misconfiguration
- Cascade preservation semantics
- Binary plugin auto-ferry of the host-issued token (transparent
  to plugin authors using the standard SDK)
- Migration guidance for out-of-tree plugins.

Bead: holomush-ec22.1 (acceptance criterion 11)"
```

(Do NOT run `jj new`.)

---

## Task 14: Final verification + bead close + push + PR

**Files:** none modified — verification + state update only.

- [ ] **Step 1: Run `task pr-prep`**

```bash
task pr-prep
```

Expected: green. Mirrors all CI: lint, format, schema, license, unit, integration, e2e + the two new lint tasks.

If any gate fails, fix inline and re-run. Common likely failures:
- Markdown lint: heading levels, line lengths.
- License headers: any new `.go`/`.sh` file missing `SPDX-License-Identifier` (run `task license:add`).
- `goleak` not in go.mod: re-run `go mod tidy`.
- `lint:plugin-manifests` complains about a plugin: that plugin needs migration.
- `lint:docs-symmetry` complains: AGENTS.md and CLAUDE.md subsections diverged.

- [ ] **Step 2: Run `/review-code`**

Per CLAUDE.md "Pre-Push Review Gates" before push:

```text
/review-code
```

Expected: READY verdict. Address blocking findings inline (re-run `task pr-prep` after fixes), re-review.

- [ ] **Step 3: Update bead and close**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd update holomush-ec22.1 --notes "Implementation complete via 13-commit stacked jj branch on top of main@origin (post PR #269).

Closes the host-trust gap on plugin EmitEvent (binary plugins forging actor metadata claims) via three coordinated layers:
1. Upstream actor-stamping at subscriber + dispatcher boundaries (G7) — activates the actor-metadata channel that was dead in production.
2. Universal manifest gate at event_emitter.go::Emit (G2) — operator-controlled actor_kinds_claimable opt-in, applied symmetrically to Lua and binary plugins.
3. Per-dispatch token authentication at the binary gRPC EmitEvent boundary (G1) — host-issued tokens replace plugin-claimed metadata; forgery becomes impossible.

Plugin runtime symmetry invariant (G6) codified in AGENTS.md + CLAUDE.md with anchored byte-equivalence CI check.

In-tree migrations: plugins/core-scenes/plugin.yaml, plugins/core-communication/plugin.yaml, plugins/echo-bot/plugin.yaml.

CI gates added: task lint:plugin-manifests, task lint:docs-symmetry. Both wired into task pr-prep.

5 design-review rounds; all blocking findings addressed.
task pr-prep green; /review-code READY."

BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd close holomush-ec22.1
```

- [ ] **Step 4: File the follow-up bead**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd create \
    --title "Plugin-as-character capability grant for non-dispatch contexts (Discord, AI puppeteers, external bridges)" \
    --description "Out-of-scope from holomush-ec22.1. Design a HostService.GrantActorCapability(characterID, externalProof) RPC that issues short-lived bearers feeding the same token-validation path on the host. Use case: Discord plugin receives an external message, looks up linked HoloMUSH character, emits a 'say' event as that character without an in-flight host dispatch. Expands the manifest schema with new opt-in (e.g., actor_capabilities: [discord_bridge])." \
    --type feature \
    --priority 3 \
    --parent holomush-ec22.1
```

- [ ] **Step 5: Set bookmark and push**

```bash
TIP=$(jj log -r 'main@origin..@ & ~empty()' --no-graph --no-pager -T 'change_id.short() ++ "\n"' | head -1)
echo "Pushing tip: $TIP"
jj bookmark create holomush-ec22.1 -r "$TIP"
jj git push --branch holomush-ec22.1
jj st  # verify clean
```

- [ ] **Step 6: Open the PR**

```bash
cd /Volumes/Code/github.com/holomush/holomush
GIT_SSL_NO_VERIFY=1 gh pr create \
  --base main \
  --head holomush-ec22.1 \
  --title "feat(plugin,security): authenticate plugin actor claims via manifest gate + per-dispatch token (ec22.1)" \
  --body "$(cat <<'EOF'
## Summary

Closes the host-trust gap on plugin `EmitEvent` (binary plugins can forge actor metadata claims) via three coordinated layers:

1. **Upstream actor-stamping (G7)** at `subscriber.go::deliverAsync` and `dispatcher.go::dispatchToPlugin` — activates the actor-metadata channel that was dead in production.
2. **Universal manifest gate (G2)** at `event_emitter.go::Emit` — operator-controlled `actor_kinds_claimable` opt-in, applied symmetrically to Lua and binary plugins (per project invariant).
3. **Per-dispatch token authentication (G1)** at the binary gRPC `EmitEvent` boundary — host-issued tokens replace plugin-claimed metadata. Forgery becomes impossible.

Cause-origin cascade semantic preserved (per `internal/core/event.go:188`); `ActorSystem` re-anchored at issuance (plugins never speak as host's system identity); `[system]` rejected at manifest load. Loud-error failure mode throughout — no silent fallback.

## Scope

- Internal: `internal/plugin/manifest.go`, `internal/plugin/event_emitter.go`, `internal/plugin/subscriber.go`, `internal/command/dispatcher.go`, `internal/plugin/goplugin/host.go`, `internal/plugin/goplugin/host_service.go`, plus new `internal/plugin/goplugin/emit_token_store.go`.
- Plugin migrations: `plugins/core-scenes/plugin.yaml`, `plugins/core-communication/plugin.yaml`, `plugins/echo-bot/plugin.yaml`.
- CI: new `task lint:plugin-manifests` + `task lint:docs-symmetry`.
- Docs: `AGENTS.md` + `CLAUDE.md` plugin runtime symmetry invariant (with anchored byte-equivalence); `site/docs/extending/actor-kinds-claimable.md` plugin-author guide.

## Spec & plan

- Spec: `docs/superpowers/specs/2026-04-25-plugin-actor-claim-authentication-design.md`
- Plan: `docs/superpowers/plans/2026-04-25-plugin-actor-claim-authentication.md`
- Bead: `holomush-ec22.1` (closed)
- Follow-up: `holomush-ec22.1.1` for plugin-as-character capability grant (Discord/AI bridges)

## Test plan

- [x] `task pr-prep` — green
- [x] §5.1 emitTokenStore unit (Issue/Lookup/Revoke/sweeper/Close + 10k-token uniqueness + concurrent-safety)
- [x] §5.2 manifest validation table-driven (8 cases including duplicates, system-forbidden, missing-plugin, unknown-kind)
- [x] §5.3 event_emitter manifest gate
- [x] §5.4 host_service.EmitEvent token-based (forgery override + missing/unknown/cross-plugin token + ActorSystem re-anchor)
- [x] §5.5 Host.DeliverEvent / DeliverCommand token issuance + defer-revoke
- [x] §5.6 integration forgery + Lua manifest gate (5 binary scenarios + 2 Lua scenarios)
- [x] §5.7 Lua manifest gate end-to-end
- [x] §5.8 upstream actor-stamping unit at subscriber + dispatcher boundaries
- [x] `/review-code` verdict: READY

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Expected: PR URL printed. Capture and report.

- [ ] **Step 7: Sync beads DB**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd dolt push
```

---

## Self-review (post-write checklist)

| Check | Result |
| ----- | ------ |
| G1 (forgery prevention) | Tasks 6 (store), 8 (issuance), 9 (lookup), 10 (e2e forgery test) |
| G2 (manifest opt-in symmetric) | Tasks 3 (schema), 5 (gate at common path), 10 (Lua e2e) |
| G3 (no system claim) | Task 3 (validation rejects `system`); Task 8 (re-anchor at issuance) |
| G4 (cascade preservation) | Task 5 (gate doesn't transform actor); Task 10 (e2e cascade test) |
| G5 (loud errors) | Tasks 5 + 9 (no silent fallback) |
| G6 (docs symmetry) | Tasks 11 (AGENTS.md + CLAUDE.md text) + 12 (lint:docs-symmetry CI check) |
| G7 (upstream stamping) | Tasks 1 + 2 |
| §5.1 token store unit | Task 6 |
| §5.2 manifest validation | Task 3 |
| §5.3 emitter manifest gate | Task 5 |
| §5.4 host_service.EmitEvent | Task 9 |
| §5.5 Host token issuance | Task 8 |
| §5.6 integration forgery | Task 10 |
| §5.7 Lua manifest gate e2e | Task 10 |
| §5.8 upstream stamping unit | Tasks 1 + 2 (subscriber + dispatcher tests) |
| §5.9 coverage targets | Task 14 verifies via `task pr-prep` |
| Acceptance criterion 1-4 (task lint/test/test:int/pr-prep) | Task 14 |
| Acceptance criterion 5 (Manifest field) | Task 3 |
| Acceptance criterion 6 (Host carries token store + Close) | Task 7 |
| Acceptance criterion 7 (EmitEvent doesn't read claim metadata) | Task 9 |
| Acceptance criterion 8 (3 plugin manifests + lint task) | Tasks 4 + 12 |
| Acceptance criterion 9 (AGENTS.md + CLAUDE.md anchored byte-equivalence) | Tasks 11 + 12 |
| Acceptance criterion 10 (ActorFromContext at Deliver entrance) | Tasks 1 + 2 unit tests |
| Acceptance criterion 11 (site/docs) | Task 13 |
| Placeholder scan | All steps have concrete code blocks; no "TBD"/"fill in" |
| Type/signature consistency | `Manifest.ActorKindsClaimable []string`, `(m *Manifest).DeclaresActorKindClaimable(kind core.ActorKind) bool`, `(s *emitTokenStore).Issue(pluginName string, actor core.Actor) (string, error)`, `(s *emitTokenStore).Lookup(pluginName, token string) (core.Actor, bool)` — used consistently across tasks |
| TDD ordering | Tasks 1, 2, 3, 5, 6, 7, 8, 9: red-green cycles. Tasks 4, 10, 11, 12, 13: not strictly TDD (config/migration/docs) |
| jj cadence | All 13 implementation tasks open with `jj new` Step 0; describe at end without trailing `jj new`. Task 14 uses `jj log -r 'main@origin..@ & ~empty()' ... \| head -1` for tip detection (per `feedback_writing-plans-tip-detection-jj-log-r-main`) |
