<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Session Liveness Derives From a Decaying Connection Lease

**Date:** 2026-05-30
**Status:** Accepted
**Decision:** holomush-6syxb
**Deciders:** HoloMUSH Contributors

## Context

Sessions stuck at `status='active'` after a core restart or an unclean
disconnect are structurally uncollectable: the reaper only sweeps
`detached`-and-past-TTL rows (`ListExpired`), `active` rows carry
`expires_at = NULL`, and the cooperative `Disconnect` RPC is the only path
that transitions `active → detached`. The root cause is that presence trusts
*stored intent* (the `status` enum) rather than an *observed, self-decaying*
signal — so any time the cooperative cleanup is skipped (SIGKILL on redeploy,
browser-tab close with no `Disconnect`, network drop), `status` freezes at
`active` over a dead transport and the session becomes a permanent
presence ghost. This also produces a two-reaper deadlock: the guest idle
reaper excludes any guest with an `active`/`detached` session, so the orphan
immunizes the guest from cleanup too.

## Decision

Session liveness is determined **solely** by a per-connection
`session_connections.last_seen_at` lease that MUST be actively refreshed. A
connection whose lease has lapsed (`last_seen_at < now − L`) is removed by the
lease sweep without requiring a cooperative `Disconnect`. `active` and
`grid_present` become **derived** quantities recomputed from the live
connection set. No read path may treat `status='active'` as evidence of a live
transport independent of the lease layer (invariant **I-LIVE-5**). The
cooperative `Disconnect` RPC is retained as the graceful-close fast-path; the
lease is the backstop for every non-cooperative case.

## Rationale

- The stored `status` enum is intent, not observation; any approach that trusts
  it directly recreates ghost-presence whenever cooperative cleanup is skipped.
- A decaying lease is *correct-by-construction*: a dead transport simply stops
  being refreshed, the lease lapses, and the connection is swept — surviving a
  core restart (a live gateway re-asserts within one interval) and killing
  ghosts (nothing refreshes a corpse).
- The pattern has direct precedent in the codebase: `plugin_repo.last_seen_at` +
  `SweepInactive` already do exactly this for plugins.
- It breaks the guest-reaper / session-reaper deadlock by construction.

## Alternatives Considered

- **A — boot reconcile (mark all `active` detached on core boot).** Rejected:
  unsafe under the gateway topology — a core restart does not disconnect clients
  whose sockets live in the gateway process, so this would forcibly detach
  genuinely-live users.
- **B — sweep `active` sessions with zero `session_connections` rows.**
  Rejected: subsumed by the lease model, and the existence of a connection row
  is not evidence of liveness (a SIGKILL leaves dangling rows).
- **C — extend the reaper to `active` + no-connection + stale `updated_at`.**
  Rejected: subsumed; `updated_at` is not a liveness signal.
- **D — presence query joins `session_connections` (EXISTS check).** Rejected:
  a SIGKILL leaves connection rows dangling too, so `EXISTS(connection)` is
  satisfied by a corpse. The decaying `last_seen_at` is the correct-by-construction
  form of this direction.
