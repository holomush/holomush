# Phase 8: God-Object Decomposition - Pattern Map

**Mapped:** 2026-07-19
**Files analyzed:** 14 planned new/modified units (2 seam packages, 4 CoreServer units, 3 Manager units, 1 ratchet meta-test, plus per-unit tests)
**Analogs found:** 13 / 14 (one half — the LoC-ceiling ratchet — has NO in-repo analog; stated explicitly below)

> **Note for the planner:** this is a behavior-preserving refactor. For most units the
> "analog" is not aspirational — it is *the current shape of the code being moved*, plus a
> repo-native example of the target shape. Both are given per unit. Every excerpt below was
> read this session at the cited `path:line` in the phase-08 worktree.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/focuscontract/` (Wave 0 seam 1) | neutral leaf package (types only) | transform / contract | `internal/ulidgen/ulidgen.go` | exact (both Phase-7 leaf extractions to break an import edge) |
| manifest-contract seam (Wave 0 seam 2) | neutral contract / interface inversion | request-response | `internal/grpc/character_name_resolver.go` (consumer-defined narrow iface) | role-match |
| `internal/grpc/<stream unit>` | controller sub-unit | streaming | `internal/grpc/auth_handlers.go` + `character_name_resolver.go` | exact / role-match |
| `internal/grpc/<command unit>` | controller sub-unit | request-response | `internal/grpc/refresh_connection.go` | exact |
| `internal/grpc/<lifecycle unit>` | controller sub-unit | event-driven | `internal/grpc/refresh_connection.go` | exact |
| `internal/grpc/<query unit>` | controller sub-unit | CRUD / read | `internal/grpc/list_focus_presence.go`, `refresh_connection.go` | exact |
| `internal/plugin/<loader>` | service (load-time wiring) | batch | `internal/plugin/manager_unload.go` | role-match |
| `internal/plugin/<runtime>` | service (dispatch) | event-driven | `internal/plugin/manager_unload.go` | role-match |
| `internal/plugin/<identity>` | registry / store | CRUD (in-memory map) | `internal/plugin/registry.go` (`ServiceRegistry`) | **exact** |
| `test/meta/phase8_ratchet_test.go` (direction half) | test (meta) | transform | `test/meta/world_import_graph_test.go` | exact |
| `test/meta/phase8_ratchet_test.go` (size half) | test (meta) | file-I/O | **NO ANALOG** — see § No Analog Found | none |
| per-unit CoreServer unit tests | test | request-response | `internal/grpc/character_name_resolver_test.go` | **exact** |
| per-unit Manager unit tests | test | request-response | `internal/grpc/character_name_resolver_test.go` | exact |
| `internal/grpc/server.go`, `internal/plugin/manager.go` (facades, modified) | facade / config | — | current file (option pattern preserved) | n/a (modify-in-place) |

---

## Pattern Assignments

### `internal/focuscontract/` — Wave 0 seam 1 (neutral leaf package, types only)

**Analog:** `internal/ulidgen/ulidgen.go` — Phase 7 extracted this precisely to break an import
edge (`internal/telnet`/`internal/web` → `internal/core`). Same shape, same motivation.

**Package-doc + SPDX convention** (`internal/ulidgen/ulidgen.go:1-8`) — copy this structure
verbatim, substituting the seam being broken:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package ulidgen is the single home for HoloMUSH's monotonic ULID generator.
// It is a dependency-free leaf (its only imports are stdlib + oklog/ulid +
// samber/oops) so gateway packages (internal/telnet, internal/web) can
// generate IDs without importing internal/core (INV-EVENTBUS-1).
package ulidgen
```

Note the convention the planner should mirror: **the package doc names the forbidden edge and
the invariant it serves.** `internal/grpcclient/client.go:4-9` does the same thing for the
other Phase-7 leaf:

```go
// Package grpcclient provides the gRPC client for the Core, Content, and
// SceneAccess services. It is a protocol-translation leaf: proto + grpc-go +
// oops only, with no domain package dependencies, so gateway processes
// (telnet, web) can hold a gRPC client without pulling the CoreServer
// monolith's domain closure into their build graph.
package grpcclient
```

