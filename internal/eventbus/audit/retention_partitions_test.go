// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package audit

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	retaudit "github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/test/testutil"
)

// containsPrefix reports whether any name in names starts with prefix.
func containsPrefix(names []string, prefix string) bool {
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}
	return false
}

// --- test helpers ---------------------------------------------------------

// eventMSForMonth returns an event_ms comfortably inside the month starting at
// ms (the 10th at midnight UTC), so a seeded row lands in that month's
// partition.
func eventMSForMonth(ms time.Time) int64 {
	return monthStart(ms).AddDate(0, 0, 9).UnixNano()
}

// seedAuditRow inserts a minimal valid events_audit row at the given event_ms.
// It requires a covering partition to already exist.
func seedAuditRow(t *testing.T, pool *pgxpool.Pool, eventMS int64) {
	t.Helper()
	id := ulid.Make().Bytes()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO events_audit
		  (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering, event_ms)
		VALUES ($1, 'events.main.test', 'test', $2, 'system', '\x00', 1, 'identity', 1, '{}', $3)`,
		id, eventMS, eventMS)
	require.NoError(t, err)
}

// objDescription returns the COMMENT ON TABLE marker for a relation, or nil if
// the relation carries no comment.
func objDescription(t *testing.T, pool *pgxpool.Pool, name string) *string {
	t.Helper()
	var desc *string
	err := pool.QueryRow(context.Background(),
		`SELECT obj_description(($1::regclass)::oid, 'pg_class')`, schemaName+"."+name).Scan(&desc)
	require.NoError(t, err)
	return desc
}

func relExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var reg *string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT to_regclass($1)::text`, schemaName+"."+name).Scan(&reg))
	return reg != nil
}

func isChildOfEventsAudit(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT EXISTS (
		  SELECT 1 FROM pg_inherits i
		    JOIN pg_class c      ON c.oid = i.inhrelid
		    JOIN pg_namespace n  ON n.oid = c.relnamespace
		    JOIN pg_class p      ON p.oid = i.inhparent
		    JOIN pg_namespace pn ON pn.oid = p.relnamespace
		   WHERE pn.nspname = $1 AND p.relname = $2 AND n.nspname = $1 AND c.relname = $3)`,
		schemaName, eventsAuditTable, name).Scan(&exists))
	return exists
}

// detachedChildNames returns all detached-named relations carrying the
// provenance marker whose canonical prefix matches `canonicalPrefix`.
func detachedNamesWithPrefix(t *testing.T, pool *pgxpool.Pool, canonicalPrefix string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = $1 AND c.relname LIKE $2`,
		schemaName, canonicalPrefix+"%"+detachedInfix+"%")
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	require.NoError(t, rows.Err())
	return out
}

// --- tests ----------------------------------------------------------------

// TestEnsurePartitionsCoversRetentionBackwardAndStampsMarker proves
// EnsurePartitions creates a covering partition for a within-retention PAST
// event_ms (not only current+forward) and stamps the durable provenance marker
// on every genuine created child.
func TestEnsurePartitionsCoversRetentionBackwardAndStampsMarker(t *testing.T) {
	pool := auditIdemPool(t)
	ctx := context.Background()
	mgr := NewEventsAuditPartitionManager(pool, 90*24*time.Hour)

	require.NoError(t, mgr.EnsurePartitions(ctx, 1))

	// A within-retention past event_ms (30 days ago) must land in a real,
	// current child partition.
	pastMonth := monthStart(time.Now().UTC().AddDate(0, 0, -30))
	pastName := partitionNameForMonth(pastMonth)
	require.True(t, relExists(t, pool, pastName), "backward partition %s must exist", pastName)
	require.True(t, isChildOfEventsAudit(t, pool, pastName), "backward partition must be a current child")

	// Seeding a row with a past event_ms must succeed (covering partition).
	seedAuditRow(t, pool, eventMSForMonth(pastMonth))

	// The created child carries the durable marker.
	desc := objDescription(t, pool, pastName)
	require.NotNil(t, desc, "created partition must carry provenance marker")
	assert.Equal(t, partitionProvenanceMarker, *desc)

	// Re-running is idempotent (no error, marker unchanged).
	require.NoError(t, mgr.EnsurePartitions(ctx, 1))
}

