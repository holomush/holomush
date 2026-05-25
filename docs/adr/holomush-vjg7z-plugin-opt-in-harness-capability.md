<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Plugin Layer as an Opt-In Harness Capability in Full-Stack Integration

**Date:** 2026-05-25
**Status:** Accepted
**Decision:** holomush-vjg7z
**Deciders:** HoloMUSH Contributors
**Related:** holomush-1eps2, holomush-f5t07

## Context

The `integrationtest` harness (canonical per `CLAUDE.md`) starts a real
in-process `CoreServer` with Postgres and embedded NATS, but loads **zero
plugins**. Production loads 10 in-tree plugins (6 Lua, 2 binary, 2 setting) via
`plugin.Manager.LoadAll`. Tests exercising scenes, channels, or objects against
a plugin-free harness exercise a hollow core — the behaviors are plugin-provided.

## Decision

Full-stack integration includes the plugin layer, but plugin-loading is an
**opt-in harness capability** (`integrationtest.WithInTreePlugins()` driving
`plugin.Manager.LoadAll`), not mandatory on every test. A single **whole-system
suite** loads all in-tree plugins (mirroring production) as the top Go-fidelity
tier; targeted tests (privacy, presence) stay lean. The capability + suite are
implemented in a follow-up bead (discovered-from holomush-1eps2); this decision
defines the intent and tier meaning.

## Alternatives Considered

### A — Opt-in capability + dedicated whole-system suite (chosen)

Targeted tests stay lean; one whole-system suite gives manifest-DAG, load-order,
and cross-plugin-ABAC coverage. Affordable: 6 of 8 functional plugins are
in-process Lua, so only the 2 binary plugins need an availability gate.

### B — Mandatory plugin loading for all full-stack tests

Maximum default fidelity, but burdens every targeted test with binary-plugin
build artifacts, subprocess startup latency, and DAG-load failure surface
unrelated to what it tests.

### C — No plugin loading (status quo)

No change, but plugin-provided behaviors (scenes, channels, communication) stay
untested at the integration tier; cross-plugin-ABAC / manifest-DAG regressions
surface only in E2E or production.

## Rationale

- Plugins are not optional in production; a zero-plugin full-stack test does not
  test the production topology.
- Opt-in avoids imposing binary-plugin cost on tests that don't need it.
- One whole-system suite provides the high-fidelity coverage without universal
  cost; the Lua-vs-binary asymmetry makes it cheap.

## Consequences

**Positive:** manifest-DAG / load-order / cross-plugin-ABAC regressions become
catchable at `task test:int`; existing targeted tests are unaffected;
`WithInTreePlugins()` self-documents intent.

**Negative:** the capability + suite are deferred to a follow-up bead — until it
lands, the plugin-loading fidelity gap remains; the binary-plugin availability
gate fails at runtime (not compile time) if artifacts are missing.

**Neutral:** INV-5 (whole-system suite uses `Manager.LoadAll`) is declared here,
enforced in the follow-up.

## Implementation

Deferred to a follow-up bead (discovered-from holomush-1eps2). This design fixes
only the tier *meaning*; see spec §6/§7.

## References

- Spec: `docs/superpowers/specs/2026-05-25-test-tier-taxonomy-design.md` §6, §7
- Sibling harness-fidelity gap: holomush-f5t07 (ABAC bypass)