**The 5 declarations that move** (RESEARCH.md § Seam 1; all re-verified this session):

| Symbol | Current location | Consumers needing it |
|---|---|---|
| `Coordinator` (interface, 9 methods) | `internal/grpc/focus/coordinator.go:44` | all 7 `internal/plugin` files |
| `RestorePlan` | `internal/grpc/focus/coordinator.go:108` | `lua/hostcap_adapter.go` only |
| `SetConnectionFocusResult` | `internal/grpc/focus/coordinator.go:116` | `lua/hostcap_adapter.go` only |
| `AutoFocusOnJoinResponse` | `internal/grpc/focus/auto_focus_on_join.go:19` | `lua/hostcap_adapter.go` only |
| `AutoFocusFailure` | `internal/grpc/focus/auto_focus_on_join.go:43` | `lua/hostcap_adapter.go` only |

**Interface-declaration style to preserve** (`internal/grpc/focus/coordinator.go:42-58`) — the
`Coordinator` doc comments are load-bearing (they encode INV-SCENE-* error semantics). Move
them **verbatim** with the interface:

```go
// Coordinator is the sole authoritative mutator of a session's
// focused-context state.
type Coordinator interface {
	JoinFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	LeaveFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	// LeaveFocusByTarget removes the given focus membership from every
	// non-expired session that holds it. Used for cross-session fan-out
	// (e.g., scene end). Returns a LeaveByTargetResult describing the
	// sweep; per-session failures are carried in result.Failed.
	//
	// Error semantics: the returned error covers only the enumeration
	// step (session store ListByFocus). On enumeration failure the
	// result is zero-valued and the error is coded FOCUS_SWEEP_LIST_FAILED.
	LeaveFocusByTarget(ctx context.Context, target session.FocusKey) (session.LeaveByTargetResult, error)
	// ... 6 more methods
}
```

**Dependency budget for the new package:** `context`, `github.com/oklog/ulid/v2`, and
`internal/session`. Verified safe — `internal/session` has no production import of
`internal/grpc` (the `grpc/focus` mentions in `internal/session/session.go` are comments only).

---

### Manifest-contract seam — Wave 0 seam 2

**Analog for the target shape:** `internal/grpc/character_name_resolver.go:15-34` — the repo's
canonical *consumer-defined narrow interface + compile-time assertion + constructor* triple:

```go
// characterNameResolver resolves character display names by ID. Narrow
// seam to keep the ListFocusPresence handler free of world-repo plumbing
// and to make handler tests substitutable.
type characterNameResolver interface {
	Names(ctx context.Context, ids []ulid.ULID) (map[ulid.ULID]string, error)
}

// Compile-time assertion: RepoCharacterNameResolver satisfies characterNameResolver.
var _ characterNameResolver = (*RepoCharacterNameResolver)(nil)

// RepoCharacterNameResolver is the production implementation, backed by
// world.CharacterRepository.GetNamesByIDs.
type RepoCharacterNameResolver struct {
	repo world.CharacterRepository
}

// NewRepoCharacterNameResolver constructs a resolver bound to the given repo.
func NewRepoCharacterNameResolver(repo world.CharacterRepository) *RepoCharacterNameResolver {
	return &RepoCharacterNameResolver{repo: repo}
}
```

**The code being deleted** — `internal/eventbus/authguard/adapter_manifest.go` in full (29
lines; the whole seam). Note both methods are pure delegation with nil-guards; the nil-guard
behavior MUST be preserved wherever the logic lands:

```go
type manifestAdapter struct{ mgr *plugins.Manager }

// NewPluginManifestLookup wraps a *plugins.Manager as a ManifestLookup.
func NewPluginManifestLookup(mgr *plugins.Manager) ManifestLookup {
	return &manifestAdapter{mgr: mgr}
}

func (a *manifestAdapter) PluginRequestsDecryption(pluginName, eventType string) bool {
	if a == nil || a.mgr == nil {
		return false
	}
	return a.mgr.PluginRequestsDecryption(pluginName, eventType)
}

func (a *manifestAdapter) PluginCanReadBack(pluginName, eventType string) bool {
	if a == nil || a.mgr == nil {
		return false
	}
	return a.mgr.PluginCanReadBack(pluginName, eventType)
}
```

