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
	// Alias is the trigger string (e.g. `"` or `desc`).
	Alias string
	// Command is the canonical command name the alias expands to (e.g. "say").
	Command string
	// Plugin is the manifest name of the plugin that declared this alias.
	// It is written to system_aliases.source for provenance tracking.
	Plugin string
}

// AliasSeeder is the narrow subset of store.AliasRepository needed for
// manifest-driven seeding. The plugin package defines its own narrow interface
// (rather than importing store.AliasRepository) to keep the dependency
// direction clean and to simplify test fakes.
type AliasSeeder interface {
	GetSystemAliases(ctx context.Context) (map[string]string, error)
	SetSystemAlias(ctx context.Context, alias, cmd, createdBy, source string) error
}

// CollectManifestAliases gathers all alias declarations from loaded plugin
// manifests. Cross-plugin duplicates are logged as warnings and skipped;
// the first plugin in the input slice wins. Callers are responsible for
// passing plugins in a deterministic order (e.g. DAG load order) if stable
// conflict resolution is required.
func CollectManifestAliases(loaded []*DiscoveredPlugin) []ManifestAlias {
	var result []ManifestAlias
	seen := make(map[string]string) // alias → owning plugin name

	for _, dp := range loaded {
		for i := range dp.Manifest.Commands {
			cmd := &dp.Manifest.Commands[i]
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

	return result
}

// SeedManifestAliases persists collected aliases to the database using "skip
// existing" semantics and reloads all system aliases into the cache afterward.
// Each row is written with createdBy="" (stored as NULL in system_aliases.created_by,
// which is FK to players.id) and source=plugin name, distinguishing manifest-seeded
// rows from operator-created ones (which carry the operator's player ID and
// source="sysalias").
//
// Individual SetSystemAlias failures are logged and skipped (non-fatal).
// The cache is fully replaced (not merged) by LoadSystemAliases.
// An error is returned only if the initial fetch or the final cache reload fails.
func SeedManifestAliases(ctx context.Context, aliases []ManifestAlias, repo AliasSeeder, cache *command.AliasCache) error {
	existing, err := repo.GetSystemAliases(ctx)
	if err != nil {
		return oops.In("plugin-alias-seeder").
			Code("ALIAS_SEED_FETCH_FAILED").
			With("operation", "get existing aliases").
			Wrap(err)
	}

	// Track in-batch writes so a duplicate alias in the same `aliases` slice
	// doesn't upsert over an earlier successful write. `CollectManifestAliases`
	// already dedupes, but this keeps SeedManifestAliases safe for any caller.
	batchWritten := make(map[string]struct{})
	var seeded []string
	for _, a := range aliases {
		if _, exists := existing[a.Alias]; exists {
			continue
		}
		if _, inBatch := batchWritten[a.Alias]; inBatch {
			continue
		}
		if setErr := repo.SetSystemAlias(ctx, a.Alias, a.Command, "", a.Plugin); setErr != nil {
			slog.ErrorContext(ctx, "failed to seed manifest alias",
				"alias", a.Alias,
				"command", a.Command,
				"plugin", a.Plugin,
				"error", setErr,
			)
			continue
		}
		batchWritten[a.Alias] = struct{}{}
		seeded = append(seeded, a.Alias)
	}

	if len(seeded) > 0 {
		slog.InfoContext(ctx, "seeded manifest aliases", "aliases", seeded)
	}

	all, reloadErr := repo.GetSystemAliases(ctx)
	if reloadErr != nil {
		return oops.In("plugin-alias-seeder").
			Code("ALIAS_CACHE_RELOAD_FAILED").
			With("operation", "reload aliases into cache").
			Wrap(reloadErr)
	}
	cache.LoadSystemAliases(all)

	return nil
}
