# Requirements (from PRDs)

## Corpus note

This ingest batch contains **zero documents classified as PRD**. All 50 classifications
are `SPEC` (48) or `DOC` (2). Per the synthesis process, `requirements.md` is populated
only from PRD-type sources — none were selected into this curated 50-doc ingest.

## What this means for downstream consumers (`gsd-roadmapper`)

- There is **no PRD-acceptance-criteria surface** in this batch — the "competing
  acceptance variants" check trivially passes with zero PRD requirements to compare.
- Feature-level intent that would normally live in a PRD is instead expressed as
  RFC2119-keyword requirements embedded directly in the SPEC docs (goals/non-goals,
  in-scope/out-of-scope sections, "MUST"/"MUST NOT" statements). These are preserved in
  `constraints.md`, attributed to their SPEC source, but are NOT re-derived as `REQ-*`
  IDs here — doing so would require the synthesizer to invent requirement identity that
  the source docs never declared, which risks fabricating structure the docs don't have.
- If `gsd-roadmapper` needs REQ-style traceability, it should derive requirement IDs from
  the SPEC-level MUST statements captured in `constraints.md`, scoped per subsystem
  (e.g. a `REQ-scenes-publish-vote` could be derived from the Phase 6 constraints entry).
  This synthesis deliberately does not perform that derivation to avoid inventing
  requirement identity beyond what the source corpus states.

No entries below by design — see note above.