`ManifestLookup` already exists in `authguard` (returned at `adapter_manifest.go:13`). The
cheapest cut is **dependency inversion**: have `*Manager` satisfy the existing interface
directly and delete this adapter — no new package needed. This is also the seam that gates
D-08 (see § Shared Patterns → D-08 sequencing).

---

### `internal/grpc/<stream | command | lifecycle | query>` units (controller, D-01/D-02)

**Analog A — the current shape being moved from.** `internal/grpc/refresh_connection.go` is the
whole file (46 lines) and is the cleanest single-method example of the *existing* per-file
split. It shows the receiver style D-02 changes, and the auth preamble that must survive the
move:

```go
// Source: internal/grpc/refresh_connection.go:17-31
// RefreshConnection bumps a connection's liveness lease (holomush-rsoe6).
// Ownership failures collapse to SESSION_NOT_FOUND (enumeration-safe, I-SEC-1).
func (s *CoreServer) RefreshConnection(ctx context.Context, req *corev1.RefreshConnectionRequest) (*corev1.RefreshConnectionResponse, error) {
	if req.GetSessionId() == "" || req.GetConnectionId() == "" {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("session_id and connection_id are required")
	}
	if _, err := auth.ValidateSessionOwnership(
		ctx, s.playerSessionRepo, s.sessionStore,
		req.GetPlayerSessionToken(), req.GetSessionId(),
	); err != nil {
		slog.DebugContext(ctx, "refresh connection ownership validation failed",
			"session_id", req.GetSessionId(), "error", err)
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("session_id", req.GetSessionId()).Errorf("session not found")
	}
```

Three things to carry forward verbatim when relocating any RPC method:
1. **`auth.ValidateSessionOwnership(ctx, <repo>, <store>, token, sessionID)`** — the free
   function in `internal/auth`. It appears at `server.go:405,845,1550,1822`,
   `list_focus_presence.go:47`, `refresh_connection.go:24`. **Do not extract a new
   `sessionResolver` type**; each unit holds the two fields and calls this helper.
2. **`slog.DebugContext(ctx, ...)`** — the `*Context` variant (`.claude/rules/logging.md`,
   `sloglint context: scope`). A mechanical move preserves compliance; a rewrite may not.
3. **`oops.Code("X").With(k, v).Errorf(...)`** — preserve codes byte-identically. Error-code
   changes are behavior changes under D-15.

**Analog B — the D-02 target shape.** `character_name_resolver.go:15-34` (excerpted above) is
already an extracted, constructor-injected, `*CoreServer`-free unit **living in this very
package**. This is the concrete proof that D-02's shape is repo-native, not novel.

**Imports pattern for a new unit file** (`internal/grpc/refresh_connection.go:1-15`) — SPDX
header, then stdlib / third-party / in-repo groups separated by blank lines:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)
```

**The facade to preserve** (`internal/grpc/server.go:234-249`) — `CoreServerOption` is a plain
`func(*CoreServer)`. D-04 keeps this signature; options route into sub-unit constructors
*internally*:

```go
// CoreServerOption configures a CoreServer.
type CoreServerOption func(*CoreServer)

