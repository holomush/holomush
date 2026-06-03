// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
)

func TestConnection_FocusKeyNilByDefault(t *testing.T) {
	t.Parallel()
	// Zero-value Connection: INV-SCENE-15 asserts FocusKey starts nil.
	var c Connection
	assert.Nil(t, c.FocusKey, "INV-SCENE-15: new Connection MUST default to nil FocusKey (= grid focus)")
}

func TestConnection_FocusKeyAcceptsSceneKey(t *testing.T) {
	t.Parallel()
	sceneID := ulid.Make()
	fk := &FocusKey{Kind: FocusKindScene, TargetID: sceneID}
	c := Connection{FocusKey: fk}
	assert.NotNil(t, c.FocusKey)
	assert.Equal(t, FocusKindScene, c.FocusKey.Kind)
	assert.Equal(t, sceneID, c.FocusKey.TargetID)
}
