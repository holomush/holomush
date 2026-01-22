// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package accesstest provides test helpers for access control.
package accesstest

import (
	"context"

	"github.com/holomush/holomush/internal/access"
)

// AllowAll is an AccessControl that allows everything.
type AllowAll struct{}

// Check always returns true.
func (AllowAll) Check(_ context.Context, _, _, _ string) bool {
	return true
}

// DenyAll is an AccessControl that denies everything.
type DenyAll struct{}

// Check always returns false.
func (DenyAll) Check(_ context.Context, _, _, _ string) bool {
	return false
}

// MapResolver is a simple LocationResolver backed by maps.
type MapResolver struct {
	Locations  map[string]string   // charID → locationID
	Characters map[string][]string // locationID → charIDs
}

// CurrentLocation returns the location for a character.
func (r *MapResolver) CurrentLocation(_ context.Context, charID string) (string, error) {
	return r.Locations[charID], nil
}

// CharactersAt returns characters at a location.
func (r *MapResolver) CharactersAt(_ context.Context, locationID string) ([]string, error) {
	return r.Characters[locationID], nil
}

// Verify interfaces are satisfied.
var (
	_ access.AccessControl    = AllowAll{}
	_ access.AccessControl    = DenyAll{}
	_ access.LocationResolver = (*MapResolver)(nil)
)
