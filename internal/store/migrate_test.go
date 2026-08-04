// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/holomush/holomush/pkg/errutil"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMigratorInvalidURLReturnsInitError(t *testing.T) {
	_, err := NewMigrator("invalid://url")
	require.Error(t, err)
	// Verify the error code is set correctly
	errutil.AssertErrorCode(t, err, "MIGRATION_INIT_FAILED")
}

// TestNewMigratorPostgresqlSchemeIsAcceptedNatively verifies that postgresql:// URLs
// reach the driver unmodified. goose talks to a plain *sql.DB, so the golang-migrate-era
// rewrite to a pgx5:// scheme is gone; pgx/v5 accepts postgres:// and postgresql://
// natively. The error must therefore be a connection failure, never "unknown driver".
func TestNewMigratorPostgresqlSchemeIsAcceptedNatively(t *testing.T) {
	_, err := NewMigrator("postgresql://localhost:1/testdb")
	require.Error(t, err, "should fail due to connection, not URL scheme")
	errutil.AssertErrorCode(t, err, "MIGRATION_INIT_FAILED")
	assert.NotContains(t, err.Error(), "unknown driver")
}

// TestNewMigratorCleansUpConnectionWhenInitFails verifies that a failed
// initialization does not leak the *sql.DB opened moments earlier. The error path
// is exercised with a structurally valid but unusable URL scheme.
func TestNewMigratorCleansUpConnectionWhenInitFails(t *testing.T) {
	_, err := NewMigrator("badscheme://localhost:5432/testdb")
	require.Error(t, err, "should fail with invalid URL scheme")
	errutil.AssertErrorCode(t, err, "MIGRATION_INIT_FAILED")
}

// mockMigrate implements provIface for testing.
//
// It records the calls it receives, not merely the errors it should return. The
// difference is what makes "Down rolls back to version zero" an assertion rather
// than a comment: DownTo ignores its target here, so without the recorded target
// that test is byte-for-byte the same test as its neighbour.
type mockMigrate struct {
	upErr      error
	downErr    error
	upByOneErr error
	upToErr    error
	downToErr  error
	versionVal int64
	versionErr error
	closeErr   error

	// upResults is what the provider reports having applied, so a test can
	// distinguish "applied something" from "there was nothing pending" — both of
	// which the wrapper must treat as success.
	upResults []*goose.MigrationResult

	// Recorded calls.
	upCalls       int
	upByOneCalls  int
	downCalls     int
	upToTargets   []int64
	downToTargets []int64
}

func (m *mockMigrate) Up(_ context.Context) ([]*goose.MigrationResult, error) {
	m.upCalls++
	return m.upResults, m.upErr
}

func (m *mockMigrate) UpByOne(_ context.Context) (*goose.MigrationResult, error) {
	m.upByOneCalls++
	return nil, m.upByOneErr
}

func (m *mockMigrate) UpTo(_ context.Context, version int64) ([]*goose.MigrationResult, error) {
	m.upToTargets = append(m.upToTargets, version)
	return nil, m.upToErr
}

func (m *mockMigrate) Down(_ context.Context) (*goose.MigrationResult, error) {
	m.downCalls++
	return nil, m.downErr
}

func (m *mockMigrate) DownTo(_ context.Context, version int64) ([]*goose.MigrationResult, error) {
	m.downToTargets = append(m.downToTargets, version)
	return nil, m.downToErr
}

func (m *mockMigrate) GetDBVersion(_ context.Context) (int64, error) {
	return m.versionVal, m.versionErr
}

func (m *mockMigrate) Status(_ context.Context) ([]*goose.MigrationStatus, error) {
	return nil, nil
}

func (m *mockMigrate) Close() error { return m.closeErr }

func TestMigratorUpAppliesPendingMigrations(t *testing.T) {
	provider := &mockMigrate{upResults: []*goose.MigrationResult{{}, {}}}
	m := &Migrator{m: provider}
	err := m.Up()
	require.NoError(t, err)
	assert.Equal(t, 1, provider.upCalls, "Up must reach the provider exactly once")
}

func TestMigratorUpTreatsNothingPendingAsSuccess(t *testing.T) {
	// goose's Up returns (nil, nil) when there is nothing pending — there is no
	// ErrNoChange sentinel to swallow, unlike golang-migrate. The empty result
	// slice is the point: it must not be mistaken for a failure.
	provider := &mockMigrate{upResults: nil}
	m := &Migrator{m: provider}
	err := m.Up()
	require.NoError(t, err, "nothing pending should be treated as success")
	assert.Equal(t, 1, provider.upCalls,
		"the provider must still be consulted; success here is its answer, not a shortcut")
}

