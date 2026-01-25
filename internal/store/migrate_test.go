// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"errors"
	"fmt"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMigrator_InvalidURL(t *testing.T) {
	_, err := NewMigrator("invalid://url")
	require.Error(t, err)
	// Verify the error code is set correctly
	errutil.AssertErrorCode(t, err, "MIGRATION_INIT_FAILED")
}

// TestNewMigrator_PostgresqlScheme verifies that postgresql:// URLs are converted
// to pgx5:// for golang-migrate compatibility. The conversion happens at line 64-65
// of migrate.go. This test confirms the scheme is recognized (no "unknown driver" error)
// even though connection will fail with a non-existent host.
func TestNewMigrator_PostgresqlScheme(t *testing.T) {
	// Use postgresql:// which should be converted to pgx5://
	// The error should be a connection error, not an "unknown driver" error
	_, err := NewMigrator("postgresql://localhost:5432/testdb")
	require.Error(t, err, "should fail due to connection, not URL scheme")
	errutil.AssertErrorCode(t, err, "MIGRATION_INIT_FAILED")
	// If the conversion didn't work, we'd see "unknown driver postgresql"
	assert.NotContains(t, err.Error(), "unknown driver")
}

// TestNewMigrator_SourceCleanupOnFailure verifies that when NewWithSourceInstance
// fails (e.g., invalid database URL), the migration source is properly closed.
// This prevents resource leaks when migrator initialization fails.
//
// The test exercises the error path by providing an invalid URL scheme that
// causes NewWithSourceInstance to fail. The source.Close() call in the error
// handler ensures the iofs source is cleaned up before returning the error.
func TestNewMigrator_SourceCleanupOnFailure(t *testing.T) {
	// Use a URL with valid-looking structure but invalid scheme
	// This causes iofs.New to succeed but migrate.NewWithSourceInstance to fail
	// The fix ensures source.Close() is called before returning the error
	_, err := NewMigrator("badscheme://localhost:5432/testdb")
	require.Error(t, err, "should fail with invalid URL scheme")
	errutil.AssertErrorCode(t, err, "MIGRATION_INIT_FAILED")
	// Note: We cannot easily verify source.Close() was called without mocking,
	// but this test documents the error path that the fix addresses.
	// The implementation fix adds source.Close() before returning the error.
}

// mockMigrate implements migrateIface for testing.
type mockMigrate struct {
	upErr          error
	downErr        error
	stepsErr       error
	versionVal     uint
	versionErr     error
	dirty          bool
	forceErr       error
	closeSourceErr error
	closeDbErr     error
}

func (m *mockMigrate) Up() error                    { return m.upErr }
func (m *mockMigrate) Down() error                  { return m.downErr }
func (m *mockMigrate) Steps(_ int) error            { return m.stepsErr }
func (m *mockMigrate) Version() (uint, bool, error) { return m.versionVal, m.dirty, m.versionErr }
func (m *mockMigrate) Force(_ int) error            { return m.forceErr }
func (m *mockMigrate) Close() (error, error)        { return m.closeSourceErr, m.closeDbErr }

func TestMigrator_Up_Success(t *testing.T) {
	m := &Migrator{m: &mockMigrate{}}
	err := m.Up()
	require.NoError(t, err)
}

func TestMigrator_Up_NoChange(t *testing.T) {
	m := &Migrator{m: &mockMigrate{upErr: migrate.ErrNoChange}}
	err := m.Up()
	require.NoError(t, err, "ErrNoChange should be treated as success")
}

func TestMigrator_Up_Error(t *testing.T) {
	m := &Migrator{m: &mockMigrate{upErr: errors.New("database locked")}}
	err := m.Up()
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_UP_FAILED")
}

func TestMigrator_Down_Success(t *testing.T) {
	m := &Migrator{m: &mockMigrate{}}
	err := m.Down()
	require.NoError(t, err)
}

func TestMigrator_Down_NoChange(t *testing.T) {
	m := &Migrator{m: &mockMigrate{downErr: migrate.ErrNoChange}}
	err := m.Down()
	require.NoError(t, err)
}

func TestMigrator_Down_Error(t *testing.T) {
	m := &Migrator{m: &mockMigrate{downErr: errors.New("constraint violation")}}
	err := m.Down()
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_DOWN_FAILED")
}

