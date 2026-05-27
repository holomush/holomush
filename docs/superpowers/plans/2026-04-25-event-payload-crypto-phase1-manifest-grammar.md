<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Event Payload Cryptography — Phase 1: Manifest Grammar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make plugin manifests the source of truth for event types and their sensitivity declarations. Move plugin-owned `EventType` constants out of `internal/core/event.go`, extend `plugin.yaml` with a `crypto.emits` block, validate it at install time, expose it via CLI introspection and auto-generated reference docs.

**Architecture:** Plugin manifests gain a `crypto` section with `emits` (per-event-type sensitivity declarations) and `consumes` (decryption opt-in for subscribed event types). The `internal/plugin` loader validates the new section before allowing a plugin to load. Each plugin owns its `EventType` constants in a per-plugin `events.go` file using qualified names (`<plugin>:<event>`). The host's `internal/core/event.go` keeps only host-owned types (`EventTypeSystem`, `EventTypeSessionEnded`, `EventTypeCommandResponse`, `EventTypeCommandError`).

**Tech Stack:** Go 1.25, yaml.v3, samber/oops, gopher-lua (for Lua plugin event-type strings), zensical (for doc generation), Taskfile, jsonschema validation.

**Spec:** `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` (Section 7.1, Section 11.1 phase 1, INV-6, INV-7, INV-45)

**Bead:** `holomush-k18g` (re-scoped 2026-04-25 to include sensitivity declarations)

**Scope:** Phase 1 only. Phases 2–8 (provider interface, EventSink encryption, lifecycle ops, CLI break-glass, Vault, plugin SDK, docs) get separate plans.

**Out of scope for this phase:**

- Any actual encryption. Phase 1 produces declarations only; runtime enforcement of `Sensitive=true` lands in Phase 3.
- The `crypto.consumes` block is **declared in this plan's manifest schema** but never evaluated at runtime — it's recorded for forward compatibility so plugins can author it now and Phase 3 picks it up.
- The `requests_decryption` ABAC policy resource (`dek:<context_type>:<context_id>`) lands in Phase 3.
- **Host event-type string-value changes.** Per spec §11.1, "existing plaintext events stay plaintext forever" and there is no retroactive data migration. Host-owned event types (`system`, `arrive`, `leave`, `move`, `command_response`, `command_error`, `location_state`, `exit_update`) keep their current bare string values in Phase 1. The qualified-name story for host events is deferred to Phase 3 alongside the runtime gates that can reason about it. Plugin-owned event types DO migrate to qualified form in this phase because their wire impact is contained to plugin code paths (covered by Tasks 5 and 5b).

---

## File Structure

### New files

| File | Responsibility |
| --- | --- |
| `internal/plugin/crypto_manifest.go` | `CryptoSection`, `CryptoEmit`, `CryptoConsume`, `Sensitivity` types + parsing |
| `internal/plugin/crypto_manifest_test.go` | Unit tests for parsing and validation |
| `internal/plugin/crypto_validator.go` | `ValidateCrypto` function applied during manifest load |
| `internal/plugin/crypto_validator_test.go` | Unit tests for validator |
| `plugins/core-communication/events.go` | Plugin-owned `EventType` constants (Say, Pose, Page, Whisper, Pemit, OOC, WhisperNotice, Arrive, Leave, Move) |
| `plugins/core-communication/events_test.go` | Round-trip and qualified-name tests |
| `plugins/core-objects/events.go` | Plugin-owned constants (ObjectCreate, ObjectDestroy, ObjectUse, ObjectExamine, ObjectGive) |
| `plugins/core-objects/events_test.go` | Unit tests |
| `plugins/core-scenes/events.go` | Plugin-owned constants (SceneCreate, SceneJoin, SceneLeave, SceneIC, etc.) |
| `plugins/core-scenes/events_test.go` | Unit tests |
| `cmd/holomush/cmd_plugin.go` | NEW parent cobra command `holomush plugin` (none exists today; Tasks 8+9 hang from this) |
| `cmd/holomush/cmd_plugin_test.go` | Test for the parent group registration |
| `cmd/holomush/cmd_plugin_validate.go` | `holomush plugin validate` subcommand |
| `cmd/holomush/cmd_plugin_events.go` | `holomush plugin events list/show` subcommands |
| `cmd/holomush/cmd_plugin_events_test.go` | CLI integration tests |
| `cmd/holomush/test_helper_test.go` | Shared `runCmd(t, args)` helper for cobra-based tests |
| `scripts/gen-event-docs.sh` | Generates `site/docs/reference/events/<plugin>.md` from manifests |
| `scripts/validate-plugin.sh` | Build-time manifest validator (`task plugin:validate`) |
| `gorules/qualified_event_type.go` | Optional ruleguard rule rejecting bare event-type literals |

### Modified files

| File | Change |
| --- | --- |
| `internal/plugin/manifest.go` | Add `Crypto *CryptoSection` field to `Manifest`; update jsonschema generation |
| `internal/plugin/manifest_test.go` | Tests for the new field |
| `internal/plugin/manager.go` | Call `ValidateCrypto` after manifest parse |
| `internal/plugin/manager_test.go` | Tests for crypto-validation integration |
| `internal/core/event.go` | Remove plugin-owned constants (Say, Pose, ObjectCreate/Destroy/Use/Examine/Give, Page, Whisper, Pemit, OOC, WhisperNotice). Keep host-owned with **unchanged string values** (System, SessionEnded, CommandResponse, CommandError, LocationState, ExitUpdate, Arrive, Leave, Move). |
| `pkg/plugin/event.go` | Remove convenience constants (`EventTypeSay`, `EventTypePose`, `EventTypeEmit`, `EventTypeArrive`, `EventTypeLeave`, `EventTypeSystem`). Keep the `EventType` type itself, `Event`, `EmitEvent`, `EmitIntent`, `ActorKind` — those are SDK-shape, not constants. |
| `internal/core/event_test.go` | Update tests against the reduced constant set |
| `internal/world/*.go` (call sites) | Replace bare `core.EventTypeSay` with `corecomm.EventTypeSay` etc. |
| `internal/grpc/*.go` (call sites) | Same |
| `internal/telnet/*.go` (call sites) | Same |
| `internal/store/*.go` (call sites) | Same |
| `plugins/core-communication/main.lua` | Use qualified event-type strings (`"core-communication:say"` instead of `"say"`) |
| `plugins/core-aliases/plugin.yaml` | Add `crypto.emits` block |
| `plugins/core-building/plugin.yaml` | Add `crypto.emits` block |
| `plugins/core-communication/plugin.yaml` | Add `crypto.emits` block |
| `plugins/core-help/plugin.yaml` | Add `crypto.emits` block |
| `plugins/core-objects/plugin.yaml` | Add `crypto.emits` block |
| `plugins/core-scenes/plugin.yaml` | Add `crypto.emits` block |
| `plugins/echo-bot/plugin.yaml` | Add `crypto.emits` block |
| `plugins/setting-crossroads/plugin.yaml` | Add `crypto.emits` block (if it emits events) |
| `plugins/setting-skeleton/plugin.yaml` | Add `crypto.emits` block (if it emits events) |
| `plugins/test-abac-widget/plugin.yaml` | Add `crypto.emits` block (if it emits events) |
| `Taskfile.yaml` | Add `plugin:validate` and `docs:gen-events` targets |
| `site/docs/reference/events.md` | Index page linking per-plugin pages |
| `site/docs/extending/event-sensitivity.md` | NEW — author-facing guide for `crypto.emits` (full content shown in Task 11) |

---

## Task 1: Define `CryptoSection` types and YAML parsing

**Files:**

- Create: `internal/plugin/crypto_manifest.go`
- Create: `internal/plugin/crypto_manifest_test.go`

- [ ] **Step 1: Write failing test for parsing the crypto block from YAML**

```go
// internal/plugin/crypto_manifest_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	plugins "github.com/holomush/holomush/internal/plugin"
)

func TestCryptoSectionParsesEmitsBlock(t *testing.T) {
	src := `
emits:
  - event_type: whisper
    sensitivity: always
    description: "Direct character-to-character private message."
  - event_type: pose
    sensitivity: may
  - event_type: presence
    sensitivity: never
`
	var got plugins.CryptoSection
	require.NoError(t, yaml.Unmarshal([]byte(src), &got))

	require.Len(t, got.Emits, 3)
	assert.Equal(t, "whisper", got.Emits[0].EventType)
	assert.Equal(t, plugins.SensitivityAlways, got.Emits[0].Sensitivity)
	assert.Equal(t, "Direct character-to-character private message.", got.Emits[0].Description)
	assert.Equal(t, plugins.SensitivityMay, got.Emits[1].Sensitivity)
	assert.Equal(t, plugins.SensitivityNever, got.Emits[2].Sensitivity)
}

func TestCryptoSectionParsesConsumesBlock(t *testing.T) {
	src := `
consumes:
  - subjects:
      - "events.*.character.*.whisper"
    requests_decryption:
      - "core-communication:whisper"
`
	var got plugins.CryptoSection
	require.NoError(t, yaml.Unmarshal([]byte(src), &got))

	require.Len(t, got.Consumes, 1)
	assert.Equal(t, []string{"events.*.character.*.whisper"}, got.Consumes[0].Subjects)
	assert.Equal(t, []string{"core-communication:whisper"}, got.Consumes[0].RequestsDecryption)
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `task test -- -run "TestCryptoSection" ./internal/plugin/`

Expected: FAIL with `undefined: plugins.CryptoSection`.

- [ ] **Step 3: Implement `CryptoSection` types**

```go
// internal/plugin/crypto_manifest.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

// Sensitivity classifies an event type's payload protection contract.
//
//   - SensitivityAlways: every event of this type MUST be emitted with
//     Sensitive=true. Emit-time enforcement lands in Phase 3.
//   - SensitivityMay: the emit-site decides per-event via the Sensitive
//     flag. The plugin's emit code carries the runtime decision.
//   - SensitivityNever: the event type is never sensitive. Emit-time
//     attempts to set Sensitive=true on this type are rejected.
type Sensitivity string

// Sensitivity contract values declared in the manifest's crypto.emits block.
const (
	SensitivityAlways Sensitivity = "always"
	SensitivityMay    Sensitivity = "may"
	SensitivityNever  Sensitivity = "never"
)

// CryptoSection is the manifest's `crypto:` block. Optional; absence
// means the plugin emits no sensitive events and consumes no sensitive
// subjects (every emit is treated as if declared SensitivityNever).
type CryptoSection struct {
	Emits    []CryptoEmit    `yaml:"emits,omitempty" json:"emits,omitempty"`
	Consumes []CryptoConsume `yaml:"consumes,omitempty" json:"consumes,omitempty"`
}

// CryptoEmit declares one event type this plugin emits, plus its
// sensitivity contract.
type CryptoEmit struct {
	EventType   string      `yaml:"event_type" json:"event_type"`
	Sensitivity Sensitivity `yaml:"sensitivity" json:"sensitivity"`
	Description string      `yaml:"description,omitempty" json:"description,omitempty"`
}

