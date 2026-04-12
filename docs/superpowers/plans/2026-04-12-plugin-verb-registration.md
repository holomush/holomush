# Plugin Verb Registration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable plugins to declare verb registrations in their manifest, removing the boundary violation where plugin-owned event types are hardcoded in `internal/core/builtins.go`.

**Architecture:** Add `verbs:` section to plugin manifests, parsed at load time. The loader calls `VerbRegistry.Register()` for each entry — same path builtins use. Add `Source` field for ownership tracking and `Unregister`/`UnregisterBySource` methods for load-failure cleanup and future unload. Move channel verb types from builtins to the channel plugin manifest.

**Tech Stack:** Go, YAML manifest parsing, testify assertions, Ginkgo/Gomega for integration tests.

**Spec:** `docs/superpowers/specs/2026-04-12-plugin-verb-registration-design.md`

---

## Task 1: Add Source field and Unregister methods to VerbRegistry

**Files:**

- Modify: `internal/core/registry.go`
- Modify: `internal/core/builtins.go`
- Modify: `internal/core/registry_test.go`

- [ ] **Step 1: Write failing tests for Source field and Unregister**

Add to `internal/core/registry_test.go`:

```go
func TestVerbRegistrySourceFieldPreservedThroughRegisterLookup(t *testing.T) {
	r := NewVerbRegistry()
	err := r.Register(VerbRegistration{
		Type: "custom", Category: "communication", Format: "action", Source: "my-plugin",
	})
	require.NoError(t, err)

	reg, ok := r.Lookup("custom")
	require.True(t, ok)
	assert.Equal(t, "my-plugin", reg.Source)
}

func TestVerbRegistryUnregisterRemovesEntry(t *testing.T) {
	r := NewVerbRegistry()
	err := r.Register(VerbRegistration{
		Type: "temp", Category: "system", Format: "notification", Source: "test",
	})
	require.NoError(t, err)

	removed := r.Unregister("temp")
	assert.True(t, removed)

	_, ok := r.Lookup("temp")
	assert.False(t, ok)
}

func TestVerbRegistryUnregisterNonexistentReturnsFalse(t *testing.T) {
	r := NewVerbRegistry()
	removed := r.Unregister("nonexistent")
	assert.False(t, removed)
}

func TestVerbRegistryUnregisterBySourceRemovesAllFromSource(t *testing.T) {
	r := NewVerbRegistry()
	require.NoError(t, r.Register(VerbRegistration{
		Type: "a", Category: "communication", Format: "action", Source: "plugin-x",
	}))
	require.NoError(t, r.Register(VerbRegistration{
		Type: "b", Category: "system", Format: "notification", Source: "plugin-x",
	}))
	require.NoError(t, r.Register(VerbRegistration{
		Type: "c", Category: "command", Format: "narrative", Source: "builtin",
	}))

	count := r.UnregisterBySource("plugin-x")
	assert.Equal(t, 2, count)

	_, ok := r.Lookup("a")
	assert.False(t, ok)
	_, ok = r.Lookup("b")
	assert.False(t, ok)
	_, ok = r.Lookup("c")
	assert.True(t, ok)
}

func TestVerbRegistryUnregisterBySourceUnknownReturnsZero(t *testing.T) {
	r := NewVerbRegistry()
	count := r.UnregisterBySource("nonexistent")
	assert.Equal(t, 0, count)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestVerbRegistry ./internal/core/`
Expected: FAIL — `Source` field does not exist, `Unregister`/`UnregisterBySource` methods do not exist.

- [ ] **Step 3: Add Source field and implement Unregister methods**

In `internal/core/registry.go`, add `Source` field to `VerbRegistration`:

```go
// VerbRegistration holds rendering metadata for an event type.
type VerbRegistration struct {
	Type          string
	Category      string // "communication", "movement", "state", "system", "command"
	Format        string // "speech", "action", "narrative", "notification", "error", "snapshot", "delta"
	Label         string // "says", "telepathically sends" -- required when Format is "speech"
	DisplayTarget webv1.EventChannel
	MetadataKeys  []MetadataKey
	Source        string // "builtin" or plugin name -- tracks ownership for unload
}
```

