//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"

	"github.com/holomush/holomush/internal/settings"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

var _ = Describe("GameSettings with real Postgres", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		eventStore *store.PostgresEventStore
		gs         settings.GameSettings
		container  testcontainers.Container
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)

		pgEnv, err := testutil.StartPostgres(ctx)
		Expect(err).NotTo(HaveOccurred())
		container = pgEnv.Container

		migrator, err := store.NewMigrator(pgEnv.ConnStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migrator.Up()).To(Succeed())
		_ = migrator.Close()

		eventStore, err = store.NewPostgresEventStore(ctx, pgEnv.ConnStr)
		Expect(err).NotTo(HaveOccurred())

		gs = settings.NewGameSettings(&settings.SystemInfoAdapter{
			Store:       eventStore,
			NotFoundErr: store.ErrSystemInfoNotFound,
		})
	})

	AfterEach(func() {
		if eventStore != nil {
			eventStore.Close()
		}
		cancel()
		if container != nil {
			_ = container.Terminate(context.Background())
		}
	})

	Describe("Seeded defaults", func() {
		It("reads the seeded scenes.focus.replay_tail_default value", func() {
			v, ok := gs.IntN(ctx, "scenes.focus.replay_tail_default")
			Expect(ok).To(BeTrue())
			Expect(v).To(Equal(3))
		})
	})

	Describe("Round-trip write and read", func() {
		It("persists a string value and reads it back", func() {
			err := gs.SetString(ctx, "scenes.focus.mode", "bounded")
			Expect(err).NotTo(HaveOccurred())

			v, ok := gs.StringN(ctx, "scenes.focus.mode")
			Expect(ok).To(BeTrue())
			Expect(v).To(Equal("bounded"))
		})

		It("persists an int value as string and reads as int", func() {
			err := gs.SetString(ctx, "scenes.focus.replay_tail_default", "7")
			Expect(err).NotTo(HaveOccurred())

			v, ok := gs.IntN(ctx, "scenes.focus.replay_tail_default")
			Expect(ok).To(BeTrue())
			Expect(v).To(Equal(7))
		})

		It("persists a bool value as string and reads as bool", func() {
			err := gs.SetString(ctx, "core.maintenance_mode", "true")
			Expect(err).NotTo(HaveOccurred())

			v, ok := gs.BoolN(ctx, "core.maintenance_mode")
			Expect(ok).To(BeTrue())
			Expect(v).To(BeTrue())
		})

		It("persists a duration value as string and reads as duration", func() {
			err := gs.SetString(ctx, "core.session_timeout", "5m")
			Expect(err).NotTo(HaveOccurred())

			v, ok := gs.DurationN(ctx, "core.session_timeout")
			Expect(ok).To(BeTrue())
			Expect(v).To(Equal(5 * time.Minute))
		})
	})

	Describe("Namespace validation", func() {
		It("rejects writes with unknown namespace", func() {
			err := gs.SetString(ctx, "bogus.key", "value")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown namespace"))
		})
	})

	Describe("Missing keys", func() {
		It("returns false for a key that does not exist", func() {
			_, ok := gs.StringN(ctx, "core.nonexistent")
			Expect(ok).To(BeFalse())
		})
	})
})
