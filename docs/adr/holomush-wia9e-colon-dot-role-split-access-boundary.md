<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Colon/Dot Role Split Enforced as an internal/access Package Boundary

**Date:** 2026-05-29
**Status:** Accepted
**Decision:** holomush-wia9e
**Deciders:** HoloMUSH Contributors

## Context

After the stream-name migration ([holomush-21ahd](holomush-21ahd-dot-form-sole-subject-representation.md)),
the string `character:<id>` serves two distinct roles: a pub/sub **stream name**
(now `character.<id>` dot form) and an ABAC policy-DSL **subject/resource
identifier** (stays colon). Both were previously constructed as inline
`"character:" + id` literals scattered across the codebase, making the two roles
structurally conflatable — and making INV-ROPS-3's eradication scan unable to
tell a missed stream producer from a legitimate ABAC subject by text alone.

`internal/access/prefix.go` already exports canonical ABAC builders
(`access.CharacterSubject`, `access.SceneResource`, `access.PluginSubject`, …),
used across `world/`, `command/`, `grpc/`, and `hostfunc/`. A handful of straggler
host sites still inlined the literal.

## Decision

Colon-style identifiers are produced **exclusively** by `internal/access`
builders. No host code outside `internal/access/` may contain an inline
colon-prefixed entity literal: the `character.<id>` stream form and the
`character:<id>` ABAC-subject form are built by **different packages**, so the two
roles are structurally unconflatable. The INV-ROPS-3 CI scan allowlists
`internal/access/` as the sole sanctioned package; any colon entity-prefix
literal outside it is an unambiguous stream-producer bug.

Plugin code (`plugins/core-scenes/`) builds ABAC resources but cannot import
`internal/access` (plugin boundary); its residual colon literals are scan-exempt
via the `Evaluate(` call-site or a `// ABAC resource ref` marker, and are audited
as the only sanctioned residual.

## Rationale

- A package boundary makes the role split enforceable **mechanically**
  (INV-ROPS-3 scan, INV-ROPS-6 unit test) instead of by per-line heuristics.
- The builders are already the established convention; routing the stragglers
  through them completes it rather than inventing a new mechanism.
- It makes a future over-eager sweep that accidentally migrates an ABAC subject
  to dot form a visible compiler error or scan miss — you cannot conflate the
  roles if you must use different packages to construct them.
- The builders panic on empty input, strictly safer than bare concatenation
  (an empty subject would silently bypass access control).

## Alternatives Considered

1. **Convention only — document the split, rely on review.** No code change
   beyond the migration, but INV-ROPS-3 then needs per-line heuristics (marker
   comments, call-site patterns) to classify every colon literal, and any new
   inline literal is ambiguous at review time. Rejected — the round-3/4 review
   experience showed this is whack-a-mole.
2. **Package boundary via the existing `internal/access` builders (chosen).**
   Any inline colon literal outside `internal/access/` is unambiguously a bug;
   the role split is enforced at the construction site, never by runtime string
   sniffing. Cost: a documented plugin escape hatch, and routing seven straggler
   host sites through the builders in the same PR.

## Consequences

**Positive:** INV-ROPS-3 logic is simple (skip `internal/access/`; any other hit
is a bug); INV-ROPS-6 guards against an over-eager sweep silently breaking
character-principal policies; straggler sites gain the builders' empty-input
safety.

**Negative:** plugin ABAC-resource construction needs a scan escape hatch
(`Evaluate(` / `// ABAC resource ref`) that must be audited; seven straggler host
sites are routed through the builders in the migration PR.

**Neutral:** the `stream:` ABAC resource straddles the boundary — the `stream:`
type-prefix is colon (built in `internal/access`) but the embedded stream name is
dot, so `StreamProvider.ResolveResource` parses dot-form names even though it
lives inside `internal/access/`. ABAC seed text embedding a stream name
(`like "location:*"`) is not exempt — those flip to the `has_location` witness in
lockstep (INV-ROPS-8).
