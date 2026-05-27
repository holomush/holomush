<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Task 0 Survey — Plugin Manager Construction Sites

**Plan:** Phase 1.6 gateway-verb-registry-sourcing
**Status:** Pre-flight survey (read-only)
**Audience:** Task 20 implementer (plugin manager `WithVerbRegistry` tightening)

---

## Purpose

Enumerate every `plugins.NewManager(...)` call site across the repository and
classify how it should be migrated when Task 20 makes `WithVerbRegistry`
required.

## Classification key

Per spec Section 5 decision table:

| Code | Meaning | What to pass post-Task 20 |
|------|---------|---------------------------|
| **EMPTY** | Pure manager construction / lifecycle (load, unload, dependency resolution, audit, alias seeding, etc.). No event emission via `PluginEventEmitter.Emit`. Manifest-load tests that expect verb registrations to take effect ALSO go here — the loader populates the registry. | `core.NewVerbRegistry()` |
| **SEEDED** | Test emits any event via `PluginEventEmitter.Emit` — directly or transitively (typically via `ConfigureEventEmitter` + `EmitPluginEvent`). Seeded with builtins so host-owned types resolve. | `core.BootstrapVerbRegistry("test")` (introduced in Task 5) |
| **SEEDED+** | Same as SEEDED but the test exercises explicit plugin-verb registration on top of builtins. | `core.BootstrapVerbRegistry("test")` plus explicit `reg.Register(...)` calls. |
| **PRODUCTION** | The real production wiring (`cmd/holomush/sub_grpc.go` indirectly via `internal/plugin/setup/subsystem.go`). Gets the host-owned `VerbRegistry` Task 1 introduces. | `WithVerbRegistry(verbRegistry)` from the gateway/host bootstrap. |

**Default:** When uncertain, choose **SEEDED** (safer — extra registrations
never break a test).

---

## Path A: Production / Setup (1 site)

| File:Line | Already has `WithVerbRegistry`? | Classification | Notes |
|---|---|---|---|
| `internal/plugin/setup/subsystem.go:267` | No | **PRODUCTION** | This is *the* hole this whole effort closes. The plugin manager is constructed without a verb registry, while the gateway has its own separate `core.NewVerbRegistry()` + `RegisterBuiltinTypes` (`cmd/holomush/gateway.go:286-287`). Task 1 will introduce a single shared `VerbRegistry` constructed at the host (sub_grpc) and threaded into both the gateway and this `managerOpts` slice via `plugins.WithVerbRegistry(verbRegistry)`. |

---

## Path B: Tests in `internal/plugin/`

### `internal/plugin/manager_test.go`

