// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings

import (
	"context"
	"time"
)

// Chain composes scoped Settings instances with first-match-wins
// resolution. Typical construction for a session context:
//
//	chain := settings.NewChain(
//	    characterStore.For(ctx, info.CharacterID),
//	    playerStore.For(ctx, info.PlayerID),
//	    gameSettings,
//	)
//	tail, ok := chain.IntN(ctx, "scenes.focus.replay_tail_default")
//
// Chain itself implements Settings, so the resolution chain is a Settings
// instance that can be passed to consumers unaware of the scope layering.
type Chain struct {
	scopes []Settings
}

// NewChain constructs a Chain from scopes in resolution priority order
// (most-specific first). Nil scopes are accepted and skipped (null-object
// pattern), making it safe to pass a nil CharacterSettingsStore result.
func NewChain(scopes ...Settings) *Chain {
	filtered := make([]Settings, 0, len(scopes))
	for _, s := range scopes {
		if s != nil {
			filtered = append(filtered, s)
		}
	}
	return &Chain{scopes: filtered}
}

// StringN returns the first non-absent string value across scopes.
func (c *Chain) StringN(ctx context.Context, key string) (string, bool) {
	for _, s := range c.scopes {
		if v, ok := s.StringN(ctx, key); ok {
			return v, true
		}
	}
	return "", false
}

// IntN returns the first non-absent int value across scopes.
func (c *Chain) IntN(ctx context.Context, key string) (int, bool) {
	for _, s := range c.scopes {
		if v, ok := s.IntN(ctx, key); ok {
			return v, true
		}
	}
	return 0, false
}

// BoolN returns the first non-absent bool value across scopes.
func (c *Chain) BoolN(ctx context.Context, key string) (value, ok bool) {
	for _, s := range c.scopes {
		if v, ok := s.BoolN(ctx, key); ok {
			return v, true
		}
	}
	return false, false
}

// DurationN returns the first non-absent duration value across scopes.
func (c *Chain) DurationN(ctx context.Context, key string) (time.Duration, bool) {
	for _, s := range c.scopes {
		if v, ok := s.DurationN(ctx, key); ok {
			return v, true
		}
	}
	return 0, false
}

// StringSliceN returns the first non-absent string-slice value across scopes.
func (c *Chain) StringSliceN(ctx context.Context, key string) ([]string, bool) {
	for _, s := range c.scopes {
		if v, ok := s.StringSliceN(ctx, key); ok {
			return v, true
		}
	}
	return nil, false
}