// TestEnsurePartitionsLandsChildrenInPublicUnderLeakedSearchPath proves the
// PartitionManager's DDL is robust to a non-public session search_path leaked
// onto the pooled connection (the eventbus_e2e harness's non-LOCAL
// `SET search_path` × bare-DDL interaction). A partition child is placed in the
// CURRENT schema (search_path head), independent of the parent's schema, so
// bare `CREATE ... PARTITION OF` under a leaked search_path would land the child
// in the wrong namespace and the public-pinned child-ness gate would fail closed
// with AUDIT_PARTITION_NAME_OCCUPIED. Schema-qualifying every write to public
// makes it land in public unconditionally.
func TestEnsurePartitionsLandsChildrenInPublicUnderLeakedSearchPath(t *testing.T) {
	shared := testutil.SharedPostgres(t)
	cfg, err := pgxpool.ParseConfig(testutil.FreshDatabase(t, shared))
	require.NoError(t, err)
	// A single connection so a non-LOCAL SET leaks onto every later operation
	// (pgxpool performs no session reset on release), faithfully reproducing the
	// harness leak.
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	ctx := context.Background()

	// Leak a non-public search_path onto the pooled connection.
	_, err = pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS leaked_ns")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "SET search_path TO leaked_ns, public")
	require.NoError(t, err)

	// A 180-day retention window forces a backward span reaching ~6 months back,
	// including a month migration 000052 did NOT pre-create (it pre-creates only
	// current+2), so a real CREATE is exercised under the leaked search_path.
	mgr := NewEventsAuditPartitionManager(pool, 180*24*time.Hour)
	require.NoError(t, mgr.EnsurePartitions(ctx, 1),
		"EnsurePartitions must not fail closed under a leaked non-public search_path")

	pastMonth := monthStart(time.Now().UTC().AddDate(0, -5, 0))
	pastName := partitionNameForMonth(pastMonth)

	// The child must be a genuine current child of public.events_audit.
	require.True(t, isChildOfEventsAudit(t, pool, pastName),
		"backward partition %s must be a current public child", pastName)

	// Its namespace must be exactly public, never the leaked schema.
	var nsp string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE c.relname = $1`, pastName).Scan(&nsp))
	require.Equal(t, "public", nsp, "child partition must be created in public, not the leaked schema")

	// No stray copy leaked into the non-public schema.
	var leaked *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT to_regclass($1)::text`, "leaked_ns."+pastName).Scan(&leaked))
	require.Nil(t, leaked, "no partition child leaked into the non-public schema")
}

