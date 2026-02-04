// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Default rate limiting values per spec.
const (
	// DefaultBurstCapacity is the maximum number of commands a session can
	// execute in a burst before rate limiting kicks in.
	DefaultBurstCapacity = 10

	// DefaultSustainedRate is the number of commands per second allowed as
	// sustained rate (token refill rate).
	DefaultSustainedRate = 2.0

	// MinBurstCapacity ensures burst capacity is at least 1.
	MinBurstCapacity = 1

	// MinSustainedRate ensures sustained rate is at least 0.1 tokens/second.
	MinSustainedRate = 0.1

	// CapabilityRateLimitBypass is the capability that exempts a character
	// from rate limiting when granted.
	CapabilityRateLimitBypass = "admin.ratelimit.bypass"
)

// RateLimiterConfig configures the rate limiter.
type RateLimiterConfig struct {
	// BurstCapacity is the maximum number of commands allowed in a burst.
	// Defaults to DefaultBurstCapacity (10) if zero or negative.
	BurstCapacity int

	// SustainedRate is the number of commands per second allowed as sustained rate.
	// Defaults to DefaultSustainedRate (2.0) if zero or negative.
	SustainedRate float64
}

// sessionBucket tracks rate limiting state for a single session using the
// token bucket algorithm.
type sessionBucket struct {
	tokens    float64
	lastCheck time.Time
}

// RateLimiter implements per-session rate limiting using a token bucket algorithm.
// It is safe for concurrent use.
type RateLimiter struct {
	mu            sync.Mutex
	sessions      map[ulid.ULID]*sessionBucket
	burstCapacity int
	sustainedRate float64 // tokens per second
}

// NewRateLimiter creates a new rate limiter with the given configuration.
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	burstCapacity := cfg.BurstCapacity
	if burstCapacity <= 0 {
		// Use default when not specified
		burstCapacity = DefaultBurstCapacity
	}
	// Ensure minimum burst capacity
	if burstCapacity < MinBurstCapacity {
		burstCapacity = MinBurstCapacity
	}

	sustainedRate := cfg.SustainedRate
	if sustainedRate <= 0 {
		// Use default when not specified
		sustainedRate = DefaultSustainedRate
	}
	// Ensure minimum sustained rate
	if sustainedRate < MinSustainedRate {
		sustainedRate = MinSustainedRate
	}

	return &RateLimiter{
		sessions:      make(map[ulid.ULID]*sessionBucket),
		burstCapacity: burstCapacity,
		sustainedRate: sustainedRate,
	}
}

// Allow checks if a command is allowed for the given session.
// Returns (allowed, cooldownMs) where:
//   - allowed: true if the command should be executed
//   - cooldownMs: milliseconds until the next token is available (0 if allowed)
//
// Each call to Allow consumes one token if available. Tokens refill at the
// sustained rate, up to the burst capacity.
func (rl *RateLimiter) Allow(sessionID ulid.ULID) (allowed bool, cooldownMs int64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	bucket, exists := rl.sessions[sessionID]
	if !exists {
		// New session starts with full bucket
		bucket = &sessionBucket{
			tokens:    float64(rl.burstCapacity),
			lastCheck: now,
		}
		rl.sessions[sessionID] = bucket
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(bucket.lastCheck).Seconds()
	bucket.tokens += elapsed * rl.sustainedRate
	if bucket.tokens > float64(rl.burstCapacity) {
		bucket.tokens = float64(rl.burstCapacity)
	}
	bucket.lastCheck = now

	// Check if we have a token available
	if bucket.tokens >= 1.0 {
		bucket.tokens -= 1.0
		return true, 0
	}

	// Calculate cooldown until next token
	deficit := 1.0 - bucket.tokens
	cooldownSeconds := deficit / rl.sustainedRate
	cooldownMs = int64(cooldownSeconds * 1000)

	return false, cooldownMs
}

// SessionCount returns the number of tracked sessions. Useful for testing and
// monitoring.
func (rl *RateLimiter) SessionCount() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return len(rl.sessions)
}

// Cleanup removes sessions that haven't been seen since maxAge ago.
// Should be called periodically to prevent memory leaks from disconnected
// sessions.
func (rl *RateLimiter) Cleanup(maxAge time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	threshold := time.Now().Add(-maxAge)
	for sessionID, bucket := range rl.sessions {
		if bucket.lastCheck.Before(threshold) {
			delete(rl.sessions, sessionID)
		}
	}
}