// WithSessionStore sets the session store for the server.
func WithSessionStore(store session.Store) CoreServerOption {
	return func(s *CoreServer) {
		s.sessionStore = store
	}
}
```

> ⚠️ **Struct-field doc comments are load-bearing.** `server.go:154-232` carries fail-closed
> semantics in comments — e.g. `accessEngine` at `:193-195` ("Nil if ABAC is not configured
> (public stream reads will be denied)") and `sceneMute` at `:225-231` ("Nil (unwired) or any
> returned error fails OPEN … INV-SCENE-62"). When a field migrates to a sub-unit struct, the
> comment migrates with it. Dropping it loses the only written record of the fail direction.

---

### `internal/plugin/<loader>` and `<runtime>` units (service, D-05)

**Analog:** `internal/plugin/manager_unload.go` — the one `Manager` method already living in
its own file, and (per RESEARCH.md Pitfall 7) the method that already implements the
idempotent shape. It is also one of the four cross-cutting methods needing an explicit unit
assignment, so the planner will be editing this exact file.

**Lock-discipline pattern to preserve** (`manager_unload.go:22-36`) — short critical section,
explicit `Unlock()` (not `defer`), decision captured into a local, work done outside the lock:

```go
func (m *Manager) UnloadPlugin(ctx context.Context, name string) error {
	// 1. Cache cleanup FIRST and unconditionally.
	m.mu.Lock()
	delete(m.activeByName, name)
	// nameByID intentionally retained for historical resolution.
	host, hostLoaded := m.pluginHosts[name]
	if hostLoaded {
		delete(m.loaded, name)
		delete(m.pluginHosts, name)
	}
	m.mu.Unlock()

	if !hostLoaded {
		return nil // idempotent — no host to unload
	}
```

This shape is why RESEARCH.md's lock finding holds: **no critical section spans an identity/
runtime boundary**, so splitting `m.mu` into three locks introduces no nested acquisition.
Note this single method touches all three future units (`activeByName` = identity,
`loaded`/`pluginHosts` = runtime, `policyInstaller` = load) — preserving the *order* of the
three numbered steps is the behavior-preservation obligation.

**Error-wrapping pattern** (`manager_unload.go:39-50`) — `oops.Code(...).With("plugin", name).Wrap(err)`:

```go
	if err := host.Unload(ctx, name); err != nil {
		return oops.Code("PLUGIN_UNLOAD_HOST").
			With("plugin", name).Wrap(err)
	}
```

---

### `internal/plugin/<identity>` — the D-06 identity registry (registry, exact analog)

**Analog:** `internal/plugin/registry.go` — `ServiceRegistry`, the repo's existing "type owning
its own `sync.RWMutex` over a name-keyed map." Same package, same idiom, directly copyable.

**Full shape to copy** (`internal/plugin/registry.go:12-50`):

```go
// ServiceRegistry maps proto service names to their registered implementations.
// Thread-safe for concurrent registration and resolution.
type ServiceRegistry struct {
	mu       sync.RWMutex
	services map[string]RegisteredService
}

// NewServiceRegistry creates an empty service registry.
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{services: make(map[string]RegisteredService)}
}

// Register adds a service to the registry. Returns an error if the service name is already registered.
func (r *ServiceRegistry) Register(svc RegisteredService) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.services[svc.Name]; exists {
		return oops.Code("SERVICE_ALREADY_REGISTERED").
			With("service", svc.Name).
			Errorf("service %q is already registered", svc.Name)
	}
	r.services[svc.Name] = svc
	return nil
}

// Resolve looks up a service by fully qualified proto name.
func (r *ServiceRegistry) Resolve(name string) (*RegisteredService, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// ...
}
```

**The interface the extracted type must satisfy already exists** —
`internal/plugin/identity_registry.go:19-41` declares `IdentityRegistry` with `NameByID` /
`IDByName`, and its doc comment already names the three populations and the consumers. The new
concrete type implements this; `Manager` delegates. Doc excerpt worth preserving on the new type:

```go
// Source: internal/plugin/identity_registry.go:19-31
type IdentityRegistry interface {
	// NameByID returns the name registered for the given ULID. Resolves
	// THREE populations:
	//   1. Currently-active plugins (rows with gc_at IS NULL).
	//   2. Historically-registered plugins (rows with gc_at IS NOT NULL —
	//      preserved across the registry's lifetime per INV-PLUGIN-17).
	//   3. Compile-time system actor sentinels registered at Manager
	//      bootstrap (e.g., SystemActorULID -> "system", ...).
	NameByID(id ulid.ULID) (name string, ok bool)
```

**The delegation the extraction replaces** (`internal/plugin/manager.go:1825-1839`) — currently
direct field access under `m.mu`; post-split these become one-line forwards to the new type:

```go
// NameByID implements IdentityRegistry.
func (m *Manager) NameByID(id ulid.ULID) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	name, ok := m.nameByID[id]
	return name, ok
}

