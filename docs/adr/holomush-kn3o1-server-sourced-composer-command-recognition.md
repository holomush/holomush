<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Server-Source Web Composer Command Recognition

**Date:** 2026-05-29
**Status:** Accepted
**Decision:** holomush-kn3o1
**Deciders:** HoloMUSH Contributors

## Context

The web composer's speech-mode chip was driven by a hardcoded prefix list at
`web/src/lib/components/terminal/CommandInput.svelte:40-46`, recognizing only
`"`/`say`→ say, `:`/`pose`→ pose, `ooc`→ ooc. That list duplicated
server-authoritative data: the sigils are real aliases declared in
`plugins/core-communication/plugin.yaml` and seeded into the alias store
(`internal/command/alias.go::AliasCache`).

This is the exact drift class the gateway-boundary invariant exists to prevent —
the frontend re-encoding state the server owns. The recognized-command chip
(holomush-2zjio) would otherwise require the frontend to acquire and maintain a
second, larger duplicate (the full command set), compounding the drift.

## Decision

The hardcoded prefix matcher is deleted. All command and speech-mode recognition
in the composer derives from a server-provided command-name set and alias map
fetched via `WebService.WebListCommands` (proxied from
`CoreService.ListAvailableCommands` by the gateway), cached per session. Spec
invariants INV-3 (gateway obtains the list only via RPC) and INV-4 (composer
recognition is server-sourced; the hardcoded matcher MUST be removed) codify this
as a hard constraint on all future composer recognition logic.

## Rationale

- The gateway-boundary invariant forbids the gateway or frontend from computing
  state the server owns; the hardcoded matcher directly violated it.
- Sigil-to-command mapping is server-authoritative; duplicating it in the
  frontend creates a drift that has already occurred and recurs as plugins
  evolve.
- INV-4 makes the removal permanent and discoverable: a contributor cannot
  re-introduce a local matcher without violating a named, tested invariant.
- The server-sourced list is ABAC-filtered, so the composer will not chip
  commands the character cannot execute (INV-5) — a property a frontend list
  cannot guarantee.

## Alternatives Considered

- **Keep/extend the hardcoded matcher for non-speech commands.** Zero-latency,
  no new RPC, but forces frontend edits on every command/alias change, creates a
  second source of truth that silently desyncs, and violates gateway-boundary.
  Rejected.
- **Validity-dot indicator (design option C): keep speech chips hardcoded, add a
  subtler server-sourced dot only for other commands.** Still needs a new data
  source, doesn't fix the existing drift, and leaves two parallel recognition
  paths. Rejected in favor of unifying both chip kinds on one server source.

## Consequences

- Command additions and alias changes never require a frontend change to be
  recognized in the composer.
- Recognition is unavailable until the first per-session fetch resolves
  (mitigated by a session-start fetch + cache; absence degrades to no chip,
  INV-6).
- Adds one RPC to the `WebService` gateway-proxy surface.
- Speech-mode chips (say/pose/ooc) continue to render identically (INV-7), now
  derived from the same server data rather than a parallel hardcoded list.

## References

- Spec: `docs/superpowers/specs/2026-05-29-recognized-command-chip-design.md` (§ Architecture / Adapter 3 — Web RPC; § Web composer; INV-3, INV-4)
- Epic: holomush-2zjio
- Related: gateway-boundary invariant (`.claude/rules/gateway-boundary.md`)