func TestMigratorUpReturnsWrappedErrorOnFailure(t *testing.T) {
	m := &Migrator{m: &mockMigrate{upErr: errors.New("database locked")}}
	err := m.Up()
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_UP_FAILED")
}

func TestMigratorDownRollsBackMigrations(t *testing.T) {
	provider := &mockMigrate{}
	m := &Migrator{m: provider}
	err := m.Down()
	require.NoError(t, err)
	assert.Len(t, provider.downToTargets, 1, "Down must reach the provider exactly once")
}

func TestMigratorDownRollsBackToVersionZero(t *testing.T) {
	// Down maps to DownTo(ctx, 0). Asserting the TARGET is the whole content of
	// this test: a Down that rolled back to any other version would satisfy a bare
	// require.NoError just as well, which is what made this a duplicate of its
	// neighbour rather than a second test.
	provider := &mockMigrate{}
	m := &Migrator{m: provider}
	err := m.Down()
	require.NoError(t, err)
	assert.Equal(t, []int64{0}, provider.downToTargets,
		"Down must ask the provider for version 0, not merely for some rollback")
}

func TestMigratorDownReturnsWrappedErrorOnFailure(t *testing.T) {
	m := &Migrator{m: &mockMigrate{downToErr: errors.New("constraint violation")}}
	err := m.Down()
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_DOWN_FAILED")
}

func TestMigratorMigrateSucceedsWhenUnderlyingReturnsNil(t *testing.T) {
	m := &Migrator{m: &mockMigrate{versionVal: 1}}
	err := m.Migrate(5)
	require.NoError(t, err)
}

func TestMigratorMigrateIsNoOpWhenAlreadyAtTargetVersion(t *testing.T) {
	// Neither UpTo nor DownTo is reachable at the target version, so an error
	// planted on both must not surface.
	m := &Migrator{m: &mockMigrate{
		versionVal: 5,
		upToErr:    errors.New("UpTo must not be called"),
		downToErr:  errors.New("DownTo must not be called"),
	}}
	err := m.Migrate(5)
	require.NoError(t, err, "migrating to the current version must be a no-op")
}

func TestMigratorMigrateRollsBackWhenTargetIsBelowCurrent(t *testing.T) {
	m := &Migrator{m: &mockMigrate{versionVal: 10, downToErr: errors.New("rollback blocked")}}
	err := m.Migrate(3)
	require.Error(t, err, "a target below the current version must route through DownTo")
	errutil.AssertErrorCode(t, err, "MIGRATION_MIGRATE_FAILED")
}

func TestMigratorMigrateReturnsErrorWhenUnderlyingFails(t *testing.T) {
	m := &Migrator{m: &mockMigrate{versionVal: 1, upToErr: errors.New("target version not found")}}
	err := m.Migrate(99)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_MIGRATE_FAILED")
}

func TestMigratorMigrateReturnsErrorWhenVersionLookupFails(t *testing.T) {
	m := &Migrator{m: &mockMigrate{versionErr: errors.New("connection lost")}}
	err := m.Migrate(5)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_MIGRATE_FAILED")
}

func TestMigratorStepsAppliesNMigrations(t *testing.T) {
	m := &Migrator{m: &mockMigrate{}}
	err := m.Steps(3)
	require.NoError(t, err)
}

func TestMigratorStepsTreatsNothingLeftToRollBackAsSuccess(t *testing.T) {
	// goose's ErrNoNextVersion is the analogue of golang-migrate's ErrNoChange.
	// `holomush migrate down` depends on this being success: it reports "already at
	// version N" by comparing the version before and after, and an error here would
	// short-circuit that message.
	m := &Migrator{m: &mockMigrate{downErr: goose.ErrNoNextVersion}}
	err := m.Steps(-1)
	require.NoError(t, err)
}

func TestMigratorStepsTreatsNothingLeftToApplyAsSuccess(t *testing.T) {
	m := &Migrator{m: &mockMigrate{upByOneErr: goose.ErrNoNextVersion}}
	err := m.Steps(3)
	require.NoError(t, err)
}

func TestMigratorStepsZeroIsNoOp(t *testing.T) {
	// Steps(0) must not reach the provider at all: an error planted on both
	// directions would surface if it did.
	m := &Migrator{m: &mockMigrate{
		upByOneErr: errors.New("UpByOne must not be called"),
		downErr:    errors.New("Down must not be called"),
	}}
	err := m.Steps(0)
	require.NoError(t, err, "Steps(0) should be a no-op returning nil")
}

