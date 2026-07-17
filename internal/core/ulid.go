// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/ulidgen"
)

// NewULID generates a monotonic-within-millisecond ULID using crypto/rand.
// The generator's single home is internal/ulidgen; NewULID is retained as
// the canonical core-side spelling (CLAUDE.md § ULID Generation) so the
// existing call sites and the internal/eventbus -> internal/core import edge
// are unchanged (CONTEXT.md D-03 rejects inverting that dependency). This is
// a forwarder, not a second generator — there is exactly one entropy source,
// in internal/ulidgen.
func NewULID() ulid.ULID {
	return ulidgen.New()
}

// ParseULID parses a ULID string. Forwards to internal/ulidgen.Parse.
func ParseULID(s string) (ulid.ULID, error) {
	return ulidgen.Parse(s)
}
