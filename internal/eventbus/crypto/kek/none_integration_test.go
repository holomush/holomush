//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestNoneProvider_Constructor_RefusesIfCryptoKeysNonempty verifies INV-32:
// startup with provider.name=none MUST refuse if any crypto_keys row exists.
// Enforced at constructor time (synchronous DB SELECT).
func TestNoneProvider_Constructor_RefusesIfCryptoKeysNonempty(t *testing.T) {
	ctx := context.Background()
	pgContainer, err := postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	defer pgContainer.Terminate(ctx)

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	migrator, err := store.NewMigrator(connStr)
	require.NoError(t, err)
	defer migrator.Close()
	require.NoError(t, migrator.Up())

	pool, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer pool.Close(ctx)

	// Empty table: constructor succeeds.
	provider, err := kek.NewNoneProvider(ctx, pool)
	require.NoError(t, err)
	require.NotNil(t, provider)

	// Insert a row (simulating a previously-encrypted deployment).
	_, err = pool.Exec(ctx, `
        INSERT INTO crypto_keys
            (context_type, context_id, version, wrapped_dek, wrap_provider, wrap_key_id, participants)
        VALUES ('scene', 'test-scene', 1, '\x00', 'local-aead/file', 'kek-fingerprint', '[]')
    `)
	require.NoError(t, err)

	// Non-empty table: constructor refuses.
	_, err = kek.NewNoneProvider(ctx, pool)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CRYPTO_KEYS_NONEMPTY_WITH_NONE_PROVIDER")
}
