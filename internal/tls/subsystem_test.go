// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package tlscerts

import (
	"context"
	cryptotls "crypto/tls"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/lifecycle"
)

// Compile-time interface check: *TLSSubsystem must satisfy lifecycle.Subsystem.
var _ lifecycle.Subsystem = (*TLSSubsystem)(nil)

func TestTLSSubsystemIDReturnsSubsystemTLS(t *testing.T) {
	sub := NewTLSSubsystem(TLSSubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemTLS, sub.ID())
}

func TestTLSSubsystemDependsOnDatabase(t *testing.T) {
	sub := NewTLSSubsystem(TLSSubsystemConfig{})
	assert.Equal(t, []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}, sub.DependsOn())
}

// TestNewTLSSubsystemAllocatesNoRuntimeResources proves the constructor
// touches no filesystem state — the "does not allocate or start any runtime
// resources" contract other subsystem constructors document.
func TestNewTLSSubsystemAllocatesNoRuntimeResources(t *testing.T) {
	certsDir := t.TempDir() + "/certs" // deliberately not created
	sub := NewTLSSubsystem(TLSSubsystemConfig{
		CertsDir: certsDir,
		GameID:   func() string { return "test-game" },
	})
	require.NotNil(t, sub)

	_, statErr := os.Stat(certsDir)
	assert.True(t, os.IsNotExist(statErr), "constructor must not create the certs directory")
}

// TestTLSSubsystemTLSConfigPanicsBeforePrepare proves the accessor's
// panic-before-Prepare guard — the same idiom as store.DatabaseSubsystem.Pool().
func TestTLSSubsystemTLSConfigPanicsBeforePrepare(t *testing.T) {
	sub := NewTLSSubsystem(TLSSubsystemConfig{
		CertsDir: t.TempDir(),
		GameID:   func() string { return "test-game" },
	})
	assert.Panics(t, func() { sub.TLSConfig() })
}

// TestTLSSubsystemPrepareResolvesGameIDAndPopulatesTLSConfig proves Prepare
// resolves the gameID from the provider — via a CertEnsurer override, the
// existing test seam — and TLSConfig() returns the ensured config afterward.
func TestTLSSubsystemPrepareResolvesGameIDAndPopulatesTLSConfig(t *testing.T) {
	certsDir := t.TempDir()
	var gotGameID string
	wantConfig := &cryptotls.Config{}
	sub := NewTLSSubsystem(TLSSubsystemConfig{
		CertsDir: certsDir,
		GameID:   func() string { return "resolved-game-id" },
		CertEnsurer: func(_, gameID string) (*cryptotls.Config, error) {
			gotGameID = gameID
			return wantConfig, nil
		},
	})

	require.NoError(t, sub.Prepare(context.Background()))
	require.NoError(t, sub.Activate(context.Background()))
	assert.Equal(t, "resolved-game-id", gotGameID)
	assert.Same(t, wantConfig, sub.TLSConfig())
	assert.NoError(t, sub.Stop(context.Background()))
}

// TestTLSSubsystemPrepareUsesRealEnsurerWhenNoOverride proves Prepare falls back
// to the real EnsureCerts when no CertEnsurer override is supplied.
func TestTLSSubsystemPrepareUsesRealEnsurerWhenNoOverride(t *testing.T) {
	certsDir := t.TempDir()
	sub := NewTLSSubsystem(TLSSubsystemConfig{
		CertsDir: certsDir,
		GameID:   func() string { return "resolved-game-id" },
	})

	require.NoError(t, sub.Prepare(context.Background()))
	require.NoError(t, sub.Activate(context.Background()))
	cfg := sub.TLSConfig()
	require.NotNil(t, cfg)
	assert.NoError(t, sub.Stop(context.Background()))
}

// --- Tests relocated from cmd/holomush/core_test.go (EnsureCerts/fileExists moved here) ---

