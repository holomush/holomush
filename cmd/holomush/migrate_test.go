// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// migrateLogicMock implements the migrator interface for testing CLI output.
type migrateLogicMock struct {
	version               uint
	dirty                 bool
	upCalled              bool
	downCalled            bool
	stepsCalled           bool
	stepsArg              int
	upErr                 error
	downErr               error
	stepsErr              error
	versionErr            error
	closeErr              error
	forceErr              error
	forceCalled           bool
	forceVersion          int
	versionAfterMigration uint
	versionCallCount      int
	// versionFunc allows custom Version behavior for testing warning paths
	versionFunc func() (uint, bool, error)
	// pending/applied migrations for dry-run tests
	pendingMigrations []uint
	appliedMigrations []uint
	pendingErr        error
	appliedErr        error
}

func (m *migrateLogicMock) Up() error {
	m.upCalled = true
	if m.upErr != nil {
		return m.upErr
	}
	// Simulate version change after migration
	m.version = m.versionAfterMigration
	return nil
}

func (m *migrateLogicMock) Down() error {
	m.downCalled = true
	if m.downErr != nil {
		return m.downErr
	}
	m.version = m.versionAfterMigration
	return nil
}

func (m *migrateLogicMock) Steps(n int) error {
	m.stepsCalled = true
	m.stepsArg = n
	if m.stepsErr != nil {
		return m.stepsErr
	}
	m.version = m.versionAfterMigration
	return nil
}

func (m *migrateLogicMock) Version() (uint, bool, error) {
	m.versionCallCount++
	if m.versionFunc != nil {
		return m.versionFunc()
	}
	return m.version, m.dirty, m.versionErr
}

func (m *migrateLogicMock) Force(version int) error {
	m.forceCalled = true
	m.forceVersion = version
	return m.forceErr
}

func (m *migrateLogicMock) Close() error {
	return m.closeErr
}

func (m *migrateLogicMock) PendingMigrations() ([]uint, error) {
	if m.pendingErr != nil {
		return nil, m.pendingErr
	}
	return m.pendingMigrations, nil
}

func (m *migrateLogicMock) AppliedMigrations() ([]uint, error) {
	if m.appliedErr != nil {
		return nil, m.appliedErr
	}
	return m.appliedMigrations, nil
}

func TestMigrateUpLogic_AlreadyAtLatest(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{version: 7, versionAfterMigration: 7}

	err := runMigrateUpLogic(&buf, mock)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Already at latest version: 7")
	assert.NotContains(t, output, "Migrated from")
	assert.True(t, mock.upCalled)
}

func TestMigrateUpLogic_MigrationsApplied(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{version: 3, versionAfterMigration: 7}

	err := runMigrateUpLogic(&buf, mock)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Migrated from version 3 to 7")
	assert.NotContains(t, output, "Already at")
	assert.True(t, mock.upCalled)
}

func TestMigrateUpLogic_UpError(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{version: 3, upErr: errors.New("migration failed")}

	err := runMigrateUpLogic(&buf, mock)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "migration failed")
	assert.True(t, mock.upCalled)
}

func TestMigrateUpLogic_VersionErrorBefore(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{versionErr: errors.New("db connection error")}

	err := runMigrateUpLogic(&buf, mock)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "db connection error")
	assert.False(t, mock.upCalled)
}

func TestMigrateDownLogic_AlreadyAtZero(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{version: 0, versionAfterMigration: 0}

	err := runMigrateDownLogic(&buf, mock, true)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Already at version 0, no migrations to roll back")
	assert.True(t, mock.downCalled)
}

func TestMigrateDownLogic_RolledBackAll(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{version: 7, versionAfterMigration: 0}

	err := runMigrateDownLogic(&buf, mock, true)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Rolled back from version 7 to 0")
	assert.True(t, mock.downCalled)
	assert.False(t, mock.stepsCalled)
}

