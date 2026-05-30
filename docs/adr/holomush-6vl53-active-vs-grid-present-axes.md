<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# `active` and `grid_present` Are Distinct Domain Axes; Roster Uses `grid_present`

**Date:** 2026-05-30
**Status:** Accepted
**Decision:** holomush-6vl53
**Deciders:** HoloMUSH Contributors

## Context

The existing presence model collapses two distinct questions into one field:
"does the session have any live transport?" and "is the character visible on the
grid (a terminal/telnet connection)?". `ListActiveByLocation`
(`internal/store/session_store.go:620`) filters on `status='active'` only, so a
character connected via only a non-grid transport would appear in the location
roster. These axes have different consumers and different semantics.

## Decision

`session.active` means the session has ≥1 live connection of **any** transport
type; `session.grid_present` means the session has ≥1 live connection of
`client_type ∈ {terminal, telnet}`. The location presence roster MUST be
filtered to `grid_present` characters, not raw `active` sessions (invariants
**I-LIVE-3**, **I-PRES-1**). Both flags are recomputed from the live connection
set on every connection-set change.

## Rationale

- The location roster represents "who can be seen and interacted with on the
  grid" — a terminal/telnet semantic, not generic connectivity.
- Separating the axes is forward-compatible with future non-grid transports
  (API, webhook callbacks) that create `active` sessions without conferring grid
  presence.
- `grid_present` is already a stored `BOOLEAN` column on `sessions`
  (`migrations/000001_baseline.up.sql:209`); the split reuses existing schema
  rather than introducing new tables, and keeps presence reads cheap (flag-based,
  no per-read join into `session_connections`).

## Alternatives Considered

- **Collapse `active` and `grid_present`** (treat `status='active'` as implying
  grid presence). Rejected: not forward-compatible with non-grid transports;
  incorrectly surfaces any-transport connections on the location roster; and
  conflates two orthogonal concerns, making a later decoupling a breaking change.

## Consequences

Any code that currently equates `active` with grid-visible must move to
`grid_present`. Two boolean flags must be kept in sync on every connection-set
change (owned by the lease/connection recompute layer).
