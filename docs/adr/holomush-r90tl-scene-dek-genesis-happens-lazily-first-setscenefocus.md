<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-r90tl; do not edit manually; use `/adr update holomush-r90tl` -->

# Scene DEK genesis happens lazily on first SetSceneFocus

**Date:** 2026-06-09
**Status:** Accepted
**Decision:** holomush-r90tl
**Deciders:** Sean Brandt

## Context

The production scene DEK does not exist until a sensitive event (pose) is emitted (genesis-on-emit). But the first `SetSceneFocus` always precedes the first pose. `SetSceneFocus` seeds the focusing character as a DEK participant via `dek.Manager.Add`, which can only append to an *existing* active DEK — on a never-posed scene `updateParticipants` returns `pgx.ErrNoRows`, and per the fail-closed seed contract (ADR holomush-mihtk) focus is refused, making the live decrypt path unreachable (the holomush-5rh.8.29.10 E2E S3 "pose card appears live" 500). Surfaced by holomush-5rh.8.29.13.

## Decision

The scene DEK is genesised **lazily at the first `SetSceneFocus`** on a scene with no active DEK, seeded with the focusing reader — not eagerly at scene creation/activation. The focus seed path is made genesis-safe (genesis-if-absent, then idempotent add) via a new `dek.Manager.EnsureParticipant` method.

## Rationale

Minimal and localized to the focus path; reuses the existing `GetOrCreate` genesis primitive (which already mints v1 with the supplied initial participants on `ErrNoRows`). Preserves the master crypto design scene asymmetry (`publisher.go:494` — scene contexts seed no one at genesis; readers seeded at `SetSceneFocus`): lazy-at-focus upholds "readers seeded at focus" by fixing only the unstated assumption that the DEK pre-exists. Genesis triggers converge — the scene DEK is created by whichever of first-focus or first-pose happens first, both via idempotent `GetOrCreate`; no participant loss in either ordering.

## Alternatives Considered

**Eager genesis at scene creation/activation** — mint the scene DEK when the scene is created, so focus only ever appends. Rejected: adds a genesis trigger to the scene-creation path (more surface, more code touched), couples the DEK lifecycle to the scene lifecycle, and is unnecessary given `GetOrCreate` already provides idempotent genesis-on-demand. Lazy-at-focus is the smaller, behavior-equivalent change.

## Consequences

First `SetSceneFocus` on a fresh scene both genesises the DEK and seeds the first reader. Concurrent first-focusers (or focus racing the publisher first-pose genesis) converge via `GetOrCreate` unique-violation re-select + `updateParticipants` `SELECT … FOR UPDATE`. The fail-closed-on-seed-failure policy (ADR holomush-mihtk) is unchanged — only the genesis-gap *cause* of failure is removed. Pinned by INV-CRYPTO-121.
