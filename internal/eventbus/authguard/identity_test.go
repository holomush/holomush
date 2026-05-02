// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/authguard"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestNewCharacterIdentityRequiresAllThreeIDs(t *testing.T) {
	cases := []struct {
		name        string
		playerID    string
		characterID string
		bindingID   string
	}{
		{"empty player rejected", "", "01XYZ", "01DEF"},
		{"empty character rejected", "01ABC", "", "01DEF"},
		{"empty binding rejected", "01ABC", "01XYZ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := authguard.NewCharacterIdentity(tc.playerID, tc.characterID, tc.bindingID)
			errutil.AssertErrorCode(t, err, "AUTHGUARD_IDENTITY_INVALID")
		})
	}
}

func TestNewCharacterIdentityHappyPath(t *testing.T) {
	id, err := authguard.NewCharacterIdentity("01ABC", "01XYZ", "01DEF")
	require.NoError(t, err)
	assert.Equal(t, authguard.IdentityKindCharacter, id.Kind)
	assert.Equal(t, "01ABC", id.PlayerID)
	assert.Equal(t, "01XYZ", id.CharacterID)
	assert.Equal(t, "01DEF", id.BindingID)
}

func TestNewPlayerIdentityRequiresPlayerID(t *testing.T) {
	_, err := authguard.NewPlayerIdentity("")
	errutil.AssertErrorCode(t, err, "AUTHGUARD_IDENTITY_INVALID")

	id, err := authguard.NewPlayerIdentity("01ABC")
	require.NoError(t, err)
	assert.Equal(t, authguard.IdentityKindPlayer, id.Kind)
	assert.Equal(t, "01ABC", id.PlayerID)
}

func TestNewPluginIdentityRequiresBoth(t *testing.T) {
	_, err := authguard.NewPluginIdentity("", "01INST")
	errutil.AssertErrorCode(t, err, "AUTHGUARD_IDENTITY_INVALID")
	_, err = authguard.NewPluginIdentity("mod-filter", "")
	errutil.AssertErrorCode(t, err, "AUTHGUARD_IDENTITY_INVALID")

	id, err := authguard.NewPluginIdentity("mod-filter", "01INST")
	require.NoError(t, err)
	assert.Equal(t, authguard.IdentityKindPlugin, id.Kind)
	assert.Equal(t, "mod-filter", id.PluginName)
	assert.Equal(t, "01INST", id.InstanceID)
}

func TestNewOperatorIdentityCarriesNoIDs(t *testing.T) {
	id := authguard.NewOperatorIdentity()
	assert.Equal(t, authguard.IdentityKindOperator, id.Kind)
	assert.Empty(t, id.PlayerID)
	assert.Empty(t, id.CharacterID)
}
