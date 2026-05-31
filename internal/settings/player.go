// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
)

// PlayerPrefsReader is the narrow interface for reading and writing player
// preferences as raw JSON. Satisfied by a thin adapter around
// auth.PlayerRepository.
type PlayerPrefsReader interface {
	GetPlayerPreferencesJSON(ctx context.Context, playerID ulid.ULID) (json.RawMessage, error)
	SetPlayerPreferenceKey(ctx context.Context, playerID ulid.ULID, key, value string) error
}

// PlayerRepository is the narrow whole-struct player persistence surface the
// repo-backed player settings store needs. It is satisfied by
// auth.PlayerRepository (and by *postgres.PlayerRepository). Reads load the
// full player; Update persists the whole player, whole-struct-marshaling
// Preferences (including the opaque Plugins bag) to the players.preferences
// JSONB column.
type PlayerRepository interface {
	GetByID(ctx context.Context, id ulid.ULID) (*auth.Player, error)
	Update(ctx context.Context, player *auth.Player) error
}

// PlayerSettings implements PlayerSettingsStore. It is backed either by a
// read-only PlayerPrefsReader (NewPlayerSettingsStore) for the legacy host-
// partition path, or by a whole-struct PlayerRepository
// (NewRepoPlayerSettingsStore) whose plugin partition writes persist via a
// read-modify-write commit func. The two construction paths differ in write
// persistence — see each constructor's doc — and that difference is otherwise
// invisible at the PlayerSettingsStore interface (holomush-iokti.17 .11/.15).
type PlayerSettings struct {
	reader PlayerPrefsReader
	repo   PlayerRepository
}

// NewPlayerSettingsStore creates a PlayerSettingsStore backed by a
// PlayerPrefsReader.
//
// Persistence semantics: NON-PERSISTING for partition writes. Plugin/Host
// writes through the views returned by For() update in-memory maps only and are
// silently discarded when the handle goes out of scope (the commit func is
// nil). This path exists to serve bare host-partition READS for the resolution
// Chain; it is NOT a write-through store. Single-key host writes via the
// store-level SetString() DO persist (through the reader's
// SetPlayerPreferenceKey). Use NewRepoPlayerSettingsStore when plugin-partition
// writes must persist.
func NewPlayerSettingsStore(reader PlayerPrefsReader) *PlayerSettings {
	return &PlayerSettings{reader: reader}
}

// NewRepoPlayerSettingsStore creates a PlayerSettingsStore backed by the player
// repository.
//
// Persistence semantics: PERSISTING. Both plugin-partition AND host-partition
// writes through For() persist via a read-modify-write commit func: each write
// re-reads the player, merges only the mutated partitions into
// Preferences.Plugins / Preferences.Host, and Updates — so sibling partitions
// written by a separate For() call are not lost. This is the production
// write-through store; contrast NewPlayerSettingsStore, whose partition writes
// are in-memory only. Host writes persist into the Preferences.Host sub-bag,
// mirroring the character store (holomush-sl0ir.17); the store-level SetString
// host-key path remains unsupported on this store (use For().Host()).
func NewRepoPlayerSettingsStore(repo PlayerRepository) *PlayerSettings {
	return &PlayerSettings{repo: repo}
}

// For returns a plugin-partitioned Scoped handle for a specific player.
//
// Bare Settings reads on the returned handle target the host partition,
// reading the player's preferences as a flat dot-keyed map (keys like
// "scenes.focus.replay_tail_default") with namespace validation — preserving
// the legacy behavior the resolution Chain depends on. Plugin(name) narrows to
// that plugin's isolated, namespace-unvalidated partition.
//
// When the store is repo-backed, the handle's host partition is loaded from
// Preferences.Host and its plugin partitions from Preferences.Plugins, and both
// Host and Plugin writes persist via a non-nil commit func. When the store is
// reader-backed, the commit func is nil (writes are in-memory only). On any load
// failure the handle degrades to an empty, read-only view so bare reads and the
// Chain resolve to defaults rather than panicking.
//
// Concurrency: the returned handle is per-request — one For() call per request.
// Its in-memory partition maps are mutated without synchronization and MUST NOT
// be shared across goroutines. Lost-update safety across separate For() calls is
// provided by the commit func re-reading the player.
func (s *PlayerSettings) For(ctx context.Context, playerID ulid.ULID) Scoped {
	if s.repo != nil {
		return s.repoScopedFor(ctx, playerID)
	}
	return s.readerScopedFor(ctx, playerID)
}

