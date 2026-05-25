# Plugin Host `Evaluate` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a host `Evaluate(action, resource)` RPC + Lua hostfunc so plugin command handlers can authorize a specific action against a specific resource instance through the real ABAC engine, and migrate `core-scenes` per-action authorization onto it (unblocking E2's admin-gated `scene extend`).

**Architecture:** A runtime-neutral shared evaluation core (`internal/plugin/pluginauthz`) performs entitlement + `engine.Evaluate` + audit, given a **host-trusted subject**. Two thin surfaces feed it: the binary `PluginHostService.Evaluate` gRPC (subject recovered from the dispatch token, mirroring `EmitEvent`) and the Lua `holomush.evaluate` global (subject from `core.ActorFromContext(L.Context())`). The SDK adds a `host.Evaluate` client helper and a gated subcommand dispatcher that makes the gate structural. `core-scenes` migrates its ad-hoc Go authz checks to `Evaluate` calls against policies that already exist in its `plugin.yaml`.

**Tech Stack:** Go, ConnectRPC/gRPC (`buf generate` via `task proto`), gopher-lua, the existing ABAC engine (`internal/access/policy`), `internal/audit`, Ginkgo/Gomega for E2E.

**Spec:** `docs/superpowers/specs/2026-05-25-plugin-host-evaluate-design.md`
**Design bead:** holomush-8kkv5

---

## File Structure

| Path | Responsibility |
| ---- | -------------- |
| `internal/plugin/pluginauthz/evaluate.go` (Create) | Runtime-neutral shared core: entitlement check, `engine.Evaluate`, audit emission. The single function both surfaces delegate to (INV-5). |
| `internal/plugin/pluginauthz/evaluate_test.go` (Create) | Unit tests for the shared core. |
| `api/proto/holomush/plugin/v1/plugin.proto` (Modify) | Add `Evaluate` RPC + `EvaluateRequest`/`EvaluateResponse` to `PluginHostService`. |
| `internal/plugin/goplugin/host.go` (Modify) | `WithEngine` / `WithAuditLogger` host options + fields; per-plugin owned-type lookup. |
| `internal/plugin/goplugin/host_service.go` (Modify) | Implement `pluginHostServiceServer.Evaluate` (token→actor→subject→pluginauthz). |
| `internal/plugin/hostfunc/evaluate.go` (Create) | `holomush.evaluate` Lua global (ctx→subject→pluginauthz). |
| `internal/plugin/hostfunc/functions.go` (Modify) | Register `evaluate`; add `WithAuditLogger` option. |
| `pkg/plugin/evaluate_client.go` (Create) | SDK `host.Evaluate(ctx, action, resource)` binary client helper. |
| `pkg/plugin/gated_dispatch.go` (Create) | SDK gated subcommand dispatcher. |
| `plugins/core-scenes/plugin.yaml` (Modify) | Declare `extend_publish_attempts` action; add admin policy. |
| `plugins/core-scenes/commands.go` (Modify) | Wire subcommands through the gated dispatcher; remove ad-hoc Go authz checks. |
| `site/docs/extending/plugin-host-evaluate.md` (Create) | Plugin-author guide (PR-blocking). |
| `internal/access/policy/plugin_evaluate_gate_test.go` (Create) | Real-engine integration test of the admin gate via `pluginauthz.Evaluate` (Tier 1). Full-stack Ginkgo/Gomega command-path E2E is a deferred follow-up (Tier 2, depends on iwzt-9). |

---

## Phase 1: Shared evaluation core + proto

### Task 1: `pluginauthz` shared evaluation core

**Files:**

- Create: `internal/plugin/pluginauthz/evaluate.go`
- Test: `internal/plugin/pluginauthz/evaluate_test.go`

- [ ] **Step 1: Write the failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginauthz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// stubEngine returns a fixed decision/error and records the request.
type stubEngine struct {
	decision types.Decision
	err      error
	gotReq   types.AccessRequest
	called   bool
}

func (s *stubEngine) Evaluate(_ context.Context, req types.AccessRequest) (types.Decision, error) {
	s.called = true
	s.gotReq = req
	return s.decision, s.err
}

func (s *stubEngine) CanPerformAction(context.Context, string, string, string, string) (bool, error) {
	return false, nil
}

// recordingAuditor captures audit events.
type recordingAuditor struct{ events []audit.Event }

func (r *recordingAuditor) Log(_ context.Context, e audit.Event) error {
	r.events = append(r.events, e)
	return nil
}

func TestEvaluate_AllowEmitsAuditAndReturnsDecision(t *testing.T) {
	eng := &stubEngine{decision: types.NewDecision(types.EffectAllow, "permitted by p", "p")}
	aud := &recordingAuditor{}

	dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine:     eng,
		Auditor:    aud,
		PluginName: "core-scenes",
		OwnedTypes: map[string]bool{"scene": true},
		Subject:    "character:01ABC",
		Action:     "extend_publish_attempts",
		Resource:   "scene:01SCENE",
	})

	require.NoError(t, err)
	assert.True(t, dec.Allowed)
	assert.Equal(t, "p", dec.MatchedPolicy)
	assert.Equal(t, "character:01ABC", eng.gotReq.Subject)
	assert.Equal(t, "extend_publish_attempts", eng.gotReq.Action)
	assert.Equal(t, "scene:01SCENE", eng.gotReq.Resource)
	require.Len(t, aud.events, 1)
	assert.Equal(t, "character:01ABC", aud.events[0].Subject)
	assert.Equal(t, audit.SourcePlugin, aud.events[0].Source)
	assert.Equal(t, "core-scenes", aud.events[0].Component)
	assert.Equal(t, types.EffectAllow, aud.events[0].Effect)
}

func TestEvaluate_EntitlementRejectsForeignType(t *testing.T) {
	eng := &stubEngine{decision: types.NewDecision(types.EffectAllow, "", "p")}
	aud := &recordingAuditor{}

	dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine: eng, Auditor: aud, PluginName: "core-scenes",
		OwnedTypes: map[string]bool{"scene": true},
		Subject:    "character:01ABC", Action: "read", Resource: "server:global",
	})

	require.Error(t, err)
	assert.False(t, dec.Allowed)
	assert.False(t, eng.called, "engine MUST NOT be consulted for an unentitled resource type")
}

func TestEvaluate_CommandCarveOutAllowed(t *testing.T) {
	eng := &stubEngine{decision: types.NewDecision(types.EffectAllow, "", "p")}
	dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine: eng, Auditor: &recordingAuditor{}, PluginName: "lua-plug",
		OwnedTypes: map[string]bool{}, // Lua: empty
		Subject:    "character:01ABC", Action: "execute", Resource: "command:foo",
	})
	require.NoError(t, err)
	assert.True(t, dec.Allowed)
	assert.True(t, eng.called)
}

