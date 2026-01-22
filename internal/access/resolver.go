// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import "context"

// LocationResolver provides dynamic location information for access control.
// Used to resolve tokens like $room_members at evaluation time.
type LocationResolver interface {
	// CurrentLocation returns the location ID for a character.
	CurrentLocation(ctx context.Context, charID string) (string, error)

	// CharactersAt returns character IDs present at a location.
	CharactersAt(ctx context.Context, locationID string) ([]string, error)
}

// NullResolver is a LocationResolver that returns empty results.
// Used when location-based permissions are not needed.
type NullResolver struct{}

// CurrentLocation always returns empty string.
func (NullResolver) CurrentLocation(_ context.Context, _ string) (string, error) {
	return "", nil
}

// CharactersAt always returns empty slice.
func (NullResolver) CharactersAt(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
