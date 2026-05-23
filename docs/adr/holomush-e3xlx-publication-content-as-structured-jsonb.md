<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Scene Publication Content Stored as Structured JSONB Entry Array, Rendered on Demand

**Date:** 2026-05-23
**Status:** Accepted
**Decision:** holomush-e3xlx
**Deciders:** HoloMUSH Contributors
**Related:** [`holomush-jrefa`](holomush-jrefa-published-scenes-table-distinct-from-scene-log.md)

## Context

The Phase 6 publication artifact (per `holomush-jrefa`) must serve multiple consumers:

- The participant `scene publish download` command (terminal output)
- The future Phase 9 web client (HTML/Markdown rendering)
- Public archive URLs (anonymous reads)
- The `scene log export` command from `holomush-cb4x` (participant-only audit-history export, same renderer surface)
- Potential future formats: PDF, RSS, syndicated feed entries

The Scenes v2 design (§1.5) named the field "Content" with no specified shape. The brainstorm question Q7 asked: store content as a rendered markdown blob, or as a structured representation?

Three approaches were considered:

- **Markdown blob.** Single TEXT column holds rendered markdown, written once at PUBLISHED transition. Cheap render at read time (it's already markdown); fixed format.
- **Multi-format pre-rendered.** Separate columns or rows for each format (markdown + plain_text + jsonl). No render at read time; storage cost multiplied per format; adding new formats requires backfill.
- **Structured JSONB.** Array of `{speaker, kind, content}` entries where `kind ∈ {pose, say, emit}`. Renderers derive their output from the structure at request time.

## Decision

`published_scenes.content_entries` is a JSONB array of structured entry objects. Each entry has three fields:

- `speaker` (string) — character name as of publish time, taken from `participants_snapshot`
- `kind` (string enum: `pose | say | emit`) — discriminates the original IC event kind; OOC and ops events are excluded per ADR `holomush-sb3n` and v2 spec §3.1
- `content` (string) — the decrypted IC payload

The renderer interface lives in `plugins/core-scenes/publish_render.go` and exposes three functions:

- `renderMarkdown(entries []Entry) string`
- `renderPlainText(entries []Entry) string`
- `renderJSONL(entries []Entry) ([]byte, error)`

All three are pure functions over the structured representation. `DownloadPublishedScene` and `DownloadPublicSceneArchive` pick a renderer based on the request's `format` field at request time. The snapshot pipeline (COOLOFF → PUBLISHED transition; spec §11) reads decrypted IC events from `scene_log`, filters to the three IC content kinds, and writes the structured entries to `content_entries` atomically with the status change.

## Rationale

**Format extensibility is the load-bearing requirement.** Phase 6 ships markdown + plain_text + jsonl, but the publication artifact's lifetime is years (immutable post-publish). HTML (Phase 9 web view), PDF (export), RSS (syndication), Atom — each is a plausible future format. Pre-rendered storage forces a re-snapshot of every archived scene whenever a new format ships, which is operationally prohibitive once thousands of publications exist. Structured storage makes format addition a pure-Go change.

**Decoupling storage and presentation is good architecture.** The structured entry shape (`speaker`, `kind`, `content`) captures everything semantically interesting about a pose/say/emit: who spoke, what kind of utterance, what they said. The renderer interface is a stable extension point; renderers are pure functions; the storage layer is unaware of formatting. This mirrors the broader holomush pattern of "store structured data, render at the boundary."

**Future per-entry operations become tractable.** Content flagging (a user-reports-this-line surface), per-entry attribution updates (if a character's display name changes post-publish — though current design freezes via `participants_snapshot`), partial redaction (admin-mediated content moderation, future) — all become possible without schema migration once the granularity exists at storage time.

**Render cost at request time is acceptable.** Most scenes have hundreds to low-thousands of entries; rendering N entries is O(N) string concatenation, microseconds in practice. Phase 6 spec §15.5 sets coverage targets that include renderer paths (100% on `publish_render.go`). If a future scene is so large that render cost becomes load-bearing, caching pre-rendered formats in a separate table is an additive change that requires no migration of the canonical `content_entries`.

## Alternatives Considered

**Option A: Structured JSONB entry array, render on demand (chosen).**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | Format-agnostic; renderer interface is a stable extension point; new formats are additive; future per-entry operations tractable; storage decoupled from presentation |
| Weaknesses | O(N) render cost on every download; opaque to ad-hoc SQL (requires `jq` or renderer); JSON marshaling at storage time |

**Option B: Pre-rendered markdown blob.**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | Zero render cost at download; cheap to inspect via SQL; single-column storage |
| Weaknesses | Locks the publication to one format for its lifetime; adding plain_text/jsonl/HTML requires re-snapshot of all archived scenes (operationally prohibitive); per-entry operations require regex-based markdown parsing; renderer logic changes don't retroactively apply to old publications (which is correct, but the path to add new formats is blocked) |

**Option C: Multiple pre-rendered columns, one per format.**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | Zero render cost; multiple formats served immediately |
| Weaknesses | Storage cost multiplied per format (3 formats = 3× row size); adding a new format requires both schema migration AND backfill of every archived row; per-entry operations same blockers as Option B |

## Consequences

- **Format extensibility.** Adding HTML, PDF, RSS, etc. requires only a new renderer function; no schema change, no backfill of archived data.
- **Decoupled storage and presentation.** The renderer interface is a stable extension point. Storage is unaware of formatting; formatting is unaware of storage.
- **Render cost at read time, not write time.** Every `Download*` call pays a rendering cost (O(N entries) for N pose/say/emit events). For scenes with thousands of poses, this matters; large-scene optimization (e.g., caching the most-popular renders in a separate table) becomes a follow-up question, not a backfill emergency.
- **Future per-entry operations are tractable.** Content flagging, per-entry attribution updates, partial redaction — all become possible without schema migration once the granularity exists.
- **Opaque to ad-hoc SQL.** Direct DB inspection (`SELECT content_entries FROM published_scenes ...`) requires `jq` or the renderer for human readability. Acceptable trade-off; ad-hoc inspection of publication content is not a primary access path.
- **Renderer test coverage matters.** The structured representation is "correct"; the renderer functions are where format-specific bugs would live. Phase 6 ships ≥90% coverage on `publish_render.go` (plan §15.5 coverage targets).

## References

- [Scenes Phase 6 design spec](../superpowers/specs/2026-05-23-scenes-phase-6-logs-vote-privacy-design.md) §3.1, §12, §11 (snapshot pipeline)
- [Scenes Phase 6 implementation plan](../superpowers/plans/2026-05-23-scenes-phase-6-logs-vote-privacy.md) Task C1 (Markdown), C2 (plain-text), C3 (JSONL), C6/C7 (snapshot pipeline)
- Design bead `holomush-5rh.20` (brainstorm Q7 — storage shape and renderer formats)
- [ADR holomush-sb3n](holomush-sb3n-scene-content-sensitivity-always.md) — content kind classification (drives the kind enum values)