| Line | `WithVerbRegistry` today? | Classification | Rationale |
|---|---|---|---|
| 67 (`TestManagerDiscover`) | No | **EMPTY** | Discovery only; no LoadAll, no emit. |
| 96 (`TestManagerDiscoverSkipsInvalidPlugins`) | No | **EMPTY** | Discovery only. |
| 108 (`TestManagerDiscoverEmptyDirectory`) | No | **EMPTY** | Discovery only. |
| 118 (`TestManagerDiscoverNonExistentDirectory`) | No | **EMPTY** | Discovery only. |
| 143 (`TestManagerDiscoverSkipsFilesNotDirectories`) | No | **EMPTY** | Discovery only. |
| 159 (`TestManagerDiscoverSkipsDirWithoutManifest`) | No | **EMPTY** | Discovery only. |
| 186 (`TestManagerDiscoverMultiplePlugins`) | No | **EMPTY** | Discovery only. |
| 216 (`TestManagerDiscoverBinaryPlugin`) | No | **EMPTY** | Discovery only. |
| 228 (`TestManagerListPluginsNoPluginsLoaded`) | No | **EMPTY** | Listing only. |
| 251 (`TestManagerLoadAllLuaPlugins`) | No | **EMPTY** | Plugin load lifecycle; no emit. |
| 278 (`TestManagerLoadAllSkipsInvalidManifests`) | No | **EMPTY** | Manifest validation; no emit. |
| 297 (`TestManagerLoadAllSkipsLuaPluginsWithoutHost`) | No | **EMPTY** | Lifecycle. |
| 315 (`TestManagerLoadAllSkipsBinaryPlugins`) | No | **EMPTY** | Lifecycle. |
| 337 (`TestManagerLoadAllFailsOnLuaSyntaxError`) | No | **EMPTY** | Load failure path. |
| 364 (`TestLoadAllSkipsBrokenPluginsWhenGracefulDegradationEnabled`) | No | **EMPTY** | Load lifecycle; graceful degradation. |
| 382 (`TestManagerCloseWithoutLuaHost`) | No | **EMPTY** | Close lifecycle. |
| 399 (`TestManagerClose`) | No | **EMPTY** | Close lifecycle. |
| 429 (`TestManagerClosePropagatesHostError`) | No | **EMPTY** | Close error path. |
| 448 (`TestManagerIsPluginLoaded`) | No | **EMPTY** | Read-only state check. |
| 453 (`TestManagerGetLoadedPluginReturnsFalseWhenNotLoaded`) | No | **EMPTY** | Read-only. |
| 475 (`TestManagerGetLoadedPluginReturnsPluginAfterLoad`) | No | **EMPTY** | Lifecycle. |
| 488 (`TestManagerWithServiceRegistryReturnsConfiguredRegistry`) | No | **EMPTY** | Construction-only sanity check. |
| 493 (`TestManagerRegistryReturnsNilWhenNotConfigured`) | No | **EMPTY** | Construction-only. |
| 526 (`TestManagerLoadAllUsesDAGWhenRegistryConfigured`) | No | **EMPTY** | DAG resolution; no emit. |
| 591 (`TestManagerRegistersProvidedServicesAfterBinaryPluginLoad`) | No | **EMPTY** | Service registration; no emit. |
| 625 (`TestManagerSkipsServiceRegistrationWhenNoRegistry`) | No | **EMPTY** | Service registration negative path. |
| 653 (`TestManagerSkipsServiceRegistrationWhenHostLacksConnProvider`) | No | **EMPTY** | Service registration negative path. |
| 687 (`TestManagerRegistersMultipleProvidedServices`) | No | **EMPTY** | Service registration. |
| 747 (`TestManagerLoadAllSeedsAliasesFromManifests`) | No | **EMPTY** | Alias seeding; no emit. |
| 819 (`TestManagerLoadAllSeedsAliasesDeterministicallyAcrossLoads`) | No | **EMPTY** | Alias seeding. |
| 854 (`TestManagerLoadAllWithoutAliasSeederSkipsSeeding`) | No | **EMPTY** | Alias seeding negative path. |
| 973 (`TestManagerLoadAllRejectsCommandCapabilityOnUnknownResourceType`) | No | **EMPTY** | Capability validation; no emit. |
| 1015 (`TestManagerLoadAllAcceptsCapabilityOnAnotherPluginsResourceType`) | No | **EMPTY** | Capability validation. |
| 1045 (`TestManagerLoadAllRejectsCommandCapabilityOnUnknownAction`) | No | **EMPTY** | Capability validation. |
| 1076 (`TestManagerLoadAllAcceptsCapabilityWithDeclaredAction`) | No | **EMPTY** | Capability validation. |
| 1116 (`TestManagerLoadAllAcceptsCapabilityOnAnotherPluginsAction`) | No | **EMPTY** | Capability validation. |
| 1144 (`TestManagerLoadAllAcceptsPluginRedeclaringCoreAction`) | No | **EMPTY** | Capability validation. |
| 1159 (`TestManagerWithTrustAllowlistDoesNotInterfereWithBasicLoad`) | No | **EMPTY** | Trust allowlist. |
| 1182 (`TestManagerLoadAllWarnsOnTrustAllowlistedPluginNotDiscovered`) | No | **EMPTY** | Trust allowlist warning path. |
| 1219 (`TestManagerLoadAllDoesNotWarnWhenAllowlistMatchesDiscoveredPlugin`) | No | **EMPTY** | Trust allowlist. |
| 1250 (`TestManagerLoadAllStrictModeJoinsMultipleErrors`) | No | **EMPTY** | Strict mode error joining. |
| 1307 (`TestManagerLoadAllFailsWhenSchemaDiscoveryReturnsError`) | No | **EMPTY** | Schema validation. |
| 1342 (`TestManagerLoadAllFailsWhenSchemaMissingDeclaredResourceType`) | No | **EMPTY** | Schema validation. |
| 1367 (`TestManagerLoadAllFailsWhenHostMissingAttributeResolverProvider`) | No | **EMPTY** | Attribute provider validation. |
| 1428 (`TestManagerLoadAllUnregistersAttributeProviderWhenSchemaValidationFailsAfterRegistration`) | No | **EMPTY** | Attribute provider unregistration. |
| 1480 (`TestManagerLoadAllRegistersAttributeProviderViaCallback`) | No | **EMPTY** | Attribute provider callback. |
| 1492 (`newManagerForTest` helper, used by `QuerySessionStreams` tests L1514–1610) | No | **EMPTY** | Helper for `TestManagerQuerySessionStreams*` — pure routing/contribution tests, no emit. |
| 1663 (`TestManagerLoadAllRegistersVerbsFromManifest`) | **YES (`reg := core.NewVerbRegistry()`)** | **EMPTY (already)** | Already passes empty registry — manifest loader populates it. Already aligned with target shape. **No migration needed.** |
| 1712 (`TestManagerLoadAllRejectsPluginWithDuplicateVerbType`) | **YES** | **EMPTY (already)** | Already passes a registry seeded with one pre-existing entry, then exercises duplicate detection. **No migration needed.** Note: pre-seeds via `reg.Register(...)` — keep as-is. |
| 1757 (`TestManagerLoadAllCleansUpVerbsOnPartialFailure`) | **YES** | **EMPTY (already)** | Same shape as L1712. **No migration needed.** |
| 1797 (`TestManagerLoadAllWithoutVerbRegistrySkipsVerbRegistration`) | No (intentional) | **DELETE / REWRITE** | This test asserts the *current* "silently skip when nil" contract that Task 20 is removing. **Action:** delete (covered by manifest-loaded tests above) or rewrite to assert "rejects nil registry at construction" once `WithVerbRegistry` becomes required. Flag for Task 20 implementer. |
| 1835 (`TestConfigureFocusDepsInjectsCoordinatorIntoLuaHost`) | No | **EMPTY** | Focus coordinator wiring; no emit. |
| 1849 (`TestConfigureFocusDepsWithNilLuaHostDoesNotPanic`) | No | **EMPTY** | Construction-only nil-safety check. |

