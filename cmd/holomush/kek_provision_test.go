// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
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