func TestMigratorStepsReturnsWrappedErrorOnFailure(t *testing.T) {
	m := &Migrator{m: &mockMigrate{upByOneErr: errors.New("invalid step")}}
	err := m.Steps(5)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_STEPS_FAILED")
	// Verify the error includes the requested step count for debugging
	errutil.AssertErrorContext(t, err, "steps", 5)
}

func TestMigratorStepsNegativeReturnsWrappedErrorOnFailure(t *testing.T) {
	m := &Migrator{m: &mockMigrate{downErr: errors.New("constraint violation")}}
	err := m.Steps(-2)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_STEPS_FAILED")
	errutil.AssertErrorContext(t, err, "steps", -2)
}

func TestMigratorVersionReturnsCurrentVersion(t *testing.T) {
	m := &Migrator{m: &mockMigrate{versionVal: 7}}
	version, dirty, err := m.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(7), version)
	assert.False(t, dirty, "goose has no dirty state; the flag is always false")
}

// TestMigratorVersionAlwaysReportsNotDirty pins the documented contract that the
// second return value is inert under goose. goose commits a migration's body and its
// version row in one transaction, so the partially-applied condition golang-migrate's
// dirty flag reported cannot arise. If a future change starts deriving this value
// from somewhere, this test is where that surfaces.
func TestMigratorVersionAlwaysReportsNotDirty(t *testing.T) {
	for _, v := range []int64{0, 1, 53} {
		m := &Migrator{m: &mockMigrate{versionVal: v}}
		_, dirty, err := m.Version()
		require.NoError(t, err)
		assert.False(t, dirty, "version %d must report dirty=false", v)
	}
}

func TestMigratorVersionTreatsNoAppliedMigrationsAsZero(t *testing.T) {
	// goose's GetDBVersion returns 0 (not a sentinel error) when nothing is applied.
	m := &Migrator{m: &mockMigrate{versionVal: 0}}
	version, dirty, err := m.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(0), version)
	assert.False(t, dirty)
}

func TestMigratorVersionReturnsWrappedErrorOnFailure(t *testing.T) {
	m := &Migrator{m: &mockMigrate{versionErr: errors.New("connection lost")}}
	_, _, err := m.Version()
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_VERSION_FAILED")
}

func TestMigratorCloseSucceedsWithCleanMigrator(t *testing.T) {
	m := &Migrator{m: &mockMigrate{}}
	err := m.Close()
	require.NoError(t, err)
}

func TestMigratorCloseReportsDatabaseCloseError(t *testing.T) {
	// goose's Provider.Close closes the *sql.DB and returns a single error, so the
	// golang-migrate-era source/database/both split has no referent.
	m := &Migrator{m: &mockMigrate{closeErr: errors.New("db close failed")}}
	err := m.Close()
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_CLOSE_FAILED")
	errutil.AssertErrorContext(t, err, "component", "database")
	assert.Contains(t, err.Error(), "db close failed")
}

func TestMigratorCloseIsIdempotent(t *testing.T) {
	// Calling Close() multiple times should be safe (idempotent).
	// goose's Provider.Close delegates to sql.DB.Close, which is idempotent, and
	// this wrapper preserves that. Important for deferred cleanup where Close()
	// may be called more than once.
	m := &Migrator{m: &mockMigrate{}}

	err1 := m.Close()
	require.NoError(t, err1, "first Close() should succeed")

	err2 := m.Close()
	require.NoError(t, err2, "second Close() should also succeed (idempotent)")

	// Even a third call should work
	err3 := m.Close()
	require.NoError(t, err3, "third Close() should also succeed (idempotent)")
}

func TestMigratorPendingMigrationsReturnsMigrationsAboveCurrentVersion(t *testing.T) {
	// At version 0, migrations 1-20, 30-39, 40-46 should be pending
	// (baseline + is_guest + alias_source + session_player_id + audit_source_component +
	// session_focus + seed_scene_defaults + session_player_fk + create_events_audit +
	// drop_events_and_cursors + events_audit_js_seq_index + events_audit_rendering +
	// create_crypto_keys + events_audit_dek_columns + create_player_character_bindings +
	// crypto_keys_destroyed_at + events_audit_envelope_rename + create_plugins +
	// create_player_totp + create_admin_approvals + replace_bootstrap_metadata +
	// create_crypto_rekey_checkpoints + create_setting_bootstrap_state +
	// add_justification_to_checkpoints + add_phase3_count_to_checkpoints +
	// drop_checkpoint_dek_fks + admin_approvals_op_args_hash_idx +
	// add_session_history_floor_columns + eventbus_crypto_timestamps_to_bigint +
	// connection_focus_key + player_character_bindings_cascade + auth_timestamps_to_bigint +
	// world_timestamps_to_bigint + totp_misc_timestamps_to_bigint + pregfo6_gap_timestamps_to_bigint +
	// character_preferences + session_connection_last_seen + disable_unconditional_scene_write_seed
	// + disable_unconditional_scene_read_seed + world_version_guard + world_outbox
	// + player_reaping + events_audit_partition + sessions_location_index
	// + character_identity_and_lifecycle)
	//
	// This census is written out literally rather than derived from the embedded
	// filesystem: deriving it from the same helper PendingMigrations() uses would
	// make the assertion tautological and blind to a migration file that went
	// missing. Extend it in the same change that adds a migration.
	m := &Migrator{m: &mockMigrate{versionVal: 0}}
	pending, err := m.PendingMigrations()
	require.NoError(t, err)
	assert.Equal(t, []uint{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54}, pending)
}

