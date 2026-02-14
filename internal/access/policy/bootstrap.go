// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/store"
)

// BootstrapPartitionCreator is the subset of audit.PartitionManager needed by bootstrap.
type BootstrapPartitionCreator interface {
	EnsurePartitions(ctx context.Context, months int) error
}

// BootstrapOptions configures bootstrap behavior.
type BootstrapOptions struct {
	SkipSeedMigrations bool // Disable automatic seed version upgrades
}

// Bootstrap seeds the policy store with default policies and creates initial
// audit log partitions. Failures are fatal — any error MUST cause the server
// to abort startup (ADR #92).
func Bootstrap(
	ctx context.Context,
	partitions BootstrapPartitionCreator,
	policyStore store.PolicyStore,
	compiler *Compiler,
	logger *slog.Logger,
	opts BootstrapOptions,
) error {
	// Step 1: Create initial audit log partitions (ADR #91).
	// This must happen before seed insertion so audit log writes succeed.
	if err := partitions.EnsurePartitions(ctx, 3); err != nil {
		return oops.Errorf("bootstrap: failed to create audit log partitions: %w", err)
	}
	logger.Info("audit log partitions ensured", "months", 3)

	// Step 2: Seed policies.
	seeds := SeedPolicies()
	for _, seed := range seeds {
		if err := bootstrapSeed(ctx, policyStore, compiler, logger, seed, opts); err != nil {
			return oops.
				With("seed", seed.Name).
				Errorf("bootstrap: fatal seed policy error: %w", err)
		}
	}

	logger.Info("bootstrap complete", "seeds_total", len(seeds))
	return nil
}

// bootstrapSeed handles a single seed policy: insert if new, upgrade if outdated,
// skip if already current or admin-customized.
func bootstrapSeed(
	ctx context.Context,
	policyStore store.PolicyStore,
	compiler *Compiler,
	logger *slog.Logger,
	seed SeedPolicy,
	opts BootstrapOptions,
) error {
	existing, err := policyStore.Get(ctx, seed.Name)
	if err != nil && !store.IsNotFound(err) {
		return fmt.Errorf("checking existing policy %q: %w", seed.Name, err)
	}

	// Policy doesn't exist — compile and create.
	if store.IsNotFound(err) {
		return createSeedPolicy(ctx, policyStore, compiler, logger, seed)
	}

	// Policy exists with different source — admin collision, skip with warning.
	if existing.Source != "seed" {
		logger.Warn("seed policy name collision with non-seed policy, skipping",
			"name", seed.Name,
			"existing_source", existing.Source,
		)
		return nil
	}

	// Policy exists as seed — check for version upgrade.
	if !opts.SkipSeedMigrations && existing.SeedVersion != nil && *existing.SeedVersion < seed.SeedVersion {
		return upgradeSeedPolicy(ctx, policyStore, compiler, logger, seed, existing)
	}

	logger.Info("seed policy already current, skipping",
		"name", seed.Name,
		"version", seed.SeedVersion,
	)
	return nil
}

// createSeedPolicy compiles and inserts a new seed policy.
func createSeedPolicy(
	ctx context.Context,
	policyStore store.PolicyStore,
	compiler *Compiler,
	logger *slog.Logger,
	seed SeedPolicy,
) error {
	compiled, _, err := compiler.Compile(seed.DSLText)
	if err != nil {
		return fmt.Errorf("compiling seed policy %q: %w", seed.Name, err)
	}

	astJSON, err := json.Marshal(compiled)
	if err != nil {
		return fmt.Errorf("marshaling compiled AST for %q: %w", seed.Name, err)
	}

	seedVersion := seed.SeedVersion
	sp := &store.StoredPolicy{
		Name:        seed.Name,
		Description: seed.Description,
		Effect:      compiled.Effect,
		Source:      "seed",
		DSLText:     seed.DSLText,
		CompiledAST: astJSON,
		Enabled:     true,
		SeedVersion: &seedVersion,
		CreatedBy:   "system",
	}

	if err := policyStore.Create(ctx, sp); err != nil {
		return fmt.Errorf("creating seed policy %q: %w", seed.Name, err)
	}

	logger.Info("seed policy created", "name", seed.Name, "version", seed.SeedVersion)
	return nil
}

