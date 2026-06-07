<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Event-Type Wire Convention Canonicalization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every plugin's emitted wire event type plugin-qualified `<plugin>:<verb>`, fix core-scenes (which renders nothing in prod — `holomush-r0kup`) and `core-communication:emit` to conform, and add a loader gate + meta-test so the convention can't drift again.

**Architecture:** Three event-type vocabularies, governed differently: the **registered-emit set** and **`crypto.emits[].event_type`** stay **bare** (INV-PLUGIN-32 forces them equal; `requests_decryption`/`splitQualifiedRef` recover the bare verb). The **wire type + `verbs[].type` + downstream stored type** are **qualified**. `emitEntryMatchesWireType` (`internal/plugin/crypto_manifest.go:89`, already merged in #4395) bridges bare-crypto ↔ qualified-wire. core-scenes is migrated **qualify-end-to-end** (no users → uniformity over churn).

**Tech Stack:** Go, gopher-lua (Lua plugins), `task` runner, testify, Ginkgo (integration), the `integrationtest` harness, PostgreSQL (scene_log).

**Spec:** `docs/superpowers/specs/2026-06-06-event-type-wire-convention-design.md`
**Design bead:** `holomush-aneim` (depends on `holomush-r0kup`)

---

## File map

| File | Responsibility | Change |
| --- | --- | --- |
| `plugins/core-scenes/plugin.yaml` | core-scenes manifest | **Add** `verbs:` block (14 qualified types); `crypto.emits` stays bare |
| `plugins/core-scenes/commands.go` | scene command emit + replay | Qualify `EmitIntent.Type` stamp; re-key `replayEventKinds` |
| `plugins/core-scenes/service.go` | lifecycle emits | Qualify 3 stamped `Type` literals |
| `plugins/core-scenes/publish_events.go` | publish-notice emits | Qualify 6 stamped `Type` literals |
| `plugins/core-scenes/publish_snapshot.go` | snapshot reconstruction | Re-key `snapshotEventKinds` |
| `plugins/core-scenes/publish_store.go` | scene_log read | Qualify the SQL `type IN (...)` filter |
| `plugins/core-scenes/audit.go` | audit dispatch | Qualify the `eventType == "scene_pose"` check |
| `plugins/core-scenes/main.go` | emit-type registration | **No change** — `phase4/phase6EmitTypes` stay bare |
| `plugins/core-communication/main.lua` | comm emits | Qualify `emit` wire type (`:178`) |
| `internal/plugin/manifest.go` | manifest validation | **Add** gate in `Manifest.Validate()`: `verbs[].type` must be `<plugin>:<verb>` (runs at `ParseManifest`, fail-fast at load) |
| `internal/testsupport/integrationtest/crypto.go` | test harness | **Qualify** emit-helper stamp (`plugin + ":" + eventType`) so ~15 bare-type callers keep working; **remove** scene-verb seeding |
| `test/meta/` | meta-tests | **Add** all-plugins-verbs-qualified test |
| `.claude/rules/event-conventions.md` | conventions doc | Document the three-vocabulary rule |
| `docs/architecture/invariants.yaml` | invariant registry | `INV-PLUGIN-40` already added with the spec (`binding: pending`); Task 8 **binds** it |

**Invariant — these stay BARE, do NOT qualify:** `plugins/core-scenes/main.go` `phase4EmitTypes()`/`phase6EmitTypes()` and every `crypto.emits[].event_type` in `plugins/*/plugin.yaml`. Qualifying them trips `EVENT_TYPE_REGISTRY_MISMATCH` at load (INV-PLUGIN-32).

The qualified prefix is composed at the **emit-stamp** boundary only; `handleEmit`'s `verb := strings.TrimPrefix(eventType, "scene_")` (`commands.go:1230`) and the dispatch site (`commands.go:519-525`) keep passing the **bare** verb-type into `handleEmit` — only the stamped `EmitIntent.Type` is qualified. So those two sites need **no change**.

---

## Phase 1: Foundational fix — make core-scenes a normal plugin (`holomush-r0kup`)

### Task 1: Add the core-scenes `verbs:` block

**Files:**

- Modify: `plugins/core-scenes/plugin.yaml` (add a top-level `verbs:` block)
- Test: `internal/plugin/manager_test.go` (or a new `plugins/core-scenes/manifest_verbs_test.go`)

- [ ] **Step 1: Write the failing test** — assert the parsed manifest registers all 14 scene types as qualified verbs.

In a new file `plugins/core-scenes/manifest_verbs_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestCoreScenesManifestDeclaresQualifiedVerbsForEveryEmitType pins
// holomush-r0kup: every emitted scene wire type MUST have a qualified
// verbs[].type entry, or RenderingPublisher hard-fails EMIT_UNKNOWN_VERB.
func TestCoreScenesManifestDeclaresQualifiedVerbsForEveryEmitType(t *testing.T) {
	data, err := os.ReadFile("plugin.yaml")
	require.NoError(t, err)
	var m struct {
		Verbs []struct {
			Type string `yaml:"type"`
		} `yaml:"verbs"`
	}
	require.NoError(t, yaml.Unmarshal(data, &m))

	got := make(map[string]bool, len(m.Verbs))
	for _, v := range m.Verbs {
		got[v.Type] = true
	}
	want := []string{
		"core-scenes:scene_pose", "core-scenes:scene_say", "core-scenes:scene_emit",
		"core-scenes:scene_ooc", "core-scenes:scene_join_ic", "core-scenes:scene_leave_ic",
		"core-scenes:scene_pose_order_changed_ic", "core-scenes:scene_idle_nudge",
		"core-scenes:scene_publish_started", "core-scenes:scene_publish_vote_cast",
		"core-scenes:scene_publish_cooloff_started", "core-scenes:scene_publish_resolved",
		"core-scenes:scene_publish_withdrawn", "core-scenes:scene_publish_vote_attempts_extended",
	}
	for _, w := range want {
		require.Truef(t, got[w], "missing qualified verb entry %q", w)
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `task test -- -run TestCoreScenesManifestDeclaresQualifiedVerbsForEveryEmitType ./plugins/core-scenes/`
Expected: FAIL (no `verbs:` block → zero entries).

- [ ] **Step 3: Add the `verbs:` block** to `plugins/core-scenes/plugin.yaml` (top level, sibling of `crypto:`). The enums are CLOSED (`internal/plugin/manifest.go`): `category` ∈ {communication, movement, state, system, command} (`:176`); `format` ∈ {speech, action, narrative, notification, error, snapshot, delta} (`:181`); `display_target` ∈ {terminal, state, both} (`:1655`). `label` is a free optional string. IC content uses `category: communication`; lifecycle + publish notices use `category: state` + `format: notification`:

```yaml
verbs:
  - type: core-scenes:scene_pose
    category: communication
    format: action
    display_target: terminal
  - type: core-scenes:scene_say
    category: communication
    format: speech
    label: says
    display_target: terminal
  - type: core-scenes:scene_emit
    category: communication
    format: action
    display_target: terminal
  - type: core-scenes:scene_ooc
    category: communication
    format: action
    display_target: terminal
  - type: core-scenes:scene_join_ic
    category: state
    format: notification
    display_target: terminal
  - type: core-scenes:scene_leave_ic
    category: state
    format: notification
    display_target: terminal
  - type: core-scenes:scene_pose_order_changed_ic
    category: state
    format: notification
    display_target: terminal
  - type: core-scenes:scene_idle_nudge
    category: state
    format: notification
    display_target: terminal
  - type: core-scenes:scene_publish_started
    category: state
    format: notification
    display_target: terminal
  - type: core-scenes:scene_publish_vote_cast
    category: state
    format: notification
    display_target: terminal
  - type: core-scenes:scene_publish_cooloff_started
    category: state
    format: notification
    display_target: terminal
  - type: core-scenes:scene_publish_resolved
    category: state
    format: notification
    display_target: terminal
  - type: core-scenes:scene_publish_withdrawn
    category: state
    format: notification
    display_target: terminal
  - type: core-scenes:scene_publish_vote_attempts_extended
    category: state
    format: notification
    display_target: terminal
```

- [ ] **Step 4: Run the test + schema check, verify pass**

Run: `task test -- -run TestCoreScenesManifestDeclaresQualifiedVerbsForEveryEmitType ./plugins/core-scenes/`
Expected: PASS.
Run: `task lint` (the plugin schema check validates `plugin.yaml`).
Expected: PASS (verbs items satisfy `schemas/plugin.schema.json`: `type`/`category`/`format`/`display_target` required).

- [ ] **Step 5: Commit**

`jj commit -m "feat(core-scenes): declare qualified verbs for every emit type (holomush-r0kup)"`

---

### Task 2: Qualify core-scenes emit-site stamped types

**Files:**

- Modify: `plugins/core-scenes/commands.go:1323` (the `EmitIntent.Type` stamp)
- Modify: `plugins/core-scenes/service.go:801,842,879`
- Modify: `plugins/core-scenes/publish_events.go` (six `scene_publish_*` stamps)
- Test: `plugins/core-scenes/commands_emit_test.go` (existing — update expected types)

- [ ] **Step 1: Add the qualifier constant + helper** at the top of `plugins/core-scenes/commands.go` (after imports):

```go
// scenePluginName is this plugin's manifest name; the qualified wire event
// type is scenePluginName + ":" + <bare verb>. crypto.emits + RegisterEmitTypes
// keep the bare form (INV-PLUGIN-32); only the wire/stored type is qualified
// (holomush-aneim).
const scenePluginName = "core-scenes"

// qualifyScene returns the qualified wire event type for a bare scene verb.
func qualifyScene(bare string) string { return scenePluginName + ":" + bare }
```

- [ ] **Step 2: Update the failing test** — `commands_emit_test.go` currently asserts `findIntentByType(sink.intents, "scene_pose")`. Change the four IC assertions to the qualified form:

```go
found := findIntentByType(sink.intents, "core-scenes:scene_pose")
require.NotNil(t, found, "scene pose MUST emit core-scenes:scene_pose")
assert.True(t, found.Sensitive, "scene_pose MUST be Sensitive=true (sensitivity:always)")
```

Apply the same `core-scenes:`-prefix change to the `scene_say`, `scene_emit`, `scene_ooc` lookups in that file.

- [ ] **Step 3: Run it, verify it fails**

Run: `task test -- -run TestSceneSubcommand ./plugins/core-scenes/`
Expected: FAIL (`findIntentByType` returns nil — emit still stamps bare `scene_pose`).

- [ ] **Step 4: Qualify the stamp** at `commands.go:1323`:

```go
	intent := pluginsdk.EmitIntent{
		Subject:   subject,
		Type:      pluginsdk.EventType(qualifyScene(eventType)),
		Payload:   string(payload),
		Sensitive: true, // sensitivity:always per crypto.emits manifest §2 / INV-SCENE-3
	}
```

(Leave the dispatch at `commands.go:519-525` and `verb := strings.TrimPrefix(eventType, "scene_")` at `:1230` **unchanged** — `eventType` stays bare into `handleEmit`; only the stamp qualifies.)

Then qualify the lifecycle stamps in `service.go` (lines ~801/842/879):

```go
		Type:      "core-scenes:scene_join_ic",
		// ...
		Type:      "core-scenes:scene_leave_ic",
		// ...
		Type:      "core-scenes:scene_pose_order_changed_ic",
```

And the six notice stamps in `publish_events.go`, e.g.:

```go
		Type:      pluginsdk.EventType("core-scenes:scene_publish_started"),
```

(repeat for `scene_publish_vote_cast`, `scene_publish_cooloff_started`, `scene_publish_resolved`, `scene_publish_withdrawn`, `scene_publish_vote_attempts_extended`).

- [ ] **Step 5: Update integration delivery-side assertions to the qualified type.** Qualifying the wire type changes what subscribers receive, so the three integration tests that wait-for/assert the bare delivered type MUST update in lockstep (else they time out on a `scene_pose` frame that never arrives — `WaitForEvent` matches `ev.GetType() == eventType`, `internal/testsupport/integrationtest/session.go:137`; the frame type is stamped verbatim at `internal/grpc/server.go:674`). Change `"scene_pose"` → `"core-scenes:scene_pose"` at:
  - `test/integration/scenes/scene_command_join_delivery_test.go:102,106`
  - `test/integration/scenes/lua_focus_parity_test.go:116,121`
  - `test/integration/scenes/real_scene_join_subscription_test.go:104,109`

  (Each is a `WaitForEvent(ctx, "scene_pose")` + a companion `Expect(frame.GetType()).To(Equal("scene_pose"))`. Grep `rg -n 'WaitForEvent\(.*scene_|GetType\(\)\).*scene_' test/integration/scenes/` to confirm the full set before editing — there may be other bare scene types asserted similarly.)

- [ ] **Step 6: Run unit + the affected integration tests, verify pass**

Run: `task test -- -run TestSceneSubcommand ./plugins/core-scenes/`
Expected: PASS.
Run: `task test:int -- ./test/integration/scenes/...`
Expected: PASS (delivery assertions now expect the qualified type).

- [ ] **Step 7: Commit**

`jj commit -m "feat(core-scenes): qualify emit-site wire types + delivery assertions to core-scenes:<verb> (holomush-r0kup)"`

---

### Task 3: Re-key core-scenes read-side consumers to the qualified stored type

**Files:**

- Modify: `plugins/core-scenes/commands.go:80-84` (`replayEventKinds`)
- Modify: `plugins/core-scenes/publish_snapshot.go:32-36` (`snapshotEventKinds`)
- Modify: `plugins/core-scenes/publish_store.go:640` (SQL `type IN (...)`)
- Modify: `plugins/core-scenes/audit.go:399` (`eventType == "scene_pose"`)
- Test: `plugins/core-scenes/commands_emit_test.go` / existing replay + snapshot + audit tests

- [ ] **Step 1: Write/extend the failing test** — assert replay decodes a qualified-typed event. Add to `plugins/core-scenes/commands_emit_test.go`:

```go
func TestDecodeReplayEntriesAcceptsQualifiedSceneTypes(t *testing.T) {
	events := []pluginsdk.Event{
		{Type: "core-scenes:scene_pose", Payload: `{"actor_id":"01HZZ0000000000000000000AA","text":"waves"}`},
	}
	entries, err := decodeReplayEntries(events)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, EntryKindPose, entries[0].Kind)
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `task test -- -run TestDecodeReplayEntriesAcceptsQualifiedSceneTypes ./plugins/core-scenes/`
Expected: FAIL (`replayEventKinds` keyed on bare `"scene_pose"` → `continue`, zero entries).

- [ ] **Step 3: Re-key the maps + SQL + audit check to qualified.**

`commands.go:80-84`:

```go
var replayEventKinds = map[string]EntryKind{
	"core-scenes:scene_pose": EntryKindPose,
	"core-scenes:scene_say":  EntryKindSay,
	"core-scenes:scene_emit": EntryKindEmit,
}
```

`publish_snapshot.go:32-36`:

```go
var snapshotEventKinds = map[string]EntryKind{
	"core-scenes:scene_pose": EntryKindPose,
	"core-scenes:scene_say":  EntryKindSay,
	"core-scenes:scene_emit": EntryKindEmit,
}
```

`publish_store.go:640` (SQL literal):

```go
		WHERE subject = $1 AND type IN ('core-scenes:scene_pose', 'core-scenes:scene_say', 'core-scenes:scene_emit')
```

`audit.go:399`:

```go
	if eventType == "core-scenes:scene_pose" {
```

- [ ] **Step 4: Run the read-side tests, verify pass**

Run: `task test -- ./plugins/core-scenes/`
Expected: PASS (replay/snapshot/audit tests green with qualified types).

- [ ] **Step 5: Commit**

`jj commit -m "refactor(core-scenes): re-key read-side consumers to qualified stored type (holomush-r0kup)"`

---

### Task 4: Qualify harness emit helpers, remove scene-verb seeding, add the r0kup regression

**Background (grounded):** `EmitSceneICContent`/`EmitScenePlaintextContent` (`internal/testsupport/integrationtest/crypto.go:183`) are the harness stand-ins for the core-scenes plugin emitting. They take a `plugin` arg and an `eventType`, stamp `Type: pluginsdk.EventType(eventType)` **verbatim** (`crypto.go:245`), publish via the `RenderingPublisher`-backed emitter wired by `WithPluginCrypto` (`crypto.go:117`), and **return `EmittedEvent`** (not `error`) — they `require.NoError` the publish internally, so a failed publish (e.g. `EMIT_UNKNOWN_VERB`) fails the spec. ~15 existing callers pass **bare** types (`"scene_pose"`); they only work today because `registerSceneEmitVerbs` seeds bare verbs into the registry.

**Decision:** centralize qualification in the helpers (they own the `plugin` name) so they faithfully mirror the post-fix plugin AND the ~15 existing bare-type callers keep compiling unchanged. The helpers compose `plugin + ":" + eventType`; seeding is removed; verbs now come from the core-scenes manifest (Task 1).

**Files:**

- Modify: `internal/testsupport/integrationtest/crypto.go` — qualify the stamp in the emit helpers; delete `registerSceneEmitVerbs` + its call site
- Test: `test/integration/scenes/scene_render_verb_test.go` (new, Ginkgo, `//go:build integration`)

- [ ] **Step 1: Qualify the harness emit helpers.** In `crypto.go`, change the verbatim stamp at `:245` so the helper composes the owning-plugin prefix (mirroring the real plugin). At the stamp site in `emitPluginEventForScene` (and any sibling used by `EmitScenePlaintextContent`):

```go
	// Mirror the real plugin: emit the plugin-qualified wire type so it resolves
	// against the manifest-sourced verb registry (holomush-aneim / r0kup).
	wireType := eventType
	if !strings.Contains(wireType, ":") {
		wireType = plugin + ":" + eventType
	}
	// ... Type: pluginsdk.EventType(wireType)
```

(The `!Contains(":")` guard keeps it idempotent if a caller already passes a qualified type. Confirm the exact local var names by reading `emitPluginEventForScene`; ensure `strings` is imported.)

- [ ] **Step 2: Write the r0kup regression test** — a core-scenes IC emit resolves against the **manifest-sourced** verb registry (no seeding) and publishes without `EMIT_UNKNOWN_VERB`. New file `test/integration/scenes/scene_render_verb_test.go`:

```go
//go:build integration

package scenes_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// Verifies holomush-r0kup: core-scenes events resolve in the verb registry
// from its MANIFEST verbs: block (no test seeding), so the production
// RenderingPublisher does not reject them with EMIT_UNKNOWN_VERB.
var _ = Describe("core-scenes renders via its own manifest verbs", func() {
	It("publishes an IC pose without EMIT_UNKNOWN_VERB", func(ctx SpecContext) {
		// WithPluginCrypto wires the RenderingPublisher-backed plugin emitter
		// (crypto.go:117); EmitSceneICContent panics without it (crypto.go:282).
		ts := integrationtest.Start(GinkgoT(),
			integrationtest.WithInTreePlugins(), integrationtest.WithPluginCrypto())
		// ... create a scene + participant + actorID via the harness helpers
		// (ground the create-scene/join pattern from a sibling scenes spec, e.g.
		// real_scene_join_subscription_test.go).
		//
		// Pass the BARE verb — the helper now qualifies it (Step 1). The helper
		// require.NoErrors the publish internally, so EMIT_UNKNOWN_VERB fails the
		// spec. emitted is an EmittedEvent (NOT error).
		emitted := ts.EmitSceneICContent(ctx, "core-scenes", sceneID, actorID,
			"scene_pose", `{"actor_id":"`+actorID.String()+`","text":"waves"}`)
		Expect(emitted.SubjectStr).NotTo(BeEmpty(),
			"core-scenes:scene_pose must resolve in the manifest verb registry and publish")
	})
})
```

(`EmitSceneICContent` returns `EmittedEvent` with a `SubjectStr` field — confirm the field name against `crypto.go` + existing callers like `seed_encrypted_ic_validation_test.go:51`. Do NOT assign to `err`.)

- [ ] **Step 3: Delete the seeding.** Remove `registerSceneEmitVerbs` and its call site from `crypto.go`. core-scenes now contributes verbs via its manifest `verbs:` block (Task 1). Existing callers pass bare types; the Step-1 helper change qualifies them, so they keep working.

- [ ] **Step 4: Verify the regression ordering** — confirm the new test would FAIL without Task 1's verbs block: temporarily comment out the core-scenes `verbs:` block, run the new spec, observe the `EMIT_UNKNOWN_VERB` failure (proving the test is not vacuous), then restore the block.

Run: `task test:int -- ./test/integration/scenes/...`
Expected (block present): PASS. Expected (block removed): FAIL with `EMIT_UNKNOWN_VERB`.

- [ ] **Step 5: Run the full scene + crypto integration suites, verify pass**

Run: `task test:int -- ./test/integration/scenes/... ./test/integration/crypto/...`
Expected: PASS — all ~15 existing `EmitSceneICContent`/`EmitScenePlaintextContent` callers (passing bare types) now emit qualified wire types via the helper and resolve against manifest verbs; crypto/readback suites stay green (the `emitEntryMatchesWireType` bridge resolves bare `crypto.emits` against the qualified wire type).

- [ ] **Step 6: Commit**

`jj commit -m "test(core-scenes): qualify harness emit helpers, drop verb seeding, add r0kup regression (holomush-r0kup)"`

---

## Phase 2: core-communication:emit fix

### Task 5: Qualify the `core-communication:emit` wire type

**Files:**

- Modify: `plugins/core-communication/main.lua:178`
- Test: `internal/plugin/lua/corecomm_sensitive_emit_test.go` (extend the existing brace-aware scan, or a sibling assertion)

- [ ] **Step 1: Write the failing test** — assert the generic `emit` command emits the qualified wire type. Add to `internal/plugin/lua/corecomm_sensitive_emit_test.go` (it already reads `main.lua` via `repoRoot`):

```go
func TestCoreCommunicationEmitUsesQualifiedWireType(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "plugins", "core-communication", "main.lua"))
	require.NoError(t, err)
	src := string(raw)
	require.Contains(t, src, `type = "core-communication:emit"`,
		"the generic emit command MUST emit the qualified wire type (matches verbs[].type; holomush-aneim)")
	require.NotContains(t, src, `type = "emit"`,
		"bare emit wire type must be gone (EMIT_UNKNOWN_VERB in production)")
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `task test -- -run TestCoreCommunicationEmitUsesQualifiedWireType ./internal/plugin/lua/`
Expected: FAIL (`main.lua:178` still emits bare `type = "emit"`).

- [ ] **Step 3: Qualify the emit** at `plugins/core-communication/main.lua:178`:

```lua
        {subject ="location." .. loc, type = "core-communication:emit", payload = payload}
```

- [ ] **Step 4: Run the test, verify pass**

Run: `task test -- -run TestCoreCommunicationEmitUsesQualifiedWireType ./internal/plugin/lua/`
Expected: PASS.

- [ ] **Step 5: Commit**

`jj commit -m "fix(core-communication): qualify the emit wire type to core-communication:emit (holomush-aneim)"`

---

## Phase 3: Enforcement (prevent drift)

### Task 6: Manifest gate — reject unqualified `verbs[].type` in `Manifest.Validate()`

> **Refinement over the spec:** the spec said "at the verb-registration loop in `manager.go:1138`". Implement it instead in `Manifest.Validate()` (`internal/plugin/manifest.go:525-537`), where the sibling verb `category`/`format`/`display_target` validation already lives. `ParseManifest` calls `Validate()` (`manifest.go:377`), so this still fires fast-at-load (at discovery/parse, before any load side-effects), AND it is directly unit-testable — unlike `manager.go`'s `TestLoadPlugin` (`:1313`), which returns no error.

**Files:**

- Modify: `internal/plugin/manifest.go` (verb validation loop, ~`:525-537`)
- Test: `internal/plugin/manifest_test.go` (sibling of the existing verb-category/format validation tests)

- [ ] **Step 1: Write the failing test** — a manifest whose `verbs[].type` is bare (or foreign-prefixed, or multi-colon) is rejected by `Validate()` with the typed code. Add to `internal/plugin/manifest_test.go`:

```go
func TestManifestValidateRejectsUnqualifiedVerbType(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"bare":        "say",
		"foreign":     "other:say",
		"multi-colon": "demo:say:extra",
		"prefix-only": "demo:",
	}
	for name, verbType := range cases {
		t.Run(name, func(t *testing.T) {
			m := &plugins.Manifest{
				Name: "demo", Version: "1.0.0", Type: plugins.TypeLua,
				Verbs: []plugins.VerbSpec{{
					Type: verbType, Category: "communication", Format: "action", DisplayTarget: "terminal",
				}},
			}
			err := m.Validate()
			require.Error(t, err)
			require.Equal(t, "PLUGIN_WIRE_TYPE_NOT_QUALIFIED", oops.AsOops(err).Code())
		})
	}
}

