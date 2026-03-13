// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
	"context"
	"testing"

	"github.com/holomush/holomush/internal/access"
	"github.com/stretchr/testify/assert"
)

type nestedContextKey struct{}

func TestIsSystemContext(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		expected bool
	}{
		{
			name:     "regular context returns false",
			ctx:      context.Background(),
			expected: false,
		},
		{
			name:     "system context returns true",
			ctx:      access.WithSystemSubject(context.Background()),
			expected: true,
		},
		{
			name: "nested system context returns true",
			ctx: context.WithValue(
				access.WithSystemSubject(context.Background()),
				nestedContextKey{}, "val",
			),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, access.IsSystemContext(tt.ctx))
		})
	}
}