// TestDetachRenameDropByNameAge proves the full prune cycle: an old partition
// is DETACHed, renamed to the _detached_<unix> form (marker preserved), and
// DROPped past grace, while a recent partition is retained and a just-detached
// partition within grace is NOT dropped.
func TestDetachRenameDropByNameAge(t *testing.T) {
	pool := auditIdemPool(t)
	ctx := context.Background()
	mgr := NewEventsAuditPartitionManager(pool, 90*24*time.Hour)

	now := time.Now().UTC()
	oldA := monthStart(now.AddDate(0, -6, 0))
	oldB := monthStart(now.AddDate(0, -5, 0))
	recent := monthStart(now)

	require.NoError(t, mgr.ensureMonthPartition(ctx, oldA))
	require.NoError(t, mgr.ensureMonthPartition(ctx, oldB))
	require.NoError(t, mgr.ensureMonthPartition(ctx, recent))
	seedAuditRow(t, pool, eventMSForMonth(oldA))
	seedAuditRow(t, pool, eventMSForMonth(oldB))
	seedAuditRow(t, pool, eventMSForMonth(recent))

	oldAName := partitionNameForMonth(oldA)
	oldBName := partitionNameForMonth(oldB)
	recentName := partitionNameForMonth(recent)

	// Detach everything older than 3 months ago.
	renamed, err := mgr.DetachExpiredPartitions(ctx, now.AddDate(0, -3, 0))
	require.NoError(t, err)
	require.Len(t, renamed, 2, "both old partitions detached: %v", renamed)

	// Old partitions are no longer children and no longer carry the canonical
	// name; the recent one is retained.
	assert.False(t, isChildOfEventsAudit(t, pool, oldAName), "oldA detached")
	assert.False(t, isChildOfEventsAudit(t, pool, oldBName), "oldB detached")
	assert.True(t, isChildOfEventsAudit(t, pool, recentName), "recent retained")
	assert.False(t, relExists(t, pool, oldAName), "oldA canonical name freed by rename")

	// The marker survives DETACH + the _detached_<unix> rename.
	for _, name := range renamed {
		desc := objDescription(t, pool, name)
		require.NotNil(t, desc, "detached table %s keeps its marker", name)
		assert.Equal(t, partitionProvenanceMarker, *desc)
	}

	// Force oldA's detached table to an OLD epoch so it is past grace, leaving
	// oldB fresh (within grace).
	detachedA := detachedNamesWithPrefix(t, pool, oldAName)
	require.Len(t, detachedA, 1)
	agedName := fmt.Sprintf("%s%s%d", oldAName, detachedInfix, now.Add(-30*24*time.Hour).Unix())
	_, err = pool.Exec(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s",
		pgx.Identifier{detachedA[0]}.Sanitize(), pgx.Identifier{agedName}.Sanitize()))
	require.NoError(t, err)

	dropped, err := mgr.DropDetachedPartitions(ctx, 7*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, []string{agedName}, dropped, "only the past-grace detached table is dropped")

	assert.False(t, relExists(t, pool, agedName), "past-grace detached table dropped")
	// oldB's detached table is within grace → retained.
	detachedB := detachedNamesWithPrefix(t, pool, oldBName)
	require.Len(t, detachedB, 1, "within-grace detached table retained")
	assert.True(t, relExists(t, pool, detachedB[0]))
}

// TestReconcileCrashOrphanedPartition proves a canonical-named partition that
// was DETACHed but not renamed (a cycle that crashed mid-way) is reconciled on
// the next cycle (renamed to _detached_<unix>) and eventually dropped — never
// permanently stranded. It carries the provenance marker, so the marker-gated
// reconcile claims it.
func TestReconcileCrashOrphanedPartition(t *testing.T) {
	pool := auditIdemPool(t)
	ctx := context.Background()
	mgr := NewEventsAuditPartitionManager(pool, 90*24*time.Hour)

	now := time.Now().UTC()
	orphanMonth := monthStart(now.AddDate(0, -8, 0))
	require.NoError(t, mgr.ensureMonthPartition(ctx, orphanMonth))
	orphanName := partitionNameForMonth(orphanMonth)

	// Simulate a crash after DETACH but before the rename: plain (blocking)
	// DETACH leaves the canonical-named table a marker-carrying non-child.
	_, err := pool.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DETACH PARTITION %s",
		pgx.Identifier{eventsAuditTable}.Sanitize(), pgx.Identifier{orphanName}.Sanitize()))
	require.NoError(t, err)
	require.False(t, isChildOfEventsAudit(t, pool, orphanName), "orphan is a non-child")
	require.True(t, relExists(t, pool, orphanName), "orphan keeps its canonical name")

	// Next cycle reconciles it into the _detached_<unix> form.
	renamed, err := mgr.DetachExpiredPartitions(ctx, now.AddDate(0, -3, 0))
	require.NoError(t, err)
	require.True(t, containsPrefix(renamed, orphanName), "orphan reconciled: %v", renamed)
	assert.False(t, relExists(t, pool, orphanName), "orphan canonical name freed by reconcile rename")

	// Age it and drop it — proving it is not permanently stranded.
	detached := detachedNamesWithPrefix(t, pool, orphanName)
	require.Len(t, detached, 1)
	agedName := fmt.Sprintf("%s%s%d", orphanName, detachedInfix, now.Add(-30*24*time.Hour).Unix())
	_, err = pool.Exec(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s",
		pgx.Identifier{detached[0]}.Sanitize(), pgx.Identifier{agedName}.Sanitize()))
	require.NoError(t, err)

	dropped, err := mgr.DropDetachedPartitions(ctx, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Contains(t, dropped, agedName, "reconciled orphan eventually dropped")
}