func TestEnsureCerts(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "holomush-test-certs-*")
	require.NoError(t, err, "Failed to create temp dir")
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})

	gameID := "test-game-id"

	config1, err := EnsureCerts(tmpDir, gameID)
	require.NoError(t, err)
	require.NotNil(t, config1, "EnsureCerts() returned nil config")

	expectedFiles := []string{
		"root-ca.crt",
		"root-ca.key",
		"core.crt",
		"core.key",
		"gateway.crt",
		"gateway.key",
	}
	for _, file := range expectedFiles {
		path := tmpDir + "/" + file
		_, statErr := os.Stat(path)
		assert.False(t, os.IsNotExist(statErr), "Expected file %s was not created", file)
	}

	config2, err := EnsureCerts(tmpDir, gameID)
	require.NoError(t, err, "EnsureCerts() second call error")
	require.NotNil(t, config2, "EnsureCerts() second call returned nil config")
}

// TestEnsureCerts_CorruptedCertFile verifies that EnsureCerts returns an error
// when certificate files exist but are corrupted, rather than silently
// regenerating. Regression test for a bug where any error from LoadServerTLS
// would trigger regeneration, conflating "file not found" with "file corrupted".
func TestEnsureCerts_CorruptedCertFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "holomush-test-certs-corrupted-*")
	require.NoError(t, err, "Failed to create temp dir")
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})

	gameID := "test-game-id"

	_, err = EnsureCerts(tmpDir, gameID)
	require.NoError(t, err, "Initial EnsureCerts() error")

	corruptedCertPath := tmpDir + "/core.crt"
	require.NoError(t, os.WriteFile(corruptedCertPath, []byte("THIS IS NOT A VALID CERTIFICATE"), 0o600), "Failed to corrupt cert file")

	_, err = EnsureCerts(tmpDir, gameID)
	require.Error(t, err, "EnsureCerts() should return error for corrupted cert file, not silently regenerate")

	assert.True(t, assert.Condition(t, func() bool {
		return assert.Contains(t, err.Error(), "certificate") || assert.Contains(t, err.Error(), "load")
	}), "Error should mention certificate/load issue, got: %v", err)
}

// TestEnsureCerts_PermissionDenied verifies that EnsureCerts returns an error
// when certificate files exist but are not readable due to permissions.
func TestEnsureCerts_PermissionDenied(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip("Skipping permission test on Windows")
	}

	tmpDir, err := os.MkdirTemp("", "holomush-test-certs-perms-*")
	require.NoError(t, err, "Failed to create temp dir")
	t.Cleanup(func() {
		_ = os.Chmod(tmpDir+"/core.crt", 0o600)
		_ = os.RemoveAll(tmpDir)
	})

	gameID := "test-game-id"

	_, err = EnsureCerts(tmpDir, gameID)
	require.NoError(t, err, "Initial EnsureCerts() error")

	certPath := tmpDir + "/core.crt"
	require.NoError(t, os.Chmod(certPath, 0o000), "Failed to remove permissions")

	_, err = EnsureCerts(tmpDir, gameID)
	require.Error(t, err, "EnsureCerts() should return error for permission denied, not silently regenerate")

	assert.True(t, assert.Condition(t, func() bool {
		errMsg := err.Error()
		return assert.Contains(t, errMsg, "permission") ||
			assert.Contains(t, errMsg, "denied") ||
			assert.Contains(t, errMsg, "certificate")
	}), "Error should mention permission/denied/certificate issue, got: %v", err)
}

// TestEnsureCerts_DirectoryCreationFailure verifies that EnsureCerts returns
// an error when the certs directory cannot be created.
func TestEnsureCerts_DirectoryCreationFailure(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "holomush-test-certs-block-*")
	require.NoError(t, err, "Failed to create temp file")
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_ = tmpFile.Close()

	badDir := tmpFile.Name() + "/nested/certs"

	_, err = EnsureCerts(badDir, "test-game-id")
	require.Error(t, err, "EnsureCerts() should fail when directory cannot be created")

	assert.True(t, assert.Condition(t, func() bool {
		return assert.Contains(t, err.Error(), "directory") || assert.Contains(t, err.Error(), "not a directory")
	}), "Error should mention directory issue, got: %v", err)
}

