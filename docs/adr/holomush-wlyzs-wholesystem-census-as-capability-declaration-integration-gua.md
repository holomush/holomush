<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-wlyzs; do not edit manually; use `/adr update holomush-wlyzs` -->

# Wholesystem census as the capability-declaration integration guard

**Date:** 2026-06-13
**Status:** Accepted
**Decision:** holomush-wlyzs
**Deciders:** Sean Brandt, Claude Opus 4.8

## Context

With load-time SDK validation, a misdeclared binary plugin fails Manager.LoadAll. The in-tree enforcement approach needed deciding: reuse the existing wholesystem census (loads all in-tree plugins via the real path) or add a separate static meta-test.

## Decision

The existing test/integration/wholesystem census is the integration enforcement point; no separate capability meta-test is introduced. A misdeclared plugin fails load → fails the census automatically.

## Rationale

- With Init validation in place, the census becomes an enforcer for free — enforcement is the runtime itself, not a test approximation.\n- A separately-maintained token map in test code would drift from the runtime registry (two sources of truth).\n- Reflection/AST alternatives are blocked or advisory.

## Alternatives Considered

**AST/source-scan meta-test**: rejected — CI-only, advisory, hand-maintained map, redundant once census is the guard.\n**Reflection meta-test**: rejected/blocked — binary plugins are package main, providers not importable.\n**Wholesystem census** (chosen).

## Consequences

Positive: no new infra; future in-tree plugins auto-covered; no test false-negative possible (runtime is the enforcer). Negative: runs in task test:int (Docker), not the fast pr-prep lane; covers only in-tree plugins (acceptable — no external ecosystem). Neutral: pkg/plugin unit tests still cover the validation logic itself.
