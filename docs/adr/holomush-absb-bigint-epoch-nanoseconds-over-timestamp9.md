<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Use BIGINT Epoch Nanoseconds Over the timestamp9 PostgreSQL Extension

**Date:** 2026-05-22
**Status:** Accepted
**Decision:** holomush-absb
**Deciders:** HoloMUSH Contributors
**Related:** holomush-gfo6, holomush-rbw6, holomush-f5h0

## Context

HoloMUSH needs nanosecond-resolution timestamps for two reasons:

1. **Ergonomic.** 138 `Truncate(time.Microsecond)` call sites across 19 files (counted at design time) exist solely to defend against PG `TIMESTAMPTZ`'s µs ceiling. Tests carry `time.Sleep(50 * time.Millisecond)` hacks to avoid sub-µs timestamp ties.
2. **Correctness.** INV-P7-16 (AAD byte-equality between encrypt-time and audit-read time) currently depends on every code path truncating to µs consistently — a discipline obligation that is silent on violation until a downstream AAD-tag-check failure surfaces it.

The `timestamp9` PostgreSQL extension (`int64` ns-since-epoch, 8 bytes, MIT-licensed, supports PG 14-18 via Pigsty) was the leading candidate. Two alternatives were also evaluated: pure `BIGINT` epoch nanoseconds at the application layer, and "status quo plus stronger discipline."

The grounding sources for the comparison are recorded on `holomush-gfo6` (probe scans of the codebase, context7 docs for pgx Codec interface, Exa search of timestamp9's packaging via Pigsty).

## Decision

All persistent timestamps MUST be stored as `BIGINT` representing `time.Time.UnixNano()` (UTC).

`timestamp9` is rejected on portability and maintenance grounds. Status-quo is rejected because it makes INV-P7-16 structurally untestable — every future contributor must internalize the truncation contract, and violation is silent.

This decision sets a permanent schema-type convention across all 54 current and all future persistent-timestamp columns. New migrations adding `TIMESTAMPTZ` or `TIMESTAMP` columns are rejected by `task lint:no-timestamptz` (INV-TS-1); the escape hatch is the inline comment `-- pgnanos-exempt: <reason>`.

## Alternatives Considered

### A — `timestamp9` extension

**Strengths:** Native PG semantics (`ORDER BY`, `NOW()`, interval arithmetic, `psql`-readable); same on-disk size as `TIMESTAMPTZ` (8 bytes); bidirectional casts to `BIGINT` and `TIMESTAMPTZ`.

**Weaknesses:** Requires a custom Docker image (`postgres:18-alpine` plus a source build, or a switch to the Pigsty-distributed image); pgx needs a custom Codec implementation with OID lookup at bootstrap; not in core `postgres-contrib`; a permanent maintenance liability tracking the extension version against the PG patch line. A→B migration (back out to BIGINT) is painful: rip out the Codec, rebuild the image, alter every column.

### B — `BIGINT` epoch nanoseconds at the application layer (chosen)

**Strengths:** No extension dependency, `postgres:18-alpine` unchanged. pgx native `int64` round-trip — no Codec registration. The codebase is already ns-native at the AAD encoding layer (`internal/eventbus/crypto/aad/aad.go:74` serializes `event.Timestamp.AsTime().UnixNano()`) and at `BIGINT` columns like `events_audit.js_seq`. Portable to any managed PG offering. B→A migration is mechanical if native semantics are ever needed.

**Weaknesses:** No native PG interval math or `NOW()`-readable timestamps in `psql`; ad-hoc SQL requires epoch arithmetic (`WHERE ts > extract(epoch from now() - interval '1 day') * 1e9`). Acceptable because the affected tables (event-history, audit) are accessed programmatically, not via `psql` sessions.

### C — Status quo plus stronger discipline

**Strengths:** No migration; no new packages.

**Weaknesses:** All 138 `Truncate(time.Microsecond)` sites remain. AAD byte-equality stays code-review-enforced. Every future contributor writing an event-producing path must know to truncate to µs before AAD construction. The discipline is silent on violation.

## Rationale

1. **The codebase is already ns-native at the layer that matters.** The AAD encoding at `aad.go:74` uses `UnixNano()`. The µs-truncate at `publisher.go:202` is a workaround for column-resolution mismatch, not a load-bearing semantic choice. BIGINT closes the gap structurally.

2. **Asymmetric reversibility.** B→A (BIGINT to `timestamp9`) is a mechanical migration if we ever want native semantics. A→B is painful (Codec rip-out, image rebuild). The safer long-term bet is the one with the cheap escape hatch.

3. **No external-extension dependency in the persistence layer.** Adopting `timestamp9` would be the first non-`postgres-contrib` extension in the deployment. The maintenance cost (track extension releases, rebuild image on PG patches, recompile Codec on pgx upgrades) is permanent and disproportionate to the ergonomic gain.

4. **Lint enforcement is mechanical.** INV-TS-1 (`task lint:no-timestamptz`) catches any new `TIMESTAMPTZ`/`TIMESTAMP` column in a future migration. Future contributors do not need to read this ADR to comply.

## Consequences

**Positive:**

- No Docker image customization required; `postgres:18-alpine` stays as-is across `compose.yaml`, `compose.prod.yaml`, and testcontainers.
- INV-TS-1 lint enforces the convention mechanically; no human checklist required.
- Down-migration path exists per column if native PG semantics are ever needed (precision-lossy, ns→µs).
- AAD byte-equality (INV-TS-5; see `holomush-f5h0`) becomes structural rather than discipline-dependent.

**Negative:**

- Ad-hoc SQL over timestamp columns requires epoch arithmetic — `psql` sessions are less readable for human inspection.
- DB-side `DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT` emits µs-truncated values because PG's `now()` clock resolution is µs. Correctness-critical columns (the event `timestamp` field bound by AAD, scope-floor inputs) MUST be app-supplied to carry full ns precision.
- No native PG interval comparisons or range-query convenience against timestamp values.

**Neutral:**

- All 54 existing `TIMESTAMPTZ` columns require paired up/down migrations.
- Down migrations are precision-lossy (ns→µs on rollback) and MUST be labeled as such in the migration header.
- `internal/pgnanos.Time` (see `holomush-rbw6`) is the canonical scan/insert seam between Go `time.Time` and the new BIGINT columns.

## Implementation

See the implementation plan at `docs/superpowers/plans/2026-05-22-nanosecond-timestamps.md` (epic `holomush-gfo6`). Phase 0 ships the `internal/pgnanos` helper. Phase 1 atomically lands publisher + floor truncate deletions, crypto-domain migrations, and AAD test strengthening. Phases 2-4 are parallel-safe ergonomic cleanups. Phase 5 wires the three new `task lint:*` guards to prevent regression.

## References

- Spec: `docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md` §1 (Alternatives evaluated and rejected table)
- AAD encoding: `internal/eventbus/crypto/aad/aad.go:62-117`
- Publisher truncate site (Phase 1 deletion target): `internal/eventbus/publisher.go:202`
- timestamp9 packaging: https://ext.pigsty.io/e/timestamp9/
- pgx Codec interface: https://pkg.go.dev/github.com/jackc/pgx/v5/pgtype#Codec
