// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package idgen provides crypto/rand-backed ULID generation.
//
// All production code MUST use idgen.New() instead of ulid.Make(), which uses
// math/rand internally. Test code may continue using ulid.Make() since
// cryptographic strength is not required for test identifiers.
package idgen

import (
	cryptorand "crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

// New generates a new ULID using crypto/rand for entropy.
// Panics if the system's cryptographic random source is unavailable,
// which indicates an unrecoverable OS-level failure.
func New() ulid.ULID {
	id, err := ulid.New(ulid.Timestamp(time.Now()), cryptorand.Reader)
	if err != nil {
		panic("id: crypto/rand unavailable: " + err.Error())
	}
	return id
}
