// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
)

// runBootstrapOrphanCheck refuses to start if any plugin-kind event lacks an
// actor_id (a legacy event that survived a w9ml migration mis-step).
// Defense-in-depth: migration 000018 makes orphans impossible from a clean
// install. This guards against manual restore from old backup or partial
// migration recovery.
//
// After 06-01's migration 000052 renamed the pre-partition history off
// events_audit into events_audit_unpartitioned, a restore-from-old-backup
// lands residual orphans in the LEGACY table — invisible to a probe that only
// scans events_audit. Since this check runs BEFORE 06-02's Backfill re-homes
// those rows (core.go), it must scan BOTH tables. events_audit_unpartitioned is
// referenced only when present (to_regclass guard) so a clean install — where
// the legacy table is absent — does not error with "relation does not exist".
//
// Uses an EXISTS probe (returns on the first matching row) instead of a
// full-table COUNT(*) — on large audit tables the aggregate scan can
// noticeably delay startup. We only count on the failure path, where startup
// is already aborting.
func runBootstrapOrphanCheck(ctx context.Context, pool *pgxpool.Pool) error {
	pluginKind := eventbus.ActorKindPlugin.String()

	// Is the legacy unpartitioned table present? (Absent on a clean install and
	// after Backfill re-homes it.)
	var legacyPresent *string
	if err := pool.QueryRow(ctx,
		`SELECT to_regclass('public.events_audit_unpartitioned')::text`).Scan(&legacyPresent); err != nil {
		return oops.Code("BOOTSTRAP_ORPHAN_CHECK_FAILED").Wrap(err)
	}

	existsQuery := `SELECT EXISTS (
		  SELECT 1 FROM events_audit WHERE actor_kind = $1 AND actor_id IS NULL
		)`
	countQuery := `SELECT COUNT(*) FROM events_audit WHERE actor_kind = $1 AND actor_id IS NULL`
	if legacyPresent != nil {
		slog.DebugContext(ctx, "bootstrap orphan check also scanning events_audit_unpartitioned")
		existsQuery = `SELECT
		  EXISTS (SELECT 1 FROM events_audit WHERE actor_kind = $1 AND actor_id IS NULL)
		  OR EXISTS (SELECT 1 FROM events_audit_unpartitioned WHERE actor_kind = $1 AND actor_id IS NULL)`
		countQuery = `SELECT
		  (SELECT COUNT(*) FROM events_audit WHERE actor_kind = $1 AND actor_id IS NULL)
		  + (SELECT COUNT(*) FROM events_audit_unpartitioned WHERE actor_kind = $1 AND actor_id IS NULL)`
	}

	var hasOrphan bool
	if err := pool.QueryRow(ctx, existsQuery, pluginKind).Scan(&hasOrphan); err != nil {
		return oops.Code("BOOTSTRAP_ORPHAN_CHECK_FAILED").Wrap(err)
	}
	if !hasOrphan {
		return nil
	}
	// Only count on the failure path so the operator gets the magnitude.
	var count int
	if cerr := pool.QueryRow(ctx, countQuery, pluginKind).Scan(&count); cerr != nil {
		// Fall through with count=0 if the count fails — the EXISTS probe
		// already proved there's at least one orphan.
		count = 0
	}
	return oops.Code("PLUGIN_ACTOR_ORPHAN_DETECTED").
		With("count", count).
		Errorf("legacy plugin-actor events present after w9ml migration")
}