func TestManifestValidateAcceptsQualifiedVerbType(t *testing.T) {
	t.Parallel()
	m := &plugins.Manifest{
		Name: "demo", Version: "1.0.0", Type: plugins.TypeLua,
		Verbs: []plugins.VerbSpec{{
			Type: "demo:say", Category: "communication", Format: "action", DisplayTarget: "terminal",
		}},
	}
	require.NoError(t, m.Validate())
}
```

(`plugins.VerbSpec` — `manifest.go:168` — fields `Type`/`Category`/`Format`/`Label`/`DisplayTarget`. `oops.AsOops(err).Code()` per `internal/plugin/manager_test.go:1796` + the grpc-errors rule. Confirm `manifest_test.go` is package `plugins_test` and imports `oops`.)

- [ ] **Step 2: Run it, verify it fails**

Run: `task test -- -run 'TestManifestValidateRejectsUnqualifiedVerbType|TestManifestValidateAcceptsQualifiedVerbType' ./internal/plugin/`
Expected: the reject cases FAIL (bare/foreign/etc. currently accepted); the accept case passes.

- [ ] **Step 3: Add the gate** in the verb loop in `Manifest.Validate()`, immediately after the existing `if v.Type == ""` empty-check (`manifest.go:525-527`), before the `validVerbCategories` check:

```go
		if want := m.Name + ":"; !strings.HasPrefix(v.Type, want) ||
			len(v.Type) <= len(want) || strings.Count(v.Type, ":") != 1 {
			return oops.Code("PLUGIN_WIRE_TYPE_NOT_QUALIFIED").
				In("manifest").With("plugin", m.Name).With("verb", v.Type).
				Errorf("verbs[].type must be %q-prefixed (<plugin>:<verb>, one colon); got %q", m.Name, v.Type)
		}
