<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-1psri; do not edit manually; use `/adr update holomush-1psri` -->

# Two single-clause capability-enforcement invariants (INV-PLUGIN-54 / 55)

**Date:** 2026-06-13
**Status:** Accepted
**Decision:** holomush-1psri
**Deciders:** Sean Brandt, Claude Opus 4.8

## Context

Binary capability enforcement is provable now; Lua enforcement is unverified until holomush-eykuh.4 migrates production Lua off the legacy hostfunc shim. The invariant registry prohibits a pending entry carrying asserted_by, and warns against a bound multi-clause entry whose test proves only one clause (partial-binding hazard).

## Decision

Register INV-PLUGIN-54 (binary, binding: bound, with asserted_by) and INV-PLUGIN-55 (Lua, binding: pending, no asserted_by) as two separate entries in docs/architecture/invariants.yaml.

## Rationale

- No registry-honest way to represent 'binary proven, Lua not yet' with one entry (partial-binding hazard + pending-with-asserted_by is rejected by TestProvenanceGuard).\n- Two entries let the binary guarantee bind immediately and give eykuh.4 a named target (55 pending → bound).

## Alternatives Considered

**Single multi-clause invariant** (binary+Lua together): rejected — cannot bind until both proven; would delay a provable guarantee or fabricate provenance.\n**Two single-clause entries** (chosen).

## Consequences

Positive: 54 bound now with genuine provenance; 55 is a durable cross-reference eykuh.4 cannot forget; meta-tests pass. Negative: parity story split across two IDs. Neutral: 55 stays pending until eykuh.4 (not a CI failure).
