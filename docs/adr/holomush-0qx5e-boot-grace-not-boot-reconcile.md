<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Boot-Grace Window Replaces Boot-Reconcile for Core-Restart Safety

**Date:** 2026-05-30
**Status:** Accepted
**Decision:** holomush-0qx5e
**Deciders:** HoloMUSH Contributors

## Context

The naive fix for orphaned `active` sessions is to mark all `active` sessions
`detached` when core boots — the historical MUSH "everyone goes linkdead on
reboot" pattern. HoloMUSH's process topology changes the semantics: core and
the gateway are **separate processes**, and a core restart does not disconnect
clients whose sockets live in the gateway (`cmd/holomush/gateway.go` hosts both
the web and telnet servers). Single-core-process is a documented invariant
(`docs/plans/2026-04-07-cursor-lock-finding-1-closure.md`), but that does not
imply single *transport* process.

## Decision

On core process start the lease sweep is suppressed for a **boot-grace window**
of at least `L + margin`; no `active` sessions are forcibly detached at boot.
Live gateway connections re-assert their leases within one refresh interval,
after which the sweep resumes normally (invariant **I-LIVE-4**). This is
orthogonal to the `AddConnection` lease stamp (`last_seen_at = now`), which
independently protects the legitimate zero-connection window of a
freshly-created session.

## Rationale

- A core restart is invisible to clients attached through the gateway; treating
  it as a mass disconnect would force a detach/reattach "reconnect storm" on
  every deploy.
- The grace window gives live gateways time to re-assert their leases before the
  sweep could incorrectly reap them — no special gateway↔core boot coordination
  is required.
- It is a time-bounded suppression, not a permanent exemption: pre-restart
  ghosts are still swept once the window elapses.

## Alternatives Considered

- **Boot reconcile (mark all `active` sessions `detached` on core boot).**
  Rejected: unsafe under the separate-gateway topology — it would forcibly
  detach users who are actively connected through the gateway. Immediate
  clearing of pre-restart ghosts does not justify dropping live sessions.

## Consequences

Pre-restart ghost sessions persist for up to `L` + the grace window (≈60s
default) after a core restart before being swept. The grace parameter MUST
exceed the gateway's refresh interval; misconfiguration could cause spurious
detaches.
