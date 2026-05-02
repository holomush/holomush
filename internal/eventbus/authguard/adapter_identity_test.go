// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/authguard"
)

func TestToSessionIdentityCarriesAllFieldsForCharacter(t *testing.T) {
	id, err := authguard.NewCharacterIdentity("01ABC", "01XYZ", "01DEF")
	assert.NoError(t, err)
	got := authguard.ToSessionIdentity(id)
	assert.Equal(t, eventbus.IdentityKindCharacter, got.Kind)
	assert.Equal(t, "01ABC", got.PlayerID)
	assert.Equal(t, "01XYZ", got.CharacterID)
	assert.Equal(t, "01DEF", got.BindingID)
}

func TestToSessionIdentityCarriesPlayerIDOnlyForPlayer(t *testing.T) {
	id, err := authguard.NewPlayerIdentity("01ABC")
	assert.NoError(t, err)
	got := authguard.ToSessionIdentity(id)
	assert.Equal(t, eventbus.IdentityKindPlayer, got.Kind)
	assert.Equal(t, "01ABC", got.PlayerID)
	assert.Empty(t, got.CharacterID)
	assert.Empty(t, got.BindingID)
}

func TestToSessionIdentityCarriesPluginNameAndInstanceForPlugin(t *testing.T) {
	id, err := authguard.NewPluginIdentity("mod-filter", "01INST")
	assert.NoError(t, err)
	got := authguard.ToSessionIdentity(id)
	assert.Equal(t, eventbus.IdentityKindPlugin, got.Kind)
	assert.Equal(t, "mod-filter", got.PluginName)
	assert.Equal(t, "01INST", got.InstanceID)
}

func TestToSessionIdentityProducesOperatorWithEmptyIDs(t *testing.T) {
	id := authguard.NewOperatorIdentity()
	got := authguard.ToSessionIdentity(id)
	assert.Equal(t, eventbus.IdentityKindOperator, got.Kind)
	assert.Empty(t, got.PlayerID)
	assert.Empty(t, got.CharacterID)
}
