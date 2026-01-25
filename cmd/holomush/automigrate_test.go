// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"context"
	cryptotls "crypto/tls"
	"fmt"
	"log/slog"
	"testing"

	"github.com/holomush/holomush/internal/control"
	"github.com/holomush/holomush/internal/observability"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// autoMigrateMockMigrator implements AutoMigrator interface for testing.
type autoMigrateMockMigrator struct {
	upCalled    bool
	upError     error
	closeCalled bool
	closeError  error
}

func (m *autoMigrateMockMigrator) Up() error {
	m.upCalled = true
	return m.upError
}

func (m *autoMigrateMockMigrator) Close() error {
	m.closeCalled = true
	return m.closeError
}

func TestAutoMigrate_RunsByDefault(t *testing.T) {
	// Set up mock migrator
	migrator := &autoMigrateMockMigrator{}

	// Create context that will be cancelled immediately after startup
	ctx, cancel := context.WithCancel(context.Background())

	// Create deps with mock migrator
	deps := &CoreDeps{
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return t.TempDir(), nil
			},
			ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
				return testTLSConfig(), nil
			},
			ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
				return &mockControlServer{}, nil
			},
			ObservabilityServerFactory: func(_ string, _ observability.ReadinessChecker) ObservabilityServer {
				return &mockObservabilityServer{}
			},
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		MigratorFactory: func(_ string) (AutoMigrator, error) {
			return migrator, nil
		},
		AutoMigrateGetter: func() bool {
			return true // Default behavior
		},
	}

	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		metricsAddr: "", // Disable metrics for this test
		logFormat:   "json",
	}

	// Cancel context immediately to prevent waiting for signals
	cancel()

	// Run core - it should return quickly since context is cancelled
	_ = runCoreWithDeps(ctx, cfg, NewCoreCmd(), deps)

	// Verify migration was called
	assert.True(t, migrator.upCalled, "Migrator.Up() should be called by default")
	assert.True(t, migrator.closeCalled, "Migrator.Close() should be called")
}

func TestAutoMigrate_DisabledWhenEnvVarFalse(t *testing.T) {
	// Set up mock migrator
	migrator := &autoMigrateMockMigrator{}

	// Create context that will be cancelled immediately
	ctx, cancel := context.WithCancel(context.Background())

	deps := &CoreDeps{
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return t.TempDir(), nil
			},
			ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
				return testTLSConfig(), nil
			},
			ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
				return &mockControlServer{}, nil
			},
			ObservabilityServerFactory: func(_ string, _ observability.ReadinessChecker) ObservabilityServer {
				return &mockObservabilityServer{}
			},
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		MigratorFactory: func(_ string) (AutoMigrator, error) {
			return migrator, nil
		},
		AutoMigrateGetter: func() bool {
			return false // Explicitly disabled
		},
	}

	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		metricsAddr: "",
		logFormat:   "json",
	}

	cancel()
	_ = runCoreWithDeps(ctx, cfg, NewCoreCmd(), deps)

	// Verify migration was NOT called
	assert.False(t, migrator.upCalled, "Migrator.Up() should NOT be called when disabled")
}

func TestAutoMigrate_ErrorSurfaced(t *testing.T) {
	// Set up mock migrator that returns an error
	migrationErr := fmt.Errorf("migration failed: column already exists")
	migrator := &autoMigrateMockMigrator{
		upError: migrationErr,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := &CoreDeps{
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return t.TempDir(), nil
			},
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		MigratorFactory: func(_ string) (AutoMigrator, error) {
			return migrator, nil
		},
		AutoMigrateGetter: func() bool {
			return true
		},
	}

	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		metricsAddr: "",
		logFormat:   "json",
	}

	err := runCoreWithDeps(ctx, cfg, NewCoreCmd(), deps)

	require.Error(t, err, "Migration error should be surfaced")
	assert.Contains(t, err.Error(), "migration", "Error should mention migration")
	assert.True(t, migrator.upCalled, "Migrator.Up() should have been called")
	assert.True(t, migrator.closeCalled, "Migrator.Close() should be called even on error")
}

func TestAutoMigrate_MigratorCreationError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := &CoreDeps{
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return t.TempDir(), nil
			},
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		MigratorFactory: func(_ string) (AutoMigrator, error) {
			return nil, fmt.Errorf("failed to connect to database for migrations")
		},
		AutoMigrateGetter: func() bool {
			return true
		},
	}

	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		metricsAddr: "",
		logFormat:   "json",
	}

	err := runCoreWithDeps(ctx, cfg, NewCoreCmd(), deps)

	require.Error(t, err, "Migrator creation error should be surfaced")
	assert.Contains(t, err.Error(), "migration", "Error should mention migration")
}

