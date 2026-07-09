// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// muteLoaderStub is a controllable SceneMuteLoader recording its invocations.
type muteLoaderStub struct {
	calls   atomic.Int64
	enabled bool
	muted   []string
	err     error
	// perChar overrides the flat enabled/muted/err on a per-character basis.
	perChar map[string]struct {
		enabled bool
		muted   []string
		err     error
	}
}

func (l *muteLoaderStub) load(_ context.Context, characterID, _ string) (bool, []string, error) {
	l.calls.Add(1)
	if l.perChar != nil {
		if v, ok := l.perChar[characterID]; ok {
			return v.enabled, v.muted, v.err
		}
	}
	return l.enabled, l.muted, l.err
}

func TestSceneMuteCacheSuppressesEverySceneWhenGlobalNotifyOff(t *testing.T) {
	loader := &muteLoaderStub{enabled: false, muted: nil}
	c := NewSceneMuteChecker(loader.load, time.Minute, time.Now)

	// Global notifications OFF suppresses any scene, muted or not.
	got, err := c.ShouldSuppress(context.Background(), "char-1", "player-1", "scene-A")
	require.NoError(t, err)
	assert.True(t, got, "global-off must suppress an unmuted scene")

	got, err = c.ShouldSuppress(context.Background(), "char-1", "player-1", "scene-Z")
	require.NoError(t, err)
	assert.True(t, got, "global-off must suppress any other scene too")
}

func TestSceneMuteCacheSuppressesMutedSceneWhenGlobalNotifyOn(t *testing.T) {
	loader := &muteLoaderStub{enabled: true, muted: []string{"scene-A"}}
	c := NewSceneMuteChecker(loader.load, time.Minute, time.Now)

	got, err := c.ShouldSuppress(context.Background(), "char-1", "player-1", "scene-A")
	require.NoError(t, err)
	assert.True(t, got, "a muted scene must be suppressed even with global notify on")
}

func TestSceneMuteCacheDeliversUnmutedSceneWhenGlobalNotifyOn(t *testing.T) {
	loader := &muteLoaderStub{enabled: true, muted: []string{"scene-A"}}
	c := NewSceneMuteChecker(loader.load, time.Minute, time.Now)

	got, err := c.ShouldSuppress(context.Background(), "char-1", "player-1", "scene-B")
	require.NoError(t, err)
	assert.False(t, got, "an unmuted scene with global notify on must be delivered")
}

func TestSceneMuteCacheServesRepeatCallsFromCacheWithinTTL(t *testing.T) {
	loader := &muteLoaderStub{enabled: true, muted: []string{"scene-A"}}
	c := NewSceneMuteChecker(loader.load, time.Minute, time.Now)

	for i := 0; i < 5; i++ {
		_, err := c.ShouldSuppress(context.Background(), "char-1", "player-1", "scene-A")
		require.NoError(t, err)
	}
	assert.Equal(t, int64(1), loader.calls.Load(), "repeated calls within TTL must hit the cache, not the loader")
}

func TestSceneMuteCacheRefreshesAfterTTLExpiry(t *testing.T) {
	loader := &muteLoaderStub{enabled: true, muted: []string{"scene-A"}}
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	c := NewSceneMuteChecker(loader.load, 30*time.Second, clock)

	_, err := c.ShouldSuppress(context.Background(), "char-1", "player-1", "scene-A")
	require.NoError(t, err)
	assert.Equal(t, int64(1), loader.calls.Load())

	// Still within TTL: cache hit.
	now = now.Add(29 * time.Second)
	_, err = c.ShouldSuppress(context.Background(), "char-1", "player-1", "scene-A")
	require.NoError(t, err)
	assert.Equal(t, int64(1), loader.calls.Load())

	// Past TTL: refresh.
	now = now.Add(2 * time.Second)
	_, err = c.ShouldSuppress(context.Background(), "char-1", "player-1", "scene-A")
	require.NoError(t, err)
	assert.Equal(t, int64(2), loader.calls.Load(), "a call past TTL must refresh from the loader")
}

func TestSceneMuteCacheFailsOpenOnLoaderErrorWithoutPoisoning(t *testing.T) {
	loader := &muteLoaderStub{err: assert.AnError}
	c := NewSceneMuteChecker(loader.load, time.Minute, time.Now)

	got, err := c.ShouldSuppress(context.Background(), "char-1", "player-1", "scene-A")
	require.Error(t, err, "loader error is surfaced to the caller")
	assert.False(t, got, "on loader error the answer defaults to deliver (fail-open)")

	// The error must not be cached: a subsequent (now-succeeding) load is attempted.
	loader.err = nil
	loader.enabled = true
	loader.muted = []string{"scene-A"}
	got, err = c.ShouldSuppress(context.Background(), "char-1", "player-1", "scene-A")
	require.NoError(t, err)
	assert.True(t, got, "after the error clears, the next call refreshes and suppresses the muted scene")
	assert.Equal(t, int64(2), loader.calls.Load(), "the error path must not poison the cache")
}

func TestSceneMuteCacheIsolatesStatePerCharacter(t *testing.T) {
	loader := &muteLoaderStub{perChar: map[string]struct {
		enabled bool
		muted   []string
		err     error
	}{
		"char-1": {enabled: true, muted: []string{"scene-A"}},
		"char-2": {enabled: true, muted: []string{"scene-B"}},
	}}
	c := NewSceneMuteChecker(loader.load, time.Minute, time.Now)

	// char-1 muted scene-A; scene-B is not muted for char-1.
	got, err := c.ShouldSuppress(context.Background(), "char-1", "player-1", "scene-A")
	require.NoError(t, err)
	assert.True(t, got)
	got, err = c.ShouldSuppress(context.Background(), "char-1", "player-1", "scene-B")
	require.NoError(t, err)
	assert.False(t, got, "char-1 must not inherit char-2's muted set")

	// char-2 muted scene-B; scene-A is not muted for char-2.
	got, err = c.ShouldSuppress(context.Background(), "char-2", "player-2", "scene-B")
	require.NoError(t, err)
	assert.True(t, got)
	got, err = c.ShouldSuppress(context.Background(), "char-2", "player-2", "scene-A")
	require.NoError(t, err)
	assert.False(t, got, "char-2 must not inherit char-1's muted set")
}
