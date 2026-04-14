// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package content_test

import (
	"context"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/bootstrap"
	"github.com/holomush/holomush/internal/content"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/test/testutil"
)

// testEnv holds shared resources for the content integration suite.
type testEnv struct {
	ctx  context.Context
	pool *pgxpool.Pool
}

var env *testEnv

var _ = BeforeSuite(func() {
	ctx := context.Background()

	shared := testutil.SharedPostgres(suiteT)
	connStr := testutil.FreshDatabase(suiteT, shared)

	pool, err := pgxpool.New(ctx, connStr)
	Expect(err).NotTo(HaveOccurred())

	env = &testEnv{
		ctx:  ctx,
		pool: pool,
	}
})

var _ = AfterSuite(func() {
	if env == nil {
		return
	}
	if env.pool != nil {
		env.pool.Close()
	}
})

// cleanupContent removes all rows from content-related tables between tests.
func cleanupContent(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, "DELETE FROM content_items")
	_, _ = pool.Exec(ctx, "DELETE FROM bootstrap_metadata")
}

// loadCrossroadsManifest reads and parses the crossroads plugin.yaml.
func loadCrossroadsManifest() (*plugins.Manifest, error) {
	data, err := os.ReadFile("../../../plugins/setting-crossroads/plugin.yaml")
	if err != nil {
		return nil, err
	}
	return plugins.ParseManifest(data)
}