// repoScopedFor loads the player via the repo, materializes its
// Preferences.Plugins bag into plugin partitions, and wires a commit func that
// persists plugin writes via read-modify-write.
func (s *PlayerSettings) repoScopedFor(ctx context.Context, playerID ulid.ULID) Scoped {
	player, err := s.repo.GetByID(ctx, playerID)
	if err != nil {
		// Fail closed: reads still resolve to defaults (Settings never errors),
		// but a write surfaces the load failure rather than silently dropping.
		slog.WarnContext(ctx, "player settings load failed",
			"player_id", playerID.String(), "error", err)
		return newFailClosedView(oops.With("player_id", playerID.String()).Wrap(err))
	}

	// host and plugins are the live maps the scopedView's Host()/Plugin()
	// writables mutate. The commit closure captures them directly so a write
	// serializes the touched partitions back.
	host := decodeHostPartition(ctx, "player", playerID, player.Preferences.Host)
	plugins := decodePluginPartitions(ctx, player.Preferences.Plugins)

	return newTrackedScopedView(host, plugins,
		func(dirty *dirtyTracker) func(ctx context.Context) error {
			return func(ctx context.Context) error {
				// Re-read so the merge runs against the latest persisted state, then
				// overwrite ONLY the partitions this view mutated. A clean-loaded
				// sibling partition is never re-serialized with its stale value, so a
				// concurrent For() handle that changed a different plugin keeps its
				// update (cross-plugin lost-update safety).
				fresh, err := s.repo.GetByID(ctx, playerID)
				if err != nil {
					return oops.With("player_id", playerID.String()).Wrap(err)
				}
				if fresh.Preferences.Plugins == nil {
					fresh.Preferences.Plugins = map[string]json.RawMessage{}
				}
				for plugin := range dirty.plugins {
					encoded, err := json.Marshal(plugins[plugin])
					if err != nil {
						return oops.With("player_id", playerID.String(), "plugin", plugin).Wrap(err)
					}
					fresh.Preferences.Plugins[plugin] = encoded
				}
				if dirty.host {
					encodedHost, err := json.Marshal(host)
					if err != nil {
						return oops.With("player_id", playerID.String()).Wrap(err)
					}
					fresh.Preferences.Host = encodedHost
				}
				if err := s.repo.Update(ctx, fresh); err != nil {
					return oops.With("player_id", playerID.String()).Wrap(err)
				}
				return nil
			}
		})
}

// readerScopedFor is the legacy reader-backed path: host partition only, no
// persistence.
func (s *PlayerSettings) readerScopedFor(ctx context.Context, playerID ulid.ULID) Scoped {
	emptyHost := func() Scoped {
		return newScopedView(map[string]json.RawMessage{})
	}

	raw, err := s.reader.GetPlayerPreferencesJSON(ctx, playerID)
	if err != nil {
		slog.DebugContext(ctx, "player settings read failed",
			"player_id", playerID.String(), "error", err)
		return emptyHost()
	}
	if raw == nil {
		return emptyHost()
	}

	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil {
		slog.DebugContext(ctx, "player settings JSON unmarshal failed",
			"player_id", playerID.String(), "error", err)
		return emptyHost()
	}
	return newScopedView(data)
}

// decodePluginPartitions materializes the opaque Preferences.Plugins bag
// (plugin -> serialized partition JSON) into the scopedView's nested map
// (plugin -> key -> raw value). A partition that fails to decode is skipped and
// logged; the host never interprets partition contents, but a malformed
// partition is unreadable and would otherwise fail the unmarshal.
func decodePluginPartitions(
	ctx context.Context, bag map[string]json.RawMessage,
) map[string]map[string]json.RawMessage {
	out := make(map[string]map[string]json.RawMessage, len(bag))
	for plugin, raw := range bag {
		var partition map[string]json.RawMessage
		if err := json.Unmarshal(raw, &partition); err != nil {
			slog.WarnContext(ctx, "skipping undecodable plugin settings partition",
				"plugin", plugin, "error", err)
			continue
		}
		out[plugin] = partition
	}
	return out
}

