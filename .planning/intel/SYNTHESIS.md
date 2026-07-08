# Synthesis Summary

Mode: **new** (bootstrap ingest — no existing `.planning/` PROJECT/REQUIREMENTS/ROADMAP
to reconcile against). Source: a curated 50-doc strategic subset of a much larger corpus
(194 ADRs + 273 specs + 212 plans exist in the repo; only these 50 high-signal docs were
selected for this ingest).

## Doc counts by type

| Type | Count | Notes |
| --- | --- | --- |
| ADR | 0 | None in this ingest batch. |
| SPEC | 48 | All `confidence: high`, none `locked`, none with a per-doc precedence override. |
| PRD | 0 | None in this ingest batch. |
| DOC | 2 | `docs/architecture/invariants.md`, `docs/roadmap.md` — both treated as authoritative for constraints/strategy per this ingest's special instruction (precedence used only to break direct contradictions). |
| **Total** | **50** | Matches classification file count. |

## Decisions locked

**0.** No ADR-type docs exist in this batch, so no LOCKED decisions were extracted.
See `decisions.md` for the explanation and pointer to the real ADR corpus
(`docs/adr/*.md`, not part of this ingest).

## Requirements extracted

**0** formal `REQ-*` entries. No PRD-type docs exist in this batch. Feature-level
intent is instead expressed as RFC2119 MUST statements embedded in the 48 SPEC docs —
see `requirements.md` for the rationale on why these were not force-fit into invented
`REQ-*` identity, and `constraints.md` for where that intent actually lives.

## Constraints (48 SPEC entries, grouped by domain)

| Domain group | Entries |
| --- | --- |
| Foundational architecture | 7 (1 archived/historical) |
| ABAC / access control | 2 |
| Web client & session persistence | 2 |
| Channels | 1 |
| Scenes / RP subsystem (Epic 9) | 17 |
| Social-spaces substrate & presence | 3 |
| Event bus, crypto, wire conventions | 5 |
| Plugin capability & least-privilege epic | 8 |
| Web portal / shell / rendering slices | 3 |
| **Total** | **48** |

See `constraints.md` for the full per-doc breakdown (title, source, type tag, constraint
summary, scope).

## Context topics (2 DOC entries)

1. **System invariant registry** (`docs/architecture/invariants.md`) — 14 scopes,
   200+ cataloged invariants (`INV-CRYPTO` 122, `INV-SCENE` 68, plus 12 smaller scopes),
   binding ratchet (`pending` → `bound`). Treated as authoritative for CONSTRAINTS.
2. **HoloMUSH strategic roadmap** (`docs/roadmap.md`) — 3 active themes
   (`theme:social-spaces`, `theme:plugin-capability-architecture`,
   `theme:web-portals`), 4 completed theme retrospectives, 2 maintenance threads.
   Treated as authoritative for STRATEGY/SCOPE.

See `context.md` for the full topic writeup.

## Conflicts

- **BLOCKERS: 0** — no LOCKED-vs-LOCKED contradictions (zero ADRs in batch), no
  UNKNOWN/low-confidence docs, no cross-ref cycles (DAG confirmed acyclic across all 50
  nodes).
- **WARNINGS: 2** — both are same-precedence-tier (`SPEC` vs `SPEC`) architecture
  disputes that this synthesis deliberately did NOT auto-resolve, even though corroborating
  DOC-authoritative evidence exists for both:
  1. Scene ownership/data-model: `world-model-design` (locations-table scenes) vs the
     17-spec Epic 9 lineage (plugin-owned `core-scenes`).
  2. Web scene-creation write path: `web-portal-scenes-design` (E9.5 D4,
     command-path-only) vs `web-create-scene-design` (typed-RPC restoration).
- **INFO: 6** — auto-resolved/transparency notes: no ADR/PRD in batch; archived
  architecture doc superseded (WASM → Lua/go-plugin); scene bare-ULID bugfix; session
  liveness redesign (extension, not contradiction); plugin-capability epic (8 sub-specs,
  coherent sequential redesign); scenes/RP phase series (17 specs, coherent incremental
  build).

Full detail, every entry grounded with `source:`/`Found:`/`Impact:`/`→` remediation
lines: `.planning/INGEST-CONFLICTS.md`.

## Pointers for `gsd-roadmapper`

- `.planning/intel/decisions.md` — empty by design (no ADRs in batch); read for the
  explanation.
- `.planning/intel/requirements.md` — empty by design (no PRDs in batch); read for the
  explanation.
- `.planning/intel/constraints.md` — the primary technical-constraint surface; 48
  entries grouped by domain, each with `source:`/`type:`/`constraint:`/`scope:`.
- `.planning/intel/context.md` — invariant registry + roadmap themes, both DOC-sourced
  and treated as authoritative for their respective concerns.
- `.planning/INGEST-CONFLICTS.md` — the 2 WARNINGS require explicit user resolution
  before PROJECT.md/ROADMAP.md bake in a stance on either scene-ownership-model or the
  web-scene-creation write path. Neither is a hard BLOCKER, but both are architecturally
  significant enough to warrant a human decision rather than a silent pick.

## STATUS: AWAITING USER — competing variants need resolution