### `internal/plugin/manager_routing_test.go`

| Line | `WithVerbRegistry` today? | Classification | Rationale |
|---|---|---|---|
| 128 (`TestManagerRegisterHost`) | No | **EMPTY** | Pure registration test; no emit. |
| 141 (`TestManagerRegisterHostPanicsOnNil`) | No | **EMPTY** | Construction nil-check. |
| 149 (`TestManagerRegisterHostBackfillsConfiguredEventEmitter`) | No | **SEEDED** | Calls `mgr.ConfigureEventEmitter(bus.Bus.Publisher())` (L152). Even if no event flows in the assertion, Task 20's tightening means the host's emitter is wired and any host-owned event Type would resolve. Default to seeded since emitter is configured. |
| 160 (`TestManagerLoadAllExposesInflightManifestToInitTimeEmitter`) | No | **SEEDED+** | Calls `ConfigureEventEmitter` (L190) AND emits `pluginsdk.EventType(core.EventTypeSystem)` (L181-183). `system` is host-owned → must be in the registry. **Definitely seeded.** Test plugin manifest declares no plugin-owned verbs, so no extra `Register` needed; `BootstrapVerbRegistry("test")` alone suffices. |
| 188 (`TestManagerDeliverCommandRoutesToCorrectHost`) | No | **EMPTY** | DeliverCommand routing only; mock host returns no emits and no `ConfigureEventEmitter` call. |
| 220 (`TestManagerDeliverCommandRoutesToCorrectHost` continued — actual emitter test) | No | **EMPTY** | DeliverCommand mock returns `expectedEmits` (L250) but the test verifies the host received them — does not invoke `EmitPluginEvent` itself. No `ConfigureEventEmitter`. |
| 235 (`TestManagerDeliverCommandUnknownPlugin`) | No | **EMPTY** | Negative-path routing. |
| 253 (`TestManagerDeliverEventRoutesToCorrectHost`) | No | **EMPTY** | DeliverEvent routing only; no emit. |
| 268 (`TestManagerDeliverEventUnknownPlugin`) | No | **EMPTY** | Negative-path routing. |
| 285 (`TestManagerEmitPluginEventUsesConfiguredSharedEmitter`) | No | **SEEDED+** | `ConfigureEventEmitter` (L289) + direct `mgr.EmitPluginEvent(...)` (L299) with `Type: pluginsdk.EventType("say")`. `say` is plugin-owned, not in `BootstrapVerbRegistry`. **Action:** seed with `BootstrapVerbRegistry("test")` and explicitly `reg.Register(...)` for `say` (or whatever verb the test publishes). |
| 327 (`TestManagerDeliverCommandConcurrentSafety`) | No | **EMPTY** | Concurrency-safety on DeliverCommand routing only; no emit. |
| 364 (`TestManagerLoadAllSkipsPluginsWithoutHost`) | No | **EMPTY** | Lifecycle. |
| 386 (`TestManagerPluginHostMappingTrackedCorrectly`) | No | **EMPTY** | Internal mapping check. |
| 423 (`TestManagerCloseClearsPluginHostMapping`) | No | **EMPTY** | Close lifecycle. |
| 445 (`TestManagerCloseClosesAllHosts`) | No | **EMPTY** | Close lifecycle. |
| 487 (`TestManagerLoadAllWithPoliciesMultiHost`) | No | **EMPTY** | Multi-host policy install; no emit. |

