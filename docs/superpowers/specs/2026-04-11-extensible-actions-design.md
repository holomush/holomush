# Extensible Plugin Actions

**Status:** Approved  
**Date:** 2026-04-11  
**Issue:** holomush-275o  
**Blocks:** holomush-0sc.12 (channel plugin rework)

---

## Problem

`validActions` in `internal/command/types.go` is a hardcoded map. `Capability.Validate()` rejects any action string not in that map, which blocks plugins that introduce domain-specific ABAC actions. The core-channels plugin needs `join` and `leave`; both fail validation today.

`validResourceTypes` had the same problem and was already solved with explicit manifest declarations (`resource_types`), a `CollectResourceTypes` merge pass, and a deferred `ValidateResourceType` check. Actions need the same treatment.

---

## Decision

Use a **hybrid approach**: an explicit `actions` field in the manifest as the authoritative source, plus a load-time warning when a plugin borrows an action declared by another plugin.

This mirrors the `resource_types` pattern for auditability and enables future tooling (policy editors, permission introspection) to enumerate all ABAC action primitives from manifests alone, without loading plugins.

---

## Design

### 1. `internal/command/types.go`

**Remove** the `!validActions[c.Action]` check from `Capability.Validate()`. The action field still MUST be non-empty; unknown values are no longer rejected here. Validation of the action vocabulary is deferred to load time, consistent with how resource types already work.

Add two new exports:

```go
// CoreActions returns a defensive copy of the built-in action set.
// Used by the plugin manager to build the full known-actions map.
func CoreActions() map[string]bool

// ValidateAction checks that the capability's action is in the provided set.
// Called during plugin load with a set that includes both core actions and
// plugin-declared actions.
func (c Capability) ValidateAction(known map[string]bool) error
```

The built-in set remains: `read`, `write`, `emit`, `enter`, `use`, `delete`, `execute`, `admin`.

### 2. `internal/plugin/manifest.go`

Add an `Actions` field to `Manifest`:

```go
Actions []string `yaml:"actions,omitempty" json:"actions,omitempty"`
```

Unlike `resource_types`, this field is available to all plugin types (Lua, binary, setting). There is no structural coupling between actions and the `AttributeResolverService` gRPC contract, so the binary-only restriction does not apply.

Validation rules (enforced in `Manifest.Validate()`):

- Each entry MUST be non-empty.
- Entries MUST be unique within the manifest.
- Re-declaring a core action (e.g., `actions: [read]`) is silently allowed. It is redundant but not harmful.

### 3. `internal/plugin/manager.go`

**`CollectActions`** — new exported function, parallel to `CollectResourceTypes`:

```go
// CollectActions builds the full set of known ABAC actions: core actions plus
// all actions explicitly declared across discovered plugins. Exported as a test
// seam so callers can verify the merge logic without driving LoadAll.
func CollectActions(discovered []*DiscoveredPlugin) map[string]bool
```

Returns `CoreActions()` merged with the union of each manifest's `Actions` field. Does not scan capability action strings — only explicit declarations feed the authoritative set.

**`loadPlugin` signature change:**

```go
func (m *Manager) loadPlugin(
    ctx context.Context,
    dp *DiscoveredPlugin,
    knownResourceTypes map[string]bool,
    knownActions map[string]bool,
) error
```

In `loadPlugin`, after the existing `ValidateResourceType` loop, add a second loop:

```go
for i := range dp.Manifest.Commands {
    cmd := &dp.Manifest.Commands[i]
    for _, cap := range cmd.Capabilities {
        if err := cap.ValidateAction(knownActions); err != nil {
            return oops.In("manager").With("plugin", dp.Manifest.Name).
                With("command", cmd.Name).Wrap(err)
        }
        // Warn when a plugin borrows an action declared by another plugin.
        if !CoreActions()[cap.Action] && !slices.Contains(dp.Manifest.Actions, cap.Action) {
            slog.Warn("capability uses action not declared by this plugin",
                "plugin", dp.Manifest.Name,
                "command", cmd.Name,
                "action", cap.Action)
        }
    }
}
```

