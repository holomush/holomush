<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Nanosecond-Resolution Timestamps Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `dev-flow:subagent-driven-development` (recommended) or `dev-flow:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate all 54 persistent `TIMESTAMPTZ` columns to `BIGINT` storing epoch nanoseconds, with a `internal/pgnanos` helper as the type-safe scan/insert seam, so that AAD byte-equality (INV-P7-16 → INV-TS-5) becomes structural and the 138 `Truncate(time.Microsecond)` discipline disappears.

**Architecture:** Phase 0 ships the `internal/pgnanos` helper (precursor). Phase 1 ("crypto-correctness") atomically lands publisher + floor truncate deletions, both migration trees' crypto-bound columns, AAD/floor test updates, and the privacy_test sleep removal as a single PR. Phases 2-4 are parallel-safe ergonomic cleanups (auth, world, totp+misc). Phase 5 wires lint guards to prevent regression.

**Tech Stack:** Go 1.22+, PostgreSQL 18 (postgres:18-alpine, self-hosted), pgx v5, `database/sql/driver` (Scanner/Valuer), Ginkgo/Gomega for integration tests, testify for unit tests, testcontainers-go.

**Spec:** [`docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md`](../specs/2026-05-22-nanosecond-timestamps-design.md)
**Design bead:** `holomush-gfo6`

---

## Phase 0: pgnanos Helper Package

### Task 1: Create `internal/pgnanos` package with type, Scan, Value, and tests

**Files:**

- Create: `internal/pgnanos/doc.go`
- Create: `internal/pgnanos/pgnanos.go`
- Create: `internal/pgnanos/pgnanos_test.go`

- [ ] **Step 1: Create the package directory**

Run: `mkdir -p internal/pgnanos`
Expected: directory created (no output).

- [ ] **Step 2: Write the package doc comment**

Create `internal/pgnanos/doc.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package pgnanos is the canonical scan/insert seam between Go time.Time
// and BIGINT epoch-nanosecond columns.
//
// HoloMUSH stores all persistent timestamps as BIGINT representing
// nanoseconds since UNIX epoch (UTC). This package wraps time.Time with
// sql.Scanner + driver.Valuer so the boundary is type-safe and visible
// at every call site:
//
//	var createdAt pgnanos.Time
//	err := pool.QueryRow(ctx, `SELECT created_at FROM x WHERE id=$1`, id).
//	    Scan(&createdAt)
//	t := createdAt.Time()
//
//	_, err = pool.Exec(ctx,
//	    `INSERT INTO x (..., created_at) VALUES (..., $5)`,
//	    ..., pgnanos.From(time.Now()))
//
// See docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md
// (INV-TS-1 through INV-TS-7) for the spec this package serves.
package pgnanos
```

- [ ] **Step 3: Write the failing tests**

Create `internal/pgnanos/pgnanos_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pgnanos_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/pgnanos"
)

func TestScanInt64ReturnsTimeAtNanosecondPrecision(t *testing.T) {
	var got pgnanos.Time
	require.NoError(t, got.Scan(int64(1700000000123456789)))
	assert.Equal(t, time.Unix(1700000000, 123456789).UTC(), got.Time())
}

func TestScanNilReturnsZeroTime(t *testing.T) {
	var got pgnanos.Time
	require.NoError(t, got.Scan(nil))
	assert.True(t, got.IsZero())
}

func TestScanWrongTypeReturnsErrorWithType(t *testing.T) {
	var got pgnanos.Time
	err := got.Scan("not-an-int64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "string")
	assert.Contains(t, err.Error(), "pgnanos.Time")
}

func TestValueZeroTimeReturnsZero(t *testing.T) {
	var zero pgnanos.Time
	v, err := zero.Value()
	require.NoError(t, err)
	assert.Equal(t, int64(0), v)
}

func TestValueSpecificTimeReturnsExpectedNanoseconds(t *testing.T) {
	const wantNanos = int64(1700000000123456789)
	in := pgnanos.From(time.Unix(0, wantNanos))
	v, err := in.Value()
	require.NoError(t, err)
	assert.Equal(t, wantNanos, v)
}

func TestRoundTripPreservesSubMicrosecondNanoseconds(t *testing.T) {
	orig := time.Date(2026, 5, 22, 12, 34, 56, 123456789, time.UTC)
	in := pgnanos.From(orig)
	v, err := in.Value()
	require.NoError(t, err)

	var out pgnanos.Time
	require.NoError(t, out.Scan(v))
	assert.Equal(t, orig, out.Time(), "round-trip MUST preserve sub-µs nanos")
	assert.Equal(t, 789, out.Time().Nanosecond()%1000,
		"sub-µs ns component MUST survive Value+Scan round-trip")
}

func TestFromConvertsToUTC(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	in := time.Date(2026, 5, 22, 12, 0, 0, 0, loc)
	got := pgnanos.From(in).Time()
	assert.Equal(t, time.UTC, got.Location(), "From MUST normalize to UTC")
	assert.True(t, in.Equal(got), "From MUST preserve instant")
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `task test -- ./internal/pgnanos/`
Expected: FAIL — `package pgnanos` not found / `undefined: pgnanos.Time`.

- [ ] **Step 5: Write the minimal implementation**

Create `internal/pgnanos/pgnanos.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pgnanos

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// Time is the canonical scan/insert seam for BIGINT-epoch-nanosecond
// columns. Construct via From; read via Time().
type Time time.Time

// From constructs a Time from a time.Time, preserving nanosecond
// precision and normalizing to UTC. Callers MUST NOT have already
// truncated t (INV-TS-3).
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
		return fmt.Errorf("pgnanos.Time: cannot scan %T", src)
	}
}

// Value implements driver.Valuer. Emits int64 nanoseconds since UNIX
// epoch. Zero time.Time values emit 0; callers MUST distinguish "unset"
// via column nullability (use *pgnanos.Time for nullable columns), not
// via the in-band zero.
func (n Time) Value() (driver.Value, error) {
	return time.Time(n).UnixNano(), nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `task test -- ./internal/pgnanos/`
Expected: PASS — 7 tests pass.

- [ ] **Step 7: Run lint**

Run: `task lint`
Expected: no violations in `internal/pgnanos/`.

- [ ] **Step 8: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`. Suggested message:

```text
feat(pgnanos): add internal/pgnanos helper for BIGINT-ns ↔ time.Time

Precursor to nanosecond-timestamps migration (gfo6). Phase 0/5.

Introduces pgnanos.Time as the canonical scan/insert seam for
BIGINT-epoch-nanosecond columns. Subsequent phases convert
TIMESTAMPTZ columns to BIGINT and route through this seam.

Refs: docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md
Bead: holomush-gfo6
```

---

## Phase 1: Crypto-Correctness (single atomic PR)

This phase MUST land as a single PR. Dropping the publisher truncate without dropping the floor truncate (or vice versa) breaks the comparison invariant. Plugin migration ships in the same PR because INV-P7-16 (now INV-TS-5) covers the plugin path.

### Task 2: Host migration — convert `events_audit` and `crypto_keys` to BIGINT

**Files:**

- Create: `internal/store/migrations/000NNN_eventbus_crypto_timestamps_to_bigint.up.sql` (NNN = next free; check `ls internal/store/migrations/ | sort -t_ -k1 -n | tail -3` at PR-open time — currently 000038)
- Create: `internal/store/migrations/000NNN_eventbus_crypto_timestamps_to_bigint.down.sql`

- [ ] **Step 1: Determine the next free migration number**

Run: `ls internal/store/migrations/ | rg -o "^[0-9]+" | sort -u | tail -3`
Expected output: shows current ceiling (e.g., `000035 / 000036 / 000037`). Use the next sequential number (e.g., `000038`).

- [ ] **Step 2: Write the up migration**

Create `internal/store/migrations/<NNN>_eventbus_crypto_timestamps_to_bigint.up.sql`:

```sql
-- Convert events_audit and crypto_keys timestamp columns from TIMESTAMPTZ
-- to BIGINT (epoch nanoseconds, UTC). See:
--   docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md
-- INV-TS-1, INV-TS-4, INV-TS-5.

ALTER TABLE events_audit
    ALTER COLUMN timestamp
        TYPE BIGINT USING (EXTRACT(EPOCH FROM timestamp) * 1e9)::BIGINT,
    ALTER COLUMN inserted_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM inserted_at) * 1e9)::BIGINT;

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

- [ ] **Step 3: Write the down migration**

Create `internal/store/migrations/<NNN>_eventbus_crypto_timestamps_to_bigint.down.sql`:

```sql
-- DOWN MIGRATION IS PRECISION-LOSSY. Recovers TIMESTAMPTZ semantics but
-- truncates ns → µs. No backfill of pre-down-migration data is provided.

ALTER TABLE events_audit
    ALTER COLUMN timestamp
        TYPE TIMESTAMPTZ USING to_timestamp(timestamp::double precision / 1e9),
    ALTER COLUMN inserted_at
        TYPE TIMESTAMPTZ USING to_timestamp(inserted_at::double precision / 1e9);

ALTER TABLE events_audit
    ALTER COLUMN inserted_at SET DEFAULT now();

ALTER TABLE crypto_keys
    ALTER COLUMN created_at
        TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9),
    ALTER COLUMN rotated_at
        TYPE TIMESTAMPTZ USING to_timestamp(rotated_at::double precision / 1e9),
    ALTER COLUMN destroyed_at
        TYPE TIMESTAMPTZ USING to_timestamp(destroyed_at::double precision / 1e9);

ALTER TABLE crypto_keys
    ALTER COLUMN created_at SET DEFAULT now();
```

- [ ] **Step 4: Verify the migration runs cleanly forward and backward**

Run: `task test:int -- ./internal/store/...`
Expected: PASS. The store-migration integration tests apply all migrations end-to-end and check column types via `information_schema`.

- [ ] **Step 5: Commit (do not push — wait until end of Phase 1)**

Suggested message:

```text
feat(store): migrate events_audit + crypto_keys timestamps to BIGINT-ns

Phase 1.1 of nanosecond-timestamps (gfo6). Down migration is
precision-lossy by design (no backfill); see spec.

Refs: docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md
Bead: holomush-gfo6
```

### Task 3: Plugin migration — convert `plugins/core-scenes` columns to BIGINT

**Files:**

- Create: `plugins/core-scenes/migrations/000007_timestamps_to_bigint.up.sql`
- Create: `plugins/core-scenes/migrations/000007_timestamps_to_bigint.down.sql`

- [ ] **Step 1: Verify the next free plugin-migration number**

Run: `ls plugins/core-scenes/migrations/ | rg -o "^[0-9]+" | sort -u | tail -3`
Expected: shows current ceiling 000006; next free is 000007.

- [ ] **Step 2: Write the up migration**

Create `plugins/core-scenes/migrations/000007_timestamps_to_bigint.up.sql`:

```sql
-- Convert core-scenes timestamp columns from TIMESTAMPTZ to BIGINT (epoch
-- nanoseconds, UTC). Plugin-side companion to host migration. INV-TS-1,
-- INV-TS-5 (plugin AAD path).

ALTER TABLE scenes
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT,
    ALTER COLUMN ended_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM ended_at) * 1e9)::BIGINT,
    ALTER COLUMN archived_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM archived_at) * 1e9)::BIGINT;

ALTER TABLE scenes
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE scene_participants
    ALTER COLUMN joined_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM joined_at) * 1e9)::BIGINT;

ALTER TABLE scene_participants
    ALTER COLUMN joined_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE ops_events
    ALTER COLUMN occurred_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM occurred_at) * 1e9)::BIGINT;

ALTER TABLE ops_events
    ALTER COLUMN occurred_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE scene_log
    ALTER COLUMN timestamp
        TYPE BIGINT USING (EXTRACT(EPOCH FROM timestamp) * 1e9)::BIGINT,
    ALTER COLUMN inserted_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM inserted_at) * 1e9)::BIGINT;

ALTER TABLE scene_log
    ALTER COLUMN inserted_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE scenes
    ALTER COLUMN last_pose_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM last_pose_at) * 1e9)::BIGINT;
