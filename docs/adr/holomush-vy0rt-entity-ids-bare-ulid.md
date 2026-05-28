<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Entity Identifiers Are Bare ULIDs; Type Discrimination Lives in the Subject Domain and ABAC Prefix

**Date:** 2026-05-28
**Status:** Accepted
**Decision:** holomush-vy0rt
**Deciders:** HoloMUSH Contributors

## Context

Scenes were the only entity in HoloMUSH whose identifier carried an embedded
type-tag prefix: `newSceneID()` (`plugins/core-scenes/service.go:1113`) minted
`"scene-" + ulid.String()`. Every other entity — locations, characters, exits,
objects, bindings (`internal/world/*.go`) — mints a bare ULID via `idgen.New()`,
and even a sibling table inside the same plugin (scene publish attempts,
`plugins/core-scenes/publish_store.go:48`) uses bare ULIDs. The prefix was
introduced in PR #200 with no comment, ADR, or spec; it was undocumented.

The type tag is also redundant: an event's type is already discriminated by the
subject domain segment (`events.<game>.scene.<id>.ic`) and, for authorization,
by the ABAC resource-type prefix (`scene:<id>`, a separate colon-style
mechanism at a different layer). Nothing in the tree parses `scene-` to recover
a type.

Because the stored id was prefixed but every host parse boundary called
`ulid.Parse` on the raw subject token, three boundaries rejected real scene
identifiers:

- `streamToFocusKey` / `extractSceneID` (`internal/grpc/stream_access.go:137`) —
  `ulid.Parse("scene-<ULID>")` → `ErrDataSize` → `INVALID_ARGUMENT`.
- `streamScopeFloor` (`internal/grpc/scope_floor.go:44`) — compares the bare
  `FocusMembership.TargetID.String()` against the prefixed extracted id, so the
  temporal floor never matches.
- `protoToFocusKey` (`internal/plugin/goplugin/host_service.go:469`) —
  `ulid.Parse(TargetID)` rejects the prefixed join key.

This broke `scene log` / `scene log export`, scene join/subscription, and the
privacy temporal floor for every command-created scene
(`holomush-y5inx`). A masking `TrimPrefix(…, "scene-")` in the integration test
harness (`internal/testsupport/integrationtest/session.go:473`) — the only
strip anywhere in the tree — hid the regression by keeping all scene tests in
bare-id space. This is the same parse boundary the prior atomic subject
migration (`holomush-s9nu`) touched without surfacing the prefix mismatch.

## Decision

Entity identifiers in HoloMUSH are bare ULIDs. Type discrimination is carried
exclusively by the event-subject domain segment
(`events.<game>.<domain>.<id>`) and the ABAC resource-type prefix
(`<type>:<id>`) — never by a tag embedded in the identifier string itself.

`newSceneID()` mints a bare ULID identical in form to every other entity. No
production code path may strip a type-tag prefix from an identifier or subject
token (INV-Y5INX-5). The bare form makes the stored id, the subject token,
`FocusKey.TargetID`, and `FocusMembership.TargetID` byte-identical end-to-end,
so the three host parse boundaries work without any normalization.

The `"ope-"` ops-event prefix (`plugins/core-scenes/ops_events.go:111`) is the
same anti-pattern but is a pure internal primary key never parsed at a focus
boundary; it is not a live bug and is flagged P3 for future normalization under
this same convention.

## Rationale

**A type tag inside the identifier forces every `ulid.Parse` boundary to strip
it first.** Forgetting the strip fails closed (`INVALID_ARGUMENT`) or, worse,
fails open silently (a floor comparison that never matches → unfiltered
history). The prefix recurred independently at three host boundaries, which
demonstrates it is a systematic bug class, not a one-off oversight.

**Bare identifiers make the invariant self-enforcing.** When the string form
equals the parsed form, every parsing boundary becomes a no-op pass-through.
Any future entity that mistakenly embeds a prefix fails `ulid.Parse` at the same
boundaries immediately, surfacing in tests rather than silently in production.

**Type information is already fully encoded elsewhere** — the subject domain
segment and the ABAC resource-type prefix. Embedding it in the id is redundant
and harmful.

**Consistency.** Scenes now match every other entity in the world model;
there is one identity convention to learn, not a per-entity exception.

## Alternatives Considered

**Option A: Bare ULID mint (chosen).**

| Aspect | Assessment |
| --- | --- |
| Strengths | Stored id, subject token, and focus-key fields are byte-identical end-to-end; no per-boundary normalization; the bug class cannot recur; consistent with all other entities; deletes the masking harness strip |
| Weaknesses | Abandons existing prefixed scene rows (dev/sandbox data; disposable); future contributors must know the convention and resist adding prefixes |

**Option B: Tolerate the prefix — `TrimPrefix` at each parse boundary.**

| Aspect | Assessment |
| --- | --- |
| Strengths | Preserves existing stored data without a reset; additive change |
| Weaknesses | Enshrines a permanent standing requirement — every new boundary must remember to strip; the next boundary added forgets, reintroducing the exact bug; adds code to preserve undocumented cruft |

**Option C: Duplicate the temporal floor inside the plugin `QueryHistory` path.**

| Aspect | Assessment |
| --- | --- |
| Strengths | Isolates the fix to the plugin boundary without touching host infrastructure |
| Weaknesses | The host already computes and forwards `NotBefore`; duplicating the floor risks drift between two implementations of one invariant; does not fix the parse failures at `streamToFocusKey` or `protoToFocusKey` |

## Consequences

**Positive:**

- `scene log`, `scene log export`, scene join/subscription, and the privacy
  temporal floor all work correctly for real `CreateScene`-minted scenes.
- The bug class (type-tagged id rejected by `ulid.Parse`) cannot recur: a
  future prefixed id fails at the same boundaries immediately, in tests.
- No per-boundary normalization code; boundaries stay simple and uniform.
- Establishes a system-wide identity convention binding all future entity types.

**Negative:**

- Existing prefixed scene rows (dev/sandbox data) are abandoned — no migration
  path. Acceptable because the data is disposable; would NOT be acceptable for
  production data.
- Future contributors must learn the bare-ULID convention; this ADR is the
  record that prevents reintroducing a distinguishing prefix for a new entity.

**Neutral:**

- The `"ope-"` ops-event prefix is unchanged (internal PK, never parsed at a
  focus boundary); a P3 follow-up normalizes it under this convention.
- The ABAC resource-type prefix convention (`scene:<id>`) is unaffected — it is
  a distinct, intentional mechanism at the authorization layer.

## References

- [Scene Bare-ULID Identity Design](../superpowers/specs/2026-05-28-scene-bare-ulid-identity-design.md) — §Decision, §Rationale, §Alternatives, INV-Y5INX-1 / INV-Y5INX-5
- [Scene Bare-ULID Identity Plan](../superpowers/plans/2026-05-28-scene-bare-ulid-identity.md)
- [ADR `holomush-s9nu`](holomush-s9nu-scene-subject-atomic-migration.md) — Atomic scene-subject dot-style migration (touched the same parse boundaries; this ADR completes the identity side)
- Bead: `holomush-y5inx` (the P1 bug this convention resolves)
- Out of scope: `"ope-"` ops-event prefix normalization (P3 follow-up under this ADR)