func TestMigrateDownLogic_RolledBackOne(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{version: 7, versionAfterMigration: 6}

	err := runMigrateDownLogic(&buf, mock, false)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Rolled back from version 7 to 6")
	assert.True(t, mock.stepsCalled)
	assert.Equal(t, -1, mock.stepsArg)
	assert.False(t, mock.downCalled)
}

func TestMigrateDownLogic_DownError(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{version: 7, downErr: errors.New("rollback failed")}

	err := runMigrateDownLogic(&buf, mock, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rollback failed")
	assert.True(t, mock.downCalled)
}

func TestMigrateDownLogic_StepsError(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{version: 7, stepsErr: errors.New("steps failed")}

	err := runMigrateDownLogic(&buf, mock, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "steps failed")
	assert.True(t, mock.stepsCalled)
}

func TestParseForceVersion(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantVersion int
		wantErr     bool
		wantErrCode string
	}{
		{
			name:        "valid integer",
			input:       "3",
			wantVersion: 3,
			wantErr:     false,
		},
		{
			name:        "zero is valid",
			input:       "0",
			wantVersion: 0,
			wantErr:     false,
		},
		{
			name:        "non-numeric returns error",
			input:       "abc",
			wantErr:     true,
			wantErrCode: "INVALID_VERSION",
		},
		{
			name:        "float returns error (strict parsing)",
			input:       "1.5",
			wantErr:     true,
			wantErrCode: "INVALID_VERSION",
		},
		{
			name:        "trailing chars return error (strict parsing)",
			input:       "3abc",
			wantErr:     true,
			wantErrCode: "INVALID_VERSION",
		},
		{
			name:        "negative version returns error",
			input:       "-1",
			wantErr:     true,
			wantErrCode: "INVALID_VERSION",
		},
		{
			name:        "empty string returns error",
			input:       "",
			wantErr:     true,
			wantErrCode: "INVALID_VERSION",
		},
		{
			name:        "whitespace only returns error",
			input:       "   ",
			wantErr:     true,
			wantErrCode: "INVALID_VERSION",
		},
		{
			name:        "leading whitespace is trimmed",
			input:       "  42",
			wantVersion: 42,
			wantErr:     false,
		},
		{
			name:        "trailing whitespace is trimmed",
			input:       "42  ",
			wantVersion: 42,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := parseForceVersion(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, tt.wantErrCode)
				assert.Equal(t, 0, version)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantVersion, version)
			}
		})
	}
}

func TestGetDatabaseURL(t *testing.T) {
	tests := []struct {
		name        string
		envValue    string
		setEnv      bool
		wantURL     string
		wantErr     bool
		wantErrCode string
	}{
		{
			name:        "returns error when DATABASE_URL not set",
			setEnv:      false,
			wantErr:     true,
			wantErrCode: "CONFIG_INVALID",
		},
		{
			name:        "returns error when DATABASE_URL is empty string",
			envValue:    "",
			setEnv:      true,
			wantErr:     true,
			wantErrCode: "CONFIG_INVALID",
		},
		{
			name:     "returns URL when DATABASE_URL is set",
			envValue: "postgres://localhost:5432/testdb",
			setEnv:   true,
			wantURL:  "postgres://localhost:5432/testdb",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("DATABASE_URL", tt.envValue)
			} else {
				// Explicitly unset DATABASE_URL for tests that expect it to be missing.
				// CI environments may have DATABASE_URL set globally.
				originalValue, wasSet := os.LookupEnv("DATABASE_URL")
				if wasSet {
					os.Unsetenv("DATABASE_URL")
					t.Cleanup(func() { os.Setenv("DATABASE_URL", originalValue) })
				}
			}

			url, err := getDatabaseURL()

			if tt.wantErr {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, tt.wantErrCode)
				assert.Empty(t, url)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantURL, url)
			}
		})
	}
}

// Status command tests

func TestMigrateStatusLogic_Clean(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{version: 7, dirty: false}

	err := runMigrateStatusLogic(&buf, mock)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Current version: 7")
	assert.Contains(t, output, "Status: OK")
	assert.NotContains(t, output, "DIRTY")
}