**`LoadAll` Phase 2** — call `CollectActions` alongside `CollectResourceTypes`:

```go
knownResourceTypes := CollectResourceTypes(discovered)
knownActions := CollectActions(discovered)
```

Pass `knownActions` into each `loadPlugin` call.

---

## Validation Summary

| Scenario | Result |
|---|---|
| Core action (e.g., `read`) in capability | Pass, no warning |
| Non-core action declared in own `actions` field | Pass, no warning |
| Non-core action declared by another plugin's `actions` field | Pass, warning logged |
| Non-core action not declared in any plugin's `actions` field | Hard fail (INVALID_CAPABILITY) |
| Empty action string | Hard fail (INVALID_CAPABILITY, from `Capability.Validate()`) |
| Duplicate entry in `actions` field | Hard fail at manifest parse time |

---

## Testing

### `internal/command/types_test.go`

**Updates:**
- Remove or invert tests that assert `Capability.Validate()` rejects unknown action strings. A non-empty unknown action now passes `Validate()`.

**Additions:**
- `TestCoreActionsContainsExpectedDefaults` — invariant: returned map always contains `read`, `write`, `emit`, `enter`, `use`, `delete`, `execute`, `admin`.
- `TestCoreActionsReturnsCopy` — mutating the returned map does not affect a second call.
- `TestCapability_ValidateActionWithKnownAction` — action present in provided map returns nil.
- `TestCapability_ValidateActionWithUnknownAction` — action absent from provided map returns INVALID_CAPABILITY error.
- `TestCapability_ValidateActionBoundaryEmptyKnownMap` — empty known map rejects any action.

### `internal/plugin/manifest_test.go`

- `TestManifestValidateRejectsEmptyActionEntry` — empty string in `actions` fails validation.
- `TestManifestValidateRejectsDuplicateActions` — duplicate strings in `actions` fails validation.
- `TestManifestValidateAcceptsActionsOnLuaPlugin` — Lua plugin with `actions` parses and validates.
- `TestManifestValidateAcceptsActionsOnBinaryPlugin` — Binary plugin with `actions` parses and validates.
- `TestManifestValidateAcceptsRedeclaredCoreAction` — `actions: [read]` parses and validates without error.

### `internal/plugin/manager_test.go`

**`CollectActions` unit tests:**
- `TestCollectActionsIncludesCoreActions` — `CollectActions(nil)` returns all core actions.
- `TestCollectActionsMergesExplicitManifestActions` — plugin declaring `actions: [join]` causes `join` in result.
- `TestCollectActionsDeduplicatesAcrossPlugins` — two plugins declaring the same action produce one entry.
- `TestCollectActionsReturnsNewMapPerCall` — invariant: mutations to the returned map do not affect a second call.
- `TestCollectActionsIgnoresCapabilityActionsNotInActionsField` — action appearing only in capabilities (not in `actions` field) does not appear in result.

**Load integration tests:**
- Plugin with `actions: [join]` and `action: join` in a capability loads successfully.
- Plugin with `action: join` in a capability but no `actions: [join]` in any discovered plugin fails with INVALID_CAPABILITY.
- Plugin using an action declared by a different plugin loads with a warning logged.
- Plugin re-declaring a core action in `actions` field loads without error.

### `test/integration/` (Ginkgo/Gomega)

- `Describe("Plugin loading with custom actions")`:
  - `It("loads a plugin that declares non-core actions in the actions field")`
  - `It("rejects a plugin whose capability uses an undeclared action")`
  - `It("loads two plugins where one borrows an action declared by the other")`

### E2E scope

E2E for this issue is integration-level: the core-channels plugin manifest (`actions: [join, leave]`) loads cleanly through the full `LoadAll` pipeline. Gameplay E2E for channel commands (join/leave behavior, ABAC authorization) is in scope for holomush-0sc.12.

---

## Out of Scope

- Removing the redundant `scene` entry from `validResourceTypes` (pre-existing tech debt, noted in holomush-275o but not addressed here).
- Any changes to Cedar policy DSL or the ABAC evaluation engine.
- Restricting which plugins may borrow actions from other plugins (the warning is informational; cross-plugin action reuse is permitted).
