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

func TestInfo_IsDetachable(t *testing.T) {
	info := &Info{Status: StatusActive}
	assert.True(t, info.IsActive())

	info.Status = StatusDetached
	assert.False(t, info.IsActive())
}
