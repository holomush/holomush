package core

import (
	"crypto/rand"
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
	return ulid.Parse(s)
}
