// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"os"

	"github.com/samber/oops"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// FileSourceName is the canonical KEKSource.Name() value.
const FileSourceName = "local-aead/file"

// fileMagic identifies version 1 of the key-file format.
var fileMagic = []byte("HMK\x01")

// Argon2id parameters for passphrase derivation per master spec §5.3.
// 64 MiB memory, 3 iterations, 4-way parallelism, 32-byte output.
const (
	argonMemoryKiB uint32 = 64 * 1024
	argonTime      uint32 = 3
	argonThreads   uint8  = 4
	argonKeyLen    uint32 = 32
	saltLen               = 16
	nonceLen              = chacha20poly1305.NonceSizeX // 24 (XChaCha20)
)

// PassphraseFunc supplies the unlock passphrase. Implementations
// prompt at the CLI, read from a credential, or (in tests) return a
// fixed string.
type PassphraseFunc func(ctx context.Context) ([]byte, error)

// FileSource reads and writes a master KEK to a passphrase-encrypted
// file. File format v1:
//
//	magic   = "HMK\x01"      (4 bytes)
//	salt    = 16 bytes (Argon2id salt)
//	nonce   = 24 bytes (XChaCha20-Poly1305 nonce)
//	wrapped = N bytes (ciphertext + 16-byte AEAD tag)
//
// Argon2id derives a 32-byte unlock key from passphrase + salt; that
// key opens the AEAD-sealed wrapped KEK.
type FileSource struct {
	path           string
	passphraseFunc PassphraseFunc
}

// NewFileSource constructs a FileSource. passphraseFunc supplies the
// unlock passphrase on Load and Persist.
//
// Returns KEK_FILE_PASSPHRASE_FUNC_NIL if passphraseFunc is nil — that
// is a misconfiguration the caller cannot recover from at runtime, so
// surfacing it at construction is preferred over panicking on first
// Load/Persist.
func NewFileSource(path string, passphraseFunc PassphraseFunc) (*FileSource, error) {
	if passphraseFunc == nil {
		return nil, oops.Code("KEK_FILE_PASSPHRASE_FUNC_NIL").
			With("path", path).
			Errorf("FileSource requires a non-nil PassphraseFunc")
	}
	return &FileSource{path: path, passphraseFunc: passphraseFunc}, nil
}

// Name returns "local-aead/file".
func (s *FileSource) Name() string { return FileSourceName }

// Load reads the key file, derives the unlock key from passphrase +
// salt via Argon2id, and AEAD-decrypts the wrapped KEK.
func (s *FileSource) Load(ctx context.Context) ([]byte, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, oops.Code("KEK_FILE_LOAD_FAILED").
			With("path", s.path).
			Wrap(err)
	}
	if len(raw) < len(fileMagic)+saltLen+nonceLen+chacha20poly1305.Overhead {
		return nil, oops.Code("KEK_FILE_FORMAT_INVALID").
			With("path", s.path).
			With("size", len(raw)).
			Errorf("key file too short")
	}
	if !bytes.Equal(raw[:len(fileMagic)], fileMagic) {
		return nil, oops.Code("KEK_FILE_FORMAT_INVALID").
			With("path", s.path).
			Errorf("key file magic prefix mismatch")
	}

	offset := len(fileMagic)
	salt := raw[offset : offset+saltLen]
	offset += saltLen
	nonce := raw[offset : offset+nonceLen]
	offset += nonceLen
	wrapped := raw[offset:]

	passphrase, err := s.passphraseFunc(ctx)
	if err != nil {
		return nil, oops.Code("KEK_PASSPHRASE_FETCH_FAILED").Wrap(err)
	}

	unlockKey := argon2.IDKey(passphrase, salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	aead, err := chacha20poly1305.NewX(unlockKey)
	if err != nil {
		return nil, oops.Code("KEK_AEAD_CONSTRUCT_FAILED").Wrap(err)
	}

	kekBytes, err := aead.Open(nil, nonce, wrapped, nil)
	if err != nil {
		return nil, oops.Code("KEK_PASSPHRASE_INVALID").
			With("path", s.path).
			Errorf("AEAD open failed — wrong passphrase or corrupt file")
	}
	if len(kekBytes) != KEKByteLength {
		return nil, oops.Code("KEK_FILE_FORMAT_INVALID").
			With("path", s.path).
			With("kek_bytes", len(kekBytes)).
			Errorf("unwrapped KEK has wrong length: expected %d, got %d", KEKByteLength, len(kekBytes))
	}
	return kekBytes, nil
}

