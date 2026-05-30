<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Centralize the Command-Visibility Filter in `internal/command`

**Date:** 2026-05-29
**Status:** Accepted
**Decision:** holomush-nxwl5
**Deciders:** HoloMUSH Contributors

## Context

Command enumeration with ABAC filtering — the two-layer check (`execute
command:<name>` then per-capability `CanPerformAction`) plus a 3-error
circuit-breaker that marks the result `incomplete` — lived only inside
`internal/plugin/hostfunc/commands.go`, serving Lua plugins exclusively.

The recognized-command chip work (holomush-2zjio) adds two more consumers of
that same enumeration: a binary-plugin `PluginHostService.ListCommands` RPC
(plugin-runtime parity) and a client-facing `CoreService.ListAvailableCommands`
RPC for the web composer. If each consumer re-implements the filter, three
independent copies of an ABAC-policy-bearing routine can diverge — a
security-correctness hazard, and a direct violation of the plugin-runtime-symmetry
invariant, which requires one shared enforcement path for trust decisions.

Grounding also surfaced that the filter's logic was mislocated: although it is
Go, it sat in the plugin-runtime adapter package, making a core concern (a query
over the core-owned unified command registry, per ADR holomush-5nu7) look like a
plugin concern.

## Decision

The ABAC-filtered command enumeration is extracted into a single core-owned
function in `internal/command/commandquery`, beside the registry it queries. The
Lua hostfunc, the binary `PluginHostService` handler, and the `CoreService` RPC
are all thin adapters that delegate to it; none reimplements the filter. Spec
invariant INV-1 ("single command-visibility filter") names this as a hard,
tested constraint.

## Rationale

- ABAC policy (the two-layer check) and the graceful-degradation contract (the
  `incomplete` circuit-breaker) must be identical across every enumeration
  surface; structural colocation is the only durable enforcement mechanism.
- Ownership should follow the registry, which is core-owned (holomush-5nu7) —
  not the plugin-runtime adapter that happened to be the first caller.
- A future contributor adding a new command-list transport finds the canonical
  function next to the registry, not buried in a runtime-specific package.

## Alternatives Considered

- **Keep the filter in `hostfunc`; duplicate it per new adapter.** Zero
  relocation cost, but yields three filter implementations that can diverge,
  forces ABAC changes to be applied in three places, and contradicts INV-1 and
  plugin-runtime-symmetry. Rejected.
- **Extract a helper but leave it in the `hostfunc` package.** Avoids
  duplication but keeps a core concern in the plugin-runtime layer and makes the
  web RPC reach across a package boundary into plugin code. Rejected in favor of
  relocating ownership to `internal/command`.

## Consequences

- Single policy-enforcement point: ABAC changes to command visibility propagate
  to Lua, binary, and web surfaces automatically.
- The three adapters can be tested for behavioral equivalence against the same
  function (the parity test, spec INV-2).
- `internal/command` gains a subpackage depending on the ABAC engine and the
  alias cache — a mild, deliberate coupling increase.
- The holomush-mexs Lua partial-error integration test becomes a regression
  guard for the relocation (no behavior change expected).

## References

- Spec: `docs/superpowers/specs/2026-05-29-recognized-command-chip-design.md` (§ Architecture / Core command-query service; INV-1)
- Epic: holomush-2zjio
- Related: holomush-5nu7 (unified command registry)
