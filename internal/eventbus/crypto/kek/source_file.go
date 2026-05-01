// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"

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

	// Write atomically. Each call gets its own random temp filename via
	// os.CreateTemp in the target directory — concurrent Persist calls
	// don't collide on a fixed `.tmp` suffix, and stale `.tmp` files
	// from a prior crash are not reused as-is. A crash between create
	// and rename leaves the original file intact.
	dir := filepath.Dir(s.path)
	base := filepath.Base(s.path)
	f, err := os.CreateTemp(dir, base+".tmp.*")
	if err != nil {
		return oops.Code("KEK_FILE_WRITE_FAILED").
			With("dir", dir).
			Wrap(err)
	}
	tmpPath := f.Name()
	committed := false
	defer func() {
		_ = f.Close() //nolint:errcheck // best-effort; commit path closes explicitly before rename
		if !committed {
			removeIgnoringError(tmpPath)
		}
	}()

	if _, err := f.Write(buf.Bytes()); err != nil {
		return oops.Code("KEK_FILE_WRITE_FAILED").
			With("path", tmpPath).
			Wrap(err)
	}
	// CreateTemp on most platforms gives 0o600 already, but Chmod
	// defensively for filesystems / umasks that widen the mode.
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return oops.Code("KEK_FILE_CHMOD_FAILED").
			With("path", tmpPath).
			Wrap(err)
	}
	if err := f.Sync(); err != nil {
		return oops.Code("KEK_FILE_FSYNC_FAILED").
			With("path", tmpPath).
			Wrap(err)
	}
	if err := f.Close(); err != nil {
		return oops.Code("KEK_FILE_CLOSE_FAILED").
			With("path", tmpPath).
			Wrap(err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return oops.Code("KEK_FILE_RENAME_FAILED").
			With("from", tmpPath).
			With("to", s.path).
			Wrap(err)
	}
	committed = true
	return nil
}

// removeIgnoringError unlinks path on a cleanup path. We discard the
// error intentionally — the caller is already returning a primary
// error from the just-failed write/chmod/fsync/rename, and the rollback
// is best-effort.
func removeIgnoringError(path string) {
	_ = os.Remove(path) //nolint:errcheck // best-effort rollback; primary error already being returned
}
