// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
)

// ManifestAlias is an alias declaration collected from a plugin manifest.
type ManifestAlias struct {
	Alias   string
	Command string
	Plugin  string
}

// AliasSeeder is the subset of store.AliasRepository needed for seeding.
type AliasSeeder interface {
	GetSystemAliases(ctx context.Context) (map[string]string, error)
	SetSystemAlias(ctx context.Context, alias, command, createdBy string) error
}

// CollectManifestAliases gathers all alias declarations from loaded plugin
// manifests. Cross-plugin duplicates are logged as warnings and skipped;
// the first plugin to declare an alias wins.
func CollectManifestAliases(loaded []*DiscoveredPlugin) ([]ManifestAlias, error) {
	var result []ManifestAlias
	seen := make(map[string]string) // alias → owning plugin name

	for _, dp := range loaded {
		for _, cmd := range dp.Manifest.Commands {
			for _, alias := range cmd.Aliases {
				if owner, dup := seen[alias]; dup {
					slog.Warn("duplicate manifest alias, skipping",
						"alias", alias,
						"command", cmd.Name,
						"plugin", dp.Manifest.Name,
						"owner", owner,
					)
					continue
				}
				seen[alias] = dp.Manifest.Name
				result = append(result, ManifestAlias{
					Alias:   alias,
					Command: cmd.Name,
					Plugin:  dp.Manifest.Name,
				})
			}
		}
	}

	return result, nil
}

// SeedManifestAliases persists collected aliases to the database using "skip
// existing" semantics and reloads all system aliases into the cache afterward.
// Individual SetSystemAlias failures are logged and skipped (non-fatal).
// An error is returned only if the final cache reload fails.
func SeedManifestAliases(ctx context.Context, aliases []ManifestAlias, repo AliasSeeder, cache *command.AliasCache) error {
	existing, err := repo.GetSystemAliases(ctx)
	if err != nil {
		return oops.With("operation", "get existing aliases").Wrap(err)
	}

	var seeded []string
	for _, a := range aliases {
		if _, exists := existing[a.Alias]; exists {
			continue
		}
		if setErr := repo.SetSystemAlias(ctx, a.Alias, a.Command, ""); setErr != nil {
			slog.Error("failed to seed manifest alias",
				"alias", a.Alias,
				"command", a.Command,
				"plugin", a.Plugin,
				"error", setErr,
			)
			continue
		}
		seeded = append(seeded, a.Alias)
	}

	if len(seeded) > 0 {
		slog.Info("seeded manifest aliases", "aliases", seeded)
	}

	all, reloadErr := repo.GetSystemAliases(ctx)
	if reloadErr != nil {
		return oops.With("operation", "reload aliases into cache").Wrap(reloadErr)
	}
	cache.LoadSystemAliases(all)

	return nil
}
