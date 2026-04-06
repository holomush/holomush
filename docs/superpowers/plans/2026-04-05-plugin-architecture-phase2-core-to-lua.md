# Plugin Architecture Phase 2: Core-to-Lua Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate all five core plugins from compiled-in Go (`type: core`) to Lua (`type: lua`), removing the explicit handler registration imports from the plugin subsystem. Along the way, renovate the hostfunc system to use modular capability injection aligned with proto service contracts.

**Architecture:** Restructure the hostfunc system into capability modules (world, session, alias, property, command) that are injected into the Lua VM based on manifest `requires` declarations. Add missing bindings, remove cruft, then rewrite each plugin as Lua. Finally, remove `type: core` and the `LocalPluginHost` explicit registration pattern.

**Tech Stack:** Go 1.25, gopher-lua, testify

**Spec:** `docs/superpowers/specs/2026-04-05-plugin-architecture-rework-design.md` (Sections 1.3, 7)

**Depends on:** Phase 1 infrastructure (service registry, manifest schema with `requires`)

---

## Scope Note

This plan intentionally includes hostfunc renovation alongside plugin migration. The two are inseparable — migrating plugins to Lua without fixing the hostfunc system would mean bolting more methods onto an already overgrown god object.

**VM pooling** (P0 perf issue) is out of scope — it's orthogonal to migration and can be done independently. Noted as future work.

---

## Renovation Items Addressed

| Issue | Resolution |
|-------|-----------|
| Functions struct as god object | Decompose into capability modules |
| No function scoping | Inject modules based on manifest `requires` |
| Duplicate error sanitization | Consolidate into single helper |
| Dead WorldQuerier interface | Remove |
| WithWorldQuerier panic stub | Remove |
| Missing hostfunc bindings (15 methods) | Add via capability modules |
| Parity table maintenance burden | Update to reflect modular structure |
| Properties layer duplication | Consolidate |

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `internal/plugin/hostfunc/capability.go` | Capability module interface + registry |
| `internal/plugin/hostfunc/capability_test.go` | Tests |
| `internal/plugin/hostfunc/cap_session.go` | Session capability module (find, list, broadcast, whisper) |
| `internal/plugin/hostfunc/cap_session_test.go` | Tests |
| `internal/plugin/hostfunc/cap_alias.go` | Alias capability module (set, delete, list, shadow check) |
| `internal/plugin/hostfunc/cap_alias_test.go` | Tests |
| `internal/plugin/hostfunc/cap_property.go` | Property capability module (list, find by prefix) |
| `internal/plugin/hostfunc/cap_property_test.go` | Tests |
| `internal/plugin/hostfunc/cap_world_query.go` | World query capability module (objects by location, characters by location) |
| `internal/plugin/hostfunc/cap_world_query_test.go` | Tests |
| `internal/plugin/hostfunc/errors.go` | Consolidated error sanitization |
| `plugins/core-help/main.lua` | Lua rewrite of core-help |
| `plugins/core-building/main.lua` | Lua rewrite of core-building |
| `plugins/core-objects/main.lua` | Lua rewrite of core-objects |
| `plugins/core-communication/main.lua` | Lua rewrite of core-communication |
| `plugins/core-aliases/main.lua` | Lua rewrite of core-aliases |

### Modified Files

| File | Change |
|------|--------|
| `internal/plugin/hostfunc/functions.go` | Refactor to use capability modules; inject based on requires |
| `internal/plugin/hostfunc/adapter.go` | Remove dead WorldQuerier interface and panic stubs |
| `internal/plugin/hostfunc/world.go` | Clean up to use consolidated errors |
| `internal/plugin/hostfunc/world_write.go` | Clean up, remove duplicate property handling |
| `internal/plugin/lua/host.go` | Pass manifest to hostfunc registration for requires-based scoping |
| `plugins/core-help/plugin.yaml` | Change type: core → type: lua, add lua-plugin entry |
| `plugins/core-building/plugin.yaml` | Same |
| `plugins/core-objects/plugin.yaml` | Same |
| `plugins/core-communication/plugin.yaml` | Same |
| `plugins/core-aliases/plugin.yaml` | Same |
| `internal/plugin/setup/subsystem.go` | Remove explicit RegisterHandler calls and core plugin imports |
| `internal/plugin/parity_test.go` | Update for modular structure |

### Deleted Files

