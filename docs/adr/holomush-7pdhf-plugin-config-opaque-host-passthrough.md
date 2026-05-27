<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Plugin Runtime Config Is Plugin-Owned via Opaque Host Passthrough

**Date:** 2026-05-27
**Status:** Accepted
**Decision:** holomush-7pdhf
**Deciders:** HoloMUSH Contributors

## Context

HoloMUSH plugins had no sanctioned mechanism for tunable runtime behavior
config (windows, intervals, limits). The core server already provisions
resources a plugin declares in its manifest — a Postgres schema for
`storage: postgres`, host service addresses for `requires:`, crypto-emit
capture for `crypto.emits`. That raised the question: should the core server
also *understand* a plugin's behavior config?

The core-scenes plugin made the gap concrete with a real bug ("cfg-zero"):
`main.go` constructed the service with a raw struct literal `&SceneServiceImpl{}`,
so the 7d/30m publish-vote window defaults — applied only in the unused
`NewSceneServiceImpl` constructor — were never set. Production publish windows
resolved to `0/0`, collapsing a 7-day vote window to the next ≤30s scheduler
sweep. There was no seam for a test harness to set short windows either.

This decision pairs with [[holomush-ikozq]] (the manifest declares a *typed
schema*); this ADR covers *who owns the config and how it crosses the
host↔plugin boundary*.

## Decision

Plugin runtime config is **plugin-owned**, delivered by the host as an **opaque
passthrough**:

- The plugin **manifest** declares the config schema (keys → type, default,
  required); see [[holomush-ikozq]].
- The **host** reads that schema, validates only **generic types**
  (`duration`/`int`/`bool`/`string`), merges an optional **server-provided
  override** per key (`manifest default < override`), and delivers the resulting
  `map<string,string>` to the plugin — for binary plugins via a new
  `ServiceConfig.plugin_config` field at `Init`; for Lua via the `holomush.config`
  global. The host **never interprets a key's meaning**.
- The **plugin** owns all key semantics and decodes the delivered map itself
  (`pluginsdk.DecodeConfig[T]` for Go; `holomush.config` accessors for Lua).

The host is therefore **semantically opaque but generically type-aware** —
"this value is a duration" is structural validation the host may do; "`vote_window`
governs publish voting" is plugin semantics the host must not know.

**Delivery contract:** the binary `needsInit` gate (`internal/plugin/goplugin/host.go`)
is extended to include `len(manifest.Config) > 0`, so a plugin declaring *only*
`config:` still receives `Init` and therefore its `plugin_config`. This is
codified as invariant **INV-PC-8** with a regression test (rather than a separate
ADR — it is the binary-runtime delivery mechanism for this decision).

## Rationale

Opaque passthrough is the only option that keeps config provisioning consistent
with how the host already treats every other declared resource: it provisions
the Postgres schema, host service addresses, and crypto-emit capture a plugin
declares without *understanding* any of them. Making config the one resource the
host interprets would single it out as a boundary violation
([[holomush-z1e7]]) and force a host change for every new plugin config key.

Plugin ownership also fixes the cfg-zero bug class by construction rather than by
discipline: because config is loaded from the manifest on every init path —
including the extended binary `needsInit` gate (INV-PC-8) — the zero-value struct
literal that silently collapsed core-scenes' publish windows is unreachable in
production. Generic-type awareness (not semantic awareness) is the minimum the
host needs to validate structure and apply the `default < override` merge, so it
buys load-time safety and a clean test-override seam without coupling the
substrate to any plugin's meaning.

## Alternatives Considered

- **Host-semantic config** (the core server registers and interprets known
  config keys). Rejected: violates the plugin boundary ([[holomush-z1e7]]),
  couples plugin semantics to the substrate, and requires a host change for
  every new plugin config key. Directly contrary to the directive that the core
  server cannot know or care about a plugin's config needs.
- **Hardcoded Go defaults (status quo).** Rejected: no test tunability, the
  cfg-zero bug class (constructor bypass silently yields zero values), and no
  override seam — fundamentally not a config primitive.

## Consequences

**Positive**

- Any plugin can declare runtime config with no substrate change.
- The cfg-zero bug class is eliminated by construction: config is always loaded
  from the manifest on every init path; the zero-value struct literal is
  unreachable in production.
- The server-provided override channel (`PluginSubsystemConfig.PluginConfigOverrides`)
  gives a test harness a clean seam to set short windows without production
  impact — the seam [[holomush-shcyu]] consumes to drive the Phase 6 publish E2E.

**Negative**

- The host cannot enforce cross-key semantic constraints (e.g. `vote_window >
  cooloff_window`); such checks live in the plugin.
- Plugin authors must call `DecodeConfig` / the `holomush.config` accessors;
  forgetting them yields zero values — mitigated by `required: true` +
  load-time fail-fast.

**Neutral**

- The host understands the four generic types for structural validation only;
  this is generic-type awareness, not plugin-semantic awareness.