### `internal/plugin/manager_audit_test.go`

| Line | Classification | Rationale |
|---|---|---|
| 23, 64, 86 (audit subject aggregation) | **EMPTY** | Pure audit-subject readback via `TestLoadPlugin` helper. No LoadAll/no emit. |

### `internal/plugin/manager_crypto_test.go`

| Line | Classification | Rationale |
|---|---|---|
| 146 (`TestDiscoverSkipsPluginWithInvalidCryptoSection`) | **EMPTY** | Discovery only. |
| 171 (`TestDiscoverSkipsPluginWithUnresolvableCryptoRefs`) | **EMPTY** | Discovery only. |
| 194 (`TestDiscoverAcceptsValidCryptoSection`) | **EMPTY** | Discovery only. |
| 205 (`TestDiscoverAcceptsRealPluginsDirectory`) | **EMPTY** | Discovery only on the real plugins directory. |

### `internal/plugin/integration_test.go`

| Line | Classification | Rationale |
|---|---|---|
| 52 (`setupEchoBotTest`) | **EMPTY** | Helper that calls `manager.Discover(ctx)` then loads the echo-bot plugin via `luaHost.Load` — does not call `Manager.LoadAll`, so the manifest-verb path on the manager is not exercised. The downstream `Describe` blocks deliver events via the `luaHost` directly, not via the manager's emit path. The `EmitPluginEvent` mock at L281 is on a separate fake — not the manager's emitter. **Empty registry suffices.** |

### `internal/plugin/subscriber_test.go`

| Line | Classification | Rationale |
|---|---|---|
| 469 (`TestSubscriberRoutesResponseEventsThroughSharedEmitterWithIncomingActor`) | **SEEDED+** | `ConfigureEventEmitter` (L473) + the subscriber routes plugin emits with `Type: "say"` through `EmitPluginEvent` → `PluginEventEmitter.Emit`. `say` is plugin-owned. **Seed with `BootstrapVerbRegistry("test")` and pre-register `say`.** (Verify by reading the test body — same pattern as routing_test L285.) |

### Lua-plugin integration tests in `internal/plugin/`

These tests load real plugins from `../../plugins/` (which DO declare verbs in
their manifests). None call `ConfigureEventEmitter` — the emit path runs
through DeliverEvent/DeliverCommand returning `[]pluginsdk.EmitEvent`, but the
test inspects those returned slices directly rather than routing them through
`EmitPluginEvent`.

| File:Line | Classification | Rationale |
|---|---|---|
| `internal/plugin/help_integration_test.go:289` | **EMPTY** | Loads help plugin, asserts `DeliverCommand` returned-emit shape. Manager populates registry from manifest. No `ConfigureEventEmitter`. |
| `internal/plugin/communication_integration_test.go:48` | **EMPTY** | Loads core-communication plugin (which declares `verbs:`). Manager populates registry. Assertions are on returned-emit slices, not on the emitter path. |
| `internal/plugin/aliases_integration_test.go:90` | **EMPTY** | Alias-loading lifecycle; no emit through emitter. |
| `internal/plugin/building_integration_test.go:111` | **EMPTY** | Building plugin DeliverCommand assertions. |
| `internal/plugin/objects_integration_test.go:50` | **EMPTY** | Loads core-objects plugin (declares verbs); same pattern. |

---

## Path C: Tests in `test/integration/plugin/`

### `test/integration/plugin/verb_registration_test.go`

| Line | `WithVerbRegistry` today? | Classification | Rationale |
|---|---|---|---|
| 64, 99, 131 | **YES** (and pre-calls `core.RegisterBuiltinTypes(verbReg)` in `BeforeEach` L31-32) | **EMPTY (already aligned)** | Already passes a `verbReg` seeded with builtins — exactly the `BootstrapVerbRegistry("test")` shape. **After Task 5 ships `core.BootstrapVerbRegistry`, this test SHOULD migrate to it for clarity, but functionally it is already correct.** |

### `test/integration/plugin/extensible_actions_test.go`

