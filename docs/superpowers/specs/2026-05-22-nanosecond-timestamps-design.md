# Nanosecond-Resolution Timestamps — Design

> **Status:** Design draft
> **Owner:** Sean Brandt
> **Bead:** `holomush-gfo6`
> **Date:** 2026-05-22
> **RFC2119:** This document uses MUST / MUST NOT / SHOULD / SHOULD NOT / MAY per [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).
> **ADRs:** [`holomush-absb`](../../adr/holomush-absb-bigint-epoch-nanoseconds-over-timestamp9.md) (BIGINT over timestamp9), [`holomush-rbw6`](../../adr/holomush-rbw6-pgnanos-time-named-type-seam.md) (pgnanos.Time seam), [`holomush-f5h0`](../../adr/holomush-f5h0-aad-byte-equality-structural-guarantee.md) (AAD structural guarantee, supersedes INV-P7-16)

## Section 1 — Goal and chosen approach

### Goal

Standardize the holomush persistence layer on **nanosecond-resolution time** so that:

1. The `Truncate(time.Microsecond)` discipline disappears (138 call sites across 19 files + 2 production sites + 1 documented `time.Sleep(50 * time.Millisecond)` tie-prevention hack).
2. The AAD invariant `INV-P7-16` (byte-equal AAD reconstruction at audit-read time) becomes a **structural property of the column type**, not a code-review obligation.
3. The codebase's already-ns-native posture at the AAD, wire-internal, and test-helper layers extends end-to-end down to persistence.

### Approach: `BIGINT` storing epoch nanoseconds

All persistent timestamps MUST move from `TIMESTAMPTZ` to **`BIGINT` representing `time.UnixNano()` (UTC)**. Application code reads and writes via a thin `internal/pgnanos` helper package that wraps `time.Time` with `sql.Scanner` + `driver.Valuer`, so the boundary is type-safe and discoverable at every call site.

### Alternatives evaluated and rejected

| Concern | A — `timestamp9` extension | **B — `BIGINT` nanos (chosen)** | C — status quo + discipline |
|---|---|---|---|
| Docker image | Custom (alpine + extension) | `postgres:18-alpine` unchanged | unchanged |
| pgx wiring | Custom `Codec` + OID lookup | None (native `int64`) | None |
| Native PG semantics (`ORDER BY`, `NOW()`, intervals) | Yes | No (epoch math in SQL) | Yes |
| Portability to managed PG | Bad (extension dep) | Excellent | Excellent |
| AAD byte-equality | Structural | Structural | Discipline-dependent |
| Maintenance liability | Track timestamp9 + image | None new | 138 `Truncate` sites forever |
| Already in codebase? | No | Yes (AAD `aad.go:74`, BIGINT `js_seq`, emit-token tests) | Yes |

**Why B over A.** The ergonomic loss in ad-hoc SQL (no native interval math, no `NOW()`-readable timestamps in `psql`) is real but small — holomush does not query its event-history and audit tables via ad-hoc SQL; they are accessed programmatically. The portability win is large and asymmetric: B→A is cheap if we ever want it (mechanical migration), A→B is painful (rip out pgx Codec + custom image). B also avoids a permanent maintenance liability (Docker image + extension version tracking) for a problem the language already speaks: the AAD layer at `internal/eventbus/crypto/aad/aad.go:74` already serializes `event.Timestamp.AsTime().UnixNano()` as an `int64` BE blob. The codebase is already ns-native at exactly the layer that matters.

**Why B over C.** C documents around the problem and leaves AAD byte-equality as a discipline obligation that every future contributor must internalize. B makes the column type structurally guarantee what we currently enforce by code review.

### Out of scope

- **Web wire format.** `web/src/lib/connect/holomush/{core,plugin,web}/v1/*_pb.ts` carries timestamps as `int64` epoch **milliseconds** by ergonomic convention (JS `Date` API takes ms). A separate follow-up bead investigates whether the wire format should also carry nanoseconds.
- Cross-system clock synchronization.
- Introducing `(timestamp, monotonic-seq)` tuples for tie-free ordering. If ns ties prove to be a problem in practice (statistically vanishingly rare on real hardware), file a follow-up at that point.