// CryptoConsume declares a set of subjects the plugin subscribes to and,
// per-event-type, whether the plugin requests plaintext (decryption) for
// sensitive events of those types.
//
// Phase 1 records this declaration but does not enforce it at runtime.
// Phase 3's AuthGuard reads it.
type CryptoConsume struct {
	Subjects           []string `yaml:"subjects" json:"subjects"`
	RequestsDecryption []string `yaml:"requests_decryption,omitempty" json:"requests_decryption,omitempty"`
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `task test -- -run "TestCryptoSection" ./internal/plugin/`

Expected: PASS.

- [ ] **Step 5: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`. Suggested message:

```text
feat(plugin): CryptoSection types for crypto.emits/consumes manifest block

Phase 1 of event-payload-crypto. Adds the manifest types (Sensitivity,
CryptoSection, CryptoEmit, CryptoConsume) without yet wiring them into
the loader or enforcing them at runtime.

Refs: docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md §7.1
Refs: docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md
```

---

## Task 2: Wire `Crypto *CryptoSection` into `Manifest`

**Files:**

- Modify: `internal/plugin/manifest.go:70-95`
- Modify: `internal/plugin/manifest_test.go`

- [ ] **Step 1: Write failing test that an existing manifest YAML can carry the crypto block**

Append to `internal/plugin/manifest_test.go`:

```go
func TestManifestCarriesCryptoSection(t *testing.T) {
	src := `
name: test-plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
crypto:
  emits:
    - event_type: foo
      sensitivity: always
`
	m, err := plugins.ParseManifest([]byte(src))
	require.NoError(t, err)
	require.NotNil(t, m.Crypto)
	require.Len(t, m.Crypto.Emits, 1)
	assert.Equal(t, "foo", m.Crypto.Emits[0].EventType)
	assert.Equal(t, plugins.SensitivityAlways, m.Crypto.Emits[0].Sensitivity)
}

func TestManifestWithoutCryptoSectionLeavesCryptoNil(t *testing.T) {
	src := `
name: test-plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`
	m, err := plugins.ParseManifest([]byte(src))
	require.NoError(t, err)
	assert.Nil(t, m.Crypto)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run "TestManifestCarriesCryptoSection|TestManifestWithoutCryptoSection" ./internal/plugin/`

Expected: FAIL — `m.Crypto` does not exist on the `Manifest` struct.

- [ ] **Step 3: Add `Crypto` field to `Manifest`**

Edit `internal/plugin/manifest.go` after the existing `Storage StorageType` line in the `Manifest` struct (the spec spot is around line 94):

```go
	// Crypto declares the plugin's event-type sensitivity contracts and
	// (forward-looking) decryption opt-in subscriptions. Phase 1 of the
	// event-payload-crypto design records these declarations; Phase 3
	// adds runtime enforcement.
	Crypto *CryptoSection `yaml:"crypto,omitempty" json:"crypto,omitempty"`
```

- [ ] **Step 4: Run to verify pass**

Run: `task test -- -run "TestManifestCarriesCryptoSection|TestManifestWithoutCryptoSection" ./internal/plugin/`

Expected: PASS.

- [ ] **Step 5: Regenerate jsonschema and verify lint clean**

Run: `task lint`

Expected: PASS. If `go generate` is wired into lint, it regenerates the manifest schema; otherwise run `go generate ./internal/plugin/...` explicitly.

- [ ] **Step 6: Commit**

Suggested message:

```text
feat(plugin): add optional Crypto section to Manifest

Manifests can now carry a crypto.emits/consumes block. Loader does not
validate or enforce yet — Task 3 adds validation, Phase 3 adds runtime
enforcement.

Refs: docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md
```

---

## Task 3: Validator for `crypto.emits` and `crypto.consumes`

**Files:**

- Create: `internal/plugin/crypto_validator.go`
- Create: `internal/plugin/crypto_validator_test.go`

- [ ] **Step 1: Write failing tests covering all six rules from spec §7.1**

```go
// internal/plugin/crypto_validator_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestValidateCryptoAcceptsValidManifest(t *testing.T) {
	m := &plugins.Manifest{
		Name: "core-communication",
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "whisper", Sensitivity: plugins.SensitivityAlways},
				{EventType: "say", Sensitivity: plugins.SensitivityNever},
			},
			Consumes: []plugins.CryptoConsume{{
				Subjects:           []string{"events.*.character.*.whisper"},
				RequestsDecryption: []string{"core-communication:whisper"},
			}},
		},
		Dependencies: map[string]string{}, // self-reference allowed
	}
	require.NoError(t, plugins.ValidateCrypto(m))
}

func TestValidateCryptoRejectsUnknownSensitivity(t *testing.T) {
	m := &plugins.Manifest{
		Name: "x",
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "foo", Sensitivity: "kinda"},
			},
		},
	}
	err := plugins.ValidateCrypto(m)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_INVALID_SENSITIVITY")
}

func TestValidateCryptoRejectsDuplicateEmitEventType(t *testing.T) {
	m := &plugins.Manifest{
		Name: "x",
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "foo", Sensitivity: plugins.SensitivityMay},
				{EventType: "foo", Sensitivity: plugins.SensitivityAlways},
			},
		},
	}
	err := plugins.ValidateCrypto(m)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_DUPLICATE_EMIT")
}

func TestValidateCryptoRejectsWildcardInRequestsDecryption(t *testing.T) {
	m := &plugins.Manifest{
		Name: "x",
		Crypto: &plugins.CryptoSection{
			Consumes: []plugins.CryptoConsume{{
				Subjects:           []string{"events.>"},
				RequestsDecryption: []string{"*"},
			}},
		},
	}
	err := plugins.ValidateCrypto(m)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_WILDCARD_DECRYPT")
}

func TestValidateCryptoRejectsUnqualifiedRequestsDecryption(t *testing.T) {
	m := &plugins.Manifest{
		Name: "x",
		Crypto: &plugins.CryptoSection{
			Consumes: []plugins.CryptoConsume{{
				Subjects:           []string{"events.>"},
				RequestsDecryption: []string{"whisper"}, // missing plugin: prefix
			}},
		},
	}
	err := plugins.ValidateCrypto(m)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_UNQUALIFIED_REF")
}

func TestValidateCryptoRejectsRefToNonDependencyPlugin(t *testing.T) {
	m := &plugins.Manifest{
		Name:         "consumer",
		Dependencies: map[string]string{}, // declares no deps
		Crypto: &plugins.CryptoSection{
			Consumes: []plugins.CryptoConsume{{
				Subjects:           []string{"events.>"},
				RequestsDecryption: []string{"core-communication:whisper"}, // not in deps
			}},
		},
	}
	err := plugins.ValidateCrypto(m)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_REF_NOT_REQUIRED")
}

func TestValidateCryptoAcceptsSelfReference(t *testing.T) {
	// A plugin's consumes block MAY request decryption for its OWN emitted
	// event types without listing itself in dependencies (self-reference).
	m := &plugins.Manifest{
		Name: "core-communication",
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "whisper", Sensitivity: plugins.SensitivityAlways},
			},
			Consumes: []plugins.CryptoConsume{{
				Subjects:           []string{"events.*.character.*.whisper"},
				RequestsDecryption: []string{"core-communication:whisper"},
			}},
		},
	}
	require.NoError(t, plugins.ValidateCrypto(m))
}

func TestValidateCryptoNilSectionIsAccepted(t *testing.T) {
	m := &plugins.Manifest{Name: "x"}
	assert.NoError(t, plugins.ValidateCrypto(m))
}
```

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run "TestValidateCrypto" ./internal/plugin/`

Expected: FAIL — `plugins.ValidateCrypto` not defined.

- [ ] **Step 3: Implement `ValidateCrypto`**

```go
// internal/plugin/crypto_validator.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"strings"

	"github.com/samber/oops"
)

// ValidateCrypto enforces the manifest crypto.emits / crypto.consumes
// rules from spec §7.1. Caller MUST invoke after parsing the manifest
// and before adding it to the loader registry.
//
// Returns nil for manifests without a crypto section.
func ValidateCrypto(m *Manifest) error {
	if m.Crypto == nil {
		return nil
	}

	// Rule 1: every Sensitivity value is one of the closed enum.
	seenEmit := make(map[string]bool, len(m.Crypto.Emits))
	for i, e := range m.Crypto.Emits {
		if !validSensitivity(e.Sensitivity) {
			return oops.Code("PLUGIN_CRYPTO_INVALID_SENSITIVITY").
				With("plugin", m.Name).
				With("event_type", e.EventType).
				With("sensitivity", string(e.Sensitivity)).
				With("emits_index", i).
				Errorf("invalid sensitivity value (must be always|may|never)")
		}
		// Rule 2: emit event_type is non-empty and unique within this manifest.
		if e.EventType == "" {
			return oops.Code("PLUGIN_CRYPTO_EMPTY_EVENT_TYPE").
				With("plugin", m.Name).
				With("emits_index", i).
				Errorf("crypto.emits entry has empty event_type")
		}
		if seenEmit[e.EventType] {
			return oops.Code("PLUGIN_CRYPTO_DUPLICATE_EMIT").
				With("plugin", m.Name).
				With("event_type", e.EventType).
				Errorf("crypto.emits has duplicate event_type")
		}
		seenEmit[e.EventType] = true
	}

	// Rule 3: requests_decryption is well-formed.
	for ci, c := range m.Crypto.Consumes {
		for ri, ref := range c.RequestsDecryption {
			// Rule 3a: no wildcards.
			if ref == "*" || strings.ContainsAny(ref, ">") {
				return oops.Code("PLUGIN_CRYPTO_WILDCARD_DECRYPT").
					With("plugin", m.Name).
					With("consumes_index", ci).
					With("decryption_index", ri).
					With("ref", ref).
					Errorf("requests_decryption MUST NOT contain wildcards; enumerate event types")
			}
			// Rule 3b: must be qualified <plugin>:<event_type>.
			pluginName, eventType, ok := splitQualifiedRef(ref)
			if !ok {
				return oops.Code("PLUGIN_CRYPTO_UNQUALIFIED_REF").
					With("plugin", m.Name).
					With("ref", ref).
					Errorf("requests_decryption ref MUST be <plugin>:<event_type>")
			}
			// Rule 3c: pluginName must be either this plugin (self) or in dependencies.
			if pluginName != m.Name {
				if _, dep := m.Dependencies[pluginName]; !dep {
					return oops.Code("PLUGIN_CRYPTO_REF_NOT_REQUIRED").
						With("plugin", m.Name).
						With("ref_plugin", pluginName).
						With("ref", ref).
						Errorf("requests_decryption references plugin %q not listed in dependencies", pluginName)
				}
			}
			_ = eventType // resolution against the referenced plugin's emits happens at loader-level, see Task 4
		}
	}

	return nil
}

func validSensitivity(s Sensitivity) bool {
	switch s {
	case SensitivityAlways, SensitivityMay, SensitivityNever:
		return true
	}
	return false
}

// splitQualifiedRef parses "<plugin>:<event_type>" into its components.
// Returns ok=false if the form does not match.
func splitQualifiedRef(ref string) (pluginName, eventType string, ok bool) {
	colon := strings.IndexByte(ref, ':')
	if colon <= 0 || colon == len(ref)-1 {
		return "", "", false
	}
	pluginName = ref[:colon]
	eventType = ref[colon+1:]
	if pluginName == "" || eventType == "" {
		return "", "", false
	}
	return pluginName, eventType, true
}
```