```

- [ ] **Step 3: Write the down migration**

Create `plugins/core-scenes/migrations/000007_timestamps_to_bigint.down.sql`:

```sql
-- DOWN MIGRATION IS PRECISION-LOSSY. Recovers TIMESTAMPTZ semantics but
-- truncates ns → µs.

ALTER TABLE scenes
    ALTER COLUMN created_at
        TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9),
    ALTER COLUMN ended_at
        TYPE TIMESTAMPTZ USING to_timestamp(ended_at::double precision / 1e9),
    ALTER COLUMN archived_at
        TYPE TIMESTAMPTZ USING to_timestamp(archived_at::double precision / 1e9),
    ALTER COLUMN last_pose_at
        TYPE TIMESTAMPTZ USING to_timestamp(last_pose_at::double precision / 1e9);

ALTER TABLE scenes
    ALTER COLUMN created_at SET DEFAULT now();

ALTER TABLE scene_participants
    ALTER COLUMN joined_at
        TYPE TIMESTAMPTZ USING to_timestamp(joined_at::double precision / 1e9);

ALTER TABLE scene_participants
    ALTER COLUMN joined_at SET DEFAULT now();

ALTER TABLE ops_events
    ALTER COLUMN occurred_at
        TYPE TIMESTAMPTZ USING to_timestamp(occurred_at::double precision / 1e9);

ALTER TABLE ops_events
    ALTER COLUMN occurred_at SET DEFAULT now();

ALTER TABLE scene_log
    ALTER COLUMN timestamp
        TYPE TIMESTAMPTZ USING to_timestamp(timestamp::double precision / 1e9),
    ALTER COLUMN inserted_at
        TYPE TIMESTAMPTZ USING to_timestamp(inserted_at::double precision / 1e9);

ALTER TABLE scene_log
    ALTER COLUMN inserted_at SET DEFAULT now();
```

- [ ] **Step 4: Run plugin-migration tests**

Run: `task test:int -- ./test/integration/plugin/... ./plugins/core-scenes/...`
Expected: PASS — plugin migration runner applies migration cleanly.

- [ ] **Step 5: Commit**

Suggested message: `feat(core-scenes): migrate plugin timestamps to BIGINT-ns (Phase 1.2, gfo6)`

### Task 4: Delete publisher truncate + update `TestPublisherPreservesNanoseconds`

**Files:**

- Modify: `internal/eventbus/publisher.go:200-211` (delete `truncatedTimestamp` line and use `event.Timestamp` directly)
- Modify: `internal/eventbus/publisher_test.go` (replace `TestPublisherTruncatesTimestampToMicrosecond` with the new test)

- [ ] **Step 1: Write the failing replacement test**

Open `internal/eventbus/publisher_test.go`. Locate `TestPublisherTruncatesTimestampToMicrosecond` (around line 315). Replace its body with:

```go
// TestPublisherPreservesNanoseconds gates INV-TS-4. The publisher MUST
// NOT truncate the event timestamp before AAD construction or envelope
// marshal. After the BIGINT-ns migration, AAD reconstruction at read
// time receives the full-precision timestamp from PG, so byte-equal AAD
// is structurally guaranteed.
//
// This test inverts the prior TestPublisherTruncatesTimestampToMicrosecond:
// it asserts that sub-µs nanos SURVIVE the publish path.
func TestPublisherPreservesNanoseconds(t *testing.T) {
	pub, sub, _ := setupPublisherTest(t) // existing helper in this file
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessID := "test-session-" + ulid.Make().String()
	subject := eventbus.Subject("events.test.publisher.preserves.ns")
	stream, err := sub.OpenSession(ctx, sessID, testIdentity(),
		[]eventbus.Subject{subject}, time.Time{})
	require.NoError(t, err)

	ev := newTestEvent(subject)
	// Sub-µs nanosecond component on purpose.
	ev.Timestamp = time.Date(2026, 5, 14, 12, 34, 56, 123456789, time.UTC)
	require.Equal(t, 789, ev.Timestamp.Nanosecond()%1000,
		"test fixture sanity: pre-publish timestamp MUST carry sub-µs nanos")

	require.NoError(t, pub.Publish(ctx, ev))

	got, err := readOneFromStream(ctx, stream)
	require.NoError(t, err)
	assert.Equal(t, 789, got.Timestamp.Nanosecond()%1000,
		"INV-TS-4: publisher MUST preserve sub-µs nanoseconds; sub-µs digits MUST survive")
	assert.Equal(t, ev.Timestamp, got.Timestamp.UTC(),
		"INV-TS-4: published timestamp MUST equal source timestamp at full precision")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test -- -run TestPublisherPreservesNanoseconds ./internal/eventbus/`
Expected: FAIL — the current publisher truncates, so `got.Timestamp.Nanosecond() % 1000 == 0`.

- [ ] **Step 3: Delete the publisher truncate**

Open `internal/eventbus/publisher.go`. Find lines 183-211 (the block ending in `truncatedTimestamp := event.Timestamp.Truncate(time.Microsecond)` and the envelope build that uses it). Replace the truncation logic with direct use of `event.Timestamp`:

Locate this region (lines 183-211 approximately):

```go
	// Truncate the event timestamp to microsecond precision BEFORE both
	// aad.Build and envelope marshal. PostgreSQL TIMESTAMPTZ — used by
	// ... [explanatory comment block] ...
	truncatedTimestamp := event.Timestamp.Truncate(time.Microsecond)
```

Delete the entire truncation comment block + the `truncatedTimestamp` line. Then update the envelope construction that follows. Find the existing line `Timestamp: timestamppb.New(truncatedTimestamp),` and change it to:

```go
		Timestamp: timestamppb.New(event.Timestamp),
```

The full envelope construction should read (the surrounding context will already exist; only the Timestamp field changes):

```go
	envelope := &eventbusv1.Event{
		Id:        event.ID[:],
		Subject:   string(event.Subject),
		Type:      string(event.Type),
		Timestamp: timestamppb.New(event.Timestamp),
		Actor:     actorProto,
		// ...
	}
```

- [ ] **Step 4: Run the new test to verify it passes**

Run: `task test -- -run TestPublisherPreservesNanoseconds ./internal/eventbus/`
Expected: PASS — sub-µs nanos survive end-to-end.

- [ ] **Step 5: Run the full eventbus unit suite to catch regressions**

Run: `task test -- ./internal/eventbus/`
Expected: PASS — all eventbus unit tests pass.

- [ ] **Step 6: Commit**

Suggested message: `feat(eventbus): drop publisher µs-truncate; preserve full ns precision (Phase 1.3, gfo6)`

### Task 5: Delete `dispatchDelivery` floor truncate + retire stale test + add new floor tests

**Files:**

- Modify: `internal/grpc/server.go:1100` (delete the `.Truncate(time.Microsecond)` on `streamScopeFloor`'s return value)
- Modify: `internal/grpc/subscribe_loop_test.go:326` (retire `TestDispatchDeliveryForwardsEventTruncatedWithinSameMicrosecondAsFloor`; add `TestDispatchDeliveryDropsEventEmittedInSameNanosecondAsArrival` and `TestDispatchDeliveryIncludesEventAtExactFloorNanosecond`)

- [ ] **Step 1: Verify the current floor-truncate site**

Run: `rg -n "streamScopeFloor.*Truncate" internal/grpc/server.go`
Expected: one match at line ~1100: `floor := streamScopeFloor(currentInfo, legacyStream).Truncate(time.Microsecond)`

- [ ] **Step 2: Write the two failing new tests**

The existing helper at `internal/grpc/subscribe_loop_test.go:290` is `makeLocationDelivery(t *testing.T, locID string, ts time.Time) *fakeDelivery`. Tests construct a `session.Info` directly, instantiate `CoreServer{sessionStore: store}`, and call `s.dispatchDelivery(ctx, info, delivery, stream, nil)`.

Open `internal/grpc/subscribe_loop_test.go`. Add the following two test functions immediately after `TestDispatchDeliveryForwardsEventTruncatedWithinSameMicrosecondAsFloor` (which we will retire in step 5):

```go
// TestDispatchDeliveryDropsEventEmittedInSameNanosecondAsArrival gates
// INV-TS-6. The floor comparison MUST operate at nanosecond resolution.
// An event whose Timestamp is one nanosecond BELOW the floor
// (LocationArrivedAt) MUST be filtered out by dispatchDelivery.
func TestDispatchDeliveryDropsEventEmittedInSameNanosecondAsArrival(t *testing.T) {
	t.Parallel()
	locID := core.NewULID()
	arrivedAt := time.Date(2026, 5, 22, 12, 0, 0, 123456789, time.UTC)
	info := &session.Info{
		ID:                "s1",
		CharacterID:       core.NewULID(),
		LocationID:        locID,
		LocationArrivedAt: arrivedAt,
	}
	store := newTestSessionStore(t, map[string]*session.Info{"s1": info})
	s := &CoreServer{sessionStore: store}
	stream := &fakeSubscribeStream{ctx: context.Background()}

	// Event timestamp one ns BELOW the floor.
	evTs := arrivedAt.Add(-1 * time.Nanosecond)
	d := makeLocationDelivery(t, locID.String(), evTs)

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil)
	require.NoError(t, err)
	require.Len(t, stream.sent, 0,
		"INV-TS-6: event one ns below floor MUST be filtered at dispatchDelivery")
	assert.Equal(t, 1, d.acks(),
		"filtered event is ack'd (consumed, not forwarded)")
}

// TestDispatchDeliveryIncludesEventAtExactFloorNanosecond gates INV-TS-7.
// The floor MUST use >= semantics: an event whose Timestamp exactly equals
// LocationArrivedAt MUST be INCLUDED in the visible window.
func TestDispatchDeliveryIncludesEventAtExactFloorNanosecond(t *testing.T) {
	t.Parallel()
	locID := core.NewULID()
	arrivedAt := time.Date(2026, 5, 22, 12, 0, 0, 123456789, time.UTC)
	info := &session.Info{
		ID:                "s1",
		CharacterID:       core.NewULID(),
		LocationID:        locID,
		LocationArrivedAt: arrivedAt,
	}
	store := newTestSessionStore(t, map[string]*session.Info{"s1": info})
	s := &CoreServer{sessionStore: store}
	stream := &fakeSubscribeStream{ctx: context.Background()}

	// Event timestamp exactly equal to the floor.
	d := makeLocationDelivery(t, locID.String(), arrivedAt)

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil)
	require.NoError(t, err)
	require.Len(t, stream.sent, 1,
		"INV-TS-7: event at exact floor ns MUST be included (>= semantics)")
	assert.Equal(t, 1, d.acks())
}
```

Verify the helper names match the file's actual API before pasting — `rg -n "func make.*Delivery|func newTestSessionStore|type fakeSubscribeStream" internal/grpc/subscribe_loop_test.go` should show all three referenced above.

- [ ] **Step 3: Run the two new tests to verify they fail**

Run: `task test -- -run "TestDispatchDeliveryDropsEventEmittedInSameNanosecondAsArrival|TestDispatchDeliveryIncludesEventAtExactFloorNanosecond" ./internal/grpc/`
Expected: FAIL — the current truncate-to-µs behavior makes the floor coarse-grained; both tests fail because the floor is rounded to µs.

- [ ] **Step 4: Delete the floor truncate**

Open `internal/grpc/server.go`. Locate line 1100. Change:

```go
	floor := streamScopeFloor(currentInfo, legacyStream).Truncate(time.Microsecond)
```

to:

```go
	floor := streamScopeFloor(currentInfo, legacyStream)
```

Also remove the comment block immediately above line 1100 that explains the µs-truncation rationale (it is now outdated). Search backwards from line 1100 for the comment block opening (typically a `//` block ~10 lines above) and delete it.

- [ ] **Step 5: Retire the stale test**

Open `internal/grpc/subscribe_loop_test.go`. Delete the entire function `TestDispatchDeliveryForwardsEventTruncatedWithinSameMicrosecondAsFloor` (lines ~315-358 including the doc comment). Its premise (publisher truncates to µs, floor truncates to µs, sub-µs events from same µs as floor are forwarded) is invalidated by Phase 1: there is no truncation any more, so the scenario the test pinned does not exist.

- [ ] **Step 6: Run the two new tests to verify they pass**