func TestEvaluate_EmptyActionRejected(t *testing.T) {
	_, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine: &stubEngine{}, Auditor: &recordingAuditor{}, PluginName: "core-scenes",
		OwnedTypes: map[string]bool{"scene": true},
		Subject:    "character:01ABC", Action: "", Resource: "scene:01SCENE",
	})
	require.Error(t, err)
}

func TestEvaluate_MalformedResourceRejected(t *testing.T) {
	for _, res := range []string{"noseparator", ":noid", "notype:", ""} {
		_, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
			Engine: &stubEngine{}, Auditor: &recordingAuditor{}, PluginName: "core-scenes",
			OwnedTypes: map[string]bool{"scene": true},
			Subject:    "character:01ABC", Action: "read", Resource: res,
		})
		require.Errorf(t, err, "resource %q must be rejected", res)
	}
}

func TestEvaluate_EmptySubjectFailsClosed(t *testing.T) {
	eng := &stubEngine{}
	dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine: eng, Auditor: &recordingAuditor{}, PluginName: "core-scenes",
		OwnedTypes: map[string]bool{"scene": true},
		Subject:    "", Action: "read", Resource: "scene:01SCENE",
	})
	require.Error(t, err)
	assert.False(t, dec.Allowed)
	assert.False(t, eng.called, "no authenticated subject MUST fail closed before the engine")
}

func TestEvaluate_EngineErrorFailsClosed(t *testing.T) {
	eng := &stubEngine{err: assertAnErr()}
	dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine: eng, Auditor: &recordingAuditor{}, PluginName: "core-scenes",
		OwnedTypes: map[string]bool{"scene": true},
		Subject:    "character:01ABC", Action: "read", Resource: "scene:01SCENE",
	})
	require.Error(t, err)
	assert.False(t, dec.Allowed, "engine error MUST NOT fail open")
}

func TestEvaluate_DefaultDenyOnUnmatchedPolicy(t *testing.T) {
	eng := &stubEngine{decision: types.NewDecision(types.EffectDefaultDeny, "no match", "")}
	dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine: eng, Auditor: &recordingAuditor{}, PluginName: "core-scenes",
		OwnedTypes: map[string]bool{"scene": true},
		Subject:    "character:01ABC", Action: "read", Resource: "scene:01SCENE",
	})
	require.NoError(t, err)
	assert.False(t, dec.Allowed)
}

func assertAnErr() error { return context.DeadlineExceeded }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./internal/plugin/pluginauthz/`
Expected: FAIL — package `pluginauthz` does not exist.

- [ ] **Step 3: Write the implementation**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package pluginauthz holds the runtime-neutral per-action authorization
// core shared by the binary (PluginHostService.Evaluate) and Lua
// (holomush.evaluate) surfaces. Both delegate here so policy/trust
// behavior cannot diverge between runtimes (INV-5).
package pluginauthz

import (
	"context"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/errutil"
)

// ActorSubject maps a host-stamped Actor to its ABAC subject string. It is
// the single mapping shared by the binary and Lua surfaces (INV-5) so the
// two cannot derive divergent subjects. Returns "" for an unknown/zero
// actor kind, which Evaluate treats as fail-closed.
func ActorSubject(a core.Actor) string {
	switch a.Kind {
	case core.ActorCharacter:
		return "character:" + a.ID
	case core.ActorPlugin:
		return "plugin:" + a.ID
	case core.ActorSystem:
		return "system"
	default:
		return ""
	}
}

// Auditor is the minimal audit sink pluginauthz needs. *audit.Logger
// satisfies it.
type Auditor interface {
	Log(ctx context.Context, event audit.Event) error
}

// Input carries everything the shared core needs. Subject is HOST-DERIVED
// and MUST NOT originate from plugin-supplied data (INV-1).
type Input struct {
	Engine     types.AccessPolicyEngine
	Auditor    Auditor
	PluginName string
	OwnedTypes map[string]bool // plugin's resource_types; empty for Lua
	Subject    string
	Action     string
	Resource   string // "type:id"
}

// Decision is the runtime-neutral result returned to both surfaces.
type Decision struct {
	Allowed       bool
	Reason        string
	MatchedPolicy string
}

// commandResourceType is the carve-out type any plugin may evaluate for its
// own commands (see spec §3).
const commandResourceType = "command"

// Evaluate runs entitlement → engine → audit and returns a runtime-neutral
// Decision. It fails closed on every error path: a non-nil error always
// accompanies a non-allowing Decision.
func Evaluate(ctx context.Context, in Input) (Decision, error) {
	if in.Subject == "" {
		// No authenticated actor bound to the call (INV-2).
		return Decision{}, oops.Code("EVALUATE_NO_SUBJECT").
			With("plugin", in.PluginName).
			Errorf("evaluate called without an authenticated subject")
	}
	if in.Action == "" {
		return Decision{}, oops.Code("EVALUATE_EMPTY_ACTION").
			With("plugin", in.PluginName).Errorf("action must not be empty")
	}

	resType, resID, ok := splitResourceRef(in.Resource)
	if !ok {
		return Decision{}, oops.Code("EVALUATE_BAD_RESOURCE").
			With("plugin", in.PluginName).With("resource", in.Resource).
			Errorf("resource must be of the form type:id")
	}

	// Entitlement (INV-3): plugin-owned type or the command carve-out.
	if resType != commandResourceType && !in.OwnedTypes[resType] {
		return Decision{}, oops.Code("EVALUATE_UNENTITLED_TYPE").
			With("plugin", in.PluginName).With("resource_type", resType).
			Errorf("plugin may not evaluate resource type %q", resType)
	}
	_ = resID // present-and-non-empty already validated by splitResourceRef

	req, reqErr := types.NewAccessRequest(in.Subject, in.Action, in.Resource, nil)
	if reqErr != nil {
		return Decision{}, oops.With("plugin", in.PluginName).Wrap(reqErr)
	}

	dec, evalErr := in.Engine.Evaluate(ctx, req)
	if evalErr != nil {
		errutil.LogErrorContext(ctx, "plugin evaluate engine error", evalErr,
			"plugin", in.PluginName, "action", in.Action, "resource", in.Resource)
		// Fail closed: error accompanies a non-allowing decision.
		return Decision{}, oops.With("plugin", in.PluginName).Wrap(evalErr)
	}

	result := Decision{
		Allowed:       dec.IsAllowed(),
		Reason:        dec.Reason(),
		MatchedPolicy: dec.PolicyID(),
	}

	// Audit (INV-4): exactly one host-stamped event per evaluation.
	if in.Auditor != nil {
		effect := types.EffectDeny
		if dec.IsAllowed() {
			effect = types.EffectAllow
		}
		if logErr := in.Auditor.Log(ctx, audit.Event{
			Name:      "plugin.evaluate",
			Source:    audit.SourcePlugin,
			Component: in.PluginName,
			Subject:   in.Subject,
			Action:    in.Action,
			Resource:  in.Resource,
			Effect:    effect,
			Timestamp: time.Now(),
		}); logErr != nil {
			errutil.LogErrorContext(ctx, "plugin evaluate audit log failed", logErr,
				"plugin", in.PluginName, "action", in.Action)
			// Audit failure does not change the authorization outcome.
		}
	}

	return result, nil
}

// splitResourceRef parses "type:id" and requires both halves non-empty.
func splitResourceRef(ref string) (resType, resID string, ok bool) {
	i := strings.IndexByte(ref, ':')
	if i <= 0 || i >= len(ref)-1 {
		return "", "", false
	}
	return ref[:i], ref[i+1:], true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- ./internal/plugin/pluginauthz/`
