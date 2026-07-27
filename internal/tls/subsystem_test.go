// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package tlscerts

import (
	"context"
	cryptotls "crypto/tls"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/pkg/errutil"
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

// TestTLSSubsystemPrepareReportsSetupFailureAndLeavesTheSubsystemUnstarted
// proves the subsystem fails to start rather than starting without TLS. The
// second half is the security-relevant half: if Prepare returned an error but
// still populated tlsConfig, a caller that logged the error and carried on
// would get a config it never validated. TLSConfig() must still panic, exactly
// as it does before Prepare has ever run.
//
// The ensurer returns a plain (non-oops) error here, so TLS_SETUP_FAILED is the
// only code in the chain. The companion test below covers the case where the
// cause carries its own stage code.
func TestTLSSubsystemPrepareReportsSetupFailureAndLeavesTheSubsystemUnstarted(t *testing.T) {
	certsDir := t.TempDir()
	cause := errors.New("certificate material unavailable")

	sub := NewTLSSubsystem(TLSSubsystemConfig{
		CertsDir: certsDir,
		GameID:   func() string { return "resolved-game-id" },
		CertEnsurer: func(string, string) (*cryptotls.Config, error) {
			return nil, cause
		},
	})

	err := sub.Prepare(context.Background())

	require.Error(t, err, "Prepare must fail when the ensurer fails")
	require.ErrorIs(t, err, cause, "the underlying cause must remain in the error chain")
	errutil.AssertErrorCode(t, err, "TLS_SETUP_FAILED")
	errutil.AssertErrorContext(t, err, "certs_dir", certsDir)
	assert.Panics(t, func() { sub.TLSConfig() },
		"a failed Prepare must leave the subsystem unstarted, not holding an unvalidated config")
}

// TestTLSSubsystemPrepareSurfacesTheCertificateStageThatFailed proves an
// operator reading Prepare's error learns WHICH certificate stage failed, not
// merely that TLS setup failed. oops reports the deepest code in the chain, so
// EnsureCerts' stage code survives Prepare's TLS_SETUP_FAILED wrap while
// Prepare's certs_dir context is still attached. Without this the two failures
// an operator most needs to tell apart — "I cannot write to the certs
// directory" and "the game ID is malformed" — would be one opaque message.
func TestTLSSubsystemPrepareSurfacesTheCertificateStageThatFailed(t *testing.T) {
	certsDir := t.TempDir()

	sub := NewTLSSubsystem(TLSSubsystemConfig{
		CertsDir: certsDir,
		// U+007F DEL cannot appear in a URL, so the CA's identity SAN cannot
		// be formed and CA generation is the stage that fails.
		GameID: func() string { return "bad\x7fgame-id" },
	})

	err := sub.Prepare(context.Background())

	require.Error(t, err, "Prepare must fail when the real ensurer cannot generate a CA")
	errutil.AssertErrorCode(t, err, "CA_GENERATE_FAILED")
	errutil.AssertErrorContext(t, err, "certs_dir", certsDir)
	assert.Panics(t, func() { sub.TLSConfig() },
		"a failed Prepare must leave the subsystem unstarted")
}

// --- Tests relocated from cmd/holomush/core_test.go (EnsureCerts/fileExists moved here) ---

// TestEnsureCertsReportsWhichGenerationStageFailed pins a distinct coded error
// per stage of first-boot certificate generation. The stages fail for unrelated
// operator-visible reasons — an unwritable parent directory, a malformed game
// ID, an obstructed gateway certificate path — and collapsing them to one code
// would leave an operator guessing which of the three to fix. Each subtest
// asserts a code no other subtest expects, so a regression that reported the
// wrong stage fails here rather than shipping a misleading boot error.
func TestEnsureCertsReportsWhichGenerationStageFailed(t *testing.T) {
	const validGameID = "01HX7MZABC123DEF456GHJ"

	tests := []struct {
		name     string
		setup    func(t *testing.T) (certsDir, gameID string)
		wantCode string
		wantKey  string
		wantVal  any
	}{
		{
			name: "a certs directory that cannot be created reports the directory stage",
			setup: func(t *testing.T) (string, string) {
				parent := t.TempDir()
				//nolint:gosec // G302: a read-only parent is the condition under test
				require.NoError(t, os.Chmod(parent, 0o500))
				t.Cleanup(func() {
					//nolint:gosec // G302: Need 0700 to clean up directory
					_ = os.Chmod(parent, 0o700)
				})

				// Precondition: chmod must genuinely deny creation. Running as
				// root defeats it, which would make this subtest pass without
				// ever reaching the branch it claims to cover.
				if mkErr := os.Mkdir(filepath.Join(parent, "probe"), 0o700); mkErr == nil {
					t.Skip("filesystem allows creation under a 0500 parent (running as root?)")
				}
				return filepath.Join(parent, "certs"), validGameID
			},
			wantCode: "CERTS_DIR_CREATE_FAILED",
			wantKey:  "operation",
			wantVal:  "create certs directory",
		},
		{
			name: "a game ID that cannot form a SAN URI reports the CA generation stage",
			setup: func(t *testing.T) (string, string) {
				return t.TempDir(), "bad\x7fgame-id"
			},
			wantCode: "CA_GENERATE_FAILED",
			wantKey:  "game_id",
			wantVal:  "bad\x7fgame-id",
		},
		{
			name: "an obstructed gateway certificate path reports the client certificate save stage",
			setup: func(t *testing.T) (string, string) {
				certsDir := t.TempDir()
				// A directory where gateway.crt belongs. None of the three
				// files EnsureCerts probes for existence are present, so it
				// still takes the generate-from-scratch path and only trips at
				// the very last save.
				require.NoError(t, os.Mkdir(filepath.Join(certsDir, "gateway.crt"), 0o700))
				return certsDir, validGameID
			},
			wantCode: "CLIENT_CERT_SAVE_FAILED",
			wantKey:  "component",
			wantVal:  "gateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			certsDir, gameID := tt.setup(t)

			cfg, err := EnsureCerts(certsDir, gameID)

			require.Error(t, err, "EnsureCerts must fail for this stage")
			assert.Nil(t, cfg, "no TLS config may be returned alongside an error")
			errutil.AssertErrorCode(t, err, tt.wantCode)
			errutil.AssertErrorContext(t, err, tt.wantKey, tt.wantVal)
		})
	}
}

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

	// strings.Contains, not assert.Contains: assert.Contains REPORTS a failure
	// on t when it does not match, so in an || chain the first non-matching
	// alternative already fails the test even when a later one matches. The
	// either/or intent was never what the code did.
	errMsg := err.Error()
	assert.True(t,
		strings.Contains(errMsg, "certificate") || strings.Contains(errMsg, "load"),
		"Error should mention certificate/load issue, got: %v", err)
}