- [ ] **Step 4: Run to verify pass**

Run: `task test -- -run "TestValidateCrypto" ./internal/plugin/`

Expected: PASS for all 8 sub-tests.

- [ ] **Step 5: Commit**

Suggested message:

```text
feat(plugin): ValidateCrypto enforces manifest crypto.emits/consumes rules

Implements spec §7.1 loader-validation rules:
- closed enum for sensitivity (always|may|never)
- non-empty, unique event_type per emit
- no wildcards in requests_decryption
- requests_decryption refs use qualified <plugin>:<event_type>
- referenced plugin must be self or in dependencies

Cross-plugin event-type resolution (does the referenced plugin actually
declare this event type?) lands in Task 4 alongside the loader integration.

Refs: docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md
```

---

## Task 4: Loader integration + cross-plugin event-type resolution

**Files:**

- Modify: `internal/plugin/manager.go:325-364` (the existing `Discover` function calls `ParseManifest` at line 349; integration goes there)
- Create: `internal/plugin/manager_crypto_test.go` (new test file — keeps crypto-validation tests separate from existing manager tests)

The actual entry point is `Manager.Discover` which walks `m.pluginsDir`, calls `ParseManifest` on each `plugin.yaml`, and appends to `[]*DiscoveredPlugin`. There is no `loadManifest` helper today. We integrate `ValidateCrypto` and `ResolveCryptoRefs` into `Discover` and expose a small test-friendly entry point that lets unit tests exercise the path with synthetic manifests.

- [ ] **Step 1: Confirm the integration site by reading `Discover`**

Run: `rg -n "func .*Discover" internal/plugin/manager.go`

Expected: matches the function around `manager.go:325`. Read lines 325-364 to confirm the structure described above (directory walk → `ParseManifest` → append to `plugins` slice).

- [ ] **Step 2: Write failing tests for cross-plugin reference resolution**

The tests work directly against `ResolveCryptoRefs` — a pure function that takes a `*Manifest` and a registry map. No `Manager` instance needed; this keeps the test fast and avoids needing a fake plugins directory.

```go
// internal/plugin/manager_crypto_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
)

func parseManifest(t *testing.T, src string) *plugins.Manifest {
	t.Helper()
	var m plugins.Manifest
	require.NoError(t, yaml.Unmarshal([]byte(src), &m))
	require.NoError(t, plugins.ValidateCrypto(&m), "manifest fixture must pass static validation")
	return &m
}

func TestResolveCryptoRefsRejectsUnknownEventTypeInOtherPlugin(t *testing.T) {
	consumer := parseManifest(t, `
name: plugin-b
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
dependencies:
  plugin-a: ">= 1.0.0"
crypto:
  consumes:
    - subjects: ["events.>"]
      requests_decryption: ["plugin-a:whisper"]
`)
	registry := map[string][]plugins.CryptoEmit{
		"plugin-a": {{EventType: "actually_known", Sensitivity: plugins.SensitivityAlways}},
	}
	err := plugins.ResolveCryptoRefs(consumer, registry)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_UNKNOWN_EVENT_REF")
}

func TestResolveCryptoRefsAcceptsKnownEventTypeInOtherPlugin(t *testing.T) {
	consumer := parseManifest(t, `
name: plugin-b
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
dependencies:
  plugin-a: ">= 1.0.0"
crypto:
  consumes:
    - subjects: ["events.>"]
      requests_decryption: ["plugin-a:whisper"]
`)
	registry := map[string][]plugins.CryptoEmit{
		"plugin-a": {{EventType: "whisper", Sensitivity: plugins.SensitivityAlways}},
	}
	require.NoError(t, plugins.ResolveCryptoRefs(consumer, registry))
}

func TestResolveCryptoRefsRejectsRefToPluginNotInRegistry(t *testing.T) {
	consumer := parseManifest(t, `
name: plugin-b
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
dependencies:
  plugin-a: ">= 1.0.0"
crypto:
  consumes:
    - subjects: ["events.>"]
      requests_decryption: ["plugin-a:whisper"]
`)
	err := plugins.ResolveCryptoRefs(consumer, map[string][]plugins.CryptoEmit{})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_REF_PLUGIN_NOT_LOADED")
}

func TestResolveCryptoRefsRejectsRefToNeverSensitiveEventType(t *testing.T) {
	consumer := parseManifest(t, `
name: plugin-b
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
dependencies:
  plugin-a: ">= 1.0.0"
crypto:
  consumes:
    - subjects: ["events.>"]
      requests_decryption: ["plugin-a:heartbeat"]
`)
	registry := map[string][]plugins.CryptoEmit{
		"plugin-a": {{EventType: "heartbeat", Sensitivity: plugins.SensitivityNever}},
	}
	err := plugins.ResolveCryptoRefs(consumer, registry)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_REF_NEVER_SENSITIVE")
}
```

- [ ] **Step 3: Run to verify failure**

Run: `task test -- -run "TestResolveCryptoRefs" ./internal/plugin/`

Expected: FAIL — `ResolveCryptoRefs` does not exist yet.

- [ ] **Step 4: Add `ResolveCryptoRefs` to `crypto_validator.go`**

Append to `internal/plugin/crypto_validator.go`:

```go
// ResolveCryptoRefs verifies every requests_decryption reference points
// at a plugin in the registry whose manifest declares the named event
// type with sensitivity in {always, may}. Caller is the loader, after
// all manifests in this load batch have been parsed.
//
// registry maps plugin name → that plugin's emit declarations. The
// loader populates this from the manifests it has already accepted.
// Self-references (m.Name → its own emits) are resolved against m
// directly, so a plugin's manifest can request decryption for its
// own emitted event types without listing itself in the registry.
func ResolveCryptoRefs(m *Manifest, registry map[string][]CryptoEmit) error {
	if m.Crypto == nil {
		return nil
	}
	for ci, c := range m.Crypto.Consumes {
		for ri, ref := range c.RequestsDecryption {
			pluginName, eventType, _ := splitQualifiedRef(ref)
			emits := m.Crypto.Emits
			if pluginName != m.Name {
				e, ok := registry[pluginName]
				if !ok {
					return oops.Code("PLUGIN_CRYPTO_REF_PLUGIN_NOT_LOADED").
						With("plugin", m.Name).
						With("ref_plugin", pluginName).
						With("ref", ref).
						With("consumes_index", ci).
						With("decryption_index", ri).
						Errorf("requests_decryption references plugin not yet loaded")
				}
				emits = e
			}
			var found *CryptoEmit
			for i := range emits {
				if emits[i].EventType == eventType {
					found = &emits[i]
					break
				}
			}
			if found == nil {
				return oops.Code("PLUGIN_CRYPTO_UNKNOWN_EVENT_REF").
					With("plugin", m.Name).
					With("ref", ref).
					Errorf("requests_decryption references event type not declared by referenced plugin")
			}
			if found.Sensitivity == SensitivityNever {
				return oops.Code("PLUGIN_CRYPTO_REF_NEVER_SENSITIVE").
					With("plugin", m.Name).
					With("ref", ref).
					Errorf("requests_decryption MUST NOT reference SensitivityNever event types")
			}
		}
	}
	return nil
}
```

- [ ] **Step 5: Run unit tests to verify pass**

Run: `task test -- -run "TestResolveCryptoRefs" ./internal/plugin/`

Expected: PASS for all four sub-tests.

- [ ] **Step 6: Wire `ValidateCrypto` and `ResolveCryptoRefs` into `Manager.Discover`**

The plan-reviewer-confirmed integration site is `internal/plugin/manager.go:325-364` (the `Discover` function). The current loop calls `ParseManifest` at line 349 and appends to `plugins`. We extend the loop to skip plugins whose crypto block is malformed (logged like the existing manifest-parse skip path), and add a second pass that resolves cross-plugin refs after all manifests are gathered.

Edit `internal/plugin/manager.go` around lines 349-360 (after `ParseManifest` succeeds, before the append):

```go
		manifest, err := ParseManifest(data)
		if err != nil {
			slog.Warn("skipping plugin with invalid manifest",
				"dir", entry.Name(),
				"error", err)
			continue
		}

		// NEW: static crypto-section validation. Failure means the manifest
		// is internally malformed (unknown sensitivity, duplicate emit, etc.)
		// and we skip the plugin entirely with a logged warning, mirroring
		// the existing ParseManifest skip path.
		if err := ValidateCrypto(manifest); err != nil {
			slog.Warn("skipping plugin with invalid crypto section",
				"dir", entry.Name(),
				"plugin", manifest.Name,
				"error", err)
			continue
		}

		plugins = append(plugins, &DiscoveredPlugin{
			Manifest: manifest,
			Dir:      pluginDir,
		})
```

Then immediately before `return plugins, nil` at line 363, add a cross-plugin resolution pass:

```go
	// Build the per-plugin emit registry from successfully-discovered manifests.
	emitRegistry := make(map[string][]CryptoEmit, len(plugins))
	for _, dp := range plugins {
		if dp.Manifest.Crypto != nil {
			emitRegistry[dp.Manifest.Name] = dp.Manifest.Crypto.Emits
		}
	}

	// Filter out plugins whose cross-plugin refs don't resolve.
	resolved := plugins[:0]
	for _, dp := range plugins {
		if err := ResolveCryptoRefs(dp.Manifest, emitRegistry); err != nil {
			slog.Warn("skipping plugin with unresolvable crypto refs",
				"plugin", dp.Manifest.Name,
				"error", err)
			continue
		}
		resolved = append(resolved, dp)
	}
	return resolved, nil
```

- [ ] **Step 7: Write integration test exercising the Discover path**

```go
// Append to internal/plugin/manager_crypto_test.go

func TestDiscoverSkipsPluginWithInvalidCryptoSection(t *testing.T) {
	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, "bad-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(pluginDir, "plugin.yaml"),
		[]byte(`
name: bad-plugin
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
crypto:
  emits:
    - event_type: foo
      sensitivity: kinda
`),
		0o644,
	))
	mgr := plugins.NewManager(plugins.ManagerConfig{PluginsDir: tmp})
	discovered, err := mgr.Discover()
	require.NoError(t, err)
	assert.Empty(t, discovered, "plugin with invalid crypto section MUST be filtered out")
}

func TestDiscoverSkipsPluginWithUnresolvableCryptoRefs(t *testing.T) {
	tmp := t.TempDir()
	// Plugin B references plugin-a:whisper but no plugin-a is present.
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "plugin-b"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(tmp, "plugin-b", "plugin.yaml"),
		[]byte(`
name: plugin-b
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
dependencies:
  plugin-a: ">= 1.0.0"
crypto:
  consumes:
    - subjects: ["events.>"]
      requests_decryption: ["plugin-a:whisper"]
`),
		0o644,
	))
	mgr := plugins.NewManager(plugins.ManagerConfig{PluginsDir: tmp})
	discovered, err := mgr.Discover()
	require.NoError(t, err)
	assert.Empty(t, discovered)
}

func TestDiscoverAcceptsValidCryptoSection(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "plugin-a"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(tmp, "plugin-a", "plugin.yaml"),
		[]byte(`
name: plugin-a
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
crypto:
  emits:
    - event_type: whisper
      sensitivity: always
