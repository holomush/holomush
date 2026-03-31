// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
)

type mockMigrator struct {
	upCalled bool
	upErr    error
	closeErr error
}

func (m *mockMigrator) Up() error {
	m.upCalled = true
	return m.upErr
}

func (m *mockMigrator) Close() error {
	return m.closeErr
}

func TestMigrationBootstrapper_Priority(t *testing.T) {
	bootstrapper := NewMigrationBootstrapper("postgres://localhost", nil, true)
	assert.Equal(t, plugins.BootstrapPrioritySchema, bootstrapper.Priority())
}

func TestMigrationBootstrapper(t *testing.T) {
	tests := []struct {
		name           string
		enabled        bool
		migrator       *mockMigrator
		factoryErr     error
		expectErr      bool
		expectCode     string
		expectUpCalled bool
	}{
		{
			name:           "calls migrator Up() when enabled",
			enabled:        true,
			migrator:       &mockMigrator{},
			expectErr:      false,
			expectUpCalled: true,
		},
		{
			name:           "skips migration when disabled",
			enabled:        false,
			migrator:       &mockMigrator{},
			expectErr:      false,
			expectUpCalled: false,
		},
		{
			name:       "returns error on migrator creation failure",
			enabled:    true,
			migrator:   nil,
			factoryErr: errors.New("connection refused"),
			expectErr:  true,
			expectCode: "MIGRATION_INIT_FAILED",
		},
		{
			name:           "returns error on Up() failure",
			enabled:        true,
			migrator:       &mockMigrator{upErr: errors.New("migration failed")},
			expectErr:      true,
			expectCode:     "AUTO_MIGRATION_FAILED",
			expectUpCalled: true,
		},
		{
			name:           "ignores Close() error",
			enabled:        true,
			migrator:       &mockMigrator{closeErr: errors.New("close failed")},
			expectErr:      false,
			expectUpCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := func(_ string) (AutoMigrator, error) {
				if tt.factoryErr != nil {
					return nil, tt.factoryErr
				}
				return tt.migrator, nil
			}

			bootstrapper := NewMigrationBootstrapper(
				"postgres://localhost",
				factory,
				tt.enabled,
			)

			err := bootstrapper.Bootstrap(context.Background(), nil, "")

			if tt.expectErr {
				require.Error(t, err)
				if tt.expectCode != "" {
					errutil.AssertErrorCode(t, err, tt.expectCode)
				}
			} else {
				assert.NoError(t, err)
			}

			if tt.migrator != nil {
				if tt.expectUpCalled {
					assert.True(t, tt.migrator.upCalled)
				} else {
					assert.False(t, tt.migrator.upCalled)
				}
			}
		})
	}
}

// Compile-time check.
var _ plugins.BootstrapPlugin = (*MigrationBootstrapper)(nil)
