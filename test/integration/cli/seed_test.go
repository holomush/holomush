// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package cli_test

import (
	"context"
	"os"
	"os/exec"
	"sync"

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

		It("warns when existing location attributes differ from expected", func() {
			// First run - creates location
			cmd1 := exec.CommandContext(ctx, "go", "run", ".", "seed")
			cmd1.Dir = "../../../cmd/holomush"
			cmd1.Env = append(cmd1.Environ(), "DATABASE_URL="+env.connStr)

			output1, err := cmd1.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), "first seed failed: %s", string(output1))

			// Modify the location to have different attributes
			_, err = env.pool.Exec(ctx,
				"UPDATE locations SET name = $1 WHERE id = $2",
				"Modified Nexus", "01HZN3XS000000000000000000",
			)
			Expect(err).NotTo(HaveOccurred())

			// Second run - should log warning about mismatch
			cmd2 := exec.CommandContext(ctx, "go", "run", ".", "seed")
			cmd2.Dir = "../../../cmd/holomush"
			cmd2.Env = append(cmd2.Environ(), "DATABASE_URL="+env.connStr)

			output2, err := cmd2.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), "second seed failed: %s", string(output2))
			Expect(string(output2)).To(ContainSubstring("Starting location already exists"))
			// The warning is logged via slog.Warn which goes to stderr in JSON format
			Expect(string(output2)).To(ContainSubstring("mismatch"))
		})

		It("handles concurrent seed commands without creating duplicates", func() {
			var wg sync.WaitGroup
			results := make(chan error, 5)

			for i := 0; i < 5; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					cmd := exec.CommandContext(ctx, "go", "run", ".", "seed")
					cmd.Dir = "../../../cmd/holomush"
					cmd.Env = []string{
						"DATABASE_URL=" + env.connStr,
						"PATH=" + os.Getenv("PATH"),
						"HOME=" + os.Getenv("HOME"),
					}
					err := cmd.Run()
					results <- err
				}()
			}
			wg.Wait()
			close(results)

			// Some may succeed, some may fail with constraint violation - that's expected
			// The important thing is exactly one location exists

			// Verify exactly one location exists
			var count int
			err := env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM locations WHERE name = 'The Nexus'").Scan(&count)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))
		})
	})

	Describe("Error handling", func() {
		It("fails with CONFIG_INVALID when DATABASE_URL is missing", func() {
			cmd := exec.CommandContext(ctx, "go", "run", ".", "seed")
			cmd.Dir = "../../../cmd/holomush"
			// Explicitly set environment to exclude DATABASE_URL.
			// Without this, exec.Command inherits ALL environment variables from parent.
			cmd.Env = []string{
				"PATH=" + os.Getenv("PATH"),
				"HOME=" + os.Getenv("HOME"),
			}

			output, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred())
			// Error message should indicate missing config
			Expect(string(output)).To(ContainSubstring("DATABASE_URL"))
		})
	})
})