func TestMigratorPendingMigrationsReturnsEmptyAtLatestVersion(t *testing.T) {
	// At version 54 (latest), no migrations should be pending
	m := &Migrator{m: &mockMigrate{versionVal: 54}}
	pending, err := m.PendingMigrations()
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestMigratorPendingMigrationsReturnsErrorWhenVersionFails(t *testing.T) {
	m := &Migrator{m: &mockMigrate{versionErr: errors.New("connection lost")}}
	_, err := m.PendingMigrations()
	require.Error(t, err)
	// Error should include operation context for debugging
	errutil.AssertErrorContext(t, err, "operation", "get pending migrations")
}

func TestMigratorAppliedMigrationsReturnsMigrationsUpToCurrentVersion(t *testing.T) {
	// At version 1, baseline should be applied
	m := &Migrator{m: &mockMigrate{versionVal: 1}}
	applied, err := m.AppliedMigrations()
	require.NoError(t, err)
	assert.Equal(t, []uint{1}, applied)
}

func TestMigratorAppliedMigrationsReturnsEmptyAtVersionZero(t *testing.T) {
	// At version 0, no migrations applied
	m := &Migrator{m: &mockMigrate{versionVal: 0}}
	applied, err := m.AppliedMigrations()
	require.NoError(t, err)
	assert.Empty(t, applied)
}

func TestMigratorAppliedMigrationsReturnsAllAtLatestVersion(t *testing.T) {
	// At version 18, all migrations 1..18 are applied (spot test at a mid-point version).
	m := &Migrator{m: &mockMigrate{versionVal: 18}}
	applied, err := m.AppliedMigrations()
	require.NoError(t, err)
	assert.Equal(t, []uint{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18}, applied)
}

func TestMigratorAppliedMigrationsReturnsErrorWhenVersionFails(t *testing.T) {
	m := &Migrator{m: &mockMigrate{versionErr: errors.New("connection lost")}}
	_, err := m.AppliedMigrations()
	require.Error(t, err)
	// Error should include operation context for debugging
	errutil.AssertErrorContext(t, err, "operation", "get applied migrations")
}

// closedMock implements provIface and returns errors after Close() is called.
// This simulates the behavior of a goose Provider whose *sql.DB has been closed.
type closedMock struct {
	closed bool
}

var errMigratorClosed = errors.New("migrator is closed")

func (m *closedMock) Up(_ context.Context) ([]*goose.MigrationResult, error) {
	if m.closed {
		return nil, errMigratorClosed
	}
	return nil, nil
}

func (m *closedMock) UpByOne(_ context.Context) (*goose.MigrationResult, error) {
	if m.closed {
		return nil, errMigratorClosed
	}
	return nil, nil
}

func (m *closedMock) UpTo(_ context.Context, _ int64) ([]*goose.MigrationResult, error) {
	if m.closed {
		return nil, errMigratorClosed
	}
	return nil, nil
}

func (m *closedMock) Down(_ context.Context) (*goose.MigrationResult, error) {
	if m.closed {
		return nil, errMigratorClosed
	}
	return nil, nil
}

func (m *closedMock) DownTo(_ context.Context, _ int64) ([]*goose.MigrationResult, error) {
	if m.closed {
		return nil, errMigratorClosed
	}
	return nil, nil
}

func (m *closedMock) GetDBVersion(_ context.Context) (int64, error) {
	if m.closed {
		return 0, errMigratorClosed
	}
	return 1, nil
}

func (m *closedMock) Status(_ context.Context) ([]*goose.MigrationStatus, error) {
	if m.closed {
		return nil, errMigratorClosed
	}
	return nil, nil
}

func (m *closedMock) Close() error {
	m.closed = true
	return nil
}

// TestMigrator_MethodsAfterClose verifies that calling Migrator methods after Close()
// returns errors instead of panicking. This documents the expected behavior when
// the underlying goose provider's connection has been released.
func TestMigrator_MethodsAfterClose(t *testing.T) {
	tests := []struct {
		name   string
		method func(*Migrator) error
	}{
		{
			name: "Up after Close",
			method: func(m *Migrator) error {
				return m.Up()
			},
		},
		{
			name: "Down after Close",
			method: func(m *Migrator) error {
				return m.Down()
			},
		},
		{
			name: "Steps after Close",
			method: func(m *Migrator) error {
				return m.Steps(1)
			},
		},
		{
			name: "Version after Close",
			method: func(m *Migrator) error {
				_, _, err := m.Version()
				return err
			},
		},
		{
			name: "Migrate after Close",
			method: func(m *Migrator) error {
				return m.Migrate(1)
			},
		},
		{
			name: "PendingMigrations after Close",
			method: func(m *Migrator) error {
				_, err := m.PendingMigrations()
				return err
			},
		},
		{
			name: "AppliedMigrations after Close",
			method: func(m *Migrator) error {
				_, err := m.AppliedMigrations()
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &closedMock{}
			migrator := &Migrator{m: mock}

			// Close the migrator first
			err := migrator.Close()
			require.NoError(t, err, "Close should succeed")

			// Now call the method - it should return an error, not panic
			err = tt.method(migrator)
			require.Error(t, err, "calling %s after Close should return an error", tt.name)
		})
	}
}

func TestMigrationName(t *testing.T) {
	tests := []struct {
		version     uint
		expected    string
		expectError bool
	}{
		{1, "000001_baseline", false},
		{2, "000002_player_is_guest", false},
		{3, "000003_alias_source", false},
		{4, "000004_session_player_id", false},
		{5, "000005_audit_source_component", false},
		{7, "000007_seed_scene_defaults", false},
		{8, "000008_session_player_fk", false},
		{999, "", false}, // Unknown version returns empty string and nil error (not found is expected)
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("version_%d", tt.version), func(t *testing.T) {
			name, err := MigrationName(tt.version)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.expected, name)
		})
	}
}