// TestMarkerlessNonChildNeverReconciledOrDropped proves the provenance-marker
// gate (round-4 F9): a same-named, same-shape canonical table WITHOUT the marker
// is never renamed by reconcile, and a detached-named table without the marker
// is never dropped.
func TestMarkerlessNonChildNeverReconciledOrDropped(t *testing.T) {
	pool := auditIdemPool(t)
	ctx := context.Background()
	mgr := NewEventsAuditPartitionManager(pool, 90*24*time.Hour)

	// Canonical-named non-child, markerless, far outside any ensure window.
	_, err := pool.Exec(ctx, `CREATE TABLE events_audit_1999_01 (id bytea)`)
	require.NoError(t, err)
	// Detached-named non-child, markerless, with an ancient epoch.
	_, err = pool.Exec(ctx, `CREATE TABLE events_audit_1999_02_detached_100 (id bytea)`)
	require.NoError(t, err)

	// Reconcile pass must not rename the markerless canonical table.
	_, err = mgr.DetachExpiredPartitions(ctx, time.Now().UTC())
	require.NoError(t, err)
	assert.True(t, relExists(t, pool, "events_audit_1999_01"), "markerless canonical non-child untouched by reconcile")

	// Drop pass (huge grace window disabled via 0) must not drop the markerless
	// detached-named table.
	_, err = mgr.DropDetachedPartitions(ctx, 0)
	require.NoError(t, err)
	assert.True(t, relExists(t, pool, "events_audit_1999_02_detached_100"), "markerless detached-named table not dropped")
}

// TestStampTimeGateRefusesSameNamedNonChild proves round-5 H2: EnsurePartitions
// applies the marker ONLY behind a child-ness check. A pre-existing same-named
// NON-CHILD in the ensure window is never stamped (fail closed), and a full
// RunOnce leaves it markerless, un-renamed, and un-dropped.
func TestStampTimeGateRefusesSameNamedNonChild(t *testing.T) {
	pool := auditIdemPool(t)
	ctx := context.Background()
	mgr := NewEventsAuditPartitionManager(pool, 90*24*time.Hour)

	// Occupy a within-window month (2 months back) with a same-named non-child.
	occupiedMonth := monthStart(time.Now().UTC().AddDate(0, -2, 0))
	occupiedName := partitionNameForMonth(occupiedMonth)
	_, err := pool.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (id bytea)", pgx.Identifier{occupiedName}.Sanitize()))
	require.NoError(t, err)

	// EnsurePartitions fails closed on the occupied name (structured oops).
	ensErr := mgr.EnsurePartitions(ctx, 1)
	require.Error(t, ensErr, "EnsurePartitions fails closed on a same-named non-child")

	// Full RunOnce (Ensure → Purge → Detach → Drop) leaves the non-child intact.
	worker := retaudit.NewRetentionWorker(retaudit.DefaultRetentionConfig(), mgr)
	_ = worker.RunOnce(ctx) // errors are expected (Ensure fails closed); end state is the assertion

	assert.Nil(t, objDescription(t, pool, occupiedName), "same-named non-child NEVER stamped with the marker")
	assert.True(t, relExists(t, pool, occupiedName), "same-named non-child not renamed away")
	assert.False(t, isChildOfEventsAudit(t, pool, occupiedName), "same-named non-child never became a child")
	// It was never renamed to a _detached_ form nor dropped.
	assert.Empty(t, detachedNamesWithPrefix(t, pool, occupiedName), "non-child never reconciled to _detached_ form")
}

