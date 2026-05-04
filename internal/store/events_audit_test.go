// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"database/sql"
	"testing"

	// Register pgx stdlib driver for database/sql.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/test/testutil"
)

func TestEventsAuditTablePresentWithIndexes(t *testing.T) {
	pg := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, pg)
	ctx := context.Background()

	db, err := sql.Open("pgx", connStr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var n int
	require.NoError(t,
		db.QueryRowContext(ctx,
			"SELECT count(*) FROM information_schema.tables WHERE table_name='events_audit'",
		).Scan(&n),
	)
	require.Equal(t, 1, n, "events_audit table not created by migrations")

	rows, err := db.QueryContext(ctx,
		"SELECT indexname FROM pg_indexes WHERE tablename='events_audit' ORDER BY indexname",
	)
	require.NoError(t, err)
	defer rows.Close()
	var indexes []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		indexes = append(indexes, name)
	}
	require.NoError(t, rows.Err())
	require.Contains(t, indexes, "events_audit_subject_id")
	require.Contains(t, indexes, "events_audit_subject_ts")
	require.Contains(t, indexes, "events_audit_subject_pat")
	require.Contains(t, indexes, "events_audit_pkey")
}

func TestEventsAuditInsertOnConflictIsIdempotent(t *testing.T) {
	pg := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, pg)
	ctx := context.Background()

	db, err := sql.Open("pgx", connStr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	id := ulid.Make()
	insert := `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			envelope, schema_ver, codec, js_seq, rendering
		) VALUES ($1, $2, $3, now(), 'system', NULL, $4, 1, 'identity', 100, '{}'::jsonb)
		ON CONFLICT (id) DO NOTHING`
	envelope := []byte(`{"hello":"world"}`)

	res1, err := db.ExecContext(ctx, insert, id[:], "events.main.test", "test.t", envelope)
	require.NoError(t, err)
	n1, _ := res1.RowsAffected()
	require.EqualValues(t, 1, n1, "first insert should affect 1 row")

	res2, err := db.ExecContext(ctx, insert, id[:], "events.main.test", "test.t", envelope)
	require.NoError(t, err)
	n2, _ := res2.RowsAffected()
	require.EqualValues(t, 0, n2, "duplicate insert should affect 0 rows")

	var count int
	require.NoError(t,
		db.QueryRowContext(ctx, "SELECT count(*) FROM events_audit WHERE id=$1", id[:]).Scan(&count),
	)
	require.Equal(t, 1, count)
}

func TestEventsAuditCodecColumnIsNotNull(t *testing.T) {
	pg := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, pg)
	ctx := context.Background()

	db, err := sql.Open("pgx", connStr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	id := ulid.Make()
	insert := `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			envelope, schema_ver, codec, js_seq, rendering
		) VALUES ($1, 'events.main.test', 'test.t', now(), 'system', NULL, $2, 1, NULL, 100, '{}'::jsonb)`
	_, err = db.ExecContext(ctx, insert, id[:], []byte(`{}`))
	require.Error(t, err, "NULL codec should be rejected by NOT NULL constraint")
}
