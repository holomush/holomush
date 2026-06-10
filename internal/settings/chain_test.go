// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/settings"
)

// Compile-time interface check: *settings.Chain must satisfy settings.Settings.
var _ settings.Settings = (*settings.Chain)(nil)

// stubSettings implements Settings for testing. Stores values as strings
// keyed by setting name. Returns false for missing keys.
type stubSettings struct {
	strings   map[string]string
	ints      map[string]int
	bools     map[string]bool
	durations map[string]time.Duration
	slices    map[string][]string
}

func newStub() *stubSettings {
	return &stubSettings{
		strings:   make(map[string]string),
		ints:      make(map[string]int),
		bools:     make(map[string]bool),
		durations: make(map[string]time.Duration),
		slices:    make(map[string][]string),
	}
}

func (s *stubSettings) StringN(_ context.Context, key string) (string, bool) {
	v, ok := s.strings[key]
	return v, ok
}

func (s *stubSettings) IntN(_ context.Context, key string) (int, bool) {
	v, ok := s.ints[key]
	return v, ok
}

func (s *stubSettings) BoolN(_ context.Context, key string) (bool, bool) {
	v, ok := s.bools[key]
	return v, ok
}

func (s *stubSettings) DurationN(_ context.Context, key string) (time.Duration, bool) {
	v, ok := s.durations[key]
	return v, ok
}

func (s *stubSettings) StringSliceN(_ context.Context, key string) ([]string, bool) {
	v, ok := s.slices[key]
	return v, ok
}

func TestChainStringNReturnsFirstMatchInScopeOrder(t *testing.T) {
	ctx := context.Background()
	player := newStub()
	player.strings["scenes.focus.mode"] = "player-value"
	game := newStub()
	game.strings["scenes.focus.mode"] = "game-value"

	chain := settings.NewChain(player, game)
	v, ok := chain.StringN(ctx, "scenes.focus.mode")
	assert.True(t, ok)
	assert.Equal(t, "player-value", v)
}

func TestChainStringNFallsToLaterScope(t *testing.T) {
	ctx := context.Background()
	player := newStub() // no value set
	game := newStub()
	game.strings["scenes.focus.mode"] = "game-value"

	chain := settings.NewChain(player, game)
	v, ok := chain.StringN(ctx, "scenes.focus.mode")
	assert.True(t, ok)
	assert.Equal(t, "game-value", v)
}

func TestChainStringNReturnsFalseWhenNoScopeHasKey(t *testing.T) {
	ctx := context.Background()
	chain := settings.NewChain(newStub(), newStub())
	_, ok := chain.StringN(ctx, "scenes.focus.mode")
	assert.False(t, ok)
}

func TestChainIntNReturnsFirstMatch(t *testing.T) {
	ctx := context.Background()
	char := newStub()
	char.ints["scenes.focus.replay_tail_default"] = 5
	player := newStub()
	player.ints["scenes.focus.replay_tail_default"] = 7
	game := newStub()
	game.ints["scenes.focus.replay_tail_default"] = 3

	chain := settings.NewChain(char, player, game)
	v, ok := chain.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.True(t, ok)
	assert.Equal(t, 5, v)
}

func TestChainIntNDistinguishesZeroFromUnset(t *testing.T) {
	ctx := context.Background()
	player := newStub()
	player.ints["scenes.focus.replay_tail_default"] = 0
	game := newStub()
	game.ints["scenes.focus.replay_tail_default"] = 3

	chain := settings.NewChain(player, game)
	v, ok := chain.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.True(t, ok)
	assert.Equal(t, 0, v, "explicit 0 must win over game default 3")
}

func TestChainBoolNReturnsFirstMatch(t *testing.T) {
	ctx := context.Background()
	player := newStub()
	player.bools["auth.auto_login"] = true
	game := newStub()
	game.bools["auth.auto_login"] = false

	chain := settings.NewChain(player, game)
	v, ok := chain.BoolN(ctx, "auth.auto_login")
	assert.True(t, ok)
	assert.True(t, v)
}

func TestChainDurationNReturnsFirstMatch(t *testing.T) {
	ctx := context.Background()
	player := newStub()
	player.durations["core.session_timeout"] = 30 * time.Second
	game := newStub()
	game.durations["core.session_timeout"] = 60 * time.Second

	chain := settings.NewChain(player, game)
	v, ok := chain.DurationN(ctx, "core.session_timeout")
	assert.True(t, ok)
	assert.Equal(t, 30*time.Second, v)
}

func TestChainSkipsNilScopes(t *testing.T) {
	ctx := context.Background()
	game := newStub()
	game.strings["scenes.focus.mode"] = "game-value"

	// nil represents CharacterSettingsStore.For returning nil
	chain := settings.NewChain(nil, nil, game)
	v, ok := chain.StringN(ctx, "scenes.focus.mode")
	assert.True(t, ok)
	assert.Equal(t, "game-value", v)
}

func TestChainEmptyScopesReturnsFalse(t *testing.T) {
	ctx := context.Background()
	chain := settings.NewChain()
	_, ok := chain.StringN(ctx, "scenes.focus.mode")
	assert.False(t, ok)
}


