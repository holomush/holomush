// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package access_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
)

var _ = Describe("ABAC Full Evaluation Path (Canary)", func() {
	It("exercises the complete evaluation path with seed policies", func() {
		ctx := context.Background()

		_, _ = env.pool.Exec(ctx, "DELETE FROM characters")
		_, _ = env.pool.Exec(ctx, "DELETE FROM players")
		_, _ = env.pool.Exec(ctx, "DELETE FROM locations")

		locID := core.NewULID()
		_, err := env.pool.Exec(ctx, `
			INSERT INTO locations (id, name, description, type, replay_policy)
			VALUES ($1, 'Canary Location', 'Test.', 'persistent', 'last:0')`,
			locID.String())
		Expect(err).NotTo(HaveOccurred())

		playerID := core.NewULID()
		_, err = env.pool.Exec(ctx, `
			INSERT INTO players (id, username, password_hash)
			VALUES ($1, $2, 'hash')`,
			playerID.String(), "canary_"+time.Now().Format("150405.000"))
		Expect(err).NotTo(HaveOccurred())

		charID := core.NewULID()
		_, err = env.pool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, location_id)
			VALUES ($1, $2, 'CanaryChar', $3)`,
			charID.String(), playerID.String(), locID.String())
		Expect(err).NotTo(HaveOccurred())

		env.auditWriter.Reset()

		decision := evalAccess("character:"+charID.String(), "read", "character:"+charID.String())
		Expect(decision.Effect()).To(Equal(types.EffectAllow))
		Expect(env.auditWriter.Entries()).NotTo(BeEmpty())

		decision = evalAccess("character:"+charID.String(), "destroy", "location:"+locID.String())
		Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
	})
})

var _ = Describe("System Bypass", func() {
	It("allows system subject with system context", func() {
		ctx := access.WithSystemSubject(context.Background())
		req := types.AccessRequest{
			Subject:  "system",
			Action:   "read",
			Resource: "location:any-id",
		}
		decision, err := env.engine.Evaluate(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(decision.Effect()).To(Equal(types.EffectSystemBypass))
	})

	It("rejects system subject without system context", func() {
		req := types.AccessRequest{
			Subject:  "system",
			Action:   "read",
			Resource: "location:any-id",
		}
		_, err := env.engine.Evaluate(context.Background(), req)
		Expect(err).To(HaveOccurred())
	})
})