Expected: PASS (all 9 tests).

- [ ] **Step 5: Lint**

Run: `task lint:go`
Expected: no findings. (If `errutil` import is unused in a variant, remove it — do not add a blanket nolint.)

- [ ] **Step 6: Commit**

`jj commit -m "feat(pluginauthz): shared per-action plugin authz core (holomush-8kkv5)"`

---

### Task 2: Add `Evaluate` to the `PluginHostService` proto

**Files:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto` (service `PluginHostService`, after `IsAnyConnFocused` at line ~98)

- [ ] **Step 1: Add the RPC and messages**

In `service PluginHostService { ... }` add:

```protobuf
  // Evaluate runs the host ABAC engine for a single action against a single
  // resource instance owned by the calling plugin. The subject is derived
  // host-side from the dispatch token (see EmitEvent) — there is no subject
  // field on the wire (spec §2, INV-1).
  rpc Evaluate(PluginHostServiceEvaluateRequest) returns (PluginHostServiceEvaluateResponse);
```

At the bottom of the file, add the messages:

```protobuf
message PluginHostServiceEvaluateRequest {
  string action = 1 [(buf.validate.field).string.min_len = 1];
  // resource is a typed instance ref: "scene:01ABC...".
  string resource = 2 [(buf.validate.field).string.min_len = 3];
}

message PluginHostServiceEvaluateResponse {
  bool allowed = 1;
  string reason = 2;
  string matched_policy = 3;
}
```

- [ ] **Step 2: Regenerate proto code**

Run: `task proto`
Expected: `pkg/proto/holomush/plugin/v1/plugin.pb.go` and `plugin_grpc.pb.go` regenerate with `PluginHostServiceEvaluateRequest/Response` and a `PluginHostServiceClient.Evaluate` method. `UnimplementedPluginHostServiceServer.Evaluate` returns `codes.Unimplemented`.

- [ ] **Step 3: Verify generated symbols exist**

Run: `rg -n "func.*PluginHostServiceClient.*Evaluate|PluginHostServiceEvaluateResponse" pkg/proto/holomush/plugin/v1/`
Expected: matches in `plugin_grpc.pb.go` and `plugin.pb.go`.

- [ ] **Step 4: Build**

Run: `task build`
Expected: PASS (the `UnimplementedPluginHostServiceServer` embedding keeps the host service compiling before Task 3 implements the method).

- [ ] **Step 5: Commit**

`jj commit -m "feat(proto): add Evaluate RPC to PluginHostService (holomush-8kkv5)"`

---

## Phase 2: Binary surface

### Task 3: Implement `PluginHostService.Evaluate` (binary)

**Files:**

- Modify: `internal/plugin/goplugin/host.go` (add `engine`, `auditor` fields + `WithEngine`/`WithAuditLogger` options near the other `HostOption`s at line ~147)
- Modify: `internal/plugin/goplugin/host_service.go` (add `Evaluate` method modeled on `EmitEvent` at line 42)
- Test: `internal/plugin/goplugin/host_service_test.go`

- [ ] **Step 1: Write the failing test**

```go
// TestEvaluateDerivesSubjectFromTokenAndDelegates asserts the binary
// Evaluate recovers the actor from the dispatch token (NOT plugin-supplied
// metadata), builds a character: subject, and returns the engine decision.
func TestEvaluateDerivesSubjectFromToken(t *testing.T) {
	eng := policytest.AllowAllEngine()
	manifest := &plugins.Manifest{
		Name: "core-scenes", Type: plugins.TypeBinary,
		ResourceTypes: []string{"scene"},
	}
	h := newTestHostWithEngine(t, "core-scenes", manifest, eng)
	srv := &pluginHostServiceServer{host: h, pluginName: "core-scenes"}

	charID := core.NewULID()
	ctx, _ := contextWithValidToken(t, srv, core.Actor{Kind: core.ActorCharacter, ID: charID.String()})

	resp, err := srv.Evaluate(ctx, &pluginv1.PluginHostServiceEvaluateRequest{
		Action:   "extend_publish_attempts",
		Resource: "scene:01SCENE0000000000000000000",
	})
	require.NoError(t, err)
	assert.True(t, resp.GetAllowed())
}