// TestFinalizeInterruptedConcurrentDetach proves round-3 finding 2: a child left
// pg_inherits.inhdetachpending=true by an interrupted DETACH ... CONCURRENTLY is
// FINALIZEd on the next DetachExpiredPartitions cycle, renamed to the
// _detached_<unix> form, and eventually dropped.
func TestFinalizeInterruptedConcurrentDetach(t *testing.T) {
	pool := auditIdemPool(t)
	ctx := context.Background()
	mgr := NewEventsAuditPartitionManager(pool, 90*24*time.Hour)

	now := time.Now().UTC()
	pendMonth := monthStart(now.AddDate(0, -7, 0))
	require.NoError(t, mgr.ensureMonthPartition(ctx, pendMonth))
	pendName := partitionNameForMonth(pendMonth)

	// Drive the partition into inhdetachpending=true: a concurrent open txn
	// that read events_audit blocks the second internal txn of DETACH
	// CONCURRENTLY; a short statement_timeout interrupts it after the first
	// (inhdetachpending-setting) txn has already committed.
	blocker, err := pool.Acquire(ctx)
	require.NoError(t, err)
	_, err = blocker.Exec(ctx, "BEGIN")
	require.NoError(t, err)
	_, err = blocker.Exec(ctx, "SELECT 1 FROM events_audit LIMIT 0")
	require.NoError(t, err)

	detacher, err := pool.Acquire(ctx)
	require.NoError(t, err)
	_, err = detacher.Exec(ctx, "SET statement_timeout = '750ms'")
	require.NoError(t, err)
	_, detErr := detacher.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DETACH PARTITION %s CONCURRENTLY",
		pgx.Identifier{eventsAuditTable}.Sanitize(), pgx.Identifier{pendName}.Sanitize()))
	require.Error(t, detErr, "concurrent detach must be interrupted by statement_timeout")
	detacher.Release()

	// Release the blocker so subsequent DDL is not itself blocked.
	_, _ = blocker.Exec(ctx, "ROLLBACK")
	blocker.Release()

	// Confirm the pending-detach state was actually created.
	var pending bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COALESCE(bool_or(i.inhdetachpending), false)
		  FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = $1 AND c.relname = $2`, schemaName, pendName).Scan(&pending))
	require.True(t, pending, "partition must be left inhdetachpending=true")

	// The next cycle FINALIZEs and renames it.
	renamed, err := mgr.DetachExpiredPartitions(ctx, now.AddDate(0, -3, 0))
	require.NoError(t, err)
	require.True(t, containsPrefix(renamed, pendName), "pending child finalized+renamed: %v", renamed)
	assert.False(t, isChildOfEventsAudit(t, pool, pendName), "finalized child no longer a child")
	assert.False(t, relExists(t, pool, pendName), "finalized child renamed away from canonical name")
}

// TestBackfillReHomesLegacyRowsAndStraddleDedups proves the one-time Backfill
// re-homes legacy events_audit_unpartitioned rows into the partitioned table
// with no history loss, and that a legacy row + a later DLQ replay of the SAME
// event dedup to exactly one row (backfill and writeAuditRow derive event_ms
// identically via the shared eventMsFromULID helper). Also proves idempotency.
func TestBackfillReHomesLegacyRowsAndStraddleDedups(t *testing.T) {
	pool := auditIdemPool(t)
	ctx := context.Background()
	mgr := NewEventsAuditPartitionManager(pool, 365*24*time.Hour)

	// Legacy row with an OLD event ULID (~60 days ago) in the unpartitioned table.
	oldTime := time.Now().UTC().AddDate(0, 0, -60)
	oldID := ulid.MustNew(ulid.Timestamp(oldTime), ulid.DefaultEntropy())
	_, err := pool.Exec(ctx, `
		INSERT INTO events_audit_unpartitioned
		  (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering)
		VALUES ($1, 'events.main.legacy', 'legacy', $2, 'system', '\x00', 1, 'identity', 1, '{}')`,
		oldID.Bytes(), oldTime.UnixNano())
	require.NoError(t, err)

	require.NoError(t, mgr.Backfill(ctx))

	// The legacy row is now queryable via the partitioned events_audit.
	require.Equal(t, 1, countAllAuditRows(t, pool))
	var gotEventMS int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT event_ms FROM events_audit WHERE id = $1`, oldID.Bytes()).Scan(&gotEventMS))
	require.Equal(t, eventMsFromULID(oldID), gotEventMS)

	// The legacy table was renamed away.
	require.False(t, relExists(t, pool, "events_audit_unpartitioned"))
	require.True(t, relExists(t, pool, "events_audit_legacy_migrated"))

	// Straddle: a later DLQ replay of the SAME event (different store-time) must
	// dedup to exactly one row.
	h := validHeaders(t)
	h.Set(headerMsgID, oldID.String())
	replay := idemStubMsg(t, h, "events.main.legacy", time.Now().UTC(), 99)
	require.NoError(t, writeAuditRow(ctx, pool, "events.main.legacy", replay))
	require.Equal(t, 1, countAllAuditRows(t, pool), "legacy row + replay of same event → exactly one row")

	// Idempotent: a second Backfill is a no-op (legacy table already renamed).
	require.NoError(t, mgr.Backfill(ctx))
	require.Equal(t, 1, countAllAuditRows(t, pool))
}

