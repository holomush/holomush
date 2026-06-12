<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!--
SPDX-License-Identifier: Apache-2.0
-->

# Plugin Host Capability Decomposition Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Decompose the 23-RPC `PluginHostService` god-service into 14 capability-scoped proto services in a dedicated `holomush.plugin.host.v1` namespace, rewire the binary plugin SDK to consume them, and delete the god-service — making a capability grant a meaningful least-privilege unit.

**Architecture:** A controlled capability vocabulary (14 tokens) maps to 14 small proto services, all served on the **one** existing host-broker gRPC server (multiple service registrations on a single `*grpc.Server`, so the broker handshake is unchanged). The binary SDK's per-capability client wrappers each construct their own per-service client over the same broker `*grpc.ClientConn`. The legacy Lua hostfunc surface is left fully intact (its gRPC consumption is sub-spec 3). Server handler logic is moved, not rewritten — no behavior change.

**Tech Stack:** Go, Protocol Buffers + buf (`task lint:proto`, `buf generate`), gRPC, hashicorp/go-plugin grpcbroker, testify.

**Spec:** `docs/superpowers/specs/2026-06-11-plugin-host-capability-decomposition-design.md`

---

## Orientation (read before Task 1)

### The 14 capability services (spec §2)

| Token | `host.v1` Service | RPCs |
| --- | --- | --- |
| `world.query` | `WorldQueryService` | `QueryLocation`, `QueryCharacter`, `QueryLocationCharacters`, `QueryObject`, `FindLocation` |
| `world.mutation` | `WorldMutationService` | `CreateLocation`, `CreateExit`, `CreateObject` |
| `property` | `PropertyService` | `GetProperty`, `SetProperty` |
| `session` | `SessionService` | `FindByName`, `ListActive`, `SetLastWhispered` |
| `session.admin` | `SessionAdminService` | `Broadcast`, `Disconnect` |
| `focus` | `FocusService` | `JoinFocus`, `LeaveFocus`, `LeaveFocusByTarget`, `PresentFocus`, `SetConnectionFocus`, `GetConnectionFocus`, `AutoFocusOnJoin`, `IsAnyConnFocused` |
| `eval` | `EvalService` | `Evaluate` |
| `emit` | `EmitService` | `EmitEvent`, `RequestEmitToken`, `RegisterEmitType` |
| `settings` | `SettingsService` | `GetSetting`, `SetSetting` |
| `kv` | `KVService` | `Get`, `Set`, `Delete` |
| `stream.history` | `StreamHistoryService` | `QueryStreamHistory` |
| `stream.subscription` | `StreamSubscriptionService` | `AddSessionStream`, `RemoveSessionStream` |
| `audit` | `AuditService` | `DecryptOwnAuditRows` |
| `command-registry` | `CommandRegistryService` | `ListCommands`, `GetCommandHelp` |

### Source of each service's surface (grounded)

