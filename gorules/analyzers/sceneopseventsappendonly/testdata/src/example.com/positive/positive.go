// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package positive

import "context"

// txExec mimics pgx's tx.Exec / tx.Query / tx.QueryRow shape so the
// analyzer matches by method name.
type tx struct{}

func (tx) Exec(ctx context.Context, sql string, args ...any) (any, error) { return nil, nil }
func (tx) Query(ctx context.Context, sql string, args ...any) (any, error) { return nil, nil }
func (tx) QueryRow(ctx context.Context, sql string, args ...any) any       { return nil }

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

const deleteSQL = "DELETE FROM scene_ops_events WHERE id = 1"