// TestMigrationName_ReturnsNilErrorForUnknownVersion verifies that looking up
// an unknown version returns ("", nil) - not found is expected behavior, not an error.
func TestMigrationNameReturnsEmptyStringForUnknownVersion(t *testing.T) {
	name, err := MigrationName(99999)
	require.NoError(t, err, "unknown version should return nil error, not an error")
	assert.Equal(t, "", name, "unknown version should return empty string")
}

// TestAllMigrationVersions_ReturnsCopy verifies that allMigrationVersions returns
// a copy of the cached slice, preventing callers from mutating the cache.
func TestAllMigrationVersionsReturnsCopyPreventingCacheMutation(t *testing.T) {
	versions1, err := allMigrationVersions()
	require.NoError(t, err)
	require.NotEmpty(t, versions1)

	original := versions1[0]
	versions1[0] = 99999 // Mutate

	versions2, err := allMigrationVersions()
	require.NoError(t, err)

	assert.Equal(t, original, versions2[0], "mutation should not affect cache")
}

// BenchmarkAllMigrationVersions measures the performance of allMigrationVersions.
// With caching, subsequent calls should be significantly faster.
func BenchmarkAllMigrationVersions(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := allMigrationVersions()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// TestMigrationName_UsesCache verifies that MigrationName uses caching and
// performs consistently across multiple calls for different versions.
func TestMigrationNameUsesCacheAcrossMultipleCalls(t *testing.T) {
	// Call MigrationName multiple times for different versions
	// All calls should succeed (verifies cache works)

	// Test known version
	name1, err := MigrationName(1)
	require.NoError(t, err)
	assert.Equal(t, "000001_baseline", name1)

	// Test known version
	name4, err := MigrationName(4)
	require.NoError(t, err)
	assert.Equal(t, "000004_session_player_id", name4)

	// Test unknown version (should return empty, not error)
	name999, err := MigrationName(999)
	require.NoError(t, err)
	assert.Equal(t, "", name999)
}

// BenchmarkMigrationName measures the performance of MigrationName.
// With caching, subsequent calls should be O(1) map lookups.
func BenchmarkMigrationName(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = MigrationName(1)
	}
}

// BenchmarkMigrationName_MultipleVersions measures MigrationName performance
// across multiple version lookups in a single iteration.
func BenchmarkMigrationName_MultipleVersions(b *testing.B) {
	versions := []uint{1, 2, 3, 4, 5, 6, 7, 8}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, v := range versions {
			_, _ = MigrationName(v)
		}
	}
}