Run: `task test -- -run "TestDispatchDeliveryDropsEventEmittedInSameNanosecondAsArrival|TestDispatchDeliveryIncludesEventAtExactFloorNanosecond" ./internal/grpc/`
Expected: PASS.

- [ ] **Step 7: Run the full grpc test suite**

Run: `task test -- ./internal/grpc/`
Expected: PASS — all grpc tests pass.

- [ ] **Step 8: Commit**

Suggested message: `feat(grpc): drop dispatchDelivery floor µs-truncate; >= ns semantics (Phase 1.4, gfo6)`

### Task 6: Update production `events_audit` INSERT to pass `pgnanos.From`

**Files:**

- Modify: `internal/eventbus/audit/projection.go:248-269`

- [ ] **Step 1: Locate the INSERT call**

Run: `rg -n "INSERT INTO events_audit" internal/eventbus/audit/projection.go`
Expected: one match at line ~248.

- [ ] **Step 2: Update the INSERT to route timestamps through `pgnanos.From`**

Open `internal/eventbus/audit/projection.go`. Find the import block at the top and add the `pgnanos` import (alphabetical order):

```go
	"github.com/holomush/holomush/internal/pgnanos"
```

Find the `p.pool.Exec(ctx, ...INSERT INTO events_audit...)` call around line 248. The current Exec passes `meta.Timestamp` (a `time.Time` from `*jetstream.MsgMetadata`) as the `timestamp` column value. Change that argument:

Before:

