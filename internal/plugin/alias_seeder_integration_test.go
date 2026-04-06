// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugins_test

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/command"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

var _ = Describe("Plugin Alias Seeding Integration", func() {
	var (
		pgEnv   *testutil.PostgresEnv
		pool    *pgxpool.Pool
		repo    *store.PostgresAliasRepository
		cache   *command.AliasCache
		cleanup func()
	)

	BeforeEach(func() {
		ctx := context.Background()
		var err error
		pgEnv, err = testutil.StartPostgres(ctx)
		Expect(err).NotTo(HaveOccurred())

		migrator, err := store.NewMigrator(pgEnv.ConnStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migrator.Up()).To(Succeed())
		_ = migrator.Close()

		pool, err = pgxpool.New(ctx, pgEnv.ConnStr)
		Expect(err).NotTo(HaveOccurred())

		repo = store.NewPostgresAliasRepository(pool)
		cache = command.NewAliasCache()

		cleanup = func() {
			pool.Close()
			_ = pgEnv.Terminate(ctx)
		}
	})

	AfterEach(func() {
		cleanup()
	})

	Describe("Full startup with manifest aliases", func() {
		It("seeds all declared aliases to the database", func() {
			ctx := context.Background()

			pluginsDir, err := findPluginsDir()
			Expect(err).NotTo(HaveOccurred())

			luaHost := pluginlua.NewHost()
			defer func() { _ = luaHost.Close(ctx) }()

			mgr := plugins.NewManager(pluginsDir,
				plugins.WithLuaHost(luaHost),
				plugins.WithAliasSeeder(repo, cache),
			)
			Expect(mgr.LoadAll(ctx)).To(Succeed())

			aliases, err := repo.GetSystemAliases(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(aliases).To(HaveKeyWithValue(`"`, "say"))
			Expect(aliases).To(HaveKeyWithValue(":", "pose"))
			Expect(aliases).To(HaveKeyWithValue(";", "pose"))
			Expect(aliases).To(HaveKeyWithValue("p", "page"))
			Expect(aliases).To(HaveKeyWithValue("w", "whisper"))
			Expect(aliases).To(HaveKeyWithValue("desc", "describe"))

			cached := cache.ListSystemAliases()
			Expect(cached).To(HaveKeyWithValue(`"`, "say"))
			Expect(cached).To(HaveKeyWithValue("desc", "describe"))
		})
	})

	Describe("Operator override via sysalias", func() {
		It("allows operator to change an alias command and keeps it on restart", func() {
			ctx := context.Background()

			pluginsDir, err := findPluginsDir()
			Expect(err).NotTo(HaveOccurred())

			luaHost1 := pluginlua.NewHost()
			mgr1 := plugins.NewManager(pluginsDir,
				plugins.WithLuaHost(luaHost1),
				plugins.WithAliasSeeder(repo, cache),
			)
			Expect(mgr1.LoadAll(ctx)).To(Succeed())
			_ = luaHost1.Close(ctx)

			// Use the bootstrap test player from the baseline migration as the operator.
			Expect(repo.SetSystemAlias(ctx, `"`, "shout", "01KDVDNA00041061050R3GG28A")).To(Succeed())

			cache2 := command.NewAliasCache()
			luaHost2 := pluginlua.NewHost()
			mgr2 := plugins.NewManager(pluginsDir,
				plugins.WithLuaHost(luaHost2),
				plugins.WithAliasSeeder(repo, cache2),
			)
			Expect(mgr2.LoadAll(ctx)).To(Succeed())
			_ = luaHost2.Close(ctx)

			aliases, err := repo.GetSystemAliases(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(aliases).To(HaveKeyWithValue(`"`, "shout"))
		})
	})

	Describe("Idempotent seeding", func() {
		It("does not error when loaded twice against same database", func() {
			ctx := context.Background()

			pluginsDir, err := findPluginsDir()
			Expect(err).NotTo(HaveOccurred())

			luaHost1 := pluginlua.NewHost()
			mgr1 := plugins.NewManager(pluginsDir,
				plugins.WithLuaHost(luaHost1),
				plugins.WithAliasSeeder(repo, cache),
			)
			Expect(mgr1.LoadAll(ctx)).To(Succeed())
			_ = luaHost1.Close(ctx)

			cache2 := command.NewAliasCache()
			luaHost2 := pluginlua.NewHost()
			mgr2 := plugins.NewManager(pluginsDir,
				plugins.WithLuaHost(luaHost2),
				plugins.WithAliasSeeder(repo, cache2),
			)
			Expect(mgr2.LoadAll(ctx)).To(Succeed())
			_ = luaHost2.Close(ctx)

			aliases, err := repo.GetSystemAliases(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(aliases).To(HaveKeyWithValue(`"`, "say"))
			Expect(aliases).To(HaveKeyWithValue("desc", "describe"))
		})
	})
})