---

## Section 2 — Affected surface inventory

### Schema: 54 `TIMESTAMPTZ` columns across two migration trees

| Migration tree | Files | Columns |
|---|---|---|
| `internal/store/migrations/` (host) | 9 up-files | **46** |
| `plugins/core-scenes/migrations/` | 4 up-files | **8** |

**Host migrations:**

| Migration | Columns |
|---|---|
| `000001_baseline.up.sql` | 33 (players, characters, locations, exits, objects, scene_participants, player_sessions, password_resets, sessions, session_connections, events, access_policies ×3, content_items, system_aliases, player_aliases, entity_properties) |
| `000009_create_events_audit.up.sql` | 2 (`timestamp`, `inserted_at`) |
| `000013_create_crypto_keys.up.sql` | 2 (`created_at`, `rotated_at`) |
| `000015_create_player_character_bindings.up.sql` | 2 (`created_at`, `ended_at`) |
| `000016_crypto_keys_destroyed_at.up.sql` | 1 (`destroyed_at`) |
| `000018_create_plugins.up.sql` | 3 (`first_seen_at`, `last_seen_at`, `gc_at`) |
| `000019_create_player_totp.up.sql` | 6 (`enrolled_at`, `last_verified_at`, `locked_until`, recovery `created_at`/`consumed_at`, `consumed_at`) |
| `000020_create_admin_approvals.up.sql` | 3 (`expires_at`, `approved_at`, `created_at`) |
| `000037_add_session_history_floor_columns.up.sql` | 2 (`location_arrived_at`, `guest_character_created_at`) |

**Plugin migrations (core-scenes):**

| Migration | Columns |
|---|---|
| `000001_scenes.up.sql` | 3 (`created_at`, `ended_at`, `archived_at`) |
| `000003_scene_participants_and_ops_events.up.sql` | 2 (`joined_at`, `occurred_at`) |
| `000004_create_scene_log.up.sql` | 2 (`timestamp`, `inserted_at`) |
| `000006_pose_order_metadata.up.sql` | 1 (`last_pose_at`) |

### Production code touch points (2 sites — both DELETE the truncate)

- `internal/eventbus/publisher.go:202` — `event.Timestamp.Truncate(time.Microsecond)` before AAD build and envelope marshal.
- `internal/grpc/server.go:1100` (`dispatchDelivery`) — `.Truncate(time.Microsecond)` applied to `streamScopeFloor`'s return value: `floor := streamScopeFloor(currentInfo, legacyStream).Truncate(time.Microsecond)`. `streamScopeFloor` itself is untouched; only the truncation of its return value at the comparison site is deleted.

### Test cleanup: 138 `Truncate(time.Microsecond)` sites across 19 files

| Subsystem | Files | Count |
|---|---|---|
| `internal/world/postgres/` | 5 test files | 75 |
| `internal/auth/postgres/` | 2 test files | 41 |
| `internal/eventbus/` (publisher, history, crypto/dek) | 5 test files | 8 |
| `internal/totp/repo_integration_test.go` | 1 | 4 |
| `internal/store/player_session_store_test.go` | 1 | 5 |
| `internal/grpc/` | 3 test files | 5 (including 1 production site listed above) |
| `plugins/core-scenes/poseorder_integration_test.go` | 1 | 4 |
| `test/integration/` (scenes + crypto operator validation) | 2 | 2 |

### Time-tie hack to remove

- `test/integration/privacy/privacy_test.go:137-141` — `time.Sleep(50 * time.Millisecond)` with the documented rationale: `"Brief gap so guest B's SessionCreatedAt is strictly later than guest A's emit timestamp. The embedded bus publish is synchronous, but the wall-clock advance ensures unambiguous ordering when sub-millisecond co-occurrence could tie timestamps."` Becomes unnecessary at ns resolution; replaced by `>=`-floor semantics where the test asserts ordering.

