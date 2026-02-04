// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRateLimiter(t *testing.T) {
	t.Run("creates limiter with default values", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{})
		defer rl.Close()

		// Should use defaults
		assert.Equal(t, DefaultBurstCapacity, rl.burstCapacity)
		assert.Equal(t, DefaultSustainedRate, rl.sustainedRate)
		assert.Equal(t, DefaultSessionMaxAge, rl.sessionMaxAge)
	})

	t.Run("creates limiter with custom values", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity:   20,
			SustainedRate:   5.0,
			CleanupInterval: 10 * time.Minute,
			SessionMaxAge:   2 * time.Hour,
		})
		defer rl.Close()

		assert.Equal(t, 20, rl.burstCapacity)
		assert.Equal(t, 5.0, rl.sustainedRate)
		assert.Equal(t, 2*time.Hour, rl.sessionMaxAge)
	})

	t.Run("zero burst capacity uses default", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 0,
		})
		defer rl.Close()
		assert.Equal(t, DefaultBurstCapacity, rl.burstCapacity)

		rl2 := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: -5,
		})
		defer rl2.Close()
		assert.Equal(t, DefaultBurstCapacity, rl2.burstCapacity)
	})

	t.Run("zero sustained rate uses default", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			SustainedRate: 0,
		})
		defer rl.Close()
		assert.Equal(t, DefaultSustainedRate, rl.sustainedRate)

		rl2 := NewRateLimiter(RateLimiterConfig{
			SustainedRate: -1.0,
		})
		defer rl2.Close()
		assert.Equal(t, DefaultSustainedRate, rl2.sustainedRate)
	})

	t.Run("sustained rate below minimum is clamped to minimum", func(t *testing.T) {
		// Test a value between 0 and MinSustainedRate (0.1)
		rl := NewRateLimiter(RateLimiterConfig{
			SustainedRate: 0.05, // Below minimum
		})
		defer rl.Close()
		assert.Equal(t, MinSustainedRate, rl.sustainedRate, "rate below minimum should be clamped")

		// Test exactly at minimum
		rl2 := NewRateLimiter(RateLimiterConfig{
			SustainedRate: MinSustainedRate,
		})
		defer rl2.Close()
		assert.Equal(t, MinSustainedRate, rl2.sustainedRate, "rate at minimum should be preserved")
	})

	t.Run("zero cleanup interval uses default", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			CleanupInterval: 0,
		})
		defer rl.Close()
		// We can't directly check the interval, but we can verify the limiter works
		assert.NotNil(t, rl.stopChan)
	})

	t.Run("zero session max age uses default", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			SessionMaxAge: 0,
		})
		defer rl.Close()
		assert.Equal(t, DefaultSessionMaxAge, rl.sessionMaxAge)
	})
}

func TestRateLimiter_Allow(t *testing.T) {
	sessionID := ulid.Make()

	t.Run("allows commands up to burst capacity", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 3,
			SustainedRate: 1.0,
		})
		defer rl.Close()

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
		defer rl.Close()

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
		defer rl.Close()

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
		defer rl.Close()

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
		defer rl.Close()

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
		defer rl.Close()

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
		defer rl.Close()

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
	defer rl.Close()

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

func TestRateLimiter_CloseDuringAllow(t *testing.T) {
	t.Run("no races or panics when Close called during concurrent Allow", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 100,
			SustainedRate: 10.0,
		})

		const numGoroutines = 20
		started := make(chan struct{})
		done := make(chan struct{}, numGoroutines)

		// Spawn goroutines that call Allow in a tight loop until Close fires.
		for range numGoroutines {
			go func() {
				sessionID := ulid.Make()
				<-started // wait for all goroutines to be ready
				for {
					rl.Allow(sessionID)
					// Check if stopChan is closed (Close was called).
					select {
					case <-rl.stopChan:
						done <- struct{}{}
						return
					default:
					}
				}
			}()
		}

		// Release all goroutines and let them hammer Allow().
		close(started)

		// Give goroutines time to start executing Allow() calls.
		time.Sleep(5 * time.Millisecond)

		// Close while Allow goroutines are still running.
		// Must not panic (sync.Once protects the channel close).
		assert.NotPanics(t, func() {
			rl.Close()
		})

		// Wait for all goroutines to observe the close and exit.
		for range numGoroutines {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("goroutine did not exit after Close")
			}
		}

		// After Close, Allow must still function without panic.
		// It returns whatever the token bucket state is; the key
		// invariant is no data race or panic.
		assert.NotPanics(t, func() {
			rl.Allow(ulid.Make())
		})
	})

	t.Run("multiple concurrent Close calls do not panic", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 10,
			SustainedRate: 1.0,
		})

		const numClosers = 10
		started := make(chan struct{})
		done := make(chan struct{}, numClosers)

		for range numClosers {
			go func() {
				<-started
				rl.Close()
				done <- struct{}{}
			}()
		}

		close(started)

		for range numClosers {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("concurrent Close did not return in time")
			}
		}
	})
}

