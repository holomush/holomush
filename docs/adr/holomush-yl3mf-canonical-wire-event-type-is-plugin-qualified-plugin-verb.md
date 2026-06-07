<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-yl3mf; do not edit manually; use `/adr update holomush-yl3mf` -->

# Canonical wire event-type is plugin-qualified <plugin>:<verb>

**Date:** 2026-06-07
**Status:** Accepted
**Decision:** holomush-yl3mf
**Deciders:** Sean Brandt

## Context

HoloMUSH had two coexisting wire event-type conventions — plugin-qualified `<plugin>:<verb>` (core-communication, core-objects, docs, web renderer) and bare `<verb>` (core-scenes). Exact-match consumers (RenderingPublisher verb registry, crypto match sites, audit dispatch, history) break silently when a plugin's wire type does not match what the consumer expects; core-scenes hard-failed EMIT_UNKNOWN_VERB in production (holomush-r0kup), masked only by test seeding. Nothing prevented future drift.

## Decision

The canonical wire event-type identity is plugin-qualified `<owning-plugin>:<verb>`. Every emitted wire type and every `verbs[].type` MUST be exactly `<owning-plugin>:<verb>`; a load-time gate (Manifest.Validate) rejects manifests with bare or foreign-qualified verbs[].type entries.

## Rationale

Qualified is already the documented convention, the web-renderer expectation, and the shape core-communication/core-objects use — convergence, not a pivot. Exact-match consumers need one authoritative form. No users exist and backward-compat is waived, so the migration is clean (no mixed-history). A load-time gate enforces the invariant structurally so new plugins cannot reintroduce bare wire types.

## Alternatives Considered

Bare everywhere — rejected (conflicts with docs/web-renderer/verb-registry; majority would migrate the wrong way). Document-both-and-normalize-every-consumer — rejected (unbounded drift; each new consumer must normalize). Structured fields on core.Event — rejected (YAGNI; proto + core.Event + every-consumer rewrite; string convention entrenched).

## Consequences

Positive: EMIT_UNKNOWN_VERB structurally impossible for plugins passing loader validation; audit/history/snapshot key on one form; drift bounded at load; core-scenes becomes a normal plugin. Negative: core-scenes migration (emit sites, dispatch maps, SQL filter, scene_log rows); verb-name derivation updated; future plugins learn qualify-on-wire/bare-in-registry. Neutral: registered-emit + crypto.emits stay bare by INV-PLUGIN-32 (three-vocabulary model).
