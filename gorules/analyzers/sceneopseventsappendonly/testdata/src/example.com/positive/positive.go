// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package positive

import "context"

// txExec mimics pgx's tx.Exec / tx.Query / tx.QueryRow shape (SQL at
// args[1] after ctx) so the analyzer matches by method name.
type tx struct{}

func (tx) Exec(ctx context.Context, sql string, args ...any) (any, error) { return nil, nil }
func (tx) Query(ctx context.Context, sql string, args ...any) (any, error) { return nil, nil }
func (tx) QueryRow(ctx context.Context, sql string, args ...any) any       { return nil }

// dbSQL mimics database/sql's DB.Exec / DB.Query / DB.QueryRow shape
// (SQL at args[0], no ctx). The analyzer must check both arg positions.
type dbSQL struct{}

func (dbSQL) Exec(sql string, args ...any) (any, error)  { return nil, nil }
func (dbSQL) Query(sql string, args ...any) (any, error) { return nil, nil }
func (dbSQL) QueryRow(sql string, args ...any) any       { return nil }

func updates(ctx context.Context) {
	var t tx
	t.Exec(ctx, "UPDATE scene_ops_events SET op = 'noop' WHERE id = 1") // want `scene_ops_events is append-only`
	t.Query(ctx, "DELETE FROM scene_ops_events WHERE id = 1")            // want `scene_ops_events is append-only`
	t.QueryRow(ctx, "TRUNCATE scene_ops_events")                         // want `scene_ops_events is append-only`
	t.QueryRow(ctx, "TRUNCATE TABLE scene_ops_events")                   // want `scene_ops_events is append-only`
	// Concatenation chain: must also fire.
	t.Exec(ctx, "UPDATE "+"scene_ops_events"+" SET op = 'noop'") // want `scene_ops_events is append-only`
	// Named const: must also fire.
	t.Exec(ctx, deleteSQL) // want `scene_ops_events is append-only`
}

// database/sql-style: SQL is args[0]. CodeRabbit finding on PR #3457.
func updatesDBStyle() {
	var d dbSQL
	d.Exec("UPDATE scene_ops_events SET op = 'noop' WHERE id = 1") // want `scene_ops_events is append-only`
	d.Query("DELETE FROM scene_ops_events WHERE id = 1")            // want `scene_ops_events is append-only`
	d.QueryRow("TRUNCATE scene_ops_events")                         // want `scene_ops_events is append-only`
}

const deleteSQL = "DELETE FROM scene_ops_events WHERE id = 1"
