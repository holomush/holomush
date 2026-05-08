// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRealClockReturnsCurrentTime(t *testing.T) {
	c := NewRealClock()
	before := time.Now()
	got := c.Now()
	after := time.Now()
	assert.True(t, !got.Before(before) && !got.After(after))
}

func TestFakeClockReturnsAndAdvances(t *testing.T) {
	t0 := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	c := NewFakeClock(t0)
	assert.Equal(t, t0, c.Now())
	c.Advance(45 * time.Second)
	assert.Equal(t, t0.Add(45*time.Second), c.Now())
}
