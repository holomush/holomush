// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package bootstrap provides first-boot initialization for HoloMUSH.
package bootstrap

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
)

// AliasSeeder is the subset of store.AliasRepository needed for seeding.
type AliasSeeder interface {
	GetSystemAliases(ctx context.Context) (map[string]string, error)
	SetSystemAlias(ctx context.Context, alias, command, createdBy string) error
}

// standardAliases defines the MUSH aliases seeded on every startup.
var standardAliases = []struct {
	alias   string
	command string
}{
	{`"`, "say"},
	{":", "pose"},
	{";", "pose"},
}

// SeedSystemAliases ensures standard MUSH command aliases exist in the database
// and alias cache. Idempotent — skips aliases that already exist.
// Always reloads all system aliases into the cache regardless of seeding.
func SeedSystemAliases(ctx context.Context, repo AliasSeeder, cache *command.AliasCache) error {
	existing, err := repo.GetSystemAliases(ctx)
	if err != nil {
		return oops.With("operation", "get existing aliases").Wrap(err)
	}

	var seeded []string
	for _, a := range standardAliases {
		if _, exists := existing[a.alias]; exists {
			continue
		}
		if err := repo.SetSystemAlias(ctx, a.alias, a.command, ""); err != nil {
			return oops.With("operation", "set system alias").With("alias", a.alias).Wrap(err)
		}
		seeded = append(seeded, a.alias)
	}

	if len(seeded) > 0 {
		slog.Info("seeded system aliases", "aliases", seeded)
	}

	// Always reload all system aliases into cache (handles both fresh seed
	// and subsequent boots where aliases already exist in the database).
	all, reloadErr := repo.GetSystemAliases(ctx)
	if reloadErr != nil {
		return oops.With("operation", "reload aliases into cache").Wrap(reloadErr)
	}
	cache.LoadSystemAliases(all)

	return nil
}