Add `Unregister` and `UnregisterBySource` methods after the existing `All()` method:

```go
// Unregister removes a single verb by event type. Returns true if found.
func (r *VerbRegistry) Unregister(eventType string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.types[eventType]; !exists {
		return false
	}
	delete(r.types, eventType)
	return true
}

// UnregisterBySource removes all verbs registered by a given source.
// Returns the count of removed entries.
func (r *VerbRegistry) UnregisterBySource(source string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for key, reg := range r.types {
		if reg.Source == source {
			delete(r.types, key)
			count++
		}
	}
	return count
}
```

- [ ] **Step 4: Update builtins to set Source field**

In `internal/core/builtins.go`, update `RegisterBuiltinTypes` — add `Source: "builtin"` to every registration. The function signature and loop stay the same; each struct literal gains one field.

For each entry in the `builtins` slice, add `Source: "builtin"`. Example for the first entry:

```go
{Type: "say", Category: "communication", Format: "speech", Label: "says", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
```

Apply this to all entries in the slice (lines 13-74 of the current file).

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- -run TestVerbRegistry ./internal/core/`
Expected: ALL PASS.

- [ ] **Step 6: Run full test suite to check for regressions**

Run: `task test`
Expected: ALL PASS. The `Source` field is a new addition — existing code that reads `VerbRegistration` won't break because Go zero-values uninitialized string fields to `""`.

- [ ] **Step 7: Commit**

```text
feat(core): add Source field and Unregister methods to VerbRegistry

Source tracks ownership (builtin vs plugin name) for load-failure
cleanup and future unload. UnregisterBySource removes all verbs
from a given plugin in one call.