| Service | Current home (moved from) |
| --- | --- |
| Focus, Emit, Eval, Settings, StreamHistory, Audit, CommandRegistry | served `pluginHostServiceServer` methods in `internal/plugin/goplugin/host_service.go` |
| KV, StreamSubscription | unserved `PluginHostService` RPCs (`holomush-l6std`) — proto contract only; server may stay `Unimplemented` (status note → sub-spec 3) |
| WorldQuery, WorldMutation, Property, Session, SessionAdmin | Lua-only today; fresh proto derived from `internal/plugin/hostfunc/functions.go` + `internal/plugin/hostfunc/cap_session.go`. **No binary server is wired in this sub-spec** (binary consumers don't exist yet); the proto + a `codes.Unimplemented` stub are defined, full server is out of scope here |

### Build-green discipline

`PluginHostService` cannot be deleted until every binary client is rewired. The order is therefore: **(1)** vocab → **(2)** new proto (additive) → **(3)** new servers registered alongside the old → **(4)** rewire each SDK client wrapper (old service still served, so each is green) → **(5)** delete the god-service once nothing dials it → **(6)** invariants + meta-tests. Run `task build` after every task; it MUST stay green.

### Naming conventions (grounded)

- Proto package `holomush.plugin.host.v1`; `option go_package = "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1;hostv1";`
- Message types drop the `PluginHostService` prefix (spec §2): `PluginHostServiceEmitEventRequest` → `hostv1.EmitEventRequest`.
- Domain messages already in `audit.proto` (`AuditRow`, `RowResult`) are **imported**, not duplicated.
- Every proto element needs a Go-grounded doc comment — carry over the existing `plugin.proto` comments verbatim (`.claude/rules/proto-doc-comments.md`; `task lint:proto` enforces).

---

## Phase 1: Capability vocabulary

### Task 1: Extend the vocabulary to the full 14-token taxonomy

**Files:**

- Modify: `internal/plugin/capability_vocab.go:28-37` (`DefaultCapabilityVocabulary`)
- Test: `internal/plugin/capability_vocab_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/plugin/capability_vocab_test.go`:

```go
// Verifies: INV-PLUGIN-48
func TestDefaultCapabilityVocabularyIsTheFullTaxonomy(t *testing.T) {
	v := DefaultCapabilityVocabulary() // white-box: capability_vocab_test.go is package plugins
	want := []string{
		"world.query", "world.mutation", "property", "session", "session.admin",
		"focus", "eval", "emit", "settings", "kv",
		"stream.history", "stream.subscription", "audit", "command-registry",
	}
	for _, name := range want {
		assert.True(t, v.Has(name), "vocabulary must contain %q", name)
	}
	// Ambient substrate is NOT a capability (spec §4 / INV-PLUGIN-48).
	for _, ambient := range []string{"log", "new_request_id", "config"} {
		assert.False(t, v.Has(ambient), "ambient %q must NOT be a capability token", ambient)
	}
	// alias is delivered at the command layer, never a capability (spec §Background).
	assert.False(t, v.Has("alias"), "alias must NOT be a capability token")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestDefaultCapabilityVocabularyIsTheFullTaxonomy ./internal/plugin/`
Expected: FAIL (only `session`, `property`, `world.query` registered today).

- [ ] **Step 3: Replace the minimal vocabulary with the full taxonomy**

In `internal/plugin/capability_vocab.go`, replace the `DefaultCapabilityVocabulary` body:

```go
// DefaultCapabilityVocabulary returns the full host-capability taxonomy
// (sub-spec 2, spec §1). Each name maps to exactly one capability-scoped service
// in holomush.plugin.host.v1. Ambient substrate (log, new_request_id, stdlib,
// config) is intentionally absent — it is not a capability (spec §4).
func DefaultCapabilityVocabulary() *CapabilityVocabulary {
	v := NewCapabilityVocabulary()
	for _, name := range []string{
		"world.query", "world.mutation", "property", "session", "session.admin",
		"focus", "eval", "emit", "settings", "kv",
		"stream.history", "stream.subscription", "audit", "command-registry",
	} {
		v.Register(name)
	}
	return v
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestDefaultCapabilityVocabulary ./internal/plugin/`
Expected: PASS. Also run `task test -- ./internal/plugin/` to confirm no resolver test regressed (the foundation's reclassified manifests still resolve).

- [ ] **Step 5: Update the doc comment on `CapabilityVocabulary`**

Edit the type doc comment (`capability_vocab.go:6-9`) to drop "the FULL taxonomy is defined in sub-spec 2; the foundation registers only the minimum" — replace with "the full taxonomy (spec §1); each token backs one `holomush.plugin.host.v1` service."

- [ ] **Step 6: Commit**

`jj commit -m "feat(plugin): full host-capability vocabulary (14 tokens) — sub-spec 2 (holomush-eykuh.1)"`

---

## Phase 2: The `holomush.plugin.host.v1` proto package

### Task 2: Scaffold the proto package and author all 14 services (additive)

**Files:**

- Create: `api/proto/holomush/plugin/host/v1/focus.proto` (worked example below)
- Create: `api/proto/holomush/plugin/host/v1/{emit,eval,settings,stream,audit,command_registry,kv,world,property,session}.proto`
- Modify: `buf.yaml` / `buf.gen.yaml` only if the new directory needs an explicit module entry (verify first)
- Generated: `pkg/proto/holomush/plugin/host/v1/*.pb.go` (via `buf generate`)

This task is **additive** — `plugin.proto`'s `PluginHostService` is untouched here, so the build stays green.

- [ ] **Step 1: Confirm buf picks up the new directory**

Run: `rg -n "modules:|path:" buf.yaml` and inspect. The repo uses a single proto root (`api/proto`), so a new sub-package needs no module edit. If `buf.yaml` enumerates explicit paths, add `api/proto/holomush/plugin/host/v1`.

- [ ] **Step 2: Write `focus.proto` (the worked pattern for all 14)**

Create `api/proto/holomush/plugin/host/v1/focus.proto`. Carry the 8 focus RPCs and their messages over from `plugin.proto:106-188`, dropping the `PluginHostService` prefix and preserving the doc comments verbatim:

```proto
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

syntax = "proto3";

package holomush.plugin.host.v1;

option go_package = "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1;hostv1";

// FocusService is the host-brokered `focus` capability: a plugin granted
// `capability: focus` may mutate and read session/connection focus state through
// the host focus coordinator. Carved from the former PluginHostService
// (holomush-eykuh.1). Served by focusServer in
// internal/plugin/goplugin/host_capability_servers.go.
service FocusService {
  // JoinFocus adds a focus membership (e.g. a scene) to a session via the host
  // focus coordinator. The plugin declares intent; the coordinator applies the
  // kind-specific replay policy. Fails if the focus coordinator is not configured.
  rpc JoinFocus(JoinFocusRequest) returns (JoinFocusResponse);
  // LeaveFocus removes one focus membership from a session. Idempotent — leaving
  // a target the session does not hold is a successful no-op.
  rpc LeaveFocus(LeaveFocusRequest) returns (LeaveFocusResponse);
  // LeaveFocusByTarget removes the given focus membership from every non-expired
  // session that holds it — cross-session fan-out. Partial success is normal:
  // per-session failures aggregate into the response, not an RPC error.
  rpc LeaveFocusByTarget(LeaveFocusByTargetRequest) returns (LeaveFocusByTargetResponse);
  // PresentFocus repoints a session's PresentingFocus to an existing membership.
  // The target MUST already be in the session's FocusMemberships.
  rpc PresentFocus(PresentFocusRequest) returns (PresentFocusResponse);
  // SetConnectionFocus is the explicit focus mutation for a single Connection;
  // validates the membership, then writes Connection.FocusKey and (D9-gated)
  // Info.PresentingFocus atomically under one Store-lock acquisition.
  rpc SetConnectionFocus(SetConnectionFocusRequest) returns (SetConnectionFocusResponse);
  // GetConnectionFocus returns the named connection's current per-connection
  // focus, or absent when grid-focused (FocusKey nil) or unknown. Read-only.
  rpc GetConnectionFocus(GetConnectionFocusRequest) returns (GetConnectionFocusResponse);
  // AutoFocusOnJoin focuses all of a character's terminal/telnet connections on
  // a scene at once. Connections already explicitly focused elsewhere are
  // skipped. The caller MUST have completed JoinFocus first.
  rpc AutoFocusOnJoin(AutoFocusOnJoinRequest) returns (AutoFocusOnJoinResponse);
  // IsAnyConnFocused reports whether any of the character's connections focuses
  // the given scene, so callers can decide whether to emit a notification.
  rpc IsAnyConnFocused(IsAnyConnFocusedRequest) returns (IsAnyConnFocusedResponse);
}

// (messages: copy each PluginHostServiceJoinFocusRequest/Response body from
//  plugin.proto verbatim, renamed JoinFocusRequest/Response, etc. — field
//  numbers, types, and field doc comments unchanged.)
```

- [ ] **Step 3: Author the remaining 13 service files**

One `.proto` per service, identical mechanics (drop prefix, carry comments). Exact contents:

| File | Service / RPCs | Message source |
| --- | --- | --- |
| `emit.proto` | `EmitService`: `EmitEvent`, `RequestEmitToken`, `RegisterEmitType` | `plugin.proto` Emit/RequestEmitToken messages; add `RegisterEmitTypeRequest{string event_type=1}` / empty response (promoted from the Lua `register_emit_type`, spec §5) |
| `eval.proto` | `EvalService`: `Evaluate` | `plugin.proto` Evaluate messages |
| `settings.proto` | `SettingsService`: `GetSetting`, `SetSetting` | `plugin.proto` Get/SetSetting messages |
| `stream.proto` | `StreamHistoryService`: `QueryStreamHistory`; `StreamSubscriptionService`: `AddSessionStream`, `RemoveSessionStream` | `plugin.proto` QueryStreamHistory + Add/RemoveSessionStream messages |
| `audit.proto` | `AuditService`: `DecryptOwnAuditRows` | **import** `holomush/plugin/v1/audit.proto` for `AuditRow`/`RowResult`; define `DecryptOwnAuditRowsRequest`/`Response` referencing them |
| `command_registry.proto` | `CommandRegistryService`: `ListCommands`, `GetCommandHelp` | `plugin.proto` ListCommands/GetCommandHelp messages |
| `kv.proto` | `KVService`: `Get`, `Set`, `Delete` | `plugin.proto` KVGet/KVSet/KVDelete messages (renamed `GetRequest`…); unserved today |
| `world.proto` | `WorldQueryService`: `QueryLocation`,`QueryCharacter`,`QueryLocationCharacters`,`QueryObject`,`FindLocation`; `WorldMutationService`: `CreateLocation`,`CreateExit`,`CreateObject` | derive messages from `functions.go` hostfunc signatures (Lua-only; no prior proto) |
| `property.proto` | `PropertyService`: `GetProperty`, `SetProperty` | derive from `functions.go` `get_property`/`set_property` |
| `session.proto` | `SessionService`: `FindByName`,`ListActive`,`SetLastWhispered`; `SessionAdminService`: `Broadcast`,`Disconnect` | derive from `internal/plugin/hostfunc/cap_session.go` `SessionCapability` / `SessionAccess` interface |

For the Lua-derived services (`world`, `property`, `session`), the message fields mirror the Lua function args/returns (e.g. `FindByNameRequest{string name=1}`, `FindByNameResponse{SessionInfo session=1}` where `SessionInfo` carries `id, character_id, character_name, location_id, grid_present, last_whispered` per `internal/plugin/hostfunc/cap_session.go:94-99`). Each element gets a Go-grounded doc comment.

- [ ] **Step 4: Generate code**

Run: `buf generate` (from repo root, or the task wrapper — check `rg -n "buf generate" Taskfile*.yml`).
Expected: `pkg/proto/holomush/plugin/host/v1/*.pb.go` created.

- [ ] **Step 5: Proto lint**

Run: `task lint:proto`
Expected: PASS (every service/rpc/message/field has a non-name-echo doc comment). Fix any `COMMENTS` or name-echo failures inline.

- [ ] **Step 6: Build**

Run: `task build`
Expected: PASS (additive; nothing consumes the new packages yet).

- [ ] **Step 7: Commit**

`jj commit -m "feat(plugin): add holomush.plugin.host.v1 capability proto services (holomush-eykuh.1)"`

---

## Phase 3: Server-side rehoming (served alongside the god-service)

### Task 3: Implement and register the per-capability servers

**Files:**

- Create: `internal/plugin/goplugin/host_capability_servers.go`
- Modify: `internal/plugin/goplugin/host_service.go:36-45` (`newPluginHostServiceServer` → also register the new services on the same `*grpc.Server`)
- Test: `internal/plugin/goplugin/host_capability_servers_test.go` (re-point existing served-RPC assertions)

The 12 served handlers (Focus×8, Emit×2, Eval, Settings×2, StreamHistory, Audit, CommandRegistry) move from `pluginHostServiceServer` onto per-capability structs that share the same `*Host` + `pluginName`. Registering multiple services on one `*grpc.Server` keeps the single broker handshake (`host.go:663`).

- [ ] **Step 1: Write the failing test**

```go
func TestHostBrokerServerServesFocusService(t *testing.T) {
	h := NewHost() // constructor at internal/plugin/goplugin/host.go:280
	build := newPluginHostServiceServer(h, "test-plugin")
	srv := build(nil)
	info := srv.GetServiceInfo()
	require.Contains(t, info, "holomush.plugin.host.v1.FocusService")
	require.Contains(t, info, "holomush.plugin.host.v1.EmitService")
	// Old service still registered during migration (deleted in Task 12).
	require.Contains(t, info, "holomush.plugin.v1.PluginHostService")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestHostBrokerServerServesFocusService ./internal/plugin/goplugin/`
Expected: FAIL (only `PluginHostService` registered).

- [ ] **Step 3: Create the per-capability server structs**

In `host_capability_servers.go`, one struct per service embedding the generated `Unimplemented<Svc>Server` and holding `*Host` + `pluginName`. Move the method bodies from `host_service.go` (verbatim — only the receiver type and the `pluginv1.PluginHostServiceX` request/response types change to `hostv1.X`). Worked example:

```go
type focusServer struct {
	hostv1.UnimplementedFocusServiceServer
	host       *Host
	pluginName string
}

func (s *focusServer) JoinFocus(ctx context.Context, req *hostv1.JoinFocusRequest) (*hostv1.JoinFocusResponse, error) {
	// body moved verbatim from pluginHostServiceServer.JoinFocus (host_service.go:106)
}
// ... LeaveFocus, LeaveFocusByTarget, PresentFocus, SetConnectionFocus,
//     GetConnectionFocus, AutoFocusOnJoin, IsAnyConnFocused — same move.
```

Repeat for `emitServer` (EmitEvent, RequestEmitToken, RegisterEmitType — RegisterEmitType is a new thin handler delegating to the existing emit-type registry), `evalServer` (Evaluate), `settingsServer` (GetSetting, SetSetting + the unexported helpers `resolveSettingScope`, `principalScopedStore`, `requirePrincipalOwnership`, `authorizeGameWrite`, `actorFromToken` move with it), `streamHistoryServer` (QueryStreamHistory), `auditServer` (DecryptOwnAuditRows), `commandRegistryServer` (ListCommands, GetCommandHelp). `kvServer` and `streamSubscriptionServer` embed the `Unimplemented` base only (no impl — `holomush-l6std`, status → sub-spec 3).

Shared helpers (`actorFromToken`, etc.) that several servers need: lift them to package-level functions taking `(*Host, ctx)` or attach to a shared embedded `*hostCapabilityBase` struct to avoid duplication (DRY).

- [ ] **Step 4: Register all services on the one broker server**

Modify `newPluginHostServiceServer` (`host_service.go:36`):

```go
func newPluginHostServiceServer(host *Host, pluginName string) func([]grpc.ServerOption) *grpc.Server {
	return func(opts []grpc.ServerOption) *grpc.Server {
		server := grpc.NewServer(opts...)
		// Legacy god-service — removed in Task 12 once clients are rewired.
		pluginv1.RegisterPluginHostServiceServer(server, &pluginHostServiceServer{host: host, pluginName: pluginName})
		// Capability-scoped services (holomush-eykuh.1).
		hostv1.RegisterFocusServiceServer(server, &focusServer{host: host, pluginName: pluginName})
		hostv1.RegisterEmitServiceServer(server, &emitServer{host: host, pluginName: pluginName})
		hostv1.RegisterEvalServiceServer(server, &evalServer{host: host, pluginName: pluginName})
		hostv1.RegisterSettingsServiceServer(server, &settingsServer{host: host, pluginName: pluginName})
		hostv1.RegisterStreamHistoryServiceServer(server, &streamHistoryServer{host: host, pluginName: pluginName})
		hostv1.RegisterStreamSubscriptionServiceServer(server, &streamSubscriptionServer{host: host, pluginName: pluginName})
		hostv1.RegisterAuditServiceServer(server, &auditServer{host: host, pluginName: pluginName})
		hostv1.RegisterCommandRegistryServiceServer(server, &commandRegistryServer{host: host, pluginName: pluginName})
		hostv1.RegisterKVServiceServer(server, &kvServer{host: host, pluginName: pluginName})
		return server
	}
}
```

(World/Property/Session have no binary consumer in this sub-spec; do **not** register a server for them here — the proto exists for sub-spec 3. Registering an `Unimplemented`-only server is optional and adds nothing.)

- [ ] **Step 5: Run tests**

Run: `task test -- ./internal/plugin/goplugin/`
Expected: PASS. The old `pluginHostServiceServer` methods now delegate to the moved logic or are left in place until Task 12 — keep both compiling. (If you moved bodies out, have the old methods call the new server structs to avoid duplicate logic.)

- [ ] **Step 6: Build + commit**

Run: `task build` → PASS.
`jj commit -m "feat(plugin): serve capability-scoped host services on the broker (holomush-eykuh.1)"`

---

## Phase 4: Rewire the binary SDK clients (one wrapper per task)

Each wrapper currently wraps `pluginv1.PluginHostServiceClient` (constructed in `pkg/plugin/sdk.go:172`). Rewire each to its per-capability client over the same broker `conn`. The old service is still served, so each task is independently green.

### Task 4: Rewire `focus_client.go`

**Files:**

- Modify: `pkg/plugin/focus_client.go:219-225,648`
- Test: `pkg/plugin/focus_client_test.go`

- [ ] **Step 1: Update the test double** — change the test server registration from `pluginv1.RegisterPluginHostServiceServer` to `hostv1.RegisterFocusServiceServer` and the client to `hostv1.NewFocusServiceClient`. Run: `task test -- -run Focus ./pkg/plugin/` → FAIL (type mismatch).
- [ ] **Step 2: Swap the client type** — in `focus_client.go`, change the struct field and constructor:

```go
type pluginHostFocusClient struct {
	client hostv1.FocusServiceClient
}

func newPluginHostFocusClient(client hostv1.FocusServiceClient) FocusClient { ... }

// at the dial site (focus_client.go:648):
return newPluginHostFocusClient(hostv1.NewFocusServiceClient(conn)), nil
```

Update each call (`s.client.JoinFocus(ctx, &hostv1.JoinFocusRequest{...})`) to the `hostv1` request types.

- [ ] **Step 3: Run** — `task test -- -run Focus ./pkg/plugin/` → PASS.
- [ ] **Step 4: Build** — `task build` → PASS.
- [ ] **Step 5: Commit** — `jj commit -m "refactor(plugin): focus SDK client dials FocusService (holomush-eykuh.1)"`

Tasks 5–10 each follow the exact mechanics of Task 4 — swap `pluginv1.PluginHostServiceClient` → the per-capability `hostv1` client (struct field + constructor + dial site), update request/response types to the `hostv1` names, and update the test double's server registration to the matching `hostv1.Register<Svc>Server`. They are separate tasks (and separate beads) because each is an independent file with its own test and commit; the old god-service stays served, so each is independently green. Verify each with `task test -- ./pkg/plugin/` + `task build`, then commit.

### Task 5: Rewire `settings_client.go`

**Files:** Modify `pkg/plugin/settings_client.go`; Test `pkg/plugin/settings_client_test.go`.

- [ ] **Step 1** — test double → `hostv1.RegisterSettingsServiceServer`; client → `hostv1.SettingsServiceClient`; run `task test -- -run Settings ./pkg/plugin/` → FAIL.
- [ ] **Step 2** — swap the struct field/constructor to `hostv1.SettingsServiceClient` (dialed `hostv1.NewSettingsServiceClient(conn)`); update `GetSetting`/`SetSetting` calls to `hostv1.GetSettingRequest`/`SetSettingRequest`.
- [ ] **Step 3** — `task test -- -run Settings ./pkg/plugin/` → PASS; `task build` → PASS.
- [ ] **Step 4: Commit** — `jj commit -m "refactor(plugin): settings SDK client dials SettingsService (holomush-eykuh.1)"`

### Task 6: Rewire `command_lister.go`

**Files:** Modify `pkg/plugin/command_lister.go`; Test `pkg/plugin/command_lister_test.go`.

- [ ] **Step 1** — test double → `hostv1.RegisterCommandRegistryServiceServer`; client → `hostv1.CommandRegistryServiceClient`; run `task test -- -run Command ./pkg/plugin/` → FAIL.
- [ ] **Step 2** — swap to `hostv1.CommandRegistryServiceClient` (`hostv1.NewCommandRegistryServiceClient(conn)`); update `ListCommands`/`GetCommandHelp` calls to `hostv1` request types.
- [ ] **Step 3** — `task test -- -run Command ./pkg/plugin/` → PASS; `task build` → PASS.
- [ ] **Step 4: Commit** — `jj commit -m "refactor(plugin): command lister dials CommandRegistryService (holomush-eykuh.1)"`

### Task 7: Rewire `evaluate_client.go`

**Files:** Modify `pkg/plugin/evaluate_client.go`; Test `pkg/plugin/evaluate_client_test.go`.

- [ ] **Step 1** — test double → `hostv1.RegisterEvalServiceServer`; client → `hostv1.EvalServiceClient`; run `task test -- -run Evaluate ./pkg/plugin/` → FAIL.
- [ ] **Step 2** — swap to `hostv1.EvalServiceClient` (`hostv1.NewEvalServiceClient(conn)`); update the `Evaluate` call to `hostv1.EvaluateRequest`.
- [ ] **Step 3** — `task test -- -run Evaluate ./pkg/plugin/` → PASS; `task build` → PASS.
- [ ] **Step 4: Commit** — `jj commit -m "refactor(plugin): evaluate client dials EvalService (holomush-eykuh.1)"`

### Task 8: Rewire `event_sink.go` + `event_marshal.go` (emit)

**Files:** Modify `pkg/plugin/event_sink.go`, `pkg/plugin/event_marshal.go`; Test `pkg/plugin/event_marshal_test.go`.

- [ ] **Step 1** — test double → `hostv1.RegisterEmitServiceServer`; client → `hostv1.EmitServiceClient`; run `task test -- -run 'Emit|EventSink|Marshal' ./pkg/plugin/` → FAIL.
- [ ] **Step 2** — swap `event_sink.go:42,115` field/constructor to `hostv1.EmitServiceClient` (`hostv1.NewEmitServiceClient(conn)`); update `EmitEvent`, `RequestEmitToken`, and the new `RegisterEmitType` calls + their `hostv1` request types in `event_marshal.go`.
- [ ] **Step 3** — `task test -- -run 'Emit|EventSink|Marshal' ./pkg/plugin/` → PASS; `task build` → PASS.
- [ ] **Step 4: Commit** — `jj commit -m "refactor(plugin): event sink dials EmitService (holomush-eykuh.1)"`

### Task 9: Rewire `decrypt_client.go` + `audit.go` (audit)

**Files:** Modify `pkg/plugin/decrypt_client.go`, `pkg/plugin/audit.go`; Test `pkg/plugin/audit_test.go`.

- [ ] **Step 1** — test double → `hostv1.RegisterAuditServiceServer`; client → `hostv1.AuditServiceClient`; run `task test -- -run Audit ./pkg/plugin/` → FAIL.
- [ ] **Step 2** — swap `decrypt_client.go:51` field to `hostv1.AuditServiceClient`. For the free function `audit.go:308`, change **only the client parameter type** to `hostv1.AuditServiceClient`; the `AuditRow`/`RowResult` message types stay `pluginv1.*` because Task 2's `audit.proto` **imports** them from `holomush/plugin/v1/audit.proto` rather than redefining them. So the signature becomes `func DecryptOwnAuditRows(ctx context.Context, client hostv1.AuditServiceClient, rows []*pluginv1.AuditRow) ([]*pluginv1.RowResult, error)`. Update its two callers (the decrypt-client wrapper and any test) to pass the `hostv1.AuditServiceClient`.
- [ ] **Step 3** — `task test -- -run Audit ./pkg/plugin/` → PASS; `task build` → PASS.
- [ ] **Step 4: Commit** — `jj commit -m "refactor(plugin): audit decrypt dials AuditService (holomush-eykuh.1)"`

### Task 10: Drop the single `hostClient` in `sdk.go`

**Files:** Modify `pkg/plugin/sdk.go:172-182`; Test `pkg/plugin/` suite.

- [ ] **Step 1** — confirm `sdk.go` no longer needs `var hostClient pluginv1.PluginHostServiceClient`; each capability wrapper now constructs its own client from the broker `conn` (done in Tasks 4–9). Remove the `hostClient` declaration (`sdk.go:172`) and the `NewPluginHostServiceClient(conn)` line (`sdk.go:182`), then update the wrapper-injection block (currently `sdk.go:185-201`, where `hostClient` is passed to each wrapper constructor) so each call passes `conn` to the per-capability constructor instead (e.g. the focus wrapper is built via its `conn`-taking constructor from Task 4). Net: `sdk.go` holds `conn`, not a shared host client.
- [ ] **Step 2** — `task test -- ./pkg/plugin/` → PASS; `task build` → PASS.
- [ ] **Step 3: Commit** — `jj commit -m "refactor(plugin): sdk.go stops constructing PluginHostServiceClient (holomush-eykuh.1)"`

### Task 11: Confirm no residual client callers; fix stale doc comments

`plugins/core-scenes/main.go` and `cmd/holomush/core.go` consume host
capabilities through the **SDK abstraction** (`pluginsdk.FocusClient`,
`pluginsdk.EventSink`), not `PluginHostServiceClient` directly — so they need NO
client rewire (that all happened in Tasks 4–10). They only carry **doc comments**
that name the soon-to-be-deleted `PluginHostService`, which this task corrects.

**Files:**

- Modify: `cmd/holomush/core.go:409` (comment), `plugins/core-scenes/main.go:116` (comment)

- [ ] **Step 1: Confirm zero residual direct client usage**

Run: `rg -n "PluginHostServiceClient|NewPluginHostServiceClient" --type go -g '!*.pb.go' -g '!*.connect.go' plugins/ cmd/`
Expected: ZERO hits (the SDK wrappers in `pkg/plugin/` were the only callers; rewired in Tasks 4–10). If any hit appears, it's a missed Phase 4 caller — rewire it to the per-capability client before proceeding.

- [ ] **Step 2: Update the two stale comments**

`cmd/holomush/core.go:409` ("…the gRPC subsystem (CoreServer + PluginHostService).") and `plugins/core-scenes/main.go:116` ("…drive session focus state via PluginHostService.{JoinFocus,LeaveFocus,…}") — replace the `PluginHostService` mention with the new service name (`FocusService` for the core-scenes focus comment; drop or generalize the core.go mention to "the capability-scoped host services").

- [ ] **Step 3: Build** — `task build` → PASS.
- [ ] **Step 4: Commit** — `jj commit -m "docs(plugin): retire stale PluginHostService comments in non-SDK callers (holomush-eykuh.1)"`

---

## Phase 5: Delete the god-service

### Task 12: Remove `PluginHostService` and prove the rehoming is lossless

**Files:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto` (delete the `service PluginHostService { … }` block, lines 53-225, and any now-orphaned `PluginHostService*` messages whose only use was that service)
- Modify: `internal/plugin/goplugin/host_service.go` (delete `pluginHostServiceServer` + its `RegisterPluginHostServiceServer` line in Task 3's constructor)
- Create: `test/meta/plugin_host_capability_decomp_test.go`
- Regenerate: `buf generate`

- [ ] **Step 1: Pre-flight — confirm nothing dials the god-service**

Run: `rg -n "PluginHostServiceClient|NewPluginHostServiceClient" --type go -g '!*.pb.go' -g '!*.connect.go'`
Expected: ZERO non-generated hits. If any remain, finish Phase 4 first.

- [ ] **Step 2: Write the rehoming-completeness meta-test**

```go
// Verifies: INV-PLUGIN-47
package meta

// TestPluginHostServiceIsDeleted asserts the god-service no longer exists and
// every former RPC is rehomed into a holomush.plugin.host.v1 service (or is the
// explicitly retired Log RPC).
func TestPluginHostServiceIsDeleted(t *testing.T) {
	proto, err := os.ReadFile("../../api/proto/holomush/plugin/v1/plugin.proto")
	require.NoError(t, err)
	assert.NotContains(t, string(proto), "service PluginHostService",
		"PluginHostService must be deleted (spec §5, INV-PLUGIN-47)")

	// Every former RPC resolves to a host.v1 service member under its NEW name
	// (KVGet→Get, KVSet→Set, KVDelete→Delete per the Orientation table; all
	// others keep their name). Log is retired, not rehomed (spec §4, §5).
	// Map: host.v1 RPC name (as written in the .proto) → file it must appear in.
	rehomed := map[string]string{
		"EmitEvent": "emit.proto", "RequestEmitToken": "emit.proto",
		"JoinFocus": "focus.proto", "LeaveFocus": "focus.proto", "LeaveFocusByTarget": "focus.proto",
		"PresentFocus": "focus.proto", "SetConnectionFocus": "focus.proto", "GetConnectionFocus": "focus.proto",
		"AutoFocusOnJoin": "focus.proto", "IsAnyConnFocused": "focus.proto",
		"Evaluate": "eval.proto", "DecryptOwnAuditRows": "audit.proto",
		"QueryStreamHistory": "stream.proto", "AddSessionStream": "stream.proto", "RemoveSessionStream": "stream.proto",
		"ListCommands": "command_registry.proto", "GetCommandHelp": "command_registry.proto",
		"GetSetting": "settings.proto", "SetSetting": "settings.proto",
		"Get": "kv.proto", "Set": "kv.proto", "Delete": "kv.proto",
	}
	for rpcName, file := range rehomed {
		body, err := os.ReadFile(filepath.Join("../../api/proto/holomush/plugin/host/v1", file))
		require.NoError(t, err, "rehome target %s missing", file)
		assert.Contains(t, string(body), "rpc "+rpcName+"(",
			"RPC %s must be declared in host/v1/%s (rehoming, INV-PLUGIN-47)", rpcName, file)
	}
	// The retired Log RPC must NOT reappear anywhere in host.v1.
	for _, file := range []string{"emit.proto", "kv.proto", "stream.proto"} {
		body, err := os.ReadFile(filepath.Join("../../api/proto/holomush/plugin/host/v1", file))
		require.NoError(t, err, "rehome target %s missing", file)
		assert.NotContains(t, string(body), "rpc Log(", "Log is retired, not rehomed (spec §4)")
	}
}
```

(The 22-entry map is the rehoming bijection minus the retired `Log`; the three KV entries use the renamed `Get`/`Set`/`Delete`. If a future change adds/removes a capability RPC, this test forces the map to be updated in lockstep.)

- [ ] **Step 3: Delete the service + server**, run `buf generate`, delete `pluginHostServiceServer`.

- [ ] **Step 4: Sweep stale references** (spec §Testing minor 1)

Run: `rg -n "PluginHostService" --type go -g '!*.pb.go' -g '!*.connect.go'; rg -n "PluginHostService" docs/ api/`
Update or remove each surviving textual reference (the deleted block's own doc comment is gone; check contributor docs).

- [ ] **Step 5: Run the full gate**

Run: `task lint:proto && task test -- ./internal/plugin/... ./pkg/plugin/... ./test/meta/ && task build`
Expected: PASS.

- [ ] **Step 6: Commit** — `jj commit -m "feat(plugin)!: delete PluginHostService god-service; rehoming complete (holomush-eykuh.1)"`

---

## Phase 6: Invariants and registry

### Task 13: Register INV-PLUGIN-47/48/49 and update INV-COMMAND-2

**Files:**

- Modify: `docs/architecture/invariants.yaml`
- Modify: `docs/architecture/invariants.md` (regenerated, never hand-edited)
- Test: `test/meta/invariant_registry_test.go` (drift/binding guards already exist)

- [ ] **Step 1: Add the three entries** after `INV-PLUGIN-46` in `invariants.yaml`:

```yaml
  - id: INV-PLUGIN-47
    scope: INV-PLUGIN
    origin_spec: "docs/superpowers/specs/2026-06-11-plugin-host-capability-decomposition-design.md"
    summary: "Every host-brokered capability function MUST map to exactly one capability-scoped service in
      holomush.plugin.host.v1; no host.v1 service MUST span two capability domains, and PluginHostService MUST NOT exist."
    binding: bound
    asserted_by:
      - "test/meta/plugin_host_capability_decomp_test.go"
  - id: INV-PLUGIN-48
    scope: INV-PLUGIN
    origin_spec: "docs/superpowers/specs/2026-06-11-plugin-host-capability-decomposition-design.md"
    summary: "Ambient runtime substrate (log, new_request_id, stdlib, config) MUST NOT be modeled as a capability:
      it MUST NOT appear in holomush.plugin.host.v1 and MUST NOT be a valid requires capability token."
    binding: bound
    asserted_by:
      - "internal/plugin/capability_vocab_test.go"
  - id: INV-PLUGIN-49
    scope: INV-PLUGIN
    origin_spec: "docs/superpowers/specs/2026-06-11-plugin-host-capability-decomposition-design.md"
    summary: "A capability's RPC contract MUST be the single source both runtimes consume; there MUST NOT be a
      runtime-specific capability surface. Generalizes INV-COMMAND-2 to the whole host-capability surface."
    binding: pending
```

(INV-PLUGIN-49 stays `pending` — no test genuinely asserts cross-runtime single-source until sub-spec 3 wires Lua. Do NOT fabricate a binding; this is a real coverage gap to carry forward. File `bd create -t bug` for it.)

- [ ] **Step 2: Update INV-COMMAND-2's summary** — find the `id: INV-COMMAND-2` entry (`rg -n "id: INV-COMMAND-2" docs/architecture/invariants.yaml`) and in its `summary:` replace `(PluginHostService)` with `(CommandRegistryService)` so it no longer names the deleted service.

- [ ] **Step 3: Regenerate the rendered table**

Run: `go run ./cmd/inv-render`
Expected: `docs/architecture/invariants.md` updated in the generated regions only.

- [ ] **Step 4: Run the registry guards**

Run: `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted|TestInvariantRegistryRenderIsCurrent' ./test/meta/`
Expected: PASS (47 and 48 bound to real assertions; 49 pending with no `asserted_by`).

- [ ] **Step 5: File the INV-PLUGIN-49 coverage bead**

Run: `bd create -t bug "INV-PLUGIN-49 cross-runtime single-source binding (deferred to sub-spec 3 Lua transport)" -p 2 --parent holomush-eykuh`

- [ ] **Step 6: Commit** — `jj commit -m "docs(invariants): register INV-PLUGIN-47/48; update INV-COMMAND-2 for god-service deletion (holomush-eykuh.1)"`

---

## Post-Implementation Checklist

- [ ] `task pr-prep` is green (fast lane: proto lint, unit tests, build).
- [ ] `task test:int` passes — the whole-system plugin tier (`test/integration/wholesystem/`) still loads all in-tree plugins (binary SDK rewire didn't break load).
- [ ] No `PluginHostService` references survive outside generated/historical files (`rg -n PluginHostService --type go -g '!*.pb.go' -g '!*.connect.go'`).
- [ ] `DefaultCapabilityVocabulary` has 14 tokens; ambient names absent.
- [ ] INV-PLUGIN-47/48 bound; INV-PLUGIN-49 pending with a coverage bead filed.
- [ ] Lua hostfunc surface untouched (no diff under `internal/plugin/hostfunc/` except incidental) — sub-spec 3 owns Lua consumption.
- [ ] Spec scope honored: no `access:`/`scope:` enforcement (sub-spec 4), no manifest migration / Lua injection gating (sub-spec 5) crept in.
<!-- adr-capture: sha256=e4ff3e25cdf5d94f; session=cli; ts=2026-06-12T00:10:32Z; adrs=holomush-nbscl,holomush-e9go5,holomush-cryy2,holomush-2fb90 -->