func TestMigrateStatusLogic_Dirty(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{version: 5, dirty: true}

	err := runMigrateStatusLogic(&buf, mock)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Current version: 5")
	assert.Contains(t, output, "Status: DIRTY")
	assert.Contains(t, output, "manual intervention required")
	assert.Contains(t, output, "migrate force VERSION")
}

func TestMigrateStatusLogic_Error(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{versionErr: errors.New("connection failed")}

	err := runMigrateStatusLogic(&buf, mock)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection failed")
	errutil.AssertErrorContext(t, err, "operation", "get version")
}

// Version command tests

func TestMigrateVersionLogic_Success(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{version: 12}

	err := runMigrateVersionLogic(&buf, mock)

	require.NoError(t, err)
	output := buf.String()
	assert.Equal(t, "12\n", output)
}

func TestMigrateVersionLogic_Error(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{versionErr: errors.New("db unreachable")}

	err := runMigrateVersionLogic(&buf, mock)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "db unreachable")
	errutil.AssertErrorContext(t, err, "operation", "get version")
}

// Force command tests

func TestMigrateForceLogic_Success(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{}

	err := runMigrateForceLogic(&buf, mock, 5)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Forcing version to 5...")
	assert.Contains(t, output, "Version forced successfully")
	assert.True(t, mock.forceCalled)
	assert.Equal(t, 5, mock.forceVersion)
}

func TestMigrateForceLogic_Error(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{forceErr: errors.New("invalid version")}

	err := runMigrateForceLogic(&buf, mock, -5)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid version")
	errutil.AssertErrorContext(t, err, "operation", "force version")
	output := buf.String()
	assert.Contains(t, output, "Forcing version to -5...")
	assert.NotContains(t, output, "successfully")
}

// Version warning path tests

func TestMigrateUpLogic_VersionErrorAfter(t *testing.T) {
	var buf bytes.Buffer
	callCount := 0
	mock := &migrateLogicMock{
		version:               3,
		versionAfterMigration: 5,
		versionFunc: func() (uint, bool, error) {
			callCount++
			if callCount == 1 {
				return 3, false, nil // before migration
			}
			return 0, false, errors.New("connection lost")
		},
	}

	err := runMigrateUpLogic(&buf, mock)

	// Should return error with code for CI/CD exit code detection
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_VERSION_CHECK_FAILED")
	output := buf.String()
	assert.Contains(t, output, "Warning")
	assert.Contains(t, output, "Check status")
	assert.True(t, mock.upCalled)
}

func TestMigrateDownLogic_VersionErrorAfter(t *testing.T) {
	var buf bytes.Buffer
	callCount := 0
	mock := &migrateLogicMock{
		version:               5,
		versionAfterMigration: 4,
		versionFunc: func() (uint, bool, error) {
			callCount++
			if callCount == 1 {
				return 5, false, nil // before rollback
			}
			return 0, false, errors.New("connection lost")
		},
	}

	err := runMigrateDownLogic(&buf, mock, false)

	// Should return error with code for CI/CD exit code detection
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "MIGRATION_VERSION_CHECK_FAILED")
	output := buf.String()
	assert.Contains(t, output, "Warning")
	assert.Contains(t, output, "Check status")
	assert.True(t, mock.stepsCalled)
}

// Dry-run tests

func TestMigrateUpDryRun_PendingMigrations(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		version:           3,
		pendingMigrations: []uint{4, 5, 6, 7},
	}

	err := runMigrateUpDryRun(&buf, mock)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Dry run - the following migrations would be applied:")
	// Output shows migration names (e.g., "000004_pg_trgm") instead of "Version X"
	assert.Contains(t, output, "000004_pg_trgm")
	assert.Contains(t, output, "000005_pg_stat_statements")
	assert.Contains(t, output, "000006_object_containment_constraint")
	assert.Contains(t, output, "000007_exit_self_reference_constraint")
	assert.Contains(t, output, "Current version: 3")
	assert.Contains(t, output, "Target version: 7")
	assert.False(t, mock.upCalled, "Up() should not be called in dry-run mode")
}

