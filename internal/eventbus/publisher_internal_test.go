// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package eventbus

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func TestInitialParticipantsForContextCharacterSeedsRecipient(t *testing.T) {
	got := initialParticipantsForContext(dek.ContextID{Type: "character", ID: "01HRECIPIENT0000000000000000"})
	require.Len(t, got, 1)
	require.Equal(t, "01HRECIPIENT0000000000000000", got[0].CharacterID)
	require.Empty(t, got[0].BindingID, "binding resolved downstream in GetOrCreate, not here")
}

func TestInitialParticipantsForContextSceneIsNil(t *testing.T) {
	require.Nil(t, initialParticipantsForContext(dek.ContextID{Type: "scene", ID: "01HSCENE"}))
}

func TestInitialParticipantsForContextOtherIsNil(t *testing.T) {
	require.Nil(t, initialParticipantsForContext(dek.ContextID{Type: "location", ID: "01HLOC"}))
}
