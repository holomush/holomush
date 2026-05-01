// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek_test

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/pkg/errutil"
)

// staticPassphraseFunc returns a PassphraseFunc that always yields the
// given passphrase — useful for tests that need a known passphrase.
func staticPassphraseFunc(passphrase string) kek.PassphraseFunc {
	return func(_ context.Context) ([]byte, error) {
		return []byte(passphrase), nil
	}
}

func TestFileSource_LoadDerivesKEKDeterministically(t *testing.T) {
	tmp := t.TempDir()
	keyFile := filepath.Join(tmp, "master.key.enc")

	// Mint a fresh KEK and write it through FileSource.Persist.
	kekBytes := make([]byte, kek.KEKByteLength)
	_, err := rand.Read(kekBytes)
	require.NoError(t, err)

	src := kek.NewFileSource(keyFile, staticPassphraseFunc("correct horse battery staple"))
	require.NoError(t, src.Persist(context.Background(), kekBytes))

	// Round-trip: Load returns the same bytes.
	got, err := src.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, kekBytes, got)

	// Loading again returns the same bytes (idempotent).
	got2, err := src.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, kekBytes, got2)
}

func TestFileSource_Load_FailsOnWrongPassphrase(t *testing.T) {
	tmp := t.TempDir()
	keyFile := filepath.Join(tmp, "master.key.enc")
	kekBytes := make([]byte, kek.KEKByteLength)
	_, err := rand.Read(kekBytes)
	require.NoError(t, err)

	writeSrc := kek.NewFileSource(keyFile, staticPassphraseFunc("right"))
	require.NoError(t, writeSrc.Persist(context.Background(), kekBytes))

	readSrc := kek.NewFileSource(keyFile, staticPassphraseFunc("wrong"))
	_, err = readSrc.Load(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_PASSPHRASE_INVALID")
}

func TestFileSource_Load_FailsOnMissingFile(t *testing.T) {
	src := kek.NewFileSource("/nonexistent/master.key.enc", staticPassphraseFunc("any"))
	_, err := src.Load(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_FILE_LOAD_FAILED")
}

func TestFileSource_Load_FailsOnCorruptMagic(t *testing.T) {
	tmp := t.TempDir()
	keyFile := filepath.Join(tmp, "master.key.enc")
	require.NoError(t, os.WriteFile(keyFile, []byte("XXXX"), 0o600))

	src := kek.NewFileSource(keyFile, staticPassphraseFunc("any"))
	_, err := src.Load(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_FILE_FORMAT_INVALID")
}

func TestFileSource_Name_IsLocalAEADFile(t *testing.T) {
	src := kek.NewFileSource("/tmp/x", staticPassphraseFunc(""))
	assert.Equal(t, "local-aead/file", src.Name())
}
