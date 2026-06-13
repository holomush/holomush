<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-nk46j; do not edit manually; use `/adr update holomush-nk46j` -->

# Defer Lua capability-declaration enforcement to eykuh.4 (binary-only now)

**Date:** 2026-06-13
**Status:** Accepted
**Decision:** holomush-nk46j
**Deciders:** Sean Brandt, Claude Opus 4.8

## Context

Both runtimes have ungated capability injection in production today. Binary consumption is statically knowable via *Aware interfaces; Lua requires migrating production off the legacy hostfunc shim onto the declaration-gated luabridge — work scoped to holomush-eykuh.4.

## Decision

This spec delivers the binary half only; production Lua wiring is explicitly not altered. INV-PLUGIN-55 is registered pending until eykuh.4 migrates production Lua onto the declaration-gated bridge.

## Rationale

- Binary *Aware interfaces give a complete static picture of consumption; Lua script usage is not knowable from Go.\n- The declaration-gated Lua bridge already exists (luabridge/WithHostCapBridge); the missing work is the production migration = eykuh.4's scope.\n- Doing both here would make this spec contingent on eykuh.4 and unbounded.

## Alternatives Considered

**Attempt partial Lua migration in this spec**: rejected — eykuh.4-sized; would delay a completeable binary improvement.\n**Binary-only, defer Lua** (chosen).

## Consequences

Positive: binary enforcement ships on a clean bounded timeline; INV-PLUGIN-55 pending entry is a durable cross-reference. Negative: runtime asymmetry (and a temporary plugin-runtime-symmetry gap in production) persists until eykuh.4. Neutral: the eykuh.3 call-time interceptor already enforces both runtimes at CALL time; this adds binary LOAD-time.
