// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStatus_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{"active", StatusActive, true},
		{"detached", StatusDetached, true},
		{"expired", StatusExpired, true},
		{"empty", Status(""), false},
		{"unknown", Status("unknown"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.IsValid())
		})
	}
}

func TestStatus_String(t *testing.T) {
	assert.Equal(t, "active", StatusActive.String())
	assert.Equal(t, "detached", StatusDetached.String())
	assert.Equal(t, "expired", StatusExpired.String())
}

func TestInfo_IsActive(t *testing.T) {
	info := &Info{Status: StatusActive}
	assert.True(t, info.IsActive())

	info.Status = StatusDetached
	assert.False(t, info.IsActive())
}

func TestInfo_IsExpired(t *testing.T) {
	t.Run("returns false when ExpiresAt is nil", func(t *testing.T) {
		info := &Info{ExpiresAt: nil}
		assert.False(t, info.IsExpired())
	})

	t.Run("returns true when ExpiresAt is in the past", func(t *testing.T) {
		past := time.Now().Add(-1 * time.Hour)
		info := &Info{ExpiresAt: &past}
		assert.True(t, info.IsExpired())
	})

	t.Run("returns false when ExpiresAt is in the future", func(t *testing.T) {
		future := time.Now().Add(1 * time.Hour)
		info := &Info{ExpiresAt: &future}
		assert.False(t, info.IsExpired())
	})
}