// TestBackfillAndHealthCheckTargetPublicUnderLeakedSearchPath proves the
// round-6 WR-01 sibling to the EnsurePartitions leaked-search_path test: the
// Backfill INSERT / legacy RENAME and the HealthCheck probe operate on
// public.events_audit unconditionally, even when a non-public session
// search_path is leaked onto the pooled connection. A decoy leaked_ns.events_audit
// (search_path HEAD) is created so a BARE `INSERT INTO events_audit` would land
// the re-homed row in the wrong schema; the public-qualified INSERT lands it in
// public regardless.
func TestBackfillAndHealthCheckTargetPublicUnderLeakedSearchPath(t *testing.T) {
	shared := testutil.SharedPostgres(t)
	cfg, err := pgxpool.ParseConfig(testutil.FreshDatabase(t, shared))
	require.NoError(t, err)
	// A single connection so a non-LOCAL SET leaks onto every later operation.
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	ctx := context.Background()
	mgr := NewEventsAuditPartitionManager(pool, 365*24*time.Hour)

	// Seed a valid legacy row into the (public) unpartitioned table BEFORE the
	// search_path is leaked.
	oldTime := time.Now().UTC().AddDate(0, 0, -60)
	oldID := ulid.MustNew(ulid.Timestamp(oldTime), ulid.DefaultEntropy())
	_, err = pool.Exec(ctx, `
		INSERT INTO public.events_audit_unpartitioned
		  (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering)
		VALUES ($1, 'events.main.legacy', 'legacy', $2, 'system', '\x00', 1, 'identity', 1, '{}')`,
		oldID.Bytes(), oldTime.UnixNano())
	require.NoError(t, err)

	// Decoy events_audit in a non-public schema that would silently swallow a
	// bare INSERT. INCLUDING ALL copies the (id, event_ms) PK so ON CONFLICT
	// resolves against the decoy.
	_, err = pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS leaked_ns")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "CREATE TABLE leaked_ns.events_audit (LIKE public.events_audit INCLUDING ALL)")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "SET search_path TO leaked_ns, public")
	require.NoError(t, err)

	require.NoError(t, mgr.Backfill(ctx),
		"Backfill must not fail under a leaked non-public search_path")

	// The re-homed row must land in public.events_audit, never the decoy.
	var publicCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM public.events_audit WHERE id = $1`, oldID.Bytes()).Scan(&publicCount))
	require.Equal(t, 1, publicCount, "re-homed legacy row must land in public.events_audit")

	var decoyCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM leaked_ns.events_audit`).Scan(&decoyCount))
	require.Equal(t, 0, decoyCount, "no re-homed row leaked into the non-public decoy")

	// The legacy RENAME operated on the public table.
	var unpart, migrated *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT to_regclass('public.events_audit_unpartitioned')::text`).Scan(&unpart))
	require.Nil(t, unpart, "public legacy table renamed away")
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT to_regclass('public.events_audit_legacy_migrated')::text`).Scan(&migrated))
	require.NotNil(t, migrated, "public legacy table renamed to events_audit_legacy_migrated")

	// HealthCheck probes public and succeeds under the leaked search_path.
	require.NoError(t, mgr.HealthCheck(ctx), "HealthCheck must operate on public under a leaked search_path")
}

// TestBackfillNoLegacyTableIsNoop proves Backfill returns nil immediately when
// events_audit_unpartitioned is absent (already re-homed / clean install).
func TestBackfillNoLegacyTableIsNoop(t *testing.T) {
	pool := auditIdemPool(t)
	ctx := context.Background()
	mgr := NewEventsAuditPartitionManager(pool, 90*24*time.Hour)

	// Drain the (empty) legacy table first so it is absent.
	require.NoError(t, mgr.Backfill(ctx))
	require.False(t, relExists(t, pool, "events_audit_unpartitioned"))
	// Second call with the table absent is a clean no-op.
	require.NoError(t, mgr.Backfill(ctx))
}
