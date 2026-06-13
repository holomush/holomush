<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-wvrtc; do not edit manually; use `/adr update holomush-wvrtc` -->

# Stamp dispatch context host-side; never accept a wire subject

**Date:** 2026-06-13
**Status:** Accepted
**Decision:** holomush-wvrtc
**Deciders:** Sean Brandt

## Context

Plugin-mediated authorization decisions (scope checks, command-registry enumeration) require a host-vouched acting-character subject and attributes. Before this design, command-registry RPCs derived the ABAC subject from wire-supplied `character_id`, enabling subject spoofing identical to the class ADR holomush-qeypl closed for host.Evaluate. The fix had to be symmetric across both binary and Lua runtimes (plugin-runtime-symmetry).

## Decision

The host stamps a `DispatchContext` (host-vouched subject + attributes) onto the delivery context.Context once, before any plugin code runs, via an unexported key in pluginauthz. All downstream paths — binary broker, Lua bufconn, and legacy in-VM hostfuncs — inherit it; neither runtime can set or observe it. Binds INV-PLUGIN-51.

## Rationale

- Closes the same subject-spoofing class as holomush-qeypl with one primitive, one guarantee.
- An unexported context key is a structural unforgeable guarantee, not a bypassable runtime check.
- Absent dispatch context fails closed (deny) across both runtimes identically.
- Serves as the shared input for M3 scope anchoring and M4 command-registry fix, making the sub-spec cohesive.

## Alternatives Considered

- **Wire-supplied character_id as subject (status quo):** simple field read, but any plugin with command-registry can enumerate any character by forging the ID. Rejected.
- **Per-call token auth (binary only):** reuses the EmitEvent token, but cannot extend to Lua hostfuncs — a runtime privilege gradient the symmetry rule forbids. Rejected.
- **Host-stamped DispatchContext (chosen):** structurally unforgeable, symmetric, doubles as the M3/M4 spine.

## Consequences

Positive: spoofing is structurally impossible regardless of plugin type; both runtimes reach the same guarantee; timer/startup emits fail closed rather than leak. Negative: calls without an acting character cannot use scope-gated capabilities; the stamp must be threaded through every host delivery entry point or INV-PLUGIN-51 is silently violated. Neutral: the wire character_id field disposition (keep-but-ignore vs remove) is a bounded proto-compat follow-up.
