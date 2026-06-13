<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-m4ac3; do not edit manually; use `/adr update holomush-m4ac3` -->

# Gate binary plugin capability injection on manifest declaration

**Date:** 2026-06-13
**Status:** Accepted
**Decision:** holomush-m4ac3
**Deciders:** Sean Brandt, Claude Opus 4.8

## Context

Binary plugin SDK Init (pkg/plugin/sdk.go) unconditionally type-asserts the provider against *Aware interfaces and injects host-capability clients regardless of manifest declarations (the root of the eykuh.3 scenes CI regression). Enforcement model needed for INV-PLUGIN-54.

## Decision

pluginServerAdapter.Init validates declared capabilities BEFORE injecting any host-capability client; injection is reached only for a validated (declared) capability — gate and validate realized as a single pre-injection pass that fails load (CAPABILITY_NOT_DECLARED) on an undeclared non-exempt capability.

## Rationale

- Gating on declaration enforces least-privilege structurally, not just at error-path time — a plugin cannot receive a client it did not declare.\n- Converges binary wiring with eykuh.4's declaration-gated Lua end state (plugin-runtime-symmetry).\n- Closes an escape hatch: validate-only would still inject if the error were ever suppressed.

## Alternatives Considered

**Validate-only** (fail load but still inject by *Aware): rejected — leaves binary structurally divergent from the Lua end state and not least-privilege.\n**Gate + validate** (chosen): injection structurally gated on declaration.

## Consequences

Positive: binary capability wiring is structurally least-privilege; convergent with eykuh.4. Negative: existing injection tests that omit declarations must add DeclaredCapabilities. Neutral: zero overhead on the healthy path.