`),
		0o644,
	))
	mgr := plugins.NewManager(plugins.ManagerConfig{PluginsDir: tmp})
	discovered, err := mgr.Discover()
	require.NoError(t, err)
	require.Len(t, discovered, 1)
	assert.Equal(t, "plugin-a", discovered[0].Manifest.Name)
}
```

Verify the actual `NewManager` / `ManagerConfig` shape with: `rg -n "func NewManager|type ManagerConfig" internal/plugin/manager.go` and adapt the test setup to the real constructor signature.

- [ ] **Step 8: Run integration tests**

Run: `task test -- -run "TestDiscover" ./internal/plugin/`

Expected: PASS.

- [ ] **Step 9: Run the full plugin-package test suite to confirm no regressions**

Run: `task test -- ./internal/plugin/...`

Expected: PASS.

- [ ] **Step 10: Lint**

Run: `task lint`

Expected: PASS.

- [ ] **Step 11: Commit**

Suggested message:

```text
feat(plugin): loader resolves and validates crypto.consumes refs

Adds ResolveCryptoRefs which runs after a manifest parses and validates,
walking every requests_decryption ref against the already-loaded plugin
registry. Refs to unknown plugins, unknown event types, or
SensitivityNever event types are rejected with typed errors.

Refs: docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md
```

---

## Task 5: Migrate `core-communication` event-type constants to plugin package

This is the **template task** — Task 6 repeats this procedure for the remaining plugins using a parameter table.

**Files:**

- Create: `plugins/core-communication/events.go`
- Create: `plugins/core-communication/events_test.go`
- Modify: `internal/core/event.go` (remove migrated constants)
- Modify: `plugins/core-communication/main.lua` (qualify event-type strings)
- Modify: call sites — `internal/world/*.go`, `internal/grpc/*.go`, `internal/telnet/*.go`, `internal/store/*.go`

- [ ] **Step 1: Write failing test for the new plugin-side constants**

```go
// plugins/core-communication/events_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corecomm_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	corecomm "github.com/holomush/holomush/plugins/core-communication"
)

func TestEventTypesAreQualifiedWithPluginName(t *testing.T) {
	for _, et := range []corecomm.EventType{
		corecomm.EventTypeSay,
		corecomm.EventTypePose,
		corecomm.EventTypePage,
		corecomm.EventTypeWhisper,
		corecomm.EventTypePemit,
		corecomm.EventTypeOOC,
		corecomm.EventTypeWhisperNotice,
	} {
		assert.True(t,
			strings.HasPrefix(string(et), "core-communication:"),
			"event type %q must be qualified with plugin prefix", et,
		)
	}
}

func TestEventTypeAttributionIsExact(t *testing.T) {
	assert.Equal(t, corecomm.EventType("core-communication:say"), corecomm.EventTypeSay)
	assert.Equal(t, corecomm.EventType("core-communication:whisper"), corecomm.EventTypeWhisper)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run "TestEventType" ./plugins/core-communication/`

