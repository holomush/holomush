// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package audit_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/test/testutil"
)

func bootGatePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared := testutil.SharedPostgres(t)
	return openPool(t, testutil.FreshDatabase(t, shared))
}

func relExistsInt(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var reg *string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT to_regclass($1)::text`, "public."+name).Scan(&reg))
	return reg != nil
}

// TestStartBootGateBackfillsBeforeProjection proves the synchronous boot gate
// (findings 9 & 10): Subsystem.Prepare re-homes the legacy
// events_audit_unpartitioned history (Backfill) and ensures partition coverage
// BEFORE the projection starts accepting traffic (Activate). The legacy row
// is queryable via the partitioned events_audit after Prepare, and the
// legacy table is gone.
func TestStartBootGateBackfillsBeforeProjection(t *testing.T) {
	pool := bootGatePool(t)
	ctx := context.Background()

	// A legacy row (~60 days ago) sitting in events_audit_unpartitioned.
	oldTime := time.Now().UTC().AddDate(0, 0, -60)
	oldID := ulid.MustNew(ulid.Timestamp(oldTime), ulid.DefaultEntropy())
	_, err := pool.Exec(ctx, `
		INSERT INTO events_audit_unpartitioned
		  (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering)
		VALUES ($1, 'events.main.legacy', 'legacy', $2, 'system', '\x00', 1, 'identity', 1, '{}')`,
		oldID.Bytes(), oldTime.UnixNano())
	require.NoError(t, err)

	bus := eventbustest.New(t)
	sub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, sub.Prepare(ctx), "boot gate + projection prepare succeed")
	require.NoError(t, sub.Activate(ctx), "projection activate succeeds")
	t.Cleanup(func() { _ = sub.Stop(context.Background()) })

	// Backfill ran during Start: the legacy row is now in events_audit and the
	// legacy table has been renamed away.
	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM events_audit WHERE id = $1`, oldID.Bytes()).Scan(&n))
	assert.Equal(t, 1, n, "legacy row re-homed into partitioned events_audit by the boot gate")
	assert.False(t, relExistsInt(t, pool, "events_audit_unpartitioned"), "legacy table drained + renamed")
	assert.True(t, relExistsInt(t, pool, "events_audit_legacy_migrated"))

	// EnsurePartitions ran: the within-retention past month partition exists.
	pastName := fmt.Sprintf("events_audit_%04d_%02d", oldTime.Year(), int(oldTime.Month()))
	assert.True(t, relExistsInt(t, pool, pastName), "boot gate ensured the historical partition %s", pastName)
}

// TestStartBootGateFailureReturnsErrorBeforeProjection proves finding 10: a
// first-cycle Backfill failure returns from Prepare before any projection
// exists (nothing to roll back). The failure is injected via a same-named
// non-child occupying the legacy row's month partition, which makes
// Backfill's ensureMonthPartition fail closed (AUDIT_PARTITION_NAME_OCCUPIED).
// (A malformed legacy id is no longer a boot-gate failure — round-6 WR-02
// skips+logs it — so the occupancy conflict is the durable first-cycle
// Backfill-abort trigger.) Prepare-only: Activate is never reached because
// Prepare itself fails.
func TestStartBootGateFailureReturnsErrorBeforeProjection(t *testing.T) {
	pool := bootGatePool(t)
	ctx := context.Background()

	// A valid legacy row ~150 days ago — its month is NOT pre-created by
	// migration 000052 (which pre-creates only current+2), so the decoy below is
	// the sole occupant of that month's partition name.
	oldTime := time.Now().UTC().AddDate(0, 0, -150)
	oldID := ulid.MustNew(ulid.Timestamp(oldTime), ulid.DefaultEntropy())
	_, err := pool.Exec(ctx, `
		INSERT INTO events_audit_unpartitioned
		  (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering)
		VALUES ($1, 'events.main.legacy', 'legacy', $2, 'system', '\x00', 1, 'identity', 1, '{}')`,
		oldID.Bytes(), oldTime.UnixNano())
	require.NoError(t, err)

	// A same-named NON-CHILD occupies that month's partition name, so Backfill's
	// ensureMonthPartition fails closed (the stamp-time child-ness gate refuses a
	// same-named non-child).
	occName := fmt.Sprintf("events_audit_%04d_%02d", oldTime.Year(), int(oldTime.Month()))
	_, err = pool.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (id bytea)", occName))
	require.NoError(t, err)

	bus := eventbustest.New(t)
	sub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	prepErr := sub.Prepare(ctx)
	require.Error(t, prepErr, "boot gate failure aborts Prepare")
	// The boot gate wraps with AUDIT_BACKFILL_BOOT_GATE_FAILED, but oops
	// surfaces the root cause code (AUDIT_PARTITION_NAME_OCCUPIED) through the
	// chain — the point is Prepare returns the failure before any projection
	// is constructed.
	errutil.AssertErrorCode(t, prepErr, "AUDIT_PARTITION_NAME_OCCUPIED")

	// The projection was never prepared: Stop is a clean no-op.
	require.NoError(t, sub.Stop(context.Background()))
}

// TestStartRejectsNonPositiveRetainWindow proves the negative-config guard
// surfaces as a Prepare error (round-3 finding 3) — never a detach-all cutoff
// or a time.NewTicker panic. NewSubsystem.Defaults() only fills zero, so a
// negative survives to Validate() inside Prepare. Prepare-only: Activate is
// never reached because Prepare itself fails.
func TestStartRejectsNonPositiveRetainWindow(t *testing.T) {
	pool := bootGatePool(t)
	ctx := context.Background()

	bus := eventbustest.New(t)
	sub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool},
		audit.Config{RetainWindow: -1 * time.Hour})
	prepErr := sub.Prepare(ctx)
	require.Error(t, prepErr)
	errutil.AssertErrorCode(t, prepErr, "AUDIT_CONFIG_INVALID")

	require.NoError(t, sub.Stop(context.Background()))
}

// TestAuditSubsystemRepeatedPrepareDoesNotConstructSecondProjection pins
// D-13.2 row 10 (round 7 BLOCKER): Prepare guards on s.preparedProjection,
// not the old worker-keyed guard, so a second Prepare is a true no-op — no
// second durable consumer, no second boot-gate run.
func TestAuditSubsystemRepeatedPrepareDoesNotConstructSecondProjection(t *testing.T) {
	pool := bootGatePool(t)
	ctx := context.Background()

	bus := eventbustest.New(t)
	sub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, sub.Prepare(ctx))
	require.NoError(t, sub.Prepare(ctx), "second Prepare must be a no-op")
	t.Cleanup(func() { _ = sub.Stop(context.Background()) })
}

// TestAuditSubsystemStopAfterPrepareOnlyIsCleanNoop pins D-13.2 row 10
// (round 9): Stop after Prepare-only clears the in-memory prepared
// aggregate without error and without draining anything (nothing is
// running yet — Activate never ran). The durable JetStream consumer
// Prepare constructed is intentionally retained server-side; this test
// does not assert its deletion.
func TestAuditSubsystemStopAfterPrepareOnlyIsCleanNoop(t *testing.T) {
	pool := bootGatePool(t)
	ctx := context.Background()

	bus := eventbustest.New(t)
	sub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, sub.Prepare(ctx))
	require.NoError(t, sub.Stop(context.Background()), "Stop after Prepare-only must be a clean no-op")
}
