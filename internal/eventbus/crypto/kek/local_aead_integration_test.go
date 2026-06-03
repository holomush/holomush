//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/jackc/pgx/v5"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestLocalAEADProvider_Startup_RefusesIfWrapKeyIDUnknown verifies INV-CRYPTO-19:
// startup integrity check fails if any crypto_keys row references a
// wrap_key_id the current provider cannot unwrap.
var _ = Describe("LocalAEADProvider startup (INV-CRYPTO-19)", func() {
	It("refuses startup if wrap_key_id is unknown", func() {
		ctx := context.Background()
		// Use postgres.BasicWaitStrategies() which combines the log wait
		// with wait.ForListeningPort. Bare wait.ForLog is documented as
		// flaky on Mac/Windows because Docker's port-mapping table can lag
		// the readiness log line; without the port wait, ConnectionString
		// can fail with `port "5432/tcp" not found`. See holomush-bmcq.
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

		// Insert a row with a wrap_key_id from a previous (now unknown) KEK.
		_, err = pool.Exec(ctx, `
        INSERT INTO crypto_keys
            (context_type, context_id, version, wrapped_dek, wrap_provider, wrap_key_id, participants)
        VALUES ('scene', 'orphan', 1, '\x00', 'local-aead/env', 'orphan-fingerprint', '[]')
    `)
		Expect(err).NotTo(HaveOccurred())

		// Construct a provider with a fresh KEK — its fingerprint will not
		// match 'orphan-fingerprint'.
		kekBytes := make([]byte, kek.KEKByteLength)
		_, err = rand.Read(kekBytes)
		Expect(err).NotTo(HaveOccurred())
		suiteT.Setenv("HOLOMUSH_INV33_KEK", hex.EncodeToString(kekBytes))

		src := kek.NewEnvSource("HOLOMUSH_INV33_KEK", false)
		_, err = kek.NewLocalAEADProvider(ctx, src, pool)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "KEK_PROVIDER_CANNOT_UNWRAP_EXISTING_DEKS")
	})
})