func TestEvaluateMissingTokenFailsClosed(t *testing.T) {
	h := newTestHostWithEngine(t, "core-scenes",
		&plugins.Manifest{Name: "core-scenes", Type: plugins.TypeBinary, ResourceTypes: []string{"scene"}},
		policytest.AllowAllEngine())
	srv := &pluginHostServiceServer{host: h, pluginName: "core-scenes"}

	_, err := srv.Evaluate(context.Background(), &pluginv1.PluginHostServiceEvaluateRequest{
		Action: "read", Resource: "scene:01SCENE0000000000000000000",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_MISSING")
}

func TestEvaluateForeignResourceTypeRejected(t *testing.T) {
	h := newTestHostWithEngine(t, "core-scenes",
		&plugins.Manifest{Name: "core-scenes", Type: plugins.TypeBinary, ResourceTypes: []string{"scene"}},
		policytest.AllowAllEngine())
	srv := &pluginHostServiceServer{host: h, pluginName: "core-scenes"}
	ctx, _ := contextWithValidToken(t, srv, core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()})

	_, err := srv.Evaluate(ctx, &pluginv1.PluginHostServiceEvaluateRequest{
		Action: "read", Resource: "server:global",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVALUATE_UNENTITLED_TYPE")
}
```

Add the test helper (after `newTestServer`):

```go
func newTestHostWithEngine(t *testing.T, name string, m *plugins.Manifest, eng types.AccessPolicyEngine) *Host {
	t.Helper()
	h := NewHost(WithEngine(eng))
	h.plugins[name] = &loadedPlugin{manifest: m}
	return h
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./internal/plugin/goplugin/ -run TestEvaluate`
Expected: FAIL — `WithEngine` undefined, `srv.Evaluate` undefined.

- [ ] **Step 3: Add host fields + options in `host.go`**

Add fields to the `Host` struct (near `eventEmitter`/`tokenStore`, line ~171):

```go
	engine  types.AccessPolicyEngine // optional; nil = Evaluate fails closed
	auditor pluginauthz.Auditor      // optional; nil = no audit emission
```

Add options near the other `HostOption`s:

```go
// WithEngine configures the host with the ABAC engine used by the
// PluginHostService.Evaluate RPC.
func WithEngine(e types.AccessPolicyEngine) HostOption {
	return func(h *Host) { h.engine = e }
}

// WithAuditLogger configures the host with the audit sink used to record
// per-action Evaluate decisions.
func WithAuditLogger(a pluginauthz.Auditor) HostOption {
	return func(h *Host) { h.auditor = a }
}
```

Add a helper to read a plugin's owned resource types:

```go
// ownedResourceTypes returns the set of resource type names declared by the
// named plugin's manifest. Returns an empty (non-nil) set if unknown.
func (h *Host) ownedResourceTypes(pluginName string) map[string]bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[string]bool)
	if lp, ok := h.plugins[pluginName]; ok && lp.manifest != nil {
		for _, rt := range lp.manifest.ResourceTypes {
			out[rt] = true
		}
	}
	return out
}
```

Add imports: `"github.com/holomush/holomush/internal/access/policy/types"` and `"github.com/holomush/holomush/internal/plugin/pluginauthz"`.

- [ ] **Step 4: Implement `Evaluate` in `host_service.go`**

```go
// Evaluate runs the host ABAC engine for one action against one resource
// instance owned by the calling plugin. The subject is recovered from the
// dispatch token (identical anti-spoof posture to EmitEvent, spec §2); the
// plugin's actor metadata is NOT trusted as an identity claim.
func (s *pluginHostServiceServer) Evaluate(ctx context.Context, req *pluginv1.PluginHostServiceEvaluateRequest) (*pluginv1.PluginHostServiceEvaluateResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	s.host.mu.RLock()
	engine := s.host.engine
	auditor := s.host.auditor
	tokenStore := s.host.tokenStore
	s.host.mu.RUnlock()
	if engine == nil {
		return nil, oops.Code("EVALUATE_ENGINE_UNCONFIGURED").
			With("plugin", s.pluginName).Errorf("plugin host engine is not configured")
	}
	if tokenStore == nil {
		return nil, oops.Code("EMIT_TOKEN_STORE_UNCONFIGURED").
			With("plugin", s.pluginName).Errorf("plugin token store is not configured")
	}

	md, _ := metadata.FromIncomingContext(ctx)
	tokens := md.Get("x-holomush-emit-token")
	if len(tokens) == 0 || tokens[0] == "" {
		return nil, oops.Code("EMIT_TOKEN_MISSING").
			With("plugin", s.pluginName).Errorf("evaluate without a host-issued dispatch token")
	}
	storedActor, ok := tokenStore.Lookup(s.pluginName, tokens[0])
	if !ok {
		return nil, oops.Code("EMIT_TOKEN_REJECTED").
			With("plugin", s.pluginName).Errorf("dispatch token is not valid for this plugin")
	}

	dec, err := pluginauthz.Evaluate(ctx, pluginauthz.Input{
		Engine:     engine,
		Auditor:    auditor,
		PluginName: s.pluginName,
		OwnedTypes: s.host.ownedResourceTypes(s.pluginName),
		Subject:    pluginauthz.ActorSubject(storedActor), // shared mapping (INV-5)
		Action:     req.GetAction(),
		Resource:   req.GetResource(),
	})
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}
	return &pluginv1.PluginHostServiceEvaluateResponse{
		Allowed:       dec.Allowed,
		Reason:        dec.Reason,
		MatchedPolicy: dec.MatchedPolicy,
	}, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- ./internal/plugin/goplugin/ -run TestEvaluate`
Expected: PASS (3 tests).

- [ ] **Step 6: Lint + commit**

Run: `task lint:go`
`jj commit -m "feat(plugin): binary PluginHostService.Evaluate via dispatch-token subject (holomush-8kkv5)"`

---

## Phase 3: Lua surface

### Task 4: `holomush.evaluate` Lua hostfunc

**Files:**

- Create: `internal/plugin/hostfunc/evaluate.go`
- Modify: `internal/plugin/hostfunc/functions.go` (register `evaluate` in `Register`; add `WithAuditLogger` option + `auditor` field)
- Test: `internal/plugin/hostfunc/evaluate_test.go`

- [ ] **Step 1: Write the failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"testing"

	lua "github.com/yuin/gopher-lua"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

func TestEvaluateAllowedByEngine(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID()
	L.SetContext(core.WithActor(context.Background(),
		core.Actor{Kind: core.ActorCharacter, ID: charID.String()}))

	hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "lua-plug")

	require.NoError(t, L.DoString(`allowed, reason = holomush.evaluate("execute", "command:greet")`))
	assert.True(t, bool(L.GetGlobal("allowed").(lua.LBool)))
}

func TestEvaluateDeniedByEngine(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	L.SetContext(core.WithActor(context.Background(),
		core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()}))

	hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.DenyAllEngine()))
	hf.Register(L, "lua-plug")

	require.NoError(t, L.DoString(`allowed, reason = holomush.evaluate("execute", "command:greet")`))
	assert.False(t, bool(L.GetGlobal("allowed").(lua.LBool)))
}

func TestEvaluateNoActorFailsClosed(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	// No actor on the context.
	L.SetContext(context.Background())
	hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "lua-plug")

	require.NoError(t, L.DoString(`allowed, err = holomush.evaluate("execute", "command:greet")`))
	assert.False(t, bool(L.GetGlobal("allowed").(lua.LBool)))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./internal/plugin/hostfunc/ -run TestEvaluate`
Expected: FAIL — `holomush.evaluate` is not registered (Lua attempt-to-call-nil).

- [ ] **Step 3: Implement the hostfunc**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// evaluateFn returns the holomush.evaluate(action, resource) implementation
// for the named plugin. Lua plugins own no resource_types, so entitlement
// degrades to the command carve-out (spec §3). Subject is read from the
// dispatch context (in-process, not forgeable) — never from Lua args.
func (f *Functions) evaluateFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		action := L.CheckString(1)
		resource := L.CheckString(2)
		ctx := luaContext(L)

		if f.engine == nil {
			L.Push(lua.LFalse)
			L.Push(lua.LString("access engine not available"))
			return 2
		}

		var subject string
		if actor, ok := core.ActorFromContext(ctx); ok {
			subject = pluginauthz.ActorSubject(actor) // shared mapping (INV-5)
		}

		dec, err := pluginauthz.Evaluate(ctx, pluginauthz.Input{
			Engine:     f.engine,
			Auditor:    f.auditor,
			PluginName: pluginName,
			OwnedTypes: map[string]bool{}, // Lua plugins own no resource types
			Subject:    subject,
			Action:     action,
			Resource:   resource,
		})
		if err != nil {
			L.Push(lua.LFalse)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LBool(dec.Allowed))
		if dec.Reason != "" {
			L.Push(lua.LString(dec.Reason))
		} else {
			L.Push(lua.LNil)
		}
		return 2
	}
}
```

The subject mapping is the shared `pluginauthz.ActorSubject` (Task 1) — do
NOT redefine it here; both surfaces MUST use the one mapper (INV-5).

> `luaContext(L)` is the existing helper used by other hostfuncs to read `L.Context()` with a background fallback — confirm its name with `rg -n "func luaContext|L.Context\(\)" internal/plugin/hostfunc/` and reuse it. If the helper is named differently, match the existing call sites.

- [ ] **Step 4: Register it + add the auditor field/option in `functions.go`**

In the `Functions` struct add: `auditor pluginauthz.Auditor`.
Add the option:

```go
// WithAuditLogger sets the audit sink used by holomush.evaluate.
func WithAuditLogger(a pluginauthz.Auditor) Option {
	return func(f *Functions) { f.auditor = a }
}
```

In `Register`, alongside the other `holomush.*` registrations, add:

```go
	L.SetField(holo, "evaluate", L.NewFunction(f.evaluateFn(pluginName)))
