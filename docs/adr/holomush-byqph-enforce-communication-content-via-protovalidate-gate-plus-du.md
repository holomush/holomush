<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-byqph; do not edit manually; use `/adr update holomush-byqph` -->

# Enforce communication content via a protovalidate gate plus dual-runtime builders

**Date:** 2026-07-04
**Status:** Accepted
**Decision:** holomush-byqph
**Deciders:** sean

## Context

Plugin payloads travel as untrusted JSON (`internal/plugin/event_emitter.go::Emit` only checks `json.Valid`, there is no proto-binary path), and both Lua and binary plugins must be gated identically (plugin-runtime-symmetry) while still having an ergonomic way to produce conforming payloads.

## Decision

Enforce the contract with a `ContentValidationPublisher` chain link that `protojson.Unmarshal`s and `protovalidate`s `category: communication` payloads at emit, paired with Go (`pkg/plugin/comm`) and Lua (`holo.comm.*`) builder SDKs that share one sigil grammar. The gate is the hard backstop; the builders are the ergonomic default.

## Rationale

- A single chain chokepoint satisfies plugin-runtime-symmetry without runtime-specific gates — both runtimes publish through the same chain.
- Builders alone leave conformance optional; the gate makes it structurally guaranteed against a future or buggy plugin.
- It extends the `protovalidate`-at-emit pattern `RenderingPublisher` already established.

## Alternatives Considered

- **Gate + dual builders (A+C, chosen):** single chokepoint enforces both runtimes identically by construction; builders make conformance the ergonomic default. Cost: an untrusted-JSON decode step (which `RenderingPublisher`'s host-constructed-proto validation does not need) and an exact chain position (inner-of-`RenderingPublisher`, outer-of-encryption).
- **Convention-only builders, no gate (rejected):** simpler, but conformance becomes only a suggestion — insufficient against a future or buggy plugin.
- **Host-constructed payload from a semantic intent (B, rejected):** over-centralizes plugin-specific pose/ooc grammar into the host and changes the emit model for content events.

## Consequences

- Positive: non-conforming communication payloads fail closed (`EMIT_CONTENT_INVALID`) regardless of runtime or author diligence; collapses duplicated sigil-parsing into one shared grammar.
- Negative: adds an untrusted-JSON decode step that must land at an exact chain position — misordering silently validates the wrong data or loses the `Category` stamp; the gate ships built-but-not-live in Slice 1, so INV-COMM-1 stays `binding: pending` until Slice 2.
- Neutral: Lua's access to the shared grammar (hostfunc vs parity-tested pure-Lua copy) is left open to plan time.