```

(`strings` is already imported in `manifest.go`.)

- [ ] **Step 4: Run the test + the full plugin package, verify pass**

Run: `task test -- -run 'TestManifestValidate' ./internal/plugin/`
Expected: PASS.
Run: `task test -- ./internal/plugin/...`
Expected: PASS — core-communication/core-objects/core-scenes verbs are all qualified (Tasks 1, 5), so no real manifest regresses. If any *test fixture* constructs a bare-verb `VerbSpec`, update it to qualified (a grep found none in-tree today).

- [ ] **Step 5: Commit**

`jj commit -m "feat(plugin): Manifest.Validate rejects unqualified verbs[].type (holomush-aneim)"`

---

### Task 7: Meta-test — every in-tree plugin's `verbs[].type` is qualified

**Files:**

- Test: `test/meta/plugin_verb_qualification_test.go` (new)

- [ ] **Step 1: Write the meta-test** — walk `plugins/*/plugin.yaml`, assert each `verbs[].type` is `<dir-name>:<verb>` (single colon). It MUST NOT assert anything about `crypto.emits` / registered-emit sets (those are bare by INV-PLUGIN-32).

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestEveryInTreePluginVerbTypeIsQualified pins holomush-aneim: every plugin's
// verbs[].type MUST be "<plugin-dir>:<verb>" so RenderingPublisher resolves it.
func TestEveryInTreePluginVerbTypeIsQualified(t *testing.T) {
	root := findRepoRoot(t) // the test/meta repo-root helper (meta_helpers_test.go:33)
	dirs, err := filepath.Glob(filepath.Join(root, "plugins", "*", "plugin.yaml"))
	require.NoError(t, err)
	for _, path := range dirs {
		pluginDir := filepath.Base(filepath.Dir(path))
		data, err := os.ReadFile(path) //nolint:gosec // test-only scan of in-tree manifests
		require.NoError(t, err)
		var m struct {
			Verbs []struct {
				Type string `yaml:"type"`
			} `yaml:"verbs"`
		}
		require.NoError(t, yaml.Unmarshal(data, &m), "parse %s", path)
		want := pluginDir + ":"
		for _, v := range m.Verbs {
			require.Truef(t, strings.HasPrefix(v.Type, want) && strings.Count(v.Type, ":") == 1 && len(v.Type) > len(want),
				"%s: verbs[].type %q must be %q-prefixed (<plugin>:<verb>)", path, v.Type, pluginDir)
		}
	}
}
```

(`findRepoRoot(t)` is the `test/meta` helper at `meta_helpers_test.go:33`. The package is `meta` — every file in `test/meta/` is `package meta`, NOT `meta_test`; using `meta_test` would put `findRepoRoot` out of scope and fail to compile.)

- [ ] **Step 2: Run it, verify it passes** (all in-tree plugins are now qualified after Tasks 1, 5)

Run: `task test -- -run TestEveryInTreePluginVerbTypeIsQualified ./test/meta/`
Expected: PASS.

- [ ] **Step 3: Sanity-check the guard catches a regression** — temporarily change one core-scenes verb to bare in a scratch copy mentally; the test would fail. (No code change; this step is a reasoning check that the assertion is not vacuous — `len(m.Verbs) > 0` for core-scenes/core-communication/core-objects guarantees it runs.)

- [ ] **Step 4: Commit**

`jj commit -m "test(meta): enforce qualified verbs[].type across in-tree plugins (holomush-aneim)"`

---

### Task 8: Documentation + invariant registry

**Files:**

- Modify: `.claude/rules/event-conventions.md` (add the three-vocabulary rule)
- Modify: `docs/architecture/invariants.yaml` (`INV-PLUGIN-40` already present from the spec change; flip to `binding: bound` in Step 4)
- Generate: `docs/architecture/invariants.md` via `go run ./cmd/inv-render`

- [ ] **Step 1: Add the convention** to `.claude/rules/event-conventions.md` under a new `## Event-type vocabularies` section:

```markdown
## Event-type vocabularies (qualified wire / bare crypto)

An event type appears in three places, governed differently:

- **Wire type + `verbs[].type` + downstream stored type** MUST be plugin-qualified
  `<plugin>:<verb>` (one colon). `RenderingPublisher.Lookup` resolves the wire
  type against `verbs[].type` and hard-fails `EMIT_UNKNOWN_VERB` on a miss. The
  loader rejects an unqualified `verbs[].type` (`PLUGIN_WIRE_TYPE_NOT_QUALIFIED`).
- **Registered-emit set (`RegisterEmitTypes`/`register_emit_type`) and
  `crypto.emits[].event_type`** stay **bare** `<verb>`. INV-PLUGIN-32 forces the
  two equal; `requests_decryption` refs are `<plugin>:<verb>` and
  `splitQualifiedRef` recovers the bare verb. Qualifying these trips
  `EVENT_TYPE_REGISTRY_MISMATCH` at load.
- `emitEntryMatchesWireType` (`internal/plugin/crypto_manifest.go`) is the single
  bridge: it matches a bare `crypto.emits` entry against the qualified wire type
  by composing the plugin name. Do not add other bare↔qualified shims.
```

- [ ] **Step 2: The invariant entry already exists** — `INV-PLUGIN-40` was added to `docs/architecture/invariants.yaml` (`binding: pending`) alongside the spec (per `.claude/rules/invariants.md`'s capture-at-spec-time rule). This task does NOT add it; verify it is present and matches the summary below. Task 8's job for the invariant is the *binding* in Step 4.

```yaml
  - id: INV-PLUGIN-40
    scope: INV-PLUGIN
    origin_spec: "docs/superpowers/specs/2026-06-06-event-type-wire-convention-design.md"
    summary: "Every emitted wire event type and every verbs[].type MUST be plugin-qualified
      <owning-plugin>:<verb> (one colon); the registered-emit set and crypto.emits[].event_type
      stay bare, bridged by emitEntryMatchesWireType. Manifest.Validate rejects an unqualified
      verbs[].type with PLUGIN_WIRE_TYPE_NOT_QUALIFIED."
    binding: pending
```

- [ ] **Step 3: Regenerate + verify**

Run: `go run ./cmd/inv-render`
Expected: `wrote docs/architecture/invariants.md`.
Run: `task test -- ./test/meta/`
Expected: PASS (registry drift check + the new verb-qualification meta-test green; the new invariant is tolerated as `binding: pending`).

- [ ] **Step 4: Bind the invariant (optional, same change)** — the Task 6 gate test genuinely asserts the reject behavior, and the Task 7 meta-test asserts the positive (all in-tree verbs qualified). If binding now: add `// Verifies: INV-PLUGIN-40` above `TestManifestValidateRejectsUnqualifiedVerbType` (`internal/plugin/manifest_test.go`) and above `TestEveryInTreePluginVerbTypeIsQualified` (`test/meta/plugin_verb_qualification_test.go`), flip the entry to `binding: bound` + `asserted_by: ["internal/plugin/manifest_test.go", "test/meta/plugin_verb_qualification_test.go"]`, rerun `go run ./cmd/inv-render`, and confirm `task test -- ./test/meta/` stays green. Otherwise leave `binding: pending` (tolerated; backfill tracked separately).

- [ ] **Step 5: Commit**

`jj commit -m "docs(invariants): document + register the qualified-wire event-type convention (holomush-aneim)"`

---

## Final verification

- [ ] `task test` (full unit suite) — green.
- [ ] `task test:int -- ./test/integration/scenes/... ./test/integration/crypto/...` — green (Docker).
- [ ] `task lint` — green (plugin schema, sloglint, license).
- [ ] `task pr-prep` — green before PR.
- [ ] `rg -n 'type = "emit"' plugins/core-communication/main.lua` — zero hits.
- [ ] Confirm `holomush-r0kup` acceptance: a manifest-sourced (un-seeded) core-scenes emit publishes without `EMIT_UNKNOWN_VERB`.

## Out of scope

- Structured (non-string) event types (spec: considered, rejected).
- Any change to `crypto.emits` semantics, `requests_decryption`, `splitQualifiedRef`, `emitEntryMatchesWireType`, or the `EnforceSensitivity` truth table.
- Backward compatibility / mixed-history (no users; waived).

## Pre-push review gates

This change touches `internal/plugin/` (loader, plugin emit/readback adjacency) → **crypto-reviewer** (it touches the crypto-vocabulary boundary) **and** code-reviewer MUST run before push, per CLAUDE.md.
<!-- adr-capture: sha256=c3c0095649f68e1f; session=cli; ts=2026-06-07T02:30:01Z; adrs=holomush-yl3mf,holomush-8aure,holomush-1gwns -->
