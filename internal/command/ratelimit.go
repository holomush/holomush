// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
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

	// DefaultCleanupInterval is the interval at which the background goroutine
	// runs to clean up stale sessions.
	DefaultCleanupInterval = 5 * time.Minute

	// DefaultSessionMaxAge is the default maximum age for a session before it
	// is considered stale and eligible for cleanup.
	DefaultSessionMaxAge = time.Hour
)

// RateLimiterConfig configures the rate limiter.
type RateLimiterConfig struct {
	// BurstCapacity is the maximum number of commands allowed in a burst.
	// Defaults to DefaultBurstCapacity (10) if zero or negative.
	BurstCapacity int

	// SustainedRate is the number of commands per second allowed as sustained rate.
	// Defaults to DefaultSustainedRate (2.0) if zero or negative.
	SustainedRate float64

	// CleanupInterval is the interval at which background cleanup runs.
	// Defaults to DefaultCleanupInterval (5 minutes) if zero.
	CleanupInterval time.Duration

	// SessionMaxAge is the maximum age for a session before cleanup removes it.
	// Defaults to DefaultSessionMaxAge (1 hour) if zero.
	SessionMaxAge time.Duration
}

// sessionBucket tracks rate limiting state for a single session using the
// token bucket algorithm.
type sessionBucket struct {
	tokens    float64
	lastCheck time.Time
}

// RateLimiter implements per-session rate limiting using a token bucket algorithm.
// It is safe for concurrent use.
//
// The RateLimiter runs a background goroutine to periodically clean up stale
// sessions. Call Close() to stop the goroutine and release resources.
type RateLimiter struct {
	mu            sync.Mutex
	sessions      map[ulid.ULID]*sessionBucket
	burstCapacity int
	sustainedRate float64 // tokens per second
	sessionMaxAge time.Duration

	// Background cleanup
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Metrics gauge for session count (nil if no registry provided)
	sessionGauge prometheus.Gauge
}

// NewRateLimiter creates a new rate limiter with the given configuration.
// It starts a background goroutine for cleanup. Call Close() to stop it.
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	return newRateLimiter(cfg, nil)
}

// NewRateLimiterWithRegistry creates a new rate limiter and registers a
// session count gauge with the provided Prometheus registry.
// It starts a background goroutine for cleanup. Call Close() to stop it.
func NewRateLimiterWithRegistry(cfg RateLimiterConfig, reg prometheus.Registerer) *RateLimiter {
	return newRateLimiter(cfg, reg)
}

func newRateLimiter(cfg RateLimiterConfig, reg prometheus.Registerer) *RateLimiter {
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

	cleanupInterval := cfg.CleanupInterval
	if cleanupInterval <= 0 {
		cleanupInterval = DefaultCleanupInterval
	}

	sessionMaxAge := cfg.SessionMaxAge
	if sessionMaxAge <= 0 {
		sessionMaxAge = DefaultSessionMaxAge
	}

	rl := &RateLimiter{
		sessions:      make(map[ulid.ULID]*sessionBucket),
		burstCapacity: burstCapacity,
		sustainedRate: sustainedRate,
		sessionMaxAge: sessionMaxAge,
		stopChan:      make(chan struct{}),
	}

	// Register session gauge if registry provided
	if reg != nil {
		rl.sessionGauge = prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "holomush_ratelimiter_sessions",
			Help: "Current number of tracked rate limiter sessions",
		})
		reg.MustRegister(rl.sessionGauge)
	}

	// Start background cleanup goroutine
	rl.wg.Add(1)
	go rl.cleanupLoop(cleanupInterval)

	return rl
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
// This is called automatically by the background goroutine, but can also
// be called manually if immediate cleanup is desired.
func (rl *RateLimiter) Cleanup(maxAge time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	threshold := time.Now().Add(-maxAge)
	for sessionID, bucket := range rl.sessions {
		if bucket.lastCheck.Before(threshold) {
			delete(rl.sessions, sessionID)
		}
	}

	// Update metrics if gauge is registered
	if rl.sessionGauge != nil {
		rl.sessionGauge.Set(float64(len(rl.sessions)))
	}
}

// cleanupLoop runs periodic cleanup in the background.
func (rl *RateLimiter) cleanupLoop(interval time.Duration) {
	defer rl.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopChan:
			return
		case <-ticker.C:
			rl.Cleanup(rl.sessionMaxAge)
		}
	}
}

// Close stops the background cleanup goroutine and releases resources.
// It blocks until the goroutine has stopped.
func (rl *RateLimiter) Close() {
	close(rl.stopChan)
	rl.wg.Wait()
}
