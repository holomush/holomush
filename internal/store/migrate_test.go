// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"errors"
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
