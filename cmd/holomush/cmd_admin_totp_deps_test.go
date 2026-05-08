// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// buildKEKProviderFromConfig env-var validation paths. The full happy
// path requires a real PG pool + valid KEK file and is exercised in
// the E2E suite at test/integration/totp/.

func TestBuildKEKProviderFromConfigRejectsMissingFileEnv(t *testing.T) {
	t.Setenv(envKEKFile, "")
	t.Setenv(envKEKPassphrase, "irrelevant")
	_, err := buildKEKProviderFromConfig(context.Background(), nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_KEK_FILE_MISSING")
}

func TestBuildKEKProviderFromConfigRejectsMissingPassphraseEnv(t *testing.T) {
	t.Setenv(envKEKFile, "/tmp/some.kek")
	t.Setenv(envKEKPassphrase, "")
	_, err := buildKEKProviderFromConfig(context.Background(), nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_KEK_PASSPHRASE_MISSING")
}

func TestBuildKEKProviderFromConfigRejectsNonExistentFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.kek")
	t.Setenv(envKEKFile, missing)
	t.Setenv(envKEKPassphrase, "any-passphrase")
	// kek.NewFileSource only validates passphraseFunc != nil, so
	// construction succeeds. The actual file read happens inside
	// NewLocalAEADProvider; surface the inner KEK_FILE_LOAD_FAILED.
	_, err := buildKEKProviderFromConfig(context.Background(), nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_FILE_LOAD_FAILED")
}

// buildAdminTOTPDeps env-var validation. The HOLOMUSH_GAME_ID check
// fires after pool construction; we use an unreachable pgx URL so
// pgxpool.New itself succeeds (lazy connect) and the env-var branch
// is exercised. DATABASE_URL must be set for getDatabaseURL to pass.

func TestBuildAdminTOTPDepsRejectsMissingGameID(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example:5432/db")
	t.Setenv(envGameID, "")

	_, _, err := buildAdminTOTPDeps(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_GAME_ID_MISSING")
}

// Sanity check: env constants resolve to the expected names so
// operators can configure the CLIs from the documented env vars.
func TestEnvVarNames(t *testing.T) {
	assert.Equal(t, "HOLOMUSH_KEK_FILE", envKEKFile)
	assert.Equal(t, "HOLOMUSH_KEK_PASSPHRASE", envKEKPassphrase)
	assert.Equal(t, "HOLOMUSH_GAME_ID", envGameID)
}