Expected: FAIL — package does not exist as Go (it's currently a Lua-only plugin directory).

- [ ] **Step 3: Add a minimal Go package alongside the Lua plugin**

Lua plugins in this repo are co-located with `main.lua`; a Go events package can sit beside it without making the plugin a binary plugin. core-communication and core-objects are both Lua-only today; this plan introduces a sibling Go events package alongside the existing `main.lua` for each. The Go package is consumed by host-side call sites that emit events on behalf of the plugin (the Lua side continues to use string literals for event types in its return-table emits).

```go
// plugins/core-communication/events.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package corecomm holds plugin-owned types and constants for the
// core-communication plugin. The plugin runtime is Lua (main.lua); this
// Go package provides typed event-type constants for host-side call
// sites that emit communication events on behalf of the plugin.
//
// Per spec §7.1, event-type identifiers are qualified <plugin>:<type>
// when crossing plugin boundaries.
package corecomm

// EventType is a string identifier for events emitted by the
// core-communication plugin.
type EventType string

// Event-type constants. All are qualified with the plugin name.
const (
	EventTypeSay           EventType = "core-communication:say"
	EventTypePose          EventType = "core-communication:pose"
	EventTypePage          EventType = "core-communication:page"
	EventTypeWhisper       EventType = "core-communication:whisper"
	EventTypePemit         EventType = "core-communication:pemit"
	EventTypeOOC           EventType = "core-communication:ooc"
	EventTypeWhisperNotice EventType = "core-communication:whisper_notice"
)
```

- [ ] **Step 4: Run to verify pass**

Run: `task test -- -run "TestEventType" ./plugins/core-communication/`

Expected: PASS.

- [ ] **Step 5: Commit the new constants (before any call-site migration)**

Suggested message:

```text
feat(core-communication): add plugin-owned EventType constants

Introduces qualified event-type constants (core-communication:say etc.)
in the plugin package. Call-site migration to these new constants follows
in this same task series; legacy core.EventType* constants stay until
all call sites are switched.

Refs: docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md
```

- [ ] **Step 6: Find every call site using the legacy `core.EventType*` for migrated types**

Run: `rg -n "core\.EventType(Say|Pose|Page|Whisper|Pemit|OOC|WhisperNotice)" --type go | wc -l`

Expected: approximately 48 matches across files in `internal/world/`, `internal/grpc/`, `internal/telnet/`, `internal/store/`, and tests.

NOTE: `Arrive`, `Leave`, `Move` stay in `internal/core/event.go` for this task — they're host-owned movement events. See the earlier "Out of scope for this phase" note about preserving host event-type string values.

- [ ] **Step 7: Update call sites to use `corecomm.EventTypeXxx` and qualified strings**

For each call site, replace:

| Legacy | Replacement |
| --- | --- |
| `core.EventTypeSay` | `corecomm.EventTypeSay` |
| `core.EventTypePose` | `corecomm.EventTypePose` |
| `core.EventTypePage` | `corecomm.EventTypePage` |
| `core.EventTypeWhisper` | `corecomm.EventTypeWhisper` |
| `core.EventTypePemit` | `corecomm.EventTypePemit` |
| `core.EventTypeOOC` | `corecomm.EventTypeOOC` |
| `core.EventTypeWhisperNotice` | `corecomm.EventTypeWhisperNotice` |

Where the call site is using a string literal like `"say"`, replace with `string(corecomm.EventTypeSay)`. Add the import:

```go
import corecomm "github.com/holomush/holomush/plugins/core-communication"
```

Group call-site changes by file, not by constant. Easier to review.

NOTE: `EventTypeArrive`, `EventTypeLeave`, `EventTypeMove` are world-events, not communication events. They migrate in Task 6 (core-world) — leave them in `internal/core/event.go` for this task.

- [ ] **Step 8: Update `plugins/core-communication/main.lua` event-emit table literals**

Lua plugins use the `ok_events({{stream=..., type=..., payload=...}})` return-table pattern (NOT `emit_event(...)` calls — that pattern doesn't exist in this codebase). Find every such literal:

```bash
rg -n 'type *= *"(say|pose|page|whisper|pemit|ooc|whisper_notice)"' plugins/core-communication/main.lua
```

Expected matches at approximately lines 65, 110, 145, 265, 386–387, 432 (verify by running the rg).

For each match, change the bare event-type string to the qualified form. Example:

```lua
-- before (around line 65 in handle_say)
return ok_events({
    {stream = "location:" .. ctx.location_id, type = "say", payload = payload}
})

-- after
return ok_events({
    {stream = "location:" .. ctx.location_id, type = "core-communication:say", payload = payload}
})
```

**Do NOT change**:

- Command-name strings (`if ctx.invoked_as == ":"`) — those identify the *command*, not the event.
- `subscribe_to` / event-filter strings in OTHER plugins (handled in Step 8b below) — they need their own coordinated update.
- Any string that's an alias name (e.g., `","`) — those are user-facing command shorthand.

The Lua hostfunc dispatch in `internal/plugin/lua/host.go` reads `event.type` as a verbatim string; updating the emit-side string changes the wire value.

- [ ] **Step 8b: Audit cross-plugin Lua subscribers for legacy bare event-type strings**

Search every other plugin's Lua source for filters keyed on the legacy bare strings of comm events:

```bash
rg -n 'event\.type *(==|~=) *"(say|pose|page|whisper|pemit|ooc|whisper_notice)"' plugins/
```

Expected critical match: `plugins/echo-bot/main.lua` filters on `event.type == "say"` (echo-bot listens for `say` events to echo them back). Update the comparison to the qualified form:

```lua
-- before
if event.type == "say" then ... end

-- after
if event.type == "core-communication:say" then ... end
```

Without this audit step, echo-bot's `say` filter becomes always-false after Step 8 and the bot stops echoing — a regression that the unit tests for core-communication would not catch.

- [ ] **Step 9: Run integration tests for the communication plugin**

Run: `task test:int -- -tags=integration -run "Communication" ./test/integration/...`

Expected: PASS. Communication events flow end-to-end with the qualified type names.

If failures occur, the most likely cause is a missed call site — re-run the rg from Step 6 to find lingering `core.EventType(Say|Pose|...)` references.

- [ ] **Step 10: Remove the migrated constants from `internal/core/event.go`**

Edit `internal/core/event.go`. Remove these lines (note the existing TODO at line 45 that flagged this migration):

```go
EventTypeSay           EventType = "say"
EventTypePose          EventType = "pose"
EventTypePage          EventType = "page"
EventTypeWhisper       EventType = "whisper"
EventTypePemit         EventType = "pemit"
EventTypeOOC           EventType = "ooc"
EventTypeWhisperNotice EventType = "whisper_notice"
```

Update the TODO comment at line 45 to reflect that the comm-plugin migration is complete and only the remaining types await migration (object types in Task 6, world types if applicable).

- [ ] **Step 11: Build and test the whole repo**

Run: `task build && task test`

Expected: PASS. Any remaining call site referencing the deleted constants is a hard compile error — fix by adding the corecomm import and the new constant.

- [ ] **Step 12: Commit the migration**

Suggested message:

```text
refactor: migrate core-communication EventType constants out of internal/core

Moves Say, Pose, Page, Whisper, Pemit, OOC, WhisperNotice from
internal/core/event.go into plugins/core-communication/events.go.
Updates all call sites and the Lua hostfunc emit-strings to use the
qualified <plugin>:<event_type> form.

Refs: holomush-k18g (re-scoped phase 1 of event-payload-crypto)
Refs: docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md
```

---

## Task 5b: Migrate `pkg/plugin/event.go` SDK convenience constants

The binary-plugin SDK package `pluginsdk` (`pkg/plugin/event.go`) ships a parallel set of `EventType` convenience constants (`EventTypeSay`, `EventTypePose`, `EventTypeEmit`, `EventTypeArrive`, `EventTypeLeave`, `EventTypeSystem`). These are used by 100+ call sites in tests and in `pkg/holo/emit.go`. Removing the bare constants without replacement breaks compilation across `pkg/holo/`, `internal/plugin/integration_test.go`, `internal/plugin/subscriber_test.go`, `plugins/core-scenes/service.go`, and others.

The clean answer per spec §7.1: **plugin-owned event-type constants live in the owning plugin's package, not in the SDK.** The SDK keeps the `EventType` type and the `Event`/`EmitEvent`/`EmitIntent` shapes, but drops the convenience constants. Call sites import the constants from the appropriate plugin package (or use string literals where a test fixture is constructing an event for a non-existent plugin scenario).

**Files:**

- Modify: `pkg/plugin/event.go` (remove convenience constants; keep types)
- Modify: `pkg/holo/emit.go` and `pkg/holo/emit_test.go`
- Modify: `internal/plugin/integration_test.go`, `internal/plugin/subscriber_test.go`, `internal/plugin/event_emitter_test.go`, `internal/plugin/event_emitter_round3_test.go`, `internal/plugin/communication_integration_test.go`, `internal/plugin/help_integration_test.go`, `internal/plugin/manager_routing_test.go`
- Modify: `plugins/core-scenes/service.go` (line 206 — `pluginsdk.EventTypeSystem`; the `system` type is host-owned and stays a bare string for now per the "out of scope" note above)

- [ ] **Step 1: Survey the SDK usage**

Run: `rg -n "pluginsdk\.EventType" --type go | wc -l`

Expected: 100+ matches.

Run: `rg -n "pluginsdk\.EventType" --type go | rg -o "EventType[A-Z][a-z]+" | sort -u`

Expected output identifies which constants are actually in use (likely `EventTypeSay`, `EventTypeSystem`, `EventTypeArrive`, `EventTypeLeave`, with `EventTypePose`/`Emit` possibly unused).

- [ ] **Step 2: Write failing test that the convenience constants are gone**

```go
// pkg/plugin/event_constants_removed_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk_test

// This test file's purpose is to fail to compile if the convenience
// constants are reintroduced. If you find yourself updating this file,
// reconsider — the migration is intentional.

import (
	"testing"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func TestEventTypeIsAStringType(t *testing.T) {
	// EventType still exists.
	var et pluginsdk.EventType = "core-communication:say"
	_ = et
}

// NO references to pluginsdk.EventTypeSay etc. — those are gone.
```

- [ ] **Step 3: Run to verify the test compiles BEFORE removal (sanity)**

Run: `task test -- -run "TestEventTypeIsAStringType" ./pkg/plugin/`

Expected: PASS. The test should pass against the current code where the convenience constants exist (because the test only uses the type, not the constants).

- [ ] **Step 4: Remove the convenience constants from `pkg/plugin/event.go`**

Edit `pkg/plugin/event.go` lines 14-23. Remove:

```go
// These constants are provided for convenience. Plugins may emit any event
// type registered in the VerbRegistry.
const (
	EventTypeSay    EventType = "say"
	EventTypePose   EventType = "pose"
	EventTypeEmit   EventType = "emit"
	EventTypeArrive EventType = "arrive"
	EventTypeLeave  EventType = "leave"
	EventTypeSystem EventType = "system"
)
```

Replace with:

```go
// Plugin-owned event-type constants live in the owning plugin's Go package
// (e.g., plugins/core-communication/events.go). The SDK exposes the
// EventType string type only. Cross-package event-type references use the
// qualified form "<plugin>:<event_type>" per spec §7.1.
//
// Host-owned event-type strings (e.g., "system" for system events) are
// constants in internal/core/event.go and may be referenced via that
// package or as bare string literals in test fixtures.
```

- [ ] **Step 5: Run the build to discover every call site**

Run: `task build`

Expected: FAIL with a list of "undefined: pluginsdk.EventTypeSay" and similar errors. The compiler is now your migration tool.

- [ ] **Step 6: Migrate each call site**

For each compile error, use this lookup table:

| Legacy reference | Replacement | Rationale |
| --- | --- | --- |
| `pluginsdk.EventTypeSay` | `corecomm.EventTypeSay` (after `import corecomm "github.com/holomush/holomush/plugins/core-communication"`) | core-communication owns `say` |
| `pluginsdk.EventTypePose` | `corecomm.EventTypePose` | same |
| `pluginsdk.EventTypeEmit` | `corecomm.EventType("core-communication:emit")` if no constant exists; or define `EventTypeEmit` in `plugins/core-communication/events.go` to match | core-communication owns `emit` |
| `pluginsdk.EventTypeSystem` | `core.EventTypeSystem` (host-owned, unchanged value) | host-owned, stays in `internal/core/event.go` |
| `pluginsdk.EventTypeArrive` | `core.EventTypeArrive` (host-owned, unchanged value) | per "out of scope" — host events stay host-owned for Phase 1 |
| `pluginsdk.EventTypeLeave` | `core.EventTypeLeave` (host-owned, unchanged value) | same |

For test fixtures that construct synthetic events with arbitrary types (e.g., `Type: pluginsdk.EventTypeSay` in a unit test that doesn't care about real plugins), replacing with `Type: pluginsdk.EventType("test-fixture:say")` is acceptable and clearer than coupling the test to a real plugin's namespace.

If `EventTypeEmit` (the `core-communication:emit` event) doesn't appear in `plugins/core-communication/events.go` from Task 5, add it now:

```go
// In plugins/core-communication/events.go — append:
EventTypeEmit EventType = "core-communication:emit"
```

- [ ] **Step 7: Run the build to verify clean**

Run: `task build`

Expected: PASS.

- [ ] **Step 8: Run the full test suite**

Run: `task test`

Expected: PASS. Any test that constructs a `pluginsdk.Event` with one of the migrated constants now uses the new name.

- [ ] **Step 9: Run integration tests**

Run: `task test:int`

Expected: PASS.

- [ ] **Step 10: Lint**

Run: `task lint`

Expected: PASS.

- [ ] **Step 11: Commit**

Suggested message:

```text
refactor(pluginsdk): remove convenience EventType constants

The SDK's EventTypeSay/Pose/Emit/Arrive/Leave/System constants
duplicated event-type strings that should live in the owning plugin's
package per the plugin boundary discipline (spec §7.1). Migrates 100+
call sites:
- core-communication owners (Say, Pose, Emit) → corecomm.EventTypeXxx
- host-owned (System, Arrive, Leave) → core.EventTypeXxx (unchanged values)
- test fixtures with synthetic events → string literals or test-namespace types

The EventType type itself stays in pluginsdk; only the convenience constants
are removed.

Refs: holomush-k18g (re-scoped phase 1 of event-payload-crypto)
Refs: docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md
```

---

## Task 6: Migrate remaining plugin-owned EventType constants

Repeat the Task 5 procedure for each remaining plugin-owned constant group. Use the same step pattern (add new constants → migrate call sites → remove old constants from `internal/core/event.go` → commit).

| Plugin | Target package path | Constants to migrate from `internal/core/event.go` |
| --- | --- | --- |
| `core-objects` | `plugins/core-objects/events.go` (NEW Go package — core-objects is Lua-only today, like core-communication; the Go package sits alongside `main.lua`) | `EventTypeObjectCreate`, `EventTypeObjectDestroy`, `EventTypeObjectUse`, `EventTypeObjectExamine`, `EventTypeObjectGive` |

**Host-owned event types stay in `internal/core/event.go` with unchanged string values.** These are emitted by the host itself (not by any plugin) and per spec §11.1's "no data migration" rule, their wire string values do not change in Phase 1: `EventTypeSystem` = `"system"`, `EventTypeArrive` = `"arrive"`, `EventTypeLeave` = `"leave"`, `EventTypeMove` = `"move"`, `EventTypeCommandResponse` = `"command_response"`, `EventTypeCommandError` = `"command_error"`, `EventTypeLocationState` = `"location_state"`, `EventTypeExitUpdate` = `"exit_update"`, `EventTypeSessionEnded` = `"session_ended"`. The qualified-form transition for host events is deferred to Phase 3 alongside the runtime gates that need to reason about it.

**core-scenes is NOT in the migration table.** Inspection of `plugins/core-scenes/service.go:206` shows the scenes service emits only `pluginsdk.EventTypeSystem` (host-owned). The `OpsEventKind` constants in `plugins/core-scenes/ops_events.go` are scene-audit-row kinds (membership.invite, lifecycle.created, …), NOT JetStream event types — they go to the plugin's own `scene_log` audit table per the F5 cutover, not to the bus. Phase 1 declares these accurately in core-scenes' manifest (Task 7), but does not invent new EventType constants for events that the plugin doesn't actually emit on the bus today. Adding plugin-owned scene event-types (e.g. `core-scenes:scene_ic`) is a Phase 2+ change paired with the runtime work to actually emit them.

- [ ] **Step 1: Perform Task 5 steps 1–12 for `core-objects`**

Substitute the plugin name and constants. The pattern is identical to Task 5; only the values change. Plan ~30-45 minutes including call-site updates.

The new `plugins/core-objects/events.go` skeleton (analogous to Task 5 step 3's `corecomm` package) is:

```go
// plugins/core-objects/events.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package coreobj holds plugin-owned types and constants for the
// core-objects plugin. The plugin runtime is Lua (main.lua); this
// Go package provides typed event-type constants for host-side call
// sites that emit object events on behalf of the plugin.
package coreobj

// EventType is a string identifier for events emitted by the
// core-objects plugin.
type EventType string

// Event-type constants. All are qualified with the plugin name.
const (
	EventTypeObjectCreate  EventType = "core-objects:object_create"
	EventTypeObjectDestroy EventType = "core-objects:object_destroy"
	EventTypeObjectUse     EventType = "core-objects:object_use"
	EventTypeObjectExamine EventType = "core-objects:object_examine"
	EventTypeObjectGive    EventType = "core-objects:object_give"
)
```

Call-site grep:

```bash
rg -n "core\.EventType(ObjectCreate|ObjectDestroy|ObjectUse|ObjectExamine|ObjectGive)" --type go
```

Replace with `coreobj.EventTypeObjectCreate` etc. and add the import at each call site:

```go
import coreobj "github.com/holomush/holomush/plugins/core-objects"
```

Lua-side audit (the same step-8b audit as Task 5):

```bash
rg -n 'type *= *"(object_create|object_destroy|object_use|object_examine|object_give)"' plugins/core-objects/main.lua
rg -n 'event\.type *(==|~=) *"(object_create|object_destroy|object_use|object_examine|object_give)"' plugins/
```

Update both emit-site table literals and any cross-plugin filter strings to the qualified form (`core-objects:object_create` etc.).

- [ ] **Step 2: Add a comment in `internal/core/event.go` documenting the deliberate host-only retention**

```go
// Host-owned event types. These are emitted by the host itself, not by a
// plugin. Their string values stay bare (e.g. "system", "arrive") in
// Phase 1 of event-payload-crypto per spec §11.1's "no data migration"
// rule — re-keying these wire values would break audit-row continuity
// and any deployed cursor/replay logic. Phase 3 revisits whether to add
// a "core:" qualified prefix once runtime gates exist.
const (
	EventTypeSystem          EventType = "system"
	EventTypeSessionEnded    EventType = "session_ended"
	EventTypeCommandResponse EventType = "command_response"
	EventTypeCommandError    EventType = "command_error"
	EventTypeArrive          EventType = "arrive"
	EventTypeLeave           EventType = "leave"
	EventTypeMove            EventType = "move"
	EventTypeLocationState   EventType = "location_state"
	EventTypeExitUpdate      EventType = "exit_update"
)
```

The constant set above is what remains after Tasks 5, 5b, and 6 step 1 have removed plugin-owned constants. Verify by running:

```bash
rg -n "^\s+EventType\w+\s+EventType\s+=" internal/core/event.go
```

Expected: only host-owned types listed above.

- [ ] **Step 3: Run full test suite**

Run: `task test && task test:int`

Expected: PASS.

- [ ] **Step 4: Lint and commit**

Run: `task lint`. Expected: PASS.

Suggested commit message:

```text
refactor(core-objects): migrate object EventType constants to plugin package

Moves ObjectCreate/Destroy/Use/Examine/Give from internal/core/event.go
into plugins/core-objects/events.go with qualified <plugin>:<event>
form. Updates call sites. Host-owned event types (system, arrive, leave,
move, command_response, command_error, location_state, exit_update,
session_ended) stay in internal/core with unchanged string values per
spec §11.1.

Refs: holomush-k18g
```

---

## Task 7: Update each plugin's `plugin.yaml` with `crypto.emits` block

**Files:**

- Modify: `plugins/core-aliases/plugin.yaml`
- Modify: `plugins/core-building/plugin.yaml`
- Modify: `plugins/core-communication/plugin.yaml`
- Modify: `plugins/core-help/plugin.yaml`
- Modify: `plugins/core-objects/plugin.yaml`
- Modify: `plugins/core-scenes/plugin.yaml`
- Modify: `plugins/echo-bot/plugin.yaml`
- Modify: `plugins/setting-crossroads/plugin.yaml`
- Modify: `plugins/setting-skeleton/plugin.yaml`
- Modify: `plugins/test-abac-widget/plugin.yaml`

For each plugin, add a `crypto:` section listing every event type it emits with the appropriate sensitivity classification. The classifications below come from spec §7.1 examples + threat-model judgment.

- [ ] **Step 1: Update `plugins/core-communication/plugin.yaml`**

Append to the existing `plugin.yaml`:

```yaml
crypto:
  emits:
    - event_type: say
      sensitivity: never
      description: "Speech in the current location, visible to everyone present."
    - event_type: pose
      sensitivity: never
      description: "Action description in the current location."
    - event_type: ooc
      sensitivity: never
      description: "Out-of-character speech in the current location."
    - event_type: page
      sensitivity: always
      description: "OOC private message between two characters; participants only."
    - event_type: whisper
      sensitivity: always
      description: "In-character private message between two characters in the same location."
    - event_type: pemit
      sensitivity: always
      description: "Storyteller-issued private narration to a single character."
    - event_type: whisper_notice
      sensitivity: never
      description: "Public notice that a whisper occurred (no content), visible in the location."
```

- [ ] **Step 2: Update `plugins/core-objects/plugin.yaml`**

```yaml
crypto:
  emits:
    - event_type: object_create
      sensitivity: never
      description: "An object came into existence."
    - event_type: object_destroy
      sensitivity: never
      description: "An object was destroyed."
    - event_type: object_use
      sensitivity: never
      description: "An object was used or activated."
    - event_type: object_examine
      sensitivity: never
      description: "An object was examined."
    - event_type: object_give
      sensitivity: never
      description: "An object changed possession."
```

- [ ] **Step 3: Update `plugins/core-scenes/plugin.yaml`**

Inspection of `plugins/core-scenes/service.go:206` shows the scenes service today emits only one bus event type: `pluginsdk.EventTypeSystem` (host-owned). The richer scene events (`scene_ic`, `scene_join`, etc.) live in the plugin's own audit table (`scene_log`) as `OpsEventKind` rows — NOT as JetStream event types.

Phase 1 declares accurately what core-scenes emits today, which is nothing of its own:

```yaml
crypto:
  emits: []   # core-scenes emits only the host-owned "system" event today;
              # plugin-owned scene-event types (scene_ic, scene_join, etc.)
              # land in a future phase coupled with runtime work to actually
              # emit them on the bus.
```

Filing a follow-up bead for this:

```bash
bd create --title="core-scenes: emit plugin-owned scene events on the bus" \
    --type=feature --priority=2 \
    --description="Currently core-scenes emits only pluginsdk.EventTypeSystem on the bus; richer scene-event types live only in scene_log. Promote selected ops_events to bus events with appropriate sensitivity per spec §7.1. Coupled with event-payload-crypto Phase 3 since scene_ic etc. are the canonical 'sensitivity: may' examples."
```

- [ ] **Step 4: Update remaining plugin manifests**

For each remaining plugin, inspect the plugin's actual emit sites (Lua return-table literals using `ok_events({{type="...",...}})` or Go `EventSink.Emit` calls) and add a `crypto:` section. The table below is grounded in the actual repo state at the time of writing — verify against current code with `rg -n 'type *= *"' plugins/<plugin>/main.lua` (or `rg -n 'EventSink\.Emit' plugins/<plugin>/'` for Go plugins).

| Plugin | Inspect | Action |
| --- | --- | --- |
| `core-aliases` | Lua source | Declarative aliases; if no `ok_events` calls, add `crypto: {}` (or omit the section entirely — both are valid) |
| `core-building` | Lua source | If it emits any `room_*` events: declare each with `sensitivity: never` |
| `core-help` | Lua source | Read-only command; omit `crypto:` section |
| `echo-bot` | `plugins/echo-bot/main.lua:13,40` | Echo-bot's `event.type ~= "say"` filter requires the qualified-form update from Task 5 step 8b. Echo-bot itself does NOT own a unique event type (it re-emits `say`-like events that are routed through the host). Omit the `crypto.emits` section — emitting another plugin's event type is not a Phase 1 manifest contract; file a follow-up bead for echo-bot's emit semantics |
| `setting-crossroads` | `plugins/setting-crossroads/` content + landing dirs | Setting plugins have no `main.lua`; check `plugin.yaml` `type:` field — if `setting`, omit `crypto:` (no runtime code emits events) |
| `setting-skeleton` | same as above | Same — omit `crypto:` |
| `test-abac-widget` | `plugins/test-abac-widget/` Go source | If the plugin has a `service.go` calling `EventSink.Emit`, list the types; otherwise omit the section |

Where the table says "omit" or "no runtime emit," the manifest does not need a `crypto:` section. The validator (Task 3) treats absent `crypto:` as "this plugin does not declare event types." Adding `crypto: {}` makes the absence explicit if the plugin author wants to signal intent — both forms pass loader validation.

- [ ] **Step 5: Run plugin loader tests**

Run: `task test -- ./internal/plugin/`

Expected: PASS — every plugin manifest now declares its event types; loader validation accepts them.

- [ ] **Step 6: Run integration tests**

Run: `task test:int`

Expected: PASS.

- [ ] **Step 7: Commit each plugin's manifest update as its own small commit**

Suggested message format:

```text
feat(<plugin-name>): declare crypto.emits sensitivity for emitted events

Adds the manifest's crypto.emits block per phase 1 of
event-payload-crypto. Sensitivity classifications:
- <event_type>: <sensitivity> — <reason>

Refs: docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md
```

---

## Task 7b: Introduce `holomush plugin` parent cobra command and shared test helper

The `holomush plugin` subcommand parent does not exist today (verified by `ls cmd/holomush/` showing only `gateway`, `core`, `migrate`, `status`). Tasks 8 and 9 depend on this parent — they hang their own subcommands off it. We create it here and verify it shows up in `holomush --help`.

**Files:**

- Create: `cmd/holomush/cmd_plugin.go`
- Create: `cmd/holomush/cmd_plugin_test.go`
- Create: `cmd/holomush/test_helper_test.go`
- Modify: `cmd/holomush/root.go:37-40` (register the new parent)

- [ ] **Step 1: Write failing test for the plugin parent registration**

```go
// cmd/holomush/cmd_plugin_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootHasPluginSubcommand(t *testing.T) {
	root := NewRootCmd()
	plugin, _, err := root.Find([]string{"plugin"})
	require.NoError(t, err)
	require.NotNil(t, plugin)
	assert.Equal(t, "plugin", plugin.Name())
}
```

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run "TestRootHasPluginSubcommand" ./cmd/holomush/`

Expected: FAIL — `Find` cannot resolve `plugin`.

- [ ] **Step 3: Create the parent command file**

```go
// cmd/holomush/cmd_plugin.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "github.com/spf13/cobra"

// NewPluginCmd is the `holomush plugin` parent command. Subcommands
// (validate, events) attach via NewPluginValidateCmd / NewPluginEventsCmd
// added in subsequent tasks.
func NewPluginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plugin",
		Short: "Plugin authoring and inspection commands",
		Long:  "Inspect and validate plugin manifests, list declared event types, and run author-time checks.",
	}
}
```

- [ ] **Step 4: Register the parent with the root**

Edit `cmd/holomush/root.go` around line 40. Add `cmd.AddCommand(NewPluginCmd())` after the existing `AddCommand` calls.

- [ ] **Step 5: Add the `runCmd` test helper**

```go
// cmd/holomush/test_helper_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"testing"
)

// runCmd executes the holomush root command with the given args and
// returns captured stdout/stderr and the exit code (0 on success,
// nonzero if the command returned an error). Used by CLI subcommand
// tests in this package.
func runCmd(t *testing.T, args []string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	root := NewRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		return out.String(), 1
	}
	return out.String(), 0
}
```

- [ ] **Step 6: Run tests to verify pass**

Run: `task test -- -run "TestRootHasPluginSubcommand" ./cmd/holomush/`

Expected: PASS.

- [ ] **Step 7: Lint and commit**

Run: `task lint`. Expected: PASS.

Suggested commit message:

```text
feat(cli): introduce holomush plugin subcommand parent

