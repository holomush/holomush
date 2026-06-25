<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-bbwe7; do not edit manually; use `/adr update holomush-bbwe7` -->

# Scope rendering-seam invariants (SEAM-*) as web-local vitest checks, not central-registry entries

**Date:** 2026-06-26
**Status:** Accepted
**Decision:** holomush-bbwe7
**Deciders:** Sean Brandt

## Context

The shared rendering seam needs anti-drift guarantees: phrasing centralization (SEAM-1), `--mush-*` token discipline (SEAM-2), XSS-safe escaping via `linkUrls` (SEAM-3), and Go↔TS golden parity (SEAM-4). HoloMUSH has a central invariant registry (`docs/architecture/invariants.yaml`) scoped to durable cross-system guarantees (ABAC fail-closed, crypto no-plaintext, plugin runtime symmetry). The recognized-command-chip design already established a registry-exemption precedent (decision `.14.27`) for web-layer invariants enforced entirely by vitest (its INV-4/6/7).

## Decision

Define SEAM-1..4 as web-local, vitest-enforced invariants living in `web/src/lib/comm/*.test.ts`, and do NOT enter them in `docs/architecture/invariants.yaml`.

## Rationale

- The central registry is for cross-system guarantees; UI phrasing and color-token rules are web-scoped.
- Enforcement is equally mechanical (a failing vitest is a failing build) — the distinction is the test runner and registry scope, not bindingness.
- Follows the recognized-command-chip `.14.27` precedent, keeping a consistent class rather than inventing a third pattern.

## Alternatives Considered

- **Register SEAM-1..4 centrally (rejected):** discoverable via `bd` / `inv-render`, but inflates a cross-system registry with wrong-tier UI rules.
- **Web-local vitest invariants (chosen):** registry stays focused; invariants co-located with the tests that enforce them.

## Consequences

- Positive: the central registry stays scoped to cross-system architectural guarantees; SEAM invariants sit next to their enforcement.
- Negative: not visible in `bd list --type decision` or `inv-render` output — a contributor reading only the registry misses these web-layer guarantees.
- Neutral: matches the existing recognized-command-chip web-invariant class.