### Subsystem groupings: phased rollout

| Phase | Scope | Rationale |
|---|---|---|
| **0** — `internal/pgnanos` helper | New package (`pgnanos.Time` with `sql.Scanner` + `driver.Valuer`); unit tests | Precursor; all later phases import it |
| **1** — Crypto-correctness | Publisher truncate DELETE (`publisher.go:202`) + `dispatchDelivery` truncate DELETE (`server.go:1100`) + retire `internal/grpc/subscribe_loop_test.go::TestDispatchDeliveryForwardsEventTruncatedWithinSameMicrosecondAsFloor` (regression lock for the µs-truncation behavior being eliminated — premise becomes invalid post-Phase-1) + events_audit + crypto_keys + all 8 core-scenes columns + poseorder/publisher/history tests + `privacy_test.go:141` sleep removal + INV-P7-16 strengthening | Single PR. The publisher and floor truncations MUST land together; dropping one without the other leaves the comparison invariant broken. Plugin-scene migration ships in the same PR because of the INV-P7-16 plugin-path coupling. |
| **2** — `auth/postgres` | players, password_resets, sessions, etc. (41 test sites) | Parallel-safe after Phase 0+1 |
| **3** — `world/postgres` | characters, locations, exits, objects, entity_properties (75 test sites; largest cleanup) | Parallel-safe |
| **4** — `totp` + misc | player_totp, recovery_codes, crypto_bootstrap_state, admin_approvals, plugins table | Parallel-safe |
| **5** — docs + lint guard | `task lint:no-timestamptz`, `task lint:no-microsecond-truncate`, `task lint:no-unixnano-in-repos`, update `site/docs/contributing/database-migrations.md` | Prevent regression |

---

## Section 3 — `internal/pgnanos` helper package design

The seam between Go `time.Time` and `BIGINT`-nanos columns. The package is intentionally tiny — its job is to make the conversion **typed, single-direction-correct, and visible at call sites** without driving every repo to write its own scanner.

### Public surface (~60 LoC)

```go
package pgnanos

import (
    "database/sql/driver"
    "fmt"
    "time"
)

// Time is the canonical scan/insert seam for BIGINT-epoch-nanosecond columns.
// Construct via From; read via Time().
type Time time.Time

// From constructs a Time from a time.Time, preserving nanosecond precision.
// Caller MUST NOT have already truncated t (see INV-TS-3).
func From(t time.Time) Time { return Time(t.UTC()) }

// Time returns the underlying time.Time in UTC.
func (n Time) Time() time.Time { return time.Time(n) }

// IsZero reports whether the underlying time is the zero value.
func (n Time) IsZero() bool { return time.Time(n).IsZero() }

// Scan implements sql.Scanner. Accepts int64 (the column's native type)
// and treats it as nanoseconds since UNIX epoch (UTC).
func (n *Time) Scan(src any) error {
    switch v := src.(type) {
    case int64:
        *n = Time(time.Unix(0, v).UTC())
        return nil
    case nil:
        *n = Time{}
        return nil
    default:
        return fmt.Errorf("pgnanos: cannot scan %T into pgnanos.Time", src)
    }
}

// Value implements driver.Valuer. Emits int64 nanoseconds since UNIX epoch.
// Zero time.Time values emit 0; callers MUST distinguish "unset" via column
// nullability (use *pgnanos.Time for nullable columns), not via the in-band
// zero.
func (n Time) Value() (driver.Value, error) {
    return time.Time(n).UnixNano(), nil
}
```

### Design choices and rationale

