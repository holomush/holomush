<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Dot-Form as the Sole Pub/Sub Subject Representation; subjectxlate Deleted

**Date:** 2026-05-29
**Status:** Accepted
**Decision:** holomush-21ahd
**Deciders:** HoloMUSH Contributors

## Context

HoloMUSH carried two representations for every pub/sub stream name: the
JetStream-native dot form (`events.<gid>.<domain>.<id>`) and a legacy colon form
(`location:<id>`, `character:<id>`). The `internal/eventbus/subjectxlate/` shim
translated between them at every publish, every history read, and every client
delivery. Its own header marked it transitional — to be removed once host/plugin
code emitted JetStream-native subjects ("F5") — but that removal never shipped
with the JetStream cutover.

The dual representation was an active source of privacy bugs: a classifier that
received the form it did not recognize returned "not private," skipped the I-17
membership gate, and fell through to the ABAC default branch
(`holomush-pkixe`, `holomush-ofpi`). `holomush-s9nu` migrated the **scene** domain
atomically to dot form; this decision extends that to the entire stream namespace
and deletes the shim. It supersedes the `holomush-ec22.3` "subjectxlate endgame"
decision.

## Decision

Adopt the dot form (`events.<gid>.<domain>.<id>`) as the **single canonical**
pub/sub subject representation across all producers, classifiers, gRPC wire
fields, the SvelteKit client, and durable audit. Delete
`internal/eventbus/subjectxlate/` in the same PR as the producer and classifier
migrations.

GameID injection moves from `subjectxlate.Legacy` to a single
`eventbus.Qualify(gid, relativeRef)` helper, called at exactly two boundaries:
the **emit qualifier** (`internal/plugin/event_emitter.go` and the host core emit
path) and the **read-entry qualifier** (`QueryStreamHistory` / `Subscribe`,
before the classifier switch). Producers and the Lua SDK emit **domain-relative
references** (`location.<id>`, not `events.<gid>.location.<id>`) because they do
not hold the gameID; the host qualifies at the boundary.

## Rationale

- Any intermediate state between producer migration and classifier migration
  reproduces the `pkixe`/`ofpi` failure mode — an unrecognized form skips its
  authorization branch. A single canonical form makes that state unrepresentable.
- The translation shim was already marked transitional by the project; a single
  form removes the maintenance surface entirely rather than extending it.
- A single, named injection point (`eventbus.Qualify`) makes "where gameID enters
  the subject" explicit and auditable, versus diffuse `subjectxlate` calls.
- The invariant is enforced mechanically: INV-ROPS-3 (repo-wide CI scan over Go
  and Lua) forbids any surviving colon stream literal, and INV-ROPS-4
  (producer↔subscriber round-trip) proves emit-subject equals subscriber-filter.

## Alternatives Considered

1. **Retain subjectxlate; extend it bidirectionally for all domains.** Lets
   producers migrate independently, but keeps a permanent translation surface and
   leaves the `pkixe`/`ofpi` half-migrated-classifier failure class open
   indefinitely (and a latent fail-open if a `stream:*` permit seed is ever
   added). Rejected.
2. **Dual-format classifiers during a transition window.** Smaller per-PR blast
   radius, but multiplies the classifier surface that must be correct and extends
   the window for the `pkixe` failure class. `holomush-s9nu` showed even a
   zero-width window (emit dot, gate still colon) silently breaks stream access.
   Rejected.
3. **Single-form cutover; delete the shim in the same PR (chosen).** Classifiers
   see exactly one form; the `pkixe`/`ofpi` root cause is eliminated permanently;
   INV-ROPS-3 can enforce eradication as a CI gate. Cost: one large coordinated PR
   across host producers, the Lua SDK and inline Lua, the SvelteKit client,
   classifiers, ABAC seeds, and the test harness.

## Consequences

**Positive:** classifiers parse one form; the `pkixe`/`ofpi` privacy bug classes
are eliminated structurally (an unrecognized name is rejected at handler entry per
INV-ROPS-2); ~300 lines of translation infrastructure removed; a CI gate blocks
re-introduction.

**Negative:** a large coordinated PR; `abac-reviewer` + `code-reviewer` required
before push due to classifier and seed changes.

**Neutral:** the colon form survives only as ABAC policy-DSL type-prefixes inside
`internal/access/` (see [holomush-wia9e](holomush-wia9e-colon-dot-role-split-access-boundary.md));
`holomush-s9nu` is the direct scene-domain predecessor.
