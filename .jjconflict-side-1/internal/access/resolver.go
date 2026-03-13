// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import "context"

// LocationResolver provides dynamic location information for access control.
// Used to resolve $here and $here:* tokens at evaluation time.
type LocationResolver interface {
	// CurrentLocation returns the location ID for a character.
	// Used to resolve the $here token.
	CurrentLocation(ctx context.Context, charID string) (string, error)

	// CharactersAt returns character IDs present at a location.
	// Used to resolve $here:* token (future scope - not yet implemented).
	// Defined in interface per spec to ensure implementers provide it.
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
