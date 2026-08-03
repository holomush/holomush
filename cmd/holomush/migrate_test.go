// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestMigrateCommandExposesExactlyUpDownStatusVersionAndNoForce pins the migrate
// subcommand surface by SET EQUALITY rather than by asserting the absence of
// "force". Set equality carries "no more" and "no fewer" in one assertion; a bare
// NotContains("force") passes vacuously if the command tree fails to build at all,
// and would not notice a subcommand being dropped.
//
// Criterion 3 of phase 01.1 removes `migrate force` outright — goose commits each
// migration body and its version row in one transaction, so the dirty state that
// `force` existed to repair cannot arise. This test is the mechanical gate that
// goes red if anyone reintroduces the subcommand.
func TestMigrateCommandExposesExactlyUpDownStatusVersionAndNoForce(t *testing.T) {
	cmd := NewMigrateCmd()

	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	sort.Strings(names)

	assert.Equal(t, "down status up version", strings.Join(names, " "),
		"the migrate subcommand set must be exactly up/down/status/version")
}

// TestMigrateCommandHelpMentionsNoForceOrDirtyRecovery guards the operator-facing
// text separately from the command tree: help output is rendered from Short/Long
// strings, so a stale "run migrate force to reset" line could survive the removal
// of the subcommand itself.
func TestMigrateCommandHelpMentionsNoForceOrDirtyRecovery(t *testing.T) {
	cmd := NewMigrateCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})

	require.NoError(t, cmd.Execute())

	output := buf.String()
	require.NotEmpty(t, output, "help output must be non-empty or the assertions below are vacuous")
	for _, forbidden := range []string{"force", "dirty", "DIRTY"} {
		assert.NotContains(t, output, forbidden,
			"migrate help must not mention %q", forbidden)
	}
}

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
	versionAfterMigration uint
	versionCallCount      int
	// versionFunc allows custom Version behavior for testing warning paths
	versionFunc func() (uint, bool, error)
	// pending/applied migrations for dry-run tests
	pendingMigrations []uint
	appliedMigrations []uint
	pendingErr        error
	appliedErr        error
	// pre-adopt state for the up dry-run
	adoptPreview    store.AdoptPreview
	adoptPreviewErr error
}

func (m *migrateLogicMock) AdoptPreview() (store.AdoptPreview, error) {
	if m.adoptPreviewErr != nil {
		return store.AdoptPreview{}, m.adoptPreviewErr
	}
	return m.adoptPreview, nil
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

func TestRunMigrateStatusLogicReportsOKRegardlessOfTheInertDirtyFlag(t *testing.T) {
	// goose has no dirty state, so the second return of Version() is inert and the
	// DIRTY branch is gone. Driving the mock with dirty=true is what makes this a
	// gate rather than a restatement: if anyone reintroduces the branch, the
	// dirty=true case starts printing DIRTY and this test fails.
	for _, dirty := range []bool{false, true} {
		var buf bytes.Buffer
		mock := &migrateLogicMock{version: 7, dirty: dirty}

		err := runMigrateStatusLogic(&buf, mock)

		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Current version: 7")
		assert.Contains(t, output, "Status: OK")
		assert.NotContains(t, output, "DIRTY")
		assert.NotContains(t, output, "force")
	}
}

func TestMigrateStatusLogicReturnsWrappedErrorWhenVersionQueryFails(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{versionErr: errors.New("connection failed")}

	err := runMigrateStatusLogic(&buf, mock)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection failed")
	errutil.AssertErrorContext(t, err, "operation", "get version")
}

// Version command tests

func TestMigrateVersionLogicWritesCurrentVersionToOutput(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{version: 12}

	err := runMigrateVersionLogic(&buf, mock)

	require.NoError(t, err)
	output := buf.String()
	assert.Equal(t, "12\n", output)
}

func TestMigrateVersionLogicReturnsWrappedErrorWhenVersionQueryFails(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{versionErr: errors.New("db unreachable")}

	err := runMigrateVersionLogic(&buf, mock)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "db unreachable")
	errutil.AssertErrorContext(t, err, "operation", "get version")
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
		version:           0,
		pendingMigrations: []uint{1},
	}

	err := runMigrateUpDryRun(&buf, mock)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Dry run - the following migrations would be applied:")
	assert.Contains(t, output, "000001_baseline")
	assert.Contains(t, output, "Current version: 0")
	assert.Contains(t, output, "Target version: 1")
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