| Choice | Rationale |
|---|---|
| Named type, not a `(Insert, Scan)` function pair | Surfaces the conversion in the type signature. `Scan(&t)` on a `BIGINT` column with `t time.Time` is a compile error — the call site MUST write `&nanos` where `nanos` is a `pgnanos.Time`. The type system catches direction errors at compile time. |
| `Scan` accepts only `int64` (not `[]byte` or `string`) | pgx hands us `int64` directly in binary format. Refusing `string` prevents silent acceptance of `pg_dump`-style textual output that would lose nanoseconds. |
| `Value()` emits `int64` (not formatted string) | Matches pgx binary-format expectations; no parsing on the PG side. |
| No `Null<T>` variant | Use `*pgnanos.Time` (pointer) for nullable columns. pgx + `database/sql` handle `nil` correctly. Avoids the doubled API surface that `database/sql.NullTime` carries. |
| No `pgtype.Codec` registration | Would hijack ALL `BIGINT` columns. The codebase has legitimate non-nanosecond `BIGINT` columns (`events_audit.js_seq`, `crypto_keys.dek_ref`, `schema_ver`). The seam stays opt-in. |

### Usage at call sites (representative)

```go
// SELECT — before
var createdAt time.Time
err := pool.QueryRow(ctx, `SELECT created_at FROM players WHERE id=$1`, id).
    Scan(&createdAt)

// SELECT — after
var createdAt pgnanos.Time
err := pool.QueryRow(ctx, `SELECT created_at FROM players WHERE id=$1`, id).
    Scan(&createdAt)
t := createdAt.Time()

// INSERT — before
_, err := pool.Exec(ctx,
    `INSERT INTO players (..., created_at) VALUES (..., $5)`, ..., t)

// INSERT — after
_, err := pool.Exec(ctx,
    `INSERT INTO players (..., created_at) VALUES (..., $5)`,
    ..., pgnanos.From(t))
```

### Tests

Unit tests in `internal/pgnanos/pgnanos_test.go`:

- `TestScanInt64ReturnsTimeAtNanosecondPrecision` — scan `int64` → `pgnanos.Time` with sub-µs nanos preserved.
- `TestScanNilReturnsZeroTime` — `Scan(nil)` → `pgnanos.Time{}`.
- `TestScanWrongTypeReturnsErrorWithType` — `Scan("string")` → error containing `string`.
- `TestValueZeroTimeReturnsZero` — `Value()` on zero `pgnanos.Time` returns `int64(0)`.
- `TestValueSpecificTimeReturnsExpectedNanoseconds` — golden test against `time.Unix(N, M).UnixNano()`.
- `TestRoundTripPreservesSubMicrosecondNanoseconds` — `From(t).Value()` then `Scan` → byte-equal to original.

The package is pure stdlib — no pgx pool config required. pgx picks up `sql.Scanner` and `driver.Valuer` automatically.

---

## Section 4 — Migration mechanics

One paired up/down migration per phase. Each phase's migration is mechanically generated from the `(table, column, nullability, default)` tuples in its scope.

### Up-path SQL template

```sql
-- Example: 000NNN_eventbus_timestamps_to_bigint.up.sql
-- (NNN assigned at PR-open time; next free at time of writing is 000038.)

ALTER TABLE events_audit
    ALTER COLUMN timestamp
        TYPE BIGINT USING (EXTRACT(EPOCH FROM timestamp) * 1e9)::BIGINT,
    ALTER COLUMN inserted_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM inserted_at) * 1e9)::BIGINT;

-- Drop the TIMESTAMPTZ default; add a BIGINT one.
ALTER TABLE events_audit
    ALTER COLUMN inserted_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE crypto_keys
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT,
    ALTER COLUMN rotated_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM rotated_at) * 1e9)::BIGINT,
    ALTER COLUMN destroyed_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM destroyed_at) * 1e9)::BIGINT;

ALTER TABLE crypto_keys
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;
```

### Default-expression handling