| Line | Classification | Rationale |
|---|---|---|
| 58, 77, 109 | **EMPTY** | Tests that exercise the action-extension manifest fields. No emit through `PluginEventEmitter`. |

### `test/integration/plugin/alias_seeder_test.go`

| Line | Classification | Rationale |
|---|---|---|
| 77, 138, 152, 181, 190 | **EMPTY** | Alias-seeder lifecycle; no emit. |

### `test/integration/plugin/binary_plugin_test.go`

| Line | `ConfigureEventEmitter`? | Classification | Rationale |
|---|---|---|---|
| 116 (`discovers core-scenes plugin`) | No | **EMPTY** | Discovery only. |
| 201 (`loads core-scenes, registers SceneService, and answers RPCs`) | **YES (L205)** | **SEEDED** | `manager.ConfigureEventEmitter(publisher)` is called. The test exercises full plugin lifecycle including event emission through the configured emitter. core-scenes emits scene-domain events, but those are plugin-owned and validated against the plugin's own manifest. Seeded `BootstrapVerbRegistry("test")` is the safe default; the manifest-load path will populate plugin-owned verbs from core-scenes' manifest. |

---

## Summary — counts

| Classification | Count | Notes |
|---|---|---|
| **PRODUCTION** | 1 | `internal/plugin/setup/subsystem.go:267` |
| **EMPTY** (migrate to `core.NewVerbRegistry()` explicitly) | ~75 | Most of the surface |
| **EMPTY (already aligned)** | 4 | `manager_test.go:1663,1712,1757` + `verb_registration_test.go:64/99/131` (already seeded — keep, optionally migrate to `BootstrapVerbRegistry` for clarity) |
| **SEEDED** | 2 | `manager_routing_test.go:149` (Backfills emitter); `binary_plugin_test.go:201` |
| **SEEDED+** | 3 | `manager_routing_test.go:160` (uses `core.EventTypeSystem`); `manager_routing_test.go:285` (`EmitPluginEvent` for `say`); `subscriber_test.go:469` (subscriber routing for `say`) |
| **DELETE / REWRITE** | 1 | `manager_test.go:1797` `TestManagerLoadAllWithoutVerbRegistrySkipsVerbRegistration` — its current contract is exactly what Task 20 removes. |

**Total NewManager call sites surveyed:** 86 (1 production + 85 test)

---

## Notes for Task 20 implementer

1. **Already-aligned tests** (`manager_test.go:1663,1712,1757` + the
   verb_registration integration tests) need no migration *behaviorally*.
   For consistency, they MAY be updated to use
   `core.BootstrapVerbRegistry("test")` once Task 5 ships it, but this is
   cosmetic — `core.NewVerbRegistry()` + manual `Register` calls works identically.

2. **`TestManagerLoadAllWithoutVerbRegistrySkipsVerbRegistration`**
   (`manager_test.go:1797`) **MUST be deleted or rewritten**. It asserts the
   "silently skip when nil registry" behavior that Task 20 removes. The
   manifest-load happy-path assertions in `manager_test.go:1663` and the
   integration tests in `verb_registration_test.go` already cover the
   positive case.

3. **SEEDED+ sites** must register the specific plugin-owned verb the test
   publishes (`say` in two cases) on top of the bootstrap registry. Do NOT
   rely on manifest population — these tests publish without actually loading
   a manifest that declares the verb.

4. **Production wiring** (`internal/plugin/setup/subsystem.go:267`) needs the
   `verbRegistry` value plumbed in via the host bootstrap. This is the
   single-shared-registry change Task 1 introduces. The `managerOpts` slice
   on L258-263 is the insertion point — append
   `plugins.WithVerbRegistry(s.cfg.VerbRegistry)` (or whatever the new field
   on `subsystem.Config` is named).

5. **Lua-plugin integration tests** (`help_integration_test.go`,
   `communication_integration_test.go`, `aliases_integration_test.go`,
   `building_integration_test.go`, `objects_integration_test.go`) load real
   plugins that declare verbs. After Task 20, they MUST pass an empty
   registry so the manifest-load path can populate it. If any of these
   tests transitively emit through `PluginEventEmitter.Emit` (verify by
   inspecting whether they run a path that hits `EmitPluginEvent` with a
   verb not listed in the loaded plugin's manifest), upgrade to SEEDED.

6. **Migration safety**: the spec table's "default to seeded if uncertain"
   rule means `BootstrapVerbRegistry("test")` is always safe. The only
   reason to prefer empty is for tests that specifically assert the
   manifest-load path populates the registry. When in doubt, choose seeded.
