// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
	istore "github.com/holomush/holomush/internal/store"
)

func TestPolicyStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Policy Store Integration Suite")
}

var (
	pool      *pgxpool.Pool
	container *postgres.PostgresContainer
	ps        *store.PostgresStore
)

var _ = BeforeSuite(func() {
	ctx := context.Background()

	var err error
	container, err = postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("holomush_test"),
		postgres.WithUsername("holomush"),
		postgres.WithPassword("holomush"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	Expect(err).NotTo(HaveOccurred())

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	Expect(err).NotTo(HaveOccurred())

	migrator, err := istore.NewMigrator(connStr)
	Expect(err).NotTo(HaveOccurred())
	Expect(migrator.Up()).To(Succeed())
	_ = migrator.Close()

	pool, err = pgxpool.New(ctx, connStr)
	Expect(err).NotTo(HaveOccurred())

	ps = store.NewPostgresStore(pool)
})

var _ = AfterSuite(func() {
	if pool != nil {
		pool.Close()
	}
	if container != nil {
		_ = container.Terminate(context.Background())
	}
})

func cleanupPolicies(ctx context.Context) {
	_, _ = pool.Exec(ctx, "DELETE FROM access_policy_versions")
	_, _ = pool.Exec(ctx, "DELETE FROM access_policies")
}

func sampleAST() json.RawMessage {
	return json.RawMessage(`{"type":"policy","effect":"permit"}`)
}

var _ = Describe("PostgresStore", func() {
	BeforeEach(func() {
		cleanupPolicies(context.Background())
	})

	Describe("Create", func() {
		It("inserts a policy and assigns an ID", func() {
			ctx := context.Background()
			p := &store.StoredPolicy{
				Name:        "allow-say",
				Description: "Allow say command",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     `permit(principal, action == "say", resource);`,
				CompiledAST: sampleAST(),
				Enabled:     true,
				CreatedBy:   "system",
			}

			Expect(ps.Create(ctx, p)).To(Succeed())
			Expect(p.ID).NotTo(BeEmpty())
			Expect(p.Version).To(Equal(1))
		})

		It("enforces unique name constraint", func() {
			ctx := context.Background()
			p1 := &store.StoredPolicy{
				Name:        "unique-policy",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleAST(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, p1)).To(Succeed())

			p2 := &store.StoredPolicy{
				Name:        "unique-policy",
				Effect:      types.PolicyEffectForbid,
				Source:      "admin",
				DSLText:     "forbid(principal, action, resource);",
				CompiledAST: sampleAST(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, p2)).To(HaveOccurred())
		})

		It("validates source naming (ADR 35)", func() {
			ctx := context.Background()
			p := &store.StoredPolicy{
				Name:        "seed:default",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleAST(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			err := ps.Create(ctx, p)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("POLICY_SOURCE_MISMATCH"))
		})

		It("accepts seed-prefixed names with seed source", func() {
			ctx := context.Background()
			p := &store.StoredPolicy{
				Name:        "seed:default-allow",
				Effect:      types.PolicyEffectPermit,
				Source:      "seed",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleAST(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, p)).To(Succeed())
		})
	})

	Describe("Get", func() {
		It("retrieves a policy by name", func() {
			ctx := context.Background()
			created := &store.StoredPolicy{
				Name:        "get-test",
				Description: "Test get",
				Effect:      types.PolicyEffectForbid,
				Source:      "admin",
				DSLText:     `forbid(principal, action == "delete", resource);`,
				CompiledAST: sampleAST(),
				Enabled:     false,
				CreatedBy:   "admin-user",
			}
			Expect(ps.Create(ctx, created)).To(Succeed())

			got, err := ps.Get(ctx, "get-test")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(created.ID))
			Expect(got.Name).To(Equal("get-test"))
			Expect(got.Description).To(Equal("Test get"))
			Expect(got.Effect).To(Equal(types.PolicyEffectForbid))
			Expect(got.Source).To(Equal("admin"))
			Expect(got.Enabled).To(BeFalse())
			Expect(got.CreatedBy).To(Equal("admin-user"))
			Expect(got.Version).To(Equal(1))
			Expect(got.CreatedAt).NotTo(BeZero())
			Expect(got.UpdatedAt).NotTo(BeZero())
		})

		It("returns POLICY_NOT_FOUND for missing name", func() {
			ctx := context.Background()
			_, err := ps.Get(ctx, "nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("POLICY_NOT_FOUND"))
		})
	})

	Describe("GetByID", func() {
		It("retrieves a policy by ID", func() {
			ctx := context.Background()
			created := &store.StoredPolicy{
				Name:        "getbyid-test",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleAST(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, created)).To(Succeed())

			got, err := ps.GetByID(ctx, created.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Name).To(Equal("getbyid-test"))
		})

		It("returns POLICY_NOT_FOUND for missing ID", func() {
			ctx := context.Background()
			_, err := ps.GetByID(ctx, "01NONEXISTENT")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("POLICY_NOT_FOUND"))
		})
	})

	Describe("Update", func() {
		It("increments version and creates version history", func() {
			ctx := context.Background()
			p := &store.StoredPolicy{
				Name:        "update-test",
				Description: "Original",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleAST(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, p)).To(Succeed())
			originalID := p.ID

			p.Description = "Updated"
			p.DSLText = `permit(principal, action == "say", resource);`
			p.CompiledAST = json.RawMessage(`{"type":"policy","effect":"permit","updated":true}`)
			p.ChangeNote = "refined scope"
			p.CreatedBy = "admin-user"
			Expect(ps.Update(ctx, p)).To(Succeed())
			Expect(p.Version).To(Equal(2))
			Expect(p.ID).To(Equal(originalID))

			// Verify the updated policy round-trips.
			got, err := ps.Get(ctx, "update-test")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Description).To(Equal("Updated"))
			Expect(got.Version).To(Equal(2))

			// Verify version history was recorded.
			var historyCount int
			err = pool.QueryRow(ctx,
				"SELECT count(*) FROM access_policy_versions WHERE policy_id = $1", originalID,
			).Scan(&historyCount)
			Expect(err).NotTo(HaveOccurred())
			Expect(historyCount).To(Equal(1))

			// Verify history content.
			var historyDSL, historyNote string
			var historyVersion int
			err = pool.QueryRow(ctx,
				"SELECT version, dsl_text, change_note FROM access_policy_versions WHERE policy_id = $1",
				originalID,
			).Scan(&historyVersion, &historyDSL, &historyNote)
			Expect(err).NotTo(HaveOccurred())
			Expect(historyVersion).To(Equal(1))
			Expect(historyDSL).To(Equal("permit(principal, action, resource);"))
			Expect(historyNote).To(Equal("refined scope"))
		})

		It("returns POLICY_NOT_FOUND for missing policy", func() {
			ctx := context.Background()
			p := &store.StoredPolicy{
				Name:   "nonexistent",
				Source: "admin",
			}
			err := ps.Update(ctx, p)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("POLICY_NOT_FOUND"))
		})
	})

	Describe("Delete", func() {
		It("removes a policy and its version history", func() {
			ctx := context.Background()
			p := &store.StoredPolicy{
				Name:        "delete-test",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleAST(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, p)).To(Succeed())

			// Update to create version history.
			p.DSLText = "permit(principal, action, resource) when { true };"
			p.CompiledAST = sampleAST()
			p.CreatedBy = "system"
			Expect(ps.Update(ctx, p)).To(Succeed())

			Expect(ps.Delete(ctx, "delete-test")).To(Succeed())

			_, err := ps.Get(ctx, "delete-test")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("POLICY_NOT_FOUND"))

			// CASCADE should have removed version history.
			var historyCount int
			err = pool.QueryRow(ctx,
				"SELECT count(*) FROM access_policy_versions WHERE policy_id = $1", p.ID,
			).Scan(&historyCount)
			Expect(err).NotTo(HaveOccurred())
			Expect(historyCount).To(Equal(0))
		})

		It("returns POLICY_NOT_FOUND for missing policy", func() {
			ctx := context.Background()
			err := ps.Delete(ctx, "nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("POLICY_NOT_FOUND"))
		})
	})

	Describe("ListEnabled", func() {
		It("returns only enabled policies", func() {
			ctx := context.Background()
			enabled := &store.StoredPolicy{
				Name:        "enabled-policy",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleAST(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			disabled := &store.StoredPolicy{
				Name:        "disabled-policy",
				Effect:      types.PolicyEffectForbid,
				Source:      "admin",
				DSLText:     "forbid(principal, action, resource);",
				CompiledAST: sampleAST(),
				Enabled:     false,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, enabled)).To(Succeed())
			Expect(ps.Create(ctx, disabled)).To(Succeed())

			policies, err := ps.ListEnabled(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(policies).To(HaveLen(1))
			Expect(policies[0].Name).To(Equal("enabled-policy"))
		})
	})

	Describe("List", func() {
		BeforeEach(func() {
			ctx := context.Background()
			for _, p := range []*store.StoredPolicy{
				{Name: "seed:base", Effect: types.PolicyEffectPermit, Source: "seed", DSLText: "permit;", CompiledAST: sampleAST(), Enabled: true, CreatedBy: "system"},
				{Name: "lock:no-dig", Effect: types.PolicyEffectForbid, Source: "lock", DSLText: "forbid;", CompiledAST: sampleAST(), Enabled: true, CreatedBy: "system"},
				{Name: "admin-custom", Effect: types.PolicyEffectPermit, Source: "admin", DSLText: "permit;", CompiledAST: sampleAST(), Enabled: false, CreatedBy: "admin"},
			} {
				Expect(ps.Create(ctx, p)).To(Succeed())
			}
		})

		It("returns all policies with empty options", func() {
			ctx := context.Background()
			policies, err := ps.List(ctx, store.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(policies).To(HaveLen(3))
		})

		It("filters by source", func() {
			ctx := context.Background()
			policies, err := ps.List(ctx, store.ListOptions{Source: "seed"})
			Expect(err).NotTo(HaveOccurred())
			Expect(policies).To(HaveLen(1))
			Expect(policies[0].Name).To(Equal("seed:base"))
		})

		It("filters by enabled state", func() {
			ctx := context.Background()
			enabled := true
			policies, err := ps.List(ctx, store.ListOptions{Enabled: &enabled})
			Expect(err).NotTo(HaveOccurred())
			Expect(policies).To(HaveLen(2))
		})

		It("filters by effect", func() {
			ctx := context.Background()
			effect := types.PolicyEffectForbid
			policies, err := ps.List(ctx, store.ListOptions{Effect: &effect})
			Expect(err).NotTo(HaveOccurred())
			Expect(policies).To(HaveLen(1))
			Expect(policies[0].Name).To(Equal("lock:no-dig"))
		})

		It("combines filters", func() {
			ctx := context.Background()
			enabled := true
			effect := types.PolicyEffectPermit
			policies, err := ps.List(ctx, store.ListOptions{
				Enabled: &enabled,
				Effect:  &effect,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(policies).To(HaveLen(1))
			Expect(policies[0].Name).To(Equal("seed:base"))
		})
	})

	Describe("pg_notify", func() {
		It("sends notification on create", func() {
			ctx := context.Background()

			// Acquire a connection to listen for notifications.
			conn, err := pool.Acquire(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Release()

			_, err = conn.Exec(ctx, "LISTEN policy_changed")
			Expect(err).NotTo(HaveOccurred())

			p := &store.StoredPolicy{
				Name:        "notify-create",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleAST(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, p)).To(Succeed())

			notifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			notification, err := conn.Conn().WaitForNotification(notifyCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(notification.Channel).To(Equal("policy_changed"))
			Expect(notification.Payload).To(Equal(p.ID))
		})

		It("sends notification on update", func() {
			ctx := context.Background()

			p := &store.StoredPolicy{
				Name:        "notify-update",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleAST(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, p)).To(Succeed())

			conn, err := pool.Acquire(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Release()

			_, err = conn.Exec(ctx, "LISTEN policy_changed")
			Expect(err).NotTo(HaveOccurred())

			p.DSLText = "permit(principal, action, resource) when { true };"
			p.CompiledAST = sampleAST()
			p.CreatedBy = "system"
			Expect(ps.Update(ctx, p)).To(Succeed())

			notifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			notification, err := conn.Conn().WaitForNotification(notifyCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(notification.Channel).To(Equal("policy_changed"))
			Expect(notification.Payload).To(Equal(p.ID))
		})

		It("sends notification on delete", func() {
			ctx := context.Background()

			p := &store.StoredPolicy{
				Name:        "notify-delete",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleAST(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, p)).To(Succeed())
			policyID := p.ID

			conn, err := pool.Acquire(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Release()

			_, err = conn.Exec(ctx, "LISTEN policy_changed")
			Expect(err).NotTo(HaveOccurred())

			Expect(ps.Delete(ctx, "notify-delete")).To(Succeed())

			notifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			notification, err := conn.Conn().WaitForNotification(notifyCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(notification.Channel).To(Equal("policy_changed"))
			Expect(notification.Payload).To(Equal(policyID))
		})
	})
})
