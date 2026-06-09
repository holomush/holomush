// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek_test

import (
	"context"
	"crypto/rand"
	"errors"
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

	src, err := kek.NewFileSource(keyFile, staticPassphraseFunc("correct horse battery staple"))
	require.NoError(t, err)
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

	writeSrc, err := kek.NewFileSource(keyFile, staticPassphraseFunc("right"))
	require.NoError(t, err)
	require.NoError(t, writeSrc.Persist(context.Background(), kekBytes))

	readSrc, err := kek.NewFileSource(keyFile, staticPassphraseFunc("wrong"))
	require.NoError(t, err)
	_, err = readSrc.Load(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_PASSPHRASE_INVALID")
}

func TestFileSource_Load_FailsOnMissingFile(t *testing.T) {
	src, err := kek.NewFileSource("/nonexistent/master.key.enc", staticPassphraseFunc("any"))
	require.NoError(t, err)
	_, err = src.Load(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_FILE_LOAD_FAILED")
}

func TestFileSource_Load_AbsentFileIsDistinguishable(t *testing.T) {
	src, err := kek.NewFileSource("/nonexistent/master.key.enc", staticPassphraseFunc("x"))
	require.NoError(t, err)
	_, loadErr := src.Load(context.Background())
	require.Error(t, loadErr)
	require.True(t, errors.Is(loadErr, os.ErrNotExist),
		"absent keyfile MUST be distinguishable via errors.Is(os.ErrNotExist) through the oops wrap")
}

func TestFileSource_New_FailsWhenPassphraseFuncIsNil(t *testing.T) {
	_, err := kek.NewFileSource("/tmp/x", nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_FILE_PASSPHRASE_FUNC_NIL")
}

func TestFileSource_Load_FailsOnCorruptMagic(t *testing.T) {
	tmp := t.TempDir()
	keyFile := filepath.Join(tmp, "master.key.enc")
	// Write a file long enough to clear the size precondition (4-byte
	// magic + 16-byte salt + 24-byte nonce + 16-byte AEAD overhead = 60
	// minimum) but with a wrong magic prefix. This forces Load past the
	// length check and into the magic-comparison branch.
	bogus := make([]byte, 64)
	copy(bogus, "XXXX") // wrong magic
	require.NoError(t, os.WriteFile(keyFile, bogus, 0o600))

	src, err := kek.NewFileSource(keyFile, staticPassphraseFunc("any"))
	require.NoError(t, err)
	_, err = src.Load(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_FILE_FORMAT_INVALID")
	require.ErrorContains(t, err, "magic")
}

func TestFileSource_Name_IsLocalAEADFile(t *testing.T) {
	src, err := kek.NewFileSource("/tmp/x", staticPassphraseFunc(""))
	require.NoError(t, err)
	assert.Equal(t, "local-aead/file", src.Name())
}

// TestFileSource_Persist_FailsWhenWriteUnavailable verifies the
// KEK_FILE_WRITE_FAILED error path: pointing the FileSource at a path
// inside a non-existent directory makes os.WriteFile fail.
func TestFileSource_Persist_FailsWhenWriteUnavailable(t *testing.T) {
	keyFile := "/nonexistent-parent-dir/master.key.enc"
	src, err := kek.NewFileSource(keyFile, staticPassphraseFunc("any"))
	require.NoError(t, err)

	kekBytes := make([]byte, kek.KEKByteLength)
	_, err = rand.Read(kekBytes)
	require.NoError(t, err)

	err = src.Persist(context.Background(), kekBytes)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_FILE_WRITE_FAILED")
}

// TestFileSource_Persist_RejectsWrongLengthKEK covers the input
// validation branch that fires before any filesystem op.
func TestFileSource_Persist_RejectsWrongLengthKEK(t *testing.T) {
	tmp := t.TempDir()
	keyFile := filepath.Join(tmp, "master.key.enc")
	src, err := kek.NewFileSource(keyFile, staticPassphraseFunc("any"))
	require.NoError(t, err)

	err = src.Persist(context.Background(), make([]byte, 16)) // wrong length
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_BYTE_LENGTH_INVALID")
}

// TestFileSource_Persist_PassphraseFuncError covers the propagation
// of a PassphraseFunc error into KEK_PASSPHRASE_FETCH_FAILED.
func TestFileSource_Persist_PassphraseFuncError(t *testing.T) {
	tmp := t.TempDir()
	keyFile := filepath.Join(tmp, "master.key.enc")
	failingPF := func(_ context.Context) ([]byte, error) {
		return nil, errSentinel
	}
	src, err := kek.NewFileSource(keyFile, failingPF)
	require.NoError(t, err)

	kekBytes := make([]byte, kek.KEKByteLength)
	_, err = rand.Read(kekBytes)
	require.NoError(t, err)

	err = src.Persist(context.Background(), kekBytes)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_PASSPHRASE_FETCH_FAILED")
}

// TestFileSource_Load_PassphraseFuncError covers the symmetric
// propagation on the Load path.
func TestFileSource_Load_PassphraseFuncError(t *testing.T) {
	tmp := t.TempDir()
	keyFile := filepath.Join(tmp, "master.key.enc")

	// Write a valid file first using a working passphrase func.
	kekBytes := make([]byte, kek.KEKByteLength)
	_, err := rand.Read(kekBytes)
	require.NoError(t, err)
	writeSrc, err := kek.NewFileSource(keyFile, staticPassphraseFunc("correct"))
	require.NoError(t, err)
	require.NoError(t, writeSrc.Persist(context.Background(), kekBytes))

	// Load with a passphrase func that errors.
	failingPF := func(_ context.Context) ([]byte, error) {
		return nil, errSentinel
	}
	readSrc, err := kek.NewFileSource(keyFile, failingPF)
	require.NoError(t, err)
	_, err = readSrc.Load(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_PASSPHRASE_FETCH_FAILED")
}

// errSentinel is a fixed non-nil error for tests that need to inject
// a failure into a callback.
var errSentinel = &sentinelErr{msg: "test sentinel"}

type sentinelErr struct{ msg string }

func (e *sentinelErr) Error() string { return e.msg }
