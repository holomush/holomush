// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

// eventMsFromULIDForTest mirrors the production eventMsFromULID helper
// (internal/eventbus/audit/projection.go): the ULID's embedded millisecond
// rendered to UnixNano. Kept local so the store package test does not import
// the audit package.
func eventMsFromULIDForTest(id ulid.ULID) int64 {
	return int64(id.Time()) * int64(time.Millisecond)
}

// TestMigration000052PartitionsEventsAudit is the dedicated home for the
// migration-52 acceptance assertions: index/constraint OWNERSHIP on the new
// partitioned parent, NO DEFAULT partition, unchanged BIGINT timestamp column,
// covering-partition presence for live writes, up→down DATA preservation, and
// idempotent (no-op) re-apply.
func TestMigration000052PartitionsEventsAudit(t *testing.T) {
	ctx := context.Background()
	pool := rawPool(t)

	require.NoError(t, runMigrations(ctx, pool, 52), "migrate up to 000052")

	// (a) events_audit is a partitioned parent (relkind='p') with composite PK.
	var relkind string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT relkind FROM pg_class WHERE oid = 'public.events_audit'::regclass`).Scan(&relkind))
	require.Equal(t, "p", relkind, "events_audit must be a partitioned table")

	// Composite PRIMARY KEY (id, event_ms).
	var pkCols string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT string_agg(a.attname, ',' ORDER BY array_position(con.conkey, a.attnum))
		FROM pg_constraint con
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = ANY(con.conkey)
		WHERE con.conrelid = 'public.events_audit'::regclass AND con.contype = 'p'
	`).Scan(&pkCols))
	require.Equal(t, "id,event_ms", pkCols, "PK must be composite (id, event_ms)")

	// (b) index/constraint OWNERSHIP — each index's indrelid IS the new parent,
	// NOT events_audit_unpartitioned. Name existence alone is insufficient
	// (review finding 2).
	for _, idxName := range []string{
		"events_audit_subject_id",
		"events_audit_subject_ts",
		"events_audit_subject_pat",
		"events_audit_subject_js_seq",
		"events_audit_dek_ref",
		"events_audit_event_ms_brin",
	} {
		var ownedByParent bool
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT i.indrelid = 'public.events_audit'::regclass
			FROM pg_index i JOIN pg_class ic ON ic.oid = i.indexrelid
			WHERE ic.relname = $1
		`, idxName).Scan(&ownedByParent), "index %s must exist", idxName)
		require.True(t, ownedByParent, "index %s must be owned by the new partitioned parent", idxName)
	}

	// The PK constraint named events_audit_pkey is owned by the new parent.
	var pkOwnedByParent bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT conrelid = 'public.events_audit'::regclass
		FROM pg_constraint WHERE conname = 'events_audit_pkey'
	`).Scan(&pkOwnedByParent))
	require.True(t, pkOwnedByParent, "events_audit_pkey must be owned by the new partitioned parent")

	// (c) NO DEFAULT partition.
	var defOID uint32
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT partdefid FROM pg_partitioned_table WHERE partrelid = 'public.events_audit'::regclass`).Scan(&defOID))
	require.EqualValues(t, 0, defOID, "events_audit must have NO DEFAULT partition")

	// (d) the timestamp column type is unchanged BIGINT.
	var tsType string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT data_type FROM information_schema.columns
		WHERE table_name = 'events_audit' AND column_name = 'timestamp'
	`).Scan(&tsType))
	require.Equal(t, "bigint", tsType, "timestamp column must remain BIGINT (store-time unchanged)")

	// (e) a covering partition exists for a now()-timestamp ULID event — live
	// writes work immediately after deploy.
	probeID := ulid.Make()
	probeMs := eventMsFromULIDForTest(probeID)
	_, err := pool.Exec(ctx, `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			envelope, schema_ver, codec, js_seq, rendering, event_ms
		) VALUES ($1, 'events.main.probe', 'test.probe',
			(EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, 'system', NULL,
			'\x00', 1, 'identity', 1, '{}'::jsonb, $2)
	`, probeID[:], probeMs)
	require.NoError(t, err, "live write to a covering partition must succeed")

	// (f) idempotent re-apply: running the up SQL body again is a no-op (the
	// regclass/DO-block guards hold; golang-migrate itself won't re-run an
	// applied version, so exercise the SQL directly).
	upSQL, err := os.ReadFile("migrations/000052_events_audit_partition.up.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(upSQL))
	require.NoError(t, err, "re-applying 000052 up must be a no-op")

	// The probe row survives the re-apply (parent not clobbered).
	var afterReapply int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM events_audit WHERE id = $1`, probeID[:]).Scan(&afterReapply))
	require.Equal(t, 1, afterReapply, "re-apply must not clobber the partitioned parent")

	// (g) up→down DATA PRESERVATION: roll 000052 down; the probe row survives.
	require.NoError(t, runMigrations(ctx, pool, 51), "migrate down past 000052")

	// events_audit is back to a plain relation with single PK (id) and no event_ms.
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT relkind FROM pg_class WHERE oid = 'public.events_audit'::regclass`).Scan(&relkind))
	require.Equal(t, "r", relkind, "events_audit must be un-partitioned after down")

	var eventMsCols int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'events_audit' AND column_name = 'event_ms'
	`).Scan(&eventMsCols))
	require.Equal(t, 0, eventMsCols, "event_ms column must be gone after down")

	var pkColsAfter string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT string_agg(a.attname, ',' ORDER BY array_position(con.conkey, a.attnum))
		FROM pg_constraint con
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = ANY(con.conkey)
		WHERE con.conrelid = 'public.events_audit'::regclass AND con.contype = 'p'
	`).Scan(&pkColsAfter))
	require.Equal(t, "id", pkColsAfter, "PK must be single (id) after down")

	// The restored table owns the original-named secondary indexes.
	for _, idxName := range []string{
		"events_audit_subject_id",
		"events_audit_subject_ts",
		"events_audit_subject_pat",
		"events_audit_subject_js_seq",
		"events_audit_dek_ref",
	} {
		var ownedByRestored bool
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT i.indrelid = 'public.events_audit'::regclass
			FROM pg_index i JOIN pg_class ic ON ic.oid = i.indexrelid
			WHERE ic.relname = $1
		`, idxName).Scan(&ownedByRestored), "restored index %s must exist", idxName)
		require.True(t, ownedByRestored, "restored index %s must be owned by events_audit", idxName)
	}

	// DATA PRESERVATION: the probe row written between up and down is still here.
	var probeCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM events_audit WHERE id = $1`, probeID[:]).Scan(&probeCount))
	require.Equal(t, 1, probeCount, "probe row must survive the up→down rollback (data-preserving)")
}
