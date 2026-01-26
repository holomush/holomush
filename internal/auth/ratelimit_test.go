// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/auth"
)

func TestRateLimiter_CheckFailures(t *testing.T) {
	t.Run("no failures returns no delay", func(t *testing.T) {
		result := auth.CheckFailures(0, nil)
		assert.Zero(t, result.Delay)
		assert.False(t, result.RequiresCaptcha)
		assert.False(t, result.IsLockedOut)
	})

	t.Run("1-3 failures returns progressive delay", func(t *testing.T) {
		result1 := auth.CheckFailures(1, nil)
		assert.Equal(t, time.Second, result1.Delay)
		assert.False(t, result1.RequiresCaptcha)

		result2 := auth.CheckFailures(2, nil)
		assert.Equal(t, 2*time.Second, result2.Delay)

		result3 := auth.CheckFailures(3, nil)
		assert.Equal(t, 4*time.Second, result3.Delay)
	})

	t.Run("4-6 failures requires captcha (web)", func(t *testing.T) {
		result4 := auth.CheckFailures(4, nil)
		assert.True(t, result4.RequiresCaptcha)
		assert.Equal(t, 8*time.Second, result4.Delay)

		result6 := auth.CheckFailures(6, nil)
		assert.True(t, result6.RequiresCaptcha)
		assert.Equal(t, 32*time.Second, result6.Delay)
	})

	t.Run("7+ failures causes lockout", func(t *testing.T) {
		result := auth.CheckFailures(7, nil)
		assert.True(t, result.IsLockedOut)
		assert.Equal(t, auth.LockoutDuration, result.LockoutRemaining)
	})

	t.Run("existing lockout is detected", func(t *testing.T) {
		future := time.Now().Add(10 * time.Minute)
		result := auth.CheckFailures(0, &future)
		assert.True(t, result.IsLockedOut)
		assert.True(t, result.LockoutRemaining > 0)
		assert.True(t, result.LockoutRemaining <= 10*time.Minute)
	})
}

func TestRateLimiter_IsLockedOut(t *testing.T) {
	now := time.Now()

	t.Run("nil locked_until means not locked", func(t *testing.T) {
		assert.False(t, auth.IsLockedOut(nil))
	})

	t.Run("past locked_until means not locked", func(t *testing.T) {
		past := now.Add(-time.Hour)
		assert.False(t, auth.IsLockedOut(&past))
	})

	t.Run("future locked_until means locked", func(t *testing.T) {
		future := now.Add(time.Hour)
		assert.True(t, auth.IsLockedOut(&future))
	})
}

func TestRateLimiter_ComputeLockoutTime(t *testing.T) {
	t.Run("7 failures returns lockout time", func(t *testing.T) {
		lockout := auth.ComputeLockoutTime(7)
		assert.NotNil(t, lockout)
		assert.True(t, lockout.After(time.Now()))
	})

	t.Run("less than 7 failures returns nil", func(t *testing.T) {
		assert.Nil(t, auth.ComputeLockoutTime(6))
	})

	t.Run("more than 7 failures still returns lockout", func(t *testing.T) {
		lockout := auth.ComputeLockoutTime(10)
		assert.NotNil(t, lockout)
	})
}

func TestRateLimiter_ResetOnSuccess(t *testing.T) {
	t.Run("returns zero failures and nil lockout", func(t *testing.T) {
		failures, lockout := auth.ResetOnSuccess()
		assert.Equal(t, 0, failures)
		assert.Nil(t, lockout)
	})
}
