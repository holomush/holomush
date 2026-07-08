# Decisions (from ADRs)

## Corpus note

This ingest batch (50 classified docs, `.planning/intel/classifications/`) contains
**zero documents classified as ADR**. All 50 classifications are `SPEC` (48) or `DOC` (2).
No `locked: true` markers appear anywhere in the batch.

Per the synthesis process, `decisions.md` is populated only from ADR-type sources. Since
none were selected into this curated 50-doc ingest, this file has no per-ADR entries.

## What this means for downstream consumers (`gsd-roadmapper`)

- There is **no LOCKED-decision surface** in this batch — the LOCKED-vs-LOCKED blocker
  check (see `INGEST-CONFLICTS.md`) trivially passes with zero candidates.
- Architectural decisions still exist in this codebase (the repo has a `docs/adr/`
  directory referenced throughout the SPEC corpus — e.g. `docs/adr/holomush-v4qmu-...md`,
  `docs/adr/holomush-mihtk-...md`, `docs/adr/holomush-o8gx8`, `docs/adr/holomush-pbp9j`,
  ADR 0005, ADR 0007, ADR 0014 — but none of those ADR files were part of this 50-doc
  curated ingest set, so their content is **not** synthesized here.
- Design-rationale-level decisions embedded *inside* the ingested SPEC docs (e.g. "why we
  chose X over Y") are preserved in `constraints.md` under each SPEC's entry, attributed to
  their SPEC source — they are NOT elevated to ADR status by this synthesis, since the
  classifier did not tag their source doc as ADR.
- If a future ingest run includes the `docs/adr/*.md` corpus, re-run ingestion so those
  LOCKED/proposed decisions can be synthesized here and cross-checked against the
  constraints/requirements already captured from this batch (particularly around the two
  same-tier SPEC-vs-SPEC conflicts flagged in `INGEST-CONFLICTS.md`, both of which are
  exactly the kind of dispute a governing ADR would normally settle).

No entries below by design — see note above.
