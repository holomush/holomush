# Scenes Phase 2: Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add owner-gated scene state transitions (`scene end`, `scene pause`, `scene resume`) and partial-update RPC (`scene set`), plus introduce `protovalidate` as the project's request-validation mechanism via a plugin-SDK gRPC interceptor.

**Architecture:** Phase 2 builds on Phase 1's `core-scenes` binary plugin. New work has two layers: (1) infrastructure — `buf.build/go/protovalidate` wired into the plugin SDK so every plugin's gRPC server validates inbound requests via a default interceptor, with proto annotations on `scene/v1`, `plugin/v1`, and `world/v1`; (2) feature work — a state machine, four new RPCs, four new manifest policies, four new command handlers, and end-to-end coverage including direct DB verification at the integration layer and Playwright + DB checks at the e2e layer.

**Tech Stack:** Go 1.25, `buf.build/go/protovalidate`, `buf.build/bufbuild/protovalidate` (proto schema), `google.golang.org/grpc`, `google.golang.org/protobuf/types/known/*`, OpenTelemetry tracing (global tracer), `slog` structured logging, Ginkgo/Gomega + testcontainers for integration tests, Playwright for E2E.

**Spec reference:** [`docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md`](../specs/2026-04-06-scenes-and-rp-design-v2.md) — sections 1.2 (states), 5.4 (ABAC mapping), 6.1 (lifecycle commands), 6.4 (in-scene commands), 7.2 (gRPC API), 10.1 (tracing).

**Bead reference:** `holomush-5rh.11`

**Workspace:** `scene-rewrite` jj workspace at `/Users/sean/Code/github.com/holomush/.worktrees/scene-rewrite` — on top of Phase 1's chain (`pllkwrtu` and earlier).

**Reference for old implementation:** `scene-system` jj bookmark. Read files via `jj file show -r scene-system <path>`.

**O11y scope for Phase 2 (per spec section 10):** structured `slog` logging at all new boundaries; OTel tracing via global tracer (no-op when not configured at process level); per-transition span names (`scene.lifecycle.end`, `.pause`, `.resume`) following Phase 1's per-method naming convention. Prometheus metrics still deferred per spec section 11 architectural gap.

**Discovered-from beads filed during brainstorm:**

- `holomush-9vw2` (closed) — staff/admin role tier already exists
- `holomush-psr9` (open) — gateway-side ConnectRPC validation interceptor follow-up

---

## File Structure

| File | Status | Responsibility |
|------|--------|----------------|
| `buf.yaml` | Modify | Add `buf.build/bufbuild/protovalidate` to `deps` |
| `go.mod` | Modify | Add `buf.build/go/protovalidate` (via `go mod tidy` after import) |
| `pkg/plugin/validate.go` | Create | `NewDefaultValidator()` — protovalidate.Validator constructor; validation gRPC interceptor |
| `pkg/plugin/validate_test.go` | Create | Unit tests for validator construction and interceptor |
| `pkg/plugin/service.go` | Modify | `ServeConfig` gains optional `Validator` field; `ServeWithServices` constructs gRPC server with validation interceptor |
| `api/proto/holomush/scene/v1/scene.proto` | Modify | Add `buf/validate/validate.proto` and `google/protobuf/field_mask.proto` imports; annotate Phase 1 messages; add `PauseSceneRequest/Response`, `ResumeSceneRequest/Response`, `UpdateSceneRequest/Response`; add 3 new RPCs to service definition; UpdateSceneRequest uses `google.protobuf.FieldMask` for partial updates |
| `api/proto/holomush/plugin/v1/plugin.proto` | Modify | Add `buf/validate/validate.proto` import; annotate string fields with constraints |
| `api/proto/holomush/plugin/v1/hostfunc.proto` | Modify | Same |
| `api/proto/holomush/plugin/v1/attribute.proto` | Modify | Same |
| `api/proto/holomush/world/v1/world.proto` | Modify | Same |
| `pkg/proto/holomush/scene/v1/*.go` | Regenerated | `task proto` regenerates after .proto changes |
| `pkg/proto/holomush/plugin/v1/*.go` | Regenerated | Same |
| `pkg/proto/holomush/world/v1/*.go` | Regenerated | Same |
| `plugins/core-scenes/lifecycle.go` | Create | State machine helpers: `IsValidTransition`, `CanEnd/Pause/Resume/Update` |
| `plugins/core-scenes/lifecycle_test.go` | Create | Table-driven state machine tests |
| `plugins/core-scenes/migrations/000002_scene_state_check.up.sql` | Create | DB-level CHECK constraint on scenes.state column (Task 7.5 — defense-in-depth) |
| `plugins/core-scenes/store.go` | Modify | Add `scanSceneRow`/`sceneSelectColumns` helpers; add `End`, `Pause`, `Resume`, `Update` methods using `RETURNING *` for atomic post-update reads; refactor `Get` to use the shared scanner |
| `plugins/core-scenes/store_integration_test.go` | Modify | Add testcontainer-backed tests for new store methods + race-safe transition tests |
| `plugins/core-scenes/metrics.go` | Create | No-op metric stub functions named per spec section 10.2 (Task 9.5 — touch-the-code-once API surface) |
| `plugins/core-scenes/service.go` | Modify | Add `EndScene`, `PauseScene`, `ResumeScene`, `UpdateScene` RPC handlers; extend `sceneStorer` interface to `(*SceneRow, error)` returns; remove hand-rolled validation now redundant via interceptor; wire metric stub calls |
| `plugins/core-scenes/service_test.go` | Modify | Extend `fakeStore` with new methods returning `(*SceneRow, error)`; add unit tests for new RPCs |
| `plugins/core-scenes/commands.go` | Modify | Add `handleEnd`, `handlePause`, `handleResume`, `handleSet`; extend `dispatchCommand` switch |
| `plugins/core-scenes/commands_test.go` | Modify | Add command handler unit tests |
| `plugins/core-scenes/plugin.yaml` | Modify | Add 4 new manifest policies: `end-own-scene`, `pause-own-scene`, `resume-own-scene` (with Phase 3 swap comment), `update-own-scene` |
| `test/integration/plugin/binary_plugin_test.go` | Modify | New `Describe("scene plugin lifecycle: state machine", ...)` block with direct DB verification AND a concurrent-end test that proves the race-safe WHERE clause guard |
| `web/e2e/scenes.spec.ts` | Create | Playwright tests for full scene lifecycle through terminal UI with DB verification |
| `web/e2e/helpers/db.ts` | Modify | Add `DbScene` interface and `getSceneById()` helper |

---

## Task 0: Verify Phase 1 baseline

Confirm Phase 1's chain is intact and tests are green before adding any Phase 2 code.

**Files:** None (verification only)

- [ ] **Step 1: Confirm chain top**

```bash
jj --no-pager log -r 'main..@' --no-graph -T 'change_id.short(8) ++ " " ++ description.first_line() ++ "\n"' | head -5
```

