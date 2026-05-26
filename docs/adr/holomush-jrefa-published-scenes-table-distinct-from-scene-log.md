<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Publication Artifact Stored in `published_scenes`, Not Reusing the `scene_log` Audit Table

**Date:** 2026-05-23
**Status:** Accepted
**Decision:** holomush-jrefa
**Deciders:** HoloMUSH Contributors
**Related:** [`holomush-qd3r5`](holomush-qd3r5-two-pair-rpc-no-shared-gate-path.md), [`holomush-e3xlx`](holomush-e3xlx-publication-content-as-structured-jsonb.md)

## Context

The Scenes v2 design spec (§1.5) named the publication artifact "Scene Log (Published)" — the same human-readable label used in the `scene log` command surface. Implementation work on Phase 6 surfaced a collision: the `scene_log` table already exists at `plugins/core-scenes/migrations/000004_create_scene_log.up.sql` and `000005_add_scene_log_dek_columns.up.sql` (shipped in PR #267). That table is the per-plugin audit-history mirror: it stores raw event rows with BYTEA-encrypted payloads, DEK references for sensitivity:always events, and mirrors the host `events_audit` schema. Its access surface is the membership-gated `PluginAuditService.QueryHistory` (audit.go:493-555).

The publication artifact has a fundamentally different shape: an immutable, public-when-PUBLISHED snapshot derived from the IC event stream, carrying a rendered structured representation (per ADR `holomush-e3xlx`), frozen `participants_snapshot`, `title_snapshot`, vote-roster summary, retry-attempt tracking. It is one row per publication attempt per scene, not one row per event.

Two approaches were considered:

- **Reuse `scene_log` by extending its schema** to carry publication metadata via additional columns (publication status enum, content_entries JSONB, etc.).
- **New table `published_scenes`** with publication-specific schema and indices; `scene_log` left unchanged.

## Decision

The publication artifact lives in a new `published_scenes` table (and a companion `published_scene_votes` roster table per ADR `holomush-39a5f`). The `scene_log` table remains the per-plugin audit-history mirror, unmodified by Phase 6.

The two tables occupy distinct conceptual roles:

| Concern | `scene_log` (audit) | `published_scenes` (publication) |
| ------- | ------------------- | -------------------------------- |
| Cardinality | One row per scene IC event | One row per publication attempt |
| Payload | BYTEA, encrypted per event | JSONB content_entries, plaintext (frozen at PUBLISHED) |
| Mutability | Append-only event stream | Status transitions per attempt; content_entries set once on PUBLISHED |
| Access surface | `PluginAuditService.QueryHistory` (membership-gated) | `SceneService.GetPublishedScene` / `GetPublicSceneArchive` |
| Privacy model | Participant-only forever (INV-S9) | Participant-only while non-PUBLISHED; public when PUBLISHED |
| Schema lineage | Mirrors host `events_audit` | Phase 6 native |

The `scene log` command (participant audit-history replay) continues to read `scene_log` via `QueryHistory`. The `scene publish download` command (publication artifact) reads `published_scenes` via the participant-gated RPC. Neither surface implies the other.

## Rationale

**The two entities serve fundamentally different roles.** `scene_log` is the per-plugin mirror of the host's `events_audit` table — an append-only event stream with BYTEA-encrypted payloads, DEK references, and a schema driven by the event-bus codec. `published_scenes` is the immutable result of a participant-controlled vote: a structured snapshot frozen at the PUBLISHED transition. Conflating them would force every reader to discriminate between event rows and publication rows via a status column, and would couple two unrelated schema-evolution paths.

**Schema shape diverges.** `scene_log` rows carry BYTEA payloads (encrypted per the crypto manifest), `dek_ref`/`dek_version` (FK-ish references to the DEK registry), and event-bus metadata (subject, type, timestamp, codec, schema_ver). `published_scenes` rows carry JSONB `content_entries` (plaintext, structured), `participants_snapshot` (JSONB frozen names), `title_snapshot`, `vote_window`/`cooloff_window` configuration, retry-attempt tracking, status enum, failure-reason enum. Adding the publication columns to `scene_log` would bloat a hot append-only table with cold reference data; adding event-bus columns to `published_scenes` would carry crypto liability onto the public surface.

**Naming clarity for the command surface.** With distinct tables, `scene log` unambiguously means "the participant audit-history replay" (reads `scene_log`), and `scene publish` / `scene publish download` unambiguously means "the publication artifact" (reads `published_scenes`). The v2 spec's "Scene Log (Published)" naming was a load-bearing source of confusion — every reviewer asked "is that the same `scene_log` table?" and the answer is now unambiguously no.

## Alternatives Considered

**Option A: New `published_scenes` table (chosen).**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | Independent schema lifecycle; clean naming distinction; each table's access patterns and indices are tuned for its role; no crypto liability on the public surface |
| Weaknesses | One additional table to maintain; one cross-table flow (the snapshot pipeline reads `scene_log`, writes `published_scenes.content_entries`) — but this happens once per attempt at a well-defined point |

**Option B: Extend `scene_log` schema with publication columns + status discriminator.**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | One table; the snapshot pipeline becomes a single UPDATE; SQL JOINs simpler |
| Weaknesses | Two distinct concerns share schema; readers must discriminate by status; bloats a hot append-only table with cold publication metadata; entangles crypto-encrypted event rows with plaintext publication rows; future evolution of either concern is blocked by the other's constraints |

**Option C: Same `scene_log` table, separate row type via JSON envelope.**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | No schema change at all; everything is a JSON-payload row |
| Weaknesses | Loses all type-safety on publication-specific fields (status enum, content_entries shape); SQL filters on publication state become JSON path traversals; query performance degrades; the existing crypto contract on `scene_log` (encrypted payloads) doesn't apply naturally to plaintext publication content |

## Consequences

- **Independent schema lifecycle.** Future schema changes to `scene_log` (e.g., AAD reshape, new DEK column for a crypto upgrade) do not affect `published_scenes`, and vice versa.
- **Clearer naming.** "Publication" and "publish" reference `published_scenes`; "scene log" and "audit history" reference `scene_log`. The previously-ambiguous "Scene Log" (Published)" naming (v2 spec §1.5) is replaced by `PublishedScene` everywhere in Phase 6.
- **Cost of duplication.** A read of "everything ever recorded for scene X" hits two tables — `scene_log` for raw events plus `published_scenes` for any publication artifact. This is conceptually correct: those are two different things. The snapshot pipeline at COOLOFF → PUBLISHED transition does cross the boundary (reads `scene_log`, writes `published_scenes.content_entries`) but only in one direction and only at one well-defined point.
- **Tests do not share fixtures.** Audit-history tests use one schema and one access path; publication tests use another. Tests cannot accidentally cross-contaminate.
- **FK posture.** Cross-schema FKs from `published_scenes` or `published_scene_votes` to `public.characters` and `public.players` are forbidden by plugin role isolation — a substrate constraint, not a design choice. The plugin database role cannot reference host-schema tables, so `initiated_by` and `character_id` referential integrity is enforced in the Go service layer. One intra-schema FK is added: `published_scene_votes.published_scene_id → published_scenes(id) ON DELETE CASCADE`. This is defense-in-depth (vote rows can never outlive their parent attempt row) and is inert until an attempt-deletion or GC path exists. The `published_scenes.scene_id` reference to `scenes` is intentionally omitted — publications are designed to outlive the scene they record.
- **Snapshot-pipeline soft no-op (C7, `holomush-5rh.20.26`).** Without an FK on `published_scenes.scene_id`, a scene deleted mid-publication causes the scene-state UPDATE (`UPDATE scenes SET state = 'archived'`, snapshot Step 7) to silently no-op. The pipeline MUST detect this via rowcount and log a warning (`code=SCENE_PUBLISH_PARENT_SCENE_GONE`), but publication still completes — the archive intentionally outlives its source. A 0-row result is NOT a publish failure: the attempt still finalizes to PUBLISHED with `content_entries`, and the snapshot MUST NOT transition to ATTEMPT_FAILED. The companion scene-metadata read (Step 5) is correspondingly tolerant of a missing scene (empty title + nil participants). This is the direct operational consequence of the "publications outlive the scene" design choice above. See the Phase 6 spec §11 (Step 7 rows-affected check) and §11.4 "Soft no-op cases".

## References

- [Scenes Phase 6 design spec](../superpowers/specs/2026-05-23-scenes-phase-6-logs-vote-privacy-design.md) §2 (divergence table — v2 §1.5 renaming), §3.1, §3.3, §11.1 (snapshot reads from scene_log into published_scenes)
- [Scenes Phase 6 implementation plan](../superpowers/plans/2026-05-23-scenes-phase-6-logs-vote-privacy.md) Task A1 (000008 migration)
- [Scenes v2 design](../superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md) §1.5 (superseded naming)
- Design bead `holomush-5rh.20` (brainstorm Q2 — publication-artifact rename)