```

(Use whatever local variable the existing code uses for the `holomush` table — confirm with `rg -n "SetField\(.*new_request_id|holomush" internal/plugin/hostfunc/functions.go`.)

If an audit/context meta-test enumerates registered hostfuncs (e.g.
`RegisteredFunctionsForAudit` exercised by `context_audit_test.go`), add
`evaluate` to that inventory so the per-hostfunc context-cancellation meta-test
covers it. Confirm with `rg -n "RegisteredFunctionsForAudit|context_audit" internal/plugin/hostfunc/`; if the inventory is derived automatically from the registered globals, no change is needed.

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- ./internal/plugin/hostfunc/ -run TestEvaluate`
Expected: PASS (3 tests).

- [ ] **Step 6: Lint + commit**

Run: `task lint:go`
`jj commit -m "feat(hostfunc): holomush.evaluate Lua global delegating to pluginauthz (holomush-8kkv5)"`

---

## Phase 4: SDK ergonomics

### Task 5: SDK `host.Evaluate` client helper (binary)

**Files:**

- Create: `pkg/plugin/evaluate_client.go`
- Test: `pkg/plugin/evaluate_client_test.go`

- [ ] **Step 1: Write the failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// evalTestServer is a minimal PluginHostService returning a fixed decision.
type evalTestServer struct {
	pluginv1.UnimplementedPluginHostServiceServer
	gotAction, gotResource string
	allow                  bool
}

func (s *evalTestServer) Evaluate(_ context.Context, req *pluginv1.PluginHostServiceEvaluateRequest) (*pluginv1.PluginHostServiceEvaluateResponse, error) {
	s.gotAction = req.GetAction()
	s.gotResource = req.GetResource()
	return &pluginv1.PluginHostServiceEvaluateResponse{Allowed: s.allow, Reason: "ok"}, nil
}

func TestHostEvaluateForwardsAndReturnsDecision(t *testing.T) {
	srv := &evalTestServer{allow: true}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &hostEvaluateClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	dec, err := client.Evaluate(context.Background(), "extend_publish_attempts", "scene:01SCENE")
	require.NoError(t, err)
	assert.True(t, dec.Allowed)
	assert.Equal(t, "extend_publish_attempts", srv.gotAction)
	assert.Equal(t, "scene:01SCENE", srv.gotResource)
}