`TIMESTAMPTZ DEFAULT NOW()` becomes `BIGINT DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT`. PostgreSQL evaluates this on every insert. The in-PostgreSQL clock has microsecond resolution, so **DB-side `DEFAULT` values are µs-truncated even with the BIGINT column**. App-supplied timestamps via `pgnanos.From(time.Now())` carry full Go `time.Now().UnixNano()` precision.

This split is intentional:

- **DB-side `DEFAULT now()`** is a convenience for `inserted_at`-style columns where µs is fine.
- **Correctness-critical columns** (the event `timestamp` field bound by AAD, scope floor inputs) MUST be app-supplied — the spec's invariant section makes this explicit.

### Down-path SQL template (precision-lossy)

```sql
-- 000NNN_eventbus_timestamps_to_bigint.down.sql

ALTER TABLE events_audit
    ALTER COLUMN timestamp
        TYPE TIMESTAMPTZ USING to_timestamp(timestamp::double precision / 1e9),
    ALTER COLUMN inserted_at
        TYPE TIMESTAMPTZ USING to_timestamp(inserted_at::double precision / 1e9);

ALTER TABLE events_audit
    ALTER COLUMN inserted_at SET DEFAULT now();

-- (and so on, mirroring up)
```

Every down migration's leading comment MUST contain the line:

> `-- DOWN MIGRATION IS PRECISION-LOSSY. Recovers TIMESTAMPTZ semantics but truncates ns → µs. No backfill of pre-down-migration data is provided.`

### No backfill

Per repo posture (no production deployment), the migration runs against test databases (testcontainers + dev compose) only. When production rollout eventually happens, the schema starts BIGINT — the migration is a no-op on empty tables.

### Plugin migration coupling

`plugins/core-scenes/migrations/` runs through the same `internal/store/migrate` machinery (plugins declare their own migration directory in `plugin.yaml`). The Phase 1 PR ships changes in BOTH `internal/store/migrations/` AND `plugins/core-scenes/migrations/` because of the INV-P7-16 coupling. Plugin migrations run after host migrations by the plugin-loader contract; the spec relies on that ordering.

### Lint guards (Phase 5)

| Task | Implementation | Escape hatch |
|---|---|---|
| `task lint:no-timestamptz` | `rg "TIMESTAMPTZ\|TIMESTAMP WITH TIME ZONE\|TIMESTAMP\b"` over `internal/store/migrations/*.up.sql` and `plugins/*/migrations/*.up.sql` | `-- pgnanos-exempt: <reason>` on the same line |
| `task lint:no-microsecond-truncate` | `rg "Truncate\(time\.Microsecond\)"` over `**/*.go` | `// pgnanos-exempt: <reason>` on the same line |
| `task lint:no-unixnano-in-repos` | `rg "UnixNano\(\)\|time\.Unix\(0,"` over `internal/{auth,world,totp}/postgres/`, `internal/eventbus/history/`, `plugins/*/` excluding `*_test.go` and `internal/pgnanos/` | `// pgnanos-exempt: <reason>` |

---

## Section 5 — Invariants

