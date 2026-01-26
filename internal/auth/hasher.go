// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package auth provides authentication primitives for HoloMUSH.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/samber/oops"
	"golang.org/x/crypto/argon2"
)

// OWASP-recommended argon2id parameters.
const (
	argon2Time    = 1         // iterations
	argon2Memory  = 64 * 1024 // 64 MB
	argon2Threads = 4         // parallelism
	argon2SaltLen = 16        // salt length in bytes
	argon2KeyLen  = 32        // output length in bytes
)

// ErrEmptyPassword is returned when attempting to hash an empty password.
var ErrEmptyPassword = oops.Code("AUTH_EMPTY_PASSWORD").Errorf("password cannot be empty")

// PasswordHasher provides password hashing and verification.
type PasswordHasher interface {
	// Hash produces an argon2id hash of the password.
	Hash(password string) (string, error)

	// Verify checks if the password matches the hash.
	// Returns (true, nil) on match, (false, nil) on mismatch, or error on invalid hash.
	Verify(password, hash string) (bool, error)

	// NeedsUpgrade returns true if the hash should be upgraded to argon2id.
	NeedsUpgrade(hash string) bool
}

// Argon2idHasher implements PasswordHasher using argon2id.
type Argon2idHasher struct{}

// NewArgon2idHasher creates a new Argon2idHasher.
func NewArgon2idHasher() *Argon2idHasher {
	return &Argon2idHasher{}
}

// Hash produces an argon2id hash of the password.
func (h *Argon2idHasher) Hash(password string) (string, error) {
	if password == "" {
		return "", ErrEmptyPassword
	}

	// Generate random salt
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", oops.Code("AUTH_SALT_FAILED").Wrap(err)
	}

	// Compute hash
	hash := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

	// Encode as PHC string format
	// $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>
	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argon2Memory,
		argon2Time,
		argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)

	return encoded, nil
}

// Verify checks if the password matches the hash.
func (h *Argon2idHasher) Verify(password, encodedHash string) (bool, error) {
	// Parse the hash
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false, oops.Code("AUTH_INVALID_HASH").Errorf("invalid hash format")
	}

	if parts[1] != "argon2id" {
		return false, oops.Code("AUTH_INVALID_HASH").Errorf("unsupported hash algorithm: %s", parts[1])
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, oops.Code("AUTH_INVALID_HASH").Wrap(err)
	}

	var memory, time, threads uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, oops.Code("AUTH_INVALID_HASH").Wrap(err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, oops.Code("AUTH_INVALID_HASH").Wrap(err)
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, oops.Code("AUTH_INVALID_HASH").Wrap(err)
	}

	// Validate threads fits in uint8 to prevent silent truncation
	if threads > 255 {
		return false, oops.Code("AUTH_INVALID_HASH").Errorf("threads value %d exceeds uint8 max", threads)
	}

	// Validate key length to prevent integer overflow in uint32 conversion
	keyLen := len(expectedHash)
	if keyLen <= 0 || keyLen > 1<<30 {
		return false, oops.Code("AUTH_INVALID_HASH").Errorf("invalid hash key length: %d", keyLen)
	}

	// Compute hash with same parameters
	computedHash := argon2.IDKey([]byte(password), salt, time, memory, uint8(threads), uint32(keyLen))

	// Constant-time comparison
	if subtle.ConstantTimeCompare(computedHash, expectedHash) == 1 {
		return true, nil
	}

	return false, nil
}

// NeedsUpgrade returns true if the hash is not argon2id (e.g., bcrypt).
func (h *Argon2idHasher) NeedsUpgrade(hash string) bool {
	return !strings.HasPrefix(hash, "$argon2id$")
}