func TestParseAutoMigrate(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected bool
	}{
		{
			name:     "not set - defaults to true",
			envValue: "",
			expected: true,
		},
		{
			name:     "set to true",
			envValue: "true",
			expected: true,
		},
		{
			name:     "set to false",
			envValue: "false",
			expected: false,
		},
		{
			name:     "set to 1",
			envValue: "1",
			expected: true,
		},
		{
			name:     "set to 0",
			envValue: "0",
			expected: false,
		},
		{
			name:     "set to TRUE (uppercase)",
			envValue: "TRUE",
			expected: true,
		},
		{
			name:     "set to FALSE (uppercase)",
			envValue: "FALSE",
			expected: false,
		},
		{
			name:     "invalid value - defaults to true",
			envValue: "invalid",
			expected: true,
		},
		{
			name:     "set to False (mixed case)",
			envValue: "False",
			expected: false,
		},
		{
			name:     "set to fAlSe (mixed case)",
			envValue: "fAlSe",
			expected: false,
		},
		{
			name:     "set to True (mixed case)",
			envValue: "True",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv("HOLOMUSH_DB_AUTO_MIGRATE", tt.envValue)
			}
			result := parseAutoMigrate()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRunAutoMigration(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		migrator := &autoMigrateMockMigrator{}
		err := runAutoMigration("postgres://test@localhost/test", func(_ string) (AutoMigrator, error) {
			return migrator, nil
		})
		require.NoError(t, err)
		assert.True(t, migrator.upCalled)
		assert.True(t, migrator.closeCalled)
	})

	t.Run("factory error", func(t *testing.T) {
		err := runAutoMigration("postgres://test@localhost/test", func(_ string) (AutoMigrator, error) {
			return nil, fmt.Errorf("connection failed")
		})
		require.Error(t, err)
		// Check that the error has the MIGRATION error code from oops
		errutil.AssertErrorCode(t, err, "MIGRATION_INIT_FAILED")
	})

	t.Run("up error", func(t *testing.T) {
		migrator := &autoMigrateMockMigrator{upError: fmt.Errorf("schema error")}
		err := runAutoMigration("postgres://test@localhost/test", func(_ string) (AutoMigrator, error) {
			return migrator, nil
		})
		require.Error(t, err)
		// Check that the error has the MIGRATION error code from oops
		errutil.AssertErrorCode(t, err, "AUTO_MIGRATION_FAILED")
		assert.True(t, migrator.closeCalled, "Close should be called even on Up() error")
	})

	t.Run("already at latest version (no migrations needed)", func(t *testing.T) {
		// When database is already at latest version, Up() succeeds (returns nil)
		// because our wrapper treats ErrNoChange as success
		migrator := &autoMigrateMockMigrator{}
		err := runAutoMigration("postgres://test@localhost/test", func(_ string) (AutoMigrator, error) {
			return migrator, nil
		})
		require.NoError(t, err, "auto-migration should succeed when already at latest")
		assert.True(t, migrator.upCalled, "Up() should be called")
		assert.True(t, migrator.closeCalled, "Close() should be called")
	})

	t.Run("close error is logged but does not fail operation", func(t *testing.T) {
		// Capture log output to verify warning is logged
		var buf bytes.Buffer
		handler := slog.NewJSONHandler(&buf, nil)
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(handler))
		defer slog.SetDefault(oldLogger)

		migrator := &autoMigrateMockMigrator{closeError: fmt.Errorf("connection reset")}
		err := runAutoMigration("postgres://test@localhost/test", func(_ string) (AutoMigrator, error) {
			return migrator, nil
		})

		// Main operation should succeed despite close error
		require.NoError(t, err, "close error should not fail the operation")
		assert.True(t, migrator.upCalled, "Up() should be called")
		assert.True(t, migrator.closeCalled, "Close() should be called")

		// Verify warning was logged
		logOutput := buf.String()
		assert.Contains(t, logOutput, "error closing migrator", "Should log warning about close error")
		assert.Contains(t, logOutput, "connection reset", "Warning should include the error message")
		assert.Contains(t, logOutput, "connection may leak", "Warning should include the note")
	})
}

func TestParseAutoMigrate_WarnsOnInvalidValue(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	t.Setenv("HOLOMUSH_DB_AUTO_MIGRATE", "flase") // typo

	result := parseAutoMigrate()

	assert.True(t, result, "Invalid value should default to true")
	assert.Contains(t, buf.String(), "unrecognized", "Should log warning for invalid value")
	assert.Contains(t, buf.String(), "flase", "Warning should include the invalid value")
}