func TestChainResolutionOrderMatchesSpecCharacterPlayerGame(t *testing.T) {
	ctx := context.Background()

	char := newStub()
	char.ints["scenes.focus.replay_tail_default"] = 2
	player := newStub()
	player.ints["scenes.focus.replay_tail_default"] = 5
	game := newStub()
	game.ints["scenes.focus.replay_tail_default"] = 3

	chain := settings.NewChain(char, player, game)
	v, ok := chain.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.True(t, ok)
	assert.Equal(t, 2, v, "character scope (first) must win")
}

func TestChainResolutionSkipsCharacterFallsToPlayer(t *testing.T) {
	ctx := context.Background()

	char := newStub() // no value
	player := newStub()
	player.ints["scenes.focus.replay_tail_default"] = 5
	game := newStub()
	game.ints["scenes.focus.replay_tail_default"] = 3

	chain := settings.NewChain(char, player, game)
	v, ok := chain.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.True(t, ok)
	assert.Equal(t, 5, v, "player scope must win when character unset")
}

func TestChainResolutionSkipsCharacterAndPlayerFallsToGame(t *testing.T) {
	ctx := context.Background()

	char := newStub()   // no value
	player := newStub() // no value
	game := newStub()
	game.ints["scenes.focus.replay_tail_default"] = 3

	chain := settings.NewChain(char, player, game)
	v, ok := chain.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.True(t, ok)
	assert.Equal(t, 3, v, "game scope must win when character and player unset")
}

func TestChainResolutionAllUnsetReturnsFalse(t *testing.T) {
	ctx := context.Background()

	chain := settings.NewChain(newStub(), newStub(), newStub())
	_, ok := chain.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.False(t, ok, "all-unset chain must return false")
}

func TestChainWithNilCharacterScopeFallsToPlayer(t *testing.T) {
	ctx := context.Background()

	player := newStub()
	player.ints["scenes.focus.replay_tail_default"] = 7
	game := newStub()
	game.ints["scenes.focus.replay_tail_default"] = 3

	// nil simulates CharacterSettingsStore.For returning nil
	chain := settings.NewChain(nil, player, game)
	v, ok := chain.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.True(t, ok)
	assert.Equal(t, 7, v)
}

func TestChainBoolNReturnsFalseWhenNoScopeHasKey(t *testing.T) {
	ctx := context.Background()
	chain := settings.NewChain(newStub(), newStub())
	_, ok := chain.BoolN(ctx, "auth.auto_login")
	assert.False(t, ok)
}

func TestChainDurationNReturnsFalseWhenNoScopeHasKey(t *testing.T) {
	ctx := context.Background()
	chain := settings.NewChain(newStub(), newStub())
	_, ok := chain.DurationN(ctx, "core.session_timeout")
	assert.False(t, ok)
}

func TestChainDurationNFallsToLaterScope(t *testing.T) {
	ctx := context.Background()
	player := newStub() // no duration set
	game := newStub()
	game.durations["core.session_timeout"] = 60 * time.Second

	chain := settings.NewChain(player, game)
	v, ok := chain.DurationN(ctx, "core.session_timeout")
	assert.True(t, ok)
	assert.Equal(t, 60*time.Second, v)
}

func TestChainBoolNFallsToLaterScope(t *testing.T) {
	ctx := context.Background()
	player := newStub() // no bool set
	game := newStub()
	game.bools["auth.auto_login"] = true

	chain := settings.NewChain(player, game)
	v, ok := chain.BoolN(ctx, "auth.auto_login")
	assert.True(t, ok)
	assert.True(t, v)
}

func TestChainStringSliceNReturnsFirstMatchInScopeOrder(t *testing.T) {
	ctx := context.Background()
	player := newStub()
	player.slices["scenes.focus.tags"] = []string{"player"}
	game := newStub()
	game.slices["scenes.focus.tags"] = []string{"game"}

	chain := settings.NewChain(player, game)
	v, ok := chain.StringSliceN(ctx, "scenes.focus.tags")
	assert.True(t, ok)
	assert.Equal(t, []string{"player"}, v)
}

func TestChainStringSliceNFallsToLaterScope(t *testing.T) {
	ctx := context.Background()
	player := newStub() // no value set
	game := newStub()
	game.slices["scenes.focus.tags"] = []string{"game"}

	chain := settings.NewChain(player, game)
	v, ok := chain.StringSliceN(ctx, "scenes.focus.tags")
	assert.True(t, ok)
	assert.Equal(t, []string{"game"}, v)
}

func TestChainStringSliceNReturnsFalseWhenNoScopeHasKey(t *testing.T) {
	ctx := context.Background()
	chain := settings.NewChain(newStub(), newStub())
	v, ok := chain.StringSliceN(ctx, "scenes.focus.tags")
	assert.False(t, ok)
	assert.Nil(t, v)
}

func TestChainTypeMixStringFromCharIntFromGame(t *testing.T) {
	ctx := context.Background()

	char := newStub()
	char.strings["scenes.focus.mode"] = "bounded"
	game := newStub()
	game.ints["scenes.focus.replay_tail_default"] = 3

	chain := settings.NewChain(char, game)

	mode, ok := chain.StringN(ctx, "scenes.focus.mode")
	assert.True(t, ok)
	assert.Equal(t, "bounded", mode)

	tail, ok := chain.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.True(t, ok)
	assert.Equal(t, 3, tail)
}
