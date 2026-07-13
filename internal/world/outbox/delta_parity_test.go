// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package outbox_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/holomush/holomush/test/testutil"
)

// This file is the INV-WORLD-2 (DELTA-PARITY) binding target (the // Verifies:
// annotation is flipped on in 05-12 Task 2). It commits a REAL world mutation,
// takes the MutationDelta the guarded repo returns, feeds it through the
// production WriteIntent envelope writer, and proves the emitted envelope's
// affected-aggregates manifest MATCHES that delta — every id, tombstone flag, and
// before/after version equals the row's actual version transition — for the two
// non-trivial cases the plan calls out: a location DELETE that DB-cascades its
// exits, and a bidirectional exit Create whose reverse aggregate must appear. It
// does NOT merely assert a manifest is present (presence is insufficient).

// testPool is the shared database pool for the outbox integration tests.
var testPool *pgxpool.Pool

// TestMain stands up a PostgreSQL testcontainer (Docker) for the outbox
// integration tests. The outbox package's other tests (relay/genesis/taxonomy)
// are in-memory unit tests and do not touch this pool.
func TestMain(m *testing.M) {
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	if err != nil {
		panic("failed to start postgres container: " + err.Error())
	}

	migrator, err := store.NewMigrator(pgEnv.ConnStr)
	if err != nil {
		_ = pgEnv.Terminate(ctx)
		panic("failed to create migrator: " + err.Error())
	}
	if err := migrator.Up(); err != nil {
		_ = migrator.Close()
		_ = pgEnv.Terminate(ctx)
		panic("failed to run migrations: " + err.Error())
	}
	_ = migrator.Close()

	pool, err := pgxpool.New(ctx, pgEnv.ConnStr)
	if err != nil {
		_ = pgEnv.Terminate(ctx)
		panic("failed to create pool: " + err.Error())
	}
	testPool = pool

	code := m.Run()

	pool.Close()
	_ = pgEnv.Terminate(ctx)
	os.Exit(code)
}

