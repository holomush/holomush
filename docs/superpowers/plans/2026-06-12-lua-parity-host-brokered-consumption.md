<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 HoloMUSH Contributors
-->

# Lua Parity Layer — Host-Brokered Consumption Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Lua plugins one host-brokered mechanism to consume both `host.v1` capabilities and other plugins' services — the same contracts and `BrokerProxy` binary uses — making the proto contract the single source both runtimes consume.

**Architecture:** Ports & adapters. Relocate the `host.v1` server impls behind a runtime-neutral `HostCapabilities` port (Phase 0); the Lua host stands up a per-plugin in-process gRPC server (bufconn) registering those same servers via a `hostfunc.Functions`-backed adapter, and consumes them from Lua through a codegen'd typed bridge (host caps) plus a load-time descriptor-driven bridge that reuses `BrokerProxy` (plugin services).

**Tech Stack:** Go, gRPC, `google.golang.org/grpc/test/bufconn`, `google.golang.org/protobuf/reflect/protoreflect` + `dynamicpb`, gopher-lua (`github.com/yuin/gopher-lua`), the existing `internal/plugin/{goplugin,lua,hostfunc}` packages and `pkg/proto/holomush/plugin/host/v1`.

**Spec:** `docs/superpowers/specs/2026-06-12-lua-parity-host-brokered-consumption-design.md`
**Bead:** `holomush-eykuh.2` (closes `holomush-eykuh.6`; follow-on `holomush-eykuh.9`).

**PR boundaries:** Phase 0 lands as its own PR first (behavior-preserving binary refactor — bakes before net-new code rides on it). Phases 1–4 land as the second PR.

---

## File Structure

| Path | Responsibility | Phase |
| --- | --- | --- |
| `internal/plugin/hostcap/capabilities.go` | the `HostCapabilities` port interface (extracted from server call sites) | 0 |
| `internal/plugin/hostcap/servers.go` | relocated `hostCapabilityBase` + 9 existing server impls (moved from `goplugin`) | 0 |
| `internal/plugin/hostcap/register.go` | `RegisterCapabilities(server, base, set)` shared registration helper + the `CapabilitySet` enum | 0 |
| `internal/plugin/hostcap/property.go` | new `propertyServer` (`PropertyService`) | 0 |
| `internal/plugin/hostcap/session.go` | new `sessionServer` + `sessionAdminServer` (`SessionService`/`SessionAdminService`) | 0 |
| `internal/plugin/hostcap/world.go` | new `worldServer` (`WorldQueryService`) | 0 |
| `internal/plugin/goplugin/host_service.go` | refactor `newPluginHostServiceServer` to call `hostcap.RegisterCapabilities`; `*Host` satisfies the port | 0 |
| `internal/plugin/lua/hostcap_adapter.go` | `hostfunc.Functions`-backed adapter implementing `HostCapabilities` | 1 |
| `internal/plugin/lua/bufconn_endpoint.go` | per-plugin bufconn `*grpc.Server` + `ClientConn` lifecycle on `lua.Host` | 1 |
| `internal/plugin/luabridge/bridge.go` | bridge entry: registers generated host-cap tables + descriptor-driven plugin-service tables into a VM | 2,3 |
| `internal/plugin/luabridge/marshal.go` | Lua table ↔ proto message marshaling (shared by both bridge halves) | 2,3 |
| `internal/plugin/luabridge/gen/main.go` | `go:generate` generator: emits typed host-cap Lua bindings from `host.v1` descriptors | 2 |
| `internal/plugin/luabridge/bindings_gen.go` | generated typed host-cap bindings (DO NOT hand-edit) | 2 |
| `internal/plugin/luabridge/pluginsvc.go` | load-time descriptor-driven plugin-service table builder + `BrokerProxy` loopback | 3 |
| `test/integration/pluginparity/parity_test.go` | cross-runtime INV-PLUGIN-49 + gate INV-PLUGIN-44/45 + identity + coexistence | 4 |

---

## Phase 0 — Runtime-neutral host-capability servers (foundation PR)

### Task 1: Extract the `HostCapabilities` port; make `*goplugin.Host` satisfy it

**Files:**

- Create: `internal/plugin/hostcap/capabilities.go`
- Modify: `internal/plugin/goplugin/host_capability_servers.go` (change `hostCapabilityBase.host` type)
- Test: `internal/plugin/hostcap/capabilities_test.go`

The interface is extracted from the *complete, grounded* inventory of operations the 9 servers call (`rg 's\.host\.\w+|b\.host\.\w+' host_capability_servers.go`). The full set:

| Call site today | Port method | Notes |
| --- | --- | --- |
| `s.host.FocusCoordinator()` | `FocusCoordinator() focus.Coordinator` | already an accessor |
| `s.host.HistoryReader()` | `HistoryReader() plugins.HistoryReader` | already an accessor |
| `s.host.ReadbackDecryptor()` | `ReadbackDecryptor() plugins.ReadbackDecryptor` | already an accessor |
| `s.host.GameSettings()/PlayerSettings()/CharacterSettings()` | same three | already accessors |
| `s.host.mu.RLock(); s.host.engine` | `AccessEngine() types.AccessPolicyEngine` | **accessor locks internally** — the port MUST NOT expose `mu` |
| `s.host.mu.RLock(); s.host.auditor` | `Auditor() pluginauthz.Auditor` | internal lock |
| `s.host.mu.RLock(); s.host.eventEmitter` | `EventEmitter() plugins.PluginIntentEmitter` | internal lock |
| `s.host.mu.RLock(); s.host.commandQuerier` | `CommandQuerier() *commandquery.Querier` | internal lock |
| `s.host.mu.RLock(); s.host.tokenStore` (Evaluate:502, EmitEvent:356, RequestEmitToken:420, actorFromToken:840) | `LookupActor(ctx, pluginName) (core.Actor, string, error)` + `IssueEmitToken(ctx, pluginName, actor) (string, error)` | **do NOT expose `*emitTokenStore`** — expose the two operations the servers need. Binary adapter wires the token store; Lua adapter: `LookupActor`→`core.ActorFromContext`, `IssueEmitToken`→unsupported (no Lua forgery surface) |
| `s.host.identityRegistrySnapshot()` (private) | `IdentityRegistrySnapshot() plugins.IdentityRegistry` | promote to exported interface method; Lua adapter may return nil |
| `s.host.ownedResourceTypes(name)` (private) | `OwnedResourceTypes(pluginName string) []string` | promote to exported |
| new (Tasks 3–5) | `PropertyDefinition(name string) (PropertyDefinition, bool)`, `WorldQuerier(pluginName string) WorldQuerier`, `WorldMutator() WorldMutator`, `SessionAccess() session.Access`, `SessionAdmin() SessionAdmin` | backings for the 3 new servers (see below) |

