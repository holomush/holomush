// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
	"context"
	"testing"

	"github.com/holomush/holomush/internal/access"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocationResolver_Interface(_ *testing.T) {
	// Verify NullResolver satisfies LocationResolver interface with both value and pointer
	var _ access.LocationResolver = access.NullResolver{}
	var _ access.LocationResolver = (*access.NullResolver)(nil)
}

func TestNullResolver_CurrentLocation(t *testing.T) {
	tests := []struct {
		name   string
		charID string
	}{
		{"empty character ID", ""},
		{"valid character ID", "char:01ABC"},
		{"any string", "anything"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := access.NullResolver{}
			location, err := resolver.CurrentLocation(context.Background(), tt.charID)

			require.NoError(t, err)
			assert.Empty(t, location)
		})
	}
}

func TestNullResolver_CharactersAt(t *testing.T) {
	tests := []struct {
		name       string
		locationID string
	}{
		{"empty location ID", ""},
		{"valid location ID", "location:01ABC"},
		{"any string", "anything"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := access.NullResolver{}
			chars, err := resolver.CharactersAt(context.Background(), tt.locationID)

			require.NoError(t, err)
			assert.Empty(t, chars)
		})
	}
}
