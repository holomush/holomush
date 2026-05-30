// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/samber/oops"
)

// ErrNotFound is returned by SystemInfoStore when a key does not exist.
var ErrNotFound = errors.New("setting not found")

// SystemInfoStore is the narrow interface for reading and writing
// key/value pairs from holomush_system_info. Satisfied by
// *store.PostgresEventStore.
type SystemInfoStore interface {
	GetSystemInfo(ctx context.Context, key string) (string, error)
	SetSystemInfo(ctx context.Context, key, value string) error
}

// SystemInfoAdapter wraps a store that uses its own not-found sentinel,
// mapping it to settings.ErrNotFound for consistent handling.
type SystemInfoAdapter struct {
	Store       SystemInfoStore
	NotFoundErr error // the store's not-found sentinel (e.g., store.ErrSystemInfoNotFound)
}

// GetSystemInfo delegates to the underlying store, mapping the store's
// not-found error to settings.ErrNotFound.
func (a *SystemInfoAdapter) GetSystemInfo(ctx context.Context, key string) (string, error) {
	v, err := a.Store.GetSystemInfo(ctx, key)
	if err != nil && a.NotFoundErr != nil && errors.Is(err, a.NotFoundErr) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", oops.With("key", key).Wrap(err)
	}
	return v, nil
}

// SetSystemInfo delegates to the underlying store.
func (a *SystemInfoAdapter) SetSystemInfo(ctx context.Context, key, value string) error {
	if err := a.Store.SetSystemInfo(ctx, key, value); err != nil {
		return oops.With("key", key).Wrap(err)
	}
	return nil
}

// postgresGameSettings implements GameSettings backed by holomush_system_info.
type postgresGameSettings struct {
	store SystemInfoStore
}

// NewGameSettings creates a GameSettings backed by the given SystemInfoStore.
func NewGameSettings(store SystemInfoStore) GameSettings {
	return &postgresGameSettings{store: store}
}

func (g *postgresGameSettings) StringN(ctx context.Context, key string) (string, bool) {
	v, err := g.store.GetSystemInfo(ctx, key)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			slog.DebugContext(ctx, "game settings read failed",
				"key", key, "error", err)
		}
		return "", false
	}
	return v, true
}

func (g *postgresGameSettings) IntN(ctx context.Context, key string) (int, bool) {
	s, ok := g.StringN(ctx, key)
	if !ok {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		slog.DebugContext(ctx, "game settings int parse failed",
			"key", key, "raw", s, "error", err)
		return 0, false
	}
	return v, true
}

func (g *postgresGameSettings) BoolN(ctx context.Context, key string) (value, ok bool) {
	s, ok := g.StringN(ctx, key)
	if !ok {
		return false, false
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		slog.DebugContext(ctx, "game settings bool parse failed",
			"key", key, "raw", s, "error", err)
		return false, false
	}
	return v, true
}

func (g *postgresGameSettings) DurationN(ctx context.Context, key string) (time.Duration, bool) {
	s, ok := g.StringN(ctx, key)
	if !ok {
		return 0, false
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		slog.DebugContext(ctx, "game settings duration parse failed",
			"key", key, "raw", s, "error", err)
		return 0, false
	}
	return v, true
}

func (g *postgresGameSettings) StringSliceN(ctx context.Context, key string) ([]string, bool) {
	s, ok := g.StringN(ctx, key)
	if !ok {
		return nil, false
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		slog.DebugContext(ctx, "game settings string-slice parse failed",
			"key", key, "raw", s, "error", err)
		return nil, false
	}
	return out, true
}

func (g *postgresGameSettings) SetString(ctx context.Context, key, value string) error {
	if err := ValidateNamespace(key); err != nil {
		return oops.With("key", key).Wrap(err)
	}
	if err := g.store.SetSystemInfo(ctx, key, value); err != nil {
		return oops.With("key", key).Wrap(err)
	}
	return nil
}

// SetStringSlice stores values as a JSON-array-encoded string under key,
// consistent with StringSliceN's decode path. Like SetString, the key is
// namespace-validated (host partition).
func (g *postgresGameSettings) SetStringSlice(ctx context.Context, key string, values []string) error {
	if err := ValidateNamespace(key); err != nil {
		return oops.With("key", key).Wrap(err)
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return oops.With("key", key).Wrap(err)
	}
	if err := g.store.SetSystemInfo(ctx, key, string(encoded)); err != nil {
		return oops.With("key", key).Wrap(err)
	}
	return nil
}

