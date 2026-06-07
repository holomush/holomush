<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-1gwns; do not edit manually; use `/adr update holomush-1gwns` -->

# Qualify core-scenes event types end-to-end (no backward-compat transformation layer)

**Date:** 2026-06-07
**Status:** Accepted
**Decision:** holomush-1gwns
**Deciders:** Sean Brandt

## Context

Given canonical-qualified-wire, core-scenes internal consumers (replayEventKinds, snapshotEventKinds, audit dispatch, the SQL type filter in ReadSceneLogForSnapshot, verb-name derivation) all key on the stored/wire type. Two shapes: (a) qualify at emit only + a transformation layer so internals keep seeing bare; (b) qualify end-to-end, re-keying internals in the same change. HoloMUSH has no users and waives backward-compat.

## Decision

core-scenes qualifies END-TO-END: emit sites stamp `core-scenes:<verb>`, scene_log.type stores the qualified form, and every internal consumer keyed on the stored/wire type is re-keyed to the qualified form in the same atomic change. No transformation layer is introduced.

## Rationale

No users exist and backward-compat is explicitly waived (2026-06-06), making the clean migration free of the usual constraint. A transformation layer trades a one-time migration for permanent maintenance complexity — the wrong trade. Atomicity (emit + internal consumers in one change) is achievable: core-scenes is self-contained with a well-enumerated consumer list. End-to-end qualification means internal and wire consumers see the same string, eliminating which-form-does-this-consumer-expect confusion.

## Alternatives Considered

Qualify-at-wire-only + normalize at each internal consumer — rejected (permanent transformation layer; every new internal consumer must normalize; storage/code form mismatch). Phased migration with backward-compat period — rejected (no users; mixed-history + reader-side legacy normalization add permanent complexity for a temporary non-problem).

## Consequences

Positive: scene_log.type, snapshot, audit, SQL filter all key on the same qualified form as wire+verbs — no impedance mismatch; migration is a mechanical re-key over a known site set; core-scenes becomes indistinguishable from any other plugin. Negative: all internal re-keys must land in the same PR as emit qualification (else a window where stored bare types fail to dispatch); verb-name derivation must strip the full core-scenes:scene_ prefix. Neutral: registered-emit + crypto.emits for core-scenes stay bare; existing bare scene_log rows are a non-issue (no users).