// TestEnsureCerts_SaveCertificatesFailure verifies that EnsureCerts returns an
// error when certificates cannot be saved to a read-only directory.
func TestEnsureCerts_SaveCertificatesFailure(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip("Skipping permission test on Windows")
	}

	tmpDir, err := os.MkdirTemp("", "holomush-test-certs-readonly-*")
	require.NoError(t, err, "Failed to create temp dir")
	t.Cleanup(func() {
		//nolint:gosec // G302: Need 0700 to clean up directory
		_ = os.Chmod(tmpDir, 0o700)
		_ = os.RemoveAll(tmpDir)
	})

	//nolint:gosec // G302: Intentionally setting restrictive permissions for test
	require.NoError(t, os.Chmod(tmpDir, 0o500), "Failed to make dir read-only")

	_, err = EnsureCerts(tmpDir, "test-game-id")
	require.Error(t, err, "EnsureCerts() should fail when certs cannot be saved")

	assert.True(t, assert.Condition(t, func() bool {
		errMsg := err.Error()
		return assert.Contains(t, errMsg, "permission") ||
			assert.Contains(t, errMsg, "save") ||
			assert.Contains(t, errMsg, "create") ||
			assert.Contains(t, errMsg, "denied")
	}), "Error should mention save/permission issue, got: %v", err)
}

// TestEnsureCerts_PartialCertState verifies behavior when only some
// certificate files exist (e.g., CA exists but server cert doesn't).
func TestEnsureCerts_PartialCertState(t *testing.T) {
	tests := []struct {
		name          string
		filesToCreate []string
		expectError   bool
	}{
		{
			name:          "only CA cert exists",
			filesToCreate: []string{"root-ca.crt"},
			expectError:   true,
		},
		{
			name:          "only core cert exists",
			filesToCreate: []string{"core.crt"},
			expectError:   true,
		},
		{
			name:          "core cert and key but no CA",
			filesToCreate: []string{"core.crt", "core.key"},
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "holomush-test-partial-*")
			require.NoError(t, err, "Failed to create temp dir")
			t.Cleanup(func() {
				_ = os.RemoveAll(tmpDir)
			})

			for _, file := range tt.filesToCreate {
				path := tmpDir + "/" + file
				require.NoError(t, os.WriteFile(path, []byte("dummy content"), 0o600), "Failed to create %s", file)
			}

			_, err = EnsureCerts(tmpDir, "test-game-id")
			if tt.expectError {
				assert.Error(t, err, "Expected error for partial cert state")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestFileExists verifies the fileExists helper function edge cases.
func TestFileExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "holomush-test-fileexists-*")
	require.NoError(t, err, "Failed to create temp dir")
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})

	tests := []struct {
		name     string
		setup    func(t *testing.T) string
		expected bool
	}{
		{
			name: "existing file",
			setup: func(t *testing.T) string {
				path := tmpDir + "/exists.txt"
				require.NoError(t, os.WriteFile(path, []byte("content"), 0o600), "Failed to write test file")
				return path
			},
			expected: true,
		},
		{
			name: "non-existent file",
			setup: func(_ *testing.T) string {
				return tmpDir + "/does-not-exist.txt"
			},
			expected: false,
		},
		{
			name: "directory exists",
			setup: func(t *testing.T) string {
				path := tmpDir + "/subdir"
				require.NoError(t, os.Mkdir(path, 0o700), "Failed to create test dir")
				return path
			},
			expected: true,
		},
		{
			name: "symlink to existing file",
			setup: func(t *testing.T) string {
				target := tmpDir + "/target.txt"
				require.NoError(t, os.WriteFile(target, []byte("content"), 0o600), "Failed to write target file")
				link := tmpDir + "/link.txt"
				require.NoError(t, os.Symlink(target, link), "Failed to create symlink")
				return link
			},
			expected: true,
		},
		{
			name: "broken symlink",
			setup: func(t *testing.T) string {
				link := tmpDir + "/broken-link.txt"
				require.NoError(t, os.Symlink("/nonexistent/path", link), "Failed to create broken symlink")
				return link
			},
			// Broken symlink: lstat succeeds (link exists) but target doesn't
			// The function uses os.Stat which follows symlinks, so this returns false
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup(t)
			got := fileExists(path)
			assert.Equal(t, tt.expected, got)
		})
	}
}
