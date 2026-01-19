// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	entropy     = ulid.Monotonic(rand.Reader, 0)
	entropyLock sync.Mutex
)

// NewULID generates a new ULID.
func NewULID() ulid.ULID {
	entropyLock.Lock()
	defer entropyLock.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), entropy)
}

// ParseULID parses a ULID string.
func ParseULID(s string) (ulid.ULID, error) {
	id, err := ulid.Parse(s)
	if err != nil {
		return ulid.ULID{}, fmt.Errorf("invalid ULID %q: %w", s, err)
	}
	return id, nil
}
