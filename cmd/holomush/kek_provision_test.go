// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/holomush/holomush/pkg/errutil"
	"github.com/stretchr/testify/require"
)

func TestResolvePassphrase(t *testing.T) {
	t.Run("env var wins", func(t *testing.T) {
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE", "from-env")
		got, err := resolvePassphrase(passphraseSources{interactive: false})
		require.NoError(t, err)
		require.Equal(t, "from-env", string(got))
	})
	t.Run("file ref read and trimmed", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "pass")
		require.NoError(t, os.WriteFile(f, []byte("from-file\n"), 0o600))
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE", "")
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE_FILE", f)
		got, err := resolvePassphrase(passphraseSources{interactive: false})
		require.NoError(t, err)
		require.Equal(t, "from-file", string(got)) // trailing newline trimmed
	})
	t.Run("none and non-interactive errors", func(t *testing.T) {
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE", "")
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE_FILE", "")
		_, err := resolvePassphrase(passphraseSources{interactive: false})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "KEK_PASSPHRASE_UNAVAILABLE")
	})
	t.Run("missing file ref errors (not silently empty)", func(t *testing.T) {
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE", "")
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE_FILE", "/nonexistent/pass")
		_, err := resolvePassphrase(passphraseSources{interactive: false})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "KEK_PASSPHRASE_FILE_READ_FAILED")
	})
}

func TestEnsureKeyfile(t *testing.T) {
	t.Run("absent + autoGen mints and persists, reused on second call", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "master.key.enc")
		pf := func(_ context.Context) ([]byte, error) { return []byte("pw"), nil }
		require.NoError(t, ensureKeyfile(context.Background(), path, pf, true))
		info1, err := os.Stat(path)
		require.NoError(t, err)
		require.NoError(t, ensureKeyfile(context.Background(), path, pf, true)) // idempotent
		info2, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, info1.ModTime(), info2.ModTime(), "MUST NOT regenerate an existing keyfile")
	})
	t.Run("absent + NOT autoGen refuses", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "master.key.enc")
		err := ensureKeyfile(context.Background(), path, func(_ context.Context) ([]byte, error) { return []byte("pw"), nil }, false)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "KEK_FILE_NOT_FOUND")
	})
	t.Run("wrong passphrase surfaces error, never regenerates", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "master.key.enc")
		right := func(_ context.Context) ([]byte, error) { return []byte("right"), nil }
		wrong := func(_ context.Context) ([]byte, error) { return []byte("wrong"), nil }
		require.NoError(t, ensureKeyfile(context.Background(), path, right, true))
		info1, err := os.Stat(path)
		require.NoError(t, err)
		err = ensureKeyfile(context.Background(), path, wrong, true)
		require.Error(t, err, "a keyfile that fails to unseal MUST surface, not be regenerated")
		info2, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, info1.ModTime(), info2.ModTime(), "keyfile MUST be untouched after a failed unseal")
	})
}

func TestProvisionBootKEKProviderBootMatrix(t *testing.T) {
	t.Run("no passphrase, non-interactive ⇒ KEK_PASSPHRASE_UNAVAILABLE", func(t *testing.T) {
		t.Setenv("HOLOMUSH_KEK_FILE", filepath.Join(t.TempDir(), "m.key.enc"))
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE", "")
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE_FILE", "")
		_, err := provisionBootKEKProvider(context.Background(), nil, true)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "KEK_PASSPHRASE_UNAVAILABLE")
	})
	t.Run("passphrase set, keyfile absent, autoGen off ⇒ KEK_FILE_NOT_FOUND", func(t *testing.T) {
		t.Setenv("HOLOMUSH_KEK_FILE", filepath.Join(t.TempDir(), "m.key.enc"))
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE", "pw")
		_, err := provisionBootKEKProvider(context.Background(), nil, false)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "KEK_FILE_NOT_FOUND")
	})
	t.Run("no KEK file path ⇒ BOOT_KEK_FILE_MISSING", func(t *testing.T) {
		t.Setenv("HOLOMUSH_KEK_FILE", "")
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE", "pw")
		_, err := provisionBootKEKProvider(context.Background(), nil, true)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "BOOT_KEK_FILE_MISSING")
	})
}
