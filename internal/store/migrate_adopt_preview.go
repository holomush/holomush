// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/samber/oops"
)

// AdoptPreview describes what the one-shot cutover would do to this database on
// the next upward migration.
//
// It exists because every number the CLI can otherwise report — Version,
// PendingMigrations, and everything derived from them — is read out of goose's
// ledger, and on a not-yet-adopted database that ledger is empty. A preview built
// from it says "44 migrations would be applied" about a database that is already
// fully migrated and will have none applied.
type AdoptPreview struct {
	// Pending reports that legacy golang-migrate bookkeeping is present and goose
	// bookkeeping has not been seeded, so the next upward migration performs the
	// irreversible cutover.
	Pending bool

	// Recorded is the version the legacy ledger records. Meaningful only when
	// Pending; it is the version the cutover would treat as already applied.
	Recorded uint

	// Dirty reports that the legacy ledger is marked dirty, in which case the
	// cutover refuses and aborts boot rather than seeding. Meaningful only when
	// Pending.
	Dirty bool
}

// AdoptPreview reports what adoptFromGolangMigrate would do, WITHOUT doing any of
// it.
//
// This is deliberately a separate, read-only path rather than a flag on the adopt
// gate itself: the gate is the phase's one irreversible write and stays as small
// as it can be. Every statement issued here is a probe — two to_regclass lookups,
// an EXISTS, and the legacy ledger read — so calling it can neither create nor
// modify bookkeeping. It takes no advisory lock, because a preview that has to
// serialize against a booting replica would be a worse diagnostic than a slightly
// stale one.
//
// It reuses readLegacyVersion rather than reading the ledger a second way, so a
// preview reports the same refusals (dirty, ambiguous row count) the deploy will
// hit rather than a rosier view of the same table.
func (m *Migrator) AdoptPreview() (AdoptPreview, error) {
	// Unit tests construct a Migrator around a mock provider with no database
	// handle; there is nothing to probe, and nothing would be adopted either.
	if m.db == nil {
		return AdoptPreview{}, nil
	}

	ctx := context.Background()

	conn, err := m.db.Conn(ctx)
	if err != nil {
		return AdoptPreview{}, oops.Code("MIGRATION_ADOPT_PROBE_FAILED").
			With("operation", "acquire preview connection").Wrap(err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			slog.WarnContext(ctx, "failed to return adopt preview connection to the pool",
				"error", closeErr)
		}
	}()

	return previewAdoptOn(ctx, conn)
}

// previewAdoptOn mirrors adoptLocked's decision tree, minus every write. The two
// must agree on what "already adopted" means, or the preview describes a
// different deploy than the one that runs.
func previewAdoptOn(ctx context.Context, conn *sql.Conn) (AdoptPreview, error) {
	var legacyExists, gooseExists bool
	if err := conn.QueryRowContext(ctx, probeTablesSQL).Scan(&legacyExists, &gooseExists); err != nil {
		return AdoptPreview{}, oops.Code("MIGRATION_ADOPT_PROBE_FAILED").
			With("operation", "probe bookkeeping tables").Wrap(err)
	}

	// No legacy table: a fresh database. Nothing to adopt, so goose's own view is
	// already the truthful one.
	if !legacyExists {
		return AdoptPreview{}, nil
	}

	if gooseExists {
		var seeded bool
		if err := conn.QueryRowContext(ctx, probeGooseSeededSQL).Scan(&seeded); err != nil {
			return AdoptPreview{}, oops.Code("MIGRATION_ADOPT_PROBE_FAILED").
				With("operation", "probe goose bookkeeping").Wrap(err)
		}
		// A row above version 0 can only have been written by a real migration or a
		// previous adopt. The version-0 bootstrap row alone does NOT count — a
		// read-only verb leaves exactly that behind, and treating it as evidence of
		// a completed cutover is the same trap probeGooseSeededSQL's filter exists
		// to avoid.
		if seeded {
			return AdoptPreview{}, nil
		}
	}

	recorded, dirty, err := readLegacyVersion(ctx, conn)
	if err != nil {
		return AdoptPreview{}, err
	}
	return AdoptPreview{Pending: true, Recorded: recorded, Dirty: dirty}, nil
}
