// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestSeedULIDIsValid(t *testing.T) {
	// The well-known seed ULID used for idempotency
	// Must be exactly 26 characters using Crockford's base32 alphabet
	seedULID := "01HZN3XS000000000000000000"

	require.Len(t, seedULID, 26, "seed ULID must be exactly 26 characters")

	id, err := ulid.Parse(seedULID)
	require.NoError(t, err, "seed ULID should be valid")
	require.NotEqual(t, ulid.ULID{}, id, "parsed ULID should not be zero")
}
