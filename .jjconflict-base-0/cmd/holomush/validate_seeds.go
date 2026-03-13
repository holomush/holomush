// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// NewValidateSeedsCmd creates the validate-seeds subcommand.
func NewValidateSeedsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate-seeds",
		Short: "Validate all seed policy DSL without starting the server",
		Long: `Validates all seed policy DSL text using the compiler.
Does NOT start the server or require a database connection.
Exits with code 0 on success, non-zero on failure.

Useful in CI pipelines to catch seed policy errors early:
  holomush validate-seeds`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runValidateSeeds()
		},
	}
}

func runValidateSeeds() error {
	seeds := policy.SeedPolicies()
	schema := types.NewAttributeSchema()
	compiler := policy.NewCompiler(schema)

	var errors []string
	for _, seed := range seeds {
		_, _, err := compiler.Compile(seed.DSLText)
		if err != nil {
			errors = append(errors, fmt.Sprintf("  %s: %v", seed.Name, err))
		}
	}

	if len(errors) > 0 {
		for _, e := range errors {
			slog.Error("seed validation failed", "detail", e)
		}
		return fmt.Errorf("validation failed: %d of %d seed policies invalid", len(errors), len(seeds))
	}

	slog.Info("all seed policies valid", "count", len(seeds))
	return nil
}