// IDByName implements IdentityRegistry.
func (m *Manager) IDByName(name string) (ulid.ULID, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.activeByName[name]
	return id, ok
}
```

**The struct comment that D-06 makes obsolete** (`internal/plugin/manager.go:95-104`) — this
comment explicitly names the coupling being broken; it must be rewritten, not left stale:

```go
	// Identity registry: name ↔ ULID maps populated at bootstrap from the
	// plugins table; mutated on load/unload. nameByID resolves three
	// populations (active plugins + historical plugins + system sentinels);
	// activeByName resolves only currently-loaded plugins. Both are
	// guarded by the existing m.mu RWMutex.
	pluginRepo       store.PluginRepo
	nameByID         map[ulid.ULID]string
	activeByName     map[string]ulid.ULID
	retentionDays    int  // plugin row TTL (days); 0 = sweep disabled; default 3
	retentionDaysSet bool // true iff WithRetentionDays was called explicitly
```

---

### `test/meta/phase8_ratchet_test.go` — direction half (D-11/D-12)

**Analog:** `test/meta/world_import_graph_test.go` — production-only import gate. Both the
helper and the assertion loop are directly reusable.

**Package + module constant + the `go/build` walk** (`test/meta/world_import_graph_test.go:1-31`, verbatim):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"go/build"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// modulePath is the Go module path — the prefix of every in-repo import.
const modulePath = "github.com/holomush/holomush"

// worldPkgImports returns the PRODUCTION import list of the package rooted at rel
// (relative to the repo root). build.Package.Imports excludes _test.go files
// (those are TestImports/XTestImports) — so this guards production imports only,
// per round-3 MEDIUM (test files may legitimately hold concrete fixtures).
func worldPkgImports(t *testing.T, root, rel string) []string {
	t.Helper()
	bctx := build.Default
	bctx.CgoEnabled = false
	pkg, err := bctx.ImportDir(filepath.Join(root, rel), 0)
	require.NoErrorf(t, err, "import package %s", rel)
	return pkg.Imports
}
```

> ⚠️ **`modulePath` is already declared in `package meta` at `world_import_graph_test.go:18`.**
> A new file in the same package MUST NOT redeclare it (compile error). Same for
> `worldPkgImports` — either reuse it as-is or give the new helper a distinct name. RESEARCH.md's
> proposed snippet calls a `pkgProductionImports` helper that **does not exist**; the planner must
> either reuse `worldPkgImports` or create that helper.

**Forbidden-edge table + assertion loop** (`world_import_graph_test.go:47-63`, verbatim):

```go
	forbidden := []struct{ fromRel, toRel string }{
		{world, outbox},    // world must not import the relay package
		{world, postgres},  // world must not import the concrete writer package
		{outbox, postgres}, // round-2 second cycle: outbox -> postgres -> outbox
		// ...
	}

	for _, e := range forbidden {
		imports := worldPkgImports(t, root, e.fromRel)
		toPath := modulePath + "/" + e.toRel
		require.NotContainsf(t, imports, toPath,
			"forbidden import edge: %s must NOT import %s (production imports only)", e.fromRel, e.toRel)
	}
```

**Repo-root helper — already exists, do not rewrite** (`test/meta/meta_helpers_test.go:31-49`):

```go
// findRepoRoot walks upward from the test's working directory until it finds
// a directory containing go.mod, which marks the repository root.
func findRepoRoot(t *testing.T) string {
```