```go
		_, err = p.pool.Exec(
			ctx, `
			INSERT INTO events_audit (
				id, subject, type, timestamp, actor_kind, actor_id,
				envelope, schema_ver, codec, js_seq, rendering,
				dek_ref, dek_version
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (id) DO NOTHING`,
			idBytes,
			msg.Subject(),
			eventType,
			meta.Timestamp,
			actorKind,
			// ...
```

After:

```go
		_, err = p.pool.Exec(
			ctx, `
			INSERT INTO events_audit (
				id, subject, type, timestamp, actor_kind, actor_id,
				envelope, schema_ver, codec, js_seq, rendering,
				dek_ref, dek_version
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (id) DO NOTHING`,
			idBytes,
			msg.Subject(),
			eventType,
			pgnanos.From(meta.Timestamp),
			actorKind,
			// ...
```

Note: `inserted_at` is not passed by the app — it uses the DB-side BIGINT DEFAULT from the migration, which is µs-truncated (intentional per spec).

- [ ] **Step 3: Update any SELECT/Scan paths that read from `events_audit` in the same file or sibling files**

Run: `rg -n "SELECT.*timestamp.*FROM events_audit|FROM events_audit.*timestamp" internal/eventbus/ --type=go`
Expected: zero or a small number of matches.

For each match, change the `Scan(&ts)` where `ts` is `time.Time` to `Scan(&nanos)` where `nanos` is `pgnanos.Time`, then use `nanos.Time()`. (No additional matches expected in projection.go itself; the host projection is write-only.)

- [ ] **Step 4: Run the audit projection tests**

Run: `task test -- ./internal/eventbus/audit/`
Expected: PASS.

- [ ] **Step 5: Run the eventbus integration suite**

Run: `task test:int -- ./test/integration/eventbus_e2e/...`
Expected: PASS.

- [ ] **Step 6: Commit**

Suggested message: `refactor(eventbus): route events_audit timestamps through pgnanos (Phase 1.5, gfo6)`

### Task 7: Update `crypto_keys` repo writes + scans to use `pgnanos`

**Files:**

- Modify: `internal/eventbus/crypto/dek/store.go` (locate all `INSERT INTO crypto_keys` and `UPDATE crypto_keys` and `SELECT ... FROM crypto_keys` paths)

- [ ] **Step 1: Locate all crypto_keys SQL paths**

Run: `rg -n "crypto_keys" internal/eventbus/crypto/dek/store.go`
Expected: multiple matches for INSERT, UPDATE, SELECT.

- [ ] **Step 2: Add pgnanos import**

Open `internal/eventbus/crypto/dek/store.go`. Add to the import block:

```go
	"github.com/holomush/holomush/internal/pgnanos"
```

- [ ] **Step 3: Update each call site**

For each INSERT/UPDATE that writes `created_at`, `rotated_at`, `destroyed_at` (or column references like `$N::timestamptz`), change:

- App-supplied `time.Time` arguments → wrap in `pgnanos.From(...)`.
- `Scan(&t)` where `t` is `time.Time` for these columns → change `t` to `pgnanos.Time`, then call `.Time()` at use site.

Show the file-wide pattern with one representative example. If the file has a function `InsertKey(ctx context.Context, key Key) error` writing `created_at`:

Before:

```go
	_, err := s.pool.Exec(ctx,
		`INSERT INTO crypto_keys (id, version, created_at, ...) VALUES ($1, $2, $3, ...)`,
		key.ID, key.Version, key.CreatedAt, ...,
	)
```

After:

```go
	_, err := s.pool.Exec(ctx,
		`INSERT INTO crypto_keys (id, version, created_at, ...) VALUES ($1, $2, $3, ...)`,
		key.ID, key.Version, pgnanos.From(key.CreatedAt), ...,
	)
```

For SELECT paths reading the same columns:

Before:

```go
	var createdAt time.Time
	err := row.Scan(&id, &version, &createdAt, ...)
```

After:

```go
	var createdAt pgnanos.Time
	err := row.Scan(&id, &version, &createdAt, ...)
	// ... downstream code uses createdAt.Time()
```

- [ ] **Step 4: Handle nullable columns (`rotated_at`, `destroyed_at`)**

For nullable timestamp columns, use a pointer:

```go
	var rotatedAt *pgnanos.Time
	err := row.Scan(..., &rotatedAt, ...)

	if rotatedAt != nil {
		t := rotatedAt.Time()
		// ...
	}
```

If the existing code used `*time.Time` or `sql.NullTime`, replace it with `*pgnanos.Time`.

- [ ] **Step 5: Run dek crypto tests**

Run: `task test -- ./internal/eventbus/crypto/dek/`
Expected: PASS.

- [ ] **Step 6: Run dek integration tests**

Run: `task test:int -- ./internal/eventbus/crypto/dek/...`
Expected: PASS.

- [ ] **Step 7: Commit**

Suggested message: `refactor(crypto/dek): route crypto_keys timestamps through pgnanos (Phase 1.6, gfo6)`

### Task 8: Update plugin core-scenes `scene_log` audit writes + scans

**Files:**

- Modify: `plugins/core-scenes/audit.go` (the plugin's audit writer)

- [ ] **Step 1: Locate scene_log INSERT path**

Run: `rg -n "INSERT INTO.*scene_log|UPDATE.*scene_log" plugins/core-scenes/audit.go`
Expected: one match.

- [ ] **Step 2: Add pgnanos import**

Open `plugins/core-scenes/audit.go`. Add to imports:

```go
	"github.com/holomush/holomush/internal/pgnanos"
```

- [ ] **Step 3: Update the INSERT call**

Find the Exec call writing `scene_log` and change `time.Time` arguments to `pgnanos.From(...)`:

Before (representative):

```go
	_, err := tx.Exec(ctx, `
		INSERT INTO scene_log (id, scene_id, subject, type, timestamp, ...)
		VALUES ($1, $2, $3, $4, $5, ...)`,
		row.ID, row.SceneID, row.Subject, row.Type, row.Timestamp, ...,
	)
```

After:

```go
	_, err := tx.Exec(ctx, `
		INSERT INTO scene_log (id, scene_id, subject, type, timestamp, ...)
		VALUES ($1, $2, $3, $4, $5, ...)`,
		row.ID, row.SceneID, row.Subject, row.Type, pgnanos.From(row.Timestamp), ...,
	)
```

Note: `inserted_at` uses the DB-side BIGINT DEFAULT from the migration; not passed by the app.

- [ ] **Step 4: Update any SELECT path that reads `timestamp` from `scene_log`**

Run: `rg -n "SELECT.*timestamp.*scene_log|SELECT.*scene_log.*timestamp" plugins/core-scenes/ --type=go`
Expected: typically the dispatcher / history read path.

For each, change `var ts time.Time` → `var ts pgnanos.Time` and use `ts.Time()` at consumers.

- [ ] **Step 5: Run plugin core-scenes tests**

Run: `task test -- ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 6: Commit**

Suggested message: `refactor(core-scenes): route scene_log timestamps through pgnanos (Phase 1.7, gfo6)`

### Task 9: Update plugin core-scenes remaining columns (`scenes`, `scene_participants`, `ops_events`, pose order)

**Files:**

- Modify: `plugins/core-scenes/service.go` (or wherever the scene CRUD lives)
- Modify: `plugins/core-scenes/participants.go`
- Modify: `plugins/core-scenes/ops_events.go`

- [ ] **Step 1: Locate write/read sites**

Run: `rg -n "scenes\(|scene_participants|ops_events|last_pose_at" plugins/core-scenes/ --type=go`
Expected: multiple matches across the listed files.

- [ ] **Step 2: For each file, add pgnanos import + wrap time arguments and scan targets**

Follow the same pattern as Tasks 6-8: `pgnanos.From(t)` on writes, `pgnanos.Time` on scans, `.Time()` at consumers.

The columns to handle (per the spec's Section 2 inventory):

- `scenes.created_at`, `scenes.ended_at`, `scenes.archived_at`, `scenes.last_pose_at`
- `scene_participants.joined_at`
- `ops_events.occurred_at`

- [ ] **Step 3: Run core-scenes tests**

Run: `task test -- ./plugins/core-scenes/`
Expected: PASS.

- [ ] **Step 4: Run core-scenes integration tests**

Run: `task test:int -- ./plugins/core-scenes/...`
Expected: PASS.

- [ ] **Step 5: Commit**

Suggested message: `refactor(core-scenes): route remaining scene timestamps through pgnanos (Phase 1.8, gfo6)`

### Task 10: Strengthen `plugin_aad_reconstruction_test.go` (INV-TS-5)

**Files:**

- Modify: `internal/eventbus/history/plugin_aad_reconstruction_test.go` (remove four `Truncate(time.Microsecond)` calls; add `TestRoundTripPreservesAADWithSubMicrosecondNanos`)

- [ ] **Step 1: Locate the four Truncate calls**

Run: `rg -n "Truncate.*time\.Microsecond" internal/eventbus/history/plugin_aad_reconstruction_test.go`
Expected: 3-4 matches at lines ~84, ~97, ~170 (and possibly nil-actor test).

- [ ] **Step 2: Remove each Truncate call**

For each match:

Before (representative, at line 84):

```go
	publisherEvent.Timestamp = timestamppb.New(publisherEvent.GetTimestamp().AsTime().Truncate(time.Microsecond))
```

After:

```go
	// INV-TS-5: publisher preserves full ns; no truncation here either.
	// publisherEvent.Timestamp already has the source ns precision.
```

Remove the assignment entirely. The previous line that sets `publisherEvent.Timestamp` to a sub-µs value should be left intact — only the truncation is removed.

For the line ~97 `auditRowTimestamp := timestamppb.New(publisherEvent.GetTimestamp().AsTime().Truncate(time.Microsecond))`:

Replace with:

```go
	// PG BIGINT-ns column preserves full ns; no truncation.
	auditRowTimestamp := publisherEvent.GetTimestamp()
```

For the line ~170 in `TestRoundTripFailsWithoutPublisherMicrosecondTruncation` — this entire test pinned the failure mode of NOT truncating at the publisher. Its premise is invalidated by Phase 1. **Delete the entire test function `TestRoundTripFailsWithoutPublisherMicrosecondTruncation`.**

- [ ] **Step 3: Update existing `TestRoundTripProducesByteEqualAAD` to assert sub-µs survival**

Rename `TestRoundTripProducesByteEqualAAD` to `TestRoundTripPreservesAADWithSubMicrosecondNanos`. After the existing byte-equality assertion, add:

```go
	// INV-TS-5 reinforcement: the sub-µs nanosecond component survives the
	// publish → DB → read → AAD-reconstruct round-trip.
	roundTripTs := reconstructedEvent.GetTimestamp().AsTime()
	assert.Equal(t, 789, roundTripTs.Nanosecond()%1000,
		"INV-TS-5: AAD round-trip MUST preserve sub-µs nanoseconds at ns column resolution")
```

Adjust the fixture timestamp (currently `time.Unix(1700000000, 12345)`) to one with the digit `789` in the sub-µs slot so the assertion has a deterministic check, e.g.:

```go
	Timestamp: timestamppb.New(time.Unix(1700000000, 12345789).UTC()),
```

- [ ] **Step 4: Run the AAD reconstruction tests**

Run: `task test -- ./internal/eventbus/history/`
Expected: PASS — byte-equal AAD with sub-µs nanos.

- [ ] **Step 5: Commit**

Suggested message: `test(eventbus/history): strengthen AAD round-trip to assert ns precision (Phase 1.9, gfo6)`

### Task 11: Remove `time.Sleep(50 * time.Millisecond)` from `privacy_test.go` and re-test floor ordering deterministically

**Files:**

- Modify: `test/integration/privacy/privacy_test.go:137-141` (remove sleep, replace with deterministic ordering or a fixed-time fixture)

- [ ] **Step 1: Locate the sleep**

Run: `rg -n "time\.Sleep.*Millisecond" test/integration/privacy/privacy_test.go`
Expected: one match at line 141 (with the comment block at lines 137-140).

- [ ] **Step 2: Remove the sleep + comment block**

Open `test/integration/privacy/privacy_test.go`. Delete lines 137-141 (the four-line comment block plus the `time.Sleep` call). The test scenario continues without the artificial gap — at ns resolution, two `time.Now()` calls separated by an ABAC check + emit + logout + ConnectGuest are guaranteed (effectively) to produce distinct ns values, and the floor uses `>=` semantics for the boundary case.

If your test environment runs fast enough that ns ties become a practical concern (vanishingly unlikely but worth a fallback), use the test-harness's deterministic clock — search for an existing clock injection mechanism (`integrationtest.WithClock` or similar in `internal/testsupport/integrationtest/`) and inject explicit timestamps for guestA's emit and guestB's connection. If no clock injection exists yet, this is out of scope — file a follow-up bead.

- [ ] **Step 3: Run the privacy integration suite**

Run: `task test:int -- ./test/integration/privacy/...`
Expected: PASS — INV-PRIV-N suite still green.

- [ ] **Step 4: Commit**

Suggested message: `test(privacy): remove time.Sleep tie-prevention hack; rely on ns resolution (Phase 1.10, gfo6)`

### Task 12: Strip `Truncate(time.Microsecond)` from remaining crypto + plugin Phase-1 tests

**Files:**

- Modify: `internal/eventbus/publisher_test.go` (3 sites; not counting the replaced TestPublisher* test from Task 4)
- Modify: `internal/eventbus/history/plugin_aad_reconstruction_test.go` (already updated in Task 10; verify)
- Modify: `internal/eventbus/crypto/dek/manager_integration_test.go` (2 sites)
- Modify: `plugins/core-scenes/poseorder_integration_test.go` (4 sites)
- Modify: `cmd/holomush/crypto_operator_validation_test.go` (1 site)

- [ ] **Step 1: Locate remaining Truncate calls in Phase 1 scope**

Run: `rg -n "Truncate.*time\.Microsecond" internal/eventbus/publisher_test.go internal/eventbus/crypto/dek/manager_integration_test.go plugins/core-scenes/poseorder_integration_test.go cmd/holomush/crypto_operator_validation_test.go`
Expected: ~10 matches across the listed files.

- [ ] **Step 2: For each match, remove the truncation OR flip tolerance**

Two patterns exist in the listed files. Handle each per its shape:

**Pattern A — bare round-trip comparison** (most sites in `publisher_test.go`, `manager_integration_test.go`, `crypto_operator_validation_test.go`):

Before:

```go
	require.Equal(t,
		want.Truncate(time.Microsecond),
		got.Truncate(time.Microsecond),
		"timestamps should round-trip")
```

After:

```go
	require.Equal(t, want.UTC(), got.UTC(), "timestamps should round-trip at ns precision")
```

**Pattern B — Ginkgo `BeTemporally("~", ..., time.Microsecond)` tolerance** (all four sites in `plugins/core-scenes/poseorder_integration_test.go:269-275`):

This is NOT a bare Truncate strip — the `BeTemporally("~", X, time.Microsecond)` carries a ±1µs tolerance whose semantic was "absorb any nanosecond truncation during round-trip" (per the comment at line 263-264). After Phase 1, `last_pose_at` is `BIGINT`-ns and the round-trip is bit-exact at ns resolution, so both the truncation AND the tolerance are obsolete.

Before:

```go
	// Timestamps: Postgres TIMESTAMPTZ has microsecond resolution;
	// allow ±1µs to absorb any nanosecond truncation during round-trip.
	Expect(gotChar1.lastPoseAt).NotTo(BeNil(),
		"INV-P4-8: rebuilt char1.last_pose_at MUST be set")
	Expect(gotChar2.lastPoseAt).NotTo(BeNil(),
		"INV-P4-8: rebuilt char2.last_pose_at MUST be set")
	Expect(gotChar1.lastPoseAt.Truncate(time.Microsecond)).To(
		BeTemporally("~", wantChar1.lastPoseAt.Truncate(time.Microsecond), time.Microsecond),
		"INV-P4-8: rebuilt char1.last_pose_at MUST match maintained value (±1µs)",
	)
	Expect(gotChar2.lastPoseAt.Truncate(time.Microsecond)).To(
		BeTemporally("~", wantChar2.lastPoseAt.Truncate(time.Microsecond), time.Microsecond),
		"INV-P4-8: rebuilt char2.last_pose_at MUST match maintained value (±1µs)",
	)
```

After:

```go
	// Timestamps: last_pose_at is BIGINT-ns; round-trip is bit-exact (INV-TS-1, INV-TS-2).
	Expect(gotChar1.lastPoseAt).NotTo(BeNil(),
		"INV-P4-8: rebuilt char1.last_pose_at MUST be set")
	Expect(gotChar2.lastPoseAt).NotTo(BeNil(),
		"INV-P4-8: rebuilt char2.last_pose_at MUST be set")
	Expect(gotChar1.lastPoseAt.Equal(*wantChar1.lastPoseAt)).To(BeTrue(),
		"INV-P4-8: rebuilt char1.last_pose_at MUST equal maintained value at ns precision")
	Expect(gotChar2.lastPoseAt.Equal(*wantChar2.lastPoseAt)).To(BeTrue(),
		"INV-P4-8: rebuilt char2.last_pose_at MUST equal maintained value at ns precision")
```

Use `time.Time.Equal` rather than `==` because the two values may differ in `time.Location` (one came from the DB scan, one from Go-side `time.Now()`); `Equal` compares the instant, not the wall-time representation.

**Source timestamps:** when a test uses `time.Now()` as the source, replace with a deterministic fixture (`time.Date(2026, 5, 22, 12, 0, 0, 123456789, time.UTC)`) so the assertion is reproducible.

- [ ] **Step 3: Run all touched tests**

Run: `task test -- ./internal/eventbus/ ./plugins/core-scenes/ ./cmd/holomush/`
Expected: PASS.

- [ ] **Step 4: Verify no Phase-1-scoped Truncate sites remain**

Run: `rg -c "Truncate\(time\.Microsecond\)" internal/eventbus/ plugins/core-scenes/ cmd/holomush/`
Expected: zero counts for Phase 1 files. (`internal/auth/postgres/`, `internal/world/postgres/`, `internal/totp/`, `internal/store/`, `internal/grpc/` still have Truncate calls — those are Phases 2-4.)

- [ ] **Step 5: Commit**

Suggested message: `test: strip µs-truncate from Phase 1 crypto + plugin tests (Phase 1.11, gfo6)`

### Task 13: Update `2026-05-17-history-scope-privacy-design.md` for ns floor semantics

**Files:**

- Modify: `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md` (the "Precision contract" subsection)

- [ ] **Step 1: Locate the precision-contract paragraph**

Run: `rg -n "Precision contract\|microsecond granularity\|µs granularity" docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md`
Expected: one or two matches.

- [ ] **Step 2: Update the prose**

Replace the µs-granularity language with ns-granularity language. The "Precision contract" paragraph currently asserts:

> The comparison MUST be performed at microsecond granularity. Event timestamps are truncated to microseconds at publish time...

Change to:

> The comparison MUST be performed at nanosecond granularity (post-`holomush-gfo6`). Event timestamps preserve full nanosecond precision at publish time per INV-TS-4. Floor inputs (`LocationArrivedAt`, `GuestCharacterCreatedAt`, `FocusMembership.JoinedAt`) preserve nanosecond precision per INV-TS-1 (BIGINT epoch-ns columns). Comparison uses `>=` semantics so an event at the exact floor ns is INCLUDED (INV-TS-7). The former µs-granularity contract is superseded.

- [ ] **Step 3: Add a cross-reference at the top of the affected spec**

Add a note near the top of `2026-05-17-history-scope-privacy-design.md`:

```markdown
> **Update 2026-05-22 (`holomush-gfo6`):** Floor precision contract upgraded
> from microsecond to nanosecond. See
> [`2026-05-22-nanosecond-timestamps-design.md`](2026-05-22-nanosecond-timestamps-design.md)
> §5 INV-TS-6 / INV-TS-7.
```

- [ ] **Step 4: Run docs lint**

Run: `task lint:docs` (or whichever doc lint task is current — `rumdl` is the markdown linter)
Expected: PASS.

- [ ] **Step 5: Commit**

Suggested message: `docs: update history-scope-privacy spec for ns floor (Phase 1.12, gfo6)`

### Task 14: Add `INV-TS-META` meta-test

**Files:**

- Create: `internal/store/spec_meta_test.go` (mirrors `internal/eventbus/history/phase7_boundary_meta_test.go`)

- [ ] **Step 1: Inspect the mirrored pattern**

Run: `rg -n "TestPhase7InvariantsHaveNamedTests" internal/eventbus/history/phase7_boundary_meta_test.go`
Expected: line 47. Read this function (`Read internal/eventbus/history/phase7_boundary_meta_test.go offset=20 limit=120`) to understand the pattern: hardcoded `cases` slice + `collectTestFuncNames` AST walk + assertions.

- [ ] **Step 2: Write the meta-test**

Create `internal/store/spec_meta_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestNanosecondTimestampsInvariantsHaveNamedTests is the drift detector
// for the invariants table in
// docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md §5.
// For each INV-TS-N (1..7), the test verifies the named test that pins
// the invariant exists somewhere in the repo's *_test.go corpus.
//
// If this test FAILS:
//   - Either an invariant was removed without updating this table, OR
//   - A named test was renamed/removed without updating this table.
//
// Fix by adjusting the cases slice below AND the spec's invariant table
// in lockstep — the two MUST agree at all times.
//
// INV-TS-META (this test itself) is intentionally excluded; the table
// protects everything except itself.
//
// Implementation: walk the test file's location via runtime.Caller(0),
// climb until a `go.mod` is found, then walk the tree with go/parser
// to enumerate top-level Test* function names. Pure-Go (no external `rg`
// dependency) so the test runs in any environment with a Go toolchain.
func TestNanosecondTimestampsInvariantsHaveNamedTests(t *testing.T) {
	cases := []struct {
		inv      string
		testName string
	}{
		// INV-TS-1: lint + integration meta-test
		{"INV-TS-1", "TestNoTimestamptzColumnsAfterMigration"},
		// INV-TS-2: pgnanos round-trip test
		{"INV-TS-2", "TestRoundTripPreservesSubMicrosecondNanoseconds"},
		// INV-TS-3: enforced by lint:no-microsecond-truncate; meta-test asserts the lint passes
		{"INV-TS-3", "TestLintNoMicrosecondTruncatePasses"},
		// INV-TS-4: publisher preserves ns
		{"INV-TS-4", "TestPublisherPreservesNanoseconds"},
		// INV-TS-5: AAD round-trip preserves ns
		{"INV-TS-5", "TestRoundTripPreservesAADWithSubMicrosecondNanos"},
		// INV-TS-6: floor drops sub-floor-ns events
		{"INV-TS-6", "TestDispatchDeliveryDropsEventEmittedInSameNanosecondAsArrival"},
		// INV-TS-7: floor includes exact-floor-ns events
		{"INV-TS-7", "TestDispatchDeliveryIncludesEventAtExactFloorNanosecond"},
	}

	repoRoot := findRepoRoot(t)
	testNames := collectTestFuncNames(t, repoRoot)

	// Phase-5-deferred subtests: TestNoTimestamptzColumnsAfterMigration
	// (INV-TS-1) and TestLintNoMicrosecondTruncatePasses (INV-TS-3) are
	// created in Phase 5 alongside the lint guards they exercise. Skipping
	// them keeps task pr-prep green for Phase 2-4 PRs. Once Phase 5 merges
	// and both tests exist, the skip becomes a no-op (the lookup succeeds)
	// and CAN be removed safely as the last step of Phase 5 (Task 22).
	phaseFiveDeferred := map[string]struct{}{
		"INV-TS-1": {},
		"INV-TS-3": {},
	}

	for _, tc := range cases {
		t.Run(tc.inv, func(t *testing.T) {
			if _, deferred := phaseFiveDeferred[tc.inv]; deferred {
				if _, ok := testNames[tc.testName]; !ok {
					t.Skipf("Phase-5-deferred: %s names test %q which lands in Phase 5; "+
						"remove this skip-guard once Phase 5 merges (see plan Task 22 Step 6)",
						tc.inv, tc.testName)
					return
				}
				// Test exists — fail loudly so the cleanup obligation
				// cannot be missed. Once Phase 5 lands and both tests
				// exist, this branch fires and breaks `task pr-prep`
				// until Task 22 Step 6 removes the guard. This is
				// preferable to a `t.Logf` (silently suppressed by
				// gotestsum compact mode) because the failure forces
				// the cleanup into the same PR that lands the tests.
				t.Errorf(
					"phase-5-deferred guard fires for %s test %q (which now exists). "+
						"Remove the entry from phaseFiveDeferred (plan Task 22 Step 6) "+
						"to restore drift detection. This failure is the cleanup obligation.",
					tc.inv, tc.testName)
				return
			}
			if _, ok := testNames[tc.testName]; !ok {
				t.Errorf("spec invariant %s names test %q, but no such Test* function exists anywhere in the repo",
					tc.inv, tc.testName)
			}
		})
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	dir := filepath.Dir(here)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found in any ancestor of test file")
		}
		dir = parent
	}
}

func collectTestFuncNames(t *testing.T, root string) map[string]struct{} {
	t.Helper()
	names := make(map[string]struct{})
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip vendor, build, .git, node_modules, etc.
			name := info.Name()
			if name == "vendor" || name == "node_modules" || name == ".git" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		// Parse with comments off; we only need top-level decls.
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			// Skip files that don't parse cleanly (e.g., generated stubs).
			return nil
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil {
				continue
			}
			n := fd.Name.Name
			if strings.HasPrefix(n, "Test") {
				names[n] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("collecting test func names: %v", err)
	}
	return names
}
```

(Import block above already includes `"go/ast"` for the `*ast.FuncDecl` type assertion.)

- [ ] **Step 3: Run the meta-test to verify it passes (or fails with a clear list)**

Run: `task test -- ./internal/store/ -run TestNanosecondTimestampsInvariantsHaveNamedTests`
Expected: PASS — all seven INV-TS-N tests exist (assuming Tasks 1-13 landed correctly). If any are missing, the test names them.

Note: `TestNoTimestamptzColumnsAfterMigration` (INV-TS-1) and `TestLintNoMicrosecondTruncatePasses` (INV-TS-3) are added in Phase 5. The meta-test's `phaseFiveDeferred` map causes those two subtests to **skip** (not fail) until both tests exist. This keeps `task pr-prep` green across Phase 2-4 PRs while still pinning INV-TS-2 / 4 / 5 / 6 / 7 immediately. Once Phase 5 merges, Task 22's final step removes the skip-guard so the two newly-existing tests get the same drift detection as the rest.

- [ ] **Step 4: Commit**

Suggested message: `test(store): add INV-TS-META meta-test for ns-timestamps spec (Phase 1.13, gfo6)`

### Task 15: Run `task pr-prep` to full completion — Phase 1 gate

**Files:**

- (no files; verification step)

- [ ] **Step 1: Run pr-prep full lane**

Run: `task pr-prep`
Expected: green across lint, format, schema, license, unit, integration, E2E. May take 5-15 minutes.

If any failure occurs, fix it before proceeding. Do NOT skip individual checks. Per the project rule `feedback_pr_prep`: never approximate, never trust a partial run.

- [ ] **Step 2: Confirm zero remaining Phase-1 Truncate sites**

Run: `rg -c "Truncate\(time\.Microsecond\)" internal/eventbus/ plugins/core-scenes/ cmd/holomush/ test/integration/privacy/`
Expected: all zeros.

- [ ] **Step 3: Confirm zero remaining `time.Sleep` tie-prevention hacks in Phase 1 scope**

Run: `rg "tie timestamps\|sub-millisecond co-occurrence" test/integration/`
Expected: zero matches.

- [ ] **Step 4: Open Phase 1 PR**

Use `gh pr create` per CLAUDE.md "Creating pull requests" rules. PR body should reference `holomush-gfo6`, list the phases collapsed into this PR, and call out the precision-lossy down-migration.

---

## Phase 2: auth/postgres

### Task 16: Migrate `auth/postgres` columns to BIGINT + route through pgnanos

**Files:**

- Create: `internal/store/migrations/000NNN_auth_timestamps_to_bigint.up.sql`
- Create: `internal/store/migrations/000NNN_auth_timestamps_to_bigint.down.sql`
- Modify: `internal/auth/postgres/player_repo.go`
- Modify: `internal/auth/postgres/reset_repo.go`
- Modify: `internal/store/session_store.go` (Set, UpdateStatus, UpdateLocationOnMove, BumpLocationArrivedAt, scanSession/scanSessions)
- Modify: `internal/session/session.go` (struct fields remain `time.Time`; verify no UnixNano calls)
- Modify: `internal/grpc/scope_floor.go` (no change to the function body — consumes `time.Time` from `session.Info`; this file is listed only to be re-tested after the scan-path edit)

- [ ] **Step 1: Determine the next free migration number**

Run: `ls internal/store/migrations/ | rg -o "^[0-9]+" | sort -u | tail -3`
Expected: the next number after Phase 1's migration (e.g., if Phase 1 used 000038, this is 000039).

- [ ] **Step 2: Write the up migration**

Create `internal/store/migrations/<NNN>_auth_timestamps_to_bigint.up.sql`:

```sql
-- Convert auth-domain timestamp columns from TIMESTAMPTZ to BIGINT
-- (epoch nanoseconds, UTC). INV-TS-1.

ALTER TABLE players
    ALTER COLUMN locked_until
        TYPE BIGINT USING (EXTRACT(EPOCH FROM locked_until) * 1e9)::BIGINT,
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT,
    ALTER COLUMN updated_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM updated_at) * 1e9)::BIGINT;