func TestMigrator_Steps_Success(t *testing.T) {
	m := &Migrator{m: &mockMigrate{}}
	err := m.Steps(3)
	require.NoError(t, err)
}

func TestMigrator_Steps_NoChange(t *testing.T) {
	m := &Migrator{m: &mockMigrate{stepsErr: migrate.ErrNoChange}}
	err := m.Steps(-1)
	require.NoError(t, err)
}

func TestMigrator_Steps_ZeroIsNoOp(t *testing.T) {
	// golang-migrate returns ErrNoChange when n=0, which our wrapper
	// treats as success. This documents that Steps(0) is a safe no-op.
	m := &Migrator{m: &mockMigrate{stepsErr: migrate.ErrNoChange}}
	err := m.Steps(0)
	require.NoError(t, err, "Steps(0) should be a no-op returning nil")
}

func TestMigrator_Steps_Error(t *testing.T) {
	m := &Migrator{m: &mockMigrate{stepsErr: errors.New("invalid step")}}
	err := m.Steps(5)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_STEPS_FAILED")
}

func TestMigrator_Version_Success(t *testing.T) {
	m := &Migrator{m: &mockMigrate{versionVal: 7, dirty: false}}
	version, dirty, err := m.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(7), version)
	assert.False(t, dirty)
}

func TestMigrator_Version_Dirty(t *testing.T) {
	m := &Migrator{m: &mockMigrate{versionVal: 5, dirty: true}}
	version, dirty, err := m.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(5), version)
	assert.True(t, dirty)
}

func TestMigrator_Version_NilVersion(t *testing.T) {
	m := &Migrator{m: &mockMigrate{versionErr: migrate.ErrNilVersion}}
	version, dirty, err := m.Version()
	require.NoError(t, err, "ErrNilVersion should return 0, false, nil")
	assert.Equal(t, uint(0), version)
	assert.False(t, dirty)
}

func TestMigrator_Version_Error(t *testing.T) {
	m := &Migrator{m: &mockMigrate{versionErr: errors.New("connection lost")}}
	_, _, err := m.Version()
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_VERSION_FAILED")
}

func TestMigrator_Force_Success(t *testing.T) {
	m := &Migrator{m: &mockMigrate{}}
	err := m.Force(5)
	require.NoError(t, err)
}

func TestMigrator_Force_Error(t *testing.T) {
	m := &Migrator{m: &mockMigrate{forceErr: errors.New("invalid version")}}
	err := m.Force(5)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_FORCE_FAILED")
}

func TestMigrator_Force_NegativeVersionRejected(t *testing.T) {
	m := &Migrator{m: &mockMigrate{}}
	err := m.Force(-1)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_VERSION")
}

func TestMigrator_Close_Success(t *testing.T) {
	m := &Migrator{m: &mockMigrate{}}
	err := m.Close()
	require.NoError(t, err)
}

func TestMigrator_Close_SourceError(t *testing.T) {
	m := &Migrator{m: &mockMigrate{closeSourceErr: errors.New("source close failed")}}
	err := m.Close()
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_CLOSE_FAILED")
	errutil.AssertErrorContext(t, err, "component", "source")
}

func TestMigrator_Close_DatabaseError(t *testing.T) {
	m := &Migrator{m: &mockMigrate{closeDbErr: errors.New("db close failed")}}
	err := m.Close()
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_CLOSE_FAILED")
	errutil.AssertErrorContext(t, err, "component", "database")
}

func TestMigrator_Close_BothErrors(t *testing.T) {
	// When both source and database close fail, we should report both errors
	m := &Migrator{m: &mockMigrate{
		closeSourceErr: errors.New("source close failed"),
		closeDbErr:     errors.New("db close failed"),
	}}
	err := m.Close()
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_CLOSE_FAILED")
	errutil.AssertErrorContext(t, err, "component", "both")
	// The error message should contain both original errors
	assert.Contains(t, err.Error(), "source close failed")
	assert.Contains(t, err.Error(), "db close failed")
}

func TestMigrator_PendingMigrations_Success(t *testing.T) {
	// At version 3, migrations 4-7 should be pending
	m := &Migrator{m: &mockMigrate{versionVal: 3}}
	pending, err := m.PendingMigrations()
	require.NoError(t, err)
	assert.Equal(t, []uint{4, 5, 6, 7}, pending)
}