Type aliases the port uses (declare in `capabilities.go`): `type PropertyDefinition = property.Definition`, `type WorldMutator = world.Mutator`, and a local `WorldQuerier interface { SubjectID() string; GetLocation(...); GetCharacter(...); GetCharactersByLocation(...); GetObject(...) }` matching `*hostfunc.WorldQuerierAdapter` (`adapter.go:18-61`). `SessionAdmin` covers the admin surface core `session.Access` lacks — `BroadcastSystemMessage(ctx, msg) error` and `DisconnectSession(ctx, sessionID, reason) error` (grounded: the `hostfunc.SessionAccess` shim interface in `cap_session.go`); the **binary adapter may return an `Unimplemented`-equivalent for `SessionAdmin` ops (no binary consumer)**, the Lua adapter wires them.

- [ ] **Step 1: Write the failing test** — `internal/plugin/hostcap/capabilities_test.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap_test

import (
	"testing"

	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
)

// TestHostStructSatisfiesHostCapabilitiesPort pins that the binary Host
// implements the runtime-neutral port — the compile-time assertion that keeps
// the two runtimes single-source (INV-PLUGIN-49).
func TestHostStructSatisfiesHostCapabilitiesPort(t *testing.T) {
	var _ hostcap.HostCapabilities = (*goplugin.Host)(nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestHostStructSatisfiesHostCapabilitiesPort ./internal/plugin/hostcap/`
Expected: FAIL — package `hostcap` does not exist / `HostCapabilities` undefined.

- [ ] **Step 3: Define the port** — `internal/plugin/hostcap/capabilities.go`

Define a method-narrow interface. Promote the two private `*Host` methods (`identityRegistrySnapshot`, `ownedResourceTypes`) to exported interface methods. Example shape (enumerate every operation the relocated servers call — derive the full set mechanically from compile errors in Task 2):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package hostcap holds the runtime-neutral holomush.plugin.host.v1 capability
// server implementations. Both the binary (goplugin) and Lua runtimes consume
// these same servers through the HostCapabilities port, satisfying INV-PLUGIN-49
// at the server level (one handler body, no runtime-specific surface).
package hostcap

import (
	"context"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/settings"
)

// HostCapabilities is the narrow port the capability servers depend on instead
// of a concrete *goplugin.Host. Each runtime supplies an adapter. Methods are
// exactly what servers.go + property.go + session.go + world.go call — no more.
type HostCapabilities interface {
	// AccessEngine returns the ABAC engine for Evaluate + capability checks.
	AccessEngine() types.AccessPolicyEngine
	// Auditor returns the plugin-authz auditor (nil ⇒ no audit sink).
	Auditor() pluginauthz.Auditor
	// LookupActor recovers the vouched actor for a dispatch identity. The binary
	// adapter reads the host-issued emit token from ctx metadata; the Lua adapter
	// reads core.ActorFromContext(ctx) (connection-scoped, no token store).
	// pluginName is the host-established calling-plugin identity.
	LookupActor(ctx context.Context, pluginName string) (core.Actor, string, error)
	// SessionAccess returns the session read/update surface.
	SessionAccess() session.Access
	// GameSettings / PlayerSettings / CharacterSettings back GetSetting/SetSetting.
	GameSettings() settings.GameSettings
	PlayerSettings() settings.PlayerSettingsStore
	CharacterSettings() settings.CharacterSettingsStore
	// PropertyDefinition resolves a registry property by name (property.go).
	PropertyDefinition(name string) (PropertyDefinition, bool)
	// WorldQuerier returns the ABAC-subject-stamped world read adapter for the
	// named plugin; WorldMutator the write surface.
	WorldQuerier(pluginName string) WorldQuerier
	WorldMutator() WorldMutator
	SessionAdmin() SessionAdmin
	// IssueEmitToken mints a host dispatch token (binary RequestEmitToken path).
	// Lua adapter returns an unsupported error (no Lua forgery surface).
	IssueEmitToken(ctx context.Context, pluginName string, actor core.Actor) (string, error)
	// EventEmitter / CommandQuerier / IdentityRegistrySnapshot / FocusCoordinator /
	// HistoryReader / ReadbackDecryptor — see the inventory table above; each is a
	// port method whose binary adapter locks h.mu internally.
	EventEmitter() plugins.PluginIntentEmitter
	CommandQuerier() *commandquery.Querier
	IdentityRegistrySnapshot() plugins.IdentityRegistry
	FocusCoordinator() focus.Coordinator
	HistoryReader() plugins.HistoryReader
	ReadbackDecryptor() plugins.ReadbackDecryptor
	// OwnedResourceTypes returns the ABAC resource types the plugin owns.
	OwnedResourceTypes(pluginName string) []string
}
```

> The method set above is the **complete** inventory from the grounded table; verify by compiling `goplugin` in Task 2 with `hostCapabilityBase.host` typed as `HostCapabilities` — zero residual `s.host.<field>` reads should remain. The `mu`-guarded snapshot pattern (`s.host.mu.RLock(); x := s.host.engine; s.host.mu.RUnlock()`) becomes a single `s.host.AccessEngine()` call whose binary-adapter body does the locking internally.

- [ ] **Step 4: Change the embed type** — `host_capability_servers.go:~38`

```go
type hostCapabilityBase struct {
	host       hostcap.HostCapabilities // was: *Host
	pluginName string
}
```

Then make `*Host` satisfy the port. Two parts:

1. **Add accessor methods on `*Host`** for every field previously read directly, each locking internally:

```go
func (h *Host) AccessEngine() types.AccessPolicyEngine { h.mu.RLock(); defer h.mu.RUnlock(); return h.engine }
func (h *Host) Auditor() pluginauthz.Auditor          { h.mu.RLock(); defer h.mu.RUnlock(); return h.auditor }
func (h *Host) EventEmitter() plugins.PluginIntentEmitter { h.mu.RLock(); defer h.mu.RUnlock(); return h.eventEmitter }
func (h *Host) CommandQuerier() *commandquery.Querier { h.mu.RLock(); defer h.mu.RUnlock(); return h.commandQuerier }
// LookupActor + IssueEmitToken read h.tokenStore (the binary forgery defense).
func (h *Host) LookupActor(ctx context.Context, pluginName string) (core.Actor, string, error) { /* move actorFromToken body here, reading h.tokenStore */ }
func (h *Host) IssueEmitToken(ctx context.Context, pluginName string, actor core.Actor) (string, error) { /* current RequestEmitToken token-mint body */ }
// Promote the two private methods to exported (rename call sites):
func (h *Host) IdentityRegistrySnapshot() plugins.IdentityRegistry { /* was identityRegistrySnapshot */ }
func (h *Host) OwnedResourceTypes(pluginName string) []string       { /* was ownedResourceTypes */ }
```

2. **`hostCapabilityBase.actorFromToken`** (`host_capability_servers.go:830`) is NOT renamed onto `*Host` — it stays a `hostCapabilityBase` helper but its body changes to delegate to the port: `return b.host.LookupActor(ctx, b.pluginName)`. The direct `s.host.mu.RLock(); ts := s.host.tokenStore` reads in `evalServer.Evaluate`, `emitServer.EmitEvent`, and `emitServer.RequestEmitToken` become `b.host.LookupActor(...)` / `b.host.IssueEmitToken(...)` calls. (This is why the port exposes the two token *operations*, not the `*emitTokenStore` — the Lua adapter has no token store.)

- [ ] **Step 5: Run test to verify it passes**

Run: `task test -- -run TestHostStructSatisfiesHostCapabilitiesPort ./internal/plugin/hostcap/`
Expected: PASS.

- [ ] **Step 6: Run the full goplugin suite — behavior preservation**

Run: `task test -- ./internal/plugin/goplugin/`
Expected: PASS (no behavior change; servers now reach `*Host` via the port).

- [ ] **Step 7: Commit**

Commit per `references/vcs-preamble.md`: `refactor(plugin): extract HostCapabilities port from host.v1 servers (holomush-eykuh.2)`.

### Task 2: Relocate the 9 servers + `hostCapabilityBase` into `hostcap`; add `RegisterCapabilities`

**Files:**

- Create: `internal/plugin/hostcap/servers.go` (moved bodies), `internal/plugin/hostcap/register.go`
- Modify: `internal/plugin/goplugin/host_capability_servers.go` (delete moved code), `internal/plugin/goplugin/host_service.go` (call the helper)
- Test: `internal/plugin/hostcap/register_test.go`

- [ ] **Step 1: Write the failing test** — `register_test.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap_test