// Host returns a Writable over the bare host keyspace. Reads and writes are
// namespace-validated, identical to the bare postgresGameSettings methods —
// this is the legacy operator-tooling surface.
func (g *postgresGameSettings) Host() Writable {
	return g
}

// Owner returns a Writable over the named plugin's isolated game-scope
// partition. Every key is transparently prefixed with "plugin/<name>/" before
// it reaches holomush_system_info, so an owner's keys can neither collide with
// nor be read by the host partition or any other owner. Owner keys are NOT
// namespace-validated: a plugin owns its keyspace and may use any key shape
// (e.g. "content.cw_taxonomy", which is not a registered host namespace).
func (g *postgresGameSettings) Owner(name string) Writable {
	return &gameOwnerSettings{store: g.store, prefix: ReservedNamespace + "/" + name + "/"}
}

// gameOwnerSettings is the owner-partitioned Writable over holomush_system_info.
// It wraps the same SystemInfoStore as postgresGameSettings but prefixes every
// key with "plugin/<name>/" and skips namespace validation (the plugin owns its
// keyspace). Storage encoding matches the host game scope: scalars are stored as
// raw strings, slices as JSON-array-encoded strings.
type gameOwnerSettings struct {
	store  SystemInfoStore
	prefix string
}

// readString fetches the prefixed key as a raw string, mirroring
// postgresGameSettings.StringN but without namespace validation.
func (o *gameOwnerSettings) readString(ctx context.Context, key string) (string, bool) {
	v, err := o.store.GetSystemInfo(ctx, o.prefix+key)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			slog.DebugContext(ctx, "game owner settings read failed",
				"key", key, "prefix", o.prefix, "error", err)
		}
		return "", false
	}
	return v, true
}

func (o *gameOwnerSettings) StringN(ctx context.Context, key string) (string, bool) {
	return o.readString(ctx, key)
}

func (o *gameOwnerSettings) IntN(ctx context.Context, key string) (int, bool) {
	s, ok := o.readString(ctx, key)
	if !ok {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		slog.DebugContext(ctx, "game owner settings int parse failed",
			"key", key, "raw", s, "error", err)
		return 0, false
	}
	return v, true
}

func (o *gameOwnerSettings) BoolN(ctx context.Context, key string) (value, ok bool) {
	s, ok := o.readString(ctx, key)
	if !ok {
		return false, false
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		slog.DebugContext(ctx, "game owner settings bool parse failed",
			"key", key, "raw", s, "error", err)
		return false, false
	}
	return v, true
}

func (o *gameOwnerSettings) DurationN(ctx context.Context, key string) (time.Duration, bool) {
	s, ok := o.readString(ctx, key)
	if !ok {
		return 0, false
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		slog.DebugContext(ctx, "game owner settings duration parse failed",
			"key", key, "raw", s, "error", err)
		return 0, false
	}
	return v, true
}

func (o *gameOwnerSettings) StringSliceN(ctx context.Context, key string) ([]string, bool) {
	s, ok := o.readString(ctx, key)
	if !ok {
		return nil, false
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		slog.DebugContext(ctx, "game owner settings string-slice parse failed",
			"key", key, "raw", s, "error", err)
		return nil, false
	}
	return out, true
}

// SetString stores value as a raw string under the prefixed key. No namespace
// validation (plugin owns its keyspace).
func (o *gameOwnerSettings) SetString(ctx context.Context, key, value string) error {
	if err := o.store.SetSystemInfo(ctx, o.prefix+key, value); err != nil {
		return oops.With("key", key).With("prefix", o.prefix).Wrap(err)
	}
	return nil
}

// SetStringSlice stores values as a JSON-array-encoded string under the
// prefixed key so StringSliceN round-trips. No namespace validation.
func (o *gameOwnerSettings) SetStringSlice(ctx context.Context, key string, values []string) error {
	encoded, err := json.Marshal(values)
	if err != nil {
		return oops.With("key", key).With("prefix", o.prefix).Wrap(err)
	}
	if err := o.store.SetSystemInfo(ctx, o.prefix+key, string(encoded)); err != nil {
		return oops.With("key", key).With("prefix", o.prefix).Wrap(err)
	}
	return nil
}

// Compile-time interface checks.
var (
	_ Scoped   = (*postgresGameSettings)(nil)
	_ Writable = (*postgresGameSettings)(nil)
	_ Writable = (*gameOwnerSettings)(nil)
)