| File | Reason |
|------|--------|
| `plugins/core-help/plugin.go` | Replaced by main.lua |
| `plugins/core-building/plugin.go` | Replaced by main.lua |
| `plugins/core-building/dig.go` | Logic moves to main.lua |
| `plugins/core-building/link.go` | Logic moves to main.lua |
| `plugins/core-objects/plugin.go` | Replaced by main.lua |
| `plugins/core-objects/create.go` | Logic moves to main.lua |
| `plugins/core-objects/examine.go` | Logic moves to main.lua |
| `plugins/core-objects/describe.go` | Logic moves to main.lua |
| `plugins/core-objects/set.go` | Logic moves to main.lua |
| `plugins/core-communication/plugin.go` | Replaced by main.lua |
| `plugins/core-communication/say.go` | Logic moves to main.lua |
| `plugins/core-communication/pose.go` | Logic moves to main.lua |
| `plugins/core-communication/page.go` | Logic moves to main.lua |
| `plugins/core-communication/whisper.go` | Logic moves to main.lua |
| `plugins/core-communication/ooc.go` | Logic moves to main.lua |
| `plugins/core-communication/pemit.go` | Logic moves to main.lua |
| `plugins/core-communication/emit.go` | Logic moves to main.lua |
| `plugins/core-communication/wall.go` | Logic moves to main.lua |
| `plugins/core-aliases/alias.go` | Logic moves to main.lua |
| `plugins/core-aliases/plugin_test.go` | Covered by Lua integration tests |
| `plugins/core-communication/plugin_test.go` | Covered by Lua integration tests |
| `plugins/core-objects/plugin_test.go` | Covered by Lua integration tests |

---

## Task 1: Consolidated Error Sanitization

**Files:**

- Create: `internal/plugin/hostfunc/errors.go`
- Create: `internal/plugin/hostfunc/errors_test.go`
- Modify: `internal/plugin/hostfunc/world.go` (remove duplicate sanitize function)
- Modify: `internal/plugin/hostfunc/functions.go` (remove duplicate sanitize function)

- [ ] **Step 1: Write failing test for consolidated sanitizer**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/world"
)