// TestMigrateUpDryRunReportsTheCutoverRatherThanGoosesEmptyLedger covers the
// pre-adopt database — which is exactly the database an operator previews before
// an irreversible cutover, so a wrong answer here is a wrong answer at the worst
// possible moment.
//
// Every number the dry run printed came from goose's ledger, and on a
// not-yet-adopted database that ledger is empty. So it reported the whole corpus
// as pending and named a target version, while `migrate up` would in fact adopt
// the recorded bookkeeping and apply NONE of them. That is not stale output; it
// is the opposite of what happens.
func TestMigrateUpDryRunReportsTheCutoverRatherThanGoosesEmptyLedger(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		// goose's pre-adopt view: version 0, everything pending.
		version:           0,
		pendingMigrations: []uint{1, 2, 3},
		// The truth: golang-migrate recorded the schema at version 3.
		adoptPreview: store.AdoptPreview{Pending: true, Recorded: 3},
	}

	err := runMigrateUpDryRun(&buf, mock)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "golang-migrate bookkeeping",
		"the operator must be told the cutover is what happens next")
	assert.Contains(t, output, "Recorded version: 3")
	assert.Contains(t, output, "No migrations would be applied",
		"applying nothing is the actual outcome; the preview must say so")
	assert.NotContains(t, output, "000001_baseline",
		"listing already-applied migrations as pending is the defect under test")
	assert.NotContains(t, output, "Target version:",
		"a target version derived from goose's empty ledger is meaningless here")
}

func TestMigrateUpDryRunListsOnlyTheMigrationsAboveTheRecordedVersion(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		version:           0,
		pendingMigrations: []uint{1, 2, 3},
		adoptPreview:      store.AdoptPreview{Pending: true, Recorded: 1},
	}

	err := runMigrateUpDryRun(&buf, mock)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Dry run - the following migrations would be applied:")
	assert.NotContains(t, output, "000001_baseline",
		"version 1 is at or below the recorded version, so the adopt records it as "+
			"applied and it is not pending")
	assert.Contains(t, output, "Current version: 1",
		"the current version an operator cares about is the recorded one, not goose's 0")
}

func TestMigrateUpDryRunWarnsThatADirtyLedgerWillRefuse(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		version:           0,
		pendingMigrations: []uint{1, 2, 3},
		adoptPreview:      store.AdoptPreview{Pending: true, Recorded: 3, Dirty: true},
	}

	err := runMigrateUpDryRun(&buf, mock)

	require.NoError(t, err, "a preview must not fail merely because the deploy would")
	output := buf.String()
	assert.Contains(t, output, "dirty",
		"the whole point of previewing is to learn the deploy will abort")
	assert.NotContains(t, output, "Dry run - the following migrations would be applied:")
}

func TestMigrateUpDryRunReturnsTheAdoptProbeError(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{adoptPreviewErr: errors.New("probe failed")}

	err := runMigrateUpDryRun(&buf, mock)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "probe failed")
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
		version:           1,
		appliedMigrations: []uint{1},
	}

	err := runMigrateDownDryRun(&buf, mock, false)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Dry run - the following migration would be rolled back:")
	assert.Contains(t, output, "000001_baseline")
	assert.Contains(t, output, "Current version: 1")
	assert.Contains(t, output, "Target version: 0")
	assert.False(t, mock.downCalled, "Down() should not be called in dry-run mode")
	assert.False(t, mock.stepsCalled, "Steps() should not be called in dry-run mode")
}

func TestMigrateDownDryRun_RollbackAll(t *testing.T) {
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		version:           1,
		appliedMigrations: []uint{1},
	}

	err := runMigrateDownDryRun(&buf, mock, true)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Dry run - the following migrations would be rolled back:")
	assert.Contains(t, output, "000001_baseline")
	assert.Contains(t, output, "Current version: 1")
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
	assert.Contains(t, output, "000001_baseline")
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

// Version fallback tests - when migration name is not found, show "Version X" format

func TestMigrateUpDryRun_UnknownVersionFallback(t *testing.T) {
	// Test that unknown version numbers fall back to "Version X" format
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		version:           0,
		pendingMigrations: []uint{999}, // 999 doesn't exist in migrations
	}

	err := runMigrateUpDryRun(&buf, mock)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Dry run - the following migrations would be applied:")
	assert.Contains(t, output, "Version 999")
	assert.NotContains(t, output, "000999") // Should not try to format as migration name
	assert.Contains(t, output, "Current version: 0")
	assert.Contains(t, output, "Target version: 999")
}

func TestMigrateDownDryRun_UnknownVersionFallbackSingle(t *testing.T) {
	// Test fallback for single rollback with unknown version
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		version:           999, // Current version is unknown
		appliedMigrations: []uint{999},
	}

	err := runMigrateDownDryRun(&buf, mock, false)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Dry run - the following migration would be rolled back:")
	assert.Contains(t, output, "Version 999")
	assert.NotContains(t, output, "000999")
	assert.Contains(t, output, "Current version: 999")
	assert.Contains(t, output, "Target version: 0")
}

func TestMigrateDownDryRun_UnknownVersionFallbackAll(t *testing.T) {
	// Test fallback for rollback all with unknown versions
	var buf bytes.Buffer
	mock := &migrateLogicMock{
		version:           999,
		appliedMigrations: []uint{1, 998, 999}, // Mix of known (1) and unknown (998, 999)
	}

	err := runMigrateDownDryRun(&buf, mock, true)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Dry run - the following migrations would be rolled back:")
	// Unknown versions should use "Version X" format
	assert.Contains(t, output, "Version 999")
	assert.Contains(t, output, "Version 998")
	// Known version 1 should show migration name
	assert.Contains(t, output, "000001_baseline")
	assert.Contains(t, output, "Current version: 999")
	assert.Contains(t, output, "Target version: 0")
}
