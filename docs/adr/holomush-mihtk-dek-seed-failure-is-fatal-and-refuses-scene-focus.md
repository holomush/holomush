<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-mihtk; do not edit manually; use `/adr update holomush-mihtk` -->

# DEK seed failure is FATAL and refuses scene focus

**Date:** 2026-06-09
**Status:** Accepted
**Decision:** holomush-mihtk
**Deciders:** Sean Brandt

## Context

SetSceneFocus establishes a web connection's focus on a scene. The WIP implementation logged a warning on dek.Manager.Add failure but continued to call SetConnectionFocus. With the production sensitive-event crypto go-live (holomush-5rh.8.29), the live DEK participant set is authoritative: an unseeded character receives metadata-only payloads — silently blank poses — for every subsequent sensitive scene event on that connection.

## Decision

A DEK seed failure in SetSceneFocus MUST return codes.Internal and MUST NOT call SetConnectionFocus — the scene focus is refused. The ordering is: participation gate → JoinFocus → Add (fatal) → SetConnectionFocus. This supersedes the WIP non-fatal Warn-log behavior.

## Rationale

- Preserves the invariant that a focused connection's character can decrypt every sensitive event on that scene (INV-CRYPTO-116): focused ⟹ participant.
- dek.Manager.Add is idempotent (manager.go:357 — store returns added=false when the participant is already present), so a client retry after a transient failure is always a safe no-op.
- A non-fatal path silently delivers blank (metadata-only) poses with no signal the user can act on — a worse experience than a retryable error.
- Consistent with the fail-closed posture of every other crypto gate in the eventbus stack.

## Alternatives Considered

**Fatal — return codes.Internal, do not call SetConnectionFocus (CHOSEN).** Yields a clean, testable guarantee (focus iff participant); retry is safe via Add idempotency; the dangling FocusMembership left after a fatal Add (post-JoinFocus) is benign and self-heals on retry.

**Non-fatal Warn-log (the WIP behavior) — rejected.** Focuses a connection whose character cannot decrypt scene events; the user silently receives blank poses with no indication of the problem.

**Metric-only degradation — rejected.** Same silent degradation as warn-log; arguably worse for the user, whose error pathway is invisible so they cannot retry.

## Consequences

**Positive:** SetSceneFocus is provably safe (focus iff participant), testable via a failing-adder injection; no latent 'why are my poses blank' support case.

**Negative:** Users see a focus error on transient DEK-seed failures, so the web client must implement retry; a dangling FocusMembership (without connection focus) is left after a fatal Add following JoinFocus — benign but requires understanding.

**Neutral:** The fatal posture for comms is 'free' by construction (a failed GetOrCreate fails Publish), so both features share the posture with different enforcement points.
