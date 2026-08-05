// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/attribute"
)

// TestABACConfigCarriesTheTwoPlan0213Seams pins the two func/interface seams
// this plan adds to ABACConfig.
//
// Both are silently consequential when left nil: a nil PlayerRoleLookup removes
// player.roles AND viewer.roles, and a nil CharacterOwnerResolver removes all
// three derived property peers. In neither case does anything error — the
// namespace is simply absent, every condition referencing it is false, and whole
// policy families default-deny in production while unit tests that stub the bag
// stay green (RESEARCH P-7's failure mode).
//
// This test is the compile-time-plus-reflection guard that the seams exist with
// the SHARED types, so a refactor cannot quietly swap one for a locally-declared
// twin.
func TestABACConfigCarriesTheTwoPlan0213Seams(t *testing.T) {
	cfgType := reflect.TypeOf(ABACConfig{})

	roleField, ok := cfgType.FieldByName("PlayerRoleLookup")
	require.True(t, ok, "ABACConfig MUST carry PlayerRoleLookup — the func-field seam that "+
		"reaches the concrete *PostgresRoleStore without widening the store.RoleStore interface")
	assert.Equal(t,
		reflect.TypeOf((*attribute.PlayerRoleLookup)(nil)).Elem(), roleField.Type,
		"PlayerRoleLookup MUST be the SHARED attribute.PlayerRoleLookup type, so the viewer "+
			"and player namespaces cannot be wired to different sources")

	ownerField, ok := cfgType.FieldByName("CharacterOwnerResolver")
	require.True(t, ok, "ABACConfig MUST carry CharacterOwnerResolver — without it the derived "+
		"property peers are absent and the viewer twins default-deny silently")
	assert.Equal(t,
		reflect.TypeOf((*attribute.CharacterOwnerResolver)(nil)).Elem(), ownerField.Type,
		"CharacterOwnerResolver MUST be the consumer-side attribute.CharacterOwnerResolver "+
			"interface, not a storage-package concrete type")
}