func TestHostEvaluateNilClientFailsClosed(t *testing.T) {
	client := &hostEvaluateClient{}
	dec, err := client.Evaluate(context.Background(), "read", "scene:01SCENE")
	require.Error(t, err)
	assert.False(t, dec.Allowed)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./pkg/plugin/ -run TestHostEvaluate`
Expected: FAIL — `hostEvaluateClient` undefined.

- [ ] **Step 3: Implement the client helper**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"

	"github.com/samber/oops"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// EvaluateDecision is the SDK-facing result of a host Evaluate call.
type EvaluateDecision struct {
	Allowed       bool
	Reason        string
	MatchedPolicy string
}

// HostEvaluator lets a binary plugin authorize one action against one
// resource instance through the host ABAC engine. The host derives the
// subject from the dispatch context — the plugin does not (and cannot)
// supply it.
type HostEvaluator interface {
	Evaluate(ctx context.Context, action, resource string) (EvaluateDecision, error)
}

type hostEvaluateClient struct {
	client pluginv1.PluginHostServiceClient
}

func (c *hostEvaluateClient) Evaluate(ctx context.Context, action, resource string) (EvaluateDecision, error) {
	if c.client == nil {
		return EvaluateDecision{}, oops.New("host evaluate client is not configured")
	}
	resp, err := c.client.Evaluate(ctx, &pluginv1.PluginHostServiceEvaluateRequest{
		Action:   action,
		Resource: resource,
	})
	if err != nil {
		return EvaluateDecision{}, oops.With("action", action).With("resource", resource).Wrap(err)
	}
	return EvaluateDecision{
		Allowed:       resp.GetAllowed(),
		Reason:        resp.GetReason(),
		MatchedPolicy: resp.GetMatchedPolicy(),
	}, nil
}
```

> Wire `hostEvaluateClient` into the plugin's host-service injection path the same way `pluginHostFocusClient` is injected during `Init` (see `pkg/plugin/service.go` / `focus_client.go`). Add that injection step here only if Task 7's `core-scenes` wiring needs it; otherwise the constructor is enough for the unit test and a follow-up step wires it.

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- ./pkg/plugin/ -run TestHostEvaluate`
Expected: PASS (2 tests).

- [ ] **Step 5: Lint + commit**

Run: `task lint:go`
`jj commit -m "feat(pluginsdk): host.Evaluate client helper (holomush-8kkv5)"`

---

### Task 6: SDK gated subcommand dispatcher

**Files:**

- Create: `pkg/plugin/gated_dispatch.go`
- Test: `pkg/plugin/gated_dispatch_test.go`

- [ ] **Step 1: Write the failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEvaluator struct {
	allow     bool
	gotAction string
	gotResrc  string
	calls     int
}

func (f *fakeEvaluator) Evaluate(_ context.Context, action, resource string) (EvaluateDecision, error) {
	f.calls++
	f.gotAction, f.gotResrc = action, resource
	return EvaluateDecision{Allowed: f.allow, Reason: "nope"}, nil
}

func TestGatedSubcommand_DenyShortCircuitsBeforeHandler(t *testing.T) {
	ev := &fakeEvaluator{allow: false}
	handlerRan := false
	gs := GatedSubcommand{
		Name:        "extend",
		Action:      "extend_publish_attempts",
		ResourceRef: func(args string) (string, error) { return "scene:" + args, nil },
		Handler: func(context.Context, CommandRequest, string) (*CommandResponse, error) {
			handlerRan = true
			return OK(""), nil
		},
	}

	resp, err := gs.Run(context.Background(), ev, CommandRequest{Command: "scene", Args: "extend 01SCENE"}, "01SCENE")
	require.NoError(t, err)
	assert.False(t, handlerRan, "handler MUST NOT run when the gate denies")
	assert.Equal(t, CommandError, resp.Status)
	assert.Equal(t, "extend_publish_attempts", ev.gotAction)
	assert.Equal(t, "scene:01SCENE", ev.gotResrc)
}

func TestGatedSubcommand_AllowRunsHandler(t *testing.T) {
	ev := &fakeEvaluator{allow: true}
	gs := GatedSubcommand{
		Name: "extend", Action: "extend_publish_attempts",
		ResourceRef: func(args string) (string, error) { return "scene:" + args, nil },
		Handler: func(context.Context, CommandRequest, string) (*CommandResponse, error) {
			return OK("extended"), nil
		},
	}
	resp, err := gs.Run(context.Background(), ev, CommandRequest{Command: "scene", Args: "extend 01SCENE"}, "01SCENE")
	require.NoError(t, err)
	assert.Equal(t, CommandOK, resp.Status)
	assert.Equal(t, "extended", resp.Output)
}

func TestGatedSubcommand_ResourceRefErrorSkipsGateAndHandler(t *testing.T) {
	ev := &fakeEvaluator{allow: true}
	handlerRan := false
	gs := GatedSubcommand{
		Name: "extend", Action: "extend_publish_attempts",
		ResourceRef: func(string) (string, error) { return "", assertRefErr() },
		Handler: func(context.Context, CommandRequest, string) (*CommandResponse, error) {
			handlerRan = true
			return OK(""), nil
		},
	}
	resp, err := gs.Run(context.Background(), ev, CommandRequest{Command: "scene", Args: "extend"}, "")
	require.NoError(t, err)
	assert.Equal(t, CommandError, resp.Status)
	assert.False(t, handlerRan)
	assert.Equal(t, 0, ev.calls, "gate MUST NOT be consulted when the resource ref can't be derived")
}

func assertRefErr() error { return context.Canceled }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./pkg/plugin/ -run TestGatedSubcommand`
Expected: FAIL — `GatedSubcommand` undefined.

- [ ] **Step 3: Implement the gated dispatcher**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import "context"

// GatedSubcommand binds a subcommand to an ABAC action + a resource-ref
// extractor so the host Evaluate gate is structural, not a remembered call.
// ResourceRef parses the resource instance from the already-split arg
// remainder (the plugin owns arg-grammar); Handler runs only if Evaluate
// allows. Signatures match the existing core-scenes subcommand handlers
// (ctx, req, args) -> (*CommandResponse, error).
type GatedSubcommand struct {
	Name        string
	Action      string
	ResourceRef func(args string) (string, error)
	Handler     func(ctx context.Context, req CommandRequest, args string) (*CommandResponse, error)
}

// Run resolves the resource ref, evaluates the gate, and runs the handler
// only on allow. A resource-ref error or a denial both short-circuit to a
// CommandError response; the handler does not run. An engine error returns
// a CommandFailure (service-degraded) response.
func (g GatedSubcommand) Run(ctx context.Context, ev HostEvaluator, req CommandRequest, args string) (*CommandResponse, error) {
	resource, refErr := g.ResourceRef(args)
	if refErr != nil {
		return Errorf("%s: %v", g.Name, refErr), nil
	}
	dec, err := ev.Evaluate(ctx, g.Action, resource)
	if err != nil {
		return Failuref("permission check failed: %v", err), nil
	}
	if !dec.Allowed {
		reason := dec.Reason
		if reason == "" {
			reason = "you are not permitted to do that"
		}
		return Errorf("%s", reason), nil
	}
	return g.Handler(ctx, req, args)
}
```

> Constructors confirmed in `pkg/plugin/command.go`: `OK(output) *CommandResponse`, `Errorf(format, ...) *CommandResponse` (returns `Status: CommandError`), and `Failuref(format, ...) *CommandResponse` (returns `Status: CommandFailure`). The engine-error path here wants `CommandFailure`, so it MAY call `Failuref("permission check failed: %v", err)` instead of the inline literal shown — either is fine; prefer `Failuref` for consistency with the rest of the SDK.

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- ./pkg/plugin/ -run TestGatedSubcommand`
Expected: PASS (3 tests).

- [ ] **Step 5: Lint + commit**

Run: `task lint:go`
`jj commit -m "feat(pluginsdk): structural gated subcommand dispatcher (holomush-8kkv5)"`

---

## Phase 5: core-scenes migration

### Task 7: Declare the extend action + admin policy; wire `scene extend`

**Files:**

- Modify: `plugins/core-scenes/plugin.yaml` (add `actions:` entry + admin policy)
- Modify: `plugins/core-scenes/commands.go` (wire `extend` via `GatedSubcommand`)
- Test: `plugins/core-scenes/commands_test.go`

- [ ] **Step 1: Add the action + policy to `plugin.yaml`**

Add an `actions:` block (top-level, sibling to `policies:`):

```yaml
actions:
  - extend_publish_attempts
```

Add to the `policies:` list:

```yaml
  - name: admin-extend-publish-attempts
    dsl: >-
      permit(principal is character, action in ["extend_publish_attempts"],
      resource is scene) when { "admin" in principal.character.roles };
```

- [ ] **Step 2: Verify manifest still parses**

Run: `task test -- ./internal/plugin/ -run TestParseManifest`
Expected: PASS — the new `actions:` entry and policy validate (free-form action name is allowed; admin DSL references the core `principal.character.roles` attribute).

- [ ] **Step 3: Write the failing handler test**

```go
func TestSceneExtendDeniedForNonAdmin(t *testing.T) {
	p := newScenePluginWithEvaluator(t, denyEvaluator{}) // Evaluate → not allowed
	sceneID := createSceneInTest(t, p, "char-alice", "Extendable")

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "extend " + sceneID, CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "permitted")
}

func TestSceneExtendAllowedForAdmin(t *testing.T) {
	p := newScenePluginWithEvaluator(t, allowEvaluator{}) // Evaluate → allowed
	sceneID := createSceneInTest(t, p, "char-admin", "Extendable")

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "extend " + sceneID, CharacterID: "char-admin",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
}
```

Add the fakes (`allowEvaluator`/`denyEvaluator` implement `pluginsdk.HostEvaluator`) and a `newScenePluginWithEvaluator` constructor that injects the evaluator into the plugin's command router. Match the existing scene test harness in `commands_test.go`.

- [ ] **Step 4: Run test to verify it fails**

Run: `task test -- ./plugins/core-scenes/ -run TestSceneExtend`
Expected: FAIL — `extend` subcommand unknown / not gated.

- [ ] **Step 5: Wire the `extend` subcommand through `GatedSubcommand`**

In the subcommand router (`dispatchCommand`), register `extend`:

```go
pluginsdk.GatedSubcommand{
	Name:        "extend",
	Action:      "extend_publish_attempts",
	ResourceRef: sceneResourceRef, // shared helper added in Task 8 (or inline here if Task 7 lands first)
	Handler:     p.handleExtend,   // (ctx, req, args) (*pluginsdk.CommandResponse, error)
}
```

where the shared resource-ref helper mirrors how existing handlers parse the id
(`sceneID := strings.TrimSpace(args)`):

```go
func sceneResourceRef(args string) (string, error) {
	id := strings.TrimSpace(args)
	if id == "" {
		return "", oops.Errorf("scene id is required")
	}
	return "scene:" + id, nil
}
```

Dispatch it from `dispatchCommand` after the subcommand split, passing the
plugin's injected `pluginsdk.HostEvaluator` and the arg remainder:

```go
case "extend":
	return extendGate.Run(ctx, p.evaluator, req, args)
```

(If `handleExtend` does not yet exist, add the minimal handler that performs the
bump via the scene store/service — its body is E2 work tracked under
holomush-5rh.20.35; this task only proves the gate denies/permits. `p.evaluator`
is a new `pluginsdk.HostEvaluator` field on `scenePlugin`, injected from the
`hostEvaluateClient` during `Init` in production and from a fake in tests.)

- [ ] **Step 6: Run tests to verify they pass**

Run: `task test -- ./plugins/core-scenes/ -run TestSceneExtend`
Expected: PASS.

- [ ] **Step 7: Lint + commit**

Run: `task lint:go`
`jj commit -m "feat(core-scenes): admin-gated scene extend via host Evaluate (holomush-8kkv5)"`

---

### Task 8: Migrate existing subcommands onto the engine; remove ad-hoc Go authz

**Files:**

- Modify: `plugins/core-scenes/commands.go` (replace Go authz decisions with `GatedSubcommand` gates)
- Test: `plugins/core-scenes/commands_test.go`

- [ ] **Step 1: Write the failing table-driven test (INV-7 backstop)**

```go
// TestSceneGatedSubcommands_DenyWhenPolicyDenies asserts every gated
// subcommand short-circuits to a denial when the engine denies.
func TestSceneGatedSubcommands_DenyWhenPolicyDenies(t *testing.T) {
	cases := []struct{ sub, action string }{
		{"end", "end"},
		{"pause", "pause"},
		{"resume", "resume"},
		{"set", "update"},
		{"invite", "invite"},
		{"kick", "kick"},
		{"transfer", "transfer-ownership"},
		{"leave", "leave"},
	}
	for _, tc := range cases {
		t.Run(tc.sub, func(t *testing.T) {
			p := newScenePluginWithEvaluator(t, denyEvaluator{})
			sceneID := createSceneInTest(t, p, "char-alice", "T")
			resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command: "scene", Args: tc.sub + " " + sceneID, CharacterID: "char-bob",
			})
			require.NoError(t, err)
			assert.Equal(t, pluginsdk.CommandError, resp.Status, "subcommand %q must deny via engine", tc.sub)
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./plugins/core-scenes/ -run TestSceneGatedSubcommands`
Expected: FAIL — subcommands still gate via Go `IsParticipant`, not the injected evaluator.

- [ ] **Step 3: Convert each subcommand to a `GatedSubcommand`**

For each row in the migration table (spec §7), register a `GatedSubcommand` with the verbatim action string and a `ResourceRef` of `"scene:" + <parsed id>`. Then delete the now-redundant Go authorization-decision checks — specifically the `store.IsParticipant` gate in `handleEmit` (`commands.go::handleEmit`, ~L844) and the owner checks in the end/pause/transfer/kick handlers. Keep cheap structural guards (arity, scene-id present/parseable).

Example for `end`:

```go
endGate := pluginsdk.GatedSubcommand{
	Name: "end", Action: "end",
	ResourceRef: sceneResourceRef, // shared helper: "scene:" + TrimSpace(args)
	Handler:     p.handleEnd,      // existing (ctx, req, args) (*CommandResponse, error)
}
// in dispatchCommand: case "end": return endGate.Run(ctx, p.evaluator, req, args)
```

Add the shared `ResourceRef` helper once (introduced in Task 7 if that lands
first; otherwise here). For subcommands whose id is not the whole remainder
(e.g. `invite <scene> <char>`, which uses `strings.Fields(args)[0]`), give that
subcommand its own `ResourceRef` that mirrors its existing parser:

```go
func sceneResourceRef(args string) (string, error) {
	id := strings.TrimSpace(args)
	if id == "" {
		return "", oops.Errorf("scene id is required")
	}
	return "scene:" + id, nil
}

func sceneResourceRefFirstField(args string) (string, error) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return "", oops.Errorf("scene id is required")
	}
	return "scene:" + fields[0], nil
}
```

- [ ] **Step 4: Run the full core-scenes suite**

Run: `task test -- ./plugins/core-scenes/`
Expected: PASS. Pre-existing participant/owner tests now pass through the engine gate; the `IsParticipant`-specific unit tests that asserted the Go path are updated or removed (the behavior is now asserted through the evaluator).

- [ ] **Step 5: Lint + commit**

Run: `task lint:go`
`jj commit -m "refactor(core-scenes): migrate per-action authz from Go checks to host Evaluate (holomush-8kkv5)"`

---

## Phase 6: Meta-tests, E2E, docs

### Task 9: Structural invariant meta-tests (INV-1, INV-5)

**Files:**

- Test: `internal/plugin/goplugin/evaluate_invariants_test.go` (Create)

- [ ] **Step 1: Write INV-1 — no subject field on the wire**

```go
func TestINV1_EvaluateRequestHasNoSubjectField(t *testing.T) {
	md := (&pluginv1.PluginHostServiceEvaluateRequest{}).ProtoReflect().Descriptor()
	for i := 0; i < md.Fields().Len(); i++ {
		name := string(md.Fields().Get(i).Name())
		assert.NotEqual(t, "subject", name, "EvaluateRequest MUST NOT carry a subject field (INV-1)")
	}
}
```

- [ ] **Step 2: Write INV-5 — both surfaces delegate to pluginauthz.Evaluate**

```go
// TestINV5_BothSurfacesDelegateToPluginauthz asserts the binary and Lua
// surfaces produce identical decisions for the same engine + inputs, which
// holds because both call pluginauthz.Evaluate. A divergence (e.g. one
// surface skipping entitlement) breaks this.
func TestINV5_BinaryAndLuaAgreeOnEntitlementReject(t *testing.T) {
	// Binary: foreign type rejected.
	// (reuse TestEvaluateForeignResourceTypeRejected harness)
	// Lua: foreign type rejected.
	L := lua.NewState(); defer L.Close()
	L.SetContext(core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()}))
	hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "lua-plug")
	require.NoError(t, L.DoString(`allowed, err = holomush.evaluate("read", "scene:01X")`)) // lua owns no types
	assert.False(t, bool(L.GetGlobal("allowed").(lua.LBool)), "lua must reject unowned type via shared entitlement")
}
```

- [ ] **Step 3: Run + commit**

Run: `task test -- ./internal/plugin/goplugin/ -run TestINV`
Expected: PASS.
`jj commit -m "test(plugin): INV-1 no-subject-field + INV-5 surface-parity meta-tests (holomush-8kkv5)"`

---

### Task 10: Real-engine integration test of the admin gate

**Harness reality (why this is not a full-stack Ginkgo E2E):** the
`integrationtest` harness wires an **empty** command registry
(`internal/testsupport/integrationtest/harness.go:214` —
`command.NewDispatcher(command.NewRegistry(), pe)`), loads **no plugins**, and
uses **fake** engines by design (the privacy suite passes
`WithPolicyEngine(policytest.DenyAllEngine())`); `Session.CreateScene` is a
`t.Fatalf` stub (`session.go:461`, TODO iwzt-9). So a `scene extend`
command-path E2E and any participant/owner assertion (which need the
core-scenes `AttributeResolver` loaded) are **blocked on harness extension** and
are deferred to a follow-up (Step 3). The `extend_publish_attempts` admin gate
is **principal-attribute-only** (`"admin" in principal.character.roles`), so it
needs no scene attribute resolution and is provable **now** against a real
engine using the policy package's existing engine-builder
(`createTestEngineWithPolicies` + `characterProvider` in `seed_smoke_test.go`).
`pluginauthz` imports only `policy/types` + `audit` + `core`, so a `package
policy` test may import it with no cycle.

**Files:**

- Create: `internal/access/policy/plugin_evaluate_gate_test.go` (package `policy`)

- [ ] **Step 1: Write the real-engine table test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// adminExtendDSL mirrors the admin-extend-publish-attempts policy declared in
// plugins/core-scenes/plugin.yaml (Task 7). The test installs it into a real
// Engine so the gate is exercised through real DSL parsing + condition
// evaluation against principal.character.roles, via pluginauthz.Evaluate.
const adminExtendDSL = `permit(principal is character, action in ["extend_publish_attempts"], resource is scene) when { "admin" in principal.character.roles };`

func TestPluginEvaluate_AdminExtendGate_RealEngine(t *testing.T) {
	cases := []struct {
		name      string
		roles     []string
		wantAllow bool
	}{
		{"admin allowed", []string{"admin"}, true},
		{"plain player denied", []string{"player"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prov := characterProvider(map[string]any{
				"id":    "01CHAR",
				"roles": tc.roles,
			}, nil)
			eng := createTestEngineWithPolicies(t, []string{adminExtendDSL},
				[]attribute.AttributeProvider{prov})

			dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
				Engine:     eng,
				PluginName: "core-scenes",
				OwnedTypes: map[string]bool{"scene": true},
				Subject:    "character:01CHAR",
				Action:     "extend_publish_attempts",
				Resource:   "scene:01SCENE0000000000000000000",
			})
			require.NoError(t, err)
			assert.Equal(t, tc.wantAllow, dec.Allowed)
		})
	}
}
```

- [ ] **Step 2: Run the test**

Run: `task test -- ./internal/access/policy/ -run TestPluginEvaluate_AdminExtendGate`
Expected: PASS — admin allowed, player denied, through the real engine + real
DSL condition evaluation. (No Docker; mock providers + in-memory engine.)
Commit: `jj commit -m "test(policy): real-engine admin-extend gate via pluginauthz.Evaluate (holomush-8kkv5)"`

- [ ] **Step 3: File the deferred full-stack E2E follow-up**

The full `scene extend` command-path E2E (telnet → loaded core-scenes plugin →
gated dispatcher → host Evaluate) and participant/owner-via-`AttributeResolver`
assertions require the `integrationtest` harness to (a) load the core-scenes
binary plugin + its command registry and (b) implement the scene-creation RPC
(`Session.CreateScene`, TODO iwzt-9). That is harness infrastructure, out of
scope for this feature. File it:

Run:

```bash
bd create --type=task --priority=2 --labels="abac,plugin,test,e2e" \
  --title="E2E: scene extend command-path via integrationtest (Ginkgo/Gomega)" \
  --description="Full-stack E2E for the host Evaluate gate: load core-scenes into the integrationtest harness + scene-creation RPC, then drive 'scene extend' (admin allow / player deny) and 'scene end' non-participant deny THROUGH the real engine + AttributeResolver. MUST use Ginkgo/Gomega, //go:build integration, task test:int. Deferred from holomush-8kkv5 because the harness currently wires an empty command registry and Session.CreateScene is a stub (iwzt-9)."
