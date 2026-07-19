// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewULIDForwarderYieldsValidStrictlyIncreasingULIDs pins the
// core.NewULID forwarder seam: the exhaustive generator tests (clamp,
// entropy, rapid-succession monotonicity) live only in internal/ulidgen, but
// this smoke test guards against a future change silently pointing the
// forwarder at a different generator.
func TestNewULIDForwarderYieldsValidStrictlyIncreasingULIDs(t *testing.T) {
	const n = 3
	var prev string
	for i := 0; i < n; i++ {
		id := NewULID()

		parsed, err := ParseULID(id.String())
		require.NoError(t, err)
		require.Equal(t, id, parsed)

		if i > 0 {
			require.True(t, prev < id.String(),
				"non-monotonic ULIDs at index %d: prev=%s cur=%s", i, prev, id.String())
		}
		prev = id.String()
	}
}