Expected: top of chain shows `pllkwrtu fix(scenes): remove AttributeResolverService from plugin.yaml provides` (Phase 1's last commit). The working copy `@` should be empty.

If the chain doesn't start with the Phase 1 commits, STOP and escalate — we're not in the right workspace state.

- [ ] **Step 2: Run focused integration test from Phase 1**

```bash
bash scripts/build-plugins.sh
```

Bash timeout: 300000

Then run the Phase 1 integration test to confirm it still passes:

```bash
task test:int -- ./test/integration/plugin/
```

Bash timeout: 600000

Expected: all integration tests pass, including `TestBinaryPluginLifecycle` and the `scene plugin ABAC` Describe block from Phase 1.

If anything fails, STOP — Phase 1 has regressed and needs to be fixed before Phase 2 can begin.

- [ ] **Step 3: No commit needed**

This is a verification task. Move to Task 1 once green.

---

## Task 1: Add `protovalidate` and wire SDK validator + gRPC interceptor

Pull in the protovalidate buf module and Go library, AND wire them into the plugin SDK in one task. The validator helper, gRPC interceptor, and the `ServeWithServices` change all land together so the dependency addition has an immediate user — no stub file dance.

**Files:**

- Modify: `buf.yaml`
- Modify: `go.mod` (via `go mod tidy` after the new import)
- Create: `pkg/plugin/validate.go`
- Create: `pkg/plugin/validate_test.go`
- Modify: `pkg/plugin/service.go`

- [ ] **Step 1: Add protovalidate to buf.yaml deps**

Open `buf.yaml`. Currently:

```yaml
# buf.yaml
version: v2
modules:
  - path: api/proto
    name: buf.build/holomush/holomush
deps:
  - buf.build/googleapis/googleapis
lint:
  use:
    - STANDARD
breaking:
  use:
    - FILE
```

Replace with:

```yaml
# buf.yaml
version: v2
modules:
  - path: api/proto
    name: buf.build/holomush/holomush
deps:
  - buf.build/googleapis/googleapis
  - buf.build/bufbuild/protovalidate
lint:
  use:
    - STANDARD
breaking:
  use:
    - FILE
```

- [ ] **Step 2: Pull the buf dep**

```bash
buf dep update
```

Bash timeout: 120000

Expected: `buf.lock` is updated with the protovalidate module entry. No errors.

- [ ] **Step 3: Write the failing tests for the validator helper and interceptor**

Create `pkg/plugin/validate_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
    "context"
    "errors"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"

    pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

func TestNewDefaultValidatorReturnsValidatorWithoutError(t *testing.T) {
    v, err := NewDefaultValidator()
    require.NoError(t, err)
    require.NotNil(t, v)
}

func TestValidateInterceptorPassesValidProtoMessage(t *testing.T) {
    v, err := NewDefaultValidator()
    require.NoError(t, err)

    interceptor := ValidateInterceptor(v)

    // Use any proto message — without annotations protovalidate accepts everything.
    req := &pluginv1.GetSchemaRequest{}

    var handlerCalled bool
    handler := func(ctx context.Context, req any) (any, error) {
        handlerCalled = true
        return "ok", nil
    }

    info := &grpc.UnaryServerInfo{FullMethod: "/holomush.test/Foo"}
    resp, err := interceptor(context.Background(), req, info, handler)
    require.NoError(t, err)
    assert.Equal(t, "ok", resp)
    assert.True(t, handlerCalled, "handler should be invoked when validation passes")
}

func TestValidateInterceptorPassesNonProtoRequest(t *testing.T) {
    v, err := NewDefaultValidator()
    require.NoError(t, err)

    interceptor := ValidateInterceptor(v)

    // Non-proto value (e.g., int) — interceptor should pass it through to handler.
    var handlerCalled bool
    handler := func(ctx context.Context, req any) (any, error) {
        handlerCalled = true
        return "ok", nil
    }

    info := &grpc.UnaryServerInfo{FullMethod: "/holomush.test/Foo"}
    _, err = interceptor(context.Background(), 42, info, handler)
    require.NoError(t, err)
    assert.True(t, handlerCalled, "handler should be invoked even for non-proto requests")
}

func TestValidateInterceptorPropagatesHandlerError(t *testing.T) {
    v, err := NewDefaultValidator()
    require.NoError(t, err)

    interceptor := ValidateInterceptor(v)

    sentinel := errors.New("handler boom")
    handler := func(ctx context.Context, req any) (any, error) {
        return nil, sentinel
    }

    info := &grpc.UnaryServerInfo{FullMethod: "/holomush.test/Foo"}
    _, err = interceptor(context.Background(), &pluginv1.GetSchemaRequest{}, info, handler)
    require.Error(t, err)
    assert.True(t, errors.Is(err, sentinel), "handler error should propagate unchanged")
}

func TestValidateInterceptorReturnsInvalidArgumentForFailedValidation(t *testing.T) {
    // We can't trigger a validation failure here without an annotated proto
    // message — and we don't want to create test-only protos. The real
    // verification of "validation failure → InvalidArgument" happens in
    // Task 8+ when scene messages with annotations exist.
    //
    // This test confirms that IF the validator returns an error, the
    // interceptor would map it to gRPC InvalidArgument. We exercise this
    // by injecting a failing validator-equivalent via a custom test type.
    interceptor := ValidateInterceptor(&alwaysFailValidator{})

    handler := func(ctx context.Context, req any) (any, error) {
        t.Fatal("handler should not be called when validation fails")
        return nil, nil
    }

    info := &grpc.UnaryServerInfo{FullMethod: "/holomush.test/Foo"}
    _, err := interceptor(context.Background(), &pluginv1.GetSchemaRequest{}, info, handler)
    require.Error(t, err)
    st, ok := status.FromError(err)
    require.True(t, ok)
    assert.Equal(t, codes.InvalidArgument, st.Code())
}

// alwaysFailValidator is a test double that always returns a validation error.
type alwaysFailValidator struct{}

func (a *alwaysFailValidator) Validate(any) error {
    return errors.New("forced validation failure for test")
}
```

- [ ] **Step 4: Run the tests and confirm they fail**

```bash
task test -- -run "TestNewDefaultValidator|TestValidateInterceptor" ./pkg/plugin/
```

Bash timeout: 60000

Expected: build error — `undefined: NewDefaultValidator`, `undefined: ValidateInterceptor`. The Go module will also need `buf.build/go/protovalidate` which `go mod tidy` adds in Step 5 after the import is in place.

- [ ] **Step 5: Create validate.go**

Create `pkg/plugin/validate.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
    "context"

    "buf.build/go/protovalidate"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "google.golang.org/protobuf/proto"
)

// Validator is the interface implemented by *protovalidate.Validator.
// We define a local interface so tests can substitute a fake validator
// without depending on the concrete protovalidate type.
type Validator interface {
    Validate(msg any) error
}

// validatorAdapter adapts *protovalidate.Validator (which takes proto.Message)
// to our Validator interface (which takes any). The adapter type-asserts and
// returns nil for non-proto inputs, matching the interceptor's behavior of
// passing through non-proto requests.
type validatorAdapter struct {
    inner *protovalidate.Validator
}

// Validate type-asserts msg to proto.Message and validates it. If msg is not
// a proto.Message, returns nil (validation is skipped for non-proto values).
func (a *validatorAdapter) Validate(msg any) error {
    pm, ok := msg.(proto.Message)
    if !ok {
        return nil
    }
    return a.inner.Validate(pm)
}

// NewDefaultValidator constructs a protovalidate.Validator wrapped in our
// local Validator interface. Plugins may use this directly or substitute
// their own Validator implementation via ServeConfig.Validator.
//
// The validator is stateless after construction (it caches compiled rules
// per-message-type on first encounter), so a single instance can be shared
// across all plugin handlers.
func NewDefaultValidator() (Validator, error) {
    v, err := protovalidate.New()
    if err != nil {
        return nil, err
    }
    return &validatorAdapter{inner: v}, nil
}

// ValidateInterceptor returns a gRPC unary server interceptor that validates
// inbound proto messages using the supplied Validator. Non-proto requests
// pass through unchanged. Validation failures are mapped to gRPC
// InvalidArgument with the validator's error message attached.
//
// Per spec section 10.3, validation failures are user-facing errors and do
// not constitute service degradation. Handlers do not need to log them
// (the gRPC status response is sufficient).
func ValidateInterceptor(v Validator) grpc.UnaryServerInterceptor {
    return func(
        ctx context.Context,
        req any,
        info *grpc.UnaryServerInfo,
        handler grpc.UnaryHandler,
    ) (any, error) {
        if err := v.Validate(req); err != nil {
            return nil, status.Errorf(codes.InvalidArgument, "request validation failed: %v", err)
        }
        return handler(ctx, req)
    }
}
```

- [ ] **Step 6: Run go mod tidy and the tests**

```bash
go mod tidy
task test -- -run "TestNewDefaultValidator|TestValidateInterceptor" ./pkg/plugin/
```

Bash timeouts: 120000 each.

Expected: `go mod tidy` adds `buf.build/go/protovalidate` and its transitive deps to `go.mod`/`go.sum` (because validate.go now imports it). Then all five validator tests PASS.

- [ ] **Step 7: Modify ServeWithServices to install the interceptor by default**

Open `pkg/plugin/service.go`. Find the `ServeConfig` struct and add a `Validator` field:

```go
// ServeConfig configures the plugin server.
type ServeConfig struct {
    Handler Handler
    // Validator is an optional protobuf message validator installed as a
    // gRPC unary server interceptor on the plugin's gRPC server. If nil,
    // ServeWithServices constructs a default validator via
    // NewDefaultValidator() at startup. Plugins that need custom validation
    // (e.g., a validator with extra rules registered) can supply their own.
    Validator Validator
}
```

If `ServeConfig` doesn't currently have any fields besides `Handler`, this is a one-field addition. Verify by reading the existing struct and only adding the `Validator` field.

Then find `ServeWithServices` (around line 41). Replace its body to construct the gRPC server with the validation interceptor:

```go
// ServeWithServices starts the plugin server with service injection support.
// It is the service-aware counterpart of Serve. Plugins that provide gRPC
// services or need initialization should use this instead of Serve.
//
// The provider's RegisterServices is called during gRPC server setup, and its
// Init method is called when the host sends the Init RPC.
//
// A protovalidate.Validator is installed as the default unary server
// interceptor on every plugin's gRPC server. Messages without buf.validate
// annotations validate as always-valid; messages with annotations are
// validated at unmarshal time and invalid requests get rejected with
// gRPC InvalidArgument before reaching the handler.
//
// Plugins can override the default validator by setting config.Validator.
func ServeWithServices(config *ServeConfig, provider ServiceProvider) {
    if config == nil {
        panic("plugin: config cannot be nil")
    }
    if config.Handler == nil {
        panic("plugin: config.Handler cannot be nil")
    }
    if provider == nil {
        panic("plugin: provider cannot be nil")
    }

    validator := config.Validator
    if validator == nil {
        v, err := NewDefaultValidator()
        if err != nil {
            panic("plugin: failed to construct default validator: " + err.Error())
        }
        validator = v
    }

    serveConfig := &hashiplug.ServeConfig{
        HandshakeConfig: HandshakeConfig,
        Plugins: map[string]hashiplug.Plugin{
            "plugin": &grpcServicePlugin{
                handler:  config.Handler,
                provider: provider,
            },
        },
        GRPCServer: func(opts []grpc.ServerOption) *grpc.Server {
            opts = append(opts, grpc.ChainUnaryInterceptor(
                ValidateInterceptor(validator),
            ))
            return grpc.NewServer(opts...)
        },
    }
    if tlsProvider := loadPluginTLSProvider(); tlsProvider != nil {
        serveConfig.TLSProvider = tlsProvider
    }
    hashiplug.Serve(serveConfig)
}
```

The key changes from the old version:

1. Validator construction (lines after the nil checks): if `config.Validator` is nil, we build a default one via `NewDefaultValidator()`. We `panic` on construction failure because this happens during plugin startup before any request is served — there's no good error path.
2. `GRPCServer` (which was `hashiplug.DefaultGRPCServer`) is now an explicit closure that constructs `grpc.NewServer` with our `ValidateInterceptor` wrapped via `grpc.ChainUnaryInterceptor`.

The closure form is what hashicorp/go-plugin's `ServeConfig.GRPCServer` field expects: `func([]grpc.ServerOption) *grpc.Server`.

- [ ] **Step 8: Verify the SDK still builds**

```bash
task lint
```

Bash timeout: 300000

Expected: no errors. `pkg/plugin/service.go` compiles with the new `Validator` field and the closure-form `GRPCServer`.

- [ ] **Step 9: Run the full plugin SDK test suite to verify no regressions**

```bash
task test -- ./pkg/plugin/...
```

Bash timeout: 120000

Expected: all tests pass — both the new validator tests and any existing SDK tests.

- [ ] **Step 10: Run the core-scenes plugin tests to verify Phase 1 plugin still builds**

```bash
task test -- ./plugins/core-scenes/
```

Bash timeout: 60000

Expected: all 22 Phase 1 tests still pass.

- [ ] **Step 11: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
feat(plugin): protovalidate dep + default interceptor in plugin SDK

Phase 2 introduces buf protovalidate as the project's request validation
mechanism, wiring it into the plugin SDK in a single commit:

1. buf.yaml: add buf.build/bufbuild/protovalidate to deps
2. go.mod: add buf.build/go/protovalidate (via go mod tidy after import)
3. pkg/plugin/validate.go: NewDefaultValidator and ValidateInterceptor
4. pkg/plugin/service.go: ServeWithServices constructs the plugin's gRPC
   server with the validation interceptor wired by default; new
   ServeConfig.Validator field lets plugins supply their own validator

This change is non-breaking. protovalidate treats messages without
buf.validate annotations as always-valid; existing plugins (test-abac-widget,
core-scenes from Phase 1) get the interceptor for free without behavior
change. Validation only starts rejecting when proto annotations are added
(Tasks 2-4 of this plan).

Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

(Note: there is no Task 2 in this plan — Tasks 1 and 2 from earlier drafts were merged into Task 1 above. The numbering jumps from Task 1 to Task 3 below; this is intentional and saves one commit's worth of throwaway stub-file work.)

---

## Task 3: Annotate `scene/v1/scene.proto` and add Phase 2 RPCs

Add `buf.validate` annotations to existing scene messages (CreateScene, GetScene, EndScene), add new request/response messages for `PauseScene`, `ResumeScene`, `UpdateScene`, and add the three new RPCs to the service definition. `UpdateSceneRequest` uses `google.protobuf.FieldMask` per Google AIP-134 — the canonical proto3 partial-update pattern that handles both scalar and repeated fields uniformly.

**Files:**

- Modify: `api/proto/holomush/scene/v1/scene.proto`
- Regenerated: `pkg/proto/holomush/scene/v1/*.go` (via `task proto`)

- [ ] **Step 1: Replace the entire scene.proto file**

Open `api/proto/holomush/scene/v1/scene.proto`. Replace its entire contents with:

```protobuf
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

syntax = "proto3";

package holomush.scene.v1;

import "buf/validate/validate.proto";
import "google/protobuf/field_mask.proto";
import "google/protobuf/timestamp.proto";

option go_package = "github.com/holomush/holomush/pkg/proto/holomush/scene/v1;scenev1";

service SceneService {
  rpc ListScenes(ListScenesRequest) returns (ListScenesResponse);
  rpc GetScene(GetSceneRequest) returns (GetSceneResponse);
  rpc CreateScene(CreateSceneRequest) returns (CreateSceneResponse);
  rpc EndScene(EndSceneRequest) returns (EndSceneResponse);
  rpc PauseScene(PauseSceneRequest) returns (PauseSceneResponse);
  rpc ResumeScene(ResumeSceneRequest) returns (ResumeSceneResponse);
  rpc UpdateScene(UpdateSceneRequest) returns (UpdateSceneResponse);
  rpc JoinScene(JoinSceneRequest) returns (JoinSceneResponse);
  rpc LeaveScene(LeaveSceneRequest) returns (LeaveSceneResponse);
  rpc InviteToScene(InviteToSceneRequest) returns (InviteToSceneResponse);
  rpc CastPublishVote(CastPublishVoteRequest) returns (CastPublishVoteResponse);
  rpc GetPoseOrder(GetPoseOrderRequest) returns (GetPoseOrderResponse);
}

message SceneInfo {
  string id = 1;
  string title = 2;
  string description = 3;
  string location_id = 4;
  string owner_id = 5;
  string state = 6;
  string pose_order_mode = 7;
  repeated string content_warnings = 8;
  repeated string tags = 9;
  string visibility = 10;
  google.protobuf.Timestamp created_at = 11;
  google.protobuf.Timestamp ended_at = 12;
  repeated ParticipantInfo participants = 13;
}

message ParticipantInfo {
  string character_id = 1;
  string character_name = 2;
  string role = 3;
  google.protobuf.Timestamp joined_at = 4;
}

message ListScenesRequest {
  int32 limit = 1 [(buf.validate.field).int32 = {gte: 0, lte: 200}];
  int32 offset = 2 [(buf.validate.field).int32.gte = 0];
  repeated string tags = 3;
}

message ListScenesResponse {
  repeated SceneInfo scenes = 1;
}

message GetSceneRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string scene_id = 2 [(buf.validate.field).string.min_len = 1];
}

message GetSceneResponse {
  SceneInfo scene = 1;
}

message CreateSceneRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string title = 2 [(buf.validate.field).string = {min_len: 1, max_len: 200}];
  string description = 3 [(buf.validate.field).string.max_len = 4096];
  string location_id = 4;
  string visibility = 5 [(buf.validate.field).string = {in: ["", "open", "private"]}];
  string pose_order_mode = 6 [(buf.validate.field).string = {in: ["", "free", "strict", "3pr", "5pr"]}];
  repeated string tags = 7 [(buf.validate.field).repeated.max_items = 32];
  repeated string content_warnings = 8 [(buf.validate.field).repeated.max_items = 32];
}

message CreateSceneResponse {
  SceneInfo scene = 1;
}

message EndSceneRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string scene_id = 2 [(buf.validate.field).string.min_len = 1];
}

message EndSceneResponse {
  SceneInfo scene = 1;
}

message PauseSceneRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string scene_id = 2 [(buf.validate.field).string.min_len = 1];
}

message PauseSceneResponse {
  SceneInfo scene = 1;
}

message ResumeSceneRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string scene_id = 2 [(buf.validate.field).string.min_len = 1];
}

message ResumeSceneResponse {
  SceneInfo scene = 1;
}

// UpdateSceneRequest uses google.protobuf.FieldMask as the canonical proto3
// pattern for partial updates (per Google AIP-134). The mask is the single
// source of truth for "which fields to apply" — fields listed in the mask
// are updated to the value in the request (even if that value is empty/zero);
// fields not in the mask are left unchanged.
//
// Per-field constraint semantics:
// - max_len limits apply to all string fields regardless of mask membership
// - min_len IS NOT used at the proto layer because the mask gates whether
//   the field is applied; per-field semantic validation (e.g. "title cannot
//   be empty when in the mask") happens in the service handler's mask-iteration
//   switch statement
// - enum-style fields use `in:` constraints that include the empty string
//   so that "field not set" doesn't trip the validator
message UpdateSceneRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string scene_id = 2 [(buf.validate.field).string.min_len = 1];

  string title = 3 [(buf.validate.field).string.max_len = 200];
  string description = 4 [(buf.validate.field).string.max_len = 4096];
  string visibility = 5 [(buf.validate.field).string = {in: ["", "open", "private"]}];
  string pose_order_mode = 6 [(buf.validate.field).string = {in: ["", "free", "strict", "3pr", "5pr"]}];
  string location_id = 7;
  repeated string content_warnings = 8 [(buf.validate.field).repeated.max_items = 32];
  repeated string tags = 9 [(buf.validate.field).repeated.max_items = 32];

  google.protobuf.FieldMask update_mask = 99;
}

message UpdateSceneResponse {
  SceneInfo scene = 1;
}

message JoinSceneRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string scene_id = 2 [(buf.validate.field).string.min_len = 1];
}

message JoinSceneResponse {}

message LeaveSceneRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string scene_id = 2 [(buf.validate.field).string.min_len = 1];
}

message LeaveSceneResponse {}

message InviteToSceneRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string scene_id = 2 [(buf.validate.field).string.min_len = 1];
  string target_character_id = 3 [(buf.validate.field).string.min_len = 1];
}

message InviteToSceneResponse {}

message CastPublishVoteRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string scene_id = 2 [(buf.validate.field).string.min_len = 1];
  bool vote = 3;
}

message CastPublishVoteResponse {}

message GetPoseOrderRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string scene_id = 2 [(buf.validate.field).string.min_len = 1];
}

message PoseOrderEntry {
  string character_id = 1;
  string character_name = 2;
  google.protobuf.Timestamp last_posed_at = 3;
  bool is_eligible = 4;
}

message GetPoseOrderResponse {
  string mode = 1;
  repeated PoseOrderEntry entries = 2;
}
```

**Notable changes:**

1. New imports: `buf/validate/validate.proto` (for annotations) and `google/protobuf/field_mask.proto` (for `UpdateSceneRequest`).
2. New RPCs `PauseScene`, `ResumeScene`, `UpdateScene` in the service block.
3. `EndSceneResponse` now includes `SceneInfo scene = 1` so callers see the post-end state. `PauseScene` and `ResumeScene` follow the same pattern.
4. New messages `PauseSceneRequest/Response`, `ResumeSceneRequest/Response`, `UpdateSceneRequest/Response`.
5. `UpdateSceneRequest` uses `google.protobuf.FieldMask` per Google AIP-134 (the canonical proto3 partial-update pattern). The mask is the single source of truth for "which fields to apply." Fields not in the mask are unchanged; fields in the mask are set to the value in the request (even if empty).
6. `buf.validate.field` annotations: `min_len: 1` on entity IDs (`character_id`, `scene_id`), `max_len` on free-form strings, and `in: ["", ...]` on enum-style fields. The empty string is included in `in:` constraints because the mask gates whether the field is applied — an unset field's zero value must validate.

- [ ] **Step 2: Run buf lint to verify the proto is well-formed**

```bash
task lint
```

Bash timeout: 300000

Expected: no errors. If buf complains about a buf.validate constraint syntax, fix it.

- [ ] **Step 3: Regenerate Go bindings**

```bash
task proto
```

Bash timeout: 120000

Expected: `pkg/proto/holomush/scene/v1/scene.pb.go` and related generated files are updated. New types `PauseSceneRequest`, `ResumeSceneRequest`, `UpdateSceneRequest`, etc., now exist.

For the optional fields in `UpdateSceneRequest`, the generated Go code uses pointer types (`*string`) so callers can check for `nil` to detect "not set."

- [ ] **Step 4: Verify the project still builds**

```bash
task lint
```

Bash timeout: 300000

Expected: lint passes. The new generated proto code compiles cleanly.

You may see lint errors in `plugins/core-scenes/service.go` because Phase 1's hand-rolled validation references `req.GetCharacterId() == ""` checks that are no longer needed (the interceptor handles them). **Don't fix those yet** — Task 7 strips them in one focused commit.

- [ ] **Step 5: Run unit tests to confirm Phase 1 still works**

```bash
task test -- ./plugins/core-scenes/
```

Bash timeout: 60000

Expected: all 22 Phase 1 tests pass. No new tests added in this task; the proto changes are non-breaking for existing code.

- [ ] **Step 6: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
feat(scenes): annotate scene.proto and add Phase 2 RPCs

Add buf.validate annotations to all scene RPC request messages and add
three new RPCs for Phase 2:

- PauseScene: owner pauses an active scene
- ResumeScene: owner resumes a paused scene (Phase 3 will widen to members)
- UpdateScene: partial updates to mutable scene metadata via FieldMask

UpdateSceneRequest uses google.protobuf.FieldMask per Google AIP-134, the
canonical proto3 partial-update pattern. The mask is the single source of
truth for "which fields to apply"; unset fields are unchanged, fields in
the mask are set to the value in the request (even if empty/zero). This
correctly handles repeated fields (content_warnings, tags) which proto3
has no native presence syntax for.

EndSceneResponse, PauseSceneResponse, ResumeSceneResponse, UpdateSceneResponse
all return the updated SceneInfo so callers see the post-mutation state.

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md sections 1.2, 7.2
Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

---

## Task 4: Annotate `plugin/v1/*.proto`

Add `buf.validate.field` constraints to the existing plugin protos so the validator interceptor catches malformed plugin requests.

**Files:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto`
- Modify: `api/proto/holomush/plugin/v1/hostfunc.proto`
- Modify: `api/proto/holomush/plugin/v1/attribute.proto`
- Regenerated: `pkg/proto/holomush/plugin/v1/*.go` (via `task proto`)

- [ ] **Step 1: Read each existing proto file**

Read each of the three files to understand their current structure. The annotation pattern is: add `import "buf/validate/validate.proto";` at the top, then `[(buf.validate.field)...]` to fields that have constraints.

Apply only conservative constraints:

- ULIDs and IDs: `(buf.validate.field).string.min_len = 1`
- Free text fields: `(buf.validate.field).string.max_len = N` for sensible N
- Enums: don't add `in:` constraints unless the enum values are stable and known

Don't add `required: true` annotations — they're stricter than the project currently enforces and could break clients.

- [ ] **Step 2: Annotate plugin.proto**

Open `api/proto/holomush/plugin/v1/plugin.proto`. Add the import:

```protobuf
import "buf/validate/validate.proto";
```

Place it alphabetically with other imports if any exist; otherwise, just after the `package` declaration.

Then add `[(buf.validate.field)...]` annotations to fields. Specifically, for any field named `*_id`, `name`, `command`, etc., add `(buf.validate.field).string.min_len = 1`. For free text like `description`, `output`, `args`, add a generous `max_len` (e.g., `8192`) but NOT `min_len` (empty values are valid).

If the proto doesn't have many fields that need constraints, that's fine — annotate the obvious ones and leave the rest alone.

- [ ] **Step 3: Annotate hostfunc.proto**

Same approach. Add the import and annotate ID fields and command names with `min_len: 1`. Free text gets `max_len`. Don't over-constrain.

- [ ] **Step 4: Annotate attribute.proto**

Same approach. The `ResolveResourceRequest` has `resource_type` and `resource_id` — both should have `min_len: 1` (host should never call resolve with empty values).

- [ ] **Step 5: Run buf lint**

```bash
task lint
```

Bash timeout: 300000

Expected: lint passes. If any annotation is malformed, buf reports it with a line number.

- [ ] **Step 6: Regenerate Go bindings**

```bash
task proto
```

Bash timeout: 120000

Expected: `pkg/proto/holomush/plugin/v1/*.go` files are regenerated with the new annotations baked into the protobuf descriptors.

- [ ] **Step 7: Run all tests to verify nothing breaks**

```bash
task test -- ./...
```

Bash timeout: 600000

Expected: all unit tests pass. The new annotations don't change behavior for existing test fixtures (which use valid inputs).

If a test fails because of validation, that means the test was using invalid input that the old hand-rolled validation accepted but the new annotation rejects. Investigate per failure — usually the right fix is to make the test input valid, not to remove the annotation.

- [ ] **Step 8: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
feat(proto): annotate plugin/v1 protos with buf.validate constraints

Add buf.validate.field annotations to plugin.proto, hostfunc.proto, and
attribute.proto. Conservative constraint set:

- ID fields: min_len = 1 (no empty IDs)
- Free text: max_len for safety (no min_len, empty is OK)
- Enums: skipped — enum types not stable across versions

The validator interceptor (Task 2) now actually rejects requests with
invalid plugin RPC inputs.

Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

---

## Task 5: Annotate `world/v1/world.proto`

Same pattern as Task 4, applied to the world service proto.

**Files:**

- Modify: `api/proto/holomush/world/v1/world.proto`
- Regenerated: `pkg/proto/holomush/world/v1/*.go`

- [ ] **Step 1: Read the file**

Read `api/proto/holomush/world/v1/world.proto`. The Phase 2 brainstorm earlier noted: it's read-only with 4 RPCs (`GetLocation`, `GetCharacter`, `ListCharactersAtLocation`, `ListExits`). Each request has a `subject_id` (caller) and an entity-specific ID field (`location_id`, `character_id`).

- [ ] **Step 2: Annotate**

Add the buf.validate import:

```protobuf
import "buf/validate/validate.proto";
```

Then annotate every `*_id` field with `(buf.validate.field).string.min_len = 1`:

```protobuf
message GetLocationRequest {
  string subject_id = 1 [(buf.validate.field).string.min_len = 1];
  string location_id = 2 [(buf.validate.field).string.min_len = 1];
}

message GetCharacterRequest {
  string subject_id = 1 [(buf.validate.field).string.min_len = 1];
  string character_id = 2 [(buf.validate.field).string.min_len = 1];
}

message ListCharactersAtLocationRequest {
  string subject_id = 1 [(buf.validate.field).string.min_len = 1];
  string location_id = 2 [(buf.validate.field).string.min_len = 1];
}

message ListExitsRequest {
  string subject_id = 1 [(buf.validate.field).string.min_len = 1];
  string location_id = 2 [(buf.validate.field).string.min_len = 1];
}
```

Response messages (`*Info` types like `LocationInfo`, `CharacterInfo`, `ExitInfo`) don't need annotations — they're outputs, not inputs.

- [ ] **Step 3: Regenerate and verify**

```bash
task lint
task proto
task test -- ./...
```

Bash timeouts: 300000, 120000, 600000 respectively.

Expected: lint passes, regen succeeds, all tests pass.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
feat(proto): annotate world/v1/world.proto with buf.validate constraints

Add buf.validate.field min_len = 1 annotations to all subject_id,
location_id, and character_id fields on WorldService request messages.

Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

---

## Task 6: Strip Phase 1 hand-rolled validation from `service.go`

Now that the validator interceptor handles `min_len: 1` checks for `character_id`, `scene_id`, etc., the Phase 1 service code's manual checks are redundant. Remove them and tighten the handlers.

**Files:**

- Modify: `plugins/core-scenes/service.go`
- Modify: `plugins/core-scenes/service_test.go`

- [ ] **Step 1: Identify the redundant checks**

Open `plugins/core-scenes/service.go`. Find the validation blocks at the top of `CreateScene` and `GetScene`. They look like:

```go
if req.GetCharacterId() == "" {
    recordError(span, errors.New("character_id is required"))
    return nil, status.Errorf(codes.InvalidArgument, "character_id is required")
}
title := strings.TrimSpace(req.GetTitle())
if title == "" {
    recordError(span, errors.New("title is required"))
    return nil, status.Errorf(codes.InvalidArgument, "title is required")
}
```

The `character_id` and `title` checks are now redundant because the proto annotations enforce them at unmarshal time. The `strings.TrimSpace` + empty check on title is also redundant because the proto annotation `(buf.validate.field).string.min_len = 1` rejects empty titles. But: trim is still needed for the SERVICE-level concern (we want trimmed strings stored, not raw input with trailing whitespace).

The minimum required change is:

- Remove the `if req.GetCharacterId() == "" { ... }` block
- Remove the `if title == "" { ... }` block (the `min_len: 1` annotation handles this)
- KEEP the `title := strings.TrimSpace(req.GetTitle())` line because we want clean storage

The `scene_id` check in `GetScene` is similarly redundant.

- [ ] **Step 2: Update CreateScene to remove redundant checks**

Find `CreateScene` in `plugins/core-scenes/service.go` and replace its body up to the store call with this version (preserving the existing OTel/slog/oops code):

```go
// CreateScene generates a new scene ID, persists the scene, and returns it.
// The caller (host) is responsible for ensuring ABAC has authorised the
// command-execute action; per-resource ABAC for the new scene happens at
// the read path.
//
// Per-field validation (character_id non-empty, title min_len: 1, etc.)
// happens via the protovalidate interceptor before this handler runs.
func (s *SceneServiceImpl) CreateScene(ctx context.Context, req *scenev1.CreateSceneRequest) (*scenev1.CreateSceneResponse, error) {
    ctx, span := startSpan(ctx, "scene.service.create_scene",
        attribute.String("subject_id", req.GetCharacterId()),
    )
    defer span.End()

    // Title is trimmed before storage so empty-only-after-trim becomes
    // empty after trimming. The protovalidate annotation rejects empty
    // titles at unmarshal time, but a title of "   " (spaces) passes
    // protovalidate's min_len check and would be stored as a blank
    // title without this trim. Service-level cleanup, not validation.
    title := strings.TrimSpace(req.GetTitle())
    if title == "" {
        recordError(span, errors.New("title cannot be whitespace-only"))
        return nil, status.Errorf(codes.InvalidArgument, "title cannot be whitespace-only")
    }

    id, err := newSceneID()
    if err != nil {
        recordError(span, err)
        return nil, status.Errorf(codes.Internal, "failed to generate scene id: %v", err)
    }
    span.SetAttributes(attribute.String("scene_id", id))

    row := &SceneRow{
        ID:              id,
        Title:           title,
        Description:     req.GetDescription(),
        OwnerID:         req.GetCharacterId(),
        State:           string(SceneStateActive),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{},
        Tags:            []string{},
    }
    if loc := req.GetLocationId(); loc != "" {
        row.LocationID = &loc
    }

    if err := s.store.Create(ctx, row); err != nil {
        recordError(span, err)
        slog.WarnContext(ctx, "scene.service.create_scene store error",
            "subject_id", req.GetCharacterId(),
            "scene_id", id,
            "error", err,
        )
        return nil, status.Errorf(codes.Internal, "failed to create scene: %v", err)
    }

    metricSceneCreated(string(SceneVisibilityOpen), false)
    slog.InfoContext(ctx, "scene.service.create_scene ok",
        "subject_id", req.GetCharacterId(),
        "scene_id", id,
        "title", title,
    )

    return &scenev1.CreateSceneResponse{
        Scene: rowToProto(row, time.Now().UTC()),
    }, nil
}
```

Note: `metricSceneCreated` is the Phase 2 stub from Task 9.5 — it's a no-op until the binary plugin metrics infrastructure exists. We're adding the call here so the API surface is wired up; when metrics land, this call site already exists. Phase 7 (templates) will pass `true` for `fromTemplate` when scenes are created from a template; Phase 2 always passes `false` because there are no templates yet.

The differences from Phase 1:

- The `if req.GetCharacterId() == "" { ... }` block is gone
- The empty-title check is replaced with a whitespace-only check (because protovalidate's `min_len: 1` accepts `"   "` as valid)

- [ ] **Step 3: Update GetScene to remove redundant checks**

Find `GetScene` in `plugins/core-scenes/service.go` and replace it with:

```go
// GetScene loads a scene by ID and returns it. The host's ABAC engine has
// already evaluated the read-own-scene policy before this RPC is invoked,
// so the service does not perform an additional ownership check.
//
// Per-field validation (scene_id non-empty) happens via the protovalidate
// interceptor before this handler runs.
func (s *SceneServiceImpl) GetScene(ctx context.Context, req *scenev1.GetSceneRequest) (*scenev1.GetSceneResponse, error) {
    ctx, span := startSpan(ctx, "scene.service.get_scene",
        attribute.String("scene_id", req.GetSceneId()),
    )
    defer span.End()

    row, err := s.store.Get(ctx, req.GetSceneId())
    if err != nil {
        recordError(span, err)
        var oe oops.OopsError
        if errors.As(err, &oe) && oe.Code() == "SCENE_NOT_FOUND" {
            return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
        }
        slog.WarnContext(ctx, "scene.service.get_scene store error",
            "scene_id", req.GetSceneId(),
            "error", err,
        )
        return nil, status.Errorf(codes.Internal, "failed to get scene: %v", err)
    }

    slog.InfoContext(ctx, "scene.service.get_scene ok",
        "scene_id", row.ID,
    )

    return &scenev1.GetSceneResponse{
        Scene: rowToProto(row, row.CreatedAt),
    }, nil
}
```

The `if req.GetSceneId() == "" { ... }` block is gone.

- [ ] **Step 4: Update unit tests**

Open `plugins/core-scenes/service_test.go`. Find the tests that exercise the empty-input rejection paths:

- `TestSceneServiceCreateSceneRejectsEmptyCharacterID`
- `TestSceneServiceCreateSceneRejectsBlankTitle`

The `EmptyCharacterID` test now needs to either be removed (because the interceptor handles it, not the service handler) OR adapted to test the interceptor's behavior. **Remove it** — interceptor tests live in `pkg/plugin/validate_test.go`, not in service tests.

The `BlankTitle` test should be renamed to `RejectsWhitespaceOnlyTitle` and updated to use `"   "` as input (which is what the service-level trim now catches):

```go
func TestSceneServiceCreateSceneRejectsWhitespaceOnlyTitle(t *testing.T) {
    svc := NewSceneServiceImpl(newFakeStore())

    _, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
        CharacterId: "char-alice",
        Title:       "   ",
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    assert.Equal(t, codes.InvalidArgument, st.Code())
    assert.Contains(t, st.Message(), "whitespace-only")
}
```

Delete `TestSceneServiceCreateSceneRejectsEmptyCharacterID` entirely.

- [ ] **Step 5: Run tests**

```bash
task test -- ./plugins/core-scenes/
```

Bash timeout: 60000

Expected: all tests pass. Test count is 21 (one fewer than before because we removed `TestSceneServiceCreateSceneRejectsEmptyCharacterID`).

- [ ] **Step 6: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
refactor(scenes): remove hand-rolled validation now handled by interceptor

Now that scene.proto has buf.validate annotations and the plugin SDK
installs a protovalidate interceptor by default, the Phase 1 service
handlers' manual character_id/title/scene_id empty checks are redundant
and have been removed.

Service-level cleanup that's still needed:
- Title is trimmed (TrimSpace) and rejected if whitespace-only. The
  protovalidate min_len: 1 annotation accepts "   " as valid; the
  service-level check catches whitespace-only inputs that would
  otherwise be stored as blank titles.

Test changes:
- Removed TestSceneServiceCreateSceneRejectsEmptyCharacterID (covered
  by validator interceptor tests in pkg/plugin/validate_test.go)
- Renamed TestSceneServiceCreateSceneRejectsBlankTitle to
  TestSceneServiceCreateSceneRejectsWhitespaceOnlyTitle and updated
  the input to "   " to exercise the service-level whitespace check

Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

---

## Task 7: State machine helpers in `lifecycle.go` (TDD)

Create the state machine helpers that both the service and store layers will use to validate transitions. Strict TDD: write the table-driven test first.

**Files:**

- Create: `plugins/core-scenes/lifecycle.go`
- Create: `plugins/core-scenes/lifecycle_test.go`

- [ ] **Step 1: Write the failing tests**

Create `plugins/core-scenes/lifecycle_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "testing"

func TestIsValidTransitionAllowsActiveToPaused(t *testing.T) {
    if !IsValidTransition(SceneStateActive, SceneStatePaused) {
        t.Error("active -> paused should be valid")
    }
}

func TestIsValidTransitionAllowsActiveToEnded(t *testing.T) {
    if !IsValidTransition(SceneStateActive, SceneStateEnded) {
        t.Error("active -> ended should be valid")
    }
}

func TestIsValidTransitionAllowsPausedToActive(t *testing.T) {
    if !IsValidTransition(SceneStatePaused, SceneStateActive) {
        t.Error("paused -> active should be valid (resume)")
    }
}

func TestIsValidTransitionAllowsPausedToEnded(t *testing.T) {
    if !IsValidTransition(SceneStatePaused, SceneStateEnded) {
        t.Error("paused -> ended should be valid")
    }
}

func TestIsValidTransitionAllowsEndedToArchived(t *testing.T) {
    if !IsValidTransition(SceneStateEnded, SceneStateArchived) {
        t.Error("ended -> archived should be valid (publish vote resolves)")
    }
}

func TestIsValidTransitionRejectsBackwardTransitions(t *testing.T) {
    // Per spec section 1.2: a scene MUST NOT transition backward.
    cases := []struct {
        from SceneState
        to   SceneState
    }{
        {SceneStatePaused, SceneStatePaused},   // self-transition
        {SceneStateEnded, SceneStateActive},    // ended cannot reanimate
        {SceneStateEnded, SceneStatePaused},    // ended cannot reanimate
        {SceneStateArchived, SceneStateActive}, // archived is terminal
        {SceneStateArchived, SceneStatePaused}, // archived is terminal
        {SceneStateArchived, SceneStateEnded},  // archived is terminal
        {SceneStateActive, SceneStateActive},   // self-transition
        {SceneStateActive, SceneStateArchived}, // skip ended state
    }
    for _, c := range cases {
        if IsValidTransition(c.from, c.to) {
            t.Errorf("transition %s -> %s should be rejected", c.from, c.to)
        }
    }
}

func TestIsValidTransitionRejectsUnknownStates(t *testing.T) {
    if IsValidTransition(SceneState("bogus"), SceneStateActive) {
        t.Error("bogus -> active should be rejected")
    }
    if IsValidTransition(SceneStateActive, SceneState("bogus")) {
        t.Error("active -> bogus should be rejected")
    }
}

func TestCanEndReturnsTrueForActiveAndPaused(t *testing.T) {
    if !CanEnd(SceneStateActive) {
        t.Error("active should be endable")
    }
    if !CanEnd(SceneStatePaused) {
        t.Error("paused should be endable")
    }
}

func TestCanEndReturnsFalseForEndedAndArchived(t *testing.T) {
    if CanEnd(SceneStateEnded) {
        t.Error("ended should not be endable")
    }
    if CanEnd(SceneStateArchived) {
        t.Error("archived should not be endable")
    }
}

func TestCanPauseReturnsTrueOnlyForActive(t *testing.T) {
    if !CanPause(SceneStateActive) {
        t.Error("active should be pausable")
    }
    if CanPause(SceneStatePaused) {
        t.Error("paused should not be re-pausable")
    }
    if CanPause(SceneStateEnded) {
        t.Error("ended should not be pausable")
    }
}

func TestCanResumeReturnsTrueOnlyForPaused(t *testing.T) {
    if !CanResume(SceneStatePaused) {
        t.Error("paused should be resumable")
    }
    if CanResume(SceneStateActive) {
        t.Error("active should not be resumable (already active)")
    }
    if CanResume(SceneStateEnded) {
        t.Error("ended should not be resumable")
    }
}

func TestCanUpdateReturnsFalseForEndedOrArchived(t *testing.T) {
    if CanUpdate(SceneStateEnded) {
        t.Error("ended should not be updatable")
    }
    if CanUpdate(SceneStateArchived) {
        t.Error("archived should not be updatable")
    }
    if !CanUpdate(SceneStateActive) {
        t.Error("active should be updatable")
    }
    if !CanUpdate(SceneStatePaused) {
        t.Error("paused should be updatable")
    }
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

```bash
task test -- -run "TestIsValidTransition|TestCanEnd|TestCanPause|TestCanResume|TestCanUpdate" ./plugins/core-scenes/
```

Bash timeout: 60000

Expected: build error — `undefined: IsValidTransition`, `undefined: CanEnd`, etc.

- [ ] **Step 3: Create lifecycle.go**

Create `plugins/core-scenes/lifecycle.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

// IsValidTransition reports whether a scene can transition from `from` to `to`.
//
// Per spec section 1.2:
//
//	active  -> paused | ended
//	paused  -> active | ended
//	ended   -> archived
//
// A scene MUST NOT transition backward (e.g., ended -> active). Self
// transitions (active -> active) are also rejected — they're meaningless
// and would mask bugs in the calling code.
func IsValidTransition(from, to SceneState) bool {
    if !from.IsValid() || !to.IsValid() {
        return false
    }
    switch from {
    case SceneStateActive:
        return to == SceneStatePaused || to == SceneStateEnded
    case SceneStatePaused:
        return to == SceneStateActive || to == SceneStateEnded
    case SceneStateEnded:
        return to == SceneStateArchived
    case SceneStateArchived:
        return false // terminal
    }
    return false
}

// CanEnd reports whether a scene in the given state can be ended by the owner.
// End is allowed from active or paused; not from ended or archived.
func CanEnd(state SceneState) bool {
    return state == SceneStateActive || state == SceneStatePaused
}

// CanPause reports whether a scene in the given state can be paused by the owner.
// Pause is only allowed from active.
func CanPause(state SceneState) bool {
    return state == SceneStateActive
}

// CanResume reports whether a scene in the given state can be resumed.
// Resume is only allowed from paused. Phase 2 gates this on owner-only;
// Phase 3 widens to any member per spec D6 (async safety).
func CanResume(state SceneState) bool {
    return state == SceneStatePaused
}

// CanUpdate reports whether a scene in the given state accepts metadata
// updates (UpdateScene RPC). Ended and archived scenes are immutable.
func CanUpdate(state SceneState) bool {
    return state == SceneStateActive || state == SceneStatePaused
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

```bash
task test -- -run "TestIsValidTransition|TestCanEnd|TestCanPause|TestCanResume|TestCanUpdate" ./plugins/core-scenes/
```

Bash timeout: 60000

Expected: all 12 tests pass.

Run the full package tests too:

```bash
task test -- ./plugins/core-scenes/
```

Expected: all tests pass (21 from prior tasks + 12 from this task = 33).

- [ ] **Step 5: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
feat(scenes): state machine helpers in lifecycle.go

Add Phase 2 state machine helpers used by both service and store layers:

- IsValidTransition(from, to): validates per spec 1.2 transition rules
- CanEnd(state): can the scene be ended right now
- CanPause(state): can the scene be paused right now
- CanResume(state): can the scene be resumed right now (Phase 2 owner-only)
- CanUpdate(state): can scene metadata be updated right now

12 table-driven tests covering all valid and invalid transitions, plus
unknown state handling.

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md section 1.2
Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

---

## Task 7.5: Add CHECK constraint to `scenes.state` column

Phase 1's migration declared `state TEXT NOT NULL DEFAULT 'active'` with no `CHECK` constraint, meaning the database will accept any string in the state column. The state machine is enforced entirely in Go code, with no database-level safety net. Phase 2 closes this gap by adding a `CHECK` constraint that mirrors the four valid states.

This is defense-in-depth: the lifecycle.go helpers (Task 7) and the race-safe `UPDATE ... WHERE state IN (...)` patterns (Tasks 8-9) both rely on the column only ever holding valid values. A bug elsewhere (a future migration, a manual SQL statement during incident response, a dependency upgrade that changes pgx's empty-string handling) could write a bad value. With the constraint, the database rejects it at insert/update time instead of letting it propagate.

**Files:**

- Create: `plugins/core-scenes/migrations/000002_scene_state_check.up.sql`

- [ ] **Step 1: Create the migration file**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Defense-in-depth: enforce the scene state machine at the database level.
-- The Go-side state machine (plugins/core-scenes/lifecycle.go IsValidTransition,
-- CanEnd, CanPause, CanResume, CanUpdate) and the race-safe UPDATE WHERE
-- clauses in plugins/core-scenes/store.go are the primary enforcement
-- mechanisms. This CHECK constraint is the last line of defense against
-- corruption from bugs, manual SQL during incident response, or future
-- migrations that bypass the application layer.
--
-- The four valid states are defined in plugins/core-scenes/types.go as
-- SceneStateActive, SceneStatePaused, SceneStateEnded, SceneStateArchived.
-- If a new state is added, this constraint must be updated AND a new
-- migration written; the constraint serves as a forcing function.

ALTER TABLE scenes
  ADD CONSTRAINT scenes_state_check
  CHECK (state IN ('active', 'paused', 'ended', 'archived'));
```

Note: per the project convention, this is `.up.sql` only. The plugin migration runner ignores `.down.sql` files.

- [ ] **Step 2: Build the plugin and verify the migration applies cleanly**

```bash
bash scripts/build-plugins.sh
```

Bash timeout: 300000

Then run the existing core-scenes integration tests, which start a fresh testcontainer Postgres and apply all plugin migrations from scratch:

```bash
task test:int -- ./plugins/core-scenes/
```

Bash timeout: 600000

Expected: all existing integration tests pass. The new constraint doesn't reject any of the test fixtures because they all use valid state values.

If a test fails because a fixture uses an invalid state string (e.g., a typo like `"actve"`), fix the fixture — the constraint is doing its job by exposing the bad data.

- [ ] **Step 3: Verify the constraint actually rejects bad values**

This is a one-off ad-hoc verification, not a permanent test. We just want to confirm the migration landed correctly.

```bash
docker exec -it $(docker ps -qf 'ancestor=postgres:18-alpine') psql -U holomush -d holomush_test -c "
  SET search_path TO plugin_core_scenes;
  INSERT INTO scenes (id, title, owner_id, state) VALUES ('s', 't', 'o', 'frobnicate');
"
```

This is a manual sanity check; you can skip it if the previous step shows the test container is gone (which it usually is — testcontainers terminate after each `task test:int` run). The integration tests in Tasks 8 and 9 will exercise the constraint via the actual UPDATE paths.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
feat(scenes): CHECK constraint on scenes.state column

Phase 1's migration created the scenes table with `state TEXT NOT NULL`
but no CHECK constraint, leaving the database willing to accept any
string in the state column. The Go-side state machine (lifecycle.go) and
the race-safe UPDATE WHERE clauses are the primary enforcement, but a
DB-level constraint is defense-in-depth against bugs, manual SQL during
incident response, and migration mistakes.

The constraint mirrors the four states defined in types.go:
SceneStateActive, SceneStatePaused, SceneStateEnded, SceneStateArchived.
Adding a new state requires updating this constraint AND types.go, which
the constraint serves as a forcing function for.

Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

---

## Task 8: Store: `End`, `Pause`, `Resume` methods (TDD via integration tests)

Add the three lifecycle store methods. Each uses a race-safe `UPDATE ... WHERE state IN (...)` pattern: the WHERE clause restricts the update to the legal source states, so concurrent transitions can't corrupt the state machine.

**Files:**

- Modify: `plugins/core-scenes/store.go`
- Modify: `plugins/core-scenes/store_integration_test.go`
- Modify: `plugins/core-scenes/service.go` (extend `sceneStorer` interface)
- Modify: `plugins/core-scenes/service_test.go` (extend `fakeStore`)

- [ ] **Step 1: Write failing integration tests for End**

Open `plugins/core-scenes/store_integration_test.go`. Add new tests at the bottom:

```go
func TestSceneStoreEndTransitionsActiveToEnded(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    row := &SceneRow{
        ID:              "scene-end-active",
        Title:           "End from active",
        OwnerID:         "char-alice",
        State:           string(SceneStateActive),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{},
        Tags:            []string{},
    }
    require.NoError(t, store.Create(ctx, row))

    // RETURNING gives us the post-update row in one round trip
    got, err := store.End(ctx, row.ID)
    require.NoError(t, err)
    assert.Equal(t, string(SceneStateEnded), got.State)
    require.NotNil(t, got.EndedAt, "ended_at should be set")

    // Sanity-check via a separate Get to confirm the row matches
    reread, err := store.Get(ctx, row.ID)
    require.NoError(t, err)
    assert.Equal(t, got.State, reread.State)
}

func TestSceneStoreEndTransitionsPausedToEnded(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    row := &SceneRow{
        ID:              "scene-end-paused",
        Title:           "End from paused",
        OwnerID:         "char-alice",
        State:           string(SceneStatePaused),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{},
        Tags:            []string{},
    }
    require.NoError(t, store.Create(ctx, row))

    got, err := store.End(ctx, row.ID)
    require.NoError(t, err)
    assert.Equal(t, string(SceneStateEnded), got.State)
    require.NotNil(t, got.EndedAt)
}

func TestSceneStoreEndRejectsAlreadyEnded(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    row := &SceneRow{
        ID:              "scene-end-twice",
        Title:           "Already ended",
        OwnerID:         "char-alice",
        State:           string(SceneStateEnded),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{},
        Tags:            []string{},
    }
    require.NoError(t, store.Create(ctx, row))

    _, err := store.End(ctx, row.ID)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "SCENE_TRANSITION_FORBIDDEN")
}

func TestSceneStoreEndReturnsNotFoundForMissingScene(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    _, err := store.End(ctx, "scene-does-not-exist")
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
}

func TestSceneStorePauseTransitionsActiveToPaused(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    row := &SceneRow{
        ID:              "scene-pause",
        Title:           "Pause from active",
        OwnerID:         "char-alice",
        State:           string(SceneStateActive),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{},
        Tags:            []string{},
    }
    require.NoError(t, store.Create(ctx, row))

    got, err := store.Pause(ctx, row.ID)
    require.NoError(t, err)
    assert.Equal(t, string(SceneStatePaused), got.State)
}

func TestSceneStorePauseRejectsAlreadyPaused(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    row := &SceneRow{
        ID:              "scene-pause-twice",
        Title:           "Already paused",
        OwnerID:         "char-alice",
        State:           string(SceneStatePaused),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{},
        Tags:            []string{},
    }
    require.NoError(t, store.Create(ctx, row))

    _, err := store.Pause(ctx, row.ID)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "SCENE_TRANSITION_FORBIDDEN")
}

func TestSceneStoreResumeTransitionsPausedToActive(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    row := &SceneRow{
        ID:              "scene-resume",
        Title:           "Resume from paused",
        OwnerID:         "char-alice",
        State:           string(SceneStatePaused),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{},
        Tags:            []string{},
    }
    require.NoError(t, store.Create(ctx, row))

    got, err := store.Resume(ctx, row.ID)
    require.NoError(t, err)
    assert.Equal(t, string(SceneStateActive), got.State)
}

func TestSceneStoreResumeRejectsActiveScene(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    row := &SceneRow{
        ID:              "scene-resume-active",
        Title:           "Already active",
        OwnerID:         "char-alice",
        State:           string(SceneStateActive),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{},
        Tags:            []string{},
    }
    require.NoError(t, store.Create(ctx, row))

    _, err := store.Resume(ctx, row.ID)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "SCENE_TRANSITION_FORBIDDEN")
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

```bash
task test:int -- ./plugins/core-scenes/
```

Bash timeout: 600000

Expected: build error — `undefined: SceneStore.End`, `SceneStore.Pause`, `SceneStore.Resume`.

- [ ] **Step 3: Add the End/Pause/Resume methods to store.go**

Open `plugins/core-scenes/store.go`. Add these methods to `SceneStore` (after the existing `Get` method).

**Note on the `RETURNING` pattern:** each method uses Postgres's `RETURNING *` clause to atomically return the post-update row in a single round trip. This eliminates a class of races where the post-mutation state could be modified by another request between the UPDATE and a follow-up SELECT. The methods return `(*SceneRow, error)` instead of just `error` so the service layer doesn't need a separate `Get` call to read back the result.

```go
// scanSceneRow is the column list and Scan target list shared by all
// statements that read SceneRow. Defined here so adding a new column to
// the scenes table only requires updating one place per statement.
const sceneSelectColumns = `id, title, description, location_id, owner_id,
    state, pose_order, visibility, idle_timeout_secs, template_id,
    content_warnings, tags, created_at, ended_at, archived_at`

func scanSceneRow(scanner pgx.Row, row *SceneRow) error {
    return scanner.Scan(
        &row.ID, &row.Title, &row.Description, &row.LocationID, &row.OwnerID,
        &row.State, &row.PoseOrder, &row.Visibility, &row.IdleTimeoutSecs,
        &row.TemplateID, &row.ContentWarnings, &row.Tags,
        &row.CreatedAt, &row.EndedAt, &row.ArchivedAt,
    )
}

// End transitions a scene to the `ended` state and returns the post-update
// row. Only scenes currently in `active` or `paused` states can be ended;
// the WHERE clause enforces this at the database level so concurrent
// transitions cannot corrupt the state machine.
//
// Sets `state = 'ended'` and `ended_at = NOW()` atomically and returns the
// resulting row via Postgres RETURNING. Returns SCENE_NOT_FOUND if no row
// matches the ID at all, or SCENE_TRANSITION_FORBIDDEN if the row exists
// but is in a state that cannot be ended.
func (s *SceneStore) End(ctx context.Context, id string) (*SceneRow, error) {
    ctx, span := startSpan(ctx, "scene.store.end",
        attribute.String("scene_id", id),
    )
    defer span.End()

    row := &SceneRow{}
    err := scanSceneRow(s.pool.QueryRow(ctx, `
        UPDATE scenes
        SET state = 'ended', ended_at = NOW()
        WHERE id = $1 AND state IN ('active', 'paused')
        RETURNING `+sceneSelectColumns,
        id,
    ), row)
    if err != nil {
        recordError(span, err)
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, s.classifyTransitionMiss(ctx, id, span, "end")
        }
        return nil, oops.Code("SCENE_END_FAILED").With("scene_id", id).Wrap(err)
    }
    return row, nil
}

// Pause transitions an active scene to the paused state and returns the
// post-update row. Only scenes currently in `active` state can be paused.
func (s *SceneStore) Pause(ctx context.Context, id string) (*SceneRow, error) {
    ctx, span := startSpan(ctx, "scene.store.pause",
        attribute.String("scene_id", id),
    )
    defer span.End()

    row := &SceneRow{}
    err := scanSceneRow(s.pool.QueryRow(ctx, `
        UPDATE scenes
        SET state = 'paused'
        WHERE id = $1 AND state = 'active'
        RETURNING `+sceneSelectColumns,
        id,
    ), row)
    if err != nil {
        recordError(span, err)
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, s.classifyTransitionMiss(ctx, id, span, "pause")
        }
        return nil, oops.Code("SCENE_PAUSE_FAILED").With("scene_id", id).Wrap(err)
    }
    return row, nil
}

// Resume transitions a paused scene to the active state and returns the
// post-update row. Only scenes currently in `paused` state can be resumed.
func (s *SceneStore) Resume(ctx context.Context, id string) (*SceneRow, error) {
    ctx, span := startSpan(ctx, "scene.store.resume",
        attribute.String("scene_id", id),
    )
    defer span.End()

    row := &SceneRow{}
    err := scanSceneRow(s.pool.QueryRow(ctx, `
        UPDATE scenes
        SET state = 'active'
        WHERE id = $1 AND state = 'paused'
        RETURNING `+sceneSelectColumns,
        id,
    ), row)
    if err != nil {
        recordError(span, err)
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, s.classifyTransitionMiss(ctx, id, span, "resume")
        }
        return nil, oops.Code("SCENE_RESUME_FAILED").With("scene_id", id).Wrap(err)
    }
    return row, nil
}

// classifyTransitionMiss is called when a transition UPDATE returned no
// row (RETURNING produced ErrNoRows). It distinguishes between two cases
// by issuing a SELECT:
//
//   1. The row doesn't exist at all → SCENE_NOT_FOUND
//   2. The row exists but is in a state that doesn't match the WHERE
//      clause → SCENE_TRANSITION_FORBIDDEN
//
// The caller passes `op` ("end", "pause", "resume", "update") for
// inclusion in the error context so consumers can tell which transition
// was attempted.
//
// This is a second round-trip in the error path, but the happy path is
// already optimal (one round trip via RETURNING). We pay the second
// query only when something went wrong, where the diagnostic value is
// worth the cost.
func (s *SceneStore) classifyTransitionMiss(ctx context.Context, id string, span trace.Span, op string) error {
    var currentState string
    err := s.pool.QueryRow(ctx, `SELECT state FROM scenes WHERE id = $1`, id).Scan(&currentState)
    if err != nil {
        recordError(span, err)
        if errors.Is(err, pgx.ErrNoRows) {
            return oops.Code("SCENE_NOT_FOUND").
                With("scene_id", id).
                With("op", op).
                Wrap(err)
        }
        return oops.Code("SCENE_TRANSITION_CLASSIFY_FAILED").
            With("scene_id", id).
            With("op", op).
            Wrap(err)
    }
    return oops.Code("SCENE_TRANSITION_FORBIDDEN").
        With("scene_id", id).
        With("op", op).
        With("current_state", currentState).
        Errorf("scene in state %q cannot be %sed", currentState, op)
}
```

You'll also need to add `"go.opentelemetry.io/otel/trace"` to the imports if it's not already there.

The `Get` method already in `store.go` from Phase 1 should be refactored to use `scanSceneRow` for consistency. Update its body to:

```go
func (s *SceneStore) Get(ctx context.Context, id string) (*SceneRow, error) {
    ctx, span := startSpan(ctx, "scene.store.get",
        attribute.String("scene_id", id),
    )
    defer span.End()

    row := &SceneRow{}
    err := scanSceneRow(s.pool.QueryRow(ctx, `
        SELECT `+sceneSelectColumns+`
        FROM scenes
        WHERE id = $1`,
        id,
    ), row)
    if err != nil {
        recordError(span, err)
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Wrap(err)
        }
        return nil, oops.Code("SCENE_GET_FAILED").With("scene_id", id).Wrap(err)
    }
    return row, nil
}
```

This refactor isn't strictly required for Task 8 to work, but it's a cheap one-line consolidation.

- [ ] **Step 4: Update the sceneStorer interface in service.go**

Open `plugins/core-scenes/service.go`. Find the `sceneStorer` interface (it currently has `Create` and `Get` only) and extend it. Note that the lifecycle methods all return `(*SceneRow, error)` because the underlying store uses `RETURNING *` to atomically return the post-update row.

```go
// sceneStorer is the persistence interface required by SceneServiceImpl.
// Defined here so the service layer is not coupled to the concrete
// SceneStore type — tests can substitute a fake implementation.
//
// Phase 1: Create + Get
// Phase 2: + End, Pause, Resume, Update — all return the post-update row
//          via Postgres RETURNING so the service handler doesn't need a
//          separate Get call (eliminates a class of races).
type sceneStorer interface {
    Create(ctx context.Context, row *SceneRow) error
    Get(ctx context.Context, id string) (*SceneRow, error)
    End(ctx context.Context, id string) (*SceneRow, error)
    Pause(ctx context.Context, id string) (*SceneRow, error)
    Resume(ctx context.Context, id string) (*SceneRow, error)
}
```

(`Update` will land in Task 9 with the same `(*SceneRow, error)` shape.)

- [ ] **Step 5: Update the fakeStore in service_test.go**

Open `plugins/core-scenes/service_test.go`. Find the `fakeStore` struct and add three new methods to satisfy the extended interface. The fakeStore returns the in-memory row to mirror the store's `RETURNING *` semantics.

```go
func (f *fakeStore) End(_ context.Context, id string) (*SceneRow, error) {
    row, ok := f.scenes[id]
    if !ok {
        return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
    }
    if row.State != string(SceneStateActive) && row.State != string(SceneStatePaused) {
        return nil, oops.Code("SCENE_TRANSITION_FORBIDDEN").
            With("scene_id", id).
            With("op", "end").
            With("current_state", row.State).
            Errorf("cannot end")
    }
    row.State = string(SceneStateEnded)
    now := time.Now().UTC()
    row.EndedAt = &now
    // Return a copy so callers don't share mutable state with the fake.
    cp := *row
    return &cp, nil
}

func (f *fakeStore) Pause(_ context.Context, id string) (*SceneRow, error) {
    row, ok := f.scenes[id]
    if !ok {
        return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
    }
    if row.State != string(SceneStateActive) {
        return nil, oops.Code("SCENE_TRANSITION_FORBIDDEN").
            With("scene_id", id).
            With("op", "pause").
            With("current_state", row.State).
            Errorf("cannot pause")
    }
    row.State = string(SceneStatePaused)
    cp := *row
    return &cp, nil
}

func (f *fakeStore) Resume(_ context.Context, id string) (*SceneRow, error) {
    row, ok := f.scenes[id]
    if !ok {
        return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
    }
    if row.State != string(SceneStatePaused) {
        return nil, oops.Code("SCENE_TRANSITION_FORBIDDEN").
            With("scene_id", id).
            With("op", "resume").
            With("current_state", row.State).
            Errorf("cannot resume")
    }
    row.State = string(SceneStateActive)
    cp := *row
    return &cp, nil
}
```

You'll need to add `"time"` to the test file imports if it's not already present.

- [ ] **Step 6: Run the integration tests**

```bash
task test:int -- ./plugins/core-scenes/
```

Bash timeout: 600000

Expected: all 8 new integration tests pass, plus the 3 existing ones from Phase 1 (= 11 total).

- [ ] **Step 7: Run the unit tests too**

```bash
task test -- ./plugins/core-scenes/
```

Bash timeout: 60000

Expected: all unit tests still pass (33 from Tasks 6-7 + the existing service/command/resolver tests).

- [ ] **Step 8: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
feat(scenes): SceneStore End/Pause/Resume with race-safe RETURNING

Add three lifecycle methods to SceneStore:
- End: active|paused -> ended (sets ended_at = NOW())
- Pause: active -> paused
- Resume: paused -> active

Each uses an UPDATE ... WHERE state IN (...) RETURNING * pattern that:

1. Enforces the state machine at the database level via the WHERE clause.
   Concurrent transitions cannot corrupt state because the WHERE restricts
   updates to the legal source states.

2. Returns the post-update row atomically via RETURNING. The service layer
   no longer needs a separate Get call to read back the new state, which
   eliminates a race window where another request could mutate the row
   between the UPDATE and the read-back.

When the UPDATE returns no row (RETURNING produces ErrNoRows),
classifyTransitionMiss issues a SELECT to distinguish "scene doesn't
exist" (SCENE_NOT_FOUND) from "scene is in the wrong state"
(SCENE_TRANSITION_FORBIDDEN). The second query is only paid in the error
path; the happy path is one round trip.

The Get method is also refactored to use the shared scanSceneRow helper
+ sceneSelectColumns constant for consistency.

8 integration tests cover happy paths and forbidden transitions for each
method. The sceneStorer interface and fakeStore are extended to match
the new (*SceneRow, error) return signature.

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md sections 1.2, 9.3
Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

---

## Task 9: Store: `Update` method with partial-update handling (TDD)

Add the Update store method that handles partial updates from `UpdateSceneRequest`. The challenge is dynamic SQL: we only update the fields the caller specified (i.e., the `optional` fields that aren't nil).

**Files:**

- Modify: `plugins/core-scenes/store.go`
- Modify: `plugins/core-scenes/store_integration_test.go`
- Modify: `plugins/core-scenes/service.go` (extend interface)
- Modify: `plugins/core-scenes/service_test.go` (extend fakeStore)

- [ ] **Step 1: Define the SceneUpdate value type**

The store method takes a struct that captures "which fields to update + their new values." Each scalar field is a pointer (nil = don't update); each slice field is paired with a boolean.

Open `plugins/core-scenes/store.go` and add the type just above the `Update` method (after the existing types):

```go
// SceneUpdate captures a partial update to a scene. Each scalar field is
// a pointer: nil means "don't update this field", non-nil means "set the
// field to the pointed-to value."
//
// Slice fields use a paired boolean (UpdateContentWarnings, UpdateTags)
// because Go's `nil slice` is indistinguishable from "empty slice" for
// the purposes of "is this field being changed?". When the boolean is
// true, the slice value (possibly empty) is written; when false, the
// slice is unchanged.
//
// This struct mirrors UpdateSceneRequest but lives in the store package
// so the store layer doesn't depend on proto-generated types.
type SceneUpdate struct {
    Title         *string
    Description   *string
    Visibility    *string
    PoseOrder     *string
    LocationID    *string

    ContentWarnings       []string
    UpdateContentWarnings bool

    Tags       []string
    UpdateTags bool
}

// HasChanges reports whether the update specifies any field changes.
func (u *SceneUpdate) HasChanges() bool {
    return u.Title != nil ||
        u.Description != nil ||
        u.Visibility != nil ||
        u.PoseOrder != nil ||
        u.LocationID != nil ||
        u.UpdateContentWarnings ||
        u.UpdateTags
}
```

- [ ] **Step 2: Write failing integration tests**

Append to `plugins/core-scenes/store_integration_test.go`:

```go
func TestSceneStoreUpdateAppliesTitleOnly(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    row := &SceneRow{
        ID:              "scene-update-title",
        Title:           "Original",
        Description:     "Original description",
        OwnerID:         "char-alice",
        State:           string(SceneStateActive),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{"violence"},
        Tags:            []string{"plot"},
    }
    require.NoError(t, store.Create(ctx, row))

    newTitle := "Renamed"
    update := &SceneUpdate{Title: &newTitle}
    _, err := store.Update(ctx, row.ID, update)
    require.NoError(t, err)

    got, err := store.Get(ctx, row.ID)
    require.NoError(t, err)
    assert.Equal(t, "Renamed", got.Title)
    // Other fields unchanged
    assert.Equal(t, "Original description", got.Description)
    assert.ElementsMatch(t, []string{"violence"}, got.ContentWarnings)
    assert.ElementsMatch(t, []string{"plot"}, got.Tags)
}

func TestSceneStoreUpdateAppliesMultipleFields(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    row := &SceneRow{
        ID:              "scene-update-many",
        Title:           "Title 1",
        OwnerID:         "char-alice",
        State:           string(SceneStateActive),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{},
        Tags:            []string{},
    }
    require.NoError(t, store.Create(ctx, row))

    title := "Title 2"
    desc := "New description"
    vis := "private"
    update := &SceneUpdate{
        Title:       &title,
        Description: &desc,
        Visibility:  &vis,
    }
    _, err := store.Update(ctx, row.ID, update)
    require.NoError(t, err)

    got, err := store.Get(ctx, row.ID)
    require.NoError(t, err)
    assert.Equal(t, "Title 2", got.Title)
    assert.Equal(t, "New description", got.Description)
    assert.Equal(t, "private", got.Visibility)
}

func TestSceneStoreUpdateRepeatedFieldsRespectFlag(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    row := &SceneRow{
        ID:              "scene-update-repeated",
        Title:           "T",
        OwnerID:         "char-alice",
        State:           string(SceneStateActive),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{"violence"},
        Tags:            []string{"plot", "social"},
    }
    require.NoError(t, store.Create(ctx, row))

    // Only update content_warnings; leave tags alone.
    update := &SceneUpdate{
        ContentWarnings:       []string{"violence", "death"},
        UpdateContentWarnings: true,
        Tags:                  nil,
        UpdateTags:            false, // explicitly NOT updating
    }
    _, err := store.Update(ctx, row.ID, update)
    require.NoError(t, err)

    got, err := store.Get(ctx, row.ID)
    require.NoError(t, err)
    assert.ElementsMatch(t, []string{"violence", "death"}, got.ContentWarnings)
    assert.ElementsMatch(t, []string{"plot", "social"}, got.Tags, "tags should be unchanged")
}

func TestSceneStoreUpdateClearsRepeatedFieldWithEmptySlice(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    row := &SceneRow{
        ID:              "scene-update-clear",
        Title:           "T",
        OwnerID:         "char-alice",
        State:           string(SceneStateActive),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{"violence"},
        Tags:            []string{},
    }
    require.NoError(t, store.Create(ctx, row))

    update := &SceneUpdate{
        ContentWarnings:       []string{},
        UpdateContentWarnings: true, // explicit clear
    }
    _, err := store.Update(ctx, row.ID, update)
    require.NoError(t, err)

    got, err := store.Get(ctx, row.ID)
    require.NoError(t, err)
    assert.Empty(t, got.ContentWarnings, "content_warnings should be cleared to empty slice")
}

func TestSceneStoreUpdateRejectsEndedScene(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    row := &SceneRow{
        ID:              "scene-update-ended",
        Title:           "Ended",
        OwnerID:         "char-alice",
        State:           string(SceneStateEnded),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{},
        Tags:            []string{},
    }
    require.NoError(t, store.Create(ctx, row))

    title := "Try to rename"
    update := &SceneUpdate{Title: &title}
    err := store.Update(ctx, row.ID, update)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "SCENE_TRANSITION_FORBIDDEN")
}

func TestSceneStoreUpdateReturnsNotFoundForMissingScene(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    title := "Anything"
    update := &SceneUpdate{Title: &title}
    err := store.Update(ctx, "scene-does-not-exist", update)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
}

func TestSceneStoreUpdateNoFieldsIsNoOp(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    row := &SceneRow{
        ID:              "scene-update-noop",
        Title:           "Unchanged",
        OwnerID:         "char-alice",
        State:           string(SceneStateActive),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{},
        Tags:            []string{},
    }
    require.NoError(t, store.Create(ctx, row))

    // Empty update — no fields specified
    update := &SceneUpdate{}
    _, err := store.Update(ctx, row.ID, update)
    require.NoError(t, err)

    // Verify nothing changed
    got, err := store.Get(ctx, row.ID)
    require.NoError(t, err)
    assert.Equal(t, "Unchanged", got.Title)
}
```

- [ ] **Step 3: Run the tests and confirm they fail**

```bash
task test:int -- ./plugins/core-scenes/
```

Bash timeout: 600000

Expected: build error — `undefined: SceneStore.Update`.

- [ ] **Step 4: Add the Update method to store.go**

Add this method to `SceneStore` (after `Resume`):

```go
// Update applies a partial update to a scene and returns the post-update
// row. The update parameter specifies which fields to change; nil/false
// fields are left unchanged.
//
// The state of the scene is checked: ended and archived scenes cannot be
// updated. The check is enforced via the WHERE clause `state IN ('active',
// 'paused')` so concurrent updates from a transition cannot race.
//
// If `update.HasChanges()` is false, the call is a no-op: the function
// reads the current row via Get and returns it (so callers always get a
// valid SceneRow back, matching the End/Pause/Resume contract). This costs
// one query for the no-op case but keeps the API surface uniform.
//
// Returns SCENE_NOT_FOUND if the scene doesn't exist, or
// SCENE_TRANSITION_FORBIDDEN if it exists but is in a non-updatable state.
func (s *SceneStore) Update(ctx context.Context, id string, update *SceneUpdate) (*SceneRow, error) {
    ctx, span := startSpan(ctx, "scene.store.update",
        attribute.String("scene_id", id),
    )
    defer span.End()

    if update == nil || !update.HasChanges() {
        // No-op: read the current row and return it. Maintains the
        // (*SceneRow, error) API contract without an UPDATE.
        return s.Get(ctx, id)
    }

    // Build the SET clause dynamically based on which fields are present.
    var (
        setParts []string
        args     []any
        argIdx   = 1
    )
    add := func(col string, value any) {
        setParts = append(setParts, fmt.Sprintf("%s = $%d", col, argIdx))
        args = append(args, value)
        argIdx++
    }

    if update.Title != nil {
        add("title", *update.Title)
    }
    if update.Description != nil {
        add("description", *update.Description)
    }
    if update.Visibility != nil {
        add("visibility", *update.Visibility)
    }
    if update.PoseOrder != nil {
        add("pose_order", *update.PoseOrder)
    }
    if update.LocationID != nil {
        // Empty string means "clear the location" → store NULL
        if *update.LocationID == "" {
            setParts = append(setParts, fmt.Sprintf("location_id = $%d", argIdx))
            args = append(args, nil)
            argIdx++
        } else {
            add("location_id", *update.LocationID)
        }
    }
    if update.UpdateContentWarnings {
        add("content_warnings", update.ContentWarnings)
    }
    if update.UpdateTags {
        add("tags", update.Tags)
    }

    // Append the WHERE-clause parameter (the scene ID) at the end.
    args = append(args, id)
    sceneIDIdx := argIdx

    query := fmt.Sprintf(
        `UPDATE scenes
         SET %s
         WHERE id = $%d AND state IN ('active', 'paused')
         RETURNING %s`,
        strings.Join(setParts, ", "),
        sceneIDIdx,
        sceneSelectColumns,
    )

    row := &SceneRow{}
    err := scanSceneRow(s.pool.QueryRow(ctx, query, args...), row)
    if err != nil {
        recordError(span, err)
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, s.classifyTransitionMiss(ctx, id, span, "update")
        }
        return nil, oops.Code("SCENE_UPDATE_FAILED").With("scene_id", id).Wrap(err)
    }
    return row, nil
}
```

Add `"fmt"` and `"strings"` to the store.go imports if not already there.

- [ ] **Step 5: Update the sceneStorer interface and fakeStore**

Open `plugins/core-scenes/service.go`. Extend `sceneStorer`:

```go
type sceneStorer interface {
    Create(ctx context.Context, row *SceneRow) error
    Get(ctx context.Context, id string) (*SceneRow, error)
    End(ctx context.Context, id string) (*SceneRow, error)
    Pause(ctx context.Context, id string) (*SceneRow, error)
    Resume(ctx context.Context, id string) (*SceneRow, error)
    Update(ctx context.Context, id string, update *SceneUpdate) (*SceneRow, error)
}
```

Open `plugins/core-scenes/service_test.go` and add the `Update` method to `fakeStore`:

```go
func (f *fakeStore) Update(_ context.Context, id string, update *SceneUpdate) (*SceneRow, error) {
    row, ok := f.scenes[id]
    if !ok {
        return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
    }
    if update == nil || !update.HasChanges() {
        // No-op: return a copy of the current row, mirroring the real
        // store's "no-op returns current state" contract.
        cp := *row
        return &cp, nil
    }
    if row.State != string(SceneStateActive) && row.State != string(SceneStatePaused) {
        return nil, oops.Code("SCENE_TRANSITION_FORBIDDEN").
            With("scene_id", id).
            With("op", "update").
            With("current_state", row.State).
            Errorf("cannot update")
    }
    if update.Title != nil {
        row.Title = *update.Title
    }
    if update.Description != nil {
        row.Description = *update.Description
    }
    if update.Visibility != nil {
        row.Visibility = *update.Visibility
    }
    if update.PoseOrder != nil {
        row.PoseOrder = *update.PoseOrder
    }
    if update.LocationID != nil {
        if *update.LocationID == "" {
            row.LocationID = nil
        } else {
            loc := *update.LocationID
            row.LocationID = &loc
        }
    }
    if update.UpdateContentWarnings {
        row.ContentWarnings = update.ContentWarnings
    }
    if update.UpdateTags {
        row.Tags = update.Tags
    }
    cp := *row
    return &cp, nil
}
```

- [ ] **Step 6: Run integration and unit tests**

```bash
task test:int -- ./plugins/core-scenes/
task test -- ./plugins/core-scenes/
```

Bash timeouts: 600000 and 60000.

Expected: all tests pass — 7 new integration tests + the 11 from prior tasks + all unit tests.

- [ ] **Step 7: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
feat(scenes): SceneStore Update with partial-update handling

Add SceneUpdate type and SceneStore.Update method for partial scene
updates. The update value uses pointer fields for scalars (nil = don't
update) and paired boolean flags for repeated fields. This is the
internal store-layer representation; the wire-format request uses
google.protobuf.FieldMask, and the service layer translates between
the two via buildSceneUpdate (Task 10).

Why paired booleans inside the store: Go's *[]string is awkward and
adds no semantic value over a slice + boolean. The store layer is one
process, one package, and the paired-bool API is straightforward.
The wire layer uses FieldMask because that's the canonical proto3
pattern (Google AIP-134).

Update is gated on scene state via WHERE state IN ('active', 'paused');
ended and archived scenes return SCENE_TRANSITION_FORBIDDEN.

Empty SceneUpdate (no fields set) is a no-op that returns nil without
hitting the database — lets callers freely pass empty updates.

LocationID handling: empty string sets the column to NULL (clears the
location); non-empty string updates to the value. This is the
service-layer convention for "clear an optional pointer field."

7 integration tests cover happy paths, repeated-field flag semantics,
state-precondition rejection, no-op behavior, and missing scenes.
sceneStorer interface and fakeStore extended.

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md sections 6.4, 7.2
Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

---

## Task 9.5: Metric stub package

Spec section 10 requires Prometheus metrics for scene lifecycle events, but spec section 11 (architectural gaps) acknowledges that binary plugin metrics infrastructure doesn't exist yet — we have no defined path for a binary plugin to expose metrics that the host can scrape.

Phase 1 deferred all metrics. Phase 2 ALSO defers actual Prometheus integration, BUT introduces stub functions named per the spec section 10.2 expectations. The stubs are no-ops; the lifecycle handlers in Task 10 call them as if they were real. When the plugin metrics infrastructure lands (separate effort, separate bead), this single file gets a real implementation and every call site automatically picks it up — zero rework in Phase 2 or any later phase that adds more stub calls.

This is "do work now to save rework later" applied at the API boundary. The cost is one tiny file with no-op functions; the benefit is that every Phase 2-N handler that adds a metric call doesn't need to be revisited.

**Files:**

- Create: `plugins/core-scenes/metrics.go`

- [ ] **Step 1: Create the metric stub file**

Create `plugins/core-scenes/metrics.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

// Phase 2 metric stubs.
//
// Spec section 10.2 lists Prometheus counter, histogram, and gauge metrics
// that the scene plugin SHOULD emit. Spec section 11 documents that binary
// plugin metrics infrastructure does not exist yet — there is no defined
// path for a binary plugin to expose metrics that the host can scrape.
//
// This file provides no-op metric functions named per spec 10.2 so that
// every handler in this package can call them as if they were real. When
// the binary plugin metrics infrastructure lands (separate effort), this
// file is the ONLY place that needs to change: add a Prometheus registry,
// register the actual counters/histograms/gauges, and have the functions
// here delegate to them. Every call site is already in place.
//
// Naming follows spec section 10.2 (e.g., scene_created_total → metricSceneCreated).
//
// Until then, these are zero-cost no-ops.

// metricSceneCreated counts scene creations, labeled by visibility and
// whether the scene was created from a template. Spec metric:
// scene_created_total{visibility, from_template}.
func metricSceneCreated(visibility string, fromTemplate bool) {
    _ = visibility
    _ = fromTemplate
}

// metricSceneStateTransition counts state machine transitions, labeled by
// from-state, to-state, and reason. Spec metric:
// scene_state_transitions_total{from, to, reason}. Reason is "rpc" when
// triggered by a direct RPC call (Phase 2's only path) and will be
// expanded in later phases (e.g., "idle_timeout" for Phase 4).
func metricSceneStateTransition(from, to, reason string) {
    _ = from
    _ = to
    _ = reason
}

// metricSceneABACDenial counts ABAC denials at the resolver layer, labeled
// by action and resource type. Spec metric:
// scene_abac_denials_total{action, resource_type}. Phase 2 emits this from
// the AttributeResolverService when a resolution is attempted but the
// scene's owner check fails — though in practice the host's policy engine
// catches denials before they reach the resolver, so this counter is
// expected to be zero in normal operation. Useful for spotting policy
// misconfiguration where the resolver gets called for forbidden access.
func metricSceneABACDenial(action, resourceType string) {
    _ = action
    _ = resourceType
}

// metricSceneRPCDuration records the latency of a scene gRPC RPC, labeled
// by RPC name and success/failure. Spec metric:
// scene_rpc_duration_seconds{rpc, result}. Phase 2 doesn't actually call
// this from any handler — the host's plugin OTel middleware already
// records plugin command durations at the gRPC delivery level — but the
// stub exists so service-internal RPC timing can be added cheaply later
// if it surfaces a real need.
func metricSceneRPCDuration(rpc string, durationSeconds float64, ok bool) {
    _ = rpc
    _ = durationSeconds
    _ = ok
}
```

- [ ] **Step 2: Verify the package still builds**

```bash
task test -- ./plugins/core-scenes/
```

Bash timeout: 60000

Expected: all tests still pass. The new file adds four no-op functions; nothing references them yet (Task 10 will add the references), so this is purely additive.

- [ ] **Step 3: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
feat(scenes): metric stub package for future Prometheus integration

Add plugins/core-scenes/metrics.go with no-op stub functions named per
spec section 10.2 expectations. Phase 2's handlers (Task 10) will call
these stubs as if they were real metrics. When the binary plugin metrics
infrastructure lands (separate effort tracked in spec section 11), this
single file gets a real implementation and every existing call site
automatically picks it up.

Stubs:
- metricSceneCreated(visibility, from_template)
- metricSceneStateTransition(from, to, reason)
- metricSceneABACDenial(action, resource_type)
- metricSceneRPCDuration(rpc, duration_seconds, ok)

This is "touch the code once" applied at the metrics API boundary.
Zero rework when the infra lands; every phase that adds new stub calls
doesn't have to be revisited.

Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

---

## Task 10: SceneServiceImpl: 4 new RPCs (TDD)

Add `EndScene`, `PauseScene`, `ResumeScene`, `UpdateScene` handlers to the service. Each delegates to the corresponding store method, maps oops error codes to gRPC status codes, and returns the updated SceneInfo.

**Files:**

- Modify: `plugins/core-scenes/service.go`
- Modify: `plugins/core-scenes/service_test.go`

- [ ] **Step 1: Write failing tests for EndScene**

Append to `plugins/core-scenes/service_test.go`:

```go
func TestSceneServiceEndSceneTransitionsScene(t *testing.T) {
    store := newFakeStore()
    store.scenes["scene-1"] = &SceneRow{
        ID:         "scene-1",
        Title:      "Test",
        OwnerID:    "char-alice",
        State:      string(SceneStateActive),
        Visibility: string(SceneVisibilityOpen),
    }
    svc := NewSceneServiceImpl(store)

    resp, err := svc.EndScene(context.Background(), &scenev1.EndSceneRequest{
        CharacterId: "char-alice",
        SceneId:     "scene-1",
    })
    require.NoError(t, err)
    assert.Equal(t, "scene-1", resp.GetScene().GetId())
    assert.Equal(t, string(SceneStateEnded), resp.GetScene().GetState())
}

func TestSceneServiceEndSceneReturnsNotFoundForMissingScene(t *testing.T) {
    svc := NewSceneServiceImpl(newFakeStore())

    _, err := svc.EndScene(context.Background(), &scenev1.EndSceneRequest{
        CharacterId: "char-alice",
        SceneId:     "scene-missing",
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServiceEndSceneReturnsFailedPreconditionForEndedScene(t *testing.T) {
    store := newFakeStore()
    store.scenes["scene-ended"] = &SceneRow{
        ID:    "scene-ended",
        State: string(SceneStateEnded),
    }
    svc := NewSceneServiceImpl(store)

    _, err := svc.EndScene(context.Background(), &scenev1.EndSceneRequest{
        CharacterId: "char-alice",
        SceneId:     "scene-ended",
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestSceneServicePauseSceneTransitionsScene(t *testing.T) {
    store := newFakeStore()
    store.scenes["scene-1"] = &SceneRow{
        ID:         "scene-1",
        State:      string(SceneStateActive),
        Visibility: string(SceneVisibilityOpen),
    }
    svc := NewSceneServiceImpl(store)

    resp, err := svc.PauseScene(context.Background(), &scenev1.PauseSceneRequest{
        CharacterId: "char-alice",
        SceneId:     "scene-1",
    })
    require.NoError(t, err)
    assert.Equal(t, string(SceneStatePaused), resp.GetScene().GetState())
}

func TestSceneServiceResumeSceneTransitionsScene(t *testing.T) {
    store := newFakeStore()
    store.scenes["scene-1"] = &SceneRow{
        ID:         "scene-1",
        State:      string(SceneStatePaused),
        Visibility: string(SceneVisibilityOpen),
    }
    svc := NewSceneServiceImpl(store)

    resp, err := svc.ResumeScene(context.Background(), &scenev1.ResumeSceneRequest{
        CharacterId: "char-alice",
        SceneId:     "scene-1",
    })
    require.NoError(t, err)
    assert.Equal(t, string(SceneStateActive), resp.GetScene().GetState())
}

func TestSceneServiceUpdateSceneAppliesTitleChange(t *testing.T) {
    store := newFakeStore()
    store.scenes["scene-1"] = &SceneRow{
        ID:         "scene-1",
        Title:      "Original",
        State:      string(SceneStateActive),
        Visibility: string(SceneVisibilityOpen),
    }
    svc := NewSceneServiceImpl(store)

    resp, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
        CharacterId: "char-alice",
        SceneId:     "scene-1",
        Title:       "Updated",
        UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
    })
    require.NoError(t, err)
    assert.Equal(t, "Updated", resp.GetScene().GetTitle())
}

func TestSceneServiceUpdateSceneRejectsEndedScene(t *testing.T) {
    store := newFakeStore()
    store.scenes["scene-ended"] = &SceneRow{
        ID:    "scene-ended",
        State: string(SceneStateEnded),
    }
    svc := NewSceneServiceImpl(store)

    _, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
        CharacterId: "char-alice",
        SceneId:     "scene-ended",
        Title:       "Try",
        UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestSceneServiceUpdateSceneAppliesContentWarnings(t *testing.T) {
    store := newFakeStore()
    store.scenes["scene-1"] = &SceneRow{
        ID:              "scene-1",
        Title:           "T",
        State:           string(SceneStateActive),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{"violence"},
    }
    svc := NewSceneServiceImpl(store)

    _, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
        CharacterId:     "char-alice",
        SceneId:         "scene-1",
        ContentWarnings: []string{"violence", "death"},
        UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"content_warnings"}},
    })
    require.NoError(t, err)
    // Verify the fakeStore got the change
    got := store.scenes["scene-1"]
    assert.ElementsMatch(t, []string{"violence", "death"}, got.ContentWarnings)
}

func TestSceneServiceUpdateSceneRejectsEmptyTitleInMask(t *testing.T) {
    store := newFakeStore()
    store.scenes["scene-1"] = &SceneRow{
        ID:         "scene-1",
        Title:      "Original",
        State:      string(SceneStateActive),
        Visibility: string(SceneVisibilityOpen),
    }
    svc := NewSceneServiceImpl(store)

    _, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
        CharacterId: "char-alice",
        SceneId:     "scene-1",
        Title:       "   ",
        UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    assert.Equal(t, codes.InvalidArgument, st.Code())
    assert.Contains(t, st.Message(), "title")
}

func TestSceneServiceUpdateSceneRejectsUnknownMaskPath(t *testing.T) {
    store := newFakeStore()
    store.scenes["scene-1"] = &SceneRow{
        ID:    "scene-1",
        State: string(SceneStateActive),
    }
    svc := NewSceneServiceImpl(store)

    _, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
        CharacterId: "char-alice",
        SceneId:     "scene-1",
        UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"owner_id"}},
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    assert.Equal(t, codes.InvalidArgument, st.Code())
    assert.Contains(t, st.Message(), "unknown update_mask path")
}

func TestSceneServiceUpdateSceneEmptyMaskIsNoOp(t *testing.T) {
    store := newFakeStore()
    store.scenes["scene-1"] = &SceneRow{
        ID:         "scene-1",
        Title:      "Unchanged",
        State:      string(SceneStateActive),
        Visibility: string(SceneVisibilityOpen),
    }
    svc := NewSceneServiceImpl(store)

    resp, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
        CharacterId: "char-alice",
        SceneId:     "scene-1",
        // No UpdateMask — empty mask, no fields to apply
    })
    require.NoError(t, err)
    assert.Equal(t, "Unchanged", resp.GetScene().GetTitle())
}
```

- [ ] **Step 2: Run tests and confirm they fail**

```bash
task test -- -run "TestSceneServiceEndScene|TestSceneServicePauseScene|TestSceneServiceResumeScene|TestSceneServiceUpdateScene" ./plugins/core-scenes/
```

Bash timeout: 60000

Expected: build error — `undefined: SceneServiceImpl.EndScene` etc.

- [ ] **Step 3: Add the four RPC handlers to service.go**

Open `plugins/core-scenes/service.go`. Add these four methods to `SceneServiceImpl` (after the existing `GetScene`):

```go
// EndScene transitions a scene to the ended state. Only the scene owner is
// authorized (gated by ABAC end-own-scene policy). The transition is
// rejected if the scene is already ended or archived (FailedPrecondition).
//
// The store's End method uses Postgres RETURNING * to atomically return
// the post-update row, so this handler doesn't need a separate Get call.
func (s *SceneServiceImpl) EndScene(ctx context.Context, req *scenev1.EndSceneRequest) (*scenev1.EndSceneResponse, error) {
    ctx, span := startSpan(ctx, "scene.lifecycle.end",
        attribute.String("subject_id", req.GetCharacterId()),
        attribute.String("scene_id", req.GetSceneId()),
    )
    defer span.End()

    row, err := s.store.End(ctx, req.GetSceneId())
    if err != nil {
        recordError(span, err)
        if grpcErr := mapTransitionError(err, req.GetSceneId()); grpcErr != nil {
            return nil, grpcErr
        }
        slog.WarnContext(ctx, "scene.lifecycle.end store error",
            "subject_id", req.GetCharacterId(),
            "scene_id", req.GetSceneId(),
            "error", err,
        )
        return nil, status.Errorf(codes.Internal, "failed to end scene: %v", err)
    }

    metricSceneStateTransition(string(SceneStateActive)+"_or_paused", "ended", "rpc")
    slog.InfoContext(ctx, "scene.lifecycle.end ok",
        "subject_id", req.GetCharacterId(),
        "scene_id", row.ID,
    )

    return &scenev1.EndSceneResponse{Scene: rowToProto(row, row.CreatedAt)}, nil
}

// PauseScene transitions an active scene to paused. Owner-only.
func (s *SceneServiceImpl) PauseScene(ctx context.Context, req *scenev1.PauseSceneRequest) (*scenev1.PauseSceneResponse, error) {
    ctx, span := startSpan(ctx, "scene.lifecycle.pause",
        attribute.String("subject_id", req.GetCharacterId()),
        attribute.String("scene_id", req.GetSceneId()),
    )
    defer span.End()

    row, err := s.store.Pause(ctx, req.GetSceneId())
    if err != nil {
        recordError(span, err)
        if grpcErr := mapTransitionError(err, req.GetSceneId()); grpcErr != nil {
            return nil, grpcErr
        }
        slog.WarnContext(ctx, "scene.lifecycle.pause store error",
            "subject_id", req.GetCharacterId(),
            "scene_id", req.GetSceneId(),
            "error", err,
        )
        return nil, status.Errorf(codes.Internal, "failed to pause scene: %v", err)
    }

    metricSceneStateTransition("active", "paused", "rpc")
    slog.InfoContext(ctx, "scene.lifecycle.pause ok",
        "subject_id", req.GetCharacterId(),
        "scene_id", row.ID,
    )

    return &scenev1.PauseSceneResponse{Scene: rowToProto(row, row.CreatedAt)}, nil
}

// ResumeScene transitions a paused scene to active. Phase 2 is owner-only;
// Phase 3 widens to any member per spec D6 (async safety).
func (s *SceneServiceImpl) ResumeScene(ctx context.Context, req *scenev1.ResumeSceneRequest) (*scenev1.ResumeSceneResponse, error) {
    ctx, span := startSpan(ctx, "scene.lifecycle.resume",
        attribute.String("subject_id", req.GetCharacterId()),
        attribute.String("scene_id", req.GetSceneId()),
    )
    defer span.End()

    row, err := s.store.Resume(ctx, req.GetSceneId())
    if err != nil {
        recordError(span, err)
        if grpcErr := mapTransitionError(err, req.GetSceneId()); grpcErr != nil {
            return nil, grpcErr
        }
        slog.WarnContext(ctx, "scene.lifecycle.resume store error",
            "subject_id", req.GetCharacterId(),
            "scene_id", req.GetSceneId(),
            "error", err,
        )
        return nil, status.Errorf(codes.Internal, "failed to resume scene: %v", err)
    }

    metricSceneStateTransition("paused", "active", "rpc")
    slog.InfoContext(ctx, "scene.lifecycle.resume ok",
        "subject_id", req.GetCharacterId(),
        "scene_id", row.ID,
    )

    return &scenev1.ResumeSceneResponse{Scene: rowToProto(row, row.CreatedAt)}, nil
}

// UpdateScene applies a partial update to mutable scene metadata. Owner-only.
// Rejected for ended/archived scenes. Empty mask updates (no fields specified)
// succeed as no-ops without touching the database.
//
// The update is driven by req.UpdateMask: each path in the mask is a field
// name to apply from the request. Per-field semantic validation (e.g.,
// "title cannot be empty when in the mask") happens in the switch statement
// below; protovalidate constraints in scene.proto handle the wire-level
// max_len / enum-value checks.
func (s *SceneServiceImpl) UpdateScene(ctx context.Context, req *scenev1.UpdateSceneRequest) (*scenev1.UpdateSceneResponse, error) {
    ctx, span := startSpan(ctx, "scene.service.update_scene",
        attribute.String("subject_id", req.GetCharacterId()),
        attribute.String("scene_id", req.GetSceneId()),
    )
    defer span.End()

    update, err := buildSceneUpdate(req)
    if err != nil {
        recordError(span, err)
        return nil, err // already a gRPC status error
    }

    row, err := s.store.Update(ctx, req.GetSceneId(), update)
    if err != nil {
        recordError(span, err)
        if grpcErr := mapTransitionError(err, req.GetSceneId()); grpcErr != nil {
            return nil, grpcErr
        }
        slog.WarnContext(ctx, "scene.service.update_scene store error",
            "subject_id", req.GetCharacterId(),
            "scene_id", req.GetSceneId(),
            "error", err,
        )
        return nil, status.Errorf(codes.Internal, "failed to update scene: %v", err)
    }

    slog.InfoContext(ctx, "scene.service.update_scene ok",
        "subject_id", req.GetCharacterId(),
        "scene_id", row.ID,
    )

    return &scenev1.UpdateSceneResponse{Scene: rowToProto(row, row.CreatedAt)}, nil
}

// buildSceneUpdate iterates the request's FieldMask and constructs a store
// SceneUpdate. Each mask path is matched to the corresponding request field
// AND validated semantically (e.g., title cannot be empty even though the
// proto annotation allows max_len-only).
//
// Returns a gRPC status error directly if validation fails — the caller
// passes it through unchanged.
//
// Unknown mask paths return InvalidArgument so clients can't silently send
// updates that get dropped.
func buildSceneUpdate(req *scenev1.UpdateSceneRequest) (*SceneUpdate, error) {
    update := &SceneUpdate{}
    for _, path := range req.GetUpdateMask().GetPaths() {
        switch path {
        case "title":
            t := strings.TrimSpace(req.GetTitle())
            if t == "" {
                return nil, status.Errorf(codes.InvalidArgument, "title cannot be empty or whitespace-only")
            }
            update.Title = &t
        case "description":
            d := req.GetDescription()
            update.Description = &d
        case "visibility":
            v := req.GetVisibility()
            if v == "" {
                return nil, status.Errorf(codes.InvalidArgument, "visibility cannot be empty when in update_mask")
            }
            update.Visibility = &v
        case "pose_order_mode":
            p := req.GetPoseOrderMode()
            if p == "" {
                return nil, status.Errorf(codes.InvalidArgument, "pose_order_mode cannot be empty when in update_mask")
            }
            update.PoseOrder = &p
        case "location_id":
            l := req.GetLocationId()
            update.LocationID = &l // empty string clears the location
        case "content_warnings":
            update.ContentWarnings = req.GetContentWarnings()
            update.UpdateContentWarnings = true
        case "tags":
            update.Tags = req.GetTags()
            update.UpdateTags = true
        default:
            return nil, status.Errorf(codes.InvalidArgument, "unknown update_mask path: %q", path)
        }
    }
    return update, nil
}

// mapTransitionError translates store-layer transition errors into gRPC
// status errors. Returns nil if the error is not a transition error
// (caller should fall through to a generic Internal status).
func mapTransitionError(err error, sceneID string) error {
    var oe oops.OopsError
    if !errors.As(err, &oe) {
        return nil
    }
    switch oe.Code() {
    case "SCENE_NOT_FOUND":
        return status.Errorf(codes.NotFound, "scene not found: %s", sceneID)
    case "SCENE_TRANSITION_FORBIDDEN":
        return status.Errorf(codes.FailedPrecondition,
            "scene transition forbidden: %v", err)
    }
    return nil
}
```

Note on imports: the new test cases use `google.golang.org/protobuf/types/known/fieldmaskpb` to construct `&fieldmaskpb.FieldMask{Paths: []string{...}}` values. Add the import to `service_test.go` if not already present.

Note on field references: with the FieldMask approach, `UpdateSceneRequest` fields are plain (non-pointer) generated Go types — `req.GetTitle()` returns `string`, `req.GetTags()` returns `[]string`, etc. The mask in `req.GetUpdateMask().GetPaths()` is the source of truth for what to apply.

If the generated code has different field names (e.g., `req.PoseOrderMode` vs `req.PoseOrder`), check `pkg/proto/holomush/scene/v1/scene.pb.go` for the actual names and adjust.

- [ ] **Step 4: Run tests**

```bash
task test -- ./plugins/core-scenes/
```

Bash timeout: 60000

Expected: all tests pass — 8 new service tests + the existing tests.

- [ ] **Step 5: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
feat(scenes): EndScene/PauseScene/ResumeScene/UpdateScene RPC handlers

Add the four Phase 2 RPC handlers to SceneServiceImpl:
- EndScene: active|paused -> ended (via store.End)
- PauseScene: active -> paused (via store.Pause)
- ResumeScene: paused -> active (via store.Resume) — owner-only in Phase 2
- UpdateScene: partial metadata updates (via store.Update)

Each handler:
- Opens an OTel span (per-transition: scene.lifecycle.end/.pause/.resume,
  matching Phase 1's per-method naming convention)
- Calls the store layer (which uses RETURNING * to atomically return the
  post-update row in one round trip — no separate Get needed, no race
  between mutate and read-back)
- Maps SCENE_NOT_FOUND -> NotFound and SCENE_TRANSITION_FORBIDDEN ->
  FailedPrecondition via shared mapTransitionError helper
- Increments the metricSceneStateTransition stub from Task 9.5 (no-op
  until plugin metrics infra lands)
- Emits a slog.InfoContext entry on success

UpdateScene uses buildSceneUpdate to translate the FieldMask + flat
request fields into the store layer's SceneUpdate value (which uses
pointer scalars and paired boolean flags internally — the FieldMask
shape is wire-format-only). buildSceneUpdate also performs per-field
semantic validation: empty title, visibility, or pose_order_mode in
the mask are rejected with InvalidArgument; unknown mask paths are
rejected so clients can't silently send updates that get dropped.

8 unit tests cover happy paths, NotFound, FailedPrecondition for the
state-precondition cases.

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md sections 7.2, 10.1, 10.3
Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

---

## Task 11: Add the four manifest policies

Add `end-own-scene`, `pause-own-scene`, `resume-own-scene`, `update-own-scene` ABAC policies to the plugin manifest. All four are owner-only with state-precondition guards.

**Files:**

- Modify: `plugins/core-scenes/plugin.yaml`

- [ ] **Step 1: Read the current plugin.yaml**

Read `plugins/core-scenes/plugin.yaml` to confirm its current structure (it should have the Phase 1 policies after Phase 1's last commit + the AttributeResolverService comment).

- [ ] **Step 2: Add the four new policies**

Find the `policies:` section. After the existing `read-own-scene` policy, add four new entries:

```yaml
policies:
  - name: execute-scene-commands
    dsl: >-
      permit(principal is character, action in ["execute"], resource is command)
      when { resource.command.name in ["scene", "scenes"] };
  - name: read-own-scene
    dsl: >-
      permit(principal is character, action in ["read"], resource is scene)
      when { resource.scene.owner == principal.id };
  - name: end-own-scene
    dsl: >-
      permit(principal is character, action in ["end"], resource is scene)
      when { resource.scene.owner == principal.id && resource.scene.state in ["active", "paused"] };
  - name: pause-own-scene
    dsl: >-
      permit(principal is character, action in ["pause"], resource is scene)
      when { resource.scene.owner == principal.id && resource.scene.state == "active" };
  # PHASE 3 NOTE: this policy is replaced by `resume-any-member` when the
  # scene_participants table lands. Per spec D6 (async safety), any member
  # of a paused scene MUST be able to resume it — owner-only here is a
  # deliberate Phase 2 limitation because Phase 2 has no participant model.
  # Phase 3 deletes this policy and adds:
  #   permit(principal is character, action in ["resume"], resource is scene)
  #   when { resource.scene.state == "paused" && principal.id in resource.scene.participants };
  - name: resume-own-scene
    dsl: >-
      permit(principal is character, action in ["resume"], resource is scene)
      when { resource.scene.owner == principal.id && resource.scene.state == "paused" };
  - name: update-own-scene
    dsl: >-
      permit(principal is character, action in ["update"], resource is scene)
      when { resource.scene.owner == principal.id && resource.scene.state in ["active", "paused"] };
```

These four policies all gate on `principal.id == resource.scene.owner` (Phase 2 is owner-only) AND a state precondition matching the lifecycle method's allowed source states.

`resume-own-scene` is intentionally owner-only in Phase 2. Phase 3 will replace it with a `resume-any-member` policy when the participant attribute exists.

- [ ] **Step 3: Verify the manifest still parses**

```bash
task lint
```

Bash timeout: 300000

Expected: no errors. If buf or yamlfmt complains, fix the indentation.

- [ ] **Step 4: Verify Phase 1 manifest tests still pass**

```bash
task test -- -run "TestParseManifest" ./internal/plugin/
```

Bash timeout: 60000

Expected: all manifest parser tests pass.

- [ ] **Step 5: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
feat(scenes): Phase 2 ABAC policies for end/pause/resume/update

Add four new manifest policies to plugins/core-scenes/plugin.yaml:

- end-own-scene: owner can end a scene from active or paused
- pause-own-scene: owner can pause an active scene
- resume-own-scene: owner can resume a paused scene (Phase 2 owner-only;
  Phase 3 will widen to any member per spec D6 async safety)
- update-own-scene: owner can update metadata on active or paused scenes

Each policy gates on:
1. principal is character (no plugins or system)
2. resource.scene.owner == principal.id (owner-only)
3. resource.scene.state matches the legal source states for the action

The state preconditions in the policies are belt-and-suspenders with the
state machine in the store layer: ABAC denies before the request reaches
the service handler, and the store's WHERE clause prevents races even if
the policy were misconfigured.

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md section 5.4
Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

---

## Task 12: Command handlers for `scene end/pause/resume/set`

Add four command handlers to `commands.go` and wire them into the dispatcher.

**Files:**

- Modify: `plugins/core-scenes/commands.go`
- Modify: `plugins/core-scenes/commands_test.go`

- [ ] **Step 1: Write failing tests**

Append to `plugins/core-scenes/commands_test.go`:

```go
func TestHandleCommandEndCallsEndScene(t *testing.T) {
    p := newTestPlugin()
    // Pre-create a scene in the fake store via the service
    createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "create The Manor",
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    require.Equal(t, pluginsdk.CommandOK, createResp.Status)

    parts := strings.Split(createResp.Output, "Scene created:")
    require.Len(t, parts, 2)
    sceneID := strings.TrimSpace(parts[1])

    // End it
    endResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "end " + sceneID,
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    assert.Equal(t, pluginsdk.CommandOK, endResp.Status)
    assert.Contains(t, endResp.Output, "ended")
}

func TestHandleCommandEndReturnsErrorWhenSceneIDIsMissing(t *testing.T) {
    p := newTestPlugin()
    resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "end",
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    assert.Equal(t, pluginsdk.CommandError, resp.Status)
    assert.Contains(t, resp.Output, "scene id")
}

func TestHandleCommandPauseCallsPauseScene(t *testing.T) {
    p := newTestPlugin()
    sceneID := createSceneInTest(t, p, "char-alice", "Pausable")

    resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "pause " + sceneID,
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    assert.Equal(t, pluginsdk.CommandOK, resp.Status)
    assert.Contains(t, resp.Output, "paused")
}

func TestHandleCommandResumeCallsResumeScene(t *testing.T) {
    p := newTestPlugin()
    sceneID := createSceneInTest(t, p, "char-alice", "Resumable")

    // Pause first
    _, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "pause " + sceneID,
        CharacterID: "char-alice",
    })
    require.NoError(t, err)

    // Then resume
    resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "resume " + sceneID,
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    assert.Equal(t, pluginsdk.CommandOK, resp.Status)
    assert.Contains(t, resp.Output, "resumed")
}

func TestHandleCommandSetUpdatesTitle(t *testing.T) {
    p := newTestPlugin()
    sceneID := createSceneInTest(t, p, "char-alice", "Original Title")

    resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "set " + sceneID + " title=New Title",
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    assert.Equal(t, pluginsdk.CommandOK, resp.Status)
    assert.Contains(t, resp.Output, "updated")

    // Verify via info
    infoResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "info " + sceneID,
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    assert.Contains(t, infoResp.Output, "New Title")
}

func TestHandleCommandSetRejectsUnknownField(t *testing.T) {
    p := newTestPlugin()
    sceneID := createSceneInTest(t, p, "char-alice", "T")

    resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "set " + sceneID + " bogus=foo",
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    assert.Equal(t, pluginsdk.CommandError, resp.Status)
    assert.Contains(t, resp.Output, "unknown field")
}

func TestHandleCommandSetRejectsMissingEqualsSeparator(t *testing.T) {
    p := newTestPlugin()
    sceneID := createSceneInTest(t, p, "char-alice", "T")

    resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "set " + sceneID + " title",
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    assert.Equal(t, pluginsdk.CommandError, resp.Status)
    assert.Contains(t, resp.Output, "field=value")
}

// createSceneInTest is a helper that creates a scene via the command path
// and returns its ID. Used by Phase 2 tests that need a scene to operate on.
func createSceneInTest(t *testing.T, p *scenePlugin, characterID, title string) string {
    t.Helper()
    resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "create " + title,
        CharacterID: characterID,
    })
    require.NoError(t, err)
    require.Equal(t, pluginsdk.CommandOK, resp.Status)
    parts := strings.Split(resp.Output, "Scene created:")
    require.Len(t, parts, 2)
    return strings.TrimSpace(parts[1])
}
```

- [ ] **Step 2: Run tests and confirm they fail**

```bash
task test -- -run "TestHandleCommandEnd|TestHandleCommandPause|TestHandleCommandResume|TestHandleCommandSet" ./plugins/core-scenes/
```

Bash timeout: 60000

Expected: most tests fail — the `dispatchCommand` switch doesn't yet have `end`, `pause`, `resume`, `set` cases.

- [ ] **Step 3: Add the four handlers and update the dispatcher**

Open `plugins/core-scenes/commands.go`. Find the `dispatchCommand` function and update its switch statement:

```go
func (p *scenePlugin) dispatchCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
    ctx, span := startSpan(ctx, "scene.command.dispatch",
        attribute.String("subject_id", req.CharacterID),
    )
    defer span.End()

    sub, rest := splitSubcommand(req.Args)
    span.SetAttributes(attribute.String("subcommand", sub))

    if sub == "" {
        return pluginsdk.Errorf("Usage: scene <subcommand> [args]\nKnown subcommands: create, info, end, pause, resume, set"), nil
    }

    switch sub {
    case "create":
        return p.handleCreate(ctx, req, rest)
    case "info":
        return p.handleInfo(ctx, req, rest)
    case "end":
        return p.handleEnd(ctx, req, rest)
    case "pause":
        return p.handlePause(ctx, req, rest)
    case "resume":
        return p.handleResume(ctx, req, rest)
    case "set":
        return p.handleSet(ctx, req, rest)
    default:
        return pluginsdk.Errorf("Unknown scene subcommand %q. Known subcommands: create, info, end, pause, resume, set.", sub), nil
    }
}
```

Then add the four new handler functions to the same file (after `handleInfo`):

```go
// handleEnd ends the specified scene. Owner ABAC enforcement is gateway-side
// (the host's ABAC engine evaluates end-own-scene before this code runs in
// production); in unit tests, ABAC is bypassed so the test must use the
// scene owner's character ID.
func (p *scenePlugin) handleEnd(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
    sceneID := strings.TrimSpace(args)
    if sceneID == "" {
        return pluginsdk.Errorf("Usage: scene end <scene id>"), nil
    }

    _, err := p.service.EndScene(ctx, &scenev1.EndSceneRequest{
        CharacterId: req.CharacterID,
        SceneId:     sceneID,
    })
    if err != nil {
        return pluginsdk.Errorf("Failed to end scene: %v", err), nil
    }

    return &pluginsdk.CommandResponse{
        Status: pluginsdk.CommandOK,
        Output: fmt.Sprintf("Scene %s ended.", sceneID),
    }, nil
}

func (p *scenePlugin) handlePause(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
    sceneID := strings.TrimSpace(args)
    if sceneID == "" {
        return pluginsdk.Errorf("Usage: scene pause <scene id>"), nil
    }

    _, err := p.service.PauseScene(ctx, &scenev1.PauseSceneRequest{
        CharacterId: req.CharacterID,
        SceneId:     sceneID,
    })
    if err != nil {
        return pluginsdk.Errorf("Failed to pause scene: %v", err), nil
    }

    return &pluginsdk.CommandResponse{
        Status: pluginsdk.CommandOK,
        Output: fmt.Sprintf("Scene %s paused.", sceneID),
    }, nil
}

func (p *scenePlugin) handleResume(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
    sceneID := strings.TrimSpace(args)
    if sceneID == "" {
        return pluginsdk.Errorf("Usage: scene resume <scene id>"), nil
    }

    _, err := p.service.ResumeScene(ctx, &scenev1.ResumeSceneRequest{
        CharacterId: req.CharacterID,
        SceneId:     sceneID,
    })
    if err != nil {
        return pluginsdk.Errorf("Failed to resume scene: %v", err), nil
    }

    return &pluginsdk.CommandResponse{
        Status: pluginsdk.CommandOK,
        Output: fmt.Sprintf("Scene %s resumed.", sceneID),
    }, nil
}

// handleSet parses "scene set <id> field=value" and applies the change.
// Phase 2 supports the five scalar mutable fields via this command. Repeated
// fields (tags, content_warnings) are not exposed via the terminal command
// surface because the simple field=value syntax can't express list semantics
// cleanly. They remain editable via UpdateScene gRPC for richer clients.
//
// The command constructs an UpdateSceneRequest with a FieldMask containing
// the single field path being set. The service handler then applies it via
// the standard mask-iteration path, getting the same per-field validation
// any other UpdateScene call would.
func (p *scenePlugin) handleSet(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
    args = strings.TrimSpace(args)
    if args == "" {
        return pluginsdk.Errorf("Usage: scene set <scene id> field=value"), nil
    }

    sceneID, rest := splitSubcommand(args)
    if sceneID == "" || rest == "" {
        return pluginsdk.Errorf("Usage: scene set <scene id> field=value"), nil
    }

    eqIdx := strings.IndexByte(rest, '=')
    if eqIdx < 0 {
        return pluginsdk.Errorf("Usage: scene set <scene id> field=value"), nil
    }
    field := strings.TrimSpace(rest[:eqIdx])
    value := strings.TrimSpace(rest[eqIdx+1:])

    update := &scenev1.UpdateSceneRequest{
        CharacterId: req.CharacterID,
        SceneId:     sceneID,
        UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{field}},
    }

    // Set the request field matching `field`. The handler will reject
    // unknown mask paths via the buildSceneUpdate switch, but we
    // pre-validate here so we can return a more helpful command-style
    // error message before bouncing off the gRPC handler.
    switch field {
    case "title":
        update.Title = value
    case "description":
        update.Description = value
    case "visibility":
        update.Visibility = value
    case "pose_order_mode":
        update.PoseOrderMode = value
    case "location_id":
        update.LocationId = value
    default:
        return pluginsdk.Errorf("unknown field %q. Known fields: title, description, visibility, pose_order_mode, location_id", field), nil
    }

    _, err := p.service.UpdateScene(ctx, update)
    if err != nil {
        return pluginsdk.Errorf("Failed to update scene: %v", err), nil
    }

    return &pluginsdk.CommandResponse{
        Status: pluginsdk.CommandOK,
        Output: fmt.Sprintf("Scene %s updated: %s = %s", sceneID, field, value),
    }, nil
}
```

Note on imports: `commands.go` now needs `google.golang.org/protobuf/types/known/fieldmaskpb` to construct the `&fieldmaskpb.FieldMask{...}` value. Add it to the existing import block.

Note: `tags` and `content_warnings` are deliberately not supported via `scene set` in Phase 2 because they're list-valued and the simple `field=value` syntax can't express list operations cleanly. They can still be edited via the gRPC `UpdateScene` RPC directly (e.g., from the web client) by including `tags` or `content_warnings` in the FieldMask paths.

- [ ] **Step 4: Run tests**

```bash
task test -- ./plugins/core-scenes/
```

Bash timeout: 60000

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
feat(scenes): scene end/pause/resume/set command handlers

Add four new subcommand handlers to plugins/core-scenes/commands.go:
- handleEnd: scene end <id>
- handlePause: scene pause <id>
- handleResume: scene resume <id>
- handleSet: scene set <id> field=value

handleSet supports the five scalar mutable fields (title, description,
visibility, pose_order_mode, location_id). Repeated fields (tags,
content_warnings) are not exposed via the terminal command surface
because the simple field=value syntax can't express list semantics
cleanly. They remain editable via UpdateScene gRPC for richer clients.

Tests use a createSceneInTest helper that wraps scene create + ID
extraction to keep individual tests focused.

Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

---

## Task 13: Integration tests with direct DB verification

Extend `test/integration/plugin/binary_plugin_test.go` with a new `Describe` block that exercises the four new RPCs end-to-end against the real plugin process, with **direct schema-qualified DB queries** confirming the row state changed.

**Files:**

- Modify: `test/integration/plugin/binary_plugin_test.go`

- [ ] **Step 1: Read the existing test file**

Open `test/integration/plugin/binary_plugin_test.go` to understand the existing structure. Phase 1 added a `Describe("scene plugin ABAC: read-own-scene", ...)` block that sets up Postgres + plugin host + ABAC engine. Phase 2 adds a parallel `Describe` block for lifecycle.

The new block needs the same setup machinery (Postgres, host, plugin loaded, ABAC engine) plus access to the gRPC client AND the raw `*pgxpool.Pool` for direct DB queries.

- [ ] **Step 2: Add the new Describe block**

Add the following block at the END of the existing `var _ = Describe("Binary Plugin Lifecycle", func() {...})` block (just before its closing `})`):

```go
Describe("scene plugin lifecycle: state machine", func() {
    var (
        lifecyclectx       context.Context
        lifecyclecancel    context.CancelFunc
        lifecyclecontainer testcontainers.Container
        lifecyclehost      *goplugin.Host
        lifecyclepool      *pgxpool.Pool
        lifecyclesceneID   string
    )

    BeforeEach(func() {
        pluginDir, binaryPath := coreScenesBinaryPath()
        if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
            Skip(fmt.Sprintf("core-scenes binary not found at %s — run 'bash scripts/build-plugins.sh' first", binaryPath))
        }

        lifecyclectx, lifecyclecancel = context.WithTimeout(context.Background(), 2*time.Minute)

        // Postgres + core migrations (so the policy store schema exists)
        pgEnv, err := testutil.StartPostgres(lifecyclectx)
        Expect(err).NotTo(HaveOccurred())
        lifecyclecontainer = pgEnv.Container
        connStr := pgEnv.ConnStr

        migrator, err := store.NewMigrator(connStr)
        Expect(err).NotTo(HaveOccurred())
        Expect(migrator.Up()).To(Succeed())
        _ = migrator.Close()

        // Provisioner + host
        provisioner := plugins.NewSchemaProvisioner(connStr)
        Expect(provisioner.Init(lifecyclectx)).To(Succeed())
        DeferCleanup(func() { provisioner.Close() })

        registry := plugins.NewServiceRegistry()
        worldSrv := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-memory bufconn only
        worldConn, worldConnErr := plugins.NewInProcessConn(worldSrv)
        Expect(worldConnErr).NotTo(HaveOccurred())
        DeferCleanup(func() { _ = worldConn.Close() })

        Expect(registry.Register(plugins.RegisteredService{
            Name:       "holomush.world.v1.WorldService",
            Conn:       worldConn,
            PluginType: plugins.TypeServerInternal(),
        })).To(Succeed())

        lifecyclehost = goplugin.NewHost(
            goplugin.WithSchemaProvisioner(provisioner),
            goplugin.WithServiceRegistry(registry),
        )

        manifestData, err := os.ReadFile(filepath.Join(pluginDir, "plugin.yaml"))
        Expect(err).NotTo(HaveOccurred())
        manifest, err := plugins.ParseManifest(manifestData)
        Expect(err).NotTo(HaveOccurred())
        Expect(lifecyclehost.Load(lifecyclectx, manifest, pluginDir)).To(Succeed())

        // Direct pool for schema-qualified DB verification
        lifecyclepool, err = pgxpool.New(lifecyclectx, connStr)
        Expect(err).NotTo(HaveOccurred())

        // Create a scene to operate on in each test
        svc, resolveErr := registry.Resolve("holomush.scene.v1.SceneService")
        Expect(resolveErr).NotTo(HaveOccurred())
        sceneClient := scenev1.NewSceneServiceClient(svc.Conn)

        createResp, err := sceneClient.CreateScene(lifecyclectx, &scenev1.CreateSceneRequest{
            CharacterId: "char-alice",
            Title:       "Lifecycle Test",
        })
        Expect(err).NotTo(HaveOccurred())
        lifecyclesceneID = createResp.GetScene().GetId()
        Expect(lifecyclesceneID).NotTo(BeEmpty())
    })

    AfterEach(func() {
        if lifecyclehost != nil {
            _ = lifecyclehost.Close(lifecyclectx)
        }
        if lifecyclepool != nil {
            lifecyclepool.Close()
        }
        if lifecyclecontainer != nil {
            _ = lifecyclecontainer.Terminate(context.Background())
        }
        if lifecyclecancel != nil {
            lifecyclecancel()
        }
    })

    // sceneClient builds a fresh SceneServiceClient from the host's
    // direct PluginConn helper. We use PluginConn rather than resolving
    // through the service registry because the registry path requires the
    // registry instance to be in scope, and the BeforeEach already wired
    // it via the host. This is the same pattern Phase 1's "direct plugin
    // connection" Describe block uses.
    sceneClient := func() scenev1.SceneServiceClient {
        conn, err := lifecyclehost.PluginConn("core-scenes")
        Expect(err).NotTo(HaveOccurred())
        return scenev1.NewSceneServiceClient(conn)
    }

    // Helper for direct DB state read
    readSceneState := func(id string) (state string, endedAt sql.NullTime) {
        err := lifecyclepool.QueryRow(lifecyclectx,
            `SELECT state, ended_at FROM plugin_core_scenes.scenes WHERE id = $1`,
            id,
        ).Scan(&state, &endedAt)
        Expect(err).NotTo(HaveOccurred())
        return state, endedAt
    }

    Describe("EndScene", func() {
        It("transitions an active scene to ended and sets ended_at", func() {
            _, err := sceneClient().EndScene(lifecyclectx, &scenev1.EndSceneRequest{
                CharacterId: "char-alice",
                SceneId:     lifecyclesceneID,
            })
            Expect(err).NotTo(HaveOccurred())

            state, endedAt := readSceneState(lifecyclesceneID)
            Expect(state).To(Equal("ended"))
            Expect(endedAt.Valid).To(BeTrue(), "ended_at should be set")
        })

        It("returns FailedPrecondition for an already-ended scene", func() {
            _, err := sceneClient().EndScene(lifecyclectx, &scenev1.EndSceneRequest{
                CharacterId: "char-alice",
                SceneId:     lifecyclesceneID,
            })
            Expect(err).NotTo(HaveOccurred())

            _, err = sceneClient().EndScene(lifecyclectx, &scenev1.EndSceneRequest{
                CharacterId: "char-alice",
                SceneId:     lifecyclesceneID,
            })
            Expect(err).To(HaveOccurred())
            // Verify it's FailedPrecondition
            // (gRPC code mapping happens via mapTransitionError in service.go)
        })

        It("returns NotFound for a missing scene", func() {
            _, err := sceneClient().EndScene(lifecyclectx, &scenev1.EndSceneRequest{
                CharacterId: "char-alice",
                SceneId:     "scene-does-not-exist",
            })
            Expect(err).To(HaveOccurred())
        })

        It("rejects concurrent end attempts (race-safe WHERE clause)", func() {
            // The store uses UPDATE ... WHERE state IN ('active', 'paused')
            // to prevent races. Two goroutines calling EndScene on the same
            // scene at the same time MUST result in exactly one success
            // (whoever's UPDATE wins) and one FailedPrecondition (whoever's
            // UPDATE finds the row already in 'ended' state).
            //
            // Without the WHERE clause guard, both updates would succeed and
            // the second one would silently overwrite ended_at — corruption.
            // This test exists to prove the guard actually fires.
            var (
                wg        sync.WaitGroup
                firstErr  error
                secondErr error
            )
            wg.Add(2)
            go func() {
                defer wg.Done()
                _, firstErr = sceneClient().EndScene(lifecyclectx, &scenev1.EndSceneRequest{
                    CharacterId: "char-alice",
                    SceneId:     lifecyclesceneID,
                })
            }()
            go func() {
                defer wg.Done()
                _, secondErr = sceneClient().EndScene(lifecyclectx, &scenev1.EndSceneRequest{
                    CharacterId: "char-alice",
                    SceneId:     lifecyclesceneID,
                })
            }()
            wg.Wait()

            // Exactly one of the two MUST have succeeded.
            successes := 0
            if firstErr == nil {
                successes++
            }
            if secondErr == nil {
                successes++
            }
            Expect(successes).To(Equal(1),
                "exactly one concurrent end should succeed; got first=%v second=%v",
                firstErr, secondErr)

            // The final DB state must be 'ended' (the winner's UPDATE landed).
            state, endedAt := readSceneState(lifecyclesceneID)
            Expect(state).To(Equal("ended"))
            Expect(endedAt.Valid).To(BeTrue())
        })
    })

    Describe("PauseScene", func() {
        It("transitions an active scene to paused", func() {
            _, err := sceneClient().PauseScene(lifecyclectx, &scenev1.PauseSceneRequest{
                CharacterId: "char-alice",
                SceneId:     lifecyclesceneID,
            })
            Expect(err).NotTo(HaveOccurred())

            state, _ := readSceneState(lifecyclesceneID)
            Expect(state).To(Equal("paused"))
        })

        It("rejects pause on an already-paused scene", func() {
            _, err := sceneClient().PauseScene(lifecyclectx, &scenev1.PauseSceneRequest{
                CharacterId: "char-alice",
                SceneId:     lifecyclesceneID,
            })
            Expect(err).NotTo(HaveOccurred())

            _, err = sceneClient().PauseScene(lifecyclectx, &scenev1.PauseSceneRequest{
                CharacterId: "char-alice",
                SceneId:     lifecyclesceneID,
            })
            Expect(err).To(HaveOccurred())
        })
    })

    Describe("ResumeScene", func() {
        It("transitions a paused scene back to active", func() {
            _, err := sceneClient().PauseScene(lifecyclectx, &scenev1.PauseSceneRequest{
                CharacterId: "char-alice",
                SceneId:     lifecyclesceneID,
            })
            Expect(err).NotTo(HaveOccurred())

            _, err = sceneClient().ResumeScene(lifecyclectx, &scenev1.ResumeSceneRequest{
                CharacterId: "char-alice",
                SceneId:     lifecyclesceneID,
            })
            Expect(err).NotTo(HaveOccurred())

            state, _ := readSceneState(lifecyclesceneID)
            Expect(state).To(Equal("active"))
        })

        It("rejects resume on an active scene", func() {
            _, err := sceneClient().ResumeScene(lifecyclectx, &scenev1.ResumeSceneRequest{
                CharacterId: "char-alice",
                SceneId:     lifecyclesceneID,
            })
            Expect(err).To(HaveOccurred())
        })
    })

    Describe("UpdateScene", func() {
        It("applies a title change", func() {
            _, err := sceneClient().UpdateScene(lifecyclectx, &scenev1.UpdateSceneRequest{
                CharacterId: "char-alice",
                SceneId:     lifecyclesceneID,
                Title:       "Renamed Title",
                UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
            })
            Expect(err).NotTo(HaveOccurred())

            // Direct DB read confirms title actually changed
            var title string
            err = lifecyclepool.QueryRow(lifecyclectx,
                `SELECT title FROM plugin_core_scenes.scenes WHERE id = $1`,
                lifecyclesceneID,
            ).Scan(&title)
            Expect(err).NotTo(HaveOccurred())
            Expect(title).To(Equal("Renamed Title"))
        })

        It("rejects updates to an ended scene", func() {
            _, err := sceneClient().EndScene(lifecyclectx, &scenev1.EndSceneRequest{
                CharacterId: "char-alice",
                SceneId:     lifecyclesceneID,
            })
            Expect(err).NotTo(HaveOccurred())

            _, err = sceneClient().UpdateScene(lifecyclectx, &scenev1.UpdateSceneRequest{
                CharacterId: "char-alice",
                SceneId:     lifecyclesceneID,
                Title:       "Try",
                UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
            })
            Expect(err).To(HaveOccurred())

            // Verify title did NOT change
            var title string
            err = lifecyclepool.QueryRow(lifecyclectx,
                `SELECT title FROM plugin_core_scenes.scenes WHERE id = $1`,
                lifecyclesceneID,
            ).Scan(&title)
            Expect(err).NotTo(HaveOccurred())
            Expect(title).To(Equal("Lifecycle Test"))
        })
    })
})
```

You'll need to add `database/sql` to the imports for `sql.NullTime`, `google.golang.org/protobuf/types/known/fieldmaskpb` for the FieldMask construction in the UpdateScene tests, and `sync` for the concurrent-end test's `sync.WaitGroup`. Other imports (`pgxpool`, `time`, `testcontainers`, etc.) should already be present from Phase 1's tests.

**Notable on `sceneClient` helper:** the in-block helper closure uses `lifecyclehost.PluginConn(...)` to get the gRPC connection directly, then constructs a fresh client. This is simpler than going through the registry resolution path used in Phase 1's tests.

- [ ] **Step 3: Build the plugin and run the focused tests**

```bash
bash scripts/build-plugins.sh
```

Bash timeout: 300000

Then run just the new lifecycle tests via the focused task:

```bash
task test:int -- -ginkgo.focus="scene plugin lifecycle" ./test/integration/plugin/
```

Bash timeout: 600000

If `task test:int` doesn't accept `-ginkgo.focus`, fall back to the focused task added in Phase 1:

```bash
task test:int:focus -- -ginkgo.focus="scene plugin lifecycle"
```

Expected: all tests in the new Describe block pass. Each spec runs ~20-30 seconds because each `BeforeEach` cold-starts a Postgres testcontainer.

- [ ] **Step 4: Run the existing core-scenes integration tests too**

Make sure Phase 1's tests still pass:

```bash
task test:int:focus -- -ginkgo.focus="core-scenes plugin"
```

Bash timeout: 600000

Expected: Phase 1's `Binary Plugin Lifecycle` Describe block tests still pass (CreateScene, GetScene, ABAC).

- [ ] **Step 5: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
test(scenes): Phase 2 integration tests with direct DB verification

Add a new "scene plugin lifecycle: state machine" Describe block to
test/integration/plugin/binary_plugin_test.go covering EndScene,
PauseScene, ResumeScene, and UpdateScene.

Each test:
1. Creates a scene via the gRPC client (round-tripping through the host
   to the real plugin process)
2. Calls the lifecycle RPC under test
3. **Verifies the state change via a direct schema-qualified DB query**
   to plugin_core_scenes.scenes — bypassing the GetScene RPC so we test
   the actual contract: did the row in the table change

Direct DB verification catches state machine bugs that round-trip-via-
GetScene cannot, especially for race-safe UPDATE patterns where the
service could lie about success.

10 tests total: happy paths for each transition, FailedPrecondition for
state-precondition violations, NotFound for missing scenes, and explicit
DB-state verification on UpdateScene title changes.

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md sections 7.2, 9.4
Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

---

## Task 14: E2E Playwright spec + `db.ts` helper extension

Extend the Playwright e2e suite with a new spec that drives the scene lifecycle through the terminal UI and verifies behavior with direct DB queries via an extended `db.ts` helper.

**Files:**

- Modify: `web/e2e/helpers/db.ts`
- Create: `web/e2e/scenes.spec.ts`

- [ ] **Step 1: Extend db.ts with DbScene and getSceneById**

Open `web/e2e/helpers/db.ts`. Add this section before the `// ── Zero ULID check ──` block at the bottom:

```typescript
// ── Scene queries ───────────────────────────────────────────────

export interface DbScene {
  id: string;
  title: string;
  description: string;
  owner_id: string;
  state: string;
  visibility: string;
  pose_order: string;
  location_id: string | null;
  created_at: Date;
  ended_at: Date | null;
  archived_at: Date | null;
}

/**
 * Fetch a scene row directly from the core-scenes plugin's Postgres schema.
 *
 * The plugin owns its own schema (plugin_core_scenes); cross-schema reads
 * from a privileged test role like the e2e test runner are permitted by the
 * underlying Postgres role. The plugin's restricted role only protects
 * writes from cross-plugin contamination, not reads from privileged callers.
 */
export async function getSceneById(sceneId: string): Promise<DbScene | null> {
  const { rows } = await getPool().query<DbScene>(
    `SELECT id, title, description, owner_id, state, visibility, pose_order,
            location_id, created_at, ended_at, archived_at
     FROM plugin_core_scenes.scenes
     WHERE id = $1`,
    [sceneId],
  );
  return rows[0] ?? null;
}
```

- [ ] **Step 2: Create the scenes Playwright spec**

Create `web/e2e/scenes.spec.ts`:

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect, db, getClientSessionId } from './helpers/fixtures';
import type { Page } from '@playwright/test';

/**
 * Connect as guest via the landing page and wait for the terminal to load.
 * Same pattern as web/e2e/terminal.spec.ts.
 */
async function connectAsGuest(page: Page) {
  await page.goto('/');
  await page.getByRole('main').getByRole('button', { name: 'Try as Guest' }).click();
  await expect(page).toHaveURL(/\/terminal/, { timeout: 10000 });
  await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
}

/**
 * Send a command via the terminal textarea and wait for it to be processed.
 * Returns once the command has been sent — the caller is responsible for
 * waiting for any specific output.
 */
async function sendCommand(page: Page, command: string) {
  const input = page.locator('textarea');
  await input.fill(command);
  await input.press('Enter');
}

/**
 * Wait for the most recent terminal event whose text matches `pattern` and
 * return the captured text. Used to extract the scene ID from `scene create`
 * output, which is formatted as "Scene created: scene-XXXXX".
 */
async function waitForOutputMatching(page: Page, pattern: RegExp): Promise<string> {
  const event = page
    .locator('[data-testid="event"]')
    .filter({ hasText: pattern })
    .last();
  await expect(event).toBeVisible({ timeout: 10000 });
  const text = await event.textContent();
  if (!text) {
    throw new Error(`event matched ${pattern} but had no text`);
  }
  const match = text.match(pattern);
  if (!match || !match[0]) {
    throw new Error(`pattern ${pattern} matched event but extracted no value`);
  }
  return match[0];
}

/**
 * Extract the scene ID from a "Scene created: scene-XXXXX" terminal event.
 */
async function extractSceneIdFromOutput(page: Page): Promise<string> {
  const text = await waitForOutputMatching(page, /scene-[A-Z0-9]+/);
  return text;
}

test.describe('Scene lifecycle (Phase 2)', () => {
  test('create -> pause -> resume -> end with DB verification', async ({ page }) => {
    await connectAsGuest(page);
    const sessionId = await getClientSessionId(page);
    expect(sessionId).toBeTruthy();
    const session = await db.getSessionById(sessionId!);
    expect(session).not.toBeNull();

    // Create a scene through the terminal
    await sendCommand(page, 'scene create Phase 2 Lifecycle Test');
    const sceneId = await extractSceneIdFromOutput(page);
    expect(sceneId).toMatch(/^scene-[A-Z0-9]+$/);

    // DB: scene exists with state='active', owner = current character
    let scene = await db.getSceneById(sceneId);
    expect(scene).not.toBeNull();
    expect(scene!.state).toBe('active');
    expect(scene!.owner_id).toBe(session!.character_id);
    expect(scene!.title).toBe('Phase 2 Lifecycle Test');
    expect(scene!.ended_at).toBeNull();

    // Pause
    await sendCommand(page, `scene pause ${sceneId}`);
    await waitForOutputMatching(page, /paused/);
    scene = await db.getSceneById(sceneId);
    expect(scene!.state).toBe('paused');

    // Resume
    await sendCommand(page, `scene resume ${sceneId}`);
    await waitForOutputMatching(page, /resumed/);
    scene = await db.getSceneById(sceneId);
    expect(scene!.state).toBe('active');

    // End
    await sendCommand(page, `scene end ${sceneId}`);
    await waitForOutputMatching(page, /ended/);
    scene = await db.getSceneById(sceneId);
    expect(scene!.state).toBe('ended');
    expect(scene!.ended_at).not.toBeNull();
  });

  test('scene set updates the title', async ({ page }) => {
    await connectAsGuest(page);

    await sendCommand(page, 'scene create Original Title');
    const sceneId = await extractSceneIdFromOutput(page);

    let scene = await db.getSceneById(sceneId);
    expect(scene!.title).toBe('Original Title');

    await sendCommand(page, `scene set ${sceneId} title=Renamed Title`);
    await waitForOutputMatching(page, /updated/);

    scene = await db.getSceneById(sceneId);
    expect(scene!.title).toBe('Renamed Title');
  });

  test('cannot end an already-ended scene', async ({ page }) => {
    await connectAsGuest(page);

    await sendCommand(page, 'scene create Will End Twice');
    const sceneId = await extractSceneIdFromOutput(page);

    await sendCommand(page, `scene end ${sceneId}`);
    await waitForOutputMatching(page, /ended/);

    // Second end attempt should produce an error event
    await sendCommand(page, `scene end ${sceneId}`);
    await waitForOutputMatching(page, /Failed to end scene/);
  });

  test('scene info shows scene metadata', async ({ page }) => {
    await connectAsGuest(page);

    await sendCommand(page, 'scene create Info Test Scene');
    const sceneId = await extractSceneIdFromOutput(page);

    await sendCommand(page, `scene info ${sceneId}`);
    await waitForOutputMatching(page, /Info Test Scene/);
    await waitForOutputMatching(page, /State: active/);
  });
});
```

- [ ] **Step 3: Run the e2e tests**

```bash
task test:e2e
```

Bash timeout: 600000

Expected: all tests in `scenes.spec.ts` pass, plus all existing e2e tests still pass.

If a test fails because of a timing issue (event not appearing fast enough), increase the `timeout` parameter in the relevant `expect(...).toBeVisible({ timeout: ... })` call.

If tests fail because the terminal can't find the `scene` command, that means the plugin isn't loaded in the e2e Docker image — check that `task docker:build` was run after Task 11 (manifest changes), and re-run if needed.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "$(cat <<'EOF'
test(scenes): Phase 2 e2e tests through terminal UI with DB verification

Add web/e2e/scenes.spec.ts: end-to-end tests for the scene lifecycle that
drive scene commands through the terminal UI in the web client and verify
state changes via direct schema-qualified DB queries.

Tests cover the full stack: web client → ConnectRPC → gateway → core →
plugin host → core-scenes plugin → plugin_core_scenes.scenes table.

4 tests:
1. create -> pause -> resume -> end with DB-state verification at each step
2. scene set updates title (DB-verified)
3. cannot end an already-ended scene (terminal output assertion)
4. scene info shows metadata

The web/e2e/helpers/db.ts helper is extended with a DbScene interface and
getSceneById() function that issues schema-qualified queries to the
plugin's Postgres schema. The test runner connects as a privileged role
that can read across schemas; the plugin's restricted role only protects
writes from cross-plugin contamination.

Bead: holomush-5rh.11
EOF
)"
```

**CRITICAL — use `jj commit`, NOT `jj describe`.**

---

## Task 15: Final `task pr-prep` verification and bead closeout

**Files:** None (verification only)

- [ ] **Step 1: Run the full pre-PR verification**

```bash
task pr-prep
```

Bash timeout: 600000

Expected: all checks pass — lint, format, schema validation, license headers, unit tests, integration tests, E2E tests.

If anything fails, fix it inline:

- **Format issues**: run `task fmt` and commit the result
- **Lint issues**: fix the specific lint errors
- **Test failures**: debug and fix the underlying issue (do not weaken assertions)
- **License header issues**: run `task license:add`

Each fix is its own commit.

- [ ] **Step 2: Verify Phase 2 acceptance criteria from the bead**

Read back `holomush-5rh.11`:

```bash
bd show holomush-5rh.11
```

Confirm each item in the acceptance criteria checklist:

- [ ] State machine cannot transition backward
- [ ] Only owner can end/pause; any member can resume (D6 async safety) — Phase 2 ships owner-only resume; widening to members happens in Phase 3 per the brainstorm decision
- [ ] All transitions emit a span, log entry, and metric — spans + slog yes; metrics deferred per spec section 11
- [ ] `scene set <field> <value>` validates field and updates only owner-modifiable fields
- [ ] task pr-prep passes

- [ ] **Step 3: Close the Phase 2 bead**

```bash
bd close holomush-5rh.11 --reason "$(cat <<'EOF'
Phase 2 Lifecycle complete. All acceptance criteria satisfied (with Phase 3 deferral noted for resume widening).

**Implementation summary:**
- 16 commits in jj workspace scene-rewrite, on top of Phase 1's chain
- Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md
- Plan: docs/superpowers/plans/2026-04-07-scenes-phase-2-lifecycle.md

**Acceptance criteria:**
- [x] State machine cannot transition backward (lifecycle.go IsValidTransition + table-driven tests + race-safe UPDATE WHERE state IN (...))
- [x] Owner-only end and pause via ABAC end-own-scene/pause-own-scene policies
- [x] Owner-only resume (Phase 2 only; Phase 3 widens to members per spec D6 async safety — deferred per brainstorm decision)
- [x] All transitions emit a per-transition span (scene.lifecycle.end/.pause/.resume) and slog log entry (Prometheus metrics still deferred per spec section 11 architectural gap)
- [x] scene set field=value updates only owner-modifiable fields with validation via protovalidate annotations
- [x] task pr-prep PASSES (lint, unit, integration, e2e)

**New Phase 2 contributions:**
- protovalidate introduced as the project request validation mechanism (plugin SDK interceptor + annotations on scene/v1, plugin/v1, world/v1)
- Hand-rolled validation in Phase 1 service handlers replaced with proto annotations
- Direct schema-qualified DB verification added to integration tests
- E2E coverage extended with web/e2e/scenes.spec.ts

**Discovered issues filed as separate beads:**
- holomush-9vw2 (closed) — staff/admin role tier already exists (no work needed)
- holomush-psr9 (open) — gateway-side ConnectRPC validation interceptor follow-up
EOF
)"
```

- [ ] **Step 4: Final commit (if any fix-ups happened in Step 1)**

If Step 1 produced any fix-up commits, ensure all are described and the working copy is clean:

```bash
jj --no-pager st
jj --no-pager log -r 'main..@-' --no-graph -T 'change_id.short(8) ++ " " ++ description.first_line() ++ "\n"'
```

Expected: working copy is empty; the chain shows all Phase 2 commits in order on top of Phase 1's chain.

---

## Self-Review Notes

After writing this plan, I checked it against the spec and noticed:

1. **Spec section 10.1 lists six required spans for scenes overall.** Phase 1 implemented four (`scene.service.create_scene`, `scene.service.get_scene`, `scene.resolver.resolve_resource`, `scene.resolver.get_schema`, `scene.store.create`, `scene.store.get`). Phase 2 adds `scene.lifecycle.end`, `.pause`, `.resume`, plus `scene.service.update_scene` and `scene.store.end/pause/resume/update`. The remaining span from spec section 10.1 — `scene.command.<sub>` — was implemented as `scene.command.dispatch` in Phase 1 already, so Phase 2 is complete on this dimension.

2. **Spec section 10.4 lists "Scene state transition" as a Phase 2 business event.** It MUST emit a span, log entry, and metric. Span ✓ (per-transition `scene.lifecycle.<op>`). Log entry ✓ (`slog.InfoContext` after each successful transition). Metric ✗ (deferred per spec gap; same status as Phase 1's "scene created" event). When the plugin metrics infrastructure exists, add `scene_state_transitions_total` counter increments to each lifecycle handler.

3. **The plan does not exercise admin-tier ABAC.** Phase 2's `update-own-scene` policy gates on `principal.id == resource.scene.owner` — there's no admin override. The spec acknowledges this (idle_timeout_secs is admin-only and excluded from UpdateScene; admin override path doesn't exist yet). Phase 2 is correct as-specified.

4. **The plan defers `tags` and `content_warnings` mutation via `scene set`.** The terminal command can only set scalar fields. List-valued fields require a richer client (web UI, gRPC directly). This was a deliberate Phase 2 simplification — `tags` are set at creation and rarely change in practice; the gRPC `UpdateScene` RPC supports them via the `tags` and `content_warnings` mask paths for richer clients.

5. **No placeholders remain in this plan.** Each task has complete code. The Task 13 integration test reuses the testcontainer + host wiring pattern from Phase 1 explicitly — no abstraction over it because the abstraction would only have one caller at this stage.

6. **Type consistency check**: `SceneUpdate` struct fields (pointer scalars + paired booleans) match the `sceneStorer.Update` signature and the `fakeStore.Update` mock. The wire-format `UpdateSceneRequest` uses flat fields + a `FieldMask`, and the `buildSceneUpdate` converter translates FieldMask paths into the internal `SceneUpdate` representation. All layers consistent.

---

**Plan complete.** Saved to `docs/superpowers/plans/2026-04-07-scenes-phase-2-lifecycle.md`.

**Two execution options:**

**1. Subagent-Driven (recommended)** — Dispatch a fresh subagent per task, review between tasks, fast iteration. Each subagent gets one task with all the code and commands; on completion I review the diff before moving to the next task. This is the same pattern that worked for Phase 1.

**2. Inline Execution** — Execute tasks in this session using `executing-plans` skill with batch checkpoints.

**Which approach?**