func TestMigrator_PendingMigrations_AtLatest(t *testing.T) {
	// At version 7 (latest), no migrations should be pending
	m := &Migrator{m: &mockMigrate{versionVal: 7}}
	pending, err := m.PendingMigrations()
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestMigrator_PendingMigrations_AtZero(t *testing.T) {
	// At version 0 (fresh db), all migrations should be pending
	m := &Migrator{m: &mockMigrate{versionVal: 0, versionErr: migrate.ErrNilVersion}}
	pending, err := m.PendingMigrations()
	require.NoError(t, err)
	assert.Equal(t, []uint{1, 2, 3, 4, 5, 6, 7}, pending)
}

func TestMigrator_PendingMigrations_VersionError(t *testing.T) {
	m := &Migrator{m: &mockMigrate{versionErr: errors.New("connection lost")}}
	_, err := m.PendingMigrations()
	require.Error(t, err)
	// Error should include operation context for debugging
	errutil.AssertErrorContext(t, err, "operation", "get pending migrations")
}

func TestMigrator_AppliedMigrations_Success(t *testing.T) {
	// At version 3, migrations 1-3 should be applied
	m := &Migrator{m: &mockMigrate{versionVal: 3}}
	applied, err := m.AppliedMigrations()
	require.NoError(t, err)
	assert.Equal(t, []uint{1, 2, 3}, applied)
}

func TestMigrator_AppliedMigrations_AtZero(t *testing.T) {
	// At version 0, no migrations applied
	m := &Migrator{m: &mockMigrate{versionVal: 0, versionErr: migrate.ErrNilVersion}}
	applied, err := m.AppliedMigrations()
	require.NoError(t, err)
	assert.Empty(t, applied)
}

func TestMigrator_AppliedMigrations_AtLatest(t *testing.T) {
	// At version 7, all migrations applied
	m := &Migrator{m: &mockMigrate{versionVal: 7}}
	applied, err := m.AppliedMigrations()
	require.NoError(t, err)
	assert.Equal(t, []uint{1, 2, 3, 4, 5, 6, 7}, applied)
}

func TestMigrator_AppliedMigrations_VersionError(t *testing.T) {
	m := &Migrator{m: &mockMigrate{versionErr: errors.New("connection lost")}}
	_, err := m.AppliedMigrations()
	require.Error(t, err)
	// Error should include operation context for debugging
	errutil.AssertErrorContext(t, err, "operation", "get applied migrations")
}

// closedMock implements migrateIface and returns errors after Close() is called.
// This simulates the behavior of golang-migrate after resources are released.
type closedMock struct {
	closed bool
}

var errMigratorClosed = errors.New("migrator is closed")

func (m *closedMock) Up() error {
	if m.closed {
		return errMigratorClosed
	}
	return nil
}

func (m *closedMock) Down() error {
	if m.closed {
		return errMigratorClosed
	}
	return nil
}

func (m *closedMock) Steps(_ int) error {
	if m.closed {
		return errMigratorClosed
	}
	return nil
}

func (m *closedMock) Version() (uint, bool, error) {
	if m.closed {
		return 0, false, errMigratorClosed
	}
	return 1, false, nil
}

func (m *closedMock) Force(_ int) error {
	if m.closed {
		return errMigratorClosed
	}
	return nil
}

func (m *closedMock) Close() (error, error) {
	m.closed = true
	return nil, nil
}

// TestMigrator_MethodsAfterClose verifies that calling Migrator methods after Close()
// returns errors instead of panicking. This documents the expected behavior when
// the underlying golang-migrate resources have been released.
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
			name: "Force after Close",
			method: func(m *Migrator) error {
				return m.Force(1)
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
		{1, "000001_initial", false},
		{2, "000002_system_info", false},
		{3, "000003_world_model", false},
		{7, "000007_exit_self_reference_constraint", false},
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
func TestMigrationName_ReturnsNilErrorForUnknownVersion(t *testing.T) {
	name, err := MigrationName(99999)
	require.NoError(t, err, "unknown version should return nil error, not an error")
	assert.Equal(t, "", name, "unknown version should return empty string")
}

// TestAllMigrationVersions_ReturnsCopy verifies that allMigrationVersions returns
// a copy of the cached slice, preventing callers from mutating the cache.
func TestAllMigrationVersions_ReturnsCopy(t *testing.T) {
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
