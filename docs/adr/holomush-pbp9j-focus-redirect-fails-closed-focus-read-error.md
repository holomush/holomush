<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-pbp9j; do not edit manually; use `/adr update holomush-pbp9j` -->

# Focus-redirect fails closed on focus-read error

**Date:** 2026-07-06
**Status:** Accepted
**Decision:** holomush-pbp9j
**Deciders:** Sean Brandt

## Context

The focus-routed scene input design (docs/superpowers/specs/2026-07-05-focus-routed-scene-input-design.md §4.5, holomush-g1qcw, PR #4574) shipped a fail-OPEN contract: when the dispatcher's FocusReader errored during dispatch of a redirect-candidate verb (pose/say/ooc/emit), the command was routed to the grid location handler as if the connection had no focus. Scene content is participant-only and encrypted (INV-SCENE-3, sensitivity: always); location content is plaintext (sensitivity: never). A transient focus-store error therefore broadcast a scene-focused player's pose in PLAINTEXT to grid bystanders with no user-facing notice (PR #4574 review finding holomush-x1lwf.5, tracked as holomush-uprtc). Observability for the path (WARN + engine-failure metric + span attribute) was added in holomush-x1lwf.10 but the leak remained.

## Decision

maybeRedirectForFocus's caller (Dispatcher.Dispatch) now fails CLOSED on a focus-read error: dispatch aborts with the coded error FOCUS_READ_FAILED and the player-visible message 'Couldn't check your scene focus, so your message was not sent. Please try again.' The command reaches NO handler. A read error is distinguished from genuine no-focus: nil focus, grid focus, and a vanished connection (CONNECTION_NOT_FOUND) still route to the location handler unchanged. Pinned as INV-SCENE-67; spec §4.5 revised in the same change.

## Rationale

The asymmetry of harm is extreme: a leaked pose is an unrecoverable confidentiality downgrade of encrypted-by-design content, while a failed command is a retry. Fail-closed's UX blast radius (a store blip errors the four ambient verbs for everyone, because the focus read is how scene-vs-grid is determined) shrinks to near zero once holomush-wm0fi serves focus reads from memory — the two changes compose, and the semantics change deliberately lands BEFORE the cache so the cache is built around final semantics.

## Alternatives Considered

(1) Keep fail-open and rely on the new observability (WARN/metric/span) to alert operators — rejected: alerting is detection after the leak, not prevention; the harm is unrecoverable. (2) Fail-safe via last-known cached focus (stale-scene → fail closed, stale-grid → proceed) — rejected: couples the semantics fix to the wm0fi cache design and still leaks when the stale value says grid for a player who just focused a scene; it narrows the window rather than closing it.

## Consequences

Players see an explicit retryable delivery error during focus-store degradation instead of silent misdelivery. The four highest-frequency verbs error during a store blip until wm0fi lands (accepted cost). FocusReader implementations MUST NOT map genuine read errors to absent focus (interface contract updated). INV-SCENE-67 (bound to TestDispatcherFailsClosedOnFocusReadError) prevents a future UX-motivated regression to fail-open. The original §4.5 fail-open rationale ('the command MUST NOT be dropped') is superseded: the command MAY be dropped, but never silently and never misrouted.