**Census / decomposition-assertion analog** (`test/meta/plugin_host_capability_decomp_test.go:1-24`)
— note the file-level `// Verifies:` placement *above* `package`, and the "god-service is gone
+ every member rehomed" two-part structure:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

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

	rehomed := map[string]string{ /* member → destination file */ }
	for rpcName, file := range rehomed {
		body, err := os.ReadFile(filepath.Join("../../api/proto/holomush/plugin/host/v1", file))
		require.NoError(t, err, "rehome target %s missing", file)
		assert.Contains(t, string(body), "rpc "+rpcName+"(",
			"RPC %s must be declared in host/v1/%s (rehoming, INV-PLUGIN-47)", rpcName, file)
	}
}
```

Two reusable conventions: **relative `../../` paths** (not `findRepoRoot`) for direct file
reads, and a **`map[member]destinationFile` census table**. Applied to Phase 8, this becomes
`map[methodName]unitFile` for the CoreServer/Manager method rehoming census.

---

### Per-unit "separately testable" unit tests (SC1/SC2, D-16, D-02's proof)

**Analog — exact, and in the target package:** `internal/grpc/character_name_resolver_test.go`
(45 lines, the whole file). This is precisely the shape D-02 is trying to make possible for
every extracted unit: **construct the narrow type directly with a mock collaborator, no
`CoreServer`, no harness, no integration tag.**

```go
// Source: internal/grpc/character_name_resolver_test.go:1-34
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	grpcpkg "github.com/holomush/holomush/internal/grpc"
	worldmocks "github.com/holomush/holomush/internal/world/worldtest"
)

func TestRepoCharacterNameResolverReturnsNamesForPresentIDs(t *testing.T) {
	repo := worldmocks.NewMockCharacterRepository(t)
	resolver := grpcpkg.NewRepoCharacterNameResolver(repo)

	id1 := ulid.MustParse("01HYXCHAR00000000000000001")
	id2 := ulid.MustParse("01HYXCHAR00000000000000002")
	repo.EXPECT().
		GetNamesByIDs(mock.Anything, []ulid.ULID{id1, id2}).
		Return(map[ulid.ULID]string{id1: "alice", id2: "bob"}, nil).
		Once()

	got, err := resolver.Names(context.Background(), []ulid.ULID{id1, id2})
	require.NoError(t, err)
	assert.Equal(t, "alice", got[id1])
	assert.Equal(t, "bob", got[id2])
}
```

Conventions to copy: `package grpc_test` (external, proving the unit is constructible from
outside), `grpcpkg` import alias, mockery `EXPECT()...Once()`, `require` for preconditions /
`assert` for the check, and ACE test names (`TestXReturnsYWhenZ`). The negative-path sibling at
`:36-45` shows the "no mock expectation = must not call" idiom:

```go
func TestRepoCharacterNameResolverShortCircuitsOnEmptyInput(t *testing.T) {
	repo := worldmocks.NewMockCharacterRepository(t)
	resolver := grpcpkg.NewRepoCharacterNameResolver(repo)
	// No mock expectation — empty input must short-circuit with no repo call.
	...
}
```

**This test is the operational definition of SC1/SC2.** If an extracted unit's test cannot be
written in this shape, the split did not land.

---

## Shared Patterns

### SPDX header — every new `.go` file

**Source:** every file read this session (`internal/ulidgen/ulidgen.go:1-2`, `internal/plugin/registry.go:1-2`, …)
**Apply to:** all new files in Waves 0 / A / B / C

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
```

Applied/verified by `task fmt` (license-eye) and `task license:check`.

### Consumer-defined narrow interface (the D-02 idiom)

**Source:** `internal/grpc/focus/coordinator.go:22-27`
**Apply to:** every extracted CoreServer and Manager sub-unit

```go
// StreamSender delivers stream subscription updates to the live loop.
// Decouples the coordinator from the concrete SessionStreamRegistry
// type in internal/grpc (avoiding an import cycle).
type StreamSender interface {
	Send(sessionID string, stream string, add bool, mode ReplayMode) error
}
```

The convention: **the interface doc names what it decouples from and why.** Paired with the
compile-time assertion `var _ Iface = (*Impl)(nil)` (`character_name_resolver.go:23`).

### Error handling — `oops` codes preserved verbatim

**Source:** `internal/grpc/refresh_connection.go:21,29-30`; `internal/plugin/manager_unload.go:40-41`
**Apply to:** every moved method body

```go
return nil, oops.Code("SESSION_NOT_FOUND").
	With("session_id", req.GetSessionId()).Errorf("session not found")
```

Also preserve the line-scoped nolint idiom where present
(`refresh_connection.go:43`) — never widen `.golangci.yaml` (D-13):

