<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Migrate Scene Subjects to NATS Dot-Style Atomically With Plugin Emit Code

**Date:** 2026-05-19
**Status:** Accepted
**Decision:** holomush-s9nu
**Deciders:** HoloMUSH Contributors

## Context

Scenes Phase 4 (`holomush-5rh.13`) is the first time `core-scenes` emits plugin-owned scene events. The substrate-contract spec INV-S4 requires NATS dot-style subjects for all new code:

```text
events.<game_id>.<domain>.<entity-id>[.<facet>...]
```

For scenes specifically:

- IC stream: `events.<game_id>.scene.<scene_id>.ic`
- OOC stream: `events.<game_id>.scene.<scene_id>.ooc`

Existing substrate-side scene-aware code (`internal/grpc/stream_access.go`, `internal/grpc/scope_floor.go`, `internal/grpc/query_stream_history.go`) was authored before INV-S4 and parses legacy colon-style subjects (`scene:<id>:ic`, `scene:<id>:ooc`). Two migration strategies were available:

1. Add a translation layer (extend `internal/eventbus/subjectxlate/`) so colon-style and dot-style coexist; plugin emits dot-style; substrate gates parse colon-style; the layer translates.
2. Migrate plugin emit code AND substrate-side scene-aware code atomically in the same PR; no translation layer for scene subjects.

A complicating discovery: `internal/grpc/scope_floor.go:21-28` carries a developer note that the function's colon-style prefix match never fires for production NATS subjects:

> "`streamScopeFloor` currently inspects legacy stream-name prefixes (`location:`, `scene:`), but production callers pass NATS subjects (`events.<gid>.location.X`), so the loop returns `time.Time{}` for every real-world subject today."

This means the iwzt §6.1 temporal floor for scene streams is not merely untested — it is **actively non-functional for all real traffic**. The function is wired up, but its prefix match never matches production subjects. INV-P4-9 ("late-joining participants MUST see only IC events from `joined_at` forward") is therefore a live invariant currently being violated by silent fallthrough.

## Decision

Scene-subject migration ships atomically: plugin emit code AND substrate-side scene-aware code migrate to NATS dot-style in one PR. No translation layer for scene subjects.

Concretely, the single PR contains:

- Plugin: `plugins/core-scenes/service.go` emit subjects updated; new helpers `dotStyleSceneSubject` / `IC` / `OOC` in `store.go`.
- Substrate: `internal/grpc/stream_access.go` (`isPrivateStream`, `extractSceneID`, scene-stream detection), `internal/grpc/scope_floor.go` (scene branch in `streamScopeFloor`), `internal/grpc/query_stream_history.go` (I-17 hardcoded scene-membership gate) — all migrated to recognise dot-style.
- Tests: corresponding `_test.go` files updated; INV-P4-9 unit-level pre/post pin lands as a table-driven test whose diff in the migration commit captures the bug-fix moment.
- Docs: iwzt §3 + scenes v2 §3.1 stream-naming tables updated.

Non-scene colon-style subjects (`location:*`, `character:*`, `notifications:*`) are explicitly out of Phase 4 scope; tracked separately by `holomush-rops` (P1, top-level).

INV-S1 boundary discipline: the substrate-side change crosses the `internal/grpc/` boundary, so the PR is reviewed under both `code-reviewer` (substrate) AND `abac-reviewer` (`query_stream_history.go` adjacency to access control gating). The Phase 4 plugin work and the substrate-side scene-aware migration are reviewed together; INV-S1 forbids bundling cross-cutting substrate changes inside a plugin PR — Phase 4 is permitted because the substrate change is scene-aware (not domain-free substrate) and the boundary contract for that substrate code IS the scene subject format.

## Rationale

**The I-17 gate fails closed on unrecognized subject forms.** A translation window where plugin emits dot-style but substrate gates parse colon-style would silently break scene stream access for all Phase 4 emits. There is no acceptable transitional state.

**The translation layer adds permanent maintenance surface.** `subjectxlate/` is a generic translator with no scene-specific path today. Extending it to handle scene subjects in both directions adds code that must be maintained indefinitely; the broader `holomush-rops` sweep would eventually remove it anyway. Atomic migration avoids accreting the very layer the project directive wants gone.

**Closes the `scope_floor.go` live silent regression as a correctness fix.** Atomic migration converts a "format hygiene" change into a "bug-fix" change — the iwzt §6.1 temporal floor is currently inactive for scene streams, and Phase 4's migration is the first time it actually starts working. INV-P4-9's unit-level test pins the bug-fix moment via the test-file diff in the migration commit.