ALTER TABLE players
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    ALTER COLUMN updated_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE password_resets
    ALTER COLUMN expires_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM expires_at) * 1e9)::BIGINT,
    ALTER COLUMN used_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM used_at) * 1e9)::BIGINT,
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT;

ALTER TABLE password_resets
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE sessions
    ALTER COLUMN expires_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM expires_at) * 1e9)::BIGINT,
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT,
    ALTER COLUMN updated_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM updated_at) * 1e9)::BIGINT;

ALTER TABLE sessions
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    ALTER COLUMN updated_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE session_connections
    ALTER COLUMN connected_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM connected_at) * 1e9)::BIGINT;

ALTER TABLE session_connections
    ALTER COLUMN connected_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE player_sessions
    ALTER COLUMN detached_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM detached_at) * 1e9)::BIGINT,
    ALTER COLUMN expires_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM expires_at) * 1e9)::BIGINT,
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT,
    ALTER COLUMN updated_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM updated_at) * 1e9)::BIGINT;

ALTER TABLE player_sessions
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    ALTER COLUMN updated_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

-- Session history floor columns (added in migration 000037). Migrated in
-- Phase 2 alongside the rest of the `sessions` table so the table converts
-- atomically and `session_store.go::Set()` does not have to handle a
-- mixed-type interim state.
ALTER TABLE sessions
    ALTER COLUMN location_arrived_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM location_arrived_at) * 1e9)::BIGINT,
    ALTER COLUMN guest_character_created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM guest_character_created_at) * 1e9)::BIGINT;

ALTER TABLE sessions
    ALTER COLUMN location_arrived_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    ALTER COLUMN guest_character_created_at
        SET DEFAULT 0;
```

- [ ] **Step 3: Write the down migration**

Mirror the up using `to_timestamp(col::double precision / 1e9)`. Include the standard precision-lossy header comment. For the floor columns, the down-migration restore for `guest_character_created_at` should use `'epoch'::timestamptz` as the default (matching the pre-migration shape).

- [ ] **Step 4: Update `player_repo.go` and `reset_repo.go` to route through pgnanos**

For every `time.Time` argument passed to Exec for a now-BIGINT column, wrap with `pgnanos.From(...)`. For every `Scan(&t)` where `t time.Time` and the column is now BIGINT, change to `pgnanos.Time` + `.Time()` at use.

Add the import `"github.com/holomush/holomush/internal/pgnanos"` to both repo files.

- [ ] **Step 5: Update `internal/store/session_store.go` (scan path + four write paths)**

The `sessions` table now has all timestamp columns as BIGINT-ns: `created_at`, `updated_at`, `detached_at`, `expires_at`, `location_arrived_at`, `guest_character_created_at`. Every site that reads or writes these columns must route through `pgnanos.Time`. Add `"github.com/holomush/holomush/internal/pgnanos"` to the import block.

**5a — Scan path (`scanSession` / `scanSessions`).** Change scan targets:

```go
	var (
		createdAt, updatedAt, arrived, guestCreated pgnanos.Time
		detachedAt, expiresAt                       *pgnanos.Time
	)
	err := row.Scan(
		/*..., other columns, */
		&createdAt, &updatedAt, &detachedAt, &expiresAt,
		&arrived, &guestCreated,
		/*, ...*/
	)
	info.CreatedAt = createdAt.Time()
	info.UpdatedAt = updatedAt.Time()
	if detachedAt != nil {
		t := detachedAt.Time()
		info.DetachedAt = &t
	}
	if expiresAt != nil {
		t := expiresAt.Time()
		info.ExpiresAt = &t
	}
	info.LocationArrivedAt = arrived.Time()
	info.GuestCharacterCreatedAt = guestCreated.Time()