import (
	"testing"

	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/plugin/hostcap"
)

// TestRegisterCapabilitiesRegistersBinaryDefaultSet asserts the helper wires the
// binary default capability set onto a server without panicking and that the
// set excludes Session/Property/World (no binary consumer; spec §1).
func TestRegisterCapabilitiesRegistersBinaryDefaultSet(t *testing.T) {
	srv := grpc.NewServer()
	base := hostcap.NewBase(stubHostCaps(t), "test-plugin")
	hostcap.RegisterCapabilities(srv, base, hostcap.BinaryDefaultSet)
	info := srv.GetServiceInfo()
	if _, ok := info["holomush.plugin.host.v1.SessionService"]; ok {
		t.Fatal("binary default set must not register SessionService")
	}
	if _, ok := info["holomush.plugin.host.v1.EvalService"]; !ok {
		t.Fatal("binary default set must register EvalService")
	}
}
```

(`stubHostCaps(t)` returns a zero-value adapter satisfying `HostCapabilities`; generate with mockery or a hand stub in `register_test.go`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestRegisterCapabilitiesRegistersBinaryDefaultSet ./internal/plugin/hostcap/`
Expected: FAIL — `RegisterCapabilities`/`NewBase`/`BinaryDefaultSet` undefined.

- [ ] **Step 3: Move the server bodies** verbatim into `hostcap/servers.go` (the 9 `*Server` types + `actorFromToken`→`LookupActor` usage + helpers), changing only the package clause to `package hostcap` and the embed (already `hostcap.HostCapabilities`). Export `hostCapabilityBase` as needed via `NewBase`:

```go
func NewBase(host HostCapabilities, pluginName string) hostCapabilityBase {
	return hostCapabilityBase{host: host, pluginName: pluginName}
}
```

- [ ] **Step 4: Write `register.go`** — the shared registration:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"google.golang.org/grpc"

	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// CapabilitySet selects which host.v1 services a per-plugin server registers.
type CapabilitySet int

const (
	// BinaryDefaultSet is the 9 services binary registers today (no
	// Session/Property/World — they have no binary consumer; spec §1).
	BinaryDefaultSet CapabilitySet = iota
	// LuaDefaultSet adds Session/Property/World, which Lua consumes.
	LuaDefaultSet
)

