<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-toh7a; do not edit manually; use `/adr update holomush-toh7a` -->

# Make binary plugin Init unconditional to close the degenerate-manifest escape

**Date:** 2026-06-13
**Status:** Accepted
**Decision:** holomush-toh7a
**Deciders:** Sean Brandt, Claude Opus 4.8

## Context

The host loader gates Init on manifestNeedsInit (requires/storage/config/emits). With capability validation in the SDK Init, a binary plugin implementing a *Aware interface but declaring nothing would skip Init and escape INV-PLUGIN-54 load-time enforcement. Discovered during plan grounding.

## Decision

Replace the manifestNeedsInit gate with needsInit := true for binary plugins; all binary plugins are Init'd so the SDK capability validation always runs.

## Rationale

- A *Aware-implementing plugin with no other manifest fields would otherwise satisfy the skip conditions — a silent fail-open.\n- Unconditional Init is the simplest predicate (no edge cases).\n- The extra gRPC round-trip for a trivial plugin is negligible.

## Alternatives Considered

**Keep the gate + add a capability-check exemption**: rejected — a two-predicate gate is harder to reason about and still risks escape.\n**Unconditional Init** (chosen): definitively closes the gap; manifestNeedsInit + its tests can be deleted.

## Consequences

Positive: INV-PLUGIN-54 genuinely airtight; less code. Negative: empty-manifest binary plugins incur one extra round-trip; skip-Init tests inverted. Neutral: recorded as a refinement of spec §3.2.