// Persist writes a fresh key file using the configured passphrase.
// Generates a new random salt + nonce on each call (rotation-safe).
func (s *FileSource) Persist(ctx context.Context, kekBytes []byte) error {
	if len(kekBytes) != KEKByteLength {
		return oops.Code("KEK_BYTE_LENGTH_INVALID").
			With("expected", KEKByteLength).
			With("got", len(kekBytes)).
			Errorf("KEK must be %d bytes; got %d", KEKByteLength, len(kekBytes))
	}
	passphrase, err := s.passphraseFunc(ctx)
	if err != nil {
		return oops.Code("KEK_PASSPHRASE_FETCH_FAILED").Wrap(err)
	}

	salt := make([]byte, saltLen)
	if _, rngErr := io.ReadFull(rand.Reader, salt); rngErr != nil {
		return oops.Code("KEK_FILE_RNG_FAILED").Wrap(rngErr)
	}
	nonce := make([]byte, nonceLen)
	if _, rngErr := io.ReadFull(rand.Reader, nonce); rngErr != nil {
		return oops.Code("KEK_FILE_RNG_FAILED").Wrap(rngErr)
	}

	unlockKey := argon2.IDKey(passphrase, salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	aead, err := chacha20poly1305.NewX(unlockKey)
	if err != nil {
		return oops.Code("KEK_AEAD_CONSTRUCT_FAILED").Wrap(err)
	}
	wrapped := aead.Seal(nil, nonce, kekBytes, nil)

	var buf bytes.Buffer
	buf.Write(fileMagic)
	buf.Write(salt)
	buf.Write(nonce)
	buf.Write(wrapped)

	// Write atomically: write to a temp file in the same directory,
	// fsync, set 0o600 explicitly (in case the file already existed
	// with broader permissions), then rename. A crash between WriteFile
	// and Rename leaves the original file intact.
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0o600); err != nil {
		return oops.Code("KEK_FILE_WRITE_FAILED").
			With("path", tmpPath).
			Wrap(err)
	}
	// Defensive chmod in case umask widened mode on a pre-existing
	// .tmp inode (rare but cheap to defend against).
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		removeIgnoringError(tmpPath)
		return oops.Code("KEK_FILE_CHMOD_FAILED").
			With("path", tmpPath).
			Wrap(err)
	}
	// fsync the temp file so its bytes hit disk before the rename
	// commits the swap.
	if err := fsyncFile(tmpPath); err != nil {
		removeIgnoringError(tmpPath)
		return oops.Code("KEK_FILE_FSYNC_FAILED").
			With("path", tmpPath).
			Wrap(err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		removeIgnoringError(tmpPath)
		return oops.Code("KEK_FILE_RENAME_FAILED").
			With("from", tmpPath).
			With("to", s.path).
			Wrap(err)
	}
	return nil
}

// fsyncFile opens path, fsyncs it, and closes. Best-effort; the
// rename below provides the durability guarantee on most filesystems
// even if fsync is a no-op (e.g., overlayfs in some test environments).
func fsyncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return oops.Wrap(err)
	}
	defer func() {
		_ = f.Close() //nolint:errcheck // best-effort cleanup; the rename below is the durability point
	}()
	if err := f.Sync(); err != nil {
		return oops.Wrap(err)
	}
	return nil
}

// removeIgnoringError unlinks path on a cleanup path. We discard the
// error intentionally — the caller is already returning a primary
// error from the just-failed write/chmod/fsync/rename, and the rollback
// is best-effort. The deferred-discard is wrapped in a helper so
// errcheck doesn't flag every call site individually.
func removeIgnoringError(path string) {
	_ = os.Remove(path) //nolint:errcheck // best-effort rollback; primary error already being returned
}