// TestEnsureCerts_PermissionDenied verifies that EnsureCerts returns an error
// when certificate files exist but are not readable due to permissions.
func TestEnsureCerts_PermissionDenied(t *testing.T) {
	// runtime.GOOS, not os.Getenv("GOOS"): GOOS is a build constant, not an
	// environment variable, so os.Getenv("GOOS") returns "" unconditionally and
	// this guard never fired on any platform.
	if runtime.GOOS == "windows" {
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

	// See TestEnsureCerts_CorruptedCertFile: assert.Contains inside an || chain
	// fails the test on the first non-matching alternative.
	errMsg := err.Error()
	assert.True(t,
		strings.Contains(errMsg, "permission") ||
			strings.Contains(errMsg, "denied") ||
			strings.Contains(errMsg, "certificate"),
		"Error should mention permission/denied/certificate issue, got: %v", err)
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

	errMsg := err.Error()
	assert.True(t,
		strings.Contains(errMsg, "directory") || strings.Contains(errMsg, "not a directory"),
		"Error should mention directory issue, got: %v", err)
}

// TestEnsureCerts_SaveCertificatesFailure verifies that EnsureCerts returns an
// error when certificates cannot be saved to a read-only directory.
func TestEnsureCerts_SaveCertificatesFailure(t *testing.T) {
	// runtime.GOOS, not os.Getenv("GOOS"): GOOS is a build constant, not an
	// environment variable, so os.Getenv("GOOS") returns "" unconditionally and
	// this guard never fired on any platform.
	if runtime.GOOS == "windows" {
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

	errMsg := err.Error()
	assert.True(t,
		strings.Contains(errMsg, "permission") ||
			strings.Contains(errMsg, "save") ||
			strings.Contains(errMsg, "create") ||
			strings.Contains(errMsg, "denied"),
		"Error should mention save/permission issue, got: %v", err)
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
