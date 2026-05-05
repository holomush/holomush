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
//
// Uses an EXISTS probe (returns on the first matching row) instead of a
// full-table COUNT(*) — on large audit tables the aggregate scan can
// noticeably delay startup. We only count on the failure path, where
// startup is already aborting.
func runBootstrapOrphanCheck(ctx context.Context, pool *pgxpool.Pool) error {
	pluginKind := eventbus.ActorKindPlugin.String()
	var hasOrphan bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM events_audit
		   WHERE actor_kind = $1 AND actor_id IS NULL
		)
	`, pluginKind).Scan(&hasOrphan)
	if err != nil {
		return oops.Code("BOOTSTRAP_ORPHAN_CHECK_FAILED").Wrap(err)
	}
	if !hasOrphan {
		return nil
	}
	// Only count on the failure path so the operator gets the magnitude.
	var count int
	if cerr := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM events_audit
		 WHERE actor_kind = $1 AND actor_id IS NULL
	`, pluginKind).Scan(&count); cerr != nil {
		// Fall through with count=0 if the count fails — the EXISTS probe
		// already proved there's at least one orphan.
		count = 0
	}
	return oops.Code("PLUGIN_ACTOR_ORPHAN_DETECTED").
		With("count", count).
		Errorf("legacy plugin-actor events present after w9ml migration")
}
