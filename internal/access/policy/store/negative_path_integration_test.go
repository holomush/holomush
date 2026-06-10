// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// sampleASTNeg returns a minimal valid compiled_ast for negative-path tests.
func sampleASTNeg() json.RawMessage {
	return json.RawMessage(`{"type":"policy","effect":"permit","grammar_version":1}`)
}

var _ = Describe("PostgresStore negative-path constraints", func() {
	BeforeEach(func() {
		cleanupPolicies(context.Background())
	})

	// -----------------------------------------------------------------------
	// Unique-name constraint
	// -----------------------------------------------------------------------

	Describe("Create — duplicate name unique constraint", func() {
		It("returns an error when a second policy is inserted with the same name", func() {
			ctx := context.Background()

			first := &store.StoredPolicy{
				Name:        "neg-unique-name",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleASTNeg(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, first)).To(Succeed())

			second := &store.StoredPolicy{
				Name:        "neg-unique-name", // same name, different everything else
				Effect:      types.PolicyEffectForbid,
				Source:      "admin",
				DSLText:     "forbid(principal, action, resource);",
				CompiledAST: sampleASTNeg(),
				Enabled:     false,
				CreatedBy:   "system",
			}
			err := ps.Create(ctx, second)
			Expect(err).To(HaveOccurred(), "inserting a duplicate policy name must fail")
		})

		It("wraps the constraint error as POLICY_CREATE_FAILED", func() {
			ctx := context.Background()

			p := &store.StoredPolicy{
				Name:        "neg-create-failed-code",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleASTNeg(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, p)).To(Succeed())

			dup := &store.StoredPolicy{
				Name:        "neg-create-failed-code",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleASTNeg(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			err := ps.Create(ctx, dup)
			Expect(err).To(HaveOccurred())
			oopsErr, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue(), "expected oops error")
			Expect(oopsErr.Code()).To(Equal("POLICY_CREATE_FAILED"))
		})
	})

	// -----------------------------------------------------------------------
	// CreateBatch — duplicate name within batch causes full rollback
	// -----------------------------------------------------------------------

	Describe("CreateBatch — partial failure rolls back the whole batch", func() {
		It("leaves no rows when one policy in the batch has a duplicate name", func() {
			ctx := context.Background()

			// Pre-existing policy whose name will collide with the second item in the batch.
			preExisting := &store.StoredPolicy{
				Name:        "neg-batch-collision",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleASTNeg(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, preExisting)).To(Succeed())

			// Batch: first item is new and fine; second collides with pre-existing row.
			batch := []*store.StoredPolicy{
				{
					Name:        "neg-batch-new-first",
					Effect:      types.PolicyEffectPermit,
					Source:      "admin",
					DSLText:     "permit(principal, action, resource);",
					CompiledAST: sampleASTNeg(),
					Enabled:     true,
					CreatedBy:   "system",
				},
				{
					Name:        "neg-batch-collision", // collision — should cause rollback
					Effect:      types.PolicyEffectForbid,
					Source:      "admin",
					DSLText:     "forbid(principal, action, resource);",
					CompiledAST: sampleASTNeg(),
					Enabled:     false,
					CreatedBy:   "system",
				},
			}

			err := ps.CreateBatch(ctx, batch)
			Expect(err).To(HaveOccurred(), "batch with duplicate name must fail")

			// The first (clean) item must NOT have been written — full rollback.
			policies, listErr := ps.List(ctx, store.ListOptions{})
			Expect(listErr).NotTo(HaveOccurred())

			names := make([]string, 0, len(policies))
			for _, pol := range policies {
				names = append(names, pol.Name)
			}
			Expect(names).NotTo(ContainElement("neg-batch-new-first"),
				"rolled-back first item must not appear in the store")
			// The pre-existing policy must remain unchanged.
			Expect(names).To(ContainElement("neg-batch-collision"),
				"pre-existing policy must survive the failed batch")
		})
	})

	// -----------------------------------------------------------------------
	// ReplaceBySource — partial failure rolls back the whole replace
	// -----------------------------------------------------------------------

	Describe("ReplaceBySource — partial failure rolls back the whole replace", func() {
		It("leaves the original rows intact when one replacement policy has a duplicate name outside the replaced set", func() {
			ctx := context.Background()

			// Two seed policies that will be the replace targets.
			seedA := &store.StoredPolicy{
				Name:        "seed:neg-replace-a",
				Effect:      types.PolicyEffectPermit,
				Source:      "seed",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleASTNeg(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			seedB := &store.StoredPolicy{
				Name:        "seed:neg-replace-b",
				Effect:      types.PolicyEffectPermit,
				Source:      "seed",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleASTNeg(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, seedA)).To(Succeed())
			Expect(ps.Create(ctx, seedB)).To(Succeed())

			// An unrelated admin policy whose name will collide with the second replacement.
			adminCollider := &store.StoredPolicy{
				Name:        "neg-replace-admin-collider",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleASTNeg(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, adminCollider)).To(Succeed())

			// Replacement batch: first is valid, second collides with the admin policy
			// (different source, same name — unique constraint on name column).
			replacements := []*store.StoredPolicy{
				{
					Name:        "seed:neg-replace-a-new",
					Effect:      types.PolicyEffectPermit,
					Source:      "seed",
					DSLText:     "permit(principal, action, resource);",
					CompiledAST: sampleASTNeg(),
					Enabled:     true,
					CreatedBy:   "system",
				},
				{
					Name:        "neg-replace-admin-collider", // collides — wrong source too
					Effect:      types.PolicyEffectPermit,
					Source:      "seed", // violates source-naming rule for non-seed: prefix name
					DSLText:     "permit(principal, action, resource);",
					CompiledAST: sampleASTNeg(),
					Enabled:     true,
					CreatedBy:   "system",
				},
			}

			err := ps.ReplaceBySource(ctx, "seed", "seed:", replacements)
			Expect(err).To(HaveOccurred(),
				"replace with a policy that violates source-naming must fail")

			// Original seed rows must still be present (full rollback).
			policies, listErr := ps.List(ctx, store.ListOptions{Source: "seed"})
			Expect(listErr).NotTo(HaveOccurred())

			names := make([]string, 0, len(policies))
			for _, pol := range policies {
				names = append(names, pol.Name)
			}
			Expect(names).To(ContainElement("seed:neg-replace-a"),
				"original seed:neg-replace-a must survive the failed replace")
			Expect(names).To(ContainElement("seed:neg-replace-b"),
				"original seed:neg-replace-b must survive the failed replace")
			Expect(names).NotTo(ContainElement("seed:neg-replace-a-new"),
				"rolled-back replacement must not appear in the store")
		})

		It("leaves the original rows intact when a replacement batch causes a DB unique-name collision", func() {
			ctx := context.Background()

			// A lock policy that will be replaced.
			lockP := &store.StoredPolicy{
				Name:        "lock:neg-rollback-test",
				Effect:      types.PolicyEffectForbid,
				Source:      "lock",
				DSLText:     "forbid(principal, action, resource);",
				CompiledAST: sampleASTNeg(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, lockP)).To(Succeed())

			// An admin policy that will collide with the second replacement.
			collidingAdmin := &store.StoredPolicy{
				Name:        "lock:neg-duplicate-in-batch",
				Effect:      types.PolicyEffectPermit,
				Source:      "lock",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleASTNeg(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, collidingAdmin)).To(Succeed())

			// Replacement: first item is new and valid; second has the same name as
			// collidingAdmin (which is NOT in the "lock:neg-rollback-test" prefix set
			// being replaced, so it remains and causes a duplicate on insert).
			replacements := []*store.StoredPolicy{
				{
					Name:        "lock:neg-rollback-new",
					Effect:      types.PolicyEffectForbid,
					Source:      "lock",
					DSLText:     "forbid(principal, action, resource);",
					CompiledAST: sampleASTNeg(),
					Enabled:     true,
					CreatedBy:   "system",
				},
				{
					Name:        "lock:neg-duplicate-in-batch", // already exists, not deleted by the prefix
					Effect:      types.PolicyEffectForbid,
					Source:      "lock",
					DSLText:     "forbid(principal, action, resource);",
					CompiledAST: sampleASTNeg(),
					Enabled:     true,
					CreatedBy:   "system",
				},
			}

			err := ps.ReplaceBySource(ctx, "lock", "lock:neg-rollback-test", replacements)
			Expect(err).To(HaveOccurred(),
				"replace that causes a DB unique-name collision must fail")

			// Original lock:neg-rollback-test row must still be present.
			got, getErr := ps.Get(ctx, "lock:neg-rollback-test")
			Expect(getErr).NotTo(HaveOccurred(),
				"original lock policy must still exist after failed replace")
			Expect(got.Name).To(Equal("lock:neg-rollback-test"))

			// The new row must not have been written.
			_, getErr = ps.Get(ctx, "lock:neg-rollback-new")
			Expect(getErr).To(HaveOccurred(),
				"rolled-back replacement must not appear in the store")
			oopsErr, ok := oops.AsOops(getErr)
			Expect(ok).To(BeTrue())
			Expect(oopsErr.Code()).To(Equal("POLICY_NOT_FOUND"))
		})
	})

	// -----------------------------------------------------------------------
	// Update — concurrent optimistic lock
	// -----------------------------------------------------------------------

	Describe("Update — concurrent row-level lock", func() {
		It("fails when the context is cancelled while the row is locked", func() {
			ctx := context.Background()

			p := &store.StoredPolicy{
				Name:        "neg-update-lock",
				Effect:      types.PolicyEffectPermit,
				Source:      "admin",
				DSLText:     "permit(principal, action, resource);",
				CompiledAST: sampleASTNeg(),
				Enabled:     true,
				CreatedBy:   "system",
			}
			Expect(ps.Create(ctx, p)).To(Succeed())

			// Lock the row in an open transaction.
			tx, txErr := pool.Begin(ctx)
			Expect(txErr).NotTo(HaveOccurred())
			defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // cleanup

			_, lockErr := tx.Exec(ctx,
				`SELECT id FROM access_policies WHERE name = $1 FOR UPDATE`, "neg-update-lock")
			Expect(lockErr).NotTo(HaveOccurred())

			// Attempt an update with a pre-cancelled context — the connection
			// acquisition or lock-wait must fail immediately.
			cancelledCtx, cancelFn := context.WithCancel(ctx)
			cancelFn() // pre-cancel so any blocking operation fails at once

			p.DSLText = "permit(principal, action, resource) when { false };"
			p.CompiledAST = sampleASTNeg()
			p.CreatedBy = "system"
			err := ps.Update(cancelledCtx, p)
			Expect(err).To(HaveOccurred(),
				"update with a cancelled context must fail")
		})
	})
})
