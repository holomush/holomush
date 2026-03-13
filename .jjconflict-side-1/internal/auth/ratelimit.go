// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"time"
)

// Rate limiting configuration.
const (
	// LockoutDuration is the time a user is locked out after too many failures.
	LockoutDuration = 15 * time.Minute

	// LockoutThreshold is the number of failures that triggers a lockout.
	LockoutThreshold = 7

	// CaptchaThreshold is the number of failures that triggers CAPTCHA requirement (web only).
	CaptchaThreshold = 4
)

// RateLimitResult contains the result of a rate limit check.
type RateLimitResult struct {
	// Delay is the time to wait before allowing another attempt.
	Delay time.Duration

	// RequiresCaptcha indicates the web client should require CAPTCHA.
	RequiresCaptcha bool

	// IsLockedOut indicates the account is temporarily locked.
	IsLockedOut bool

	// LockoutRemaining is the time until the lockout expires.
	LockoutRemaining time.Duration
}

// CheckFailures evaluates the rate limit state based on failure count.
// lockedUntil is the current lockout timestamp (nil if not locked).
func CheckFailures(failures int, lockedUntil *time.Time) RateLimitResult {
	result := RateLimitResult{}

	// Check existing lockout first
	if IsLockedOut(lockedUntil) {
		result.IsLockedOut = true
		result.LockoutRemaining = time.Until(*lockedUntil)
		return result
	}

	// Progressive delay: 2^(failures-1) seconds, max 32s before lockout
	if failures > 0 && failures < LockoutThreshold {
		result.Delay = time.Duration(1<<(failures-1)) * time.Second
		if result.Delay > 32*time.Second {
			result.Delay = 32 * time.Second
		}
	}

	// CAPTCHA required at 4+ failures (for web clients)
	if failures >= CaptchaThreshold && failures < LockoutThreshold {
		result.RequiresCaptcha = true
	}

	// Lockout at 7+ failures
	if failures >= LockoutThreshold {
		result.IsLockedOut = true
		result.LockoutRemaining = LockoutDuration
	}

	return result
}

// IsLockedOut returns true if the lockout time is in the future.
func IsLockedOut(lockedUntil *time.Time) bool {
	return lockedUntil != nil && lockedUntil.After(time.Now())
}

// ComputeLockoutTime returns the lockout timestamp for the given failure count.
// Returns nil if failures < LockoutThreshold.
func ComputeLockoutTime(failures int) *time.Time {
	if failures < LockoutThreshold {
		return nil
	}
	lockout := time.Now().Add(LockoutDuration)
	return &lockout
}

// ResetOnSuccess returns the values to set after a successful login.
// Returns 0 for failed_attempts and nil for locked_until.
func ResetOnSuccess() (int, *time.Time) {
	return 0, nil
}