func TestRateLimiter_BackgroundCleanup(t *testing.T) {
	t.Run("cleanup runs periodically and removes stale sessions", func(t *testing.T) {
		// Use very short intervals for testing
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity:   10,
			SustainedRate:   1.0,
			CleanupInterval: 20 * time.Millisecond,
			SessionMaxAge:   10 * time.Millisecond,
		})
		defer rl.Close()

		// Create a session
		session := ulid.Make()
		rl.Allow(session)
		assert.Equal(t, 1, rl.SessionCount())

		// Wait for background cleanup to run (interval + buffer)
		time.Sleep(50 * time.Millisecond)

		// Session should be cleaned up
		assert.Equal(t, 0, rl.SessionCount())
	})

	t.Run("active sessions are not cleaned up", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity:   10,
			SustainedRate:   1.0,
			CleanupInterval: 20 * time.Millisecond,
			SessionMaxAge:   100 * time.Millisecond, // Longer than test duration
		})
		defer rl.Close()

		session := ulid.Make()
		rl.Allow(session)
		assert.Equal(t, 1, rl.SessionCount())

		// Wait for cleanup to run
		time.Sleep(50 * time.Millisecond)

		// Session should still exist (not old enough)
		assert.Equal(t, 1, rl.SessionCount())
	})
}

func TestRateLimiter_Close(t *testing.T) {
	t.Run("stops background goroutine", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			CleanupInterval: 10 * time.Millisecond,
		})

		// Close should return without blocking
		done := make(chan struct{})
		go func() {
			rl.Close()
			close(done)
		}()

		select {
		case <-done:
			// Success - Close returned
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Close did not return in time")
		}
	})

	t.Run("is idempotent and does not panic on double close", func(t *testing.T) {
		rl := NewRateLimiter(RateLimiterConfig{
			CleanupInterval: 10 * time.Millisecond,
		})

		// First close should work
		rl.Close()

		// Second close should not panic
		assert.NotPanics(t, func() {
			rl.Close()
		})

		// Third close should also not panic
		assert.NotPanics(t, func() {
			rl.Close()
		})
	})
}

func TestRateLimiter_WithRegistry(t *testing.T) {
	t.Run("registers session gauge metric", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		rl := NewRateLimiterWithRegistry(RateLimiterConfig{
			BurstCapacity: 10,
			SustainedRate: 1.0,
		}, reg)
		defer rl.Close()

		// Create some sessions
		session1 := ulid.Make()
		session2 := ulid.Make()
		rl.Allow(session1)
		rl.Allow(session2)

		// Trigger cleanup to update gauge
		rl.Cleanup(time.Hour)

		// Gather metrics
		mfs, err := reg.Gather()
		require.NoError(t, err)

		// Find our metric
		var found bool
		for _, mf := range mfs {
			if mf.GetName() == "holomush_ratelimiter_sessions" {
				found = true
				require.Len(t, mf.GetMetric(), 1)
				assert.Equal(t, float64(2), mf.GetMetric()[0].GetGauge().GetValue())
			}
		}
		assert.True(t, found, "metric holomush_ratelimiter_sessions not found")
	})

	t.Run("gauge updates after cleanup removes sessions", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		rl := NewRateLimiterWithRegistry(RateLimiterConfig{
			BurstCapacity: 10,
			SustainedRate: 1.0,
		}, reg)
		defer rl.Close()

		// Create sessions
		session1 := ulid.Make()
		session2 := ulid.Make()
		rl.Allow(session1)
		rl.Allow(session2)

		// Cleanup with 0 max age removes all
		time.Sleep(1 * time.Millisecond)
		rl.Cleanup(0)

		// Gather metrics
		mfs, err := reg.Gather()
		require.NoError(t, err)

		for _, mf := range mfs {
			if mf.GetName() == "holomush_ratelimiter_sessions" {
				assert.Equal(t, float64(0), mf.GetMetric()[0].GetGauge().GetValue())
			}
		}
	})
}