Adds NewPluginCmd as a parent for plugin authoring/inspection
subcommands (validate, events) that follow in subsequent tasks. Adds a
shared runCmd test helper for cobra-driven CLI tests in cmd/holomush.

Refs: docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md
```

---

## Task 8: Build-time manifest validator (`task plugin:validate`)

**Files:**

- Create: `scripts/validate-plugin.sh`
- Create: `cmd/holomush/cmd_plugin_validate.go`
- Modify: `Taskfile.yaml`

- [ ] **Step 1: Write the validator shell script**

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# scripts/validate-plugin.sh
#
# Manifest validator for plugin authors. Parses the manifest and runs
# the same ValidateCrypto + ResolveCryptoRefs pipeline the loader uses,
# without actually loading the plugin.
#
# Usage: scripts/validate-plugin.sh <plugin-dir-or-yaml-path>

set -euo pipefail

target="${1:?usage: validate-plugin.sh <plugin-dir-or-yaml-path>}"

if [[ -d "$target" ]]; then
    manifest="$target/plugin.yaml"
elif [[ -f "$target" ]]; then
    manifest="$target"
else
    echo "validate-plugin: $target does not exist" >&2
    exit 2
fi

if [[ ! -f "$manifest" ]]; then
    echo "validate-plugin: $manifest not found" >&2
    exit 2
fi

# Invoke the Go-side validator via a small CLI binary built on demand.
go run ./cmd/holomush plugin validate "$manifest"
```

- [ ] **Step 2: Make script executable and lint clean**

Run:

```bash
chmod +x scripts/validate-plugin.sh
shellcheck scripts/validate-plugin.sh
```

Expected: shellcheck PASS.

- [ ] **Step 3: Add the Taskfile target**