**Single-form parsing is structurally simpler.** Post-migration, substrate gates know exactly one subject form. No branching, no conversion errors, no "did the translation layer handle this case" debugging.

## Alternatives Considered

**Option A: Translation layer for scene subjects.**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Plugin and substrate can land independently; no need to coordinate a single large PR |
| Weaknesses | Translation layer adds permanent maintenance surface; I-17 gate would need to handle both forms; creates a window where dot-style emits hit colon-style gate and fail closed; doesn't fix the `scope_floor.go` live regression — just papers over it |

**Option B: Atomic migration in one PR (chosen).**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Eliminates translation surface; substrate gates parse exactly one form; INV-P4-1 enforceable by meta-test rg scan; closes the `scope_floor.go` live silent regression as a correctness change; unit-level test diff pins the bug-fix moment |
| Weaknesses | Larger single PR; substrate-side reviewed under `code-reviewer` + `abac-reviewer`; INV-S1 boundary discipline requires the substrate change to be reviewed alongside the plugin change |

**Option C: Project-wide migration of ALL colon-style subjects.**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Single coherent sweep; nothing left in legacy form |
| Weaknesses | Touches multiple plugins (core-communication), session stream code, web client backfill code, iwzt code paths; Phase 4 doubles or triples in size; harder to review, harder to roll back; non-scene migration is not blocking Phase 4 |

## Consequences

**Positive:**

- Single canonical subject form for scene streams; no parsing branches for legacy colon-style in substrate.
- Fixes the `scope_floor.go` temporal-floor regression silently bypassed by all production traffic.
- INV-P4-1 meta-test enforces "no colon-style scene subjects in production code" as a CI-enforced contract.
- The decision establishes the precedent for `holomush-rops`'s broader sweep: each domain (scenes, location, character, notifications) migrates atomically with its substrate-side gate consumers.

**Negative:**

- Larger PR footprint; coordinated review under multiple gates.
- Non-scene colon-style migration remains; the codebase is partially migrated for the duration of `holomush-rops`.
- The substrate-side scene-aware code crosses the plugin boundary INV-S1 — defensible because it's scene-aware substrate (not domain-free), but the precedent must be cited carefully to avoid normalizing INV-S1 violations.

**Neutral:**

- The non-scene colon-style sweep (`location:*`, `character:*`, `notifications:*`) is tracked by `holomush-rops` (P1).
- ABAC resource IDs (`scene:<id>` in Cedar policies) are explicitly NOT topics; they remain in `<resource_type>:<id>` form per policy-DSL convention.

## Consequences Addendum (2026-05-30, holomush-rops)

`holomush-rops` completed the broader non-scene colon-style sweep, making the
dot-style subject form universal across all domains (location, character,
notifications, scene). As a result:

- **INV-P4-1's 3-file scene scan is superseded by INV-ROPS-3** — the
  repo-wide colon-stream-literal gate (`holomush-rops`) now enforces the
  invariant across all domains. INV-P4-1 remains historically accurate as the
  first per-domain instance; INV-ROPS-3 is the authoritative gate going forward.
- The dot-style scene migration this ADR recorded is now the universal stream
  form. All pub/sub subjects use `events.<game_id>.<domain>.<entity-id>[.<facet>...]`
  with no colon-style path remaining in production emit code.
- `internal/eventbus/subjectxlate/` (the legacy translation layer) was removed
  as part of `holomush-rops`.

## References

- [Scenes Phase 4 Design](../superpowers/specs/2026-05-19-scenes-phase-4-streams-and-pose-order-design.md) §3.1, §3.2, §3.3
- [Substrate Contract Spec](../superpowers/specs/2026-05-16-social-spaces-substrate-contract.md) §1.1, INV-S4 (NATS dot-style mandatory)
- [History Scope Privacy Design](../superpowers/specs/2026-05-17-history-scope-privacy-design.md) §3, §6.1 (iwzt scene-stream temporal floor)
- [ADR `holomush-jhl5`](holomush-jhl5-plugin-history-scope-opt-in.md) — Plugin manifest history_scope opt-in (related plugin/host contract pattern)
- Bead: `holomush-rops` (P1 — broader non-scene colon-style sweep, supersedes INV-P4-1)
- Bead: `holomush-5rh.13` (Phase 4 design)
