<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Harness Threads the Engine's ABAC Resolver/PluginProvider Into the Plugin Layer

**Status:** Accepted
**Decision:** holomush-jvrq3
**Related:** holomush-vjg7z (plugin layer as opt-in harness capability — not superseded), holomush-f5t07 (WithRealABAC), holomush-0f0f4.9 (consumer)

## Context

The `internal/testsupport/integrationtest` harness loads in-tree plugins via
`startPlugins` (PR #4275, `WithInTreePlugins`). That code allocates **standalone**
`attribute.NewResolver(...)` and `attribute.NewPluginProvider(nil)` instances,
independent of whichever access engine the harness wires. `PluginSubsystem.Start`
calls `resolver.RegisterProvider` per plugin that declares resource types (e.g.
core-scenes' `scene` namespace) onto that standalone resolver.

Under the harness default (`allowAllPolicyEngine`) this is harmless — the engine
ignores attributes. But when a **real seeded ABAC engine** is present
(`WithRealABAC`, holomush-f5t07), the engine evaluates against the
`*attribute.Resolver` it was *built* with. If the plugin subsystem registered its
providers on a *different* (standalone) resolver, every policy gating a
plugin-declared namespace (`resource.scene.*`) resolves against an empty
attribute set and **silently default-denies** — no error, no log. This is the
exact failure fingerprint of holomush-g776 (LocationProvider unregistered) and
holomush-xxel (PropertyProvider), and it is the blocking defect for
holomush-0f0f4.9 (cross-plugin ABAC permit/deny).

## Decision

When the harness boots a real ABAC engine alongside plugins, `startPlugins` MUST
receive the engine subsystem's **own** `*attribute.Resolver`,
`*attribute.PluginProvider`, and `pluginauthz.Auditor` instances (pointer
identity), obtained from `abacsetup.ABACSubsystem.AttributeResolver()` /
`PluginProvider()` / `AuditLogger()`. When no real engine is present (allow-all
default), freshly-allocated standalone instances remain correct.

The selection is centralized in a `pluginAttrSources(abacSub)` helper: it returns
the subsystem's instances when `abacSub != nil`, else fresh standalone ones. The
contract is enforced by INV-RA-4 (`require.Same` on the resolver and plugin
provider).

## Rationale

- Plugin attribute providers register on a resolver instance at load time; the
  engine evaluates against the resolver it was built with. Mismatched instances
  produce silent default-deny — the hardest failure mode to diagnose, and the
  documented root cause of two prior multi-week regressions.
- `abacsetup.ABACSubsystem` exposes all three concrete handles precisely because
  the plugin layer and the engine must share instances; using them is "doing what
  production does."
- The invariant is cross-cutting: any future harness capability that combines an
  attribute-providing subsystem with a real evaluating engine must honor it.
  Recording it as an ADR makes the contract discoverable before a contributor
  introduces a third subsystem that re-breaks it.

## Alternatives Considered

- **Keep the standalone resolver; post-construction swap the engine's resolver
  reference.** Rejected: requires a mutable resolver-swap API that does not (and
  should not) exist; introduces an ordering hazard between plugin registration
  and engine use; no clean pointer-identity test.
- **Status quo (standalone resolver, allow-all only).** Rejected: makes
  real-ABAC + plugins a permanently broken combination in the harness;
  holomush-0f0f4.9 could never pass; the `WithInTreePlugins` path would silently
  misevaluate `resource.scene.*` the moment a real engine is introduced.

## Consequences

- **Positive:** cross-plugin ABAC permit/deny becomes testable at integration
  tier; the wiring rule is regression-detected by `require.Same`; future
  harness opt-ins have a documented contract.
- **Negative:** `startPlugins` accepts caller-supplied resolver/provider/auditor
  via `pluginDeps` instead of self-allocating; its code path branches on whether
  a real subsystem is present.
- **Neutral:** the `pluginAttrSources` helper makes the branch explicit and
  unit-testable.