// insertDeltaTestLocation inserts a location row and returns its id, registering
// cleanup of it and any exits that reference it.
func insertDeltaTestLocation(ctx context.Context, t *testing.T, name string) ulid.ULID {
	t.Helper()
	id := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, $2, 'delta-parity test', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, id.String(), name)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM exits WHERE from_location_id = $1 OR to_location_id = $1`, id.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, id.String())
	})
	return id
}

// locationDBVersion reads a location's raw version column.
func locationDBVersion(ctx context.Context, t *testing.T, id ulid.ULID) int {
	t.Helper()
	var v int
	require.NoError(t, testPool.QueryRow(ctx, `SELECT version FROM locations WHERE id = $1`, id.String()).Scan(&v))
	return v
}

// exitDBVersion reads an exit's raw version column.
func exitDBVersion(ctx context.Context, t *testing.T, id ulid.ULID) int {
	t.Helper()
	var v int
	require.NoError(t, testPool.QueryRow(ctx, `SELECT version FROM exits WHERE id = $1`, id.String()).Scan(&v))
	return v
}

// newDeltaIntent builds an EnvelopeIntent for gameID with a minimal payload.
func newDeltaIntent(gameID, kind string, aggType wmodel.AggregateType, aggID ulid.ULID) wmodel.EnvelopeIntent {
	return wmodel.NewEnvelopeIntent(wmodel.IntentParams{
		GameID:        gameID,
		Kind:          kind,
		SchemaVersion: 1,
		Actor:         "system",
		AggregateType: aggType,
		AggregateID:   aggID,
		Payload:       []byte(`{"id":"` + aggID.String() + `"}`),
	})
}

// TestDeltaParityLocationDeleteCascade proves a location DELETE that DB-cascades
// its exits emits a manifest that EQUALS the returned MutationDelta AND reflects
// the rows' actual version transition: the primary location tombstone and every
// cascaded exit tombstone appear in the envelope's affected manifest with the
// exact before-versions the rows held.
//
// Verifies: INV-WORLD-2
func TestDeltaParityLocationDeleteCascade(t *testing.T) {
	ctx := context.Background()
	locRepo := postgres.NewLocationRepository(testPool)
	exitRepo := postgres.NewExitRepository(testPool)
	store := postgres.NewOutboxStore(testPool)
	tr := postgres.NewTransactor(testPool)

	from := insertDeltaTestLocation(ctx, t, "Delta From")
	to := insertDeltaTestLocation(ctx, t, "Delta To")

	// A real exit referencing the location we will delete — the FK cascade removes
	// it, so the DELETE's delta must list it as a tombstone (delta-parity).
	exitID := ulid.Make()
	_, err := exitRepo.Create(ctx, &world.Exit{
		ID:             exitID,
		FromLocationID: from,
		ToLocationID:   to,
		Name:           "delta-north",
		Visibility:     world.VisibilityAll,
	})
	require.NoError(t, err)

	// The rows' ACTUAL versions before the mutation.
	fromVersion := locationDBVersion(ctx, t, from)
	exitVersion := exitDBVersion(ctx, t, exitID)

	intent := newDeltaIntent(ulid.Make().String(), "location_deleted", wmodel.AggregateLocation, from)

	var delta *wmodel.MutationDelta
	var env *wmodel.Envelope
	require.NoError(t, tr.InTransaction(ctx, func(txCtx context.Context) error {
		var derr error
		delta, derr = locRepo.Delete(txCtx, from, 0)
		if derr != nil {
			return derr
		}
		env, derr = store.WriteIntent(txCtx, intent, delta)
		return derr
	}))

	// The returned delta reflects the real rows: the location tombstone carries the
	// row's actual version, and the cascaded exit is listed with its real version.
	require.NotNil(t, delta)
	assert.True(t, delta.Primary.Tombstone, "location delete is a tombstone")
	assert.Equal(t, fromVersion, delta.Primary.BeforeVersion, "primary before-version = the row's actual version")
	assert.Equal(t, 0, delta.Primary.AfterVersion, "a tombstone has no after-version")
	require.Len(t, delta.Affected, 1, "the cascaded exit is in the delta")
	assert.Equal(t, exitID, delta.Affected[0].ID, "the cascaded exit id")
	assert.True(t, delta.Affected[0].Tombstone)
	assert.Equal(t, exitVersion, delta.Affected[0].BeforeVersion, "cascaded exit before-version = its actual row version")

	// PARITY: the emitted manifest is a lossless, field-exact projection of the
	// delta — primary first, then every cascade — not merely a non-empty manifest.
	require.NotNil(t, env)
	require.Len(t, env.Affected, 1+len(delta.Affected), "manifest = primary + cascades")
	assert.Equal(t, delta.Primary, env.Affected[0], "manifest[0] equals the delta primary (id/tombstone/versions)")
	assert.Equal(t, delta.Affected, env.Affected[1:], "manifest tail equals the delta cascades")
}

// TestDeltaParityBidirectionalExitCreate proves a bidirectional exit Create emits
// a manifest that lists BOTH the primary exit and the repository-generated reverse
// exit, each with the before/after versions equal to the real rows' transition
// (0 -> 1), matching the returned MutationDelta exactly.
//
// Verifies: INV-WORLD-2
func TestDeltaParityBidirectionalExitCreate(t *testing.T) {
	ctx := context.Background()
	exitRepo := postgres.NewExitRepository(testPool)
	store := postgres.NewOutboxStore(testPool)
	tr := postgres.NewTransactor(testPool)

	from := insertDeltaTestLocation(ctx, t, "Bi From")
	to := insertDeltaTestLocation(ctx, t, "Bi To")

	exitID := ulid.Make()
	exit := &world.Exit{
		ID:             exitID,
		FromLocationID: from,
		ToLocationID:   to,
		Name:           "bi-north",
		Bidirectional:  true,
		ReturnName:     "bi-south",
		Visibility:     world.VisibilityAll,
	}

	intent := newDeltaIntent(ulid.Make().String(), "exit_created", wmodel.AggregateExit, exitID)

	var delta *wmodel.MutationDelta
	var env *wmodel.Envelope
	require.NoError(t, tr.InTransaction(ctx, func(txCtx context.Context) error {
		var derr error
		delta, derr = exitRepo.Create(txCtx, exit)
		if derr != nil {
			return derr
		}
		env, derr = store.WriteIntent(txCtx, intent, delta)
		return derr
	}))

	// The delta reflects the real rows: the primary exit and its reverse both went
	// 0 -> 1 (fresh creates at the DB default version).
	require.NotNil(t, delta)
	assert.Equal(t, exitID, delta.Primary.ID)
	assert.Equal(t, 0, delta.Primary.BeforeVersion)
	assert.Equal(t, 1, delta.Primary.AfterVersion)
	require.Len(t, delta.Affected, 1, "bidirectional create carries the reverse exit")
	reverseID := delta.Affected[0].ID
	assert.Equal(t, 1, delta.Affected[0].AfterVersion)
	assert.False(t, delta.Affected[0].Tombstone)

	// The reverse exit's after-version equals its real committed row version.
	assert.Equal(t, delta.Primary.AfterVersion, exitDBVersion(ctx, t, exitID))
	assert.Equal(t, delta.Affected[0].AfterVersion, exitDBVersion(ctx, t, reverseID))

	// PARITY: the manifest carries both aggregates exactly as the delta reported.
	require.NotNil(t, env)
	require.Len(t, env.Affected, 2, "manifest lists the primary and the reverse exit")
	assert.Equal(t, delta.Primary, env.Affected[0])
	assert.Equal(t, delta.Affected[0], env.Affected[1])
}
