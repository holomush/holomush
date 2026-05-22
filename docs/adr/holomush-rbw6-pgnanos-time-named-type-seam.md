<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# `pgnanos.Time` Named Type as the Canonical BIGINT-Nanos Scan/Insert Seam

**Date:** 2026-05-22
**Status:** Accepted
**Decision:** holomush-rbw6
**Deciders:** HoloMUSH Contributors
**Related:** holomush-gfo6, holomush-absb, holomush-f5h0

## Context

With `BIGINT` epoch nanoseconds chosen as the storage shape for all persistent timestamps (see `holomush-absb`), the codebase needs a conversion boundary between Go `time.Time` and `int64`. Every repository that reads or writes a timestamp column crosses this boundary.

Three shapes were evaluated:

1. A named type wrapping `time.Time`, implementing `sql.Scanner` + `driver.Valuer`.
2. A pair of free functions (`From(time.Time) int64`, `Scan(int64) time.Time`) with no named type.
3. Global `pgtype.Codec` registration that maps `BIGINT` columns to `time.Time` automatically.

The choice shapes every repo that touches a timestamp column and governs whether the compiler can catch direction errors at the seam.

## Decision

Adopt a named type `pgnanos.Time` (a `time.Time` wrapper) in a new package `internal/pgnanos`. The type implements `sql.Scanner` and `driver.Valuer`. Repos use `pgnanos.From(t time.Time)` to construct a value for `Exec` arguments and scan into `pgnanos.Time` / `*pgnanos.Time` (pointer for nullable columns), then call `.Time()` to extract the Go-side `time.Time`.

Direct `int64` ↔ `time.Time` arithmetic in repository code outside the `pgnanos` package is prohibited (INV-TS-2). The lint `task lint:no-unixnano-in-repos` enforces this over production paths; the escape hatch is `// pgnanos-exempt: <reason>` on the same line.

## Alternatives Considered

### A — Named type `pgnanos.Time` (chosen)

**Strengths:** Surfaces the conversion in the type signature. `Scan(&t)` on a `BIGINT` column with `t time.Time` is a compile error: the call site MUST write `&nanos` where `nanos` is a `pgnanos.Time`. Direction errors (reading as `int64`, writing raw `time.Time`) are caught at compile time, not at runtime. The type also serves as a project convention — future packages that persist timestamps inherit the seam without documentation lookup.

**Weaknesses:** Adds one indirection at every call site. SELECT goes from `Scan(&t)` to `Scan(&n); t := n.Time()`; INSERT goes from `Exec(..., t)` to `Exec(..., pgnanos.From(t))`. Contributors unfamiliar with the pattern will encounter compile errors until they learn the seam.

### B — Plain function pair without a named type

**Strengths:** Less verbose. No new type to learn.

**Weaknesses:** Does not surface the seam in type signatures. A repo method returning `time.Time` is indistinguishable from one returning a truncated or raw value. Direction errors are silent.

### C — Global `pgtype.Codec` registration

**Strengths:** Automatic — any `Scan` into `time.Time` from a `BIGINT` column would use the registered codec.

**Weaknesses:** Hijacks ALL `BIGINT` columns. The codebase has legitimate non-nanosecond `BIGINT` columns:

- `events_audit.js_seq` — JetStream sequence number
- `crypto_keys.dek_ref` — DEK reference identifier
- `schema_ver` — protocol envelope schema version

Global codec registration would silently mis-interpret these columns as timestamps. The seam MUST be opt-in.

## Rationale

1. **Compile-time enforcement.** The named type catches the most common bug — `Scan(&t)` where `t` is `time.Time` against a BIGINT column — at compile time. The same bug under option B or C is a silent runtime corruption.

2. **`pgtype.Codec` is too broad.** The codebase has three legitimate non-timestamp `BIGINT` column shapes. A global codec would corrupt all three. Even a narrowed codec (e.g., applied only to columns named `*_at`) is a leaky convention that ties the type system to column-name patterns.

3. **Opt-in is the right default.** A new BIGINT column added in some future feature might not be a timestamp. The named type forces the author to opt in to `pgnanos.Time` rather than to opt out of an automatic conversion.

4. **Lint covers the gaps.** `task lint:no-unixnano-in-repos` rejects raw `UnixNano()` / `time.Unix(0, ...)` calls in production paths under `internal/{auth,world,totp}/postgres/`, `internal/eventbus/history/`, and `plugins/*/`. Test code is exempt because test fixtures legitimately construct nanos directly.

## Consequences

**Positive:**

- Compiler catches direction errors at the scan/insert seam.
- INV-TS-2 lint covers any production path that bypasses the type.
- Single package (`internal/pgnanos`) is the audit target for conversion correctness; all other packages are consumers.
- Sets a project-wide convention: future packages persisting timestamps inherit the type without reading this ADR.

**Negative:**

- Two-line read pattern (`var n pgnanos.Time; ... .Scan(&n); t := n.Time()`) is more verbose than `Scan(&t)`.
- One-call write pattern (`Exec(..., pgnanos.From(t))`) is one wrapper call more than `Exec(..., t)`.
- Contributors hit compile errors until they learn the pattern.

**Neutral:**

- Nullable columns use `*pgnanos.Time` (pointer); pgx and `database/sql` handle `nil` correctly. No `pgnanos.NullTime` variant is needed.
- The package is pure stdlib (`database/sql/driver` + `time`); no pgx pool configuration required at bootstrap.

## Implementation

```go
package pgnanos

import (
    "database/sql/driver"
    "fmt"
    "time"
)

type Time time.Time

func From(t time.Time) Time { return Time(t.UTC()) }

func (n Time) Time() time.Time { return time.Time(n) }
func (n Time) IsZero() bool    { return time.Time(n).IsZero() }

func (n *Time) Scan(src any) error {
    switch v := src.(type) {
    case int64:
        *n = Time(time.Unix(0, v).UTC())
        return nil
    case nil:
        *n = Time{}
        return nil
    default:
        return fmt.Errorf("pgnanos.Time: cannot scan %T", src)
    }
}

func (n Time) Value() (driver.Value, error) {
    return time.Time(n).UnixNano(), nil
}
```

Full implementation plus tests in `internal/pgnanos/`. Phase 0 of `holomush-gfo6` ships the package.

## References

- Spec: `docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md` §3 (pgnanos helper package design)
- Implementation plan: `docs/superpowers/plans/2026-05-22-nanosecond-timestamps.md` Task 1 (Phase 0)
- `sql.Scanner` interface: https://pkg.go.dev/database/sql#Scanner
- `driver.Valuer` interface: https://pkg.go.dev/database/sql/driver#Valuer