Edit `Taskfile.yaml`:

```yaml
  plugin:validate:
    desc: "Validate a plugin's manifest (manifest grammar + crypto.emits/consumes rules)"
    cmds:
      - ./scripts/validate-plugin.sh {{.CLI_ARGS}}
    silent: false
```

- [ ] **Step 4: Add `plugin validate` subcommand**

The parent `NewPluginCmd` was added in Task 7b. Add the validate subcommand and wire it into the parent.

```go
// cmd/holomush/cmd_plugin_validate.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// NewPluginValidateCmd is `holomush plugin validate <manifest-path>`.
// Author-time validator that runs ValidateCrypto + ResolveCryptoRefs
// (self-refs only, since at author time we don't have the full
// registry).
func NewPluginValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <manifest-path>",
		Short: "Validate a plugin manifest (grammar + crypto.emits rules)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			m, err := plugins.ParseManifest(raw)
			if err != nil {
				return fmt.Errorf("parse: %w", err)
			}
			if err := plugins.ValidateCrypto(m); err != nil {
				return fmt.Errorf("validate: %w", err)
			}
			selfReg := map[string][]plugins.CryptoEmit{}
			if m.Crypto != nil {
				selfReg[m.Name] = m.Crypto.Emits
			}
			if err := plugins.ResolveCryptoRefs(m, selfReg); err != nil {
				return fmt.Errorf("resolve: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "OK")
			return nil
		},
	}
}
```

Wire it into the plugin parent. Edit `cmd/holomush/cmd_plugin.go`:

```go
func NewPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Plugin authoring and inspection commands",
	}
	cmd.AddCommand(NewPluginValidateCmd())  // NEW
	return cmd
}
```

- [ ] **Step 5: Run a smoke test against a real plugin**

Run: `task plugin:validate -- plugins/core-communication/plugin.yaml`

Expected: prints `OK`.

Run a negative case:

```bash
echo '
name: bad
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
crypto:
  emits:
    - event_type: foo
      sensitivity: kinda
' > /tmp/bad.yaml

task plugin:validate -- /tmp/bad.yaml
```

Expected: nonzero exit, error output mentions `PLUGIN_CRYPTO_INVALID_SENSITIVITY`.

- [ ] **Step 6: Commit**

Suggested message:

```text
feat(cli): plugin validate subcommand + task plugin:validate

Author-time manifest validator running ValidateCrypto +
ResolveCryptoRefs (self-refs only). Builds and runs the existing
holomush binary; no new artifacts.

Refs: docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md
```

---

## Task 9: CLI introspection — `holomush plugin events list/show`

**Files:**

- Create: `cmd/holomush/cmd_plugin_events.go`
- Create: `cmd/holomush/cmd_plugin_events_test.go`
- Modify: existing plugin-subcommand registration (e.g. `cmd/holomush/cmd_plugin.go`)

- [ ] **Step 1: Write failing test for `plugin events list`**

```go
// cmd/holomush/cmd_plugin_events_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginEventsListPrintsAllManifestEmits(t *testing.T) {
	// runCmd is a test helper that invokes the cobra root with args and
	// returns stdout + exit code. (Add this helper if not present.)
	out, code := runCmd(t, []string{
		"plugin", "events", "list",
		"--plugin-dir", "../../plugins", // relative to cmd/holomush/ tests
	})
	require.Equal(t, 0, code)
	require.Contains(t, out, "core-communication:whisper")
	require.Contains(t, out, "always")
	require.Contains(t, out, "core-objects:object_create")
	require.Contains(t, out, "never")
}

func TestPluginEventsListFiltersBySensitivity(t *testing.T) {
	out, code := runCmd(t, []string{
		"plugin", "events", "list",
		"--plugin-dir", "../../plugins",
		"--sensitivity", "always",
	})
	require.Equal(t, 0, code)
	require.Contains(t, out, "core-communication:whisper")
	assert.False(t, strings.Contains(out, "object_create"),
		"never-sensitivity events must not appear in --sensitivity=always output")
}

func TestPluginEventsShowPrintsDetail(t *testing.T) {
	out, code := runCmd(t, []string{
		"plugin", "events", "show",
		"--plugin-dir", "../../plugins",
		"core-communication:whisper",
	})
	require.Equal(t, 0, code)
	require.Contains(t, out, "Direct character-to-character private message")
	require.Contains(t, out, "Owned by: core-communication")
	require.Contains(t, out, "Sensitivity: always")
}
```

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run "TestPluginEvents" ./cmd/holomush/`

Expected: FAIL — subcommands don't exist.

- [ ] **Step 3: Implement the subcommands**

```go
// cmd/holomush/cmd_plugin_events.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	plugins "github.com/holomush/holomush/internal/plugin"
)

type pluginEvent struct {
	Plugin      string
	EventType   string
	Sensitivity plugins.Sensitivity
	Description string
}

func newPluginEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Inspect plugin event-type declarations",
	}
	cmd.AddCommand(newPluginEventsListCmd())
	cmd.AddCommand(newPluginEventsShowCmd())
	return cmd
}

func newPluginEventsListCmd() *cobra.Command {
	var pluginDir string
	var filterPlugin string
	var filterSensitivities []string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all event types declared by loaded plugins",
		RunE: func(cmd *cobra.Command, args []string) error {
			events, err := scanPluginEvents(pluginDir)
			if err != nil {
				return err
			}
			rows := filterEvents(events, filterPlugin, filterSensitivities)
			return printEventsTable(cmd.OutOrStdout(), rows)
		},
	}
	cmd.Flags().StringVar(&pluginDir, "plugin-dir", "plugins", "directory containing plugin subdirectories")
	cmd.Flags().StringVar(&filterPlugin, "plugin", "", "filter to a single plugin")
	cmd.Flags().StringSliceVar(&filterSensitivities, "sensitivity", nil, "filter to specific sensitivities (always, may, never)")
	return cmd
}

func newPluginEventsShowCmd() *cobra.Command {
	var pluginDir string
	cmd := &cobra.Command{
		Use:   "show <plugin>:<event_type>",
		Short: "Show full declaration for one event type",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			events, err := scanPluginEvents(pluginDir)
			if err != nil {
				return err
			}
			ref := args[0]
			for _, e := range events {
				qualified := e.Plugin + ":" + e.EventType
				if qualified == ref {
					fmt.Fprintf(cmd.OutOrStdout(), "Owned by: %s\nEvent: %s\nSensitivity: %s\nDescription: %s\n",
						e.Plugin, e.EventType, e.Sensitivity, e.Description)
					return nil
				}
			}
			return fmt.Errorf("event type %q not found", ref)
		},
	}
	cmd.Flags().StringVar(&pluginDir, "plugin-dir", "plugins", "directory containing plugin subdirectories")
	return cmd
}

func scanPluginEvents(rootDir string) ([]pluginEvent, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, err
	}
	var out []pluginEvent
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		manifestPath := filepath.Join(rootDir, ent.Name(), "plugin.yaml")
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			continue // not a plugin directory
		}
		m, err := plugins.ParseManifest(raw)
		if err != nil {
			continue
		}
		if m.Crypto == nil {
			continue
		}
		for _, e := range m.Crypto.Emits {
			out = append(out, pluginEvent{
				Plugin: m.Name, EventType: e.EventType,
				Sensitivity: e.Sensitivity, Description: e.Description,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Plugin != out[j].Plugin {
			return out[i].Plugin < out[j].Plugin
		}
		return out[i].EventType < out[j].EventType
	})
	return out, nil
}

func filterEvents(in []pluginEvent, plugin string, sensitivities []string) []pluginEvent {
	if plugin == "" && len(sensitivities) == 0 {
		return in
	}
	allowed := map[string]bool{}
	for _, s := range sensitivities {
		allowed[s] = true
	}
	var out []pluginEvent
	for _, e := range in {
		if plugin != "" && e.Plugin != plugin {
			continue
		}
		if len(allowed) > 0 && !allowed[string(e.Sensitivity)] {
			continue
		}
		out = append(out, e)
	}
	return out
}

func printEventsTable(w io.Writer, events []pluginEvent) error {
	for _, e := range events {
		fmt.Fprintf(w, "%-32s %-8s %s\n",
			e.Plugin+":"+e.EventType,
			string(e.Sensitivity),
			e.Description)
	}
	return nil
}
```

Wire `newPluginEventsCmd()` into the `plugin` parent (created in Task 7b). Edit `cmd/holomush/cmd_plugin.go`:

```go
func NewPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Plugin authoring and inspection commands",
	}
	cmd.AddCommand(NewPluginValidateCmd())
	cmd.AddCommand(newPluginEventsCmd())   // NEW
	return cmd
}
```

- [ ] **Step 4: Run to verify pass**

Run: `task test -- -run "TestPluginEvents" ./cmd/holomush/`

Expected: PASS for all three sub-tests.

- [ ] **Step 5: Smoke test the CLI manually**

```bash
go build -o /tmp/holomush ./cmd/holomush
/tmp/holomush plugin events list --plugin-dir plugins
/tmp/holomush plugin events list --plugin-dir plugins --sensitivity always
/tmp/holomush plugin events show --plugin-dir plugins core-communication:whisper
```

Expected: tabular output for list; detail block for show.

- [ ] **Step 6: Commit**

Suggested message:

```text
feat(cli): plugin events list/show subcommands

Reads plugin manifests from --plugin-dir and prints declared event-type
catalogue. Supports filtering by plugin and sensitivity. Show subcommand
takes a qualified <plugin>:<event_type> and prints the full declaration.

Refs: docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md
```

---

## Task 10: Auto-generate per-plugin event reference docs

**Files:**

- Create: `scripts/gen-event-docs.sh`
- Modify: `Taskfile.yaml` (add `docs:gen-events` target; wire into `docs:build`)
- Create: `site/docs/reference/events/.gitkeep` (so the directory exists in git)
- Create: `site/docs/reference/events/.gitignore` (ignore generated files)
- Modify: `site/docs/reference/events.md` (index page)

The generator reuses the `holomush plugin events` CLI to produce per-plugin Markdown.

- [ ] **Step 1: Write the generator script**

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# scripts/gen-event-docs.sh — regenerate site/docs/reference/events/*.md
# from plugin manifests. Idempotent. Safe to run repeatedly.

set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
plugin_dir="$repo_root/plugins"
out_dir="$repo_root/site/docs/reference/events"

mkdir -p "$out_dir"

# Build the holomush binary into a temp location so we don't depend on
# what's installed.
bin="$(mktemp -d)/holomush"
go build -o "$bin" "$repo_root/cmd/holomush"

# One page per plugin that declares any crypto.emits.
for plugin in $(ls -1 "$plugin_dir"); do
    if [[ ! -f "$plugin_dir/$plugin/plugin.yaml" ]]; then
        continue
    fi
    out="$out_dir/$plugin.md"
    {
        printf "# %s — events\n\n" "$plugin"
        printf "_Auto-generated from \`plugins/%s/plugin.yaml\` by \`task docs:gen-events\`. Do not edit._\n\n" "$plugin"
        printf "| Event type | Sensitivity | Description |\n"
        printf "| --- | --- | --- |\n"
        "$bin" plugin events list --plugin-dir "$plugin_dir" --plugin "$plugin" \
            | awk '{
                etype = $1
                sens = $2
                $1 = $2 = ""
                desc = $0
                gsub(/^ +| +$/, "", desc)
                printf "| `%s` | %s | %s |\n", etype, sens, desc
            }'
        printf "\n"
    } > "$out"
done

# Re-emit the top-level index.
{
    printf "# Event type reference\n\n"
    printf "Per-plugin event-type catalogues, auto-generated from plugin manifests.\n\n"
    printf "Each event type identifier is qualified with its owning plugin, e.g. \`core-communication:whisper\`.\n\n"
    for plugin in $(ls -1 "$plugin_dir"); do
        if [[ -f "$out_dir/$plugin.md" ]]; then
            printf "- [%s](events/%s.md)\n" "$plugin" "$plugin"
        fi
    done
} > "$repo_root/site/docs/reference/events.md"

echo "Generated $(ls -1 "$out_dir"/*.md 2>/dev/null | wc -l | tr -d ' ') plugin event pages + index"
```

