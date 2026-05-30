// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// CharacterPreferences is the persisted shape of a character's settings bag,
// stored whole-struct in the characters.preferences JSONB column. It mirrors
// the player Preferences layout (auth.PlayerPreferences): a reserved host
// partition plus an owner-partitioned plugin map. The whole struct round-trips
// through the repository so partitions are never clobbered by an
// owner-partition write.
type CharacterPreferences struct {
	// Host holds the host-owned settings partition: a flat dot-keyed map
	// serialized as JSON. Reserved for symmetry with the player layout;
	// character host settings are minimal today (SetString persists here).
	Host json.RawMessage `json:"host,omitempty"`
	// Plugins is the opaque, owner-partitioned settings bag. The host never
	// interprets partition contents; it maps an owner key (plugin name) to that
	// owner's serialized partition. JSON (de)marshaling carries it to/from the
	// characters.preferences JSONB column — the mirror of
	// auth.PlayerPreferences.Plugins.
	Plugins map[string]json.RawMessage `json:"plugins,omitempty"`
}

// CharacterRepository is the narrow whole-struct character-preferences
// persistence surface the repo-backed character settings store needs. Reads
// load the whole preferences bag (returning a zero bag, not an error, for an
// unprovisioned character); writes persist the whole bag. It is satisfied by
// *store.CharacterSettingsRepository.
type CharacterRepository interface {
	GetPreferences(ctx context.Context, characterID ulid.ULID) (CharacterPreferences, error)
	SetPreferences(ctx context.Context, characterID ulid.ULID, prefs CharacterPreferences) error
}

// CharacterSettings implements CharacterSettingsStore backed by a
// CharacterRepository. Owner partition writes persist via a read-modify-write
// commit func: each write re-reads the character's preferences, merges only the
// mutated partitions into the bag, and writes the whole bag — so sibling owner
// partitions written by a separate For() call are not lost.
type CharacterSettings struct {
	repo CharacterRepository
}

// NewRepoCharacterSettingsStore creates a CharacterSettingsStore backed by the
// character settings repository.
func NewRepoCharacterSettingsStore(repo CharacterRepository) *CharacterSettings {
	return &CharacterSettings{repo: repo}
}

// For returns an owner-partitioned Scoped handle for a character.
//
// Owner partitions and the host partition are loaded from the character's
// persisted preferences, and writes persist via a non-nil commit func. On any
// load failure the handle degrades to an empty, read-only view so bare reads
// and the resolution Chain resolve to defaults rather than panicking — matching
// the player store's degrade-on-error contract and the Settings
// reads-never-error invariant.
//
// Concurrency: the returned handle is per-request. Its in-memory partition maps
// are mutated without synchronization and MUST NOT be shared across goroutines.
// Lost-update safety across separate For() calls is provided by the commit func
// re-reading the character.
func (s *CharacterSettings) For(ctx context.Context, characterID ulid.ULID) Scoped {
	prefs, err := s.repo.GetPreferences(ctx, characterID)
	if err != nil {
		// Fail closed: reads resolve to defaults (Settings never errors), but a
		// write surfaces the load failure rather than silently dropping.
		slog.WarnContext(ctx, "character settings load failed",
			"character_id", characterID.String(), "error", err)
		return newFailClosedView(oops.With("character_id", characterID.String()).Wrap(err))
	}

	// host and plugins are the live maps the scopedView's Host()/Owner()
	// writables mutate. The commit closure captures them directly so a write
	// serializes the touched partitions back.
	host := decodeHostPartition(ctx, characterID, prefs.Host)
	plugins := decodePluginPartitions(ctx, prefs.Plugins)

	return newTrackedScopedView(host, plugins,
		func(dirty *dirtyTracker) func(ctx context.Context) error {
			return func(ctx context.Context) error {
				// Re-read so the merge runs against the latest persisted state, then
				// overwrite ONLY the partitions this view mutated. A clean-loaded
				// sibling owner is never re-serialized with its stale value, so a
				// concurrent For() handle that changed a different owner keeps its
				// update (cross-owner lost-update safety).
				fresh, err := s.repo.GetPreferences(ctx, characterID)
				if err != nil {
					return oops.With("character_id", characterID.String()).Wrap(err)
				}
				if fresh.Plugins == nil {
					fresh.Plugins = map[string]json.RawMessage{}
				}
				for owner := range dirty.owners {
					encoded, encErr := json.Marshal(plugins[owner])
					if encErr != nil {
						return oops.With("character_id", characterID.String(), "owner", owner).Wrap(encErr)
					}
					fresh.Plugins[owner] = json.RawMessage(encoded)
				}
				if dirty.host {
					encodedHost, encErr := json.Marshal(host)
					if encErr != nil {
						return oops.With("character_id", characterID.String()).Wrap(encErr)
					}
					fresh.Host = json.RawMessage(encodedHost)
				}
				if err := s.repo.SetPreferences(ctx, characterID, fresh); err != nil {
					return oops.With("character_id", characterID.String()).Wrap(err)
				}
				return nil
			}
		})
}

// decodeHostPartition materializes the serialized host partition (a flat
// dot-keyed map) into the scopedView's host map. A NULL/empty or undecodable
// host blob yields an empty map and a warning; the host never panics on a
// malformed partition.
func decodeHostPartition(
	ctx context.Context, characterID ulid.ULID, raw json.RawMessage,
) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	if len(raw) == 0 {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		slog.WarnContext(ctx, "skipping undecodable character host settings partition",
			"character_id", characterID.String(), "error", err)
		return map[string]json.RawMessage{}
	}
	return out
}

// SetString writes a single host-partition preference key for a character. It
// loads the character's current preferences, sets the key on the host
// partition, and persists the whole bag via the commit func, so it never
// clobbers the owner-partitioned Plugins bag. The host writable namespace-
// validates the key.
func (s *CharacterSettings) SetString(
	ctx context.Context, characterID ulid.ULID, key, value string,
) error {
	if err := s.For(ctx, characterID).Host().SetString(ctx, key, value); err != nil {
		return oops.With("key", key, "character_id", characterID.String()).Wrap(err)
	}
	return nil
}

// Compile-time interface check.
var _ CharacterSettingsStore = (*CharacterSettings)(nil)
