// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRateLimiter(t *testing.T) {
	t.Run("creates limiter with default values", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{})

		// Should use defaults
		assert.Equal(t, DefaultBurstCapacity, rl.burstCapacity)
		assert.Equal(t, DefaultSustainedRate, rl.sustainedRate)
	})

	t.Run("creates limiter with custom values", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 20,
			SustainedRate: 5.0,
		})

		assert.Equal(t, 20, rl.burstCapacity)
		assert.Equal(t, 5.0, rl.sustainedRate)
	})

	t.Run("zero burst capacity uses default", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 0,
		})
		assert.Equal(t, DefaultBurstCapacity, rl.burstCapacity)

		rl2 := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: -5,
		})
		assert.Equal(t, DefaultBurstCapacity, rl2.burstCapacity)
	})

	t.Run("zero sustained rate uses default", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			SustainedRate: 0,
		})
		assert.Equal(t, DefaultSustainedRate, rl.sustainedRate)

		rl2 := NewRateLimiter(RateLimiterConfig{
			SustainedRate: -1.0,
		})
		assert.Equal(t, DefaultSustainedRate, rl2.sustainedRate)
	})
}

func TestRateLimiter_Allow(t *testing.T) {
	sessionID := ulid.Make()

	t.Run("allows commands up to burst capacity", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 3,
			SustainedRate: 1.0,
		})

		// First 3 commands should be allowed
		allowed1, cooldown1 := rl.Allow(sessionID)
		assert.True(t, allowed1)
		assert.Equal(t, int64(0), cooldown1)

		allowed2, cooldown2 := rl.Allow(sessionID)
		assert.True(t, allowed2)
		assert.Equal(t, int64(0), cooldown2)

		allowed3, cooldown3 := rl.Allow(sessionID)
		assert.True(t, allowed3)
		assert.Equal(t, int64(0), cooldown3)

		// 4th command should be rate limited
		allowed4, cooldown4 := rl.Allow(sessionID)
		assert.False(t, allowed4)
		assert.Greater(t, cooldown4, int64(0))
	})

	t.Run("returns correct cooldown time", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 1,
			SustainedRate: 2.0, // 2 tokens/second = 500ms per token
		})

		// First command uses the token
		allowed1, _ := rl.Allow(sessionID)
		require.True(t, allowed1)

		// Second command should be rate limited with ~500ms cooldown
		allowed2, cooldownMs := rl.Allow(sessionID)
		assert.False(t, allowed2)
		// Should be roughly 500ms, allow some tolerance for test timing
		assert.InDelta(t, 500, cooldownMs, 50)
	})

	t.Run("different sessions have independent limits", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 1,
			SustainedRate: 1.0,
		})

		session1 := ulid.Make()
		session2 := ulid.Make()

		// Session 1 uses its token
		allowed1, _ := rl.Allow(session1)
		require.True(t, allowed1)

		// Session 1 is now rate limited
		allowed2, _ := rl.Allow(session1)
		assert.False(t, allowed2)

		// Session 2 should still have its own token
		allowed3, _ := rl.Allow(session2)
		assert.True(t, allowed3)
	})

	t.Run("tokens refill over time", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 1,
			SustainedRate: 100.0, // 100 tokens/second = 10ms per token
		})

		// Use the token
		allowed1, _ := rl.Allow(sessionID)
		require.True(t, allowed1)

		// Should be rate limited
		allowed2, _ := rl.Allow(sessionID)
		assert.False(t, allowed2)

		// Wait for token to refill (10ms + buffer)
		time.Sleep(15 * time.Millisecond)

		// Should now be allowed
		allowed3, _ := rl.Allow(sessionID)
		assert.True(t, allowed3)
	})

	t.Run("tokens do not exceed burst capacity", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 2,
			SustainedRate: 1000.0, // Very fast refill
		})

		// Use both tokens
		rl.Allow(sessionID)
		rl.Allow(sessionID)

		// Wait for potential refill
		time.Sleep(20 * time.Millisecond)

		// Should only have 2 tokens available (burst capacity)
		allowed1, _ := rl.Allow(sessionID)
		assert.True(t, allowed1)
		allowed2, _ := rl.Allow(sessionID)
		assert.True(t, allowed2)
		allowed3, _ := rl.Allow(sessionID)
		assert.False(t, allowed3)
	})
}

func TestRateLimiter_Cleanup(t *testing.T) {
	t.Run("removes stale sessions", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 10,
			SustainedRate: 1.0,
		})

		// Create sessions
		session1 := ulid.Make()
		session2 := ulid.Make()

		rl.Allow(session1)
		rl.Allow(session2)

		// Both sessions should exist
		assert.Equal(t, 2, rl.SessionCount())

		// Cleanup with 0 max age should remove both (they're > 0 old)
		time.Sleep(1 * time.Millisecond)
		rl.Cleanup(0)
		assert.Equal(t, 0, rl.SessionCount())
	})

	t.Run("keeps recent sessions", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 10,
			SustainedRate: 1.0,
		})

		session := ulid.Make()
		rl.Allow(session)

		// Cleanup with large max age should keep the session
		rl.Cleanup(time.Hour)
		assert.Equal(t, 1, rl.SessionCount())
	})
}

func TestRateLimiter_Concurrency(_ *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		BurstCapacity: 100,
		SustainedRate: 10.0,
	})

	sessionID := ulid.Make()
	done := make(chan bool, 10)

	// Run 10 goroutines each making 20 requests
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 20; j++ {
				rl.Allow(sessionID)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic or race (run with -race flag)
}