```go
return refreshErr //nolint:wrapcheck // store returns canonical CONNECTION_NOT_FOUND oops code
```

### Context-carrying logging

**Source:** `internal/grpc/refresh_connection.go:27-28`
**Apply to:** every moved method body that logs

```go
slog.DebugContext(ctx, "refresh connection ownership validation failed",
	"session_id", req.GetSessionId(), "error", err)
```

Enforced by `sloglint` `context: scope`. A verbatim move preserves this automatically.

### Options-as-facade

**Source:** `internal/grpc/server.go:234-242`; `internal/plugin/manager.go:107-115`
**Apply to:** `server.go` and `manager.go` facades (D-04, D-07)

Both packages use the identical `type XOption func(*X)` shape. Signatures stay fixed; only the
option *body* changes to route into sub-unit constructors.

### D-08 sequencing gate (cross-wave dependency)

**Source:** `internal/eventbus/authguard/adapter_manifest.go` (the deleted seam) + RESEARCH.md Pitfall 1
**Applies to:** Wave 0 seam 2 → the D-08 decision

Seam 2 deletes the adapter whose test (`internal/eventbus/authguard/adapter_manifest_test.go`,
`package authguard_test`) is the **only** out-of-package `TestLoadPlugin` caller. Sequence
seam 2 first, then **re-enumerate the caller set** before choosing `export_test.go` vs a build
tag. Do not pre-commit either outcome.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| ratchet **size-ceiling** half (`TestPhase8SizeCeilings`) | test (meta) | file-I/O | **No in-repo meta-test measures LoC.** All 17 files in `test/meta/` assert structure (imports, proto members, config keys, invariant bindings) — none counts lines. RESEARCH.md's sketch (§ Code Examples) is new code, not a reuse. The planner should treat it as greenfield within the `test/meta` package conventions (SPDX, `package meta`, `findRepoRoot`, testify `require`), and must reuse the existing `modulePath` / `findRepoRoot` rather than redeclaring them. |
| `pkgProductionImports` helper (cited in RESEARCH.md's proposed ratchet) | test helper | — | **Does not exist.** The real helper is `worldPkgImports` at `test/meta/world_import_graph_test.go:24`. A plan that calls `pkgProductionImports` without creating it will not compile. |

**Verified-absent claims** (stated so the planner does not search for them):
- There is no existing `Manager`-side loader/runtime type to model on — `manager_unload.go` is
  the only extracted-method file in `internal/plugin`, and it holds one method.
- There is no in-repo example of a `CoreServer` sub-unit taking constructor-injected
  collaborators *and* implementing RPC methods. `characterNameResolver` is constructor-injected
  but is a helper, not an RPC handler. The two halves (RPC-method shape from
  `refresh_connection.go`, injection shape from `character_name_resolver.go`) must be combined
  — this composition is genuinely new in this package.

---

## Baseline Measurements (for D-12 ceiling-setting)

Verified this session via `wc -l`:

| File | #4674 baseline | Today (`cce89c702`) | Growth |
|---|---|---|---|
| `internal/grpc/server.go` | 1331 | **1891** | +42% |
| `internal/plugin/manager.go` | 1222 | **1869** | +53% |
| `internal/grpc/auth_handlers.go` | — | **960** | (the existing split-off) |
| `internal/grpc/focus/coordinator.go` | — | 296 | (seam 1 source) |
| `internal/grpc/focus/auto_focus_on_join.go` | — | 213 | (seam 1 source) |

Ceilings are set from **post-split actuals + modest headroom** (Claude's discretion per
CONTEXT.md), not from these numbers.

---

## Metadata

**Analog search scope:** `internal/grpc/`, `internal/grpc/focus/`, `internal/plugin/`,
`internal/eventbus/authguard/`, `internal/ulidgen/`, `internal/grpcclient/`, `test/meta/`
**Files read this session:** 14 (all excerpts above are from files read at the cited lines)
**Pattern extraction date:** 2026-07-19
**Invalidated by:** any merge touching `internal/grpc` or `internal/plugin` — line numbers in
the excerpts will drift. Re-verify citations if `main` moves before planning.
