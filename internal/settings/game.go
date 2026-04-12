// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings

import (
	"context"
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
	return v, err
}

// SetSystemInfo delegates to the underlying store.
func (a *SystemInfoAdapter) SetSystemInfo(ctx context.Context, key, value string) error {
	return a.Store.SetSystemInfo(ctx, key, value)
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

func (g *postgresGameSettings) BoolN(ctx context.Context, key string) (bool, bool) {
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

func (g *postgresGameSettings) SetString(ctx context.Context, key, value string) error {
	if err := ValidateNamespace(key); err != nil {
		return oops.With("key", key).Wrap(err)
	}
	return g.store.SetSystemInfo(ctx, key, value)
}
