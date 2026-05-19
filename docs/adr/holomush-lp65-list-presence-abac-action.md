<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Introduce list_presence ABAC Action with Default-Deny and Same-Location Seed

**Date:** 2026-05-19
**Status:** Accepted
**Decision:** holomush-lp65
**Deciders:** HoloMUSH Contributors

## Context

The new `CoreService.ListFocusPresence` RPC (ADR `holomush-da2q`) surfaces who is present at a
location. This is sensitive enough to require an explicit access-control gate. No existing ABAC
action covers this query, and the engine defaults to deny (`internal/access/policy/engine.go`).
The decision is whether to re-use a coarser existing action (e.g., `list_characters`), gate
access purely in handler code, or introduce a purpose-specific ABAC action.

The codebase already establishes a precedent in `internal/access/policy/seed.go`: every
game-state read RPC is gated by a dedicated ABAC action, with same-location characters granted
access via a `*_same_location` seed and admin override covered by the global super-rule
`permit(principal is character, action, resource) when { "admin" in principal.character.roles };`.

## Decision

A new ABAC action `list_presence` on `resource is location` gates the snapshot RPC. Default-deny.
Same-location characters are permitted via a seed policy modelled exactly on
`list_characters_same_location`:

```text
permit(principal is character,
       action in ["list_presence"],
       resource is location)
  when { resource.location.id == principal.character.location };
```

Admin remote-presence access is covered by the existing super-rule (no special-casing required).

## Rationale

- **Consistency with the established pattern.** Every game-state read RPC in the system is gated
  by an ABAC action expressed in `seed.go` (see `list_characters_same_location`,
  `player_location_read`, scene policies, etc.). Adding a new RPC without a corresponding action
  would break the invariant that all access decisions are policy-configurable and auditable.
- **Distinct semantic from `list_characters`.** `list_characters` governs character-row access
  (the table); `list_presence` governs session-state access (who's actively here). They have
  different lifecycle and audit profiles; conflating them prevents independent revocation.
- **Admin remote-presence is free.** The existing super-rule's unconstrained `action` parameter
  covers `list_presence` without modification. No special handler code needed for admin access.
- **DSL attribute path matches existing convention.** `principal.character.location` is the
  established attribute name (`internal/access/policy/seed.go` line 99 and elsewhere), not
  `principal.character.location_id`. The matching `resource.location.id` is the canonical
  resource-side attribute.

## Alternatives Considered

- **Reuse `list_characters` action.** Wrong semantic — `list_characters` governs character-row
  access; `list_presence` governs session-state. Conflating them prevents independent revocation
  and creates misleading audit logs (an audit row "subject denied list_characters" wouldn't
  distinguish a denied table read from a denied presence query).
- **Gate access in handler code only (no ABAC action).** Breaks the invariant that all
  game-state access gates live in the ABAC engine; non-auditable; inconsistent with the rest
  of the codebase. Even simple checks like "same location" go through ABAC.

## Consequences

**Positive**

- Presence access is auditable via the `Decision`'s policy-match tracking.
- Same-location default permits normal play; admin override is already seeded.
- Future per-character visibility (invisible/hidden roles) can layer onto `list_presence`
  without touching `list_characters`.

**Negative**

- One additional seed entry and one smoke-test row to maintain.

**Neutral**

- Admin remote-query plumbing (`target_location_id` field on the request, or an
  `AdminListPresence` RPC) is deferred to a follow-up bead; the ABAC policy is already
  correct for that future surface.

## References

- Spec: `docs/superpowers/specs/2026-05-19-presence-snapshot-design.md` (§2 D-5 ABAC seed,
  §4.4 implementation, §7 I-PRES-4)
- Pattern reference: `internal/access/policy/seed.go` `list_characters_same_location`
- Admin super-rule: `internal/access/policy/seed.go` `seed:admin-full-access`
- Related ADR: `holomush-da2q` (snapshot RPC as source of truth)
- Related ADR: `holomush-o46k` (snapshot exempt from I-PRIV-1 floor)
- Parent bead: `holomush-5b2j`