```

The `session.Info` struct fields in `internal/session/session.go` MUST remain `time.Time` (or `*time.Time` for nullable) — `pgnanos.Time` is the **scan seam**, not the in-memory representation. `streamScopeFloor` continues to work without changes.

**5b — `Set()` (lines 271-329 of `internal/store/session_store.go`).** The full INSERT touches columns `created_at`, `updated_at`, `detached_at`, `expires_at`, `location_arrived_at`, `guest_character_created_at` — all now BIGINT. Replace every `$N::timestamptz` cast with `$N::BIGINT`, and replace SQL-literal defaults `now()` / `'epoch'::timestamptz` with `(EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT` / `0::BIGINT`:

Before (representative excerpt):

```go
	`INSERT INTO sessions (..., location_arrived_at, guest_character_created_at, ...)
	 VALUES (...,
		COALESCE($7::timestamptz, now()),
		COALESCE($8::timestamptz, 'epoch'::timestamptz),
		...)
	 ON CONFLICT (id) DO UPDATE SET
		...
		location_arrived_at = COALESCE($7::timestamptz, sessions.location_arrived_at),
		guest_character_created_at = COALESCE($8::timestamptz, sessions.guest_character_created_at),
		...`
```

After:

```go
	`INSERT INTO sessions (..., location_arrived_at, guest_character_created_at, ...)
	 VALUES (...,
		COALESCE($7::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT),
		COALESCE($8::BIGINT, 0::BIGINT),
		...)
	 ON CONFLICT (id) DO UPDATE SET
		...
		location_arrived_at = COALESCE($7::BIGINT, sessions.location_arrived_at),
		guest_character_created_at = COALESCE($8::BIGINT, sessions.guest_character_created_at),
		...`
```

Arguments to `s.pool.Exec`: parameters `$7` and `$8` were previously `*time.Time` (nullable). Change to `*pgnanos.Time`. Other arguments for `$15` (detached_at), `$16` (expires_at), `$17` (created_at) — apply the same wrapping pattern, matching the column's nullability.

**5c — `UpdateLocationOnMove()` (line 770-773).** Wrap with `pgnanos.From`:

Before:

```go
	query := `UPDATE sessions
	          SET location_id = $1, location_arrived_at = $2, updated_at = $2
	          WHERE character_id = $3 AND status = 'active'`
	_, err := s.pool.Exec(ctx, query, newLocationID.String(), arrivedAt, characterID.String())
```

After:

```go
	query := `UPDATE sessions
	          SET location_id = $1, location_arrived_at = $2, updated_at = $2
	          WHERE character_id = $3 AND status = 'active'`
	_, err := s.pool.Exec(ctx, query, newLocationID.String(), pgnanos.From(arrivedAt), characterID.String())
```

Both `location_arrived_at` and `updated_at` are BIGINT after Phase 2, so the single `pgnanos.From(arrivedAt)` argument satisfies both via `$2`.

**5d — `BumpLocationArrivedAt()` (line 798-801).** Same pattern:

Before:

```go
	query := `UPDATE sessions
	          SET location_arrived_at = $1, updated_at = $1
	          WHERE id = $2`
	res, err := s.pool.Exec(ctx, query, arrivedAt, sessionID)
```

After:

```go
	query := `UPDATE sessions
	          SET location_arrived_at = $1, updated_at = $1
	          WHERE id = $2`
	res, err := s.pool.Exec(ctx, query, pgnanos.From(arrivedAt), sessionID)
```

**5e — `UpdateStatus()` (around line 411).** Passes `detachedAt *time.Time` and `expiresAt *time.Time`. Wrap with `pgnanos`:

```go
	var detachedNanos, expiresNanos *pgnanos.Time
	if detachedAt != nil {
		t := pgnanos.From(*detachedAt)
		detachedNanos = &t
	}
	if expiresAt != nil {
		t := pgnanos.From(*expiresAt)
		expiresNanos = &t
	}
	_, err := s.pool.Exec(ctx, query, /*..., */ detachedNanos, expiresNanos /*, ...*/)
```

**Verify after all five edits:** `rg -n "::timestamptz" internal/store/session_store.go` should return zero matches.

- [ ] **Step 6: Strip Truncate calls from auth + session-store tests**

Run: `rg -n "Truncate\(time\.Microsecond\)" internal/auth/postgres/ internal/store/`
Expected: ~46 matches (41 in `internal/auth/postgres/{player,reset}_repo_test.go` + 5 in `internal/store/player_session_store_test.go`).

For each, remove the `Truncate(time.Microsecond)` call (the round-trip preserves ns now). Where a test compared `want.Truncate(time.Microsecond)` to `got.Truncate(time.Microsecond)`, change to `want.UTC()` vs `got.UTC()`.

- [ ] **Step 7: Run the auth + session integration suites**

Run: `task test:int -- ./internal/auth/postgres/... ./internal/store/... ./internal/session/... ./internal/grpc/...`
Expected: PASS. Includes the session-store scan/write path changes from Step 5 and the floor-comparison consumers in `scope_floor.go`.

- [ ] **Step 8: Verify zero Truncate sites in auth + zero `::timestamptz` casts in session_store.go**

Run: `rg -c "Truncate\(time\.Microsecond\)" internal/auth/postgres/ internal/store/session_store.go`
Expected: zero per file.

Run: `rg -n "::timestamptz" internal/store/session_store.go`
Expected: zero matches.

- [ ] **Step 9: Run `task pr-prep`**

Run: `task pr-prep`
Expected: green.

- [ ] **Step 10: Commit and open Phase 2 PR**

Suggested message: `refactor(auth+sessions): migrate auth/postgres + sessions table timestamps to BIGINT-ns (Phase 2, gfo6)`

---

## Phase 3: world/postgres

### Task 17: Migrate `world/postgres` columns to BIGINT + route through pgnanos

**Files:**

- Create: `internal/store/migrations/000NNN_world_timestamps_to_bigint.up.sql`
- Create: `internal/store/migrations/000NNN_world_timestamps_to_bigint.down.sql`
- Modify: `internal/world/postgres/character_repo.go`
- Modify: `internal/world/postgres/location_repo.go`
- Modify: `internal/world/postgres/exit_repo.go`
- Modify: `internal/world/postgres/object_repo.go`
- Modify: `internal/world/postgres/property_repo.go`
- Modify: `internal/world/postgres/binding_repo.go`
- Modify: `internal/world/postgres/scene_repo.go`

- [ ] **Step 1: Determine the next free migration number**

Run: `ls internal/store/migrations/ | rg -o "^[0-9]+" | sort -u | tail -3`
Expected: the next number after Phase 2.

- [ ] **Step 2: Write the up migration**

Create `internal/store/migrations/<NNN>_world_timestamps_to_bigint.up.sql`. Convert the following columns (per spec Section 2 inventory):

- `characters.created_at`
- `locations.created_at`
- `exits.created_at`
- `objects.created_at`
- `objects.updated_at`
- `entity_properties.created_at`, `entity_properties.updated_at`
- `scene_participants.joined_at` (host-side; the plugin-side migration in Phase 1 covered the plugin's table)
- `player_character_bindings.created_at`, `player_character_bindings.ended_at`
- Other tables per a thorough re-inventory: run `rg -n "TIMESTAMPTZ" internal/store/migrations/000001_baseline.up.sql` and pick out the world-domain tables.

Use the same `ALTER COLUMN ... TYPE BIGINT USING (EXTRACT(EPOCH FROM col) * 1e9)::BIGINT` pattern; set `DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT` for columns that previously had `DEFAULT NOW()`.

- [ ] **Step 3: Write the down migration**

Mirror the up using `to_timestamp(col::double precision / 1e9)`. Include the precision-lossy header comment.

- [ ] **Step 4: Update each repo to route through pgnanos**

For each of the 7 repo files, add the pgnanos import and wrap `time.Time` arguments / scan targets.

- [ ] **Step 5: Strip Truncate calls from world tests**

Run: `rg -n "Truncate\(time\.Microsecond\)" internal/world/postgres/`
Expected: ~75 matches across 5 test files.

Remove each. Where comparisons used the truncate trick, switch to `.UTC()` comparison.

- [ ] **Step 6: Run the world integration suite**

Run: `task test:int -- ./internal/world/postgres/...`
Expected: PASS.

- [ ] **Step 7: Verify zero Truncate sites in world**

Run: `rg -c "Truncate\(time\.Microsecond\)" internal/world/postgres/`
Expected: zero.

- [ ] **Step 8: Run `task pr-prep`**

Run: `task pr-prep`
Expected: green.

- [ ] **Step 9: Commit and open Phase 3 PR**

Suggested message: `refactor(world): migrate world/postgres timestamps to BIGINT-ns (Phase 3, gfo6)`

---

## Phase 4: totp + misc

### Task 18: Migrate totp, admin_approvals, plugins, content_items, aliases columns to BIGINT

**Files:**

- Create: `internal/store/migrations/000NNN_totp_misc_timestamps_to_bigint.up.sql`
- Create: `internal/store/migrations/000NNN_totp_misc_timestamps_to_bigint.down.sql`
- Modify: `internal/totp/repo.go` (player_totp INSERT at line 164, UPDATE at line 229, UPDATE at line 254; **also non-trivial SQL rewrite at lines 228-236 — see Step 5**)
- Modify: `internal/admin/...` (admin_approvals writer)
- Modify: `internal/plugin/manager.go` or `internal/plugin/loader.go` (`plugins` table writer — verify)

Note: `session_history_floor_columns` (location_arrived_at, guest_character_created_at) and the entire `internal/store/session_store.go` conversion were moved to Phase 2 (Task 16 Step 5) so the `sessions` table converts atomically. Phase 4 no longer touches session storage.

- [ ] **Step 1: Determine the next free migration number**

Run: `ls internal/store/migrations/ | rg -o "^[0-9]+" | sort -u | tail -3`

- [ ] **Step 2: Write the up migration**

Cover the columns:

- `player_totp.enrolled_at`, `last_verified_at`, `locked_until`
- `player_totp_recovery_codes.created_at`, `consumed_at`
- `crypto_bootstrap_state.consumed_at`
- `admin_approvals.expires_at`, `approved_at`, `created_at`
- `plugins.first_seen_at`, `last_seen_at`, `gc_at`
- `content_items.created_at`, `updated_at`
- `system_aliases.created_at`, `updated_at`
- `player_aliases.created_at`, `updated_at`
- `access_policies.created_at`, `updated_at`
- `access_policy_versions.changed_at`
- `access_audit_log.timestamp`

Note: `sessions.location_arrived_at` and `sessions.guest_character_created_at` (added in migration 000037) are now migrated in Phase 2 (Task 16) alongside the rest of the `sessions` table. They are no longer in Phase 4 scope.

(If any of these were already covered in Phase 1, 2, or 3, skip them. Verify via `rg "ALTER TABLE <table>" internal/store/migrations/`.)

- [ ] **Step 3: Write the down migration** — mirror pattern.

- [ ] **Step 4: Update each repo file to route through pgnanos**

For `INSERT`/`UPDATE`/`SELECT` paths in `internal/totp/repo.go`, `internal/admin/...`, `internal/plugin/manager.go`: same pattern as Phase 2-3 (wrap `time.Time` args in `pgnanos.From(...)`; scan into `pgnanos.Time` / `*pgnanos.Time`; call `.Time()` at consumers).

- [ ] **Step 5: Rewrite `IncrementFailedAttempts` SQL (BIGINT-native arithmetic)**

`internal/totp/repo.go:228-236` uses `$3::TIMESTAMPTZ + ($4::BIGINT || ' microseconds')::INTERVAL` arithmetic that is type-tied to `TIMESTAMPTZ` semantics. Once `player_totp.locked_until` is `BIGINT`-ns, this expression is semantically wrong: a `TIMESTAMPTZ + INTERVAL` resolves to `timestamptz`, which then conflicts with the `BIGINT` column type for the assignment. The cast was load-bearing for type inference (see the doc-comment at lines 206-227 explaining the `$3::TIMESTAMPTZ` / `$4::BIGINT` requirements); the rewrite must preserve type inference at the new column type.

Replace the constant `q` block:

Before:

```go
	const q = `
		UPDATE player_totp
		SET failed_attempts = failed_attempts + 1,
		    locked_until    = CASE
		      WHEN failed_attempts + 1 >= $2 THEN $3::TIMESTAMPTZ + ($4::BIGINT || ' microseconds')::INTERVAL
		      ELSE locked_until
		    END
		WHERE player_id = $1
		RETURNING wrapped_secret, wrap_key_id, last_used_step, failed_attempts, locked_until`
	var s VerifyState
	s.PlayerID = playerID
	err := dbFromCtx(ctx, r.pool).QueryRow(
		ctx, q,
		playerID, threshold, now, lockoutDuration.Microseconds(),
	).Scan(&s.WrappedSecret, &s.WrapKeyID, &s.LastUsedStep, &s.FailedAttempts, &s.LockedUntil)
```

After:

```go
	// $3::BIGINT and $4::BIGINT casts are load-bearing.
	//
	// $3 carries the floor (now.UnixNano()) and $4 carries the lockout
	// duration in nanoseconds. Both are int64; without the explicit BIGINT
	// casts pgx may infer text encoding and the addition resolves to text
	// concatenation. The CASE branches MUST both resolve to BIGINT so the
	// final assignment to locked_until (BIGINT column) type-checks.
	//
	// The previous TIMESTAMPTZ + INTERVAL formulation was tied to the
	// pre-gfo6 column type; with locked_until as BIGINT-ns the math is
	// plain integer addition.
	const q = `
		UPDATE player_totp
		SET failed_attempts = failed_attempts + 1,
		    locked_until    = CASE
		      WHEN failed_attempts + 1 >= $2 THEN $3::BIGINT + $4::BIGINT
		      ELSE locked_until
		    END
		WHERE player_id = $1
		RETURNING wrapped_secret, wrap_key_id, last_used_step, failed_attempts, locked_until`
	var s VerifyState
	s.PlayerID = playerID
	var lockedUntil *pgnanos.Time
	err := dbFromCtx(ctx, r.pool).QueryRow(
		ctx, q,
		playerID, threshold, now.UnixNano(), lockoutDuration.Nanoseconds(),
	).Scan(&s.WrappedSecret, &s.WrapKeyID, &s.LastUsedStep, &s.FailedAttempts, &lockedUntil)
	if lockedUntil != nil {
		t := lockedUntil.Time()
		s.LockedUntil = &t
	}
```

Note: `now.UnixNano()` and `lockoutDuration.Nanoseconds()` are the int64 forms; `pgnanos.From(now)` would be equivalent for the timestamp but adds an unnecessary type wrap when the downstream SQL is doing plain integer arithmetic on both args. Per INV-TS-2 the lint task `task lint:no-unixnano-in-repos` (Task 21) explicitly scans `internal/totp/` for `UnixNano(` outside `*_test.go`, so this call WILL be flagged. **MUST add `// pgnanos-exempt: SQL-cast boundary for BIGINT arithmetic` on the line containing `now.UnixNano()`** so the lint task accepts the exemption when Phase 5 runs.

Regression-lock the rewrite with an integration test that exercises the lockout path; the existing test at `internal/totp/repo_integration_test.go:285` (`UPDATE player_totp SET failed_attempts = 5, locked_until = $1 WHERE player_id = $2`) covers the column scan, but the **arithmetic branch** of `IncrementFailedAttempts` needs its own test that triggers the `WHEN failed_attempts + 1 >= $2` branch — verify via `rg -n "IncrementFailedAttempts" internal/totp/`; if no integration test currently exercises the lockout-firing branch, add one.

- [ ] **Step 6: Verify session-store conversion landed in Phase 2**

The `sessions` table — including the floor columns `location_arrived_at` and `guest_character_created_at` — is fully converted in Phase 2 (Task 16 Step 5). Phase 4 does not touch `internal/store/session_store.go`. As a safety check before opening the Phase 4 PR:

```bash
rg -n "::timestamptz" internal/store/session_store.go
```

Expected: zero matches. If non-zero, Phase 2 is incomplete; rebase or fix before proceeding.

<!-- dead-content marker — historical: prior plan revision held the session_store.go conversion in Phase 4; collapsed into Phase 2 in plan-review round 3.

**Note on Phase 2 / Phase 4 overlap on this file.** `session_store.go::Set()` writes both auth-domain columns (created_at, updated_at, detached_at, expires_at — migrated in Phase 2) AND the floor columns (migrated in Phase 4). Phase 2 already touched this file for its columns; Phase 4 now finishes the conversion for the floor columns. Both phases edit the same INSERT statement; PR review on Phase 4 must merge cleanly against Phase 2's earlier diff.

**6a — Scan path (e.g., `scanSession` / `scanSessions`).** Add `pgnanos` import. Change the scan target:

```go
	var arrived pgnanos.Time
	var guestCreated pgnanos.Time  // NOT NULL DEFAULT 'epoch' → safe to scan into value type
	err := row.Scan(/*..., other columns, */ &arrived, &guestCreated /*, ...*/)
	info.LocationArrivedAt = arrived.Time()
	info.GuestCharacterCreatedAt = guestCreated.Time()
```

The struct fields `LocationArrivedAt time.Time` and `GuestCharacterCreatedAt time.Time` (in `internal/session/session.go`) MUST remain `time.Time` — `pgnanos.Time` is the **scan seam**, not the in-memory representation. Consumers like `streamScopeFloor` continue to work without changes.

**6b — `Set()` write path (lines 271-281 + 288-289 of `internal/store/session_store.go`).** The current SQL casts `$7::timestamptz` and `$8::timestamptz` and uses `COALESCE($7::timestamptz, now())` / `COALESCE($8::timestamptz, 'epoch'::timestamptz)`. After Phase 4 the column types are BIGINT, so:

- Casts: `$7::timestamptz` → `$7::BIGINT`, `$8::timestamptz` → `$8::BIGINT`.
- COALESCE defaults: `now()` → `(EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT`; `'epoch'::timestamptz` → `0::BIGINT` (epoch zero in ns).
- Arguments to `s.pool.Exec`: parameters `$7` and `$8` were previously `*time.Time` (nullable) or `time.Time`. Change to `*pgnanos.Time` / `pgnanos.Time` respectively. Where the caller has a zero `time.Time`, pass `nil` (for `*pgnanos.Time`) so the COALESCE falls back to the BIGINT default. Where it has a value, wrap with `pgnanos.From(t)` (and take the address if the parameter shape requires `*pgnanos.Time`).

Before:

```go
	`INSERT INTO sessions (..., location_arrived_at, guest_character_created_at, ...)
	 VALUES (...,
		COALESCE($7::timestamptz, now()),
		COALESCE($8::timestamptz, 'epoch'::timestamptz),
		...)
	 ON CONFLICT (id) DO UPDATE SET
		...
		location_arrived_at = COALESCE($7::timestamptz, sessions.location_arrived_at),
		guest_character_created_at = COALESCE($8::timestamptz, sessions.guest_character_created_at),
		...`
```

After:

```go
	`INSERT INTO sessions (..., location_arrived_at, guest_character_created_at, ...)
	 VALUES (...,
		COALESCE($7::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT),
		COALESCE($8::BIGINT, 0::BIGINT),
		...)
	 ON CONFLICT (id) DO UPDATE SET
		...
		location_arrived_at = COALESCE($7::BIGINT, sessions.location_arrived_at),
		guest_character_created_at = COALESCE($8::BIGINT, sessions.guest_character_created_at),
		...`
```

**6c — `UpdateLocationOnMove()` (line 770-773).** Wrap both args:

Before:

```go
	query := `UPDATE sessions
	          SET location_id = $1, location_arrived_at = $2, updated_at = $2
	          WHERE character_id = $3 AND status = 'active'`
	_, err := s.pool.Exec(ctx, query, newLocationID.String(), arrivedAt, characterID.String())
```

After:

```go
	query := `UPDATE sessions
	          SET location_id = $1, location_arrived_at = $2, updated_at = $2
	          WHERE character_id = $3 AND status = 'active'`
	_, err := s.pool.Exec(ctx, query, newLocationID.String(), pgnanos.From(arrivedAt), characterID.String())
```

Note: `updated_at` is migrated in **Phase 2**, so by the time Phase 4 lands, both `location_arrived_at` and `updated_at` are BIGINT — the single `pgnanos.From(arrivedAt)` argument satisfies both column writes via the `$2` placeholder.

**6d — `BumpLocationArrivedAt()` (line 798-801).** Same pattern:

Before:

```go
	query := `UPDATE sessions
	          SET location_arrived_at = $1, updated_at = $1
	          WHERE id = $2`
	res, err := s.pool.Exec(ctx, query, arrivedAt, sessionID)
```

After:

```go
	query := `UPDATE sessions
	          SET location_arrived_at = $1, updated_at = $1
	          WHERE id = $2`
	res, err := s.pool.Exec(ctx, query, pgnanos.From(arrivedAt), sessionID)
```

**Verify after all four edits:** `rg -n "::timestamptz\|::TIMESTAMPTZ" internal/store/session_store.go` should return zero matches.

-->

- [ ] **Step 7: Strip Truncate calls**

Run: `rg -n "Truncate\(time\.Microsecond\)" internal/totp/ internal/store/`
Expected: ~9 matches.

Remove each.

- [ ] **Step 8: Run touched test suites**

Run: `task test:int -- ./internal/totp/... ./internal/admin/... ./internal/plugin/... ./internal/store/... ./internal/session/...`
Expected: PASS — includes the session-store scan-path changes from Step 6.

- [ ] **Step 9: Verify zero remaining Truncate sites repo-wide**

Run: `rg -c "Truncate\(time\.Microsecond\)" --type=go`
Expected: zero (or only matches inside `internal/pgnanos/` comments referencing the pattern historically).

- [ ] **Step 10: Run `task pr-prep`**

Expected: green.

- [ ] **Step 11: Commit and open Phase 4 PR**

Suggested message: `refactor: migrate totp + misc timestamps to BIGINT-ns (Phase 4, gfo6)`

---

## Phase 5: Docs + Lint Guard

### Task 19: Add `task lint:no-timestamptz` migration-schema guard

**Files:**

- Modify: `Taskfile.yaml` (add the new task to the lint namespace)

- [ ] **Step 1: Inspect existing lint tasks for patterns**

Run: `rg -n "lint:" Taskfile.yaml | head -10`
Expected: shows the lint namespace structure.

- [ ] **Step 2: Add the new task**

Open `Taskfile.yaml`. Inside the lint namespace, add:

```yaml
  lint:no-timestamptz:
    desc: "Reject new TIMESTAMPTZ/TIMESTAMP columns in migrations (INV-TS-1)"
    cmds:
      - |
        # Historic migrations (those landing before holomush-gfo6) legitimately
        # contain TIMESTAMPTZ literals — both the pre-conversion baseline and
        # each Phase N ALTER COLUMN ... TYPE BIGINT USING ... migration. We
        # only want to catch NEW migrations adding TIMESTAMPTZ columns.
        #
        # Strategy: read the marker file written at gfo6 land-time, then only
        # lint migrations whose filename number is strictly greater than the
        # marker. The marker file lives at:
        #   internal/store/migrations/.gfo6-cutoff (host)
        #   plugins/core-scenes/migrations/.gfo6-cutoff (plugin)
        # and contains the highest migration number landed as part of gfo6
        # (e.g., "000041" if Phase 4 was the last to land at 000041).
        host_cutoff=$(cat internal/store/migrations/.gfo6-cutoff 2>/dev/null || echo "000000")
        plugin_cutoff=$(cat plugins/core-scenes/migrations/.gfo6-cutoff 2>/dev/null || echo "000000")

        violations=0
        for f in internal/store/migrations/*.up.sql; do
          num=$(basename "$f" | rg -o "^[0-9]+")
          if [[ "$num" > "$host_cutoff" ]]; then
            if rg -n "TIMESTAMPTZ|TIMESTAMP WITH TIME ZONE|\bTIMESTAMP\b" "$f" | rg -v "pgnanos-exempt:" ; then
              violations=$((violations + 1))
            fi
          fi
        done
        for f in plugins/*/migrations/*.up.sql; do
          num=$(basename "$f" | rg -o "^[0-9]+")
          dir=$(dirname "$f")
          this_cutoff=$(cat "$dir/.gfo6-cutoff" 2>/dev/null || echo "000000")
          if [[ "$num" > "$this_cutoff" ]]; then
            if rg -n "TIMESTAMPTZ|TIMESTAMP WITH TIME ZONE|\bTIMESTAMP\b" "$f" | rg -v "pgnanos-exempt:" ; then
              violations=$((violations + 1))
            fi
          fi
        done
        if [[ "$violations" -gt 0 ]]; then
          echo "FAIL: $violations TIMESTAMPTZ/TIMESTAMP violations in post-gfo6 migrations. Use BIGINT (epoch ns)."
          echo "If unavoidable, annotate with '-- pgnanos-exempt: <reason>' on the same line."
          exit 1
        fi
```

- [ ] **Step 3: Create the cutoff marker files**

The lint task only flags migrations whose number is strictly greater than the cutoff. Capture the highest migration numbers landed during Phases 1-4 as the cutoff:

```bash
ls internal/store/migrations/ | rg -o "^[0-9]+" | sort -u | tail -1 \
  > internal/store/migrations/.gfo6-cutoff
ls plugins/core-scenes/migrations/ | rg -o "^[0-9]+" | sort -u | tail -1 \
  > plugins/core-scenes/migrations/.gfo6-cutoff
```

Commit both `.gfo6-cutoff` files alongside the lint task; without them, the task short-circuits to `000000` and scans every historic migration, which contains legitimate `TIMESTAMPTZ` strings (the pre-conversion baseline + every `ALTER COLUMN ... TYPE BIGINT USING ...` migration).

- [ ] **Step 4: Verify the task fails on a synthetic post-cutoff TIMESTAMPTZ column**

Create a temporary file with a number above the cutoff:

```bash
echo "ALTER TABLE foo ADD COLUMN bar TIMESTAMPTZ;" \
  > internal/store/migrations/999998_synthetic_test.up.sql
```

Run: `task lint:no-timestamptz`
Expected: FAIL (synthetic violation).

Remove the synthetic file: `rm internal/store/migrations/999998_synthetic_test.up.sql`

- [ ] **Step 5: Verify the task passes on the current tree**

Run: `task lint:no-timestamptz`
Expected: PASS (no migrations with number > cutoff exist post-Phase-4).

- [ ] **Step 6: Commit**

Suggested message: `chore(taskfile): add lint:no-timestamptz guard (INV-TS-1, Phase 5.1, gfo6)`

### Task 20: Add `task lint:no-microsecond-truncate` guard

**Files:**

- Modify: `Taskfile.yaml`

- [ ] **Step 1: Add the new task**

Append to the lint namespace:

```yaml
  lint:no-microsecond-truncate:
    desc: "Reject .Truncate(time.Microsecond) in code (INV-TS-3)"
    cmds:
      - |
        if rg -n "Truncate\(time\.Microsecond\)" --type=go \
             | rg -v "pgnanos-exempt:" ; then
          echo "FAIL: .Truncate(time.Microsecond) found. Persistent timestamps are BIGINT-ns; truncation is no longer needed."
          echo "If you have a non-PG external system that requires µs, annotate with '// pgnanos-exempt: <reason>' on the same line."
          exit 1
        fi
```

- [ ] **Step 2: Run the new task**

Run: `task lint:no-microsecond-truncate`
Expected: PASS (after Phases 1-4 cleanup).

If it FAILs, fix any remaining Truncate sites missed in earlier phases.

- [ ] **Step 3: Commit**

Suggested message: `chore(taskfile): add lint:no-microsecond-truncate guard (INV-TS-3, Phase 5.2, gfo6)`

### Task 21: Add `task lint:no-unixnano-in-repos` guard

**Files:**

- Modify: `Taskfile.yaml`

- [ ] **Step 1: Add the new task**

```yaml
  lint:no-unixnano-in-repos:
    desc: "Reject UnixNano() / time.Unix(0, ...) in repo packages outside pgnanos (INV-TS-2)"
    cmds:
      - |
        if rg -n "UnixNano\(\)|time\.Unix\(0," \
             internal/auth/postgres/ internal/world/postgres/ internal/totp/ \
             internal/eventbus/history/ plugins/*/ \
             --type=go \
             -g '!*_test.go' -g '!internal/pgnanos/**' \
             | rg -v "pgnanos-exempt:" ; then
          echo "FAIL: raw UnixNano() / time.Unix(0, ...) in repo code. Use internal/pgnanos.Time as the scan/insert seam."
          echo "If unavoidable, annotate with '// pgnanos-exempt: <reason>' on the same line."
          exit 1
        fi
```

- [ ] **Step 2: Run the new task**

Run: `task lint:no-unixnano-in-repos`
Expected: PASS.

- [ ] **Step 3: Commit**

Suggested message: `chore(taskfile): add lint:no-unixnano-in-repos guard (INV-TS-2, Phase 5.3, gfo6)`

### Task 22: Wire the three lint tasks into `pr-prep` and `task lint`

**Files:**

- Modify: `Taskfile.yaml`

- [ ] **Step 1: Locate the `lint` aggregate task**

Run: `rg -n "^  lint:" Taskfile.yaml | head -10`
Expected: a task `lint:` (no suffix) that runs the suite.

- [ ] **Step 2: Add the three new tasks as dependencies of `lint`**

In the `lint:` task's `deps:` (or `cmds:` calling sub-tasks), add:

```yaml
      - lint:no-timestamptz
      - lint:no-microsecond-truncate
      - lint:no-unixnano-in-repos
```

- [ ] **Step 3: Verify `task lint` runs all three**

Run: `task lint`
Expected: PASS — all three new lints run alongside existing lints.

- [ ] **Step 4: Verify pr-prep picks them up**

Run: `task pr-prep`
Expected: PASS (the pr-prep pipeline depends on `task lint`).

- [ ] **Step 5: Add the `TestLintNoMicrosecondTruncatePasses` test that satisfies INV-TS-3's meta-test requirement**

Create `internal/store/lint_meta_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestLintNoMicrosecondTruncatePasses pins INV-TS-3 via the lint task.
// If anyone reintroduces a .Truncate(time.Microsecond) call without a
// pgnanos-exempt annotation, this test fails along with the lint.
func TestLintNoMicrosecondTruncatePasses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping shell-out lint test in short mode")
	}
	cmd := exec.Command("task", "lint:no-microsecond-truncate")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("INV-TS-3 violated: %s\n%s", err, strings.TrimSpace(string(out)))
	}
}
```

Create the companion `TestNoTimestamptzColumnsAfterMigration` in `internal/store/migrate_integration_test.go` to satisfy INV-TS-1's meta-test reference:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/test/testutil"
)

// TestNoTimestamptzColumnsAfterMigration pins INV-TS-1. After all
// migrations run, no holomush-owned schema may contain a TIMESTAMPTZ
// or TIMESTAMP column. Plugin tables are checked too.
func TestNoTimestamptzColumnsAfterMigration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	env := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, env) // returns a connection string
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	defer pool.Close()
	// Migrations are run by FreshDatabase; nothing more to do.

	rows, err := pool.Query(ctx, `
		SELECT table_schema, table_name, column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = ANY($1)
		  AND data_type IN ('timestamp without time zone', 'timestamp with time zone')
		ORDER BY table_schema, table_name, column_name
	`, []string{"public", "plugin_core_scenes"})
	require.NoError(t, err)
	defer rows.Close()

	var violations []string
	for rows.Next() {
		var schema, table, col, dataType string
		require.NoError(t, rows.Scan(&schema, &table, &col, &dataType))
		violations = append(violations,
			schema+"."+table+"."+col+" ("+dataType+")")
	}
	require.NoError(t, rows.Err())

	assert.Empty(t, violations,
		"INV-TS-1: holomush schemas MUST NOT contain TIMESTAMPTZ/TIMESTAMP columns after migration. Violations: %v",
		violations)
}
```

If `plugin_core_scenes` is not the actual plugin schema name, replace with the correct value — verify via `rg "SET LOCAL search_path|CREATE SCHEMA" plugins/core-scenes/migrations/`.

- [ ] **Step 6: Remove the `phaseFiveDeferred` skip-guard from `TestNanosecondTimestampsInvariantsHaveNamedTests`**

Now that both `TestNoTimestamptzColumnsAfterMigration` (Step 5) and `TestLintNoMicrosecondTruncatePasses` (Task 22 Step 5) exist, the Phase-1 meta-test's skip-guard is no longer needed. Open `internal/store/spec_meta_test.go` and delete:

```go
	phaseFiveDeferred := map[string]struct{}{
		"INV-TS-1": {},
		"INV-TS-3": {},
	}
