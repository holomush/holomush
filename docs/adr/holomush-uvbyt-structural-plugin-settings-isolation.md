<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Structural Cross-Plugin Isolation via Authenticated Caller Binding

**Status:** Accepted
**Decision:** holomush-uvbyt
**Design bead:** holomush-iokti
**Date:** 2026-05-30

## Context

The `GetSetting`/`SetSetting` host RPCs (`holomush-74ib4`) give plugins access to
the owner-partitioned settings substrate. The owner dimension selects which
plugin's partition a call reads or writes. If the owner were a caller-supplied
field, any plugin could name another plugin's partition — a cross-plugin
exfiltration and corruption surface. The owner must be an **enforceable trust
boundary**, not a naming convention.

## Decision

The owner is **never on the wire**. The host RPC server resolves it from
`pluginHostServiceServer.pluginName`, which is stamped at server construction
(`internal/plugin/goplugin/host_service.go:28`) from the authenticated calling
plugin's identity — there is one server instance per loaded plugin. Every call is
served against `base.Owner(s.pluginName)`. There is no RPC parameter through which
a plugin can express another owner.

This complements — does not replace — ABAC authorization (`holomush-iokti` INV-7),
which governs *principal*-scope access (may this subject read/write its own vs.
another's preferences). Structural isolation governs *plugin*-scope access (which
plugin partition is addressable at all).

## Rationale

- Isolation-by-construction is strictly stronger than isolation-by-check: there
  is no code path through which a plugin can supply an owner name, so the
  guarantee cannot be bypassed by a forgotten authz predicate or a future
  refactor that adds a field.
- Consistent with the plugin-boundary posture (`holomush-z1e7`): the host enforces
  structural constraints, not soft conventions.
- Eliminates an entire confused-deputy class without a per-call predicate.

## Alternatives Considered

- **Caller-supplied owner parameter on the wire.** Rejected: any plugin can forge
  any owner; isolation becomes an unenforced convention.
- **Runtime check comparing a caller-supplied owner to the authenticated plugin
  name.** Rejected: still a per-call predicate that a bug or refactor can bypass;
  the enforcement is not evident from the type. A wire field that must always
  equal the caller's identity is better removed entirely.

## Consequences

- **Positive:** cross-plugin partition reads/writes are structurally impossible;
  the guarantee survives future RPC field additions because owner is not a field.
- **Negative:** one `pluginHostServiceServer` instance per loaded plugin (not
  shared) — minor memory overhead, already the established pattern for the
  per-plugin emit-token mechanism.
- **Neutral:** layered with ABAC — structural isolation bounds *which plugin
  partition*; ABAC bounds *which principal's* data within it.

## References

- Spec: `docs/superpowers/specs/2026-05-29-scenes-phase-8-board-content-warnings-design.md` §3.2, INV-11
- Grounding: `internal/plugin/goplugin/host_service.go:28,31` (`pluginName` stamped at construction)
- Related: `holomush-74ib4` (owner-partitioned settings substrate), `holomush-z1e7` (strict plugin boundary)
- Design bead: `holomush-iokti`