| ID | Invariant | Test(s) |
|---|---|---|
| **INV-TS-1** | All persistent time values MUST be stored as `BIGINT` representing nanoseconds since UNIX epoch (UTC). No new migrations MAY add `TIMESTAMPTZ` or `TIMESTAMP` columns. | Lint: `task lint:no-timestamptz` greps `*.up.sql`. Escape hatch: `-- pgnanos-exempt: <reason>`. Meta-test: `internal/store/migrate_integration_test.go::TestNoTimestamptzColumnsAfterMigration` queries `information_schema.columns` after running all migrations and asserts zero rows where `data_type IN ('timestamp without time zone', 'timestamp with time zone')` in holomush-owned schemas. |
| **INV-TS-2** | `pgnanos.Time` MUST be the canonical scan/insert seam between Go `time.Time` and `BIGINT` epoch-ns columns. Direct `int64` ↔ `time.Time` arithmetic in repo code is prohibited outside the `pgnanos` package. | Unit: `internal/pgnanos/pgnanos_test.go::TestRoundTripPreservesSubMicrosecondNanoseconds`. Lint: `task lint:no-unixnano-in-repos`. |
| **INV-TS-3** | After migration, application code (production and tests) MUST NOT call `Truncate(time.Microsecond)` on any `time.Time` round-tripping through PG. | Lint: `task lint:no-microsecond-truncate`. Escape hatch: `// pgnanos-exempt: <reason>` (expected zero uses; reserved for hypothetical non-PG external systems requiring µs). |
| **INV-TS-4** | `internal/eventbus/publisher.go::Publish` MUST NOT truncate `event.Timestamp` before AAD construction or envelope marshal. The on-wire timestamp MUST carry full nanosecond precision. | `internal/eventbus/publisher_test.go::TestPublisherPreservesNanoseconds` — publish an event with sub-µs nanos, subscribe, assert `received.Timestamp.Nanosecond() % 1000 != 0`. Replaces `TestPublisherTruncatesTimestampToMicrosecond`. |
| **INV-TS-5** | AAD round-trip from publish → DB persist → audit read → AAD reconstruction MUST be byte-equal at full nanosecond resolution. (Strengthens the former INV-P7-16 from "µs-discipline-dependent byte-equal" to "structurally byte-equal at ns resolution.") | Update `internal/eventbus/history/plugin_aad_reconstruction_test.go`: remove the four `Truncate(time.Microsecond)` calls modeling publisher and PG truncation; assert byte-equal AAD with sub-µs ns timestamps. Add `TestRoundTripPreservesAADWithSubMicrosecondNanos`. |
| **INV-TS-6** | Privacy/scope floor comparisons (the floor produced by `streamScopeFloor` and consumed in `dispatchDelivery`, history `WHERE` predicates) MUST operate at nanosecond resolution. Floor inputs (`LocationArrivedAt`, `GuestCharacterCreatedAt`, `FocusMembership.JoinedAt`) MUST be stored and compared without truncation. The `.Truncate(time.Microsecond)` call at `internal/grpc/server.go:1100` (`dispatchDelivery`, applied to `streamScopeFloor`'s return value) is **deleted, not replaced with a stub**. | `internal/grpc/subscribe_loop_test.go::TestDispatchDeliveryDropsEventEmittedInSameNanosecondAsArrival` — construct an event with `Timestamp` exactly one nanosecond below `LocationArrivedAt`; assert it is excluded by the floor comparison. (Placed in `subscribe_loop_test.go` because that's where the existing coverage at this layer lives — the comparison happens in `dispatchDelivery`, not in the pure `streamScopeFloor` computation function.) |
| **INV-TS-7** | Sub-microsecond timestamp **ties** (two `time.Now().UnixNano()` calls returning the same value) MUST be resolvable deterministically. The privacy floor MUST use `>=` (not `>`) semantics so that an event emitted at the exact same nanosecond as the floor is **included** in the visible window. | `internal/grpc/subscribe_loop_test.go::TestDispatchDeliveryIncludesEventAtExactFloorNanosecond` — same-layer placement as INV-TS-6 (comparison happens in `dispatchDelivery`). Replaces the `time.Sleep(50ms)` defensive hack at `test/integration/privacy/privacy_test.go:141` with a deterministic-clock test. |
| **INV-TS-9** | The `TIMESTAMPTZ`→`BIGINT` conversion migrations (`000038`, `000041`, `000042`, `000043`, `000044`) MUST NOT error on pre-existing data outside the int64-nanosecond range or on `±infinity`. Out-of-range and infinity values MUST saturate to the int64 bounds (`9223372036854775807` ≈ 2262-04-11 for `+infinity`/overflow, `-9223372036854775808` ≈ 1677-09-21 for `-infinity`/underflow); `NULL` MUST pass through unchanged; in-range values MUST convert to exact `UnixNano`. The conversion arithmetic MUST be performed in `numeric` (not `double precision`) so the int64 bound is exactly representable and `numeric`-`Infinity` clamps cleanly; the `USING` clause MUST `NULL`-guard explicitly because `LEAST`/`GREATEST` ignore `NULL` inputs. The `SET DEFAULT (… now() …)` clauses are left as-is — `now()` cannot overflow. (Backfills the gap that wedged the sandbox deploy in `holomush-0b3ec`.) | `internal/store/migrate_clamp_integration_test.go` Ginkgo spec `INV-TS-9: TIMESTAMPTZ→BIGINT conversion saturates out-of-range and infinity to int64-ns bounds and preserves NULL` (suite-registered under `TestStore`) steps each migration over seeded boundary/infinity/NULL rows and asserts saturation + exact in-range conversion + NULL passthrough. |
| **INV-TS-META** | The spec's invariant table MUST be in sync with the tests cited. | A new meta-test (location TBD during Phase 0 — likely `internal/pgnanos/spec_meta_test.go` or `internal/store/spec_meta_test.go`) uses a **hardcoded `cases` slice** mapping each `INV-TS-N` to its named test, then walks the repo's `*_test.go` corpus via `go/parser` to enumerate top-level `Test*` function names and asserts every named test exists somewhere in the tree. The `cases` slice and the spec's invariant table MUST be updated together; drift between them is the failure mode this test catches. Mirrors the mechanism (NOT the file-parsing fiction the previous spec draft described) used at `internal/eventbus/history/phase7_boundary_meta_test.go:47::TestPhase7InvariantsHaveNamedTests`. Pure-Go (no `rg` dependency) so the test runs in any environment with a Go toolchain. |

