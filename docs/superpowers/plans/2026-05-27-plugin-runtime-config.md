<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 HoloMUSH Contributors
-->

# Plugin Runtime Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a generic plugin-runtime-config primitive — a manifest-declared typed config schema delivered to plugins via an opaque host passthrough — and adopt it in core-scenes (fixing the cfg-zero bug).

**Architecture:** A plugin declares a typed `config:` schema in its manifest (keys → type/default/required). The host reads it opaquely, merges an optional server-provided override per key (`manifest default < override`), validates generic types, and delivers the merged `map[string]string` to the plugin at init — binary via a new `ServiceConfig.plugin_config` field, Lua via the `holomush.config` global. The SDK decodes typed config (`DecodeConfig[T]` for Go; `holomush.config` accessors for Lua). The host never interprets a key's meaning.

**Tech Stack:** Go 1.24+, `github.com/go-viper/mapstructure/v2` (struct decode; already in the module graph as a koanf dep, promoted to direct), protobuf via `buf`, gopher-lua, testify (unit), Ginkgo (integration). Spec: `docs/superpowers/specs/2026-05-26-plugin-runtime-config-design.md`.

**Invariants:** INV-PC-1..8 (spec §7). Each task notes which it implements.

---

## Task 1: Manifest `ConfigParam` type + `Config` field

**Files:**

- Modify: `internal/plugin/manifest.go` (Manifest struct ~`:71`)
- Test: `internal/plugin/manifest_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/plugin/manifest_test.go`:

```go
func TestParseManifestConfigBlock(t *testing.T) {
	data := []byte(`
name: demo
version: 1.0.0
type: binary
config:
  vote_window:
    type: duration
    default: 168h
    required: true
    description: "vote collection window"
  max_attempts:
    type: int
    default: "3"
`)
	m, err := ParseManifest(data)
	require.NoError(t, err)
	require.Len(t, m.Config, 2)
	require.Equal(t, "duration", m.Config["vote_window"].Type)
	require.Equal(t, "168h", m.Config["vote_window"].Default)
	require.True(t, m.Config["vote_window"].Required)
	require.Equal(t, "int", m.Config["max_attempts"].Type)
	require.False(t, m.Config["max_attempts"].Required)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestParseManifestConfigBlock ./internal/plugin/`
Expected: FAIL — `m.Config` undefined.

- [ ] **Step 3: Add the type + field**

In `internal/plugin/manifest.go`, add near the other config helper types:

```go
// ConfigParam declares one plugin runtime config key. Opaque to the host:
// type/default/required are validated generically; the host never interprets
// what a key controls. See
// docs/superpowers/specs/2026-05-26-plugin-runtime-config-design.md.
type ConfigParam struct {
	Type        string `yaml:"type" json:"type"` // duration|int|bool|string
	Default     string `yaml:"default,omitempty" json:"default,omitempty"`
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}
```

In the `Manifest` struct (`:71`), add the field:

```go
	// Config is the plugin's runtime config schema, keyed by config key.
	// Opaque to host semantics (host validates generic types + merges values;
	// plugin owns meaning). Empty for plugins with no runtime config.
	Config map[string]ConfigParam `yaml:"config,omitempty" json:"config,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestParseManifestConfigBlock ./internal/plugin/`
Expected: PASS.

