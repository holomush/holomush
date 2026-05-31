<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-6wcf2; do not edit manually; use `/adr update holomush-6wcf2` -->

# ADR: INV-<SCOPE>-<N> canonical invariant naming convention

**Date:** 2026-05-31
**Status:** Accepted
**Decision:** holomush-6wcf2
**Deciders:** Sean Brandt

## Context

HoloMUSH invariants were scattered across 62 spec files with 30+ concurrent naming schemes — bare INV-N (crypto master spec), per-phase families (INV-A-N..INV-F-N, INV-P4-N..INV-P7-N), per-spec families (INV-RB-N, INV-ROPS-N, INV-GW-N, INV-LOAD-N, INV-TS-N, INV-WS-N, INV-PC-N, INV-FS-N, INV-FW-N, INV-SH-N, INV-RA-N), I-prefix families (I-PRIV-N, I-PRES-N). A contributor finding an invariant ID in code had to grep the entire docs/ tree to learn what it meant. Some invariants had prose definitions in 2+ specs (drift risk). New invariants got added without an index update. The lack of a canonical scheme caused namespace collisions: bare INV-1 meant at least three different things across different source files.

## Decision

All HoloMUSH system invariants use a canonical INV-<SCOPE>-<N> naming scheme where <SCOPE> is a short uppercase domain label (INV-CRYPTO, INV-PRIVACY, INV-SCENE, etc.) and <N> is a sequential integer within that scope starting at 1. Existing legacy names (I-PRIV-7, INV-39, INV-P7-15) are recorded as aliases in the registry and renamed to the canonical form across all files (~470 files: 62 specs + 407 Go files).

## Rationale

The domain-driven scoping (vs. spec-driven or registry-assigned) keeps the number of scopes manageable (13 vs. 30+) while ensuring each invariant ID is self-documenting — INV-PRIVACY-3 immediately communicates "privacy domain, invariant 3." The full-rename strategy (vs. alias-only or split) eliminates namespace ambiguity permanently; the pain of 470-file rename is one-time, while the pain of ambiguous names compounds with every new invariant. The sequential numbering within each scope is simple to maintain and avoids renumbering cascades when invariants are superseded.

## Alternatives Considered

1. **Registry-primary IDs (INV-001..INV-NNN) with legacy aliases kept** — zero code/doc churn, but the registry ID is a meaningless opaque number that provides no domain context without a lookup. Rejected: the registry should complement the naming convention, not substitute for it.
2. **Spec-driven scoping (one scope per design spec)** — every design spec that defines invariants is its own scope (INV-CRYPTO-1, INV-PRIV-1, INV-SCENE-P4-1, INV-SCENE-P6-1, etc.). Rejected: scopes proliferate (30+), and scene invariants that cross Phase boundaries would have to be arbitrarily assigned to one spec scope or duplicated.
3. **Split migration (rename small families, alias the rest)** — rename I-PRIV/I-PRES/P4/P5/P6/P7 families (under 20 invariants each), leave the crypto master spec's 60 bare INV-N as-is with registry aliases. Rejected: this creates a two-tier system where some domains follow the convention and others don't, prolonging the ambiguity indefinitely.
4. **Hybrid: scope for new, alias for old** — all new invariants use INV-<SCOPE>-<N>, existing names aliased, no rename migration. Rejected: the 30 existing schemes would persist indefinitely in code comments, confusing new contributors.

## Consequences

- All ~470 files with invariant references must be mechanically renamed (grep-driven pass, no behavior changes).
- The 13-scope taxonomy (INV-CRYPTO, INV-PRIVACY, INV-PRESENCE, INV-SCENE, INV-PLUGIN, INV-EVENTBUS, INV-CLUSTER, INV-ACCESS, INV-SESSION, INV-STORE, INV-TELEMETRY, INV-BRANDING, INV-DOCS) is established as the canonical domain breakdown.
- Future invariants use INV-<SCOPE>-<N> from day one. A new scope is warranted when at least 3 invariants exist that don't fit an existing scope's boundary, or when a new major subsystem ships with its own invariants.
- The holomush-zkmfc bead (Phase 8 INV-P8-N rename) is unblocked: it now has a canonical naming scheme to follow.
