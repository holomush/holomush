// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
)

// runBootstrapOrphanCheck refuses to start if any plugin-kind event in
// events_audit lacks an actor_id (a legacy event that survived a w9ml
// migration mis-step). Defense-in-depth: migration 000018 makes orphans
// impossible from a clean install. This guards against manual restore
// from old backup or partial migration recovery.
func runBootstrapOrphanCheck(ctx context.Context, pool *pgxpool.Pool) error {
	var count int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM events_audit
		 WHERE actor_kind = $1 AND actor_id IS NULL
	`, eventbus.ActorKindPlugin.String()).Scan(&count)
	if err != nil {
		return oops.Code("BOOTSTRAP_ORPHAN_CHECK_FAILED").Wrap(err)
	}
	if count > 0 {
		return oops.Code("PLUGIN_ACTOR_ORPHAN_DETECTED").
			With("count", count).
			Errorf("legacy plugin-actor events present after w9ml migration")
	}
	return nil
}
