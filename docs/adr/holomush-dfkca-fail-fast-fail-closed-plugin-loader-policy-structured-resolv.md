<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-dfkca; do not edit manually; use `/adr update holomush-dfkca` -->

# Fail-fast fail-closed plugin-loader policy with structured ResolveResult

**Date:** 2026-06-11
**Status:** Accepted
**Decision:** holomush-dfkca
**Deciders:** Sean Brandt

## Context

The legacy plugin resolver returned `(order, error)` and, on any unsatisfied `requires`, emitted a WARN and fell back to a global priority sort for the ENTIRE plugin set — masking the breadth (it returned on the first miss) and silently violating load-order on every boot. This is the root of bug holomush-oeb4d / holomush-o262d (three of four phantom requires were hidden behind the first).

## Decision

Replace `(order, error)` with a structured `ResolveResult{Ordered, Unsatisfied[], Cycles[]}`. The loader's default policy treats any non-empty `Unsatisfied` (excluding `optional: true` entries, which are skipped) or any `Cycles` as a FATAL boot error — fail-fast, fail-closed. The structured shape lets a future per-plugin quarantine policy be a policy-layer flip, not a resolver rewrite.

## Rationale

- Fail-closed mirrors the crypto KEK-mandatory-boot posture (INV-CRYPTO-119): unsatisfied plugin dependencies are as load-bearing as missing encryption keys.
- The structured result decouples resolver logic from policy, so per-plugin quarantine (future) needs no resolver changes.
- Collecting ALL unsatisfied entries in one pass surfaces the full breadth, unlike the legacy first-miss-then-stop that hid three of the four phantom requires.

## Alternatives Considered

**Legacy WARN + global priority-sort fallback** — rejected: masks the count (first-miss stop), silently violates load-order, hid the boot bug.
**Pure whole-server fatal on first unsatisfied dep (no structured result)** — rejected: no future quarantine path without a rewrite; one error per run.
**Structured ResolveResult + fail-fast default policy** — CHOSEN.

## Consequences

**Positive:** boot fails loudly + completely on any non-optional unsatisfied dep — no silent degradation; all unsatisfied deps reported in one pass; future quarantine needs no resolver change. **Negative:** a missing provider refuses the whole boot until fixed; the four phantom requires MUST be reclassified before fail-fast lands safely (spec §4, in the foundation). **Neutral:** `optional: true` deps are skipped, preserving an explicit graceful-degrade path.