```

Then simplify the loop body back to the unconditional form:

```go
	for _, tc := range cases {
		t.Run(tc.inv, func(t *testing.T) {
			if _, ok := testNames[tc.testName]; !ok {
				t.Errorf("spec invariant %s names test %q, but no such Test* function exists anywhere in the repo",
					tc.inv, tc.testName)
			}
		})
	}
```

- [ ] **Step 7: Run the meta-tests**

Run: `task test -- ./internal/store/`
Expected: PASS — all INV-TS-N subtests assert (no skips), including the two formerly deferred ones.

- [ ] **Step 8: Commit**

Suggested message: `chore: wire ns-timestamp lint guards into pr-prep (Phase 5.4, gfo6)`

### Task 23: Update `site/docs/contributing/database-migrations.md` to document the BIGINT-ns convention

**Files:**

- Modify: `site/docs/contributing/database-migrations.md`

- [ ] **Step 1: Read the current state of the doc**

Run: `head -50 site/docs/contributing/database-migrations.md`
Expected: shows the migration-writing guide.

- [ ] **Step 2: Add a new section "Timestamp columns: BIGINT epoch nanoseconds"**

Append a new section to the doc with the following structure (avoid nested
fenced code blocks per `docs/CLAUDE.md` — describe inline rather than
quoting the whole section):

- Heading: `## Timestamp columns: BIGINT epoch nanoseconds`
- Opening paragraph citing `gfo6` (INV-TS-1) and stating that all new
  migrations MUST use `BIGINT` for persistent time values, storing
  nanoseconds since UNIX epoch (UTC); `TIMESTAMPTZ` and `TIMESTAMP` are
  prohibited.