func TestSanitizeErrorForPlugin(t *testing.T) {
	t.Run("returns not found for ErrNotFound", func(t *testing.T) {
		err := SanitizeErrorForPlugin(world.ErrNotFound, "test-plugin", "get_location")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("returns permission denied for ErrPermissionDenied", func(t *testing.T) {
		err := SanitizeErrorForPlugin(world.ErrPermissionDenied, "test-plugin", "create_location")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("returns generic error for unknown errors", func(t *testing.T) {
		err := SanitizeErrorForPlugin(errors.New("db connection lost"), "test-plugin", "query")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "operation failed")
	})

	t.Run("returns nil for nil error", func(t *testing.T) {
		err := SanitizeErrorForPlugin(nil, "test-plugin", "query")
		assert.NoError(t, err)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestSanitizeErrorForPlugin ./internal/plugin/hostfunc/`

- [ ] **Step 3: Implement consolidated sanitizer**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/holomush/holomush/internal/world"
)

// SanitizeErrorForPlugin converts internal errors to plugin-safe messages.
// Internal details (DB errors, stack traces) are logged but not returned.
// Plugins see a generic message unless the error is a known safe type.
func SanitizeErrorForPlugin(err error, pluginName, operation string) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, world.ErrNotFound):
		return fmt.Errorf("%s: not found", operation)
	case errors.Is(err, world.ErrPermissionDenied):
		return fmt.Errorf("%s: permission denied", operation)
	default:
		slog.Warn("plugin operation failed",
			"plugin", pluginName,
			"operation", operation,
			"error", err,
		)
		return fmt.Errorf("%s: operation failed", operation)
	}
}
```

- [ ] **Step 4: Run test — should pass**

- [ ] **Step 5: Update world.go and functions.go to use consolidated sanitizer**

Remove `sanitizeErrorForPlugin` from `world.go` and `sanitizeKVErrorForPlugin` from `functions.go`. Replace all call sites with `SanitizeErrorForPlugin`.

- [ ] **Step 6: Run full hostfunc tests**

Run: `task test -- ./internal/plugin/hostfunc/...`

- [ ] **Step 7: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "refactor(hostfunc): consolidate error sanitization into single helper"
jj new
```

---

## Task 2: Remove Dead Code (WorldQuerier, WithWorldQuerier)

**Files:**

- Modify: `internal/plugin/hostfunc/adapter.go`
- Modify: `internal/plugin/hostfunc/functions.go`

- [ ] **Step 1: Remove dead WorldQuerier interface from adapter.go**

Remove the `WorldQuerier` interface (deprecated, never used at runtime). Keep `WorldQuerierAdapter` which is the actual adapter.

- [ ] **Step 2: Remove WithWorldQuerier panic from functions.go**

Remove the `WithWorldQuerier` option that panics with a deprecation message.

- [ ] **Step 3: Remove any compile-time checks referencing the dead interface**

- [ ] **Step 4: Run full test suite**

Run: `task test`

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "refactor(hostfunc): remove dead WorldQuerier interface and panic stub"
jj new
```

---

## Task 3: Capability Module System

**Files:**

- Create: `internal/plugin/hostfunc/capability.go`
- Create: `internal/plugin/hostfunc/capability_test.go`
- Modify: `internal/plugin/hostfunc/functions.go`

This task introduces the modular capability system. Each capability module registers a set of Lua functions into a namespace. Modules are registered based on manifest `requires` declarations.

- [ ] **Step 1: Write failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapabilityRegistry(t *testing.T) {
	t.Run("registers and retrieves a capability module by name", func(t *testing.T) {
		reg := NewCapabilityRegistry()
		module := &stubCapability{name: "test"}
		reg.Register("test-service", module)

		found := reg.Get("test-service")
		require.NotNil(t, found)
		assert.Equal(t, "test", found.Namespace())
	})

	t.Run("returns nil for unregistered service", func(t *testing.T) {
		reg := NewCapabilityRegistry()
		assert.Nil(t, reg.Get("missing"))
	})

	t.Run("injects only required capabilities into Lua state", func(t *testing.T) {
		reg := NewCapabilityRegistry()
		reg.Register("svc-a", &stubCapability{name: "a"})
		reg.Register("svc-b", &stubCapability{name: "b"})

		L := lua.NewState()
		defer L.Close()

		// Only require svc-a
		reg.InjectRequired(L, []string{"svc-a"}, "test-plugin")

		// svc-a should be injected
		assert.NotNil(t, L.GetGlobal("a"))
		// svc-b should NOT be injected
		assert.Equal(t, lua.LNil, L.GetGlobal("b"))
	})
}

type stubCapability struct {
	name string
}

func (s *stubCapability) Namespace() string { return s.name }

func (s *stubCapability) Register(L *lua.LState, pluginName string) {
	tbl := L.NewTable()
	L.SetGlobal(s.name, tbl)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestCapabilityRegistry ./internal/plugin/hostfunc/`

- [ ] **Step 3: Implement capability system**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import lua "github.com/yuin/gopher-lua"

// Capability is a module of Lua host functions that can be injected
// into the VM based on a plugin's manifest requires declarations.
type Capability interface {
	// Namespace returns the Lua global table name (e.g., "session", "alias").
	Namespace() string

	// Register injects this capability's functions into the Lua state.
	Register(L *lua.LState, pluginName string)
}

// CapabilityRegistry maps proto service names to capability modules.
type CapabilityRegistry struct {
	modules map[string]Capability
}

// NewCapabilityRegistry creates an empty registry.
func NewCapabilityRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{modules: make(map[string]Capability)}
}

// Register associates a proto service name with a capability module.
func (r *CapabilityRegistry) Register(serviceName string, cap Capability) {
	r.modules[serviceName] = cap
}

// Get returns the capability for a service name, or nil.
func (r *CapabilityRegistry) Get(serviceName string) Capability {
	return r.modules[serviceName]
}

// InjectRequired registers capability modules into the Lua state
// for each service in the requires list.
func (r *CapabilityRegistry) InjectRequired(L *lua.LState, requires []string, pluginName string) {
	for _, svc := range requires {
		if cap, ok := r.modules[svc]; ok {
			cap.Register(L, pluginName)
		}
	}
}
```

- [ ] **Step 4: Run tests — should pass**

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(hostfunc): add capability module system for requires-based injection"
jj new
```

---

## Task 4: Session Capability Module

**Files:**

- Create: `internal/plugin/hostfunc/cap_session.go`
- Create: `internal/plugin/hostfunc/cap_session_test.go`

Adds missing session hostfunc bindings: `FindSessionByName`, `ListActiveSessions`, `BroadcastSystemMessage`, `SetLastWhispered`, `DisconnectSession`, `UpdateActivity`.

- [ ] **Step 1: Write tests for session capability**

Test that the module registers the expected functions in the Lua namespace.

- [ ] **Step 2: Implement session capability module**

The module wraps `hostfunc.SessionAccess` (from `internal/plugin/hostfunc/cap_session.go`) and registers functions like:

- `session.find_by_name(name)` → `{character_id, character_name, location_id, last_whispered}`
- `session.list_active()` → array of session objects
- `session.broadcast(message)` → nil
- `session.set_last_whispered(session_id, name)` → nil
- `session.disconnect(session_id, reason)` → nil

- [ ] **Step 3: Run tests**
- [ ] **Step 4: Commit**

---

## Task 5: Alias Capability Module

**Files:**

- Create: `internal/plugin/hostfunc/cap_alias.go`
- Create: `internal/plugin/hostfunc/cap_alias_test.go`

Adds missing alias hostfunc bindings.

- [ ] **Step 1: Write tests**
- [ ] **Step 2: Implement alias capability module**

Functions:

- `alias.set_player(player_id, alias, command)` → nil/error
- `alias.delete_player(player_id, alias)` → nil/error
- `alias.list_player(player_id)` → array of `{alias, command}`
- `alias.check_shadow(alias)` → `{shadows, command}`
- `alias.set_system(alias, command, created_by)` → nil/error
- `alias.delete_system(alias)` → nil/error
- `alias.list_system()` → array of `{alias, command}`

- [ ] **Step 3: Run tests**
- [ ] **Step 4: Commit**

---

## Task 6: Property Capability Module

**Files:**

- Create: `internal/plugin/hostfunc/cap_property.go`
- Create: `internal/plugin/hostfunc/cap_property_test.go`

Adds missing property hostfunc bindings.

- [ ] **Step 1: Write tests**
- [ ] **Step 2: Implement property capability module**

Functions:

- `property.list_by_parent(subject_id, parent_type, parent_id)` → array of `{name, value, visibility}`
- `property.find_by_prefix(prefix)` → array of property definitions
- `property.update_character_description(subject_id, character_id, description)` → nil/error

- [ ] **Step 3: Run tests**
- [ ] **Step 4: Commit**

---

## Task 7: World Query Capability Module (extended)

**Files:**

- Create: `internal/plugin/hostfunc/cap_world_query.go`
- Create: `internal/plugin/hostfunc/cap_world_query_test.go`

Adds missing world query bindings.

- [ ] **Step 1: Write tests**
- [ ] **Step 2: Implement world query capability module**

Functions:

- `world.get_objects_by_location(subject_id, location_id)` → array of objects
- `world.get_characters_by_location(subject_id, location_id)` → array of characters

These supplement the existing `query_location`, `query_character`, `query_object` which remain in the base hostfunc set.

- [ ] **Step 3: Run tests**
- [ ] **Step 4: Commit**

---

## Task 8: Wire Capability Registry into Lua Host

**Files:**

- Modify: `internal/plugin/hostfunc/functions.go`
- Modify: `internal/plugin/lua/host.go`

- [ ] **Step 1: Add CapabilityRegistry to Functions**

The `Functions` struct gains a `capabilities *CapabilityRegistry` field. In `Register()`, after registering base functions, call `capabilities.InjectRequired(L, manifest.Requires, pluginName)`.

- [ ] **Step 2: Update Lua host to pass manifest to Register**

Change `Register(L, pluginName)` to `Register(L, pluginName, manifest)` so the hostfunc system knows which capabilities to inject.

- [ ] **Step 3: Register all capability modules in plugin subsystem setup**

In `internal/plugin/setup/subsystem.go`, create capability modules and register them:

```go
capRegistry := hostfunc.NewCapabilityRegistry()
capRegistry.Register("holomush.session.v1.SessionService", hostfunc.NewSessionCapability(sessionStore))
capRegistry.Register("holomush.alias.v1.AliasService", hostfunc.NewAliasCapability(aliasWriter, aliasCache))
// etc.
```

- [ ] **Step 4: Run full test suite**

Run: `task test`

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(hostfunc): wire capability registry into Lua host with requires-based injection"
jj new
```

---

## Task 9: Migrate core-help to Lua

**Files:**

- Modify: `plugins/core-help/plugin.yaml` (type: core → type: lua)
- Create: `plugins/core-help/main.lua`
- Delete: `plugins/core-help/plugin.go` (Go implementation)

This is the simplest migration — core-help only uses `list_commands` and `get_command_help`, both already available as hostfuncs.

- [ ] **Step 1: Write main.lua**

Port the Go handler logic to Lua. The `on_command(ctx)` function handles `help` and `help <command>`.

- [ ] **Step 2: Update plugin.yaml**

Change `type: core` to `type: lua` and add `lua-plugin: {entry: main.lua}`.

- [ ] **Step 3: Delete Go files**

Remove `plugins/core-help/plugin.go`.

- [ ] **Step 4: Remove RegisterHandler call from subsystem setup**

In `internal/plugin/setup/subsystem.go`, remove:

```go
localHost.RegisterHandler("core-help", &corehelp.Handler{}, nil)
```

And the `corehelp` import.

- [ ] **Step 5: Run E2E tests to verify help command still works**

Run: `task test:e2e`

- [ ] **Step 6: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): migrate core-help from Go to Lua"
jj new
```

---

## Task 10: Migrate core-building to Lua

Same pattern as Task 9. core-building uses `create_location`, `create_exit`, `query_location`, `find_location`, `log` — all available.

- [ ] **Step 1: Write main.lua** — port dig/link handlers
- [ ] **Step 2: Update plugin.yaml** — type: lua, lua-plugin entry
- [ ] **Step 3: Delete Go files** — plugin.go, dig.go, link.go
- [ ] **Step 4: Remove RegisterHandler from subsystem** — remove `corebuilding` import
- [ ] **Step 5: Run E2E tests**
- [ ] **Step 6: Commit**

---

## Task 11: Migrate core-objects to Lua

Requires the property and world query capability modules from Tasks 6+7.

- [ ] **Step 1: Update plugin.yaml** — type: lua, add requires for property and world query services
- [ ] **Step 2: Write main.lua** — port create/examine/describe/set handlers using new capability functions
- [ ] **Step 3: Delete Go files**
- [ ] **Step 4: Remove RegisterHandler from subsystem**
- [ ] **Step 5: Run E2E tests**
- [ ] **Step 6: Commit**

---

## Task 12: Migrate core-communication to Lua

Requires the session capability module from Task 4.

- [ ] **Step 1: Update plugin.yaml** — type: lua, add requires for session service
- [ ] **Step 2: Write main.lua** — port say/pose/page/whisper/ooc/pemit/emit/wall handlers
- [ ] **Step 3: Delete Go files**
- [ ] **Step 4: Remove RegisterHandler from subsystem**
- [ ] **Step 5: Run E2E tests**
- [ ] **Step 6: Commit**

---

## Task 13: Migrate core-aliases to Lua

Requires the alias capability module from Task 5.

- [ ] **Step 1: Update plugin.yaml** — type: lua, add requires for alias service
- [ ] **Step 2: Write main.lua** — port alias/unalias/aliases/sysalias handlers
- [ ] **Step 3: Delete Go files**
- [ ] **Step 4: Remove RegisterHandler from subsystem**
- [ ] **Step 5: Run E2E tests**
- [ ] **Step 6: Commit**

---

## Task 14: Remove type: core and Explicit Registration

**Files:**

- Modify: `internal/plugin/manifest.go` — remove `TypeCore` constant; update validation
- Modify: `internal/plugin/setup/subsystem.go` — remove all core plugin imports, remove `LocalPluginHost` explicit registration block
- Modify: `internal/plugin/local_host.go` — remove `RegisterHandler` and `registrations` map (no longer needed)

- [ ] **Step 1: Remove TypeCore from manifest**

Remove `TypeCore Type = "core"` and the load priority constant. Update `Validate()` to reject `type: core`.

- [ ] **Step 2: Clean up LocalPluginHost**

Remove `RegisterHandler`, `registrations` map, and the pre-registration lookup in `Load()`. Core plugins are now Lua — they load through the standard Lua host path.

- [ ] **Step 3: Clean up subsystem setup**

Remove all `plugins/core-*` imports from `internal/plugin/setup/subsystem.go`. The core plugin registration block disappears entirely.

- [ ] **Step 4: Run full test suite + E2E**

Run: `task test && task test:e2e`

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "refactor(plugin): remove type: core and explicit handler registration"
jj new
```

---

## Task 15: Update Parity Tests

**Files:**

- Modify: `internal/plugin/parity_test.go`

- [ ] **Step 1: Update parity table**

Reflect the new modular capability structure. Each capability module's functions should have entries in the parity table.

- [ ] **Step 2: Run tests**
- [ ] **Step 3: Commit**

---

## Deferred Work

| Item | Notes |
|------|-------|
| Lua VM pooling | P0 perf improvement — reuse VMs across deliveries instead of creating fresh per call. Orthogonal to migration. |
| Proto-based Lua binding generation | Phase 2 uses hand-written capability modules. Auto-generation from proto comes later. |
| Adapter layer removal | Depends on WorldService taking subjectID from context, not arguments |