// RegisterCapabilities registers the host.v1 capability servers for the given
// set onto srv. Single source for both runtimes (INV-PLUGIN-49); the only
// per-runtime difference is the set + the adapter inside base.
func RegisterCapabilities(srv *grpc.Server, base hostCapabilityBase, set CapabilitySet) {
	hostv1.RegisterFocusServiceServer(srv, &focusServer{hostCapabilityBase: base})
	hostv1.RegisterEmitServiceServer(srv, &emitServer{hostCapabilityBase: base})
	hostv1.RegisterEvalServiceServer(srv, &evalServer{hostCapabilityBase: base})
	hostv1.RegisterSettingsServiceServer(srv, &settingsServer{hostCapabilityBase: base})
	hostv1.RegisterStreamHistoryServiceServer(srv, &streamHistoryServer{hostCapabilityBase: base})
	hostv1.RegisterStreamSubscriptionServiceServer(srv, &streamSubscriptionServer{hostCapabilityBase: base})
	hostv1.RegisterAuditServiceServer(srv, &auditServer{hostCapabilityBase: base})
	hostv1.RegisterCommandRegistryServiceServer(srv, &commandRegistryServer{hostCapabilityBase: base})
	hostv1.RegisterKVServiceServer(srv, &kvServer{hostCapabilityBase: base})
	if set == LuaDefaultSet {
		hostv1.RegisterSessionServiceServer(srv, &sessionServer{hostCapabilityBase: base})
		hostv1.RegisterSessionAdminServiceServer(srv, &sessionAdminServer{hostCapabilityBase: base})
		hostv1.RegisterPropertyServiceServer(srv, &propertyServer{hostCapabilityBase: base})
		hostv1.RegisterWorldQueryServiceServer(srv, &worldServer{hostCapabilityBase: base})
	}
}
```

(The Session/Property/World registrations reference servers created in Tasks 3–5; until then, guard them behind those tasks or land Task 2 with only the 9 and add the `LuaDefaultSet` branch in Task 5. Recommended: land Tasks 3–5 first as separate files, then this branch compiles.)

- [ ] **Step 5: Refactor `goplugin/host_service.go`** — `newPluginHostServiceServer` becomes:

```go
func newPluginHostServiceServer(host *Host, pluginName string) func([]grpc.ServerOption) *grpc.Server {
	return func(opts []grpc.ServerOption) *grpc.Server {
		server := grpc.NewServer(opts...)
		hostcap.RegisterCapabilities(server, hostcap.NewBase(host, pluginName), hostcap.BinaryDefaultSet)
		return server
	}
}
```

- [ ] **Step 6: Run both suites**

Run: `task test -- ./internal/plugin/hostcap/ ./internal/plugin/goplugin/`
Expected: PASS. Then `task test:int -- ./test/integration/...` smoke for the binary plugin path.

- [ ] **Step 7: Commit** — `refactor(plugin)!: relocate host.v1 servers to hostcap pkg behind the port (holomush-eykuh.2)`.

### Task 3: Implement `propertyServer` (`PropertyService`)

**Files:**

- Create: `internal/plugin/hostcap/property.go`
- Modify: `api/proto/holomush/plugin/host/v1/property.proto` (the doc-comment already claims `propertyServer` exists — now it does; no schema change)
- Test: `internal/plugin/hostcap/property_test.go`

`GetProperty`/`SetProperty` map to the existing `world_write.go` logic: `property.Definition.Get(ctx, querier, entityType, entityID)` and `.Set(ctx, querier, mutator, subjectID, entityType, entityID, value)`. The server reaches these via the port (`PropertyDefinition`, `WorldQuerier`, `WorldMutator`).

- [ ] **Step 1: Write the failing test** — `property_test.go`

```go
// Verifies the new PropertyService server reads through the same
// property.Definition.Get path the Lua holomush.get_property shim uses.
func TestPropertyServerGetPropertyReadsViaDefinition(t *testing.T) {
	caps := newFakeHostCaps(t) // stub: PropertyDefinition("name") → fake def returning "Town Square"
	srv := &propertyServer{hostCapabilityBase: hostcap.NewBase(caps, "core-objects")}
	resp, err := srv.GetProperty(context.Background(), &hostv1.GetPropertyRequest{
		EntityType: "location", EntityId: validULID, Property: "name",
	})
	require.NoError(t, err)
	assert.Equal(t, "Town Square", resp.GetValue())
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestPropertyServerGetPropertyReadsViaDefinition ./internal/plugin/hostcap/`
Expected: FAIL — `propertyServer` undefined.

- [ ] **Step 3: Implement** — `property.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"context"

	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

type propertyServer struct {
	hostv1.UnimplementedPropertyServiceServer
	hostCapabilityBase
}

func (s *propertyServer) GetProperty(ctx context.Context, req *hostv1.GetPropertyRequest) (*hostv1.GetPropertyResponse, error) {
	def, ok := s.host.PropertyDefinition(req.GetProperty())
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "unknown property")
	}
	entityID, err := ulid.Parse(req.GetEntityId()) // property.Definition.Get takes ulid.ULID (registry.go:57)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid entity id")
	}
	querier := s.host.WorldQuerier(s.pluginName)
	val, err := def.Get(ctx, querier, req.GetEntityType(), entityID)
	if err != nil {
		errutil.LogErrorContext(ctx, "property.get failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	return &hostv1.GetPropertyResponse{Value: val}, nil
}

func (s *propertyServer) SetProperty(ctx context.Context, req *hostv1.SetPropertyRequest) (*hostv1.SetPropertyResponse, error) {
	def, ok := s.host.PropertyDefinition(req.GetProperty())
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "unknown property")
	}
	entityID, err := ulid.Parse(req.GetEntityId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid entity id")
	}
	querier := s.host.WorldQuerier(s.pluginName)
	if err := def.Set(ctx, querier, s.host.WorldMutator(), querier.SubjectID(), req.GetEntityType(), entityID, req.GetValue()); err != nil {
		errutil.LogErrorContext(ctx, "property.set failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	return &hostv1.SetPropertyResponse{}, nil
}
```

(Per `.claude/rules/grpc-errors.md`: log internally via `errutil.LogErrorContext`, return `status.Errorf(codes.Internal, "internal error")` — no `%v` leak, `Errorf` static-string is wrapcheck-allowlisted.)

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- -run TestPropertyServer ./internal/plugin/hostcap/`
Expected: PASS.

- [ ] **Step 5: Add the `PropertyService` registration** to `RegisterCapabilities`' `LuaDefaultSet` branch (Task 2 Step 4).

- [ ] **Step 6: Commit** — `feat(plugin): implement host.v1 PropertyService server (holomush-eykuh.2)`.

### Task 4: Implement `sessionServer` + `sessionAdminServer` (`SessionService`/`SessionAdminService`)

**Files:**

- Create: `internal/plugin/hostcap/session.go`
- Test: `internal/plugin/hostcap/session_test.go`

Map RPCs across two port surfaces. `SessionService` (reads + whisper) → `session.Access` (grounded, `session.go:284`): `FindByName`→`FindByCharacterName`, `ListActive`→`ListActive`, `SetLastWhispered`→`UpdateLastWhispered`. `SessionAdminService` (broadcast/disconnect) → the `SessionAdmin` port method (`BroadcastSystemMessage`, `DisconnectSession`), because core `session.Access` has **no** broadcast/disconnect — those live on the `hostfunc.SessionAccess` shim surface (`cap_session.go`). The **binary adapter returns `Unimplemented` for `SessionAdminService` ops (no binary consumer)**; the Lua adapter wires them via `hostfunc`. Split read RPCs (`SessionService`) from admin RPCs (`SessionAdminService`) per the two proto services.

- [ ] **Step 1: Write the failing test** — `session_test.go`

```go
// Verifies SessionService.FindByName resolves through session.Access.FindByCharacterName.
func TestSessionServerFindByNameResolvesViaAccess(t *testing.T) {
	caps := newFakeHostCaps(t) // session.Access stub: FindByCharacterName("alice") → &session.Info{ID:"s1",...}
	srv := &sessionServer{hostCapabilityBase: hostcap.NewBase(caps, "core-communication")}
	resp, err := srv.FindByName(context.Background(), &hostv1.FindByNameRequest{Name: "alice"})
	require.NoError(t, err)
	assert.Equal(t, "s1", resp.GetSession().GetId())
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestSessionServerFindByName ./internal/plugin/hostcap/`
Expected: FAIL — `sessionServer` undefined.

- [ ] **Step 3: Implement** — `session.go` (read service shown; admin service mirrors with the mutate RPCs):

```go
type sessionServer struct {
	hostv1.UnimplementedSessionServiceServer
	hostCapabilityBase
}

func (s *sessionServer) FindByName(ctx context.Context, req *hostv1.FindByNameRequest) (*hostv1.FindByNameResponse, error) {
	info, err := s.host.SessionAccess().FindByCharacterName(ctx, req.GetName())
	if err != nil {
		errutil.LogErrorContext(ctx, "session.find_by_name failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	if info == nil {
		return &hostv1.FindByNameResponse{}, nil // not-found ⇒ empty session
	}
	return &hostv1.FindByNameResponse{Session: sessionInfoToProto(info)}, nil
}

func (s *sessionServer) ListActive(ctx context.Context, _ *hostv1.ListActiveRequest) (*hostv1.ListActiveResponse, error) {
	infos, err := s.host.SessionAccess().ListActive(ctx)
	if err != nil {
		errutil.LogErrorContext(ctx, "session.list_active failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	out := make([]*hostv1.SessionInfo, 0, len(infos))
	for _, i := range infos {
		out = append(out, sessionInfoToProto(i))
	}
	return &hostv1.ListActiveResponse{Sessions: out}, nil
}

// sessionInfoToProto maps the host session.Info to the wire SessionInfo.
func sessionInfoToProto(i *session.Info) *hostv1.SessionInfo {
	return &hostv1.SessionInfo{
		Id: i.ID, CharacterId: i.CharacterID.String(), CharacterName: i.CharacterName,
		LocationId: locationIDString(i), GridPresent: i.GridPresent, LastWhispered: i.LastWhispered,
	}
}
```

(`sessionAdminServer` implements `SetLastWhispered`→`UpdateLastWhispered`, `Broadcast`, `Disconnect`→`DeleteByCharacter`. Confirm `session.Info` field names against `internal/session/session.go` `type Info struct` when wiring `sessionInfoToProto`.)

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- -run TestSessionServer ./internal/plugin/hostcap/`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(plugin): implement host.v1 SessionService/SessionAdminService servers (holomush-eykuh.2)`.

### Task 5: Implement `worldServer` (`WorldQueryService`) + enable `LuaDefaultSet`

**Files:**

- Create: `internal/plugin/hostcap/world.go`
- Modify: `internal/plugin/hostcap/register.go` (the `LuaDefaultSet` branch now compiles — all 4 servers exist)
- Test: `internal/plugin/hostcap/world_test.go`

Map `QueryLocation`/`QueryCharacter`/`QueryLocationCharacters`/`QueryObject` to `WorldQuerier(pluginName)` (`GetLocation`/`GetCharacter`/`GetCharactersByLocation`/`GetObject`), which stamps `plugin:<name>` as the ABAC subject (grounded: `adapter.go` `WorldQuerierAdapter`).

- [ ] **Step 1: Write the failing test** — `world_test.go`

```go
// Verifies WorldQueryService.QueryLocation reads through the plugin-subject-stamped querier.
func TestWorldServerQueryLocationStampsPluginSubject(t *testing.T) {
	caps := newFakeHostCaps(t) // WorldQuerier("core-scenes").GetLocation(...) records the subjectID it used
	srv := &worldServer{hostCapabilityBase: hostcap.NewBase(caps, "core-scenes")}
	_, err := srv.QueryLocation(context.Background(), &hostv1.QueryLocationRequest{LocationId: validULID})
	require.NoError(t, err)
	assert.Equal(t, "plugin:core-scenes", caps.lastWorldSubject)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestWorldServerQueryLocation ./internal/plugin/hostcap/`
Expected: FAIL — `worldServer` undefined.

- [ ] **Step 3: Implement** `world.go` (QueryLocation shown; the other three mirror with their request/response types), parsing the ULID with the existing helper, returning `status.Errorf(codes.NotFound, "not found")` on `world.ErrNotFound`, `codes.Internal` otherwise.

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- -run TestWorldServer ./internal/plugin/hostcap/`
Expected: PASS.

- [ ] **Step 5: Run the whole foundation suite**

Run: `task test -- ./internal/plugin/hostcap/ ./internal/plugin/goplugin/` then `task lint`
Expected: PASS — Phase 0 (foundation PR) is complete and behavior-preserving for binary.

- [ ] **Step 6: Commit** — `feat(plugin): implement host.v1 WorldQueryService server + enable LuaDefaultSet (holomush-eykuh.2)`.

> **Land Phase 0 as its own PR** (`task pr-prep`, review, merge) before starting Phase 1.

---

## Phase 1 — Per-plugin Lua bufconn endpoint + adapter (bridge PR)

### Task 6: `hostfunc.Functions`-backed `HostCapabilities` adapter

**Files:**

- Create: `internal/plugin/lua/hostcap_adapter.go`
- Test: `internal/plugin/lua/hostcap_adapter_test.go`

The adapter wraps `*hostfunc.Functions` (which already holds `engine`, `auditor`, `sessionAccess`, `propertyRegistry`, `worldMutator`, settings ops, etc.) and satisfies `hostcap.HostCapabilities`. `LookupActor` reads `core.ActorFromContext(ctx)` (grounded: `hostfunc/stdlib_settings.go:76`) — the connection-scoped identity equivalent, no token store. `IdentityRegistry`-style methods may return zero values (Lua has no emit-token forgery surface).

- [ ] **Step 1: Write the failing test**

```go
// TestLuaAdapterSatisfiesHostCapabilities pins the compile-time contract.
func TestLuaAdapterSatisfiesHostCapabilities(t *testing.T) {
	var _ hostcap.HostCapabilities = (*luaHostCapAdapter)(nil)
}

// TestLuaAdapterLookupActorUsesContextActor verifies Lua identity comes from
// core.ActorFromContext, not a dispatch token (spec §0).
func TestLuaAdapterLookupActorUsesContextActor(t *testing.T) {
	a := newLuaHostCapAdapter(newTestFunctions(t))
	ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorKindPlugin, ID: "echo-bot"})
	actor, subj, err := a.LookupActor(ctx, "echo-bot")
	require.NoError(t, err)
	assert.Equal(t, "echo-bot", actor.ID)
	assert.Equal(t, "plugin:echo-bot", subj)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestLuaAdapter ./internal/plugin/lua/`
Expected: FAIL — `luaHostCapAdapter` undefined.

- [ ] **Step 3: Implement** the adapter — each method delegates to the corresponding `Functions` field/accessor; `LookupActor` uses `core.ActorFromContext`. (Add exported accessors on `hostfunc.Functions` where fields are unexported, e.g. `func (f *Functions) Engine() types.AccessPolicyEngine { return f.engine }`.)

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- -run TestLuaAdapter ./internal/plugin/lua/`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(plugin): hostfunc.Functions adapter for HostCapabilities port (holomush-eykuh.2)`.

### Task 7: Per-plugin bufconn endpoint lifecycle on `lua.Host`

**Files:**

- Create: `internal/plugin/lua/bufconn_endpoint.go`
- Modify: `internal/plugin/lua/host.go` (`luaPlugin` struct gains a `*pluginEndpoint`; `Load` creates it, `Close`/unload tears it down)
- Test: `internal/plugin/lua/bufconn_endpoint_test.go`

The endpoint is created **once per plugin at Load** (not per VM — VMs are created/destroyed per `DeliverEvent`). It builds a `*grpc.Server` via `hostcap.RegisterCapabilities(srv, hostcap.NewBase(adapter, pluginName), hostcap.LuaDefaultSet)`, wraps it with `plugins.NewInProcessConn(srv)`, and stores the resulting `grpc.ClientConnInterface` on the plugin for the bridge to capture.

- [ ] **Step 1: Write the failing test**

```go
// TestPluginEndpointServesHostCapsOverBufconn stands up an endpoint and calls a
// host.v1 cap over the in-process conn, asserting the round trip works.
func TestPluginEndpointServesHostCapsOverBufconn(t *testing.T) {
	adapter := newLuaHostCapAdapter(newTestFunctions(t)) // KV-backed
	ep, err := newPluginEndpoint(adapter, "echo-bot")
	require.NoError(t, err)
	defer ep.Close()
	client := hostv1.NewKVServiceClient(ep.Conn())
	_, err = client.Set(context.Background(), &hostv1.SetRequest{Key: "k", Value: "v"})
	require.NoError(t, err)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestPluginEndpointServesHostCaps ./internal/plugin/lua/`
Expected: FAIL — `newPluginEndpoint` undefined.

- [ ] **Step 3: Implement** `bufconn_endpoint.go`:

```go
type pluginEndpoint struct {
	conn *plugins.InProcessConn
}

func newPluginEndpoint(adapter hostcap.HostCapabilities, pluginName string) (*pluginEndpoint, error) {
	srv := grpc.NewServer()
	hostcap.RegisterCapabilities(srv, hostcap.NewBase(adapter, pluginName), hostcap.LuaDefaultSet)
	conn, err := plugins.NewInProcessConn(srv)
	if err != nil {
		return nil, oops.In("lua").With("plugin", pluginName).Wrap(err)
	}
	return &pluginEndpoint{conn: conn}, nil
}

// Conn returns the in-process client conn. *InProcessConn already satisfies
// grpc.ClientConnInterface directly (it implements Invoke/NewStream/Close —
// verified internal/plugin/inprocess_conn.go:62,67,73), so no accessor on the
// struct is needed; the generated clients take it as-is.
func (e *pluginEndpoint) Conn() grpc.ClientConnInterface { return e.conn }
func (e *pluginEndpoint) Close() error                   { return e.conn.Close() }
```

- [ ] **Step 4: Wire lifecycle in `host.go`** — add `endpoint *pluginEndpoint` to `luaPlugin`; in `Load` (after manifest parse) call `newPluginEndpoint(h.hostCapAdapter, name)` and store it; in `Close`/unload call `p.endpoint.Close()`. Guard nil (`h.hostFuncs == nil` ⇒ no endpoint).

- [ ] **Step 5: Run to verify it passes**

Run: `task test -- -run TestPluginEndpoint ./internal/plugin/lua/`
Expected: PASS.

- [ ] **Step 6: Commit** — `feat(plugin): per-plugin Lua bufconn endpoint serving host.v1 caps (holomush-eykuh.2)`.

---

## Phase 2 — Host-capability bridge (codegen typed bindings)

### Task 8: Codegen generator for typed host-cap Lua bindings

**Files:**

- Create: `internal/plugin/luabridge/marshal.go`, `internal/plugin/luabridge/gen/main.go`, `internal/plugin/luabridge/doc.go` (with `//go:generate go run ./gen`)
- Create (generated): `internal/plugin/luabridge/bindings_gen.go`
- Test: `internal/plugin/luabridge/marshal_test.go`, `internal/plugin/luabridge/bindings_gen_test.go`

The generator reads the `host.v1` service descriptors (via the generated `hostv1` package's `proto.FileDescriptor`s) and emits, per service, a Go function that registers a `namespace.Method{…}` Lua table whose functions: read the Lua table arg → build the typed `*hostv1.<Method>Request` field-by-field → call `hostv1.New<Svc>ServiceClient(conn).<Method>` → marshal the typed response into a Lua table. A drift gate (sha256, like `schemas/plugin.schema.json`) keeps `bindings_gen.go` honest.

- [ ] **Step 1: Write the failing test (marshaling primitive first)** — `marshal_test.go`

```go
// TestProtoToLuaTableMapsScalarFields round-trips a typed message into a Lua table.
func TestProtoToLuaTableMapsScalarFields(t *testing.T) {
	L := lua.NewState(); defer L.Close()
	tbl := luabridge.ProtoToLuaTable(L, &hostv1.GetPropertyResponse{Value: "Town Square"})
	assert.Equal(t, "Town Square", L.GetField(tbl, "value").String())
}

// TestLuaTableToProtoBuildsTypedRequest builds a typed request from a Lua table.
func TestLuaTableToProtoBuildsTypedRequest(t *testing.T) {
	L := lua.NewState(); defer L.Close()
	arg := L.NewTable()
	L.SetField(arg, "entity_type", lua.LString("location"))
	L.SetField(arg, "entity_id", lua.LString(validULID))
	L.SetField(arg, "property", lua.LString("name"))
	var req hostv1.GetPropertyRequest
	require.NoError(t, luabridge.LuaTableToProto(arg, &req))
	assert.Equal(t, "location", req.GetEntityType())
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run 'TestProtoToLuaTable|TestLuaTableToProto' ./internal/plugin/luabridge/`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement `marshal.go`** — `ProtoToLuaTable` / `LuaTableToProto` via `protoreflect` (walk `msg.ProtoReflect().Descriptor().Fields()`; map scalar/string/bool/message/repeated). This is the shared marshaler both bridge halves use. Also add a `luabridge`-local `luaContext(L *lua.LState) context.Context` helper (the `hostfunc.luaContext` at `cap_session.go:193` is package-private to `hostfunc`; the generated bindings in `package luabridge` need their own copy) and `pushBridgeError(L, err)` (gRPC `status` → `nil, "<message>"`, mirroring `hostfunc.pushError`).

- [ ] **Step 4: Run to verify the marshaler passes**

Run: `task test -- -run 'TestProtoToLuaTable|TestLuaTableToProto' ./internal/plugin/luabridge/`
Expected: PASS.

- [ ] **Step 5: Write `gen/main.go`** — emit `bindings_gen.go`. Representative generated output (what the generator MUST produce for `KVService`):

```go
// Code generated by luabridge/gen. DO NOT EDIT.
package luabridge

func registerKV(L *lua.LState, conn grpc.ClientConnInterface, pluginName string) {
	tbl := L.NewTable()
	client := hostv1.NewKVServiceClient(conn)
	L.SetField(tbl, "Get", L.NewFunction(func(L *lua.LState) int {
		var req hostv1.GetRequest
		if err := LuaTableToProto(L.CheckTable(1), &req); err != nil {
			return pushBridgeError(L, err)
		}
		resp, err := client.Get(luaContext(L), &req)
		if err != nil {
			return pushBridgeError(L, err) // gRPC status → nil, "<msg>"
		}
		L.Push(ProtoToLuaTable(L, resp)); L.Push(lua.LNil); return 2
	}))
	// … Set, Delete …
	L.SetGlobal("kv", tbl)
}

// registeredHostCapBindings maps CAPABILITY TOKEN (the vocab token a manifest
// declares as `requires: - capability: <token>`) → registrar. Keyed by token,
// NOT service name, because the manifest gate is RequiredCapabilities() (capability
// kind), not RequiredServiceNames() (service kind). Tokens are the capability_vocab.go
// set: kv, session, session.admin, property, world.query, world.mutation, focus,
// eval, emit, settings, stream.history, stream.subscription, audit, command-registry.
var registeredHostCapBindings = map[string]func(*lua.LState, grpc.ClientConnInterface, string){
	"kv": registerKV,
	// … all capability tokens that have a host.v1 service …
}
```

> The generator derives the token for each service from the `capability_vocab.go` mapping (e.g. `KVService`→`kv`, `SessionService`→`session`, `WorldQueryService`→`world.query`). If the canonical token↔service map does not yet exist as a single source, add it to `capability_vocab.go` and have both the resolver and this generator read it (avoids a second mapping that could drift).

- [ ] **Step 6: Generate + verify** — Run: `go generate ./internal/plugin/luabridge/` then `task test -- ./internal/plugin/luabridge/`
Expected: `bindings_gen.go` written; PASS. Add a sha256 drift gate for `bindings_gen.go` mirroring the existing `schemas/plugin.schema.json` gate (grounded: that gate runs in `task pr-prep`; follow its `Taskfile.yaml` target pattern — regenerate, `git diff --exit-code` the generated file — so CI fails on un-regenerated drift).

- [ ] **Step 7: Commit** — `feat(plugin): codegen typed Lua bindings for host.v1 capabilities (holomush-eykuh.2)`.

### Task 9: Bridge injection into the VM (coexisting, fixture-gated)

**Files:**

- Create: `internal/plugin/luabridge/bridge.go`
- Modify: `internal/plugin/lua/host.go` (`DeliverEvent` injects the bridge for plugins using the new path)
- Test: `internal/plugin/luabridge/bridge_test.go`

Coexistence (spec §5): the new bridge MUST NOT double-inject a global the legacy `cap_*.go` already injects. In this sub-spec the new path is **opt-in** — only the fixture plugin (Phase 4) routes through it. Gate on an explicit per-plugin flag (e.g. a manifest marker or a host-set allowlist used only by the test harness); production plugins keep the legacy injection untouched.

- [ ] **Step 1: Write the failing test**

```go
// TestBridgeRegistersDeclaredHostCapsOnly injects the bridge for a plugin
// declaring only the `kv` CAPABILITY and asserts `kv` is present, `session` absent.
func TestBridgeRegistersDeclaredHostCapsOnly(t *testing.T) {
	L := lua.NewState(); defer L.Close()
	// declared = capability tokens (manifest RequiredCapabilities()), not service names.
	luabridge.RegisterHostCaps(L, conn, "echo-bot", []string{"kv"})
	assert.Equal(t, lua.LTTable, L.GetGlobal("kv").Type())
	assert.Equal(t, lua.LTNil, L.GetGlobal("session").Type())
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestBridgeRegistersDeclaredHostCapsOnly ./internal/plugin/luabridge/`
Expected: FAIL — `RegisterHostCaps` undefined.

- [ ] **Step 3: Implement `bridge.go`** — `RegisterHostCaps(L, conn, pluginName, declaredCaps []string)` iterates `declaredCaps` (capability tokens), looks up `registeredHostCapBindings[token]`, calls the registrar (gated by declaration = INV-PLUGIN-44/45 at the shared path).

- [ ] **Step 4: Wire `DeliverEvent`** — after `h.hostFuncs.Register(...)`, if the plugin opts into the new path, call `luabridge.RegisterHostCaps(L, p.endpoint.Conn(), name, p.manifest.RequiredCapabilities())`. **Use `RequiredCapabilities()`, NOT `RequiredServiceNames()`** — host capabilities are declared as `requires: - capability: <token>` (`DependencyCapability` kind); `RequiredServiceNames()` (`manifest.go:150`) filters to `DependencyService` and would return an empty slice for a correctly-declared host-cap consumer, injecting zero bindings and silently defeating the gate. `RequiredServiceNames()` is still correct for the plugin→plugin path (Task 10), which consumes `service:` deps.

- [ ] **Step 5: Run to verify it passes**

Run: `task test -- -run TestBridge ./internal/plugin/luabridge/`
Expected: PASS.

- [ ] **Step 6: Commit** — `feat(plugin): inject codegen'd host-cap bridge into Lua VMs, declaration-gated (holomush-eykuh.2)`.

---

## Phase 3 — Plugin→plugin consumption

### Task 10: Load-time descriptor-driven plugin-service tables + `BrokerProxy` loopback

**Files:**

- Create: `internal/plugin/luabridge/pluginsvc.go`
- Test: `internal/plugin/luabridge/pluginsvc_test.go`

For each `requires: service: <Name>` a Lua plugin declares, build a `namespace.Method{…}` Lua table at load time from the provider's registered `protoreflect.ServiceDescriptor`; each function marshals the Lua table → `dynamicpb.Message`, invokes the method over a loopback conn whose server end is the **existing** `BrokerProxy` to the provider, and marshals the dynamic response back. Validate method existence at build (fail-early, spec §2).

- [ ] **Step 1: Write the failing test**

```go
// TestPluginServiceTableInvokesViaBrokerProxy builds a table for a fake provider
// service and asserts a call reaches the provider through BrokerProxy.
func TestPluginServiceTableInvokesViaBrokerProxy(t *testing.T) {
	provider := newFakeProvider(t) // registers a 1-RPC service + its descriptor
	L := lua.NewState(); defer L.Close()
	require.NoError(t, luabridge.RegisterPluginService(L, provider.Conn(), provider.Descriptor(), "echo-bot"))
	// call echo.Echo{message="hi"} from Lua, assert provider saw "hi"
	require.NoError(t, L.DoString(`local r = echo.Echo{message="hi"}; assert(r.reply == "hi")`))
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestPluginServiceTable ./internal/plugin/luabridge/`
Expected: FAIL — `RegisterPluginService` undefined.

- [ ] **Step 3: Implement `pluginsvc.go`** — descriptor-walk to synthesize the table; `dynamicpb.NewMessage(methodDesc.Input())` for requests; invoke via `conn.Invoke(ctx, "/"+svc+"/"+method, req, resp)`; reuse `marshal.go` for table↔dynamicpb. The `conn` is the loopback to `BrokerProxy` (built by the host at load, mirroring `goplugin/host.go`'s `NewBrokerProxy(svc.Conn, pluginName)`).

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- -run TestPluginServiceTable ./internal/plugin/luabridge/`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(plugin): load-time descriptor-driven Lua plugin-service bridge via BrokerProxy (holomush-eykuh.2)`.

---

## Phase 4 — Invariant binding + coexistence tests

### Task 11: Cross-runtime parity test — bind INV-PLUGIN-49

**Files:**

- Create: `test/integration/pluginparity/parity_test.go` (build tag `//go:build integration`)
- Modify: `docs/architecture/invariants.yaml` (`INV-PLUGIN-49` → `binding: bound`, `asserted_by`), then `go run ./cmd/inv-render`

Use `kv` (no dispatch-token requirement, unlike `eval`/`emit`) so both runtimes reach the same `kvServer` handler.

- [ ] **Step 1: Write the test** — same `kvServer` reached by a Lua consumer (bufconn) and a binary consumer (broker); assert identical result + host-derived subject. Annotate:

```go
// Verifies: INV-PLUGIN-49
func TestKVCapabilityIsSingleSourceAcrossRuntimes(t *testing.T) { /* … */ }
```

- [ ] **Step 2: Run** — Run: `task test:int -- ./test/integration/pluginparity/`
Expected: PASS.

- [ ] **Step 3: Bind the invariant** — set `binding: bound` + `asserted_by: [test/integration/pluginparity/parity_test.go]`; `go run ./cmd/inv-render`; `task fmt`.

- [ ] **Step 4: Verify binding** — Run: `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`
Expected: PASS. Then `bd close holomush-eykuh.6`.

- [ ] **Step 5: Commit** — `test(plugin): bind INV-PLUGIN-49 cross-runtime single-source (holomush-eykuh.2, closes holomush-eykuh.6)`.

### Task 12: Gate tests — bind INV-PLUGIN-44 / INV-PLUGIN-45

**Files:**

- Modify: `test/integration/pluginparity/parity_test.go`
- Modify: `docs/architecture/invariants.yaml` (44, 45 → bound)

- [ ] **Step 1: Write the tests** — (a) a declared cap/service is reachable from Lua; (b) an **undeclared** one is not reachable (gate); (c) the shared resolver/registry gate denies Lua and binary identically. Annotate `// Verifies: INV-PLUGIN-44` and `// Verifies: INV-PLUGIN-45`.

- [ ] **Step 2: Run** — Run: `task test:int -- ./test/integration/pluginparity/`
Expected: PASS.

- [ ] **Step 3: Bind** 44 + 45; `go run ./cmd/inv-render`; `task fmt`; re-run the meta binding tests (Task 11 Step 4).

- [ ] **Step 4: Commit** — `test(plugin): bind INV-PLUGIN-44/45 declaration-gated consumption (holomush-eykuh.2)`.

### Task 13: Identity/anti-forgery + coexistence

**Files:**

- Modify: `test/integration/pluginparity/parity_test.go`
- Test: `internal/plugin/lua/host_test.go` (coexistence)

- [ ] **Step 1: Write the anti-forgery test** — assert the ABAC subject the host server observes is `plugin:<fixtureName>` regardless of any value the Lua code attempts to supply (no wire-supplied subject; INV-PLUGIN-22).

- [ ] **Step 2: Write the coexistence test** — a plugin on the legacy `cap_*.go` path still has its globals injected unchanged when the new bridge exists (no regression; spec §5).

- [ ] **Step 3: Run** — Run: `task test:int -- ./test/integration/pluginparity/` and `task test -- ./internal/plugin/lua/`
Expected: PASS.

- [ ] **Step 4: Full gate** — Run: `task pr-prep`
Expected: green (Phase 1–4 bridge PR complete).

- [ ] **Step 5: Commit** — `test(plugin): Lua identity anti-forgery + legacy-shim coexistence (holomush-eykuh.2)`.

---

## Notes for the implementer

- **Search precedence** (`.claude/rules/search-tools.md`): `mcp__probe__search_code`/`extract_code` for Go symbols; `rg` for text; `ast-grep` for structural. Read with offset/limit.
- **gRPC errors** (`.claude/rules/grpc-errors.md`): never `status.Errorf(codes.Internal, "...: %v", err)`; log via `errutil.LogErrorContext`, return `status.Errorf(codes.Internal, "internal error")`.
- **Logging** (`.claude/rules/logging.md`): `*Context` slog variants whenever a `ctx` is in scope.
- **Runtime symmetry** (`.claude/rules/plugin-runtime-symmetry.md`): the whole point — any gate at the shared `hostcap`/resolver path applies to both runtimes; the only runtime-specific code is the adapter (`LookupActor` token-vs-context) where a real forgery surface differs.
- **`//nolint`**: line-scoped only with a reason; never widen `.golangci.yaml`.
- **Schema/codegen drift**: `bindings_gen.go` is generated — `go generate ./internal/plugin/luabridge/`, never hand-edit; add the sha256 gate.
- **Run `task test:int`** after any `plugin.yaml`/`requires` fixture change — unit resolver tests stay green while only the integration suite exercises injection (memory `5eead5f0`, the core-aliases lesson).
<!-- adr-capture: sha256=394df9f785a2a6e6; session=cli; ts=2026-06-12T13:12:55Z; adrs=holomush-elqw4,holomush-ws2mi,holomush-l5bqb -->