Part of holomush-6l7l.
```

---

### Task 2: Add VerbSpec to manifest parsing and validation

**Files:**

- Modify: `internal/plugin/manifest.go`
- Modify: `internal/plugin/manifest_test.go`

- [ ] **Step 1: Write failing tests for manifest verb parsing**

Add to `internal/plugin/manifest_test.go`:

```go
func TestManifestAcceptsLuaPluginWithVerbs(t *testing.T) {
	data := []byte(`
name: verb-plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: custom_say
    category: communication
    format: speech
    label: "says"
    display_target: terminal
  - type: custom_action
    category: communication
    format: action
    display_target: both
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	require.Len(t, m.Verbs, 2)
	assert.Equal(t, "custom_say", m.Verbs[0].Type)
	assert.Equal(t, "communication", m.Verbs[0].Category)
	assert.Equal(t, "speech", m.Verbs[0].Format)
	assert.Equal(t, "says", m.Verbs[0].Label)
	assert.Equal(t, "terminal", m.Verbs[0].DisplayTarget)
	assert.Equal(t, "custom_action", m.Verbs[1].Type)
	assert.Equal(t, "both", m.Verbs[1].DisplayTarget)
}

func TestManifestAcceptsBinaryPluginWithVerbs(t *testing.T) {
	data := []byte(`
name: verb-binary
version: 1.0.0
type: binary
binary-plugin:
  executable: verb-binary
verbs:
  - type: custom_event
    category: state
    format: delta
    display_target: state
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	require.Len(t, m.Verbs, 1)
	assert.Equal(t, "state", m.Verbs[0].DisplayTarget)
}

func TestManifestRejectsSettingPluginWithVerbs(t *testing.T) {
	data := []byte(`
name: verb-setting
version: 1.0.0
type: setting
setting:
  display_name: Test
  content_dir: content/
  starting_location: "#1"
verbs:
  - type: custom
    category: system
    format: notification
    display_target: terminal
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "setting plugins must not declare verbs")
}

func TestManifestRejectsVerbWithEmptyType(t *testing.T) {
	data := []byte(`
name: bad-verb
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: ""
    category: communication
    format: action
    display_target: terminal
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "type must not be empty")
}

func TestManifestRejectsVerbWithUnknownCategory(t *testing.T) {
	data := []byte(`
name: bad-verb
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: custom
    category: unknown
    format: action
    display_target: terminal
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown verb category")
}

func TestManifestRejectsVerbWithUnknownFormat(t *testing.T) {
	data := []byte(`
name: bad-verb
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: custom
    category: communication
    format: unknown
    display_target: terminal
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown verb format")
}

func TestManifestRejectsVerbWithUnknownDisplayTarget(t *testing.T) {
	data := []byte(`
name: bad-verb
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: custom
    category: communication
    format: action
    display_target: sidebar
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown verb display_target")
}

func TestManifestRejectsSpeechVerbWithoutLabel(t *testing.T) {
	data := []byte(`
name: bad-verb
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: custom
    category: communication
    format: speech
    display_target: terminal
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "label is required")
}

func TestManifestRejectsDuplicateVerbType(t *testing.T) {
	data := []byte(`
name: bad-verb
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: custom
    category: communication
    format: action
    display_target: terminal
  - type: custom
    category: system
    format: notification
    display_target: terminal
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate verb type")
}

func TestManifestDisplayTargetCaseInsensitive(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"lowercase", "terminal"},
		{"uppercase", "TERMINAL"},
		{"mixed case", "Terminal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(fmt.Sprintf(`
name: case-test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: custom
    category: communication
    format: action
    display_target: %s
`, tt.input))
			m, err := plugins.ParseManifest(data)
			require.NoError(t, err)
			require.Len(t, m.Verbs, 1)
		})
	}
}

func TestManifestAcceptsPluginWithNoVerbs(t *testing.T) {
	data := []byte(`
name: no-verbs
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	assert.Empty(t, m.Verbs)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestManifest ./internal/plugin/`
Expected: FAIL — `Verbs` field and `VerbSpec` type do not exist.

- [ ] **Step 3: Add VerbSpec and manifest validation**

In `internal/plugin/manifest.go`, add the `VerbSpec` struct after `CommandSpec`:

```go
// VerbSpec declares a verb registration contributed by a plugin.
type VerbSpec struct {
	Type          string `yaml:"type" json:"type" jsonschema:"required"`
	Category      string `yaml:"category" json:"category" jsonschema:"required"`
	Format        string `yaml:"format" json:"format" jsonschema:"required"`
	Label         string `yaml:"label,omitempty" json:"label,omitempty"`
	DisplayTarget string `yaml:"display_target" json:"display_target" jsonschema:"required"`
}

// validVerbCategories lists the known verb categories.
var validVerbCategories = map[string]bool{
	"communication": true, "movement": true, "state": true, "system": true, "command": true,
}

// validVerbFormats lists the known verb formats.
var validVerbFormats = map[string]bool{
	"speech": true, "action": true, "narrative": true, "notification": true,
	"error": true, "snapshot": true, "delta": true,
}

// validDisplayTargets lists the known display target values (case-insensitive).
var validDisplayTargets = map[string]bool{
	"terminal": true, "state": true, "both": true,
}
```

Add `Verbs` field to the `Manifest` struct (after the `Commands` field, around line 79):

```go
Verbs    []VerbSpec    `yaml:"verbs,omitempty" json:"verbs,omitempty" jsonschema:"description=Verb registrations contributed by this plugin"`
```

Add verb validation inside `Manifest.Validate()`. Place it after the setting-type block (after line 321 in the current file, inside the `TypeSetting` case, alongside the existing "setting plugins must not declare commands" check):

In the `TypeSetting` case, add:

```go
if len(m.Verbs) > 0 {
	return oops.In("manifest").With("name", m.Name).New("setting plugins must not declare verbs")
}
```

Then add verb validation after the commands validation block (after the `seenCommands` loop, around line 372):

```go
// Validate verbs and check for duplicates.
seenVerbs := make(map[string]bool)
for i, v := range m.Verbs {
	if v.Type == "" {
		return oops.In("manifest").With("plugin", m.Name).With("verb_index", i).
			New("verb type must not be empty")
	}
	if !validVerbCategories[v.Category] {
		return oops.In("manifest").With("plugin", m.Name).With("verb", v.Type).
			With("category", v.Category).New("unknown verb category")
	}
	if !validVerbFormats[v.Format] {
		return oops.In("manifest").With("plugin", m.Name).With("verb", v.Type).
			With("format", v.Format).New("unknown verb format")
	}
	if !validDisplayTargets[strings.ToLower(v.DisplayTarget)] {
		return oops.In("manifest").With("plugin", m.Name).With("verb", v.Type).
			With("display_target", v.DisplayTarget).New("unknown verb display_target")
	}
	if v.Format == "speech" && v.Label == "" {
		return oops.In("manifest").With("plugin", m.Name).With("verb", v.Type).
			New("label is required when verb format is speech")
	}
	if seenVerbs[v.Type] {
		return oops.In("manifest").With("plugin", m.Name).With("verb", v.Type).
			New("duplicate verb type")
	}
	seenVerbs[v.Type] = true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run TestManifest ./internal/plugin/`
Expected: ALL PASS.

- [ ] **Step 5: Run full test suite**

Run: `task test`
Expected: ALL PASS.

- [ ] **Step 6: Regenerate JSON schema**

Run: `go generate ./internal/plugin/...`

The `//go:generate` directive at the top of `manifest.go` regenerates the manifest JSON schema. The new `Verbs` and `VerbSpec` fields need to appear in the schema.

- [ ] **Step 7: Commit**

```text
feat(plugin): add VerbSpec to manifest parsing and validation

Plugins can now declare verbs in manifest.yaml with type, category,
format, label, and display_target. Validation enforces known enum
values, speech-requires-label, duplicate detection, and setting
plugins excluded.

Part of holomush-6l7l.
```

---

### Task 3: Wire VerbRegistry into Manager and loadPlugin

**Files:**

- Modify: `internal/plugin/manager.go`
- Modify: `internal/plugin/manager_test.go`

- [ ] **Step 1: Write failing tests for verb registration in loadPlugin**

Add to `internal/plugin/manager_test.go`:

```go
func TestManagerLoadAllRegistersVerbsFromManifest(t *testing.T) {
	verbReg := core.NewVerbRegistry()
	dir := writePluginDir(t, "verb-plugin", `
name: verb-plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: custom_say
    category: communication
    format: speech
    label: "says"
    display_target: terminal
  - type: custom_action
    category: communication
    format: action
    display_target: both
`)
	host := &mockHost{}
	mgr := plugins.NewManager(dir,
		plugins.WithLuaHost(host),
		plugins.WithVerbRegistry(verbReg),
	)

	err := mgr.LoadAll(context.Background())
	require.NoError(t, err)

	reg, ok := verbReg.Lookup("custom_say")
	require.True(t, ok, "custom_say should be registered")
	assert.Equal(t, "communication", reg.Category)
	assert.Equal(t, "speech", reg.Format)
	assert.Equal(t, "says", reg.Label)
	assert.Equal(t, "verb-plugin", reg.Source)

	reg, ok = verbReg.Lookup("custom_action")
	require.True(t, ok, "custom_action should be registered")
	assert.Equal(t, "verb-plugin", reg.Source)
}

func TestManagerLoadAllRejectsPluginWithDuplicateVerbType(t *testing.T) {
	verbReg := core.NewVerbRegistry()
	// Pre-register a builtin verb
	require.NoError(t, verbReg.Register(core.VerbRegistration{
		Type: "say", Category: "communication", Format: "speech",
		Label: "says", Source: "builtin",
	}))

	dir := writePluginDir(t, "dup-verb", `
name: dup-verb
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: say
    category: communication
    format: speech
    label: "says"
    display_target: terminal
`)
	host := &mockHost{}
	mgr := plugins.NewManager(dir,
		plugins.WithLuaHost(host),
		plugins.WithVerbRegistry(verbReg),
	)

	err := mgr.LoadAll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestManagerLoadAllCleansUpVerbsOnPartialFailure(t *testing.T) {
	verbReg := core.NewVerbRegistry()
	// Pre-register "conflict" so the second verb in the manifest fails
	require.NoError(t, verbReg.Register(core.VerbRegistration{
		Type: "conflict", Category: "system", Format: "notification", Source: "builtin",
	}))

	dir := writePluginDir(t, "partial-verb", `
name: partial-verb
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: good_verb
    category: communication
    format: action
    display_target: terminal
  - type: conflict
    category: system
    format: notification
    display_target: terminal
`)
	host := &mockHost{}
	mgr := plugins.NewManager(dir,
		plugins.WithLuaHost(host),
		plugins.WithVerbRegistry(verbReg),
	)

	err := mgr.LoadAll(context.Background())
	require.Error(t, err)

	// good_verb should have been cleaned up by UnregisterBySource
	_, ok := verbReg.Lookup("good_verb")
	assert.False(t, ok, "good_verb should have been cleaned up after partial failure")
}

func TestManagerLoadAllWithoutVerbRegistrySkipsVerbRegistration(t *testing.T) {
	dir := writePluginDir(t, "no-reg", `
name: no-reg
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: orphan
    category: system
    format: notification
    display_target: terminal
`)
	host := &mockHost{}
	mgr := plugins.NewManager(dir, plugins.WithLuaHost(host))

	err := mgr.LoadAll(context.Background())
	require.NoError(t, err)
}
```

Note: The `writePluginDir` helper and `mockHost` type should already exist in `manager_test.go` from other tests. If the exact helper name differs, match the existing pattern.

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestManagerLoadAll.*Verb ./internal/plugin/`
Expected: FAIL — `WithVerbRegistry` option does not exist.

- [ ] **Step 3: Implement VerbRegistry wiring in Manager**

In `internal/plugin/manager.go`, add `verbRegistry` field to `Manager` struct:

```go
verbRegistry        *core.VerbRegistry
```

Add the `WithVerbRegistry` option function (after the existing `With*` functions):

```go
// WithVerbRegistry sets the VerbRegistry for plugin verb registration.
func WithVerbRegistry(reg *core.VerbRegistry) ManagerOption {
	return func(m *Manager) {
		m.verbRegistry = reg
	}
}
```

Add a `displayTargetFromString` helper (private function, place near the top of the file or near other helper functions):

```go
// displayTargetFromString converts a manifest display_target string to the
// proto enum. Returns EVENT_CHANNEL_UNSPECIFIED for unknown values (validation
// should catch these before this is called).
func displayTargetFromString(s string) webv1.EventChannel {
	switch strings.ToLower(s) {
	case "terminal":
		return webv1.EventChannel_EVENT_CHANNEL_TERMINAL
	case "state":
		return webv1.EventChannel_EVENT_CHANNEL_STATE
	case "both":
		return webv1.EventChannel_EVENT_CHANNEL_BOTH
	default:
		return webv1.EventChannel_EVENT_CHANNEL_UNSPECIFIED
	}
}
```

This requires adding the `webv1` import:

```go
webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
```

In `loadPlugin()`, add verb registration after the manifest warnings check (after line 668 in the current file, after `CheckManifestWarnings`) and before service registration:

```go
// Register plugin-declared verbs in the VerbRegistry.
if m.verbRegistry != nil && len(dp.Manifest.Verbs) > 0 {
	for _, vs := range dp.Manifest.Verbs {
		regErr := m.verbRegistry.Register(core.VerbRegistration{
			Type:          vs.Type,
			Category:      vs.Category,
			Format:        vs.Format,
			Label:         vs.Label,
			DisplayTarget: displayTargetFromString(vs.DisplayTarget),
			Source:        dp.Manifest.Name,
		})
		if regErr != nil {
			// Clean up any verbs already registered from this plugin.
			m.verbRegistry.UnregisterBySource(dp.Manifest.Name)
			m.unregisterPluginProviders(dp.Manifest.Name, dp.Manifest.ResourceTypes, len(dp.Manifest.ResourceTypes))
			if unloadErr := host.Unload(ctx, dp.Manifest.Name); unloadErr != nil {
				slog.Error("failed to rollback plugin load after verb registration failure",
					"plugin", dp.Manifest.Name, "error", unloadErr)
			}
			return oops.In("manager").With("plugin", dp.Manifest.Name).
				With("verb", vs.Type).Wrapf(regErr, "register plugin verb")
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run TestManagerLoadAll.*Verb ./internal/plugin/`
Expected: ALL PASS.

- [ ] **Step 5: Run full test suite**

Run: `task test`
Expected: ALL PASS.

- [ ] **Step 6: Commit**

```text
feat(plugin): wire VerbRegistry into Manager and loadPlugin

Plugin-declared verbs are registered in the VerbRegistry at load time.
Registration failures are fatal (same as bad policies or invalid
capabilities). Partial registrations are cleaned up via
UnregisterBySource on failure.

Part of holomush-6l7l.
```

---

### Task 4: Move channel verb types from builtins to channel plugin manifest

**Files:**

- Modify: `internal/core/builtins.go`
- Modify: `plugins/core-channels/plugin.yaml`
- Modify: `internal/core/registry_test.go`

- [ ] **Step 1: Write a test verifying builtins no longer include channel types**

Add to `internal/core/registry_test.go`:

```go
func TestRegisterBuiltinTypesDoesNotIncludeChannelTypes(t *testing.T) {
	r := NewVerbRegistry()
	err := RegisterBuiltinTypes(r)
	require.NoError(t, err)

	channelTypes := []string{"channel_say", "channel_pose", "channel_system"}
	for _, ct := range channelTypes {
		_, ok := r.Lookup(ct)
		assert.False(t, ok, "builtin registry should not include %s", ct)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestRegisterBuiltinTypesDoesNotIncludeChannelTypes ./internal/core/`
Expected: FAIL — channel types are currently in builtins.

- [ ] **Step 3: Remove channel verb types from builtins.go**

In `internal/core/builtins.go`, remove lines 62-74 (the entire "Channel types" block):

```go
		// Channel types (future use, registered now)
		{
			Type: "channel_say", Category: "communication", Format: "speech", Label: "says", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
			MetadataKeys: []MetadataKey{{Key: "channel", ValueType: "string", Description: "Channel name"}},
		},
		{
			Type: "channel_pose", Category: "communication", Format: "action", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
			MetadataKeys: []MetadataKey{{Key: "channel", ValueType: "string", Description: "Channel name"}},
		},
		{
			Type: "channel_system", Category: "system", Format: "notification", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
			MetadataKeys: []MetadataKey{{Key: "channel", ValueType: "string", Description: "Channel name"}},
		},
```

- [ ] **Step 4: Add verbs to channel plugin manifest**

In `plugins/core-channels/plugin.yaml`, add a `verbs:` section:

```yaml
verbs:
  - type: channel_say
    category: communication
    format: speech
    label: "says"
    display_target: terminal
  - type: channel_pose
    category: communication
    format: action
    display_target: terminal
  - type: channel_system
    category: system
    format: notification
    display_target: terminal
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- -run TestRegisterBuiltinTypes ./internal/core/`
Expected: ALL PASS.

- [ ] **Step 6: Run full test suite**

Run: `task test`
Expected: ALL PASS. If any test depended on channel types being in builtins, it will need updating. Search for `channel_say` or `channel_pose` in test files.

- [ ] **Step 7: Commit**

```text
refactor(core): move channel verb types from builtins to plugin manifest

Removes channel_say, channel_pose, channel_system from
RegisterBuiltinTypes and declares them in the core-channels plugin
manifest. This eliminates the boundary violation where plugin-owned
event types leaked into internal/core/builtins.go.

Closes holomush-6l7l.
```

---

### Task 5: Add verbs to plugin info command output

**Files:**

- Modify: `internal/command/handlers/plugin_admin.go`
- Create: `internal/command/handlers/plugin_admin_test.go` (if it does not exist, otherwise modify)

- [ ] **Step 1: Write failing test for plugin info verb display**

Check if `internal/command/handlers/plugin_admin_test.go` exists. If not, create it. Add:

```go
package handlers_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers"
	plugins "github.com/holomush/holomush/internal/plugin"
)

type stubPluginLister struct {
	plugins map[string]*plugins.DiscoveredPlugin
}

func (s *stubPluginLister) ListPlugins() []string {
	names := make([]string, 0, len(s.plugins))
	for n := range s.plugins {
		names = append(names, n)
	}
	return names
}

func (s *stubPluginLister) GetLoadedPlugin(name string) (*plugins.DiscoveredPlugin, bool) {
	dp, ok := s.plugins[name]
	return dp, ok
}

func TestPluginInfoShowsVerbs(t *testing.T) {
	lister := &stubPluginLister{
		plugins: map[string]*plugins.DiscoveredPlugin{
			"test-plugin": {
				Manifest: &plugins.Manifest{
					Name:    "test-plugin",
					Version: "1.0.0",
					Type:    plugins.TypeLua,
					Storage: plugins.StorageKV,
					Verbs: []plugins.VerbSpec{
						{Type: "custom_say", Category: "communication", Format: "speech", Label: "says", DisplayTarget: "terminal"},
						{Type: "custom_action", Category: "communication", Format: "action", DisplayTarget: "both"},
					},
				},
			},
		},
	}

	handler := handlers.NewPluginHandler(lister)
	var output string
	ctx := command.WithOutputWriter(context.Background(), func(_ context.Context, _ *command.CommandExecution, _ string, text string) {
		output = text
	})
	exec := &command.CommandExecution{Args: "info test-plugin"}

	err := handler(ctx, exec)
	require.NoError(t, err)
	assert.Contains(t, output, "custom_say (communication/speech)")
	assert.Contains(t, output, "custom_action (communication/action)")
}
```

Note: The `command.WithOutputWriter` pattern and `writeOutput` helper may work differently. Match the existing test patterns in the codebase. The key assertion is that verb type and category/format appear in the output.

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestPluginInfoShowsVerbs ./internal/command/handlers/`
Expected: FAIL — verb display not implemented.

- [ ] **Step 3: Add verb display to plugin info**

In `internal/command/handlers/plugin_admin.go`, in the `handlePluginInfo` function, after the commands block (after line 98), add:

```go
if len(m.Verbs) > 0 {
	verbDescs := make([]string, len(m.Verbs))
	for i, v := range m.Verbs {
		verbDescs[i] = fmt.Sprintf("%s (%s/%s)", v.Type, v.Category, v.Format)
	}
	fmt.Fprintf(&sb, "\nVerbs: %s", strings.Join(verbDescs, ", "))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestPluginInfoShowsVerbs ./internal/command/handlers/`
Expected: PASS.

- [ ] **Step 5: Commit**

```text
feat(plugin): show verb registrations in plugin info output

plugin info <name> now displays declared verbs as
"type (category/format)" alongside existing commands and services.

Part of holomush-6l7l.
```

---

### Task 6: Integration tests

**Files:**

- Create: `test/integration/plugin/verb_registration_test.go`

- [ ] **Step 1: Check existing integration test structure**

Read `test/integration/plugin/` to understand the Ginkgo suite setup, test container helpers, and the pattern used for existing plugin integration tests. The new tests should follow the same setup.

- [ ] **Step 2: Write integration test file**

Create `test/integration/plugin/verb_registration_test.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugin_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
)

var _ = Describe("Plugin verb registration", func() {
	Describe("loading a plugin with manifest-declared verbs", func() {
		It("registers all declared verbs in the VerbRegistry", func() {
			// Setup: create a VerbRegistry with builtins, then load a plugin
			// with verbs declared in its manifest.
			verbReg := core.NewVerbRegistry()
			Expect(core.RegisterBuiltinTypes(verbReg)).To(Succeed())

			// The core-channels plugin declares verbs in its manifest.
			// After loading, those verbs should appear in the registry.
			// Use the test harness from the existing integration suite to
			// load the plugin manager with the verb registry wired in.
			//
			// Adapt this to match the existing integration test setup pattern
			// in this directory — use the same helpers and fixtures.

			// Verify channel verbs are registered with correct source
			reg, ok := verbReg.Lookup("channel_say")
			Expect(ok).To(BeTrue(), "channel_say should be registered after plugin load")
			Expect(reg.Source).To(Equal("core-channels"))
			Expect(reg.Category).To(Equal("communication"))
			Expect(reg.Format).To(Equal("speech"))

			reg, ok = verbReg.Lookup("channel_pose")
			Expect(ok).To(BeTrue(), "channel_pose should be registered after plugin load")
			Expect(reg.Source).To(Equal("core-channels"))
		})

		It("rejects a plugin whose verb type conflicts with a builtin", func() {
			verbReg := core.NewVerbRegistry()
			Expect(core.RegisterBuiltinTypes(verbReg)).To(Succeed())

			// Attempt to load a test plugin that declares a verb with type "say"
			// (which is a builtin). Should fail.
			// Create a temp plugin dir with a conflicting verb manifest.
			// Use the same fixture pattern as existing integration tests.
		})

		It("cleans up verbs when plugin load fails partway", func() {
			verbReg := core.NewVerbRegistry()
			Expect(core.RegisterBuiltinTypes(verbReg)).To(Succeed())

			// Load a plugin with two verbs where the second conflicts.
			// Verify the first verb is cleaned up.
		})
	})
})
```

Note: The integration test bodies above are scaffolds. The implementer MUST adapt them to match the existing integration test infrastructure in `test/integration/plugin/`. Read the existing `*_test.go` files in that directory to understand the setup pattern (test containers, manager construction, fixture plugins).

- [ ] **Step 3: Run integration tests**

Run: `task test:int -- -run "Plugin verb" ./test/integration/plugin/`
Expected: ALL PASS.

- [ ] **Step 4: Commit**

```text
test(integration): add verb registration integration tests

Verifies full plugin load cycle with manifest-declared verbs,
conflict detection against builtins, and cleanup on partial failure.

Part of holomush-6l7l.
```

---

### Task 7: Final verification and cleanup

- [ ] **Step 1: Run full unit test suite**

Run: `task test`
Expected: ALL PASS.

- [ ] **Step 2: Run integration tests**

Run: `task test:int`
Expected: ALL PASS.

- [ ] **Step 3: Run linter**

Run: `task lint`
Expected: ALL PASS. Fix any issues.

- [ ] **Step 4: Run formatter**

Run: `task fmt`

- [ ] **Step 5: Run pr-prep**

Run: `task pr-prep`
Expected: ALL PASS. This mirrors all CI jobs.

- [ ] **Step 6: Update beads issue**

```bash
bd close holomush-6l7l --reason "Implemented: manifest-declared verbs with Source tracking, Unregister methods, display_target string→enum mapping, channel verbs moved from builtins to plugin manifest, plugin info display, integration tests."
```

- [ ] **Step 7: Final commit if any cleanup was needed**

Commit any formatting or lint fixes from steps 3-4.