var _ = Describe("PostgresStore", func() {
	var (
		ctx context.Context
		cs  *content.PostgresStore
	)

	BeforeEach(func() {
		ctx = context.Background()
		cleanupContent(ctx, env.pool)
		cs = content.NewPostgresStore(env.pool)
	})

	It("stores and retrieves content items", func() {
		item := &content.Item{
			Key:         "landing.hero",
			ContentType: "text/markdown",
			Body:        []byte("# Welcome\n\nHello world."),
			Metadata:    map[string]string{"title": "Hero", "order": "1"},
		}

		Expect(cs.Put(ctx, item)).To(Succeed())

		got, err := cs.Get(ctx, "landing.hero")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
		Expect(got.Key).To(Equal("landing.hero"))
		Expect(got.ContentType).To(Equal("text/markdown"))
		Expect(got.Body).To(Equal(item.Body))
		Expect(got.Metadata).To(Equal(item.Metadata))
		Expect(got.UpdatedAt).NotTo(BeZero())
	})

	It("lists items by prefix", func() {
		items := []*content.Item{
			{Key: "landing.hero", ContentType: "text/markdown", Body: []byte("hero"), Metadata: map[string]string{}},
			{Key: "landing.pitch", ContentType: "text/markdown", Body: []byte("pitch"), Metadata: map[string]string{}},
			{Key: "landing.features.1", ContentType: "text/markdown", Body: []byte("feature"), Metadata: map[string]string{}},
			{Key: "theme.colors", ContentType: "application/json", Body: []byte(`{}`), Metadata: map[string]string{}},
		}
		for _, it := range items {
			Expect(cs.Put(ctx, it)).To(Succeed())
		}

		result, err := cs.List(ctx, "landing.", content.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Items).To(HaveLen(3))

		keys := make([]string, len(result.Items))
		for i, it := range result.Items {
			keys[i] = it.Key
		}
		Expect(keys).To(ContainElements("landing.hero", "landing.pitch", "landing.features.1"))
		Expect(keys).NotTo(ContainElement("theme.colors"))
	})

	It("paginates results", func() {
		for i := 1; i <= 5; i++ {
			key := "page.item." + string(rune('0'+i))
			Expect(cs.Put(ctx, &content.Item{
				Key:         key,
				ContentType: "text/plain",
				Body:        []byte("content"),
				Metadata:    map[string]string{},
			})).To(Succeed())
		}

		first, err := cs.List(ctx, "page.", content.ListOptions{Limit: 2})
		Expect(err).NotTo(HaveOccurred())
		Expect(first.Items).To(HaveLen(2))
		Expect(first.NextCursor).NotTo(BeEmpty())

		second, err := cs.List(ctx, "page.", content.ListOptions{Limit: 2, Cursor: first.NextCursor})
		Expect(err).NotTo(HaveOccurred())
		Expect(second.Items).To(HaveLen(2))

		// Verify pages are disjoint.
		firstKeys := make(map[string]bool, len(first.Items))
		for _, it := range first.Items {
			firstKeys[it.Key] = true
		}
		for _, it := range second.Items {
			Expect(firstKeys).NotTo(HaveKey(it.Key))
		}
	})

	It("supports upsert", func() {
		item := &content.Item{
			Key:         "upsert.test",
			ContentType: "text/plain",
			Body:        []byte("original body"),
			Metadata:    map[string]string{},
		}
		Expect(cs.Put(ctx, item)).To(Succeed())

		item.Body = []byte("updated body")
		Expect(cs.Put(ctx, item)).To(Succeed())

		got, err := cs.Get(ctx, "upsert.test")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Body).To(Equal([]byte("updated body")))
	})

	It("populates search_vector for text content", func() {
		item := &content.Item{
			Key:         "search.test",
			ContentType: "text/markdown",
			Body:        []byte("dragonfire adventure quest"),
			Metadata:    map[string]string{},
		}
		Expect(cs.Put(ctx, item)).To(Succeed())

		// Verify search_vector is populated by running a ts_query against it.
		var count int
		err := env.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM content_items
			  WHERE key = $1
			    AND search_vector @@ to_tsquery('english', 'dragonfire')`,
			"search.test",
		).Scan(&count)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(1))
	})
})

var _ = Describe("SettingBootstrapper", func() {
	const crossroadsDir = "../../../plugins/setting-crossroads"

	var (
		ctx          context.Context
		cs           *content.PostgresStore
		metaStore    *bootstrap.PostgresMetadataStore
		bootstrapper *bootstrap.SettingBootstrapper
		manifest     *plugins.Manifest
	)

	BeforeEach(func() {
		ctx = context.Background()
		cleanupContent(ctx, env.pool)

		cs = content.NewPostgresStore(env.pool)
		metaStore = bootstrap.NewPostgresMetadataStore(env.pool)

		var err error
		manifest, err = loadCrossroadsManifest()
		Expect(err).NotTo(HaveOccurred())

		bootstrapper = bootstrap.NewSettingBootstrapper(bootstrap.SettingBootstrapperOpts{
			ContentStore:  cs,
			WorldService:  nil, // world seeding is skipped when nil
			MetadataStore: metaStore,
			SettingName:   "crossroads",
		})
	})

	It("seeds content from crossroads plugin", func() {
		Expect(bootstrapper.Bootstrap(ctx, manifest, crossroadsDir)).To(Succeed())

		result, err := cs.List(ctx, "landing.", content.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		keys := make([]string, len(result.Items))
		for i, it := range result.Items {
			keys[i] = it.Key
		}
		Expect(keys).To(ContainElement("landing.hero"))
		Expect(keys).To(ContainElement("landing.pitch"))

		// At least one landing.features.* entry must be present.
		hasFeature := false
		for _, k := range keys {
			if strings.HasPrefix(k, "landing.features.") {
				hasFeature = true
				break
			}
		}
		Expect(hasFeature).To(BeTrue(), "expected at least one landing.features.* item")
	})

	It("is idempotent — operator edits survive re-bootstrap", func() {
		// First bootstrap seeds default content.
		Expect(bootstrapper.Bootstrap(ctx, manifest, crossroadsDir)).To(Succeed())

		// Operator overrides landing.hero with a custom body.
		custom := []byte("# Custom Hero\n\nOperator-managed content.")
		Expect(cs.Put(ctx, &content.Item{
			Key:         "landing.hero",
			ContentType: "text/markdown",
			Body:        custom,
			Metadata:    map[string]string{},
		})).To(Succeed())

		// Re-run bootstrap WITHOUT reset — normal subsequent boot.
		// seedContent skips existing keys, so the operator edit must survive.
		rebootBootstrapper := bootstrap.NewSettingBootstrapper(bootstrap.SettingBootstrapperOpts{
			ContentStore:  cs,
			WorldService:  nil,
			MetadataStore: metaStore,
			SettingName:   "crossroads",
			ResetSetting:  false,
		})
		Expect(rebootBootstrapper.Bootstrap(ctx, manifest, crossroadsDir)).To(Succeed())

		got, err := cs.Get(ctx, "landing.hero")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
		Expect(got.Body).To(Equal(custom), "operator edit must survive re-bootstrap")
	})

	It("records active setting in metadata", func() {
		Expect(bootstrapper.Bootstrap(ctx, manifest, crossroadsDir)).To(Succeed())

		value, found, err := metaStore.Get(ctx, "active_setting")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(value).To(Equal("crossroads"))
	})
})