// upgradeSeedPolicy updates an existing seed policy to a new version.
func upgradeSeedPolicy(
	ctx context.Context,
	policyStore store.PolicyStore,
	compiler *Compiler,
	logger *slog.Logger,
	seed SeedPolicy,
	existing *store.StoredPolicy,
) error {
	compiled, _, err := compiler.Compile(seed.DSLText)
	if err != nil {
		return fmt.Errorf("compiling upgraded seed policy %q: %w", seed.Name, err)
	}

	astJSON, err := json.Marshal(compiled)
	if err != nil {
		return fmt.Errorf("marshaling compiled AST for %q: %w", seed.Name, err)
	}

	oldVersion := 0
	if existing.SeedVersion != nil {
		oldVersion = *existing.SeedVersion
	}

	seedVersion := seed.SeedVersion
	existing.DSLText = seed.DSLText
	existing.CompiledAST = astJSON
	existing.Effect = compiled.Effect
	existing.SeedVersion = &seedVersion
	existing.ChangeNote = fmt.Sprintf("Auto-upgraded from seed v%d to v%d on server upgrade",
		oldVersion, seed.SeedVersion)

	if err := policyStore.Update(ctx, existing); err != nil {
		return fmt.Errorf("upgrading seed policy %q: %w", seed.Name, err)
	}

	logger.Info("seed policy upgraded",
		"name", seed.Name,
		"from_version", oldVersion,
		"to_version", seed.SeedVersion,
	)
	return nil
}

// UpdateSeed updates a seed policy's DSL text as part of a migration-delivered fix.
// It uses compare-and-swap semantics: oldDSL must match the stored DSL for the
// update to proceed. If the stored DSL differs from oldDSL, the seed has been
// customized by an admin and the update is skipped with a warning.
func UpdateSeed(
	ctx context.Context,
	policyStore store.PolicyStore,
	compiler *Compiler,
	logger *slog.Logger,
	name string,
	oldDSL string,
	newDSL string,
	changeNote string,
) error {
	existing, err := policyStore.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("UpdateSeed: policy %q not found: %w", name, err)
	}

	if existing.Source != "seed" {
		return fmt.Errorf("UpdateSeed: policy %q has source %q, not seed", name, existing.Source)
	}

	// Idempotent: skip if DSL already matches target.
	if existing.DSLText == newDSL {
		return nil
	}

	// Compare-and-swap: skip with warning if admin customized.
	if existing.DSLText != oldDSL {
		logger.Warn("seed policy customized by admin, skipping migration update",
			"name", name,
			"expected_dsl_prefix", truncate(oldDSL, 60),
			"actual_dsl_prefix", truncate(existing.DSLText, 60),
		)
		return nil
	}

	// Compile the new DSL.
	compiled, _, compileErr := compiler.Compile(newDSL)
	if compileErr != nil {
		return fmt.Errorf("UpdateSeed: compiling new DSL for %q: %w", name, compileErr)
	}

	astJSON, marshalErr := json.Marshal(compiled)
	if marshalErr != nil {
		return fmt.Errorf("UpdateSeed: marshaling AST for %q: %w", name, marshalErr)
	}

	existing.DSLText = newDSL
	existing.CompiledAST = astJSON
	existing.Effect = compiled.Effect
	existing.ChangeNote = changeNote

	if err := policyStore.Update(ctx, existing); err != nil {
		return fmt.Errorf("UpdateSeed: updating %q: %w", name, err)
	}

	logger.Info("seed policy updated via migration", "name", name, "change_note", changeNote)
	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
