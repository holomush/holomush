// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard

import "github.com/samber/oops"

// NewCharacterIdentity creates a character Identity, rejecting empty fields.
func NewCharacterIdentity(playerID, characterID, bindingID string) (Identity, error) {
	if playerID == "" {
		return Identity{}, oops.Code("AUTHGUARD_IDENTITY_INVALID").
			With("kind", "character").With("field", "playerID").
			Errorf("character identity requires non-empty playerID")
	}
	if characterID == "" {
		return Identity{}, oops.Code("AUTHGUARD_IDENTITY_INVALID").
			With("kind", "character").With("field", "characterID").
			Errorf("character identity requires non-empty characterID")
	}
	if bindingID == "" {
		return Identity{}, oops.Code("AUTHGUARD_IDENTITY_INVALID").
			With("kind", "character").With("field", "bindingID").
			Errorf("character identity requires non-empty bindingID")
	}
	return Identity{
		Kind:        IdentityKindCharacter,
		PlayerID:    playerID,
		CharacterID: characterID,
		BindingID:   bindingID,
	}, nil
}

// NewPlayerIdentity creates a player Identity, rejecting an empty playerID.
func NewPlayerIdentity(playerID string) (Identity, error) {
	if playerID == "" {
		return Identity{}, oops.Code("AUTHGUARD_IDENTITY_INVALID").
			With("kind", "player").With("field", "playerID").
			Errorf("player identity requires non-empty playerID")
	}
	return Identity{Kind: IdentityKindPlayer, PlayerID: playerID}, nil
}

// NewPluginIdentity creates a plugin Identity, rejecting empty pluginName or instanceID.
func NewPluginIdentity(pluginName, instanceID string) (Identity, error) {
	if pluginName == "" {
		return Identity{}, oops.Code("AUTHGUARD_IDENTITY_INVALID").
			With("kind", "plugin").With("field", "pluginName").
			Errorf("plugin identity requires non-empty pluginName")
	}
	if instanceID == "" {
		return Identity{}, oops.Code("AUTHGUARD_IDENTITY_INVALID").
			With("kind", "plugin").With("field", "instanceID").
			Errorf("plugin identity requires non-empty instanceID")
	}
	return Identity{Kind: IdentityKindPlugin, PluginName: pluginName, InstanceID: instanceID}, nil
}

// NewOperatorIdentity creates an operator Identity. Operator reads
// go through AdminReadStream (INV-CRYPTO-24); Guard always denies.
func NewOperatorIdentity() Identity {
	return Identity{Kind: IdentityKindOperator}
}
