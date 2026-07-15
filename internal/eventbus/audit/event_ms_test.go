// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

// TestEventMsFromULIDRendersEmbeddedMsToUnixNano proves the shared helper
// returns the ULID's embedded millisecond rendered to UnixNano — deterministic
// and independent of any store-time.
func TestEventMsFromULIDRendersEmbeddedMsToUnixNano(t *testing.T) {
	const ms = uint64(1_700_000_000_000) // fixed, known embedded-ms

	var id ulid.ULID
	require.NoError(t, id.SetTime(ms))

	got := eventMsFromULID(id)

	require.Equal(t, ulid.Time(ms).UnixNano(), got, "must equal ulid.Time(id.Time()).UnixNano()")
	require.Equal(t, int64(ms)*int64(time.Millisecond), got, "must equal ms rendered to ns")
}
