//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cryptowiring_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/plugin/cryptowiring"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

func TestCryptoKeysLookupExistsReportsAbsentDEKAsFalse(t *testing.T) {
	ctx := context.Background()
	connStr := testutil.FreshDatabase(t, testutil.SharedPostgres(t))
	es, err := store.NewPostgresEventStore(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(es.Close)

	lookup := cryptowiring.CryptoKeysLookup(es.Pool())
	exists, err := lookup.Exists(ctx, 999999)
	require.NoError(t, err)
	assert.False(t, exists, "absent DEK id must read as Exists=false, not error")
}