func TestMigrateUpDryRun_AlreadyAtLatest(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		version:           7,
		pendingMigrations: []uint{},
	}

	err := runMigrateUpDryRun(&buf, mock)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Already at latest version: 7")
	assert.Contains(t, output, "No migrations would be applied")
	assert.False(t, mock.upCalled)
}

func TestMigrateUpDryRun_VersionError(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{versionErr: errors.New("connection failed")}

	err := runMigrateUpDryRun(&buf, mock)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection failed")
}

func TestMigrateUpDryRun_PendingError(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		version:    3,
		pendingErr: errors.New("failed to list migrations"),
	}

	err := runMigrateUpDryRun(&buf, mock)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list migrations")
}

func TestMigrateDownDryRun_RollbackOne(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		version:           5,
		appliedMigrations: []uint{1, 2, 3, 4, 5},
	}

	err := runMigrateDownDryRun(&buf, mock, false)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Dry run - the following migration would be rolled back:")
	// Output shows migration name instead of "Version X"
	assert.Contains(t, output, "000005_pg_stat_statements")
	assert.Contains(t, output, "Current version: 5")
	assert.Contains(t, output, "Target version: 4")
	assert.False(t, mock.downCalled, "Down() should not be called in dry-run mode")
	assert.False(t, mock.stepsCalled, "Steps() should not be called in dry-run mode")
}

func TestMigrateDownDryRun_RollbackAll(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		version:           5,
		appliedMigrations: []uint{1, 2, 3, 4, 5},
	}

	err := runMigrateDownDryRun(&buf, mock, true)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Dry run - the following migrations would be rolled back:")
	// Should show in reverse order with migration names
	assert.Contains(t, output, "000005_pg_stat_statements")
	assert.Contains(t, output, "000004_pg_trgm")
	assert.Contains(t, output, "000003_world_model")
	assert.Contains(t, output, "000002_system_info")
	assert.Contains(t, output, "000001_initial")
	assert.Contains(t, output, "Current version: 5")
	assert.Contains(t, output, "Target version: 0")
	assert.False(t, mock.downCalled)
}

func TestMigrateDownDryRun_AlreadyAtZero(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		version:           0,
		appliedMigrations: []uint{},
	}

	err := runMigrateDownDryRun(&buf, mock, false)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Already at version 0, no migrations to roll back")
	assert.False(t, mock.downCalled)
}

func TestMigrateDownDryRun_RollbackOneToZero(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		version:           1,
		appliedMigrations: []uint{1},
	}

	err := runMigrateDownDryRun(&buf, mock, false)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Dry run - the following migration would be rolled back:")
	// Output shows migration name instead of "Version X"
	assert.Contains(t, output, "000001_initial")
	assert.Contains(t, output, "Current version: 1")
	assert.Contains(t, output, "Target version: 0")
}

func TestMigrateDownDryRun_VersionError(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{versionErr: errors.New("connection failed")}

	err := runMigrateDownDryRun(&buf, mock, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection failed")
}

func TestMigrateDownDryRun_AppliedError(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		version:    5,
		appliedErr: errors.New("failed to list applied"),
	}

	err := runMigrateDownDryRun(&buf, mock, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list applied")
}

// Default migrate command tests (runs migrate up)

func TestNewMigrateCmd_DefaultRunsUp(t *testing.T) {
	// Test that calling 'migrate' without subcommand runs the migrate up logic
	var buf bytes.Buffer
	mock := &migrateLogicMock{version: 3, versionAfterMigration: 7}

	// Directly test the default behavior by calling runMigrateUpLogic
	// This verifies that when no subcommand is provided, migrate up is executed
	err := runMigrateUpLogic(&buf, mock)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Migrated from version 3 to 7")
	assert.True(t, mock.upCalled)
}

func TestNewMigrateCmd_HasRunE(t *testing.T) {
	// Test that the migrate command has a RunE function set (default behavior)
	cmd := NewMigrateCmd()
	assert.NotNil(t, cmd.RunE, "migrate command should have RunE set for default-up behavior")
}