bd dep add <new-id> iwzt-9   # blocked-by the harness scene-creation work
```

(Use the real iwzt-9 bead id; confirm with `bd list --json | rg iwzt`.) This
follow-up carries the Ginkgo/Gomega full-stack E2E the project standard expects;
the admin-gate security property itself is proven now by Step 1.

---

### Task 11: Plugin-author documentation (PR-blocking)

**Files:**

- Create: `site/docs/extending/plugin-host-evaluate.md`

- [ ] **Step 1: Write the guide**

Document, with runnable snippets: the `host.Evaluate(ctx, action, resource)` Go SDK call and the `holomush.evaluate(action, resource)` Lua global; the `GatedSubcommand` pattern (action + `ResourceRef` + handler); the entitlement rule (resource type must be plugin-owned, or `command` for own commands); and that the subject is **host-derived** (never plugin-supplied). Cross-link `site/docs/extending/abac-attribute-resolver.md`.

- [ ] **Step 2: Lint the markdown**

Run: `rumdl check site/docs/extending/plugin-host-evaluate.md`
Expected: no issues. (If `site/.rumdl.toml` applies, run from repo root so the right config is picked up.)

- [ ] **Step 3: Commit**

`jj commit -m "docs(extending): plugin host Evaluate + gated-subcommand guide (holomush-8kkv5)"`

---

## Post-implementation checklist

- [ ] `task pr-prep` green (full lane).
- [ ] All seven invariants (INV-1…7) have a passing test (INV-1/5 meta-tests Task 9; INV-2/3/6 + audit/INV-4 unit Task 1; INV-7 Task 8; admin gate against the real engine Task 10).
- [ ] Tier-2 full-stack command-path E2E follow-up bead filed and blocked on iwzt-9 (Task 10 Step 3).
- [ ] `bd` children all closed; epic ready to close.
- [ ] Spec, plan, ADRs, and code on `main` (docs-first PR or single PR per the writing-plans handoff).
<!-- adr-capture: sha256=9e279a280a56d971; session=cli; ts=2026-05-25T14:26:23Z; adrs=holomush-dttdj,holomush-qeypl,holomush-61rdl,holomush-9l9pu -->