- [ ] **Step 5: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md` — message: `feat(plugin): manifest ConfigParam type + Config schema field (holomush-yzt86)`.

---

## Task 2: Config-schema validation + regenerate manifest JSON schema

**Files:**

- Create: `internal/plugin/config.go`
- Modify: `internal/plugin/manifest.go` (`Validate()` `:369`)
- Test: `internal/plugin/config_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/plugin/config_test.go`:

```go
func TestValidateConfigSchema(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]ConfigParam
		wantErr string // oops code substring; "" = no error
	}{
		{"valid duration with default", map[string]ConfigParam{"w": {Type: "duration", Default: "30m"}}, ""},
		{"valid int", map[string]ConfigParam{"n": {Type: "int", Default: "3"}}, ""},
		{"unknown type", map[string]ConfigParam{"x": {Type: "float"}}, "PLUGIN_CONFIG_SCHEMA_INVALID"},
		{"bad default for type", map[string]ConfigParam{"w": {Type: "duration", Default: "banana"}}, "PLUGIN_CONFIG_SCHEMA_INVALID"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateConfigSchema(tc.cfg)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			errutil.AssertErrorCode(t, err, tc.wantErr)
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestValidateConfigSchema ./internal/plugin/`
Expected: FAIL — `validateConfigSchema` / `parseScalar` undefined.

- [ ] **Step 3: Implement `config.go` type helpers + validation**

Create `internal/plugin/config.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugins — plugin runtime config: generic-type validation and the
// host-side merge of manifest defaults with server-provided overrides. The
// host treats keys/values opaquely w.r.t. plugin semantics; only generic
// types (duration/int/bool/string) are understood, for structural validation.
package plugins

import (
	"strconv"
	"time"

	"github.com/samber/oops"
)

// validScalar reports whether value parses to the declared generic type.
// string always validates. Used by manifest schema validation and the merge.
func validScalar(typ, value string) error {
	switch typ {
	case "duration":
		if _, err := time.ParseDuration(value); err != nil {
			return oops.With("value", value).Wrap(err)
		}
	case "int":
		if _, err := strconv.Atoi(value); err != nil {
			return oops.With("value", value).Wrap(err)
		}
	case "bool":
		if _, err := strconv.ParseBool(value); err != nil {
			return oops.With("value", value).Wrap(err)
		}
	case "string":
		// any string is valid
	default:
		return oops.With("type", typ).Errorf("unknown config type %q", typ)
	}
	return nil
}

// validateConfigSchema checks each declared config param has a known generic
// type and a parseable default (if any). Semantic meaning is plugin-owned.
func validateConfigSchema(schema map[string]ConfigParam) error {
	for key, p := range schema {
		switch p.Type {
		case "duration", "int", "bool", "string":
		default:
			return oops.Code("PLUGIN_CONFIG_SCHEMA_INVALID").
				With("key", key).With("type", p.Type).
				Errorf("config key %q: unknown type %q (want duration|int|bool|string)", key, p.Type)
		}
		if p.Default != "" {
			if err := validScalar(p.Type, p.Default); err != nil {
				return oops.Code("PLUGIN_CONFIG_SCHEMA_INVALID").
					With("key", key).Wrapf(err, "config key %q: default does not parse as %s", key, p.Type)
			}
		}
	}
	return nil
}
```

Add the import `"github.com/holomush/holomush/pkg/errutil"` to `config_test.go`.

- [ ] **Step 4: Wire into `Manifest.Validate()`**

In `internal/plugin/manifest.go` `Validate()` (`:369`), before the final `return nil`:

```go
	if err := validateConfigSchema(m.Config); err != nil {
		return err
	}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `task test -- -run TestValidateConfigSchema ./internal/plugin/`
Expected: PASS.

- [ ] **Step 6: Regenerate the manifest JSON schema**

The manifest schema is generated from the Go struct. Run: `task generate:schema`
Then verify nothing else drifted: `task test -- ./internal/plugin/` and confirm the regenerated schema artifact now includes `config`. Stage the regenerated schema file(s) (the `generate:schema` output) — they MUST be committed or CI's schema-drift check fails.

- [ ] **Step 7: Commit**

Message: `feat(plugin): validate config schema + regenerate manifest schema (holomush-yzt86)`.

---

## Task 3: Host-side merge (`MergePluginConfig`) — INV-PC-2/4/5/6

**Files:**

- Modify: `internal/plugin/config.go`
- Test: `internal/plugin/config_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/plugin/config_test.go`:

```go
func TestMergePluginConfig(t *testing.T) {
	schema := map[string]ConfigParam{
		"vote_window":    {Type: "duration", Default: "168h", Required: true},
		"cooloff_window": {Type: "duration", Default: "30m"},
		"needs_override": {Type: "int", Required: true}, // no default
	}
	t.Run("INV-PC-2 override wins; defaults fill", func(t *testing.T) {
		got, err := MergePluginConfig(schema, map[string]string{"cooloff_window": "5s", "needs_override": "1"})
		require.NoError(t, err)
		require.Equal(t, map[string]string{"vote_window": "168h", "cooloff_window": "5s", "needs_override": "1"}, got)
	})
	t.Run("INV-PC-4 missing required (no default, no override)", func(t *testing.T) {
		_, err := MergePluginConfig(schema, map[string]string{})
		errutil.AssertErrorCode(t, err, "PLUGIN_CONFIG_MISSING_REQUIRED")
	})
	t.Run("INV-PC-5 override value wrong type", func(t *testing.T) {
		_, err := MergePluginConfig(schema, map[string]string{"vote_window": "banana", "needs_override": "1"})
		errutil.AssertErrorCode(t, err, "PLUGIN_CONFIG_TYPE_INVALID")
	})
	t.Run("INV-PC-6 override key not in schema", func(t *testing.T) {
		_, err := MergePluginConfig(schema, map[string]string{"needs_override": "1", "bogus": "x"})
		errutil.AssertErrorCode(t, err, "PLUGIN_CONFIG_UNKNOWN_KEY")
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestMergePluginConfig ./internal/plugin/`
Expected: FAIL — `MergePluginConfig` undefined.

- [ ] **Step 3: Implement the merge**

Append to `internal/plugin/config.go`:

```go
// MergePluginConfig computes the effective config a plugin receives: manifest
// defaults overlaid by the server-provided override, per key (override wins).
// It enforces, opaquely w.r.t. plugin meaning:
//   - INV-PC-6: an override key not declared in schema → PLUGIN_CONFIG_UNKNOWN_KEY
//   - INV-PC-5: an effective value not parseable to its declared type → PLUGIN_CONFIG_TYPE_INVALID
//   - INV-PC-4: a required key with neither default nor override → PLUGIN_CONFIG_MISSING_REQUIRED
//
// Returns a flat map[string]string ready for opaque delivery to either runtime.
func MergePluginConfig(schema map[string]ConfigParam, override map[string]string) (map[string]string, error) {
	for k := range override {
		if _, ok := schema[k]; !ok {
			return nil, oops.Code("PLUGIN_CONFIG_UNKNOWN_KEY").
				With("key", k).Errorf("override key %q not declared in manifest config schema", k)
		}
	}
	out := make(map[string]string, len(schema))
	for key, p := range schema {
		val, has := override[key]
		if !has {
			if p.Default == "" {
				if p.Required {
					return nil, oops.Code("PLUGIN_CONFIG_MISSING_REQUIRED").
						With("key", key).Errorf("required config key %q has no default and no override", key)
				}
				continue // absent, not required → omit
			}
			val = p.Default
		}
		if err := validScalar(p.Type, val); err != nil {
			return nil, oops.Code("PLUGIN_CONFIG_TYPE_INVALID").
				With("key", key).Wrapf(err, "config key %q: value does not parse as %s", key, p.Type)
		}
		out[key] = val
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestMergePluginConfig ./internal/plugin/`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

Run: `task lint:go`. Then commit: `feat(plugin): MergePluginConfig — opaque defaults<override merge (holomush-yzt86)`.

---

## Task 4: Proto — `ServiceConfig.plugin_config`

**Files:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto` (`message ServiceConfig` `:118`)
- Regenerate: `pkg/proto/holomush/plugin/v1/*.pb.go` (via `task proto`)

- [ ] **Step 1: Add the field**

In `api/proto/holomush/plugin/v1/plugin.proto`, inside `message ServiceConfig` (after `required_services = 2;`):

```proto
  // Opaque plugin-owned runtime config: the effective (manifest-default <
  // server-override) map the host delivers at init. The host does NOT
  // interpret keys/values; the plugin decodes them per its own schema.
  map<string, string> plugin_config = 3;
```

- [ ] **Step 2: Regenerate proto Go code**

Run: `task proto`
Expected: `pkg/proto/holomush/plugin/v1/plugin.pb.go` regenerated with a `GetPluginConfig()` accessor on `ServiceConfig`.

- [ ] **Step 3: Verify it compiles + lint**

Run: `task build` then `task lint`.
Expected: build passes; `ServiceConfig.GetPluginConfig()` exists.

- [ ] **Step 4: Commit**

Message: `feat(proto): ServiceConfig.plugin_config opaque map (holomush-yzt86)`. Include the regenerated `.pb.go`.

---

## Task 5: Thread `PluginConfigOverrides` through the subsystem to the host

**Files:**

- Modify: `internal/plugin/setup/subsystem.go` (`PluginSubsystemConfig` `:89`; wiring to `Manager`)
- Modify: `internal/plugin/manager.go` (pass overrides to the binary host)
- Modify: `internal/plugin/goplugin/host.go` (Host gains per-plugin override access)
- Test: `internal/plugin/goplugin/host_test.go`

- [ ] **Step 1: Add the override field to `PluginSubsystemConfig`**

In `internal/plugin/setup/subsystem.go` `PluginSubsystemConfig` (`:89`), add:

```go
	// PluginConfigOverrides maps plugin name → (config key → value), merged
	// over each plugin's manifest config defaults at init (override wins).
	// Opaque to the host. Empty in production; tests/ops populate it. Keys
	// MUST be declared in the target plugin's manifest config schema (else
	// PLUGIN_CONFIG_UNKNOWN_KEY at load).
	PluginConfigOverrides map[string]map[string]string
```

- [ ] **Step 2: Write the failing test (host carries the override)**

In `internal/plugin/goplugin/host_test.go`:

```go
func TestHostConfigOverrideForPlugin(t *testing.T) {
	h := &Host{configOverrides: map[string]map[string]string{
		"demo": {"vote_window": "5s"},
	}}
	require.Equal(t, map[string]string{"vote_window": "5s"}, h.overrideFor("demo"))
	require.Nil(t, h.overrideFor("absent")) // no override → nil (defaults apply)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `task test -- -run TestHostConfigOverrideForPlugin ./internal/plugin/goplugin/`
Expected: FAIL — `Host.configOverrides` / `overrideFor` undefined.

- [ ] **Step 4: Add the field + accessor to `Host`**

In `internal/plugin/goplugin/host.go`, add to the `Host` struct and a helper:

```go
	// configOverrides is the per-plugin server-provided config override
	// (plugin name → key → value), threaded from PluginSubsystemConfig.
	configOverrides map[string]map[string]string
```

```go
// overrideFor returns the server-provided config override for a plugin, or nil
// when none is configured (manifest defaults then apply).
func (h *Host) overrideFor(pluginName string) map[string]string {
	return h.configOverrides[pluginName]
}
```

Thread `cfg.PluginConfigOverrides` from `subsystem.go` into the `Manager`, and from the `Manager` into the binary `Host` constructor (set `Host.configOverrides`). Follow the existing pattern by which other `PluginSubsystemConfig` fields reach the host (e.g. `schemaProvisioner`).

- [ ] **Step 5: Run test to verify it passes**

Run: `task test -- -run TestHostConfigOverrideForPlugin ./internal/plugin/goplugin/`
Expected: PASS.

- [ ] **Step 6: Build + commit**

Run: `task build`. Commit: `feat(plugin): thread PluginConfigOverrides subsystem→manager→host (holomush-yzt86)`.

---

## Task 6: Binary delivery + `needsInit` gate — INV-PC-8

**Files:**

- Modify: `internal/plugin/goplugin/host.go` (`needsInit` `:595-598`; `initReq.Config` construction)
- Test: `internal/plugin/goplugin/host_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/plugin/goplugin/host_test.go`:

```go
func TestNeedsInitIncludesConfig(t *testing.T) {
	// INV-PC-8: a config-only plugin (no requires/provides/storage/crypto)
	// MUST still be initialised so its plugin_config is delivered.
	m := &plugins.Manifest{
		Name:   "demo",
		Config: map[string]plugins.ConfigParam{"w": {Type: "duration", Default: "5s"}},
	}
	require.True(t, manifestNeedsInit(m), "config-only manifest must need Init")

	bare := &plugins.Manifest{Name: "bare"}
	require.False(t, manifestNeedsInit(bare), "manifest with nothing needs no Init")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestNeedsInitIncludesConfig ./internal/plugin/goplugin/`
Expected: FAIL — `manifestNeedsInit` undefined (the gate is currently an inline expression).

- [ ] **Step 3: Extract the gate as a function and extend it**

In `internal/plugin/goplugin/host.go`, replace the inline `needsInit := …` (`:595-598`) with a call to a new package function, and define it:

```go
// manifestNeedsInit reports whether the host must call Init on a plugin.
// Init injects services (requires/provides), provisions storage, captures
// crypto.emits (INV-S5), AND — INV-PC-8 — delivers plugin_config for any
// plugin declaring a config schema.
func manifestNeedsInit(m *plugins.Manifest) bool {
	return len(m.Requires) > 0 ||
		len(m.Provides) > 0 ||
		m.Storage == plugins.StoragePostgres ||
		(m.Crypto != nil && len(m.Crypto.Emits) > 0) ||
		len(m.Config) > 0
}
```

At the call site: `needsInit := manifestNeedsInit(manifest)`.

- [ ] **Step 4: Populate `plugin_config` in the InitRequest**

In the `if needsInit {` block, after the `initReq` is built (and after the `ConnectionString` provisioning block), add:

```go
		if len(manifest.Config) > 0 {
			merged, mergeErr := plugins.MergePluginConfig(manifest.Config, h.overrideFor(manifest.Name))
			if mergeErr != nil {
				client.Kill()
				if certDir != "" {
					_ = os.RemoveAll(certDir) //nolint:errcheck // best-effort cleanup
				}
				return oops.In("goplugin").With("plugin", manifest.Name).
					With("operation", "merge_plugin_config").Wrap(mergeErr)
			}
			initReq.Config.PluginConfig = merged
		}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- ./internal/plugin/goplugin/`
Expected: PASS (gate test + existing host tests).

- [ ] **Step 6: Build + commit**

Run: `task build`. Commit: `feat(plugin): deliver plugin_config + needsInit config gate, INV-PC-8 (holomush-yzt86)`.

---

## Task 7: SDK `DecodeConfig[T]` (Go) — typed decode

**Files:**

- Create: `pkg/plugin/config.go`
- Modify: `go.mod` (promote `go-viper/mapstructure/v2` to direct via `go mod tidy`)
- Test: `pkg/plugin/config_test.go`

- [ ] **Step 1: Write the failing test**

In `pkg/plugin/config_test.go` (package `pluginsdk`):

```go
func TestDecodeConfig(t *testing.T) {
	type demoCfg struct {
		VoteWindow time.Duration `mapstructure:"vote_window"`
		MaxTries   int           `mapstructure:"max_tries"`
		Enabled    bool          `mapstructure:"enabled"`
		Label      string        `mapstructure:"label"`
	}
	sc := &pluginv1.ServiceConfig{PluginConfig: map[string]string{
		"vote_window": "168h", "max_tries": "3", "enabled": "true", "label": "x",
	}}
	got, err := DecodeConfig[demoCfg](sc)
	require.NoError(t, err)
	require.Equal(t, 168*time.Hour, got.VoteWindow)
	require.Equal(t, 3, got.MaxTries)
	require.True(t, got.Enabled)
	require.Equal(t, "x", got.Label)
}

func TestDecodeConfigNilSafe(t *testing.T) {
	type demoCfg struct{ VoteWindow time.Duration `mapstructure:"vote_window"` }
	got, err := DecodeConfig[demoCfg](&pluginv1.ServiceConfig{}) // no plugin_config
	require.NoError(t, err)
	require.Zero(t, got.VoteWindow)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestDecodeConfig ./pkg/plugin/`
Expected: FAIL — `DecodeConfig` undefined.

- [ ] **Step 3: Implement `DecodeConfig`**

Create `pkg/plugin/config.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"github.com/go-viper/mapstructure/v2"
	"github.com/samber/oops"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// DecodeConfig decodes the host-delivered opaque plugin_config map into T,
// reading `mapstructure:` struct tags. String values are coerced to the struct
// field type (durations via StringToTimeDurationHookFunc; ints/bools via weak
// typing). Defaults and required-key enforcement are applied host-side
// (MergePluginConfig); DecodeConfig is pure string→typed conversion. koanf is
// not used — there is no file/provider layering, just one in-memory map.
func DecodeConfig[T any](config *pluginv1.ServiceConfig) (T, error) {
	var out T
	raw := make(map[string]any, len(config.GetPluginConfig()))
	for k, v := range config.GetPluginConfig() {
		raw[k] = v
	}
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook:       mapstructure.StringToTimeDurationHookFunc(),
		WeaklyTypedInput: true,
		TagName:          "mapstructure",
		Result:           &out,
	})
	if err != nil {
		return out, oops.Code("PLUGIN_CONFIG_DECODE_FAILED").Wrap(err)
	}
	if err := dec.Decode(raw); err != nil {
		return out, oops.Code("PLUGIN_CONFIG_DECODE_FAILED").Wrap(err)
	}
	return out, nil
}
```

- [ ] **Step 4: Promote mapstructure to a direct dependency**

Run: `go mod tidy` (via the repo's module tooling — confirm `task` has no wrapper; if not, run `go mod tidy` directly per repo norms). Confirm `go-viper/mapstructure/v2` moves out of the `// indirect` block in `go.mod`. No new module is added (it was already in the graph as koanf's dep).

- [ ] **Step 5: Run test to verify it passes**

Run: `task test -- -run TestDecodeConfig ./pkg/plugin/`
Expected: PASS.

- [ ] **Step 6: Build + commit**

Run: `task build`. Commit: `feat(plugin-sdk): DecodeConfig[T] typed config decode (holomush-yzt86)`.

---

## Task 8: Lua delivery — `holomush.config` + typed accessors

**Files:**

- Modify: `internal/plugin/lua/host.go` (thread merged config to the per-delivery `Register` call)
- Modify: `internal/plugin/hostfunc/functions.go` (`Register` `:162`; expose `holomush.config`)
- Create: `internal/plugin/hostfunc/config.go` (the accessor builder)
- Test: `internal/plugin/hostfunc/config_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/plugin/hostfunc/config_test.go`:

```go
func TestLuaConfigAccessors(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	mod := L.NewTable()
	registerConfigTable(L, mod, map[string]string{"vote_window": "168h", "n": "3", "on": "true", "s": "hi"})
	L.SetGlobal("holomush", mod)

	require.NoError(t, L.DoString(`
		assert(holomush.config.duration("vote_window") == 168*60*60)  -- seconds
		assert(holomush.config.int("n") == 3)
		assert(holomush.config.bool("on") == true)
		assert(holomush.config.string("s") == "hi")
		assert(holomush.config.duration("absent") == nil)
		local ok = pcall(function() holomush.config.require_duration("absent") end)
		assert(ok == false)  -- require_* errors when absent
	`))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestLuaConfigAccessors ./internal/plugin/hostfunc/`
Expected: FAIL — `registerConfigTable` undefined.

- [ ] **Step 3: Implement the accessor builder**

Create `internal/plugin/hostfunc/config.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"strconv"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// registerConfigTable installs holomush.config on mod, exposing the host's
// merged (opaque string) config map with typed accessors that mirror the Go
// SDK (DecodeConfig). duration returns seconds (Lua number); require_* error
// when the key is absent. Conversion uses the same fail-loud discipline as the
// rest of the Lua host.
func registerConfigTable(L *lua.LState, mod *lua.LTable, cfg map[string]string) {
	c := L.NewTable()
	get := func(key string) (string, bool) { v, ok := cfg[key]; return v, ok }

	dur := func(require bool) lua.LGFunction {
		return func(L *lua.LState) int {
			key := L.CheckString(1)
			v, ok := get(key)
			if !ok {
				if require {
					L.RaiseError("holomush.config.require_duration: missing key %q", key)
				}
				L.Push(lua.LNil)
				return 1
			}
			d, err := time.ParseDuration(v)
			if err != nil {
				L.RaiseError("holomush.config: key %q value %q is not a duration", key, v)
			}
			L.Push(lua.LNumber(d.Seconds()))
			return 1
		}
	}
	intFn := func(require bool) lua.LGFunction {
		return func(L *lua.LState) int {
			key := L.CheckString(1)
			v, ok := get(key)
			if !ok {
				if require {
					L.RaiseError("holomush.config.require_int: missing key %q", key)
				}
				L.Push(lua.LNil)
				return 1
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				L.RaiseError("holomush.config: key %q value %q is not an int", key, v)
			}
			L.Push(lua.LNumber(n))
			return 1
		}
	}
	boolFn := func(L *lua.LState) int {
		key := L.CheckString(1)
		v, ok := get(key)
		if !ok {
			L.Push(lua.LNil)
			return 1
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			L.RaiseError("holomush.config: key %q value %q is not a bool", key, v)
		}
		L.Push(lua.LBool(b))
		return 1
	}
	strFn := func(L *lua.LState) int {
		key := L.CheckString(1)
		if v, ok := get(key); ok {
			L.Push(lua.LString(v))
		} else {
			L.Push(lua.LNil)
		}
		return 1
	}

	L.SetField(c, "duration", L.NewFunction(dur(false)))
	L.SetField(c, "require_duration", L.NewFunction(dur(true)))
	L.SetField(c, "int", L.NewFunction(intFn(false)))
	L.SetField(c, "require_int", L.NewFunction(intFn(true)))
	L.SetField(c, "bool", L.NewFunction(boolFn))
	L.SetField(c, "string", L.NewFunction(strFn))
	L.SetField(mod, "config", c)
}
```

- [ ] **Step 4: Thread config into `Register`**

In `internal/plugin/hostfunc/functions.go`, give `Functions` a per-call config map. Add a field `pluginConfig map[string]string` set by a setter, OR add a parameter. Minimal change: add a setter and call `registerConfigTable` inside `Register` just before `ls.SetGlobal("holomush", mod)` (`:230`):

```go
	registerConfigTable(ls, mod, f.pluginConfigFor(pluginName))
```

Add to `Functions`:

```go
	// pluginConfigs holds the merged (opaque) config per plugin, set by the
	// Lua host before Register. nil/absent → empty holomush.config.
	pluginConfigs map[string]map[string]string
```

```go
func (f *Functions) SetPluginConfigs(c map[string]map[string]string) { f.pluginConfigs = c }
func (f *Functions) pluginConfigFor(name string) map[string]string    { return f.pluginConfigs[name] }
```

- [ ] **Step 5: Populate the merged config in the Lua host**

Thread `PluginConfigOverrides` into the Lua host via a functional option mirroring the existing `WithCPUTimeout` (`internal/plugin/lua/host.go:51` `type HostOption func(*Host)`, `:57`). Add to `host.go`:

```go
// WithPluginConfigOverrides threads the server-provided per-plugin config
// overrides into the Lua host (mirrors the binary host's configOverrides).
func WithPluginConfigOverrides(o map[string]map[string]string) HostOption {
	return func(h *Host) { h.configOverrides = o }
}
```

Add `configOverrides map[string]map[string]string` and `mergedConfigs map[string]map[string]string` fields to the Lua `Host` struct. In `Host.Load(ctx, manifest, dir)` (`:137`), once `manifest` is in hand, compute and stash the merge (fail the load on error, mirroring the binary host):

```go
	merged, err := plugins.MergePluginConfig(manifest.Config, h.configOverrides[manifest.Name])
	if err != nil {
		return oops.In("lua").With("plugin", manifest.Name).With("operation", "merge_plugin_config").Wrap(err)
	}
	if h.mergedConfigs == nil {
		h.mergedConfigs = map[string]map[string]string{}
	}
	h.mergedConfigs[manifest.Name] = merged
```

Before each per-delivery `h.hostFuncs.Register(L, name, requires...)` (in `DeliverEvent`/`DeliverCommand`/`QuerySessionStreams`), call `h.hostFuncs.SetPluginConfigs(h.mergedConfigs)` so `registerConfigTable` (Step 3) — invoked inside `Register` via `f.pluginConfigFor(name)` (Step 4) — sees the merged map.

Wire the option where the Lua host is built (`internal/plugin/setup/subsystem.go:182`):

```go
	luaHost := pluginlua.NewHostWithFunctions(
		luaFns,
		// ...existing opts...
		pluginlua.WithPluginConfigOverrides(cfg.PluginConfigOverrides),
	)
```

This is the same functional-option threading the binary host uses for its provider deps (Task 5's `schemaProvisioner` pattern).

- [ ] **Step 6: Run tests to verify they pass**

Run: `task test -- ./internal/plugin/hostfunc/ ./internal/plugin/lua/`
Expected: PASS.

- [ ] **Step 7: Build + commit**

Run: `task build`. Commit: `feat(plugin): Lua holomush.config typed accessors (holomush-yzt86)`.

---

## Task 9: core-scenes — declare `config:` in the manifest

**Files:**

- Modify: `plugins/core-scenes/plugin.yaml`

- [ ] **Step 1: Add the config block**

In `plugins/core-scenes/plugin.yaml`, add a top-level `config:` block:

```yaml
config:
  vote_window:
    type: duration
    default: 168h
    description: "How long a publish-vote attempt collects votes before timeout."
  cooloff_window:
    type: duration
    default: 30m
    description: "Delay between unanimous-yes resolution and PUBLISHED."
  scheduler_interval:
    type: duration
    default: 30s
    description: "How often the publish scheduler sweeps for expired attempts."
```

- [ ] **Step 2: Verify the manifest still validates**

Run: `task test -- -run TestParseManifest ./internal/plugin/` (and the plugin's own manifest test if present).
Expected: PASS — the schema parses and validates.

- [ ] **Step 3: Commit**

Message: `feat(scenes): declare publish window/interval config in manifest (holomush-yzt86)`.

---

## Task 10: core-scenes — manifest-source config in `Init` AND tests; fix cfg-zero — INV-PC-7

The manifest is the single config source for **both** production and tests (no
hardcoded Go window default). Production: `Init` → `applyConfig`. Tests: a
`newTestService` helper sources from the real manifest via the same `applyConfig`
path. `DefaultSceneServiceConfig()` is removed; the ~12 test call sites migrate.

**Files:**

- Modify: `plugins/core-scenes/main.go` (`Init` ~`:155`; scheduler construction `:197-203`; add `applyConfig` + `schedInterval`)
- Modify: `plugins/core-scenes/service.go` (`NewSceneServiceImpl` `:150` — drop the `cfg: DefaultSceneServiceConfig()` seed)
- Modify: `plugins/core-scenes/publish_helpers.go` (`:114` — delete `DefaultSceneServiceConfig()`)
- Create: `plugins/core-scenes/testhelpers_test.go` (`//go:embed plugin.yaml` + `manifestServiceConfig` + `newTestService`)
- Migrate (`NewSceneServiceImpl(` → `newTestService(t, `): `service_test.go`, `publish_service_test.go`, `publish_snapshot_integration_test.go`, `service_publish_gate_test.go`, `service_public_archive_test.go`, `service_privacy_block_test.go`, `commands_test.go`, `commands_publish_test.go`, `commands_log_test.go`, `commands_emit_test.go`, `publish_scheduler_integration_test.go`
- Modify: `plugins/core-scenes/publish_helpers_test.go` (remove `TestDefaultSceneServiceConfigMatchesSpecDefaults` — the pinned function is gone; manifest is the sole source)
- Test: `plugins/core-scenes/main_test.go`

- [ ] **Step 1: Write the failing test (INV-PC-7 regression lock)**

In `plugins/core-scenes/main_test.go`:

```go
func TestInitAppliesManifestConfig(t *testing.T) {
	p := &scenePlugin{service: &SceneServiceImpl{}}
	cfg := &pluginv1.ServiceConfig{PluginConfig: map[string]string{
		"vote_window": "168h", "cooloff_window": "30m", "scheduler_interval": "30s",
	}}
	require.NoError(t, p.applyConfig(cfg))
	require.Equal(t, 168*time.Hour, p.service.cfg.DefaultVoteWindow)
	require.Equal(t, 30*time.Minute, p.service.cfg.DefaultCoolOffWindow)
	require.Equal(t, 30*time.Second, p.schedInterval)
	// cfg-zero regression: never the zero value.
	require.NotZero(t, p.service.cfg.DefaultVoteWindow)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestInitAppliesManifestConfig ./plugins/core-scenes/`
Expected: FAIL — `applyConfig` / `schedInterval` undefined.

- [ ] **Step 3: Add `applyConfig` and a scheduler-interval field**

In `plugins/core-scenes/main.go`, add a `schedInterval time.Duration` field to `scenePlugin` and:

```go
type sceneConfig struct {
	VoteWindow        time.Duration `mapstructure:"vote_window"`
	CoolOffWindow     time.Duration `mapstructure:"cooloff_window"`
	SchedulerInterval time.Duration `mapstructure:"scheduler_interval"`
}

// applyConfig decodes the host-delivered plugin_config and applies it to the
// service config + scheduler interval. Defaults come from the manifest
// (plugin.yaml config:), so the service cfg is never the zero value (fixes the
// cfg-zero bug where main built &SceneServiceImpl{} with no defaults).
func (p *scenePlugin) applyConfig(config *pluginv1.ServiceConfig) error {
	sc, err := pluginsdk.DecodeConfig[sceneConfig](config)
	if err != nil {
		return oops.Code("SCENE_INIT_FAILED").Wrap(err)
	}
	p.service.cfg = SceneServiceConfig{
		DefaultVoteWindow:    sc.VoteWindow,
		DefaultCoolOffWindow: sc.CoolOffWindow,
	}
	p.schedInterval = sc.SchedulerInterval
	return nil
}
```

- [ ] **Step 4: Call it from `Init` and wire the scheduler interval**

In `Init` (`main.go:155`), call `p.applyConfig(config)` early (after the nil-conn check), and change the scheduler construction (`:197-203`) to use the configured interval:

```go
	if err := p.applyConfig(config); err != nil {
		return err
	}
	// ...
	sched := &publishScheduler{
		svc:      p.service,
		store:    store,
		interval: p.schedInterval, // from manifest config (was hardcoded 30s)
		now:      time.Now,
	}
```

- [ ] **Step 5: Add the manifest-sourced test helper (single config source for tests)**

Create `plugins/core-scenes/testhelpers_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	_ "embed"
	"testing"

	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

//go:embed plugin.yaml
var testManifestYAML []byte

// manifestServiceConfig returns the ServiceConfig the host would deliver for
// core-scenes with no override — sourced from the real manifest. Tests and
// production thus share ONE config source (the manifest); there is no Go
// window default to drift from it.
func manifestServiceConfig(t *testing.T) *pluginv1.ServiceConfig {
	t.Helper()
	m, err := plugins.ParseManifest(testManifestYAML)
	require.NoError(t, err)
	merged, err := plugins.MergePluginConfig(m.Config, nil)
	require.NoError(t, err)
	return &pluginv1.ServiceConfig{PluginConfig: merged}
}

// newTestService builds a SceneServiceImpl whose config is applied the same way
// production applies it (applyConfig over the manifest-sourced ServiceConfig).
func newTestService(t *testing.T, store sceneStorer) *SceneServiceImpl {
	t.Helper()
	p := &scenePlugin{service: NewSceneServiceImpl(store)}
	require.NoError(t, p.applyConfig(manifestServiceConfig(t)))
	return p.service
}
```

- [ ] **Step 6: Remove the Go default; migrate the test call sites**

In `service.go` `NewSceneServiceImpl` (`:150`), drop the `cfg: DefaultSceneServiceConfig()` field — the constructor no longer seeds windows; `applyConfig` is the sole setter. In `publish_helpers.go`, delete `DefaultSceneServiceConfig()`. In `publish_helpers_test.go`, delete `TestDefaultSceneServiceConfigMatchesSpecDefaults` (the function it pins is gone; the manifest is the single source, so there is nothing to drift). Then migrate every `NewSceneServiceImpl(<store>)` call in the 11 listed test files to `newTestService(t, <store>)` (mechanical; test helper funcs that build a service gain a `t *testing.T` param; `&scenePlugin{service: NewSceneServiceImpl(store)}` → `&scenePlugin{service: newTestService(t, store)}`).

Production (`main.go`) keeps `&SceneServiceImpl{}` in `main()` (the store is injected in `Init`); `applyConfig` (Step 4) sets `cfg` from the manifest. So both prod and tests derive config from the manifest — consistency without forcing the constructor through a store it doesn't have at `main()` time.

- [ ] **Step 7: Run tests**

Run: `task test -- ./plugins/core-scenes/`
Expected: PASS — the migrated suite (services now manifest-sourced) plus the new `TestInitAppliesManifestConfig`. No package references `DefaultSceneServiceConfig` any more.

- [ ] **Step 8: Build + lint + commit**

Run: `task build` then `task lint:go`. Commit: `fix(scenes): manifest-source publish config in Init + tests; fix cfg-zero, INV-PC-7 (holomush-yzt86)`.

---

## Task 11: Meta-tests — INV-PC-1 host opacity + INV-PC enumeration

**Files:**

- Create: `internal/plugin/config_opacity_test.go`
- Create/Modify: `test/meta/plugin_config_invariants_test.go`

- [ ] **Step 1: Write the host-opacity test (INV-PC-1)**

In `internal/plugin/config_opacity_test.go`:

```go
// INV-PC-1: the host treats config opaquely — it MUST NOT contain literals of
// any plugin's config keys (it understands only generic types). This pins the
// boundary against a future host edit that special-cases a plugin key.
func TestHostDoesNotReferencePluginConfigKeys(t *testing.T) {
	bannedKeys := []string{"vote_window", "cooloff_window", "scheduler_interval"}
	pkgs := []string{".", "setup", "goplugin", "hostfunc", "lua"} // internal/plugin/...
	for _, pkg := range pkgs {
		dir := filepath.Join(".", pkg)
		matches := grepGoLiterals(t, dir, bannedKeys) // helper: rg-style scan of non-test .go
		require.Empty(t, matches, "host pkg %s must not reference plugin config key literals: %v", pkg, matches)
	}
}
```

(Implement `grepGoLiterals` as a small filepath.Walk + token scan over non-`_test.go` files, or shell out to `rg`. Keep it test-local.)

- [ ] **Step 2: Run test to verify it passes (it should already hold)**

Run: `task test -- -run TestHostDoesNotReferencePluginConfigKeys ./internal/plugin/`
Expected: PASS — no host code references those keys (they live only in `plugins/core-scenes/`).

- [ ] **Step 3: Write the INV-PC enumeration meta-test**

In `test/meta/plugin_config_invariants_test.go`:

```go
// TestPluginConfigInvariantsHaveTestCoverage asserts every INV-PC-N (spec §7)
// is cited by at least one test in the tree.
func TestPluginConfigInvariantsHaveTestCoverage(t *testing.T) {
	want := []string{"INV-PC-1", "INV-PC-2", "INV-PC-3", "INV-PC-4",
		"INV-PC-5", "INV-PC-6", "INV-PC-7", "INV-PC-8"}
	cited := scanTreeForInvariantCitations(t, "INV-PC-") // walk *_test.go for the IDs
	for _, id := range want {
		require.Contains(t, cited, id, "no test cites %s", id)
	}
}
```

Ensure each test added in Tasks 3/6/10 cites its INV-PC ID in a comment or `t.Run` name (they do per the code above). Add an explicit INV-PC-3 unit test on `MergePluginConfig` output identity (the single merged map is what both delivery paths receive) so INV-PC-3 is cited.

- [ ] **Step 4: Run the meta-test**

Run: `task test -- -run TestPluginConfigInvariants ./test/meta/`
Expected: PASS once all IDs are cited.

- [ ] **Step 5: Commit**

Message: `test(plugin): INV-PC-1 opacity + INV-PC enumeration meta-tests (holomush-yzt86)`.

---

## Task 12: Docs — plugin config for plugin authors

**Files:**

- Create: `site/docs/extending/plugin-config.md`
- Modify: the `extending/` nav/index if one exists (follow the existing pattern)

- [ ] **Step 1: Write the doc**

Create `site/docs/extending/plugin-config.md` covering: the manifest `config:` schema (keys, the four types, `default`/`required`/`description`); the merge/precedence (`manifest default < server override`); generic-type validation + the three error codes; the Go `DecodeConfig[T]` helper with the `mapstructure:` struct-tag example; the Lua `holomush.config` accessors (`duration`/`int`/`bool`/`string`/`require_*`). Use a core-scenes example (`vote_window`/`cooloff_window`/`scheduler_interval`). Mirror the structure/tone of an existing `site/docs/extending/` page.

- [ ] **Step 2: Build the docs site**

Run: `task docs:build`
Expected: builds without broken-link/lint errors.

- [ ] **Step 3: Commit**

Message: `docs(extending): plugin runtime config guide (holomush-yzt86)`.

---

## Final verification

- [ ] Run `task test` — all unit tests pass.
- [ ] Run `task test:int -- ./internal/plugin/...` — integration tests pass (Docker required).
- [ ] Run `task lint` and `task fmt` — clean.
- [ ] Confirm INV-PC-1..8 each have a citing test (`task test -- -run TestPluginConfigInvariants ./test/meta/`).
- [ ] Confirm `go.mod` shows `go-viper/mapstructure/v2` as a direct dependency and `go mod tidy` is a no-op.
<!-- adr-capture: sha256=26f468042a1331c5; ts=2026-05-27T13:28:09Z; adrs=holomush-7pdhf,holomush-ikozq -->