- A `**Schema pattern:**` sub-heading followed by a SQL code block:

```sql
CREATE TABLE thing (
    id          TEXT PRIMARY KEY,
    created_at  BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    updated_at  BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
);
```

- An `**Application code pattern:**` sub-heading followed by a Go code block:

```go
import "github.com/holomush/holomush/internal/pgnanos"

// Insert
_, err := pool.Exec(ctx, `INSERT INTO thing (id, created_at) VALUES ($1, $2)`,
    id, pgnanos.From(t))

// Scan
var createdAt pgnanos.Time
err := row.Scan(&id, &createdAt)
t := createdAt.Time()
```

- A **Why BIGINT instead of `TIMESTAMPTZ`:** paragraph covering: structural
  byte-equal AAD reconstruction (INV-TS-5), no `Truncate(time.Microsecond)`
  discipline, deterministic ordering at nanosecond resolution. Point to the
  spec for the full rationale and the rejected alternative (`timestamp9`
  PG extension).
- An `**Enforcement:**` paragraph: `task lint:no-timestamptz` rejects new
  `TIMESTAMPTZ`/`TIMESTAMP` columns. Escape hatch: `-- pgnanos-exempt: <reason>`
  on the same line.

- [ ] **Step 3: Run docs lint**

Run: `task lint:docs` (or `rumdl` directly if that's the project's convention)
Expected: PASS.

- [ ] **Step 4: Commit**

Suggested message: `docs: document BIGINT-ns timestamp convention (Phase 5.5, gfo6)`

### Task 24: Open Phase 5 PR and close `gfo6`

**Files:**

- (no files; verification + bd state)

- [ ] **Step 1: Run `task pr-prep`**

Run: `task pr-prep`
Expected: green.

- [ ] **Step 2: Open the Phase 5 PR**

Use `gh pr create`. Title: `Phase 5: lint guards + docs for ns-timestamps (gfo6)`.

- [ ] **Step 3: After merge, close the design bead**

Run: `bd close holomush-gfo6 --reason="Spec, plan, and all five phases implemented and merged"`
Expected: bead closes.

- [ ] **Step 4: File the conditional follow-up beads (if any work surfaced)**

If the wire-layer ns investigation became actionable during implementation, file via `bd create`. Otherwise it remains an open P3 follow-up (filed at spec time per Section 7).

---

## References

- Spec: `docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md`
- Design bead: `holomush-gfo6`
- pgnanos helper: `internal/pgnanos/`
- AAD canonical encoding: `internal/eventbus/crypto/aad/aad.go:62-117`
- Publisher truncate site (Phase 1 deletion target): `internal/eventbus/publisher.go:202`
- Floor truncate site (Phase 1 deletion target): `internal/grpc/server.go:1100` (`dispatchDelivery`)
- Stale regression test retired in Phase 1: `internal/grpc/subscribe_loop_test.go:326`
- Phase 7 meta-test pattern mirrored by INV-TS-META: `internal/eventbus/history/phase7_boundary_meta_test.go:47`
- `time.Sleep` tie-prevention hack removed in Phase 1: `test/integration/privacy/privacy_test.go:141`

<!-- adr-capture: sha256=8686cab809c0b8fe; session=brainstorm-gfo6; ts=2026-05-22T15:07:38Z; adrs=holomush-absb,holomush-rbw6,holomush-f5h0 -->
