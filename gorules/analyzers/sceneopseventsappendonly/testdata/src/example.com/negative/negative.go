// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package negative

import "context"

type tx struct{}

func (tx) Exec(ctx context.Context, sql string, args ...any) (any, error) { return nil, nil }

func inserts(ctx context.Context) {
	var t tx
	// INSERT is allowed (the table is append-only via INSERT).
	t.Exec(ctx, "INSERT INTO scene_ops_events (id, op) VALUES ($1, $2)", 1, "create")
	// UPDATE on a different table — not flagged.
	t.Exec(ctx, "UPDATE other_table SET x = 1")
	// String-construction shape we don't track (e.g., fmt.Sprintf): not flagged.
	t.Exec(ctx, sprintf("UPDATE %s SET x = 1", "scene_ops_events"))
}

func sprintf(format string, args ...any) string { return format }