---

## Section 6 — Risk register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Direction error in repo code — `pgnanos.Scan(&t)` when `pgnanos.From(t)` was intended; comparing `int64`-nanos against `time.Time` | Medium during migration, low after | Silent data corruption (off-by-1e9 timestamps), schema-constraint violations | The `pgnanos.Time` named type makes both directions a compile error if mismatched. Phase 0 lands round-trip tests. Each phase PR review scans for `time.Time` ↔ `int64` arithmetic outside `pgnanos/`. |
| PG-side `DEFAULT (extract(epoch from now()) * 1e9)::BIGINT` precision — PG `now()` is µs-resolution, so DB-side defaults emit µs-truncated BIGINT values | High (every `DEFAULT NOW()` column) | Low — only `inserted_at`/`updated_at`-style columns use DB defaults; AAD-bound `event.Timestamp` is app-supplied | Documented explicitly in this spec. Phase 1 audits every TIMESTAMPTZ column and flags any DB-default usage on a correctness-critical column. |
| Custom Go code paths bypass `pgnanos` — internal tools/scripts/future PRs that write raw `int64` to a BIGINT-nanos column | Medium long-term | Drift between repo and tool views of the same column | INV-TS-2 lint catches production paths. Tests are exempt (test fixtures legitimately construct nanos directly, e.g., `phase3TestSetup.InsertEncryptedRow`). |
| Plugin migration coupling — host-vs-plugin migration order changes break Phase 1 atomicity | Low | Medium — Phase 1 PR splits across both trees; partial rollback leaves one tree migrated and the other not | Verify plugin migration ordering before Phase 1 lands; document the dependency in the Phase 1 PR description. Plugin loader guarantees host migrations finish before plugin migrations start. |
| Sub-ns ties on real hardware — two `time.Now().UnixNano()` calls genuinely return the same value | Very low on modern hardware, non-zero | Floor-tie ambiguity: same-ns event with the floor either always-included or always-excluded depending on `>=` vs `>` | INV-TS-7 mandates `>=` floor semantics. If a real scenario ever needs strict-after, file a follow-up to introduce `(timestamp, monotonic-seq)` tuples — defer until empirically forced. |
| pgx nullable-column scanning — `*pgnanos.Time` for nullable columns may interact poorly with pgx NULL handling | Low | Medium — runtime errors at first use of a nullable column | Phase 0 unit tests cover `nil` scan and `(*pgnanos.Time)(nil)` value paths. First nullable column in Phase 1 (`crypto_keys.rotated_at`) acts as the canary. |
| `task pr-prep` impact — each migration touches 5–15 column ALTERs; an undetected mistype somewhere fails E2E with a confusing error | Low | Low (caught by CI) | Per-phase PRs run `task pr-prep` to completion (full lane); the AAD round-trip integration test is the canary that ties everything together. |

