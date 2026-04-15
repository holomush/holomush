// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/command"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

// findRepoPluginsDir locates the repo's plugins/ directory by walking up from
// this test file's path. Works regardless of CWD when tests run.
func findRepoPluginsDir() (string, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	// test/integration/plugin/alias_seeder_test.go → repo root → plugins
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	pluginsDir := filepath.Join(repoRoot, "plugins")
	if _, err := os.Stat(pluginsDir); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(pluginsDir)
	if err != nil {
		return "", err
	}
	return abs, nil
}

var _ = Describe("Plugin Alias Seeding Integration", func() {
	var (
		pool  *pgxpool.Pool
		repo  *store.PostgresAliasRepository
		cache *command.AliasCache
	)

	BeforeEach(func() {
		ctx := context.Background()
		connStr := testutil.FreshDatabase(suiteT, sharedPG)

		var err error
		pool, err = pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())

		repo = store.NewPostgresAliasRepository(pool)
		cache = command.NewAliasCache()
	})

	AfterEach(func() {
		if pool != nil {
			pool.Close()
		}
	})

	Describe("Full startup with manifest aliases", func() {
		It("seeds all declared aliases to the database with correct source provenance", func() {
			ctx := context.Background()

			pluginsDir, err := findRepoPluginsDir()
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

			// Query by column name (not positional) to catch param transposition.
			type row struct {
				alias, cmd, source string
				createdBy          *string
			}
			rows, err := pool.Query(ctx, `
				SELECT alias, command, created_by, source
				  FROM system_aliases
				 WHERE alias IN ('"', ':', ';', 'p', 'w', 'desc')
				 ORDER BY alias`)
			Expect(err).NotTo(HaveOccurred())
			defer rows.Close()

			var got []row
			for rows.Next() {
				var r row
				Expect(rows.Scan(&r.alias, &r.cmd, &r.createdBy, &r.source)).To(Succeed())
				got = append(got, r)
			}
			Expect(rows.Err()).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(6))
			for _, r := range got {
				Expect(r.createdBy).To(BeNil(),
					"manifest-seeded alias %q must have NULL created_by", r.alias)
				Expect(r.source).To(SatisfyAny(
					Equal("core-communication"),
					Equal("core-objects"),
				), "source must be a plugin name, not a player ID (alias=%q)", r.alias)
			}
		})
	})

	Describe("Operator override via sysalias", func() {
		It("preserves operator override across restart in DB and source column", func() {
			ctx := context.Background()

			pluginsDir, err := findRepoPluginsDir()
			Expect(err).NotTo(HaveOccurred())

			// First boot: seed from manifests.
			luaHost1 := pluginlua.NewHost()
			mgr1 := plugins.NewManager(pluginsDir,
				plugins.WithLuaHost(luaHost1),
				plugins.WithAliasSeeder(repo, cache),
			)
			Expect(mgr1.LoadAll(ctx)).To(Succeed())
			_ = luaHost1.Close(ctx)

			// Operator override. Uses the admin player seeded by the baseline migration.
			const operator = "01KDVDNA00041061050R3GG28A"
			Expect(repo.SetSystemAlias(ctx, `"`, "shout", operator, "sysalias")).To(Succeed())

			// Second boot: simulate restart.
			cache2 := command.NewAliasCache()
			luaHost2 := pluginlua.NewHost()
			mgr2 := plugins.NewManager(pluginsDir,
				plugins.WithLuaHost(luaHost2),
				plugins.WithAliasSeeder(repo, cache2),
			)
			Expect(mgr2.LoadAll(ctx)).To(Succeed())
			_ = luaHost2.Close(ctx)

			// Verify DB row reflects override: command, created_by, source all preserved.
			var cmd, source string
			var createdBy *string
			err = pool.QueryRow(ctx,
				`SELECT command, created_by, source FROM system_aliases WHERE alias = $1`, `"`,
			).Scan(&cmd, &createdBy, &source)
			Expect(err).NotTo(HaveOccurred())
			Expect(cmd).To(Equal("shout"))
			Expect(createdBy).NotTo(BeNil())
			Expect(*createdBy).To(Equal(operator))
			Expect(source).To(Equal("sysalias"))
		})
	})

	Describe("Idempotent seeding", func() {
		It("does not error when loaded twice against same database", func() {
			ctx := context.Background()

			pluginsDir, err := findRepoPluginsDir()
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
