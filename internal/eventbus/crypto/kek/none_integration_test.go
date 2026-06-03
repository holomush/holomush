//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek_test

import (
	"context"

	"github.com/jackc/pgx/v5"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestNoneProvider_Constructor_RefusesIfCryptoKeysNonempty verifies INV-CRYPTO-18:
// startup with provider.name=none MUST refuse if any crypto_keys row exists.
// Enforced at constructor time (synchronous DB SELECT).
var _ = Describe("NoneProvider startup (INV-CRYPTO-18)", func() {
	It("refuses startup if crypto_keys table is non-empty", func() {
		ctx := context.Background()
		pgContainer, err := postgres.Run(
			ctx,
			"postgres:18-alpine",
			postgres.WithDatabase("test"),
			postgres.WithUsername("test"),
			postgres.WithPassword("test"),
			postgres.BasicWaitStrategies(),
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = pgContainer.Terminate(ctx) })

		connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
		Expect(err).NotTo(HaveOccurred())

		migrator, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(migrator.Close)
		Expect(migrator.Up()).To(Succeed())

		pool, err := pgx.Connect(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = pool.Close(ctx) })

		// Empty table: constructor succeeds.
		provider, err := kek.NewNoneProvider(ctx, pool)
		Expect(err).NotTo(HaveOccurred())
		Expect(provider).NotTo(BeNil())

		// Insert a row (simulating a previously-encrypted deployment).
		_, err = pool.Exec(ctx, `
        INSERT INTO crypto_keys
            (context_type, context_id, version, wrapped_dek, wrap_provider, wrap_key_id, participants)
        VALUES ('scene', 'test-scene', 1, '\x00', 'local-aead/file', 'kek-fingerprint', '[]')
    `)
		Expect(err).NotTo(HaveOccurred())

		// Non-empty table: constructor refuses.
		_, err = kek.NewNoneProvider(ctx, pool)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "CRYPTO_KEYS_NONEMPTY_WITH_NONE_PROVIDER")
	})
})