---

## Section 7 — Follow-up beads

Filed when this spec is approved by `design-reviewer`. P-priorities are drafts; final values set at `plan-to-beads` time.

| Title | Type | Priority | Notes |
|---|---|---|---|
| Phase 0 — Add `internal/pgnanos` helper package | task | P1 | Precursor; blocks later phases. ~60 LoC + tests. |
| Phase 1 — Crypto-correctness: timestamps to ns end-to-end | epic | P1 | Eventbus publisher + `streamScopeFloor` + events_audit + crypto_keys + `plugins/core-scenes` + `privacy_test.go` sleep removal + INV-P7-16 strengthening. Single PR. |
| Phase 2 — `auth/postgres` + sessions table ns migration | task | P2 | players, password_resets, sessions (all timestamp columns including `location_arrived_at` and `guest_character_created_at` from migration 000037 — converted atomically with the rest of the table to avoid mixed-type interim state in `session_store.go`), session_connections, player_sessions. ~46 test `Truncate` sites (41 in auth/postgres + 5 in store/player_session_store_test.go). |
| Phase 3 — `world/postgres` ns migration | task | P2 | characters, locations, exits, objects, entity_properties. 75 test sites. |
| Phase 4 — `totp` + misc ns migration | task | P2 | player_totp, recovery_codes, crypto_bootstrap_state, admin_approvals, plugins table. |
| Phase 5 — docs + lint guard | task | P2 | The three new `task lint:*` targets; update `site/docs/contributing/database-migrations.md`. |
| Investigate Connect-Web wire format ns-throughout | task | P3 | Successor to dropped INV-TS-8. Owns the JS `Date` ergonomics + per-consumer conversion question for the proto/web layer. |
| (Conditional) `(timestamp, monotonic-seq)` tuple for strict-after ordering | task | P3 — only if forced | Defer until empirically needed. |

---

## References

- Bead `holomush-gfo6` — investigation tracking
- `internal/eventbus/crypto/aad/aad.go:62-117` — AAD canonical encoding (already ns-native)
- `internal/eventbus/publisher.go:202` — current `Truncate(time.Microsecond)` site
- `internal/grpc/server.go:1100` (`dispatchDelivery`) — current floor truncation applied to `streamScopeFloor`'s return value
- `internal/grpc/subscribe_loop_test.go:326::TestDispatchDeliveryForwardsEventTruncatedWithinSameMicrosecondAsFloor` — regression lock for the µs-truncation behavior; retired in Phase 1
- `internal/eventbus/history/phase7_boundary_meta_test.go:47::TestPhase7InvariantsHaveNamedTests` — meta-test pattern mirrored by `INV-TS-META`
- `docs/superpowers/specs/2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md` — INV-P7-16 (strengthened to INV-TS-5 here)
- `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md` — floor-comparison precision contract
- `test/integration/privacy/privacy_test.go` — `time.Sleep(50ms)` tie-prevention hack
- `internal/pgnanos/` — new helper package (Phase 0)
- pigsty.io/ext/e/timestamp9/ — alternative A reference (rejected)
- pkg.go.dev/github.com/jackc/pgx/v5/pgtype — pgx Codec interface (referenced for alternative A)

<!-- adr-capture: sha256=f61b0e65d7ec5496; session=brainstorm-gfo6; ts=2026-05-22T15:07:38Z; adrs=holomush-absb,holomush-rbw6,holomush-f5h0 -->