// SetString writes a single preference key for a player.
func (s *PlayerSettings) SetString(
	ctx context.Context, playerID ulid.ULID, key, value string,
) error {
	if s.reader == nil {
		// Repo-backed store: the host partition is the typed player-preferences
		// struct, not flat-key-writable through here. Plugin-partition writes go
		// through For(playerID).Plugin(name). Fail explicitly rather than
		// dereferencing a nil reader (which would panic).
		return oops.Code("PLAYER_SETTINGS_HOST_WRITE_UNSUPPORTED").
			With("key", key).With("player_id", playerID.String()).
			Errorf("host-key writes are not supported on the repo-backed player settings store")
	}
	if err := ValidateNamespace(key); err != nil {
		return oops.With("key", key).Wrap(err)
	}
	if err := s.reader.SetPlayerPreferenceKey(ctx, playerID, key, value); err != nil {
		return oops.With("key", key).With("player_id", playerID.String()).Wrap(err)
	}
	return nil
}

// jsonMapSettings implements Settings backed by a flat JSON map. Values may
// be JSON strings, numbers, or bools. String accessor returns the raw
// JSON-decoded string; int/bool/duration parse from the string
// representation.
//
// validateNamespace gates namespace validation on reads. The host partition
// sets it true (legacy behavior); plugin partitions set it false so
// plugin-private keys need no RegisteredNamespaces entry.
type jsonMapSettings struct {
	data              map[string]json.RawMessage
	validateNamespace bool
}

func (j *jsonMapSettings) StringN(ctx context.Context, key string) (string, bool) {
	if j.validateNamespace {
		if err := ValidateNamespace(key); err != nil {
			slog.DebugContext(ctx, "settings read: invalid namespace", "key", key, "error", err)
			return "", false
		}
	}
	raw, ok := j.data[key]
	if !ok {
		return "", false
	}
	// Try to unmarshal as a JSON string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	// Fall back to raw string (for numeric/bool JSON values).
	return string(raw), true
}

func (j *jsonMapSettings) IntN(ctx context.Context, key string) (int, bool) {
	if j.validateNamespace {
		if err := ValidateNamespace(key); err != nil {
			slog.DebugContext(ctx, "settings read: invalid namespace", "key", key, "error", err)
			return 0, false
		}
	}
	raw, ok := j.data[key]
	if !ok {
		return 0, false
	}
	// Try native JSON number first.
	var num json.Number
	if err := json.Unmarshal(raw, &num); err == nil {
		n, err := num.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	}
	// Fall back to string parse.
	s, ok := j.StringN(ctx, key)
	if !ok {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return v, true
}

func (j *jsonMapSettings) BoolN(ctx context.Context, key string) (value, ok bool) {
	if j.validateNamespace {
		if err := ValidateNamespace(key); err != nil {
			slog.DebugContext(ctx, "settings read: invalid namespace", "key", key, "error", err)
			return false, false
		}
	}
	raw, ok := j.data[key]
	if !ok {
		return false, false
	}
	// Try native JSON bool first.
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b, true
	}
	// Fall back to string parse.
	s, ok := j.StringN(ctx, key)
	if !ok {
		return false, false
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		return false, false
	}
	return v, true
}

func (j *jsonMapSettings) DurationN(ctx context.Context, key string) (time.Duration, bool) {
	// Namespace validation happens inside StringN; no need to duplicate here.
	s, ok := j.StringN(ctx, key)
	if !ok {
		return 0, false
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return 0, false
	}
	return v, true
}

func (j *jsonMapSettings) StringSliceN(ctx context.Context, key string) ([]string, bool) {
	if j.validateNamespace {
		if err := ValidateNamespace(key); err != nil {
			slog.DebugContext(ctx, "settings read: invalid namespace", "key", key, "error", err)
			return nil, false
		}
	}
	raw, ok := j.data[key]
	if !ok {
		return nil, false
	}
	var v []string
	if err := json.Unmarshal(raw, &v); err != nil {
		slog.DebugContext(ctx, "settings read: string slice unmarshal failed", "key", key, "error", err)
		return nil, false
	}
	return v, true
}

// emptySettings always returns (zero, false) for all reads.
type emptySettings struct{}

func (e *emptySettings) StringN(context.Context, string) (string, bool)          { return "", false }
func (e *emptySettings) IntN(context.Context, string) (int, bool)                { return 0, false }
func (e *emptySettings) BoolN(context.Context, string) (value, ok bool)          { return false, false }
func (e *emptySettings) DurationN(context.Context, string) (time.Duration, bool) { return 0, false }
func (e *emptySettings) StringSliceN(context.Context, string) ([]string, bool)   { return nil, false }

// Compile-time interface checks.
var (
	_ Settings = (*jsonMapSettings)(nil)
	_ Settings = (*emptySettings)(nil)
)
