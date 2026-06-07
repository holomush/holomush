<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-8aure; do not edit manually; use `/adr update holomush-8aure` -->

# Three-vocabulary event-type model: bare crypto/registered, qualified wire/verbs, one bridge

**Date:** 2026-06-07
**Status:** Accepted
**Decision:** holomush-8aure
**Deciders:** Sean Brandt

## Context

The same event type string appears in three vocabularies governed by different rules. INV-PLUGIN-32 requires the registered-emit set and crypto.emits to be set-equal (both bare); requests_decryption refs are `<plugin>:<verb>` and splitQualifiedRef recovers the bare verb; but the wire type and verbs[].type must be qualified. emitEntryMatchesWireType (from holomush-50zqs) is the seam where the bare crypto vocabulary meets the qualified wire vocabulary.

## Decision

The registered-emit set and crypto.emits stay BARE (required by INV-PLUGIN-32 and the requests_decryption mechanism); the wire type and verbs[].type are QUALIFIED. emitEntryMatchesWireType is the single sanctioned bridging point between the two vocabulary families.

## Rationale

INV-PLUGIN-32 requires set-equality between the registered-emit set and crypto.emits — changing either requires changing the other plus splitQualifiedRef; disproportionate. emitEntryMatchesWireType already exists as a correct bridge; promoting it to documented architecture is minimal-churn. Confining bare<->qualified translation to one named function (one call site, crypto match) keeps the bridging surface auditable by the crypto-reviewer gate.

## Alternatives Considered

Qualify all three vocabularies uniformly — rejected (breaks INV-PLUGIN-32; would change requests_decryption ref format + splitQualifiedRef; trips EVENT_TYPE_REGISTRY_MISMATCH at load). Normalize at emit time (strip qualifier before store/register) — rejected (web renderer + RenderingPublisher exact-match qualified wire types; would migrate those consumers; storage inconsistent with documented convention).

## Consequences

Positive: INV-PLUGIN-32 + crypto subsystem unaffected by the wire-qualification migration; bridging surface is one auditable function; future plugins keep bare crypto.emits. Negative: authors must internalize qualified-wire/bare-registry distinction; a mis-vocabularied string is a load/emit-time error, not compile-time. Neutral: three-vocabulary model is now documented architecture, not emergent; eventbus.NewType accepts bare tokens, so the loader gate (not NewType) is the enforcement point.
