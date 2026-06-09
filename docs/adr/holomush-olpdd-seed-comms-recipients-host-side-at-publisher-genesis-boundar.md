<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-olpdd; do not edit manually; use `/adr update holomush-olpdd` -->

# Seed comms recipients host-side at the publisher genesis boundary

**Date:** 2026-06-09
**Status:** Accepted
**Decision:** holomush-olpdd
**Deciders:** Sean Brandt

## Context

page/whisper/pemit emit a single sensitive event to character.<recipientID>. The recipient is derivable from the subject at publish time, but a personal stream has no 'join focus' hook to seed at. The publisher previously called dek.Manager.GetOrCreate(ctx, ctxID, nil), minting a DEK with an empty participant set, so the AuthGuard denied every subscriber (metadata-only). A first production seeder for comms had to be chosen.

## Decision

The host seeds the subject-derived comms recipient as the initial DEK participant at publisher.GetOrCreate genesis, for character.<id> contexts only (scene/other contexts keep nil). No new PluginHostService RPC, Lua hostfunc, or Go SDK method is introduced.

## Rationale

- publisher.Publish is the single encryption boundary both Lua and binary plugins traverse via internal/plugin/event_emitter.go::Emit, so the gate sits at the common path and plugin-runtime-symmetry (.claude/rules/plugin-runtime-symmetry.md) is satisfied by construction rather than by discipline.
- The recipient is seeded atomically with DEK genesis — there is no window where the context exists with an empty participant set.
- No per-message Add on the hot path: subsequent messages to the same character find the existing DEK with the recipient already seeded.
- Eliminates a new RPC surface and the attendant Go-SDK/Lua-hostfunc parity obligation.

## Alternatives Considered

**Publisher genesis seeding — derive initial=[{CharacterID: recipientID}] from the subject at the GetOrCreate call site (CHOSEN).** Symmetry by construction; atomic with genesis; no new RPC. Costs a context-type branch in the publisher and depends on the §3.4 binding-resolution mechanism so the seeded participant is bound.

**New PluginHostService RPC + Lua hostfunc + Go SDK method (per-plugin seeding) — rejected.** The plugin would own the seeding decision, but this requires three coordinated deliverables, forces runtime-symmetry to be maintained as discipline rather than structurally, and opens a per-emit hot-path surface for accidental double-seed calls.

## Consequences

**Positive:** No new PluginHostService RPC, Lua hostfunc, or SDK method; recipient-seeding cannot diverge between binary and Lua runtimes (enforced structurally at the common emit boundary).

**Negative:** The publisher gains a context-type branch (character vs. others) that future context types must be consciously evaluated against; the manager's GetOrCreate gains the §3.4 binding-resolution responsibility to keep the seeded participant bound.

**Neutral:** scene.<id> and other contexts keep nil initial, preserving existing GetOrCreate semantics; scene readers are seeded separately at SetSceneFocus.
