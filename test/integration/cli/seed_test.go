// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package cli_test

import (
	"context"
	"os/exec"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

var _ = Describe("Seed Command", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		cleanupDatabase(ctx, env.pool)
	})

	Describe("World seeding", func() {
		It("creates the starting location 'The Nexus'", func() {
			// Run seed command
			cmd := exec.CommandContext(ctx, "go", "run", ".", "seed")
			cmd.Dir = "../../../cmd/holomush"
			cmd.Env = append(cmd.Environ(), "DATABASE_URL="+env.connStr)

			output, err := cmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), "seed command failed: %s", string(output))
			Expect(string(output)).To(ContainSubstring("Created starting location: The Nexus"))
			Expect(string(output)).To(ContainSubstring("World seeding complete!"))

			// Verify location exists in database
			var name, description, locType string
			err = env.pool.QueryRow(ctx,
				"SELECT name, description, type FROM locations WHERE id = $1",
				"01HZN3XS000000000000000000",
			).Scan(&name, &description, &locType)
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal("The Nexus"))
			Expect(locType).To(Equal("persistent"))
			Expect(description).To(ContainSubstring("swirling vortex"))
		})

		It("is idempotent (running twice succeeds without duplicates)", func() {
			// First run - creates location
			cmd1 := exec.CommandContext(ctx, "go", "run", ".", "seed")
			cmd1.Dir = "../../../cmd/holomush"
			cmd1.Env = append(cmd1.Environ(), "DATABASE_URL="+env.connStr)

			output1, err := cmd1.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), "first seed failed: %s", string(output1))
			Expect(string(output1)).To(ContainSubstring("Created starting location"))

			// Second run - should succeed but skip creation
			cmd2 := exec.CommandContext(ctx, "go", "run", ".", "seed")
			cmd2.Dir = "../../../cmd/holomush"
			cmd2.Env = append(cmd2.Environ(), "DATABASE_URL="+env.connStr)

			output2, err := cmd2.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), "second seed failed: %s", string(output2))
			Expect(string(output2)).To(ContainSubstring("Starting location already exists"))

			// Verify still only one location with that ID
			var count int
			err = env.pool.QueryRow(ctx,
				"SELECT COUNT(*) FROM locations WHERE id = $1",
				"01HZN3XS000000000000000000",
			).Scan(&count)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))
		})

		It("sets correct replay policy for persistent location", func() {
			cmd := exec.CommandContext(ctx, "go", "run", ".", "seed")
			cmd.Dir = "../../../cmd/holomush"
			cmd.Env = append(cmd.Environ(), "DATABASE_URL="+env.connStr)

			err := cmd.Run()
			Expect(err).NotTo(HaveOccurred())

			var replayPolicy string
			err = env.pool.QueryRow(ctx,
				"SELECT replay_policy FROM locations WHERE id = $1",
				"01HZN3XS000000000000000000",
			).Scan(&replayPolicy)
			Expect(err).NotTo(HaveOccurred())
			Expect(replayPolicy).To(Equal("last:0"), "persistent locations should have no replay")
		})
	})

	Describe("Error handling", func() {
		It("fails with CONFIG_INVALID when DATABASE_URL is missing", func() {
			cmd := exec.CommandContext(ctx, "go", "run", ".", "seed")
			cmd.Dir = "../../../cmd/holomush"
			// Don't set DATABASE_URL - inherits PATH but not DATABASE_URL from parent
			// By not calling cmd.Env = append(cmd.Environ(), ...) we get empty DATABASE_URL

			output, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred())
			// Error message should indicate missing config
			Expect(string(output)).To(ContainSubstring("DATABASE_URL"))
		})
	})
})