- [ ] **Step 2: Make script executable and lint clean**

```bash
chmod +x scripts/gen-event-docs.sh
shellcheck scripts/gen-event-docs.sh
```

Expected: shellcheck PASS.

- [ ] **Step 3: Add the Taskfile target**

```yaml
  docs:gen-events:
    desc: "Regenerate site/docs/reference/events/<plugin>.md from plugin manifests"
    cmds:
      - ./scripts/gen-event-docs.sh
    sources:
      - plugins/*/plugin.yaml
    generates:
      - site/docs/reference/events/*.md
      - site/docs/reference/events.md
```

Wire into `docs:build`:

```yaml
  docs:build:
    deps: [docs:gen-events]    # add this line if not present
    # ... existing cmds ...
```

- [ ] **Step 4: Run the generator**

Run: `task docs:gen-events`

Expected: prints "Generated N plugin event pages + index"; `site/docs/reference/events/*.md` populated; `site/docs/reference/events.md` rewritten as the index.

- [ ] **Step 5: Add `.gitignore` for generated outputs**

`site/docs/reference/events/.gitignore`:

```text
*.md
!.gitkeep
```

The `.md` files inside `events/` are generated; the index `events.md` (one level up) is also generated but is the primary entry point and useful to have committed for browsing on GitHub. Decide locally whether to commit the generated index — if yes, omit it from any `.gitignore`.

- [ ] **Step 6: Build the docs site to verify**

Run: `task docs:build`

Expected: PASS. Open `site/site/` (built output path) and verify the events reference renders.

- [ ] **Step 7: Commit**

Suggested message:

```text
feat(docs): auto-generate per-plugin event reference from manifests

Adds task docs:gen-events which builds holomush, runs plugin events
list per plugin, and writes site/docs/reference/events/<plugin>.md.
Wired into task docs:build. Output files are gitignored; the index
events.md is committed.

Refs: docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md
```

---

## Task 11: Author-facing documentation for plugin authors

**Files:**

- Create: `site/docs/extending/event-sensitivity.md`
- Modify: `site/docs/extending/events.md` (add cross-link)
- Modify: `site/docs/extending/plugin-guide.md` (add cross-link)

- [ ] **Step 1: Write `site/docs/extending/event-sensitivity.md`**

This is a new author-facing guide. Voice MUST match `site/CLAUDE.md` (conversational, grounded, no filler, acknowledge MU\* tradition where it helps). Contents:

```markdown
# Declaring event sensitivity

Some events your plugin emits should not be readable by everyone — whisper
content, private-scene poses, DM contents. HoloMUSH lets you declare these
in your plugin's manifest so the host can encrypt the payload at rest and
on the wire.

This page covers what to declare, how the runtime treats it, and what to
expect when subscribing to other plugins' sensitive events.

## What sensitivity means

Every event type your plugin emits gets one of three sensitivity contracts
declared in `plugin.yaml`:

| Contract | Meaning |
| --- | --- |
| `always` | Every event of this type is sensitive. Payload always encrypted. |
| `may` | The emit-site decides per-event. Carry the privacy decision at runtime. |
| `never` | The event is never sensitive. Payload always cleartext. |

The classic MU\* `whisper` is `always`: there is no public version.

A scene `pose` is `may`: a pose in a public room is fine to broadcast; the
same pose in a hidden scene is private.

A `presence` ping is `never`: who's online is metadata, not content.

## Declaring it

Add a `crypto:` block to your plugin's `plugin.yaml`:

\`\`\`yaml
crypto:
  emits:
    - event_type: whisper
      sensitivity: always
      description: "Direct character-to-character private message."
    - event_type: pose
      sensitivity: may
      description: "Action description; sensitive when the scene is hidden."
    - event_type: presence
      sensitivity: never
\`\`\`

The host validates this when loading your plugin. Mistakes (unknown
sensitivity values, duplicate event types, malformed cross-plugin
references) fail the load with a typed error pointing at the problem.

## Subscribing to other plugins' sensitive events

When you subscribe to a subject that can carry sensitive payloads, the
default delivery is metadata-only: you get the event with `metadata_only`
set to `true` and an empty `payload`. You can read who acted, what kind of
event it was, when, where — but not what was said.

If your plugin actually needs the content (a moderation filter that scans
for slurs, a scene-summarizer that reads poses), declare it explicitly:

\`\`\`yaml
crypto:
  consumes:
    - subjects:
        - "events.*.character.*.whisper"
      requests_decryption:
        - "core-communication:whisper"
\`\`\`

`requests_decryption` is opt-in — listing a sensitive event type signals
"my plugin wants plaintext for these." The runtime still requires an ABAC
grant before you actually receive plaintext; the manifest declaration is
the *capability*, not the grant.

References use the qualified form `<plugin>:<event_type>` so the loader
can verify the referenced plugin actually declares the event type as
sensitive. The referenced plugin must also be in your `dependencies`.

## What handlers receive

Your event handler always gets the same proto shape. Check `metadata_only`
before processing the payload:

\`\`\`go
if event.MetadataOnly {
    // We weren't authorized for plaintext (or didn't request it).
    // Skip or do metadata-only work.
    return
}
// Plaintext available in event.Payload.
\`\`\`

A plugin handler that crashes on metadata-only delivery is just a bug —
the contract is documented, and the host won't shield you from forgetting
it.

## Runtime gates (Phase 3)

Phase 1 of the event-payload-crypto rollout records your declarations.
Phase 3 (in progress) enforces them at runtime: emit-time refusal of
mismatched declarations, payload encryption, and the AuthGuard that
gates plaintext delivery. If you ship a manifest now with the right
sensitivity classifications, you'll be ready when the runtime catches up.

## See also

- [Plugin guide](plugin-guide.md) — manifest structure overview
- [Events](events.md) — event delivery basics
- [Event reference](../reference/events.md) — auto-generated catalogue per plugin
```

- [ ] **Step 2: Add cross-links in `events.md` and `plugin-guide.md`**

In `events.md`, near the top:

```markdown
For event sensitivity (whisper, DM, private-scene content), see
[Declaring event sensitivity](event-sensitivity.md).
```

In `plugin-guide.md`, in the manifest reference section:

```markdown
The optional `crypto` block declares event-type sensitivity contracts
and decryption opt-ins. See [Declaring event sensitivity](event-sensitivity.md).
```

- [ ] **Step 3: Run docs build and lint**

Run: `task docs:build && task lint:markdown`

Expected: PASS. The site builds and the new page renders.

- [ ] **Step 4: Commit**

Suggested message:

```text
docs(extending): author-facing guide for declaring event sensitivity

Adds site/docs/extending/event-sensitivity.md explaining the
crypto.emits/consumes manifest sections, sensitivity contracts,
and the metadata_only delivery contract. Cross-links from events.md
and plugin-guide.md.

Refs: docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md
Refs: docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md §9.3
```

---

## Task 12: Acceptance verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full test matrix**

Run: `task pr-prep`

Expected: PASS. All gates green.

- [ ] **Step 2: Verify the bead acceptance criteria**

Re-read `holomush-k18g`'s acceptance criteria (in `bd show holomush-k18g` output) and check each:

- `internal/core/event.go` contains zero plugin-owned EventType constants. Verify:

```bash
rg -n "^\s+EventType(Say|Pose|Page|Whisper|Pemit|OOC|WhisperNotice|ObjectCreate|ObjectDestroy|ObjectUse|ObjectExamine|ObjectGive)" internal/core/event.go
```

Expected: no matches.

- Every plugin declares its event types in `crypto.emits` with sensitivity. Verify:

```bash
for p in plugins/*/; do
    yq '.crypto.emits' "$p/plugin.yaml" 2>/dev/null
done
```

Expected: every plugin yields a non-null result (or an explicit empty section for plugins that emit nothing).

- Cross-plugin event references use the qualified form. Spot-check by running:

```bash
rg -n 'requests_decryption' plugins/*/plugin.yaml
```

Expected: every entry uses `<plugin>:<event>` format.

- Auto-generated docs build cleanly. Verify:

```bash
task docs:gen-events && task docs:build
```

Expected: PASS.

- Loader rejects misdeclared manifests. Verify by running the unit tests added in Tasks 3 and 4:

```bash
task test -- -run "TestValidateCrypto|TestLoaderRejectsManifestWith" ./internal/plugin/
```

Expected: PASS.

- [ ] **Step 3: Update the bead**

Run: `bd close holomush-k18g`

(Or if the bead is to remain open until further verification: `bd update holomush-k18g --notes "Phase 1 complete; awaiting Phase 2 (provider interface)"`.)

- [ ] **Step 4: File follow-up beads**

For each open question deferred to implementation in spec §12, file a beads issue if not already filed:

```bash
bd create --title="<question>" --type=task --priority=3 --parent=holomush-k18g \
    --description="Deferred from event-payload-crypto Phase 1 per spec Section 12 question N. ..."
```

(Spec §12 questions 11 and 12 are the implementation-detail ones that should be filed at this point. The future-work questions stay in the spec until their phase arrives.)

- [ ] **Step 5: Commit any leftover changes and bead notes; do NOT push yet**

Phase 1 is now feature-complete locally. Per "Landing the Plane" in CLAUDE.md, the user controls the final push.

---

## Phase 1 acceptance summary

| Spec invariant or requirement | Verified by |
| --- | --- |
| INV-6 (sensitivity declared at manifest level) | Task 3 + Task 4 + Task 7 |
| INV-7 (sensitivity-always emit-time check) | DEFERRED to Phase 3 (runtime enforcement) |
| INV-45 (qualified `<plugin>:<event_type>` references) | Task 3 + Task 4 |
| Spec §7.1 loader-validation rules (six checks) | Task 3 + Task 4 unit tests |
| Spec §11.1 phase 1 deliverables | Tasks 5–7 (event-type migration + manifest updates) |
| Spec §9.3 docs deliverable: event-sensitivity.md | Task 11 |
| Spec §9.5 docs deliverable: per-plugin events reference | Task 10 |
| `holomush-k18g` re-scoped acceptance criteria | Task 12 verification |

Phase 2 (provider interface + crypto_keys + events_audit migration + DEKManager skeleton) follows in the next plan: `docs/superpowers/plans/2026-04-25-event-payload-crypto-phase2-provider-skeleton.md` (to be written).
