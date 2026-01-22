// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
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
		return ulid.ULID{}, oops.With("ulid", s).Wrap(err)
	}
	return id, nil
}
